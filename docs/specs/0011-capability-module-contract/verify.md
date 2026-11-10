# Verify: capability and module contract · spec 0011 · updated 2026-11-06

_Steps derived from spec 0011 acceptance criteria. `/check verify` runs these; `/test` locks the durable ones._

## Milestone 2: bindings in full

### Commands

- [ ] `go test -race -count=1 -tags e2e -run TestExternalEndToEnd ./libs/render/` (needs Docker) → passes: the consumer `pg-roaming` gets the same env names bound to `postgres` or to an external server, `DB_PASSWORD_FILE` is a path under `/run/secrets/`, `setup.sh` stops with `missing secrets/ext/db_password: password of ext, a postgresql service. Password of the login.` and creates nothing there, and after you place the file the consumer is healthy, so it signed in with the delivered values → AC-1, AC-15
- [ ] `go test -race -count=1 -tags e2e -run TestProvisionEndToEnd ./libs/render/` (needs Docker) → still passes; no service env holds a secret value → AC-5, AC-8
- [ ] `go test -race -count=1 -run 'TestRenderBindingsGolden|TestRenderManagedGolden' ./libs/render/` → passes; review `libs/render/testdata/bindings/golden/docker-compose.yml` → AC-8, AC-12, AC-15
- [ ] `go test -race -count=1 -run 'TestModuleRules|TestModuleEnvironmentAndClientFields|TestParseEnvironment' ./libs/contract/` → passes: a bad `environment` placeholder, a secret config value in a placeholder, an alias naming no config value, and a required `to_provider` field with no consumer source are each refused → AC-2
- [ ] `go test -race -count=1 -run TestGuardAcceptsEveryRender ./libs/render/` → passes: the guard accepts every managed render with nothing refused, the bindings render's `notifier` with `cap_add: [NET_BIND_SERVICE]` included (spec 0022) → AC-11, AC-21

### Golden review (libs/render/testdata/bindings/golden/)

- [ ] the `password` of `app`'s `db` slot is mounted at `/run/app/db-password` (its fixed path), from `./secrets/billing/db_password`, and no env holds it → AC-8, AC-15
- [ ] `app` has no `APP_DB_CA` and `APP_NOTIFY_REGION`: optional fields the provider gives no value for are not delivered, and `APP_REGION` is `region=` → AC-8
- [ ] `notifier` mounts `/run/secrets/clients/events/token/app` (a `both` field) and `/run/secrets/clients/events/key/app` (a `to_provider` field from `secrets/app/notify-key`), and its env holds the two directories → AC-8, AC-18
- [ ] `notifier` joins `egress` and `internal`, each with `notifier.bindings.internal` and `${MP_NOTIFIER__PUBLIC_NAME:-notify.example.test}`; `app` joins `egress` and `internal` with no `network.egress` in its entry, because its `db` slot binds to the external `billing` and its `audit` slot through the external provisioner `shared` (task 13b) → AC-12, AC-1
- [ ] `pg-roaming`'s entry has no `network.egress`, and `TestExternalEndToEnd` still passes (task 13b) → AC-1
- [ ] `notifier` has `user: "0:0"`, `cap_add: [NET_BIND_SERVICE]`, `tmpfs: [/tmp]` and no `read_only`; `app` has no `user`; every other service keeps `read_only: true`, `cap_drop: [ALL]`, `no-new-privileges` and `${MP_UID}:${MP_GID}` → AC-12
- [ ] `APP_DB_DSN` is `postgres://billing@db.example.test:5432/billing?sslmode=verify-full`, `APP_RUN_AS` is `${MP_UID}:${MP_GID}`, and `APP_PRICE` is `5$$ a month` (a literal `$`) → value sourcing: `environment` placeholders
- [ ] `shared_provision_app_audit` joins `egress` and `internal`, has no `depends_on`, and reads `secrets/shared/admin_admin_password`, which `setup.sh` lists as a file you place → AC-15, value sourcing: external field values
- [ ] In `golden-managed/`, the job of the external provisioner has the `job: provision` and `instance: shared` labels and no `module` label; `managed.json` lists the two placed files as `placed` → AC-11, AC-15

### Waiting on task 10 (the revoke job)

The spec now says how `up` waits (0014, *Waiting*: the wait anchor `mp_jobs`). Run these once the revoke job from branch `feat/capability-module-contract-revoke-job` and the anchor are merged.

- [ ] `go test -race -count=1 -tags e2e -run TestRevokeEndToEnd ./libs/render/` → passes, run five times in a row (`-count=5`) so a timing failure would show: after removing `a`, `a_db` cannot sign in while `a_db.notes` still holds 42; after binding `a` again with the same names it signs in and reads 42; after removing the last consumers the revoke job runs with `MP_KEEP: ""` and both logins lose sign in; the hand made role `handmade` keeps sign in throughout; every `up -d --wait` exits 0 → AC-6
- [ ] The e2e case for a failing revoke job (only binding removed, wrong admin password file) → `up --wait` exits non zero naming `postgres_revoke_<provide>`, and a second `up` exits non zero again → AC-6
- [ ] `go test -race -count=1 -run TestRevokeJob ./libs/render/` → `MP_KEEP` is the sorted logins of every binding to the provide, and the job waits for every provision job of that provide → AC-6
- [ ] Review `libs/render/testdata/whoami-postgres/docker-compose.yml`: `mp_jobs` depends on the revoke job with `service_completed_successfully`, uses its image, has `network_mode: none` and no `networks`, `init: true`, the hardening defaults and no `restart`; in the managed golden it has `restart: unless-stopped` and only the `graph`, `stack` and `job: wait` labels, and `managed.json` gains no image or secret for it; a golden with no revoke job has no `mp_jobs` → AC-6, AC-11, AC-12
- [ ] `go test -race -count=1 -run TestGuardAcceptsEveryRender ./libs/render/` → the guard accepts the managed `mp_jobs`, and refuses `init: false`, `network_mode: host`, `network_mode: none` together with `networks`, and `restart: "no"` on it → AC-11
- [ ] On a deployed stack with a revoke job, the deployer's live state shows no row for `mp_jobs` → AC-11
- [ ] `go test -race -count=1 -run TestGuard$ ./libs/render/` → the cases `init on a module`, `no network on a module`, `an unknown job kind`, `a wait anchor on host`, `a wait anchor on a network`, `a wait anchor not restarted` and `init off` are refused; `the wait anchor` and `a revoke job restart no` are accepted (the refusals above run here, not in `TestGuardAcceptsEveryRender`) → AC-11
- [ ] `go test -race -count=1 -tags e2e -run TestWaitAnchorImages ./libs/render/` → `sleep infinity` keeps running with `--init`, `--read-only`, `--cap-drop ALL`, `no-new-privileges`, a non root user and `--network none` in every pinned job image (`postgres:18.6`, Debian; `smallstep/step-cli:0.30.2`, Alpine) → AC-6
- [ ] `go test -race -count=1 -run 'TestWaitAnchor$|TestEgressFromBindings' ./libs/render/` → `mp_jobs` waits for each revoke job of two servers and is absent from a graph with no job; `pg-roaming` joins `egress` when bound to an external server or through an external provisioner, and only `internal` when bound inside the stack → AC-6, AC-12, AC-1
- [ ] Render the sample twice in a row with no change and `up` each time → no role or database added or dropped → AC-5

### The client of a for_each step (task 9b)

- [ ] `go test -race -count=1 -run TestModuleRules ./libs/contract/` → passes: `invalid_setup_step` for each refused case in 0013's *Critical test scenarios* (`{client}` with no `for_each` in env, command, entrypoint or writes; the four bad write forms; an undeclared folder; `asks` under `for_each`; the three bad `{provide}` reads; an `env` key `MP_INSTANCE`); a `for_each` step writing `client-digests/{client}` with `env: {CLIENT: "{client}"}` is accepted → AC-2
- [ ] `go test -race -count=1 ./libs/contract/` → the paper platform still loads and checks: `authelia`'s `client-digest` step has `env: {CLIENT: "{client}"}`, writes `client-digests/{client}`, and no `MP_CLIENT` is left in `libs/contract/testdata/` → AC-16, AC-2
- [ ] `task generate` leaves no diff after the `contracts/openapi/renderer.yaml` description change → AC-2

The `setup.sh` behavior (one run per client, skip per client, the empty folder, stale files kept) is checked with task 14 (0013, *Critical test scenarios*) → AC-10

## Acceptance criteria coverage (milestone 2)

- AC-1: external e2e step 1 · AC-6: waiting on task 10 · AC-8: golden review steps 1 to 3, provision e2e · AC-11: guard step, managed labels · AC-12: golden review steps 4 and 5 · AC-15: external e2e, golden steps 1 and 7 · AC-18: the generic `to_provider` file delivery (golden step 3); `caller_key` itself lands in milestone 4 · AC-2: the contract rule cases

## Milestone 3: install time

### Asked values, secrets, setup steps and setup.sh (task 14)

#### Commands

- [ ] `go test -race -count=1 -run 'TestCheckInstallTime|TestBindingOrder' ./libs/check/` → `setup_order` for a step reading a later step's output, a file the provider writes at run time (`{volume}`), a text field, a file you place for an external, and a client key a consumer's own step writes; none for steps in order; `env_name_collision` for `x` (`a--b`) and `x--a` (`b`), both `MP_X__A__B`; steps run providers first, ties by name → AC-10
- [ ] `go test -race -count=1 -run 'TestEREPattern' ./libs/render/` → for every case `setup.sh`'s check (`printf` to an ERE, `LC_ALL=C grep -E`) accepts exactly the answers Go's `regexp` accepts, non ASCII answers included; word boundaries, a class of only some non ASCII characters, an empty match and a literal newline are refused with `ErrNotRendered` → AC-10
- [ ] `go test -race -count=1 -run 'TestRenderInstallGolden|TestRenderManagedRefusesInstallTime' ./libs/render/` → the install golden matches; the managed flavor refuses a module that needs `setup.sh` (task 16 turns this into the 422 `setup_required`) → AC-10, AC-11
- [ ] `go test -race -count=1 -tags e2e -run TestInstallSetup ./libs/render/` (needs Docker and `postgres:18.6`) → with answers piped in, the defaults fill (`origin` from the `domain` answer), `.env` holds each answer, secrets are written, steps run in the module's image with `{client}` replaced in env, command and file name while `${GREETING}` stays as written; a second run asks nothing and leaves every file's content, mode and mtime unchanged; a third client runs the `for_each` step once, for it only; an unbound client's file stays; with no client the folder exists, empty, and the step never runs; a wrong piped domain or an out of range port stops `setup.sh` naming the value; the project then starts with `up --wait` and `docker inspect` shows no secret value in any env → AC-10, AC-8

#### Golden review

- [ ] `libs/render/testdata/install/golden/setup.sh` runs in 0013's order: `MP_UID`/`MP_GID`, asked values (instances then values in name order, the secret value without echo), `from: ask` secrets, generated files (module secrets, then per binding files), setup steps (`keeper` before `alpha` and `beta`), data folders → AC-10
- [ ] Each `docker run` has `--rm -i --network none --user "$(id -u):$(id -g)"`, `secrets/<i>` at `/out`, only its declared reads read only under `/in`, `MP_GRAPH`, `MP_INSTANCE` and one `MP_ASK_<V>` per non secret asked value; a step with asks gets its answers on stdin, any other `</dev/null` → AC-10
- [ ] `libs/render/testdata/install/golden/docker-compose.yml`: asked values arrive as `${MP_KEEPER__<V>}` with no default, `{uid}` as `${MP_UID}`, the asked secret and `from: ask` secret as files with `_FILE` env, step written files as Compose secrets, the `sealed/` folder as a read only bind → AC-10, AC-8

#### UI / manual

- [ ] Render `libs/render/testdata/install/graph.yaml`, run `sh setup.sh` in a terminal, answer the domain with `Not A Domain!` → it says why and asks again; the unseal key, license and admin password are typed without echo → AC-10

## Acceptance criteria coverage (milestone 3, task 14)

- AC-10: renderer order step, pattern step, golden review, e2e step and the manual terminal step · AC-8: e2e `docker inspect` step and the compose golden review · AC-11: the managed refusal for now (task 16 brings 422 `setup_required` and `host_access`)

### Templates (task 15)

#### Commands

- [ ] `go test -race ./libs/contract/ -run 'TestTemplateRules|TestTemplatesAccepted|TestLoadTemplatesExecute'` → passes: a missing file, a parse error, a function that is not built in (`env`), `.Uses.nope`, `.Bogus`, `.Provides.api`, a secret field in `Values`, an unguarded optional slot, a consumer side field in `.Clients` and a broken key under own delims are each `invalid_template` at catalog load, and the entry is left out → AC-9, AC-2
- [ ] `go test -race ./libs/render/ -run TestRenderTemplatesGolden` → the golden matches, a second render gives a byte identical zip, and a render without the templates fails with `ErrNoTemplate` → AC-9
- [ ] `go test -race ./libs/check/ -run TestPaperPlatform` and `go test -race ./catalog/` → the paper platform's templates and the built in catalog still load → AC-2, AC-16
- [ ] In `libs/render/testdata/templates/golden/`, `MP_UID=1000 MP_GID=1000 docker compose config -q` → exits 0 → AC-9

#### Golden review (libs/render/testdata/templates/golden/)

- [ ] `config/gateway/routes.conf`: graph `templates`, host `gateway.templates.internal`, hooks port 9000; backend `whoami.http`, `http://whoami.templates.internal:80`, the `url` value from whoami's provide; `# no cache bound` for the unbound optional slot; clients `alpha` then `beta`, each with `label=sender` from its slot's `{value}` source (`.Clients` data, task 12) → AC-9
- [ ] `config/gateway/app.yml`: the `[[ ]]` delims filled, the `{{ env "GATEWAY_TOKENS_DIR" }}` text kept as written → AC-9
- [ ] `docker-compose.yml`: top level `configs` `gateway_app.yml` and `gateway_routes.conf` pointing at `./config/gateway/...`, mounted on `gateway` at the declared paths; both files mode 0644 in the zip → AC-9

#### Value sourcing

- [ ] Rename the graph in `testdata/templates/graph.yaml` (for example `name: other`) and render → every host in `routes.conf` becomes `<instance>.other.internal` → AC-9 (template data from the graph)
- [ ] Bind `cache` to a `postgres` instance and render → the `cache` line shows its host instead of `# no cache bound` → AC-9 (`.Uses.<slot>.Bound`)
- [ ] Remove `beta` from the graph and render → only `client alpha` remains, and `clients: 1` in `app.yml` → AC-9 (`.Clients` per current binding)

## Acceptance criteria coverage (milestone 3, task 15)

- AC-9: the contract rule step, the render golden and repeat step, the golden review and the value sourcing steps · AC-2: the refused fixtures and the paper platform step · Deployed graphs with templates: the guard and the deployer's path list still refuse `config/<instance>/<file>` until task 16

### Host access and the managed refusals (task 16)

#### Commands

- [ ] `go test -race ./libs/contract/ -run 'TestModuleRules|TestHostAccessEntry'` → passes: a publish port of 0, a host port of 70000, a `{config}` host port naming no int value, a relative or config naming no text value host path, `{install: ../etc}` and `{install: .}`, a `{config}` or relative container path, a container path mounted twice, and `create` on a plain host path are each refused; every form of `publish` and `host_mounts` together is accepted → AC-2
- [ ] `go test -race ./libs/check/ -run 'TestCheckHostAccess|TestPaperPlatform'` → passes: warning `host_access` at each instance that publishes or mounts, the resolved graph stays ok with its digest; the paper platform warns at `deployer-docker` and `ingress` only → AC-11
- [ ] `go test -race ./libs/render/ -run 'TestRenderHostAccessGolden|TestRenderManagedRefuses|TestRefuse|TestGuard'` → passes: the managed flavor refuses `host_access` at `agent` and `edge` and `setup_required` at `alpha`, `beta` and `keeper`, rendering nothing; the guard accepts a template output and the templates render; for `cap_add` see the Linux capabilities steps below → AC-11, AC-12
- [ ] `go test -race ./services/renderer/internal/api/ -run TestRenderManagedRefuses` → `POST /v1/check` answers 200 with warning `host_access`; `POST /v1/render/managed` answers 422, not ok, no digest, with error `setup_required` at `modules.ask` and error `host_access` at `modules.web`, the warning replaced → AC-11
- [ ] `go test -race ./services/deployer-docker/internal/deploy/ -run 'TestApplySucceeds|TestApplyRefused'` → `config/app/routes.conf` is written 0644 in a 0755 folder; `config/a/b/c`, `config/a/..` and `config/A/x` are refused with `invalid_path` → AC-11
- [ ] In `libs/render/testdata/host-access/golden/`, `MP_UID=1000 MP_GID=1000 docker compose config` → `edge` publishes 8080 and 9443 and joins `egress`; `agent` binds `/etc/hostname` read only and `/var/spool/agent` read write and joins `internal` only → AC-12

#### Golden review (libs/render/testdata/host-access/golden/)

- [ ] `docker-compose.yml`: `edge` keeps its `entrypoint` and `command` (`$HOSTNAME` escaped as `$$HOSTNAME`), has the hardening defaults, and `ports` with `published: ${MP_EDGE__HTTP_PORT:-8080}` and `"9443"`; `agent` has two bind mounts, the first `read_only: true` → AC-12

#### Value sourcing

- [ ] Set `MP_EDGE__HTTP_PORT=8181` in the golden's `.env` and run `docker compose config` → `edge` publishes 8181; with the line removed it publishes 8080, the entry's default → AC-11 (host port from `{config}`)
- [ ] Set `MP_AGENT__SPOOL=/srv/spool` the same way → `agent` binds `/srv/spool` at `/var/spool/agent` → AC-11 (host path from `{config}`)
- [ ] Render a managed project as the deployer does and compare `deploy.env` → `MP_UID` and `MP_GID` are the deployer's `platform-uid` and `platform-gid` → AC-12 (managed user)

## Acceptance criteria coverage (milestone 3, task 16)

- AC-11: the check warning step, the managed refusal steps, the deployer path step and the host port and path value sourcing steps · AC-12: the render golden, the Compose config step and the managed user step · AC-2: the refused fixtures · `cap_add` on deployed graphs: decided by 0022, see tasks 16b and 16c below

### Linux capabilities on deployed graphs (tasks 16b and 16c, spec 0022)

#### Commands

- [ ] `go test -race ./libs/contract/ -run 'TestModuleRules|TestLinuxCapEntry|TestUndeployableLinuxCaps|TestModuleHostAccess'` → passes: `cap_add` of `net_bind_service` and `CAP_NET_BIND_SERVICE` are `invalid_hardening` at `hardening.cap_add.0` with `; write NET_BIND_SERVICE`, `ALL` says `ALL is not allowed here; name each Linux capability`, a second `NET_BIND_SERVICE` is refused at `.1` naming `.0`, `NOT_A_CAP` gets no suggestion; `SYS_ADMIN` and `CHECKPOINT_RESTORE` pass the catalog → AC-2
- [ ] `go test -race ./libs/check/ -run 'TestCheckHostAccess|TestPaperPlatform'` → passes: `admin` (`SYS_ADMIN`) warns `Instance admin adds the Linux capability SYS_ADMIN, so it reaches the host it runs on; a deployed graph refuses this.`; `gate` (publish plus `SYS_ADMIN`) gets one warning naming both; `low` (`NET_BIND_SERVICE` only) gets none; the paper platform still warns at `deployer-docker` and `ingress` only → AC-11
- [ ] `go test -race ./libs/render/ -run 'TestRenderManagedRefuses|TestRenderManagedRefusesLinuxCap'` → passes: the `linuxcap/refused.yaml` graph answers one `host_access` refusal at `modules.admin` and renders nothing in managed; the static render still writes `cap_add` with `SYS_ADMIN` → AC-11
- [ ] `go test -race ./libs/render/ -run 'TestGuard$|TestLoadProjectRefusesBrokenFiles'` → passes: `cap_add: [NET_BIND_SERVICE]` and `[]` pass; `[SYS_ADMIN]`, `[net_bind_service]`, `[CAP_NET_BIND_SERVICE]` and `[ALL]` fail at `services.app.cap_add.0`, `[NET_BIND_SERVICE, SYS_ADMIN]` at `.1`; a string or null `cap_add` fails at load (the Compose schema stops it before the guard); a service, a provision job, a revoke job and `mp_jobs` without `cap_drop` or `security_opt` fail at that key; `cap_drop: [NET_RAW]`, `seccomp=unconfined` and `apparmor=unconfined` fail; `privileged` and `profiles` still fail → AC-21
- [ ] `go test -race ./services/deployer-docker/internal/deploy/ -run TestApplyRefused` → passes: the deployer refuses a project adding `SYS_ADMIN` and one missing `cap_drop` with `host_access`, before it touches the folder or runs compose → AC-21
- [ ] `go test -race -count=1 -tags e2e -run TestLinuxCapsEndToEnd ./libs/render/` (needs Docker) → passes: the managed `linuxcap` render passes the guard and comes up; `/proc/1/status` of `low` shows `Uid: 0`, `CapEff: 0000000000000400` and `NoNewPrivs: 1` → AC-21

#### Value sourcing

- [ ] In `libs/render/testdata/linuxcap/catalog/modules/lowport/module.yaml` change `cap_add` to `[NET_BIND_SERVICE, CHOWN]` and run the check test → `low` now warns `adds the Linux capability CHOWN`, so the refused names come from `UndeployableLinuxCaps` of the entry; revert → AC-11 (names a deployed graph refuses)
- [ ] Change `DeployableLinuxCap` in `libs/contract/linuxcap.go` to also admit `CHOWN` and run `TestGuard` → the `SYS_ADMIN` cases still fail, so the guard reads the same single source; revert → AC-21 (whether an item may pass)
- [ ] Run the e2e with `cap_add: []` on `lowport` → `CapEff` is `0000000000000000`, so bit 10 comes from the admitted name alone; revert → AC-21 (the expected `CapEff`)

## Acceptance criteria coverage (milestone 3, tasks 16b and 16c)

- AC-2: the `cap_add` catalog rule step · AC-11: the check warning step, the managed refusal step and the first value sourcing step · AC-21: the guard step, the deployer step, the e2e step and the last two value sourcing steps

## Milestone 3b: slot fields (0025, task 16d)

### Commands
- [ ] `go test -race -run 'TestField|TestRequiredFieldBinding' ./sdk/` → passes: the optional `realm` slot of the test module returns one typed value per field, nil for an optional field not delivered, and `false` when nothing is set → AC-22
- [ ] `go test -race -run 'TestGenerateFields|TestGenerateUses|TestGenerateRefuses' ./sdk/cmd/sdkgen/` → passes: `<Slot>Binding`, `Uses.<Slot>`, the enum types, `Fields` in the binding spec, a slot that maps nothing gets no getter, and a Go name clash is refused → AC-22
- [ ] `task generate` twice, then `git diff --stat` → no `config.gen.go` changes on the second run → AC-22
- [ ] Start a module on the sdk with a bound slot holding a required field unset, an `int` field `abc`, an enum miss, a missing secret file and a plain text certificate file → one line each, `config: uses.<slot>.<field> (<ENV>): <code>: <message>`, sorted, exit 1, no `config loaded` line (`TestFieldBindingProblems`) → AC-23
- [ ] Start it with the slot bound → the `config loaded` line's `values` lists `uses.<slot>.<field>` as `binding` or `unset`, and no secret or PEM content shows in any log line → AC-23
- [ ] A `BindingResolver` answer with a missing required field, a text `port`, a pattern miss, an enum miss, a non PEM certificate or an unmapped field → the getter returns an error that is not `ErrCallerSource` and no value; a resolver slower than 200 ms → `ErrCallerSource` (`TestFieldBindingResolver`) → AC-23
- [ ] `go test -race -run TestStartRefused ./services/renderer/internal/api/` → internal mode without `MP_PLATFORM_HTTP_PORT` logs exactly `config: uses.ingress (MP_PLATFORM_HTTP_PORT): missing: internal mode needs the ingress binding`; with only the HTTP port set, `uses.ingress.https_port` is `missing` → AC-24
- [ ] Start `renderer` in public mode with no `MP_PLATFORM_*` env (as `deploy/online/compose.yaml` does) → it starts → AC-24
- [ ] `go test -race -run 'TestReadSettings|TestNoOldClientID' ./services/api/internal/api/` → `MP_BASE_PATH` empty starts at the site root, `/platform` sets the base path and redirect URL, `MP_OIDC_ISSUER` unset is `config: uses.oidc.issuer (MP_OIDC_ISSUER): missing`, and `mp-api` appears only in `deploy/managed/` → AC-24
- [ ] `grep -n "platform-http-port\|platform-https-port\|oidc-issuer\|oidc-client\|oidc-ca\|public-origin\|base-path\|admin-email" catalog/modules/{api,renderer,deployer-docker}/module.yaml` → no match → AC-24
- [ ] `task smoke:managed` → `@deploy` passes (install, sign in through the `oidc` slot, deploy). The `@after-restart` remove step fails the same way on commit 2a9ee81, before this change → AC-24

### Value sourcing
- [ ] SDK start, a slot field's value: set `TM_REALM_PORT=9443` in an env file listed in `MP_ENV_FILE` instead of the process env → `Realm().Port` is 9443; set it in both → the process env wins
- [ ] SDK start, slot bound or not: set only `TM_REALM_HINT` → the start is refused with a `missing` line for each required realm field; set none, or every one to empty → unbound, no line
- [ ] SDK call, a slot field for a caller: a resolver answer for `alice` → `alice` gets it, `bob` and a call with no caller get the start values, and a second call for `alice` does not ask the resolver again
- [ ] `renderer` start, platform ports: internal mode with `MP_PLATFORM_HTTP_PORT=8080` → `POST /v1/render/managed` of a graph that publishes host port 8080 is refused as reserved, and port 80 is no longer reserved
- [ ] `deployer-docker` start: without `MP_PLATFORM_HTTP_PORT` → `config: uses.ingress.http_port (MP_PLATFORM_HTTP_PORT): missing`, exit 1
- [ ] `api` start, origin, base path, issuer, client id, client secret, CA, admin email: change each env name in `deploy/managed/compose.yaml` one at a time → api reads the new value (the redirect URL follows `MP_PUBLIC_ORIGIN` and `MP_BASE_PATH`); `MP_OIDC_CA_FILE` naming a file with no certificate → `config: uses.oidc.ca (MP_OIDC_CA_FILE): invalid`

## Acceptance criteria coverage (milestone 3b, task 16d)

- AC-22: the first three command steps · AC-23: the next three command steps and the first three value sourcing steps · AC-24: the renderer, api, catalog and smoke steps and the last three value sourcing steps

## Milestone 3b, task 5 of 0025: non SDK values as `.env` variables, the `.env` check (updated 2026-11-06)

### Commands
- [ ] `go test -race -run 'TestRenderEnvCheckGolden|TestRenderInstallGolden' ./libs/render/` → passes; in `testdata/env-check/golden/docker-compose.yml` every value of `ingress` is `<ENV>: ${MP_INGRESS__<V>:-<d>}`, the optional `note` is `${MP_INGRESS__NOTE:-}`, the `$` in the banner default is `$$`; in the install golden the asked values stay bare (`${MP_KEEPER__DOMAIN}`) and `run-uid` stays `${MP_UID}` → AC-25
- [ ] Open `testdata/env-check/golden/.env` and `testdata/install/golden/.env` → one block per rendered value in instance then value order: the description, then `# MP_<I>__<V>=<d>` (the default as typed, `$` not doubled), or `# asked by setup.sh`; no block for `run-uid` or a secret → AC-25
- [ ] `go test -race -run TestNonSDKValuesReachTheService ./libs/render/` → compose-go reads `GATE_BANNER` as `pay $5 # now` and `GATE_NOTE` as empty with no `.env` line; `MP_INGRESS__TLS_MODE=acme` gives `acme`; an empty `MP_INGRESS__PORT` gives the default → AC-25
- [ ] `go test -race -tags e2e -run TestInstallSetupEndToEnd ./libs/render/` (Docker) → keeper's env holds `KEEPER_LEVEL=info`, `KEEPER_BANNER=costs $5 #1` and an empty `KEEPER_MOTD`; after `MP_KEEPER__LEVEL=debug` is added to `.env`, `setup.sh` passes and `up -d` gives `KEEPER_LEVEL=debug` → AC-25
- [ ] `go test -race -run TestComposeEnvHoldsNoPlatformVariable ./services/deployer-docker/internal/docker/` → the deployer's Compose env is `PATH` and `HOME` only, whatever `MP_*` its own process has → AC-25
- [ ] `go test -race -run 'TestSetupChecksEnvLines|TestSetupWithoutEnvFile' ./libs/render/` → `MP_INGRESS__TLS_MODE=acmee` stops `setup.sh` with exit 1 and `setup.sh: MP_INGRESS__TLS_MODE in .env: the answer is not one of: internal acme`, and no file changes; `acme`, `'acme'`, a commented bad line, a bad line then a good one, and an empty optional value pass and a second run changes nothing; a good line then a bad one, `0` and `80a` for the port, and an empty port stop (`is empty`); `MP_INGRESS__TLS_MDE=acme` prints `setup.sh: warning: MP_INGRESS__TLS_MDE in .env names no value of ingress` and goes on; with no `.env` it passes → AC-26
- [ ] Render the sample zip, unzip it, `sh setup.sh`, then add `MP_<I>__<V>=<bad>` for any rendered value and run `sh setup.sh` again → it stops naming the line before it asks or creates anything → AC-26
- [ ] Open any rendered `README.md` with a `.env` block → it says to run `./setup.sh` again after you edit `.env` → AC-26

### Value sourcing
- [ ] Render, a non SDK value's Compose default: change a catalog `default` in the env-check fixture and render → the Compose line and the `.env` hint change together (the graph value source lands with task 7)
- [ ] Render, env file blocks: change a value's `description` → its `.env` block changes, nothing else does
- [ ] `setup.sh`, the check of `.env` lines: tighten a `pattern`, `min`, `max` or `values` in the fixture and render → the `check_value` line in `setup.sh` carries the new rule, and a `.env` line that passed before now stops `setup.sh`

### Not yet covered
- The platform flavor's `.env.example` (AC-25) waits for the platform render flavor (milestone "The platform is a graph").
- The managed flavor renders the same Compose lines and `.env` blocks (see `testdata/bindings/golden-managed/`); the guard reads them after interpolation, as before.

## Acceptance criteria coverage (milestone 3b, task 5 of 0025)

- AC-25: the first five command steps and the first two value sourcing steps; the platform `.env.example` part is open · AC-26: the last three command steps and the third value sourcing step

## Milestone 3b, tasks 6 to 9 of 0025: graph config values, the inspector, the paper Caddyfile (updated 2026-11-06)

### Commands

- [ ] `go test -race -count=1 -run 'TestCanonicalRoundTrip|TestConfigText|TestDecodeGraphRejects' ./libs/contract/` → passes: `config-set`, `config-problems` and `config-quotes` round trip byte for byte; each config value is written double quoted, `config: {}` and `config: null` are left out, a long line is never wrapped; `port: 5432`, `on: yes`, a null value and a list are `invalid_graph`; a v0alpha1 graph with `config` is `invalid_graph` at `modules.a.config` → AC-27
- [ ] `pnpm --dir web exec vitest run src/test/yaml.test.ts` → the editor's writer gives the same bytes for every fixture, the config ones included, and leaves an empty or null `config` out → AC-27
- [ ] `go test -race -count=1 -run 'TestCheck$|TestGraphTextRule|TestConfigMessages|TestConfigInDigest' ./libs/check/` → `config-problems` gives one problem per path in the AC-27 order (`unknown_config`, `invalid_config` for `{`, `12`, `medium` and an empty required value, `config_not_settable` for the secret set to `{`, the `{uid}` value and the `host_mounts` source, `config_not_allowed` on the external, `missing_config` as an error on a non SDK instance and a warning on the SDK one); `"q"`, `'q'`, `""`, `"é"` and a tab are `invalid_config` on both runtimes while ` "q"`, `"q`, `a"b`, `"q'`, `"`, `yes` and `0x1` check ok; messages name the rule, never the text; a changed value changes the digest → AC-27
- [ ] `go test -race -count=1 ./sdk/manifest/ ./libs/contract/` → the catalog case `enum value in matching quotes` is `invalid_rule` at `config.mode.values.1` in both checkers → AC-27
- [ ] `go test -race -count=1 -run 'TestRenderGraphConfigGolden|TestGraphValuesReachTheService|TestSDKGraphValuesRoundTrip' ./libs/render/` → in `testdata/graph-config/golden/docker-compose.yml` each graph value is the variable's default (`$` as `$$`), `TELLER_MOTD` is `${MP_TELLER__MOTD:-}` (empty on purpose), the asked `mode` and `origin` the graph leaves out stay bare; compose-go reads `TELLER_NOTE` as `a'b`, `TELLER_MOTTO` as `$x`, `TELLER_TAGLINE` as `a #b`, and a `.env` line still wins; `config/tally.env` holds `TALLY_GREETING=a"b` and `TALLY_NOTE="q` uncommented and `sdk/envfile` reads them back unchanged → AC-25, AC-27
- [ ] `go test -race -count=1 -run 'TestSetupGraphAnswers|TestManagedGraphAnswersSetup' ./libs/render/` → `setup.sh` asks only `mode` and `origin`, prints `set by the graph  MP_TELLER__NOTE=a'b` (and the same for `$x`, `a #b`, `example.org`), writes none of them into `.env`, defaults `origin` to `https://example.org`, and hands every text to the setup step as typed in `MP_ASK_<V>`; with `MP_TELLER__DOMAIN='other.org'` in `.env` it prints `.env overrides the graph  MP_TELLER__DOMAIN` and the origin default follows `other.org`; a managed render of `simple` with `domain` set is not refused, without it `setup_required` → AC-28, AC-11, AC-10
- [ ] `pnpm --dir web exec vitest run src/test/inspector-config.test.tsx src/test/edit.test.ts` → the Config section lists `label`, `level`, `mode`, `verbose` (no secret, `{uid}` or host path value), typing writes on Enter only, clearing removes the key and then `config`, an `invalid_config` problem shows once, under its field, an unknown key shows in *Unknown values* with a working remove button, and an external instance has no Config section → AC-29
- [ ] `task smoke` → `a config value set in the inspector reaches the downloaded zip` passes: the check request holds `config: {proxy-hops: "2"}`, the zip's `config/renderer.env` holds `MP_PROXY_HOPS=2` and its `graph.yaml` `proxy-hops: "2"` → AC-29
- [ ] `go test -race -count=1 -run 'TestPaperPlatform|TestPaperCaddyfile' ./libs/check/` → the platform graph (with `renderer-mode: "internal"`) checks ok with only the two `host_access` warnings; the Caddyfile holds both snippets, `import tls-{$MP_TLS_MODE}` and one `reverse_proxy {$MP_WEB_UPSTREAM:http://web.modular-platform.internal:8080}`; the caddy entry declares no `web-upstream` → AC-30

### UI / manual

- [ ] `task dev`, open a graph file with a `renderer` instance, select it → the inspector shows Config with `renderer-mode` (a select: Not set, public, internal), `renderer-rate` and `proxy-hops` (text, placeholder the default); set `proxy-hops` to `abc`, press Enter → after the resolved graph, `Config value proxy-hops of instance renderer: "abc" is not a whole number.` shows under the field and the field has a danger border; Undo restores the field → AC-29
- [ ] Same screen with the keyboard only: Tab reaches each field, Enter writes, the select opens with arrow keys; `/design` shows the TextField states in both themes → AC-29

### Value sourcing

- [ ] Render, a non SDK value's default: set `level: "debug"` in the graph and no `.env` line → `TELLER_LEVEL` is `debug`; remove it from the graph → the catalog default `info`; an optional value with neither → empty → AC-25
- [ ] Render, an SDK env file line: a graph value → `<ENV>=<text>`; none → the commented default → AC-27
- [ ] `setup.sh`, a value set by the graph: the default baked into the Compose line and the `graph_set` line match the graph text byte for byte, `'` and `$` included → AC-28
- [ ] Check, the config problems: change the entry (drop a value the graph sets) → `unknown_config` at that path, and the inspector moves it to *Unknown values* → AC-27, AC-29
- [ ] Editor, settable values and defaults: a catalog value with `default: "{uid}"` or named by `host_mounts` never shows a field → AC-29

### Acceptance-criteria coverage

- AC-25: the graph-config golden and compose-go steps, first value sourcing step · AC-27: the contract, writer, renderer, catalog and SDK env file steps · AC-28: the setup.sh and managed steps · AC-29: the inspector Vitest, smoke and manual steps · AC-30: the paper platform step · AC-10, AC-11: the setup.sh and managed steps

## Milestone 4: the platform is a graph, tasks 17 to 19 (updated 2026-11-06)

### Commands

- [ ] `go test -race ./libs/render/ -run 'Platform'` → the platform golden in `libs/render/testdata/platform/` matches (Compose file, `setup.sh`, `.env.example`, README, `graph.yaml`, one golden per rendered template), two renders give byte identical zips, the static render refuses `{install}` with `ErrNotRendered`, and the managed render refuses `host_access` at `ingress` and `deployer-docker` and `setup_required` at `authelia` and `cert-issuer` → AC-16, AC-9, AC-11
- [ ] `go test -race ./libs/check/ -run TestPlatformGraph` → the eight platform modules are built in entries, `deploy/managed/graph.yaml` checks `ok` with warnings `host_access` at `ingress` and `deployer-docker` only, the 14 bindings sit where 0015 puts them, and the file is in canonical form → AC-16
- [ ] `task managed:render`, then `ls -a build/managed` → `docker-compose.yml`, `config/`, `setup.sh`, `.env.example`, `README.md`, `graph.yaml`, and no `.env`; running it twice and diffing the folders shows no change → AC-16
- [ ] `ls -a deploy/managed` → only `graph.yaml`, `compose.dev.yaml`, `compose.ci.yaml` and `README.md`: `compose.yaml`, `setup.sh`, `db-init/`, `Caddyfile`, `authelia/`, `cert-issuer/` and `.env.example` are gone → AC-16
- [ ] `task managed:zip VERSION=0.0.0`, then `unzip -l dist/modular-platform-0.0.0.zip` → every file under `modular-platform-0.0.0/` is one of `build/managed/` → AC-16
- [ ] `task images IMAGE_TAG=ci && SMOKE_DOCKER_RESTART=1 task smoke:managed` → `smoke-managed passed`: install with the rendered `setup.sh` (domain, empty origin, admin email, user name, password), the eight services healthy and the two provision jobs and the revoke job exited 0, sign in through Authelia, deploy the sample, running again after the Docker restart, remove with *Delete data too*, no secret in any log → AC-16
- [ ] `go test -race ./sdk/ -run TestCallerEndToEnd` → a token signed with a second key, one the provider holds for another consumer, sent with the caller's binding token, is 401 `invalid_token`; the caller's own token passes → AC-18
- [ ] `go test -race ./sdk/caller/ -run 'TestLoadKeys|TestGenerateKeyPair'` → the caller keys directory follows the token clients directory's entry rules, a token is checked only against its own consumer's keys, and `api caller-key generate`'s pair verifies → AC-18
- [ ] `grep -rn MP_CALLER_KEY_FILE --include='*.go' --include='*.md' sdk services libs` → no hit; `grep -n MP_CALLER_KEYS_DIR build/managed/docker-compose.yml` → set on `renderer` and `deployer-docker`, each with `api`'s public key mounted at `/run/secrets/clients/<provide>/caller_key/api` → AC-18

### Value sourcing

- [ ] `api`'s `MP_OIDC_ISSUER` in `build/managed/docker-compose.yml` is `${MP_INGRESS__ORIGIN}${MP_INGRESS__BASE_PATH:-}/auth` (`{uses: public, format}` through `authelia`); set `MP_INGRESS__BASE_PATH='/platform'` in `.env` and check sign in still lands on `<origin>/platform/auth`
- [ ] `api`'s `MP_OIDC_CA_FILE` is `/run/volumes/oidc/ca/root.crt` with `./data/ingress/data/caddy/pki/authorities/local` mounted read only (a `{volume}` source passed through `{uses, field}`), and `setup.sh` creates that folder before `up`
- [ ] `MP_RENDERER_URL` and `MP_DEPLOY_TARGET_URL` are `https://<instance>.modular-platform.internal:8443`, and `MP_*_CA_FILE` is `cert-issuer`'s `root-ca` (SDK TLS from the bound `acme` slot)
- [ ] `MP_DEPLOYMENTS_DIR` and `deployer-docker`'s stacks mount are `${MP_DIR}/stacks` on both sides, and `setup.sh` writes `MP_DIR` as the folder it sits in; run `setup.sh` from a folder whose path holds `'` → it stops with a message
- [ ] the question order follows the binding order: answer the domain `example.test` and an empty origin → `.env` holds `MP_INGRESS__ORIGIN='https://example.test'`

### Build findings (0015, *Milestone 4 build findings*)

- [ ] `go run ./catalog/cmd/render-platform -out build/managed -image-tag ci`, then `grep -n "image:" build/managed/docker-compose.yml` → `api`, `renderer`, `deployer-docker` and `web` end in `:ci`; `caddy`, `authelia`, `step-ca` and `postgres` keep their catalog tags; without `-image-tag` the four take the catalog's release tag
- [ ] Point `-graph` at a copy of the platform graph with one binding removed → it prints the check problem, exits 1, and `build/managed` is unchanged
- [ ] `grep -n -A3 "cap_add" build/managed/docker-compose.yml` → `deployer-docker` has `CHOWN`, `DAC_OVERRIDE`, `FOWNER` and `cap_drop: [ALL]`; `ingress`, `web` and `cert-issuer` have `NET_BIND_SERVICE` only; `go run ./catalog/cmd/render-platform -out build/managed 2>&1 | grep host_access` → the warning at `deployer-docker` names the three
- [ ] Copy `build/managed` to a scratch folder and run `setup.sh` there; run `task managed:zip VERSION=0.0.0`, unzip it, and copy the contents of its `modular-platform-0.0.0/` folder over the scratch folder → `.env` is byte identical (the zip holds only `.env.example`) → AC-25's platform part, open since 0025 task 5
- [ ] `task dev:managed`, stop it with Ctrl-C, run it again → the second run prints no password line, and `.env`, `secrets/`, `data/` and `stacks/` in `.dev/managed` are unchanged
- [ ] In a scratch copy of `build/managed`, `printf 'x.test\n\na@b.co\nAdmin!\nlongenough12\n' | sh setup.sh`, then the same with user `admin` and the password `short11char` → each time the `users` step stops with its message and writes no `secrets/authelia/users`

### Acceptance-criteria coverage

- AC-16: the golden, renderer, render, folder, zip and smoke steps, and the build findings steps · AC-18: the SDK caller steps and the `MP_CALLER_KEYS_DIR` check · AC-9, AC-11: the golden's refusal checks · AC-25: the platform `.env.example` step under build findings

## Milestone 5: TLS for unchanged providers, tasks 20 and 21 (updated 2026-11-06)

_AC-17 was not covered before this milestone. It changes two numbers in the milestone 4 steps above: the platform graph now has 15 bindings (`postgres.tls cert-issuer.certs` is the new one), and `smoke-managed` waits for four jobs (the certificate job of `postgres`, two provision jobs and the revoke job)._

### Commands

- [ ] `go test -race -count=1 ./libs/render/ -run 'TestPostgresCertificateJob|TestPostgresWithoutCertificate|TestManagedRefusesJobOutputs|TestRenderPlatformGolden'` → passes: the certificate job asks for `postgres.modular-platform.internal` and writes `/out/cert` and `/out/key` through bind mounts, `postgres` starts only after it, `api` connects with `verify-full` and the internal root, `setup.sh` makes the two files with `new_output`, `pg_hba.conf` has `hostssl` lines only; with `postgres.tls` unbound there is no job, `sslmode` is `disable` and `pg_hba.conf` has `host` lines; a deployed graph with a certificate output is `ErrNotRendered` → AC-17
- [ ] `go test -race -count=1 ./libs/check/ -run TestPlatformGraph` → passes with `postgres.tls cert-issuer.certs` among the 15 bindings → AC-17, AC-16
- [ ] `task images IMAGE_TAG=ci && task smoke:managed` (run it in the background, with `</dev/null` and a timeout) → `smoke-managed passed`, which now includes its `tls_checks` step: a plain connection to `postgres` is refused, `api` and `authelia` show `ssl = t` in `pg_stat_ssl`, the server certificate has more than 60 days left, a certificate replaced by one with 10 days left is reissued by the next `up` with a new serial and `postgres` keeps its start time, and the log scan still finds no secret value (the `*_cert` file is skipped as public, `uses_tls_key` is checked) → AC-17, AC-16
- [ ] `grep -n -B2 -A12 "new_output" libs/render/testdata/platform/setup.sh` → `new_output secrets/postgres/uses_tls_cert` and `..._key` are listed after the module secrets, the function makes an empty file with mode 0600 (`umask 077` at the top of the script) and keeps an existing one → AC-17
- [ ] Unzip `build/managed` into a scratch folder, run `setup.sh`, `docker compose up -d --wait` twice → the second `up` prints `has more than 30 days left: kept` in `docker compose logs cert-issuer_provision_postgres_tls`, and `sha256sum secrets/postgres/uses_tls_cert` is the same before and after → AC-17

### Value sourcing

- [ ] `sslmode` of the provided `server` field (`{platform: tls, on: verify-full, off: disable}`): in `deploy/managed/graph.yaml` set `postgres.tls: null`, run `task managed:render` → `MP_DATABASE_SSLMODE: disable`, no `MP_DATABASE_CA_FILE`, no `cert-issuer_provision_postgres_tls`; set it back → `verify-full` on `api`, `MP_ADMIN_SSLMODE: verify-full` on the three database jobs → AC-17
- [ ] `ca` of that field (`{platform: ca}`): with `tls` bound it is `./secrets/cert-issuer/root-ca` on `api` (`api_uses_main-db_ca`), `authelia` (`/run/secrets/db/ca.crt`) and the database jobs (`MP_ADMIN_CA_FILE`) → AC-17
- [ ] the yielded `cert` and `key` (`output`): the files are `secrets/postgres/uses_tls_cert` and `uses_tls_key`, mounted read only on `postgres` at `/run/tls/server.crt` and `server.key`, and writable at `/out/cert` and `/out/key` on the job only → AC-17
- [ ] the certificate's name (`MP_CONSUMER_HOST`): rename the instance `postgres` to `db` in a scratch copy of the graph and render → the job asks for `db.modular-platform.internal` and the files move to `secrets/db/` (setup.sh makes them empty, so the first `up` issues a new certificate) → AC-17
- [ ] the issuing settings (`url`, `provisioner`, `password`, `ca` of `cert-issuer`'s `certs` provide): `https://cert-issuer.modular-platform.internal:9000`, `jobs`, `secrets/cert-issuer/jobs-password`, `secrets/cert-issuer/root-ca` in the job's env and mounts → AC-17

### Acceptance-criteria coverage

- AC-17: the render tests, the check test, `smoke-managed`'s `tls_checks` (plain refused, TLS on `api` and `authelia`, 90 day certificate, reissue and reload without a restart, second `up` keeps the file), and the value sourcing steps · AC-16: the check test and the smoke job with the derived job count
