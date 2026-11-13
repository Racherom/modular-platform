# 0030. Pre release image source for the zip: rationale

## Context

The catalog entries of the platform's four own modules name `ghcr.io/racherom/modular-platform/<image>:0.0.0`. release-please rewrites that tag at the first release, and the release workflow pushes images only after the release pull request is merged. Nothing is released yet (`.release-please-manifest.json` is at `0.0.0`). So today three things name images that no registry holds:

- the static zip from the editor (`POST /v1/render`) for any graph that places one of these modules,
- the managed zip from `task managed:zip VERSION=0.0.0`, whose `setup.sh` starts the seed from the deployer image with `docker run` and runs the api image as a setup step (`caller-key generate`) before Compose ever runs (spec 0016),
- the online editor's own Compose stack (`deploy/online/compose.yaml`, web and renderer at `MP_VERSION=0.0.0`), which works only where `task build` tagged the images locally.

There is a second gap, and it outlives the first release. The release workflow pushes images after the merge, so for a few minutes main names a version whose images do not exist yet.

The forces: the zip must run with standard tools alone (principle of the static mode), its render must stay deterministic (the same renderer build and graph give the same bytes), `libs/render` must stay pure (no disk, network, clock or env), and the code that builds an image must be the code whose catalog rendered the zip, or the zip's config and the image can disagree. The repo's Dockerfiles copy the source from the repository root, so a Dockerfile alone cannot build anything; it needs a way to reach the source. An uncommitted first attempt (`services/renderer/internal/api/builds.go`, `render.WithBuilds`) adds a Compose build section from `#main`. That covers neither the managed `setup.sh` steps nor the drift between an old zip and today's main.

## Options considered

### Option 1: Fix in place, a Compose build section from `#main`

Keep the uncommitted `builds.go`: each `0.0.0` platform service gets `build: context: https://github.com/racherom/modular-platform.git#main` and the repo Dockerfile path.

**Pros**:
- Already written and tested; tiny.
- Needs nothing new in CI or in `setup.sh`.

**Cons**:
- A zip from last week builds today's main, whose config can disagree with the zip.
- Does not reach the managed zip: its `setup.sh` runs the deployer and api images with `docker run` before Compose.
- The Dockerfile is not in the zip, so you cannot read what will be built.

### Option 2: CI images for every commit

CI pushes the four images on every push to main with a pre release tag (`0.0.0-main.<sha>`); the renderer names that tag.

**Pros**:
- `docker compose up` pulls in seconds; nothing builds on the host.
- The zip runs exactly the commit that rendered it.

**Cons**:
- A push of four images for two architectures on every commit, plus GHCR cleanup of hundreds of tags.
- The tag must reach the embedded catalog at image build, a second retag path beside `-image-tag`.
- Unpushed local work still has no image anywhere.

### Option 3: The image's own Dockerfile in the zip, source by git at the renderer's commit

One Dockerfile per image, beside its catalog entry, chooses its source with build args. The zip carries it, and `setup.sh` builds from GitHub at the renderer's commit, or from a local checkout. Images were first pushed from the release pull request; since 2026-11-13 they are pushed right after the release merge (see below).

**Pros**:
- The zip says exactly what it builds and from which commit; builds are reproducible from that ref.
- One Dockerfile per image for every build (local, CI, online, zip); nothing to keep in step.
- Covers the managed zip, because `setup.sh` builds before the seed and the setup steps.
- No CI or registry cost before the release.

**Cons**:
- A first `setup.sh` takes minutes (Go and Vite builds) and needs BuildKit and GitHub access.
- Adds a build block to both `setup.sh` templates and a source option to the render.

### Option 4: The source in the zip

The zip carries the source tree the builds need (about 750 KB compressed) beside the Dockerfiles.

**Pros**:
- Builds without GitHub access (Go and pnpm still download dependencies).
- Works for unpushed commits on any host.

**Cons**:
- The renderer image must carry the source and hand it to the render as data; every such zip grows by about a megabyte.
- The source in a zip is a copy nobody reviews, and it ages with the zip.

## Rationale

The force that decides it is that the image must be built from the code whose catalog rendered the zip. Option 1 breaks that by design (`#main` moves), and it never reaches the managed `setup.sh`, which needs the deployer and api images before Compose runs. Option 2 keeps the link but pays a registry push per commit for a window that ends at the first release, and still leaves unpushed work without an image. Option 3 keeps the link with a git ref, a fixed content address, at no CI cost. Because `setup.sh` builds first, it serves both zips with one mechanism. Option 4 removes the GitHub dependency but makes the renderer ship its own source as data, which costs more than it saves for a short window.

The one Dockerfile with a build arg source (not a second copy, not a generated twin) is what makes Option 3 cheap to keep honest: the zip ships the very file CI and `task build` use, so the Go and Node versions and the build steps cannot drift. Moving it beside the catalog entry puts it where the catalog already describes the image, and where `go:embed` already reaches.

The first design pushed the images from the release pull request, to close the second gap: the merge would be the last step, so main would never name a missing image. The review of 2026-11-13 showed that this builds the images from the pull request head, not from the release commit, and the push moved to after the merge (below). The second gap is now accepted for the few minutes of the build.

### Release images after the merge (AC-9, revised 2026-11-13, review finding #2)

The review found that the images were built from the release pull request head while the release is the merge commit on main. When the head is behind main (a commit lands on main and release-please has not updated its branch yet, or does not update it because the commit adds nothing to the changelog), the squash merge holds code the images do not.

- **Chosen (by the engineer): build and push after the merge, from the release commit.** The mismatch cannot happen, since the image and the release come from one commit. `release-images.yml`, its required check and the overwrite risk of the open pull request all go away. The cost is the gap the first design closed: for the few minutes of the build, main names images that are not pushed yet. That gap is short and visible (a pull fails, a rerun works), and it is the order most projects use.
- Recommended in the conversation: keep the push before the merge, turn on "Require branches to be up to date before merging" for `main`, and compare the merge commit's tree with the tree of the image label's commit before the managed zip upload. It keeps main from ever naming a missing image, but it rests on a repository setting that costs an "Update branch" on every pull request, and its tree check finds a mismatch only after the release exists. The engineer weighed this and chose the simpler order; it is a sound choice, and the gap is recorded in Consequences.
- Rejected: the tree check alone. No setting to keep, but it only detects the mismatch after the tag and the release exist, and the repair is by hand.

### The ref of a local build (AC-12, added 2026-11-13, review finding #4)

With `MP_SOURCE_PATH`, `setup.sh` passed the zip's ref, so the label and a locally built renderer claimed a commit the checkout may not match; that renderer then wrote the claim into its own zips.

- **Chosen: the reserved value `local`.** True in every case (uncommitted changes, unpushed commits, another branch), needs no git on the host, and a zip that carries it fails early with the one fix that works (`MP_SOURCE_PATH`) instead of a git fetch error. `task build` uses the same rule, keeping the commit when the tree is clean, so CI builds lose nothing.
- Runner up: the checkout's `git rev-parse HEAD`. A real commit when the tree is clean, but it needs git on the host and still claims that commit when the tree has changes.
- Rejected: keep the zip's ref. It is the finding.

### Pinned build inputs and the online editor's commit (AC-8, AC-13, added 2026-11-13, review finding #5)

A commit ref pins the repository, but the frontend and base images were tags, and the online editor built `main` by default, so two builds of one zip could differ and the editor's zips named a moving branch.

- **Chosen: digests on every frontend and base image line, kept fresh by Renovate (dockerfile manager, digests only, the four catalog files only).** Renovate reads the `# syntax=` directive as well as `FROM`; tag bumps stay manual because the Go and Node versions are tied to `go.work`, CI and the lockfiles. Runner up: Dependabot, native to GitHub with nothing to install, but it leaves the frontend digest to hand updates. Rejected: pins by hand with no bot, which go stale silently and miss base image security fixes.
- **Chosen: `task online:up` resolves a branch or tag to its commit with `git ls-remote` and builds that commit.** The images, their label and every zip the editor makes then name one fixed commit, and each start still picks up the newest main. Runner up: a commit in `versions.env`, bumped by hand; fully fixed, but it gets stale and needs edits. Rejected: keep `main`; the editor's zips would build whatever main holds when someone runs `setup.sh`.

### AC-8 before a push: the online editor from a checkout (added 2026-11-13)

The online editor's prerelease build fetched its source from GitHub in two places: the Compose git context in `compose.prerelease.yaml` and the `ADD` in each Dockerfile's `source-git` stage. So its end to end AC-8 check could only run after a push, and only with network access, while the engineer wants every AC-8 check to pass on a commit that is not on GitHub yet, with no git remote and no internet for the source. The real online editor still builds from GitHub, after a push.

- **Chosen: `MP_SOURCE_PATH` for `task online:up`, with a `compose.checkout.yaml` overlay (the checkout as a local build context, `MP_SOURCE=local`), and an `integration` test that drives the real task on a fresh empty commit in a clone.** It uses the name `setup.sh` already gives the same idea (AC-3), it is a path a contributor can use offline too, and the test runs the task's own branch, export and messages. The ref follows the Taskfile rule of AC-12 (HEAD when clean, else `local`), so a clean test clone names its commit end to end, and dirty work honestly says `local`. `setup.sh` always says `local` instead, because a zip's ref can differ from the checkout it builds; here the ref comes from that checkout itself, so no such gap exists.
- Runner up: a test only overlay in `libs/ci/testdata`, with the test calling `docker compose` itself. Less product surface, but the task's 0.0.0 branch, its export and its message go untested.
- Rejected: a local git remote (an `httptest` server around `git http-backend`, or `git daemon`) passed to BuildKit through a new `MP_SOURCE_REMOTE` build arg or a whole repository git context. It keeps the git fetch in the test, but the engineer asked that nothing use a git remote; it also needs a daemon that can reach the host's loopback, which rootless Docker and Docker Desktop cannot.
- Rejected: push a throwaway branch for each check. That is the wait this decision removes.
- Accepted gap: the GitHub git context URL is now checked only by reading the overlay, and the `source-git` stage by the AC-7 step that already passed; both are stable, and a live GitHub build happens whenever someone starts the editor without `MP_SOURCE_PATH`.

### Release gate (AC-11, added 2026-11-12)

After the merge of #46, every push to `main` failed in the Release workflow: `RELEASE_PLEASE_TOKEN` was not set, release-please fell back to `github.token`, and GitHub refused with "GitHub Actions is not permitted to create or approve pull requests". The only way to make that fallback work is the repository setting that lets Actions create and approve pull requests, and approving a pull request should stay with a person, not CI. No release is near, so the job should not run yet at all.

- **Chosen: off until the repository variable `RELEASES_ENABLED` is `true`, no `github.token` fallback.** The run passes with skipped jobs until then; turning releases on is one variable, and from then on release-please runs on every push as before. A job's `if` can read `vars` but not `secrets`, which is why the switch is a variable and the token check is a step.
- Runner up: start the Release workflow by hand only (`workflow_dispatch`). It never fails on a push, but release-please creates the GitHub release on the run after the merge, so each release needs two manual starts.
- Rejected: skip quietly when the token is missing. No setting needed, but a missing token at release time would pass unnoticed.
