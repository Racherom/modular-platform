# Verify: Frozen image versions for apply and rollback · spec 0031 · updated 2026-11-13
_Steps derived from spec 0031 acceptance criteria and its Value sourcing table; the last three commands were added by /develop on 2026-11-14. `/check verify` runs these; `/test` locks the durable ones._

Each step uses a local registry, so nothing waits for a public registry: `docker run -d --name mp-registry -p 5000:5000 registry:3`, a test catalog module `whoami-local` with image `localhost:5000/mp-test/whoami:v1`, and two images pushed under that tag in turn (a good one, and a broken one whose healthcheck always fails). "Registry off" means `docker stop mp-registry`.

## UI / manual

- [ ] In the stack editor, deploy a graph with `whoami-local` and `postgres`: the review dialog lists both references under **Images** with badge `new` and a short digest → AC-5
- [ ] Review the same graph again: both rows show `frozen` with the same digests → AC-3
- [ ] Push the broken image as `v1`, click **Check for newer images**: the `whoami-local` row shows `newer available`; tick it, the row shows `updating` with old → new → AC-6
- [ ] Apply with the tick, registry off right after the review: the apply fails at `up`, the rollback runs, the stack shows `rolled-back`, and the app answers again → AC-4
- [ ] Delete a locked image from the host (remove its keep tag first): the review row shows "not on this host, pulled by digest at apply", and apply succeeds with the registry on → AC-10
- [ ] Add a module whose tag does not exist: the review shows `image_unresolved` naming the instance and the reference → AC-5
- [ ] Deploy a graph where `app` binds a database from `postgresql-provisioner` on `db`: the Images section lists `postgres:18.6` used by "provision job of postgresql-provisioner on db for app" and "revoke job of postgresql-provisioner on db", and does not list `mp_jobs` → AC-14

## Commands

- [ ] After the first deploy: `cat <MP_DIR>/mp-<stack>/images.lock` lists one entry per reference; for each, `docker inspect -f '{{.Image}}'` of its containers equals `imageId`; `grep '@sha256:' docker-compose.yml` shows every service image → AC-1, AC-2
- [ ] After deploying the provisioner graph: `grep 'image:' docker-compose.yml` shows `@sha256:` on every service, `db_provision_app_*`, `db_revoke_*` and `mp_jobs` included; `images.lock` has a `postgres:18.6` entry whose `imageId` equals `docker inspect -f '{{.Image}}'` of the exited job containers and `mp_jobs` → AC-1, AC-2, AC-14
- [ ] With the registry off and both images on the host, review and apply the same graph: success, `images.lock` digests unchanged → AC-3
- [ ] After the failed apply and rollback above: each container's image ID equals the one before; `images.lock` holds the old digest, and `sha256sum` of `images.previous.lock` is the same before the failed apply and after the rollback; `sha256sum` of `images.lock` right after the failed apply (before the rollback finished) equals the value before it → AC-1, AC-4
- [ ] Delete `images.lock` of a running stack, take the broken image with `update` and apply: `images.lock` exists (adopted) before the pull step, the rollback starts the adopted image IDs with the registry off → AC-9, AC-4
- [ ] `GET /v1/stacks/mp-<stack>/images` while an apply of that stack runs: 409 `busy` → AC-7
- [ ] Review, then push a new image under a reference that apply resolves, then apply with that review's `digest`: 409 `graph_changed`, no operation started → AC-7
- [ ] `docker image ls 'localhost/mp-keep/mp-<stack>'` after three applies with three digests: exactly the images of the two lock files; `docker image prune -f` removes neither → AC-8
- [ ] Delete `images.lock` of a running stack, then `curl` the deployer's `GET /v1/stacks/mp-<stack>/images` with the caller key: `adopted: true`, entries match the containers; review shows `frozen`; after apply the file exists again → AC-9
- [ ] Remove the stack (once without, once with delete data): no `images*.lock`, no `localhost/mp-keep/mp-<stack>` tag → AC-11
- [ ] `POST /v1/stacks/mp-<stack>/apply` straight to the deployer with `image: localhost:5000/mp-test/whoami:v1` (no digest): accepted; the lock entry carries the repo digest → AC-12
- [ ] `go test -race ./libs/render/...` and `./libs/contract/...`: the Compose goldens without a lock are unchanged (only `managed.json` gains `References`), and `references` equals the set of `image:` values of each managed golden; the freeze rule and review digest table tests pass → AC-13, AC-7, AC-14
- [ ] Without the caller key, `GET /v1/stacks/<stack>/images` and `POST /v1/images/resolve` answer 401 → AC-5, AC-9
- [ ] Write `{` into `<MP_DIR>/mp-<stack>/images.previous.lock`, then `GET /v1/stacks/mp-<stack>/images` with the caller key: 500 `lock_invalid` naming that file; a review of the stack answers 503 `lock_invalid` → AC-1, AC-9
- [ ] With `images.lock` broken, take a broken image with `update` and apply: the apply fails at `up`, and the rollback ends `failed` with error text starting `lock_invalid`, starting nothing on the host → AC-4
- [ ] `POST /api/v1/stacks/<name>/review` with `"checkNewer":true,"update":[...]` and `POST …/apply` with `"update":[...]` pass the api's strict body check; `checkNewer` on apply answers 400 `invalid_request` → AC-6, AC-7
