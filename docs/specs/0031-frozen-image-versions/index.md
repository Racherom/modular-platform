# 0031. Frozen image versions for apply and rollback

**Date**: 2026-11-13
**Status**: In Progress
**Scope**: [#47 Frozen image versions for apply and rollback](../../scope/scope.md)
**Builds on**: [0006](../0006-managed-mode-control-plane/index.md) (apply, review, automatic rollback), [0007](../0006-managed-mode-control-plane/0007-deploy-target-deployer.md) (the deployer and its apply pipeline), [0016](../0016-platform-one-container/index.md) (the keeper, for the platform rule), [0030](../0030-prerelease-image-source-zip/index.md) (images built on the host)

## Summary

In managed mode, every apply now freezes each image to the exact digest it runs (a digest is the content hash that names one image for good, unlike a tag, which can be moved). The deployer writes those digests into a lock file in the stack's folder once the apply succeeds, and keeps the images of the current and the previous good apply on the host. A rollback starts the digests from that lock, so it never pulls a moved tag again and works with the registry out of reach. A newer version of a tag is only taken when you tick it in the review dialog. This covers every service in the stack's Compose file: module services, and also the provision and revoke jobs and the wait anchor `mp_jobs`, whose images come from capability files, not from module entries. The renderer lists all of them in one place, `RenderedStack.references`.

## Requirements

**User stories**:
- As an admin, I want a failed apply to roll back to exactly the images that ran before, so that a broken image pushed under the same tag cannot take my stack down twice.
- As an admin, I want applying the same graph again to run the same images, so that nothing changes unless I change it.
- As an admin, I want to see when a newer image exists for a tag and choose to take it, so that updates are a decision I make in the review.

**Acceptance criteria**:
- **AC-1**: After an apply succeeds, the stack's folder holds `images.lock` with one entry per image reference in its `docker-compose.yml`: `reference`, `source`, `digest` (for `registry`) and `imageId`, where `imageId` equals the image ID of every container of that reference (the exited containers of provision and revoke jobs, and `mp_jobs`, included). An apply that fails at any step leaves `images.lock` and `images.previous.lock` byte for byte as they were, apart from the adopted lock written before pull for a stack that had no `images.lock` (AC-9).
- **AC-2**: Every service of a deployed stack that the api applies (module services, provision jobs, revoke jobs and `mp_jobs` alike) names its image as `<reference>@<digest>` in `docker-compose.yml`, with the digest taken from the lock that the review showed.
- **AC-3**: Reviewing and applying a graph again, with no `update`, gives the same digest for every reference that is already in the current lock, with no call to any registry for them; with the registry unreachable and every image on the host, the review and the apply both succeed.
- **AC-4**: A tag moves to a broken image; you take it with `update` and the apply fails at `up`. The automatic rollback starts the digests of the current lock (the last good apply), with the registry unreachable, and afterwards each container's image ID equals the one before the failed apply and the stack shows `rolled-back`.
- **AC-5**: At review, a reference that is new to the stack, or changed in the graph, is resolved to a digest at the registry without pulling. A reference that cannot be resolved (unknown tag, unreachable registry, private registry) refuses the review with 422 and a problem `image_unresolved` per reference, naming the reference and what uses it (the instance for a module; the capability and the provider instance for a job).
- **AC-6**: A review with `checkNewer` lists, for every frozen reference, the `available` digest when it differs from the frozen one. Only references named in `update` change their digest, and the review shows the old and the new digest of each before you apply.
- **AC-7**: The review returns a `digest` that covers the check result and the frozen lock for this graph. Apply builds that lock again with its own `update` list; when the result differs from the reviewed lock (a tag that apply resolves moved, the frozen lock for this graph changed, or an `update` different from the review's moves a digest), apply answers 409 `graph_changed` and starts nothing. An `update` different from the review's that leaves every digest the same builds the same lock, so apply goes ahead: the digest guards which images run, not which rows were ticked.
- **AC-8**: Each image in the current and the previous lock carries the keep tag `localhost/mp-keep/<compose project>:<imageId hex>`. After a successful apply, the deployer removes every keep tag of that project whose image is in neither lock, and `docker image prune` (without `-a`) removes no image of either lock.
- **AC-9**: For a stack that has containers but no `images.lock` (deployed before #47), `GET /v1/stacks/{stack}/images` answers `adopted: true` with a lock built from the running containers. The first review then shows those references as `frozen`. The first apply writes that adopted lock as `images.lock` before it pulls, so a rollback of that apply starts the images that ran before it, even after `up` recreated containers; on success it writes its own lock as usual.
- **AC-10**: When an image of the current lock is not on the host, the review marks its row `present: false` (shown as "not on this host, pulled by digest at apply"), and apply is still allowed.
- **AC-11**: Removing a stack deletes `images.lock`, `images.previous.lock` and every keep tag of its project, with or without delete data.
- **AC-12**: A `docker-compose.yml` that names an image by tag with no digest is still accepted by the deployer. After a successful apply, its lock entry records the repo digest and the image ID that Docker ran (or `source: built` and no digest when the image has no repo digest).
- **AC-13**: The static zip, the online editor and the platform render are unchanged: a render without a lock gives the same bytes as before (the golden files hold). The one expected golden change is that each managed golden's `managed.json` gains the `References` list (AC-14); its `docker-compose.yml`, `README.md` and `setup.sh` stay byte for byte the same.
- **AC-14**: For every managed render, `RenderedStack.references` holds exactly the distinct `image:` values of the rendered `docker-compose.yml`, each with every service that uses it. For a stack where `app` binds a database from `postgresql-provisioner` on `db`, the review's Images section lists the job image `postgres:18.6` as used by "provision job of postgresql-provisioner on db for app" and "revoke job of postgresql-provisioner on db" (and not `mp_jobs`), with its own `new` or `frozen` badge; after the apply, `images.lock` has an entry for `postgres:18.6`, and a rollback starts that job image from the lock like any module image.

## Decision

**Chosen option**: Option 2: a lock file per stack, owned by the deployer, given to the renderer as input.

The deployer owns the truth about which images run: it writes `images.lock` after each successful apply, keeps the current and the previous generation with keep tags, and answers the lock on request. The api resolves only new or ticked references at review, folds the lock into the review digest, and hands the lock to the renderer, which writes `reference@digest` into the Compose file. Rollback reads the current lock, which a failed apply never touched.

**Implementation skills**: `golang-error-handling` (`samber/cc-skills-golang`, `.agents/skills/golang-error-handling/`) · `golang-context` (`samber/cc-skills-golang`, `.agents/skills/golang-context/`) · `golang-testing` (`samber/cc-skills-golang`, `.agents/skills/golang-testing/`) · `golang-security` (`samber/cc-skills-golang`, `.agents/skills/golang-security/`) · `golang-observability` (`samber/cc-skills-golang`, `.agents/skills/golang-observability/`) · `openapi-spec-generation` (`wshobson/agents`, `.agents/skills/openapi-spec-generation/`)

## Rationale

Reasoning and options: see [rationale.md](rationale.md).

## Feature design

### Words

| Word | Meaning here |
|---|---|
| reference | an image name as the catalog writes it, `repo:tag`, with no digest: a module entry's `image` (`postgres:18.6-alpine3.24`) or a provisioner capability's `yields.job.image` (`postgres:18.6`, `smallstep/step-cli:0.30.2`) |
| user | one service of the Compose file that runs a reference: a module instance, a provision job, a revoke job, or the wait anchor `mp_jobs` |
| digest | the registry manifest digest `sha256:<hex>` a reference resolves to; for an image built for several platforms, the digest of the index, as `RepoDigests` records it |
| image ID | the local ID Docker gives the image it runs (`sha256:<hex>` of the image config) |
| lock | the list of references a stack runs, each frozen to a digest and an image ID |
| frozen | a reference whose digest comes from the current lock |
| keep tag | a local tag the deployer puts on a locked image, so Docker never counts it as dangling |

### Data model sketch

No database change. The lock lives in two JSON files in the stack's folder `<MP_DIR>/mp-<stack>/`, written only by the deployer, mode 0644, by writing a temp file in the same folder and renaming it.

| File | Holds |
|---|---|
| `images.lock` | the lock of the last succeeded apply (what runs now) |
| `images.previous.lock` | the lock that was current before it |

```json
{
  "formatVersion": 1,
  "stack": "0193f0c2-…",
  "operation": "op-…",
  "writtenAt": "2026-11-13T10:00:00Z",
  "images": [
    {
      "reference": "postgres:18.6-alpine3.24",
      "source": "registry",
      "digest": "sha256:…",
      "imageId": "sha256:…"
    }
  ]
}
```

| Field | Type | Required | Rule |
|---|---|---|---|
| `formatVersion` | integer | yes | `1`; any other value is `lock_invalid` |
| `stack` | uuid | yes | equal to the stack label of the apply that wrote it |
| `operation` | string | yes | the deployer operation ID that wrote it |
| `writtenAt` | RFC 3339 time | yes | UTC |
| `images` | list | yes | sorted by `reference`, each `reference` once, at least one entry |
| `images[].reference` | string | yes | Docker reference grammar, with a tag, never a digest |
| `images[].source` | `registry` \| `built` | yes | `built` when the image has no repo digest |
| `images[].digest` | string | for `registry` | `sha256:` and 64 hex; absent for `built` |
| `images[].imageId` | string | yes | `sha256:` and 64 hex |

Relationships: a stack folder has zero or one current lock and zero or one previous lock; a lock has one or more entries; one entry serves every service that uses its reference (two `postgres` instances on one tag, or a module and a job on one tag, always run one digest).

The Go value object is `contract.ImageLock` in `libs/contract` (the shared kernel, so renderer, api and deployer use one type), built by `contract.NewImageLock`, which checks every rule above and is then treated as immutable.

### State transitions (the lock files)

| Event | `images.lock` | `images.previous.lock` | Keep tags |
|---|---|---|---|
| apply starts (prepare) on a stack with no `images.lock` and running containers | the adopted lock | unchanged | tag the adopted images |
| apply (or rollback) succeeds, entries differ from the current lock | the new lock | the old `images.lock` (unchanged when there was none) | tag every image of both; remove the project's other keep tags |
| apply (or rollback) succeeds, entries equal the current lock (same `reference`, `digest`, `imageId` set) | rewritten with the new `operation` and `writtenAt` | unchanged | unchanged |
| apply fails at any step | unchanged (after the adoption write above) | unchanged | unchanged |
| remove (any `deleteData`) | deleted | deleted | every keep tag of the project removed |

So a rollback, or applying the same graph again, never pushes the real previous generation out. Images that a failed apply pulled never get a keep tag, so Docker frees them like any unused image.

**Order after `up` succeeds** (in the deployer's operation, which holds the stack's busy lock):

1. `ImageInspect` every service image of the guarded project and build the new lock. "Every service" means every key under `services:`, the provision and revoke jobs and `mp_jobs` included. Each entry's `reference` is that service's `image:` with a trailing `@sha256:<64 hex>` removed (the same rule adoption uses), and its `digest` is the part removed. Their containers exist after `up`: the apply runs `up --detach --remove-orphans --wait` (`services/deployer-docker/internal/deploy/apply.go`), and `--remove-orphans` removes only services the Compose file no longer names. So a job's container stays behind, exited 0, and the AC-1 image ID check reads it. When a service has no container (it should not happen), the entry takes its `imageId` from `ImageInspect(<reference>@<digest>)` instead. Compose recreates a container whenever its `image:` changes. So after an apply or a rollback, the job containers and `mp_jobs` carry the image ID of the `reference@digest` that `up` just ran, and the AC-1 and AC-4 checks read the containers present after that `up`.
2. Tag every image of the new lock with its keep tag.
3. When the entries differ from the current lock: write `images.previous.lock` from the current `images.lock` (temp file and rename).
4. Write `images.lock` (temp file and rename).
5. Remove every keep tag of the project whose image is in neither file.

A crash between steps leaves at worst an extra keep tag, never a locked image without one. When any of these steps fails, the operation still ends `succeeded` (the containers are healthy, and rolling them back would be worse); the deployer logs `lock write failed` at error level with the step, and the files keep their last good content. The next successful apply writes the lock again.

**Adoption** (no `images.lock` in the folder): read the project's containers (label `com.docker.compose.project`), stopped ones included (`Engine.Containers` already lists with `All: true`), so the exited job containers and `mp_jobs` count like module containers. The reference is `Config.Image` without any digest, the image ID is the container's `Image`, and the digest is the image's `RepoDigests` entry whose repository matches. A reference is left out (so it counts as `new`) when its image has no repo digest, or when its containers run different image IDs. Adopted entries are `present: true` by construction. Within one apply, Compose pulls each reference once, so all services of one reference always run one image ID.

### Where the references come from

A managed `docker-compose.yml` has four kinds of service, and their images come from two kinds of catalog file:

| Service | Kind | Image source | Set in |
|---|---|---|---|
| `<instance>` | `module` | the module entry's `image` (`catalog/modules/<module>/module.yaml`) | `libs/render` module render |
| `<p>_provision_<c>_<s>`, one per binding to a provisioner | `provision` | the provisioner capability's `yields.job.image` (`catalog/capabilities/<capability>.yaml`) | `jobService` in `libs/render/bind.go` |
| `<p>_revoke_<q>`, one per provisioner provide with a `login` | `revoke` | the same `yields.job.image` | `jobService` |
| `mp_jobs`, when some job has no dependent | `wait` | a copy of the image of the first such job | `waitAnchor` in `libs/render/bind.go` |

`ManagedProject.Images` (and so `RenderedStack.images`) lists only the module instances and stays as it is: the review uses it as its module list. The renderer adds a second list, **`RenderedStack.references`**, and every action takes the stack's references from it:

- Built in `libs/render` (pure) from the services of the finished project, after `waitAnchor` ran and before the lock is applied, so it holds exactly the distinct `image:` values the Compose file will have, whatever kind of service names them. A service kind added later is covered without touching this list.
- One entry per distinct image string, sorted by `reference`; each entry has `usedBy[]`, sorted by `service`: `service` (the Compose service name), `kind` (`module` \| `provision` \| `revoke` \| `wait`), `instance` (the module instance, or for a job the provider instance its `instance` label names; absent for `wait`), `capability` (jobs only: the name of the provisioner capability whose `yields.job` it runs), `consumer` (provision jobs only: the binding's consumer).
- The renderer records `capability` and `consumer` on its `job` value when it renders the job (beside the `instance`, `module` and `kind` it records today); nothing reads them back from labels.
- `mp_jobs` is listed as a `wait` user of the job image it copied. It never adds a reference of its own, because its image is always a job's image.
- `usedBy` values, as `provisionJob` and `revokeJobs` know them: `capability` is the name of the provisioner capability the provide offers (`admin.Name`, the capability whose `yields.job` the job runs, for example `postgresql-provisioner`), never the provide name; `instance` is the provider instance (`b.Provider` for a provision job, `p` for a revoke job); `consumer` is `b.Consumer`, on provision jobs only.
- Every `wait` user shares its reference with at least one job user (`waitAnchor` copies a job's image), so a review row never ends up empty when `wait` users are dropped. The renderer test for `references` asserts this.
- **No digest in the catalog.** A module entry's `image` and a capability's `yields.job.image` must be `repo:tag` with no `@sha256:`: the catalog check refuses a digest with the new code `image_digest` (in `libs/contract`, beside `image_untagged` and `image_latest`, for both `imageProblem` in `capability.go` and the module image check in `module.go`). No catalog file pins a digest today. The lock is now the one place an image gets pinned, so every reference is a plain lock key, and the lock render never meets a reference that already carries a digest.

This spec adds `references` to the managed render only. The platform render gains the same list when the platform rule below is built.

### How each action builds the lock (the freeze rule)

`contract.Freeze(references, current, resolved, update)` is a pure function in `libs/contract`. `references` is the `reference` of every entry of `RenderedStack.references` from a managed render without a lock. A reference is the image string exactly as the module entry or the capability file writes it, and it is the lock key; no other normalising happens. "Changed in the graph" needs no comparison of graphs: a module or capability whose catalog tag changed simply names a reference that is not in `current`. For each reference:

1. The reference is in `current` with a digest (`source: registry`) and is not in `update` → frozen, digest from `current`.
2. Otherwise (new to the stack, in `update`, a `built` entry or any entry without a digest, or left out by adoption) → digest from `resolved`; `change` is `new`, or `updated` when the reference was in `current` with a digest.

| Action | `current` comes from | Resolved at the registry |
|---|---|---|
| review | deployer `GET …/images` | the references of rule 2; with `checkNewer`, also every reference of rule 1 (shown as `available`, used only when in `update`) |
| apply | deployer `GET …/images` | the references of rule 2 again |
| rollback, and its one retry | deployer `GET …/images`, read again by each attempt (a failed rollback writes nothing, so both read the same lock) | none, unless a reference is missing from `current` (after adoption gaps, or when an earlier apply ended `unknown` in the api but succeeded on the host); then it is resolved like rule 2 and the api logs `rollback resolved an image` at warn level. That case needs the registry, so the offline promise of AC-4 holds only for references in the lock. One way to reach it: a platform release that changes a module `image` or a capability's `yields.job.image` between the failed apply and its rollback |

When `GET …/images` fails during a rollback (503, `lock_invalid`, `busy`, timeout), the rollback ends `failed` with the existing failure text (`deployer_unavailable`, or the problem code), and spec 0006's one retry follows. An `unknown` outcome still starts no rollback, as spec 0006 says.

### Platform rule (decided here, built with #19 and #25)

The platform graph freezes the same way once its changes go through an apply: the keeper (spec 0016) writes `images.lock` and `images.previous.lock` in the state folder after a successful reconcile, with the project `modular-platform`. An image built on the host (spec 0030) is frozen by its image ID: its entry is `source: built` with no digest, it gets the keep tag `localhost/mp-keep/modular-platform:<imageId hex>`, and the platform render given a lock writes that keep tag as `image:` with `pull_policy: never`. A restart never takes a rebuilt image; only the platform's own upgrade action (#25) does, in the same way as `update`. The managed render refuses a `built` entry for a deployed stack (`invalid_lock`). When this rule is built, the platform render gains the same `references` list (built the same way), and the keeper takes the platform's references from it, so any platform job is frozen like a module. No platform code changes in this spec.

### API surface

Deploy target (`contracts/openapi/deploy-target.yaml`), behind the existing caller key auth:

| Endpoint | Method | Key inputs | Key outputs | Key errors |
|---|---|---|---|---|
| `/v1/stacks/{stack}/images` | GET | `stack` (compose project) | 200 `StackImages`: `adopted` (bool), `current` (`ImageLock` or null), `previous` (`ImageLock` or null); every entry also carries `present` (bool): for `registry` entries `ImageInspect(<reference>@<digest>)` succeeds (the same name Compose looks up), for `built` entries `ImageInspect(<imageId>)` succeeds, adopted entries are always true | 400 `invalid_project`, 409 `busy` (an operation of this stack runs, so the files may be mid rotation), 500 `lock_invalid` (names the file), 503 `docker_unavailable` |
| `/v1/images/resolve` | POST | `references`: 1 to 200 strings, each a valid reference with a tag and at most 255 characters | 200 `{images: [{reference, digest}], problems: [Problem]}`, one problem `image_unresolved` per reference that failed, with the reason in plain words (`timeout` when `resolve-timeout` ran out first); at most 4 `DistributionInspect` calls run at once | 400 `invalid_reference` (the api maps it to 422 `image_unresolved` per reference), 503 `docker_unavailable` |
| `/v1/stacks/{stack}/apply` | POST | unchanged | unchanged; the lock is written by the operation after `up` succeeds | unchanged |
| `/v1/stacks/{stack}/remove` | POST | unchanged | unchanged; also deletes the lock files and keep tags | unchanged |

`GET …/images` for a project with no containers and no lock answers 200 with `adopted: false` and both locks null. `ImageLock` in the contract is the file format above; `present` exists only in the response.

Renderer (`contracts/openapi/renderer.yaml`):

| Endpoint | Method | Change |
|---|---|---|
| `/v1/render/managed` | POST | `ManagedRenderRequest` gains optional `lock`: `[{reference, digest}]`. After `waitAnchor`, each service (any kind) whose `image` equals a `reference` gets `image: <reference>@<digest>`, so module services, jobs and `mp_jobs` follow one rule; a reference not in the lock keeps its tag; a lock entry the graph does not use is ignored; a malformed entry gives 422 `invalid_lock`. `RenderedStack.images[].image` shows the written value (the tag without a lock, `reference@digest` with one); nothing in freezing reads that field. |
| `/v1/render/managed` | POST | `RenderedStack` gains required `references`: `[{reference, usedBy: [{service, kind, instance?, capability?, consumer?}]}]`, as under *Where the references come from*. `reference` is always the value before the lock is applied, so it stays the lock key in a render with a lock. |

api (`contracts/openapi/api.yaml`):

| Endpoint | Method | Key inputs | Key outputs | Key errors |
|---|---|---|---|---|
| `/api/v1/stacks/{name}/review` | POST | `ReviewRequest` gains `checkNewer` (bool, default false) and `update` (list of references, default empty) | `Review` gains `digest` (the review digest) and `images[]`: `reference`, `usedBy[]` (the render's `usedBy` without the `wait` entries: `kind`, `instance`, `capability`, `consumer`), `digest`, `change` (`frozen` \| `new` \| `updated`), `previousDigest` (for `updated`), `available` (only with `checkNewer`, when it differs), `present` | 422 `image_unresolved` per reference, 422 `invalid_update` (a reference in `update` that is not in the render's `references`; module and job references are checked alike, so ticking a job image row works like a module row), 503 `deployer_unavailable` |
| `/api/v1/stacks/{name}/apply` | POST | `ApplyRequest` gains `update` (same rule); `digest` is now the review's `digest`, not `check.digest` | unchanged | 409 `graph_changed` (message: "The graph or an image version changed since your review. Review again."), 422 `image_unresolved`, 422 `invalid_update` |

The review digest is `sha256:<hex>` over `check.digest`, a newline, and the canonical JSON of `[{reference, digest}]` sorted by reference (`contract.ReviewDigest`), the frozen lock for this graph only. `update` shapes that lock, but the list itself is not part of the digest: apply with a different `update` gives a different digest only when it changes a digest in the lock (the tag moved since that reference was frozen, or a reference the review updated is no longer updated). An `update` that resolves to the digest already frozen leaves the lock, and so the digest, the same, and the apply runs the reviewed images. `checkNewer`, `available`, `present` and lock entries the graph does not use are not part of it. A review that meets 409 `busy` from the deployer answers 409 `busy` as today. The rollback still skips the digest compare, as spec 0006 says.

Editor (`web/src/components/ReviewDialog.tsx`): an **Images** section under the modules, one row per reference with what uses it (one line per `usedBy` entry: the instance name for a module, "provision job of `<capability>` on `<instance>` for `<consumer>`", or "revoke job of `<capability>` on `<instance>`"), a short digest (first 12 hex), and a badge `frozen`, `new`, `updating` (with old → new), `newer available`, or `not on this host`. A **Check for newer images** button reviews again with `checkNewer: true`. Each row with `available` has a tick box; ticking or unticking reviews again with the new `update` list, and **Apply** sends that `update` with the latest review's `digest`. Nothing new on the canvas.

### Value sourcing

| Action | Value produced / displayed | Source |
|---|---|---|
| review, apply, rollback | the references of the stack | `RenderedStack.references[].reference` of a managed render without a lock (every distinct `image:` of its Compose file) |
| render | a module service's reference | the module entry's `image` |
| render | a provision or revoke job's reference | `yields.job.image` of the provisioner capability the job's provide gives (`jobService`) |
| render | `mp_jobs`'s reference | the image of the first job nothing depends on (`waitAnchor`); always also a job's reference |
| render | `usedBy[].capability`, `usedBy[].consumer` | the capability name and the binding's consumer, recorded on the renderer's `job` value when it renders the job |
| render | `usedBy[].instance` | the module instance; for a job, the provider instance (the same value as its `instance` label) |
| review, apply, rollback | frozen digest | `current` from deployer `GET …/images` (the file, or adopted from containers) |
| review, apply | new or updated digest | deployer `POST /v1/images/resolve`, which calls the Engine API `DistributionInspect` (descriptor digest) through the moby client |
| review | `available` digest | the same resolve call, made for the rule 1 references when `checkNewer` is true |
| review | `present` | deployer: `ImageInspect(<reference>@<digest>)` for `registry` entries, `ImageInspect(<imageId>)` for `built` entries, always true for adopted entries |
| review | `usedBy` per reference | `RenderedStack.references[].usedBy`, with the `wait` entries left out |
| review | the names in an `image_unresolved` problem | the same `usedBy` entries of the reference |
| review | `previousDigest` | `current` entry of the same reference |
| review, apply | review `digest` | `contract.ReviewDigest(check.digest, lock)` |
| apply, rollback | `image:` in `docker-compose.yml` | renderer, from `ManagedRenderRequest.lock` |
| apply | `update` | the admin's ticks in the review dialog, sent in `ApplyRequest.update` |
| lock write | `digest` of each entry | the `@sha256:` part of the guarded Compose file's image; for a tag with no digest, the `RepoDigests` entry of the image whose repository matches the reference |
| lock write | `imageId` | `ImageInspect` of the service image after `up` |
| lock write | `source` | `registry` when a digest was found, else `built` |
| lock write | `operation`, `stack`, `writtenAt` | the running operation's ID, the request's `stack`, the deployer's clock |
| adoption | each entry | the project's containers, by the rule under *Adoption* above; written to `images.lock` at the start of the next apply's prepare |
| keep tag | `localhost/mp-keep/<project>:<hex>` | the compose project and the entry's `imageId` without `sha256:` |

### Key invariants

- A failed apply never writes, renames or deletes a lock file and never adds or removes a keep tag, except the adopted lock written before pull on a stack that had none.
- `images.previous.lock` changes only when a successful apply's entries differ from the current lock.
- Every image of `images.lock` and `images.previous.lock` carries its keep tag after any successful apply whose bookkeeping succeeded; no keep tag of the project exists for an image in neither lock.
- The lock files are read and written only while holding the stack's operation lock (reads answer `busy` while an operation runs).
- `images.lock` names exactly the references of the stack's running `docker-compose.yml`.
- `RenderedStack.references` names exactly the distinct `image:` values of the Compose file it comes with (before the lock is applied); no service, whatever its kind, takes its image from anywhere else, so nothing a stack runs escapes `Freeze`.
- The deployer never deletes an image directly; it only removes its own keep tags, and Docker frees an image once nothing else uses it.
- `libs/contract` and `libs/render` stay pure: the freeze rule, the review digest and the lock render read no clock, disk, network or env.
- The pull step runs `docker compose pull --policy missing`, so an image already on the host never needs a registry.
- Only the deployer reads or writes the lock files; the api stores no digest.

### Security model

- Both new deploy target endpoints sit behind the same caller key auth as the rest of the deploy target API (spec 0006); only the api calls them.
- Review, apply, `checkNewer` and `update` need a signed in admin, like review and apply today; there is one admin role.
- `resolve` only accepts strings that parse as Docker references with a tag; references come from the module entries and capability files the api rendered, which the deployer pulls at apply anyway, so resolving adds no new place the host connects to. The daemon makes the registry call, with its own mirror and proxy settings.
- Private registries are out of scope: no registry credentials are sent, and such a reference fails with `image_unresolved`, whose message says the platform cannot log in to registries yet.
- Digests and image IDs are not secret; they may be logged. Lock files are 0644 in the stack folder, like `docker-compose.yml`.

### Configuration required

- `MP_RESOLVE_TIMEOUT` on `deployer-docker` (catalog `config:` key `resolve-timeout`, spec 0005): duration, default `30s`, min `5s`, max `50s`; the longest one `resolve` call may take for all its references (at most 4 at once). References still open when it runs out are reported as `image_unresolved` with reason `timeout`; the others are returned.
- The api waits for a resolve call at most `resolveTimeout = 60s` (a constant beside `targetTimeout` in `services/api/internal/deploy`), so the deployer's timeout (max 50 s) always ends first. `GET …/images` uses the existing `targetTimeout`.

### Observability

- Deployer, info: `images frozen` (project, operation, count of entries); `keep tags removed` (project, count); `lock adopted` (project, count, left out count).
- Deployer, warn: `image not resolved` (reference, reason); `lock invalid` (project, file, error).
- Deployer, error: `lock write failed` (project, operation, step, error); the operation still ends `succeeded`.
- api, info: `images resolved` (stack, new, updated, available counts); warn: `rollback resolved an image` (stack, reference).

### Critical test scenarios

- Happy path: deploy a stack whose image comes from a local `registry:3`, then read `images.lock`: one entry per reference, the image IDs match the containers, keep tags exist, verifies **AC-1**, **AC-2**, **AC-8**
- Same graph again: review and apply again with the registry container stopped; same digests, no pull, success, verifies **AC-3**
- Moved tag and rollback: push a broken image (its healthcheck fails) under the same tag, review with `checkNewer`, tick it, apply; `up` fails, rollback runs with the registry stopped; containers run the old image IDs, the stack shows `rolled-back`, `images.lock` holds the old digest and `images.previous.lock` is byte for byte what it was before the failed apply, verifies **AC-4**, **AC-6**, **AC-1**
- Adopted then failed: delete `images.lock` of a running stack, take a broken newer image with `update`, apply; `images.lock` holds the adopted lock before pull, `up` fails, the rollback starts the adopted image IDs offline, verifies **AC-9**, **AC-4**
- Bookkeeping order: make keep tagging fail (fake engine) after `up` → operation `succeeded`, `lock write failed` logged, both files unchanged, verifies **AC-1**
- Busy read: `GET …/images` while an apply of that stack runs → 409 `busy`, verifies **AC-7**
- Same graph twice: two successful applies with equal entries leave `images.previous.lock` unchanged, verifies **AC-8**
- Unresolvable: a graph with a module whose tag does not exist → 422 `image_unresolved` naming instance and reference, verifies **AC-5**
- Race: review, then move the tag of a new reference, then apply with the review digest → 409 `graph_changed`, nothing started, verifies **AC-7**
- Same lock, other `update`: review with no `update`, then apply with `update` naming a frozen reference whose tag has not moved → 202, the lock and every container image ID unchanged; then move that tag, review with no `update` again, and apply with the same `update` → 409 `graph_changed`, verifies **AC-7**
- Retention: three successful applies with three different digests; the first digest's keep tag is gone, the other two remain; `docker image prune -f` leaves both, verifies **AC-8**
- Adoption: a stack started before #47 (lock file deleted) → `adopted: true`, review shows `frozen`, apply keeps the image IDs and writes the file, verifies **AC-9**
- Missing image: `docker image rm` of a locked image (after removing its keep tag) → review row `present: false`, apply pulls by digest and succeeds, verifies **AC-10**
- Remove: remove the stack → no lock file and no `localhost/mp-keep/mp-<stack>` tag left, verifies **AC-11**
- Tag with no digest: send a Compose file with `image: <reference>` to the deployer directly → accepted, lock records the repo digest, verifies **AC-12**
- Unchanged renders: `go test ./libs/render/` goldens hold with no lock, verifies **AC-13**
- Reference list: for every managed golden (`whoami-postgres-managed`, `bindings/golden-managed`), the set of `references[].reference` equals the set of `image:` values in its Compose file, and every service appears in exactly one `usedBy`; for the bindings golden, `postgres:18.6` lists the provision and revoke jobs and `mp_jobs` (`wait`), verifies **AC-14**
- Lock reaches jobs: render the bindings graph with a lock that pins every reference; every service, the jobs and `mp_jobs` included, names `@sha256:` (a new golden), verifies **AC-2**, **AC-14**
- Provisioner end to end: deploy a stack with `app` on `postgresql-provisioner`, whose job image comes from the local registry; the review lists the job reference with its jobs, `images.lock` has its entry, and the moved tag rollback offline restores the job image too, verifies **AC-14**, **AC-1**, **AC-4**
- Auth: `GET /v1/stacks/{stack}/images` and `POST /v1/images/resolve` without the caller key → 401; review without a session → 401, as today, verifies **AC-5**, **AC-9** (no new role)

## Build plan

Tracer Bullet: the first slice runs the whole thread (contract, renderer, deployer, api, editor) for freeze and offline rollback; later slices thicken taking newer versions, then the lifecycle edges.

1. [x] Spike, recorded in [rationale.md](rationale.md): in `docker:29.9.2-cli`, check that `docker compose pull --policy missing` with `image: <reference>@<digest>` already on the host makes no registry call (registry container stopped); that `DistributionInspect` returns the same digest that `RepoDigests` records after a pull, for an image built for several platforms; that a digest only image with a keep tag survives `docker image prune -f`; and that an image kept only by its keep tag (its repo digest gone) is reported `present: false` by `ImageInspect(<reference>@<digest>)` and then pulled, so `present` and the pull step agree. If `--policy missing` is missing or contacts the registry, switch the pull step to per image `ImageInspect` and `ImagePull` of the missing ones, then `up --pull never`, and note it here, satisfies **AC-3**, **AC-4**, **AC-8**
2. [x] Contract: `contract.ImageLock`, `contract.NewImageLock`, `contract.Freeze` and `contract.ReviewDigest` in `libs/contract` with table tests; the `image_digest` refusal for module and job images, with a catalog test case for each; the OpenAPI changes above in all three contracts; `task generate`, satisfies **AC-1**, **AC-2**, **AC-7**
3. [x] Renderer: `ManagedProject.References` built from the finished project's services (after `waitAnchor`), with `usedBy` and the `capability` and `consumer` recorded on the `job` value; `references` in `RenderedStack`; `lock` on the managed render writes `reference@digest` on every service whose image matches, jobs and `mp_jobs` included, and refuses `invalid_lock` and `built` entries; a test that `references` equals the image set of each managed golden; new goldens with a lock (the bindings graph among them), old Compose goldens unchanged and `managed.json` regenerated with `-update`, satisfies **AC-2**, **AC-13**, **AC-14**
4. [x] Deployer thread: pull with `--policy missing`; after `up` succeeds, build the lock from the guarded project plus `ImageInspect` and write it in the stated order (rotation only when entries differ, `lock write failed` keeps the operation `succeeded`); `GET /v1/stacks/{stack}/images` (file read, `present`); `POST /v1/images/resolve` through `DistributionInspect` in the `docker` adapter; `resolve-timeout` in the catalog entry and `sdkgen`, satisfies **AC-1**, **AC-3**, **AC-5**, **AC-12**
5. [x] api thread: review and apply build the lock (render, take `references`, `GET …/images`, resolve rule 2 references, `Freeze`, render with lock), return `images` with `usedBy` and `digest`, compare the review digest; rollback renders with the current lock and resolves nothing; `image_unresolved` problems naming the `usedBy` entries, satisfies **AC-2**, **AC-3**, **AC-4**, **AC-5**, **AC-7**, **AC-14**
6. [x] Editor: the Images section in `ReviewDialog` with badges and one "used by" line per entry (module instance, provision job, revoke job); apply sends the review's `digest` (Vitest for the rows, the job lines and the badges), satisfies **AC-2**, **AC-5**, **AC-14**
7. [x] End to end: an e2e test beside the deployer's existing e2e tests with a local `registry:3` for the happy path, the same graph offline, and the moved tag rollback offline, using a graph with a provisioner whose test capability's job image also comes from the local registry, so jobs and `mp_jobs` are frozen and rolled back too; `scripts/smoke-managed.sh` asserts `images.lock` after its deploy, satisfies **AC-1**, **AC-3**, **AC-4**, **AC-14**
8. [x] Take newer on purpose: `checkNewer` and `update` in review and apply, `available` and `previousDigest`, `invalid_update`, the tick boxes and the **Check for newer images** button; the race test for `graph_changed`, satisfies **AC-6**, **AC-7**
9. [x] Keep tags: tag both generations after success, remove the project's other keep tags, the README note on `prune -a`, satisfies **AC-8**
10. [x] Edges: adoption from containers when no lock exists, written before pull at the next apply; `busy` on `GET …/images` during an operation; rollback failure when the lock cannot be read; `present` and the "not on this host" row, remove deletes the lock files and keep tags, `lock_invalid` for a broken file, a tag with no digest recorded from `RepoDigests`, satisfies **AC-9**, **AC-10**, **AC-11**, **AC-12**

## Consequences

**Positive**:
- A rollback no longer depends on the registry or on a tag staying put; the failure #16's rollback could not recover from is gone.
- What runs is what you reviewed: the review digest covers the images, so a tag that moves after the review cannot slip into the apply.
- The lock sits beside the files the deployer already owns, so the keeper can use the same format for the platform, and a host can be read without the database.

**Negative / tradeoffs**:
- The api keeps no digest, so the history (#35) cannot show which images an older apply ran; restoring an older entry resolves every reference that is not in the current lock as new.
- Review and apply now make two managed renders and up to two deploy target calls each, plus a registry round trip for new or updated references.
- Disk use grows: the images of the previous good apply stay on the host until the next successful apply that changes the lock.
- A lock write that fails after a healthy `up` leaves the files one apply behind; a rollback of the next failed apply then returns to that older good state, or resolves the missing references at the registry.
- A review fails with `busy` while an operation of the stack runs, where it used to work.
- `docker image prune -a` still removes locked images that no container uses; a rollback then needs the registry. The README says so.
- A reference the catalog moves to a new tag counts as new and is resolved at the registry, so changing a catalog tag still needs a reachable registry at review.

**Neutral**:
- `ApplyRequest.digest` changes meaning from `check.digest` to the review's `digest`; the editor is the only client and changes with it.
- api and deployer ship in one platform release, so the new endpoints and the new review never meet an older deployer.

## Migration plan

**Strategy**: no data migration; adoption on first contact.
**Phases**:
1. The new platform release starts; existing stacks have no `images.lock`.
2. The first review of each stack reads an adopted lock from its running containers (AC-9); the first successful apply writes `images.lock`.
**Rollback**: going back to the previous platform release ignores the lock files and keep tags; they do no harm, and the keep tags can be removed with `docker image ls 'localhost/mp-keep/*'` and `docker image rm`.
**Risks**: a container whose image lost its repo digest is left out of adoption, so its reference is resolved at the registry on the first review and may take a moved tag; the review shows it as `new`, so you see it before you apply. The same happens when the containers of one reference disagree on the image ID. This can only follow a hand change on the host, since Compose recreates every container of a reference whose image changed; old job containers count like module containers here.
**Catalog change**: the new `image_digest` check refuses a module or capability file that pins a digest. No catalog file in the repo does, so nothing breaks; a later catalog entry that wants a fixed image uses an exact tag and relies on the lock.

## Follow-up

- [ ] #19 (live graph changes) applies module by module: its spec decides whether a partial apply writes the lock per module or once at the end, using `contract.Freeze` and the same files.
- [ ] #35 (apply history and diff): decide how the history shows image versions, given the lock lives with the deployer (read the current and previous lock, or have the deployer report digests on a finished operation).
- [ ] Spec 0016 (keeper) and #25 (upgrades): build the platform rule above, including built images frozen by image ID and the upgrade action that takes a rebuilt image.
- [ ] Private registries: registry credentials for resolve and pull, together with spec 0016's follow up for step and keeper images.
- [ ] Update spec 0006's rollback text (step 2 says the rollback renders the stored graph; it now also uses the current lock and pulls only missing images) once task 5 lands.
