# Verify: capability and module contract · spec 0011 · updated 2026-11-05

_Steps derived from spec 0011 acceptance criteria. `/check verify` runs these; `/test` locks the durable ones._

## Milestone 2: bindings in full

### Commands

- [ ] `go test -race -count=1 -tags e2e -run TestExternalEndToEnd ./libs/render/` (needs Docker) → passes: the consumer `pg-roaming` gets the same env names bound to `postgres` or to an external server, `DB_PASSWORD_FILE` is a path under `/run/secrets/`, `setup.sh` stops with `missing secrets/ext/db_password: password of ext, a postgresql service. Password of the login.` and creates nothing there, and after you place the file the consumer is healthy, so it signed in with the delivered values → AC-1, AC-15
- [ ] `go test -race -count=1 -tags e2e -run TestProvisionEndToEnd ./libs/render/` (needs Docker) → still passes; no service env holds a secret value → AC-5, AC-8
- [ ] `go test -race -count=1 -run 'TestRenderBindingsGolden|TestRenderManagedGolden' ./libs/render/` → passes; review `libs/render/testdata/bindings/golden/docker-compose.yml` → AC-8, AC-12, AC-15
- [ ] `go test -race -count=1 -run 'TestModuleRules|TestModuleEnvironmentAndClientFields|TestParseEnvironment' ./libs/contract/` → passes: a bad `environment` placeholder, a secret config value in a placeholder, an alias naming no config value, and a required `to_provider` field with no consumer source are each refused → AC-2
- [ ] `go test -race -count=1 -run TestGuardAcceptsEveryRender ./libs/render/` → passes: the guard accepts the managed bindings render except `services.notifier.cap_add` (left for build task 16) → AC-11

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
- [ ] Review `libs/render/testdata/whoami-postgres/docker-compose.yml`: `mp_jobs` depends on the revoke job with `service_completed_successfully`, uses its image, has `network_mode: none` and no `networks`, `init: true`, the hardening defaults and no `restart`; in the managed golden it has `restart: unless-stopped` and only the `graph`, `deployment` and `job: wait` labels, and `managed.json` gains no image or secret for it; a golden with no revoke job has no `mp_jobs` → AC-6, AC-11, AC-12
- [ ] `go test -race -count=1 -run TestGuardAcceptsEveryRender ./libs/render/` → the guard accepts the managed `mp_jobs`, and refuses `init: false`, `network_mode: host`, `network_mode: none` together with `networks`, and `restart: "no"` on it → AC-11
- [ ] On a deployed stack with a revoke job, the deployer's live state shows no row for `mp_jobs` → AC-11
- [ ] `go test -race -count=1 -run TestGuard$ ./libs/render/` → the cases `init on a module`, `no network on a module`, `an unknown job kind`, `a wait anchor on host`, `a wait anchor on a network`, `a wait anchor not restarted` and `init off` are refused; `the wait anchor` and `a revoke job restart no` are accepted (the refusals above run here, not in `TestGuardAcceptsEveryRender`) → AC-11
- [ ] `go test -race -count=1 -tags e2e -run TestWaitAnchorImages ./libs/render/` → `sleep infinity` keeps running with `--init`, `--read-only`, `--cap-drop ALL`, `no-new-privileges`, a non root user and `--network none` in every pinned job image (`postgres:18.6`, Debian; `smallstep/step-cli:0.30.2`, Alpine) → AC-6
- [ ] `go test -race -count=1 -run 'TestWaitAnchor$|TestEgressFromBindings' ./libs/render/` → `mp_jobs` waits for each revoke job of two servers and is absent from a graph with no job; `pg-roaming` joins `egress` when bound to an external server or through an external provisioner, and only `internal` when bound inside the stack → AC-6, AC-12, AC-1
- [ ] Render the sample twice in a row with no change and `up` each time → no role or database added or dropped → AC-5

### The client of a for_each step (task 9b)

- [ ] `go test -race -count=1 -run TestModuleRules ./libs/contract/` → passes: `invalid_setup_step` for each refused case in 0013's *Critical test scenarios* (`{client}` with no `for_each` in env, command, entrypoint or writes; the four bad write forms; an undeclared folder; `asks` under `for_each`; the three bad `{provide}` reads; an `env` key `MP_INSTANCE`); a `for_each` step writing `client-digests/{client}` with `env: {CLIENT: "{client}"}` is accepted → AC-2
- [ ] `go test -race -count=1 ./libs/contract/` → the paper platform still loads and plans: `authelia`'s `client-digest` step has `env: {CLIENT: "{client}"}`, writes `client-digests/{client}`, and no `MP_CLIENT` is left in `libs/contract/testdata/` → AC-16, AC-2
- [ ] `task generate` leaves no diff after the `contracts/openapi/planner.yaml` description change → AC-2

The `setup.sh` behavior (one run per client, skip per client, the empty folder, stale files kept) is checked with task 14 (0013, *Critical test scenarios*) → AC-10

## Acceptance criteria coverage (milestone 2)

- AC-1: external e2e step 1 · AC-6: waiting on task 10 · AC-8: golden review steps 1 to 3, provision e2e · AC-11: guard step, managed labels · AC-12: golden review steps 4 and 5 · AC-15: external e2e, golden steps 1 and 7 · AC-18: the generic `to_provider` file delivery (golden step 3); `caller_key` itself lands in milestone 4 · AC-2: the contract rule cases

## Milestone 3: install time

### Asked values, secrets, setup steps and setup.sh (task 14)

#### Commands

- [ ] `go test -race -count=1 -run 'TestPlanInstallTime|TestBindingOrder' ./libs/planner/` → `setup_order` for a step reading a later step's output, a file the provider writes at run time (`{volume}`), a text field, a file you place for an external, and a client key a consumer's own step writes; none for steps in order; `env_name_collision` for `x` (`a--b`) and `x--a` (`b`), both `MP_X__A__B`; steps run providers first, ties by name → AC-10
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

- AC-10: planner order step, pattern step, golden review, e2e step and the manual terminal step · AC-8: e2e `docker inspect` step and the compose golden review · AC-11: the managed refusal for now (task 16 brings 422 `setup_required` and `host_access`)

### Templates (task 15)

#### Commands

- [ ] `go test -race ./libs/contract/ -run 'TestTemplateRules|TestTemplatesAccepted|TestLoadTemplatesExecute'` → passes: a missing file, a parse error, a function that is not built in (`env`), `.Uses.nope`, `.Bogus`, `.Provides.api`, a secret field in `Values`, an unguarded optional slot, a consumer side field in `.Clients` and a broken key under own delims are each `invalid_template` at catalog load, and the entry is left out → AC-9, AC-2
- [ ] `go test -race ./libs/render/ -run TestRenderTemplatesGolden` → the golden matches, a second render gives a byte identical zip, and a render without the templates fails with `ErrNoTemplate` → AC-9
- [ ] `go test -race ./libs/planner/ -run TestPaperPlatform` and `go test -race ./catalog/` → the paper platform's templates and the built in catalog still load → AC-2, AC-16
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
