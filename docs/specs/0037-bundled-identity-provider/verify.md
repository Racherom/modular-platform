# Verify: group B (the setup session) · spec 0037 build plan, from specs 0034, 0035, 0036 · updated 2026-11-21
_Steps derived from the acceptance criteria of specs 0034 (AC-17 contract part), 0035 (AC-20) and 0036 (AC-30, AC-34 to AC-39). `/check verify` runs these; `/test` locks the durable ones. The end to end steps wait for the cut over._

## Commands
- [x] `go test -race ./libs/setupsession/` → expiry, wrong key, malformed values, `MintUntil` all pass     → 0034 AC-11 (session format), 0036 AC-37
- [x] `go test -race ./libs/contract/ -run 'Enroll|ModuleRules|SetupLifetime|SDKFieldFixtures'` → every `invalid_enroll` and `invalid_secret` case is refused at its path     → 0035 AC-20, 0034 AC-17
- [x] `go test -race ./sdk/ -run 'SetupFile|SetupDir' ./sdk/cmd/sdkgen/` → a deleted or replaced file shows at the next call; a missing `MP_SETUP_DIR` stops the start     → 0036 task 2
- [x] `go test -race ./services/api/internal/setup/ ./services/api/internal/signin/ ./services/api/internal/api/` → gate, redemption (once, 429 at the 11th try), forged cookies, done retry 1s doubling to 5m, bind with a setup session, failed sign in keeps the cookie     → 0036 AC-30, AC-34, AC-35, AC-36, AC-37
- [x] `go test -tags integration -race ./services/api/internal/store/` → `admin_binding.email` is null for an identity with no email; 8 racing binds leave one admin     → 0036 AC-38
- [x] `grep` the `api` log lines in those tests for the code, the cookie value and an answer → none     → 0036 AC-39

## Commands (task 5b: the oidc capability)
- [ ] `go test -race ./libs/check/ -run TestOIDCCapability` → `oidc` has no `admin_email`, has the `to_provider` field `logout_path`, its `client_id` pattern takes `api_oidc`, and an `api` entry without `logout_path` is refused     → AC-9
- [ ] `go test -race ./libs/render/` → the platform, keeper and managed goldens carry `logout_path`, `MP_LOGOUT_PATH` and `post_logout_redirect_uris`, and no `MP_ADMIN_EMAIL`     → AC-9
- [ ] `go test -race -tags e2e -run TestOIDCClientEndToEnd ./libs/render/` (needs Docker) → the client at Pocket ID has the logout URI `<origin>/`, the redirect URI follows a base path change     → AC-8, AC-9
- [ ] Known gap until task 7b: with the `authelia` graph, `api` no longer reads an admin email, so no sign in binds the admin until the setup is wired in     → AC-9

## UI / manual (after the cut over)
- [ ] Fresh managed stack, `GET <base>/api/v1/catalog` without a session → 403 `setup_pending`     → 0036 AC-30
- [ ] Redeem the code twice from two browsers → the first gets the cookie, the second 401 `code_refused`     → 0036 AC-36
- [ ] Sign in with a provider whose claims carry no email, with the setup cookie → editor home, setup cookie gone, `GET /api/v1/setup` answers `done` once the keeper's done call lands     → 0036 AC-33, AC-34, AC-38

## Value sourcing (0036)
- [ ] `session` in `GET /setup` follows the cookie and the key file: delete `secrets/api/setup/session-key`, ask again → `session: false`     → 0036 AC-37
- [ ] `reason: no_code` when the code file is gone and no admin is bound     → 0036 AC-36
- [ ] Cookie name and path follow the base path: with `/platform` → `__Secure-mp_setup`, `Path=/platform/`     → 0036 (cookie rule of spec 0001)

## Acceptance-criteria coverage
- 0036 AC-30 · AC-34 (api half) · AC-35 · AC-36 (api half) · AC-37 · AC-38 · AC-39 (api half): the steps above
- 0035 AC-20: the contract step; AC-21 to AC-26 wait for the render, keeper and cut over tasks
- 0034 AC-17 (the `lifetime` contract part): the contract step; the render half waits for 0034 task 3
- Not built here: 0036 AC-31 to AC-33 (the editor), the keeper's endpoints, the wiring into `api`'s `Setup`

---

# Verify: contract and fixture · spec 0037 task 2 · updated 2026-11-21
_Steps derived from spec 0037 AC-1, AC-7 and AC-9 for the `pocket-id` keeper fixture. The real entry and the running stack wait for the cut over._

## Commands
- [ ] `go test -race ./libs/render/ -run Keeper` → the keeper render of the fixture graph passes: `pocket-id` provides `oidc` and `site`, no asked value, no step, no shown secret     → AC-1, AC-9
- [ ] `grep -c -E 'ENCRYPTION_KEY|STATIC_API_KEY|password' libs/render/testdata/platform-keeper/docker-compose.yml` and read each hit → only `*_FILE` paths, no key value, no password     → AC-7
- [ ] `grep -n 'pocket-id' libs/render/testdata/platform-keeper/docker-compose.yml` → `APP_URL: https://auth.${MP_INGRESS__DOMAIN}`, `ALLOW_USER_SIGNUPS: ${MP_POCKET_ID__SIGNUP:-disabled}`, the image tag `v2.19.3`     → AC-1, AC-6
- [ ] `grep -n 'gate' libs/render/testdata/platform-keeper/config/ingress/Caddyfile` → the auth host guards `/setup /signup/setup /api/signup/setup` through `api`, and answers 403 when `api` is down     → AC-2, AC-9
- [ ] Change the fixture graph's `pocket-id` back to `authelia` (real entry) and run `go test ./libs/render/ -run PlatformKeeperRefusals` → the platform flavor still refuses its asked step with `setup_required`, nothing names one identity module     → AC-9

## Value sourcing
- [ ] Issuer of `pocket-id` follows the binding `host`: set `host: id.example.com` on its `public` binding, render → `APP_URL` and the issuer use `id.example.com`     → AC-1
- [ ] The encryption and static API keys are only mounted into `pocket-id` (the real stack's `api` container has no such mount): `docker compose exec api ls /run/secrets` after the cut over     → AC-7

## Acceptance-criteria coverage
- AC-1 (render half) · AC-7 (render half) · AC-9 (the graph renders with the bundled entry): the steps above
- Waits for the cut over: AC-1 and AC-7 on a running stack, AC-10 healthcheck in Compose

---

# Verify: api identity link, handoff and gate · spec 0037 task 4 · updated 2026-11-21
_Steps derived from spec 0037 AC-2, AC-4 and AC-5 for the api half. The ingress route to `identity-enter`, the `Identity` adapter and the end to end run wait for the cut over; the client step (AC-8) waits for a spec amendment._

## Commands
- [ ] `go test -race ./libs/setupsession/ -run Handoff` → a wrong host, wrong key, expired, tampered, empty nonce or a session value as token is refused     → AC-4
- [ ] `go test -race ./services/api/internal/setup/ -run 'Identity|Enter|Gate'` → one use only, 60 second expiry, retired session (key gone or replaced), admin bound, no token or cookie value in the log     → AC-4, AC-5
- [ ] `go test -race ./services/api/internal/api/ -run Identity` → link needs the origin and a session (403 `setup_pending`), 409 after the bind, 404 without `first_admin`, `Referrer-Policy: no-referrer`, host only `__Host-mp_gate` cookie     → AC-4
- [ ] `task lint` and `task generate` leave no diff     → contract first

## UI / manual (after the cut over)
- [ ] With no cookie, `curl -k https://auth.<domain>/setup` and `/api/signup/setup` → 403; `/login` and `/.well-known/openid-configuration` → 200     → AC-2
- [ ] Stop `api`, repeat the first step with a valid gate cookie → 403 (fail closed)     → AC-2
- [ ] Use the identity link twice → the second answers 403 `handoff_refused`, no cookie     → AC-4
- [ ] After the admin is bound, replay the gate cookie on `/setup` → 403     → AC-5

## Value sourcing
- [ ] Link host and path follow the `pocket-id` binding `host` and `first_admin.path`; with base path `/platform` the link carries `/platform/api/v1/setup/identity-enter`     → AC-4
- [ ] Gate cookie expiry equals the setup session's expiry (`MaxAge` within seconds of the 24 hour end)     → AC-4

## Acceptance-criteria coverage
- AC-2 (api half) · AC-4 · AC-5 (api half): the steps above. AC-8: not built (see the open decision).

---

# Verify: client step tests · spec 0037 task 5d · updated 2026-11-21
_Steps derived from spec 0037 AC-7, AC-8, AC-12 and AC-13 for the managed flavor and the Docker test. The job image itself is verified in spec 0030._

## Commands
- [ ] `go test -race ./libs/render/ -run 'TestRenderManagedGolden|TestGuardAcceptsEveryRender'` → the `pocket-id-managed` graph renders in the managed flavor, the guard accepts it, and `app` waits on its provision job with `service_completed_successfully`     → AC-8
- [ ] `go test -race -tags e2e ./libs/render/ -run TestOIDCClientEndToEnd` (needs Docker; about a minute) → passes: fresh database with no admin ends with the client job exited 0 and the consumer running; three `up` runs leave one secret; a base path change repoints the redirect URI; a client made by hand stays through a revoke; a provision job for a client id taken by hand fails and names it; an unbound consumer's client is deleted; a second fresh database works     → AC-8, AC-12, AC-13
- [ ] `grep -n 'tmpfs' -A1 libs/render/testdata/pocket-id-managed/golden/docker-compose.yml` → each provision and revoke job has `/tmp` as tmpfs, and the root stays `read_only: true`     → AC-8
- [ ] `grep -c 'static-api-key\|api_key' libs/render/testdata/pocket-id-managed/golden/docker-compose.yml` and read each hit → only secret paths, no key value     → AC-7

## Value sourcing
- [ ] Redirect URI follows `MP_ADMIN_ORIGIN` + `MP_EDGE__BASE_PATH` + the consumer's `redirect_path`: set `MP_EDGE__BASE_PATH=/platform` in `.env`, `up` → `https://pocket.example.test/platform/callback`     → AC-8
- [ ] Client id follows the binding's `client_id` (`app_oidc` in the fixture); with it set to an id taken by hand the job fails with that id in its log     → AC-12

## Acceptance-criteria coverage
- AC-7 · AC-8 · AC-12 · AC-13: the steps above. Not covered here: the TLS connection from Pocket ID to Postgres (the Docker test turns it off; the spike in rationale.md covers it) and the passkey sign in (task 7).

---

# Verify: setup state and setup page · spec 0037 tasks 4 and 6 · updated 2026-11-21
_Steps derived from spec 0037 AC-3 for the `firstAdmin` field and the editor's `/setup` route (which spec 0036 AC-31 and AC-32 also cover). The passkey sign in waits for the cut over (task 7)._

## Commands
- [x] `go test -race ./services/api/internal/setup/ ./services/api/internal/api/ -run 'TestStateFirstAdmin|TestSetupStateFirstAdmin'` → `firstAdmin` is `true` only with a session and a `first_admin` block, absent otherwise (never `false`), and a failed lookup is a 500 problem     → AC-3
- [x] `task generate` then `git diff --exit-code` → no diff left in `api.gen.go` or `api.gen.ts`     → AC-3
- [x] `pnpm --dir web exec vitest run src/test/setup-page.test.tsx src/test/setup-gate.test.tsx` → every state of the page, the enroll form wins over `firstAdmin`, a refusal marks its field, the answers are cleared after each request and storage stays empty, pending sends every page to `/setup` and done sends it home     → AC-3

## UI / manual (after the cut over)
- [ ] Fresh managed stack, open `<origin>/`, → lands on `/setup`; enter the code → "Waiting for the identity provider", then "Create your admin account" and "Sign in to finish the setup" on one page     → AC-3
- [ ] Press "Create your admin account" → the browser goes to `auth.<domain>` with a `t` token; the setup call of the provider answers 200 now and 403 without the cookie     → AC-3, AC-4
- [ ] Reload `/setup` with the setup cookie gone → the code field, not an error     → AC-3
- [ ] `GET /api/v1/setup` with the setup cookie while the identity module is stopped → the page keeps its last state or shows the generic failure with Retry; it never hides the button by guessing     → AC-3

## Value sourcing
- [ ] `firstAdmin` follows the identity module's catalog entry: swap in a graph with `authelia` (an `enroll` block, no `first_admin`) → the enroll form shows, no create button     → AC-3, AC-9
- [ ] The sign in link goes to `<base>/api/v1/auth/login?return_to=<base>/`; with base path `/platform` the return path carries it     → AC-3

## Acceptance-criteria coverage
- AC-3 (state and page halves): the steps above. The passkey flow and the bind are the managed smoke of task 7.

---

# Verify: setup files and render · spec 0037 task 7a · updated 2026-11-21
_Steps derived from spec 0037 AC-14, AC-16 and AC-18. The adapter that reads and writes the folder (AC-15) and the smoke (AC-17) come with 7b and 7d._

## Commands
- [x] `go test -race ./libs/render/ -run 'TestSetupFiles|TestRenderPlatformGolden|TestRenderPlatformKeeperGolden'` → no Compose secret for `code` or `session-key`; one read write bind `./secrets/api/setup:/run/secrets/setup`; `MP_SETUP_DIR`; the managed flavor refuses `api` with `setup_required`     → AC-14
- [x] `go test -race ./libs/setupsession/ ./services/api/internal/setup/ -run 'Gate'` → a gate value is no session, a session value is no gate cookie, and the gate cookie sent as `__Host-mp_setup` is `ErrNoSession`     → AC-18
- [x] `docker run caddy:2.11.7-alpine caddy validate` on `libs/render/testdata/platform-keeper/config/ingress/Caddyfile` → valid; `caddy adapt` lists the `GET` handoff route first, then the gate prefixes, then the catch all     → AC-16, AC-18
- [ ] `go test -race -tags e2e ./libs/render/` → the platform render still starts under Docker     → AC-14

## UI / manual
- [ ] In an empty install folder run the rendered `setup.sh` twice → the first run prints "Setup code (shown once)" and the address; the second prints "kept" and only the path of the code     → AC-14
- [ ] `stat -c '%a' secrets/api/setup secrets/api/setup/*` → `700` for the folder, `600` for the files; the code is 24 characters of `A-Za-z0-9`, the key 64 hex characters     → AC-14
- [ ] `touch secrets/api/setup/code-used`, delete `secrets/api/setup/code`, run `setup.sh` → a new code, and `code-used` is gone     → AC-14
- [ ] `touch secrets/api/setup/.setup-done` and run `setup.sh` → it makes and prints nothing; change `MP_UID` in `.env` to another id → it stops with a message     → AC-14
- [ ] `grep -r` for the printed code and the key in `docker-compose.yml`, `.env`, `config/` and `docker compose config` → no hit     → AC-14
- [ ] Gate paths on a stack with `pocket-id`: `curl -k https://auth.<domain>/setup/`, `/setup/__data.json`, `/api/signup/setup` with no cookie → 403 each     → AC-18

## Value sourcing
- [ ] `MP_OIDC_FIRST_ADMIN_PATH` in `api`'s environment is `/setup` with `pocket-id` and absent with `authelia` (render both)     → AC-16
- [ ] With `MP_INGRESS__BASE_PATH=/platform` the handoff route in the Caddyfile is `/platform/api/v1/setup/identity-enter`, and `GET` on any other `api` path of the auth host goes to the provider     → AC-16
- [ ] The printed address is the `origin` answer in `.env` plus the base path     → AC-14

## Acceptance-criteria coverage
- AC-14 · AC-16 · AC-18: the steps above. AC-15 (the file adapter), AC-17 (the smoke) and the live handoff are 7b and 7d.
