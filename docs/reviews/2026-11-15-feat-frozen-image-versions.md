# Review, feat/frozen-image-versions, 2026-11-15

**Reviewed by**: Sonnet 5.5 (author on a different model session)
**Scope**: 74 files, branch vs 8379c63 (spec 0031 only) plus the untracked web/src/test/review-dialog.test.tsx
**Verdict**: Approve with nits

## Summary
The change freezes every image of a managed stack to a digest. The renderer lists all image references, the api builds and compares the lock, the deployer writes `images.lock` and keep tags after a good `up`, and the editor shows an Images section. I traced the freeze rule, the review digest, apply, rollback, adoption, lock rotation and removal against the spec and found no blocker or major problem. The remaining findings are small robustness and test gaps.

## Minor
### 🟡 A corrupt lock file blocks review, apply and rollback with no way out in the product, `services/deployer-docker/internal/deploy/images.go:289`
**Problem**: `writeAtomic` writes a temp file and renames it, but never calls fsync on the file or the folder. After a crash the renamed file can be empty or partial. `Images` then answers 500 `lock_invalid`, the api turns that into 503 on review and apply, and the rollback fails with `lock_invalid`. The only apply that would replace the file (`lockAfterUp`) can never start, because the api needs `Images` first.
**Why it matters**: One bad file leaves the stack stuck until someone deletes it by hand. The api message says only "see its log".
**Suggested fix**: Sync the temp file before the rename. Make the api message name the file and say that deleting it makes the deployer adopt the running containers again.

### 🟡 A failed review refresh leaves a usable Deploy button on stale data, `web/src/components/ReviewDialog.tsx:150`
**Problem**: With `placeholderData` and react-query's error handling, a refresh that fails (422 `image_unresolved`, 409 `busy`) keeps the old `review.data`. The error shows, but the form stays, and Deploy is enabled because `isFetching` is false. The tick state and the shown digest no longer match.
**Why it matters**: The digest guard turns the mismatch into 409 `graph_changed` or a 422, so nothing wrong runs. The user still sees a confusing round trip.
**Suggested fix**: Disable Deploy while `review.isError`, or drop the placeholder data on error.

### 🟡 A read of the images takes the stack's operation lock, `services/deployer-docker/internal/deploy/images.go:105`
**Problem**: `Images` uses `reserve`, so while a review reads (it inspects every image, one call each, and may adopt) an apply or rollback start for the same project gets `ErrBusy`.
**Why it matters**: A second browser tab reviewing at the moment the api starts a rollback can make that rollback end `failed` with `busy`. The one retry from spec 0006 covers most cases.
**Suggested fix**: Hold a separate read marker that apply waits on briefly, or have the api retry `busy` on the rollback's `Images` and start calls. Or accept it and note it in the spec's tradeoffs.

### 🟡 A failed check for newer images is silent, `services/api/internal/deploy/images.go:81`
**Problem**: With `checkNewer`, only failures of references in `need` become problems. A registry failure for a frozen reference is dropped, so no row shows `newer available`.
**Why it matters**: With the registry down, the admin pressing "Check for newer images" sees the same review and may read it as "nothing newer".
**Suggested fix**: When `checkNewer` is set and some frozen reference failed, return a warning (or a Problem with severity warning) or mark the row.

### 🟡 No test for the resolve timeout or the four at once limit, `services/deployer-docker/internal/deploy/images_test.go:397`
**Problem**: `TestResolve` covers found and not found only. The timeout reason, the semaphore, and the `ResolveNoEngine` path that returns `ErrDocker` have no test. Also `resolveReason` reports "timeout" for any error once the context is done, including a cancelled caller.
**Why it matters**: This is the concurrent code of the change, and `-race` only checks it if a test runs it with several references.
**Suggested fix**: Add a fake `DistributionDigest` that blocks, assert at most 4 run at once, assert a short `ResolveTimeout` gives the timeout reason for the open ones, and assert `ResolveNoEngine` gives `ErrDocker`.

### 🟡 AC-13 says the platform render is unchanged, but its goldens changed, `docs/specs/0031-frozen-image-versions/index.md` (AC-13) and `libs/render/testdata/platform/`
**Problem**: `README.md`, `config/deployer-docker.env` and `setup.sh` of the platform golden changed (the README image note and the new `MP_RESOLVE_TIMEOUT` key). The spec names only `managed.json` as an expected golden change.
**Why it matters**: The changes are correct and follow from the new config key, but the spec and the code disagree.
**Suggested fix**: Update AC-13 to list these three files.

## Nits
- ⚪ `libs/contract/imagelock.go:339`, `ReviewDigest` panics on a marshal error; it cannot happen, but returning an error or hashing without JSON would honour "no panics".
- ⚪ `services/deployer-docker/internal/deploy/images.go:292`, a failed rename leaves `.images.lock.tmp` behind; remove it on error.
- ⚪ `services/deployer-docker/internal/deploy/images.go:577`, the unresolved reason includes the daemon's raw error text, which can show registry hosts or proxy details in the editor (admin only, so low risk).
- ⚪ `services/deployer-docker/internal/deploy/images.go:340`, a broken `images.previous.lock` is read as nil, so its keep tags are removed in the same apply that replaces nothing; log which file was dropped (the warning exists, so only mention it in the spec).

## Strengths
- The freeze rule, lock rotation and review digest are small pure functions in `libs/contract` with table tests, and the digest semantics of AC-7 (digest guards images, not ticks) are implemented as written and tested (`TestApplyOtherUpdateSameLock`, `TestApplyRaceMovedTag`).
- `RenderedStack.references` is built from the finished Compose project, so jobs and `mp_jobs` can never escape the freeze, and the renderer test compares it with each golden's image set.
- The deployer keeps the failure invariants well: nothing is written on a failed apply except the stated adopted lock, bookkeeping failure leaves the operation `succeeded` with a logged step, and removal deletes locks and keep tags.
- Input handling is careful: lock and resolve references pass `CheckLockReference`, files are opened with `O_NOFOLLOW`, Compose receives only `reference@digest` strings from the renderer, and secrets are never logged.

## Test coverage
Good for the pure logic (lock rules, freeze, digest, pins), the renderer (references, lock golden, invalid lock), the deployer engine (write, rotation, failed apply, soft failure, adoption, removal, built image, disagreeing containers, broken lock), the deployer HTTP layer (auth, limits, lock_invalid mapping), the api service (new, frozen offline, refusals, checkNewer/update, race, rollback from the lock, unreadable lock, missing reference, jobs) and the editor (rows, badges, tick, digest sent). An end to end Docker test exists under the integration tags and the smoke script asserts the lock. Gaps: the resolve timeout and concurrency cap, and the review error state in the dialog (see Minor).
