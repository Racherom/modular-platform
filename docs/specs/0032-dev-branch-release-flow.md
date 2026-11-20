# 0032. Dev branch and release flow

**Date**: 2026-11-15
**Status**: In Progress

## Summary

Work now lands on a long lived `dev` branch, and `main` holds only released code. A release starts when you merge `dev` into `main`; release-please (the tool that picks the version and writes the changelog) then opens its usual release pull request on `main`, and merging that one tags the release and builds the images. A small job carries each release commit back into `dev`, so `dev` never falls behind. Two rulesets (GitHub's branch rules) and two light CI checks keep the flow honest. Nothing in the release pipeline itself moves.

## Context

> ⚠️ Premise note: the scope row says a release is "one merge of `dev` into `main`". With release-please it is two merges: `dev` into `main` starts the release, then the release pull request on `main` finishes it. The second merge is the tag, the images and the zip, and it is the gate that already exists (spec 0030). This spec keeps both and rewords the done line in the scope.

Today every branch merges into `main`, `release.yml` runs on each push to `main`, and release-please reads `main`. Everything unreleased and everything released share one branch, so `main` cannot tell a reader "this is the last release". The platform is at `0.0.0`, releases are off (`RELEASES_ENABLED` unset), and one person maintains it.

Things the flow touches: the CI trigger (`push` on `main` only), `release.yml` (runs on push to `main`, its images job builds `github.sha`), `deploy/online/versions.env` (`MP_SOURCE_REF=main`, the ref the online editor builds from before the first release), and `renovate.json` (opens digest pull requests on the default branch). release-please writes the version into `versions.env`, four catalog entries, `web/package.json` and the manifest, so the release commit changes files that `dev` also edits.

Not deciding now: more than one supported release line, a changelog policy, or what a zip built from `dev` names between releases (spec 0030 covers the `0.0.0` build; after a release, `dev` names the last released version until the next one).

## Requirements

**User stories**:
- As the maintainer, I want feature work to merge into `dev` so that `main` always shows the last release.
- As the maintainer, I want one clear step to start a release and a hotfix path so that a fix for a released version reaches users and `dev` without hand work.
- As the maintainer, I want the rules checked by CI and recorded in the repository so that a wrong merge cannot happen quietly.

**Acceptance criteria**:
- **AC-1**: A `dev` branch exists, created from the tip of `main`; `main` stays the default branch. CI runs on pull requests that target `dev` or `main`, and on pushes to both.
- **AC-2**: The ruleset for `dev` requires a pull request and the CI jobs and the branch rules check, and forbids force push and delete; it allows squash and merge commit. The ruleset for `main` has the same rules and allows merge commit only. No bypass actors, and no "up to date branch" rule on either (it would block the next `dev` into `main` pull request until a back merge exists). The repository setting for squash commits uses the pull request title and a blank body, and rebase merge is off.
- **AC-3**: The branch rules check fails a pull request into `main` unless its head branch is `dev`, `hotfix/*` or `release-please--branches--main`; it also fails a `hotfix/*` pull request into `main` unless its title is a conventional commit; it fails a pull request into `dev` unless the title is a conventional commit (`type(scope)!: text`) or GitHub's `Revert "..."` form; it passes for every other case; and an edited title reruns it.
- **AC-4**: Merging `dev` into `main` (merge commit) makes release-please open the release pull request on `main` listing the `feat` and `fix` commits that came in through `dev`; merging that pull request still tags the release, pushes the four images from the release commit and attaches the managed zip, with `release.yml` unchanged except the explicit `target-branch: main`.
- **AC-5**: After a release is created, a job opens a pull request `back-merge/<version>` into `dev`, titled `chore(release): back merge <version>`, that carries the release commit. It does nothing when `dev` already holds that commit or a pull request for the branch is open; when the branch exists at the same commit with no open pull request it opens the pull request; when the branch exists at another commit it fails with a message. It does not run while `RELEASES_ENABLED` is unset.
- **AC-6**: A fix for a released version goes `hotfix/<name>` from `main` into `main` (passes the branch rules check, conventional title), release-please opens a patch release pull request, and the release's back merge pull request brings the fix into `dev`. AGENTS.md and the README describe this, including: merge or close a pending release pull request before cutting a hotfix, and hotfix commits must be conventional commits.
- **AC-7**: `MP_SOURCE_REF` in `versions.env` is `dev`; Renovate targets `dev` with the base branch key the pinned Renovate version accepts, and its pull request titles are conventional (`semanticCommits` enabled); the Renovate digest test still passes (it creates `dev` in its clone).
- **AC-8**: Both rulesets are in the repository as JSON under `deploy/github/`, and a test fails when a required check name in a ruleset matches no job in `.github/workflows/` (the job `name:`, else its id). The test also fails when a required job is a matrix or reusable workflow job, has a job level `if`, or its workflow does not run on `pull_request` for both bases without `branches` or `paths` filters.
- **AC-9**: The branch rules script has table tests, and `act` runs the branch rules job and the back merge job (the `gh` call stubbed) in local Docker. No check waits for GitHub.
- **AC-10**: AGENTS.md (`## Git`) and the README (Releases) describe the flow, the merge methods, the hotfix path, the back merge conflict procedure and the added first release checklist steps.

## Options considered

### Option 1: release-please on main, back merge into dev

`dev` merges into `main`, release-please runs on `main` as today, and the release commit is merged back into `dev`.

**Pros**:
- `release.yml`, the gate and the image build stay as they are; a hotfix on `main` releases the same way.
- `main` is always exactly what a release or a pending release holds.

**Cons**:
- Two merges per release, and a back merge job to build and keep working.
- A back merge is a merge commit on `dev`, and squashing it by mistake makes the next release conflict.

### Option 2: release-please on dev, main follows

The release pull request merges on `dev`; then `main` is moved up to that commit.

**Pros**:
- No back merge: `dev` is the only place versions change.

**Cons**:
- `release.yml` and its tag logic move to `dev`, and the tag exists before `main` shows the release.
- A hotfix made on `main` has no release path of its own and must be carried to `dev` by hand.

### Option 3: release branches (`release/x.y`)

Each release is cut onto its own branch where fixes land.

**Pros**:
- Supports several released lines at once.

**Cons**:
- Needs a branch, a ruleset and backports per release. There is one released line, so nothing here is needed yet.

### Option 4: stay on main, call the latest tag "the last release"

No `dev`; feature branches merge into `main` as today, and the newest release tag or GitHub Release marks what was released.

**Pros**:
- No second branch, no back merge, no double CI, nothing to keep in step.

**Cons**:
- `main` holds unreleased work, which is exactly what scope row #48 wants to end; a reader of `main` cannot tell the last release from work in progress.
- Rejected because the point of the flow is a branch that shows released code. It stays the fallback if the back merge proves too costly.

## Decision

**Chosen option**: Option 1: release-please on `main`, back merge into `dev`.

**Implementation skills**: `github-actions-templates` (`wshobson/agents`, `.agents/skills/github-actions-templates/`) · `golang-continuous-integration` (`samber/cc-skills-golang`, `.agents/skills/golang-continuous-integration/`)

| Dimension | Decision |
|---|---|
| Branches | `main` (default, last release), `dev` (next release), `feat/*` and other short lived branches from `dev`, `hotfix/*` from `main`, `back-merge/<version>` made by the job |
| Into `dev` | Pull request, squash; the title is the commit message and must be a conventional commit |
| `dev` into `main` | Pull request, merge commit only, never squash (a squash would hide the commits release-please reads and make the branches diverge) |
| Release | Merge `dev` into `main`, then merge the release pull request release-please opens on `main`; tag, images and zip follow as in spec 0030 |
| Back merge | After the release is created, a job opens `back-merge/<version>` into `dev`; merge it with a merge commit |
| Hotfix | `hotfix/*` from `main` into `main`, release-please cuts a patch release, back merge carries it to `dev` |
| Default branch | `main`; Renovate told to use `dev` |
| Protection | Rulesets as AC-2, no approval count (one maintainer), admins do not bypass; to break glass, disable a ruleset for the moment and enable it again |
| Release cadence | Manual: you open the `dev` into `main` pull request when you want a release |

## Rationale

The release pipeline (gate, `RELEASES_ENABLED`, images from the release commit, the zip) was built and tested in spec 0030 around pushes to `main`. Option 1 reuses all of it and only adds the front (`dev`) and the back (the back merge); Option 2 would move and retest the pipeline for the sake of saving one job. A hotfix is the deciding case: on Option 1 it uses the normal release path, on Option 2 it needs a second path. Release branches (Option 3) solve a many lines problem this project does not have.

Squash into `dev` gives the changelog one line per feature and makes the pull request title count, which is why it is checked. The cost is that the per milestone commits from a branch are not kept on `dev`. `dev` into `main` must be a merge commit because release-please reads the commits between releases, and one squashed commit would hide them. The rulesets do not require an up to date branch: every `dev` into `main` merge leaves a merge commit on `main` that `dev` lacks, and the back merge runs only after a release, so that rule would block the next release pull request with an "Update branch" push that the `dev` ruleset refuses. A forgotten back merge stays harmless for a while (the next merge is a normal three way merge), and the back merge pull request stays open as a visible reminder.

The main guard allows `release-please--branches--main` because release-please's own pull request would otherwise be blocked by the rule that protects it. The title check runs in its own light workflow because the main CI does not rerun when a title is edited, and rerunning all of it for a title edit is waste.

## Feature design

**Data model**: none. **API surface**: none.

**Added and changed files**

| Path | Change |
|---|---|
| `.github/workflows/ci.yml` | `push` on `main` and `dev` |
| `.github/workflows/branch-rules.yml` | new; `pull_request` types `opened, edited, synchronize, reopened`; one job, `Branch rules`, runs `scripts/branch-rules.sh` |
| `scripts/branch-rules.sh` | new; arguments base ref, head ref, title; exit 0 or 1 with a one line reason |
| `.github/workflows/release.yml` | `target-branch: main` on the release-please step; new job `back-merge` (`needs: release-please`, `if` created, same token gate as above) |
| `scripts/release-back-merge.sh` | new; arguments version and sha; no checkout and no git, only `${GH:-gh}` with `GH_TOKEN` set to the release token: compare `dev...sha` (status `behind` or `identical` means `dev` holds it, stop), create the ref `back-merge/<version>` at sha, list pull requests by that head, create the pull request into `dev`; idempotency cases as AC-5 |
| `renovate.json` | also `semanticCommits: "enabled"` |
| `deploy/github/ruleset-main.json`, `ruleset-dev.json` | new; importable rulesets |
| `deploy/online/versions.env` | `MP_SOURCE_REF=dev` |
| `renovate.json` | base branch `dev` (the key name the pinned Renovate accepts) |
| `libs/ci/` | a test that reads the rulesets and the workflow job names (AC-8), act tests for the new jobs beside `TestReleaseGateAct` (AC-9) |
| `AGENTS.md`, `README.md` | `## Git` and Releases text (AC-10) |

**Rulesets**

- Both: branch deletion blocked (this also stops "delete branch after merge" from touching `dev` when it is a head), force push blocked, pull request required with zero approvals, required checks (the bare job names, pinned to the GitHub Actions app so another app cannot report them), no bypass actors, no up to date rule.
- `dev`: merge methods squash and merge. `main`: merge only.
- Required checks, both: the job names `Generate, lint, test, build`, `Integration (Docker, sdk end to end)`, `Smoke (online editor, sample zip, sub path)`, `Smoke (managed platform, install to running stack)` and `Branch rules`.

**Branch rules logic** (`branch-rules.sh BASE HEAD TITLE`)

| Base | Rule |
|---|---|
| `main` | HEAD is `dev`, `hotfix/*` or `release-please--branches--main`, else fail; for `hotfix/*` TITLE must also match the conventional pattern below |
| `dev` | TITLE matches `^(feat\|fix\|perf\|revert\|docs\|style\|refactor\|test\|build\|ci\|chore)(\([a-z0-9._/-]+\))?!?: .+` or `^Revert ".+"$`, else fail |
| other | pass |

**Value sourcing**

| Action | Value | Source |
|---|---|---|
| back merge job | the version | `needs.release-please.outputs.version` (spec 0030) |
| back merge job | the commit to carry | `github.sha` of the push to `main`, the release commit (release-please finds the release through the merged pull request, so with merge commits the tag lands on this same commit) |
| back merge job | branch name and title | `back-merge/` plus the version; `chore(release): back merge ` plus the version |
| back merge job | base branch | the fixed word `dev` in the script |
| back merge job | token | `secrets.RELEASE_PLEASE_TOKEN`, already required by the release gate |
| branch rules | base, head, title | `github.base_ref`, `github.head_ref`, `github.event.pull_request.title`, passed through `env`, never expanded in the script text |

**Key invariants**
- `main` only changes by a merge of `dev`, a `hotfix/*`, or the release pull request.
- Every release commit reaches `dev` through a merge commit, never a squash.
- The workflows that use `RELEASE_PLEASE_TOKEN` stay behind the `RELEASES_ENABLED` gate.

**Security model**: the title and branch names are untrusted input, so the job reads them from `env` and quotes them. The back merge job has `contents` and `pull_requests` write only through the existing token; the branch rules job needs `contents: read`. No new secret.

**Critical test scenarios**
- Branch rules: `feat/x` into `main` fails; `dev` into `main` passes; `release-please--branches--main` into `main` passes; a title `Update stuff` into `dev` fails; `feat(web): show failed check` passes; a title with `$(...)` or a quote does not run, verifies **AC-3**
- Branch rules: a `hotfix/x` pull request into `main` with a non conventional title fails; a Renovate title (`chore(deps): ...`), the back merge title and `Revert "feat: x"` into `dev` pass, verifies **AC-3**, **AC-7**
- act run of `release.yml` with a stub release-please (`created=true` on the release SHA) and `--env GH=<stub>`, `--var RELEASES_ENABLED=true`, `--secret RELEASE_PLEASE_TOKEN=x`: the back merge job calls the stubbed pull request creation once and the stub logs to a file in the workspace; a second run reads that log and does nothing; the branch present at another commit fails; created false or the gate off skips it, verifies **AC-5**
- Ruleset test: renaming a job in a workflow without editing the ruleset fails the test, verifies **AC-8**
- Renovate dry run still groups digests and targets `dev`, verifies **AC-7**

## Build plan

Approach: Tracer Bullet. The thread is one pull request through the guard into `dev`, then a release through the back merge; the first tasks build that thread, the rest thicken it.

1. `scripts/branch-rules.sh` with table tests, the `branch-rules.yml` workflow, and an act test, satisfies **AC-3**, **AC-9**
2. `ci.yml` pushes on `dev`; `release.yml` gets `target-branch: main`, satisfies **AC-1**, **AC-4**
3. `scripts/release-back-merge.sh` with tests (stubbed `gh` and `git`), the `back-merge` job, and an act test, satisfies **AC-5**, **AC-9**
4. The two ruleset JSON files and the test that matches required checks to job names, satisfies **AC-2**, **AC-8**
5. `MP_SOURCE_REF=dev` and Renovate `baseBranches`, satisfies **AC-7**
6. AGENTS.md `## Git` and the README Releases text, including the hotfix path and the first release checklist additions, satisfies **AC-6**, **AC-10**
7. Cut over (see Migration plan): create `dev`, apply the rulesets, retarget open pull requests, satisfies **AC-1**, **AC-2**

## Migration plan

**Strategy**: no big bang; the workflow changes ship through today's flow first, then the branch is cut.

**Phases**:
1. Tasks 1 to 6 merge into `main` the way work merges today.
2. Create `dev` at the tip of `main` and push it.
3. Import the two rulesets in GitHub, set the allowed merge methods, turn rebase merge off, set the squash commit title to the pull request title with a blank body, turn on delete branch after merge, keep `main` as the default.
4. Retarget the open feature pull requests to `dev`; new work branches from `dev`.
5. Add to the first release checklist (README, Releases): the rulesets are in place, the release token has Contents and Pull requests write, and the release-please dry runs in Follow-up are done. Note that the first release reads the whole history (about 129 commits, manifest `0.0.0`), so its changelog is long and the version is 0.1.0; accept this or set `bootstrap-sha` then.

**Rollback**: disable the rulesets, merge `dev` into `main`, and delete `dev`; `release.yml` still works on `main` alone because the back merge job only reacts to a created release.

**Risks**: a squashed back merge; a ruleset that requires a check that never reports (guarded by AC-8); release-please reading the merge commit history differently than expected (Follow-up).

## Consequences

**Positive**:
- `main` shows the last release; `dev` shows what the next one holds.
- The changelog gets one line per feature, and a bad title is caught before it is lost.
- The release pipeline from spec 0030 stays unchanged and its local tests stay valid.

**Negative / tradeoffs**:
- Two merges per release, plus a back merge to approve each time.
- The per milestone commits of a branch are squashed on `dev`; the pull request description is the record.
- `dev` allows merge commits (for the back merge), so a feature pull request could be merged that way by mistake; only a note in AGENTS.md and the pull request body prevent it.
- With one maintainer there is no review, so the rulesets guard mistakes, not trust. Without an up to date rule, a pull request can pass CI on an older base.
- A back merge conflict shows only in its pull request. Resolve it by merging `dev` into `back-merge/<version>` locally and keeping the release side for version lines (`MP_VERSION`, the four catalog image lines, `web/package.json`, the manifest, `CHANGELOG.md`).
- A hotfix cut while a release pull request is pending ships the unreleased `dev` code in `main`; merge or close the release pull request first.
- The required checks list in the rulesets must follow every renamed CI job; the test makes that loud, not automatic.

**Neutral**:
- Between releases `dev` names the last released version, so a zip rendered from `dev` points at released images; use the checkout builds (`task dev`, `task online:up`) to try newer code.
- After the first release `MP_SOURCE_REF` is unused.

## Follow-up

- [ ] Before the first release, run release-please in dry run mode against a real `dev` into `main` merge and confirm the release pull request lists the `feat` and `fix` commits that came in through `dev`. Include the hard case: a commit squashed into `dev` while a release pull request is open, dated before the release commit and merged after it. If either is missed, add the rule "no merges into `dev` while a release pull request is open" or change this spec before `RELEASES_ENABLED` is set.
- [ ] On the first release pull request, confirm that CI passes on it (the version bump may change generated or golden files), and note that "Update branch" may be needed if release-please leaves it behind.
- [ ] Scope row #48: reword "a release is one merge of `dev` into `main`" to two merges (`/scope` owns that file).
- [ ] Decide what a zip from `dev` should name between releases once the first release exists (it points at the last release's images); this may need its own spec with #46's follow ups.
