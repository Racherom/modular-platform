# Review, feat/release-gate (second pass, spec 0030), 2026-11-13

**Reviewed by**: Sonnet 5.5 (author on Sonnet 5.5, fresh context)
**Scope**: 36 files, branch vs origin/main (merge base c39188a), plus one uncommitted file (`docs/specs/0030-prerelease-image-source-zip/verify.md`)
**Verdict**: Approve with nits

## Summary

This pass checks the fixes for the first review and the new work on top: the `local` source ref, the release images job that now runs after the merge, digest pinned Dockerfiles with Renovate, and the online editor that builds at a resolved commit. All six findings of the first review are fixed in code and covered by tests. I found no blocker and no major problem. What is left is one noisy lint task (your steering note), a small mismatch between the shell and Go ref rules, and a release step that only the first real release can prove.

## Status of the first review

| First review finding | Now |
|---|---|
| Released tag could be overwritten | Fixed. The images job checks GHCR right before the push and stops when the tag exists or when the state is unknown (`release.yml:62-76`). |
| Images built from the PR head, release from the merge commit | Fixed by design. `release-images.yml` is deleted, the images are built from `github.sha` after the merge. |
| Expressions written into `run:` | Fixed. Values go through `env:`, and `TestReleaseRunSteps` fails on any `${{` in a run step. |
| Image claimed a ref its code did not come from | Fixed. `setup.sh` passes `MP_SOURCE_REF=local` for a checkout build, and the Taskfile uses `local` on a dirty tree. |
| Floating build inputs | Fixed. All four Dockerfiles pin the frontend and base images by digest, `TestPlatformDockerfilesPinned` guards it, Renovate keeps it fresh. The online editor resolves a branch to a commit at start. |
| Ref check looser or stricter than git | Fixed. `gitRefName` rejects the git invalid forms, `source_test.go` has a table for them, and the hex name rule is documented. |
| Nits (random module in the error, `pull_policy` helper) | Fixed. `imageBuilds` walks sorted names and `neverPull` is its own helper. The test location nit still stands (see Nits). |

## Minor

### 🟡 `task lint:renovate` prints a long re2 stack trace on success, `Taskfile.yml:114`

**Problem**: This is your steering note. I ran `task lint:renovate` from the repo root with `</dev/null`. It exits 0 and prints 27 lines. The first is `WARN: RE2 not usable, falling back to RegExp: regex validation may be inaccurate`, followed by a `MODULE_NOT_FOUND` error object and a Node stack for `re2/build/Release/re2.node`. Only the last two lines say what you want to know (`Config validated successfully`, `Done`).

**Where it comes from**: Renovate loads the optional native `re2` module and, if it is missing, logs that warning with the full error and falls back to the JavaScript `RegExp`. `pnpm dlx` (pnpm 12) does not run dependency install scripts by default, so `re2` never downloads or builds its `re2.node` file. The warning is real but harmless for this file, because `renovate.json` holds no regex. It only says that regex checks in a config can be less exact.

**Why it matters**: Low. A passing run looks like a failure, and people learn to ignore the output of the task, which is the output you rely on when the validator finds a real error. Where it shows: `lint:renovate` is part of `task lint` (`Taskfile.yml:102`), and `task lint` runs in CI (`.github/workflows/ci.yml:41`), so the trace sits in every CI log. It does not run in the pre commit hook: `lefthook.yml` has go, web and vocabulary jobs only. So the hook output is clean.

**Verified fixes** (I ran each against the pinned `renovate@44.198.3` in a scratch directory with a good copy of `renovate.json` and a throwaway invalid file with a wrong key and a wrong type, never the repo file):

- `LOG_LEVEL=error`: the good file prints only `Done in ...` and exits 0. The invalid file still prints `ERROR: Found errors in configuration` with both messages (`Invalid configuration option: bogusKey`, `packageRules[0].enabled should be boolean`) and exits 1. So the exit code still reflects a bad config. Side effect: the `INFO ... validated successfully` line is hidden too.
- `pnpm dlx --allow-build=re2 ...`: re2 downloads its prebuilt binary (about 8 seconds on a cold cache), the warning is gone, the INFO lines stay, exit 0, and the real RE2 engine makes regex validation exact. It runs one install script that fetches from GitHub releases, which is a small supply chain and network cost in CI.

**Suggested fix**: Simplest is to set `LOG_LEVEL: error` in the task's `env:` and add a closing `echo "renovate.json is valid"` after the command, so a pass still says so. If you would rather keep the INFO lines and exact regex checks, use `--allow-build=re2` instead and accept the install script. Either way, add a short comment above the command that says why.

Also worth knowing (same line): `pnpm dlx` resolves the transitive dependencies of Renovate fresh on every run (612 packages here), so the pinned `RENOVATE_VERSION` does not make the lint reproducible, and a cold CI run downloads a lot. Consider a CI cache for the pnpm store or accepting this cost knowingly.

### 🟡 The online script and the Go renderer disagree on uppercase hex names, `scripts/online-source-ref.sh:36`

**Problem**: The comment says the script refuses short or uppercase commits "as the renderer refuses them". The renderer does not: `shortCommit` in `libs/render/source.go:24` matches lowercase hex only, so `NewSourceRef("CAFE")` and a 40 character uppercase value are accepted as branch names (`source_test.go` even keeps "upper commit" as a valid name). The script refuses both.
**Why it matters**: A branch called `CAFE` works in a zip render and fails in `task online:up`. Rare, and the error is clear, but the comment is wrong and the two rules should be one.
**Suggested fix**: Make the script match the Go rule (lowercase only), or make the Go rule refuse uppercase hex too, and fix the comment. Add one case to each table.

### 🟡 The GHCR "not found" check is unproven for a package that does not exist yet, `.github/workflows/release.yml:67-76`

**Problem**: The guard treats only output containing `not found` as "safe to push". For the very first release the package does not exist in GHCR at all. Whether `docker buildx imagetools inspect` then prints `not found` (and not `denied` or `unauthorized`) is not tested. `TestReleaseImagesJob` only checks that the script text contains the strings, it does not run the case logic.
**Why it matters**: If GHCR answers differently, the first release stops with "could not check whether ... is in GHCR". That fails safe (nothing wrong is pushed), but it would block the first release and the fix means editing a workflow on main. The spec already says AC-9 is checked at the first release, so this is known, not hidden.
**Suggested fix**: Before the first release, run the same `imagetools inspect` against a package name that does not exist, with a token like the workflow's, and keep the printed text in `verify.md`. Optionally move the `case` logic into a small script that a test can run with fake output.

## Nits

- ⚪ `libs/render/release_test.go:1`, `online_test.go:76`, tests of workflows and shell scripts, and now tests that run `git` and `sh`, live in `libs/render`, which AGENTS.md calls a pure context. They are test files only, but a top level test package for CI and scripts would fit the bounded context rule. Carried over from the first review, and larger now.
- ⚪ `libs/render/online_test.go:162`, the last assertion only runs when `versions.env` still contains `MP_SOURCE_REF=main`. If someone changes that line, the check silently passes. Assert the file form instead, or fail on an unexpected value.
- ⚪ `libs/render/release_test.go:195`, the comment of `TestCIImagesSourceRef` still says the Taskfile passes `git rev-parse HEAD`. Since AC-12 it is the commit on a clean tree and `local` on a dirty one.
- ⚪ `docs/specs/0030-prerelease-image-source-zip/verify.md`, this file is modified but not committed (it ticks the checked steps). Commit it with the milestone, per the repo's git rule. The three open steps that need GitHub or a release (Renovate app, required check removal, the release run) stay unchecked, which is honest.

## Strengths

- The release flow is now simple to reason about: images are built from the release commit after the merge, `packages: write` sits on one job only, a pushed tag is never pushed again, and the push is the last step so nothing fails after it.
- Every build input is pinned and the pin rule has its own table test (`TestUnpinned`) that covers `--from`, `--mount` and stage names, not just `FROM`.
- `local` is a clear, reserved value end to end (value object, `setup.sh`, README, renderer warning, Taskfile), and a build from a checkout no longer claims a commit.
- `online-source-ref.sh` is tested against a real local bare repository, including a branch and tag of one name, an annotated tag and an unreachable remote.
- The first review was answered point by point in small commits, each with tests.

## Test coverage

Good. New logic has tests: the ref table (`source_test.go`), the pin rule (`catalog_test.go`), the release workflow structure and the check script list (`release_test.go`), the online ref script (`online_test.go`), the renderer warning for `local` (`server_test.go`), and golden files for the `local` zip. Not covered: the shell `case` logic of the GHCR guard (see the third minor finding), the first real release, and the Renovate app run on GitHub. All three are listed in `verify.md` as owed steps. Per the lint run above, `task lint:renovate` itself passes today.
