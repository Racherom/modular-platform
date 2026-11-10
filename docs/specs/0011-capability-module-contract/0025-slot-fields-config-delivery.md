# 0025. Slot fields in the SDK and config values for every module

**Date**: 2026-11-06
**Parent**: [0011. Capability and module contract](index.md)
**Extends**: [0005](../0005-module-sdk/index.md) (*Generated code*, *Bindings*), [0013](0013-module-manifest-v1.md) (`config`, *Asked values*, *Install time*, *Managed flavor*) and [0012](0012-capabilities-versions-matching.md) (the graph v1 instance)

## Summary

Two pieces of the v1 contract had no path to the container. A module built on the SDK could not read a binding that has no ready client (an OIDC provider, the ingress), because only the SDK may read env and it made getters only for HTTP, PostgreSQL and S3 clients. A module not built on the SDK (Caddy, the `web` image) never received the config values nobody asks and no Compose field names, such as Caddy's `tls-mode`. This spec gives the SDK a typed getter per such slot, renders every non SDK config value as an `.env` variable with its default, and lets the graph itself carry config values per instance, which you set in the editor's inspector. It lands before milestone 4 task 17, which turns the paper platform into real entries.

## Requirements

**User stories**:
- As a module author on the SDK, I want the fields of a binding without a client delivered as typed values, so that I never read env myself and a bad binding stops the start.
- As a self hoster, I want every setting of every module in one `.env` with its default, so that I change Caddy's TLS mode the same way I change any value.
- As someone designing a stack in the renderer, I want to set an instance's config values in the editor, so that the zip I download already holds them.

**Acceptance criteria** (numbered on from 0011):
- **AC-22**: For each slot of a `runtime: sdk` entry that has no `client` and maps at least one field in `env`, `sdkgen` writes a getter `Uses.<Slot>(ctx) (<Slot>Binding, error)` whose struct has one field per mapped capability field, typed by the capability: `string` → `string`, `int` → `int`, `bool` → `bool`, `url` → `*url.URL`, `enum` → a generated string type with one constant per value, `secret` → `sdk.Secret` (the file's content), `certificate` → `[]byte` (the file's PEM). An optional slot's getter returns `(<Slot>Binding, bool, error)`, false when unbound. An optional field is a pointer (`*string`, `*int`, `*bool`, `*url.URL`, `*sdk.Secret`, `*<enum type>`), nil when not delivered; an optional certificate is a nil slice.
- **AC-23**: The SDK reads and checks those fields once at start, with the config values. A required field unset, a value its type refuses, a `pattern` or `values` miss, an unreadable secret file, or a certificate file holding no certificate stops the start with one line each, `config: uses.<slot>.<field> (<ENV>): <code>: <message>`, codes as 0005 (`missing`, `invalid`, `no_match`, `not_allowed`). An optional slot with every mapped env unset or empty is unbound; with any set, its required fields must all be present. A `BindingResolver` answer for a caller replaces the binding for that call and is checked by the same rules; a bad answer is an error, never a partial value. The `config loaded` line lists each field as `uses.<slot>.<field>` with source `binding`; no log line holds a secret or certificate's content.
- **AC-24**: `renderer` reads its platform ports from an optional `ingress` slot and `deployer-docker` from a required one; their config values `platform-http-port` and `platform-https-port` are gone. `renderer` in `internal` mode with `ingress` unbound stops the start with one line; in `public` mode it starts unbound, so `deploy/online/compose.yaml` is unchanged. `api` reads `origin` and `base_path` from its `public` slot and `issuer`, `client_id`, `client_secret`, `ca` and `admin_email` from its `oidc` slot; its config values `oidc-issuer`, `oidc-client-id`, `oidc-client-secret`, `oidc-ca`, `public-origin`, `base-path` and `admin-email` are gone. `http-ingress` marks `base_path` `optional: true`, so `api` at the site root gets nil, not `missing`. Every env name stays as it was.
- **AC-25**: In every flavor, each non secret config value of a non SDK instance reaches its service as `<ENV>: ${MP_<I>__<V>:-<d>}`, where `d` is the graph's value, else the catalog `default`, else empty; `$` in `d` is written `$$`. Two exceptions keep 0013's form: an asked value the graph does not set stays bare, `${MP_<I>__<V>}`, so a missing answer fails `up`; a `{uid}`, `{gid}` or `{install}/...` default stays `${MP_UID}`, `${MP_GID}` or `${MP_DIR}/...`. Setting the variable in `.env` and running `docker compose up -d` changes the container's env. The env file the flavor ships (static `.env`, platform `.env.example`) holds one commented block per value rendered as `MP_<I>__<V>`, in instance then value name order: its description and `# MP_<I>__<V>=<d>`, or `# asked by setup.sh` for an asked value the graph does not set; token defaults get no block. The managed flavor renders the same lines; the deployer runs Compose with no `MP_*` variable in its process env, so only the project's `.env` (which sets none of them) and the rendered defaults count, and the guard checks the values after interpolation.
- **AC-26**: Each run of `setup.sh` checks every `MP_<I>__<V>` line already in `.env` against that value's `type`, `pattern`, `min`, `max` and `values`, and stops with exit 1 and `setup.sh: MP_<I>__<V> in .env: the answer <why>` on the first bad one, before it asks or creates anything. It skips comment lines, takes the last line of a name, and strips the quotes `setup.sh` writes. An empty value passes for an optional value and is `is empty` for a required one. A missing `.env` passes. A line naming a known instance but no declared value prints `setup.sh: warning: MP_<I>__<V> in .env names no value of <i>` and does not stop. A good line passes and no file changes.
- **AC-27**: A graph instance may hold `config: {<value>: <text>}`. The renderer answers `unknown_config` for a name the entry does not declare; `invalid_config` for text the value's rules refuse, that holds a control character, `{` or `}`, that is two characters or longer and starts and ends with the same quote (`"` or `'`), or that is empty on a required value; `config_not_settable` for a `secret` value, one whose `default` is `{uid}`, `{gid}` or `{install}/...`, or one a `host_mounts` source names; `config_not_allowed` on an external instance; each path gets one problem, the first that applies in the order `config_not_allowed`, `unknown_config`, `config_not_settable`, `invalid_config` (the value's own rules, then the graph text rules of *Config values of every module*); and `missing_config` when a required value (one not `optional`) has no `default`, is not asked, and the graph leaves it out, an error on a non SDK instance and a warning on an SDK one (which still stops at start, as today); each at `modules.<i>.config.<v>`. Graph text is never expanded. An empty text on an optional value means empty on purpose and beats the default. The resolved graph and the digest carry the values; the editor's and the Go canonical writers give byte identical text for every fixture that holds them. An SDK instance's `config/<i>.env` gets `<ENV>=<text>` (uncommented) for a graph value.
- **AC-28**: An asked value that the graph sets is not asked: `setup.sh` prints `set by the graph  MP_<I>__<V>=<text>`, or `.env overrides the graph  MP_<I>__<V>` when `.env` holds a different value; the render uses it as the variable's default (AC-25); `setup.sh` uses it wherever another asked default names it (`https://{domain}`) and as the step env `MP_ASK_<V>`. The managed flavor no longer counts it toward `setup_required`; an instance with any other setup need (a step, an asked secret, an asked value the graph leaves out) is still refused.
- **AC-29**: In the editor, the inspector of a module instance lists each config value of its entry that the graph may set (AC-27), with its description, default and type (a select for `enum`, `true` or `false` for `bool`, text otherwise). Typing a value writes `modules.<i>.config.<v>`; clearing it removes the key (and `config` when empty). A check problem at that path shows under its field. A key the entry no longer declares shows in an *Unknown values* list with its `unknown_config` problem and a remove button. Whether a value may be set comes from the catalog answer (its type, its default, and whether a `host_mounts` source names it); values are checked only by the resolved graph, as the editor does today. An external instance shows no config section.
- **AC-30**: The rendered platform (0015) picks Caddy's certificate source by `import tls-{$MP_TLS_MODE}`, routes `/` to the `web` binding while `MP_WEB_UPSTREAM` is unset and to the Vite server under the dev overlay (the `caddy` entry declares no `web-upstream` value; `MP_WEB_UPSTREAM` is a dev hook only the overlay sets), and serves `<meta name="mp-signed-out">` from `MP_SIGNED_OUT`, rendered as `${MP_WEB__SIGNED_OUT:-index}`; the platform graph sets `renderer`'s `renderer-mode: internal`.

## Decision

**Chosen option**: a generated, typed getter per SDK slot with no client, checked at start; every non secret config value of a non SDK module as a `.env` variable with its default; and config values carried by the graph, set in the editor, used as those defaults.

**Implementation skills**: `golang-structs-interfaces` · `golang-naming` · `golang-error-handling` · `golang-testing` · `golang-security` (all `samber/cc-skills-golang`, `.agents/skills/<name>/`) · `openapi-spec-generation` (`wshobson/agents`, `.agents/skills/openapi-spec-generation/`) · `react-state-management` and `react-flow` (`.agents/skills/<name>/`, the inspector and the graph store)

## Slot fields in the SDK

- **Which slots**: a `uses.<s>` of a `runtime: sdk` entry with no `client`. Its getter carries exactly the fields its `env` maps (0013's delivery, unchanged); a slot that maps none gets no getter. A slot with a `client` keeps its client getter and gets no field getter.
- **Generated code**: `sdkgen` reads each mapped field's `type`, `optional`, `pattern` and `values` from the capability file and writes them into the slot's `BindingSpec` (a new `Fields []sdk.FieldSpec{Name, Env, Type, Optional, Pattern, Values}`). The struct is `<Slot>Binding`, fields in Go case by the generator's existing rule (`client_id` → `ClientId`, `http_port` → `HttpPort`), listed in name order, each with a doc comment from the field's `description`. Enum types are `<Slot><Field>` with constants `<Slot><Field><Value>`, as for config enums.
- **Loading**: in `sdk/internal/load`, beside the client kinds. Each field parses by 0005's rules for a config value of the same type (`url` absolute with scheme and host, `secret` read and trimmed as a config secret's `_FILE`, `enum` and `pattern` as config). A `secret` or `certificate` field's env holds the file path (0013); the SDK reads the file at start. A certificate is accepted when the file parses as PEM with at least one `CERTIFICATE` block (the `MP_CA_FILE` rule). A set but empty env counts as unset, so a required field is `missing`. Slot field lines join 0005's one sorted problem list (value part `uses.<slot>.<field>`). `load.Binding` gains `Fields map[string]any` with the parsed values; the generated getter copies them into the struct. Go name clashes are an `sdkgen` error, as 0005 says for getters. Capability fields carry no `min` or `max` (0012), so neither does `FieldSpec`.
- **Bound**: an optional slot is bound when any mapped env is set and not empty. A provider of an optional slot whose mapped fields are all optional and empty therefore reads as unbound; accepted, since no platform slot is shaped so.
- **Per caller**: `Uses.<Slot>(ctx)` asks the `BindingResolver` first, as client getters do (0005), with the same 200 ms budget, the same 30 second cache per caller and slot, and `sdk.ErrCallerSource` on a timeout. An answer holds parsed content (secret and certificate bytes, not paths), replaces the whole binding for that call, and may bind a slot that is unbound at start.
- **Platform entries**: `renderer` `uses.ingress` (`optional: true`, `env: {http_port: MP_PLATFORM_HTTP_PORT, https_port: MP_PLATFORM_HTTPS_PORT}`); in `internal` mode an unbound slot makes `setup` return an error wrapping `sdk.ErrStart` with the line `config: uses.ingress (MP_PLATFORM_HTTP_PORT): missing: internal mode needs the ingress binding`. `deployer-docker` `uses.ingress` required, same env. `api` as in 0015's paper entry; `client_id` now comes from `{platform: consumer}`, so the rendered client id is `api` instead of the old default `mp-api`.

## Config values of every module

- **Non SDK delivery**: every config value of a non SDK entry that is not `secret` renders as a Compose variable, 0013's naming `MP_<I>__<V>`, with the exceptions of AC-25 (bare asked values, token defaults). Values already rendered that way (named by `publish`, `host_mounts`, `network.aliases` or a `{config}` source) keep their form, with the graph value as their default. An optional value with no default and no graph value renders `${MP_<I>__<V>:-}`, so the env name is always present and empty means unset. 0013's `config` rule gains: a non SDK entry whose image does not read an empty env as unset gives that value a `default`. Secret values keep 0013's file delivery.
- **SDK delivery**: unchanged (`config/<i>.env`, 0005), except a graph value writes `<ENV>=<text>` instead of the commented default.
- **Graph field**: `modules.<i>.config` is a map of value name to string, in format 1.0, since v1 is not released. Both canonical writers sort its keys, always write each value double quoted (escaping only `"` and `\`, since control characters are refused), and leave out an empty `config`. A reader accepts any YAML string scalar; a non string scalar is `invalid_graph`. Graph values are run time like `{config}` sources: templates never read them, and `.env` can still override them.
- **Checks**: the checks check graph values with the same parser the SDK uses (`sdk/manifest` gains `CheckValue(ConfigValue, string) error`, which `libs/check` calls), so a value the renderer accepts is one the module accepts.
- **Graph text rules** (task 1's spike, 0011 rationale): besides the value's own rules, `libs/check` refuses three shapes of graph text as `invalid_config`, on every instance whatever its `runtime`, checked in this order: a control character (a rune for which Go's `unicode.IsControl` is true, so C0, DEL and C1, plus U+2028 and U+2029); `{` or `}` (Compose misreads `}` inside `${V:-…}`); and a text of two characters or more whose first and last characters are the same ASCII quote, `"` or `'` (`sdk/envfile` strips that pair from a `config/<i>.env` line, so `"q"` would reach the module as `q`). The test is on the exact text, nothing trimmed: ` "q"`, `"q`, `a"b`, `"q'` and a lone `"` pass. These rules live in `libs/check`, not in `CheckValue`, since the same text from the process env reaches the SDK unchanged and stays valid there. Only Go decides; the editor shows the resolved graph's problem (AC-29). Each message names the rule, never the text: `the text holds a control character`, `the text holds { or }, which Compose misreads`, `the text starts and ends with the same quote, which the env file reader removes`.
- **Quoted enum values** (catalog): since the graph can never set such a text, a config value's `values` item of two characters or more with the same ASCII quote at both ends is `invalid_rule` at `config.<v>.values.<n>` in `sdk/manifest`'s enum check, with a case in `contracts/fixtures/catalog/sdk-fields.yaml`. A `pattern` that only matches such texts is the author's mistake and is not checked.
- **Asked and set by the graph**: `setup.sh` does not ask it and does not write it into `.env` (a later graph change then reaches the stack on the next render); `env_get` for another asked default and `MP_ASK_<V>` fall back to the graph value. An `.env` line you add still wins, and `setup.sh` says so (AC-28). Every graph text `setup.sh` prints, bakes in as a default or sets as `MP_ASK_<V>` is written through the render's `shellQuote` (`libs/render/render.go`), so `'`, `"`, `$`, backticks, `\` and `#` stay as typed.
- **`.env` check** (AC-26): `setup.sh` runs the existing `valid` function over the `MP_<I>__<V>` lines of `.env` first thing after `MP_UID` and `MP_GID`; the ask loop uses the same `valid`, so both agree. `setup.sh` embeds the rules of every value rendered as `MP_<I>__<V>` (secrets and token defaults have none). The README says to run `./setup.sh` after you edit `.env`.
- **Caddy and web** (0015): `Caddyfile.tmpl` keeps the snippets `(tls-internal)` and `(tls-acme)` and imports `tls-{$MP_TLS_MODE}`; route `/` proxies to `{$MP_WEB_UPSTREAM:<web upstream>}`. The `caddy` entry drops its `web-upstream` value, so the render never sets `MP_WEB_UPSTREAM` and Caddy's default applies whatever it does with an empty value; `compose.dev.yaml` keeps setting it in the ingress `environment`. `web` reads `MP_SIGNED_OUT` as today.

## API surface

| Surface | Change |
|---|---|
| `contracts/openapi/renderer.yaml` `GraphModule` | `config: {type: object, additionalProperties: {type: string}}`, optional |
| `Problem.code` (check) | `unknown_config`, `invalid_config`, `config_not_settable`, `config_not_allowed`, `missing_config`, each with a fixed English template in `libs/check` |
| `sdk` | `sdk.FieldSpec`; `BindingSpec.Fields`; generated `<Slot>Binding` and `Uses.<Slot>` |
| `sdk/manifest` | `CheckValue` |
| `sdk/CONTRACT.md` | one line per AC-22 and AC-23 rule, each backed by a test |

## Value sourcing

| Action | Value produced / displayed | Source |
|---|---|---|
| SDK start | a slot field's value | the env its slot maps (0013 delivery), its type and rules from the capability file via `sdkgen` |
| SDK start | slot bound or not | any mapped env set and not empty |
| SDK call | a slot field for a caller | `BindingResolver.BindingFor` answer |
| `renderer` start | platform ports | `ingress` binding's `http_port`, `https_port` (from Caddy's `{config}` sources) |
| `api` start | origin, base path, issuer, client id, client secret, CA, admin email | `public` and `oidc` bindings (0015 sources) |
| Render | a non SDK value's Compose default | graph `modules.<i>.config.<v>`, else catalog `default`, else empty |
| Render | an SDK value's env file line | graph value, else 0005's commented default |
| Render | env file blocks | the value's `description` and its default as above |
| `setup.sh` | a value set by the graph | the default baked into the rendered Compose line and `setup.sh` |
| `setup.sh` | the check of `.env` lines | the value's `type`, `pattern`, `min`, `max`, `values` from the entry |
| Check | the config problems | the entry's `config` section and the graph's `config` map |
| Editor | the inspector's config fields, defaults, types | the catalog answer's `config` per module |
| Editor | problems under a field | check problems at `modules.<i>.config.<v>` |
| Container | Caddy's TLS source | `MP_TLS_MODE` (`MP_INGRESS__TLS_MODE`, default `internal`) |

## Key invariants

- Only `sdk/` reads env and `*_FILE` paths; module code reads a binding only through a generated getter.
- A graph never holds a secret value (0001 principle 6): `secret` values are `config_not_settable`.
- A value the renderer accepts parses the same in the SDK and passes `setup.sh`'s check (one parser in `sdk/manifest`, `valid` tested against it).
- Graph values are untrusted text: checked against the value's rules, free of control characters, `{` and `}`, never two characters or longer with the same quote at both ends, and never expanded, before any render writes them into Compose, `config/<i>.env` or `setup.sh`. A graph never picks a host path (`host_mounts` sources are `config_not_settable`).
- The deployer's Compose runs see no `MP_*` variable from its own process env.
- Every env name a module's code reads keeps its name across this change.

## Critical test scenarios

- `sdkgen` over the test module with a client-less slot holding one field of every type plus an optional one: the getter returns each typed value; the slot optional and unbound gives `false` (**AC-22**).
- SDK start with a required field unset, an `int` field `abc`, an enum miss, a missing secret file and a certificate file of plain text: five lines in 0005's order, exit 1; a resolver answer with a bad field returns an error (**AC-23**).
- `renderer` in `internal` mode without `MP_PLATFORM_HTTP_PORT`: one line, exit 1; in `public` mode: starts. `api` with `MP_OIDC_ISSUER` unset: `config: uses.oidc.issuer (MP_OIDC_ISSUER): missing`; with `MP_BASE_PATH` empty: starts at the site root (**AC-24**).
- Golden: a non SDK module with a value with a default, an optional one without, an enum, an asked one (bare), a `{uid}` default, and a graph value holding `$`; the static `.env` and platform `.env.example` blocks (**AC-25**).
- The deployer applies a project while its own env holds `MP_INGRESS__TLS_MODE=acme`: the service gets the rendered default (**AC-25**).
- e2e: set `MP_<I>__<V>` in `.env`, `up -d`, `docker inspect` shows the new env (**AC-25**).
- `setup.sh` with `MP_INGRESS__TLS_MODE=acmee` in `.env`: exit 1 naming it, no file made; with `acme`: passes and a second run changes nothing; a commented bad line, an earlier bad line followed by a good one, and an empty optional value pass; `MP_INGRESS__TLS_MDE=acme` warns and goes on (**AC-26**).
- Check fixtures, one per new code and case (a `host_mounts` source, text with `{`, empty on a required value, `missing_config` as error and as SDK warning); `"q"`, `'q'`, `""` and `"é"` are `invalid_config` on an SDK and on a non SDK instance, while ` "q"`, `"q`, `a"b`, `"q'` and `"` check `ok`; `"{"` on a `secret` value gives only `config_not_settable`, and `"{"` on a plain value only the brace message; a catalog fixture with an enum item `"a"` is `invalid_rule`; a render of an SDK instance with `a"b` and `"q` writes them unchanged into `config/<i>.env` and `sdk/envfile` reads them back unchanged; a writer fixture with `config` holding `yes`, `0x1`, an empty optional value and `a"b` round trips byte identical in Go and TS (**AC-27**).
- `setup.sh` goldens with graph values `a'b`, `$x` and `a #b`: the printed line, the baked default and `MP_ASK_<V>` each hold the text as typed (**AC-28**).
- `setup.sh` from a graph that sets `ingress` `domain`: no domain question, origin default `https://<that domain>`; managed render of a graph whose only asked value the graph sets: no `setup_required` (**AC-28**).
- Vitest: typing, clearing and an `invalid_config` problem in the inspector; Playwright smoke sets one value and downloads a zip holding it (**AC-29**).
- Platform golden for `Caddyfile` and the `ingress` and `web` services (no `MP_WEB_UPSTREAM` line); `smoke-managed` passes with the default TLS mode, and `task dev` serves `/` from Vite under the overlay (**AC-30**).

## Build plan

Tracer Bullet: the SDK strand first on a real service (renderer ports), then the render strand on catalog defaults, then the graph field through renderer, render and editor; the platform entries come last and feed milestone 4.

1. Spike: compose-go reading back `${V:-x}` defaults holding `$$`, `#`, both quotes, `\` and spaces, and `sdk/envfile` reading the same texts as lines; record in 0011's rationale.md. Each text that does not round trip joins `invalid_config`'s refused characters, named in this spec, satisfies **AC-25**, **AC-27**
2. `sdk`: `FieldSpec`, `BindingSpec.Fields`, loading and checks of client-less slot fields, bound detection, `Fields` on `load.Binding`, the resolver path, the `config loaded` entries; `sdkgen` writes `<Slot>Binding` and `Uses.<Slot>`; the test module gains the slot; `sdk/CONTRACT.md` lines with tests, satisfies **AC-22**, **AC-23**
3. `renderer` and `deployer-docker`: `uses.ingress` in their catalog entries, the two values removed, code on `m.Uses.Ingress(ctx)`, the internal mode check; `task generate`; `deploy/managed/compose.yaml` already sets both env names, satisfies **AC-24**
4. `api`: `public` and `oidc` slots in its catalog entry, the seven values removed, code on the getters; `http-ingress` (paper capability) marks `base_path` optional; `deploy/managed/compose.yaml` adds `MP_OIDC_CLIENT_ID: mp-api` (the default is gone) until task 19 retires the file; a test fails when `mp-api` appears outside `deploy/managed/`, satisfies **AC-24**
5. `libs/render`: every non SDK value as a Compose variable with its catalog default, bare asked values and token defaults as 0013, `${…:-}` for optional values, `$$` escaping, env file blocks, the `.env` check in `setup.sh` (skips, last line, quotes, empty, the unknown name warning), README line; the deployer's Compose env without `MP_*`; goldens, satisfies **AC-25**, **AC-26**
6. Graph field: `GraphModule.config` in `renderer.yaml`, `task generate`; `libs/contract` decode and canonical writer (always double quoted, empty map left out); `sdk/manifest.CheckValue`; `libs/check` checks and codes (one problem per path in the AC-27 order, the graph text rules with their messages and the matching quotes case, the `missing_config` warning on SDK instances, `host_mounts` sources not settable); the quoted enum item rule in `sdk/manifest` with its `sdk-fields.yaml` case; digest; fixtures in `contracts/fixtures/graphs/` and the TS writer, satisfies **AC-27**
7. `libs/render`: graph values as Compose defaults and SDK env file lines; asked values set by the graph skipped in `setup.sh` with the `set by the graph` and `.env overrides the graph` lines, the `env_get` and `MP_ASK_<V>` fallback, every graph text through `shellQuote`; the managed flavor's `setup_required` ignores only those values; goldens, satisfies **AC-27**, **AC-28**
8. Editor: the inspector's config section, the *Unknown values* list, `setConfig` and `clearConfig` in `src/graph/edit.ts`, problems under the field, Vitest and the smoke step, satisfies **AC-29**
9. Paper platform: the `caddy` entry's `Caddyfile.tmpl` snippets and upstream line, its `web-upstream` value removed, the `web` entry, `renderer-mode: internal` in the platform graph, the `api`, `renderer` and `deployer-docker` fixtures matching tasks 3 and 4; the catalog check and the check stay `ok`; milestone 4 tasks 17 and 19 build on it, satisfies **AC-30**

## Rationale (short)

A typed getter per slot (your choice) keeps 0005's rule that module code never reads env, takes its types from the capability file the provider already honors, and fails a broken binding at start like a broken config value. Runners up: let a binding fill a config value of the same env name (no `api` code change, but one value declared twice, and a stale env file line could override the binding); a generic string lookup (no generator change, untyped). PEM bytes for certificates match what `api` reads today; a `*x509.CertPool` fits only TLS, and a bare path would push file reading into module code. Renderer's `ingress` slot is optional because the online stack has no ingress; keeping the old values as a fallback would leave two sources for one fact.

For non SDK values, `.env` variables with defaults (your choice) are what the hand written folder already did (`${PLATFORM_TLS:-internal}`), need no new Compose key in the guard, and put every setting in one file; a per instance `env_file` would need guard changes and uncommented defaults, and literal defaults could never change. The graph carrying config values (your choice) is how a value nobody asks gets set at all, and setting it in the editor is where you design the stack; sending graph set asked values past the question (your choice) trades a question for trusting the graph, which a shared graph makes visible through the `set by the graph` line.

Refusing a text wrapped in matching quotes on every instance (your choice, 2026-11-06) keeps one rule for the graph whatever runtime an entry has, so a graph stays valid when a module moves onto the SDK, and it tells someone who typed quotes thinking they were needed instead of passing them on. Runners up: refuse only on SDK instances, where the quotes are surely lost (the spike saw Compose keep `"` and `'` as single characters, but never tested a wrapped pair in `${V:-"q"}`), but then a graph's validity depends on the entry's runtime; or have the render write every SDK line as `<ENV>="<text>"`, which round trips every text since the reader unescapes nothing, but sends `"q"` typed by mistake to the module with no feedback. A cross check on a second model raised that the refusal leaves a deployed graph no way to set such a text before #34, and offered wrapping only the lines that need it; you kept the refusal, since no value needs outer quotes today, and closed the one catalog hole it opens (quoted enum items).

## Consequences

**Positive**:
- `api`, `renderer` and `deployer-docker` read every binding through the contract, so 0015's platform graph can feed them with no second answer.
- Every module, SDK or not, is configured the same two ways: the graph, then `.env`.
- A stack designed in the renderer downloads with its settings in place.

**Negative / tradeoffs**:
- A changed slot field needs a restart, like the clients directory.
- A certificate that a provider writes after start (`{volume}` source) cannot be mapped by an SDK slot; today only Caddy's CA is such a file and no SDK slot maps it.
- A graph shared online can now set your domain and other asked values; `setup.sh` prints each, but does not ask you to confirm.
- `.env` grows one block per non SDK value, and a hand edited value is only checked when you run `setup.sh`, not by `docker compose up`.
- A non SDK image must read an empty env as unset, or its entry must give the value a default.
- `MP_WEB_UPSTREAM` is an env the Caddyfile reads that no entry declares: a dev hook outside the contract, kept because it is the one way to work whatever Caddy does with an empty value.
- The rendered OIDC client id changes from `mp-api` to `api`; installs made before milestone 4 are reinstalled anyway (0011 migration plan).
- The graph format grows a field the editor and both writers must keep in step.
- A value that really is wrapped in matching quotes (`"q"`) cannot be set through the graph on any instance, even a non SDK one where Compose would keep it. You set it by hand instead, wrapped in the other quote: `MP_<I>__<V>='"q"'` (or `"'q'"`) in `.env` for a non SDK instance, `<ENV>='"q"'` (or `"'q'"`) in `config/<i>.env` for an SDK one (each reader strips only the outer pair). A deployed (managed) graph has no such workaround: nobody edits its files, so the text cannot reach a deployed module until #34 lets you change values after apply.
- The commented hint line `# MP_<I>__<V>=<d>` shows a graph text as typed. If you uncomment one that holds `$`, `#`, quotes or spaces at either end, Compose's `.env` reader may read it differently from the rendered default.

**Neutral**:
- Graph values for deployed stacks render like any other; changing them after apply waits for #34's edit layer and #26.

## Follow-up

- [ ] `/sync`: `sdk/AGENTS.md` should say that a slot with no client gets a typed getter, and `libs/render/AGENTS.md` that every non SDK value is a Compose variable.
- [ ] #34 and #26: set or change a deployed instance's config values after apply.
- [ ] Decide how the `.env` hint line writes a graph text that Compose's `.env` reader would change once uncommented (quoting it, or naming `compose.yaml` instead of the text).
- [ ] The editor cannot set an optional value to empty on purpose (AC-27), since clearing a field removes its key (AC-29); decide a control for it, such as an explicit *empty* toggle.
- [ ] If an SDK module ever needs a certificate that its provider writes after start, decide between reading it on first use and a check warning for `{volume}` sources mapped by an SDK slot.

## Out of scope

- `secret` config values on non SDK entries beyond 0013's `generate` and `ask` (#9).
- Setting config values per stack in managed mode after apply (#34, #26).
- Narrowing which graph values a public renderer accepts beyond the value's own rules.
