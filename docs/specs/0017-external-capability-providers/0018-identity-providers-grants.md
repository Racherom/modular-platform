# 0018. Identity providers and grants

**Parent**: [0017. External capability providers](index.md)

## Summary

Making a resource (a bucket, a database) and making a login are now two provisioners. The resource provisioner's capability still `yields` what the consumer gets, but takes the login fields from an identity provisioner, bound through an `identity` slot on the resource provider. An identity capability lists the resource types it can grant, makes one login per binding, and grants it exactly that resource. The same section brings adoption of existing resources, sharing one resource between consumers, `uses` on external instances, and fields linked with `same_as`. Postgres moves to the same split.

## The `identity` section (identity capabilities)

```yaml
apiVersion: modular-platform/v1
kind: Capability
name: postgresql-roles
description: Admin access to a PostgreSQL server that makes one login per consumer and grants it a database.
scheme: postgres
fields:
  host: {type: string, description: Host name of the server.}
  port: {type: int, description: TCP port of the server.}
  url: {type: url, description: postgres://host:port.}
  admin_user: {type: string, description: Superuser login.}
  admin_password: {type: secret, description: Superuser password.}
  sslmode: {type: enum, values: [disable, verify-full], description: TLS mode to connect with.}
  ca: {type: certificate, optional: true, description: Root CA that signs the server certificate.}
identity:
  type: postgresql-role
  grants:
    postgresql-database: {}
  named:
    user: {pattern: "^[a-z_][a-z0-9_]{0,62}$"}
  login: user
  credentials:
    user: named
    password: generate
  job:
    image: postgres:18.6-alpine3.24
    provision: |
      ...
    revoke: |
      ...
    list: |
      ...
```

| Field | Rule |
|---|---|
| `identity.type` | the login type list rows carry (`iam-user`, `postgresql-role`); 0002 name rules |
| `identity.grants` | a map with one or more resource types (each the `yields.resource` of some resource capability) as keys; each value may set `offers`, the default actions it grants on that type ([0023](0023-access-actions.md)) |
| `identity.named` | the login values the graph names per binding (field name to rule: `pattern` required, optional `name` template); a named value is handed to the consumer only when `credentials` lists it as `named` (Postgres `user` is, the IAM `user` is not) |
| `identity.login` | one of `named`; the value list and revoke use |
| `identity.credentials.<f>` | the fields a binding's login hands on: `named`, `generate` (per binding file, 0013), or `output` (a `secret` file the login job writes); each has a type: `named` fields are `string`, the others `secret` |
| `identity.job.image` | tagged image, never `latest`, with `/bin/sh` |
| `identity.job.provision`, `.revoke`, `.list` | all three required; `.retire` optional, required when a credential is `output` and a login can hold more than one key ([0024](0024-key-rotation-reload.md)) |

A capability has `yields` or `identity`, never both. Catalog code `invalid_identity` for any rule above.

## Resource capabilities: `yields` changes

- New `yields.resource`: the resource type (`s3-bucket`, `postgresql-database`); required on every provisioner except `tls-certificate-provisioner` (no `identity`, no login: certificates stay as 0014).
- New field source `{identity: <f>}`: the yielded field takes credential `f` of the bound identity provider's login. A provisioner with any `{identity: ...}` source needs an identity slot on every provide of it.
- `yields.login` and `yields.job.revoke` are removed (they move to the identity capability). New `yields.job.list` (required when `resource` is set).
- `yields.named` entries may set a name template on the field: `fields.<f>.name: "{graph}-{consumer}-{slot}"`. Tokens `{graph}`, `{consumer}`, `{slot}`; the default stays `{consumer}_{slot}` (0014). The filled name is lower cased; when longer than 63 characters it becomes its first 54 characters with any trailing separator (`-`, `_`, `.`) removed, then the template's separator, then the first 8 hex digits of SHA-256 of `<consumer>.<slot>` (0014's cut, never ending in a separator; the hash keeps two long names apart). The result is then checked against the field's `pattern` (`invalid_named_value` as 0014). `s3`'s `bucket` uses `{graph}-{consumer}-{slot}` (bucket names allow no `_`; graph and instance names are lower case letters, digits and `-` by 0002). The same `name` key works on `identity.named` fields.
- The binding's object form (0012) carries the resource's named values and the identity's named values side by side; a field name in both is `invalid_identity` on the identity capability at catalog load when it grants that resource type.

## The identity slot

- A module entry names it on a provide: `provides.<q>.identity: <slot>`, where `uses.<slot>` has an identity capability. Missing on a provide whose capability has `{identity: ...}` sources: `missing_identity_slot` (catalog).
- An external names it the same way, `external.provides.<q>.identity: <slot>`, and gains `external.uses.<slot>: {provider: <instance>.<provide>, capability: <identity capability>, version: <range>}` (all three required, nothing inferred). An external's `uses` may hold only slots its provides name: anything else is `invalid_external_use`. The bound provider may be an external or a module (an AWS bucket manager with logins from a module is allowed when that module's identity grants `s3-bucket`).
- One identity provide may serve the identity slots of several resource providers; its login names stay unique per identity provide across all of them (`duplicate_named_value`).
- The slot delivers nothing to the container: no env, no files, no `depends_on`. Only the jobs read it.
- It may bind a provide of its own instance (the one exception to 0012's `p ≠ i`). For an identity slot, 0012's provide match counts the own instance as a candidate; recommendation: when the own instance has exactly one fitting provide, the checks recommend that provide and report no ambiguity; otherwise 0002's rules apply over all candidates (own instance included). 0012 gets a pointer to this rule. The cycle check ignores identity slots, since they add no `depends_on`.
- Checks on the binding: the bound provide's `identity.grants` holds the resource type, else `identity_not_granting`; every `{identity: f}` names a credential the identity has with the same type, else `identity_credentials_mismatch`. Both at `modules.<p>.uses.<slot>`.

## The login job

For a binding consumer `c`, slot `s`, through resource provider `p` (provide `q`) whose identity slot binds `i` (provide `r`): service `<i>_login_<c>_<s>`, same image rules, entrypoint, hardening and networks as 0014's provision job (`egress` when `i` is external).

- env: `MP_CONSUMER`, `MP_SLOT`, `MP_CONSUMER_HOST`, `MP_MARKER` (0017); `i`'s admin fields as `MP_ADMIN_<F>` (files as `MP_ADMIN_<F>_FILE` at `/run/secrets/admin/<f>`); each identity named value as `MP_<F>`; `MP_GRANT_TYPE` (the resource type), `MP_GRANT_<SCOPE>` per scope of the type's access vocabulary (0023), and the resource's named values as `MP_RESOURCE_<F>` (for S3: `MP_RESOURCE_BUCKET`); `MP_ADOPT` `1` or `0`.
- files: `generate` credentials at `/run/secrets/bind/<f>` with `MP_<F>_FILE` (the consumer's own file, 0013); `output` credentials writable at `/out/<f>`, a bind mount of `./secrets/<c>/uses_<s>_<f>` created empty, mode 0600, by `setup.sh` or the deployer, written in place (0014).
- `depends_on`: the resource job `<p>_provision_<c>_<s>` with `service_completed_successfully`, and `i` (`service_healthy` when it has a healthcheck) unless `i` is external or `i` is `p`.
- The consumer's `depends_on` gets the login job with `service_completed_successfully` (besides the resource job and `p`, as before).
- Managed and platform labels: `job: login`, `instance: <i>` (plus `module` when `i` is a module), like 0014's `job: provision`. The deployer guard accepts `login` wherever it accepts `provision` (`restart: "no"`).
- The resource job loses its admin access to logins: its env keeps `MP_ADMIN_*` of `p`, the named resource values, `MP_MARKER` and gains `MP_ADOPT` and `MP_EXTERNAL` (`1` when `p` is external). Both jobs get `MP_EXTERNAL` for their own provider.
- A login job with `restart: "no"` that exited 0 on an earlier `up` runs again on the next `up`, because the consumer depends on it with `service_completed_successfully` (0014 spiked this for provision jobs; the render tests reuse that check for login jobs).

Every login job is an "ensure", like 0014: a second run adds no login, keeps the credentials the consumer has, and grants the same resource.

## Adoption and sharing

- A binding object may say `adopt: true`. The resource job then accepts a resource it did not make (no marker) and leaves it without a marker; without `adopt`, an existing resource with no marker or another stack's marker fails the job with "<type> <name> exists and was not made by this stack".
- The login is always fresh: adoption never takes over an existing login. An existing login is used through a direct external instance of the yielded capability (0012, AC-15 of 0011), which is the "add one with url and login" path.
- Sharing: per resource provide, two bindings may name the same resource value when every later binding (consumer, then slot order) says `adopt: true`: check warning `shared_resource` at the later binding. Without it, 0014's `duplicate_named_value` stays. Login named values are always unique per identity provide.

## `same_as`

- An external field value may be `{same_as: <instance>.<provide>.<field>}` instead of text, for any field type, secret ones included. The target is a field of an external provide in the same graph with the same type and (for `enum`) the same values; chains resolve to their end.
- A missing target, another type, a target that is not external, or a cycle is `invalid_same_as` at the linking field.
- A non secret field takes the target's text. A secret or certificate field takes the target's placed file: one file `secrets/<j>/<q2>_<f2>` (the end of the chain; 0013's external placed path, which gets a pointer to this rule) is mounted for every linked field, so `setup.sh` lists it once and the managed review (0006) asks it once.
- Managed review: a chain is one `placed` entry, reporting the instance, capability and field of the chain's end; `field` is any capability field name (0011 already widened it); `placed_missing` checks that one path. The empty `output` files the deployer or `setup.sh` creates (login job credentials) are never placed entries and never count as `exists`.
- The canonical Go and TS writers write `{same_as: ...}` and a binding with `adopt: true` as objects, with round trip fixtures for both (0011 AC-19); the editor shows "same as <path>" in the node detail.

## The Postgres split

- `postgresql-provisioner` keeps its admin fields; `yields: {capability: postgresql, resource: postgresql-database, named: [database], fields: {host, port, url, sslmode, ca: {admin: ...}, database: named, user: {identity: user}, password: {identity: password}}}`, job `provision` and `list`.
- `postgresql-roles` as above, `grants: [postgresql-database]`.
- The `postgres` module provides `server` (`postgresql-provisioner`, `identity: identity`) and `roles` (`postgresql-roles`), with the same field sources; `uses.identity` is bound to `postgres.roles` in the graph (the checks recommend it, 0028). An external Postgres server is one external with both provides and `same_as` for the shared admin fields.

**Normative script behavior** (exact SQL is the build's; names only through psql variables and `\gexec`, passwords read inside psql, as 0014):

Resource job (`postgresql-provisioner`), as admin on database `postgres`:
1. Create the marker role `MP_MARKER` (`NOLOGIN`) when missing.
2. Owner role `<marker>_o_<first 8 hex of SHA-256 of the database name>` (`NOLOGIN`, member of the marker), created when missing.
3. Database `MP_DATABASE`: missing → create it owned by the owner role. Owned by the owner role → nothing. Owned by a member of the marker (the 0014 shape) → move ownership of the database and of every object in it to the owner role, and grant the owner role to that login. Owned by anyone else → with `MP_ADOPT=1`, move ownership of the database and of every schema, table, sequence, view and function in it (connected to that database, object by object, in one transaction, never `REASSIGN OWNED`) to the owner role; objects that belong to an extension or to a superuser are left as they are and named in the job's log (names only); without `MP_ADOPT`, fail. Adoption takes ownership away from the old owner, so other applications using that role lose their DDL rights there; the README says so.
4. `REVOKE CONNECT ON DATABASE ... FROM PUBLIC` only on a database this job created or took over from the 0014 shape, never on an adopted one; on an internal server only (the `postgres` module), also `REVOKE CONNECT ON DATABASE postgres, template1 FROM PUBLIC`. An external server's other databases and grants are never touched. The login job grants `CONNECT` explicitly either way.

Login job (`postgresql-roles`):
1. Marker role as above.
2. Role `MP_USER`: missing → `CREATE ROLE ... LOGIN PASSWORD ...`, `GRANT <marker> TO ...`. A member of the marker → `ALTER ROLE ... LOGIN PASSWORD ...`. Not a member → fail "role <name> exists and was not made by the platform".
3. Grant: `GRANT <owner role> TO <user>` (inheriting), `ALTER ROLE <user> IN DATABASE <db> SET role = <owner role>` (objects it makes belong to the owner role, so a shared database stays usable by every login), `GRANT CONNECT ON DATABASE <db> TO <user>`.
4. `SELECT pg_reload_conf()` (0014's certificate reload moves here).

Revoke (managed only, 0020): for each name in `MP_REVOKE` that is a member of the marker: `ALTER ROLE ... NOLOGIN`, terminate its sessions. Never drops a role or database.

List: one `resource` row per database (`stack` = the marker its owner role belongs to, else `""`), one `login` row per role with `LOGIN` or a marker membership (`active` = `rolcanlogin`, `grants` = the databases whose owner role it holds). System roles (`pg_*`) and the admin are left out.

## Critical test scenarios

- Checks: identity slot auto bound to its own instance; `identity_not_granting` (an `iam` provide on a Postgres slot); `identity_credentials_mismatch`; `missing_identity_slot`; `invalid_external_use`; `invalid_same_as` for each cause; `shared_resource` warning; a duplicate login name stays `duplicate_named_value`; bucket default name from the template and its 63 character cut (**AC-3**, **AC-6**, **AC-7**).
- Golden: a binding through `postgres` renders the resource job, then the login job, then the consumer, with the env and mounts above; a binding through an external Postgres has both jobs on `egress` and no `depends_on` on the external (**AC-2**, **AC-14**).
- e2e (real `postgres`): 0014's two consumer scenarios on the split (**AC-2**); a database made by the 0014 shape is taken over with its rows and its login's password intact; adopt a hand made database with tables, a sequence and a schema: the new login reads and writes them and the hand made role still owns nothing outside it (**AC-6**); two consumers share one adopted database, each creates a table, each reads the other's (**AC-6**); `ALTER ROLE ... IN DATABASE ... SET role` takes effect on Postgres 18.6 for a new session of each login (**AC-2**); an adopted database on an external test server: the server's other databases keep their `CONNECT` grants to `PUBLIC`, and an extension owned object is left and logged (**AC-6**).

## Rationale (short)

You chose to split logins from resources so an identity provider can be exchanged on its own (an AWS account's IAM, the bundled server's IAM, later LDAP or certificates), and to apply it to Postgres too, before v1 is published. The identity job grants (your choice), because both AWS and SeaweedFS confirm `PutUserPolicy`, while a resource side grant would need a bucket policy naming a user created seconds earlier. The slot on the resource provider (your choice) keeps the graph saying which identity a server trusts, and the self binding keeps the common case (a server with its own logins) one instance. Runners up: one provisioner per admin API (simpler contract, but logins never exchangeable and two S3 script sets); the consumer binding naming both providers (every binding must repeat it). The owner role in Postgres is what lets a database be shared and adopted while each consumer keeps its own login.
