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
