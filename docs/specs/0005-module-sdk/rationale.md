# 0005. Module SDK: rationale

Decision record for [index.md](index.md). `/develop` builds from `index.md`; this file holds the why.

## Context

> ⚠️ Premise note: this feature has grown from "config, connections and protection" into seven strands: a generator, a config loader with three source kinds, three capability clients, ACME based TLS, an internal caller token, catalog and render changes, and the planner's migration. All of it lands before #16, which is the first slice anyone can use. That delays the first usable release, and some strands (S3, the caller token, the plug in interfaces) have no real user in thread 1. The build plan keeps it honest by putting the planner on a thin SDK first (tasks 1 to 3) and thickening one strand at a time, so each task ships on a running thread. If slice 1 runs late, tasks 7 (S3 half), 8 and 9 are the ones to move to the feature that first needs them; the interfaces they fix are already written here.

Spec 0001 fixes how the platform's own services behave: every binding carries its own credential (principle 8), a service learns everything about a provider only through its bindings and settings (principle 9), and secrets reach a service only as files. Today that lives in `libs/platform` (logger, health, `EnvInt`, `ReadSecretFile`, one shared `BearerAuth` token, `Serve`) and in each service's hand written config code, like the planner's `config.FromEnv`. Scope #27 asks for a kit that makes this the default for every module: the platform's `api`, `planner` and `deployer-docker` first (#16, slice 1, spec 0004), then any aware module a third party writes. The module contract (#3) does not exist yet, so the kit must work on the provisional `v0alpha1` catalog entry of spec 0002, whose binding wiring today is only `host` and `port` env vars plus `*_FILE` secrets.

The forces: the project is GA tier (it handles sign in, secrets and a deployer that is root on the host), built by a small team with a Tracer Bullet approach, and open source with a public module contract, so whatever the kit reads from the environment becomes part of that contract. The scope's done line asks for one declaration, no parsing code in modules, a clear start error per bad value, binding credentials used and checked, and later sources (a config server #17, a vault #9, per request values #21) that plug in without changing modules. In the design conversation you added more: in managed mode, aware modules get their values pushed from a config server rather than env vars or files; a calling user travels between modules as an internal token; per user overrides come from a policy server; and for capabilities the platform defines, the kit hands the module a ready client already pointed at the right provider with the right credentials. Traffic between modules must be encrypted with short lived certificates.

The tensions: the config server and policy server do not exist and will not until slices 12 and 16, but the platform's modules need the kit in slice 1. A declaration that #3 must adopt has to be written in the contract's own terms (YAML in the catalog entry), not only in Go. TLS needs an issuer that thread 1 must run. And the static path (a downloaded zip, standard tools only) has no issuer at all.

Left undecided, #16 builds three services that each invent their own config parsing, credential handling and TLS, and #3 then has to reconcile three dialects.

## Options considered

### Option 1: Grow `libs/platform` into a richer toolkit

Keep the package and add helpers (`EnvString`, `EnvDuration`, `EnvEnum`, a per consumer token check, an HTTP client with a bearer token). Each module's `main` calls them in its own order and keeps its own `Config` struct.

**Pros**:
- Least new code and no generator; a small step from what runs today.
- Fully flexible: each module wires only what it needs.

**Cons**:
- Every module still writes parsing and wiring code, which the done line rules out, and start errors differ by module.
- Nothing describes a module's config outside its Go code, so the render, the editor and #3 cannot see it.
- Per request and pushed values would change every module later.
- The import path and its dependency tree (compose-go, the planner) stay wrong for third party authors.

### Option 2: Declaration in the catalog entry, generated getters, an SDK that owns sources, clients and protection (chosen)

A new `sdk` Go module. The catalog entry declares config values, binding clients and provider auth; `sdkgen` turns it into typed getters that take ctx; `sdk.Run` loads and checks values from env, `<ENV>_FILE` and env files, builds clients from bindings, checks per binding tokens and `MP-Caller` tokens, and serves TLS with ACME certificates when an issuer is bound. Interfaces for push, per caller and per caller binding sources are fixed now and filled by #17 and #21.

**Pros**:
- One file describes a module to the planner, the render and its own code; #3 adopts a section that already runs.
- Modules contain no parsing, credential or TLS code, and every start error has the same shape.
- Getters through ctx let pushed and per caller values arrive later without touching module code.
- A small, separate module for third party authors.

**Cons**:
- A generator and a checked in generated file per module, with a drift check to keep.
- A large first feature that touches the catalog format, the render and the planner at once.
- Every read returns an error, even before any per caller source exists.

### Option 3: Go struct with field tags, the declaration exported from code

Like Option 2, but the module declares config as a Go struct with tags (`mp:"rate,default=60,min=1"`), filled by reflection, and a `describe` subcommand prints the declaration so a catalog entry can be checked against it.

**Pros**:
- No generator; types come straight from Go and the declaration sits next to the code that reads it.
- Plain field reads, no ctx or error on every read.

**Cons**:
- The source of truth is Go code, so #3 would adopt an export, and a second language SDK would have to copy a tag mini language.
- Plain fields cannot become pushed or per caller values without changing every read.
- Reflection errors (a bad tag) show up at start instead of at generate time.

### Option 4: A language neutral sidecar

A helper container next to each module loads config, holds certificates and tokens, and proxies calls; the module reads plain files and talks plain HTTP to localhost.

**Pros**:
- Works for any language from day one.
- Module code stays entirely free of platform concerns.

**Cons**:
- One more container per module and a proxy on every call; in thread 1 that is eight more containers.
- Protection moves out of the process, so a module can bypass it by calling a peer directly.
- Per caller clients (a PostgreSQL pool for this user's provider) cannot be expressed as a proxy without reimplementing each protocol.

## Rationale

The done line asks for "no parsing code of its own" and for later sources that plug in "without changing the modules". Option 1 fails the first, and Options 1 and 3 fail the second as soon as a value becomes pushed or per caller, because a plain field read cannot ask a source at request time. Option 4 meets both but costs a container per module on a small team's first slice and cannot hand out per caller database or S3 clients, which you named as the point of capability clients. Option 2 is the only one that meets every line of the done line and your additions while keeping one process per module.

Within Option 2, the declaration lives in the catalog entry (your choice) because the entry is already what the planner and the render read; putting config there means the editor, the render's env file and #3 all see the same declaration, and nothing can drift (basis: spec 0002, one schema for planner and editor). Generating Go from it, checked in and run through `go tool`, follows how `oapi-codegen` already works in this repository, so a stale file fails CI the same way (basis: spec 0001, `task generate` and the CI diff check). Explicit env names per value (your choice over derived names) keep native names possible and make the contract read like the entry; the cost is that a second language must read the entry rather than compute names, which it does anyway.

On protection, the per binding bearer token keeps the credential model of principle 8 and looks like PostgreSQL roles and S3 keys, and works for calls with no user. You chose short lived certificates and the deployer as the issuer, where I recommended one year certificates and a separate step-ca module. Your choice gives the strongest certificate hygiene and no new container, and embedding step-ca's own packages keeps the CA code real rather than house built (basis: smallstep certificates). The tradeoffs you accept: every managed SDK module depends on the deployer being up to restart, and the CA key lives in the service with the Docker socket; both are written into Consequences. ACME tls-alpn-01 needs no enrolment secret per module, because a module proves its name by answering on its own TLS port (basis: RFC 8737), and Go's `autocert` already speaks it against a custom directory (basis: `golang.org/x/crypto/acme/autocert`).

You chose `api` as the only minter of the internal caller token, where I recommended a separate token issuer capability. That keeps thread 1 smaller, and since `api` already signs users in, it is the natural first minter; the cost is that user apps behind the ingress carry no caller until a later feature adds a path. EdDSA with a key id from the RFC 7638 thumbprint, a per target audience and a 5 minute lifetime keep replay narrow without a token store, and go-jose v4 forces the allowed algorithms to be named at parse time, which closes the classic `alg` confusion attack (basis: go-jose v4 release notes; OWASP JWT guidance).

You chose to fail a request when the policy source cannot answer, where I first offered a fallback to base values. Failing is the safer default for per user providers: serving a user the base provider can put their data in the wrong place. To keep that rule honest, every getter returns an error, and the cost of that is a little more code at each read.

Env vars and files stay the only sources in thread 1 even in managed mode. That's a stated interim: the config server is #17, and the getter design means modules do not change when it lands. The `managed_env` rule is built now behind the push source option, so the managed rule you described ("no env or files on a managed network") is already tested and turns on with #17.

### Smaller calls made while writing (pick, why, runner up)

| Call | Pick | Why | Runner up |
|---|---|---|---|
| Bool syntax | exactly `true` or `false` | the same in every language, no surprises like `yes` | Go's `strconv.ParseBool` (accepts `1`, `t`, `T`) |
| Empty value | counts as unset | `KEY=` lines in the rendered env file then mean "required, not filled" | empty is a real value (breaks the required line idea) |
| Unknown key in an env file | error `unknown_key` | a typo never silently drops a setting | warning only |
| Bad pushed snapshot | refuse it whole, keep the previous one | never run with a mixed snapshot | apply the good values only |
| Per caller budget and cache | 200 ms, 30 seconds per caller and value | bounded latency, fewer calls to the policy source | no cache (a policy call on every read) |
| Provides per SDK module | at most one capability, one port | one listener and one clients directory in v0alpha1 | path prefixes per capability on one port |
| Certificate cache | memory only | no key on disk; a restart enrols again in seconds | a cache directory on a volume |
| Readiness on certificate | 503 under 2 hours left | 24 hour certificates renew after 16 hours, so 2 hours means renewal has failed for 6 hours | 4 hours |
| Caller key files | PEM, key id from the thumbprint | `openssl` can make them in a setup script, no key id to store | a JWK set file |
| Binding token paths | `/run/secrets/uses/<cap>/token` and `/run/secrets/clients/<cap>/<consumer>` | one secret mounted on both sides; the consumer name comes from the file name | one shared directory |
| Where `sdk` and `libs` meet | `libs` imports `sdk/manifest` for the config rules | one copy of the rules; `sdk` stays light for third parties | a copy of the rules in `libs/contract` |
| End to end test | `e2e` tagged test in `libs` with testcontainers compose | runs the real rendered Compose file; `sdk` must not import `libs` | a new smoke script stack |
| Planner in the catalog | add `catalog/planner.yaml` | the entry is where its declaration must live (principle 2) | a hidden entry outside `catalog/` |
| Caller token trust (cross check) | only after binding auth, `iss` must equal `CallerModule` | ties the user claim to the module that proved itself | trust any signed token (replayable by any module that received one) |
| Container user (cross check) | `user: "${MP_UID}:${MP_GID}"` from `setup.sh` | Compose without swarm ignores secret `uid`, `gid` and `mode`, so 0600 files are readable only as the host user | world readable secret files (0644) |
| ACME start (cross check) | listen, then enrol explicitly, renew hourly | autocert enrols lazily on the first handshake and renews on demand only | wait for the first real handshake |
| Readiness after first success | not rechecked | a later provider outage does not flap every consumer | recheck continuously |

## References

**Project sources**:
- Spec 0001, principles 8 and 9, *Managed mode wiring*, and AC-5 (the planner's internal mode)
- Spec 0002, the `v0alpha1` catalog entry, render rules and secret naming
- Spec 0004, thread 1 contents and the refused graph guard
- Scope #27 (done line), #16, #17, #21, #3
- `libs/platform` and `services/planner/internal/config/config.go` (the code this replaces)
- Installed skills: `golang-security`, `golang-testing`, `golang-dependency-management`

**Practices & standards**:
- ACME, RFC 8555; tls-alpn-01, RFC 8737
- JWK thumbprint, RFC 7638
- The `<NAME>_FILE` convention for secrets (official Postgres image)
- OWASP JWT guidance: name allowed algorithms, short lifetimes, audience checks
- Per binding (least privilege) credentials

**Links** (web verified in the design conversation; open questions are listed in index.md Follow-up):
- minio-go v7: https://pkg.go.dev/github.com/minio/minio-go/v7
- go-jose v4 jwt: https://pkg.go.dev/github.com/go-jose/go-jose/v4/jwt
- testcontainers-go: https://pkg.go.dev/github.com/testcontainers/testcontainers-go
- pebble: https://github.com/letsencrypt/pebble
- autocert: https://pkg.go.dev/golang.org/x/crypto/acme/autocert
- smallstep certificates: https://github.com/smallstep/certificates
- pgx v5: https://pkg.go.dev/github.com/jackc/pgx/v5
- RFC 8737: https://www.rfc-editor.org/rfc/rfc8737
