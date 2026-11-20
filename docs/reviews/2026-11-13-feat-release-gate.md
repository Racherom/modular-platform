# Review, feat/release-gate (spec 0030, pre release image source for the zip), 2026-11-13

**Reviewed by**: Sonnet 5.5 (author on Sonnet 5.5, fresh context)
**Scope**: about 45 files of the feature, branch vs main (narrowed to the 14 commits of spec 0030)
**Verdict**: Approve with nits

## Summary

The change lets a zip, a managed zip and the online editor build the platform's own `0.0.0` images from the Dockerfile in the catalog, from one pinned commit or a local checkout. It also adds a release pull request workflow that pushes the images before the merge, and a gate on release-please. The design follows the spec closely. `libs/render` stays pure, the ref is checked once and quoted safely in `setup.sh`, and the release steps are tested without a release. I found no blocker or major problem, only a few supply chain and release flow gaps worth closing.

## Minor

### 🟡 The release images job can overwrite the tag of an already released version, `.github/workflows/release-images.yml:41`
**Problem**: The version comes from `.release-please-manifest.json` at the pull request head, and the job pushes `:<version>` with no check that the version is new. Any branch that starts with `release-please--` and holds an old version in its manifest overwrites a released image tag.
**Why it matters**: A released tag becomes mutable. Writers to the repo can already change the workflow on their branch, so this is not a new privilege, but a mistaken or stale release branch would silently replace a shipped image.
**Suggested fix**: Before the push, fail the job when the git tag `v<version>` (or the GHCR tag) already exists. Add a test line in `release_test.go` for it.

### 🟡 Images come from the pull request head, the release comes from the merge commit, `.github/workflows/release-images.yml:49`
**Problem**: The images are built from `pull_request.head.sha`. If another commit lands on main after the last push and the pull request is merged without being updated, the tagged release holds code the images lack. Spec 0030 only says the required check must pass for the latest head.
**Why it matters**: The release tag and the image would differ in content, and the provenance would point at a commit that is not the release commit.
**Suggested fix**: Require "branches up to date before merging" for main (document it next to the required check in the README), or make the Release workflow compare `github.sha`'s tree with the image label `org.opencontainers.image.revision` before it attaches the zip.

### 🟡 Shell injection style interpolation in the managed zip job, `.github/workflows/release.yml:69-70`
**Problem**: `needs.release-please.outputs.version` and `.tag` are written straight into the `run:` text. The check step above them correctly passes the version through `env:`.
**Why it matters**: The values come from release-please and are not attacker controlled today, but the pattern is the one the GitHub hardening guide warns about, and the two steps are inconsistent.
**Suggested fix**: Pass version and tag through `env:` and use `"$VERSION"` and `"$TAG"` in the commands.

### 🟡 The built image claims a ref its code does not come from when `MP_SOURCE_PATH` is used, `libs/render/templates/setup.sh.tmpl:48`
**Problem**: With `MP_SOURCE_PATH` the build passes `MP_SOURCE_REF=<zip's ref>`. The code is the local checkout, yet the label `org.opencontainers.image.revision` and the renderer's `source_ref` log carry the zip's ref.
**Why it matters**: A locally built renderer then writes that ref into the zips it renders, which would build different code than the checkout that made the renderer. It also misleads anyone reading the label.
**Suggested fix**: In local mode, pass a clearly marked value (for example `local`) or the checkout's `git rev-parse HEAD`, and let the renderer's ref check accept it. Add a golden or e2e line for it.

### 🟡 Floating build inputs for a build that is described as pinned, `catalog/modules/*/Dockerfile:1`
**Problem**: A commit ref pins the repository source, but `# syntax=docker/dockerfile:1` is a floating frontend image and `golang:1.27.3-alpine` and the distroless base are tags, not digests. The online editor and the managed default also build `MP_SOURCE_REF=main` unless someone edits `versions.env`.
**Why it matters**: Two builds of one zip can differ, and the README promises a fixed commit while the toolchain moves. The branch default makes the online editor's zips build a moving ref.
**Suggested fix**: Pin the frontend and base images by digest (a Renovate or Dependabot rule keeps them fresh), and consider letting `task online:up` write the resolved commit into the build when it is `main`.

### 🟡 Source ref check is looser and stricter than git, `libs/render/source.go:20-38`
**Problem**: `refName` accepts names git refuses (`..`, `a//b`, a trailing `/` or `.lock`, `.`), and `shortCommit` rejects real branch or tag names that are all hex and 4 to 39 characters long (`dead`, `2024`, `cafe`).
**Why it matters**: A bad ref still stops at build time with the AC-4 message, so this is not a safety hole, but an all hex release tag would be refused for the wrong reason.
**Suggested fix**: Reject the git invalid forms with a small table of cases in `source_test.go`, and refuse only hex strings that cannot be a name you use (document that short commits and hex only names are not allowed).

## Nits

- ⚪ `libs/render/release_test.go:67`, tests of `.github/workflows` and `scripts/` live in the `libs/render` package, which is the render context. A top level `ci` test package would fit the bounded context rule better.
- ⚪ `libs/render/source.go:123`, `imageBuilds` ranges a map, so with several modules lacking a Dockerfile the reported module is random. Sort the module names first for a stable error.
- ⚪ `libs/render/source.go:129`, the `pull_policy: never` change sits inside a function named for collecting builds. A short second helper would make that side effect easier to find.

## Strengths

- `libs/render` stays pure: the ref and Dockerfile bytes enter through `WithSource`, `Unreleased` is one predicate shared by the static render, `Platform` and `render-platform`, and output is sorted so two renders give the same bytes.
- The ref is validated once into a `SourceRef` value object, written into `setup.sh` in single quotes (with `shellQuote` for names), and `setup.sh` stops before it makes any file when a build fails.
- AC-11 holds: release-please uses only `RELEASE_PLEASE_TOKEN`, is gated on `RELEASES_ENABLED`, fails with a clear message when the secret is missing, and `packages: write` is granted to one job only. The workflows and the GHCR check script are covered by tests that run without a release.
- After a real release the tags no longer match `Unreleased`, so the build blocks, the `build/` files and `pull_policy` disappear and existing goldens stay unchanged, which a test covers.

## Test coverage

Good. Golden tests cover the static zip, the managed platform folder, a branch ref, a released image (no build), a missing Dockerfile and the byte identical rerun. `source_test.go` has a ref table, the e2e test runs `setup.sh` with `MP_SOURCE_PATH` and the stop cases, and `release_test.go` checks the workflow structure and the check script. Not covered: the `MP_SOURCE_PATH` label and ref mismatch above, a released tag overwrite, and AC-9 after a real release (the spec already says that one is checked at the first release).
