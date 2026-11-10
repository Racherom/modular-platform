# Verify: One vocabulary · spec 0027 · updated 2026-11-10

_Steps derived from spec 0027 and child 0028 acceptance criteria. `/check verify` runs these; `/test` locks the durable ones._

## UI / manual

- [ ] Open a graph with two `test-app` instances and one `postgres` instance, none bound → Download zip (and Deploy in managed mode) is disabled, and the reason "Accept or change 2 recommended bindings first" sits beside it and is announced through `aria-describedby` → AC-9
- [ ] Select one `test-app` instance → the inspector shows "Recommended: `<provider>`" and a button named "Accept `<provider>` for postgresql"; press it → the binding field shows the provider and has focus, and the next check call carries the binding in the graph → AC-9
- [ ] Press "Accept all recommendations (1)" in the toolbar → the other instance is bound, focus moves to the File toolbar's first control, Download is enabled; press Undo once → that binding is gone again and the button is back → AC-9
- [ ] Accept all after deleting a recommended provider while the check is stale → the button does not show while the check runs; once current, a recommendation whose provider is gone is skipped and the status line says how many → AC-9
- [ ] Open a graph where a required use has two providers → `ambiguous_provider` lists both, and no Accept shows → AC-10
- [ ] Open a graph where an optional use has exactly one provider → the warning `optional_unbound` shows with an Accept in the inspector, Download stays enabled, and nothing binds until you press Accept → AC-10
- [ ] Open the binding list of a left out use → it offers "Choose a provider" (disabled), the candidates and "Not bound", and no option to let the checks choose → AC-9
- [ ] In managed mode, the navigation says "Stacks" and the list is at `/stacks`, a stack at `/stacks/<name>` → AC-4
- [ ] The online editor's empty stack page says "The online editor stores nothing" → AC-2

## Commands

- [ ] `task lint:vocabulary report=1` → lists only Markdown in specs, the scope and `AGENTS.md` files until slice 6 lands; then `task lint:vocabulary` → "vocabulary: no retired words", exit 0 → AC-12
- [ ] Add the word "planner" to any tracked file and run `sh scripts/vocabulary.sh` → it prints `file:line: "planner" is retired, use renderer ...` and exits 1; stage a file renamed to `deployments.md` → the path is reported too <!-- vocabulary: allow --> → AC-12
- [ ] `grep -n '## Glossary' -A40 docs/specs/0001-platform-stack-architecture/index.md` → each glossary word once, and a Retired table mapping each retired name → AC-1
- [ ] Start the renderer with only `MP_PLANNER_MODE` set → it stops at start because `MP_RENDERER_MODE` is missing; start `api` without `MP_RENDERER_URL` → it stops too <!-- vocabulary: allow --> → AC-6
- [ ] `POST /v1/plan` on the renderer, `POST /api/v1/plan`, `GET /api/v1/deployments` on `api`, `POST /v1/projects/mp-x/apply` on the deployer → 404 each <!-- vocabulary: allow --> → AC-6
- [ ] `POST <base>/planner/v1/check` through the online editor's ingress → 404 (it falls through to the web module now; check it is not the app shell or 405) <!-- vocabulary: allow --> → AC-6
- [ ] `POST /v1/check` with `contracts/fixtures/graphs/required-one.yaml` → 200, `ok` false, one `binding_recommended` at `modules.app.uses.db` with candidates `["db"]`, `bindings` empty, and the graph has no `uses.db` → AC-7, AC-3
- [ ] `POST /v1/render`, `POST /v1/render/managed` and `POST /api/v1/render` with that graph → 422 with the `CheckResult` → AC-8
- [ ] `go test -race ./libs/check/ -run 'TestCheck|TestRecommendationIsNotABinding|TestCheckV0alpha1'` → ok → AC-7, AC-10, AC-11
- [ ] `go test -race -tags integration ./services/api/internal/store/ -run TestMigrationStacks` → ok: two stacks seeded at `00001` keep every row, link and digest through `up`, and `down` brings back the old names → AC-5
- [ ] `go test -race ./libs/render/ -update && git diff --stat libs/render/testdata` → no change → AC-11
- [ ] `git log --format=%B main..HEAD | grep -c 'BREAKING CHANGE:'` → one per renaming slice; the stack slice names the folder move to `<install>/stacks` and the label refresh, the recommendation slice says to review again after `graph_changed` → AC-6
- [ ] `task generate && git status --short` → no diff; `task lint`, `task test`, `task test:integration`, `task smoke` and `task smoke:managed` → pass → AC-14

## Value sourcing

- [ ] Digest: review a stack, then change only a config value and apply with the old digest → `graph_changed` (the digest covers the resolved graph, its bindings and the catalog entries it uses) → Value sourcing row "digest"
- [ ] Resolved graph: check a graph whose binding to a provisioner leaves out `database` and `user` → `CheckResult.graph` carries the `check.DefaultName` values, while a left out use stays out → Value sourcing row "resolved graph"
- [ ] Migration: the column `stacks.compose_project` still reads `mp-<name>` for every row after `up` → Value sourcing row "Migration 00002"

## Acceptance-criteria coverage

- AC-1: glossary step · AC-2: renderer start, online editor text, check script · AC-3: required-one check step · AC-4: managed mode navigation step, the 404 step · AC-5: migration test · AC-6: start refusals, 404s, commit footers · AC-7: required-one step and Go test · AC-8: render refusal step · AC-9: the UI steps · AC-10: ambiguous and optional steps · AC-11: golden diff and Go tests · AC-12: the two check script steps · AC-13: covered once slice 6 lands (the Markdown rewrite, `/sync`, `/scope`) · AC-14: the full gate step
