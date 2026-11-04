# 0014. Provisioners and their jobs

**Parent**: [0011. Capability and module contract](index.md)

## Summary

A provisioner is a capability that gives admin access to something that can create resources, and `yields` another capability per binding: `postgresql-provisioner` yields `postgresql`, a database plus its own login for one consumer. The capability file carries the job image and two scripts: a provision job per binding creates or updates what that binding names on every `up`, and a revoke job per provisioner provide disables logins that no binding names any more, keeping their data. Names come from the graph; when the graph leaves them out, the planner writes a default into it. The same mechanism issues certificates: `tls-certificate-provisioner` yields a server certificate for the consumer's host name.

## `yields`

```yaml
apiVersion: modular-platform/v1
kind: Capability
name: postgresql-provisioner
description: Admin access to a PostgreSQL server that creates a database and login per consumer.
scheme: postgres
fields:
  host: {type: string, description: Host name of the server.}
  port: {type: int, description: TCP port of the server.}
  url: {type: url, description: postgres://host:port.}
  admin_user: {type: string, description: Superuser login.}
  admin_password: {type: secret, description: Superuser password.}
  sslmode: {type: enum, values: [disable, verify-full], description: TLS mode to connect with.}
  ca: {type: certificate, optional: true, description: Root CA that signs the server certificate.}
yields:
  capability: postgresql
  named: [database, user]
  login: user
  fields:
    host: {admin: host}
    port: {admin: port}
    url: {admin: url}
    database: named
    user: named
    password: generate
    sslmode: {admin: sslmode}
    ca: {admin: ca}
  job:
    image: postgres:18.6-alpine3.24
    provision: |
      ...POSIX sh, see "postgresql-provisioner scripts"
    revoke: |
      ...
```

| Field | Rule |
|---|---|
| `yields.capability` | a known capability that is not itself a provisioner (`invalid_yields`) |
| `yields.named` | fields of the yielded capability the graph names per binding; each must be `string` with a `pattern` |
| `yields.login` | optional; one of `named`; the value the revoke job keeps. Without it there is no revoke job |
| `yields.fields.<f>` | one per field of the yielded capability: `{admin: <own field>}`, `named`, `generate` (per binding file, as 0013), or `output` (a `secret` or `certificate` file the provision job writes) |
| `yields.job.image` | tagged image, never `latest`, containing `/bin/sh` |
| `yields.job.provision` | required script; `yields.job.revoke` required exactly when `login` is set |

Catalog rule codes: `invalid_yields`, `missing_field_source`, `invalid_field_source`, `invalid_job`.

## Named values

- The graph may name each `yields.named` field in the binding's object form (0012). For a missing one the planner writes the default: `<consumer>_<slot>`, lower case, `-` as `_`. When that is longer than 63 characters it becomes its first 54 characters, `_`, and the first 8 hex digits of SHA-256 of `<consumer>.<slot>`. A default that still breaks the field's pattern is `invalid_named_value`, asking you to name it.
- The resolved graph carries the filled object form, so a saved graph keeps its names when an instance is renamed (AC-7).
- Per provisioner provide, every named field's value is unique across its bindings; a repeat is `duplicate_named_value` at the later binding in consumer, then slot order. A named `login` equal to the provider's `admin_user` source value is `invalid_named_value`.

## Jobs in the render

For a binding consumer `c`, slot `s`, provisioner instance `p`, provide `q`:

**Provision job** (one per binding), Compose service `<p>_provision_<c>_<s>` (names hold no `_`, so it is unique and never an instance name):
- `image`: `yields.job.image`; `entrypoint: ["/bin/sh", "-eu", "-c"]`, `command: [<provision script>]`; `restart: "no"`; hardening defaults (0013); network `internal`, plus `egress` when `p` is external.
- env: `MP_CONSUMER=<c>`, `MP_SLOT=<s>`, `MP_CONSUMER_HOST=<c>.<g>.internal`, `MP_MARKER` (the stack's marker name: `mp_provisioned_` plus the first 8 hex digits of SHA-256 of the graph name, so two stacks on one server never touch each other's logins); each non file admin field `f` as `MP_ADMIN_<F>`; each named value as `MP_<F>`.
- files: each file admin field at `/run/secrets/admin/<f>` with `MP_ADMIN_<F>_FILE`; each `generate` field at `/run/secrets/bind/<f>` with `MP_<F>_FILE` (the same per binding file the consumer gets, 0013); each `output` field writable at `/out/<f>` (a bind mount of `./secrets/<c>/uses_<s>_<f>`, which `setup.sh` or the deployer creates empty, mode 0600, so Compose finds it at the first `up`). A job writes an output in place (`cat > /out/<f>`), never by rename, so the consumer's mount sees the new content.
- `depends_on`: `p` (`service_healthy` when it has a healthcheck, else `service_started`); none when `p` is external.
- The consumer's `depends_on` gets this job with `service_completed_successfully`, besides `p` as 0002 says.

**Revoke job** (one per provisioner provide whose capability has a `login`, whether or not anything binds to it), service `<p>_revoke_<q>`: same image, entrypoint, hardening, networks and admin env and files as above; `MP_KEEP` = the `login` values of every current binding to `q`, sorted, joined by `,` (the patterns allow no `,`), empty when none; `depends_on` `p` and every provision job of `q` with `service_completed_successfully`. Nothing depends on it.

**Order on one provider**: a provider's own certificate job (its `tls` slot) runs before the provider starts (the provider depends on it); then the provision jobs; then the revoke job. So the provision job, which runs on every `up` and holds admin access, is where a provider that reloads its config on request picks up a reissued certificate (Postgres, below).

**Waiting**: `up -d --wait` must end with every long running service healthy or running and every job exited 0; the index's build task 1 decides the command that gives that.

## `postgresql-provisioner` scripts

Normative behavior (exact SQL is the build's; every name goes through psql variables, `:'v'` for literals and `:"v"` for identifiers, and `\gexec`, never shell string building; passwords are read inside psql with ``\set pw `cat <file>` ``, so they never appear on a command line):

Provision, connecting as the admin to database `postgres` with `sslmode` and `ca` from the admin fields:
1. Create the marker role `MP_MARKER` (`NOLOGIN`) when missing.
2. Role `MP_USER`: missing → `CREATE ROLE ... LOGIN PASSWORD ...` and `GRANT <marker> TO ...`. Present and a member of the marker → `ALTER ROLE ... LOGIN PASSWORD ...`. Present and not a member → fail with "role <name> exists and was not made by the platform" (so a graph cannot take over `postgres` or a hand made role).
3. Database `MP_DATABASE`: missing → `CREATE DATABASE ... OWNER ...`. Present, owned by `MP_USER` → nothing. Present, owned by another member of the marker → `ALTER DATABASE ... OWNER TO ...` (a renamed login keeps the data). Present, owned by anyone else → fail.
4. `REVOKE CONNECT ON DATABASE ... FROM PUBLIC`, `GRANT CONNECT ... TO <user>`, so a login opens only its own database (AC-4). Also `REVOKE CONNECT ON DATABASE postgres, template1 FROM PUBLIC` (each run; harmless when already done). The admin is a superuser and keeps access, and the healthcheck (`pg_isready`) needs none.
5. `SELECT pg_reload_conf()`, so a certificate the certificate job reissued is loaded without a restart (Postgres reads its TLS files again on reload).

Every step is an "ensure": a second run adds or drops no role or database, and the same password still signs in. `ALTER ROLE ... PASSWORD` stores a new salt each time, which is expected.

Revoke: for every role that is a member of the marker, has `LOGIN`, and is not in `MP_KEEP`: `ALTER ROLE ... NOLOGIN` and terminate its open sessions. Never drops a role or database (AC-6). Binding the same names again makes step 2 set `LOGIN` again.

## Certificate provisioner

- `tls-certificate-provisioner` (fields: `url`, `provisioner`, `password` secret, `ca` certificate) yields `tls-certificate` with `cert: output`, `key: output`, `ca: {admin: ca}`; no `named`, no `login`, so no revoke job (certificates expire on their own).
- `job.image`: `smallstep/step-cli` at an exact tag. Provision: when `/out/cert` is non empty and `step certificate needs-renewal /out/cert --expires-in 720h` says it does not need renewal, exit 0. Otherwise `step ca certificate "$MP_CONSUMER_HOST"` with `--ca-url "$MP_ADMIN_URL"`, `--root "$MP_ADMIN_CA_FILE"`, `--provisioner "$MP_ADMIN_PROVISIONER"`, `--provisioner-password-file "$MP_ADMIN_PASSWORD_FILE"`, `--not-after 2160h`, writing to `/tmp`, then copy both files into `/out` in place.
- `cert-issuer`'s `certs` provide sources: `url: {platform: url}`, `provisioner: {value: jobs}`, `password: {secret: jobs-password}`, `ca: {secret: root-ca}`.
- `cert-issuer` gains a second step-ca provisioner `jobs` (JWK) with `claims.maxTLSCertDuration` and `defaultTLSCertDuration` `2160h` (the `acme` provisioner keeps 24 hours). A JWK provisioner's public key and encrypted private key exist only after setup, so `ca.json` is not a template: `cert-issuer`'s setup step writes the whole `ca.json` (both provisioners, the name policy `*.$MP_GRAPH.internal` from the step env 0013 gives every step, and the `jobs-password` it generates), and the module mounts it as a setup secret (0015).
- An unchanged provider takes it through an optional slot of `tls-certificate` with `files` paths, and serves TLS when that slot is bound (0013's `platform: tls` and `platform: ca`). `postgres` does it with two templates: `postgresql.conf` (`ssl = on` and the file paths when `.Uses.tls.Bound`) and `pg_hba.conf` (`hostssl` lines only when bound, else `host`), so a plain connection is refused (AC-17).

## Edge cases

- A failing provision job (bad admin password, provider down past its healthcheck) keeps its consumer from starting; `up --wait` fails and the managed apply fails and rolls back (0006). The job's own log line names the binding, never a secret.
- Removing a whole provisioner instance removes its revoke job too; its server is gone from the stack, so nothing can sign in to it from there. #7 warns about data that does not move.
- An external provisioner (#10, #26): the jobs reach it over `egress` with the admin fields you gave once.
- Two stacks on one external server: each stack's revoke job sees only its own bindings, which is why the marker is per graph name (`MP_MARKER`). Two stacks with the same graph name on one server would share it; #26 decides whether the managed platform makes markers unique per deployment.

## Critical test scenarios

- e2e (`e2e` tag, real `postgres`): two consumers provisioned; each signs in to its own database and is refused by the other's (**AC-4**, **AC-5**); a second `up` adds or drops no row in `pg_roles` or `pg_database` and both consumers still sign in with their passwords (**AC-5**); a provisioned login cannot connect to `postgres` (**AC-4**).
- Unbind one consumer, `up`: its login is refused, its table still holds its rows; bind again with the same names, `up`: it signs in and reads them. The same with the only consumer, so the revoke job runs with an empty `MP_KEEP` (**AC-6**). A hand made role in `MP_KEEP` or not is never altered (**AC-6**).
- A graph naming `user: postgres`: `invalid_named_value`. A graph naming an existing hand made role: the job fails with the "not made by the platform" message, the consumer does not start (**AC-5**).
- Wrong admin password file: job exits non zero, consumer never starts, `up --wait` non zero (**AC-5**).
- Planner: default names written back; the 63 character cut; `duplicate_named_value` (**AC-7**).
- Certificate: first `up` issues a 90 day certificate for `postgres.modular-platform.internal`; a second `up` leaves the file unchanged; with the clock 61 days on (a test issuer with a short lifetime), `up` reissues and a new TLS connection to the running `postgres` sees the new certificate without a restart (**AC-17**).

## Rationale (short)

Splitting the provisioner from the yielded capability (your call) is principle 4 made concrete: a consumer asks for "a database", and whether it is made for it or handed to it is the provider's business, so the same consumer binds to the bundled server or an external database. Jobs live on the capability (your choice) because the SQL to create a database is the same for every PostgreSQL server, which lets an external server with an admin login provision exactly like the bundled one (#10, #26). One job per binding sees one password only (your choice); the revoke job keeps a mistaken unbind from losing data while still closing the login (your choice). Names in the graph, filled once by the planner, survive renames without anyone typing them (your choice). The marker role makes "only touch what we made" checkable inside Postgres. Runners up: one job per provider (fewer containers, sees every password); jobs on the provider module (each server ships its own SQL, externals need another home); one year certificates from `setup.sh` (no job, silent expiry after a year).
