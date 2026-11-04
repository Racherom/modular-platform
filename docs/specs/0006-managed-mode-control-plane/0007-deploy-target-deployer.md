# 0007. Deploy target contract and deployer

**Date**: 2026-11-03
**Parent**: [0006. Managed mode control plane](index.md)

## Summary

`deployer-docker` is the only service with the Docker socket, so it does as little as possible: it takes a rendered project from `api`, checks it again, writes it into the deployment's folder, creates missing secrets, and runs `docker compose`. It keeps running operations in memory only and reports container state on request. It never plans, never stores history, and never decides a rollback; `api` does that. This child fixes its REST contract, the apply and remove steps, the folder layout, the host access guard and how container state maps to module state.

## Contract (`contracts/openapi/deploy-target.yaml`)

Served on `https://deployer-docker.modular-platform.internal:8443` (TLS mode, child [0008](0008-managed-tls-cert-issuer.md)), binding token auth (`provides.deploy-target.auth: binding-token`), `MP-Caller` checked and required on apply and remove (401 `invalid_token` without one). `/healthz` and `/readyz` exempt. Every answer, error answers included, carries `bootId` (16 random bytes, base64url, chosen at each start). Errors are `ProblemList`.

| Endpoint | Method | Key inputs | Key outputs | Key errors |
|---|---|---|---|---|
| `/v1/projects/{project}/apply` | POST | `deployment`: uuid; `files: [{path, mode, data (base64)}]`; `secrets: [{path, kind, instance, capability, field}]`; `placed: [{path, data (base64), replace}]`; body at most 8 MB | 202 `Operation` | 409 `busy`; 422 `invalid_project`, `host_access`, `invalid_path`, `placed_missing`, `placed_invalid` |
| `/v1/projects/{project}/remove` | POST | `deleteData`: bool | 202 `Operation` | 409 `busy`; 404 `not_found` when no folder and no container of the project exist |
| `/v1/operations/{id}` | GET | `id` | `Operation`: `id`, `project`, `action`, `status` (`running`, `succeeded`, `failed`), `step`, `error`, `startedAt`, `finishedAt` | 404 `not_found` (unknown under this boot id) |
| `/v1/projects/{project}/state` | GET | `project` | `{present, services: [{instance, module, state, health, exitCode, lastExitCode, restartCount, image, since}]}` | 503 `docker_unavailable` |
| `/v1/projects/{project}/secrets` | GET | `project` | `{present: [path]}`, the secret files that exist as `secrets/<instance>/<name>` (the same form as render's paths), never their content | none |

`project` must match `^mp-[a-z0-9]([a-z0-9-]*[a-z0-9])?$` (so it can never be or start with `modular-platform`), every service's `io.github.racherom.modular-platform.graph` label must equal the project without `mp-`, every `.deployment` label must equal the request's `deployment`, and a top level `name:` in the Compose file, if present, must equal the project; else 422 `invalid_project`.

Secret and placed paths must match `^secrets/[a-z0-9-]+/[A-Za-z0-9._-]+$`; every file is opened with `O_NOFOLLOW`, and a path whose file or parent folder is a symbolic link is refused (`invalid_path`).

## Apply pipeline

One operation per project at a time (an in memory lock; a second request gets 409 `busy`). Different projects run in parallel. Each operation runs in its own goroutine with a context detached from the request (`context.WithoutCancel`) and the deadlines below.

1. **prepare**: check the project name; decode every file; check each path (relative, cleaned, no `..`, no absolute path, only `docker-compose.yml`, `.env`, `graph.yaml`, `README.md`, `setup.sh`, `config/<instance>.env`); load `docker-compose.yml` with `compose-go` and run `render.Guard` with the configured reserved ports; refuse placed values that are empty, over 4096 bytes or hold a NUL (`placed_invalid`), and any placed path in `secrets` that does not exist and was not supplied (`placed_missing`). Read the current files of the folder into memory, then write the new ones (0644, owned by `PLATFORM_UID:PLATFORM_GID`), write `deploy.env` (`MP_UID`, `MP_GID`), create each missing generated secret (32 random bytes as hex, `O_CREATE|O_EXCL`, mode 0600, chown to `PLATFORM_UID:PLATFORM_GID`), write each supplied placed value that is missing (same mode and owner). Folders `secrets/<instance>/` are 0700.
2. **pull**: `docker compose --project-name <project> --env-file .env --env-file deploy.env pull`, deadline `pull-timeout` (10 minutes).
3. **up**: first write each placed value supplied with `replace: true` (so a failure before this point never loses the old value), then `docker compose ... up -d --remove-orphans --wait --wait-timeout <wait-timeout in seconds>` (300).
4. **done**: status `succeeded`.

Any failure sets `failed` with the step it reached and an error: a problem code, then at most the last 16 KB of Compose's combined output. A failure in **prepare** or **pull** restores the previous files from memory (and deletes the new secrets written by this operation, never an existing one), so the running containers and the folder stay as they were. A failure in **up** leaves everything in place for inspection and for `api`'s rollback.

The guard (`render.Guard`, pure, in `libs/render`) is an allow list of exactly the shape `RenderManaged` emits, checked on the loaded project:
- Top level: `name`, `services`, `networks` (only the project's own, never `external`), `volumes` (only named volumes the project creates, never `external`, no `driver_opts`), `secrets` and `configs` (each a `file:` under `./secrets/` or `./config/` of the project).
- Per service: `image`, `environment`, `healthcheck`, `depends_on`, `networks` (own networks, with `aliases`), `user`, `secrets`, `configs`, `labels`, `restart`, and `ports` only when a later render emits them (none in `v0alpha1`), never equal to a reserved port.
- Everything else is refused, one problem `host_access` per instance and key: for example `privileged`, `network_mode` (host, `container:` or `service:`), `pid`, `ipc`, `userns_mode`, `cgroup`, `cgroup_parent`, `security_opt`, `sysctls`, `devices`, `cap_add`, `volumes` on a service, `volumes_from`, `env_file`, `build`, `extends`, `include`, `container_name`, and any value naming `/var/run/docker.sock` or `/run/docker.sock`.

A golden test renders every fixture graph with `RenderManaged` and shows the guard accepts it, so the list and the render cannot drift; widening the render means widening the list in the same change.

## Remove

1. **prepare**: check the project.
2. **down**: `docker compose ... down --remove-orphans`, plus `--volumes` when `deleteData` is true; deadline 5 minutes.
3. With `deleteData`, delete `<project>/secrets/` recursively. The rest of the folder stays (the files are small and show what last ran); a hand run `docker compose` there then fails on the missing secrets, which the README states.
4. **done**.

## Operations in memory

Kept in a map until 1 hour after they finish, at most 1,000 entries (oldest finished dropped first). A restart forgets them all and picks a new boot id, which is how `api` learns that their outcome is unknown. The deployer runs with `restart: unless-stopped`. Each start and end is logged with project, operation id, action, step, status and the `MP-Caller` `sub`; never file content.

## Module state

The state read lists containers with the label `com.docker.compose.project=<project>` through the moby client and reads each one's `instance` and `module` labels. `present` is false when the project has neither containers nor a folder.

| Docker | Module state |
|---|---|
| `created` | `pending` |
| `running`, health `starting` | `starting` |
| `running`, health `healthy` or no healthcheck | `running` |
| `running`, health `unhealthy` | `unhealthy` |
| `restarting` | `restarting` |
| `exited` or `dead` | `exited` with `exitCode` |
| `paused` | `paused` |

`api` adds `missing` for an instance of the applied graph with no container (or `pending` while an operation runs).

Every service also reports `restartCount` (the inspect's `RestartCount`, which Docker keeps itself) and `lastExitCode`, so a crash that Docker restarts within a second still shows.

### Last exit code

Docker sets a container's exit code back to 0 as soon as it starts the container again, so an inspect after a restart never holds the code of the run that crashed. Only during the short `restarting` pause (100 ms on the first restart) does the inspect still show it. The deployer therefore keeps its own record, built from Docker's event stream (the live feed of what the engine does):

- **Meaning**: the code of the container's most recent exit, whatever caused it. A `die` event also fires on `docker stop`, `compose stop`, `compose down` and `compose restart` (usually 143 or 137), so a module stopped and started by hand shows 143. It is the last exit, not the last crash, and the contract descriptions say so.
- **Source**: the deployer opens one event stream through the moby client, filtered to `type=container`, the events `die` and `destroy`, and the label key `io.github.racherom.modular-platform.deployment` (only deployed containers carry it, so the platform's own services stay out). Each `die` event carries the container id, its time (`TimeNano`) and the `exitCode` attribute as a string. An event whose `exitCode` is missing or does not parse as an integer is logged (warn, container id only) and ignored, never stored as 0.
- **Lifetime**: the stream runs on the process context. `main` starts it before the server listens, and shutdown cancels it, which also ends the reconnect loop. `/readyz` does not depend on it: the state read works without it, only `lastExitCode` gets thinner.
- **Memory**: a map from container id to the latest `{exitCode, at}`, one entry per container, guarded by a mutex, plus a list ordered by `at` for eviction. An event updates an entry only when its time is later than the stored one; an equal or earlier time changes nothing, so a replayed event is harmless. A `destroy` event deletes the entry. At most 4,096 entries, the oldest `at` dropped first; entries whose `destroy` was missed during an outage stay until that cap drops them, which is accepted. A container that Compose recreates gets a new id, so it starts with no last exit code. That is right: the new container has not ended a run yet.
- **Value**: for a container that is `exited`, `dead` or `restarting`, `lastExitCode` is the inspect's `State.ExitCode`, which is current in those states and wins over the map (the map may hold an older run if a `die` was missed); for `exited` it equals `exitCode`. For any other state it is the map entry for the container's id. With no entry, and for `created` (never ran), the field is absent, never 0: 0 is a real exit code and would read as a clean exit. A `die` that lands after a state read shows on the next read.
- **Replay point**: `since` is written in RFC 3339 with nanoseconds and is the later of the last event received and one hour before the deployer's start. It is never the moment an outage began, so exits during the outage are replayed if Docker still holds them. Docker includes events at the `since` time itself, which the "later time only" rule absorbs.
- **A dropped stream** (Docker restarted, socket error): the deployer opens it again after 1 second, doubling up to 30 seconds, from the replay point. While the stream is down the state read still answers from what the map has, and a crash the buffer no longer holds is simply absent. A stream that stalls without an error is not detected (Docker sends no heartbeat); this is accepted. The deployer logs `docker events lost` (warn) once per outage and `docker events resumed` (info).
- **After a deployer restart** the map starts empty and the stream opens from the replay point, so Docker replays the exits it still keeps. Docker Engine keeps only a small in memory buffer of recent events, shared by every container on the host (health checks fill it fast), and nothing after Docker itself restarts. Exits that happened while Docker itself was down are not promised at all. So in practice a module shows no last exit code until it exits again. Its `restartCount` comes from Docker and survives, so an earlier crash still shows as a restart.
- **Through `api`**: `api` copies `lastExitCode` as is, keeping absent apart from 0 (a pointer in Go, an optional field in the contract). A change in `lastExitCode` or `restartCount` counts as a state change, so the open screen gets an update. The `exited <code>` badge reads `exitCode`; the restart line beside it reads `lastExitCode`.

Code shape: the `Docker` interface gains `Events(ctx, since) (<-chan ExitEvent, <-chan error)` next to `Containers`, with `ExitEvent{ID, Action, ExitCode, Time}`, and `Service` gains `LastExitCode *int`. The engine tests feed `die` and `destroy` events by hand, replays and missed events included.

## Wiring

- Image `docker:<version>-cli` (it includes the compose plugin) with the deployer binary and an explicit `ENTRYPOINT`, running as root, as spec 0001 pins.
- Mounts: `/var/run/docker.sock`; `${PLATFORM_DIR}/deployments` at the same absolute path (bind and config paths in deployed projects are resolved by the host daemon, so they must match); its binding token files, `MP_CA_FILE` and caller public key.
- Network `internal` only, alias `deployer-docker.modular-platform.internal`; no ingress route.

## Rationale (short)

- The deployer takes files, not a plan, so principle 5 holds (it applies a `render.Output`) and the root equivalent service carries no planner or catalog. Runner up: the deployer renders from a plan, one hop fewer but a bigger privileged surface.
- The guard runs again here because the deployer cannot trust its caller; a bypassed planner must not reach the host. It is an allow list because the render's output is a closed shape: a deny list on a root equivalent service misses keys (an `external` network could join `modular-platform_internal` and reach the deployer and `postgres`).
- `deploy.env` instead of editing `.env` keeps render output byte for byte as rendered, so golden tests mean something.
- Files restored after an early failure make "a failed pull changes nothing" true for the folder too, so a hand run `docker compose` sees what is running.
- Moby labels instead of parsing `docker compose ps` output: a stable API instead of a text format.
- Last exit code from `die` events in the deployer's memory, because no Docker state survives a restart with the old code: `State.ExitCode` goes back to 0 at start and `FinishedAt` holds only a time. The deployer already holds the socket and runs all the time, so one stream costs little and catches crashes even while nobody watches. Runner ups: `api` records the code from its polls (it sees only the short `restarting` pause, and polls only while a screen watches); a small file per project under the deployment folder (survives a deployer restart, but a root equivalent service then writes state to disk for a hint the badge can live without). Losing the code on a deployer restart matches the progress already lost there.
