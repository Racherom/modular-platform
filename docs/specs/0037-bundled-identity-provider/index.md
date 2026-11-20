# 0037. The bundled identity provider: Pocket ID, behind a spike

**Date**: 2026-11-20
**Status**: In Progress
**Amends**: the *Identity provider* row of [0001](../0001-platform-stack-architecture/index.md); the `authelia` rows of [0015](../0011-capability-module-contract/0015-platform-graph-managed-zip.md) and [0026](../0001-platform-stack-architecture/0026-identity-provider-own-host.md) (the auth host stays, the module behind it changes); [0033](../0033-first-run-web-setup/index.md) AC-2 and AC-4 (the smoke enrolls with the bundled module, and the "module without an enroll block" path now runs end to end here); [0036](../0033-first-run-web-setup/0036-first-admin-session.md) AC-31 (the page copy for that path); the `http-ingress` capability of [0012](../0011-capability-module-contract/0012-capabilities-versions-matching.md) and [0013](../0011-capability-module-contract/0013-module-manifest-v1.md) (a gated route); install steps of [0016](../0016-platform-one-container/index.md) (an optional `network`).
**Part of**: scope #37 (web setup), whose Authelia enroll task stays for Authelia only.

## Summary

The platform's bundled sign in service changes from Authelia to Pocket ID. Authelia keeps its users in a file you must edit, which is why the web setup needed a special `enroll` step. With Pocket ID the admin creates their own account on Pocket ID's first admin page and registers a passkey (a key stored on your device, no password); the platform only guards that page, so nobody else can claim it. The guard reuses the setup session you already have from the web setup. Because the research could not confirm every Pocket ID detail, the first build task is a spike with pass or fail gates, and a failed hard gate stops the build and returns to `/architect`. Authelia stays in the catalog as an optional module.

## Requirements

**User stories**:
- As an operator, I want to create the first admin on the identity provider's own page, so that the platform never handles a user name, an email or a password.
- As an operator, I want that page closed to everyone but me until I have made the account, so that a stranger cannot claim the admin account of my provider.
- As an operator, I want new users to need an invitation by default and to open sign up only by changing the graph, so that strangers cannot make accounts.
- As an operator, I want to replace the bundled provider by another module that provides `oidc`, so that roles and policies can live in a module I choose.

**Acceptance criteria**:
- **AC-1**: A fresh managed render starts `pocket-id` as the provider of `oidc` on the auth host `auth.<domain>` (spec 0026 unchanged). Its discovery document names an issuer equal to `api`'s `MP_OIDC_ISSUER` exactly, and `api` turns ready with the same CA file as today under `tls-mode: internal`.
- **AC-2**: While no admin is bound, the ingress answers 403 for Pocket ID's first admin page and its setup calls (the paths G1 records) unless the request carries the auth host cookie of AC-4; every other Pocket ID path (sign in, discovery, token) stays open. If `api` is down or answers anything but "valid", the gate answers 403, never lets the request through.
- **AC-3**: The identity module has no `enroll` block, so the setup page, once the provider is ready, shows "Create your admin account" with a button and then "Sign in to finish the setup". In Chromium with a virtual passkey device, the admin creates the account on Pocket ID's page, registers a passkey, returns to the setup page, signs in, and is bound as the admin by the rule of spec 0036 (the email is optional there). This runs in the managed smoke.
- **AC-4**: The button calls `POST /api/v1/setup/identity-link` (setup cookie required) and gets a link to the auth host with a handoff token: signed with the setup session key (`libs/setupsession`), single use, valid 60 seconds. The auth host trades it for a cookie that is host only (no `Domain`), `HttpOnly`, `Secure`, with the same expiry as the setup session. A wrong MAC, an expired, reused or wrong host token, or a retired session gets 403 and no cookie.
- **AC-5**: After the admin is bound (or the setup is done), the gate cookie stops being valid, and Pocket ID's setup route stays closed (it closes itself once its own admin exists, G1). The README says how to recover a lost passkey (G7 records the provider's command).
- **AC-6**: With the default `signup: disabled` no account can be made without an invitation. Setting `signup` to another listed value in the graph changes only that behavior; any other value is refused with a problem at `modules.<i>.config.signup` (`invalid_config`).
- **AC-7**: Pocket ID's encryption key and static API key are generated secret files (mode 0600), never an environment value in the rendered Compose file, never in a log line, never in any `api` or renderer response. The `api` container cannot read the static API key.
- **AC-8**: A one shot step runs on every `up` and upserts the OIDC clients the graph declares (for `api`) in Pocket ID by client id, with the redirect URIs from the origin and base path; `api` waits for it. Changing the base path and rendering again repoints the redirect URI with no manual step.
- **AC-9**: The contract and the checks accept (a) an optional `gate` on an `http-ingress` route (a request the ingress first asks `api` about), (b) an optional `network` on an install step, and (c) a `first_admin` block on an `oidc` provider (the provider's own first admin page and the paths to gate). A platform graph with `authelia` in place of `pocket-id` still renders and passes the checks, so nothing in the setup names one identity module.
- **AC-10**: The module's healthcheck passes in Compose, the spike records the idle memory use, and the rendered README says: the sign in uses passkeys, the auth host needs its own DNS name, where the static API key lives and why, and how to recover a lost passkey.
- **AC-11**: The spike's results (each gate below, the exact calls and values, the pinned image tag) are in [rationale.md](rationale.md) before any task after it starts, and a failed hard gate stops the build.

## Decision

**Chosen option**: Option 2: Pocket ID as the bundled provider, behind a spike, with Rauthy and then Authelia with lldap as the ordered fallbacks.

Ship `pocket-id` as the default module that provides `oidc` and `site`, keep spec 0026's auth host, let the admin create the first account on Pocket ID's own page behind a gate that reuses the setup session, register the `api` client with a one shot step, and keep Authelia in the catalog as an optional module with its file enroll.

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

**Soft gates** (record the value; a miss goes to you, it does not stop the build): the healthcheck command in the image; whether the image has a shell and an HTTP client for the client step (if not, the step names a small pinned helper image); whether the encryption key and the static key can come from files, and how the database connection string, which holds a password, is built without a secret in the Compose file; whether `UI_CONFIG_DISABLED=true` is needed for the sign up env to apply (it is then required); the literal env values of each `signup` mode; the claims (`email`, `email_verified`) at sign in; whether a consent screen shows at the first sign in (the smoke must click it); the command that mints a sign in link for a lost passkey; a writable data folder and the container's hardening; idle memory (record it; above 300 MB you decide); whether a sub path works (recorded for a later placement spec, not built here).

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

### Interface (api, spec 0036's setup routes)

| Endpoint | Method | Key inputs | Key outputs | Auth | Key errors |
|---|---|---|---|---|---|
| `/api/v1/setup/identity-link` | POST | none | `url` (auth host link with a handoff token, 60 s) | setup cookie | 403 `setup_pending` (no session), 409 `setup_done`, 503 `provider_unavailable` |
| `/api/v1/setup/identity-enter` | GET | `t` (handoff token) | 302 to the provider's first admin page; sets the auth host cookie | handoff token | 403 (bad, expired or reused token) |
| `/api/v1/setup/gate` | GET | the cookie, forwarded by the ingress | 204 valid, 403 refused | the cookie | 403 |

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
| client step | client id and secret for `api` | the `oidc` binding: `client_id` from the consumer, `client_secret` generated (or the provider's, if G3 says so) |
| client step | redirect URIs | the platform origin and base path, as rendered today |
| client step | the Pocket ID admin credential | secret `static-api-key`, generated at install, mounted only into the provider and the client step |
| render | the auth host and the issuer | spec 0026; the issuer string is the one G4 proves |
| render | the sign up mode | module config `signup`, default `disabled`, mapped to the provider's literal values |
| setup page | whether to show "Create your admin account" | the identity module's catalog entry has `first_admin` and no `enroll` |

### Key invariants

- No platform service names Pocket ID. `oidc` and the `api` admin binding (spec 0006, 0036) stay as they are.
- The platform never sees the admin's email, user name or passkey; the email is optional at the bind.
- The gate fails closed and stops guarding only when the setup is done.
- The bundle models no roles. Read only or no access for some users comes from another module or capability (scope #14, #28, #29).
- `api` never holds the provider's static API key.
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
- Rerender: a changed base path repoints the client's redirect URI after `up`, verifies **AC-8**
- Contract: a graph with `authelia` in place of `pocket-id` still renders; a module with no `first_admin` gets no gate, verifies **AC-9**

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
4. **Api**: `identity-link`, `identity-enter` and `gate` on `libs/setupsession`, with the handoff token and the fail closed rule; satisfies **AC-2**, **AC-4**, **AC-5**. Needs B1, B2, and task 3.
5. **Client step**: the one shot upsert on every `up`, `api` waits for it; satisfies **AC-8**
6. **Editor setup page**: "Create your admin account", then "Sign in to finish the setup"; satisfies **AC-3**. Needs B2.
7. **Cut over**: the real `pocket-id` catalog entry, `deploy/managed/graph.yaml` switches to it, Authelia stays as an optional entry (this is where 0026's `authelia` entry and template are finished), goldens, the managed smoke with a virtual passkey device; satisfies **AC-1**, **AC-3**, **AC-9**
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
- The static API key is a powerful shared secret that stays on disk.
- No email domain allow list, no roles, and no OpenID4VP; each needs another module or a later spec.
- Pocket ID's 2.x docs describe an unreleased version and a 3.x redesign was announced, so the tag is pinned and each upgrade is its own change.

**Neutral**:
- Spec 0035's enroll stays as is for Authelia.
- Scope #37 keeps its shape; its Authelia spike is replaced by this spike.

## Follow-up

- [ ] Confirm that no released version shipped with Authelia (decides the migration plan); if one did, add the upgrade feature first.
- [ ] Add a one line pointer to this spec on the amended rows and ACs listed in the header, and also 0006 AC-2, 0026 AC-3 and AC-9 (`mp_authelia_session` in the smoke, the public suffix README line), and update the scope #37 task lines (`/sync` or the next `/architect` update).
- [ ] A later spec for email domain limits and roles in front of the bundled provider (scope #14, #28, #29), and for OpenID4VP sign in if a provider offers it.
- [ ] Sub path placement for the provider: the spike records it, a placement spec decides.
- [ ] Rotation of the static API key (not built here).
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
