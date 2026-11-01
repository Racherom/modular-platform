# 0005. Module SDK

**Date**: 2026-10-30
**Status**: In Progress

## Summary

The module SDK is a Go kit for building a module container, so a module only describes what it needs and the SDK does the rest. A module declares its config values and its bindings once, in its catalog entry; a generator turns that into typed Go getters; the SDK fills the values from env vars, files and secrets, opens ready clients for HTTP, PostgreSQL and S3 bindings, checks a token per binding on incoming calls, serves TLS with short lived certificates when an issuer is bound, and signs and checks an internal token that says which user is calling. It comes first because the platform's own services (`planner` now, `api` and `deployer-docker` in #16) are built on it, and later sources (a config server, a policy server for per user values) plug in without changing module code.

## Requirements

**User stories**:
- As a module author, I want to declare my config and bindings in one place, so that my module has no parsing, connection or credential code of its own.
- As a platform contributor, I want `planner`, `api` and `deployer-docker` to start, connect and protect themselves the same way, so that a mistake is fixed once in the SDK.
- As an operator, I want a wrong or missing setting to stop the module at start with a message that names it, so that I fix everything in one pass.
- As a future author of a config server, policy server or second language SDK, I want the conventions written down, so that I can build against them without reading the Go code.

**Acceptance criteria**:
- **AC-1**: The planner's config is declared only in the `config:` section of `catalog/planner.yaml`. `task generate` runs `sdkgen` and writes `services/planner/internal/config/config.gen.go`, and `git diff --exit-code` is clean after it. No package outside `sdk/` calls `os.Getenv`, `os.LookupEnv` or reads a `*_FILE` path for a declared value, and `services/planner/internal/config/config.go` is gone.
- **AC-2**: A declared value is taken from its env var, or from the file its `<ENV>_FILE` names, else from the env files listed in `MP_ENV_FILE` (colon separated, a later file overriding an earlier one), else from its `default`. Each case is covered by a test of the generated loader for every type in the core set (`string`, `int`, `bool`, `duration`, `url`, `enum`, `secret`).
- **AC-3**: With several bad values at once (a missing required value, a number below `min`, a string that misses its `pattern`, an enum value not in `values`, both `MP_X` and `MP_X_FILE` set, a bad env file line), the module logs exactly one line per problem as `config: <value> (<ENV>): <code>: <message>` (env file problems carry `<file>:<line>` in the message), one line per value (its first failing rule), sorted by name, then file, then line, and exits with code 1 before it listens on any port.
- **AC-4**: A `secret` value given as a plain env var stops the start (`secret_in_env`). A secret's value never appears in any log line, in `fmt` output (`%v`, `%s`, `%+v`) or in `slog` output of the generated struct or of the `sdk.Secret` type.
- **AC-5**: A successful start logs one `config loaded` line listing each value's name and source (`env`, `file:<path>`, `env-file:<path>:<line>` or `default`) and no value at all, plus the module's host name, the TLS mode (`tls` or `plain`) and each binding's capability and client kind.
- **AC-6**: A module reaches each bound capability through a generated getter: the HTTP client sends the binding's token as `Authorization: Bearer`; the PostgreSQL pool signs in with the binding's `user`, password file and `database`; the S3 client uses the binding's access key, secret key file, `region` and `bucket`. Each is shown against a real server (testcontainers: the official `postgres` image, MinIO, and an SDK test module for HTTP).
- **AC-7**: A provider whose catalog entry says `provides.<cap>.auth: binding-token` answers 401 with one problem (`unauthorized`) to a call without a token or with a token that is not in its clients directory, compared in constant time; `/healthz` and `/readyz` are exempt. For a call with a valid token, `sdk.CallerModule(ctx)` returns the consumer's instance name (the token file's name). Removing one consumer's file refuses that consumer only (after a restart).
- **AC-8**: With `MP_ACME_URL`, `MP_CA_FILE` and `MP_DOMAIN` set, the module gets its certificate for its host name `<MP_INSTANCE>.<MP_DOMAIN>` from an ACME server over tls-alpn-01 through the SDK's own client on `x/crypto/acme` (tested against `pebble`), serves only TLS on port 8443, and serves that one ECDSA P-256 certificate to every client, so no handshake ever orders a certificate. It renews once two thirds of the certificate's lifetime have passed (8 hours left on a 24 hour leaf). A failed issue or renewal is retried with backoff from 1 to 30 seconds, each attempt cut off after 5 minutes, while a held certificate keeps serving. A plain HTTP request to 8443 fails, and an `http://` binding URL to an SDK provider while in TLS mode stops the start (`downgrade_refused`). Without `MP_ACME_URL` and `MP_CA_FILE` the module serves plain HTTP on 8080 and logs one warning; with only one of them set, or with both but no `MP_DOMAIN`, it refuses to start (`tls_config`).
- **AC-9**: `/readyz` answers 503 until every required binding (a `uses` entry that is not `optional`) has answered once (HTTP `GET <url>/readyz` with any status below 500, PostgreSQL ping, S3 bucket exists; not rechecked after that) and, in TLS mode, until a certificate is held; it answers 503 again when the held certificate has less than 2 hours left. A provider that is down at start does not stop the consumer from starting; it becomes ready once the provider answers.
- **AC-10**: Without changing module code, a test adds (through `sdk.Run` options) a push source that replaces the value snapshot while the module runs, and a caller source that answers one value differently for one caller. A pushed snapshot with any bad value is refused as a whole and the previous one stays. While a push source is set, a declared value found in an env var or env file stops the start (`managed_env`); only the SDK's bootstrap names are read from env. When the caller source cannot answer within 200 ms, the getter returns an error and the test module answers 503.
- **AC-11**: `api` style minting (`caller.Mint`) and checking in another SDK module work end to end: a valid `MP-Caller` token puts `sdk.Caller{Issuer, User, Groups}` into ctx. A token is refused with 401 (`invalid_token`) when its `aud` is not the receiving module's host name, its `iss` is not the `CallerModule` the binding token proved, the request passed no binding token auth, it expired or its `iat` lies in the future (30 seconds leeway each), it lives longer than 5 minutes, its `sub` is empty, its `kid` is unknown, its algorithm is not EdDSA, its signature is wrong, or the header appears more than once. A request without the header has no caller and is not an error.
- **AC-12**: The planner's catalog check accepts the new fields (`runtime`, `config`, `uses.<cap>.client` and its env keys, `provides.<cap>.auth`, `provides.<cap>.env.clients_dir`) and reports every rule in *Catalog rules for the new fields* with its code. Rendering a graph of two SDK test modules (a consumer bound to a provider) gives each side the per binding token, runs both as `user: "${MP_UID}:${MP_GID}"` (which `setup.sh` writes into `.env`, so the 0600 secret files stay readable), gives each instance its `config/<instance>.env` with every required value as an empty `KEY=` line plus `MP_ENV_FILE`, `MP_INSTANCE` and `MP_DOMAIN`, gives each SDK instance the network alias `<instance>.<graph>.internal` and points the consumer's binding URL at that name, and after `setup.sh` and `docker compose up`, the consumer's call to the provider answers 200 while a call without the token answers 401.
- **AC-13**: The planner runs on `sdk.Run`. Everything spec 0001 and its children require of it still holds (AC-2 to AC-5 and AC-8 to AC-11 of 0001, all of 0003), except that internal mode now refuses to start unless `MP_PLANNER_CLIENTS_DIR` holds at least one token (it replaces `MP_PLANNER_TOKEN_FILE`). `libs/platform` is deleted, and `task lint`, `task test` and `task smoke` pass.
- **AC-14**: `sdk/CONTRACT.md` states every convention a second language SDK must follow (the catalog fields it reads, env and file naming, the env file syntax, source precedence, the problem line format, the bootstrap names, the binding token paths, the TLS and ACME rules, the `MP-Caller` token format and checks, and readiness), and each statement in it is backed by a Go test named in it.

## Decision

**Chosen option**: Option 2: one declaration in the catalog entry, generated typed getters, and an SDK that owns sources, clients and protection (see [rationale.md](rationale.md)).

Build a new Go module `github.com/racherom/modular-platform/sdk` that absorbs `libs/platform`, generates each module's config and binding getters from its catalog entry with `sdkgen`, runs the module through `sdk.Run`, and protects calls between modules with per binding tokens, TLS from short lived ACME certificates, and an `api` minted EdDSA token for the calling user.

**Implementation skills**: `golang-project-layout` · `golang-structs-interfaces` · `golang-error-handling` · `golang-security` · `golang-testing` · `golang-context` · `golang-database` · `golang-observability` · `golang-naming` (all `samber/cc-skills-golang`, `.claude/skills/<name>/`) · `golang-dependency-management` (`samber/cc-skills-golang`, `.agents/skills/golang-dependency-management/`) · `openapi-spec-generation` (`wshobson/agents`, `.claude/skills/openapi-spec-generation/`) · `multi-stage-dockerfile` (`.claude/skills/multi-stage-dockerfile/`)

## Rationale

Reasoning and options: see [rationale.md](rationale.md).

## Feature design

### Package layout

```text
sdk/                                 Go module github.com/racherom/modular-platform/sdk (in go.work)
  sdk.go                             Run, Module, Options, Problem, Secret, Caller, CallerModule, WithCaller, CallerFrom
  source.go                          Source, PushSource, CallerSource, BindingResolver interfaces
  CONTRACT.md                        the language neutral contract (AC-14)
  manifest/                          reads the catalog entry fields the SDK uses and checks the config rules (also called by libs/contract)
  envfile/                           the strict env file parser
  internal/load/                     precedence, parsing by type, rules, problem lines, the start log
  httpkit/                           from libs/platform: logger, problems, health, access log, rate limit, client IP, base path, Serve
  auth/                              per consumer bearer check, CallerModule in ctx
  tlsacme/                           the SDK's ACME client on x/crypto/acme, the certificate manager (issue, renewal, readiness), TLS server and client configs
  caller/                            Mint and Verify of the MP-Caller token (go-jose v4)
  httpclient/  pg/  s3/              the three capability clients (net/http, pgx v5 pool, minio-go v7)
  sdktest/                           fake sources, a fake caller source, helpers to build env files and clients dirs
  cmd/sdkgen/                        the generator, run with `go tool sdkgen`
```

`libs` depends on `sdk` (for `manifest` rules), never the other way round. `sdk/manifest` owns the Go types of the new catalog sections and imports only the standard library and `sigs.k8s.io/yaml`; the OpenAPI `CatalogEntry` schema describes the same fields for the editor, and a test in `libs/contract` feeds one set of fixtures (every rule code, accepted and refused) to both and fails when they disagree. Each client lives in its own package so a module only links what it uses. `libs/platform` moves into `sdk/httpkit` unchanged except for imports, and is deleted.

A module's `main`:

```go
//go:generate go tool sdkgen -entry ../../../../catalog/planner.yaml -package config -out config.gen.go
func main() { sdk.Run(config.Module, setup) }

func setup(ctx context.Context, m *sdk.Module[config.Config, config.Uses]) (http.Handler, error) {
	rate, err := m.Config.PlannerRate(ctx)
	...
}
```

`sdk.Run` handles the `healthcheck` subcommand (it reads only `MP_INSTANCE`, `MP_DOMAIN`, `MP_ACME_URL` and `MP_CA_FILE` from env, loads no config, and probes `/readyz`: with both TLS settings at `https://127.0.0.1:8443/readyz` with server name = the host name, trusting `MP_CA_FILE`, else at `http://127.0.0.1:8080/readyz`; a missing or bad bootstrap name or any answer other than 200 exits 1), signals, the JSON logger, loading and checking config (exit 1 on any problem), building clients, the auth and caller middleware, `/healthz` and `/readyz`, TLS or plain serving, and graceful shutdown, using the timeouts `libs/platform.Serve` has today.

### Data model sketch (catalog entry additions, `modular-platform/v0alpha1`)

Confirmed in the design conversation. These fields join 0002's catalog entry, are added to the `CatalogEntry` schema in `contracts/openapi/planner.yaml`, and are decoded strictly like the rest.

```yaml
apiVersion: modular-platform/v0alpha1
kind: Module
name: planner
description: Checks a graph and renders it to a Compose project.
image: ghcr.io/racherom/modular-platform/planner:<exact version>
runtime: sdk
healthcheck:
  test: ["CMD", "/planner", "healthcheck"]
  interval: 5s
  timeout: 3s
  retries: 10
  start_period: 30s
provides:
  planner:
    auth: binding-token
    env:
      clients_dir: MP_PLANNER_CLIENTS_DIR
config:
  planner-mode:
    env: MP_PLANNER_MODE
    type: enum
    values: [public, internal]
    description: Where the planner runs, public behind the online tool or internal inside the platform.
  planner-rate:
    env: MP_PLANNER_RATE
    type: int
    default: "60"
    min: 1
    description: Requests per minute per client address in public mode.
  proxy-hops:
    env: MP_PROXY_HOPS
    type: int
    default: "1"
    min: 0
    description: Proxies in front of the planner that append X-Forwarded-For.
```

| Where | Field | Rule |
|---|---|---|
| top level | `runtime` | `sdk` or absent. A `runtime: sdk` entry gets the bootstrap names and its env file from the render, listens on 8080 (plain) or 8443 (TLS), and must not set `provides.<cap>.port` |
| `config.<value>` | map key | the value name, under 0002's shared naming rules; unique together with the entry's `secrets[*].name` |
| | `env` | required; `^[A-Z][A-Z0-9_]*$`; must not end in `_FILE`; unique in the entry counting every `uses.*.env`, `secrets[*].env` and each value's `<env>_FILE`; not a bootstrap name |
| | `type` | required: `string`, `int` (64 bit), `bool` (exactly `true` or `false`), `duration` (Go syntax like `10s`), `url` (absolute, with scheme and host), `enum`, `secret` |
| | `description` | required, one sentence; becomes the Go doc comment and the env file comment |
| | `default` | optional text, parsed by type and checked by the rules; not allowed on `secret` |
| | `optional` | bool, default `false`. A value with a default is never missing. An optional value with no default gets a getter returning `(T, bool, error)` |
| | `min`, `max` | `int` and `duration` only; `min` ≤ `max` |
| | `pattern` | `string` only; RE2, matched against the whole value |
| | `values` | `enum` only, required, at least one, unique, each a non empty string |
| | `generate` | `secret` only: the render creates it as 32 random bytes in hex, like 0002's `secrets` |
| `uses.<cap>` | `client` | `http`, `postgresql`, `s3`, or absent (env mapping only, as in 0002). `client` is allowed only on a `runtime: sdk` entry |
| | `env` keys | every kind: `host`, `port`, `url`, `token_file`, `ca_file`. `postgresql`: also `database`, `user`, `password_file`, `sslmode`. `s3`: also `bucket`, `region`, `access_key_id`, `secret_access_key_file`. A `client` binding must map `url` and, when the provider says `auth: binding-token`, `token_file`; `postgresql` must also map `database`, `user`, `password_file`; `s3` must also map `bucket`, `access_key_id`, `secret_access_key_file` |
| `provides.<cap>` | `auth` | `binding-token` or absent. A `runtime: sdk` entry provides at most one capability (one port in v0alpha1) |
| | `env.clients_dir` | required with `auth: binding-token`; the env name for the directory holding one token file per consumer |

**Bootstrap names** (fixed, never declared, read from env only, filled by the render or a hand written Compose file):

| Name | Required | Meaning |
|---|---|---|
| `MP_INSTANCE` | yes | the module's instance name (the graph key); the first label of its host name, and the `iss` it mints |
| `MP_DOMAIN` | in TLS mode | the suffix after the instance name in the host name; the render writes `<graph>.internal`. One or more DNS labels (`^[a-z]([a-z0-9-]{0,61}[a-z0-9])?$` each) joined by `.`; anything else is `invalid` (`config: MP_DOMAIN (MP_DOMAIN): invalid: ...`), checked in plain mode too because the host name feeds URLs and `aud` there. A rendered name is at most 136 characters (two labels of 63 under 0002's naming rule plus `.internal`), far below DNS's 253, so no length rule is needed. `MP_INSTANCE` breaking 0002's naming rule is `invalid` the same way |
| `MP_ENV_FILE` | no | colon separated env file paths |
| `MP_ACME_URL` | with `MP_CA_FILE` | the issuer's ACME directory URL |
| `MP_CA_FILE` | with `MP_ACME_URL` | the root CA (PEM) that signs the issuer and every module certificate |
| `MP_CALLER_KEY_FILE` | no | PEM file with one or more Ed25519 public keys allowed to sign `MP-Caller` tokens; unset means the module accepts no caller token (the header is ignored and logged once) |
| `MP_CALLER_SIGNING_KEY_FILE` | no | PEM (PKCS 8) Ed25519 private key; only `api` gets it |

**Host name**: `<MP_INSTANCE>.<MP_DOMAIN>` when `MP_DOMAIN` is set, else `MP_INSTANCE` alone (plain mode only, such as the hand written `deploy/online/compose.yaml`). It is the one name a module goes by: its certificate name, the server name clients verify, its Compose network alias, the host in binding URLs that point at it, and the `aud` it accepts. TLS mode requires `MP_DOMAIN` so the name is unique per stack: once one issuer serves several stacks (#16), a certificate for a bare `planner` would be valid in every stack, and a validation by bare name would be ambiguous on an issuer joined to several stack networks. The ACME client itself accepts single label names. The server presents its one certificate whatever server name a client sends (none, the bare instance or an IP included); a client that verifies a name the certificate does not carry refuses it on its own side, so a TLS client must still call a module by its host name. Like the other bootstrap names, `MP_DOMAIN` is read from the process env only. The hand written `deploy/online/compose.yaml` sets no `MP_DOMAIN` on purpose: the online planner runs plain, and Caddy calls it by service name.

Relationships: an entry has 0..N config values, 0..N `uses` (one binding each, as 0002) and 0..1 `provides` when `runtime: sdk`. Each binding whose provider says `auth: binding-token` has exactly one token, mounted on both sides. Each rendered `runtime: sdk` instance has exactly one `config/<instance>.env`.

**Generated code** (`sdkgen`, one file per module, checked in): a `Config` type with one getter per value, named in Go case from the value name (`planner-rate` → `PlannerRate(ctx) (int, error)`; enum values become typed constants, `secret` returns `sdk.Secret`); a `Uses` type with one getter per `client` binding (`Postgresql(ctx) (*pgxpool.Pool, error)`, `Planner(ctx) (*httpclient.Client, error)`, `Storage(ctx) (*minio.Client, error)` with the bucket on `s3.Client`); and a `Module` value holding the declaration for `sdk.Run`. `sdkgen` refuses an entry that breaks a rule, with the same lines as the catalog check.

Generated getter names are the value or capability name in upper camel case (`object-store` → `ObjectStore`); a clash between two names after that conversion is an `sdkgen` error. The file starts with `// Code generated by sdkgen from <entry path>. DO NOT EDIT.`, is gofmt formatted, lists values and bindings in name order, and imports only the client packages the entry uses. Each module's own `go.mod` carries `tool github.com/racherom/modular-platform/sdk/cmd/sdkgen`, and `task generate` runs `go generate ./...` in each module. Skeleton for the planner:

```go
// Code generated by sdkgen from catalog/planner.yaml. DO NOT EDIT.
package config

type PlannerMode string

const (
	PlannerModePublic   PlannerMode = "public"
	PlannerModeInternal PlannerMode = "internal"
)

// Config holds the planner's declared values; read them only through the getters.
type Config struct{ v *sdk.Values }

// PlannerMode is where the planner runs, public behind the online tool or internal inside the platform.
func (c Config) PlannerMode(ctx context.Context) (PlannerMode, error) { ... }
func (c Config) PlannerRate(ctx context.Context) (int, error)         { ... }
func (c Config) ProxyHops(ctx context.Context) (int, error)           { ... }

// Uses holds the planner's binding clients (none).
type Uses struct{ b *sdk.Bindings }

// Module is the declaration sdk.Run loads.
var Module = sdk.Declaration[Config, Uses]{
	Values:   []sdk.ValueSpec{ ... },   // name, env, type, default, rules, from the entry
	Bindings: []sdk.BindingSpec{ ... }, // capability, client kind, env keys
	Provides: &sdk.ProvideSpec{ ... },  // capability, auth, clients_dir env
	New:      func(v *sdk.Values, b *sdk.Bindings) (Config, Uses) { return Config{v}, Uses{b} },
}
```

`sdk.Run[C, U any](d sdk.Declaration[C, U], setup func(context.Context, *sdk.Module[C, U]) (http.Handler, error), opts ...sdk.Option)`; `sdk.Module[C, U]` exposes `Config C`, `Uses U`, `Log *slog.Logger`, `AllowAnonymous()` and `WithRateLimit(...)`. No reflection: `New` builds the generated types.

### Sources and precedence

1. Bootstrap names: env only.
2. A declared value: its env var `<ENV>`, or the file `<ENV>_FILE` names (trailing whitespace trimmed, like `ReadSecretFile` today). Both set is `ambiguous_source`.
3. Else the env files in `MP_ENV_FILE` order, a later file overriding an earlier one for the same key. `<ENV>_FILE` keys are allowed in env files.
4. Else `default`. Else, if not `optional`, `missing`.

`<ENV>` and `<ENV>_FILE` share one slot per level (the process env is one level, each env file another). The highest level that sets the slot wins whole; both forms set at the same level is `ambiguous_source`. So process env `FOO_FILE` beats an env file `FOO`, and a later env file's `FOO_FILE` beats an earlier file's `FOO`.

An empty value counts as unset everywhere, and so does a file named by `<ENV>_FILE` that is empty after trimming trailing whitespace (`missing` for a required value). A `secret` comes only from files: its `<ENV>_FILE` (in the process env or in an env file) or a line in an env file. A secret on a plain env file line is allowed but logs one warning per value, because the SDK cannot see whether that file is mounted as a secret. A `secret` given as a plain process env var is `secret_in_env`, because process env shows in `docker inspect`. The render never writes a secret into an env file. When a push source is set (#17), steps 2 and 3 apply only to bootstrap names and any declared value found there is `managed_env`.

**Env file syntax**: UTF 8, LF or CRLF; each line is blank, a comment starting with `#` (after optional spaces), or `KEY=value` where `KEY` matches `^[A-Z][A-Z0-9_]*$`. The value runs to the end of the line; if it starts and ends with the same quote (`'` or `"`), the quotes are removed and nothing inside is unescaped. No `export`, no `${VAR}`, no multi line values, no inline comments. A key twice in one file, a bad line, or a key that is neither declared (value, `_FILE` form, or binding env key) nor a bootstrap name is a problem (`env_file`, `unknown_key`) with `<file>:<line>`. A missing listed file is `env_file`.

**Problem codes**: `missing`, `invalid`, `out_of_range`, `no_match`, `not_allowed`, `ambiguous_source`, `secret_in_env`, `env_file`, `unknown_key`, `managed_env`, `invalid_binding`, `downgrade_refused` (in TLS mode, an `http://` URL whose host ends in `.<MP_DOMAIN>`, which only SDK providers carry; found when the client is built), `tls_config` (only one of `MP_ACME_URL` and `MP_CA_FILE` set, both set without `MP_DOMAIN`, or a CA file that is unreadable or holds no certificate), `validate` (an error from the module's optional `Validate(ctx) error` on `Config`, run after all values load and only when there is no other problem). Line format: `config: <value> (<ENV>): <code>: <message>`; for a bootstrap name or a binding key the value part is the name itself (`config: MP_ACME_URL (MP_ACME_URL): tls_config: ...`, `config: uses.postgresql.user (DB_USER): missing: ...`), and `validate` uses `module`. A value reports only its first failing rule, checked in this order: source (`ambiguous_source`, `secret_in_env`, `managed_env`), presence (`missing`), type (`invalid`), then `out_of_range`, `no_match`, `not_allowed`. Lines are sorted by that value part (byte order), then file, then line.

### Pluggable sources (interfaces now, protocols in #17 and #21)

```go
// Source answers at start, by value name (not env name). Text in; the SDK parses and checks it.
type Source interface { Lookup(ctx context.Context, value string) (string, bool, error) }

// PushSource delivers whole snapshots while the module runs (#17's config server).
type PushSource interface {
	Watch(ctx context.Context, values []string, apply func(snapshot map[string]string) error) error
}

// CallerSource answers a value for one caller (#21's policy server).
type CallerSource interface {
	LookupFor(ctx context.Context, caller Caller, value string) (string, bool, error)
}

// BindingResolver picks the provider binding for one caller and capability (#21).
type BindingResolver interface {
	BindingFor(ctx context.Context, caller Caller, capability string) (Binding, bool, error)
}
```

- A getter reads the current snapshot. With a `CallerSource` and a caller in ctx, it asks the source first (200 ms budget, an answer cached per caller and value for 30 seconds). A found answer is parsed and checked like any value; a bad one is an error. Not found means the snapshot value. A timeout or error is returned as an error (`sdk.ErrCallerSource`), and the module answers 503. With no `CallerSource` (all of thread 1) the error is always nil.
- Client getters do the same with a `BindingResolver`, and cache one client per provider binding.
- `apply` checks the whole snapshot with the start rules. Any problem refuses it, logs one line per problem, and keeps the previous snapshot. Readiness is not affected. A snapshot is complete: a value it leaves out falls back to its default, or is `missing`.
- With a push source, the module listens (with `/readyz` at 503) and waits up to 30 seconds for the first snapshot, then exits 1 with one `config` line if none arrived. The `managed_env` rule covers `<ENV>`, `<ENV>_FILE` and env file lines alike.
- The per caller cache holds at most 10,000 entries and drops the least recently used when full (as spec 0003 does for rate limit buckets); a snapshot swap clears it.

### Bindings, clients and protection

- **HTTP client**: built on `net/http` with the module's TLS client config; base URL from `url`; adds `Authorization: Bearer <token_file content>`; adds `MP-Caller` when the module holds a signing key and a caller is in ctx (minted per call for `aud` = the URL's `Hostname()` without the port, lowercased, which is the provider's host name; a bare name or an IP is minted as it is and simply fails the provider's `aud` check). It does not retry requests (callers decide); only readiness polls retry, with backoff from 1 to 30 seconds.
- **PostgreSQL**: a `pgxpool.Pool` from `url` (host and port), `database`, `user`, the password file, and `sslmode`. In TLS mode, or when `sslmode` is `verify-full`, it trusts `ca_file` (or `MP_CA_FILE`); `sslmode` values other than `disable` and `verify-full` are `invalid_binding`.
- **S3**: a `minio.Client` with path style addressing, the endpoint from `url`, static credentials from `access_key_id` and the secret key file, `region` (default `us-east-1`), and a custom transport that trusts `ca_file` for an `https` URL. `s3.Client` carries the bucket name.
- **Per binding token** (provider side): the SDK reads the clients directory at start. Each entry must be a regular file or a symlink to one (Compose mounts can be either); hidden entries (starting with `.`) are skipped; any other entry, a file name that breaks the instance naming rule, an empty file, a token under 32 characters after trimming, or two files with the same token is `invalid_binding`. File name = consumer instance name, content = token. An unset `clients_dir` env or an empty directory with `auth: binding-token` refuses the start, unless the module calls `m.AllowAnonymous()` in setup (the planner does in public mode, with one warning line), in which case `clients_dir` may be unset. Checked with `crypto/subtle` against each token. Changes take effect at the next start.
- **TLS**: the SDK's own certificate manager in `sdk/tlsacme`, on the plain `x/crypto/acme` protocol client (no `autocert`). Three parts:
  - **`tlsacme.Issuer`** (the seam the manager orders through): `Issue(ctx, name) (*tls.Certificate, error)` returns a new certificate for `name` with `Leaf` set; `Challenge(hello) (*tls.Certificate, error)` answers a tls-alpn-01 validation of a pending `Issue`. Tests replace it with a fake that signs with a test CA.
  - **`tlsacme.Client`** (the ACME engine, implements `Issuer`; `tlsacme.NewClient(directory, roots)`): an `acme.Client{DirectoryURL: MP_ACME_URL, Key: <fresh ECDSA P-256 account key per start, memory only>}` whose HTTP client trusts `MP_CA_FILE` (TLS 1.2 minimum toward the issuer), ignores proxy env vars (the issuer is internal) and waits at most 30 seconds for response headers; the attempt's context bounds the whole order. `Issue` holds a mutex for its whole run, so two calls never overlap and there is one pending answer slot. One `Issue`, in this order: register the account if this client has not yet (`Register` with `acme.AcceptTOS`, which is only called when the directory has terms; no external account binding; `acme.ErrAccountAlreadyExists` counts as success, since the client has then cached the account URL); `AuthorizeOrder` for one DNS identifier, the name, with no profile (the issuer picks the lifetime), and fail the attempt unless the order's identifiers are exactly that name; for each of the order's authorization URLs, `GetAuthorization`: `valid` needs nothing, `pending` gets its `tls-alpn-01` challenge (none offered fails the attempt): hold `TLSALPN01ChallengeCert(token, name)` as the pending answer, `Accept`, `WaitAuthorization`, and empty the slot once that returns, whatever the outcome; any other status (`invalid`, `expired`, `deactivated`, `revoked`) fails the attempt, and the next attempt starts a new order; `WaitOrder` (an `*acme.OrderError` fails the attempt); make a fresh ECDSA P-256 key and a CSR with an empty subject, `DNSNames: [name]` only, signed with ECDSA SHA 256; `CreateCertFromOrder(ctx, order, csr, true)` on the order `WaitOrder` returned (not `CreateOrderCert`, which fails against pebble); return the chain with the key and the parsed `Leaf`. When any call inside `Issue` fails with an `*acme.Error` (found with `errors.As`) whose `ProblemType` is `urn:ietf:params:acme:error:accountDoesNotExist` (the issuer lost its state), the client marks itself unregistered, still under the mutex, so the next attempt calls `Register` again with the same key, which replaces the cached account URL. `Challenge(hello)` returns the pending answer when one is held and `hello.ServerName`, lowercased with a trailing dot trimmed, equals its name; anything else is an error.
  - **`tlsacme.Manager`** (`tlsacme.New(issuer, host, roots, log)`): holds the certificate behind an atomic pointer. `Run(ctx)` issues (each attempt under its own `context.WithTimeout` of 5 minutes, passed to every call of the attempt) until a certificate passes the check below, holds it and logs `certificate held` (Info, attributes `host` and `not_after`); it then waits until `NotBefore` plus two thirds of the leaf's lifetime, but at least 30 seconds after the clock time that issue succeeded, and issues again the same way. A failed attempt logs one `certificate issue failed` warning with the error (never a key or token) and is retried with backoff from 1 second, doubling up to 30 seconds, for as long as it takes; the backoff starts at 1 second again after every success, and a held certificate keeps serving meanwhile. When ctx ends, `Run` returns (cutting any attempt in flight), revokes and deactivates nothing, and leaves stopping the server to `sdk.Run`. The check counts as a failed attempt: no leaf; a leaf that fails `x509.Certificate.Verify` with `Roots` = the `MP_CA_FILE` pool, `Intermediates` = the rest of the returned chain, `DNSName` = the host name, `KeyUsages` = server auth and `CurrentTime` = the clock's now plus 1 minute (a CA clock up to a minute ahead is tolerated); a leaf whose public key is not the certificate's private key; a leaf already expired. `MP_CA_FILE` is read once at start, so a new root needs a restart of every SDK module. The server's `tls.Config`: TLS 1.3 minimum, `NextProtos` `h2`, `http/1.1`, `acme-tls/1`, and `GetCertificate` returning `Issuer.Challenge(hello)` when the hello offers exactly `acme-tls/1` and nothing else, otherwise the held certificate for any server name, otherwise (nothing held yet) an error. `Ready()` is true while a certificate is held with at least 2 hours left. The manager reads time through an unexported `clock` (`Now() time.Time`, `After(time.Duration) <-chan time.Time`), the real one by default and replaced through `export_test.go`, for the renewal wait, the backoff wait, the leaf check and the 2 hour rule, so unit tests drive all four without waiting; the 5 minute attempt deadline is a context timeout and runs on the real clock. The TLS 1.3 minimum also applies to validation handshakes, which pebble and step-ca (both Go) speak.

  Start order: config checked (exit 1 on problems), then listen on 8443 with `/readyz` at 503, then `Run` in the background. Only `Run` orders certificates; a handshake never does. A certificate that expires because renewal kept failing is still served; the module keeps running with `/readyz` at 503 and keeps retrying, and clients refuse the expired certificate. Nothing is cached on disk, so a restart enrols again. Clients trust `MP_CA_FILE` and verify the server name. The Compose `start_period` of an SDK module's healthcheck must cover enrolment (the render default is 30 seconds).
- **MP-Caller token**: compact JWS, `alg` `EdDSA`, header `kid` = the RFC 7638 thumbprint of the public key (both sides compute it, so no key id is stored). Claims: `iss` (the minting module's `MP_INSTANCE`, so it compares with `CallerModule`), `sub` (user), `groups` (list of strings), `aud` (one host name), `iat`, `exp` (`iat` + 5 minutes), `jti` (16 random bytes, base64url). Checked with go-jose v4 `jwt.ParseSigned` with only `EdDSA` allowed, then the key by `kid` from `MP_CALLER_KEY_FILE`, then `aud` = the module's own host name, `exp` with 30 seconds leeway, and `exp` − `iat` ≤ 5 minutes. `jti` is not tracked (the audience and lifetime limit replay). The token is accepted only on a request that passed binding token auth, and only when `iss` equals `CallerModule`, so a module cannot pass on a token another module minted. Also refused: an `iat` more than 30 seconds in the future, an empty `sub`, and two `MP-Caller` headers. A refusal answers 401 with one problem `invalid_token`. `sdk.Caller` is `{Issuer string, User string, Groups []string}`. A module calling onward passes no caller in this feature.

### Middleware order and readiness

`sdk.Run` builds the handler chain in this order: access log (from `httpkit`), the rate limit if the module set one with `m.WithRateLimit` (the planner does in public mode, with its client IP rule), binding token auth, `MP-Caller` check, then the module's handler. `/healthz` and `/readyz` sit before auth and the rate limit, as today.

Readiness: each required binding is polled with backoff from 1 to 30 seconds until it answers once; each failed attempt logs one warning line naming the capability and the error (never a credential). An HTTP answer below 500 counts (a provider without `/readyz` answers 404, which is fine). A PostgreSQL sign in failure or a missing S3 bucket keeps polling, with the same warning. The render keeps 0002's `depends_on` rule (`service_healthy` when the provider has a healthcheck); no start deadlock is possible because the planner refuses binding cycles. A `runtime: sdk` entry declares its `healthcheck` like any entry (the planner's: `test: ["CMD", "/planner", "healthcheck"]`, `start_period: 30s`).

### Render additions (`libs/render`, plain mode only in this feature)

For every `runtime: sdk` instance `i`:
- environment `MP_INSTANCE=<i>`, `MP_DOMAIN=<graph>.internal` (the graph `name`; `.internal` is reserved by ICANN for private use) and `MP_ENV_FILE=/etc/modular-platform/module.env`;
- `networks: {default: {aliases: [<i>.<graph>.internal]}}`, so Docker's DNS answers the host name (checked with Docker's embedded DNS on 2026-10-31); the bare service name keeps resolving too;
- `user: "${MP_UID}:${MP_GID}"`: Compose without swarm mounts file secrets with the host file's owner and mode (it ignores `uid`, `gid` and `mode`), so the container runs as the user who ran `setup.sh`, and secret files stay 0600. `setup.sh` writes `MP_UID` and `MP_GID` (from `id -u` and `id -g`) into `.env` once, never overwriting them;
- a top level Compose `configs: {<i>_env: {file: ./config/<i>.env}}`, mounted at `/etc/modular-platform/module.env`;
- `config/<i>.env` (mode 0644), written exactly like this (blank line between blocks, LF, one trailing newline), values in name order, then bindings in capability order, then the binding keys in the order listed in the data model:

  ```text
  # Settings for <i> (module <module>). Edit and restart the module to apply.
  # Lines starting with # are defaults or optional values; remove the # to set one.

  # <description>
  # <ENV>=<default>

  # <description>
  # required
  <ENV>=

  # <description>
  # optional
  # <ENV>=

  # Binding uses.<cap> to <provider>: required, from your <provider> setup
  <KEY_ENV>=
  ```

  A `description` must be one line without `#` at its start (catalog rule `missing_field` covers empty; a newline is `invalid_rule`). Secrets never appear in it. The render writes it into every zip; `setup.sh` never overwrites a `config/<i>.env` that already exists on disk.
- per `generate: true` secret value `v`: exactly 0002's secret rule with name `v` (`secrets/<i>/<v>`, Compose secret `<i>_<v>`), and environment `<ENV>_FILE=/run/secrets/<i>_<v>`.

For every binding consumer `c`, capability `C`, provider `p`, besides 0002's `depends_on`, `host` and `port`:
- `url`: `http://<p>.<graph>.internal:8080` when `p` is `runtime: sdk` (its host name, the same in plain and TLS mode; #16 switches the scheme and port to `https` and 8443), else `http://<p>:<provides.C.port>` for `http` and `s3`, `postgres://<p>:<port>` for `postgresql`;
- when `p` says `auth: binding-token`: one Compose secret `<c>_uses_<C>` from `./secrets/<c>/uses_<C>` (created by `setup.sh` like any secret, 32 random bytes in hex; the deployer does the same at apply in #16), mounted on `c` at `/run/secrets/uses/<C>/token` (that path goes into `c`'s `token_file` env) and on `p` at `/run/secrets/clients/<C>/<c>` (`p`'s `clients_dir` env is `/run/secrets/clients/<C>`). The name has two `_`, so it can never equal a 0002 secret name `<i>_<s>` (one `_`);
- `password_file`, `secret_access_key_file`: a Compose secret `<c>_uses_<C>_password` or `<c>_uses_<C>_secret_key` from `./secrets/<c>/uses_<C>_password` or `..._secret_key`, mounted at `/run/secrets/uses/<C>/password` or `/secret_key`, whose path goes into the mapped env. `setup.sh` does not create these; the README lists them as files you place, and `docker compose up` fails on a missing one, as 0001 AC-4 does today;
- `sslmode`: `disable` (no unchanged provider has TLS before #3); `ca_file`: not written in plain mode.

Planner rule added: a binding to a provider with `auth: binding-token` whose consumer entry does not map `token_file` is error `missing_token_mapping` at `modules.<c>.uses.<C>`.

### Catalog rules for the new fields

Reported like 0002 (`catalog/<file>: <dotted.path>: <code>: <message>`, exit 1): `invalid_runtime`, `invalid_env_name` (pattern, `_FILE` suffix, bootstrap name), `duplicate_name` (value and secret names), `duplicate_env`, `invalid_type`, `invalid_default`, `rule_not_allowed` (a rule on a type that does not take it, `generate` on a non secret, `default` on a secret), `invalid_rule` (`min` > `max`, a bad `pattern`, empty or duplicate `values`), `missing_field` (`description`, `values`, `clients_dir`, a required binding key), `client_not_allowed` (`client` on a non `runtime: sdk` entry), `unknown_binding_key`, `port_not_allowed` (`port` on a `runtime: sdk` provide), `too_many_provides`.

**API surface** (the Go surface; no new HTTP endpoints):

| Call | Inputs | Outputs | Auth | Key errors |
|---|---|---|---|---|
| `sdk.Run(module, setup, opts...)` | generated `Module`; `setup(ctx, *Module[C,U]) (http.Handler, error)`; options `WithSource`, `WithPushSource`, `WithCallerSource`, `WithBindingResolver` | exit code via `os.Exit` | none | exit 1 on config problems, setup error, serve error |
| `cfg.<Value>(ctx)` | ctx (may hold a caller) | `T, error` (`T, bool, error` when optional without default) | none | `sdk.ErrCallerSource`; a bad per caller value |
| `uses.<Cap>(ctx)` | ctx | the client, error | uses the binding's credentials | `sdk.ErrCallerSource`, `invalid_binding` |
| `m.AllowAnonymous()` | none | none | turns off the per binding check for this run | none |
| `sdk.CallerModule(ctx)` | ctx of an incoming request | consumer instance, ok | set by the auth middleware | none |
| `sdk.CallerFrom(ctx)`, `sdk.WithCaller(ctx, c)` | ctx, `Caller` | `Caller`, ok / ctx | set by the caller middleware or by `api`'s sign in code | none |
| `caller.Mint(key, Caller, aud, now)` | signing key, caller, audience | compact token | none | key not Ed25519 |
| `caller.Verify(keys, token, self, now)` | public keys, token, own host name | `Caller` | none | `invalid_token` (each check in AC-11) |
| `go tool sdkgen -entry -package -out` | catalog entry path | `config.gen.go` | none | catalog rule lines, exit 1 |

**Value sourcing**:

| Action | Value produced / displayed | Source |
|---|---|---|
| Start load | each declared value | env `<ENV>` · file named by `<ENV>_FILE` · `MP_ENV_FILE` files · catalog `config.<v>.default` (precedence above) |
| Start problem line | value name, env name, code, message, `<file>:<line>` | catalog `config.<v>` key and `env`; the code table above; the env file reader's line count |
| Start log | source label per value; TLS mode; binding kinds | the source that supplied it (`env`, `file:<path>`, `env-file:<path>:<line>`, `default`, `push`); `MP_ACME_URL` and `MP_CA_FILE` present or not; the host name; catalog `uses.<cap>.client`. One `slog` line at Info, `msg` `config loaded`, attributes `host` (the host name), `tls` (`tls` or `plain`), group `values` (value name → source label) and group `bindings` (capability → client kind). Warnings: `serving plain HTTP: no certificate issuer bound` and `binding auth off: anonymous calls allowed`, each once at start |
| Render | `MP_INSTANCE` | the graph's instance key |
| Render | `MP_DOMAIN`, network alias | `<graph>.internal` from the graph `name`; the alias is `<i>.<graph>.internal` |
| Start | host name | `<MP_INSTANCE>.<MP_DOMAIN>`, or `MP_INSTANCE` when `MP_DOMAIN` is unset |
| Render / setup | `MP_UID`, `MP_GID` for `user:` | `setup.sh`, from `id -u` and `id -g`, written once into `.env` |
| Render | env file lines | catalog `config` (env, default, description, optional) and the binding credential keys with no render source |
| Render | binding `url` | the provider's host name `<p>.<graph>.internal` plus 8080 (`runtime: sdk`), or the provider instance name plus `provides.C.port`; scheme by client kind (plain only in this feature) |
| Render | `token_file`, `clients_dir` paths | fixed paths above |
| Render | `sslmode` | `disable`, decided here (unchanged providers have no TLS before #3) |
| Setup / apply | binding token value | `setup.sh` (static) or the deployer at apply (#16): 32 random bytes in hex, never overwritten |
| Database, user, bucket, region, access key id | the credential value | you, in `config/<consumer>.env` (required lines); provisioning fills them from #3 |
| Password, secret key | the file content | a file you place at `secrets/<consumer>/uses_<C>_password` or `_secret_key`; provisioning from #3 |
| HTTP client | `Authorization: Bearer` | content of `token_file` |
| Provider auth | caller module instance | the name of the matching file in `clients_dir` |
| Receiving a call | `Caller.User`, `Caller.Groups`, `Caller.Issuer` | `MP-Caller` claims `sub`, `groups`, `iss`, checked with `MP_CALLER_KEY_FILE` |
| Minting | `sub`, `groups` | the `Caller` `api` put into ctx after OIDC sign in (#16) |
| Minting | `aud` | host of the binding's `url` (the provider's host name) |
| Minting | `iss` | `MP_INSTANCE` |
| Minting / checking | `kid` | RFC 7638 thumbprint of the Ed25519 public key |
| Checking / TLS | own audience, certificate name, order identifier, CSR name, the server name a validation must carry | the host name |
| TLS | certificate key | a fresh ECDSA P-256 key per order, made by `tlsacme.Client` |
| TLS | ACME account key | a fresh ECDSA P-256 key per start, memory only, registered once per process |
| TLS | renewal time | the held leaf: `NotBefore` plus two thirds of `NotAfter` − `NotBefore`, at least 30 seconds after that issue |
| TLS | attempt deadline, retry backoff | fixed in `sdk/tlsacme`: 5 minutes per attempt; 1 second doubling to 30 seconds |
| TLS | ACME directory, trust | `MP_ACME_URL`, `MP_CA_FILE` |
| Readiness | each required binding answered once; certificate time left | HTTP `GET <url>/readyz`, pgx `Ping`, minio `BucketExists`; the held certificate's `NotAfter` |
| Per caller value | override for one caller | `CallerSource` (#21); none in this feature outside tests |

**Key invariants**:
- A declared value is read by module code only through its generated getter, and only the SDK reads env vars and files for it.
- A secret's value never reaches a log, `fmt` or `slog` output; only `Secret.Reveal()` returns it.
- A module never serves with a bad value: start stops on any problem, and a bad pushed snapshot is refused whole.
- In TLS mode a module never serves or calls another SDK module in plain HTTP.
- A token is accepted only by the provider its binding names, and an `MP-Caller` token only by its `aud`.
- A module's certificate name, verified server name, network alias, URL host and accepted `aud` are one string: its host name.
- Only `tlsacme.Manager.Run` orders certificates, one order at a time; a TLS handshake never does. A module holds at most one certificate, and holds it only after it verified against `MP_CA_FILE` for the host name.
- `libs` may import `sdk`; `sdk` never imports `libs`.

**Security model**:
- **Between modules**: the per binding bearer token proves which module calls (0001 principle 8); TLS from the internal CA encrypts SDK to SDK traffic; network isolation stays a second layer.
- **Calling user**: only `api` holds the signing key and mints `MP-Caller`; every other module only checks. The SDK never checks end user sign in (OIDC stays in `api`).
- **Health**: `/healthz` and `/readyz` are open (no token, no caller), as today.
- **Secrets**: from files (a plain env file line warns); mode 0600 from `setup.sh`, readable because SDK containers run as the user who ran `setup.sh`; never in the rendered env files, logs or the zip.
- **Caller token binding**: `MP-Caller` counts only on a call that passed binding token auth and whose `iss` is that calling module, so a module cannot replay a token minted by `api` for someone else's call path.
- **Known gap in thread 1**: `api` to `postgres` (an unchanged container) stays plain inside the internal network until #3 gives providers certificates. The certificate authority key sits in `deployer-docker` (#16), the service with the Docker socket.
- No regulated data is in scope.

**Configuration required**:
- `MP_INSTANCE`, `MP_DOMAIN`, `MP_ENV_FILE`, `MP_ACME_URL`, `MP_CA_FILE`, `MP_CALLER_KEY_FILE`, `MP_CALLER_SIGNING_KEY_FILE`: the bootstrap names above.
- `MP_PLANNER_CLIENTS_DIR`: replaces `MP_PLANNER_TOKEN_FILE` for the planner in internal mode.
- `deploy/online/compose.yaml` adds `MP_INSTANCE=planner` to the planner service.

**Critical test scenarios**:
- Happy path: the planner starts from `catalog/planner.yaml` with values from env, an env file and defaults, logs one `config loaded` line, and the online tool smoke test passes, verifies **AC-1**, **AC-2**, **AC-5**, **AC-13**
- Many problems: six bad values give six lines in name order and exit 1 before listening, verifies **AC-3**
- Secret leak: a secret set as a plain env var stops the start; a secret value is absent from every log line, `%+v` and `slog` output, verifies **AC-4**
- Clients: HTTP, PostgreSQL and MinIO clients reach real servers with binding credentials, verifies **AC-6**
- Auth: no token, a wrong token, another binding's token each get 401; a valid token sets `CallerModule`; health is open, verifies **AC-7**
- TLS against pebble: enrol `<instance>.<domain>` through `tlsacme.Client`, serve 8443, refuse plain and a downgraded `http://` URL; a TLS 1.3 only client and a client offering TLS 1.2 and 1.3 get the same P-256 certificate and pebble sees one order; one of the two TLS settings alone, or both without `MP_DOMAIN`, refuses the start; with a 90 second pebble profile a second order arrives and the server presents the new certificate 25 to 35 seconds before the first expires; the healthcheck subcommand passes over TLS and fails without `MP_CA_FILE`, verifies **AC-8**
- TLS manager and engine without Docker (fake issuer, fake clock): renewal at two thirds of the lifetime and never sooner than 30 seconds; retries at 1, 2, 4, 8, 16, 30, 30 seconds; an attempt cut at 5 minutes; a result with another name, a foreign CA, a key mismatch or no time left refused as a failed attempt while the held certificate keeps serving; an `acme-tls/1` only hello for the host name gets the pending answer, one for another name or with a second protocol gets none; against a small `httptest` ACME stub (directory, nonce, account, and an order answering `accountDoesNotExist` once), the next attempt registers the account again and no attempt registers twice otherwise; a 200 on registration (`ErrAccountAlreadyExists`) still issues; an `invalid` authorization or an order for another identifier fails the attempt; two concurrent `Issue` calls run one after the other; a leaf whose `NotBefore` is 30 seconds ahead of the clock is held and one 2 minutes ahead is refused, verifies **AC-8**, **AC-9**
- Readiness: a provider down at start keeps `/readyz` 503 until it answers; a certificate with under 2 hours left turns it 503 (fake clock), verifies **AC-9**
- Plug in: a fake push source swaps values, a bad snapshot is refused whole, `managed_env` fires, a slow caller source gives 503, verifies **AC-10**
- Caller token: each refusal case in AC-11 gets 401; a valid token puts the caller in ctx, verifies **AC-11**
- End to end render: two SDK test modules rendered, `setup.sh`, `docker compose up`, the consumer's call 200 and an untokened call 401, verifies **AC-12**
- Contract: every statement in `sdk/CONTRACT.md` names a passing test, verifies **AC-14**

## Build plan

Tracer Bullet: the first three tasks put the planner on a thin SDK end to end (catalog entry, generator, loader, `sdk.Run`), and each later task thickens one strand of that running thread.

Tests that need Docker (testcontainers) carry the build tag `integration`, and the rendered Compose test carries `e2e`. `task test` stays Docker free; a new `task test:integration` runs both tags, and CI runs it. Library and image versions (pgx, minio-go, go-jose, `x/crypto`, testcontainers and its modules, `pebble`, MinIO) are pinned to their latest stable release at build time, never `latest`.

1. [x] Create the `sdk` module in `go.work`, move `libs/platform` into `sdk/httpkit` unchanged, point the planner at it, delete `libs/platform`, and add `sdk` to `task lint`, `task test`, the Dockerfiles and the release config, satisfies **AC-13**
2. [x] Add the new fields to the `CatalogEntry` schema in `contracts/openapi/planner.yaml` (regenerate Go and TypeScript), write `sdk/manifest` with the config rules and call it from the catalog check in `libs/contract`, with a test per rule code, satisfies **AC-12**, **AC-1**
3. [x] Write `sdk/envfile`, `sdk/internal/load` (precedence, types, rules, problem lines, the start log, `sdk.Secret`), `cmd/sdkgen` with its `tool` directive and `task generate` step, and `sdk.Run`; add `catalog/planner.yaml`; move the planner onto generated getters and `sdk.Run`; delete `services/planner/internal/config/config.go`; add `MP_INSTANCE` to `deploy/online/compose.yaml`; update tests that list the built in catalog, satisfies **AC-1**, **AC-2**, **AC-3**, **AC-4**, **AC-5**, **AC-13**
4. [x] Add `sdk/auth` (clients directory, `CallerModule`, `AllowAnonymous`), `sdk/httpclient` with the binding token, and readiness polling; switch the planner's internal mode to `MP_PLANNER_CLIENTS_DIR`, satisfies **AC-6**, **AC-7**, **AC-9**, **AC-13**
5. [x] Extend the planner rules (`missing_token_mapping`) and `libs/render` with the render additions (env file per instance, `configs`, bootstrap env, binding token secrets, credential file secrets, `generate` secrets, README lines), update golden files, and add an `e2e` tagged test in `libs` that renders two SDK test modules (one small test image, two catalog entries under its `testdata/`), runs `setup.sh` and `docker compose up` through testcontainers' compose module, and checks 200 and 401, satisfies **AC-12**
6. [x] Give SDK modules their host name, then TLS, satisfies **AC-8**, **AC-9**, **AC-12**:
   - Read `MP_DOMAIN` in `sdk/internal/load` (its syntax check in both modes, `MP_INSTANCE`'s naming check, the `tls_config` rule for TLS without it), derive the host name and log it as `host`; extend `libs/render` with `MP_DOMAIN`, the network alias on `default` (the only network the static render writes) and host name URLs for SDK providers; update the `sdk-pair` golden files, keep `TestSDKPairEndToEnd` green on the dotted URL, and refresh the stale lines in `verify.md`.
   - Add `sdk/tlsacme` (autocert on the issuer for the host name, the client hello rewrite, TLS server on 8443, client trust, the TLS setting rules, the hourly `NotAfter` read for readiness), the TLS branch of the `healthcheck` subcommand, and the downgrade refusal (`.<MP_DOMAIN>` hosts) in `httpclient`. The 2 hour readiness rule reads time through a clock the tests replace; renewal itself is autocert's and is not faked (its timer uses the real clock).
   - Add `integration` tests against `pebble` v2.10.1 (pinned by digest) with `tlsPort` 8443, pebble and the module containers on one Docker network so pebble resolves the module's dotted alias through Docker's DNS: with a 24 hour profile, enrolment, a TLS 1.2 and a TLS 1.3 only client, one order seen by pebble; with a 90 second profile, one renewal (about a minute of test time).
7. [x] Add `sdk/pg` and `sdk/s3` with their binding keys, readiness checks and `sdkgen` getters, tested against the official `postgres` image and MinIO through testcontainers, satisfies **AC-6**, **AC-9**
8. [x] Replace autocert with the SDK's own ACME client (decided 2026-11-01; task 6 first built TLS on autocert), satisfies **AC-8**, **AC-9**:
   - In `sdk/tlsacme`, remove `autocert`, the `Getter` interface, `ACME()`, the client hello rewrite and the hourly `NotAfter` read; add the `Issuer` interface, `tlsacme.Client` and the `Manager` exactly as *TLS* describes (order flow, the `Issue` mutex and its one pending challenge slot, authorization states, account registration again after `accountDoesNotExist`, renewal at two thirds with the 30 second floor, backoff, the 5 minute attempt deadline, the issued certificate check, a replaceable clock). Run `go mod tidy` in `sdk`; `autocert` is no longer imported.
   - Wire `sdk.Run` to `tlsacme.NewClient(MP_ACME_URL, roots)` and `tlsacme.New(issuer, host, roots, log)`; the test option in `export_test.go` takes an `Issuer` instead of a getter.
   - Replace the hello rewrite tests in `sdk/tlsacme` with the manager and engine tests in *Critical test scenarios*; keep `TestTLSMode`, `TestTLSUnreadyWithoutCertificate` and `TestTLSBindingTrust` green on a fake issuer; set `TestTLSAgainstPebble`'s renewal window to 25 to 35 seconds before expiry.
   - Reword the `MP_DOMAIN` comment in `sdk/internal/load` (TLS needs the domain for unique names per stack, not because of autocert), and refresh the `tlsacme` and pebble lines in `verify.md` and any autocert mention in `sdk/AGENTS.md`.
9. [x] Add `sdk/caller` (`Mint`, `Verify`, the `MP-Caller` middleware, the HTTP client hook; `aud` is a host name, `iss` the instance), with a test per refusal case, satisfies **AC-11**
10. Add the `Source`, `PushSource`, `CallerSource` and `BindingResolver` options to `sdk.Run`, the snapshot swap and refusal, the `managed_env` rule, the 200 ms budget and 30 second cache, and fakes in `sdktest`, satisfies **AC-10**
11. Write `sdk/CONTRACT.md` and `sdk/README.md`, linking each contract statement to its test, satisfies **AC-14**

## Consequences

**Positive**:
- Planner, `api` and `deployer-docker` share one start, config, client and protection path; a fix lands once.
- The catalog entry is the single description of a module for the planner, the render and the module's own code, so they cannot drift, and #3 adopts a section that already runs.
- Bad config never reaches traffic: every problem shows at start, in one pass, naming the value.
- Later sources (#17 config server, #21 policy server and per user providers) plug in behind getters modules already call.
- `sdk/CONTRACT.md` lets a second language SDK follow without reverse engineering Go.

**Negative / tradeoffs**:
- This feature is large for slice 1: generator, loader, three clients, ACME TLS, token minting, render changes and the planner migration all land before #16 starts.
- Every getter takes ctx and returns an error, even when no per caller source exists; module code is a little noisier, and a policy outage later stops per user calls (503) instead of serving base values.
- Short lived certificates make the issuer (in `deployer-docker`, #16) a dependency of every SDK module's start in managed mode; while it is down, a restarting module cannot serve, and the CA key sits in the service with the Docker socket.
- tls-alpn-01 on 8443 differs from the RFC 8737 default of 443, so the issuer must validate on a configurable port (to check in #16).
- The env interim means thread 1 modules still read env and files in managed mode until #17.
- Only `api` mints caller tokens, so user apps behind the ingress carry no caller until a later feature adds a path for them.
- `pgx`, `minio-go`, `go-jose` and the ACME client join the `sdk` module's dependencies; testcontainers adds Docker to `go test` for the tagged integration tests.
- PostgreSQL and S3 credentials are typed in by hand until #3's provisioning.
- SDK containers run as your host user (`user: "${MP_UID}:${MP_GID}"`), not a fixed image user; a shared host where several people run `setup.sh` gets one user per stack.
- With a certificate issuer down for more than 16 hours, every SDK module turns unready at about the same time (they keep serving; Docker restarts only on exit).
- Dotted host names mean a module's name is no longer just its instance: one more bootstrap name (`MP_DOMAIN`), a network alias per SDK service, and longer URLs.
- The SDK owns its ACME order, challenge and renewal code (about 250 lines on `x/crypto/acme`, the protocol client the Go team maintains) and the tests for it; a change in how pebble or step-ca speak ACME is ours to follow, where autocert would have absorbed some of it.
- A failing issuer produces one warning per retry, up to two a minute per SDK module, for as long as the outage lasts.

**Neutral**:
- `libs/platform` disappears; its code lives on in `sdk/httpkit`.
- The planner joins the built in catalog, so the editor lists it like any module (0001 principle 2).
- 0002's catalog entry and render grow; 0002 carries a pointer to this spec.
- The planner's internal mode setting changes name (`MP_PLANNER_CLIENTS_DIR`).
- The ACME engine sits behind `tlsacme.Issuer`; replacing it (lego, a future x/crypto manager) touches only `sdk/tlsacme`.
- pebble v2.10.1 and step-ca 0.30.2 both issue single label names (checked 2026-10-31), so dotted names are a policy for uniqueness across stacks, not a client limit; dropping them later changes only the render and `sdk/internal/load`.

## Follow-up

- [x] Update spec 0001 on confirmation (done 2026-10-30; the release config line is left to task 1): AC-5 and the "Planner modes" row move from `MP_PLANNER_TOKEN_FILE` to `MP_PLANNER_CLIENTS_DIR`; the repository layout swaps `libs/platform` for `sdk/`; the release builds the `sdk` module with the shared version.
- [x] Update spec 0002 on confirmation with a one paragraph pointer (done 2026-10-30): the new catalog fields, the planner rule `missing_token_mapping` and the render additions are defined in 0005.
- [ ] Check at build time (task 8): that pebble v2.10.1 and step-ca 0.30.2 answer a lost account with the problem type `urn:ietf:params:acme:error:accountDoesNotExist` (RFC 8555 names it; neither was probed for it), and how much pebble backdates `NotBefore` (it moves the renewal window in `TestTLSAgainstPebble`).
- [ ] Spec #16 must: embed step-ca's Go packages in `deployer-docker` as the `cert-issuer` (ACME, tls-alpn-01 only, names limited to `<instance>.<graph>.internal` for the graph's SDK instances, 24 hour leaves, validation on port 8443 by setting `acme.InsecurePortTLSALPN01 = 8443`; the `step-ca` binary needs `--acme-tls-port 8443 --insecure` for the same; keep `--acme-strict-fqdn` off), create the CA in `setup.sh`, render managed TLS wiring (`MP_ACME_URL`, `MP_CA_FILE`, `https://<p>.<graph>.internal:8443` URLs, `ca_file`), keep each SDK service's alias on every network its peers use, refuse a second stack with a graph name already deployed (or key the issuer's name policy per stack, since the graph name alone then no longer keeps names apart), create the Ed25519 caller key pair (private key to `api` only), let the deployer create binding tokens at apply, and let 0004's refused graph guard accept render generated `configs` mounts.
- [ ] Spec #17 designs the config server's push protocol behind `PushSource` and turns on the `managed_env` rule for managed SDK modules.
- [ ] Spec #21 designs the policy server protocol behind `CallerSource` and `BindingResolver`, and how a caller passes on to onward calls.
- [ ] Spec #3 adopts `runtime`, `config`, `uses.<cap>.client` and `provides.<cap>.auth` into `v1`, adds provisioning that fills the credential keys, and gives unchanged providers certificates so `sslmode` can move to `verify-full`.
- [ ] `catalog/planner.yaml` pins the planner image to an exact version; release-please must bump it with each release (extra file).
- [ ] Check at build time (unverified in the research): minio-go custom CA through `Options.Transport`; go-jose key selection by `kid`; pgx root CA settings; the testcontainers postgres and minio module versions; ~~a pinned `pebble` release tag~~ (v2.10.1, `ghcr.io/letsencrypt/pebble@sha256:ddf230642b1a584f519f32e347de1b05a6e4c1f6c35c1863b33effeab5f78199`, checked 2026-10-31); whether smallstep states its Go packages as a stable API (#16).
- [ ] No root `AGENTS.md` exists; `/audit` or `/sync` should record the installed skill `golang-dependency-management` (project wide Go module work), the `sdk/` conventions (getters only, no env reads outside `sdk`), and the declined skill search results (no credible skill for go-jose, ACME, step-ca, pebble, minio-go).
