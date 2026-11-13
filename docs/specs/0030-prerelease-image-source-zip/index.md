# 0030. Pre release image source for the zip

**Date**: 2026-11-11, updated 2026-11-13 (review findings #2, #4 and #5: release images after the merge, the `local` ref, pinned build inputs)
**Status**: In Progress
**Scope**: [#46 Pre release image source for the zip](../../scope/scope.md)
**Builds on**: [0015](../0011-capability-module-contract/0015-platform-graph-managed-zip.md) (the platform graph and the managed zip), [0016](../0016-platform-one-container/index.md) (setup.sh, the seed and the keeper)

## Summary

Until the first release, the platform's own images (`api`, `renderer`, `web`, `deployer-docker`) carry the tag `0.0.0`, and no registry holds them. So a zip that uses one of them fails on `docker compose up`. From now on, such a zip carries the image's own Dockerfile, and `setup.sh` builds the image first, from the exact commit of the code that rendered the zip (fetched from GitHub, or from a local checkout you name). The online editor builds the same way, from the commit its branch points to when you start it. At the release, the images are built from the release commit itself, right after the release pull request merges, so an image and its release always hold the same code; for those few minutes, main names images that are still being pushed. A build from a local checkout says so (`local`) instead of claiming a commit, and every base image and the Dockerfile frontend are pinned by digest, which Renovate keeps fresh.

## Requirements

**User stories**:
- As a self hoster trying the platform before its first release, I want a downloaded zip that uses a platform module to start with `sh setup.sh` and `docker compose up`, so that I need no registry and no checkout.
- As a contributor, I want a zip rendered from my unpushed work to build from my local checkout, so that I can test it before I push.
- As a maintainer, I want the release images built from the release commit, so that an image always holds exactly the code of the release that names it.
- As anyone reading an image label or a zip's README, I want the source ref to be true, so that a build from a local checkout never claims a commit it does not come from.
- As a self hoster, I want two builds of the same zip to use the same toolchain, so that the pinned commit really pins the build.

**Acceptance criteria**:
- **AC-1**: A static zip whose graph places a module whose image (after any `-image-tag` retag) starts with `ghcr.io/racherom/modular-platform/` and carries exactly the tag `0.0.0` holds `build/<module>/Dockerfile`, byte for byte the catalog's `catalog/modules/<module>/Dockerfile`, once per module however many instances use it. Its Compose service keeps the `image:` line and has no build section. A zip with no such module is unchanged byte for byte (the existing goldens hold).
- **AC-2**: On a host with Docker (BuildKit) and access to GitHub, `sh setup.sh` from that zip first builds each of those images with `MP_SOURCE=git` and `MP_SOURCE_REF=<ref>` (the renderer's source ref), tags it with the catalog image name, and only then creates secrets or runs setup steps. `docker compose up` then starts the stack and pulls no platform image.
- **AC-3**: With `MP_SOURCE_PATH=<folder>` set, `setup.sh` resolves it to an absolute path, stops with a message when it holds no `go.work`, and otherwise builds those images with `MP_SOURCE=local` and `MP_SOURCE_REF=local` (never the zip's ref, see AC-12) from that folder as the build context and fetches nothing from GitHub.
- **AC-4**: When a build fails (the ref is not on GitHub, no network, no Docker Hub for the Dockerfile frontend, no BuildKit, Docker older than 23), `setup.sh` exits non zero before it creates any secret, file or container, and its message names the image, the ref, and `MP_SOURCE_PATH` as the way to build from a local checkout.
- **AC-5**: The zip's `README.md` has a section that names each image it builds, the ref, what the build needs (Docker 23 or newer with BuildKit, access to GitHub and Docker Hub, a few minutes), and `MP_SOURCE_PATH`; it prints no time or other changing value. Two renders of the same graph by the same renderer build give the same bytes.
- **AC-6**: A managed zip rendered while the platform images carry `0.0.0` (`task managed:zip VERSION=0.0.0`) holds `build/<module>/Dockerfile` for `api`, `renderer`, `web` and `deployer-docker`, and those four services carry `pull_policy: never`. Its `setup.sh` builds them (same rules as AC-2 to AC-4) before it starts the seed, and the keeper then brings every platform service up healthy without pulling a platform image (spec 0016 AC-2 holds). Renders retagged with `-image-tag` (`dev`, `ci`) get no build block and no `pull_policy` change.
- **AC-7**: Each of the four images builds from `catalog/modules/<module>/Dockerfile`: `docker build -f catalog/modules/<module>/Dockerfile .` at the repo root (default `MP_SOURCE=local`) gives a working image as before, and with `--build-arg MP_SOURCE=git --build-arg MP_SOURCE_REF=<ref>` it builds the code at `<ref>` from GitHub with any build context. Every image carries the label `org.opencontainers.image.revision` set to `MP_SOURCE_REF`.
- **AC-8**: While `MP_VERSION` in `deploy/online/versions.env` is `0.0.0`, `task online:up` builds (`up --build`) web and renderer from GitHub at `MP_SOURCE_REF` (a new line in `versions.env`, default `main`). When that value is not a full 40 character commit, `task online:up` first resolves it to the commit it points to on GitHub (`git ls-remote`, a branch, or a tag peeled to its commit) and builds that commit, so both images carry it in their revision label, the renderer logs it as `source_ref` with no moving branch warning, and its zips name it. A ref GitHub does not hold, or `local`, stops `task online:up` before Compose runs, with a message that names the ref. A rerun resolves again, so it builds the newest commit of a branch. Once `MP_VERSION` is a released version, `task online:up` resolves, pulls and builds nothing.
- **AC-9**: When release-please creates a release (its merge commit lands on `main`), a job of the Release workflow builds the four images from that commit (`MP_SOURCE=local`, `MP_SOURCE_REF` set to the commit) and pushes each as `:<version>` (amd64 and arm64, with provenance and SBOM). Before it pushes, each image's job fails if `:<version>` of that image is already in GHCR, so a released image is never replaced; rerunning only the failed jobs pushes only the images still missing. The release pull request runs no image job and needs no image check. The managed zip job runs only after all four images are pushed, first checks that all four exist in GHCR for amd64 and arm64, and fails if one is missing. After that, a zip, a managed zip and the online editor name `<version>`, carry no `build/` folder, and pull.
- **AC-10**: The renderer logs its source ref at startup (`source_ref`). A renderer built without `MP_SOURCE_REF` uses `main` and logs a warning that its zips build a moving branch; the zip README says so too. A ref that is neither a full 40 character commit, nor `local` (AC-12), nor a name matching `[A-Za-z0-9._/-]+` that git accepts stops the renderer (and `render-platform`) at startup.
- **AC-12**: `local` is a reserved source ref meaning "built from a local checkout, at no commit on GitHub". An image built through `MP_SOURCE_PATH` carries `org.opencontainers.image.revision=local`, and a renderer built so logs `source_ref=local` with a warning that its zips build only with `MP_SOURCE_PATH`. A zip rendered with the ref `local` says so in its README, and its `setup.sh`, run without `MP_SOURCE_PATH`, exits non zero before the BuildKit check and before it creates any file, with a message that names `MP_SOURCE_PATH`. `task build` and `task managed:render` pass the checkout's `git rev-parse HEAD` when `git status --porcelain` prints nothing, and `local` otherwise. A branch or tag named `local` cannot be used as a source ref.
- **AC-13**: Every `# syntax=` line and every `FROM` line that names a registry image in the four catalog Dockerfiles carries a digest (`<image>:<tag>@sha256:<digest>`, the multi platform index); a test fails on any that does not. `renovate.json` lets Renovate open pull requests that update only those digests, in those four files only; tag bumps (Go, Node, Caddy, Docker CLI) stay manual, since they are tied to `go.work`, CI and the lockfiles. A digest update needs no golden update: the render test compares the zip's `build/<module>/Dockerfile` with the catalog file at test time instead of a stored copy.
- **AC-11**: release-please runs only while the repository variable `RELEASES_ENABLED` is `true`. Until then, a push to `main` shows the release-please, images and managed zip jobs as skipped and the Release workflow passes. release-please always uses the secret `RELEASE_PLEASE_TOKEN`, never `github.token`, so the repository setting "Allow GitHub Actions to create and approve pull requests" stays off. With the variable `true` and the secret missing, the job fails with a message that names the secret.

## Decision

<!-- BUILD SPEC -->
**Chosen option**: Option 3: the image's own Dockerfile in the zip, source by git at the renderer's commit.

A zip (static or managed) that names a platform image with the tag `0.0.0` carries that image's Dockerfile, and `setup.sh` builds it before anything else, from GitHub at the renderer's commit or from `MP_SOURCE_PATH`. The online editor builds from the same Dockerfiles at the commit `MP_SOURCE_REF` resolves to. The Release workflow pushes the images from the release commit right after the merge. A build from a local checkout records `local`, and every build input outside the repository is pinned by digest.

**Implementation skills**: `multi-stage-dockerfile` (`github/awesome-copilot`, `.agents/skills/multi-stage-dockerfile/`) · `github-actions-templates` (`wshobson/agents`, `.agents/skills/github-actions-templates/`) · `golang-continuous-integration` (`samber/cc-skills-golang`, `.agents/skills/golang-continuous-integration/`, also for the Renovate config) · `golang-testing` (`samber/cc-skills-golang`, `.agents/skills/golang-testing/`)

## Rationale

Context, the options considered and the reasoning: see [rationale.md](rationale.md).

## Feature design

<!-- BUILD SPEC -->
**The Dockerfile shape** (each of `catalog/modules/{api,renderer,web,deployer-docker}/Dockerfile`; every `COPY` from the context becomes `COPY --from=source`):

```dockerfile
# syntax=docker/dockerfile:1@sha256:<digest>
ARG MP_SOURCE=local
ARG MP_SOURCE_REF=main

FROM scratch AS source-local
COPY . /

FROM scratch AS source-git
ARG MP_SOURCE_REF
ADD https://github.com/racherom/modular-platform.git#${MP_SOURCE_REF} /

FROM source-${MP_SOURCE} AS source

FROM golang:1.27.3-alpine@sha256:<digest> AS build
COPY --from=source /go.work /go.work.sum* ./
# ... as today, each COPY from the source stage ...
# renderer only, declared right before its go build step so other layers keep their cache:
#   ARG MP_SOURCE_REF
#   -ldflags "-s -w -X <pkg>.sourceRef=${MP_SOURCE_REF}"

FROM gcr.io/distroless/static-debian12:nonroot@sha256:<digest>
ARG MP_SOURCE_REF
LABEL org.opencontainers.image.revision=${MP_SOURCE_REF}
```

**Pinned build inputs (AC-13)**: the commit pins the repository source, and the digests pin everything else the build pulls: the frontend (`docker/dockerfile:1`) and each base (`golang`, `node`, `caddy`, `docker` CLI, distroless). Each line keeps its tag for people to read and adds the digest of the multi platform index (`docker buildx imagetools inspect <image>:<tag>` prints it). The pin rule the test enforces, per catalog Dockerfile:

- the `# syntax=` line must end in `@sha256:` plus 64 lowercase hex characters;
- every `FROM` (also `FROM --platform=<p> <image>`) must too, except `scratch`, a name equal to an earlier stage's `AS` name, and a name that holds `${` (today only `source-${MP_SOURCE}`, which always resolves to a stage);
- `COPY --from=<x>` and `RUN --mount=...,from=<x>` count only when `<x>` is not a stage name (none does today); then the same digest rule applies.

`renovate.json` at the repository root:

```json
{
  "$schema": "https://docs.renovatebot.com/renovate-schema.json",
  "enabledManagers": ["dockerfile"],
  "includePaths": ["catalog/modules/*/Dockerfile"],
  "pinDigests": true,
  "schedule": ["before 6am on monday"],
  "packageRules": [
    { "matchManagers": ["dockerfile"], "matchUpdateTypes": ["major", "minor", "patch"], "enabled": false },
    { "matchManagers": ["dockerfile"], "matchUpdateTypes": ["digest", "pin", "pinDigest"], "groupName": "build image digests" }
  ]
}
```

Only the `dockerfile` manager runs, only on the four catalog files (not the e2e `sdkpair` or `tlsmodule` test images), only digest and pin updates are enabled, and they come as one grouped pull request a week. Renovate reads the `# syntax=` directive as well as `FROM` lines, which is why it is the keeper here; check that once on its first run (the frontend digest shows up in the grouped pull request). A wrong key in this file fails silently, so `task lint` runs `pnpm dlx --package renovate@<version> renovate-config-validator renovate.json` (the validator ships with the `renovate` npm package; its exact version is a Taskfile variable, like `GOLANGCI_LINT_VERSION`). The Go test that holds the AC-13 check sits beside the catalog test of the Dockerfile invariant. The prerelease golden drops its stored `build/renderer/Dockerfile`; the test instead reads the catalog file and checks that the zip holds the same bytes (the AC-1 rule), so a Renovate pull request passes CI without `-update`.

**The `local` ref (AC-12)**: `render.NewSourceRef("local")` returns a `SourceRef` whose `Local()` is true (and `Commit()` false); `NewSourceRef` checks for `local` before the git name rules, and `String()` returns `local`. The render data gains `SourceLocal`; when it is true, `SourceMoves` is false, so a zip gets exactly one of the three README texts. The renderer's startup warning for `local` ("the renderer's source ref is local: it was built from a local checkout, and its zips build only with MP_SOURCE_PATH") replaces the AC-10 branch warning, never adds to it. A local ref has a third README text and a third `setup.sh` path:

| Zip ref | README says | `setup.sh` without `MP_SOURCE_PATH` | with `MP_SOURCE_PATH` |
|---|---|---|---|
| full commit | builds that commit | builds from GitHub | builds the checkout, label `local` |
| branch or tag | builds what the branch holds then (AC-10) | builds from GitHub | builds the checkout, label `local` |
| `local` | made by a renderer built from a local checkout; needs `MP_SOURCE_PATH` | stops first, names `MP_SOURCE_PATH` | builds the checkout, label `local` |

In local mode the build block always passes `--build-arg MP_SOURCE_REF=local`, whatever the zip's ref is, so the label and a locally built renderer never claim a commit the checkout may not match (uncommitted changes, an unpushed commit, a different branch). `local` needs no git on the host. The Taskfile's `SOURCE_REF` variable becomes: `git rev-parse HEAD` when `git status --porcelain` prints nothing, else `local`. CI checks out a clean tree, so its builds keep the commit. The Taskfile's own `setup.sh` runs (`dev:managed:install`, `smoke:managed`) render with `-image-tag dev` or `ci`, so they carry no build block and are not affected; the prerelease e2e test sets `MP_SOURCE_PATH` itself. `task managed:zip` with `VERSION=0.0.0` on a tree with changes prints `managed:zip: the tree has uncommitted changes, so this zip builds only with MP_SOURCE_PATH; commit first for a zip that builds from GitHub` to stderr and still writes the zip.

The `setup.sh` messages, exactly (tests match them; the `MP_SOURCE_PATH` folder is resolved with `cd "$MP_SOURCE_PATH" && pwd -P`, before the `cd` into the zip folder, so a relative path, a path with spaces and a symlink all work):

| Case | Order | Message on stderr |
|---|---|---|
| `MP_SOURCE_PATH` is no folder | first | `setup.sh: MP_SOURCE_PATH=<value> is not a folder` (today's) |
| folder without `go.work` | second | `setup.sh: MP_SOURCE_PATH=<abs path> holds no go.work, so it is not a Modular Platform checkout` (today's) |
| zip ref `local`, no `MP_SOURCE_PATH` | third, before the BuildKit and Docker version checks | `setup.sh: this zip was made by a renderer built from a local checkout, so no commit on GitHub holds its code; set MP_SOURCE_PATH=<a checkout of Modular Platform> and run setup.sh again` |
| a build fails (AC-4) | per image | today's text, plus `the build needs GitHub and Docker Hub (the Dockerfile frontend and base images)` |

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

**Online editor**: `deploy/online/compose.prerelease.yaml` gives web and renderer `build: context: https://github.com/racherom/modular-platform.git#${MP_SOURCE_REF}:catalog/modules/<module>` with `args: {MP_SOURCE: git, MP_SOURCE_REF: ${MP_SOURCE_REF}}`. The overlay writes `${MP_SOURCE_REF:?set in versions.env}`. `task online:up` reads `MP_VERSION` from `versions.env` in its shell command, and only while it is `0.0.0` adds this overlay and `--build`. In that branch it also resolves the ref (AC-8) with `scripts/online-source-ref.sh`, whose contract is:

- **Input**: the shell `MP_SOURCE_REF` if set, else the `MP_SOURCE_REF=` line of `deploy/online/versions.env` (the same pattern as `MP_VERSION` today). The remote is `MP_SOURCE_REMOTE`, default `https://github.com/racherom/modular-platform.git`; the test points it at a local bare repository.
- **Checks before git**: 40 lowercase hex characters passes through unchanged with no network call; `local` stops (`online-source-ref: MP_SOURCE_REF=local builds from a checkout, but the online editor builds from GitHub; set a commit, branch or tag`); a short commit, uppercase hex or a value outside `[A-Za-z0-9._/-]+` stops (`online-source-ref: MP_SOURCE_REF=<ref> is neither a full commit nor a branch or tag name`).
- **Resolve**: `GIT_TERMINAL_PROMPT=0 git ls-remote --exit-code "$remote" "refs/heads/<ref>" "refs/tags/<ref>" "refs/tags/<ref>^{}"`, then pick by exact ref name, never by line order (ls-remote sorts by name, so a tag object comes before its peeled commit): `refs/heads/<ref>` first, then `refs/tags/<ref>^{}` (the commit an annotated tag points to), then `refs/tags/<ref>` (a lightweight tag). Exit 2 from git means no match: `online-source-ref: <ref> is no branch or tag of <remote>`. Any other failure: `online-source-ref: could not reach <remote> to resolve <ref>; check the network, or set MP_SOURCE_REF to a full commit`.
- **Output**: the bare commit on stdout, messages on stderr; exit 0 on success, 1 on any stop.

`task online:up` exports the output as `MP_SOURCE_REF` for the `docker compose` command, which takes the shell value over the `--env-file` value, so `versions.env` is never rewritten. The host already has git, since `task online:up` runs from a checkout. The bare `docker compose` command in the overlay's header comment still builds whatever ref `versions.env` names; the comment says to put a commit there for that path. With `MP_SOURCE=git` the git context is cloned once for the Dockerfile and once more by `ADD`; that double fetch is accepted for a window that ends at the release. A test checks that the overlay names exactly the platform services of `compose.yaml`.

**Release order (revised 2026-11-13, review finding #2)**: the images are built after the merge, from the release commit, so the image and the release cannot differ. `release-images.yml` and its required check *Release images to GHCR* are removed. `release.yml` gets an `images` job between release-please and the managed zip: `needs: release-please`, `if: needs.release-please.outputs.created == 'true'`, a matrix over the four services with `fail-fast: false` (one failed image must not cancel the others' push), `permissions: contents: read, packages: write` (this job only). Its steps are those of the old `release-images.yml` job (checkout, GHCR login, QEMU, buildx, the build and push with the same labels and per service GHA cache scope), with three changes: it checks out `github.sha` (the push's commit, which is the release commit; a rerun days later still builds that same commit); the version comes from `needs.release-please.outputs.version` through `env:`; and the git tag guard is replaced by a GHCR guard right before the push. The guard runs `docker buildx imagetools inspect ghcr.io/racherom/modular-platform/<service>:$VERSION` and reads its result three ways: success means already released (`::error::<image>:<version> is already in GHCR; a released image is never pushed again`, exit 1); a failure whose output says `not found` means not released yet, so the job goes on; any other failure (network, auth) fails the job with its output, so an unknown state never leads to a push. The build is today's (`MP_SOURCE=local`, `MP_SOURCE_REF=${{ github.sha }}`, both platforms, provenance `mode=max`, SBOM). The build and push step is the last step of the job, so nothing can fail after a successful push. If a push still ends half done (the tag is in GHCR but the job failed), "Re-run failed jobs" stops at the guard; the repair is by hand: check the tag with `scripts/release-images-check.sh`, and if it is complete, rerun only the managed zip job, else delete the tag in GHCR and rerun. The managed zip job becomes `needs: [release-please, images]` with its `if` unchanged; when an image job fails, the managed zip job is skipped, and "Re-run failed jobs" pushes only the missing images (the existing ones fail their own check, which is why a full rerun is not the way to finish a release; rerunning the whole workflow makes release-please report no new release, so every later job skips). No concurrency group: a later push to `main` starts a run whose release-please reports no new release, so it never touches the images. The "never push a released version's images again" guard of the release pull request job (git tag `v<version>` exists) moves here as the GHCR check, since the git tag always exists at this point. The release-please job carries `if: vars.RELEASES_ENABLED == 'true'`, a repository level variable (not a secret, because a job's `if` can read `vars` but not `secrets`; not an environment variable, which a job's `if` cannot see). Actions compares strings without regard to case, so `TRUE` also turns it on; anything else, or unset, skips. Its first step, a separate `run` step named *Release token is set*, gets `TOKEN: ${{ secrets.RELEASE_PLEASE_TOKEN }}` in `env` and runs `[ -n "$TOKEN" ] || { echo "::error::RELEASE_PLEASE_TOKEN is not set; set it before RELEASES_ENABLED (README, Releases)"; exit 1; }`. The action's `token:` becomes `${{ secrets.RELEASE_PLEASE_TOKEN }}` with no `||` fallback, and the job's `permissions` drop to `contents: read`, since the token carries the rights. The managed zip job keeps `needs: release-please` and its `if: needs.release-please.outputs.created == 'true'` with no `always()`, so it is skipped with release-please; skipped jobs do not fail the run, so the Release run passes. The managed zip job's `gh release upload` keeps `github.token` with `contents: write`: it uploads a file and opens no pull request, so the "never `github.token`" rule of AC-11 covers release-please only. Turning the variable on starts no run: the first release pull request appears on the next push to `main` (or a rerun of the latest Release run). With no `bootstrap-sha` in `release-please-config.json`, that first pull request lists every commit since the repository began and releases `0.1.0`; that is accepted for a first release. Opening and approving a pull request stays with a person's token: CI never gets the right to approve one. The managed zip job first runs `scripts/release-images-check.sh` (`docker buildx imagetools inspect` for each `:<version>` image) and fails unless both `linux/amd64` and `linux/arm64` are listed. Accepted edge: from the merge until the `images` job ends (a few minutes, the time of four multi platform builds), main, the GitHub release and `versions.env` name images that are not in GHCR yet. A zip rendered or an online editor started in that window fails to pull; `docker compose up` or `task online:up` works once the push ends. Each image's revision label names the release commit exactly.

**Value sourcing**:
| Action | Value produced | Source |
|---|---|---|
| Static render | which modules get a Dockerfile | module image has the prefix `ghcr.io/racherom/modular-platform/` and the tag exactly `0.0.0` (as `builds.go` decides today) |
| Static render | Dockerfile bytes | the catalog FS, `modules/<module>/Dockerfile`, read once at renderer start |
| Static render | path in the zip | `build/<module name>/Dockerfile` (module name, so instances share one) |
| Static render | the ref in `setup.sh` and README | the renderer's `sourceRef`, set by `-ldflags -X` from the `MP_SOURCE_REF` build arg; empty means `main` (with the warning of AC-10) |
| `task build` | `MP_SOURCE_REF` | the Taskfile's `SOURCE_REF`: `git rev-parse HEAD` when `git status --porcelain` prints nothing, else `local` (AC-12) |
| Release `images` job | `MP_SOURCE_REF` | `github.sha` of the push that created the release (the release commit) |
| Release `images` job | version | `needs.release-please.outputs.version`, passed through `env:` |
| Release `images` job | whether the image is already released | `docker buildx imagetools inspect` of `:<version>` in GHCR succeeds |
| Other CI image builds | `MP_SOURCE_REF` | `github.sha` |
| Dockerfiles | each `@sha256:` digest | the multi platform index digest of the tag when it is pinned (`docker buildx imagetools inspect`), then Renovate's digest pull requests |
| Release workflow | whether release-please runs | repository variable `RELEASES_ENABLED`, set by the maintainer when a release is near; unset means skipped |
| Release workflow | the token that opens the release pull request | repository secret `RELEASE_PLEASE_TOKEN` only |
| Online editor | `MP_SOURCE_REF` as written | `deploy/online/versions.env`, default `main` |
| Online editor | the commit it builds | a full commit as written; otherwise `git ls-remote` of the repository at `task online:up` time (branch, then peeled tag, then tag), exported to `docker compose` |
| Managed render | the ref | new `render-platform` flag `-source-ref`, default `main` with the AC-10 warning; `task managed:render` passes the Taskfile's `SOURCE_REF` (a commit, or `local` with changes) |
| Managed render | which modules | every module of `deploy/managed/graph.yaml` the shared predicate matches (all four today) |
| Managed render | Dockerfile bytes | `catalog.FS`, which `render-platform` already uses |
| `setup.sh` | the image tag to build | the module's catalog image, written by the render |
| `setup.sh` | local source folder | `MP_SOURCE_PATH` from the environment of whoever runs `setup.sh`, made absolute, must hold `go.work` |
| `setup.sh` | `MP_SOURCE_REF` of a local build | the constant `local` (AC-12), never the zip's ref |

**Key invariants**:
- Every catalog module whose image starts with `ghcr.io/racherom/modular-platform/` has a `Dockerfile` beside its `module.yaml` (a catalog test). The catalog loader reads only `module.yaml` and the templates it declares, so the Dockerfile changes nothing the catalog API returns.
- `libs/render` stays pure: the Dockerfile bytes and the ref come in as a render option (`render.WithSource`), which replaces `render.WithBuilds`; the render reads no file.
- A render whose graph has no `0.0.0` platform module is byte identical to today's. `task managed:zip`'s "same version gives the same bytes" holds for released versions; at `0.0.0` the bytes also depend on the commit.
- An image tagged `:<version>` holds exactly the release commit of `<version>`, and is pushed once. Main names a version whose images are missing only between the release merge and the end of the `images` job. While `RELEASES_ENABLED` is off no version changes on main; never bump the version or the catalog tags by hand in a pull request.
- No image label, renderer log or zip names a commit its code does not come from: a local checkout build says `local`.
- Every input a platform image build fetches from outside the repository is addressed by digest, in the four catalog Dockerfiles.

**Security model**: the source is fetched over https from the public repository at a commit, which addresses its content exactly, and the frontend and base images by digest; no credentials reach a zip or `setup.sh`. The release `images` job gets `packages: write` only on a push to `main` that created a release, which no fork and no pull request can trigger. `github.token` never opens or approves a pull request, so the repository setting that allows it stays off. A `:<version>` tag is pushed once and never replaced (the GHCR check); provenance records the release commit. Renovate gets write access through its GitHub App and opens pull requests only, which go through the same CI and review as any other.

**Configuration required**:
- Build args `MP_SOURCE` (`local` or `git`, default `local`) and `MP_SOURCE_REF` (a full commit or a branch, default `main`) on all four Dockerfiles. These are build inputs, not runtime settings, so `internal/config` does not parse them.
- `MP_SOURCE_REF` in `deploy/online/versions.env`, default `main`.
- `MP_SOURCE_PATH` read by `setup.sh` only, optional.
- The repository variable `RELEASES_ENABLED`, unset until a release is near (AC-11).
- The repository secret `RELEASE_PLEASE_TOKEN` (a fine grained token or a GitHub App token, contents and pull request write), set before `RELEASES_ENABLED` turns `true`; without it the release job fails (AC-11) and AC-9 cannot hold.
- The Renovate GitHub App, installed on this repository once (it reads `renovate.json`; with that file present it opens no onboarding pull request). Without it AC-13's pins still hold, they just stop being refreshed.
- No required check for the release images any more; if *Release images to GHCR* was made required on `main`, remove it, or every pull request waits for a check that never reports.

**Critical test scenarios**:
- Render golden: a graph with `renderer` and `postgres` gives `build/renderer/Dockerfile` equal to the catalog file, an unchanged `image:` line, and the build block in `setup.sh`; verifies **AC-1**, **AC-5**.
- e2e (build tag `e2e`): render that graph, run `setup.sh` with `MP_SOURCE_PATH` set to the repo root, then `docker compose up --wait`; no pull of a platform image; verifies **AC-2**, **AC-3**.
- `setup.sh` with an unknown ref and no `MP_SOURCE_PATH`: non zero exit, no `secrets/` folder, message names the ref and `MP_SOURCE_PATH`; `MP_SOURCE_PATH` pointing at a folder without `go.work`: the same; verifies **AC-3**, **AC-4**.
- Ref check: a short commit, a ref with a space, `'` or `#`, and an empty build arg; the renderer refuses the first three at startup and uses `main` with a warning for the last; verifies **AC-10**.
- Managed: `task managed:zip VERSION=0.0.0`, install on a clean host; every platform service healthy with no platform pull, also after a keeper restart (reconcile runs `compose pull`); verifies **AC-6**.
- After the release: render the same graph with the catalog at a released version; no `build/` folder; verifies **AC-9**.
- Release gate: a push to `main` with `RELEASES_ENABLED` unset gives a passing Release run with all three jobs skipped; verifies **AC-11**.
- Release workflow test (`release_test.go`): `release.yml` has the `images` job with `needs: release-please`, the `created` condition, `packages: write` on it alone, `MP_SOURCE_REF` from `github.sha`, the version through `env:`, and the GHCR exists step before the build; `managed-zip` needs `images`; `release-images.yml` is gone; verifies **AC-9**.
- Local ref: `NewSourceRef("local")` is accepted and `Local()`; a golden zip rendered with `local` holds the README text and the early stop; `setup.sh` from it without `MP_SOURCE_PATH` exits non zero before any file and names `MP_SOURCE_PATH`; the e2e run with `MP_SOURCE_PATH` gives an image whose `org.opencontainers.image.revision` is `local`; verifies **AC-3**, **AC-12**.
- Online ref: a test of the resolve step (a shell script under `scripts/`, so it can run without the task) with a commit, `main` and an unknown ref against a local bare repository passed as the remote; verifies **AC-8**.
- Pins: a test reads the four catalog Dockerfiles and fails on a `# syntax=` or registry `FROM` line without `@sha256:`; the prerelease golden test compares `build/renderer/Dockerfile` with `catalog/modules/renderer/Dockerfile` read at test time; verifies **AC-13**, **AC-1**.

## Build plan

<!-- BUILD SPEC -->
Approach: Tracer Bullet. Task 2 runs the whole thread (catalog Dockerfile, render, zip, `setup.sh`, compose up) for the static zip; later tasks thicken the managed, online and release strands.

1. Move the four Dockerfiles to `catalog/modules/<module>/Dockerfile` with the source stages, the revision label and the renderer's `sourceRef`; add `build` and `dist` to `.dockerignore`; point `Taskfile.yml` (`build`, the api `:dev` build), `deploy/online/compose.dev.yaml`, `deploy/managed/compose.dev.yaml` and `release.yml` at them, then `git grep` the old paths (scripts, READMEs, `AGENTS.md` files, the Dockerfile header comments) until none is left; `task build` passing `MP_SOURCE_REF`; the catalog test for the invariant; the ref check and the startup log, satisfies **AC-7**, **AC-10**
2. Static zip thread: the shared predicate and `render.WithSource` in `libs/render` (replaces `WithBuilds` and `builds.go`), `build/<module>/Dockerfile` in the zip, the build block and README section in `setup.sh.tmpl`, renderer wiring; golden and handler tests; the e2e run with `MP_SOURCE_PATH`, satisfies **AC-1**, **AC-2**, **AC-3**, **AC-4**, **AC-5**
3. Managed zip: `-source-ref` on `render-platform` (after the `-image-tag` retag), the same option in `render.Platform`, `pull_policy: never` on matched services, the build block before the state folder and the seed, `task managed:render` passing the ref; check that the keeper's reconcile and pull skip those services; a verify run with `VERSION=0.0.0` including a keeper restart, satisfies **AC-6**
4. Online editor: `compose.prerelease.yaml`, `MP_SOURCE_REF` in `versions.env`, `task online:up` adding the overlay and `--build` while `MP_VERSION` is `0.0.0`, the overlay versus `compose.yaml` test, satisfies **AC-8**
5. (Replaced by task 8.) Release order: the release pull request images workflow (the `images` job moved out of `release.yml`), the image check for both platforms before the managed zip upload, the required check set on `main`, satisfies **AC-9**
6. (Its README step on the required check is replaced by task 8.) Release gate: `if: vars.RELEASES_ENABLED == 'true'` on the release-please job, the *Release token is set* step, the `github.token` fallback removed, the job's permissions down to `contents: read`; the README *Releases* section rewritten to say: set the secret `RELEASE_PLEASE_TOKEN` (required, no fallback), make *Release images to GHCR* required on `main`, then set the repository variable `RELEASES_ENABLED=true` and push or rerun the Release workflow; the setting that lets GitHub Actions create and approve pull requests stays off; the paragraph on the release images and the image check stays, satisfies **AC-11**

Tasks 1 to 6 are built. The review findings of 2026-11-13 add tasks 7 to 10 (all built); each thickens one strand that already works end to end.

7. The `local` ref (finding #4): `local` in `render.SourceRef` (`Local()`), the renderer's warning, the README text and the early stop in `setup.sh.tmpl`, `MP_SOURCE_REF=local` in the local build line, the Taskfile `SOURCE_REF` rule; goldens for a `local` zip, the e2e label check, the `setup.sh` stop test, the `task managed:zip` warning, satisfies **AC-3**, **AC-10**, **AC-12**
8. Release images after the merge (finding #2): the `images` job in `release.yml` with the GHCR exists check, `managed-zip` needing it, `release-images.yml` deleted, `release_test.go` updated (its check of the old workflow replaced, not kept); the comments that describe the old order rewritten (the managed zip job in `release.yml`, the header of `scripts/release-images-check.sh`); README *Releases*: drop the required check step and rewrite the release images paragraph (images are pushed right after the merge, a few minutes in which main names them before they exist, rerun only failed jobs); remove *Release images to GHCR* from the required checks of `main` if it was added, satisfies **AC-9**, **AC-11**
9. Pinned build inputs (finding #5, first half): digests on every `# syntax=` and registry `FROM` line of the four catalog Dockerfiles, `renovate.json`, the validator in `task lint`, the pin test, the prerelease golden comparing against the catalog file instead of a stored copy; install the Renovate GitHub App, satisfies **AC-13**, **AC-1**
10. The online editor at a commit (finding #5, second half): `scripts/online-source-ref.sh` (resolve, or stop), `task online:up` exporting its output, the overlay header comment, the resolve test, satisfies **AC-8**

## Consequences

<!-- BUILD SPEC -->
**Positive**:
- Every zip made before the release runs without a registry, and builds the exact code that rendered it.
- One Dockerfile per image for every build path.
- Every release image is built from the release commit itself and is never replaced; no repository setting has to stay on for that to hold.
- An image label, a renderer log and a zip never claim a commit their code does not come from.
- Two builds of the same commit use the same frontend and base images, and the online editor's zips name a fixed commit.

**Negative / tradeoffs**:
- For a few minutes after each release merge, main, the GitHub release and `versions.env` name images that are still being pushed; a pull in that window fails until the push ends. If the `images` job fails, that window lasts until someone reruns the failed jobs.
- A zip made by a renderer built from a checkout with changes (`local`) needs `MP_SOURCE_PATH`; so does a managed zip from `task managed:render` on such a checkout. Commit first for a zip that builds from GitHub.
- The branch or tag name `local` is reserved.
- Renovate is one more GitHub App with write access, and its digest pull requests are a small, steady review stream. Tag bumps stay manual.
- `task online:up` needs GitHub at start while `MP_VERSION` is `0.0.0`, and every run may build a newer main.
- A first `setup.sh` takes minutes and needs BuildKit and GitHub access; an unpushed ref fails until you set `MP_SOURCE_PATH`.
- The release depends on `RELEASE_PLEASE_TOKEN`; once `RELEASES_ENABLED` is `true`, a missing token fails the release job loudly instead of falling back to `github.token`.
- AC-9 can only be verified at the first release, since it needs a real release merge; #46 closes on the other criteria and the AC-9 steps carry over as the release checklist in `verify.md`.
- `docker build -f services/<name>/Dockerfile` stops working; contributors use the new path.

**Neutral**:
- The pre release path goes quiet at the first release (no entry carries `0.0.0`); the build args stay, since they serve CI and the online editor anyway.
- A managed deploy of a user stack that places a `0.0.0` platform module still pulls and fails; the deployer gets no build step. Out of scope.

## Migration plan

**Strategy**: big bang, pre release; nothing outside this repository uses these paths.
**Phases**:
1. Tasks 1 to 6 land in order (built); the uncommitted `builds.go`, `WithBuilds` and their tests are replaced in task 2, not committed.
2. Tasks 7 to 10 land in any order (each is independent); task 8 removes *Release images to GHCR* from the required checks of `main` in the same step that deletes `release-images.yml`, and task 9 installs the Renovate GitHub App after `renovate.json` is on `main`.
3. Before the first release, set `RELEASE_PLEASE_TOKEN`, then set `RELEASES_ENABLED` to `true`; after the merge of the release pull request, watch the `images` job and rerun failed jobs if one fails.
**Rollback**: revert the commits; images and zips go back to today's state. Rolling back task 8 alone brings back `release-images.yml` and needs its required check again.
**Risks**: a moved Dockerfile path missed in some script (task 1 greps for the old paths); the first `images` job failing on permissions after a real merge (watch its first run; rerun failed jobs); a stale required check left on `main` after task 8, which blocks every pull request.

## Follow-up

- [ ] After the first release, remove the pre release render path (`WithSource`, the build blocks, `compose.prerelease.yaml`), or keep it if a later need for unreleased builds appears.
- [ ] Update `AGENTS.md` (the image line under Stack) and `deploy/online/AGENTS.md` for the new Dockerfile path and `MP_SOURCE_REF` (`/sync` owns this).
- [ ] `verify.md`: the AC-9 release checklist still describes the push from the release pull request; rewrite it for the push after the merge, and add AC-12 and AC-13 (`/check verify` owns it).
- [ ] `AGENTS.md` Tooling: name Renovate (digests of the four catalog Dockerfiles only) next to the other enforced tools (`/sync` owns this).
- [ ] Consider widening Renovate later to the GitHub Actions SHAs, `versions.env` (`CADDY_IMAGE`) and the test Dockerfiles; out of scope here.
- [ ] Review nit carried over: tests of `.github/workflows` and `scripts/` live in `libs/render/release_test.go`; a top level `ci` test package would fit the bounded context rule better (task 8 touches that file).
