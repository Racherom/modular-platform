# Verify: Pre release image source for the zip · spec 0030 · updated 2026-11-12
_Steps derived from spec 0030 acceptance criteria and its Value sourcing table. `/check verify` runs these; `/test` locks the durable ones._

Steps marked **(after push)** need this branch on GitHub, because a git build can only fetch a pushed commit. Steps marked **(on GitHub)** need the release pull request or the repository settings.

## UI / manual

- [ ] Open the online editor, place `renderer` and `postgres`, download the zip, unzip it: it holds `build/renderer/Dockerfile` and no other `build/` file; `docker-compose.yml` names `ghcr.io/racherom/modular-platform/renderer:0.0.0` with no `build:` and no `pull_policy` → AC-1
- [ ] In the same zip, `README.md` has the section *Images built on this host*, naming the image, the ref and `MP_SOURCE_PATH`, and no date or time → AC-5
- [ ] Download the sample zip (no platform module): no `build/` folder, `setup.sh` has no `build_image` line → AC-1
- [ ] **(after push)** On a host without the platform images, run `sh setup.sh` in that zip with no `MP_SOURCE_PATH`: it builds the renderer from GitHub at the renderer's commit before it prints `added   MP_UID`; then `docker compose up -d --wait --pull never` starts the stack healthy → AC-2
- [ ] **(after push)** Set `MP_SOURCE_REF` in `deploy/online/versions.env` to the pushed commit and run `task online:up` from a folder that holds only `deploy/online/` (or with the compose command in the overlay's header): web and renderer build from GitHub and start healthy; a zip from that editor names that commit in `setup.sh` → AC-8
- [ ] With `MP_VERSION=dev task online:up` (or any released version): no overlay, no build, the images are only pulled or used → AC-8

## Commands

- [ ] `docker build -f catalog/modules/<module>/Dockerfile .` at the repo root, for `api`, `renderer`, `web`, `deployer-docker`: each builds, and `docker image inspect` shows the label `org.opencontainers.image.revision` (`task images` passes `git rev-parse HEAD`) → AC-7 (passed locally on 2026-11-11)
- [ ] `docker buildx build --build-arg MP_SOURCE=git --build-arg MP_SOURCE_REF=<pushed commit> -f catalog/modules/api/Dockerfile <empty folder>`: builds the code at that commit from GitHub, with any build context → AC-7 (checked against `main` on 2026-11-11; repeat with a commit once the new Dockerfiles are pushed)
- [ ] `docker run --rm -e MP_INSTANCE=renderer -e MP_RENDERER_MODE=public <renderer image>`: the `renderer starting` line carries `source_ref` equal to the build's commit; a renderer built with `MP_SOURCE_REF=main` (or none) also logs a warning about a moving branch → AC-10
- [ ] `go test -race ./services/renderer/... -run TestSourceRef` and `go test -race ./libs/render/ -run TestNewSourceRef`: a short commit, a ref with a space, `'` or `#` stops the start; an empty ref uses `main` with a warning → AC-10
- [ ] `go run ./catalog/cmd/render-platform -source-ref 'a b'` exits 1 naming the ref; with no `-source-ref` it prints a warning and renders with `main` → AC-10
- [ ] `go test -race -tags e2e -run Prerelease ./libs/render/`: setup.sh builds from `MP_SOURCE_PATH` (given relative), `up --pull never` starts healthy; an unknown ref, an `MP_SOURCE_PATH` without `go.work` and one that is no folder each exit non zero, name the image, the ref and `MP_SOURCE_PATH`, and leave `.env` unwritten with no `secrets/` or `data/` → AC-2, AC-3, AC-4 (passed locally on 2026-11-11)
- [ ] On a host with Docker older than 23, or without the buildx plugin, `sh setup.sh` of a zip with a build stops before it writes `.env`, naming the image, the ref and `MP_SOURCE_PATH` → AC-4
- [ ] `go test -race ./libs/render/ -run 'Prerelease|Unreleased'`: the golden holds, two renders give the same bytes, a branch ref adds the README warning, a released image builds nothing → AC-1, AC-5
- [ ] `task managed:zip VERSION=0.0.0`, unzip into a new folder, `printf '<domain>\n\n<email>\n<user>\n<password>\n' | MP_SOURCE_PATH=<repo> sh setup.sh`, then `docker compose up -d --wait`: the zip holds four `build/*/Dockerfile`, four `pull_policy: never`; every platform service is healthy; `docker compose pull` prints `Skipped` for the four platform images → AC-6 (passed locally on 2026-11-11)
- [ ] `task managed:render MANAGED_IMAGE_TAG=dev` (and `ci`): `build/managed` has no `build/` folder and no `pull_policy` → AC-6
- [ ] After spec 0016 lands: install the `0.0.0` managed zip through the seed and keeper, restart the keeper, and see no platform pull and every service healthy; rerun `setup.sh` with a new ref and see the keeper hand over → AC-6
- [ ] `go test -race ./libs/render/ -run TestOnlinePrereleaseOverlay`: the overlay builds exactly the platform services of `compose.yaml` → AC-8
- [ ] **(on GitHub)** Set the repository secret `RELEASE_PLEASE_TOKEN`, then let release-please open or update its pull request: the job *Release images to GHCR* pushes all four `:<version>` images for amd64 and arm64 with provenance and SBOM, again on each update → AC-9
- [ ] **(on GitHub)** Make *Release images to GHCR* a required check on `main`; on any other pull request the job shows as skipped and the pull request can merge → AC-9
- [ ] **(on GitHub)** A pull request from a fork whose branch is named `release-please--x` does not run the job → AC-9
- [ ] **(on GitHub)** After the release merge: the *Managed zip on the release* job passes its image check and attaches the zip; with one image deleted from GHCR (in a test repository) it fails naming that image → AC-9
- [ ] After the release: render the sample with a `renderer` module from the released catalog: no `build/` folder and no `build_image` in `setup.sh` → AC-9

## Value sourcing

- [ ] Which modules get a Dockerfile: change a test catalog's renderer image to `:0.0.1`, `:dev` and another registry; none gets a build (`TestUnreleased`, the *released image* case) → Static render, which modules
- [ ] Dockerfile bytes: `cmp` the zip's `build/renderer/Dockerfile` with `catalog/modules/renderer/Dockerfile` → Static render, Dockerfile bytes
- [ ] Path in the zip: a graph with `renderer` and `renderer-2` gives one `build/renderer/Dockerfile` (the prerelease golden) → Static render, path
- [ ] Ref in setup.sh and README: a renderer built from commit A gives zips with `source_ref='A'`; one built with no ref gives `main` and the README warning → Static render, the ref
- [ ] `task build` ref: with an uncommitted change, `task images` still labels the images with `git rev-parse HEAD` → `task build`
- [ ] Release pull request ref and version: the pushed images' revision label is the pull request head commit, and the tag is the manifest's version at that head → Release PR job
- [ ] Other CI image builds: the images a push to `main` would build carry `github.sha` → Other CI image builds
- [ ] Online editor ref: change `MP_SOURCE_REF` in `versions.env` and `docker compose config` shows the new ref in both build contexts and args → Online editor
- [ ] Managed render ref: `task managed:render` writes `source_ref='<HEAD>'`; `render-platform` with no flag writes `main` → Managed render, the ref
- [ ] Managed render modules: all four platform modules of `deploy/managed/graph.yaml` get a Dockerfile, no other module does → Managed render, which modules
- [ ] Image tag setup.sh builds: each `build_image` line names the catalog image of its module → setup.sh, image tag
- [ ] Local source folder: `MP_SOURCE_PATH=.` from the repo root and `MP_SOURCE_PATH=<absolute path>` both build from that checkout; a folder with no `go.work` is refused → setup.sh, local source folder

## Acceptance-criteria coverage

- AC-1: UI steps 1 and 3, the render rules test, value sourcing (which modules, bytes, path)
- AC-2: UI step 4 (after push), the e2e test (local source)
- AC-3: the e2e test, value sourcing (local source folder)
- AC-4: the e2e stop cases, the old Docker step
- AC-5: UI step 2, the render rules test
- AC-6: the managed zip run, the retag step, the keeper step (after spec 0016)
- AC-7: the two Dockerfile build steps
- AC-8: UI steps 5 and 6, the overlay test, value sourcing (online editor ref)
- AC-9: the four GitHub steps and the after release render
- AC-10: the start log step, the two ref tests, the render-platform step
