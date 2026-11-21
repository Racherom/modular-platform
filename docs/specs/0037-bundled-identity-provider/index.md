# 0037. The bundled identity provider: Pocket ID, behind a spike

**Date**: 2026-11-20
**Status**: In Progress
**Amends**: the *Identity provider* row of [0001](../0001-platform-stack-architecture/index.md); the `authelia` rows of [0015](../0011-capability-module-contract/0015-platform-graph-managed-zip.md) and [0026](../0001-platform-stack-architecture/0026-identity-provider-own-host.md) (the auth host stays, the module behind it changes); [0033](../0033-first-run-web-setup/index.md) AC-2 and AC-4 (the smoke enrolls with the bundled module, and the "module without an enroll block" path now runs end to end here); [0036](../0033-first-run-web-setup/0036-first-admin-session.md) AC-31 (the page copy for that path); the `http-ingress` capability of [0012](../0011-capability-module-contract/0012-capabilities-versions-matching.md) and [0013](../0011-capability-module-contract/0013-module-manifest-v1.md) (a gated route); install steps of [0016](../0016-platform-one-container/index.md) (an optional `network`); [0014](../0011-capability-module-contract/0014-provisioners-jobs.md) (a provisioner may yield `oidc`, the job gets the consumer's `to_provider` values, and its revoke script may delete instead of disable; see the client step).
**Part of**: scope #37 (web setup), whose Authelia enroll task stays for Authelia only.

## Summary

The platform's bundled sign in service changes from Authelia to Pocket ID. Authelia keeps its users in a file you must edit, which is why the web setup needed a special `enroll` step. With Pocket ID the admin creates their own account on Pocket ID's first admin page and registers a passkey (a key stored on your device, no password); the platform only guards that page, so nobody else can claim it. The guard reuses the setup session you already have from the web setup. Because the research could not confirm every Pocket ID detail, the first build task is a spike with pass or fail gates, and a failed hard gate stops the build and returns to `/architect`. Authelia stays in the catalog as an optional module. The `api` client is registered at Pocket ID by a provision job of a new `oidc-provisioner` capability (the mechanism that already makes a database and login per consumer): it runs on every `up`, waits for Pocket ID, and `api` waits for it. Until #32's keeper exists, `setup.sh` makes the setup code and key and prints the code, and `api` records the used code in a file (the interim setup holder, task 7), so the managed smoke runs the real first admin path.

## Requirements

**User stories**:
- As an operator, I want to create the first admin on the identity provider's own page, so that the platform never handles a user name, an email or a password.
- As an operator, I want that page closed to everyone but me until I have made the account, so that a stranger cannot claim the admin account of my provider.
- As an operator, I want new users to need an invitation by default and to open sign up only by changing the graph, so that strangers cannot make accounts.
- As an operator, I want to replace the bundled provider by another module that provides `oidc`, so that roles and policies can live in a module I choose.

**Acceptance criteria**:
- **AC-1**: A fresh managed render starts `pocket-id` as the provider of `oidc` on the auth host `auth.<domain>` (spec 0026 unchanged). Its discovery document names an issuer equal to `api`'s `MP_OIDC_ISSUER` exactly, and `api` turns ready with the same CA file as today under `tls-mode: internal`.
- **AC-2**: While no admin is bound, the ingress answers 403 for Pocket ID's first admin page and its setup calls (the paths G1 records) unless the request carries the auth host cookie of AC-4; every other Pocket ID path (sign in, discovery, token) stays open. If `api` is down or answers anything but "valid", the gate answers 403, never lets the request through.
- **AC-3**: `GET /api/v1/setup` carries `firstAdmin: true` (only with a valid setup session, only while pending) when the identity module has a `first_admin` block, and the identity module has no `enroll` block, so the setup page, once the provider is ready, shows "Create your admin account" with a button and then "Sign in to finish the setup". In Chromium with a virtual passkey device, the admin creates the account on Pocket ID's page, registers a passkey, returns to the setup page, signs in, and is bound as the admin by the rule of spec 0036 (the email is optional there). This runs in the managed smoke.
- **AC-4**: The button calls `POST /api/v1/setup/identity-link` (setup cookie required) and gets a link to the auth host with a handoff token: signed with the setup session key (`libs/setupsession`), single use, valid 60 seconds. The auth host trades it for a cookie that is host only (no `Domain`), `HttpOnly`, `Secure`, with the same expiry as the setup session. A wrong MAC, an expired, reused or wrong host token, or a retired session gets 403 and no cookie.
- **AC-5**: After the admin is bound (or the setup is done), the gate cookie stops being valid, and Pocket ID's setup route stays closed (it closes itself once its own admin exists, G1). The README says how to recover a lost passkey (G7 records the provider's command).
- **AC-6**: With the default `signup: disabled` no account can be made without an invitation. Setting `signup` to another listed value in the graph changes only that behavior; any other value is refused with a problem at `modules.<i>.config.signup` (`invalid_config`).
- **AC-7**: Pocket ID's encryption key and static API key are generated secret files (mode 0600), never an environment value in the rendered Compose file, never in a log line, never in any `api` or renderer response. The `api` container cannot read the static API key; only Pocket ID and its client jobs mount it.
- **AC-8**: For each binding to the `oidc` provide of `pocket-id`, a provision job of the `oidc-provisioner` capability runs on every `up`. It waits for Pocket ID healthy, reaches it by its service name on the internal network (not through the ingress), and upserts the client by client id with the generated client secret one redirect URI (the public origin, the base path and the consumer's `redirect_path`) and one logout URI (the same origin and base path plus the consumer's `logout_path`). `api` builds both of its own URIs from the same `redirect_path` and `logout_path` values, so the two sides cannot drift. The consumer (`api`) has `depends_on` the job with `service_completed_successfully`. Running `up` many times leaves the client with exactly one secret. Changing the base path and rendering again repoints the redirect URI with no manual step.
- **AC-9**: The contract and the checks accept (a) an optional `gate` on an `http-ingress` route (a request the ingress first asks `api` about), (b) an optional `network` on an install step, (c) a `first_admin` block on an `oidc` provider or on a provide of a provisioner that yields `oidc` (the provider's own first admin page and the paths to gate), and (d) a render rule, with no new contract source: a provision job gets every text `to_provider` field of the consumer's slot as `MP_<F>` (a file field is `ErrNotRendered`). The `oidc` capability drops its dead `admin_email` field (specs 0035 and 0036 already removed it) and gives `client_id` a `pattern`, so it can be a named field. A platform graph with `authelia` in place of `pocket-id` still renders and passes the checks, so nothing in the setup names one identity module.
- **AC-10**: The module's healthcheck passes in Compose, the spike records the idle memory use, and the rendered README says: the sign in uses passkeys, the auth host needs its own DNS name, where the static API key lives and why, and how to recover a lost passkey.
- **AC-11**: The spike's results (each gate below, the exact calls and values, the pinned image tag) are in [rationale.md](rationale.md) before any task after it starts, and a failed hard gate stops the build.
- **AC-12**: A revoke job of `oidc-provisioner` runs on every `up` after the provision jobs and deletes the clients that carry the stack's marker and whose client id no current binding names. It never changes a client without the marker. A provision job whose client id already exists without the marker fails and names the client ("client <id> exists and was not made by the platform; delete it in Pocket ID or name another client id in the graph"). A job script never prints a response body, and the revoke job deletes nothing when any page of the client list failed.
- **AC-13**: On a fresh database, before any admin exists, the provision job succeeds, so `api` starts and the setup page can open. (G7 proves the static API key works with no admin; a failure stops the build.)
- **AC-14** (before the keeper of #32 exists): `api`'s catalog entry declares `code` (the `password` format, `show: once`) and `session-key` (`hex`) as `lifetime: setup` secrets. In every managed render, with or without `WithKeeper()`, the render gives these two no Compose `secrets:` entry; it adds one read write bind `./secrets/api/setup:/run/secrets/setup` to `api` and sets `MP_SETUP_DIR=/run/secrets/setup`. `setup.sh` makes the files in `secrets/api/setup/` (folder 0700, files 0600, owner `MP_UID`) with `noclobber`: the key as 32 random bytes in hex, the code as 24 characters of `A-Za-z0-9` from `/dev/urandom` (about 143 bits, length checked). It deletes `code-used` when it makes a new code. It prints the code once, only when it made it (a kept code prints the path only), with the address to open (the `origin` answer in `.env` plus the base path). It makes nothing when `.setup-done` exists, and it stops with a message when `id -u` differs from `MP_UID`. It asks no admin user name, email or password, because `pocket-id` has no `asks`. No code or key value is in the rendered Compose file or in any environment value. Under `WithKeeper()` the keeper's `setup.json` makes the files instead; the golden of `platform_keeper_test` changes with the mount.
- **AC-15** (before the keeper): `api` runs the setup with a file based `Keeper` adapter, which reads and writes the setup folder through new SDK helpers (create with no replace, remove; only `sdk/` touches the files). `CodeUsed(sha)` first compares `sha` with the hash of the current code (`Files.Code`): a different hash answers `ErrCodeChanged`. Then it writes the hash to a temp file in the folder and `link()`s it to `code-used`, so a crash leaves no empty `code-used`; when `code-used` exists, the answer is `ErrCodeUsed`. `Done` deletes `code`, `session-key` and `code-used`, then writes `.setup-done`; it is safe to call again. A restart of `api` between the redeem and the bind keeps the code refused. `Enroll` answers nil and `RunEnroll` answers `ErrNoEnroll`. When #32's keeper lands, its client replaces this adapter behind the same port.
- **AC-16**: `api` learns the first admin page from the render, not from a constant. The origin is the host of its own `MP_OIDC_ISSUER` (G4: `https://auth.<domain>`). The path comes through a new optional `oidc` field `first_admin_path`, which the provider fills from its `first_admin.path` and `api`'s `uses.oidc.env` maps (an Authelia binding leaves it empty, so the `Identity` adapter answers `ok: false`); no env value is read outside `sdk/`. The `http-ingress` use gets an `enter` field beside `guard`. The auth host site routes exactly `GET <base>/api/v1/setup/identity-enter` to `api`, ahead of Pocket ID's catch all (with an empty base path that path sits inside Pocket ID's `/api`, so the match is exact), and nothing else of `api` is reachable on that host.
- **AC-17**: The managed smoke runs the real first admin path with no password and no minted cookie. A `@setup` test reads the code from the file named by `SMOKE_SETUP_CODE_FILE`, enters it on the setup page, clicks "Create your admin account", creates the account and registers a passkey on Pocket ID's page with a virtual authenticator, goes back with `page.goto` to the setup page (Pocket ID has no return URL, and the setup page says to come back), signs in (clicking the consent screen if it shows), and lands bound as the admin. It saves the browser state to the file in `SMOKE_STATE_FILE`, which every other managed test loads (each `--grep` is its own run, so this file is the shared session; `@after-restart` signs in through it too). Before the code step a `curl` (with `-k` or the stack's CA) of the auth host's `/setup` answers 403; after the bind, `context.request` inside `@setup` sends `POST /api/signup/setup` with the old gate cookie and gets 403. The smoke copies `secrets/api/setup` aside before the bind, so its log leak check can search for the code and key. No admin user name or password is left in the smoke.
- **AC-18**: The gate cookie of AC-4 is signed under its own MAC context, so it is never a setup session when sent to the main host as `__Host-mp_setup` (a test sends one and gets `no_session`). The gate guards by prefix: `/setup*` and `/signup/setup*` and `/api/signup/setup*` (G1 records which variants Pocket ID serves, such as `/setup/` and `/setup/__data.json`), so none passes without the cookie.

## Decision

**Chosen option**: Option 2: Pocket ID as the bundled provider, behind a spike, with Rauthy and then Authelia with lldap as the ordered fallbacks.

Ship `pocket-id` as the default module that provides `oidc` and `site`, keep spec 0026's auth host, let the admin create the first account on Pocket ID's own page behind a gate that reuses the setup session, register the `api` client with a provision job of `oidc-provisioner` (the client step below), and keep Authelia in the catalog as an optional module with its file enroll.

**Implementation skills**: `openapi-spec-generation` (`wshobson/agents`, `.agents/skills/openapi-spec-generation/`) · `golang-security` (`samber/cc-skills-golang`, `.agents/skills/golang-security/`)

### The spike (task 1), its gates, and the stop rule

Run the pinned Pocket ID image by hand in Compose behind Caddy on `auth.platform.localhost`. **Hard gates** (a failure stops the build and returns to `/architect`, which then picks the fallback with you):

| Gate | Proves | Needed for |
|---|---|---|
| G1 | Pocket ID has a first admin page for an empty database; the exact paths of that page and its calls are known; it closes by itself once its admin exists; and it can be reached only through our gate | AC-2, AC-3, AC-5 |
| G2 | Behind a Caddy `forward_auth` gate, the admin can create the account and register a passkey end to end | AC-3, AC-4 |
| G3 | With `STATIC_API_KEY`, an OIDC client can be created and updated headlessly with a client id, a client secret and redirect URIs we choose, and PKCE works; if the secret cannot be chosen, the provider's secret flows to `api` and the `client_secret` direction flips (stated in rationale.md) | AC-1, AC-8 |
| G4 | The discovery issuer is exactly `https://auth.<domain>` (no trailing slash, no path) and `api` can reach it through the ingress alias with our CA | AC-1 |
| G5 | Every env value and call this spec uses works on the pinned tag (the docs we read describe an unreleased version) | all |
| G6 | A virtual passkey device in headless Chromium can register and sign in | AC-3 |

**Soft gates** (record the value; a miss goes to you, it does not stop the build): the healthcheck command in the image; the client step's job image (decided: our own small image, see the client step); whether the encryption key and the static key can come from files, and how the database connection string, which holds a password, is built without a secret in the Compose file; whether `UI_CONFIG_DISABLED=true` is needed for the sign up env to apply (it is then required); the literal env values of each `signup` mode; the claims (`email`, `email_verified`) at sign in; whether a consent screen shows at the first sign in (the smoke must click it); the command that mints a sign in link for a lost passkey; a writable data folder and the container's hardening; idle memory (record it; above 300 MB you decide); whether a sub path works (recorded for a later placement spec, not built here).

### Contract changes (specs 0012, 0013, 0016)

```yaml
provides:
  oidc:
    first_admin:                        # optional; the provider's own first admin page
      path: /setup                      # a path on the provider's host (the value G1 records)
      gate: [/setup, /api/signup/setup] # paths the ingress guards until an admin is bound
uses:
  public:
    capability: http-ingress
    # a route may carry a gate: the ingress asks api before it passes the request on
steps:
  - name: clients
    network: internal                   # optional on a step; default stays none (0016)
```

The renderer fills the ingress route's `gate` from the identity module's `first_admin.gate` and from `api`'s own address; a module with no `first_admin` gets no gated route. The exact field names are fixed in task 3 and the contract, not here.

### The client step: `oidc-provisioner` (task 5, decided 2026-11-21)

Pocket ID has no client config file, so something must register `api` as a client. It reuses the provisioner of spec 0014 (no new `jobs:` block): a provisioner is admin access to a thing that makes one resource per binding, its provision job already runs on every `up` after the provider is healthy, and the consumer already waits for it with `service_completed_successfully`. `pocket-id` provides `oidc-provisioner` under the provide name `oidc`; `api` keeps `uses oidc` unchanged. Authelia keeps providing plain `oidc`, so both still satisfy the same use.

```yaml
# catalog/capabilities/oidc-provisioner.yaml
name: oidc-provisioner
scheme: http                             # the provider's own address on the internal network
fields:
  url: {type: url}                       # the provider's own address on the internal network
  api_key: {type: secret}
  issuer: {type: url}
  origin: {type: url}                    # the public origin
  base_path: {type: string}
  ca: {type: certificate, optional: true}
yields:
  capability: oidc
  named: [client_id]                     # default <consumer>_<slot>, the graph may name it
  login: client_id                       # the revoke job keeps these
  fields:
    issuer: {admin: issuer}
    client_id: named
    client_secret: generate              # the file the consumer gets is the file the job reads
    ca: {admin: ca}
    # redirect_path and logout_path are to_provider fields: the consumer gives them, the job
    # reads them as MP_REDIRECT_PATH and MP_LOGOUT_PATH (render rule, AC-9d)
  job: {image: <pinned, task 5a>, provision: ..., revoke: ...}
# in the pocket-id entry
provides:
  oidc:
    capability: oidc-provisioner
    first_admin: {path: /setup, gate: [/setup, /signup/setup, /api/signup/setup]}
    fields:
      url: {platform: url}                     # http://pocket-id.<graph>.internal:1411 (scheme from the capability, port from the provide)
      api_key: {secret: static-api-key}
      issuer: {uses: public, format: "https://{host}"}
      origin: {uses: public, field: origin}
      base_path: {uses: public, field: base_path}
      ca: {uses: public, field: ca}
```

The exact field names are fixed in task 5b; the shape is not. Changes to `catalog/capabilities/oidc.yaml`, made in 5b: remove `admin_email` (and `MP_ADMIN_EMAIL` from the `api` entry, since the check wants a source for every field the provider gives); give `client_id` a `pattern` that Authelia and Pocket ID both accept; add the `to_provider` field `logout_path`, which `api` sets to its home path. `api`'s own redirect and logout URIs come from `redirect_path` and `logout_path` in its entry, not from constants in Go.

**The job image (decided)**: our own small image (Alpine with `/bin/sh`, `curl`, `jq`), built from the repo like our other images, with release-please and the frozen list of spec 0031. The job runs read only, as `${MP_UID}`, with no egress, and holds the admin key, so a third party image is a supply chain risk, and the provider's busybox `wget` cannot send PUT or DELETE. The wait anchor `mp_jobs` takes the image of the first job nothing waits for, by service name, so this image also runs `sleep infinity` under all four hardening options (the e2e of 0014 proves it).

**Provision** (every `up`, one job per binding, on the `internal` network, `depends_on` `pocket-id` `service_healthy`). The script calls Pocket ID at `$MP_ADMIN_URL` with the header `X-API-KEY` read from `/run/secrets/admin/api_key` (a header file, never a command line value); every JSON body is built with `jq --arg` and `--rawfile`, so no name or secret goes through shell quoting. The job writes only to `/tmp`.
1. `GET /api/oidc/clients/<client_id>`. `404`: `POST /api/oidc/clients` with `id`, `name`, `callbackURLs` (one item: `MP_ADMIN_ORIGIN` + `${MP_ADMIN_BASE_PATH:-}` + `MP_REDIRECT_PATH`, joined with no normalisation), `logoutCallbackURLs` (one item: the same origin and base path + `MP_LOGOUT_PATH`), `pkceEnabled: true`, `isPublic: false`, `skipConsent` as task 5a decides, and `description` set to `mp-marker:<MP_MARKER>` (the marker). The 201 body of a create holds a generated secret in `createdSecret`, which the secret step below deletes. `200` with the marker: `PUT` the same body (a PUT replaces the whole client, so 5a records the full body Pocket ID expects). The script uses `curl -sS`, never `-v`, and prints no response body (the `GET` returns the client's `credentials`). `200` without the marker: fail with the message of AC-12.
2. Secret, an ensure in three moves: read the secret ids now on the client (`credentials.secrets` of the `GET`), `POST /api/oidc/clients/<id>/secrets` with the content of `MP_CLIENT_SECRET_FILE`, then `DELETE` each id read first. A run that stops between the second and third move leaves two secrets with the same value; the next run ends with one.

**Revoke** (every `up`, one job per provide, after every provision job; the wait anchor `mp_jobs` waits for it): list the clients (all pages), and `DELETE` each one that has the marker and whose id is not in `MP_KEEP`. This deletes where the postgres revoke only disables logins (0014 AC-6); a client holds no data, and the contract only fixes `MP_KEEP`, so the script decides. A client without the marker is never read as ours. If any page of the list fails, the job fails and deletes nothing. The marker is the stack's `MP_MARKER`, so renaming the graph orphans its marked clients and a provision job then fails on the old client id; the README says to delete such a client by hand.

**Order and the deadlock to avoid**: `api` waits for its job, the job waits for Pocket ID healthy, and the setup page is served by `api`. So the job must succeed before any admin exists. G7 below proves it; the build stops if it fails.

**Gate G7 (hard, task 5a; passed 2026-11-21, so the build goes on)**: with `STATIC_API_KEY` set and an empty database (no admin yet), `GET` and `POST /api/oidc/clients` and the secret calls work. The spike of G3 ran after the admin existed.

**Soft gates (task 5a, record the value)**: Results of the hand check of 2026-11-21 are in [rationale.md](rationale.md) (*Task 5a results*): the marker lives in the client's `description` as `mp-marker:<MP_MARKER>` (no name prefix, nothing shown to users); the list call pages with `pagination[limit]` and `pagination[page]` and reports `totalPages`; calls to the internal service name over plain HTTP work. Still to record in 5a: that Pocket ID answers the full internal name `http://pocket-id.<graph>.internal:1411`; whether `skipConsent: true` removes the consent screen (the smoke shows it, task 7); the full PUT body with `logoutCallbackURLs`; that `_` is accepted in a client id (the default is `api_oidc`).

**Flavors and order**: `pocket-id` may run in a deployed (managed) graph. The deployer guard accepts any provision or revoke job, so no guard change is expected; the build adds a managed flavor golden and a guard test to prove it. The ingress gate adds no `depends_on` on `api` (`forward_auth` resolves at request time and fails closed), and the chain `api` → job → `pocket-id` → postgres has no cycle; the builder must not add one.

### Before the keeper: the interim setup holder (task 7, decided 2026-11-21)

The setup page, the handoff and the bind need a code, a session key and a keeper that records the code as used and says done. The keeper is #32's thread, which waits for this spec, and the managed zip today still runs `setup.sh` with a password for Authelia. So task 7 needs a stand in that is small, safe and removable. You chose it on 2026-11-21: `setup.sh` makes the files and prints the code, `api` runs a file based adapter, and the smoke types the code.

- **Who makes the files**: `setup.sh`, in its existing stage for generated secrets, for each `lifetime: setup` secret of an instance in a render without the keeper. The code and key are made as AC-14 says: a new `new_password` helper beside `new_secret` (today `setup.sh` can only make hex; `Format` and `Show` are read only by the keeper render). This is one change to the template, not a new `api` command.
- **Where they live**: `secrets/api/setup/` on the host, `/run/secrets/setup` in `api`. It is the one folder `api` may write to under `secrets/`; no other secret of `api` is writable. The render treats the two files as no Compose secret (today it would turn them into read only secrets, which it must not).
- **File adapter** (`services/api/internal/setup`, behind the `Keeper` port, so the service code does not change): the rules of AC-15. It lives in its own file with a doc comment that says it goes away with #32. It never logs the code, a hash or a path's content.
- **Identity adapter** (the `Identity` port task 4 left without one): origin from `MP_OIDC_ISSUER`, path from the `oidc` field `first_admin_path` (AC-16), which the bound provider fills from its `first_admin.path` and which stays empty when there is no block. This keeps "the setup names no identity module" true.
- **Wiring**: `api`'s `main` builds `setup.New` with the SDK's setup getters as `Files`, the file adapter as `Keeper`, the store's admin binding as `Admins`, the OIDC provider's `Ready` and the identity adapter; the three routes of task 4 and the setup gate middleware go live. Until this task none of it ran.
- **Ingress**: the render adds the auth host route for `identity-enter` through the new `enter` field of `http-ingress` (AC-16; task 4 left it out) and the gated routes of the provider's `first_admin.gate`, with `api`'s own `guard` address, as the gate golden of task 3 already shows.
- **Recovery before the keeper**: a lost code or key before any admin exists has no command. Nothing is deployed yet at that point, so the README says to delete the files in `secrets/api/setup/` (never the folder: deleting a bind mounted folder detaches it inside `api` until a restart) and run `setup.sh` again; it makes new ones, and `api` reads them fresh at every call, so no restart is needed. After `api admin reset`, remove `.setup-done` first. After the bind there is nothing to recover: Pocket ID's own lost passkey command (G7 of AC-5) applies. The keeper's recovery of spec 0036 AC-36 comes with #32.

**The smoke** (`scripts/smoke-managed.sh` and `web/` smoke tests): `setup.sh` gets the answers `platform.localhost` and an empty origin only. A new first test (`@setup`) reads the code from the file, drives the page of spec 0036 with the code field, then the create step through Pocket ID with a Chromium virtual authenticator (CDP `WebAuthn.addVirtualAuthenticator`, resident key and user verification on), and ends signed in. It saves the browser state, which `@deploy` and the other managed tests load, so `SMOKE_ADMIN_USER` and `SMOKE_ADMIN_PASSWORD` go. `wait_healthy` keeps its counts (`pocket-id` replaces `authelia`; the two `oidc` jobs match the existing `_(provision|revoke)_` pattern), and the Postgres TLS check names `pocket_id` instead of `authelia` in its role list. The virtual authenticator belongs to the browser context that registered the passkey, so a later fresh context cannot sign in again; the tests reuse the saved state (recorded in task 7c).

### Interface (api, spec 0036's setup routes)

| Endpoint | Method | Key inputs | Key outputs | Auth | Key errors |
|---|---|---|---|---|---|
| `/api/v1/setup/identity-link` | POST | none | `url` (auth host link with a handoff token, 60 s) | setup cookie | 403 `setup_pending` (no session), 409 `setup_done`, 503 `provider_unavailable` |
| `/api/v1/setup/identity-enter` | GET | `t` (handoff token) | 302 to the provider's first admin page; sets the auth host cookie | handoff token | 403 (bad, expired or reused token) |
| `/api/v1/setup/gate` | GET | the cookie, forwarded by the ingress | 204 valid, 403 refused | the cookie | 403 |

**How the setup page learns of the first admin page.** `GET /api/v1/setup` (spec 0036) gets one new optional field, `firstAdmin` (boolean). It is present as `true` only with a valid setup session while the setup is pending, exactly where `providerReady` and `enroll` appear, and absent otherwise (never `false`). It carries no host or path: the page only needs the yes, and `identity-link` returns the real URL. `api` sets it from the identity port (`Identity.FirstAdmin`, which already tells `ok`), so the answer comes from the same catalog entry the handoff uses and never from the keeper. The page picks its step in this order: no session → code field; provider not ready → waiting; `enroll` present → the enroll form (it wins if a module declares both blocks); else `firstAdmin` → "Create your admin account"; else the sign in. If the identity lookup fails, `GET /setup` fails like a failed keeper call does today (a 500 problem); it never answers `firstAdmin` absent on an error, so the page cannot hide the button by guessing.

`identity-enter` is routed on the auth host by the ingress to `api`; the token never reaches Pocket ID. `api` keeps used handoff tokens until they expire. No token, cookie value or link appears in a log line.

### Value sourcing

| Action | Value produced or displayed | Source |
|---|---|---|
| identity link | the link's host and path | the identity module's binding `host` (spec 0026) and `first_admin.path` from its catalog entry |
| identity link | handoff token | signed by `api` with `session-key` (`libs/setupsession`), 60 s, single use |
| gate | the cookie's value and expiry | the setup session's value and expiry, as 0034 mints them |
| gate | which paths are guarded | `first_admin.gate` in the catalog entry, filled into the ingress route |
| gate | whether it still lets requests through | `api`'s answer (valid setup session and no admin bound) |
| first sign in | the admin identity | the provider's `iss` and `sub` from the callback; email only if the claims carry one (0036) |
| client job | the provider's address | the provide's `url` field: Pocket ID's `site` address on the internal network |
| client job | client id | `named` `client_id` of the binding: the graph's value, else the check's default `<consumer>_<slot>`; `api` reads it as `MP_OIDC_CLIENT_ID` |
| client job | client secret | generated per binding file (spec 0013); the job reads `MP_CLIENT_SECRET_FILE`, `api` reads the same file (G3: the secret can be chosen) |
| client job | redirect URI | `MP_ADMIN_ORIGIN` + `${MP_ADMIN_BASE_PATH:-}` (the `public` slot's `origin` and optional `base_path`) + `MP_REDIRECT_PATH` (the consumer's `redirect_path`; `api` builds its own URI from the same value) |
| client job | logout URI | the same origin and base path + `MP_LOGOUT_PATH` (the consumer's `logout_path`, `api`'s home path) |
| client job | the Pocket ID admin credential | secret `static-api-key`, generated at install, mounted only into the provider and its client jobs (`/run/secrets/admin/api_key`) |
| client job | the marker and the keep list | `MP_MARKER` and `MP_KEEP` as spec 0014 renders them |
| render | the auth host and the issuer | spec 0026; the issuer string is the one G4 proves |
| render | the sign up mode | module config `signup`, default `disabled`, mapped to the provider's literal values |
| setup page | whether to show "Create your admin account" | `firstAdmin` from `GET /api/v1/setup` (set from `Identity.FirstAdmin`'s `ok`, a catalog entry with `first_admin`), shown when `enroll` is absent |
| setup page | what happens when the lookup fails | `GET /api/v1/setup` answers a 500 problem; the page shows its generic error and retries |
| setup files | the code and the session key | `setup.sh` makes them (task 7a); `api` reads them through the SDK getters at each call |
| setup files | where `api` finds them | `MP_SETUP_DIR=/run/secrets/setup`, the render's mount of `secrets/api/setup/` |
| code redemption | whether a code was used | `code-used` in the setup folder, made with no replace by the file adapter |
| done | when the files go | `Done` from the bind (spec 0036 AC-34), carried out by the file adapter |
| identity link | the auth host origin | the host of `MP_OIDC_ISSUER` (the provider's issuer, G4) |
| identity link | the first admin page path | the `oidc` field `first_admin_path`, filled from the bound provider's `first_admin.path` |
| identity link | the route to `identity-enter` | the `enter` field of `http-ingress`, filled from `api`'s entry |
| setup.sh | the address it prints | the `origin` answer in `.env` plus the base path |
| file adapter | the folder | `MP_SETUP_DIR`, through the SDK's new create and remove helpers |
| gate | the cookie's MAC context | its own constant in `libs/setupsession`, not the setup session's |
| smoke | the setup code | the file `secrets/api/setup/code` of the install folder, passed as `SMOKE_SETUP_CODE_FILE` |
| smoke | the shared browser session | the file in `SMOKE_STATE_FILE`, saved by `@setup` |
| smoke | the passkey | a Chromium virtual authenticator made in the test's browser context |

### Key invariants

- No platform service names Pocket ID. `oidc` and the `api` admin binding (spec 0006, 0036) stay as they are.
- The platform never sees the admin's email, user name or passkey; the email is optional at the bind.
- The gate fails closed and stops guarding only when the setup is done.
- The bundle models no roles. Read only or no access for some users comes from another module or capability (scope #14, #28, #29).
- `api` never holds the provider's static API key.
- The client jobs only change clients that carry the stack's marker; they never read the provider through the ingress.
- The image tag is exact and frozen (spec 0031).

### Security model

Pocket ID's first admin page is the one risky window: it is open to any visitor until an admin exists, from `up` to the admin's sign up. The ingress gate closes it, and it opens only for a holder of the setup session through a 60 second single use handoff, so a stranger on the public host gets 403. The handoff token and the cookie are bearer tokens; the token goes in a URL once, so the `identity-enter` response sets `Referrer-Policy: no-referrer`. The static API key is a single shared secret with full admin rights at the provider; it lives in `secrets/pocket-id/` (0600), is mounted into the provider and the client step, and stays after setup because the client step runs on every `up`. Treat it like the CA password. Sign up defaults to disabled. No personal data is stored by the platform. No compliance scope.

### Configuration required

- `signup` (module config, default `disabled`): the provider's sign up mode, with the literal values G5 records.
- Secrets `encryption-key` and `static-api-key` (generated, 0600).
- The provider's `DB_CONNECTION_STRING` comes from the provisioned `postgresql` binding (its own database and role); how its password stays out of the Compose file is a soft gate.
- `admin-email` and `MP_ADMIN_EMAIL` stay gone (0035, 0036).

### Critical test scenarios

- Happy path: render the fixture platform with `pocket-id`, install, open the identity link, create the account with a virtual passkey, sign in, deploy as today, verifies **AC-1**, **AC-3**, **AC-4**
- Gate: no cookie, a forged or expired token, a reused token, `api` stopped, all give 403; a valid cookie passes only the guarded paths; after the bind the cookie is refused, verifies **AC-2**, **AC-4**, **AC-5**
- Failure: `signup: bogus` is refused, verifies **AC-6**
- Permission: `api` cannot read `secrets/pocket-id/static-api-key`; the rendered Compose file holds no key value, verifies **AC-7**
- Rerender: a changed base path repoints the client's redirect URI after `up`; three `up` runs leave one secret on the client, verifies **AC-8**
- Revoke: unbind a consumer, `up`, its client is gone; a hand made client without the marker stays, and a provision job for a client id that exists without the marker fails with its name, verifies **AC-12**
- Empty database: a fresh `up` with no admin finishes with the client job exited 0 and `api` ready, verifies **AC-13**
- Render: `api` has `depends_on` the job with `service_completed_successfully`; the job is on `internal` only, mounts `static-api-key`, and `api` does not, verifies **AC-7**, **AC-8**
- Contract: a graph with `authelia` in place of `pocket-id` still renders; a module with no `first_admin` gets no gate, verifies **AC-9**
- Interim files: `setup.sh` run twice keeps the code and key; the rendered Compose file holds no code or key value; only `secrets/api/setup/` is writable in `api`, verifies **AC-14**
- File adapter: a code redeemed, then `api` restarted: the same code is refused; `Done` twice; a stale hash gives `ErrCodeChanged`; the setup files are gone after the bind, verifies **AC-15**
- Identity wiring: with `pocket-id` the link points at `https://auth.<domain>` plus the path; with `authelia` `firstAdmin` is absent and the link answers 409, verifies **AC-16**
- Smoke: the full first admin path of AC-17, with the 403 checks before the code and after the bind, verifies **AC-3**, **AC-17**

## Build plan

Tracer Bullet (the project default): one thread through the fixture first, then thicken. A check on 2026-11-20 found that the code of spec 0026 (the auth host) and spec 0033 (the setup session) does not exist yet: no `host` on a binding, Authelia still routed at `/auth`, no `libs/setupsession`, no `enroll` in the contract, no api setup routes. The gated route, the identity link and the client step have nothing to build on until those land, so they come first, and only the pieces this spec needs.

**Order**: A (the auth host) and B (the setup session) do not depend on each other, so build them in either order. Task 2 onward waits for both.

**A. The auth host, from spec 0026, trimmed to what this spec needs** (the spike of 0026 task 1 is replaced by our own spike, which already ran; the Authelia entry and template of 0026 task 3 leave the critical path):
- A1. Contract and check: `graph: true` fields, the `host` field on a binding, `{field}` tokens, the `host` or `route` rule and its problems (0026 task 2); satisfies 0026 **AC-10**
- A2. Render: token filling, one network alias per distinct host, the Caddy host site and `Caddyfile.tmpl` (0026 task 3, without the `authelia` entry); satisfies 0026 **AC-5**, **AC-6**, **AC-8**

**B. The setup session, from spec 0033, only the pieces the handoff needs:**
- B1. 0034 tasks 1 and 2: `lifetime: setup` and `libs/setupsession` (session key, cookie, the MAC check)
- B2. 0036 tasks 1 and 2: migration `00003` and the api's setup package with the setup routes and the openapi contract
- B3. 0035 task 1: `enroll` in the contract, so `first_admin` and "no `enroll` block" are decided against the real field

Everything else of 0033 (the `setup` command pages, the keeper endpoints, the Authelia enroll) keeps its place in scope #37.

**This spec, in order:**
1. **Spike**: done 2026-11-20, results in [rationale.md](rationale.md); satisfies **AC-11**
2. **Fixture catalog entry** `pocket-id` in the `platform-keeper` fixture (`provides oidc` and `site` with `first_admin`, `uses` ingress and `postgresql`, config `signup`, the two secrets, healthcheck), with its render golden. It replaces the fixture's `authelia` entry, which also removes the stale `admin-password` secret the keeper tests trip on today. Authelia's enroll tests (0035) move to a small second fixture so they keep running; satisfies **AC-1**, **AC-7**. Needs A1, A2.
3. **Contract first** (partly done: `first_admin` and the step `network` exist): add the route `gate` on `http-ingress` in `libs/contract`, the manifest rules and the renderer, then the render that fills the gate fields; `contracts/openapi/api.yaml` for the three routes; `task generate`; satisfies **AC-9**. Needs A1, A2.
4. (built 2026-11-21: the three routes and the handoff token, then `firstAdmin` on `SetupState` with a test per case; not yet wired into `api`'s `Setup`, the `Identity` port has no adapter, and the ingress route to `identity-enter` is not rendered, all three come with task 7) **Api**: `identity-link`, `identity-enter` and `gate` on `libs/setupsession`, with the handoff token and the fail closed rule, and the `firstAdmin` field on `SetupState` (`contracts/openapi/api.yaml`, then `task generate`; the handler fills it beside `enroll`, with a test for session, no session, done, a module without `first_admin`, and a failed lookup); satisfies **AC-2**, **AC-3**, **AC-4**, **AC-5**. Needs B1, B2, and task 3.
5. **Client step** (the `oidc-provisioner` above), in four parts:
   - 5a. (G7 and most soft gates done 2026-11-21, see the soft gates above for what remains) **Check by hand** on the pinned image: G7 and the soft gates of the client step, on the pinned Pocket ID image. Record the results in [rationale.md](rationale.md). A failed G7 stops the build and returns here; satisfies **AC-13**
   - 5b. (built 2026-11-21: the `oidc` capability lost `admin_email` and gained the `to_provider` field `logout_path`; the `api` entry no longer asks for `MP_ADMIN_EMAIL` and gives `logout_path: /`, and `api` no longer reads an admin email at start, so the old email rule of spec 0006 binds nobody until the setup is wired in (task 7b); Authelia's entry dropped the field and its template sets `post_logout_redirect_uris`; the provisioner hands `logout_path` to the job, which sends `logoutCallbackURLs`; `client_id` kept its pattern, `[a-z0-9][a-z0-9_-]*`, which takes `api_oidc`; the rest of 5b, the `oidc-provisioner` capability and `first_admin` on a provisioner provide, was built before; `TestOIDCCapability` in `libs/check`, goldens, and the Docker test checks the logout URI) **Contract first**: `catalog/capabilities/oidc-provisioner.yaml`, `scheme: http`, the `oidc` changes above (no `admin_email`, a `client_id` pattern, `logout_path`; `api`'s entry and the keeper golden change with the client id `api_oidc`), `first_admin` on a provisioner provide, and the checks with their catalog fixtures; `task generate`; satisfies **AC-9**
   - 5c. **Render**: resolve the consumer slot's text `to_provider` values (`{value}` or config sources) inside the provision job, which `bind.go` builds before `toProviderValues` runs today, and hand them over as `MP_<F>` (AC-9d), with the `public` slot's `origin` and `base_path`; check that the consumer and the provider share one ingress; move the `pocket-id` fixture from providing `oidc` to providing `oidc-provisioner`; goldens (a job per binding, `api` `depends_on` it, no key value in the Compose file); satisfies **AC-7**, **AC-8**
   - 5d. (built 2026-11-21: the job image wiring by spec 0030, the managed flavor golden `testdata/pocket-id-managed/golden` with a guard case, and `TestOIDCClientEndToEnd` in `libs/render/oidcclient_e2e_test.go`; the Docker test found that a job had no writable `/tmp`, so every provision and revoke job now gets a `tmpfs: [/tmp]`) **Scripts and tests**: provision and revoke scripts, our own job image (the Dockerfile at `catalog/images/oidc-job/`, built into a pre release zip, `dev:managed` and the release by [spec 0030](../0030-prerelease-image-source-zip/index.md) AC-14 and its task 13, which lands first; release-please; the image list is `releaseImages` in `libs/ci/release_test.go`, since 0031 holds no list), a managed flavor golden with a guard test, and the Docker test (`task test:integration`: three `up` runs, a base path change, an unbound consumer, a foreign client, a fresh database); satisfies **AC-8**, **AC-12**, **AC-13**
6. (built 2026-11-21: the whole `/setup` route of spec 0036 with its states, since none existed yet: `SetupGate` in the root, `Setup.tsx`, `TextField` with `type` and `errorMessage`; vitest per state. The page shows the create step and the sign in step together, because a visit after the account exists is a fresh page load and the page has no signal to tell them apart) **Editor setup page**: "Create your admin account" (shown when `firstAdmin` is true and `enroll` is absent), then "Sign in to finish the setup"; satisfies **AC-3**. Needs B2 and task 4.
7. **Cut over** (split 2026-11-21 into four parts; it waits for no part of #32): satisfies **AC-1**, **AC-3**, **AC-9**, **AC-14** to **AC-18**
   - 7a. **Setup files and render** (first check that 5b is really built: the `api` entry still had `admin_email` and no `logout_path` on 2026-11-21): `code` and `session-key` as `lifetime: setup` secrets in `api`'s entry; `new_password` and the `setup.sh` rules of AC-14; the render's mount and `MP_SETUP_DIR` in both renders; the `oidc` field `first_admin_path` and the `http-ingress` field `enter` with the exact route on the auth host (AC-16); the gate cookie's own MAC context and the prefix gate paths (AC-18); goldens, including the changed `platform_keeper_test` golden and render tests, including a render with `authelia` that still has none of it
   - 7b. **Api wiring**: the SDK create and remove helpers, the file `Keeper` adapter (AC-15), the identity adapter, `setup.New` in `main`, the gate middleware live; tests per case of AC-15 (a restart after the redeem keeps the code refused, a second `Done`, a changed code)
   - 7c. **The real entry**: `catalog/modules/pocket-id/module.yaml` from the fixture entry (diff the two before moving it, so an edit made in between is not lost), the pinned tag of the spike, `deploy/managed/graph.yaml` switches to it, Authelia stays as an optional entry (this is where 0026's `authelia` entry and template are finished), goldens
   - 7d. **Smoke**: the `@setup` test with a virtual authenticator, the saved browser state, `SMOKE_ADMIN_*` removed, the `setup.sh` answers, `wait_healthy` and the TLS check names; the soft gate that `skipConsent` removes the consent screen is recorded here (AC-17)
8. **Sign up config and docs**: the `signup` mapping and its refusal, the README text; satisfies **AC-6**, **AC-10**

## Migration plan

**Strategy**: no migration needed before the first release (spec 0030: images are built from source until then); to be confirmed, see Follow-up.
**Phases**:
1. Switch the managed graph and the fixtures in one change; Authelia's entry and its file enroll stay in the catalog and in their tests.
**Rollback**: revert the change; the old module is still in the catalog.
**Risks**: an install made with Authelia has a different issuer string and different user ids, so `api` refuses its pinned admin; the fix is `docker compose exec api /api admin reset`, then a new setup. A shipped release would need the upgrade feature (scope, "move a running platform") first.

## Consequences

**Positive**:
- No file edit and no password: the admin makes an account on the provider's own page, and the platform never handles the credentials.
- Reuses the setup session, the 0036 bind and the "module without an enroll" path that 0033 already allowed, so the seam stays small.
- The bundle stays small (one Go container on the platform's Postgres) and the auth host from spec 0026 is unchanged.

**Negative / tradeoffs**:
- The choice rests on facts not yet seen on a running image (the first admin page above all); the spike can send us back here.
- The ingress needs a new gated route, a contract change that every ingress replacement must also support (spec 0012).
- Pocket ID is passkey only: a user without a passkey capable device cannot sign in, and the smoke needs a virtual authenticator.
- The static API key is a powerful shared secret that stays on disk, and every client job mounts it.
- `oidc` now has two provider shapes (Authelia provides it directly, Pocket ID through a provisioner); both satisfy the same use, as `postgresql` does for a server and a provisioner.
- A failed client job keeps `api` from starting, so a Pocket ID that answers badly blocks the whole stack until fixed; the job log names the client, never a secret.
- The client id of `api` becomes `api_oidc` unless the graph names it.
- No email domain allow list, no roles, and no OpenID4VP; each needs another module or a later spec.
- Pocket ID's 2.x docs describe an unreleased version and a 3.x redesign was announced, so the tag is pinned and each upgrade is its own change.

**Neutral**:
- Spec 0035's enroll stays as is for Authelia.
- Scope #37 keeps its shape; its Authelia spike is replaced by this spike.

## Follow-up

- [ ] Used handoff tokens live in memory, so an `api` restart within their 60 seconds allows one replay (accepted 2026-11-21, a replay still needs the whole link; record used tokens in the setup folder if that changes).
- [ ] Until #32 the printed code is in the terminal and CI log of whoever runs `setup.sh`, and in Playwright traces; the README says to read the file, not the scrollback.
- [ ] When #32's keeper thread lands: replace the file adapter by the keeper client, move the code and key to the keeper's prepare (the seed's phase 1 of spec 0034), delete the `setup.sh` stage for `lifetime: setup` secrets and the read write mount, and restore spec 0036 AC-36's recovery. The adapter's doc comment and this item are the two places that say so.
- [ ] The README of the managed zip says, until #32: `setup.sh` prints the setup code once, the file is `secrets/api/setup/code`, and the recovery of a lost code before any admin exists (remove the files, run `setup.sh` again).
- [ ] Confirm that no released version shipped with Authelia (decides the migration plan); if one did, add the upgrade feature first.
- [ ] Add a one line pointer to this spec on the amended rows and ACs listed in the header, and also 0006 AC-2, 0026 AC-3 and AC-9 (`mp_authelia_session` in the smoke, the public suffix README line), and update the scope #37 task lines (`/sync` or the next `/architect` update).
- [ ] A later spec for email domain limits and roles in front of the bundled provider (scope #14, #28, #29), and for OpenID4VP sign in if a provider offers it.
- [ ] Sub path placement for the provider: the spike records it, a placement spec decides.
- [ ] Rotation of the static API key (not built here).
- [ ] The README names the client job's service (a failing job blocks `api` and the setup page, and `docker compose logs` is the only diagnosis), and says that deleting other secrets also removes a secret an admin added by hand to a marked client.
- [ ] Add the client job image to the frozen list of spec 0031, and note in spec 0014 that a revoke script may delete (a pointer, like the other amended rows).
- [ ] The README says: a client the platform made carries the marker and `up` deletes it when its consumer leaves; make hand made clients without that marker.
- [ ] No installed skill covers Pocket ID or OIDC providers; consider adding one if the spike shows the API is large.

## Rationale

Reasoning and options: see [rationale.md](rationale.md).

## References

**Project sources**:
- `AGENTS.md`: the exchangeable module rule, contract first, one package per context
- Specs [0001](../0001-platform-stack-architecture/index.md), [0006](../0006-managed-mode-control-plane/index.md), [0026](../0001-platform-stack-architecture/0026-identity-provider-own-host.md), [0031](../0031-frozen-image-versions/index.md), [0033](../0033-first-run-web-setup/index.md), [0034](../0033-first-run-web-setup/0034-setup-before-install.md), [0035](../0033-first-run-web-setup/0035-enrollment-seam.md), [0036](../0033-first-run-web-setup/0036-first-admin-session.md)

**Practices and standards**:
- Spike before commit when a load bearing fact is unverified
- Fail closed on a security gate
- Secrets as files with mode 0600, never in the environment of the rendered Compose file

**Links** (opened during the 2026-11-20 research; the page text was summarized by a small model, so check each value on the running image):
- Pocket ID environment variables: https://pocket-id.org/docs/configuration/environment-variables/
- Pocket ID repository: https://github.com/pocket-id/pocket-id
- Rauthy docs: https://sebadob.github.io/rauthy/
- Rauthy bootstrap: https://sebadob.github.io/rauthy/config/bootstrap.html
- Authelia with lldap: https://www.authelia.com/integration/ldap/lldap/
- lldap repository: https://github.com/lldap/lldap
