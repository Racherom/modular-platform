# 0016. Start the platform from one container

**Date**: 2026-11-05
**Status**: In Progress
**Scope**: [#32 Start the platform from one container](../../scope/scope.md)
**Builds on**: [0007](../0006-managed-mode-control-plane/0007-deploy-target-deployer.md) (the deployer), [0013](../0011-capability-module-contract/0013-module-manifest-v1.md) (install time), [0015](../0011-capability-module-contract/0015-platform-graph-managed-zip.md) (the platform graph and its render)

## Summary

The managed platform starts from one container: the deployer, which in this role is called the keeper (the one service that keeps the platform's own containers in line with its files). `setup.sh` runs as root, asks three plain answers, copies the rendered files into a root only state folder and starts a seed container. From then on the keeper creates every secret, runs every setup step, brings up the other platform services through Compose, and replaces its own container when its files change. Deployed stacks are never touched, and no shell script ever creates a secret.

## Requirements

**User stories**:
- As a self hoster, I want to install the platform with one script that starts one container, so that the platform creates its own secrets and I never handle them.
- As an admin, I want the platform to come back as its files describe each time it starts, so that a deleted or wrongly recreated container heals, without duplicates and without touching my deployed stacks.
- As a platform maintainer, I want one place that brings the platform up from its files, so that replacing a module (#23), upgrading (#25) and live changes (#19) all go through it.

**Acceptance criteria**:
- **AC-1**: On a clean host, `sudo sh setup.sh` asks only the domain, the origin and the admin email, copies the folder into the state folder (default `/var/lib/modular-platform`, mode 0700, owned by root), and creates exactly one container: the seed, with the Docker socket, the state folder and `restart: unless-stopped`. It writes no file under `secrets/`.
- **AC-2**: From the seed alone, every generated secret (the binding tokens included), every setup step output and every `data/` folder come to exist, every platform service runs healthy in the Compose project `modular-platform`, and then exactly one keeper container runs (project `modular-platform-keeper-<hash>`) and the seed container is gone.
- **AC-3**: At a fresh install `setup.sh` prints every `show: once` secret once (the bundled Authelia's admin password for the user `admin`). You sign in with it and deploy a stack as today. The value never appears in a log line or in any container's env.
- **AC-4**: Starting the platform again (`docker restart` of the keeper, a Docker or host restart, or `sudo sh setup.sh` again) on a host where it already ran keeps every matching container (same container IDs), reuses every volume, creates no duplicate, changes no secret or step output, and `setup.sh` asks nothing.
- **AC-5**: At the next start, a platform container that was deleted is created, one whose Compose config hash or image ID no longer matches the files is recreated, a stopped one is started, and a container of the platform project with no service in the files is removed. Changes Compose's rule cannot see (`docker update`, `docker network disconnect`) stay until the container is recreated for another reason; the README says so.
- **AC-6**: When `keeper.yml`, `MP_KEEPER_IMAGE` or the keeper image ID changes, the next start creates a successor keeper; once the successor is healthy the old keeper removes itself, leaving exactly one keeper. A successor that is not healthy within `handover-timeout` (10 minutes) is removed and the old keeper keeps its job.
- **AC-7**: No deployed stack's container is touched by any platform start: the IDs, start times and restart counts of every `mp-*` container stay the same, and deployed stacks keep restarting through Docker's restart policy.
- **AC-8**: Only one keeper runs the start pipeline at a time; two keepers starting together run it one after the other and never create a duplicate container.
- **AC-9**: When prepare or reconcile fails (a pull fails, a step exits non zero, a module never gets healthy), the keeper logs the step and the error, a keeper whose definition matches still serves the deploy target API, and the pipeline runs again after 1 second, doubling up to 5 minutes, until it succeeds.
- **AC-10**: The contract carries the new fields: `keeper` on a module, `show: once` and `format: password` on a generated secret. The platform flavor refuses a graph with no keeper instance or more than one, a `from: ask` secret, a setup step with `asks`, and an asked config value of type `secret`.
- **AC-11**: When a placed file (an external secret field, 0012) is missing, `setup.sh` lists every missing path with its description and exits 1 before it creates the state folder or any container.
- **AC-12**: `task dev:managed` and `scripts/smoke-managed.sh` start the platform through `setup.sh` and the seed, with their overlays named by `COMPOSE_FILE` and their keeper image by `MP_KEEPER_IMAGE` in the state `.env`; the smoke job checks AC-2 to AC-7.

## Decision

**Chosen option**: Option 1: the deployer as keeper, started from a seed, with a self handover.

The deployer becomes the platform's keeper: a seed container made by `setup.sh` with plain `docker run` runs the keeper's start pipeline (lock, prepare, reconcile with Compose, self check), then hands over to the keeper container that `keeper.yml` describes, which runs the same pipeline at every start and replaces itself the same way when its definition changes.

**Implementation skills**: `golang-concurrency` (`samber/cc-skills-golang`, `.agents/skills/golang-concurrency/`) · `golang-context` (`samber/cc-skills-golang`, `.agents/skills/golang-context/`) · `golang-error-handling` (`samber/cc-skills-golang`, `.agents/skills/golang-error-handling/`) · `golang-security` (`samber/cc-skills-golang`, `.agents/skills/golang-security/`) · `golang-observability` (`samber/cc-skills-golang`, `.agents/skills/golang-observability/`) · `golang-testing` (`samber/cc-skills-golang`, `.agents/skills/golang-testing/`) · `openapi-spec-generation` (`wshobson/agents`, `.agents/skills/openapi-spec-generation/`)

## Rationale

The reasoning, the options weighed and the smaller choices are in [rationale.md](rationale.md).

## Feature design

### Terms

| Term | Meaning |
|---|---|
| keeper | The module instance whose module declares `keeper` (today `deployer-docker`). It keeps the platform's containers in line with the state folder. |
| seed | The container `setup.sh` creates with `docker run`, named `modular-platform-seed`. It runs the keeper image with the keeper's `command` (`seed --state <dir>`), never serves, and removes itself after the handover. |
| state folder | The root only folder that holds the platform's files: `$MP_STATE_DIR`, default `/var/lib/modular-platform`. `{install}` (0015) renders to it as `${MP_DIR}`. |
| platform project | The Compose project `modular-platform`: every platform instance except the keeper. |
| keeper project | `modular-platform-keeper-<first 12 hex of the definition hash>`: one Compose project per keeper definition, holding only the keeper. |
| definition hash | SHA 256 over the canonical JSON (keys sorted, every value resolved) of the keeper service as compose-go loads it from `keeper.yml` with the state `.env` as the only variable source (an empty process env, so the seed and the keeper compute the same hash), followed by the local image ID of its image (pulled first when missing). A keeper image with another compose-go version may serialize differently; that only adds a handover to an upgrade, which the image ID change causes anyway. |

### What this replaces

- 0013 *Install time*, for the platform flavor only: steps 3 to 6 (asked secrets, generated files, setup steps, folders) move from `setup.sh` into the keeper's prepare; step 1 writes the fixed `65532` instead of `id -u`; step 2 asks only values that are not secret; step 7 stays in `setup.sh`. The static flavor keeps 0013 as written.
- 0015: the `deployer-docker` service in the platform's `docker-compose.yml` (it moves to `keeper.yml`), the `authelia` `users` step's questions (replaced by the generated `admin-password`), the admin user name question, the platform `setup.sh` and the instruction to run `docker compose up -d`.

### State folder (the data model)

The platform keeps no database for this feature; its state is this folder. The folder itself is 0700, owned by root.

| Path | Written by | Mode, owner | Holds |
|---|---|---|---|
| `docker-compose.yml`, `keeper.yml`, `setup.json`, `config/`, `graph.yaml`, `README.md`, `.env.example` | `setup.sh`, copied once from the zip | as in the zip, root | the rendered platform (0015, plus the changes below) |
| `.env` | `setup.sh`, once | 0600, root | `MP_DIR`, `MP_UID=65532`, `MP_GID=65532`, the asked answers `MP_<I>__<V>`, and when set at install, `COMPOSE_FILE` and `MP_KEEPER_IMAGE` |
| `secrets/<i>/…` | keeper (prepare) | files 0600 (0644 where a step `writes` says so), owner `MP_UID:MP_GID`; folders 0700 | generated secrets, binding tokens, step outputs, files you place |
| `data/<i>/<n>/`, `host_mounts` with `create: true` | keeper (prepare) | 0700, `MP_UID:MP_GID` | module data |
| `deployments/` | keeper (0007, unchanged) | as 0007 | deployed stacks' folders |
| `.keeper.lock` | keeper | 0600, root | the start pipeline lock (`flock`) |
| `.shown` | `setup.sh` | 0600, root | marker: the `show: once` secrets were printed |
| `.prepare/` | keeper | 0700, root | scratch space for generated files and step outputs before they move into place; emptied at each prepare |
| overlay files (`compose.dev.yaml`, `compose.ci.yaml`) | dev and CI tasks only | root | named by `COMPOSE_FILE` |

`MP_UID` and `MP_GID` are the fixed `65532` (the distroless `nonroot` user), because `setup.sh` now runs as root and `id -u` would give 0.

### Contract changes (0013)

- **Module field `keeper`**: `keeper: {command: [<args>]}` names the command a seed runs (`deployer-docker`: `[seed]`; the seed appends `--state <dir>`). A keeper module is host access like `host_mounts`. Only the platform flavor reads it; the static and managed flavors render the module as any other (and the managed flavor refuses it through `host_access`, as today).
- **Secret fields**: `format: hex` (default, 32 random bytes as hex) or `format: password` (24 characters drawn uniformly from `A` to `Z`, `a` to `z` and `0` to `9`, about 142 bits); `show: once`, allowed only with `from: generate`. Anything else is `invalid_secret` at catalog load.
- **Platform flavor refusals** (render error `setup_required` at `modules.<i>`, naming the field): a secret with `from: ask`, a setup step with `asks`, a config value with `setup: ask` and type `secret`. Asked config values that are not secret stay; `setup.sh` asks them. (The coming web setup feature takes over the asking; see Follow-up.)
- **Platform flavor keeper rule** (render error): no instance with a keeper module is `keeper_missing`, more than one is `keeper_duplicate`, at `modules`.
- **`authelia` entry** (0015): new secret `admin-password` (`from: generate`, `format: password`, `show: once`, description `Initial password of the admin user "admin"`); the `users` step drops its `asks`, reads `{secret: admin-password}`, takes the user name from literal env `ADMIN_USER: admin` and the email from `MP_ASK_ADMIN_EMAIL` as before. The admin user name question goes away.
- **`deployer-docker` entry** (0015): `keeper: {command: [seed]}`; its host mounts become the Docker socket and `{install: .}` at the same path with `read_only: false` (it writes secrets, step outputs and deployments), replacing the `{install: deployments}` mount; new config values below.

### Render changes (0015's platform flavor)

- `docker-compose.yml`: every instance except the keeper. No service has `depends_on` on the keeper; its consumers (`api`) reach it once it serves, and their per call errors already cover a deployer that is not there yet.
- `keeper.yml`: a Compose file with only the keeper service and no top level `name:`. Its network is `internal: {external: true, name: modular-platform_internal}` with the keeper's alias; image `${MP_KEEPER_IMAGE:-<catalog image>}`; `restart: unless-stopped`; 0015's labels (`deployment: platform`, `instance`, `module`) and healthcheck; everything else rendered as for any instance.
- `setup.json`: the install actions the keeper applies, a Go type `render.Setup` in `libs/render`, written as canonical JSON. It covers every instance, the keeper included, so the keeper's own binding tokens, the CA from `cert-issuer`'s `ca` step and the caller public key exist before `keeper.yml` mounts them:
  - `formatVersion`: `1`. The keeper refuses another version and any unknown field (`state_invalid`).
  - `generate`: `[{path, format, show, readers}]` in 0013's step 4 order (module secrets, per binding files, empty job outputs). `readers` lists the `writes` of every step that reads the file.
  - `steps`: `[{instance, name, image, entrypoint, command, env, askEnv, reads, writes}]`, in 0013's step 5 order, a `for_each` step already expanded to one entry per client with `{client}` replaced. `askEnv` maps each `MP_ASK_<V>` to its `.env` variable `MP_<I>__<V>`. `reads` gives `{path, mount}` pairs; `writes` gives `{name, mode}`.
  - `folders`: `[{path, mode}]` for `data/<i>/<n>`, `host_mounts` with `create: true` (the keeper's `deployments/` among them) and the `for_each` client folders.
  - `show`: `[{path, description}]` for every `show: once` secret, in instance then name order.
  - `keeper`: `{instance, module, command}`.
- `setup.sh`: the new shape below. The static flavor's `setup.sh` does not change.
- The golden test covers `docker-compose.yml`, `keeper.yml`, `setup.json` and `setup.sh`, and `render.Guard` is never run on these files (they are trusted, see Security model). A render test checks that every file path `keeper.yml` mounts is a `generate` path, a step `writes` path or a folder in `setup.json`.

### setup.sh (platform flavor)

It runs these steps in order, with `set -eu`:
1. Stop unless it runs as root: `run as root: sudo sh setup.sh`, exit 1.
2. State folder: `$MP_STATE_DIR` or `/var/lib/modular-platform`; it must be absolute.
3. Placed files: for each placed path (0013 step 7), look in the state folder when it holds a `docker-compose.yml`, else in the folder `setup.sh` lives in. Missing ones are listed with their description, then exit 1 (AC-11).
4. Fresh install (no `docker-compose.yml` in the state folder): create the folder (0700, root), copy every file of the zip folder except `setup.sh` (placed secrets included), write `.env` with `MP_DIR`, `MP_UID`, `MP_GID`, plus `COMPOSE_FILE` and `MP_KEEPER_IMAGE` when `setup.sh` was started with them set, then ask the asked values in 0015's order without the admin user name and password (domain, origin, admin email) under 0013's answer rules. `setup.sh` writes every value unquoted, one `NAME=value` per line, with each `$` written as `$$` so Compose never expands it. An existing state folder: ask nothing, copy nothing, change no file, and ignore `COMPOSE_FILE` and `MP_KEEPER_IMAGE` in its own environment.
5. Keeper: a keeper container is one with the labels `io.github.racherom.modular-platform.deployment=platform` and `io.github.racherom.modular-platform.instance=<keeper instance>` and a `com.docker.compose.project` starting with `modular-platform-keeper-`. When one is running, print `already running` and go on. Otherwise remove a leftover `modular-platform-seed` container and run `docker run -d --name modular-platform-seed --restart unless-stopped` with only the labels `deployment=platform` and `module=<keeper module>` (never `instance`, so a seed never counts as a keeper), `-v /var/run/docker.sock:/var/run/docker.sock`, `-v "$dir:$dir"`, the image read with `sed -n 's/^MP_KEEPER_IMAGE=//p' "$dir/.env"` or else the rendered default, and the keeper command plus `--state "$dir"`.
6. When `<state>/.shown` is missing (a fresh install, or a run that stopped before printing): wait up to 15 minutes until a keeper container is healthy and `modular-platform-seed` is gone, then print every `show` file once as `<description>: <value>`, then the origin to open, then write `.shown`. Stop waiting early when the seed's `RestartCount` passes 3. On timeout or that early stop: `the platform did not finish its first start; see: docker logs modular-platform-seed`, exit 1; the next run prints them.

### Start pipeline (state transitions)

Every keeper process, seed or serving, runs this at start:

```text
lock ─▶ check ─▶ prepare ─▶ reconcile ─▶ self check ─┬─ matches ──▶ cleanup ─▶ unlock ─▶ (serving keeper) TLS ─▶ serve
                                                      └─ differs ──▶ handover
any failure in check, prepare or reconcile:
   definition matches and not the seed ─▶ unlock ─▶ TLS ─▶ serve, run the pipeline again after the backoff
   otherwise (seed, or an outdated keeper) ─▶ unlock ─▶ wait the backoff ─▶ lock …
```

1. **lock**: an exclusive `flock` on `<state>/.keeper.lock`, waiting while another keeper holds it.
2. **check**: the state folder holds `docker-compose.yml`, `keeper.yml`, `setup.json` and `.env`; the platform project name (the file's `name:`) is `modular-platform`; else the error `state_invalid`.
3. **prepare**: apply `setup.json`, for every instance, the keeper included. Every file lands atomically: it is written in `.prepare/`, synced, given its mode and owner, then moved to its path with `renameat2(RENAME_NOREPLACE)`, so a crash leaves either no file or a whole one, never an empty or cut one.
   - Each missing `generate` file is made this way, mode 0600, owner `MP_UID:MP_GID`, except a `show: once` file whose `readers` already exist, which is never made again (so deleting it never leaves a password no account uses; the README says that the password is then gone, and the account keeps working).
   - Each step whose `writes` are all present is skipped; any other runs through the moby client as 0013 says: `--rm`, `--network none`, user `MP_UID:MP_GID`, `/out` bound to a fresh scratch folder in `.prepare/`, its reads bound read only, env plus `MP_GRAPH`, `MP_INSTANCE` and each `askEnv` value read from `.env`. Its image is pulled when missing, with no registry credentials (public images only for now). Only when it exits 0 and every `writes` entry exists in the scratch folder are they moved into `secrets/<i>/` with their modes; a write whose target already exists is left as it is and the scratch copy dropped. A non zero exit or a missing entry fails prepare and moves nothing, so a step that wrote half its files runs again from nothing and never replaces a key already in use.
   - Then the `folders` (0700, `MP_UID:MP_GID`, made when missing).
   - Prepare never deletes or overwrites a file in `secrets/` or `data/`.
4. **reconcile**: `docker compose --project-directory <state> up -d --remove-orphans --wait --wait-timeout <wait-timeout>`, run in the state folder with no `-f`, so Compose takes `COMPOSE_FILE` from `.env` (default `docker-compose.yml`). Compose's rule decides what to keep, start, recreate and create. Its env is the deployer's fixed `composeEnv` (0007).
5. **self check**: compute the definition hash (a pull failure of the keeper image fails this step). Self is the container whose ID starts with the process's host name (Docker's default host name; the render never sets `hostname`, and neither may an overlay). Zero or several matching containers stop the pipeline with `state_invalid`, logged at error level. Self matches when its `com.docker.compose.project` label equals the expected keeper project.
6. **cleanup** (matching keeper only): remove every keeper container that is not running (a stopped outdated keeper, a seed left behind). A running one is left alone; it removes itself.
7. **unlock**, then the serving keeper gets its certificate through ACME (spec 0008; `cert-issuer` runs by now) and serves as today. A failed certificate request retries on the backoff below; meanwhile the keeper is not healthy.

**Serving and health**: a keeper opens 0007's event stream and its HTTP server only after its first pipeline outcome (success, or a failure while its definition matches). The seed opens neither and has no healthcheck. The keeper's healthcheck (`deployer-docker healthcheck`) reports healthy only once TLS is up and the server answers, and `keeper.yml` gives it a `start_period` of 5 minutes. Each keeper process picks a new `bootId` (0007), so a handover is, for `api`, a deployer restart: running operations' outcomes are unknown, as today.

**Compose env**: reconcile and both handover commands run with 0007's fixed `composeEnv` and the state folder as working directory. They rely on one invariant: the state folder is mounted into every keeper and the seed at the same absolute path as on the host, so the paths Compose hands to the daemon are host paths.

**Handover** (self differs):
1. `docker compose --project-directory <state> --project-name <expected keeper project> -f keeper.yml up -d` (Compose reuses a successor container a crashed earlier handover left). A missing `modular-platform_internal` network fails this step, and the next run's reconcile creates it again.
2. Unlock, so the successor can run its own pipeline.
3. Poll the successor's health every 2 seconds, up to `handover-timeout`.
4. Healthy: remove self with `force` through the moby client. The process ends with it. (A drain for a keeper that was already serving is specified below and built with #25; at start nothing runs yet.)
5. Timeout, or the successor exited: `docker compose --project-name <expected> down`, log `handover failed`. A seed waits the backoff and runs the pipeline again. An outdated keeper serves with its current definition and runs the whole pipeline again after the backoff.

**Backoff**: 1 second, doubling, at most 5 minutes; a successful pipeline resets it. Each run of the pipeline after a failure takes the lock like the first one.

**Drain contract (written now, built with #25)**: when a keeper that already serves starts a handover, it answers new apply and remove calls with 503 `handing_over` (a `ProblemList`), finishes running operations within their deadlines, and waits until each finished operation was read once in its final state (or 60 seconds passed) before it removes itself. `api` retries `handing_over` with its own backoff.

### Interface surface

No new HTTP endpoint. The deployer's command line:

| Command | Started by | Inputs | Does | Ends |
|---|---|---|---|---|
| `deployer-docker seed --state <dir>` | `setup.sh` (`docker run`) | `--state`, absolute | the start pipeline, never serves | removes its own container after a handover; on failure keeps retrying |
| `deployer-docker` (serve) | the keeper project's container | its module config (sdkgen) | the start pipeline, then TLS and the deploy target API as 0007 | runs until stopped |
| `deployer-docker healthcheck` | Docker | none | unchanged | exit 0 or 1 |

### Value sourcing

| Action | Value | Source |
|---|---|---|
| setup.sh | state folder | `$MP_STATE_DIR`, else the default `/var/lib/modular-platform` |
| setup.sh | `MP_DIR` | the state folder path |
| setup.sh | `MP_UID`, `MP_GID` | fixed `65532` (this spec) |
| setup.sh | domain, origin, admin email | asked (0015's order, 0013's rules) |
| setup.sh | seed image | `MP_KEEPER_IMAGE` in the state `.env`, else the catalog image rendered into `setup.sh` |
| setup.sh | seed command | `keeper.command` from the keeper's catalog entry, plus `--state <dir>` |
| setup.sh | which secrets to print | `setup.json` `show`, rendered from `show: once` |
| seed | state folder | its `--state` argument |
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
- Prepare never deletes or overwrites a file, and `setup.sh` writes nothing under `secrets/`.
- No secret value appears in a log line, in the env of any container, or in `setup.sh`'s output, except the one `show: once` print at a fresh install.
- The keeper project and the platform project never share a container; `--remove-orphans` on the platform project can never reach a keeper.

### Security model

- The state folder is root only (0700); secret files are 0600 for `MP_UID`, so a container sees only the files mounted into it.
- The seed and the keeper hold the Docker socket and are root equivalent, as the deployer already is (0007). Only keeper containers mount the state folder, and only they write it.
- The platform files are trusted input: the keeper runs whatever the state folder says without `render.Guard`, because writing that folder already needs root. `api` cannot reach them: nothing it calls writes there, and 0007's project rule (`^mp-`) keeps deployments apart.
- The `show: once` password stays in `secrets/authelia/admin-password` for root only; the README says it is the initial password and how to read it again.

### Configuration required

The `deployer-docker` catalog entry's config values and settings after this spec (sdkgen writes the config into `internal/config`):

| Value or setting | Env | Default | Change |
|---|---|---|---|
| `state-dir` | `MP_STATE_DIR` | `"{install}"` | new |
| `handover-timeout` | `MP_HANDOVER_TIMEOUT` | `10m`; at least `wait-timeout` plus 5 minutes, else the keeper stops at start | new |
| `deployments-dir` | `MP_DEPLOYMENTS_DIR` | `"{install}/deployments"` | unchanged; the folder comes from `setup.json` `folders` |
| `platform-uid`, `platform-gid` | `MP_PLATFORM_UID`, `MP_PLATFORM_GID` | `"{uid}"`, `"{gid}"` | unchanged; they now resolve to `65532` from `.env` |
| `wait-timeout`, `pull-timeout` | as 0007 | 300 s, 10 minutes | unchanged; also used by reconcile |
| `platform-http-port`, `platform-https-port` | as 0015 | from the `ingress` binding | unchanged |
| `host_mounts` | none | Docker socket; `{install: .}` at the same path, `read_only: false` | replaces `{install: deployments}` |
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
| `prepare done` | info | `generated` (paths), `steps` (`instance/step` or `instance/step/client`), `folders` |
| `reconcile done` | info | `created`, `recreated`, `started`, `removed` (instance names), `duration` |
| `pipeline failed` | error | `step`, `attempt`, `retryIn`, `error` (problem code plus at most the last 16 KB of Compose's output) |
| `handover started` / `handover done` / `handover failed` | info, info, error | `from`, `to` (keeper projects), `duration` |

### Critical test scenarios

- e2e (`e2e` tag) on a clean Docker: `sudo sh setup.sh` with piped answers → one seed, then all platform services healthy, one keeper, no seed, the password printed once and found in no log or `docker inspect` env, sign in and deploy the sample, verifies **AC-1**, **AC-2**, **AC-3**, **AC-12**
- Restart: `docker restart` of the keeper, then `sudo sh setup.sh` again → same container IDs and volumes, no new container, no changed file under `secrets/` (`find -newer`); `docker stop` of the keeper then `sudo sh setup.sh` → the stopped keeper started again with its ID, the seed gone, verifies **AC-4**
- Reboot during a handover: stop Docker while both the seed and the successor run, start it again → both restart, the lock orders them, one keeper and no seed remain, verifies **AC-4**, **AC-6**, **AC-8**
- Partial writes: kill the keeper while a step runs (fault injection in the unit test, and an e2e `docker kill` during `cert-issuer`'s `ca` step) → after the next start each written file is whole and the CA files belong together, verifies **AC-2**
- Rerun before the print: interrupt `setup.sh` during its wait, run it again → it asks nothing and prints the password once; a third run prints nothing, verifies **AC-3**
- Drift: delete `authelia`'s container, recreate `planner` by hand from an edited copy of the file, stop `web`, start a container labeled with the platform project and an unknown service, restart the keeper → `authelia` back, `planner` recreated, `web` running, the unknown one gone, verifies **AC-5**
- Self: set `MP_KEEPER_IMAGE` to a retagged keeper image (another image ID), restart the keeper → a new keeper project, healthy, the old container gone; with a successor image whose healthcheck fails and `handover-timeout` 1m → successor removed, old keeper still serving, verifies **AC-6**
- Stacks: deploy the sample, record its container IDs, start times and restart counts, run every start above → unchanged, verifies **AC-7**
- Lock: unit test with two pipelines on one state folder (fake Docker) → the second waits; e2e: two seeds started at once → one keeper, no duplicate service container, verifies **AC-8**
- Failure: point an instance's image at a tag that does not exist → `pipeline failed` with `step: reconcile`, the keeper serves `GET /v1/projects/mp-whoami-postgres/state`, retries spaced 1, 2, 4 seconds (fake clock in the unit test), fixing the tag heals at the next retry, verifies **AC-9**
- Contract: catalog fixtures refused, one each (`show: once` on `from: setup`, an unknown `format`); render errors for no keeper, two keepers, a `from: ask` secret, a step with `asks`, an asked secret config value, verifies **AC-10**
- Placed: a graph with an external secret field and no file → `setup.sh` lists it, exits 1, no state folder and no container exist, verifies **AC-11**

## Build plan

Tracer Bullet, starting after #3's milestone 4 (the platform is a graph, 0015). Task 4 closes the thin thread from render to a running platform; the later tasks thicken it.

1. Spike, recorded in [rationale.md](rationale.md): inside the `docker:<version>-cli` image the deployer uses, check that `docker compose --project-directory <dir> up` with no `-f` takes `COMPOSE_FILE` from `<dir>/.env`; that a second project from `keeper.yml` with an external network and the same alias starts beside a running keeper; that removing its own container with `force` from inside ends the process cleanly; and whether the pinned Compose recreates a service when only a `configs` file's content changes (record the answer in the README's drift note), satisfies **AC-12**, **AC-6**, **AC-5**
2. Contract: `keeper`, `format` and `show` in `contracts/openapi/planner.yaml`, `task generate`; `libs/contract` loads and checks them (`invalid_secret`); the `deployer-docker` and `authelia` entries change as above, satisfies **AC-10**, **AC-3**
3. Render: the platform flavor's split into `docker-compose.yml` and `keeper.yml`, `render.Setup` and `setup.json`, the refusals and keeper rule, the new `setup.sh`; goldens for each file, satisfies **AC-1**, **AC-10**, **AC-11**
4. Keeper happy path in `services/deployer-docker/internal/keeper` (the pipeline, behind the `docker` adapter): the `seed` command, lock, check, prepare (atomic generate, steps through moby with the scratch folder, folders), reconcile, definition hash and self check, handover, cleanup, the server and event stream started after the first pipeline outcome, the healthcheck gated on TLS; then `task dev:managed` and `scripts/smoke-managed.sh` switched to `setup.sh` plus the seed (run as root through a `docker run --rm` of the `docker:<version>-cli` image with the socket and both folders mounted, so no `sudo` prompt blocks a background run), the overlays dropping `build:` and the `deployer-docker` service, satisfies **AC-1**, **AC-2**, **AC-3**, **AC-12**
5. Smoke and e2e assertions for starting again, drift, untouched stacks and a rerun of `setup.sh` before the print, satisfies **AC-3**, **AC-4**, **AC-5**, **AC-7**
6. Failure paths: backoff with an injectable clock, serving while retrying, the certificate retry, the handover timeout and the successor removal, the lock test with two pipelines, the reboot during a handover, the partial write faults, satisfies **AC-9**, **AC-6**, **AC-8**, **AC-2**
7. Docs: the managed README (install with `sudo sh setup.sh`, where the state lives, the printed password and that deleting its file loses it for good, starting again, what the drift rule does not see, and that the platform network stays while a keeper runs), the deployer README's host access section, `.env.example`, satisfies **AC-1**, **AC-3**, **AC-5**

## Consequences

**Positive**:
- One entry point brings the platform up from its files; #23, #25, #19 and #33 build on it instead of each inventing a start.
- No shell script creates or sees a secret, except the one printed initial password; secrets live in a root only folder.
- The deployer can replace itself without downtime for its consumers beyond the handover, which #25 reuses for upgrades.
- A plain `docker compose up -d` in the state folder still gives the same platform containers, so you can always fall back to the standard tool.

**Negative / tradeoffs**:
- `setup.sh` needs root, and the state no longer sits where you unzipped; you must know `/var/lib/modular-platform`.
- The keeper becomes the most complex code in the root equivalent service: a lock, a step runner, a self handover.
- Compose's rule misses hand changes such as `docker update` or a disconnected network; such a container stays wrong until something recreates it.
- The platform flavor's `setup.sh` now differs a lot from the static flavor's, and `setup.json` is one more rendered format to keep golden.
- The initial password stays on disk in plain text (root only) until #14 or the web setup gives a way to change it.
- Milestone 4's `setup.sh` secret and step code is built and then removed by this feature.

**Neutral**:
- Test installs made under milestone 4 are reinstalled (pre release, as 0011's migration already says).
- `MP_UID` and `MP_GID` become the fixed `65532` instead of the installing user's IDs.

## Migration plan

**Strategy**: big bang, pre release; no published release zip exists yet.
**Phases**:
1. Tasks 1 to 3 land beside milestone 4's working `setup.sh` (the render keeps the old platform `setup.sh` until task 4 switches dev and smoke).
2. Task 4 switches the release zip, `task dev:managed` and smoke in one change, once the seeded smoke passes.
**Rollback**: revert the task 4 merge; the render then emits milestone 4's `setup.sh` and the deployer as a Compose service again.
**Risks**: Compose reading `COMPOSE_FILE` from the project `.env` (task 1 checks it); a successor that can never get healthy loops on the backoff (bounded by the timeout, logged); a keeper image that cannot be pulled leaves only the seed retrying, visible in `docker logs modular-platform-seed`.

## Follow-up

- [ ] Enroll a new scope feature, "First run setup on the web": a one time setup site protected by a generated `show: once` setup code with no user name, collecting the install answers, and first admin enrollment through whatever identity module is installed (a wallet for an OID4VP module, for example); design its enrollment seam together with #28. It replaces `setup.sh`'s questions and the Authelia admin password of this spec.
- [ ] #25 builds the drain contract (503 `handing_over`, `api` retrying) when it triggers a handover on a keeper that already serves.
- [ ] Changing the initial admin password: Authelia's file backend has no change flow wired here; #14 or the web setup feature must provide one.
- [ ] Scope #32's done line says "a container changed by hand"; with Compose's rule this means "recreated from another definition", not `docker update`. Adjust the wording when the scope is next reconciled.
- [ ] #33 maps the keeper and its handover onto the cluster (likely an operator with a rolling update of itself).
- [ ] Spec 0015's platform `setup.sh` and the deployer as a Compose service are replaced by this spec once task 4 lands (see *What this replaces*); 0015 carries a pointer.
- [ ] 0007 says only deployed containers carry the `deployment` label, but 0015 gives platform containers `deployment: platform`, so the event stream also records platform exits. Harmless (the map is capped), but 0007's sentence is wrong; correct it when 0007 is next touched.
- [ ] Private registries for step and keeper images need credentials in the keeper; not covered here.
