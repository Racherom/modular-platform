# Verify: group B (the setup session) · spec 0037 build plan, from specs 0034, 0035, 0036 · updated 2026-11-21
_Steps derived from the acceptance criteria of specs 0034 (AC-17 contract part), 0035 (AC-20) and 0036 (AC-30, AC-34 to AC-39). `/check verify` runs these; `/test` locks the durable ones. The end to end steps wait for the cut over._

## Commands
- [x] `go test -race ./libs/setupsession/` → expiry, wrong key, malformed values, `MintUntil` all pass     → 0034 AC-11 (session format), 0036 AC-37
- [x] `go test -race ./libs/contract/ -run 'Enroll|ModuleRules|SetupLifetime|SDKFieldFixtures'` → every `invalid_enroll` and `invalid_secret` case is refused at its path     → 0035 AC-20, 0034 AC-17
- [x] `go test -race ./sdk/ -run 'SetupFile|SetupDir' ./sdk/cmd/sdkgen/` → a deleted or replaced file shows at the next call; a missing `MP_SETUP_DIR` stops the start     → 0036 task 2
- [x] `go test -race ./services/api/internal/setup/ ./services/api/internal/signin/ ./services/api/internal/api/` → gate, redemption (once, 429 at the 11th try), forged cookies, done retry 1s doubling to 5m, bind with a setup session, failed sign in keeps the cookie     → 0036 AC-30, AC-34, AC-35, AC-36, AC-37
- [x] `go test -tags integration -race ./services/api/internal/store/` → `admin_binding.email` is null for an identity with no email; 8 racing binds leave one admin     → 0036 AC-38
- [x] `grep` the `api` log lines in those tests for the code, the cookie value and an answer → none     → 0036 AC-39

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
