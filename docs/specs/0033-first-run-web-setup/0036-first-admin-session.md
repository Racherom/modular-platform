# 0036. Phase 2: the first admin

**Date**: 2026-11-20
**Parent**: [0033. First run setup on the web](index.md)

## Summary

Once the platform runs, every page leads to its setup page until an admin exists. The setup cookie from phase 1 is already on the domain, so the page knows you. It waits until the identity module is ready, shows that module's enroll questions, and then asks you to sign in once as a test. That first sign in, made with a valid setup session, binds you as the admin, turns the setup cookie into a normal session, and retires the setup. If the session is lost, a new code made by the keeper gets you back in.

## Requirements

**User stories**:
- As the first admin, I want to register and sign in on the platform's own setup page, so that I become the admin without a terminal.
- As the first admin, I want a way back in when my setup session is lost, so that I need not reinstall.
- As an operator, I want no one but the code holder able to become the first admin, so that an open setup is not a takeover.

**Acceptance criteria**:
- **AC-30**: While no admin is bound, every `api` route except `GET /api/v1/setup`, `POST /api/v1/setup/code`, `POST /api/v1/setup/enroll`, `GET /api/v1/auth/login`, `GET /api/v1/auth/callback` and health answers 403 `setup_pending`; `GET /api/v1/auth/login` without a valid setup session answers 403 `setup_pending` and redirects nowhere.
- **AC-31**: With the cookie from phase 1's probe, opening `<origin><base>/` leads to `<base>/setup`. While the identity provider is not ready the page says so and checks again every 2 seconds; then it shows the enroll questions of `GET /api/v1/setup` (Authelia: user name, email, password, the password as a hidden field), or only "Sign in to finish the setup" when the identity module has no `enroll`.
- **AC-32**: Submitting the enroll form calls `POST /api/v1/setup/enroll`; on success the page shows "Sign in to finish the setup"; a rule refusal marks each field with its rule; `enroll_failed` shows "Registration failed; check your answers and try again"; enrolling again before the first sign in replaces the user.
- **AC-33**: "Sign in" goes through the identity module; the callback, while no admin is bound and with a valid setup session, binds that identity (issuer, `sub`, the email when the claims carry one, else none), creates a normal session, deletes the setup cookie in the same response, and lands on `<base>/`. You then deploy the sample as today.
- **AC-34**: Right after the bind, `api` calls the keeper's `done`; `.setup-done` exists and the holder's setup files are gone. Then `GET /api/v1/setup` answers `done`, the setup POSTs answer 409 `setup_done`, and a keeper restart makes no code. A failed `done` call is retried (1 second doubling up to 5 minutes, and at every `api` start while an admin is bound and the code file exists) until it answers 204.
- **AC-35**: A failed sign in (wrong password, provider error, state mismatch) binds nothing and keeps the setup session valid; the setup page shows the refusal reason.
- **AC-36**: Recovery: with no admin bound, `sudo rm` of the code file and `docker restart` of the keeper make a new code and key; every older setup cookie stops being a session. `<base>/setup` then offers the code field; the new code is accepted once (a new 24 hour session), any code a second time answers 401 `code_refused`, and more than 10 tries a minute from one address answer 429 (no total limit). With no admin bound and no code file (for example after `api admin reset` while `.setup-done` exists), `GET /api/v1/setup` answers `reason: no_code`, and the page says to remove `<state>/.setup-done` and restart the keeper.
- **AC-37**: A setup cookie with a wrong MAC, an expiry in the past, a key other than the current one, or a malformed value is no session: login answers 403 `setup_pending`, enroll 401 `no_session`.
- **AC-38**: `admin_binding` accepts an identity without email (migration `00003`); two browsers with valid setup sessions finishing a sign in at once bind exactly one admin, and the other is refused `not_admin` (0006's atomic bind).
- **AC-39**: No setup code, session value, key or enroll answer appears in an `api` log line, in an `api` response body, in any container's env, or in the editor's storage (the cookie is `HttpOnly`); the editor never keeps an answer after the request.

## Decision

The api's sign in callback binds the first admin at the first successful sign in that carries a valid setup session, upgrades that session, and retires the setup at once; phase 2 lives in `api` and the editor.

Short rationale: your rule of 2026-11-20 (the code gives a session; once the identity module is ready you register there; the first successful test sign in makes you the admin and finishes the setup). The api already owns the admin binding and its atomic bind; binding by session, not by email, works for a module whose users carry no email. Runner up: bind by the admin email as today (breaks for a module without email).

## Feature design

### api changes (`services/api/internal/`)

- **`setup` package** (new, application service): the pending check, the code redemption, the enroll pass through, the done retry. It holds no domain logic beyond these rules; the session format is `libs/setupsession`.
- **Pending**: true while `store.AdminBinding` finds no row. Checked per gated request until it is false, then kept false in memory for the life of the process (an admin is never unbound while `api` runs; `api admin reset` is a separate process, after which you restart `api`).
- **Gate**: a middleware in front of the contract's handlers applies AC-30, answering the contract's `Problem` with code `setup_pending`.
- **Sign in** (`signin.Service`): `Login` refuses while pending without a valid setup session (AC-30). `admit` changes: while no admin is bound, a valid setup session is required (else `setup_pending`), the email rule goes, and `BindAdmin` takes the email as optional. On a bind, the callback also deletes the setup cookie and starts the `done` call in the background. `Config.AdminEmail` and `readSettings`' admin email check go away.
- **Session reading**: the setup cookie by spec 0001's name rule (`__Host-mp_setup` or `__Secure-mp_setup` under a base path), verified with `libs/setupsession` against the key file read at each check.
- **Setup files** through the SDK: each `lifetime: setup` secret of an entry gets a generated getter that reads its file at every call and reports a missing file as absent (the files change while the module runs). `sdkgen` and `sdk/` gain this; only `sdk/` reads the files (rule of `AGENTS.md`). The render sets `MP_SETUP_DIR=/run/secrets/setup` for the folder.
- **Done retry**: after a bind, and at start when an admin is bound and the code getter finds a file, call `POST /v1/setup/done` with 1 second doubling to 5 minutes until 204.
- **Rate limits**: `POST /api/v1/setup/code`, 10 a minute per client address (the last `X-Forwarded-For` hop, which the ingress adds), no total limit, `golang.org/x/time/rate`.
- **Enroll pass through**: forwards the setup cookie's value in `X-Setup-Session`, so the keeper checks the session itself (0035).
- **Provider ready**: `signin.Provider.Ready()`, which is true once OIDC discovery against the issuer has succeeded (as today; it implies the issuer answers over TLS that `api` trusts).

### api contract additions (`contracts/openapi/api.yaml`)

| Endpoint | Method | Key inputs | Key outputs | Auth | Key errors |
|---|---|---|---|---|---|
| `/api/v1/setup` | GET | none | `state` (`pending`, `done`); when pending: `reason` (`no_code` when no code file exists, else absent), `session` (bool), `providerReady` (bool), `enroll` (`asks`) or null, the last two only with a session | public | none |
| `/api/v1/setup/code` | POST | `code` (string, req) | 204, sets the setup cookie | public | 401 `code_refused`, 409 `setup_done`, 429 `too_many_tries` |
| `/api/v1/setup/enroll` | POST | `answers` (object, req) | 204 | setup cookie | 401 `no_session`, 409 `setup_done`, 422 `invalid_answer`, 502 `enroll_failed`, 503 `busy` |
| `/api/v1/auth/login` | GET | `return_to` (opt) | 302 to the provider | setup cookie while pending | 403 `setup_pending`, 503 `provider_unavailable` |
| `/api/v1/auth/callback` | GET | `code`, `state` | 302 to `<base>/`; sets the session cookie, deletes the setup cookie on a bind | login cookie | 302 `?signed_out=setup_pending` |

`POST /api/v1/setup/code`: compare the SHA 256 of `code` with the code file in constant time; on a match call the keeper's `code-used`; its 204 mints the cookie, its 409 (`code_used`, `code_changed`) becomes 401 `code_refused`. Every POST also needs a same origin request (`Sec-Fetch-Site: same-origin`, as the editor's other writes).

### Database

Migration `services/api/internal/store/migrations/00003_admin_email_optional.sql`: `ALTER TABLE admin_binding ALTER COLUMN email DROP NOT NULL;` (down: set `''` where null, then `SET NOT NULL`). `BindAdmin` takes `email` as nullable; `GET /api/v1/session` returns `email` as optional, and the editor shows the user's `sub` when it is absent.

### Editor (`web/`)

- Route `<base>/setup` (TanStack Router, file route; `routeTree.gen.ts` regenerated). At start the app calls `GET /api/v1/setup`; `pending` sends every route to `/setup`, and `done` sends `/setup` home.
- States of the page, each with a heading and one action: no session → the code field (recovery); provider not ready → "Waiting for the identity provider" with a 2 second poll; enroll present → the form from `asks` (`secret` as a password field, `pattern` and `min_length` as hints and client checks, the server deciding); enrolled or no enroll → "Sign in to finish the setup"; a `signed_out` reason → that reason above the form.
- Answers live only in the form's state and are cleared after each request; nothing goes into storage.
- Built on spec 0010's design system; labels on every field, errors linked with `aria-describedby`, focus moves to the first error.

### Value sourcing

| Action | Value | Source |
|---|---|---|
| gate, `GET /setup` | pending | `admin_binding` has no row |
| `GET /setup` | `session` | the setup cookie, verified with the key file |
| `GET /setup` | `providerReady` | `signin.Provider.Ready()` (OIDC discovery succeeded) |
| `GET /setup` | `reason` | pending and the code getter finds no file |
| `GET /setup` | `enroll.asks` | the keeper's `GET /v1/setup` |
| redeem | the code | the code getter (`/run/secrets/setup/code`) |
| redeem | code used | the keeper's `code-used` answer |
| cookie | expiry | redemption time plus 24 hours (`libs/setupsession`) |
| cookie | name and path | `MP_BASE_PATH` (the `public` binding's `base_path`) by spec 0001's rule |
| enroll | answers | the request body, passed to the keeper unchanged |
| bind | issuer, `sub`, email | the provider's claims (email may be empty) |
| bind | allowed | a valid setup session and no admin bound |
| done | when | the bind; at start, admin bound and a code file present |
| login redirect | `return_to` | `<base>/`, as today |
| rate limit | client address | the last `X-Forwarded-For` hop, which the ingress sets |

### Key invariants

- At most one admin is ever bound (0006's singleton row), and only by a sign in that carries a valid setup session.
- A code redeemed once is never redeemed again; the keeper's `code-used` is the single record.
- While no admin is bound, nothing but the setup and sign in routes answers.
- Setup files are read fresh at each use; a deleted key ends every session at once.

### Security model

- Public: `GET /api/v1/setup` (shows only `state` without a session), `POST /api/v1/setup/code` (rate limited). Setup cookie: enroll and login while pending. Everything else: an admin session, as today.
- The setup cookie is `HttpOnly`, `Secure`, `SameSite=Lax` (it must reach the sign in callback); it is deleted at the bind. A stolen cookie is a bearer token for at most 24 hours; getting one needs the code, which needs root on the host.
- `api admin reset` (spec 0026) unbinds the admin; to set up again, `sudo rm <state>/.setup-done` and restart the keeper, which makes a new code. The README says so.

### Configuration required

- `MP_SETUP_DIR`: the folder of the `lifetime: setup` files inside `api`, set by the render (not a value you set).
- Removed: `MP_ADMIN_EMAIL` (the `oidc` field `admin_email`).

### Observability

| Message | Level | Fields |
|---|---|---|
| `setup code accepted` | info | `remote` |
| `setup code refused` | info | `remote`, `reason` (`wrong`, `used`, `rate`) |
| `setup enrolled` | info | none |
| `admin bound` | info | `iss`, `sub` (as today) |
| `setup done` | info | `attempt` |
| `setup done failed` | error | `attempt`, `retryIn`, `error` |

### Critical test scenarios

- Happy path (e2e, Chromium, part of 0033 AC-2): from phase 1's last page to `<base>/setup`, wait, enroll, sign in, editor home, deploy the sample, verifies **AC-31**, **AC-32**, **AC-33**
- Gate: every other route while pending → 403; login without a cookie → 403, verifies **AC-30**
- Done: the keeper answers 500 twice, then 204 → retried with 1 and 2 seconds (fake clock); after it, `GET /setup` is `done` and a keeper restart makes no code, verifies **AC-34**
- Failed sign in: wrong Authelia password → no bind, cookie still valid, reason shown, verifies **AC-35**
- Recovery: delete the code, restart the keeper → the old cookie is no session; new code once → cookie; again → 401; 11 tries in a minute from one address → 429; after `api admin reset` with `.setup-done` present → `reason: no_code`, verifies **AC-36**
- Forged cookies: wrong MAC, past expiry, old key, garbage → no session, verifies **AC-37**
- No email: a fake provider whose claims carry no email binds with `email` null; two concurrent callbacks → one admin (repeat the existing race test), verifies **AC-38**
- Leaks: grep `api` logs and responses for the code, the cookie and the password; check `localStorage` and `sessionStorage` are empty, verifies **AC-39**

## Build plan

1. Migration `00003`, `BindAdmin` with an optional email, the session API's optional email; store integration test, satisfies **AC-38**
2. `sdk` and `sdkgen`: the `lifetime: setup` getter (fresh read, absent when missing); `api`'s `setup` package, the gate, the redemption, the enroll pass through, the done retry, the sign in changes, with `httptest` and a fake provider and fake keeper; `contracts/openapi/api.yaml`, `task generate`. The wiring into `Setup` waits for the cut over (the new getters come from the new entry), satisfies **AC-30**, **AC-34**, **AC-35**, **AC-36**, **AC-37**, **AC-39**
3. Editor: the `/setup` route and its states, with vitest tests per state and a test that no answer reaches storage; it treats a 404 from `GET /api/v1/setup` (milestone 4's api) as `done`, so it can land early, satisfies **AC-31**, **AC-32**, **AC-39**
4. Cut over (0016 task 4): the `api` entry (the two `lifetime: setup` secrets, `uses.setup`, no `admin_email`), the wiring in `Setup`, the Chromium smoke steps, satisfies **AC-33**, **AC-34**
