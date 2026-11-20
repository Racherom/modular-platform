# Verify: Start the platform from one container · spec 0016 · updated 2026-11-05
_Steps derived from spec 0016 acceptance criteria and its Value sourcing table. `/check verify` runs these; `/test` locks the durable ones. Nothing is built yet: every step waits for the build plan._

## Install

- [ ] On a host with no `/var/lib/modular-platform`, unzip the release and run `sh setup.sh` as your user → `run as root: sudo sh setup.sh`, exit 1, nothing created → AC-1
- [ ] `sudo sh setup.sh`, answer domain `platform.localhost`, an empty origin, your email → only those three questions; `stat -c '%a %U' /var/lib/modular-platform` gives `700 root`; `.env` holds `MP_DIR`, `MP_UID=65532`, `MP_GID=65532` and the three answers → AC-1, Value sourcing "MP_UID"
- [ ] Right after `docker run` returns: `docker ps -a --filter label=io.github.racherom.modular-platform.stack=platform` lists only `modular-platform-seed`; `docker inspect` shows the socket and the state folder mounts and `RestartPolicy unless-stopped` → AC-1
- [ ] When `setup.sh` ends → it printed `Initial password of the admin user "admin": <value>` once and the origin; `docker compose --project-directory /var/lib/modular-platform ps` shows every platform service healthy; exactly one container with a `modular-platform-keeper-` project runs; `modular-platform-seed` no longer exists → AC-2, AC-3
- [ ] `sudo ls -l /var/lib/modular-platform/secrets/*/` → every generated file 0600 owned by 65532, `cert-issuer`'s root certificate 0644, folders 0700 → AC-2
- [ ] `docker logs` of the keeper and every platform service, and `docker inspect` env of every container, searched for the printed password → not found → AC-3
- [ ] Sign in at `https://platform.localhost/` as `admin` with the printed password, deploy the sample → both modules `running` → AC-3

## Starting again

- [ ] Record `docker ps -aq --no-trunc` and `docker volume ls -q`; `docker restart` the keeper; wait for healthy → same container IDs and volumes; `sudo find /var/lib/modular-platform/secrets -newer /var/lib/modular-platform/.env` lists nothing → AC-4
- [ ] `sudo sh setup.sh` again → no question, `already running`, exit 0, no file changed → AC-4
- [ ] `sudo systemctl restart docker` → after a while the same platform containers run again, one keeper, no seed → AC-4
- [ ] `docker stop` the keeper, then `sudo sh setup.sh` → a seed appears, the stopped keeper is started again (same container ID), the seed is gone → AC-4, AC-6
- [ ] On a fresh install, press Ctrl+C while `setup.sh` waits, then run it again → it asks nothing, prints the password once and writes `.shown`; a third run prints nothing → AC-3
- [ ] During a handover (both the seed and the successor running), `sudo systemctl restart docker` → one keeper and no seed remain, no duplicate service container → AC-4, AC-6, AC-8
- [ ] With the platform images at `0.0.0` (moved here from spec 0030, whose AC-6 needs the keeper): `task managed:zip VERSION=0.0.0`, install it with `MP_SOURCE_PATH=<checkout> sudo -E sh setup.sh`, then `docker restart` the keeper → no platform image is pulled (`docker events --filter type=image` shows no `pull` of a `ghcr.io/racherom/modular-platform/` image) and every service is healthy; then make an empty commit in the checkout and run `setup.sh` again with `MP_SOURCE_PATH` → the rebuilt keeper image has a new ID and hands over, one keeper and no seed remain. A new image ID, not a new GitHub ref, starts the handover, so no push is needed → AC-4, AC-6 (spec 0030 AC-6)
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
- [ ] `go test ./libs/render/...` → render errors `keeper_missing`, `keeper_duplicate`, and `setup_required` for a `from: ask` secret, a step with `asks` and an asked `secret` config value; goldens of `docker-compose.yml` (no keeper, no `depends_on` on it), `keeper.yml`, `setup.json` and `setup.sh` → AC-10
- [ ] A platform graph with an external secret field and no placed file: `sudo sh setup.sh` → lists the path with its description, exit 1, no state folder, no container → AC-11

## Dev and CI

- [ ] `task dev:managed` → the state `.env` holds `COMPOSE_FILE=docker-compose.yml:compose.dev.yaml` and `MP_KEEPER_IMAGE=…:dev`; the editor hot reloads on `https://platform.localhost`; no `sudo` prompt appears → AC-12
- [ ] `task smoke:managed` run in the background with `</dev/null` and a timeout → passes, and its log shows the seed, the handover and the AC-4 to AC-7 checks → AC-12
