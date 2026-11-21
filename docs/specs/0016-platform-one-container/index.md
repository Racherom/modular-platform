# 0016. Start the platform from one container

**Date**: 2026-11-05
**Status**: In Progress
**Scope**: [#32 Start the platform from one container](../../scope/scope.md)
**Builds on**: [0007](../0006-managed-mode-control-plane/0007-deploy-target-deployer.md) (the deployer), [0013](../0011-capability-module-contract/0013-module-manifest-v1.md) (install time), [0015](../0011-capability-module-contract/0015-platform-graph-managed-zip.md) (the platform graph and its render), [#37 First run setup on the web](../../scope/scope.md) (built first: it collects the install answers and enrolls the first admin)
**Revised**: 2026-11-20, no platform `setup.sh`: you install with one `docker run` (before the first release, `docker build` and then `docker run`), and #37 comes before this feature

## Summary

The managed platform starts from one container: the deployer, which in this role is called the keeper (the one service that keeps the platform's own containers in line with its files). You install it with one `docker run` from the zip's README, with the unzipped folder mounted read only; before the first release, `docker build` of the platform images comes first. No script runs on the host and nothing asks a question, because the web setup (#37, built before this feature) collects the install answers and enrolls the first admin. That first container, the seed, copies the files into a root only state folder. From then on the keeper creates every secret, runs every setup step, brings up the other platform services through Compose, and replaces its own container when its files change. Deployed stacks are never touched, and no shell script ever creates a secret.

## Requirements

**User stories**:
- As a self hoster, I want to install the platform with one command that starts one container, so that the platform creates its own secrets and I never handle them.
- As an admin, I want the platform to come back as its files describe each time it starts, so that a deleted or wrongly recreated container heals, without duplicates and without touching my deployed stacks.
- As a platform maintainer, I want one place that brings the platform up from its files, so that replacing a module (#23), upgrading (#25) and live changes (#19) all go through it.

**Acceptance criteria**:
- **AC-1**: On a clean host, the install commands in the zip's README are all you run: before the first release one `docker build` per platform image, then one `docker run`. The `docker run` creates exactly one container, the seed `modular-platform-seed`, with the Docker socket, the state folder at the same path inside as on the host (default `/var/lib/modular-platform`), the unzipped folder read only at `/install`, and no restart policy (only the keeper restarts with the host). Nothing asks a question, no script runs on the host, and no command needs `sudo`. The seed makes the state folder mode 0700 owned by root, copies the unzipped folder into it and writes `.env`; nothing lands under `secrets/` before its prepare.
- **AC-2**: From the seed alone, every generated secret (the binding tokens included), every setup step output and every `data/` folder come to exist, every platform service runs healthy in the Compose project `modular-platform`, and then exactly one keeper container runs (project `modular-platform-keeper-<hash>`) and the seed container is gone.
- **AC-3**: No secret value is ever shown: no README command prints one, and none appears in a log line or in any container's env. For each `show: once` file (the setup code of #37), the README names its path and the serving keeper logs its path and description at each start; you read it with `sudo cat`. You finish #37's setup with it, then sign in and deploy a stack as today.
- **AC-4**: Starting the platform again (`docker restart` of the keeper, a Docker or host restart, or the README's `docker run` again) on a host where it already ran keeps every matching container (same container IDs), reuses every volume, creates no duplicate, changes no secret or step output, and the seed copies nothing into a state folder that already holds the platform. A rerun whose `/install` files, `COMPOSE_FILE`, `MP_KEEPER_IMAGE` or seed image differ from the installed ones is refused (`install refused`, `installed_differs`, exit 1); taking them becomes an optional update you accept in the running stack later (see Follow-up).
- **AC-5**: At the next start, a platform container that was deleted is created, one whose Compose config hash or image ID no longer matches the files is recreated, a stopped one is started, and a container of the platform project with no service in the files is removed. Changes Compose's rule cannot see (`docker update`, `docker network disconnect`) stay until the container is recreated for another reason; the README says so.
- **AC-6**: When `keeper.yml`, `MP_KEEPER_IMAGE` or the keeper image ID changes, the next start creates a successor keeper; once the successor is healthy the old keeper removes itself, leaving exactly one keeper. A successor that is not healthy within `handover-timeout` (10 minutes) is removed and the old keeper keeps its job.
- **AC-7**: No deployed stack's container is touched by any platform start: the IDs, start times and restart counts of every `mp-*` container stay the same, and deployed stacks keep restarting through Docker's restart policy.
- **AC-8**: Only one keeper runs the start pipeline at a time; two keepers starting together run it one after the other and never create a duplicate container.
- **AC-9**: When prepare or reconcile fails (a pull fails, a step exits non zero, a module never gets healthy), the keeper logs the step and the error, a keeper whose definition matches still serves the deploy target API, and the pipeline runs again after 1 second, doubling up to 5 minutes, until it succeeds.
- **AC-10**: The contract carries the new fields: `keeper` on a module, `show: once` and `format: password` on a generated secret. The platform flavor refuses a graph with no keeper instance or more than one, a `from: ask` secret, a setup step with `asks`, and an asked config value of type `secret`.
- **AC-11**: Placed files (an external's secret fields, 0012) are allowed for any external and kept to a minimum, typically the key of an external authentication service, whose URL the graph holds. A placed file that a service of the setup mode needs (`beforeSetup`) and that is missing makes the seed log `install refused` with code `placed_missing`, every such path and its description; it copies nothing, creates no other container, and tries again on the backoff, so placing the file in the unzipped folder is enough to go on. Any other missing placed file does not block the install: the web setup of #37 asks it where it can.
- **AC-12**: `task dev:managed` and `scripts/smoke-managed.sh` start the platform through the README's `docker run` of the seed (no `sudo`, no wrapper container), passing their overlays as `-e COMPOSE_FILE=…` and their keeper image as `-e MP_KEEPER_IMAGE=…`, which the seed writes into the state `.env`. Their state folder is `.dev/managed-state`: `task dev:managed` keeps it across runs, and `task dev:managed:reset` removes it through `docker run --rm` of the keeper image; a changed render reaches dev only after a reset (until #25). Their overlays sit in the install folder, so the seed copies them. The smoke job finishes #37's setup through #37's own call and checks AC-2 to AC-7 and AC-13.
- **AC-13**: The seed refuses a wrong `docker run` before it copies or creates anything: no Docker socket mounted or a daemon that does not answer, no container matching its host name, no bind mount at its `--state` path, a state mount whose host path differs from `--state`, a `--state` that is not absolute, not clean or `/`, `/install` at the state path, or a fresh state folder with no `docker-compose.yml` under `/install`. It logs `install refused` naming the flag or file to fix and exits 1; with no restart policy it stays stopped, so `docker logs modular-platform-seed` shows what to change, and you `docker rm` it before you run the corrected command.

## Decision

**Chosen option**: Option 1: the deployer as keeper, started from a seed, with a self handover.

The deployer becomes the platform's keeper: a seed container you start with one plain `docker run` runs the keeper's start pipeline (lock, prepare, reconcile with Compose, self check), then hands over to the keeper container that `keeper.yml` describes, which runs the same pipeline at every start and replaces itself the same way when its definition changes.

**Implementation skills**: `golang-concurrency` (`samber/cc-skills-golang`, `.agents/skills/golang-concurrency/`) · `golang-context` (`samber/cc-skills-golang`, `.agents/skills/golang-context/`) · `golang-error-handling` (`samber/cc-skills-golang`, `.agents/skills/golang-error-handling/`) · `golang-security` (`samber/cc-skills-golang`, `.agents/skills/golang-security/`) · `golang-observability` (`samber/cc-skills-golang`, `.agents/skills/golang-observability/`) · `golang-testing` (`samber/cc-skills-golang`, `.agents/skills/golang-testing/`) · `openapi-spec-generation` (`wshobson/agents`, `.agents/skills/openapi-spec-generation/`)

## Rationale

The reasoning, the options weighed and the smaller choices are in [rationale.md](rationale.md).

## Feature design

### Terms

| Term | Meaning |
|---|---|
| keeper | The module instance whose module declares `keeper` (today `deployer-docker`). It keeps the platform's containers in line with the state folder. |
| seed | The keeper's first container: the same image, the same binary and the same start pipeline, created bare (socket, state folder, the unzipped folder) by your `docker run`, because its real definition in `keeper.yml` mounts files that only this first start creates, and sets labels, a healthcheck and hardening that Docker cannot add to a container that already exists. Named `modular-platform-seed`, it runs the keeper's `command` (`seed --state <dir>`), copies the install into a fresh state folder, never serves, and removes itself after the handover. |
| state folder | The root only folder that holds the platform's files: `$MP_STATE_DIR`, default `/var/lib/modular-platform`. `{install}` (0015) renders to it as `${MP_DIR}`. |
| platform project | The Compose project `modular-platform`: every platform instance except the keeper. |
| keeper project | `modular-platform-keeper-<first 12 hex of the definition hash>`: one Compose project per keeper definition, holding only the keeper. |
| definition hash | SHA 256 over the canonical JSON (keys sorted, every value resolved) of the keeper service as compose-go loads it from `keeper.yml` with the state `.env` as the only variable source (an empty process env, so the seed and the keeper compute the same hash), followed by the local image ID of its image (pulled first when missing). A keeper image with another compose-go version may serialize differently; that only adds a handover to an upgrade, which the image ID change causes anyway. |

### What this replaces

- 0013 *Install time*, for the platform flavor only: the platform flavor renders no `setup.sh`. Steps 1 and 7 (the `.env` header and the placed files check) move into the seed's install, which writes the fixed `65532` instead of `id -u`; steps 3 to 6 (asked secrets, generated files, setup steps, folders) move into the keeper's prepare; step 2 (asked values) moves to the web setup of #37. The static flavor keeps 0013 as written.
- 0015: the `deployer-docker` service in the platform's `docker-compose.yml` (it moves to `keeper.yml`), the platform `setup.sh` and the instruction to run `docker compose up -d`. The `authelia` `users` step's questions and the admin user name question are replaced by #37, before this feature.

### State folder (the data model)

The platform keeps no database for this feature; its state is this folder. The folder itself is 0700, owned by root.

| Path | Written by | Mode, owner | Holds |
|---|---|---|---|
| `docker-compose.yml`, `keeper.yml`, `setup.json`, `config/`, `graph.yaml`, `README.md`, `.env.example` (every file of the unzipped folder) | the seed, copied once from `/install` | as in the zip, root | the rendered platform (0015, plus the changes below) |
| `.env` | the seed, once; #37 adds the install answers | 0600, root | `MP_DIR`, `MP_UID=65532`, `MP_GID=65532`, when set on the seed `COMPOSE_FILE` and `MP_KEEPER_IMAGE`, and the install answers `MP_<I>__<V>` as #37 writes them |
| `secrets/<i>/…` | keeper (prepare) | files 0600 (0644 where a step `writes` says so), owner `MP_UID:MP_GID`; folders 0700 | generated secrets, binding tokens, step outputs, files you place |
| `data/<i>/<n>/`, `host_mounts` with `create: true` | keeper (prepare) | 0700, `MP_UID:MP_GID` | module data |
| `stacks/` | keeper (0007, unchanged) | as 0007 | deployed stacks' folders |
| `.keeper.lock` | keeper | 0600, root | the start pipeline lock (`flock`) |
| `.prepare/` | keeper | 0700, root | scratch space for generated files and step outputs before they move into place; emptied at each prepare |
| overlay files (`compose.dev.yaml`, `compose.ci.yaml`) | dev and CI tasks only | root | named by `COMPOSE_FILE` |

`MP_UID` and `MP_GID` are the fixed `65532` (the distroless `nonroot` user), because the seed runs as root and knows no installing user.

### Contract changes (0013)

- **Module field `keeper`**: `keeper: {command: [<args>]}` names the command a seed runs (`deployer-docker`: `[seed]`; the seed appends `--state <dir>`). A keeper module is host access like `host_mounts`. Only the platform flavor reads it; the static and managed flavors render the module as any other (and the managed flavor refuses it through `host_access`, as today).
- **Secret fields**: `format: hex` (default, 32 random bytes as hex) or `format: password` (24 characters drawn uniformly from `A` to `Z`, `a` to `z` and `0` to `9`, about 142 bits); `show: once`, allowed only with `from: generate`. Anything else is `invalid_secret` at catalog load.
- **Platform flavor refusals** (render error `setup_required` at `modules.<i>`, naming the field): a secret with `from: ask`, a setup step with `asks`, a config value with `setup: ask` and type `secret`. Asked config values that are not secret stay; the web setup of #37 asks them.
- **Platform flavor keeper rule** (render error): no instance with a keeper module is `keeper_missing`, more than one is `keeper_duplicate`, at `modules`.
- **`authelia` entry**: no change in this spec (revised 2026-11-20). #37, built first, replaces the asked admin user name and password of its `users` step with its own first admin enrollment, so the entry this spec renders has no step with `asks`. This spec no longer adds a generated `admin-password`; `show: once` and `format` stay in the contract (built in task 2) for #37's setup code.
- **`deployer-docker` entry** (0015): `keeper: {command: [seed]}` (task 2); with task 4, its host mounts become the Docker socket, `{install: .}` at the same path with `read_only: false` (it writes secrets, step outputs and stacks), and the existing `{install: stacks}` mount with `create: true` kept beside it, so `stacks/` still reaches `setup.json` `folders` (Docker allows the nested mount, and the two claim keys differ); it gains the new config values below.
- **The install folder itself** (task 3, additive): `{install: .}` names the install folder on either side of a host mount. It claims the key `{install}`, renders as `${MP_DIR}`, and refuses `create: true` (`invalid_rule`; the state folder already exists). A config default of a bare `{install}` is a token like `{install}/<path>` and renders as `${MP_DIR}`. Today `installPath` refuses `.` and `tokenDefault` knows only `{install}/`; both change in task 3, because the task 3 fixture uses them.
- **Why the `deployer-docker` entry waits for task 4**: milestone 4's deployer runs as a Compose service with only `stacks/` mounted. Changed earlier, the entry would break `task dev:managed` and smoke until task 4 switches them to the seed.

### Render changes (0015's platform flavor)

- `docker-compose.yml`: every instance except the keeper. No service has `depends_on` on the keeper; its consumers (`api`) reach it once it serves, and their per call errors already cover a deployer that is not there yet.
- `keeper.yml`: a Compose file with only the keeper service and no top level `name:`. Its network is `internal: {external: true, name: modular-platform_internal}` with the keeper's alias; image `${MP_KEEPER_IMAGE:-<catalog image>}`; `restart: unless-stopped`; 0015's labels (`stack: platform`, `instance`, `module`) and healthcheck; everything else rendered as for any instance.
- `setup.json`: the install actions the keeper applies, a Go type `render.Setup` in `libs/render`, written as canonical JSON. It covers every instance, the keeper included, so the keeper's own binding tokens, the CA from `cert-issuer`'s `ca` step and the caller public key exist before `keeper.yml` mounts them:
  - `formatVersion`: `1`. The keeper refuses another version and any unknown field (`state_invalid`).
  - `generate`: `[{path, format, show, readers}]` in 0013's step 4 order (module secrets, per binding files, empty job outputs). `readers` lists the `writes` of every step that reads the file.
  - `steps`: `[{instance, name, image, entrypoint, command, env, askEnv, reads, writes}]`, in 0013's step 5 order, a `for_each` step already expanded to one entry per client with `{client}` replaced. `askEnv` maps each `MP_ASK_<V>` to its `.env` variable `MP_<I>__<V>`. `reads` gives `{path, mount}` pairs; `writes` gives `{name, mode}`.
  - `folders`: `[{path, mode}]` for `data/<i>/<n>`, `host_mounts` with `create: true` (the keeper's `stacks/` among them) and the `for_each` client folders.
  - `show`: `[{path, description}]` for every `show: once` secret, in instance then name order.
  - `placed`: `[{path, description, beforeSetup}]` for every placed file (0012, one per `same_as` chain), in instance then name order. `beforeSetup` is true when an instance that runs in the setup mode reads it (#37's spec defines that set; until then, every instance).
  - `keeper`: `{instance, module, command}`.
- No `setup.sh`: the platform flavor renders none (revised 2026-11-20). Its `README.md` carries the install commands instead (see *Install*), rendered from the keeper's catalog image and `keeper.command`, the `show` paths of `setup.json`, and, while the platform images carry `0.0.0` (spec 0030), one `docker build` line per image. The static flavor's `setup.sh` does not change.
- **Switch until task 4**: everything in this list is built in task 3 behind a render option, `render.WithKeeper()` (an `Option` like `WithSource`; its doc comment says it goes away with task 4). Without it, `render.Platform` renders milestone 4's output as today, so `task managed:render`, dev and smoke keep working; the refusals and the keeper rule also run only with it, because the real `authelia` entry keeps its step with `asks` until #37 lands. `catalog/cmd/render-platform` does not pass it until task 4.
- **Task 3 golden**: `libs/render/testdata/platform-keeper/` holds the goldens, plus `catalog/modules/deployer-docker/module.yaml` with the task 4 shape of that entry and `catalog/modules/authelia/module.yaml` as a stand in for the entry #37 leaves (its `users` step without `asks`). The test calls `loadCatalogTemplates(t, "../../catalog", "testdata/platform-keeper/catalog")`, which merges by relative path, so the fixture files replace the real ones and templates still come from the real catalog; it checks `deploy/managed/graph.yaml`, retags the images as `platformResult` does, and renders with `WithKeeper()`. Task 4 diffs the `deployer-docker` fixture against its `catalog/modules/` entry (so an edit made there in between is not lost), moves it into `catalog/modules/`, drops the `authelia` stand in (the real entry now comes from #37), removes the option, the old path and the old `testdata/platform/` golden, and points the golden test at the real catalog again.
- The golden test covers `docker-compose.yml`, `keeper.yml`, `setup.json` and the install commands in `README.md`, and `render.Guard` is never run on these files (they are trusted, see Security model). A render test checks that every file path `keeper.yml` mounts is a `generate` path, a step `writes` path or a folder in `setup.json`.

### Install (the README's commands, the seed's first start)

The platform README shows these commands with the values filled in by the render. While the platform images carry `0.0.0` (spec 0030), one build per platform image comes first, run in the unzipped folder:

```sh
docker build -t ghcr.io/racherom/modular-platform/<module>:0.0.0 \
  --build-arg MP_SOURCE=git --build-arg MP_SOURCE_REF=<ref> \
  -f build/<module>/Dockerfile build/<module>
```

From a local checkout instead, the README gives `--build-arg MP_SOURCE=local --build-arg MP_SOURCE_REF=local -f build/<module>/Dockerfile <path to your checkout>`, with that placeholder for you to fill in (spec 0030's rules; a zip rendered with the ref `local` shows only this form). The README says the builds need Docker 23 or newer (where `docker build` uses BuildKit), access to Docker Hub, and for the git form access to GitHub; no script checks it, a build that cannot run fails on its own. After the first release there is no build: the run pulls the image. Then the one container, also run in the unzipped folder, which you keep until the seed is gone:

```sh
docker run -d --name modular-platform-seed \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v /var/lib/modular-platform:/var/lib/modular-platform \
  -v "$PWD:/install:ro" \
  <keeper image> <keeper command> --state /var/lib/modular-platform
```

To install somewhere else, you change both state paths and `--state` to the same absolute path, made of `A` to `Z`, `a` to `z`, `0` to `9`, `.`, `_`, `-` and `/`. The README then says how to follow the first start (`docker logs -f modular-platform-seed` until the seed is gone) and names the `show` paths to read with `sudo cat`. Starting again is the same `docker run`; when Docker reports the name `modular-platform-seed` in use, a stopped seed is left, and `docker start modular-platform-seed` goes on with it. The README also says not to add `--hostname`, `--network host` or `--uts host`, since the seed finds itself by its host name.

The seed has no restart policy on purpose. A host restart before the handover leaves it stopped, and `docker start modular-platform-seed` goes on where it stopped (the lock, the install and prepare all resume safely). A host restart after the successor exists brings the successor back by its own policy, and its cleanup removes the stopped seed.

The seed runs these steps before its pipeline goes on to check (pipeline steps 1 and 3):
1. **Mounts** (before the lock, AC-13): reach the daemon through `/var/run/docker.sock` (else `docker_unreachable`); find its own container (the host name, as in self check; else `self_unknown`); require `--state` to be absolute, unchanged by `filepath.Clean`, not `/`, and plain as above; require a bind mount whose destination is `--state` and whose source equals `--state` exactly, so a symlinked path counts as a mismatch (`mount_missing`, `mount_mismatch`); refuse `/install` at the state path (`install_is_state`). Any refusal logs `install refused` with the flag to fix and exits 1.
2. **Install** (inside the lock), only when the state folder holds no `docker-compose.yml`: require `/install/docker-compose.yml` and `/install/setup.json` with a `formatVersion` it knows (else `install refused`, `install_missing`, exit 1); check the `placed` entries of `/install/setup.json` in `/install`, and when one with `beforeSetup` is missing log `install refused` (`placed_missing`, each path with its description) and wait the backoff (AC-11); make the state folder 0700, owned by root (again at every fresh run, so a lock file made before it changes nothing); copy every file of `/install` through `.prepare/install/`, placed files as 0600 with owner `MP_UID:MP_GID`, every other file as in the zip, owned by root; write `.env` (0600) with `MP_DIR` (the `--state` value), `MP_UID` and `MP_GID` (`65532`), plus `COMPOSE_FILE` and `MP_KEEPER_IMAGE` when the seed's own env sets them, every value unquoted, one `NAME=value` per line, each `$` written as `$$` so Compose never expands it; move `docker-compose.yml` in last. Nothing has run yet in a folder without `docker-compose.yml`, so the copy may replace what an interrupted copy left; the folder counts as installed only once that last move is done. Log `install done`.
3. **An installed state folder**: copy nothing and change no file. When `/install` is mounted, compare its `docker-compose.yml`, `keeper.yml`, `setup.json`, `graph.yaml` and `config/` byte for byte with the installed ones (placed files are not compared); compare `COMPOSE_FILE` and `MP_KEEPER_IMAGE` from the seed's env with `.env` when the seed sets them; and compare the seed's own image reference with the keeper image the installed `keeper.yml` and `.env` name. Any difference: `install refused` (`installed_differs`, naming what differs), exit 1 (AC-4). Taking a newer install is an update for later (see Follow-up).

The seed needs no `already running` check: when a matching keeper already runs, the seed's handover finds it (Compose reuses the matching container), sees it healthy and removes itself.

### Setup mode (the contract with #37)

> Revised 2026-11-20 by [0033](../0033-first-run-web-setup/index.md): the seed runs a throwaway setup container before it installs, so the answers land in `.env` at install and the platform has no setup mode. Points 1 to 3 below no longer apply; point 4 stays, with `lifetime: setup` (0033) defining "only while the setup is not done". See 0033 *What this changes in 0016* for the other edits (install step 2, `beforeSetup` and `placed_missing`, prepare's two exceptions, AC-3 and AC-12).

The first start happens before the install answers (domain, origin, admin email) exist, so the platform first runs in a setup mode. #37's spec designs it; this spec relies on these points, and #37 must deliver them:
1. **Reconcile without answers**: the platform files reconcile, and every service of the setup mode gets healthy, with the install answers and the setup questions unset. #37's spec names the services of the setup mode, which also decides `beforeSetup` for placed files.
2. **No domain yet**: in the setup mode the ingress serves the setup site on a self signed certificate with no domain name, reached by the host's address. The keeper's own certificate comes from the internal `cert-issuer` (spec 0008) and needs no domain, so the seed's handover works the same before and after the setup.
3. **The answers reach the state folder through the keeper**: the keeper is the only writer of the state folder, so the setup sends the answers (and any external secret it asks instead of a placed file) to one keeper call that #37's spec defines. The keeper writes the answers into `.env` as `MP_<I>__<V>` and secrets as files in `secrets/` (atomic, as in prepare), then runs its pipeline again. That run leaves the setup mode, and the ingress gets a certificate for the domain (how is #37's to decide).
4. **The setup code**: a generated `show: once` file with no readers, made only while the setup is not done (see prepare).

Task 4 of this spec waits until #37's spec and build deliver these points.

### Start pipeline (state transitions)

Every keeper process, seed or serving, runs this at start:

```text
(seed) mounts ─▶ lock ─▶ (seed) install ─▶ check ─▶ prepare ─▶ reconcile ─▶ self check ─┬─ matches ──▶ cleanup ─▶ unlock ─▶ (serving keeper) TLS ─▶ serve
                                                                                         └─ differs ──▶ handover
a wrong mount: exit 1 (AC-13)
any failure in install, check, prepare or reconcile:
   definition matches and not the seed ─▶ unlock ─▶ TLS ─▶ serve, run the pipeline again after the backoff
   otherwise (seed, or an outdated keeper) ─▶ unlock ─▶ wait the backoff ─▶ lock …
```

1. **mounts** (seed only, before the lock): *Install*, step 1.
2. **lock**: an exclusive `flock` on `<state>/.keeper.lock`, waiting while another keeper holds it.
3. **install** (seed only): *Install*, steps 2 and 3.
4. **check**: the state folder holds `docker-compose.yml`, `keeper.yml`, `setup.json` and `.env`; the platform project name (the file's `name:`) is `modular-platform`; else the error `state_invalid`.
5. **prepare**: apply `setup.json`, for every instance, the keeper included. Every file lands atomically: it is written in `.prepare/`, synced, given its mode and owner, then moved to its path with `renameat2(RENAME_NOREPLACE)`, so a crash leaves either no file or a whole one, never an empty or cut one.
   - Each missing `generate` file is made this way, mode 0600, owner `MP_UID:MP_GID`, except a `show: once` file whose `readers` already exist, which is never made again (so deleting it never replaces a value a module already took in; the README says that the value is then gone). A `show: once` file with no readers (#37's setup code) is made only while the setup is not done; #37's spec defines how the keeper knows that (for example a marker it writes when it retires the code), so a deleted code is never made again after the setup.
   - Each step whose `writes` are all present is skipped; any other runs through the moby client as 0013 says: `--rm`, `--network none`, user `MP_UID:MP_GID`, `/out` bound to a fresh scratch folder in `.prepare/`, its reads bound read only, env plus `MP_GRAPH`, `MP_INSTANCE` and each `askEnv` value read from `.env`. Its image is pulled when missing, with no registry credentials (public images only for now). Only when it exits 0 and every `writes` entry exists in the scratch folder are they moved into `secrets/<i>/` with their modes; a write whose target already exists is left as it is and the scratch copy dropped. A non zero exit or a missing entry fails prepare and moves nothing, so a step that wrote half its files runs again from nothing and never replaces a key already in use.
   - Then the `folders` (0700, `MP_UID:MP_GID`, made when missing).
   - Prepare never deletes or overwrites a file in `secrets/` or `data/`.
6. **reconcile**: `docker compose --project-directory <state> up -d --remove-orphans --wait --wait-timeout <wait-timeout>`, run in the state folder with no `-f`, so Compose takes `COMPOSE_FILE` from `.env` (default `docker-compose.yml`). Compose's rule decides what to keep, start, recreate and create. Its env is the deployer's fixed `composeEnv` (0007).
7. **self check**: compute the definition hash (a pull failure of the keeper image fails this step). Self is the container whose ID starts with the process's host name (Docker's default host name; the render never sets `hostname`, and neither may an overlay). Zero or several matching containers stop the pipeline with `state_invalid`, logged at error level. Self matches when its `com.docker.compose.project` label equals the expected keeper project.
8. **cleanup** (matching keeper only): remove every keeper container that is not running (a stopped outdated keeper) and a stopped container named `modular-platform-seed` (a seed left behind). A running one is left alone; it removes itself.
9. **unlock**, then the serving keeper gets its certificate through ACME (spec 0008; `cert-issuer` runs by now) and serves as today. A failed certificate request retries on the backoff below; meanwhile the keeper is not healthy.

**Serving and health**: a keeper opens 0007's event stream and its HTTP server only after its first pipeline outcome (success, or a failure while its definition matches). The seed opens neither and has no healthcheck. The keeper's healthcheck (`deployer-docker healthcheck`) reports healthy only once TLS is up and the server answers, and `keeper.yml` gives it a `start_period` of 5 minutes. Each keeper process picks a new `bootId` (0007), so a handover is, for `api`, a deployer restart: running operations' outcomes are unknown, as today.

**Compose env**: reconcile and both handover commands run with 0007's fixed `composeEnv` and the state folder as working directory. They rely on one invariant: the state folder is mounted into every keeper and the seed at the same absolute path as on the host, so the paths Compose hands to the daemon are host paths. `keeper.yml` renders it so, and the seed checks it in its own `docker run` (AC-13).

**Handover** (self differs):
1. `docker compose --project-directory <state> --project-name <expected keeper project> -f keeper.yml up -d` (Compose reuses a successor container a crashed earlier handover left). A missing `modular-platform_internal` network fails this step, and the next run's reconcile creates it again.
2. Unlock, so the successor can run its own pipeline.
3. Poll the successor's health every 2 seconds, up to `handover-timeout`.
4. Healthy: remove self with `force` through the moby client. The process ends with it. (A drain for a keeper that was already serving is specified below and built with #25; at start nothing runs yet.)
5. Timeout, or the successor exited: `docker compose --project-name <expected> down`, log `handover failed`. A seed waits the backoff and runs the pipeline again. An outdated keeper serves with its current definition and runs the whole pipeline again after the backoff.

**Backoff**: 1 second, doubling, at most 5 minutes; a successful pipeline resets it. Each run of the pipeline after a failure takes the lock like the first one.

**Drain contract (written now, built with #25)**: when a keeper that already serves starts a handover, it answers new apply and remove calls with 503 `handing_over` (a `ProblemList`), finishes running operations within their deadlines, and waits until each finished operation was read once in its final state (or 60 seconds passed) before it removes itself. `api` retries `handing_over` with its own backoff.

### Interface surface

No new HTTP endpoint in this spec; #37's spec adds the keeper call that writes the install answers (*Setup mode*, point 3). The deployer's command line:

| Command | Started by | Inputs | Does | Ends |
|---|---|---|---|---|
| `deployer-docker seed --state <dir>` | you, with the README's `docker run` (no restart policy) | `--state`, absolute; `/install` on a fresh state folder; env `COMPOSE_FILE` and `MP_KEEPER_IMAGE` (dev and CI only) | the mounts check, the install, then the start pipeline; never serves | removes its own container after a handover; exits 1 on a wrong mount; on any other failure keeps retrying |
| `deployer-docker` (serve) | the keeper project's container | its module config (sdkgen) | the start pipeline, then TLS and the deploy target API as 0007 | runs until stopped |
| `deployer-docker healthcheck` | Docker | none | unchanged | exit 0 or 1 |

### Value sourcing

| Action | Value | Source |
|---|---|---|
| install command | state folder | the path you give the state mount and `--state`; the README renders the default `/var/lib/modular-platform`, and the seed checks that both match |
| install command | seed image | the keeper's catalog image, rendered into the README; before the first release, the tag your `docker build` gave it; in dev and CI, the image the task built |
| install command | seed command | `keeper.command` from the keeper's catalog entry, plus `--state <dir>`, rendered into the README |
| install command | build lines | spec 0030's build block, written as `docker build` commands (module, image, ref) |
| seed | state folder | its `--state` argument |
| seed install | files to copy | `/install`, the unzipped folder mounted read only |
| seed install | `MP_DIR` | its `--state` argument |
| seed install | `MP_UID`, `MP_GID` | fixed `65532` (this spec) |
| seed install | `COMPOSE_FILE`, `MP_KEEPER_IMAGE` | the seed's own env at a fresh install; unset in a release |
| prepare, reconcile | domain, origin, admin email | `.env` as `MP_<I>__<V>` (0015's names), written by the keeper when the web setup of #37 sends them (*Setup mode*, point 3); unset in the setup mode |
| prepare | an external's secret the setup asked | the same keeper call, written as the placed file's path in `secrets/` |
| seed install | which placed files to check | `/install/setup.json` `placed`, those with `beforeSetup` |
| seed install | own image | the seed's container, inspected (for the rerun check) |
| README, keeper log | which `show` paths to name | `setup.json` `show`, rendered from `show: once` |
| serving keeper | state folder | config value `state-dir`, default `"{install}"`, as `MP_STATE_DIR` |
| prepare | generated values | `crypto/rand`, by `format` |
| prepare | owner of written files | `MP_UID`, `MP_GID` from `.env` |
| prepare | `MP_ASK_<V>` of a step | `.env` variable named by `askEnv` |
| reconcile | Compose files | `COMPOSE_FILE` in `.env`, default `docker-compose.yml` |
| reconcile | wait deadline | existing config value `wait-timeout` (300 seconds) |
| self check | own container | host name, which Docker sets to the short container ID |
| self check | expected keeper project | `modular-platform-keeper-` plus the first 12 hex of the definition hash |
| self check | definition hash | `keeper.yml` loaded with `.env`, plus the local image ID |
| handover | deadline | config value `handover-timeout`, default `10m` |
| log lines | created, recreated, started, removed instances | container IDs and states of the platform project listed before and after reconcile |

### Key invariants

- The keeper runs Compose only for the platform project and keeper projects; a platform project name other than `modular-platform` stops the pipeline (`state_invalid`), and no call names an `mp-` project.
- A keeper removes its own container only as the last step of a handover, after its successor is healthy.
- At most one keeper runs lock to unlock at a time.
- Prepare never deletes or overwrites a file, and no script on the host writes the state folder.
- No secret value appears in a log line, in the env of any container, or in any command's output; for a `show: once` file only its path is logged.
- The keeper project and the platform project never share a container; `--remove-orphans` on the platform project can never reach a keeper.

### Security model

- The state folder is root only (0700); secret files are 0600 for `MP_UID`, so a container sees only the files mounted into it.
- The seed and the keeper hold the Docker socket and are root equivalent, as the deployer already is (0007). The install command needs Docker access, which is root equivalent already, and no `sudo`. Only keeper containers mount the state folder, and only they write it.
- The platform files are trusted input: the keeper runs whatever the state folder says without `render.Guard`, because writing that folder already needs root. `api` cannot reach them: nothing it calls writes there, and 0007's project rule (`^mp-`) keeps stacks apart.
- A `show: once` file (#37's setup code) stays root only in the state folder; only its path is logged, and reading it needs root on the host, which also shows that you own the host.

### Configuration required

The `deployer-docker` catalog entry's config values and settings after this spec (sdkgen writes the config into `internal/config`):

| Value or setting | Env | Default | Change |
|---|---|---|---|
| `state-dir` | `MP_STATE_DIR` | `"{install}"` | new |
| `handover-timeout` | `MP_HANDOVER_TIMEOUT` | `10m`; at least `wait-timeout` plus 5 minutes, else the keeper stops at start | new |
| `stacks-dir` | `MP_DEPLOYMENTS_DIR` | `"{install}/stacks"` | unchanged; the folder comes from `setup.json` `folders` |
| `platform-uid`, `platform-gid` | `MP_PLATFORM_UID`, `MP_PLATFORM_GID` | `"{uid}"`, `"{gid}"` | unchanged; they now resolve to `65532` from `.env` |
| `wait-timeout`, `pull-timeout` | as 0007 | 300 s, 10 minutes | unchanged; also used by reconcile |
| `platform-http-port`, `platform-https-port` | as 0015 | from the `ingress` binding | unchanged |
| `host_mounts` | none | Docker socket; `{install: .}` at the same path, `read_only: false`; `{install: stacks}`, `create: true`, as today | adds `{install: .}` (task 4) |
| `hardening` | none | `user: root`, root filesystem read only, `tmpfs: [/tmp]` | unchanged |
| image | none | `docker:<version>-cli` based, as 0007 (it carries the Compose plugin; not distroless) | unchanged |

Compose variables in the state `.env` (not module config):
- `MP_KEEPER_IMAGE`: the keeper image for dev and CI; unset in a release.
- `COMPOSE_FILE`: the platform project's files; unset in a release.

### Observability

`log/slog` JSON lines from the keeper, never a secret value or file content:

| Message | Level | Fields |
|---|---|---|
| `keeper start` | info | `role` (`seed` or `keeper`), `definition` (12 hex) |
| `install done` | info | `state` (path), `copied` (number of files) |
| `install refused` | error | `code` (`docker_unreachable`, `self_unknown`, `mount_missing`, `mount_mismatch`, `install_is_state`, `install_missing`, `placed_missing`, `installed_differs`), `fix` (the flag or file to add), `paths` with descriptions for `placed_missing` |
| `show` | info | `path`, `description` (never the value), once per `show` entry whose file exists, at each start of a serving keeper |
| `prepare done` | info | `generated` (paths), `steps` (`instance/step` or `instance/step/client`), `folders` |
| `reconcile done` | info | `created`, `recreated`, `started`, `removed` (instance names), `duration` |
| `pipeline failed` | error | `step`, `attempt`, `retryIn`, `error` (problem code plus at most the last 16 KB of Compose's output) |
| `handover started` / `handover done` / `handover failed` | info, info, error | `from`, `to` (keeper projects), `duration` |

### Critical test scenarios

- e2e (`e2e` tag) on a clean Docker: the README's `docker run` → one seed, then all platform services healthy, one keeper, no seed; no secret value in any log or `docker inspect` env; the keeper logs each `show` path; read the setup code, finish #37's setup, sign in and deploy the sample, verifies **AC-1**, **AC-2**, **AC-3**, **AC-12**
- Restart: `docker restart` of the keeper, then the README's `docker run` again → same container IDs and volumes, no new service container, no changed file under `secrets/` (`find -newer`), the seed gone; `docker stop` of the keeper then the `docker run` → the stopped keeper started again with its ID, the seed gone, verifies **AC-4**
- Reboot during a handover: stop Docker while both the seed and the successor run, start it again → only the successor restarts, its cleanup removes the stopped seed, one keeper and no seed remain; reboot before any successor exists → the seed stays stopped, `docker start modular-platform-seed` finishes the install, verifies **AC-4**, **AC-6**, **AC-8**
- Partial writes: kill the keeper while a step runs (fault injection in the unit test, and an e2e `docker kill` during `cert-issuer`'s `ca` step) → after the next start each written file is whole and the CA files belong together, verifies **AC-2**
- Interrupted install: `docker kill` the seed during its copy, `docker start` it → the copy runs again from the start, and `docker-compose.yml` is the last file to appear in the state folder, verifies **AC-1**
- Mounts: `docker run` without the socket, with the state folder at another path inside, through a symlinked host path, with `--state /var/lib/x/`, with `/install` at the state path, with `--hostname x`, or with no `/install` on a fresh folder → exit 1 with the matching code, nothing copied, the seed stays stopped, verifies **AC-13**
- Rerun with differences: on an installed host, run the `docker run` from a newer unzipped folder, or with another `MP_KEEPER_IMAGE` → exit 1, `installed_differs` names what differs, no file changed, verifies **AC-4**
- Drift: delete `authelia`'s container, recreate `renderer` by hand from an edited copy of the file, stop `web`, start a container labeled with the platform project and an unknown service, restart the keeper → `authelia` back, `renderer` recreated, `web` running, the unknown one gone, verifies **AC-5**
- Self: set `MP_KEEPER_IMAGE` to a retagged keeper image (another image ID), restart the keeper → a new keeper project, healthy, the old container gone; with a successor image whose healthcheck fails and `handover-timeout` 1m → successor removed, old keeper still serving, verifies **AC-6**
- Stacks: deploy the sample, record its container IDs, start times and restart counts, run every start above → unchanged, verifies **AC-7**
- Lock: unit test with two pipelines on one state folder (fake Docker) → the second waits; e2e: two seeds started at once → one keeper, no duplicate service container, verifies **AC-8**
- Failure: point an instance's image at a tag that does not exist → `pipeline failed` with `step: reconcile`, the keeper serves `GET /v1/stacks/mp-whoami-postgres/state`, retries spaced 1, 2, 4 seconds (fake clock in the unit test), fixing the tag heals at the next retry, verifies **AC-9**
- Contract: catalog fixtures refused, one each (`show: once` on `from: setup`, an unknown `format`); render errors for no keeper, two keepers, a `from: ask` secret, a step with `asks`, an asked secret config value, verifies **AC-10**
- Placed: a graph whose setup mode needs an external's secret field, with no file → the seed logs `install refused` `placed_missing` with the path and description, copies nothing, starts no other container, and goes on once the file is placed in the unzipped folder; a missing placed file with `beforeSetup` false → the install goes on, verifies **AC-11**

## Build plan

Tracer Bullet, starting after #3's milestone 4 (the platform is a graph, 0015). Tasks 1 to 3 may land before #37; task 4 waits until #37's spec and build are done, because the platform flavor refuses the `authelia` step with `asks` that #37 removes, and the first start needs #37's setup mode (*Setup mode*). Task 4 closes the thin thread from render to a running platform; the later tasks thicken it.

1. Spike, recorded in [rationale.md](rationale.md): inside the `docker:<version>-cli` image the deployer uses, check that `docker compose --project-directory <dir> up` with no `-f` takes `COMPOSE_FILE` from `<dir>/.env`; that a second project from `keeper.yml` with an external network and the same alias starts beside a running keeper; that removing its own container with `force` from inside ends the process cleanly; and whether the pinned Compose recreates a service when only a `configs` file's content changes (record the answer in the README's drift note), satisfies **AC-12**, **AC-6**, **AC-5**
2. Contract: `keeper`, `format` and `show` in `contracts/openapi/renderer.yaml`, `task generate`; `libs/contract` loads and checks them (`invalid_secret`); `deployer-docker` declares `keeper: {command: [seed]}`. The rest of the `deployer-docker` entry waits for task 4 (see *Contract changes*), satisfies **AC-10**
3. Render, behind `render.WithKeeper()`: first `{install: .}` in `libs/contract` and a bare `{install}` default in `libs/render` (see *Contract changes*, additive, nothing in the real catalog uses them yet); then the platform flavor's split into `docker-compose.yml` and `keeper.yml`, `render.Setup` and `setup.json`, the refusals and keeper rule, the README's install commands (no platform `setup.sh`); goldens for each file in `testdata/platform-keeper/`, rendered with the task 4 shape of the `deployer-docker` entry and an `authelia` stand in without `asks` as fixtures. Milestone 4's output and its golden stay unchanged, satisfies **AC-1**, **AC-10**, **AC-11**
4. Keeper happy path in `services/deployer-docker/internal/keeper` (the pipeline, behind the `docker` adapter): first the switch, in the same change: the `deployer-docker` entry (the `{install: .}` mount, `state-dir` and `handover-timeout`, then `task generate` for its sdkgen config) moves from the task 3 fixture into `catalog/modules/`, the `authelia` stand in goes away for the entry #37 left, `render.Platform` renders the keeper output by default, and the option, milestone 4's path and its golden go away; then the `seed` command with its mounts check and install (the copy and `.env`), lock, check, prepare (atomic generate, steps through moby with the scratch folder, folders), reconcile, definition hash and self check, handover, cleanup, the server and event stream started after the first pipeline outcome, the healthcheck gated on TLS; then `task dev:managed` and `scripts/smoke-managed.sh` switched to the README's `docker run` of the seed with `-e COMPOSE_FILE` and `-e MP_KEEPER_IMAGE` (no `sudo`, no wrapper container), the overlays dropping `build:` and the `deployer-docker` service, satisfies **AC-1**, **AC-2**, **AC-3**, **AC-12**, **AC-13**
5. Smoke and e2e assertions for starting again, drift, untouched stacks, an interrupted install and the wrong mounts, satisfies **AC-1**, **AC-4**, **AC-5**, **AC-7**, **AC-13**
6. Failure paths: backoff with an injectable clock, serving while retrying, the certificate retry, the handover timeout and the successor removal, the lock test with two pipelines, the reboot during a handover, the partial write faults, satisfies **AC-9**, **AC-6**, **AC-8**, **AC-2**
7. Docs: the managed README (the install commands, before the first release with the `docker build` lines; where the state lives and how to pick another path; following the first start with `docker logs -f modular-platform-seed`; the `show` paths and that deleting such a file loses it for good; starting again with the same `docker run`; what the drift rule does not see; and that the platform network stays while a keeper runs), the deployer README's host access section, `.env.example`, satisfies **AC-1**, **AC-3**, **AC-5**

## Consequences

**Positive**:
- One entry point brings the platform up from its files; #23, #25, #19 and #33 build on it instead of each inventing a start.
- No script runs on the host, and nothing shows or logs a secret value; secrets live in a root only folder.
- You, dev and the smoke job install the same way, with one `docker run`; the install checks live in Go, under test, not in shell.
- The deployer can replace itself without downtime for its consumers beyond the handover, which #25 reuses for upgrades.
- A plain `docker compose up -d` in the state folder still gives the same platform containers, so you can always fall back to the standard tool.

**Negative / tradeoffs**:
- The `docker run` line is long and pasted by hand; the seed refuses a wrong mount, but a state path typed the same wrong way twice installs elsewhere. The state no longer sits where you unzipped; you must know `/var/lib/modular-platform`.
- Before the first release you run one `docker build` per platform image (four today) before the `docker run`.
- Task 4 of this feature waits for #37.
- The keeper becomes the most complex code in the root equivalent service: a lock, a step runner, a self handover.
- Compose's rule misses hand changes such as `docker update` or a disconnected network; such a container stays wrong until something recreates it.
- The platform flavor has no `setup.sh` while the static flavor keeps one, so the two installs differ, and `setup.json` is one more rendered format to keep golden.
- Milestone 4's platform `setup.sh` (its secrets, steps and spec 0030's build block) is built and then removed by this feature.
- Between tasks 3 and 4 the platform flavor has two output paths and two goldens, and the task 4 shape of `deployer-docker` plus an `authelia` stand in live as test fixtures; task 4 must remove the old path, or it lingers.

**Neutral**:
- Test installs made under milestone 4 are reinstalled (pre release, as 0011's migration already says).
- `MP_UID` and `MP_GID` become the fixed `65532` instead of the installing user's IDs.

## Migration plan

**Strategy**: big bang, pre release; no published release zip exists yet.
**Phases**:
1. Tasks 1 to 3 land beside milestone 4's working `setup.sh`: the catalog entries keep milestone 4's shape (only the new contract fields and `deployer-docker`'s `keeper` land), and the new render runs only behind `render.WithKeeper()`, so `task managed:render`, dev and smoke render and run milestone 4's platform until task 4.
2. Once #37 is done, task 4 switches the `deployer-docker` entry, the render default (which drops the platform `setup.sh`), the release zip, `task dev:managed` and smoke in one change, once the seeded smoke passes.
**Rollback**: revert the task 4 merge; the entries, the render default and the golden then go back to milestone 4's, so the render emits milestone 4's `setup.sh` and the deployer as a Compose service again.
**Risks**: Compose reading `COMPOSE_FILE` from the project `.env` (task 1 checks it); a successor that can never get healthy loops on the backoff (bounded by the timeout, logged); a keeper image that cannot be pulled leaves only the seed retrying, visible in `docker logs modular-platform-seed`.

## Follow-up

- [x] Enroll a new scope feature, "First run setup on the web": enrolled as #37, and since 2026-11-20 built before this feature. Its spec must deliver the four points of *Setup mode*.
- [ ] Offer a newer install as an optional update you accept in the running stack (a newer unzipped folder, another keeper image), instead of the `installed_differs` refusal; with #25 and the stack editor.
- [ ] #25 builds the drain contract (503 `handing_over`, `api` retrying) when it triggers a handover on a keeper that already serves.
- [ ] Scope #32's done line says "a container changed by hand"; with Compose's rule this means "recreated from another definition", not `docker update`. Adjust the wording when the scope is next reconciled.
- [ ] #33 maps the keeper and its handover onto the cluster (likely an operator with a rolling update of itself).
- [ ] Spec 0015's platform `setup.sh` and the deployer as a Compose service are replaced by this spec once task 4 lands (see *What this replaces*); 0015 carries a pointer.
- [ ] 0007 says only deployed containers carry the `stack` label, but 0015 gives platform containers `stack: platform`, so the event stream also records platform exits. Harmless (the map is capped), but 0007's sentence is wrong; correct it when 0007 is next touched.
- [ ] Private registries for step and keeper images need credentials in the keeper; not covered here.
- [ ] The scope text of #40 (`setup.sh` asks whether to continue without a domain) and #23's done line (`setup.sh` and `docker compose up`) still name the platform `setup.sh`; with #37 first and no platform `setup.sh`, adjust them when those features are designed.
- [ ] Carry the platform files in the keeper image, so a released install needs no zip and an upgrade is a new image; weigh it with #25.
- [ ] The uncommitted task 3 work renders a platform `setup.sh` (`libs/render/templates/keeper-setup.sh.tmpl`, parts of `libs/render/setup.go`); `/develop` replaces it with the README's install commands.
