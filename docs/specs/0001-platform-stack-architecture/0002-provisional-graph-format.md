# 0002. Provisional graph file, catalog entry and check rules

**Date**: 2026-10-29 (updated 2026-10-30: collision free Compose secret names)
**Parent**: [0001. Platform stack and architecture](index.md)
**Status**: Superseded in part by [0011](../0011-capability-module-contract/index.md) (catalog entry, matching and binding render rules); the graph file part stays in force until #4.

> Extended by [0005. Module SDK](../0005-module-sdk/index.md): the catalog entry gains `runtime`, `config`, `uses.<cap>.client` with more `env` keys, and `provides.<cap>.auth` with `env.clients_dir`; the checks gain the rule `missing_token_mapping`; the render gains per instance env files, per binding token secrets and credential file secrets. Those definitions live in 0005, not here.

## Summary

This pins the formats the scaffold (`/develop stack & architecture`) builds on before specs #3, #4 and #6 exist: how a stack is written as a `graph.yaml` file, how a built in catalog entry is written, and how the checks wire a module's needs (`uses`) to another module's offers (`provides`). The format is tagged `modular-platform/v0alpha1` so everyone can see it is provisional; #3 and #4 replace it with `v1`. The sample stack gets its one edge honestly: `whoami` declares an **optional** need for `postgresql` (it ignores what it receives), and the sample file binds it to `postgres` by hand. That edge renders as a real Compose start order plus two address env vars, and it is what AC-7 draws.

## Context

Spec 0001 lets the scaffold use "a provisional entry format (name, image, provides, uses, secrets)" and says graph files are text files "from spec #3 and #4". Neither exists yet, but the scaffold cannot build the renderer, the render, the editor's open and save, or the sample without them, and the builder would otherwise invent all of it. The forces: the format must be small enough to throw away when #3 and #4 land, strict enough that the renderer never guesses, and written so the Go renderer and the browser editor read and write the same file.

AC-7 needs edge markers drawn on the sample graph, so the sample needs exactly one binding. Its two modules (`traefik/whoami` and the official `postgres` image, fixed by 0001) have no real dependency between them, so the edge has to come from the catalog without lying about what whoami does.

Engineer's choices in this session: `apiVersion` plus `kind` header; modules as a map keyed by instance name; bindings inline on the consumer; no stored positions (the editor lays out the graph); exact name matching; a required need with one candidate gets a recommendation (0028), not a binding; ambiguity and cycles are problems; the sample's edge comes from an optional need; optional needs bind only when written explicitly, an unbound optional need with candidates gives a warning, `null` opts out; a binding renders `depends_on` plus address env vars whose names the consumer's catalog entry maps; YAML read and written on both sides from one schema with `sigs.k8s.io/yaml` and the `yaml` npm package; one catalog file per entry; a small layered auto layout; the sample bundled into the editor with its own route.

Update of 2026-10-30: the first rule named a Compose secret `<instance>-<name>`. Both parts may contain hyphens, so two different pairs could give the same name (instance `a-b` with secret `c`, instance `a` with secret `b-c`: both `a-b-c`). The resolved graph said `ok`, then the render failed with a plain error that the service answers as a 500 (review [2026-10-30-main](../../reviews/2026-10-30-main.md)). The built in catalog cannot hit it yet (one secret, `password`), but the next module with a hyphen in a secret name can. Engineer's choice: join the two parts with `_` instead, and keep the in container path equal to the Compose secret name.

## Requirements

These add to the acceptance criteria of [0001](index.md#requirements) and are built in the same scaffold. AC-2, AC-3, AC-4 and AC-7 there rely on the formats below.

**User stories**:
- As a platform contributor, I want one precise, provisional file format and one set of matching rules, so that the renderer, the render, the editor and the sample agree without anyone guessing.

**Acceptance criteria**:
- **AC-8**: `POST /v1/check` returns exactly the problems and bindings listed in *Critical test scenarios* for each case there, with problems sorted by `path` then `code`, and the same request always gets a byte identical response.
- **AC-9**: For the sample graph and every check test fixture, the editor's YAML writer and the Go writer produce byte identical canonical text, and parsing that text on either side gives back the same graph. The `graph.yaml` inside a rendered zip is the resolved graph (every binding written in) in that canonical text.
- **AC-10**: The rendered sample `docker-compose.yml` gives `whoami` `depends_on: {postgres: {condition: service_healthy}}` and the environment `DEMO_POSTGRESQL_HOST=postgres` and `DEMO_POSTGRESQL_PORT=5432`, gives `postgres` its password only as the Compose secret `postgres_password` (`POSTGRES_PASSWORD_FILE=/run/secrets/postgres_password`), and gives `whoami` no secret.
- **AC-11**: The renderer refuses to start (exit code 1, one log line per problem as `catalog/<file>: <dotted.path>: <code>: <message>`) when a built in catalog entry breaks a catalog rule; a Go test runs the same checks over `catalog/` in CI.
- **AC-12**: Opening `<base>/samples/whoami-postgres` shows the two modules, auto laid out with `whoami` left of `postgres`, and exactly one edge with its marker, from whoami's `postgresql` port to postgres's `postgresql` port; reloading that URL shows the same, with nothing read from browser storage.

## Decision

**Chosen option**: one strict, versioned YAML shape for graphs and catalog entries, defined once as OpenAPI schemas in `contracts/openapi/renderer.yaml`, with a pure, deterministic renderer whose checks recommend a provider only for a required need with exactly one candidate.

**Implementation skills**: `openapi-spec-generation` (`wshobson/agents`, `.claude/skills/openapi-spec-generation/`) · `golang-structs-interfaces` · `golang-error-handling` · `golang-testing` (all `samber/cc-skills-golang`, `.claude/skills/<name>/`) · `react-flow` (`existential-birds/beagle`, `.claude/skills/react-flow/`) · `tanstack-router` (`tanstack-skills/tanstack-skills`, `.claude/skills/tanstack-router/`) · `vite` (`antfu/skills`, `.claude/skills/vite/`)

**Rationale (short)**: The map keyed by instance and the inline `uses` map make duplicate instances and two providers for one need impossible by shape, so the checks check less and a diff shows only what changed. Keeping positions out of the file means moving a node never changes the file. Exact name matching and no version syntax avoid inventing anything #3 must break. Auto binding only the unambiguous required case gives #6's "the checks bind the needed capability" without ever guessing. Making whoami's need optional, rather than required, keeps the catalog honest (whoami runs fine alone) and still gives the sample a real edge. One schema in the OpenAPI contract, read through JSON tags on the Go side, keeps a third set of struct definitions from drifting. Runners up: a top level `bindings` list (maps one to one to canvas edges but needs its own duplicate checks); a separate `demo-app` catalog entry (keeps `whoami` pure but shows two near identical entries); the renderer owning all YAML (identical bytes for free, but opening a file needs the server and the public contract grows).

## Format and rules

### Shared naming rules

| Thing | Pattern | Notes |
|---|---|---|
| Graph `name`, instance names, catalog entry `name`, capability names, secret names | `^[a-z]([a-z0-9-]{0,61}[a-z0-9])?$` | A DNS label (lowercase letters, digits, inner hyphens, at most 63 characters), so each works as a Compose service name, a host name and a file name |
| Reserved words | `y`, `n`, `yes`, `no`, `on`, `off`, `true`, `false`, `null` are rejected as any name above (`invalid_name`) | YAML 1.1 readers (the Go side) read these as booleans or null, and the two writers would quote them differently |
| Env var names in a catalog entry | `^[A-Za-z_][A-Za-z0-9_]*$` | Native names like `PGHOST` are allowed |

### `graph.yaml`

```yaml
apiVersion: modular-platform/v0alpha1
kind: Graph
modules:
  postgres:
    module: postgres
  whoami:
    module: whoami
    uses:
      postgresql: postgres
name: whoami-postgres
```

| Field | Type | Required | Meaning |
|---|---|---|---|
| `apiVersion` | string | yes | Exactly `modular-platform/v0alpha1` |
| `kind` | string | yes | Exactly `Graph` |
| `name` | name | yes | The stack's name; becomes the Compose project `name:` |
| `modules` | map: instance name → module | yes (may be `{}`) | One entry per placed module; the key is the instance name and the Compose service name |
| `modules.<i>.module` | name | yes | The catalog entry this instance runs |
| `modules.<i>.uses` | map: capability → instance name or `null` | no | A string binds that need to that instance (an edge). `null` means unbound on purpose (optional needs only). A missing key means "not decided": the checks recommend a provider (required needs) or warn (optional needs) |

No other keys anywhere (strict decoding). The file holds no positions, no settings and never a secret value. Comments are not kept: the editor and the render write the canonical form below.

**Canonical text** (both writers): every map's keys in ascending byte order (which is why `name` comes after `modules`), two space indent, block style, no `---` document marker, `null` written as `null`, LF line endings, one trailing newline. Go: `sigs.k8s.io/yaml.Marshal` (it goes through JSON, so keys come out sorted). Editor: `yaml` package `stringify` with `sortMapEntries: true`, `indent: 2` and `lineWidth: 0` (never fold a line); the graph contains no lists and no free text, only names and `null`, so list indentation and line folding never differ between the two writers.

### Catalog entry (`catalog/<name>.yaml`)

```yaml
apiVersion: modular-platform/v0alpha1
kind: Module
name: whoami
description: Tiny HTTP service that echoes each request. Demo consumer, it declares an optional postgresql need to show wiring and ignores the values it receives.
image: traefik/whoami:<exact tag>
# no healthcheck: the image is FROM scratch, nothing can probe it from inside
provides:
  http:
    port: 80
uses:
  postgresql:
    optional: true
    env:
      host: DEMO_POSTGRESQL_HOST
      port: DEMO_POSTGRESQL_PORT
```

```yaml
apiVersion: modular-platform/v0alpha1
kind: Module
name: postgres
description: PostgreSQL database server, official image, with one generated superuser password.
image: postgres:<exact tag>
provides:
  postgresql:
    port: 5432
secrets:
  - name: password
    env: POSTGRES_PASSWORD_FILE
healthcheck:
  # -h 127.0.0.1: the image's first start init server listens only on the local socket,
  # so a socket check would pass before the real server is up
  test: ["CMD", "pg_isready", "-h", "127.0.0.1", "-U", "postgres"]
  interval: 5s
  timeout: 3s
  retries: 10
```

| Field | Type | Required | Meaning |
|---|---|---|---|
| `apiVersion`, `kind` | string | yes | `modular-platform/v0alpha1`, `Module` |
| `name` | name | yes | Equal to the file name without `.yaml`; unique in the catalog |
| `description` | string | yes | Shown in the editor's catalog |
| `image` | string | yes | Parsed with `github.com/distribution/reference` (already a compose-go dependency): must carry a tag, the tag must not be `latest`, a `@sha256:` digest after the tag is allowed; a registry host with a port is fine |
| `provides` | map: capability → `{port}` | no | `port` (1 to 65535, required) is where a consumer reaches this capability on the service's host name |
| `uses` | map: capability → `{optional, env}` | no | `optional` (bool, default `false`). `env` (optional) maps `host` and/or `port` to env var names the consumer reads; an omitted key delivers nothing for it |
| `secrets` | list of `{name, env}` | no | Each becomes `secrets/<instance>/<name>` (created by `setup.sh` as 32 random bytes in hex, per 0001), the Compose secret `<instance>_<name>`, and `<env>=/run/secrets/<instance>_<name>` (see *Secrets in Compose*) |
| `healthcheck` | Compose healthcheck object (`test` list, `interval`, `timeout`, `retries`, `start_period`) | no | `test` is a list whose first item is `CMD` or `CMD-SHELL`; each duration parses with Go's `time.ParseDuration`; `retries` is 1 or more. Copied as is into the service |

**Catalog rules** (AC-11): every field and pattern above; names unique; within one entry every env var name (from `uses.*.env` and `secrets[*].env`) appears once; an entry may provide and use the same capability (a proxy), but the renderer never binds it to itself. A broken rule makes the renderer log one line per problem as `catalog/<file>: <dotted.path>: <code>: <message>` (for example `catalog/whoami.yaml: uses.postgresql.env.host: invalid_env_name: ...`) and exit with code 1. The sample's `postgres` provides `postgresql` directly; provisioning (`postgresql-provisioner`, 0001 principle 4) is left to #3.

### Check rules (`libs/check`)

A capability matches only the identical name (case sensitive, no versions). A **candidate** for need `C` of instance `i` is every other instance whose catalog entry provides `C`, sorted by name; instances with an unknown module are never candidates.

First the names: the graph `name` and every instance key are checked against the naming rules. An instance whose key breaks them gets only `invalid_name` and is then left out entirely: it is never a candidate, its own `uses` are not checked, and a binding pointing at it adds no further problem.

Then for every remaining instance in name order, then every need of its catalog entry in capability order (`<i>` is the instance, `C` the capability):

| Situation | Severity, code | Path |
|---|---|---|
| graph `name` breaks the naming rules | error `invalid_name` | `name` |
| instance key breaks the naming rules | error `invalid_name` | `modules.<i>` |
| `module` not in the catalog | error `unknown_module`; its `uses` are not checked | `modules.<i>.module` |
| a `uses` key the catalog entry does not declare | error `unknown_use` | `modules.<i>.uses.<C>` |
| bound to an instance not in the graph | error `unknown_provider` | `modules.<i>.uses.<C>` |
| bound to itself | error `self_binding` | `modules.<i>.uses.<C>` |
| bound to an instance whose entry does not provide `C` | error `capability_not_provided` (nothing if that instance has an unknown module or an invalid name; it already has its own problem) | `modules.<i>.uses.<C>` |
| required need set to `null` | error `unbound_required` | `modules.<i>.uses.<C>` |
| required need missing, 0 candidates | error `unmet_need` | `modules.<i>.uses.<C>` |
| required need missing, 1 candidate | **recommendation**: error `binding_recommended` with that candidate; nothing is written into the graph (0028) | |
| required need missing, 2 or more candidates | error `ambiguous_provider`, `candidates` lists them | `modules.<i>.uses.<C>` |
| optional need missing, 1 or more candidates | warning `optional_unbound`, `candidates` lists them; stays unbound | `modules.<i>.uses.<C>` |
| optional need missing, 0 candidates, or set to `null` | nothing | |

Then, over all valid bindings (explicit plus automatic): a cycle is error `binding_cycle` at path `modules.<i>` of the cycle's first instance in name order, its message naming the instances in order. The request level problems (`invalid_graph`, `unsupported_version`, `wrong_kind`, answered with 400) have path `` (empty) for a body that does not decode, else `apiVersion` or `kind`. One provider may serve any number of consumers. `ok` is true when there is no error; warnings never block.

### API shapes (in `contracts/openapi/renderer.yaml`)

| Endpoint | Request | Response |
|---|---|---|
| `GET /v1/catalog` | none | 200 `{entries: [CatalogEntry]}` sorted by `name` |
| `POST /v1/check` | `Graph` (JSON) | 200 `CheckResult {ok, graph, bindings, problems}`; 400 `{problems}` with one problem (`invalid_graph`, `unsupported_version` or `wrong_kind`) when the body does not decode strictly or names another `apiVersion` or `kind` |
| `POST /v1/render` | `Graph` (JSON) | 200 the zip; 400 as for check; 422 the `CheckResult` when it has an error. Warnings do not block and appear nowhere in the zip |

- Request bodies are decoded with the same strict decoder as files (`sigs.k8s.io/yaml.UnmarshalStrict`; JSON is valid YAML), never plain `encoding/json`, which silently keeps the last of two duplicate keys. So unknown and duplicate keys are rejected on every path.
- `Graph` and `CatalogEntry`: the shapes above, as JSON. `uses` values are `string` or `null`. In Go, `uses` is `map[string]*string`: no key means undecided, a key with `nil` means `null`. If `oapi-codegen` cannot express the OpenAPI 3.1 `type: [string, "null"]`, hand write this one type in `libs/contract`; a test checks that a missing key and `null` decode, check and write back differently.
- `Binding`: `{consumer, capability, provider, auto}`, sorted by consumer then capability.
- `Problem`: `{severity: error | warning, code, path, message, candidates?}`. `path` is dotted from the graph root, like `modules.whoami.uses.postgresql`; `message` is one English sentence the editor shows as is.
- `graph` in a `CheckResult` is the request graph with the defaults the checks fill in and nothing else changed. `bindings` and `problems` are always arrays, never `null`.
- `libs/check` receives the parsed catalog entries and the graph as values; it never reads files.

### Render rules for a binding (`libs/render`)

For each binding consumer `c`, capability `C`, provider `p`:
- `c` gets `depends_on: {p: {condition: service_healthy}}` when `p`'s entry has a `healthcheck`, else `condition: service_started`.
- For each key in `c`'s `uses.C.env`: `host` → `p`'s instance name; `port` → `p`'s `provides.C.port`, written as a string.
- Nothing else crosses a binding: no secret, no shared file. Per binding credentials are #3.

Compose project `name:` is the graph `name`; one service per instance named by its key; no published ports (the sample is reachable only inside its network; publishing is for #3 and #6).

**Secrets in Compose**: for each catalog secret `s` of instance `i`, a top level `secrets: {<i>_<s>: {file: ./secrets/<i>/<s>}}` entry, the service lists `secrets: [<i>_<s>]` (no `target`, so Compose mounts it at `/run/secrets/<i>_<s>`), and its environment sets the declared env var to `/run/secrets/<i>_<s>`. That env var is the only thing that names the secret. "`secrets/<module>/`" in 0001 means this instance folder. `setup.sh` creates a missing file only and never overwrites one (0001).

The Compose secret name can never collide. Compose accepts `^[a-zA-Z0-9._-]+$` as a secret name (the compose-go schema), and both parts are names under the shared naming rules, which never contain `_`. So the first `_` always marks where the instance ends, and every pair `(i, s)` gives its own name. Instance names are unique (map keys) and secret names are unique within one entry (catalog rule `duplicate_name`), so no two secrets in one stack share a name. This is why the renderer needs no problem code for it, and `libs/render` keeps no "two secrets would share the Compose name" check: it could never fire. Runners up: `.` as the separator (same guarantee, but `/run/secrets/postgres.password` reads like a file extension); keeping `-` and adding a check error (you would get an error for secret names a module author chose, fixable only by renaming an instance); a `target` of just `<s>` so the container reads `/run/secrets/password` (stable across instance renames, but one more field per secret for no current need).

What changes in code for this update: in `libs/render/render.go`, the Compose name join becomes `i + "_" + s.Name`, the "two secrets would share the Compose name" check goes, and the `// <instance>-<name>` comment on `secret.Compose` becomes `// <instance>_<name>`. In `libs/render/render_test.go`, the expected strings and, in the golden `libs/render/testdata/whoami-postgres/docker-compose.yml`, the secret key, the service's `source` and the env value move from `postgres-password` to `postgres_password`. The README, `setup.sh` and `.env` golden files name only `secrets/postgres/password` and stay as they are. The render relies on getting only graphs the renderer accepted (instance names already checked); the render endpoint already checks first and answers 422 on any error.

**Render outputs** (the zip, AC-3):

| File | Content | Mode |
|---|---|---|
| `docker-compose.yml` | the rules above, written from compose-go types with keys in sorted order | 0644 |
| `graph.yaml` | the resolved graph, canonical text | 0644 |
| `setup.sh` | per 0001: one secret file per catalog secret of every instance, in graph order | 0755 |
| `.env` | a header comment only for the sample (no instance has non secret settings yet) | 0644 |
| `README.md` | a fixed template in `libs/render`, filled with the graph name, the secret files `setup.sh` creates and the run commands | 0644 |

Zip entries are written in byte order of their path, each with the fixed modification time 1980-01-01 00:00:00 (the zip format's earliest), so the same graph gives a byte identical zip.

**Ready, for AC-4 and the `smoke` job**: a service with a `healthcheck` is ready when Docker reports it `healthy`; a service without one is ready once it is `running`. The smoke job and the zip's README use `docker compose up -d --wait --wait-timeout 120`, which applies exactly this rule and fails if a service exits or stays unhealthy. `traefik/whoami` is built `FROM scratch` with no shell and no healthcheck mode, so its entry has no `healthcheck`, and "the sample stack comes up healthy" means `postgres` healthy and `whoami` running.

### Editor

- **What the scaffold editor does**: show the catalog, open a file, show the graph, check it, save it, download the zip. No drawing or deleting of nodes and edges yet; that is #5 (canvas) and #6 (designing a graph). The working graph at `<base>/` is the last opened file, kept in IndexedDB per 0001.
- **Open and save**: parse with `yaml` `parse(text, {uniqueKeys: true})`, refuse files over 1 MB, check only that `apiVersion` and `kind` match, then send the graph to `POST /v1/check` once, which is the one validator. Save writes the canonical text. Accepted recommendations from that check are written into the editor's graph, so a saved file lists every edge. One check per open (and per download, through render) keeps the editor far below the public rate limit.
- **Problems**: every problem from the resolved graph is listed under the canvas (severity, `message`, the instance it belongs to); an error also marks its node. A 429 shows "the renderer is busy, try again in a minute". A failed catalog or check call shows an error with a retry button.
- **Ports and edges**: each node shows one handle per `provides` capability on its left side (id `provides:<capability>`) and one per `uses` capability on its right side (id `uses:<capability>`), an optional need drawn with a dashed outline. One edge per non null `uses` value, id `<consumer>.<capability>`, from the consumer's `uses:<capability>` handle to the provider's `provides:<capability>` handle, marker at the provider end (the same direction as Compose `depends_on`). Ports come from `GET /v1/catalog`.
- **Auto layout** (no library): an instance's column is the length of the longest chain of consumers leading to it (0 when nobody uses it), so every provider sits right of all its consumers; the walk keeps a visited set so a cycle in an opened bad file cannot loop; rows sorted by instance name; 320 px between columns, 160 px between rows. #5 may replace it with a library.
- **Sample route**: `samples/whoami-postgres/graph.yaml` is imported at build (`?raw`; the Vite dev server's `server.fs.allow` lists only `web/` and `samples/`, never the rest of the checkout). The route `<base>/samples/$name` (under `/platform` this is AC-7's nested deep link `/platform/samples/whoami-postgres`) fetches `GET /v1/catalog`, checks the bundled sample once and shows it read only, with download (calls `POST /v1/render`, AC-3) and save as a file. It never reads or writes browser storage, so a reload is deterministic (AC-7, AC-12). An unknown `$name` shows a "no sample named <name>" screen with a link to `<base>/`.

### Value sourcing

| Action | Value | Source |
|---|---|---|
| Check | the catalog | the embedded `catalog/*.yaml` (see Layout) |
| Check | candidates, recommendations | derived from graph `modules` and catalog `provides`, by the rules above |
| Check | problem `message` | a fixed English template per `code` in `libs/check` |
| Render | Compose project name | graph `name` |
| Render | `image`, `healthcheck` | the catalog entry |
| Render | `DEMO_POSTGRESQL_HOST` | the provider's instance name (`postgres`) |
| Render | `DEMO_POSTGRESQL_PORT` | the provider entry's `provides.postgresql.port` (`5432`) |
| Render | secret file path `secrets/<i>/<s>` | catalog `secrets[*].name` plus instance name |
| Render | Compose secret name and env value `/run/secrets/<i>_<s>` | instance name, `_`, catalog `secrets[*].name` |
| Render | `graph.yaml` in the zip | the resolved graph, canonical text |
| Editor | node positions | the auto layout, computed on every load |
| Editor | edges | non null `uses` values of the current graph |
| Editor | sample graph | the bundled `samples/whoami-postgres/graph.yaml` |

### Layout of the files

- `catalog/` is a tiny Go module (`github.com/racherom/modular-platform/catalog`, listed in `go.work`) holding `whoami.yaml`, `postgres.yaml` and `embed.go` (`//go:embed *.yaml`, exports an `fs.FS`), because `go:embed` cannot reach above a package's own folder.
- `libs/contract` holds the Go types generated from `renderer.yaml` (`oapi-codegen`, types only), the strict YAML and JSON decoding, the canonical writer and the catalog loader with its rules. `services/renderer` imports them.
- `samples/whoami-postgres/graph.yaml` is exactly the `graph.yaml` example above.
- The exact `traefik/whoami` and `postgres` tags are pinned in the catalog files, never `latest`.

### Critical test scenarios

Table tests in `libs/check` over fixed fixtures, one per row (all verify **AC-8**):
- The sample as written: `ok`, one binding `whoami.postgresql → postgres` with `auto: false`, no problems.
- The sample without `uses`: `ok`, warning `optional_unbound` at `modules.whoami.uses.postgresql` with candidates `[postgres]`, no binding.
- The sample with `postgresql: null`: `ok`, no problems, no binding.
- A required need with one candidate: `binding_recommended` with that candidate; `graph` keeps the use left out.
- A required need with no candidate: `unmet_need`. With two: `ambiguous_provider` listing both, sorted.
- `null` on a required need: `unbound_required`. Bound to itself: `self_binding`. Bound to a missing instance: `unknown_provider`. Bound to an instance that does not provide it: `capability_not_provided`.
- Unknown catalog module: one `unknown_module`, nothing more for that instance.
- Two instances requiring each other: `binding_cycle` at the first in name order.
- Instance named `yes`, or `Web`: `invalid_name` at `modules.<i>`, and a binding to `Web` from another instance adds no second problem.
- A required need missing and an optional need set to `null` in the same instance: the first gives a recommendation or an error, the second gives nothing (missing key and `null` differ).
- A body with an unknown key, a duplicate key, or `apiVersion: modular-platform/v1`: 400.

Round trip (**AC-9**): a shared fixtures folder read by both a Go test and an editor unit test, each writing every fixture and comparing to the same expected text. Render (**AC-10**): a golden test on the sample's `docker-compose.yml`, and rendering the sample twice gives byte identical zips. Secret names (inline in `libs/render/render_test.go`): a test catalog of two entries, one declaring secret `c` (env `C_FILE`) and one declaring secret `b-c` (env `BC_FILE`), and a graph with instance `a-b` running the first and instance `a` running the second. It renders without error; the top level `secrets:` keys are exactly `a-b_c` and `a_b-c`; service `a-b` lists only `a-b_c` with `C_FILE=/run/secrets/a-b_c`, and service `a` lists only `a_b-c` with `BC_FILE=/run/secrets/a_b-c`. Guard (in `libs/contract`): `ValidName("a_b")` is false, so widening the name pattern to allow `_` fails a test instead of quietly bringing collisions back. Catalog (**AC-11**): one bad fixture per catalog rule. Editor (**AC-12**): a component test on the sample route, and the `smoke` job's AC-7 check counts one edge marker.

## Consequences

**Positive**:
- The renderer, render, editor and sample share one written down contract, so the scaffold has nothing left to guess.
- The rules never guess a provider, so #6 can add preferences and versions on top without changing an existing result.
- The catalog stays honest: `whoami` runs alone, and its edge shows optional needs, which real modules will have too.
- Positions out of the file means the file changes only when the stack changes.

**Negative / tradeoffs**:
- Everything here is thrown away or migrated when #3 and #4 define `v1`; the renderer will refuse `v0alpha1` then, so any graph files people saved during the scaffold need to be made again (only samples and fixtures exist, so this is cheap now and grows the longer `v1` waits).
- Sorted keys put `name` after `modules`, which reads oddly; the price of one simple rule both writers can match exactly.
- Comments in a `graph.yaml` are lost on the next save.
- No stored positions: your arrangement on the canvas is not kept, which gets worse as graphs grow (#5 may revisit).
- Optional needs never bind by themselves, so you click once per optional edge, and the sample's edge exists only because the file names it. Every graph with an unbound optional need and a matching provider carries an `optional_unbound` warning until you bind it or write `null`, which can feel like noise.
- The env var mapping in `uses.*.env` already pins part of #3's binding format (how an address reaches an unchanged container); #3 must keep or migrate it.
- Two YAML writers must stay byte identical; a library upgrade on either side can break AC-9 (the shared fixtures catch it).
- Compose secret names mix `-` and `_` (`a-b_c`), which reads a little oddly. A stack downloaded before the 2026-10-30 update names its secrets with `-`; downloading it again changes those names in `docker-compose.yml`, but the files in `secrets/<i>/` keep their paths, so the secret values `setup.sh` made carry over. A stack already running from the old download needs `docker compose up -d` after you swap in the new `docker-compose.yml`, so Compose recreates the containers with the new `/run/secrets/` path.
- The rule leans on names never containing `_`. If #3 or #4 ever allow `_` in an instance or secret name, they must pick a new separator or a new scheme.

**Neutral**:
- Problem codes and the `Problem` shape become part of the public renderer contract early; #6 extends the list.

## Follow-up

- [x] (answered by spec [0011](../0011-capability-module-contract/index.md), 2026-11-04: v1 with capability files, versions and ranges, named slots and provides, per binding credentials, provisioners; `uses.<slot>.env` stays, secret names stay collision free because names still never contain `_`) Spec #3 must replace `v0alpha1` catalog entries: capability versions, named need slots (one module using `postgresql` twice), per binding credentials, provisioning, and whether the `uses.*.env` mapping stays. It keeps Compose secret names collision free (the `_` rule above, or a new one if names may contain `_`).
- [ ] Spec #4 must replace the `v0alpha1` graph file and decide whether positions get stored after all, and how a `v0alpha1` file is handled (refuse, or convert once).
- [ ] Spec #6 must extend the matching rules (versions, preferences between several providers, external providers) without changing the results pinned here.
- [ ] Verify at scaffold: the current `traefik/whoami` and `postgres` tags to pin.
- [ ] Specs #5 and #6: drawing, deleting and renaming in the editor, and what each writes into `uses` (deleting an optional edge writes `null`; deleting a node removes the `uses` values that point to it).
- [ ] For `/audit` and `/sync`: record in `AGENTS.md` under `Declined:` the Agent Skills for YAML (`laurigates/claude-plugins@yq-yaml-processing`, `akin-ozer/cc-devops-skills@k8s-yaml-validator`, `akin-ozer/cc-devops-skills@k8s-yaml-generator`) and YAML MCP servers; declined on 2026-10-29, as none covers `sigs.k8s.io/yaml`, the `yaml` npm package or this format.

## References

**Project sources**:
- Spec [0001](index.md): the provisional catalog entry fields, the sample (`traefik/whoami` plus `postgres` with `POSTGRES_PASSWORD_FILE`), principle 4 (provisioning), principle 6 (secrets born on the host), AC-3, AC-4, AC-7.
- Scope features #3, #4, #6 (`docs/scope/scope.md`): the contract, the graph file format and the matching rules this spec stands in for.
- Your answers in this `/architect` session (2026-10-29), every choice listed in Context.

**Practices & standards**:
- Strict decoding of configuration (reject unknown and duplicate keys)
- Canonical serialization for stable diffs
- YAML 1.1 boolean words (`yes`, `no`, `on`, `off`) as a known parsing trap

**Links** (web verified on 2026-10-29):
- [sigs.k8s.io/yaml package docs (Marshal through encoding/json, UnmarshalStrict)](https://pkg.go.dev/sigs.k8s.io/yaml)
- [kubernetes-sigs/yaml README](https://github.com/kubernetes-sigs/yaml)
- [yaml npm package docs (uniqueKeys, sortMapEntries)](https://eemeli.org/yaml/v2/)
- [traefik/whoami README (flags, env vars, /health)](https://github.com/traefik/whoami)
- [traefik/whoami Dockerfile (final stage FROM scratch)](https://github.com/traefik/whoami/blob/master/Dockerfile)
- [Docker Compose project name rules](https://docs.docker.com/compose/how-tos/project-name.md)
- Compose secret name pattern `^[a-zA-Z0-9._-]+$`: `schema/compose-spec.json` in `github.com/compose-spec/compose-go/v2` v2.16.2, the version in `libs/go.mod` (read locally on 2026-10-30)
- [React Flow edge markers example](https://reactflow.dev/examples/edges/markers)
- [React Flow MarkerType reference](https://reactflow.dev/api-reference/types/marker-type)
