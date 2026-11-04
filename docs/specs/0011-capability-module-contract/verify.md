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
- [ ] `notifier` joins `egress` and `internal`, each with `notifier.bindings.internal` and `${MP_NOTIFIER__PUBLIC_NAME:-notify.example.test}`; `app` joins only `internal` → AC-12
- [ ] `notifier` has `user: "0:0"`, `cap_add: [NET_BIND_SERVICE]`, `tmpfs: [/tmp]` and no `read_only`; `app` has no `user`; every other service keeps `read_only: true`, `cap_drop: [ALL]`, `no-new-privileges` and `${MP_UID}:${MP_GID}` → AC-12
- [ ] `APP_DB_DSN` is `postgres://billing@db.example.test:5432/billing?sslmode=verify-full`, `APP_RUN_AS` is `${MP_UID}:${MP_GID}`, and `APP_PRICE` is `5$$ a month` (a literal `$`) → value sourcing: `environment` placeholders
- [ ] `shared_provision_app_audit` joins `egress` and `internal`, has no `depends_on`, and reads `secrets/shared/admin_admin_password`, which `setup.sh` lists as a file you place → AC-15, value sourcing: external field values
- [ ] In `golden-managed/`, the job of the external provisioner has the `job: provision` and `instance: shared` labels and no `module` label; `managed.json` lists the two placed files as `placed` → AC-11, AC-15

### Waiting on task 10 (the revoke job)

Run these once the spec says how `up` waits for a job nothing depends on, and the revoke job from branch `feat/capability-module-contract-revoke-job` is merged.

- [ ] `go test -race -count=1 -tags e2e -run TestRevokeEndToEnd ./libs/render/` → passes: after removing `a`, `a_db` cannot sign in while `a_db.notes` still holds 42; after binding `a` again with the same names it signs in and reads 42; after removing the last consumers the revoke job runs with `MP_KEEP: ""` and both logins lose sign in; the hand made role `handmade` keeps sign in throughout → AC-6
- [ ] `go test -race -count=1 -run TestRevokeJob ./libs/render/` → `MP_KEEP` is the sorted logins of every binding to the provide, and the job waits for every provision job of that provide → AC-6
- [ ] Render the sample twice in a row with no change and `up` each time → no role or database added or dropped → AC-5

## Acceptance criteria coverage (milestone 2)

- AC-1: external e2e step 1 · AC-6: waiting on task 10 · AC-8: golden review steps 1 to 3, provision e2e · AC-11: guard step, managed labels · AC-12: golden review steps 4 and 5 · AC-15: external e2e, golden steps 1 and 7 · AC-18: the generic `to_provider` file delivery (golden step 3); `caller_key` itself lands in milestone 4 · AC-2: the contract rule cases
