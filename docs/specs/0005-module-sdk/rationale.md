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

On protection, the per binding bearer token keeps the credential model of principle 8 and looks like PostgreSQL roles and S3 keys, and works for calls with no user. You chose short lived certificates and the deployer as the issuer, where I recommended one year certificates and a separate step-ca module. Your choice gives the strongest certificate hygiene and no new container, and embedding step-ca's own packages keeps the CA code real rather than house built (basis: smallstep certificates). The tradeoffs you accept: every managed SDK module depends on the deployer being up to restart, and the CA key lives in the service with the Docker socket; both are written into Consequences. ACME tls-alpn-01 needs no enrolment secret per module, because a module proves its name by answering on its own TLS port (basis: RFC 8737), and Go's `x/crypto/acme` client speaks it against a custom directory (basis: `golang.org/x/crypto/acme`). `autocert` was the first pick; the engine was replaced on 2026-11-01 (see *ACME engine* below).

### Certificate names (decided 2026-10-31, while building task 6)

autocert refuses any server name without a dot, and `MP_INSTANCE` is a single DNS label (spec 0002's naming rule), so the design as first written could never enrol. The issuers are not the problem: pebble v2.10.1 and step-ca 0.30.2 both issued `planner` and `deployer-docker` over tls-alpn-01 on port 8443 (evidence below). The choice was between keeping autocert and changing the name, or keeping the name and changing the client.

| Option | Pros | Cons |
|---|---|---|
| Own small manager on `x/crypto/acme` (my recommendation) | name stays `MP_INSTANCE`, the Compose service name; no render change; no new dependency; explicit enrolment and hourly renewal were already written around autocert | about 200 lines of ACME order, challenge and renewal code the project owns and must test; pebble needs `CreateCertFromOrder` (it sends no `Location` on finalize) |
| Keep autocert, dotted names (chosen) | autocert is maintained by the Go team and already handles challenge certificates, renewal timers and concurrent handshakes; less ACME code in the repo | one more bootstrap name, a network alias per SDK service, task 5's render changes, the certificate name differs from the instance name, and a client hello rewrite so TLS 1.3 only clients do not trigger an RSA enrolment |
| lego v4 as a library | mature, widely used ACME client | one large module for one challenge type; its own renewal model to fit around readiness |
| certmagic | full automation, storage, OCSP | heaviest; built for public certificates; its internal name handling would need its own check |

You chose to keep autocert with dotted names, where I recommended a small manager on the plain ACME client (the engine half of this call was reversed on 2026-11-01, see *ACME engine* below; the dotted names stay). Your choice keeps ACME protocol code out of the repository and leans on a library the Go team maintains, which is the boring option for a GA tier security path. What it costs is spread across the render, the bootstrap names and the `aud` rule, all written into index.md, plus one non obvious workaround (the hello rewrite, below). Both paths work against both issuers, so this is a call about where the complexity lives, not about correctness.

The name is `<instance>.<graph>.internal`. The graph part keeps names unique when one issuer in `deployer-docker` (#16) serves several stacks, so a certificate from one stack is never valid for a service of the same name in another. `.internal` is reserved by ICANN for private use, so it can never shadow a public name (basis: ICANN's 2024 `.internal` reservation). The suffix arrives as `MP_DOMAIN` rather than a full name so the instance part cannot disagree with `MP_INSTANCE`, and it is used in plain mode too, so a module has one name in both modes and the `aud` a consumer mints from the URL host is exactly what the provider checks.

(This paragraph applies only to the autocert build of task 6 and is gone with the engine change.) The hello rewrite came out of reading autocert while writing this: it decides between an ECDSA and an RSA certificate from the TLS 1.2 cipher suites a client offers, and a Go client with `MinVersion` TLS 1.3 offers only TLS 1.3 suites (measured: 3 suites, no ECDHE_ECDSA). Without the rewrite, that client's first handshake would block on a second, RSA enrolment. TLS 1.3 requires every client to support ECDSA P-256 signatures, so telling autocert so is always true for a TLS 1.3 only server.

### ACME engine (decided 2026-11-01, after building task 6 on autocert)

Task 6 shipped TLS on autocert as decided above, and building it showed how much code sat around autocert rather than in it. The SDK had to start enrolment itself with a made up client hello, because autocert only orders on a real handshake. It rewrote every hello so autocert would not order a second, RSA certificate, a rule that hangs on autocert's unexported key type logic. It read the held certificate back through `GetCertificate` once an hour, because autocert exposes no state, and had to avoid that call before enrolment, because it blocks. Renewal ran on autocert's private timer and clock, so no unit test could drive it, and a failed renewal waited 30 to 60 minutes before the next try. That last part works against the 2 hour readiness rule. The backoff, readiness and logging were already the SDK's own code; autocert contributed the order and challenge steps and a timer the SDK could not see.

| Option | Pros | Cons |
|---|---|---|
| Keep autocert (as built) | already built and green against pebble; the Go team maintains the order logic | the hello rewrite on unexported behaviour; enrolment by a made up hello; renewal and its retry cadence untestable and outside readiness; state only visible through `GetCertificate` |
| Own manager on `x/crypto/acme` (chosen, your pick and my recommendation) | the order flow is a straight line of protocol client calls; one ECDSA key type by construction, so no rewrite; renewal, retry and readiness in one place with a clock tests replace; handshakes never order | about 250 lines of order, challenge and renewal code the project owns; pebble needs `CreateCertFromOrder`; a lost account has to be handled by us |
| lego v4 as a library | mature, widely used, many challenge types | one large module for one challenge type; its own renewal model would again sit outside readiness |
| certmagic | full automation and storage | heaviest; built for public certificates and on disk storage, which this SDK avoids |

You chose the own manager. It moves the hard parts into code the project can test end to end: every rule the spec states about renewal, retry and readiness now has one owner and a unit test, where before two of them lived in autocert's private state. The protocol work stays in `x/crypto/acme`, which the Go team maintains, so the code the SDK adds is orchestration (order, challenge, finalize, check, wait), not ACME message handling (basis: RFC 8555 order flow; the 2026-10-31 probe, which ran exactly this flow against both issuers). What you accept is owning that orchestration and its tests; the Issuer seam keeps it replaceable.

The names were asked again, since single label names now work. You kept `<instance>.<graph>.internal`: the reasons in *Certificate names* that are not about autocert still hold (one issuer for several stacks in #16, and an issuer joined to several stack networks must resolve the name it validates without ambiguity), and the render, alias and `aud` work is already built.

The smaller calls for the engine are in the table below, marked "(engine)".

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
| Certificate and account keys | memory only; a fresh account key per start | no key on disk, no volume per SDK service; a restart registers and enrols again in seconds | a cache directory on a volume |
| Readiness on certificate | 503 under 2 hours left | 24 hour certificates renew after 16 hours, so 2 hours means renewal has failed for 6 hours | 4 hours |
| Caller key files | PEM, key id from the thumbprint | `openssl` can make them in a setup script, no key id to store | a JWK set file |
| Binding token paths | `/run/secrets/uses/<cap>/token` and `/run/secrets/clients/<cap>/<consumer>` | one secret mounted on both sides; the consumer name comes from the file name | one shared directory |
| Where `sdk` and `libs` meet | `libs` imports `sdk/manifest` for the config rules | one copy of the rules; `sdk` stays light for third parties | a copy of the rules in `libs/contract` |
| End to end test | `e2e` tagged test in `libs` with testcontainers compose | runs the real rendered Compose file; `sdk` must not import `libs` | a new smoke script stack |
| Planner in the catalog | add `catalog/planner.yaml` | the entry is where its declaration must live (principle 2) | a hidden entry outside `catalog/` |
| Caller token trust (cross check) | only after binding auth, `iss` must equal `CallerModule` | ties the user claim to the module that proved itself | trust any signed token (replayable by any module that received one) |
| Container user (cross check) | `user: "${MP_UID}:${MP_GID}"` from `setup.sh` | Compose without swarm ignores secret `uid`, `gid` and `mode`, so 0600 files are readable only as the host user | world readable secret files (0644) |
| ACME start (cross check, updated for the engine) | listen with `/readyz` 503, then `Run` orders in the background | the first certificate does not wait for a handshake; a handshake never orders | order before listening (the issuer cannot validate a port nobody listens on) |
| Readiness after first success | not rechecked | a later provider outage does not flap every consumer | recheck continuously |
| `MP_DOMAIN` in TLS mode | required, `tls_config` when missing | names stay unique per stack under one issuer (first reason was autocert's dot check, gone with the engine) | allow single label names in TLS mode |
| Certificate key type (engine) | ECDSA P-256, a fresh key per order | every TLS 1.3 client must support it; a leaked key dies with its leaf | Ed25519 (not mandatory in TLS 1.3, some clients refuse it) |
| `iss` after the rename | stays `MP_INSTANCE` | it is compared with `CallerModule`, a clients directory file name, which is the instance | the minter's host name (needs a mapping on the checking side) |
| Testing renewal and the 2 hour rule (engine) | fake issuer and fake clock for renewal, backoff, deadline and readiness; pebble with a 90 second profile for one real renewal | renewal is now our code, so the unit tests drive the production path | pebble only (slow, and cannot show the retry cadence) |
| When to renew (engine) | two thirds of the lifetime, no jitter, never sooner than 30 seconds after an issue | deterministic and testable; a few dozen modules renewing together is no load for one issuer; the floor stops an order loop when an issuer hands out tiny or backdated leaves | jitter of a tenth of the last third (autocert's), or ACME Renewal Information (no `x/crypto` support) |
| Expired certificate (cross check) | keep serving it, `/readyz` 503 | matches "they keep serving"; dropping it changes nothing for clients, which refuse it either way | stop serving TLS |
| Downgrade detection (cross check) | an `http://` host ending in `.<MP_DOMAIN>` | a consumer has no other sign that a URL points at an SDK provider; non SDK providers keep bare names | a per binding flag written by the render |
| Bare SNI in TLS mode (engine) | the one held certificate for any server name | the certificate names only the host name, so a client checking another name refuses it on its side; a healthcheck by IP with the right `ServerName` keeps working | fail the handshake for any name but the host name (autocert's behaviour; no security gain) |
| Retry (engine) | backoff 1 second doubling to 30 seconds, forever, same at start and on renewal | a module is ready within 30 seconds of the issuer recovering; one cadence to test | up to 10 minutes, or a slower cadence while a valid certificate is held |
| Attempt deadline (engine) | 5 minutes (your pick) | tolerates a slow issuer; the backoff starts only after a stuck attempt is cut | 1 minute (my recommendation: local issuers answer in under a second, so a hung order would block readiness for less time) |
| Account (engine) | registered once per process; after `accountDoesNotExist`, registered again with the same key | an issuer restart (pebble keeps accounts in memory) must not leave a module unable to renew until its own restart | register before every order (one extra round trip each time) |
| Issued certificate check (engine) | verify against `MP_CA_FILE` for the host name, key match, valid now; a failure is a failed attempt | a module only reports ready with a certificate its clients will accept | trust whatever the issuer returns |
| Challenge answer (engine) | only for an `acme-tls/1` only hello whose server name is the host name, only while an order waits | the challenge certificate is never shown to normal clients or for other names | answer any `acme-tls/1` hello |
| Account already registered (engine, cross check) | `ErrAccountAlreadyExists` counts as success | `x/crypto` caches the account URL before returning it, so the client is usable | fail and retry (loops forever against an issuer that kept the account) |
| Overlapping `Issue` (engine, cross check) | one mutex for the whole `Issue`, one pending answer slot | `Issue` is exported, so `Run` being the only caller is not enough | a map of pending answers by token (more state for a case the SDK never needs) |
| Authorization states (engine, cross check) | `valid` skipped, `pending` answered, anything else fails the attempt | a new order on the next attempt is the simplest recovery | try to revive a dead authorization |
| Clock skew (engine, cross check) | 1 minute tolerance on `NotBefore` in the leaf check | an issuer whose clock runs slightly ahead would otherwise fail every attempt | no tolerance (stalls on any forward skew) |
| Leaf with under 2 hours left on arrival (engine, cross check) | held; `/readyz` reports it | rejecting it would break the 90 second pebble renewal test and gains nothing a client cannot see | count it as a failed attempt |
| CA file (engine, cross check) | read once at start; a new root needs a restart | one read on the security path, same as the clients' trust | read it again per attempt |
| ACME HTTP client (engine, cross check) | no proxy from env, 30 second response header timeout | the issuer is internal; a proxy setting meant for outbound traffic must not catch it | `http.DefaultTransport` as is |
| Where the engine lives (engine) | exported `tlsacme.Client` beside the manager | one package owns the whole TLS path; tests swap the `Issuer` | an internal sub package (more structure for little code) |
| Start `Source` rank (2026-11-01) | above the process env, `<ENV>_FILE`, env files and default (your pick) | a source the module wires in code (a vault, #9) is authoritative, the way a push source is in managed mode; nothing lower can shadow it | after the process env, before env files (my recommendation: keeps `environment:` as the per container override); below env files (Viper's order) |
| Start sources per module (2026-11-01) | one; a second `WithSource` replaces the first; label `source` | matches one push and one caller source; no name rule needed | several named sources, label `source:<name>` |
| Start `Source` failure (2026-11-01) | `source_error` per value; all start lookups run one after another on `sdk.Run`'s signal context under one 10 second deadline; an error fails only its value | a hung store must not hang the start past the healthcheck's `start_period`; one line per value keeps AC-3's shape | no deadline (a hung source blocks forever); parallel lookups (more code for a start path) |
| `source_error` text (2026-11-01, cross check) | fixed text, the Source's error never printed | the Source controls its error text, which may carry a secret or token (AC-4) | print `err.Error()` (more helpful, can leak) |
| Below a Source answer (2026-11-01, cross check) | lower levels not read, so no `ambiguous_source`, `secret_in_env` or env file line warning for that value; env files still parsed whole | "the highest level wins whole" already holds for the other levels; env file problems are about the file, not the value | check every level anyway (reports problems in settings that are never used) |
| Empty Source answer (2026-11-01, cross check) | only `""` is empty; spaces go to the parser | env vars are not trimmed either; trimming would hide a store's bad data | trim whitespace |
| `config loaded` with a push source (2026-11-01, cross check) | logged on each accepted snapshot, not at start | the labels are only known once a snapshot is in | log once at start with no values |
| Names `module` and `push` (2026-11-01, cross check) | not reserved | the code and message already tell the lines apart; a catalog rule for it costs more than it saves | reserve them in the catalog rules |
| `WithSource` with `WithPushSource` (2026-11-01) | stops the start (`source_error`, value part `module`) | a snapshot is complete, so the start source could never matter; silently ignoring a wired option hides a mistake | ignore the start source |
| No first snapshot (2026-11-01) | `config: push (push): no_snapshot: no snapshot arrived within 30 seconds` | the value part matches the `push` label; a new code keeps it apart from `missing`, which sends you to the env files | `module (module)`, or reuse `missing` |
| Undeclared name in a pushed snapshot (2026-11-01) | `unknown_key`, value part = the name, snapshot refused whole | the same fact and line shape as an undeclared env file key; a typo never drops a setting. #17 pushes only the names the running version declares | a new code `unknown_value` |

## Evidence: ACME issuance probe (2026-10-31)

A small Go client on `golang.org/x/crypto` v0.58.0 ran in a busybox container on a Docker network, with its name as a network alias, against each issuer. It is not in the repository; rerun it from the steps here if a version changes.

| Issuer | Client | Name | Result |
|---|---|---|---|
| pebble v2.10.1 (`tlsPort` 8443) | `x/crypto/acme` | `planner`, `api`, `deployer-docker` | issued, SAN = the name, 90 day default lifetime |
| step-ca 0.30.2 (validation on 443) | `x/crypto/acme` | `planner`, `deployer-docker` | issued, 24 hour lifetime |
| step-ca 0.30.2 (`--acme-tls-port 8443 --insecure`) | `x/crypto/acme` | `planner`, `deployer-docker` | issued, 24 hour lifetime |
| pebble v2.10.1 | `autocert` | `planner` | refused by autocert before any request: `server name component count invalid` |
| pebble v2.10.1 | `autocert` | `planner.demo.internal` | issued |
| step-ca 0.30.2 (8443) | `autocert` | `planner.demo.internal` | issued |

Checked again against pebble v2.10.1 on 2026-11-01, while building task 8: a lost account is answered with the problem type `urn:ietf:params:acme:error:accountDoesNotExist`, which is what `tlsacme.Client` looks for before it registers again; and pebble does not backdate `NotBefore`, so a 90 second leaf renews 60 seconds after issue and `TestTLSAgainstPebble`'s window of 25 to 35 seconds before expiry needs no change. step-ca 0.30.2 was not checked for either; that moves to #16, where step-ca enters.

Also found: `CreateOrderCert` fails against pebble (no `Location` header on finalize) while `CreateCertFromOrder` works with both; step-ca's tls-alpn-01 port is the package variable `acme.InsecurePortTLSALPN01` (`acme/challenge.go`), which the binary sets only with `--insecure`; pebble's lifetime is set per profile (`profiles.<name>.validityPeriod`, seconds).

## References

**Project sources**:
- The ACME issuance probe above (2026-10-31); `x/crypto` v0.58.0 `acme/autocert/autocert.go` (the dot check and `supportsECDSA`), `acme/rfc8555.go` (`AuthorizeOrder`, `WaitOrder`, `CreateCertFromOrder`) and `acme/acme.go` (`TLSALPN01ChallengeCert`); step-ca v0.30.2 `acme/challenge.go` and `commands/app.go`; pebble v2.10.1 `cmd/pebble/main.go`
- Spec 0001, principles 8 and 9, *Managed mode wiring*, and AC-5 (the planner's internal mode)
- Spec 0002, the `v0alpha1` catalog entry, render rules and secret naming
- Spec 0004, thread 1 contents and the refused graph guard
- Scope #27 (done line), #16, #17, #21, #3
- `libs/platform` and `services/planner/internal/config/config.go` (the code this replaces); `sdk/tlsacme` as built on autocert in task 6 (the code the engine change replaces)
- Installed skills: `golang-security`, `golang-testing`, `golang-dependency-management`

**Practices & standards**:
- ACME, RFC 8555; tls-alpn-01, RFC 8737
- TLS 1.3 mandatory signature algorithms (ECDSA P-256), RFC 8446 section 9.1
- `.internal` reserved for private use (ICANN, 2024)
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
