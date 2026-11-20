# 0037 rationale: the bundled identity provider

Back to the build spec: [index.md](index.md).

## Context

The platform ships Authelia as the default provider of `oidc` (spec 0001, 0015, 0026). Authelia is a sign in portal built for putting a login in front of apps; it added an OpenID Connect provider later, and its own docs still call that part open beta. Its users live in a file (the file backend, which its docs do not recommend for production) or in an LDAP directory. It has no sign up page and no user API.

That shape already cost the platform something. Spec 0035 adds an `enroll` block and a keeper call only because the first admin must be written into `users.yml` by something, and the keeper is the only writer of the state folder. The enroll asks for a password, so the platform's setup page handles a secret that a passkey provider would not need at all.

Your requirements, from the interview of 2026-11-20: swap the bundled default, a small one container footprint, a way to get users in without editing files (a user create API, or a self service sign up that an admin can restrict), passkeys are fine, and the bundle needs no role model because someone who wants read only or no access for some users adds a module or capability for that. You asked whether OpenID and the own host are required; they are not strictly (OIDC is only what `api` speaks today; the own host was chosen for Authelia's cookie), and both were left as they are unless the spike shows a reason.

The web check could not confirm everything: most docs pages for the small providers returned 404 or 503, and many facts come from repository pages summarized by a small model. The decision is therefore a recommendation with gates, not a finished fact.

## Options considered

### Option 1: Keep Authelia and its file backend

Fix in place: leave the module, document that the file backend is for small installs, and keep the enroll that writes `users.yml`.

**Pros**:
- No rework; the 0033 spike and the 0035 enroll are already designed for it.
- Certified OIDC provider, light, well known.

**Cons**:
- No user API and no self service sign up; every user is a file edit.
- Its docs do not recommend the file backend for production, which is what you flagged.
- OIDC is still labeled open beta in its docs, and the enroll seam exists only to work around the file.

### Option 2: Pocket ID, behind a spike (chosen)

A small Go provider with passkeys, Postgres through a connection string, an admin API key set by environment, a sign up switch (disabled by default), and an OIDC client list. BSD-2-Clause. v2.19.3 on 2026-11-15.

**Pros**:
- The only small candidate with a documented static admin API key and a sign up mode off by default.
- Passkeys fit your answer and remove the password from the setup page.
- Needs no second container and no file edit for users.

**Cons**:
- The create user call, client creation, claims, healthcheck and RAM are unverified (docs pages returned 404 for the API).
- Needs its own host (its `APP_URL` cannot carry a path), so the second DNS name from 0026 stays.
- No first admin variable (the first admin is made on its own page, which must be gated), no client config file (a step registers the client by API), no email domain allow list, docs describe an unreleased version.

### Option 3: Rauthy

A Rust OIDC provider with the most verified headless bootstrap: first admin, users, API keys and clients from files or env, PKCE enforced, optional Postgres, a sign up mode with a domain filter, cookies bound to a path. Apache-2.0 (from the first research pass; not seen on an opened page in the second).

**Pros**:
- Best documented unattended start; possible return to a path based placement.
- Restrictable sign up with a domain limit.

**Cons**:
- Pre 1.0 with a breaking change in each of v0.35, v0.36 and v0.37, among them an issuer change that our exact issuer check is sensitive to.
- No `*_FILE` secrets; a runtime create user call is unverified; it needs SMTP for sign up activation.

### Option 4: Authelia with lldap

lldap holds the users (a GraphQL API, a login endpoint, users can change their own password) and Authelia stays the OIDC layer. Or `api` binds against lldap directly with a new `ldap` capability.

**Pros**:
- Verified LDAP backend in Authelia's docs; the most proven pair.
- The direct LDAP variant removes OIDC entirely.

**Cons**:
- Two containers; still Authelia in the OIDC variant.
- The direct variant needs a new capability, no passkeys, no browser sign in.
- Most lldap docs pages could not be read (503), so image tag, healthcheck and RAM are unknown.

## Rationale

Pocket ID wins on the forces in Context. The cost the platform pays today is the enroll workaround for a file backend, and only Pocket ID removes both the file and the password from the setup with one small container. Rauthy has the stronger bootstrap documentation, but its repeated breaking changes and missing `_FILE` secrets are a worse fit for a platform that pins images (spec 0031) and keeps every secret in a 0600 file. You chose Pocket ID first and Rauthy second; the spec keeps your order.

The unverified facts are why the first task is a spike with a stop rule. If a hard gate fails, the right fallback depends on which gate: a missing create user call points to Rauthy (bootstrap files) or lldap, a missing `email_verified` may fit either with a small change to the admin rule. So the build returns to `/architect` instead of switching on its own (your choice).

Three things in the design follow from passkeys and from your answers in the interview. The platform cannot ask for a password, and you chose that the admin enters their email in Pocket ID itself, so the first admin is made on Pocket ID's own first admin page and the platform never sees it; spec 0033 already allowed an identity module with no `enroll` block, so the setup page only shows a button and then "Sign in to finish the setup". That page is open to any visitor until an admin exists, so Caddy gates it with the same setup session used for the web setup (your answer), and because that session's cookie is host only on the platform origin, a 60 second single use handoff link carries it to the auth host. The cross check found this window; it is why G1 and G2 are hard gates. And the smoke test needs a virtual passkey device in Chromium (gate G6), so the headless sign in that exists today has to change.

A cross check by another model (2026-11-20) found 19 gaps in the first draft, which used an enroll step that created the user through Pocket ID's API and returned a link. Your answers removed most of them (the enroll network, the helper image, the link origin check, link lifetime, re-enroll rules, the display name mapping, the admin email conflict with 0034 to 0036). The rest are built into this spec: the client registration step, the gate that fails closed, the `invalid_config` code, the database connection string soft gate, and the longer amends list.

I kept the own host from spec 0026: it costs one DNS name and is the placement Pocket ID documents. Rauthy's path bound cookies could later remove the second name, but only if the spike of a future placement spec proves it.

Authelia stays in the catalog as an optional module because the bundle is only a default (principle 9). That keeps the 0035 enroll for modules that need it, and Pocket ID is the first module without an enroll, so the two prove together that the setup names no identity module.

## Landscape evidence (2026-11-20)

From three read only research passes on the cheapest model, then one subagent per shortlisted option. Facts marked unverified were not seen on an opened page. The text was summarized by a small model, so each value must be checked on the running image.

| Candidate | What was verified | Why it is not the pick |
|---|---|---|
| Pocket ID | v2.19.3 (2026-11-15), BSD-2-Clause, Go, Postgres by connection string, `STATIC_API_KEY` (16+ chars, admin), `ALLOW_USER_SIGNUPS` default disabled, `ENCRYPTION_KEY` required, `APP_URL` without a path | (picked) |
| Rauthy | v0.37.2, Rust, bootstrap admin, users, API keys and clients from files or env, PKCE S256, sign up domain limit, optional Postgres, breaking changes v0.35 to v0.37 | second fallback after you chose Pocket ID first |
| Authelia with lldap | Authelia LDAP backend for lldap documented; lldap login endpoint, Postgres, self service password change | two containers; many lldap facts unread |
| VoidAuth | v1.19.2 (2026-11-18), AGPL-3.0, OIDC, passkeys, invitations by admin UI, first admin link in the log | no user API found, one maintainer, official docs unreadable |
| Casdoor | v4.29.0 (GitHub Latest label), `init_data.json` seeding, `initAdminPassword`, Postgres | plain text admin password in config, several 2026 CVEs seen in a snippet, REST docs unreadable |
| Hanko | `POST /users`, JWKS at `/.well-known/jwks.json`, AGPL-3.0 backend | not an OIDC provider in its docs; the admin API key is documented for its Cloud only |
| Zitadel | AGPL-3.0, needs PostgreSQL 14 or newer | too heavy for a small default (RAM not verified) |
| Kanidm | MPL-2.0 (unverified), client creation by CLI | no PostgreSQL |
| Authentik | needs at least 2 GB RAM, first admin set in the browser | heavy; fails headless start |
| Keycloak, Ory | not verified | not small (Keycloak); two services plus a login UI (Ory) |
| Dex | OIDC | no local user management |
| OpenID4VP | not found in any opened page | absence is not proof; kept as a follow up |

## Spike results (2026-11-20, task 1, AC-11)

Run by hand: Compose with `postgres:18.6-alpine3.24`, Pocket ID, and `caddy:2.11.7-alpine` on `auth.platform.localhost:8443` (`tls internal`), with a `forward_auth` gate in front of the first admin paths, driven by headless Chromium (Playwright 1.64.1) with a CDP virtual authenticator (ctap2, internal, resident key, user verification). Every value below was seen on the running image.

**Pinned tag**: `ghcr.io/pocket-id/pocket-id:v2.19.3` (digest `sha256:189e03bb73bb86d7108fe81cbe2237c408cff2176b14cd7a39cc12a2ee8b2762`). Spec 0031 pins by tag, so the catalog uses the tag.

### Hard gates

| Gate | Result | What was seen |
|---|---|---|
| G1 | pass | An empty database serves the first admin page. The browser goes from `/setup` to `/signup/setup`. The account is made by `POST /api/signup/setup` (JSON `username`, `email`, `firstName`, `lastName`), which answers 200 with the user and sets the `__Host-access_token` cookie. A second call answers 409 `setup_already_completed`, so it closes itself. **The paths to gate are `/setup`, `/signup/setup` and `/api/signup/setup`** (the spec named the first and last). The page is one static app, so the real guard is the API call. After the account exists the app then registers the passkey with `/api/webauthn/register/start` and `/finish`, which need that session, so they stay open to the ingress. |
| G2 | pass | Behind `forward_auth` the admin made the account and registered a passkey end to end. Without the cookie, `/setup` and `/api/signup/setup` answered 403; `/login`, `/.well-known/openid-configuration` and the sign in paths answered 200 for everyone. |
| G3 | pass | With the header `X-API-KEY`: `POST /api/oidc/clients` takes our `id` (we chose `mp-api`), `callbackURLs`, `pkceEnabled` and answers 201. `GET /api/oidc/clients/{id}` reads it (404 if absent), `PUT` updates it. `POST /api/oidc/clients/{id}/secrets` with `{"secret": "..."}` stores a secret **we choose** (201). The token endpoint accepted it (`client_secret_basic`). A full code flow with PKCE S256 returned an `id_token`. So the `client_secret` direction stays as the spec has it (`api` gets the generated secret, the step registers it). |
| G4 | partly proven | The discovery issuer equals `APP_URL` exactly (`https://auth.platform.localhost:8443`, no trailing slash, no path), and `iss` in the id token is the same string. The second half (`api` reaching the provider through the ingress alias with our CA) uses the same mechanism as today and is proven at the cut over (task 7). |
| G5 | pass | On v2.19.3 these work: `ENCRYPTION_KEY_FILE`, `STATIC_API_KEY_FILE`, `DB_CONNECTION_STRING`, `PGPASSFILE`, `TRUST_PROXY`, `UI_CONFIG_DISABLED`, `ALLOW_USER_SIGNUPS`, `FILE_BACKEND=database`, `ANALYTICS_DISABLED`, `APP_URL`. |
| G6 | pass | A virtual authenticator registered a passkey and signed in again. The authenticator belongs to one page, so the smoke must use the same page for both. |

### Soft gates

| Item | Value |
|---|---|
| Healthcheck | `["CMD", "/app/pocket-id", "healthcheck"]` (the image's own) |
| Shell and HTTP client | The image has `/bin/sh` and busybox `wget`. I did not test whether that `wget` can send the JSON calls above (a `POST` with a header and a body, and a `DELETE`); task 5 tests it first and names a small pinned helper image if it cannot. |
| Secrets as files | `ENCRYPTION_KEY_FILE` and `STATIC_API_KEY_FILE` work. `DB_CONNECTION_STRING_FILE` was not tried; the connection string without a password plus `PGPASSFILE=/run/secrets/pgpass` (a libpq password file, one line `host:port:db:user:password`) connected, so no password sits in the Compose file. |
| `UI_CONFIG_DISABLED` | Needed for the sign up env to stay in force (the settings page would own the value otherwise). With `true`, `allowUserSignups` reads back the env value. |
| Sign up values | `ALLOW_USER_SIGNUPS` is one of `disabled`, `withToken`, `open`. Any other value stops the container at start (`invalid environment app configuration`). With `disabled`, `POST /api/signup` answers 403 `open_signup_disabled`. The graph's `signup` values map one to one. |
| Claims at sign in | `sub` (stable user id), `email` (as typed), `email_verified: false`, `name`, `preferred_username`, `amr: ["phr"]`. The email is not verified, so spec 0036's optional email rule must not require `email_verified`. |
| Consent screen | At the first sign in the smoke clicked "Sign in" twice before the callback came (I did not confirm that the second click is a consent screen). The client has a `skipConsent` field (false by default); task 5 tests setting it to `true` for the platform's own client, and the smoke clicks until the callback. |
| Lost passkey | `docker compose exec pocket-id /app/pocket-id one-time-access-token <username or email>` prints a one hour link `https://<auth host>/lc/<token>`. The README says this. |
| Data folder and hardening | With `user: 1000:1000`, `read_only: true`, `cap_drop: [ALL]` and `no-new-privileges`, it runs healthy using `FILE_BACKEND=database`. One non fatal error remains (it cannot create `data/` for the GeoLite city database); a tmpfs on `/app/data` owned by 1000 removes it. No volume is needed besides Postgres. |
| Idle memory | 14 to 18 MiB (limit 300 MB, passed). |
| Sub path | Not tested; Pocket ID's docs say `APP_URL` has no path. Left to a placement spec. |

### Findings that change how the later tasks are built

- **The secret step is not idempotent by itself.** `POST .../secrets` adds one more secret every call, and `DELETE /api/oidc/clients/{id}/secrets/{secretId}` (204) removes one. The one shot step adds our secret, then deletes every other secret id on that client, so any number of `up` runs ends with one secret. Listed in `credentials.secrets` of the `GET`.
- **The account call makes a session before the passkey.** If the admin stops after `POST /api/signup/setup` and before the passkey, the account exists without a credential. The lost passkey command above recovers it, and the README says so.
- **Pocket ID logs request paths.** Its access log line holds the path and query, and the one time link `/lc/<token>` is a path. The handoff token never reaches Pocket ID (spec 0037, Interface), but the README should say that a one time access link shows in its log.
- **`state` needs about 16 characters or more** (`invalid_state` otherwise); `api` already makes a long one.

No hard gate failed, so the build goes on.

## The client step (decided 2026-11-21)

Your ask was a new `jobs:` block: a per `up` Compose job that waits for the provider healthy, with a `depends_on` of `service_completed_successfully` on each consumer. Spec 0014 already renders exactly that for a provisioner, so the second option below is what you picked after I showed it.

| Option | Verdict |
|---|---|
| Reuse the provisioner: `oidc-provisioner` yields `oidc` (**chosen**) | The provision job, the consumer wait, the revoke job, the marker, the keep list and the wait anchor exist and are tested. Costs one new yields source (`consumer`) and a second provider shape for `oidc`. |
| A `jobs:` block that takes its image from a capability | A second way to run a job per binding, with its own guard kind and revoke rule. |
| A `jobs:` block with image and script in the entry | Self contained, but every identity module repeats the job and the provisioner machinery stays unused. |

Other answers from the interview: one job per client (each consumer waits only for its own client; one failure names its client); the job runs the image of the capability file, as every provisioner does (the provider's own image has busybox `wget` and no JSON parser, which the secret cleanup needs); a removed client is deleted, by the revoke job, only when it carries our marker; the job reaches Pocket ID by its service name on the internal network (no CA, no Caddy, and the gate is not in the path).

Risk found while writing: `api` waits for the job, the setup page is served by `api`, and no admin exists at the first `up`. If the static key needs an existing admin, the stack deadlocks, so G7 is a hard gate in task 5a (it passed, see *Task 5a results*).

Cross check by another model (2026-11-21) found 11 gaps; you chose to apply the recommended fixes. The ones that changed the design: the `consumer` yields source is replaced by a render rule (the check refuses a source on a `to_provider` field); the dead `admin_email` leaves `oidc`; `client_id` gets a pattern; the job address uses `scheme: http` and `{platform: url}`; `api` and the job build their URIs from the same `redirect_path` and `logout_path`, and the client carries `logoutCallbackURLs`; the job image is our own, because it holds the admin key and runs read only with no egress; the flavors and the "gate adds no `depends_on`" rule are stated.

## Task 5a results (2026-11-21, G7 and the client step's soft gates)

Run by hand: `ghcr.io/pocket-id/pocket-id:v2.19.3` alone on a Docker network (SQLite, no admin, `STATIC_API_KEY_FILE` and `ENCRYPTION_KEY_FILE` as files), called from a second container by the service name over plain HTTP (no `X-Forwarded-*`, internal Host). Idle memory of that container: about 34 MB.

**G7: pass.** With an empty database and no admin, `POST /api/oidc/clients` answers 201, so the job can succeed before any admin exists and there is no deadlock (AC-13).

| Question | Result |
|---|---|
| Auth | Header `X-API-KEY: <key>`. A wrong key answers 401. Calls to `http://pocket-id:1411` work with the internal Host. |
| Read | `GET /api/oidc/clients/<id>` answers 404 when missing, 200 with the client and `credentials.secrets[]` (`id`, `prefix`, `isActive`; never the value). |
| Create | `POST /api/oidc/clients` with `id`, `name`, `callbackURLs`, `pkceEnabled`, `isPublic` answers 201; the client id is ours. An existing id answers 409 `client_id_already_exists`. The 201 body holds a generated secret in `createdSecret`, which the job deletes in the secret step. |
| Update | `PUT /api/oidc/clients/<id>` (the same body without `id`) answers 200 and replaces `callbackURLs`. |
| Secret we choose | `POST /api/oidc/clients/<id>/secrets` with `{"secret": "<value>"}` answers 201 and the client then accepts that value (the key must be `secret`; any other key is ignored and a random secret is made). The same value twice makes two secrets, so the ensure of the spec (read ids, add, delete the old ids) ends with one. |
| Delete | `DELETE /api/oidc/clients/<id>/secrets/<secretId>` answers 204; `DELETE /api/oidc/clients/<id>` answers 204, then `GET` answers 404. |
| Marker | The client field `description` is stored and returned by both `GET` and the list, so the marker lives there (`mp-marker:<MP_MARKER>`). No name prefix is needed. |
| List and paging | `GET /api/oidc/clients?pagination[limit]=N&pagination[page]=P` (brackets URL encoded); the answer has `data[]` and `pagination.totalPages`. Default 20 per page. |
| `skipConsent` | The create and update calls accept `skipConsent: true` and return it. Whether the consent screen is gone is for the smoke to show (task 7). |

**Job image: decided with you, build our own.** No existing image has `/bin/sh`, `curl`, `jq` and `sleep infinity` with a versioned tag. `badouralix/curl-jq` has all four but only the moving tags `latest` and `alpine`; `curlimages/curl` has a version but no `jq`; `nicolaka/netshoot` is too large. The platform builds a small image from `alpine:3.24` plus `curl` and `jq` (busybox `sleep infinity` works, rc 143 when stopped), published and tagged like its other images. Spec 0031's frozen list and the build and release files follow in task 5d. `/architect` should fold this into the *client step* section of index.md.

## References

**Project sources**:
- `AGENTS.md`: the exchangeable module rule and contract first
- Specs 0001, 0006, 0026, 0031, 0033, 0035 (see [index.md](index.md))

**Practices and standards**:
- Spike before commit when a load bearing fact is unverified
- Secrets as files, mode 0600

**Links** (opened during the research; not fetched again):
- Pocket ID environment variables: https://pocket-id.org/docs/configuration/environment-variables/
- Pocket ID repository: https://github.com/pocket-id/pocket-id
- Rauthy docs: https://sebadob.github.io/rauthy/
- Rauthy bootstrap: https://sebadob.github.io/rauthy/config/bootstrap.html
- Rauthy v0.35.0 release (issuer change): https://github.com/sebadob/rauthy/releases/tag/v0.35.0
- Authelia with lldap: https://www.authelia.com/integration/ldap/lldap/
- lldap repository: https://github.com/lldap/lldap
- VoidAuth repository: https://github.com/voidauth/voidauth
- Casdoor init data template: https://github.com/casdoor/casdoor/blob/master/init_data.json.template
- Hanko JWKS: https://docs.hanko.io/api-reference/public/well-known/get-json-web-key-set.md
