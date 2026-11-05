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
| `yields.job.image` | tagged image, never `latest`, containing `/bin/sh` and a `sleep` that takes `infinity` (the wait anchor runs it, *Waiting*) |
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

**Revoke job** (one per provisioner provide whose capability has a `login`, whether or not anything binds to it), service `<p>_revoke_<q>`: same image, entrypoint, hardening, networks and admin env and files as above; `MP_KEEP` = the `login` values of every current binding to `q`, sorted, joined by `,` (the patterns allow no `,`), empty when none; `depends_on` `p` and every provision job of `q` with `service_completed_successfully`. No module depends on it; the stack's wait anchor `mp_jobs` does (*Waiting*, below).

**Order on one provider**: a provider's own certificate job (its `tls` slot) runs before the provider starts (the provider depends on it); then the provision jobs; then the revoke job. So the provision job, which runs on every `up` and holds admin access, is where a provider that reloads its config on request picks up a reissued certificate (Postgres, below).

**Waiting** (decided 2026-11-05; spikes in rationale.md): `up -d --wait` stays the one command (the README, `scripts/smoke*.sh`, the deployer). It must end with every long running service healthy or running and every job exited 0, on every `up`. Compose waits for a job correctly only through a `depends_on` with `service_completed_successfully`. A job nothing depends on breaks `--wait` both ways: when it exits 0 before the wait checks it, `up` fails with `container ... exited (0)`; when it is still running at the check, it counts as up, so a later exit 1 is missed. So every job gets a dependent. A provision job has its consumer. For every other job (today: the revoke jobs), the render adds one **wait anchor** per stack:

- Service `mp_jobs`, rendered in every flavor exactly when the stack has at least one job that no other service depends on, counted over the final set of services and leaving out `mp_jobs` itself. Instance names hold no `_`, and the name fits neither `<p>_provision_<c>_<s>` nor `<p>_revoke_<q>`, so it never meets another service name.
- `image`: the `yields.job.image` of the first such job by service name. When that job changes its image, Compose recreates `mp_jobs`, which does no harm since it holds no state. `entrypoint: ["sleep", "infinity"]`, no `command`, `init: true` (so it stops at once on `down`). So a job image also needs a `sleep` that takes `infinity`. No catalog check can see inside an image, so the task 10 e2e proves it for the images this release pins (Alpine, Debian).
- Hardening defaults (0013): `read_only: true`, `cap_drop: [ALL]`, `security_opt: [no-new-privileges:true]`, `user: "${MP_UID}:${MP_GID}"`; the task 10 e2e runs it with all four. `network_mode: none` and no `networks` and no alias (it talks to nothing; AC-12 and 0013's `internal` rule exempt it by name). No volumes, env, ports or secrets. `restart: unless-stopped` in the managed and platform flavors; no `restart` key in the static flavor, like every static service (the next `up` starts a stopped anchor again).
- `depends_on`: each such job with `service_completed_successfully`, sorted by name.
- Labels (managed and platform flavors): `graph`, `deployment`, and `io.github.racherom.modular-platform.job: wait`; no `instance` and no `module`, since it belongs to no instance. It adds nothing to `managed.json` (`RenderedProject`), which lists images and secrets: it has no secret, and its image is already listed for its job.
- The deployer's guard accepts it by these rules: `init` only `true`, `network_mode` only `none` and then no `networks`, the `job` label value `wait`, and `restart` `unless-stopped` (the guard's `no` stays for `provision` and `revoke` jobs). The deployer's live state leaves out containers with `job: wait`, so it shows no row with an empty instance.
- When the last such job leaves the graph, `mp_jobs` leaves the Compose file, and the README's `--remove-orphans` removes its container.

The revoke job stays a one shot job that runs again on every `up`. Spiked on Compose v5.6.1: with `mp_jobs` already running, a second `up` runs the revoke job again and still exits 1 with `service "<p>_revoke_<q>" didn't complete successfully: exit 1` when it fails. Consumers never wait for a revoke job, so a failing one stops no module from starting; it fails `up` (and the managed apply, 0006) instead.

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
- An external provisioner (#10, #26): the jobs reach it over `egress` with the admin fields you gave once, and so does every consumer bound through it, since its yielded `host` is the external server (0013, *Host name and networks*).
- A failing revoke job (wrong admin password, server down): `up --wait` exits non zero naming the job; the consumers already started keep running, and the logins it should have closed stay open until an `up` succeeds. Its log line names the provide, never a secret.
- Two stacks on one external server: each stack's revoke job sees only its own bindings, which is why the marker is per graph name (`MP_MARKER`). Two stacks with the same graph name on one server would share it; #26 decides whether the managed platform makes markers unique per deployment.

## Critical test scenarios

- e2e (`e2e` tag, real `postgres`): two consumers provisioned; each signs in to its own database and is refused by the other's (**AC-4**, **AC-5**); a second `up` adds or drops no row in `pg_roles` or `pg_database` and both consumers still sign in with their passwords (**AC-5**); a provisioned login cannot connect to `postgres` (**AC-4**).
- Unbind one consumer, `up`: its login is refused, its table still holds its rows; bind again with the same names, `up`: it signs in and reads them. The same with the only consumer, so the revoke job runs with an empty `MP_KEEP` (**AC-6**). A hand made role in `MP_KEEP` or not is never altered (**AC-6**). Every `up -d --wait` in this test exits 0, run after run (**AC-6**).
- A stack whose only binding was removed, with a wrong admin password file: the revoke job exits non zero, `up --wait` exits non zero naming it, and a second `up` (with `mp_jobs` already running) exits non zero again (**AC-6**).
- Golden: `mp_jobs` depends on every revoke job and nothing else, has `network_mode: none` and the first revoke job's image; a graph with no revoke job renders no `mp_jobs` (**AC-6**).
- A graph naming `user: postgres`: `invalid_named_value`. A graph naming an existing hand made role: the job fails with the "not made by the platform" message, the consumer does not start (**AC-5**).
- Wrong admin password file: job exits non zero, consumer never starts, `up --wait` non zero (**AC-5**).
- Planner: default names written back; the 63 character cut; `duplicate_named_value` (**AC-7**).
- Certificate: first `up` issues a 90 day certificate for `postgres.modular-platform.internal`; a second `up` leaves the file unchanged; with the clock 61 days on (a test issuer with a short lifetime), `up` reissues and a new TLS connection to the running `postgres` sees the new certificate without a restart (**AC-17**).

## Rationale (short)

Splitting the provisioner from the yielded capability (your call) is principle 4 made concrete: a consumer asks for "a database", and whether it is made for it or handed to it is the provider's business, so the same consumer binds to the bundled server or an external database. Jobs live on the capability (your choice) because the SQL to create a database is the same for every PostgreSQL server, which lets an external server with an admin login provision exactly like the bundled one (#10, #26). One job per binding sees one password only (your choice); the revoke job keeps a mistaken unbind from losing data while still closing the login (your choice). Names in the graph, filled once by the planner, survive renames without anyone typing them (your choice). The marker role makes "only touch what we made" checkable inside Postgres. The wait anchor (your choice, 2026-11-05) keeps `up -d --wait` the one command and the revoke job a true one shot job run on every `up`, at the cost of one idle container per stack; runners up there were a revoke job that idles after its work (no extra container, but it runs again only when its config changes) and a second command after `up` (a plain `up` would silently leave logins open). Runners up: one job per provider (fewer containers, sees every password); jobs on the provider module (each server ships its own SQL, externals need another home); one year certificates from `setup.sh` (no job, silent expiry after a year).
