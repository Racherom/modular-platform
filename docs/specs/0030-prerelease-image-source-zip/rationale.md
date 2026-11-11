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

## Rationale

The force that decides it is that the image must be built from the code whose catalog rendered the zip. Option 1 breaks that by design (`#main` moves), and it never reaches the managed `setup.sh`, which needs the deployer and api images before Compose runs. Option 2 keeps the link but pays a registry push per commit for a window that ends at the first release, and still leaves unpushed work without an image. Option 3 keeps the link with a git ref, a fixed content address, at no CI cost. Because `setup.sh` builds first, it serves both zips with one mechanism. Option 4 removes the GitHub dependency but makes the renderer ship its own source as data, which costs more than it saves for a short window.

The one Dockerfile with a build arg source (not a second copy, not a generated twin) is what makes Option 3 cheap to keep honest: the zip ships the very file CI and `task build` use, so the Go and Node versions and the build steps cannot drift. Moving it beside the catalog entry puts it where the catalog already describes the image, and where `go:embed` already reaches.

Pushing images from the release pull request closes the second gap at its root: the merge, which flips the catalog tags, the managed zip and `versions.env`, becomes the last step, so main never names a missing image. The cost is that the release pull request needs CI (the `RELEASE_PLEASE_TOKEN` secret, as the README already notes) and that its version tag is overwritten while the pull request changes. Both are acceptable before anyone pulls that tag.
