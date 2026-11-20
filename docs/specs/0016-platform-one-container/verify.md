# Verify: Start the platform from one container · spec 0016 · updated 2026-11-20
_Steps derived from spec 0016 acceptance criteria and its Value sourcing table. `/check verify` runs these; `/test` locks the durable ones. Nothing is built yet: every step waits for the build plan, and the install steps wait for #37 (the web setup)._

## Install

- [ ] On a host with no `/var/lib/modular-platform`, unzip the release and run only the README's install commands as a user in the `docker` group (before the first release: its `docker build` lines, then its `docker run`) → no command asks a question or needs `sudo` → AC-1
- [ ] Right after `docker run` returns: `docker ps -a` lists one new container, `modular-platform-seed`; `docker inspect` shows the socket mount, the state folder mounted at `/var/lib/modular-platform` on both sides, the unzipped folder read only at `/install`, and `RestartPolicy` `no` → AC-1
- [ ] Once the seed logs `install done`: `sudo stat -c '%a %U' /var/lib/modular-platform` gives `700 root`; `.env` holds `MP_DIR`, `MP_UID=65532` and `MP_GID=65532` and no `COMPOSE_FILE` or `MP_KEEPER_IMAGE` → AC-1, Value sourcing "MP_UID"
- [ ] `docker logs -f modular-platform-seed` until the seed is gone → `docker compose --project-directory /var/lib/modular-platform ps` shows every platform service healthy; exactly one container with a `modular-platform-keeper-` project runs; `modular-platform-seed` no longer exists → AC-2
- [ ] `sudo ls -l /var/lib/modular-platform/secrets/*/` → every generated file 0600 owned by 65532, `cert-issuer`'s root certificate 0644, folders 0700 → AC-2
- [ ] `docker logs` of the keeper → one `show` line per `show: once` file with its path and description, and the README names the same paths; `sudo cat` of the setup code's path gives the code → AC-3
- [ ] `docker logs` of the seed (while it runs), the keeper and every platform service, and `docker inspect` env of every container, searched for the setup code and every generated secret value → not found → AC-3
- [ ] Finish #37's setup with the code, sign in at `https://platform.localhost/`, deploy the sample → both modules `running` → AC-3

## Wrong install commands

- [ ] `docker run` the seed without the socket mount → it exits 1 and stays stopped; its log shows `install refused` `docker_unreachable` naming the socket; nothing was copied → AC-13
- [ ] `docker run` the seed with `-v /tmp/mp:/var/lib/modular-platform` → exit 1, `install refused` `mount_mismatch` naming both paths → AC-13
- [ ] `docker run` the seed on a fresh state folder without the `/install` mount → exit 1, `install refused` `install_missing` → AC-13
- [ ] `docker run` the seed with `--state /var/lib/modular-platform/`, then with a symlink to the state folder as the host path, then with `/install` mounted at the state path, then with `--hostname x` → exit 1 each time with `mount_mismatch` (or the clean path rule), `mount_mismatch`, `install_is_state` and `self_unknown` → AC-13
- [ ] After a refusal, run the corrected `docker run` → Docker reports the name in use; `docker rm modular-platform-seed`, then the corrected command installs → AC-13
- [ ] `docker kill` the seed during its copy, then `docker start` it → the copy runs again; `docker-compose.yml` is the last file to appear in the state folder; the install finishes as above → AC-1

## Starting again

- [ ] Record `docker ps -aq --no-trunc` and `docker volume ls -q`; `docker restart` the keeper; wait for healthy → same container IDs and volumes; `sudo find /var/lib/modular-platform/secrets -newer /var/lib/modular-platform/.env` lists nothing → AC-4
- [ ] Run the README's `docker run` again from the same unzipped folder → the seed logs no `install done`, hands over to the running keeper (same container ID) and is gone; no service container is new, no file changed → AC-4
- [ ] Run it again from a newer unzipped folder (one changed byte in `docker-compose.yml`), then with another `-e MP_KEEPER_IMAGE` → exit 1, `install refused` `installed_differs` naming the file or value; no file in the state folder changed → AC-4
- [ ] `sudo systemctl restart docker` → after a while the same platform containers run again, one keeper, no seed → AC-4
- [ ] `docker stop` the keeper, then the README's `docker run` → the stopped keeper is started again (same container ID), the seed is gone → AC-4, AC-6
- [ ] During a handover (both the seed and the successor running), `sudo systemctl restart docker` → only the successor comes back by itself; its cleanup removes the stopped seed; one keeper, no duplicate service container → AC-4, AC-6, AC-8
- [ ] On a fresh install, `sudo systemctl restart docker` before any successor exists → the seed stays stopped; `docker start modular-platform-seed` finishes the install → AC-4
- [ ] With the platform images at `0.0.0` (moved here from spec 0030, whose AC-6 needs the keeper): `task managed:zip VERSION=0.0.0`, build its images from a checkout with the README's local `docker build` lines, install with its `docker run`, then `docker restart` the keeper → no platform image is pulled (`docker events --filter type=image` shows no `pull` of a `ghcr.io/racherom/modular-platform/` image) and every service is healthy; then make an empty commit in the checkout, run the `docker build` lines again and restart the keeper → the rebuilt keeper image has a new ID and hands over, one keeper and no seed remain. A new image ID, not a new GitHub ref, starts the handover, so no push is needed → AC-4, AC-6 (spec 0030 AC-6)
- [ ] `docker kill` the seed while `cert-issuer`'s `ca` step runs, then start the seed again → every file under `secrets/cert-issuer/` is whole, `.prepare/` holds nothing after the next prepare, and `cert-issuer` starts with them → AC-2

## Drift

- [ ] `docker rm -f` the `authelia` container, restart the keeper → `authelia` is back, healthy → AC-5
- [ ] Recreate `renderer` by hand from an edited copy of `docker-compose.yml` (another env value), restart the keeper → `renderer` recreated from the state folder's file → AC-5
- [ ] `docker stop` the `web` container, restart the keeper → `web` running again → AC-5
- [ ] `docker update --restart no` on `renderer`, restart the keeper → `renderer` not recreated (the documented gap) → AC-5
- [ ] `docker run -d --label com.docker.compose.project=modular-platform --label com.docker.compose.service=stray alpine sleep 1d`, restart the keeper → `stray` removed → AC-5

## Self handover

- [ ] Retag the keeper image to get another image ID, set `MP_KEEPER_IMAGE` to it in the state `.env`, restart the keeper → logs `handover started` and `handover done`; a new `modular-platform-keeper-` project runs healthy; the old container is gone; deploy still works → AC-6, Value sourcing "definition hash"
- [ ] Set `handover-timeout` to `1m` (below `wait-timeout` plus 5 minutes) → the keeper stops at start with a config error naming the value → Configuration required
- [ ] Set `MP_KEEPER_IMAGE` to an image whose healthcheck always fails and `handover-timeout` to `10m`, restart the keeper → after about ten minutes `handover failed`, the successor project is gone, the old keeper still answers `GET /v1/stacks/mp-whoami-postgres/state` → AC-6

## Deployed stacks

- [ ] With the sample deployed, record `docker inspect -f '{{.Id}} {{.State.StartedAt}} {{.RestartCount}}'` for every `mp-whoami-postgres` container, then run every step of *Starting again*, *Drift* and *Self handover* → the values are unchanged → AC-7
- [ ] `grep -c 'mp-' ` over the keeper's `reconcile done` log lines → 0; no keeper log names an `mp-` project → AC-7

## Failures and the lock

- [ ] Edit `docker-compose.yml` in the state folder so `renderer` names a tag that does not exist, restart the keeper → `pipeline failed` with `step: reconcile`; the keeper still serves the deploy target API; `retryIn` grows 1s, 2s, 4s…; fix the tag → the next retry logs `reconcile done` → AC-9
- [ ] Start two seeds at once (`docker run` twice with different names) on a running install → only one runs the pipeline at a time (the logs never overlap between `keeper start` and `reconcile done`), one keeper remains, no duplicate service container → AC-8

## Contract and render

- [ ] `go test ./libs/contract/...` → fixtures with `show: once` on `from: setup` and with `format: base32` are refused `invalid_secret` → AC-10
- [ ] `go test ./libs/render/...` → render errors `keeper_missing`, `keeper_duplicate`, and `setup_required` for a `from: ask` secret, a step with `asks` and an asked `secret` config value; goldens of `docker-compose.yml` (no keeper, no `depends_on` on it), `keeper.yml`, `setup.json` and the README's install commands; no platform `setup.sh` in the output → AC-1, AC-10
- [ ] A platform graph with an external authentication service (URL in the graph) and no placed key file: the README's `docker run` → the seed logs `install refused` `placed_missing` with the path and its description, copies nothing, starts no other container; place the file in the unzipped folder → the next retry installs, and the copied file is 0600, owned by 65532 → AC-11
- [ ] A placed file that no setup mode service reads (`beforeSetup` false) and is missing → the install goes on, and #37's setup asks it → AC-11

## Dev and CI

- [ ] `task dev:managed` → the seed was started with `-e COMPOSE_FILE=docker-compose.yml:compose.dev.yaml` and `-e MP_KEEPER_IMAGE=…:dev`, the state folder is `.dev/managed-state` and its `.env` holds both; the editor hot reloads on `https://platform.localhost`; no `sudo` prompt and no wrapper container → AC-12
- [ ] Run `task dev:managed` again → the same containers; change the render, run it again → `installed_differs`; `task dev:managed:reset` then `task dev:managed` → a fresh install with the new render → AC-12
- [ ] `task smoke:managed` run in the background with `</dev/null` and a timeout → passes, and its log shows the seed, the handover and the AC-4 to AC-7 and AC-13 checks → AC-12
