# 0027. One vocabulary

**Date**: 2026-11-10
**Status**: In Progress
**Scope**: [#41 One vocabulary](../../scope/scope.md)
**Builds on**: [0001](../0001-platform-stack-architecture/index.md) (the architecture principles and the ubiquitous language), the Words table in the scope (your answers of 2026-11-10)
**Changes**: [0002](../0001-platform-stack-architecture/0002-provisional-graph-format.md) and [0012](../0011-capability-module-contract/0012-capabilities-versions-matching.md): an unbound required use with one match is no longer bound by the checks; see child [0028](0028-recommended-bindings.md)

## Structure

- [0028. Recommended bindings](0028-recommended-bindings.md): the checks recommend a provider for an unbound use and never bind it on their own; the admin accepts in the editor, which writes an ordinary binding; render and apply refuse an open recommendation; supports AC-7, AC-8, AC-9, AC-10, AC-11.

## Summary

Today one word, "planner", names the matching step, the service that also renders, the managed website and the public tool, and the running stack goes by "deployment", "project" and "running graph". This spec makes one name for each thing the same everywhere: Go packages, services, catalog entries, OpenAPI contracts, the database, editor text, specs and the scope. The planner becomes the **renderer** (the service) and **checks** (`libs/check`, the rules a graph must meet), "plan" gives way to the **resolved graph**, and the running stack is a **stack** in every contract. One behaviour changes with it: the checks no longer bind a use on their own; they recommend, and you accept in the editor before anything renders (child 0028). A check in `task lint` keeps the retired words out afterwards. <!-- vocabulary: allow -->

## Requirements

**User stories**:
- As a module author or contributor, I want one name for each thing in code, contracts and specs, so that I never have to guess whether "planner" means the matching rules, the service or the website. <!-- vocabulary: allow -->
- As an admin, I want the running stack called a stack in the editor, the API and the deploy target, so that what I design and what runs share one name.
- As an admin, I want the checks to recommend a provider instead of binding it silently, so that nothing in my stack is wired without my say.
- As a maintainer, I want a lint step that fails on a retired word, so that the old names do not creep back.

**Acceptance criteria**:
- **AC-1**: Spec 0001 has a `## Glossary` section that lists each word of the Rename map's glossary once with what it names, and a "Retired" table that maps each retired name to the word that replaces it.
- **AC-2**: The service is `services/renderer` (Go module `github.com/racherom/modular-platform/services/renderer`, binary `/renderer`), the catalog module and capability are `renderer`, the `api` slot that binds it is `renderer`, its settings are `MP_RENDERER_*`, its image is `ghcr.io/racherom/modular-platform/renderer`, and the online editor routes `<base>/renderer/*` to it. No tracked file path, identifier, config name, env var, image, contract or editor text contains `planner` outside the allowlist. <!-- vocabulary: allow -->
- **AC-3**: `libs/planner` is `libs/check`, whose entry point is `check.Graph(g contract.Graph, catalog contract.Catalog) contract.CheckResult`. The renderer serves `POST /v1/check` and `api` serves `POST /api/v1/check`, both returning `CheckResult` (`ok`, `problems`, `graph` (the resolved graph), `bindings`, `digest`). No identifier, path, field, operation or problem code uses `plan`: `PlanResult` is `CheckResult`, `planDigest` is `digest`, the 409 code `plan_changed` is `graph_changed`, `planner_error` is `renderer_error`, `PlannerError` is `RendererError`. <!-- vocabulary: allow -->
- **AC-4**: The running stack is a stack in every contract: `api` serves `/api/v1/stacks`, `/api/v1/stacks/{name}` and its `operations`, `review`, `apply` and `remove`; the deploy target contract serves `/v1/stacks/{stack}/apply`, `remove`, `state` and `secrets`; the editor routes are `/stacks` and `/stacks/$name`; the deployer's folder setting is `stacks-dir` (default `{install}/stacks`) and the managed label is `io.github.racherom.modular-platform.stack` (its value is the stack id). No contract, `api` code or editor text uses "project" for a stack; the word names Compose's own project only, at the Compose edges (`libs/render` and `services/deployer-docker`), and in the column `stacks.compose_project`.
- **AC-5**: Goose migration `00002` renames table `deployments` to `stacks`; columns `deployments.project` to `compose_project`, `operations.deployment_id` to `stack_id` and `operations.plan_digest` to `digest`; index `operations_deployment_started_idx` to `operations_stack_started_idx` (`ALTER INDEX ... RENAME`); and every constraint that carries the old name, named or automatic (`ALTER TABLE ... RENAME CONSTRAINT`): `deployments_pkey` to `stacks_pkey`, `deployments_name_key` to `stacks_name_key`, `deployments_project_key` to `stacks_compose_project_key`, `deployments_name_check` to `stacks_name_check`, `deployments_latest_operation_fk` to `stacks_latest_operation_fk`, `operations_deployment_id_fkey` to `operations_stack_id_fkey`. The `-- +goose Down` block reverses each one in reverse order. `00001` is not edited. The integration test seeds two stacks with one operation each at `00001`, runs `up`, and checks every row, link and digest is still there; then `down`, and checks the old names. <!-- vocabulary: allow -->
- **AC-6**: Old names answer nothing: `/v1/plan`, `/api/v1/plan`, `/api/v1/deployments...`, `/v1/projects/...` and `<base>/planner/...` return 404; the renderer started with only `MP_PLANNER_*` settings stops at start because the required `MP_RENDERER_MODE` (no default) is missing, and `api` stops because `MP_RENDERER_URL` is missing. Every slice that renames a contract path, setting, folder, label, image or schema lands as a commit with a `BREAKING CHANGE:` footer that names the old and the new name, so release-please puts it in the changelog. The footer of slice 4 also says: move `<install>/deployments` to `<install>/stacks` before starting the new version (Compose project names `mp-<name>` do not change, so running containers keep running), and apply each stack once to refresh its labels. The footer of slice 5 says: a stack reviewed before the upgrade gets `graph_changed` at apply, since the digest no longer covers `auto`; review it again. <!-- vocabulary: allow -->
- **AC-7**: A required use that the graph leaves out and that exactly one provide matches gives the error `binding_recommended` at `modules.<i>.uses.<s>`, with that provide as its only candidate. The checks do not write it into the graph, and `CheckResult.bindings` has no entry for it (child 0028).
- **AC-8**: `POST /v1/render`, `POST /v1/render/managed`, `POST /api/v1/render` and the apply path refuse a graph with an open `binding_recommended`, answering with the `CheckResult` as for any other error. `Binding.auto` is gone from the contract.
- **AC-9**: In both editors, the inspector shows "Recommended: `<provider>`" with an Accept button for each recommended use, and the toolbar shows "Accept all recommendations" while there is at least one. Accept writes an ordinary binding into the graph, the same as picking that provider by hand; Accept all writes every open recommendation in one edit (one undo step). Download and apply stay disabled while a recommendation is open.
- **AC-10**: A use with several matching provides still gives `ambiguous_provider` with every candidate and no recommendation. An optional use with matches still gives the warning `optional_unbound`; the editor offers Accept when it has exactly one candidate, the warning does not block render, and no optional use is ever bound without a click.
- **AC-11**: Every graph the repo ships to be rendered carries every binding explicitly: `samples/*/graph.yaml`, `deploy/managed/graph.yaml`, `libs/render/testdata/*/graph.yaml`, and each `v1` fixture in `contracts/fixtures/graphs/` whose expected result in its test table is `ok` (rule: if a fixture rendered before only thanks to an automatic binding, its binding is written in). The fixtures whose test expects a problem (such as `required-one.yaml`, `required-none.yaml`, `required-two.yaml`, `required-null.yaml`, `required-and-null.yaml`, `ambiguous-provide.yaml`) stay as they are; `required-one.yaml` now expects `binding_recommended`. The render golden files are regenerated with `-update`, and their diff holds only the words of the Rename map, the label key and the rewritten template text.
- **AC-12**: `task lint` runs `task lint:vocabulary` (CI already runs `task lint`), and lefthook runs it before each commit. It fails on any retired word in a tracked file path or file content outside the allowlist, printing file, line, the word and its replacement, and it passes on `main` once #41 is done.
- **AC-13**: Living text uses only the glossary's words: each spec `index.md`, child spec and `verify.md`, every `README.md`, `AGENTS.md` and nested `AGENTS.md`, the scope rows, editor text, contract descriptions, doc comments and log messages. `rationale.md` files, `docs/reviews/` and the changelog keep the words of their time.
- **AC-14**: `task generate` leaves no diff, and `task lint`, `task test`, `task test:integration` and `task smoke` pass.

## Decision

**Chosen option**: Option 2: document, rename in Tracer Bullet slices, and enforce with a retired word check in `task lint` (see [rationale.md](rationale.md)).

One glossary in spec 0001, applied everywhere at once before the first release with a clean break (no aliases), and kept by a small script that fails the lint on a retired word. Automatic binding becomes a recommendation the admin accepts (child 0028).

**Implementation skills**: `golang-naming` · `golang-project-layout` · `golang-lint` · `golang-database` (all `samber/cc-skills-golang`, `.agents/skills/<name>/`) · `openapi-spec-generation` (`wshobson/agents`, `.agents/skills/openapi-spec-generation/`) · `lefthook` (`fandhe-ai/agent-reference-skills`, `.agents/skills/lefthook/`) · `docs` (`minio/skills`, `.agents/skills/docs/`) · `tanstack-router` (`tanstack-skills/tanstack-skills`, `.agents/skills/tanstack-router/`)

## Rationale

Reasoning and options: see [rationale.md](rationale.md).

## Standard definition

### Glossary (goes into spec 0001 as `## Glossary`)

| Word | What it names |
|------|---------------|
| module | a container image with its catalog entry: what it provides, what it uses, its config values |
| capability | a named interface a module provides or uses, with a version |
| provides / uses | what a module offers, and the slots it needs filled |
| binding | one use of one module filled by one provide; always written in the graph |
| recommendation | a provider the checks suggest for an unbound use; never in the graph until you accept it, then it is a binding |
| catalog | the module and capability entries the renderer knows |
| stack | the modules that run together, while you design them and while they run |
| graph | the file that describes a stack: its modules, config values, capabilities and bindings; it may be incomplete |
| resolved graph | a graph after the checks: defaults filled in (provisioner named values, grant sets), with its digest; what the renderer renders |
| checks | the rules a graph must meet, in `libs/check`, run by the editors (through the renderer), the renderer and apply |
| problem | one finding of the checks: a code, a path, a message, candidates |
| renderer | the service (`services/renderer`, with `libs/render`) that checks a graph and turns it into a zip or into the files the deployer applies |
| render | the step that turns a resolved graph into those files |
| zip | the ready to deploy files of a stack, made by the renderer; it carries its graph |
| online editor | the editor before anything runs: open a graph, a zip, a sample or a shared graph, edit it, download a graph or a zip |
| stack editor | the same editor on a running stack in managed mode: health, the edit layer, apply |
| apply | starting the renderer's files on a running stack, module by module |
| deploy target, deployer | the contract that applies files to a host, and the service that implements it (`deployer-docker`) |
| target, fragment, provisioner | unchanged from spec 0001 and 0014 |
| network | only the Docker network that render makes for a binding |

### Retired names

| Retired | Use instead |
|---------|-------------|
| planner (the service, module, capability, slot) | renderer <!-- vocabulary: allow --> |
| planner (the matching step, `libs/planner`) | checks, `libs/check` <!-- vocabulary: allow --> |
| planner website, managed editor | stack editor <!-- vocabulary: allow --> |
| online tool, public planner | online editor for the site, renderer in public mode for the service (the lint hint names both) <!-- vocabulary: allow --> |
| running graph, deployment (a running stack), project (in a contract) | stack <!-- vocabulary: allow --> |
| plan (noun), `PlanResult` | resolved graph, `CheckResult` |
| plan digest, `planDigest` | digest (of the resolved graph) <!-- vocabulary: allow --> |
| to plan, `/v1/plan` | to check, `/v1/check` <!-- vocabulary: allow --> |
| automatic binding, "Let the planner choose" | recommendation, "Accept" <!-- vocabulary: allow --> |

The words "deploy", "deployer", "deploy target", "managed mode" and "static mode" stay. Compose's "project" stays at the Compose edges only: `libs/render` (which builds compose-go types: `ManagedProject`, `ProjectPrefix`, `CheckProject`, the problem code `invalid_project`) and `services/deployer-docker`.

### Rename map

Every rename below lands as written; `/develop` follows it and invents no other name.

| Area | Old | New |
|------|-----|-----|
| Go library | `libs/planner` (all of it: `config.go`, `cycle.go`, `digest.go`, `install.go` stay together, as each is a rule a graph must meet), `planner.go`, `planner_test.go`, `libs/planner/testdata/`, `planner.Plan`, the type `planning`, `planner.DefaultName`, `planner.Code*` | `libs/check`, `check.go`, `check_test.go`, `libs/check/testdata/`, `check.Graph`, `checking`, `check.DefaultName`, `check.Code*` <!-- vocabulary: allow --> |
| Go service | `services/planner` (module path, `cmd/planner`, binary `/planner`, Dockerfile, generated `internal/config/config.gen.go` field `PlannerRate`) | `services/renderer` (`cmd/renderer`, `/renderer`, `RendererRate`) <!-- vocabulary: allow --> |
| `api` packages | `internal/plannerclient`, `ErrPlanner` | `internal/rendererclient`, `ErrRenderer` <!-- vocabulary: allow --> |
| `libs/render` | `ErrPlanHasErrors`, `LabelDeployment` (`...modular-platform.deployment`), `PlatformDeployment`, the `deployment` parameter of `Managed` and `label` | `ErrGraphHasErrors`, `LabelStack` (`...modular-platform.stack`), `PlatformStack`, `stack`; `ManagedProject`, `ProjectPrefix`, `CheckProject`, `invalid_project` stay (Compose's word) <!-- vocabulary: allow --> |
| Local names | Go and TS locals, params and test helpers holding a check result or a stack: `plan`, `pairPlan`, `samplePlan`, `planText`, `PlanReply`, `mockPlanner`, `testDeployment` | `result`, `pairResult`, `sampleResult`, `resultText`, `CheckReply`, `mockRenderer`, `testStack` (same substitution for any other) <!-- vocabulary: allow --> |
| Contract file | `contracts/openapi/planner.yaml`, `libs/contract/planner.gen.go` | `contracts/openapi/renderer.yaml`, `libs/contract/renderer.gen.go` <!-- vocabulary: allow --> |
| Renderer paths | `POST /v1/plan` (op `plan`) | `POST /v1/check` (op `check`) <!-- vocabulary: allow --> |
| Renderer schemas | `PlanResult`, `RenderedProject`, `Binding.auto` | `CheckResult`, `RenderedStack`, removed |
| `api` paths | `/api/v1/plan`, `/api/v1/deployments`, `/api/v1/deployments/{name}`, `.../operations`, `.../review`, `.../apply`, `.../remove` | `/api/v1/check`, `/api/v1/stacks`, `/api/v1/stacks/{name}`, `.../operations`, `.../review`, `.../apply`, `.../remove` <!-- vocabulary: allow --> |
| `api` operations | `plan`, `listDeployments`, `getDeployment`, `reviewDeployment`, `applyDeployment`, `removeDeployment` | `check`, `listStacks`, `getStack`, `reviewStack`, `applyStack`, `removeStack` |
| `api` schemas and fields | `Deployment`, `DeploymentSummary`, `DeploymentPage`, `DeploymentState`, `PlannerError`, `Review.plan`, `ApplyRequest.planDigest`, the list field `deployments`, the `events` query parameter and event name `deployment` | `Stack`, `StackSummary`, `StackPage`, `StackState`, `RendererError`, `Review.check`, `ApplyRequest.digest`, `stacks`, `stack` <!-- vocabulary: allow --> |
| Problem codes | `plan_changed`, `planner_error`, the catalog fixture key `planner_only` (`contracts/fixtures/catalog/sdk-fields.yaml`, struct tag in `sdk/manifest/manifest_test.go`) | `graph_changed`, `renderer_error`, `check_only` <!-- vocabulary: allow --> |
| Deploy target paths | `/v1/projects/{project}/apply`, `remove`, `state`, `secrets` | `/v1/stacks/{stack}/apply`, `remove`, `state`, `secrets` |
| Deploy target operations, schemas and fields | `applyProject`, `removeProject`, `getProjectState`, `getProjectSecrets`, parameter `Project`, `ProjectFile`, `ProjectSecret`, `ProjectState`, `ApplyRequest.deployment`, `Operation.project` | `applyStack`, `removeStack`, `getStackState`, `getStackSecrets`, parameter `Stack`, `StackFile`, `StackSecret`, `StackState`, `ApplyRequest.stack`, `Operation.stack`; the problem code `invalid_project` stays (it checks the Compose project name) <!-- vocabulary: allow --> |
| Deployer folder | config `deployments-dir` (env `MP_DEPLOYER_DEPLOYMENTS_DIR`), default `{install}/deployments`, host mount `{install: deployments}`, log attribute `deployments_dir` | `stacks-dir` (`MP_DEPLOYER_STACKS_DIR`), `{install}/stacks`, `{install: stacks}`, `stacks_dir` <!-- vocabulary: allow --> |
| Catalog | `catalog/modules/planner/`, `catalog/capabilities/planner.yaml`, config `planner-mode`, `planner-rate`, `api` slot `planner`, planner text in `catalog/modules/api` and `catalog/modules/web` | `catalog/modules/renderer/`, `catalog/capabilities/renderer.yaml`, `renderer-mode`, `renderer-rate`, slot `renderer` <!-- vocabulary: allow --> |
| Settings | `MP_PLANNER_MODE`, `MP_PLANNER_RATE`, `MP_PLANNER_CLIENTS_DIR`, `MP_PLANNER_CA_FILE`, `MP_PLANNER_URL`, `MP_PLANNER_TOKEN_FILE`, `MP_PLANNER__*` | `MP_RENDERER_MODE`, `MP_RENDERER_RATE`, `MP_RENDERER_CLIENTS_DIR`, `MP_RENDERER_CA_FILE`, `MP_RENDERER_URL`, `MP_RENDERER_TOKEN_FILE`, `MP_RENDERER__*` <!-- vocabulary: allow --> |
| Image, build and CI | `.../planner` image (and `:dev` from `task images`), `Taskfile.yml` (`GO_PACKAGES`, the `go generate` list, the `images` target, the `web:a11y` text), `go.work`, `release-please-config.json`, `.github/workflows/release.yml` (`service: planner`, `services/planner/Dockerfile`) | `.../renderer` everywhere <!-- vocabulary: allow --> |
| Deploy and scripts | `deploy/online/` (`compose*.yaml`, `versions.env`, `Caddyfile`, `AGENTS.md`), `deploy/managed/` (`compose.ci.yaml`, `compose.dev.yaml`, `graph.yaml`, `README.md`), `scripts/smoke.sh` (`MP_PLANNER_RATE`), `scripts/smoke-managed.sh` (`deployments/`), `catalog/cmd/render-platform` ("the platform graph does not plan") | the renderer names above, `stacks/`, "the platform graph does not pass its checks" <!-- vocabulary: allow --> |
| Ingress | `deploy/online/Caddyfile` matcher `@planner`, `<base>/planner/*`, upstream `planner:8080` | `@renderer`, `<base>/renderer/*`, `renderer:8080` <!-- vocabulary: allow --> |
| Database | see AC-5; sqlc model `Deployment`, fields `Project`, `DeploymentID`, `PlanDigest`, the store methods and the `deploy.Store` interface (`services/api/internal/deploy/deploy.go`) | `Stack`, `ComposeProject`, `StackID`, `Digest`, the same substitution in method names (`CreateDeployment` to `CreateStack` and so on) <!-- vocabulary: allow --> |
| Editor routes and code | `routes/deployments.index.tsx`, `routes/deployments.$name.tsx`, `api/planner.gen.ts`, `PlanResult` exports in `api/`, `a11y/mock-planner.ts` and its routes `**/planner/v1/plan`, `fetchDeployments`, `removeDeployment`, `ctx.plan`, `ctx.planCurrent`, `letPlannerChoose`, query keys `['deployment', …]` | `routes/stacks.index.tsx`, `routes/stacks.$name.tsx`, `api/renderer.gen.ts`, `CheckResult`, `a11y/mock-renderer.ts` and `**/renderer/v1/check`, `fetchStacks`, `removeStack`, `ctx.check`, `ctx.checkCurrent`, `acceptRecommendation`, `acceptAllRecommendations`, `['stack', …]`; `routeTree.gen.ts` is regenerated <!-- vocabulary: allow --> |
| Editor text | "Deployments", "The online tool stores nothing", "Let the planner choose", "Bound by the planner", "The planner is busy" | "Stacks", "The online editor stores nothing", "Accept", (removed), "The renderer is busy" <!-- vocabulary: allow --> |
| Render templates | planner and deployment wording in `libs/render/templates/*.tmpl` (zip and platform `README.md`, `setup.sh` text) | the glossary's words; golden files regenerated with `-update` <!-- vocabulary: allow --> |
| Living docs outside specs | `README.md`, `SECURITY.md`, `sdk/README.md`, `sdk/CONTRACT.md`, doc comments in `sdk/` (`manifest/rules.go`, `cmd/sdkgen`) | the glossary's words |

The editor's `mode` values `online` and `managed` stay: they name the mode, not the editor.

### Canonical pattern

```go
// Package check holds the rules a graph must meet (spec 0027 glossary). It is pure: no I/O.
package check

// Graph runs every check on g against catalog and returns the resolved graph and its problems.
// An unbound required use with one match is a recommendation, never a binding (spec 0028).
func Graph(g contract.Graph, catalog contract.Catalog) contract.CheckResult
```

```yaml
# contracts/openapi/renderer.yaml
paths:
  /v1/check:
    post:
      operationId: check
      responses:
        "200":
          content:
            application/json:
              schema: { $ref: "#/components/schemas/CheckResult" }
```

**Replaces**:
- "planner" for the service, library, module, capability, slot, website or tool <!-- vocabulary: allow -->
- "plan" as a noun, an identifier part (`Plan`, `planDigest`) or a path
- "deployment", "project" or "running graph" for a running stack <!-- vocabulary: allow -->
- automatic binding by the checks

**Enforcement**: `task lint:vocabulary`, part of `task lint` (CI runs `task lint` in `.github/workflows/ci.yml`) and a lefthook pre commit job with no glob and no `{staged_files}` (`run: task lint:vocabulary`), since a rename must be caught wherever it lands. It runs `scripts/vocabulary.sh`, which reads `scripts/vocabulary.txt`: one rule per line, three columns separated by ` | `: a case insensitive extended regex, the path glob it applies to (`*` for all), and the replacement hint. It searches file contents with `git grep -n -i -E` and file paths with `git ls-files -c` (tracked and staged, so a staged rename is seen). The rules:

| Pattern | Applies to | Hint |
|---|---|---|
| `planner` | `*` | renderer (service, module, capability) or checks (`libs/check`) <!-- vocabulary: allow --> |
| `running graph` | `*` | stack <!-- vocabulary: allow --> |
| `online tool` | `*` | online editor (the site) or renderer in public mode (the service) <!-- vocabulary: allow --> |
| `managed editor` | `*` | stack editor <!-- vocabulary: allow --> |
| `\bdeployments?\b` | `*` | stack |
| `\bplan(s\|ning\|ned)?\b` | `*.go`, `*.ts`, `*.tsx`, `*.yaml`, `*.yml`, `*.sql`, `*.sh`, `*.tmpl`, `Taskfile.yml` | check (verb), resolved graph (noun) |
| `Plan[A-Z]\|plan[A-Z]\|[a-z]Plan\b` | same as above | `CheckResult`, `digest` and the Rename map |
| `the plan\b\|plan digest\|/v1/plan` | `*.md` | resolved graph, digest, `/v1/check` <!-- vocabulary: allow --> |

Generated files (`*.gen.go`, `*.gen.ts`, `routeTree.gen.ts`, sqlc output) are scanned: they are clean once their sources are. It skips `docs/specs/**/rationale.md`, `docs/reviews/**`, `CHANGELOG.md`, `.agents/**`, `.claude/**` (contents and paths), lockfiles and sums (`pnpm-lock.yaml`, `go.sum`, `go.work.sum`), `services/api/internal/store/migrations/00001_first_tables.sql` (goose never edits an applied migration), `scripts/vocabulary.txt`, and any line carrying the marker `vocabulary: allow` (a comment in code, `<!-- vocabulary: allow -->` in Markdown). The marker is on: the glossary's Retired table and the scope's Words table, the lines of this spec that name an old word on purpose (its Retired names, Rename map, rule table and acceptance criteria), migration `00002` (it names the old names), the deliberate typo fixture `plannr-rate` in `sdk/sources_test.go`, and English uses in code such as "Plan of the realm" (`sdk/internal/testmodule/capabilities/realm.yaml`) and "plan time" in `libs/contract/template.go`, which are reworded where a plain word fits ("at check time") and marked only where they are a product word. It prints `file:line: "<word>" is retired, use <hint>` and exits 1 on any hit.

**Rollout**: one feature branch, sliced per the Build plan, each slice green on its own. Every slice runs `task generate` and commits the generated output in the same commit. The check reports only until the last slice turns it on.

**Exceptions**: the skipped paths and marked lines above; Compose's "project" at the Compose edges (`libs/render`, `services/deployer-docker`; there is no lint rule for "project", a common English word, so review keeps it out of contracts and `api`); English "plan" in Markdown prose such as "Build plan".

### Value sourcing

| Action | Value | Source |
|---|---|---|
| Glossary in 0001 | each word and its meaning | the table above, from the scope's Words table (2026-11-10) |
| Every rename | the new name | the Rename map above |
| `CheckResult.graph` | the resolved graph | `check.Graph`: the graph plus provisioner named value defaults (`check.DefaultName`) and grant sets (0023) |
| `CheckResult.digest`, `ApplyRequest.digest` | the digest | `sha256` over the canonical resolved graph, its bindings and the catalog entries it uses (today's rule; the values change because `auto` is gone, so the literal hashes in `digest_test.go` and the golden files are regenerated by running the tests with `-update`) |
| Migration `00002` | each renamed table, column, index, constraint | AC-5 |
| Upgrade notes | the folder move, label refresh, review again | AC-6 |
| Lint output | the replacement word | `scripts/vocabulary.txt` |
| Recommendations (AC-7 to AC-10) | the recommended provider and its text | child 0028 |

## Build plan

Tracer Bullet: each slice runs a rename end to end (catalog, Go, contract, editor, deploy, tests), so the stack works after every slice and the next one thickens a thread that already works.

1. **Glossary and the check, report only.** Add `## Glossary` to spec 0001 (both tables, with the allow marker on the Retired table). Write `scripts/vocabulary.sh` and `scripts/vocabulary.txt`, and add `task lint:vocabulary` with a `report` variable: `task lint` calls it as `task lint:vocabulary report=1` for now, which prints the hits and their count and exits 0, so each later slice can watch the count fall. Satisfies **AC-1**, **AC-12** (in part).
2. **planner to renderer.** Every row of the Rename map that names the service, module, capability, slot, settings, image, build and CI, deploy and scripts, ingress, the contract file (`renderer.yaml`, `renderer.gen.go`, `renderer.gen.ts`), `rendererclient`, `RendererError`, `renderer_error`, `check_only`, and the editor strings that say planner. `libs/planner` keeps its name until slice 3; `services/renderer` importing it for one slice is fine. `feat!:` commit with a `BREAKING CHANGE:` footer. Satisfies **AC-2**, **AC-6** (in part). <!-- vocabulary: allow -->
3. **plan to check and resolved graph.** Move `libs/planner` to `libs/check` (file names per the map) with `check.Graph`; rename `/v1/plan` and `/api/v1/plan` to `/check`, `PlanResult` to `CheckResult`, `Review.plan` to `Review.check`, `planDigest` to `digest`, `plan_changed` to `graph_changed`, `ErrPlanHasErrors`, the local names, and the editor's `ctx.plan`. `feat!:` commit. Satisfies **AC-3**, **AC-6** (in part). <!-- vocabulary: allow -->
4. **deployments and projects to stacks.** Rename the `api` paths, operations, schemas and fields, the deploy target paths, operations, schemas and fields, the `deployer-docker` handlers and its folder setting, the managed label in `libs/render`, migration `00002` with its `down` and integration test, the sqlc model and the store, and the editor routes, nav and queries. The migration, the sqlc output and the store code land in one commit. `feat!:` commit with the folder move note. Satisfies **AC-4**, **AC-5**, **AC-6** (in part). <!-- vocabulary: allow -->
5. **Recommendations instead of automatic binding** (child 0028). The `binding_recommended` code, `Binding.auto` removed, render and apply refuse an open recommendation, every shipped graph made explicit, the inspector Accept, the toolbar Accept all, download and apply disabled while one is open. `feat!:` commit. Satisfies **AC-7**, **AC-8**, **AC-9**, **AC-10**, **AC-11**.
6. **Living text and enforcement on.** Rewrite each spec `index.md`, child spec and `verify.md`, the READMEs, `SECURITY.md`, `sdk/CONTRACT.md`, contract descriptions, render templates and doc comments with the glossary's words (titles too, such as 0002's). Run `/sync` for `AGENTS.md` and the nested `AGENTS.md` files (the ubiquitous language line becomes: module, capability, provides, uses, binding, recommendation, graph, resolved graph, catalog, checks, render, renderer, stack, target, fragment, provisioner) and `/scope` for the scope rows. Drop `report=1` from `task lint`, add the lefthook job, and run the full gate. Satisfies **AC-12**, **AC-13**, **AC-14**.

## Consequences

**Positive**:
- One word per thing: a reader of a spec, a contract or a log line knows which component it means.
- Contracts no longer leak Compose's "project", so a Kubernetes target (#33) needs no Compose word.
- Nothing binds without the admin's say, and a graph file alone says how its stack is wired.

**Negative / tradeoffs**:
- A large diff across about 90 files, generated code and golden files; branches open during #41 will conflict and need a rebase.
- Anyone running `main` with a managed install must change `MP_PLANNER_*` settings, move `<install>/deployments` to `<install>/stacks`, let migration `00002` run, apply each stack once (labels), and review again any stack reviewed before the upgrade (digests change); there are no aliases. The old `planner` container goes away on its own, since the platform's `up` uses `--remove-orphans`. <!-- vocabulary: allow -->
- Every sample and hand written graph must now name each binding; a graph that used to render with gaps now stops at `binding_recommended` until accepted.
- "Resolved graph" sits close to "graph"; the glossary and the `CheckResult` field keep them apart, but a careless reader may mix them.
- The vocabulary check can catch an innocent English use (for example "deployment" in a quoted third party message); the `vocabulary: allow` marker handles it line by line.

**Neutral**:
- `rationale.md` files and reviews keep the old words; the glossary's Retired table maps them.
- The editor's `mode` values (`online`, `managed`) keep their names.

## Follow-up

- [ ] Shared and ready graphs (#40): decide whether a ready graph may hold an open recommendation (this spec says render refuses it).
- [ ] Open a zip in the editor (#42): a zip from before #41 carries a graph with gaps; the editor shows recommendations for them as for any graph.
