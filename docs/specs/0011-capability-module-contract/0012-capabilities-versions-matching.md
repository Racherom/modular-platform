# 0012. Capabilities, versions and matching

**Parent**: [0011. Capability and module contract](index.md)
**Extended by**: [0018](../0017-external-capability-providers/0018-identity-providers-grants.md) (#10): identity slots may bind their own instance, external `uses`, `same_as` fields, `adopt` and `shared_resource`.

## Summary

A capability is now a file that names a protocol and lists the fields a binding of it carries, such as host, port, database and password, each with a type and a direction. A provide states the version it speaks, a slot (one named need of a module) states the range it accepts, and the planner binds a slot only to a provide whose capability matches (or is yielded by a provisioner) inside that range. The graph writes a binding as `instance`, `instance.provide`, or an object that also carries named values like a database name, and an instance can be an external service instead of a module.

## Capability kind

```yaml
apiVersion: modular-platform/v1
kind: Capability
name: postgresql
description: One PostgreSQL database and a login for one consumer.
scheme: postgres            # optional: the url scheme the platform source writes
fields:
  host:     {type: string, description: Host name of the server.}
  port:     {type: int, description: TCP port of the server.}
  url:      {type: url, description: postgres://host:port, without credentials.}
  database: {type: string, pattern: "[a-z_][a-z0-9_]{0,62}", description: Database name.}
  user:     {type: string, pattern: "[a-z_][a-z0-9_]{0,62}", description: Login name.}
  password: {type: secret, description: Password of the login.}
  sslmode:  {type: enum, values: [disable, verify-full], description: TLS mode to connect with.}
  ca:       {type: certificate, optional: true, description: Root CA that signs the server certificate.}
```

| Field | Type | Required | Rule |
|---|---|---|---|
| `name`, `description` | name, text | yes | as for modules; `description` one line |
| `scheme` | string | no | `^[a-z][a-z0-9+.-]*$`; needed when any provider uses the `platform: url` source |
| `fields.<f>` (the key) | field name | `^[a-z][a-z0-9_]{0,62}$` (snake case, like 0005's binding keys); unique in the capability |
| `fields.<f>.type` | `string`, `int`, `bool`, `url`, `enum`, `secret`, `certificate` | yes | `secret` and `certificate` always travel as files; the others as text |
| `fields.<f>.description` | text | yes | one line, shown in the editor and in env file comments |
| `fields.<f>.optional` | bool | no, default `false` | a provider may leave an optional field without a source; a required field must have one |
| `fields.<f>.pattern` | RE2 | no | `string` only, matched whole; checked on every value the planner knows (named values, `value` sources, external values) |
| `fields.<f>.values` | list | `enum` only | as 0005's enum rule |
| `fields.<f>.direction` | `to_consumer` (default), `to_provider`, `both` | no | `both` (`secret` or `certificate` only): the same file mounted on both sides (the binding token). `to_provider`: the consumer fills it; a file field reaches the provider as one file per consumer, a text field only as plan time template data (`.Clients`, 0013), so its consumer source must be known at plan time |
| `yields` | object | provisioners only | see [0014](0014-provisioners-jobs.md) |

**Catalog rules** (problem line as 0002, path `catalog/capabilities/<name>.yaml`): `missing_field`, `invalid_name`, `name_mismatch`, `duplicate_name`, `invalid_type`, `invalid_rule` (bad pattern, empty or duplicate `values`, `pattern` on a non string), `invalid_direction` (`both` on a type other than `secret` or `certificate`), plus 0014's `yields` rules.

**Built in capabilities** (each in `catalog/capabilities/`; fields shown by name, type in parentheses where not `string`):

| Capability | Fields | Notes |
|---|---|---|
| `http` | host, port (int), url | `scheme: http` |
| `postgresql` | as above | yielded by `postgresql-provisioner` |
| `postgresql-provisioner` | host, port (int), url, admin_user, admin_password (secret), sslmode (enum), ca (certificate, optional) | yields `postgresql` |
| `planner` | url, token (secret, both), ca (certificate, optional), caller_key (certificate, to_provider, optional) | the planner's REST contract |
| `deploy-target` | same as `planner` | 0007's contract |
| `platform-api` | url, ca (certificate, optional) | `api` behind the ingress |
| `oidc` | issuer (url), client_id, client_secret (secret, both), ca (certificate, optional), admin_email (optional), redirect_path (to_provider) | an OIDC provider and one client per binding; `admin_email` is the provider's bootstrap admin, which `api` binds as its first admin; `redirect_path` is the consumer's callback path under the public origin and base path (`/api/v1/auth/callback`), which the provider joins to its own origin at run time |
| `http-ingress` | origin (url), base_path, domain, http_port (int), https_port (int), ca (certificate, optional), route (to_provider, optional), upstream (url, to_provider, optional), server_name (to_provider, optional), stream (bool, to_provider, optional) | the public entry a browser sees (principle 12). A consumer that sets `route` (a path prefix like `/auth`) and `upstream` (its own url) is served there; one that sets neither only learns the origin and ports. `server_name` is the TLS name the ingress verifies for an `https` upstream; `stream: true` turns off response buffering (server sent events). Backends bind to the ingress, not the other way round, so routing never forms a binding cycle |
| `acme` | url, ca (certificate) | an ACME directory; sdk modules' TLS |
| `tls-certificate-provisioner` | url, provisioner, password (secret), ca (certificate) | yields `tls-certificate` |
| `tls-certificate` | cert (certificate), key (secret), ca (certificate) | one server certificate for the consumer's host name |

## Versions

- **Version** (on a provide, and on an external provide): `^[0-9]+(\.[0-9]+){0,3}$`, like `18.6` or `1.0`. Compared part by part as numbers; a missing part counts as 0, so `18` equals `18.0`.
- **Range** (on a slot, optional; absent means any version): one or more clauses joined by `,` (spaces around allowed), each an operator `>=`, `>`, `<=`, `<` or `=` followed by a version. A version is inside the range when every clause holds. Example: `>=15, <19`.
- Catalog rules: `invalid_version`, `invalid_range` (a bad clause, or a range no version can satisfy, like `>=19, <15`).
- The platform's own capabilities start at `1.0`; a release that adds an operation bumps the minor, one that breaks callers gets a new capability name.

## Graph v1

Unchanged from 0002 except for the header and these parts:

```yaml
apiVersion: modular-platform/v1
kind: Graph
modules:
  api:
    module: api
    uses:
      main-db:
        provider: postgres
        database: platform_api
        user: api
      planner: planner
  billing-db:
    external:
      provides:
        db:
          capability: postgresql
          version: "16.4"
          fields:
            host: db.example.com
            port: "5432"
            database: billing
            user: billing
            sslmode: verify-full
  postgres:
    module: postgres
name: modular-platform
```

| Field | Type | Meaning |
|---|---|---|
| `modules.<i>.module` or `modules.<i>.external` | exactly one | a catalog module, or a service outside the stack |
| `modules.<i>.uses.<slot>` | string, object, or `null` | string: `instance` or `instance.provide`. Object: `provider` (that same string) plus named values (field name to value, only fields the provisioner's `yields.named` lists). `null` and a missing key keep 0002's meaning |
| `external.provides.<p>.capability`, `.version` | name, version | what the external offers |
| `external.provides.<p>.fields` | map: field to text | non secret field values; a secret or certificate field is never written here, it becomes a file you place at `secrets/<i>/<p>_<field>` |

Canonical text as 0002 (sorted keys, two space indent). A binding is written as a string when it carries no named values, else as an object. Port and other numbers in `external` fields are strings, so both writers agree.

**Conversion from v0alpha1** (in `libs/contract`, used by `DecodeGraph`): `apiVersion` becomes `modular-platform/v1`; every `uses` key and value stay as written (a v0alpha1 capability key becomes the slot of the same name, which every built in v1 entry keeps for this reason). The plan answer carries warning `converted_from_v0alpha1` at path `apiVersion`. Then normal planning applies, so a key with no such slot is `unknown_use`.

## Matching rules

A **provide match** for slot `s` (capability `C`, range `R`) of instance `i` is a pair (instance `p`, provide `q`) with `p` ≠ `i`, where `q`'s capability is `C`, or yields `C`, and `q`'s version is inside `R`. A **candidate** is an instance with at least one provide match. Instances with an invalid name or unknown module are never candidates (0002).

For every instance in name order, then every slot of its entry in name order. `<i>` is the instance, `<s>` the slot, path `modules.<i>.uses.<s>` unless stated:

| Situation | Severity, code |
|---|---|
| a `uses` key the entry does not declare | error `unknown_use` |
| bound to an instance not in the graph | error `unknown_provider` |
| bound to itself | error `self_binding` |
| `instance.provide` names a provide the instance lacks | error `unknown_provide` |
| bound instance has no provide of `C` or yielding `C` | error `capability_not_provided` |
| bound provide has `C` (or yields it) but a version outside `R` | error `version_mismatch`, message names the version and the range |
| bound to an instance with two or more matching provides, written without `.provide` | error `ambiguous_provide`, `candidates` lists `instance.provide` |
| named values on a binding whose provide does not yield `C` (a direct provide) | error `named_not_allowed` |
| a named key the provisioner does not list | error `unknown_named_field` |
| a named value breaking the field's `pattern` | error `invalid_named_value` |
| required slot `null` | error `unbound_required` |
| required slot missing, no candidate, but some instance has `C` outside `R` | error `version_mismatch`, `candidates` lists those |
| required slot missing, no candidate at all | error `unmet_need` |
| required slot missing, exactly one provide match in the whole graph | **auto bind**: written as `p` (or `p.q` when `p` has two or more provides of `C` or yielding it); `auto: true` |
| required slot missing, two or more provide matches | error `ambiguous_provider`, `candidates` lists `p` or `p.q` |
| optional slot missing, one or more matches | warning `optional_unbound` with `candidates` |
| a binding to a provisioner with named values missing | the planner fills them (0014) and marks the binding `named: true` |
| one consumer binding two slots to the same provide | error `duplicate_binding` at the second slot in name order |

Then: `duplicate_named_value` (0014) per provisioner provide; `binding_cycle` as 0002; `external_field_missing` at `modules.<i>.external.provides.<p>.fields.<f>` for a required non secret field with no value, and `invalid_named_value` for an external value breaking its pattern; `format_too_new` and the request level codes as 0011. Direct provides and yielded ones count the same for ambiguity: an instance providing both `postgresql` and `postgresql-provisioner` is two matches (#6 adds preferences).

**Per caller slots**: `per_caller: true` changes nothing in matching in this spec; #21 will let such a slot hold several providers.

## Critical test scenarios

Table tests in `libs/planner` over fixtures in `contracts/fixtures/graphs/` (all verify **AC-3** unless named):
- Sample v1 as written (`whoami` slot `postgresql` bound to `postgres`): `ok`, one binding with `provisioned: true`, named values filled and written back (**AC-7**).
- A required slot with range `>=15` and one provider at `18.6`: auto bind. With the provider at `14.2`: `version_mismatch` listing it.
- An instance with provides `primary` and `replica`, both `postgresql`: bound as `postgres` gives `ambiguous_provide`; as `postgres.replica` binds; as `postgres.nope` gives `unknown_provide`.
- Two slots of one capability on one consumer, bound to two different providers: two bindings (**AC-4**); bound to the same provide: `duplicate_binding`.
- Named values on a direct `postgresql` provide: `named_not_allowed`. A named key `owner`: `unknown_named_field`. `database: Bad-Name`: `invalid_named_value`.
- An external `postgresql` provide with all fields: candidate and auto bind (**AC-15**); without `host`: `external_field_missing`.
- A v0alpha1 graph: answered v1, warning `converted_from_v0alpha1`, digest equal to the same graph sent as v1 (**AC-14**). A graph with `formatVersion: "1.9"`: `format_too_new` (**AC-14**).
- `per_caller` on a non sdk entry: catalog problem `per_caller_not_allowed` (**AC-13**).
- Every 0002 scenario, rewritten in v1, gives the same codes.

## Rationale (short)

A capability file makes the field list a shared contract instead of Go constants, so a new protocol needs no SDK release and the editor can show what a binding carries (your choice). A version on the provide and a range on the slot let a consumer say "Postgres 15 or newer" while keeping the name stable; breaking changes become new names, which keeps matching a simple comparison (your choice, dotted numbers). Named slots and named provides make the graph say exactly which need meets which offer; the short `instance` form keeps the common case readable (your choice). External instances live in the graph because their addresses are site specific and do not belong in a shared catalog. Runners up: fields hard coded in `sdk/manifest` (no new kind, but every capability is a release); a major version in the name (`postgresql/v1`, no ranges); provides keyed by capability (shorter, but an ingress could not offer two `http` sites).
