# 0030. Pre release image source for the zip

**Date**: 2026-11-11
**Status**: Proposed
**Scope**: [#46 Pre release image source for the zip](../scope/scope.md)
**Builds on**: [0015](0011-capability-module-contract/0015-platform-graph-managed-zip.md) (the platform graph and the managed zip), [0016](0016-platform-one-container/index.md) (setup.sh, the seed and the keeper)

## Summary

Until the first release, the platform's own images (`api`, `renderer`, `web`, `deployer-docker`) carry the tag `0.0.0`, and no registry holds them. So a zip that uses one of them fails on `docker compose up`. From now on, such a zip carries the image's own Dockerfile, and `setup.sh` builds the image first, from the exact commit of the code that rendered the zip (fetched from GitHub, or from a local checkout you name). The online editor builds the same way. At the release, the images are pushed while the release pull request is still open, so merging it is the last step and every zip after it simply pulls.

## Context

<!-- DECISION RECORD -->
The catalog entries of the platform's four own modules name `ghcr.io/racherom/modular-platform/<image>:0.0.0`. release-please rewrites that tag at the first release, and the release workflow pushes images only after the release pull request is merged. Nothing is released yet (`.release-please-manifest.json` is at `0.0.0`). So today three things name images that no registry holds:

- the static zip from the editor (`POST /v1/render`) for any graph that places one of these modules,
- the managed zip from `task managed:zip VERSION=0.0.0`, whose `setup.sh` starts the seed from the deployer image with `docker run` and runs the api image as a setup step (`caller-key generate`) before Compose ever runs (spec 0016),
- the online editor's own Compose stack (`deploy/online/compose.yaml`, web and renderer at `MP_VERSION=0.0.0`), which works only where `task build` tagged the images locally.

There is a second gap, and it outlives the first release. The release workflow pushes images after the merge, so for a few minutes main names a version whose images do not exist yet.

The forces: the zip must run with standard tools alone (principle of the static mode), its render must stay deterministic (the same renderer build and graph give the same bytes), `libs/render` must stay pure (no disk, network, clock or env), and the code that builds an image must be the code whose catalog rendered the zip, or the zip's config and the image can disagree. The repo's Dockerfiles copy the source from the repository root, so a Dockerfile alone cannot build anything; it needs a way to reach the source. An uncommitted first attempt (`services/renderer/internal/api/builds.go`, `render.WithBuilds`) adds a Compose build section from `#main`. That covers neither the managed `setup.sh` steps nor the drift between an old zip and today's main.

## Requirements

**User stories**:
- As a self hoster trying the platform before its first release, I want a downloaded zip that uses a platform module to start with `sh setup.sh` and `docker compose up`, so that I need no registry and no checkout.
- As a contributor, I want a zip rendered from my unpushed work to build from my local checkout, so that I can test it before I push.
- As a maintainer, I want the release to push its images before main names them, so that no zip, managed install or online editor ever names an image that does not exist.

**Acceptance criteria**:
- **AC-1**: A static zip whose graph places a module whose image (after any `-image-tag` retag) starts with `ghcr.io/racherom/modular-platform/` and carries exactly the tag `0.0.0` holds `build/<module>/Dockerfile`, byte for byte the catalog's `catalog/modules/<module>/Dockerfile`, once per module however many instances use it. Its Compose service keeps the `image:` line and has no build section. A zip with no such module is unchanged byte for byte (the existing goldens hold).
- **AC-2**: On a host with Docker (BuildKit) and access to GitHub, `sh setup.sh` from that zip first builds each of those images with `MP_SOURCE=git` and `MP_SOURCE_REF=<ref>` (the renderer's source ref), tags it with the catalog image name, and only then creates secrets or runs setup steps. `docker compose up` then starts the stack and pulls no platform image.
- **AC-3**: With `MP_SOURCE_PATH=<folder>` set, `setup.sh` resolves it to an absolute path, stops with a message when it holds no `go.work`, and otherwise builds those images with `MP_SOURCE=local` from that folder as the build context and fetches nothing from GitHub.
- **AC-4**: When a build fails (the ref is not on GitHub, no network, no Docker Hub for the Dockerfile frontend, no BuildKit, Docker older than 23), `setup.sh` exits non zero before it creates any secret, file or container, and its message names the image, the ref, and `MP_SOURCE_PATH` as the way to build from a local checkout.
- **AC-5**: The zip's `README.md` has a section that names each image it builds, the ref, what the build needs (Docker 23 or newer with BuildKit, access to GitHub and Docker Hub, a few minutes), and `MP_SOURCE_PATH`; it prints no time or other changing value. Two renders of the same graph by the same renderer build give the same bytes.
- **AC-6**: A managed zip rendered while the platform images carry `0.0.0` (`task managed:zip VERSION=0.0.0`) holds `build/<module>/Dockerfile` for `api`, `renderer`, `web` and `deployer-docker`, and those four services carry `pull_policy: never`. Its `setup.sh` builds them (same rules as AC-2 to AC-4) before it starts the seed, and the keeper then brings every platform service up healthy without pulling a platform image (spec 0016 AC-2 holds). Renders retagged with `-image-tag` (`dev`, `ci`) get no build block and no `pull_policy` change.
- **AC-7**: Each of the four images builds from `catalog/modules/<module>/Dockerfile`: `docker build -f catalog/modules/<module>/Dockerfile .` at the repo root (default `MP_SOURCE=local`) gives a working image as before, and with `--build-arg MP_SOURCE=git --build-arg MP_SOURCE_REF=<ref>` it builds the code at `<ref>` from GitHub with any build context. Every image carries the label `org.opencontainers.image.revision` set to `MP_SOURCE_REF`.
- **AC-8**: While `MP_VERSION` in `deploy/online/versions.env` is `0.0.0`, `task online:up` builds (`up --build`) web and renderer from GitHub at `MP_SOURCE_REF` (a new line in `versions.env`, default `main`) on a host that has only the `deploy/online/` folder, and zips from that renderer name that ref. Once `MP_VERSION` is a released version, `task online:up` pulls and builds nothing.
- **AC-9**: On the release-please pull request, a job builds and pushes all four images as `:<version>` (amd64 and arm64, with provenance and SBOM), again on every update of that pull request. It runs only for the release-please branch of this repository, and it is a required check, so the pull request cannot merge before its images are pushed. After the merge, a zip, a managed zip and the online editor name `<version>`, carry no `build/` folder, and pull. The job that attaches the managed zip to the release first checks that all four images exist in GHCR and fails if one is missing.
- **AC-10**: The renderer logs its source ref at startup (`source_ref`). A renderer built without `MP_SOURCE_REF` uses `main` and logs a warning that its zips build a moving branch; the zip README says so too. A ref that is neither a full 40 character commit nor a name matching `[A-Za-z0-9._/-]+` stops the renderer (and `render-platform`) at startup.

## Options considered

<!-- DECISION RECORD -->
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

One Dockerfile per image, beside its catalog entry, chooses its source with build args. The zip carries it, and `setup.sh` builds from GitHub at the renderer's commit, or from a local checkout. Images are pushed from the release pull request.

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

## Decision

<!-- BUILD SPEC -->
**Chosen option**: Option 3: the image's own Dockerfile in the zip, source by git at the renderer's commit.

A zip (static or managed) that names a platform image with the tag `0.0.0` carries that image's Dockerfile, and `setup.sh` builds it before anything else, from GitHub at the renderer's commit or from `MP_SOURCE_PATH`. The online editor builds from the same Dockerfiles at `MP_SOURCE_REF`. The release pull request pushes the images before its merge flips the tags.

**Implementation skills**: `multi-stage-dockerfile` (`github/awesome-copilot`, `.agents/skills/multi-stage-dockerfile/`) · `github-actions-templates` (`wshobson/agents`, `.agents/skills/github-actions-templates/`) · `golang-continuous-integration` (`samber/cc-skills-golang`, `.agents/skills/golang-continuous-integration/`) · `golang-testing` (`samber/cc-skills-golang`, `.agents/skills/golang-testing/`)

## Rationale

<!-- DECISION RECORD -->
The force that decides it is that the image must be built from the code whose catalog rendered the zip. Option 1 breaks that by design (`#main` moves), and it never reaches the managed `setup.sh`, which needs the deployer and api images before Compose runs. Option 2 keeps the link but pays a registry push per commit for a window that ends at the first release, and still leaves unpushed work without an image. Option 3 keeps the link with a git ref, a fixed content address, at no CI cost. Because `setup.sh` builds first, it serves both zips with one mechanism. Option 4 removes the GitHub dependency but makes the renderer ship its own source as data, which costs more than it saves for a short window.

The one Dockerfile with a build arg source (not a second copy, not a generated twin) is what makes Option 3 cheap to keep honest: the zip ships the very file CI and `task build` use, so the Go and Node versions and the build steps cannot drift. Moving it beside the catalog entry puts it where the catalog already describes the image, and where `go:embed` already reaches.

Pushing images from the release pull request closes the second gap at its root: the merge, which flips the catalog tags, the managed zip and `versions.env`, becomes the last step, so main never names a missing image. The cost is that the release pull request needs CI (the `RELEASE_PLEASE_TOKEN` secret, as the README already notes) and that its version tag is overwritten while the pull request changes. Both are acceptable before anyone pulls that tag.

## Feature design

<!-- BUILD SPEC -->
**The Dockerfile shape** (each of `catalog/modules/{api,renderer,web,deployer-docker}/Dockerfile`; every `COPY` from the context becomes `COPY --from=source`):

```dockerfile
# syntax=docker/dockerfile:1
ARG MP_SOURCE=local
ARG MP_SOURCE_REF=main

FROM scratch AS source-local
COPY . /

FROM scratch AS source-git
ARG MP_SOURCE_REF
ADD https://github.com/racherom/modular-platform.git#${MP_SOURCE_REF} /

FROM source-${MP_SOURCE} AS source

FROM golang:1.27.3-alpine AS build
COPY --from=source /go.work /go.work.sum* ./
# ... as today, each COPY from the source stage ...
# renderer only, declared right before its go build step so other layers keep their cache:
#   ARG MP_SOURCE_REF
#   -ldflags "-s -w -X <pkg>.sourceRef=${MP_SOURCE_REF}"

FROM gcr.io/distroless/static-debian12:nonroot
ARG MP_SOURCE_REF
LABEL org.opencontainers.image.revision=${MP_SOURCE_REF}
```

BuildKit builds only the stages the final stage reaches, so a local build never fetches git and a git build never reads the context. `MP_SOURCE_REF` is declared only in the git stage, the renderer's build step and the final stage, so a new commit does not throw away the Go and pnpm cache. Its default is `main`, because `ADD` with an empty ref silently builds the default branch; `setup.sh` and CI never pass an empty ref. `.dockerignore` gains `build` and `dist`, so `COPY . /` stays as small as today's per path copies. In git mode `.dockerignore` does not apply (the whole tree at the ref is fetched); no image reads `deploy/` or `docs/`, so this is harmless.

**What the zip carries and runs**:

| Piece | Static zip | Managed zip |
|---|---|---|
| `build/<module>/Dockerfile` | per `0.0.0` platform module the graph places | the same, for all four |
| Compose service | `image:` unchanged, no build section | `image:` unchanged, `pull_policy: never` (the keeper and the apply path run `compose pull`, which fails on an image only built locally) |
| `setup.sh` | build block first, before secrets and setup steps | build block first, before the state folder and the seed |
| `README.md` | "Images built on this host" section | the same |

The build block, per image: `docker buildx build --load -t <catalog image> --build-arg MP_SOURCE=git --build-arg MP_SOURCE_REF='<ref>' -f build/<module>/Dockerfile build/<module>` (the context is the folder holding only the Dockerfile), or with `MP_SOURCE_PATH` set, `--build-arg MP_SOURCE=local --build-arg MP_SOURCE_REF='<ref>' -f build/<module>/Dockerfile "$abs_source_path"`. The ref is written in single quotes; the render already checked it against the allowed form (AC-10), so it holds no quote, space or `#`. Before the first build the block checks that `docker buildx version` succeeds and the Docker server is version 23 or newer, and stops with a message otherwise. It always builds (the BuildKit cache makes a rerun cheap for a commit ref; a branch ref is fetched again), so a stale local `0.0.0` image from another commit is never used. A managed rerun whose ref changed gives the keeper a new image ID, which starts a handover (spec 0016 AC-6); the README says so.

**One predicate**: "this image is an unreleased platform image" (the prefix and the exact tag `0.0.0`) is one exported function in `libs/render`, used by the static render, `render.Platform` and `render-platform`. `render-platform` applies `-image-tag` first, so `dev` and `ci` renders never match it. The build block, the `build/` files and `pull_policy` are all behind the same condition in `libs/render/templates/setup.sh.tmpl` and the project build, so every existing golden stays unchanged.

**Online editor**: `deploy/online/compose.prerelease.yaml` gives web and renderer `build: context: https://github.com/racherom/modular-platform.git#${MP_SOURCE_REF}:catalog/modules/<module>` with `args: {MP_SOURCE: git, MP_SOURCE_REF: ${MP_SOURCE_REF}}`. The overlay writes `${MP_SOURCE_REF:?set in versions.env}`. `task online:up` reads `MP_VERSION` from `versions.env` in its shell command, and only while it is `0.0.0` adds this overlay and `--build`. With `MP_SOURCE=git` the git context is cloned once for the Dockerfile and once more by `ADD`; that double fetch is accepted for a window that ends at the release. A test checks that the overlay names exactly the platform services of `compose.yaml`.

**Release order**: a new workflow on `pull_request` (opened, synchronize, reopened) runs only when `github.head_ref` starts with `release-please--` and the head repository is this one. It reads the version from `.release-please-manifest.json` at the pull request head and pushes the four images like today's `images` job (moved, not copied), with `MP_SOURCE=local` from the checked out head and `MP_SOURCE_REF` set to the head commit. One concurrency group per pull request cancels a stale push, and `packages: write` is granted to that one job only. The required check makes a merge wait for the latest push. `release.yml` keeps release-please and the managed zip; the managed zip job first runs `docker buildx imagetools inspect` for each `:<version>` image and fails unless both `linux/amd64` and `linux/arm64` are listed. Two accepted edges: a release pull request closed without merging leaves an orphan `:<version>` tag (the next release overwrites it), and each image's revision label names the pull request head commit, not the squash commit on main (same tree, different hash).

**Value sourcing**:
| Action | Value produced | Source |
|---|---|---|
| Static render | which modules get a Dockerfile | module image has the prefix `ghcr.io/racherom/modular-platform/` and the tag exactly `0.0.0` (as `builds.go` decides today) |
| Static render | Dockerfile bytes | the catalog FS, `modules/<module>/Dockerfile`, read once at renderer start |
| Static render | path in the zip | `build/<module name>/Dockerfile` (module name, so instances share one) |
| Static render | the ref in `setup.sh` and README | the renderer's `sourceRef`, set by `-ldflags -X` from the `MP_SOURCE_REF` build arg; empty means `main` (with the warning of AC-10) |
| `task build` | `MP_SOURCE_REF` | `git rev-parse HEAD`, also with local changes (`MP_SOURCE_PATH` builds those) |
| Release PR job | `MP_SOURCE_REF`, version | `github.event.pull_request.head.sha`, `.release-please-manifest.json` at the head |
| Other CI image builds | `MP_SOURCE_REF` | `github.sha` |
| Online editor | `MP_SOURCE_REF` | `deploy/online/versions.env`, default `main` |
| Managed render | the ref | new `render-platform` flag `-source-ref`, default `main` with the AC-10 warning; `task managed:render` passes `git rev-parse HEAD` |
| Managed render | which modules | every module of `deploy/managed/graph.yaml` the shared predicate matches (all four today) |
| Managed render | Dockerfile bytes | `catalog.FS`, which `render-platform` already uses |
| `setup.sh` | the image tag to build | the module's catalog image, written by the render |
| `setup.sh` | local source folder | `MP_SOURCE_PATH` from the environment of whoever runs `setup.sh`, made absolute, must hold `go.work` |

**Key invariants**:
- Every catalog module whose image starts with `ghcr.io/racherom/modular-platform/` has a `Dockerfile` beside its `module.yaml` (a catalog test). The catalog loader reads only `module.yaml` and the templates it declares, so the Dockerfile changes nothing the catalog API returns.
- `libs/render` stays pure: the Dockerfile bytes and the ref come in as a render option (`render.WithSource`), which replaces `render.WithBuilds`; the render reads no file.
- A render whose graph has no `0.0.0` platform module is byte identical to today's. `task managed:zip`'s "same version gives the same bytes" holds for released versions; at `0.0.0` the bytes also depend on the commit.
- No zip, managed zip or online stack names a version whose images are not in GHCR once that version is on main.

**Security model**: the source is fetched over https from the public repository at a commit, which addresses its content exactly; no credentials reach a zip or `setup.sh`. The release images job gets `packages: write` only on the release-please branch of this repository, never for a fork. The `:<version>` tag is mutable until the merge; provenance records which commit each push came from.

**Configuration required**:
- Build args `MP_SOURCE` (`local` or `git`, default `local`) and `MP_SOURCE_REF` (a full commit or a branch, default `main`) on all four Dockerfiles. These are build inputs, not runtime settings, so `internal/config` does not parse them.
- `MP_SOURCE_REF` in `deploy/online/versions.env`, default `main`.
- `MP_SOURCE_PATH` read by `setup.sh` only, optional.
- The repo secret `RELEASE_PLEASE_TOKEN` must be set, or the release pull request starts no CI and AC-9 cannot hold.

**Critical test scenarios**:
- Render golden: a graph with `renderer` and `postgres` gives `build/renderer/Dockerfile` equal to the catalog file, an unchanged `image:` line, and the build block in `setup.sh`; verifies **AC-1**, **AC-5**.
- e2e (build tag `e2e`): render that graph, run `setup.sh` with `MP_SOURCE_PATH` set to the repo root, then `docker compose up --wait`; no pull of a platform image; verifies **AC-2**, **AC-3**.
- `setup.sh` with an unknown ref and no `MP_SOURCE_PATH`: non zero exit, no `secrets/` folder, message names the ref and `MP_SOURCE_PATH`; `MP_SOURCE_PATH` pointing at a folder without `go.work`: the same; verifies **AC-3**, **AC-4**.
- Ref check: a short commit, a ref with a space, `'` or `#`, and an empty build arg; the renderer refuses the first three at startup and uses `main` with a warning for the last; verifies **AC-10**.
- Managed: `task managed:zip VERSION=0.0.0`, install on a clean host; every platform service healthy with no platform pull, also after a keeper restart (reconcile runs `compose pull`); verifies **AC-6**.
- After the release: render the same graph with the catalog at a released version; no `build/` folder; verifies **AC-9**.

## Build plan

<!-- BUILD SPEC -->
Approach: Tracer Bullet. Task 2 runs the whole thread (catalog Dockerfile, render, zip, `setup.sh`, compose up) for the static zip; later tasks thicken the managed, online and release strands.

1. Move the four Dockerfiles to `catalog/modules/<module>/Dockerfile` with the source stages, the revision label and the renderer's `sourceRef`; add `build` and `dist` to `.dockerignore`; point `Taskfile.yml` (`build`, the api `:dev` build), `deploy/online/compose.dev.yaml`, `deploy/managed/compose.dev.yaml` and `release.yml` at them, then `git grep` the old paths (scripts, READMEs, `AGENTS.md` files, the Dockerfile header comments) until none is left; `task build` passing `MP_SOURCE_REF`; the catalog test for the invariant; the ref check and the startup log, satisfies **AC-7**, **AC-10**
2. Static zip thread: the shared predicate and `render.WithSource` in `libs/render` (replaces `WithBuilds` and `builds.go`), `build/<module>/Dockerfile` in the zip, the build block and README section in `setup.sh.tmpl`, renderer wiring; golden and handler tests; the e2e run with `MP_SOURCE_PATH`, satisfies **AC-1**, **AC-2**, **AC-3**, **AC-4**, **AC-5**
3. Managed zip: `-source-ref` on `render-platform` (after the `-image-tag` retag), the same option in `render.Platform`, `pull_policy: never` on matched services, the build block before the state folder and the seed, `task managed:render` passing the ref; check that the keeper's reconcile and pull skip those services; a verify run with `VERSION=0.0.0` including a keeper restart, satisfies **AC-6**
4. Online editor: `compose.prerelease.yaml`, `MP_SOURCE_REF` in `versions.env`, `task online:up` adding the overlay and `--build` while `MP_VERSION` is `0.0.0`, the overlay versus `compose.yaml` test, satisfies **AC-8**
5. Release order: the release pull request images workflow (the `images` job moved out of `release.yml`), the image check for both platforms before the managed zip upload, the required check set on `main`, satisfies **AC-9**

## Consequences

<!-- BUILD SPEC -->
**Positive**:
- Every zip made before the release runs without a registry, and builds the exact code that rendered it.
- One Dockerfile per image for every build path.
- Main never names a version whose images are missing, for this release and every later one.

**Negative / tradeoffs**:
- A first `setup.sh` takes minutes and needs BuildKit and GitHub access; an unpushed ref fails until you set `MP_SOURCE_PATH`.
- The online editor at the default `MP_SOURCE_REF=main` builds whatever main is when it rebuilds; pin a commit in `versions.env` for a fixed one.
- The release depends on `RELEASE_PLEASE_TOKEN`; without it the images are never pushed, and the managed zip job fails loudly instead of shipping a broken zip.
- The `:<version>` tag can be overwritten until the release pull request merges.
- `docker build -f services/<name>/Dockerfile` stops working; contributors use the new path.

**Neutral**:
- The pre release path goes quiet at the first release (no entry carries `0.0.0`); the build args stay, since they serve CI and the online editor anyway.
- A managed deploy of a user stack that places a `0.0.0` platform module still pulls and fails; the deployer gets no build step. Out of scope.

## Migration plan

**Strategy**: big bang, pre release; nothing outside this repository uses these paths.
**Phases**:
1. Tasks 1 to 5 land in order; the uncommitted `builds.go`, `WithBuilds` and their tests are replaced in task 2, not committed.
2. Before the first release, merge task 5 and set `RELEASE_PLEASE_TOKEN`; then merging the release pull request is the last step.
**Rollback**: revert the commits; images and zips go back to today's state.
**Risks**: a moved Dockerfile path missed in some script (task 1 greps for the old paths); the first release pull request job failing on permissions (watch its first run).

## Follow-up

- [ ] After the first release, remove the pre release render path (`WithSource`, the build blocks, `compose.prerelease.yaml`), or keep it if a later need for unreleased builds appears.
- [ ] Update `AGENTS.md` (the image line under Stack) and `deploy/online/AGENTS.md` for the new Dockerfile path and `MP_SOURCE_REF` (`/sync` owns this).
