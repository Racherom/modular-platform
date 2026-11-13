# Verify: Pre release image source for the zip · spec 0030 · updated 2026-11-13
_Steps derived from spec 0030 acceptance criteria and its Value sourcing table. `/check verify` runs these; `/test` locks the durable ones._

Steps marked **(after push)** need this branch on GitHub, because a git build can only fetch a pushed commit. Steps marked **(on GitHub)** need GitHub. Steps marked **(at release)** need the release pull request and the repository settings of a real release, so they are a checklist for the first release: #46 can close without them, and they stay open here until then. Each of them has a check you can run now, without a release: `go test -race ./libs/ci/ -run 'TestRelease|TestCIImages'` reads the workflow files, and `scripts/release-images-check.sh` runs against a local registry (the step below shows how). The AC-11 steps run against `main`, so they wait until task 6 is merged.

## UI / manual

- [x] Open the online editor, place `renderer` and `postgres`, download the zip, unzip it: it holds `build/renderer/Dockerfile` and no other `build/` file; `docker-compose.yml` names `ghcr.io/racherom/modular-platform/renderer:0.0.0` with no `build:` and no `pull_policy` → AC-1
- [x] In the same zip, `README.md` has the section *Images built on this host*, naming the image, the ref and `MP_SOURCE_PATH`, and no date or time → AC-5
- [x] Download the sample zip (no platform module): no `build/` folder, `setup.sh` has no `build_image` line → AC-1
- [x] **(after push)** On a host without the platform images, run `sh setup.sh` in that zip with no `MP_SOURCE_PATH`: it builds the renderer from GitHub at the renderer's commit before it prints `added   MP_UID`; then `docker compose up -d --wait --pull never` starts the stack healthy → AC-2
- [ ] With a clean tree, make an empty commit on your branch (not pushed), then run `MP_SOURCE_PATH=. task online:up`: it prints `online:up: building web and renderer from the checkout <abs path> at <that commit>`, both start healthy, and a zip you download from the editor holds `source_ref='<that commit>'` in `setup.sh`; with an untracked file in the tree the same run says `local` → AC-8 (replaces the old **(after push)** GitHub step, spec 0030 task 11)
- [x] With `MP_VERSION=dev task online:up` (or any released version): no overlay, no build, the images are only pulled or used → AC-8

## Commands

- [x] `docker build -f catalog/modules/<module>/Dockerfile .` at the repo root, for `api`, `renderer`, `web`, `deployer-docker`: each builds, and `docker image inspect` shows the label `org.opencontainers.image.revision` (`task images` passes `git rev-parse HEAD`) → AC-7 (passed locally on 2026-11-11)
- [x] `docker buildx build --build-arg MP_SOURCE=git --build-arg MP_SOURCE_REF=<pushed commit> -f catalog/modules/api/Dockerfile <empty folder>`: builds the code at that commit from GitHub, with any build context → AC-7 (passed at commit 816bfbe on 2026-11-12, empty context)
- [x] `docker run --rm -e MP_INSTANCE=renderer -e MP_RENDERER_MODE=public <renderer image>`: the `renderer starting` line carries `source_ref` equal to the build's commit; a renderer built with `MP_SOURCE_REF=main` (or none) also logs a warning about a moving branch → AC-10
- [x] `go test -race ./services/renderer/... -run TestSourceRef` and `go test -race ./libs/render/ -run TestNewSourceRef`: a short commit, a ref with a space, `'` or `#` stops the start; an empty ref uses `main` with a warning → AC-10
- [x] `go run ./catalog/cmd/render-platform -source-ref 'a b'` exits 1 naming the ref; with no `-source-ref` it prints a warning and renders with `main` → AC-10
- [x] `go test -race -tags e2e -run Prerelease ./libs/render/`: setup.sh builds from `MP_SOURCE_PATH` (given relative), `up --pull never` starts healthy; an unknown ref, an `MP_SOURCE_PATH` without `go.work` and one that is no folder each exit non zero, name the image, the ref and `MP_SOURCE_PATH`, and leave `.env` unwritten with no `secrets/` or `data/` → AC-2, AC-3, AC-4 (passed locally on 2026-11-11)
- [x] On a host with Docker older than 23, or without the buildx plugin, `sh setup.sh` of a zip with a build stops before it writes `.env`, naming the image, the ref and `MP_SOURCE_PATH` → AC-4
- [x] `go test -race ./libs/render/ -run 'Prerelease|Unreleased'`: the golden holds, two renders give the same bytes, a branch ref adds the README warning, a released image builds nothing → AC-1, AC-5
- [x] `task managed:zip VERSION=0.0.0`, unzip into a new folder, `printf '<domain>\n\n<email>\n<user>\n<password>\n' | MP_SOURCE_PATH=<repo> sh setup.sh`, then `docker compose up -d --wait`: the zip holds four `build/*/Dockerfile`, four `pull_policy: never`; every platform service is healthy; `docker compose pull` prints `Skipped` for the four platform images → AC-6 (passed locally on 2026-11-11)
- [x] `task managed:render MANAGED_IMAGE_TAG=dev` (and `ci`): `build/managed` has no `build/` folder and no `pull_policy` → AC-6
- [ ] After spec 0016 lands: install the `0.0.0` managed zip through the seed and keeper, restart the keeper, and see no platform pull and every service healthy; rerun `setup.sh` with a new ref and see the keeper hand over → AC-6
- [x] `go test -race ./libs/ci/ -run TestOnlinePrereleaseOverlay`: the overlay builds exactly the platform services of `compose.yaml` → AC-8
- [ ] **(on GitHub)** With `RELEASES_ENABLED` unset, push to `main`: the Release run passes, *release-please* and *Managed zip on the release* show as skipped → AC-11
- [x] `go test -race ./libs/ci/ -run TestReleaseWorkflow`: release-please runs only with `RELEASES_ENABLED`, checks `RELEASE_PLEASE_TOKEN` first and names it, passes only that secret as its token, never `github.token`, and has only `contents: read`; the managed zip job is skipped with it (no `always()`) and runs `scripts/release-images-check.sh` before `task managed:zip` → AC-11, AC-9 (passed on 2026-11-12; breaking each of these in the file makes the test fail)
- [x] `go test -race ./libs/ci/ -run TestReleaseImagesWorkflow`: the images job runs only for this repository's `release-please--` branch, alone has `packages: write`, builds all four images from the head commit at the manifest's version for amd64 and arm64 with provenance and SBOM, and the gate *Release images to GHCR* passes when the images job succeeded or was skipped and fails otherwise → AC-9 (passed on 2026-11-12)
- [x] Run a local registry (`docker run -d --rm -p 127.0.0.1:5055:5000 registry:2`), push an index per image with `docker buildx imagetools create`, then `scripts/release-images-check.sh <version> localhost:5055/<prefix>`: it passes with all four for amd64 and arm64 (also arm64 listed as `linux/arm64/v8`), and exits 1 naming the image when one is missing, has only amd64, or has `arm/v7` but no arm64 → AC-9 (passed on 2026-11-12; the run found that `linux/arm64/v8` failed the check, now fixed)
- [ ] **(on GitHub)** The repository setting "Allow GitHub Actions to create and approve pull requests" is off (that `release.yml` names no `github.token` for release-please is covered by `TestReleaseWorkflow` above) → AC-11
- [ ] **(on GitHub)** In a fork or test repository with Actions turned on, set `RELEASES_ENABLED=true` with no `RELEASE_PLEASE_TOKEN` and push: the step *Release token is set* fails with its own message naming the secret, and the release-please step never runs → AC-11
- [ ] **(at release)** Set `RELEASE_PLEASE_TOKEN`, then `RELEASES_ENABLED=true`, then push to `main` or rerun the latest Release run (the variable alone starts nothing). The first pull request lists every commit since the start and releases `0.1.0`; read its changelog before you merge. Let release-please open or update its pull request: the job *Release images to GHCR* pushes all four `:<version>` images for amd64 and arm64 with provenance and SBOM, again on each update → AC-9
- [ ] **(at release)** Make *Release images to GHCR* a required check on `main`; on any other pull request the job shows as skipped and the pull request can merge → AC-9
- [x] A pull request from a fork whose branch is named `release-please--x` does not run the job (`TestReleaseImagesWorkflow` checks that the job runs only when the head repository is this one; breaking that check fails the test) → AC-9
- [ ] **(at release)** After the release merge: the *Managed zip on the release* job passes its image check and attaches the zip (the failing case, a missing image, is covered by the local registry step above, so no test repository is needed) → AC-9
- [x] Render the sample with a `renderer` module from a released catalog: no `build/` folder and no `build_image` in `setup.sh` (`TestUnreleased`, the *released image* case, renders with the tag `0.0.1`; at the release, a look at one zip confirms it) → AC-9

## Value sourcing

- [x] Which modules get a Dockerfile: change a test catalog's renderer image to `:0.0.1`, `:dev` and another registry; none gets a build (`TestUnreleased`, the *released image* case) → Static render, which modules
- [x] Dockerfile bytes: `cmp` the zip's `build/renderer/Dockerfile` with `catalog/modules/renderer/Dockerfile` → Static render, Dockerfile bytes
- [x] Path in the zip: a graph with `renderer` and `renderer-2` gives one `build/renderer/Dockerfile` (the prerelease golden) → Static render, path
- [x] Ref in setup.sh and README: a renderer built from commit A gives zips with `source_ref='A'`; one built with no ref gives `main` and the README warning → Static render, the ref
- [x] `task build` ref: with an uncommitted change, `task images` still labels the images with `git rev-parse HEAD` → `task build`
- [x] Release pull request ref and version: the pushed images' revision label is the pull request head commit, and the tag is the manifest's version at that head (`TestReleaseImagesWorkflow`: checkout and `MP_SOURCE_REF` are `github.event.pull_request.head.sha`, the tag is the version read from `.release-please-manifest.json` after that checkout; the Dockerfiles turn `MP_SOURCE_REF` into the label, checked by the AC-7 step) → Release PR job
- [x] Other CI image builds: the images a push to `main` would build carry `github.sha` (`TestCIImagesSourceRef`: every CI job that builds images checks out the event's commit with no `ref:` and passes no `SOURCE_REF`, and `task images` passes `git rev-parse HEAD`) → Other CI image builds
- [x] Online editor ref: change `MP_SOURCE_REF` in `versions.env` and `docker compose config` shows the new ref in both build contexts and args → Online editor
- [x] Managed render ref: `task managed:render` writes `source_ref='<HEAD>'`; `render-platform` with no flag writes `main` → Managed render, the ref
- [x] Managed render modules: all four platform modules of `deploy/managed/graph.yaml` get a Dockerfile, no other module does → Managed render, which modules
- [x] Image tag setup.sh builds: each `build_image` line names the catalog image of its module → setup.sh, image tag
- [x] Local source folder: `MP_SOURCE_PATH=.` from the repo root and `MP_SOURCE_PATH=<absolute path>` both build from that checkout; a folder with no `go.work` is refused → setup.sh, local source folder

## Acceptance-criteria coverage

- AC-1: UI steps 1 and 3, the render rules test, value sourcing (which modules, bytes, path)
- AC-2: UI step 4 (after push), the e2e test (local source)
- AC-3: the e2e test, value sourcing (local source folder)
- AC-4: the e2e stop cases, the old Docker step
- AC-5: UI step 2, the render rules test
- AC-6: the managed zip run, the retag step, the keeper step (after spec 0016)
- AC-7: the two Dockerfile build steps
- AC-8: UI steps 5 and 6 (step 5 now from a checkout, no push), the overlay test, the checkout tests, value sourcing (online editor ref)
- AC-9: `TestReleaseImagesWorkflow`, `TestReleaseWorkflow`, the local registry check and `TestUnreleased` now; the three remaining **(at release)** steps at the first release, which do not block #46
- AC-10: the start log step, the two ref tests, the render-platform step
- AC-11: `TestReleaseWorkflow` now; the three **(on GitHub)** steps after task 6 is on `main`

## Review findings (tasks 7 to 10) · added 2026-11-13

### The `local` ref (AC-3, AC-10, AC-12)
- [x] `go test -race ./libs/render/ -run 'TestNewSourceRef|TestRenderPrereleaseLocal|TestRenderPlatformPrerelease'` → passes: `local` is accepted and `Local()`, the `local` zip README says `source ref \`local\``, its `setup.sh` without `MP_SOURCE_PATH` exits non zero before the BuildKit check, writes no `MP_UID` to `.env` and makes no `secrets/` or `data/` → AC-12
- [x] `go test -race ./services/renderer/internal/api/ -run TestSourceRef` → `local` logs `source_ref=local` with exactly one warning, the one that names `MP_SOURCE_PATH`, and no branch warning → AC-10, AC-12
- [x] `go test -race -tags e2e ./libs/render/ -run TestPrerelease` (Docker 23 or newer) → `setup.sh` with `MP_SOURCE_PATH` prints `building … from the local checkout <path>` and the built renderer image has `org.opencontainers.image.revision=local` → AC-3, AC-12
- [x] On a tree with an uncommitted change, `task managed:zip VERSION=0.0.0` prints `managed:zip: the tree has uncommitted changes, so this zip builds only with MP_SOURCE_PATH …` to stderr and still writes the zip; `unzip -p dist/modular-platform-0.0.0.zip '*/setup.sh' | grep "source_ref='local'"` matches → AC-12
- [x] On a clean tree, the same `task managed:zip VERSION=0.0.0` prints no such warning and its `setup.sh` holds `source_ref='<git rev-parse HEAD>'` → AC-12 (value sourcing: `task build` and `task managed:render` ref)

### Release images after the merge (AC-9, AC-11)
- [x] `go test -race ./libs/ci/ -run 'TestReleaseImagesJob|TestReleaseWorkflow|TestReleaseRunSteps'` → `release.yml` has the `images` job (needs release-please, the `created` condition, `fail-fast: false`, the only `packages: write`, checkout of `github.sha`, `VERSION` through `env:`, the GHCR check right before the push as the last step), `managed-zip` needs `[release-please, images]`, and `release-images.yml` is gone → AC-9
- [ ] **(on GitHub)** In the repository settings of `main`, *Release images to GHCR* is not a required check (remove it if it was added) → AC-9, AC-11
- [ ] **(at release)** After the release pull request merges, the `images` job runs four matrix jobs, each prints `<image> is not in GHCR yet`, then pushes; `scripts/release-images-check.sh <version>` passes; each image's `org.opencontainers.image.revision` is the merge commit; *Re-run failed jobs* on a finished image job stops with `is already in GHCR; a released image is never pushed again` → AC-9

### Pinned build inputs (AC-13, AC-1)
- [x] `go test -race ./catalog/ -run 'TestPlatformDockerfilesPinned|TestUnpinned'` → passes; change one `FROM` in `catalog/modules/web/Dockerfile` back to a bare tag and it fails, naming that line → AC-13
- [x] `task lint:renovate` → the validator accepts `renovate.json` → AC-13
- [x] Change one digest in `catalog/modules/renderer/Dockerfile`, run `go test -race ./libs/render/ -run TestRenderPrereleaseGolden` without `-update` → still passes (the zip's Dockerfile is compared with the catalog file, no stored copy) → AC-1, AC-13
- [x] `task images IMAGE_TAG=pincheck` → all four images build with the pinned frontend and bases → AC-13
- [ ] **(on GitHub)** Install the Renovate GitHub App once `renovate.json` is on `main`; its first grouped pull request *build image digests* touches only the four catalog Dockerfiles and includes the `# syntax=` line → AC-13

### The online editor at a commit (AC-8)
- [x] `go test -race ./libs/ci/ -run TestOnlineSourceRef` → a full commit passes with no network, `main` resolves to its newest commit, a branch wins over a tag of the same name, an annotated tag resolves to its commit, and `local`, a short commit, a bad form, an unknown ref and an unreachable remote stop with their message → AC-8
- [x] `sh scripts/online-source-ref.sh` → prints the same commit as `git ls-remote https://github.com/racherom/modular-platform.git refs/heads/main` → AC-8 (value sourcing: the commit it builds)
- [ ] `go test -race -tags integration ./libs/ci/ -run TestOnlineUpCheckout` (no network needed for the source) → on a fresh empty commit that no remote holds, `task online:up` with `MP_SOURCE_PATH` prints the checkout line with that commit, both images carry it as their revision label, the renderer logs it as `source_ref` with no warning, a zip rendered through the ingress holds `source_ref='<commit>'`, and `deploy/online/versions.env` is unchanged → AC-8 (replaces the old **(after push)** step)
- [ ] `go test -race ./libs/ci/ -run 'TestOnlineSourceRefCheckout|TestOnlineUpCheckoutStops|TestOnlinePrereleaseOverlay'` → a clean clone prints its HEAD, an untracked file or a folder with no git prints `local`, no folder and no `go.work` stop with their message, `MP_SOURCE_REF` and an unreachable `MP_SOURCE_REMOTE` change nothing; `MP_SOURCE_PATH=. MP_VERSION=dev task online:up` stops without calling `docker`; `compose.checkout.yaml` names the same services as `compose.prerelease.yaml` with `MP_SOURCE: local` → AC-8
- [x] `MP_SOURCE_REF=nope task online:up` and `MP_SOURCE_REF=local task online:up` → stop before Compose runs, with a message that names the ref → AC-8

### Coverage of the new steps
- AC-8: online editor steps · AC-9: release images steps · AC-10: renderer `local` warning · AC-12: the `local` ref steps · AC-13: pinned build input steps · AC-1, AC-3: golden without a stored copy, e2e label

## Second review findings · added 2026-11-13
_From `docs/reviews/2026-11-13-feat-release-gate-2.md`. The tests of workflows and scripts moved from `libs/render` to `libs/ci`, so `libs/render` stays a pure domain; the steps above now name `./libs/ci/`._

### The GHCR check (AC-9)
- [x] `go test -race ./libs/ci/ -run 'TestReleaseImageAbsent|TestReleaseImagesJob'` → the images job runs `scripts/release-image-absent.sh` right before the push; with a fake `docker`, only `not found` exits 0, while a tag that exists, `denied`, a 403 and a network error exit 1 → AC-9 (a copy of the script that also passed `denied` fails the `denied` case)
- [x] Without a login, `docker buildx imagetools inspect ghcr.io/racherom/modular-platform/no-such-package-xyz:9.9.9` prints `failed to fetch anonymous token: unexpected status ... 403 Forbidden`, not `not found` (checked 2026-11-13). So GHCR does not say `not found` to everyone; the script stops on this answer, which is safe → AC-9
- [ ] **(on GitHub, before the first release)** With a token like the workflow's (`packages: write`; `echo $TOKEN | docker login ghcr.io -u <user> --password-stdin`), run `sh scripts/release-image-absent.sh ghcr.io/racherom/modular-platform/renderer:0.0.1` while no package exists → prints `... is not in GHCR yet` and exits 0. Keep the printed text here. If it stops with `could not check`, the first release would stop too: fix the `case` in the script on this branch before releasing → AC-9

### One ref rule (AC-8, AC-10)
- [x] `go test -race ./libs/render/ -run TestNewSourceRef` → `CAFE` is a name, `cafe` is refused → AC-10
- [x] `go test -race ./libs/ci/ -run TestOnlineSourceRef` → `CAFE` resolves as a branch, `cafe` stops as a short commit, an uppercase 40 character value is looked up as a name (and stops as unknown), and with no `MP_SOURCE_REF` in the shell a `versions.env` fixture line with a trailing comment is the input → AC-8

### Renovate lint output (AC-13)
- [x] `task lint:renovate </dev/null` → no re2 warning or stack trace; prints `lint:renovate: renovate.json is valid`, exit 0 → AC-13
- [x] The same validator with `LOG_LEVEL=error` on a scratch config with an unknown key prints `Invalid configuration option: bogusKey` and exits 1, so the quieter output still fails a bad config → AC-13
