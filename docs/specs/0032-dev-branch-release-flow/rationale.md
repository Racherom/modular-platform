# 0032. Dev branch and release flow: rationale

## Context

> ⚠️ Premise note: the scope row says a release is "one merge of `dev` into `main`". With release-please it is two merges: `dev` into `main` starts the release, then the release pull request on `main` finishes it. The second merge is the tag, the images and the zip, and it is the gate that already exists (spec 0030). This spec keeps both and rewords the done line in the scope.

Today every branch merges into `main`, `release.yml` runs on each push to `main`, and release-please reads `main`. Everything unreleased and everything released share one branch, so `main` cannot tell a reader "this is the last release". The platform is at `0.0.0`, releases are off (`RELEASES_ENABLED` unset), and one person maintains it.

Things the flow touches: the CI trigger (`push` on `main` only), `release.yml` (runs on push to `main`, its images job builds `github.sha`), `deploy/online/versions.env` (`MP_SOURCE_REF=main`, the ref the online editor builds from before the first release), and `renovate.json` (opens digest pull requests on the default branch). release-please writes the version into `versions.env`, four catalog entries, `web/package.json` and the manifest, so the release commit changes files that `dev` also edits.

Not deciding now: more than one supported release line, a changelog policy, or what a zip built from `dev` names between releases (spec 0030 covers the `0.0.0` build; after a release, `dev` names the last released version until the next one).

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

## Rationale

The release pipeline (gate, `RELEASES_ENABLED`, images from the release commit, the zip) was built and tested in spec 0030 around pushes to `main`. Option 1 reuses all of it and only adds the front (`dev`) and the back (the back merge); Option 2 would move and retest the pipeline for the sake of saving one job. A hotfix is the deciding case: on Option 1 it uses the normal release path, on Option 2 it needs a second path. Release branches (Option 3) solve a many lines problem this project does not have.

Squash into `dev` gives the changelog one line per feature and makes the pull request title count, which is why it is checked. The cost is that the per milestone commits from a branch are not kept on `dev`. `dev` into `main` must be a merge commit because release-please reads the commits between releases, and one squashed commit would hide them. The rulesets do not require an up to date branch: every `dev` into `main` merge leaves a merge commit on `main` that `dev` lacks, and the back merge runs only after a release, so that rule would block the next release pull request with an "Update branch" push that the `dev` ruleset refuses. A forgotten back merge stays harmless for a while (the next merge is a normal three way merge), and the back merge pull request stays open as a visible reminder.

The main guard allows `release-please--branches--main` because release-please's own pull request would otherwise be blocked by the rule that protects it. The title check runs in its own light workflow because the main CI does not rerun when a title is edited, and rerunning all of it for a title edit is waste.
