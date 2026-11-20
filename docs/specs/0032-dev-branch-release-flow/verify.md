# Verify: Dev branch and release flow · spec 0032 · updated 2026-11-20
_Steps derived from spec 0032 acceptance criteria and its Value sourcing table. `/check verify` runs these; `/test` locks the durable ones._

Steps marked **(on GitHub)** need the cut over (task 7) and a real repository: `dev` created, the rulesets imported. Steps marked **(at release)** belong to the first release checklist (README, Releases) and stay open until then. Everything else runs on your machine. The `act` steps need Docker and have build tag `integration` (`task test:integration`).

## UI / manual

- [ ] **(on GitHub)** Open a pull request from `feat/x` into `main`: the *Branch rules* check fails with "main takes only dev, hotfix/* or the release pull request" → AC-3
- [ ] **(on GitHub)** Open a pull request from `feat/x` into `dev` titled `Update stuff`: *Branch rules* fails; edit the title to `feat(web): x`: the check reruns and passes, and the other CI jobs do not rerun → AC-3
- [ ] **(on GitHub)** On the `dev` pull request, the merge button offers squash and merge commit only (no rebase), and the squash message is the title with a blank body → AC-2
- [ ] **(on GitHub)** On a pull request into `main`, only merge commit is offered; a direct push to `main` or `dev`, a force push and a branch delete are refused, also for you (no bypass) → AC-2
- [ ] **(on GitHub)** Create `dev` from the tip of `main`; `main` is still the default branch; CI runs on the push to `dev` and on a pull request into `dev` → AC-1
- [ ] **(at release)** Merge `dev` into `main` (merge commit): release-please opens a release pull request listing the `feat` and `fix` commits that came in through `dev`, including one squashed into `dev` while a release pull request was open → AC-4
- [ ] **(at release)** Merge the release pull request: the tag, the four images and the managed zip appear as in spec 0030, then a pull request `back-merge/<version>` titled `chore(release): back merge <version>` opens into `dev`; run the job again: no second pull request → AC-4, AC-5
- [ ] **(at release)** Cut `hotfix/x` from `main` with a conventional commit and open it into `main`: it passes *Branch rules*; release-please opens a patch release pull request; after the release the back merge brings the fix into `dev` → AC-6
- [ ] Read `AGENTS.md` (`## Git`) and the README (*Branches and releases*, *Releases*): both name the merge methods, the hotfix path, the "merge or close a pending release pull request first" rule, the back merge conflict procedure and the added first release steps → AC-6, AC-10

## Commands

- [ ] `go test -race ./libs/ci/ -run 'BranchRules|BackMerge|Flow|Ruleset|RequiredChecks'` → passes: the table tests for both scripts, the trigger and config checks, and the ruleset against job name check → AC-1, AC-2, AC-3, AC-4, AC-5, AC-7, AC-8
- [ ] `go test -race -tags integration ./libs/ci/ -run 'TestBranchRulesAct|TestBackMergeAct|TestReleaseGateAct'` → passes: `act` runs the branch rules job and the back merge job with `gh` stubbed, no GitHub call → AC-5, AC-9
- [ ] Rename the `Branch rules` job in `branch-rules.yml` without touching the rulesets, then `go test ./libs/ci/ -run RequiredChecks` → fails and names the check; put the name back → AC-8
- [ ] `go test -race -tags integration ./libs/ci/ -run TestRenovateDigests` → passes with `dev` created in its clone; `task lint` accepts `renovate.json` with `baseBranchPatterns` → AC-7
- [ ] `sh scripts/branch-rules.sh main feat/x 'feat: x'` → exit 1 with one `::error::` line; `sh scripts/branch-rules.sh dev feat/x 'feat(web): x'` → exit 0 → AC-3
- [ ] `grep -n MP_SOURCE_REF deploy/online/versions.env` → `MP_SOURCE_REF=dev` → AC-7
- [ ] `task lint && task test` → green → AC-1 to AC-10

## Value sourcing (one step per row of the spec's table)

- [ ] Back merge version: run `go test ./libs/ci/ -run TestBackMergeScript` → the branch is `back-merge/<version>` and the title `chore(release): back merge <version>` for the version passed in; a version like `1.2.3; touch x` is refused and runs nothing → AC-5
- [ ] Back merge commit: the same test with `compare-status` `behind` and `identical` → no pull request; `diverged` or `ahead` → one pull request → AC-5
- [ ] Back merge base branch: every `pr create` call in the act log carries `--base dev` → AC-5
- [ ] Back merge token: `grep -n RELEASE_PLEASE_TOKEN .github/workflows/release.yml` → the `back-merge` job passes it as `GH_TOKEN`, and the job's permissions stay `contents: read` → AC-5
- [ ] Branch rules inputs: the `Branch rules` step passes base, head and title through `env` and quotes them; a title with `$(...)`, backticks or a newline is data (the table test and `TestBranchRulesAct`) → AC-3

## Acceptance-criteria coverage
- AC-1 … branch and CI steps, `TestFlowTriggers` · AC-2 … ruleset steps, `TestRulesets` · AC-3 … branch rules steps, `TestBranchRulesScript` · AC-4 … release steps, `TestFlowTriggers` · AC-5 … back merge steps, `TestBackMergeScript`, `TestBackMergeAct` · AC-6 … hotfix step, docs step · AC-7 … `TestFlowConfig`, `TestRenovateDigests` · AC-8 … `TestRequiredChecksMatchJobs` · AC-9 … both act tests · AC-10 … docs step
