# Verify: Managed mode control plane · spec 0006 · updated 2026-11-04
_Steps derived from spec 0006 acceptance criteria and its Value sourcing table. `/check verify` runs these; `/test` locks the durable ones._

Milestone 1 (the thread runs) is built. Steps marked *(later)* belong to a milestone that has not landed yet: TLS and `cert-issuer` (4), remove, `unknown` and rollback (3), the guard and placed credentials (2), sessions, admin reset and the release (5). Milestone 4 landed on 2026-11-03: its steps are under *Commands*, TLS.

## UI / manual
- [ ] Copy `deploy/managed/` to an empty folder, run `sh setup.sh` (domain `platform.localhost`, your email, user `admin`, a 12+ character password) → `.env` and `secrets/` written, every secret file mode 0600 except the public `secrets/cert-issuer/root_ca.crt` (0644), folders 0700, `stacks/` and `data/cert-issuer/` 0700, `config/cert-issuer/ca.json` copied → AC-1
- [ ] Run `sh setup.sh` a second time → exit 0, no file changed (`find . -type f -mmin -1` lists nothing) → AC-1
- [ ] `docker compose up -d` → `ingress`, `web`, `api`, `renderer`, `deployer-docker`, `cert-issuer`, `postgres`, `authelia` healthy, `db-init` exited 0 within 5 minutes → AC-1
- [ ] Open `https://platform.localhost/` signed out → the index with a *Sign in* button; set `PLATFORM_SIGNED_OUT=sign-in`, recreate `web` → the browser goes straight to Authelia → AC-2
- [ ] Sign in as the admin → the editor with *Stacks*, your email and *Sign out* in the header → AC-2
- [ ] Add a second user to `secrets/authelia/users.yml` with another email, restart Authelia, sign in as that user → back on the index with "This account is not the platform admin" (`?signed_out=not_admin`) → AC-2
- [ ] With no admin bound yet (`docker compose exec postgres psql -U postgres -d platform_api -c 'DELETE FROM admin_binding'`), sign in with the admin email typed in another case (`Admin@Example.com` in `PLATFORM_ADMIN_EMAIL`) → admitted and bound; the binding holds Authelia's `iss` and `sub`, not the email → AC-2, Value sourcing "admin match"
- [ ] Inspect the session cookie in the browser → `__Host-mp_session`, HttpOnly, Secure, SameSite=Lax, `Path=/`, no Domain → AC-3
- [ ] Open the sample (*Sample*), choose *Deploy* → the review dialog lists `postgres` with `postgres:18.6` and `whoami` with `traefik/whoami:v1.12.0`, the binding `whoami uses postgresql from postgres`, and "Published ports: None" → AC-7
- [ ] Confirm *Deploy* → the stack screen; the step shows in the live region, then both module badges read `running` with text and colour → AC-9, AC-13
- [ ] With the stack screen open, kill the module's main process from the host: `sudo kill -9 "$(docker inspect -f '{{.State.Pid}}' mp-whoami-postgres-whoami-1)"` → within 6 seconds the badge shows a higher restart count, or `exited 137` / `restarting` if caught before Docker restarts it (last exit code 137 beside the restart count *(later, milestone 5)*) → AC-13
- [ ] *Stacks* → the list shows `whoami-postgres` with a `running` badge and its latest operation → AC-9
- [ ] *(later, milestone 5)* After the kill above, `docker compose restart deployer-docker`, then reload the stack screen → the restart count is unchanged; the last exit code is either 137 (Docker replayed the event) or absent, never 0 → AC-13

## Commands
- [ ] `curl -sk https://platform.localhost/api/v1/session` without a cookie → 401 `unauthenticated` → AC-3
- [ ] `curl -sk -X POST -b <session cookie> https://platform.localhost/api/v1/check -d @graph.json` without `Origin` → 403 `bad_origin`; with `Origin: https://evil.example` → 403 `bad_origin` → AC-5
- [ ] `docker run --rm --network modular-platform_internal -v "$PWD/secrets/cert-issuer/root_ca.crt:/ca.crt:ro" curlimages/curl -s -o /dev/null -w '%{http_code}' --cacert /ca.crt https://renderer.modular-platform.internal:8443/v1/catalog` → 401 (no binding token; the `api` image is distroless and has no `wget`); the ingress has no route to `renderer` or `deployer-docker` (`curl -sk https://platform.localhost/v1/catalog` → 404) → AC-5
- [ ] `docker compose logs deployer-docker | grep '"msg":"operation started"'` → each line carries `"caller"` equal to the admin's `sub` from `admin_binding` → AC-5, Value sourcing "deployer log caller"
- [ ] Apply without `MP-Caller` straight at the deployer (from inside the `internal` network, with the api binding token) → 401 `invalid_token` → AC-5
- [ ] `POST /api/v1/stacks/whoami-postgres/review` twice with the same graph → the same `result.digest`; change the sample's `whoami` image tag in the catalog, rebuild the renderer → a different digest → Value sourcing "digest"
- [ ] Review with a graph named `other` at `/stacks/whoami-postgres/review` → 422 `name_mismatch`; a graph named `modular-platform` → 422 `reserved_name`; neither writes a row (`SELECT count(*) FROM stacks`) → AC-7
- [ ] Apply with a `digest` from an older review → 409 `graph_changed`, and no new row in `operations` → AC-10
- [ ] Two applies of the same stack at once (two `curl` in parallel) → one 202, one 409 `busy` → AC-9
- [ ] Stop `deployer-docker`, apply → 202 with the operation `failed`, error `deployer_unavailable` → AC-16
- [ ] `ls -ln stacks/mp-whoami-postgres/secrets/postgres/password` → mode 0600, owner `PLATFORM_UID:PLATFORM_GID`; content is 64 hex characters; a second apply leaves it unchanged → AC-11, Value sourcing "secret owner"
- [ ] `grep -rF -f <(cat secrets/*/* stacks/*/secrets/*/*) <(docker compose logs; docker compose -p mp-whoami-postgres logs)` (PEM armour lines and public `*.crt` files excluded: `cert-issuer` logs every leaf, which repeats its intermediate's name) → no match → AC-1, AC-11
- [ ] `psql platform_api -c 'SELECT placed_supplied, graph FROM operations'` → paths only, never a value → AC-11
- [ ] `docker inspect mp-whoami-postgres-whoami-1 --format '{{json .Config.Labels}}'` → the four `io.github.racherom.modular-platform.*` labels, `stack` equal to the row's id; `--format '{{.HostConfig.RestartPolicy.Name}}'` → `unless-stopped` → AC-12
- [ ] `task generate && git diff --exit-code` → clean; `contracts/openapi/api.yaml` and `deploy-target.yaml` exist; the renderer in public mode answers `POST /v1/render/managed` with 404 → AC-18
- [ ] `go test -race -tags integration ./services/api/internal/store/` → the migration applies (also from two processes at once), the four tables and three indexes exist, the admin binds once, a second running operation is `busy` → data model, AC-2, AC-9
- [ ] `task images IMAGE_TAG=ci && task smoke:managed` → "smoke-managed passed" (the tag must be a Task argument; as an env var the images come out `dev`) → AC-19, AC-17

### TLS (milestone 4)
- [ ] `docker compose logs api renderer deployer-docker | grep '"msg":"listening"'` → each shows `"addr":"[::]:8443","tls":true`, and no service listens on 8080 → AC-17
- [ ] `docker run --rm --network modular-platform_internal curlimages/curl -s -m 5 http://renderer.modular-platform.internal:8080/` → connection refused; the same for `deployer-docker` and `api` → AC-17
- [ ] `docker compose logs api renderer deployer-docker | grep '"msg":"certificate held"'` → `host` is `<instance>.modular-platform.internal`, `not_after` 24 hours after the issue → AC-17
- [ ] Read a leaf: `docker run --rm --network modular-platform_internal alpine/openssl s_client -connect api.modular-platform.internal:8443 -servername api.modular-platform.internal </dev/null 2>/dev/null | docker run --rm -i alpine/openssl x509 -noout -dates -issuer -ext subjectAltName` → issuer `Modular Platform internal CA Intermediate CA`, SAN `api.modular-platform.internal`, `notAfter` minus `notBefore` = 24 hours 1 minute (step-ca backdates 1 minute) → AC-17
- [ ] Ask for an outside name: `docker build -q -f sdk/internal/tlsmodule/Dockerfile -t modular-platform/sdk-tls-module:test .`, then `docker run --rm --network modular-platform_internal --network-alias tm.other.internal -e MP_INSTANCE=tm -e MP_DOMAIN=other.internal -e MP_ACME_URL=https://cert-issuer.modular-platform.internal:9000/acme/acme/directory -e MP_CA_FILE=/ca.pem -v "$PWD/secrets/cert-issuer/root_ca.crt:/ca.pem:ro" modular-platform/sdk-tls-module:test` → `certificate issue failed` with `rejectedIdentifier`, never `certificate held` → AC-17
- [ ] Signed in, open the editor and *Stacks* → pages load, so the ingress reaches `api` over TLS; `docker compose logs ingress | grep -i 'x509\|tls'` → no handshake errors → AC-17
- [ ] After deploying the sample: `grep -r MP_ACME_URL stacks/mp-whoami-postgres/` → no match; deployed SDK modules run plain → AC-17
- [ ] Lost issuer database: `docker compose stop cert-issuer`, move `data/cert-issuer` away, create it again empty (0700), `docker compose up -d cert-issuer`, restart `renderer` → it logs `certificate held` again (the SDK registers a new account on `accountDoesNotExist`) → AC-17
- [ ] `go test -race -tags integration -run TestTLSAgainstPebble ./sdk/` → ok (the SDK's ACME client against pebble on 8443) → AC-17
- [ ] `task managed:zip` twice → the same bytes (`cmp`), entries dated 1980-01-01, no `compose.dev.yaml` → spec 0009

## Acceptance-criteria coverage
- AC-1 install, healthy, rerun safe, no secret in logs: UI steps 1 to 3, Commands "grep"
- AC-2 signed out view, admin binding: UI steps 4 to 7
- AC-3 cookie and 401: UI step 8, Commands 1 · idle and max expiry, `signed-out` event, the config rule *(later, milestone 5)*
- AC-4 sign out *(later, milestone 5)*
- AC-5 Origin, binding tokens, MP-Caller: Commands 2 to 5
- AC-6 admin reset *(later, milestone 5)*
- AC-7 review: UI step 9, Commands 7 · placed list with `exists` *(later, milestone 2)*
- AC-8 guard *(later, milestone 2)*
- AC-9 apply to running, busy: UI steps 10 and 12, Commands 9 · busy across two api processes: store integration test
- AC-10 graph_changed: Commands 8
- AC-11 generated secrets: Commands 11 to 13 · placed credentials end to end *(later, milestone 2)*
- AC-12 labels and restart policy: Commands 14 · Docker restart *(later, milestone 5)*
- AC-13 badges: UI steps 10 and 11 · poll cadence and last exit code, including after a deployer restart (UI step 13) *(later, milestone 5)*
- AC-14, AC-15 rollback and remove *(later, milestone 3)*
- AC-16 deployer unavailable at apply: Commands 10 · `unknown` and resumed polling *(later, milestone 3)*
- AC-17 TLS: Commands, TLS steps 1 to 9; smoke job
- AC-18 contracts: Commands 15
- AC-19 smoke job: Commands 17 · the zip on releases *(later, milestone 5)*
- AC-20 download zip in managed mode *(later, milestone 5)*

# Verify: milestone 2, review you can trust · spec 0006 · added 2026-11-03
_Milestone 2 is built: the allow list guard in the renderer and the deployer, the project check, and the placed value path rule. These steps add to the ones above; the *(later, milestone 2)* marks above are now due._

## UI / manual
- [ ] Review a graph with two check problems (an unmet need and an unknown module) → the dialog lists both problems, not only the first → AC-7
- [ ] Review a graph with a placed credential that does not exist yet → a password field per missing value, *Deploy* disabled until each is filled → AC-11, Value sourcing "placed list and exists"
- [ ] Deploy it, then review again → the field reads "(already set)" with a *Replace* checkbox; *Replace* opens a field and the next deploy writes the new value only after the pull → AC-11
- [ ] Between review and *Deploy*, change the catalog and restart the renderer → the dialog says the graph changed and reviews again; the deployer log shows no new operation → AC-10

## Commands
- [ ] `go test -race ./libs/render/ -run 'TestGuardAcceptsEveryRender|TestGuard$|TestCheckProject'` → every fixture render passes; each refused key gives `host_access` at `services.<instance>.<key>` (or the top level key), including an `external` network named `modular-platform_internal` and a secret file set through `${...}` in `.env` → AC-8
- [ ] From inside the `internal` network, with the api binding token and an `MP-Caller`, POST a project with `privileged: true` and `volumes: [/:/host]` to `/v1/stacks/mp-sample/apply` → 422 with `host_access` at `services.<instance>.privileged` and `.volumes`; `stacks/mp-sample/` is not created → AC-8
- [ ] The same with a `.env` holding `DOCKER_HOST=tcp://elsewhere:2375` → 422 `host_access` at `env.DOCKER_HOST` → AC-8
- [ ] The same with the sample's `stack` label changed, or its `graph` label changed → 422 `invalid_project` at `services.<instance>.labels` → AC-8
- [ ] Set `PLATFORM_HTTPS_PORT=8443` in `.env`, recreate `renderer` and `deployer-docker`, send the deployer a project publishing `8443` (or a range covering it) → 422 `host_access` at `services.<instance>.ports.<n>.published` → AC-8, Value sourcing "reserved ports"
- [ ] Apply with a placed value that is empty, over 4096 bytes, or holds a NUL → 422 `placed_invalid`; with one for a generated secret's path (`secrets/postgres/password`) → 422 `invalid_path`, and the generated file is unchanged → AC-11
- [ ] Apply without the value of a missing placed credential → 422 `placed_missing`, and no row in `operations` → AC-11
- [ ] `go test -race ./services/api/internal/deploy/ -run TestPlacedValuesNeverLeak` → no placed value in any log line, answer or stored operation, through a run, a deployer refusal and an unreachable deployer → AC-11
- [ ] After an apply with a placed value, `docker compose logs api deployer-docker | grep -F '<the value>'` and `psql platform_api -c 'SELECT * FROM operations'` → no match; `placed_supplied` holds the path → AC-11

## Acceptance-criteria coverage
- AC-7 full review answer and every refusal problem listed: UI steps 1 to 3 of this section, plus the review steps above
- AC-8 guard in renderer and deployer, project check: Commands 1 to 5 of this section
- AC-10 graph_changed in the editor: UI step 4 of this section, plus the Commands step above
- AC-11 placed credentials end to end: UI steps 2 and 3, Commands 6 to 9 of this section

# Verify: milestone 3, lifecycle and recovery · spec 0006 · added 2026-11-03
_Milestone 3 is built: remove with *Delete data too*, `latestApplyGraph` with *Apply again* and *Remove again*, the folder restored after a failure before `up`, and the automatic rollback with one retry. These steps add to the ones above; the *(later, milestone 3)* marks above are now due._

## UI / manual
- [ ] On a running stack, *Remove* without *Delete data too* → the state shows `removing`, then `removed`; the history keeps every row; `docker volume ls` and `stacks/mp-sample/secrets/` still hold the data → AC-15
- [ ] *Deploy* the same graph again from the graph screen → review lists no placed credential as missing; it comes back `running` with its old data → AC-15
- [ ] *Remove* the removed stack again without *Delete data too* → the dialog says it is removed already (409 `already_removed`) → AC-15
- [ ] *Remove* with *Delete data too* → the button stays disabled until you type the exact name; after it runs, its volumes and `secrets/` folder are gone, while `docker-compose.yml` stays → AC-15
- [ ] Deploy a changed graph whose new module never gets healthy (an image whose healthcheck always fails) on a stack that ran before → the history shows the failed apply at `up` with its error, then a `rollback` that succeeds; the banner says the change failed at step `up` and *See what failed* jumps to that history row; the state is `rolled-back` → AC-14
- [ ] The same, but make the last good graph fail too (for example remove its image from the registry mirror) → one rollback fails, a second starts about 30 seconds later, then the state is `failed` and the banner says returning to the last version failed; no third rollback → AC-14
- [ ] Repeat, and click *Apply again* within the 30 seconds after the first rollback failed → review shows the last good graph; deploying it runs, and no second rollback appears in the history → AC-14, Value sourcing "*Apply again* graph in review"
- [ ] Deploy a changed graph with an image tag that does not exist → it fails at `pull`; the banner says the previous version keeps running; no rollback starts; `docker compose ls` and `stacks/mp-sample/docker-compose.yml` are as before; a new instance's `config/<instance>.env` and its new secrets are gone → AC-14
- [ ] A first apply of a new stack that fails at `up` → state `failed`, the banner says no earlier version exists, no rollback, and its containers stay for inspection → AC-14
- [ ] Kill `deployer-docker` during an apply of a changed graph → the state turns `unknown`; *Apply again* reviews the changed graph (not the running one, Value sourcing "`latestApplyGraph`") and succeeds once the deployer is back → AC-16
- [ ] Stop `deployer-docker` during a *Remove* with *Delete data too* → `unknown` (or `failed`); the banner offers *Remove again* with *Delete data too* already ticked (Value sourcing "*Remove again* preset") beside *Apply again* → AC-16
- [ ] After a failed *Delete data too* remove, *Apply again* → review lists the lost placed credentials as missing → AC-16

## Commands
- [ ] `go test -race ./services/api/internal/deploy/ -run 'TestRemove|TestLatestApplyGraph|TestRollback|TestNoRollback|TestApplyCancelsTheRetry'` → all pass → AC-14, AC-15, AC-16
- [ ] `go test -race ./services/deployer-docker/... -run 'TestRemove|TestFailureBeforeUpRestoresTheFolder'` → all pass; a `secrets` link to a folder outside the project is refused and nothing outside is deleted → AC-14, AC-15
- [ ] `go test -race -tags integration ./services/api/internal/store/ -run 'TestBusyAcrossProcesses|TestRollbackQueries'` → in each of 10 rounds exactly one of two pools starts the operation; the rollback starts only while the failed apply is still the latest → AC-9, AC-14
- [ ] Run two `api` containers on one database and POST the same apply to both at once → one 202, one 409 `busy` → AC-9
- [ ] POST `/api/v1/stacks/sample/remove` with `{"deleteData":true,"confirmName":"other"}` → 422 `confirm_mismatch`, no row in `operations` → AC-15
- [ ] POST `/v1/stacks/mp-sample/remove` to the deployer without an `MP-Caller` → 401 `invalid_token`; for a project with no folder and no container → 404 `not_found` → AC-5, AC-15
- [ ] After a rollback, `psql platform_api -c "SELECT action, status, step, rollback_of, started_by_email FROM operations ORDER BY started_at"` → the rollback row carries `rollback_of` of the failed apply and that apply's `started_by_email` → AC-14, Value sourcing "Rollback graph to apply"
- [ ] `docker compose logs api | grep 'rollback started'` → one line per rollback with `rollback_of` and the caller's `sub`, no email → AC-14

## Acceptance-criteria coverage
- AC-9 busy across processes: Commands 3 and 4 of this section
- AC-14 rollback, one retry, no rollback before `up`, folder restored: UI steps 5 to 9, Commands 1 to 3, 7 and 8 of this section
- AC-15 remove, delete data, `confirm_mismatch`, `already_removed`: UI steps 1 to 4, Commands 1, 2, 5 and 6 of this section
- AC-16 `unknown`, *Apply again* on `latestApplyGraph`, *Remove again*: UI steps 10 to 12, Command 1 of this section, plus the `deployer_unavailable` step above

# Verify: milestone 5, live state, sessions, the admin and the release · spec 0006 · added 2026-11-04
_Steps derived from spec 0006 acceptance criteria and its Value sourcing table. `/check verify` runs these; `/test` locks the durable ones. They replace the *(later, milestone 5)* marks above._

## UI / manual
- [ ] Deploy the sample, open `/stacks/whoami-postgres`, then `sudo kill -9 "$(docker inspect -f '{{.State.Pid}}' mp-whoami-postgres-whoami-1)"` → within 6 seconds the `whoami` badge reads `running · 1 restart, last exit 137` (or `restarting · …, last exit 137`) → AC-13
- [ ] `docker compose restart deployer-docker`, then reload the stack screen → the restart count is unchanged; the badge shows `last exit 137` (Docker replayed the event) or no last exit at all, never `last exit 0` → AC-13
- [ ] `docker stop mp-whoami-postgres-whoami-1 && docker start mp-whoami-postgres-whoami-1` with the screen open → `last exit 143` (any exit counts, not only a crash) → AC-13
- [ ] Open the stack screen in two tabs, close both, wait 10 seconds → the deployer's access log shows state reads every 5 seconds while a tab is open, one poller for both tabs, and none after both close; during an apply they come every second → AC-13
- [ ] Sign out → the browser goes to `<base>/` (Authelia lists no `end_session_endpoint`); the old cookie gets 401 on `GET /api/v1/session`; *Sign in* asks for the password again (`prompt=login`) → AC-4
- [ ] Set `PLATFORM_SESSION_IDLE=5m`, `docker compose up -d`, sign in, leave the stack screen open 6 minutes → the screen falls back to the signed out view (`signed-out` event); any `/api/v1` call answers 401 `unauthenticated` → AC-3
- [ ] Set `PLATFORM_SIGNED_OUT=sign-in`, `docker compose up -d`, open `https://<domain>/` signed out → the browser goes straight to Authelia; with `index` it shows *Sign in* → AC-2
- [ ] In managed mode, open the sample and choose *Download zip* → `whoami-postgres.zip`, byte for byte the online editor's zip for the same graph (`cmp` against one from `task dev`) → AC-20
- [ ] With 51 or more stacks, the list shows 50 and *Show more stacks*; a stack with 51 or more operations shows *Show older operations* → AC-13 (list and history pagination)

## Commands
- [ ] `docker compose exec api /api admin reset` → exactly one line on stdout, exit 0; open screens sign out within 30 seconds; the next verified sign in with `PLATFORM_ADMIN_EMAIL` binds the admin again (`SELECT email FROM admin_binding`) → AC-6
- [ ] `docker compose exec api /api admin nope` → exit 1, one `command failed` log line, nothing changed → AC-6
- [ ] Deploy the sample, `sudo systemctl restart docker`, wait for the platform to be healthy → the editor shows every module `running` and the history holds no new apply → AC-12
- [ ] `SMOKE_DOCKER_RESTART=1 task smoke:managed` → passes: install, sign in, *Download zip*, deploy, Docker restart, running without an apply, *Remove* with *Delete data too* (`docker compose -p mp-whoami-postgres ps -a` is empty and `stacks/mp-whoami-postgres/secrets` is gone), no secret value in any log → AC-19, AC-12, AC-15, AC-20
- [ ] After a release PR merges: the GitHub release holds `modular-platform-<version>.zip`, its `compose.yaml` names the four `ghcr.io/racherom/modular-platform/*:<version>` images, and `docker pull` works for `api` and `deployer-docker` → AC-19, AC-1
- [ ] `go test -race ./services/deployer-docker/... ./services/api/... ./sdk/...` → `TestLastExitCode`, `TestWatchExitsReopensFromTheReplayPoint`, `TestWatchSharesOnePollerAndPublishesChanges`, `TestAdminReset`, `TestRenderPassesTheZipThrough`, `TestCommand` pass → AC-13, AC-6, AC-20

## Acceptance-criteria coverage
- AC-2 `PLATFORM_SIGNED_OUT`: UI step 7
- AC-3 idle end and `signed-out`: UI step 6
- AC-4 sign out and `prompt=login`: UI step 5
- AC-6 admin reset: Commands 1 and 2, `TestAdminReset`, sdk `SRV-4`
- AC-12 Docker restart: Commands 3 and 4
- AC-13 restart count, last exit code, poll cadence, pagination: UI steps 1 to 4 and 9, Commands 6
- AC-19 smoke job and the zip on releases: Commands 4 and 5
- AC-20 download zip in managed mode: UI step 8, Commands 4 and 6
- Value sourcing: last exit code from `die` events (UI 1 to 3), `MP-Caller` of the oldest stream on watched reads (`TestWatchSharesOnePollerAndPublishesChanges`), `prompt=login` from `__Host-mp_relogin` (UI 5), next URL without `end_session_endpoint` (UI 5), signed out view from `MP_SIGNED_OUT` (UI 7)
