# 0027. One vocabulary: rationale

## Context

The project's rules ask for one ubiquitous language (Domain Driven Design: one name per concept, shared by code, contracts and talk), and `AGENTS.md` lists those words. In practice "planner" grew to name four things: the matching rules (`libs/planner`), the service that also checks and renders (`services/planner`), the managed website and the public tool. "Plan" names an endpoint, a result type, a digest and a place in the resolved graph (0023). The running stack is a "deployment" in `api.yaml` and the editor, a "project" in the deploy target contract, and a "running graph" in specs. A reader of a spec, a log line or a contract has to guess which one is meant, and `/develop` copies whatever word the nearest spec used.

Your Words table of 2026-11-10 in the scope settled the nouns (stack, graph, zip, online editor, stack editor, renderer, apply, checks) and retired planner, planner website, managed editor, public planner and running graph. What stayed open: what `libs/planner` and `services/planner` become, whether "plan" stays, where binding a need automatically lives, how renamed contract paths stay compatible, what happens to old specs, and how the words stay put afterwards.

Forces: nothing is released (`.release-please-manifest.json` at `0.0.0`, no tags), so no outside caller depends on a path yet; managed installs exist only on contributors' machines. The rename touches about 90 files with "planner", 600 matches of "deployment", the generated Go and TypeScript, the render golden files, the api database, and the catalog the platform renders itself from. The project builds in Tracer Bullet slices, so each step must leave a working stack. If nothing is decided, every new spec (#40, #42, #43 are already queued) adds text in the old words, and the cost grows with each one.

## Options considered

### Option 1: Document and enforce going forward

Write the glossary, add the lint check for new lines only, and treat existing uses as debt to fix as files are touched.

**Pros**:
- Smallest change now; no large diff, no conflicts with open branches.

**Cons**:
- The contracts, env vars and database keep the old names until someone touches them, which after the first release means a breaking change anyway.
- `/develop` keeps reading old words in specs and code for months, so the debt grows instead of shrinking.

### Option 2: Document, rename in slices, enforce (chosen)

Write the glossary, rename everything in a few end to end slices on one branch before the first release, with a clean break and a changelog note, then turn the check on.

**Pros**:
- One coordinated break while nothing is released, the cheapest moment there will be.
- Each slice keeps the stack working (Tracer Bullet), and the check's count shows progress.
- After it lands, the check keeps the words put without anyone remembering.

**Cons**:
- A large diff; open branches must rebase.
- Contributors with a local managed install must change settings and migrate.

### Option 3: Document only

Write the glossary in spec 0001 and `AGENTS.md`, and rely on `/check review`.

**Pros**:
- No tooling to maintain.

**Cons**:
- The words drift back quietly; review catches some, not all.
- The code and contracts keep the old names.

## Rationale

Option 2, because the forces line up for it now and against it later. With version `0.0.0` and no tags, a clean break costs a changelog note; after the first release the same renames need aliases or a major version. Slicing by strand (service name, plan, stack, recommendations, text) keeps each step a working end to end thread and keeps each `BREAKING CHANGE` note focused on one rename. The check costs one short script with `git grep` (no new tool to pin), runs where `task lint` already runs, and is the only thing that keeps a vocabulary true once the people who chose it stop watching.

The choices inside it, each yours:
- **Service name `renderer`** over splitting it into `renderer` and `checks` services (one more image and binding for no runtime gain) or `render` (a verb next to `api` and `deployer-docker`).
- **Library `libs/check`** over a separate `libs/bind` (bind and check share the candidate search, so one would import the other) or merging into `libs/render` (render would own rules that the editors and apply also run).
- **"Plan" retired for "resolved graph"**, over keeping "plan" as a noun. I recommended keeping it, since it is short and 0023 already says "the grant set in the plan"; you chose one word fewer. The glossary pins "resolved graph" to the `graph` field of `CheckResult` so it does not blur with the graph file, and AC-13 rewrites 0023's wording.
- **Recommendations instead of automatic binding** (child 0028), accepted into an ordinary binding, never stored as a recommendation. I had recommended keeping automatic binding unchanged as a pure rename; you chose to make the graph file the whole truth. That is a behaviour change inside a rename feature, so it has its own child spec and its own slice.
- **Stacks in both contracts**, keeping Compose's "project" inside the Docker adapter where compose-go types already live.
- **Clean break** over aliases, since no released caller exists.
- **Rewrite living text, keep records**: specs that `/develop` reads get the new words; `rationale.md`, reviews and the changelog stay as written at the time, and the glossary's Retired table maps the two.
- **A retired word check in `task lint`** over a prose linter (one more tool for a word list) or review only (drift returns).

A read only review on another model found 25 issues; you chose to apply every recommended fix. The ones that changed the design: the deployer's folder becomes `<install>/stacks` with a move note (Compose project names stay, so running containers survive); the managed label becomes `...stack`; Compose's "project" stays at the Compose edges (`libs/render`, `deployer-docker`) and in the column `compose_project`, with no lint rule for it; the fixtures that show a gap stay as inputs; `v0alpha1` conversion adds no bindings; Accept all skips a recommendation whose candidate is gone; and digests change, so a stack reviewed before the upgrade is reviewed again. The rest filled the rename map (the database's automatic constraint names, contract fields, `libs/render` names, build, CI and deploy files) and pinned the lint's rule file, skips and markers.

## Inventory (2026-11-10)

Where the retired words live today, to size each slice. Counts are from `grep` over `libs`, `services`, `web/src`, `contracts`, `catalog`, `sdk`, `deploy` and `docs`.

| Word | Matches | Main places |
|------|---------|-------------|
| `planner` (files) | about 90 files | `services/api/internal` (17), specs 0001, 0005, 0006, 0010, 0011, 0016, 0017, `libs/render/testdata`, `services/planner`, `web/src` (graph, components, api, test), `catalog/cmd/sdkgen`, `Taskfile.yml`, `release-please-config.json`, `scripts/smoke.sh`, `.github/workflows/release.yml` |
| `MP_PLANNER_*` | about 90 | `MP_PLANNER_CLIENTS_DIR` (24), `MP_PLANNER_MODE` (23), `MP_PLANNER_RATE` (22), `MP_PLANNER_TOKEN_FILE` (10), `MP_PLANNER_URL` (5), `MP_PLANNER_CA_FILE` (3), `MP_PLANNER__*` |
| `Plan*` identifiers in Go | about 300 | `Plan` (98), `PlanResult` (66), `PlanDigest` (26), `Planner` (25), `PlanResponse` (17), `ErrPlanner` (10), `ErrPlanHasErrors` (8) |
| "the plan" in specs | 56 | mostly 0011, 0017 (0023 grant sets), 0006 |
| `deployment(s)` | about 600 | `api.yaml` paths and schemas, `services/api` (store, deploy, api), migration `00001`, `web/src/routes/deployments.*`, specs 0006, 0007, 0017 |
| `project` in contracts | 5 paths, 6 schemas | `deploy-target.yaml`; `RenderedProject` in `planner.yaml` |
| `online tool` | 83 | specs 0001, 0004, 0006, `deploy/online/AGENTS.md`, `deploy/online/Caddyfile`, `web/src/routes/deployments.index.tsx`, `web/src/lib/config.ts` |
| `public planner` / `planner website` / `managed editor` / `running graph` | 16 / 4 / 3 / 10 | specs and the scope |
| Automatic binding | 1 code path | `libs/planner/planner.go` (`undecided`, the `len(matches) == 1` case), `Binding.auto` in the contract, `letPlannerChoose` and the `auto` note in `web/src/components/Inspector.tsx` |
