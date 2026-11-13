# Verify: Frozen image versions for apply and rollback · spec 0031 · updated 2026-11-13
_Steps derived from spec 0031 acceptance criteria and its Value sourcing table. `/check verify` runs these; `/test` locks the durable ones._

Each step uses a local registry, so nothing waits for a public registry: `docker run -d --name mp-registry -p 5000:5000 registry:3`, a test catalog module `whoami-local` with image `localhost:5000/mp-test/whoami:v1`, and two images pushed under that tag in turn (a good one, and a broken one whose healthcheck always fails). "Registry off" means `docker stop mp-registry`.

## UI / manual

- [ ] In the stack editor, deploy a graph with `whoami-local` and `postgres`: the review dialog lists both references under **Images** with badge `new` and a short digest → AC-5
- [ ] Review the same graph again: both rows show `frozen` with the same digests → AC-3
- [ ] Push the broken image as `v1`, click **Check for newer images**: the `whoami-local` row shows `newer available`; tick it, the row shows `updating` with old → new → AC-6
- [ ] Apply with the tick, registry off right after the review: the apply fails at `up`, the rollback runs, the stack shows `rolled-back`, and the app answers again → AC-4
- [ ] Delete a locked image from the host (remove its keep tag first): the review row shows "not on this host, pulled by digest at apply", and apply succeeds with the registry on → AC-10
- [ ] Add a module whose tag does not exist: the review shows `image_unresolved` naming the instance and the reference → AC-5

## Commands

- [ ] After the first deploy: `cat <MP_DIR>/mp-<stack>/images.lock` lists one entry per reference; for each, `docker inspect -f '{{.Image}}'` of its containers equals `imageId`; `grep '@sha256:' docker-compose.yml` shows every service image → AC-1, AC-2
- [ ] With the registry off and both images on the host, review and apply the same graph: success, `images.lock` digests unchanged → AC-3
- [ ] After the failed apply and rollback above: each container's image ID equals the one before; `images.lock` holds the old digest, and `sha256sum` of `images.previous.lock` is the same before the failed apply and after the rollback; `sha256sum` of `images.lock` right after the failed apply (before the rollback finished) equals the value before it → AC-1, AC-4
- [ ] Delete `images.lock` of a running stack, take the broken image with `update` and apply: `images.lock` exists (adopted) before the pull step, the rollback starts the adopted image IDs with the registry off → AC-9, AC-4
- [ ] `GET /v1/stacks/mp-<stack>/images` while an apply of that stack runs: 409 `busy` → AC-7
- [ ] Review, then push a new image under a reference that apply resolves, then apply with that review's `digest`: 409 `graph_changed`, no operation started → AC-7
- [ ] `docker image ls 'localhost/mp-keep/mp-<stack>'` after three applies with three digests: exactly the images of the two lock files; `docker image prune -f` removes neither → AC-8
- [ ] Delete `images.lock` of a running stack, then `curl` the deployer's `GET /v1/stacks/mp-<stack>/images` with the caller key: `adopted: true`, entries match the containers; review shows `frozen`; after apply the file exists again → AC-9
- [ ] Remove the stack (once without, once with delete data): no `images*.lock`, no `localhost/mp-keep/mp-<stack>` tag → AC-11
- [ ] `POST /v1/stacks/mp-<stack>/apply` straight to the deployer with `image: localhost:5000/mp-test/whoami:v1` (no digest): accepted; the lock entry carries the repo digest → AC-12
- [ ] `go test -race ./libs/render/...` and `./libs/contract/...`: the goldens without a lock are unchanged; the freeze rule and review digest table tests pass → AC-13, AC-7
- [ ] Without the caller key, `GET /v1/stacks/<stack>/images` and `POST /v1/images/resolve` answer 401 → AC-5, AC-9
