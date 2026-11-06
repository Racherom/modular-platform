# 0006. Managed mode control plane

**Date**: 2026-11-03
**Status**: In Progress
**Extended by**: [0020](../0017-external-capability-providers/0020-holdings-list-revoke.md) (#10): the `record` and `holdings` apply steps, `deployments.marker`, `operations.flags`, revoke before remove.

## Summary

This spec designs the first managed thread (scope #16): you unzip the platform on your own host, run `setup.sh`, start it, sign in as the one admin, review a plan, deploy a graph to that host, and watch each module's state live. `api` keeps the record (sessions, deployments, an append only history), the planner renders the project, and the deployer only applies files with Docker Compose, so the root equivalent service (one that can control the whole host through the Docker socket) stays small. A failed start rolls back to the last working version on its own, and the platform's own services talk over TLS from a separate certificate issuer. It fixes the two new contracts, the first four tables, the editor screens, the managed zip and the CI smoke job that proves the whole thread.

## Structure

- [0007. Deploy target contract and deployer](0007-deploy-target-deployer.md): `deploy-target.yaml`, the apply and remove pipeline, the deployment folder, secret creation, the host access guard, labels and module state; supports AC-8, AC-9, AC-11 to AC-13, AC-15, AC-16.
- [0008. Managed TLS and the certificate issuer](0008-managed-tls-cert-issuer.md): the `cert-issuer` module on step-ca, TLS mode for `api`, `planner` and `deployer-docker`, the caller key pair; supports AC-5 and AC-17.
- [0009. Managed zip, install, dev loop and smoke job](0009-managed-zip-install-smoke.md): `deploy/managed/`, `setup.sh`, the zip task and release, `task dev:managed`, the `smoke-managed` CI job; supports AC-1, AC-19 and the done line.

Contracts shared by the children (names, paths, labels, the managed render) live in *Cross child contracts* below.

## Requirements

**User stories**:
- As a self hoster, I want to install the platform on my host from one zip and sign in as its admin, so that nobody else can drive a service that controls my host.
- As the admin, I want to review exactly what will run before I deploy, so that an imported graph file cannot surprise me.
- As the admin, I want to deploy, redeploy and remove a stack from the editor and see each module's state live, so that I never need a shell for the normal path.
- As the admin, I want a failed start to return my stack to the last version that worked, with a history that says what failed, so that a bad change does not leave me with a broken stack.
- As a platform contributor, I want one CI job to prove install, sign in, deploy and live state on every pull request, so that the first thread never breaks unnoticed.

**Acceptance criteria**:
- **AC-1**: On a clean Linux host with Docker, unzipping `modular-platform-<version>.zip`, running `sh setup.sh` (it asks the domain, the admin email, and the admin user name and password) and `docker compose up -d` brings `ingress`, `web`, `api`, `planner`, `deployer-docker`, `cert-issuer`, `postgres` and `authelia` to healthy and `db-init` to exit 0. Running `setup.sh` again changes no existing file. No secret value appears in `.env`, in a Compose file, or in any container log.
- **AC-2**: With `PLATFORM_SIGNED_OUT=index` (the default), a signed out visitor to `https://<domain>/` sees the index with a *Sign in* button; with `sign-in` the browser goes straight to the provider. The first sign in whose `email` equals `PLATFORM_ADMIN_EMAIL` (whole address, case insensitive) with `email_verified` true (both read from the provider's userinfo answer, falling back to the ID token) binds that `iss` plus `sub` as the admin, atomically, so two parallel first sign ins bind only one; after that only that `iss` plus `sub` gets a session, and a later change of `PLATFORM_ADMIN_EMAIL` has no effect until `admin reset`. Any other identity, or an unverified email while no admin is bound, gets no session and lands on the signed out screen with reason `not_admin` or `email_unverified`.
- **AC-3**: The session cookie is `__Host-mp_session` (HttpOnly, Secure, SameSite=Lax, `Path=/`, no `Domain`), and the `sessions` table holds only the SHA-256 of its token. A session ends after `session-idle` without a request or `session-max` after sign in, whichever comes first; then every `/api/v1` route except the auth routes answers 401 `unauthenticated`, and an open event stream sends `signed-out` and closes. A `session-idle` outside 5m to 24h, a `session-max` above 720h, or a `session-max` below `session-idle` stops `api` at start with one problem line each.
- **AC-4**: *Sign out* (`POST /api/v1/auth/logout`) deletes the session row, clears the cookie and answers the next URL: the provider's `end_session_endpoint` with `id_token_hint` and `post_logout_redirect_uri` when discovery lists one, else `<base>/`. The old cookie then gets 401. The next sign in from that browser sends `prompt=login`, so the provider asks for credentials again.
- **AC-5**: A mutating `api` request whose `Origin` is missing or not exactly `MP_PUBLIC_ORIGIN` answers 403 `bad_origin`, even with a valid session. `planner` and `deployer-docker` answer 401 without their binding token, and no ingress route reaches either. Every call from `api` to them carries an `MP-Caller` token (the session's admin, or for background calls the `sub` named in *Value sourcing*); the deployer refuses an apply or remove without one (401 `invalid_token`) and logs the caller's `sub` on each operation start.
- **AC-6**: `docker compose exec api /api admin reset` deletes the admin binding and every session and prints one line; the next verified sign in with `PLATFORM_ADMIN_EMAIL` binds an admin again.
- **AC-7**: `POST /api/v1/deployments/{name}/review` with the sample graph answers the modules with their images, the bindings, the published ports (empty in `v0alpha1`), every placed credential with whether it already exists, and the plan digest. It writes no row and touches nothing on the host. A graph whose `name` is not `{name}` answers 422 `name_mismatch`; the name `modular-platform` answers 422 `reserved_name`; a graph with plan errors answers 422 with the plan's problems.
- **AC-8**: The guard is an allow list: a rendered project may hold only the Compose keys and values `RenderManaged` emits (child [0007](0007-deploy-target-deployer.md)). Anything else, including `privileged`, a bind mount, host or container networking, host `pid` or `ipc`, devices, added capabilities, `security_opt`, `volumes_from`, `build`, `include`, an `env_file` outside the folder, an `external` network or volume, the Docker socket, or a published port equal to `PLATFORM_HTTP_PORT` or `PLATFORM_HTTPS_PORT`, is refused by the planner's managed render with problem `host_access` naming the instance and key, and refused again with 422 `host_access` when sent to the deployer directly. A golden test shows every key the render emits passes. A project whose name does not match the pattern, or whose services' `graph` label is not the project name without `mp-`, or whose `deployment` label is not the request's deployment, is refused with 422 `invalid_project`.
- **AC-9**: Confirming a review starts an apply: `POST .../apply` answers 202 with the operation; the deployment shows `applying` with the step (`prepare`, `pull`, `up`); the operation succeeds only when every service is running and every healthcheck passes within 300 seconds, after a pull of at most 10 minutes; the deployment then shows `running` and every module `running`. A second apply or remove of the same deployment while one runs answers 409 `busy`, also across two `api` processes. Different deployments apply in parallel.
- **AC-10**: An apply whose `planDigest` differs from the digest of the plan `api` computes at apply time answers 409 `plan_changed`, and nothing reaches the deployer.
- **AC-11**: The deployer creates every missing generated secret of the project in `<PLATFORM_DIR>/deployments/<project>/secrets/` (32 random bytes as hex, mode 0600, owned by `PLATFORM_UID:PLATFORM_GID`) and never overwrites one. Review lists each missing placed credential; an apply that leaves one out answers 422 `placed_missing`; a supplied value is written once, and an existing one is replaced only with `replace: true`; an empty value, one over 4096 bytes, or one holding a NUL byte answers 422 `placed_invalid`. No generated or placed value appears in the `api` database, in a log line, or in any `api` or deployer response; `operations.placed_supplied` holds paths only.
- **AC-12**: Every container of a deployment carries the labels `io.github.racherom.modular-platform.graph`, `.instance`, `.module` and `.deployment`, and `restart: unless-stopped`. After a Docker restart the deployed stack comes back and the editor shows it `running` without an apply.
- **AC-13**: The deployment screen shows each module's state as a text badge (`pending`, `starting`, `running`, `unhealthy`, `restarting`, `exited <code>`, `paused`, `missing`), never color alone. With the screen open, a crash of one module (its main process killed with `kill -9` from the host) shows within 6 seconds as a higher restart count with last exit code 137 (or `exited 137` / `restarting` if caught before Docker restarts it). The last exit code comes from the Docker `die` events the deployer has seen since it started (child 0007, *Last exit code*); after a deployer restart a module shows no last exit code, never 0, until it exits again, and its restart count still shows. While no screen watches a deployment and no operation runs, `api` does not poll the deployer for it.
- **AC-14**: An apply of a changed graph that fails at step `up` on a deployment with an earlier succeeded apply is followed at once by a rollback operation (`rollback_of` = the failed one) that applies the graph of the most recent succeeded apply before it. The history shows the failed apply (step, error) and then the rollback; the deployment shows `rolled-back` with a link to what failed until the next apply or remove. A failed rollback is retried once after 30 seconds as its own operation, then the deployment shows `failed`; an apply or remove started within those 30 seconds cancels the retry. A failure at `prepare` or `pull`, or a refusal, starts no rollback and leaves running containers and the deployment's files as they were. A first apply that fails leaves its containers for inspection and shows `failed`.
- **AC-15**: *Remove* runs `docker compose down` and keeps volumes and secrets; the deployment shows `removed` and keeps its history, and a later apply brings it back with its old data. With *Delete data too*, the request must carry `confirmName` equal to the name (else 422 `confirm_mismatch`); the deployer runs `down --volumes` and deletes the deployment's `secrets/` folder. A plain remove of a removed deployment answers 409 `already_removed`; one with *Delete data too* is allowed.
- **AC-16**: When the deployer restarts during an apply, `api` marks that operation `unknown` on its next poll (a new boot id). When `api` starts, it asks the deployer about every operation still `running`: under the same boot id it resumes polling; on a new boot id or a 404 it marks it `unknown`. When polls fail for 60 seconds in a row, or an operation has run for 20 minutes, `api` marks it `unknown`. The editor shows `failed` and `unknown` with *Apply again*, which opens review with `Deployment.latestApplyGraph` (the graph of the newest apply of the deployment, whatever its status, so after an `unknown` apply of a changed graph it is the changed graph, not the one still running) and succeeds when applied. When the latest operation is a remove that ended `failed` or `unknown`, the banner offers *Remove again* (the remove dialog with *Delete data too* preset to that remove's `deleteData`) beside *Apply again*. When the deployer cannot be reached at apply, the operation is recorded as `failed` with `deployer_unavailable` at once.
- **AC-17**: `api`, `planner` and `deployer-docker` serve only TLS on 8443 with 24 hour certificates for `<instance>.modular-platform.internal` from `cert-issuer`; `cert-issuer` refuses any name outside `*.modular-platform.internal`; the ingress reaches `api` over TLS trusting the internal root. SDK modules in deployed graphs run plain, without `MP_ACME_URL`.
- **AC-18**: `contracts/openapi/api.yaml` and `contracts/openapi/deploy-target.yaml` exist; `task generate` produces their Go servers, Go clients and the editor's TypeScript client, and CI's diff check is clean. `planner.yaml` gains `PlanResult.digest` and `POST /v1/render/managed`, which answers 404 in public mode. Every error on the wire is a `Problem` or `ProblemList`.
- **AC-19**: `task dev:managed` runs the managed graph at `https://platform.localhost` with the editor hot reloading from the Vite dev server. The CI job `smoke-managed` runs on every pull request: install with `setup.sh`, start, sign in as the admin headlessly, deploy the sample, wait until the editor shows every module `running`, then remove it with *Delete data too*. A release tag attaches `modular-platform-<version>.zip` to the GitHub release.
- **AC-20**: In managed mode *Download zip* still works and gives the same bytes as the online tool for the same graph.

## Decision

**Chosen option**: Option 1: `api` orchestrates and keeps the record, the planner renders a managed flavor, and the deployer applies files and reports state (see [rationale.md](rationale.md)).

Build `api` and `deployer-docker` on the SDK with the contracts, tables and screens below, a separate `cert-issuer` module for the platform's own TLS, and automatic rollback driven by `api`.

**Implementation skills**: `golang-context` · `golang-concurrency` · `golang-database` · `golang-security` · `golang-error-handling` · `golang-structs-interfaces` · `golang-observability` · `golang-testing` · `golang-naming` · `golang-code-style` (all `samber/cc-skills-golang`, `.claude/skills/<name>/`) · `supabase-postgres-best-practices` (`supabase/agent-skills`, `.claude/skills/supabase-postgres-best-practices/`) · `openapi-spec-generation` · `github-actions-templates` · `react-state-management` (all `wshobson/agents`, `.claude/skills/<name>/`) · `tanstack-query` · `tanstack-router` (both `tanstack-skills/tanstack-skills`, `.claude/skills/<name>/`) · `react-flow` (`existential-birds/beagle`, `.claude/skills/react-flow/`) · `multi-stage-dockerfile` (`github/awesome-copilot`, `.claude/skills/multi-stage-dockerfile/`) · `docs` (`minio/skills`, `.agents/skills/docs/`, for the managed README)

## Rationale

Reasoning and options: see [rationale.md](rationale.md).

## Feature design

### Data model sketch

One goose migration in `services/api/internal/store/migrations/` (sqlc queries beside it). All times `timestamptz`. Postgres 18 `uuidv7()` for ids.

| Table | Column | Type | Null | Notes |
|---|---|---|---|---|
| `sessions` | `token_hash` | `bytea` | no | primary key, SHA-256 of the 32 byte cookie token, `CHECK (length(token_hash) = 32)` |
| | `iss`, `sub` | `text` | no | identity of the admin |
| | `email` | `text` | no | the `email` claim at sign in, for display and history |
| | `id_token` | `text` | yes | raw ID token, stored only when discovery lists `end_session_endpoint`, used only as `id_token_hint` at sign out |
| | `created_at` | `timestamptz` | no | default `now()` |
| | `last_seen_at` | `timestamptz` | no | written at most once a minute per session |
| | `expires_at` | `timestamptz` | no | `created_at + session-max`; index for the sweep |
| `admin_binding` | `singleton` | `boolean` | no | primary key, default true, `CHECK (singleton)`, so at most one row |
| | `iss`, `sub` | `text` | no | the bound identity |
| | `email` | `text` | no | email at binding time |
| | `bound_at` | `timestamptz` | no | default `now()` |
| `deployments` | `id` | `uuid` | no | primary key, default `uuidv7()` |
| | `name` | `text` | no | unique; the graph name (0002 name rules), never `modular-platform` (`CHECK`) |
| | `project` | `text` | no | unique; `'mp-' \|\| name` (stored generated column) |
| | `created_at` | `timestamptz` | no | default `now()` |
| | `latest_operation_id` | `uuid` | yes | FK to `operations(id)`, `DEFERRABLE INITIALLY DEFERRED` |
| `operations` | `id` | `uuid` | no | primary key, default `uuidv7()` |
| | `deployment_id` | `uuid` | no | FK to `deployments(id)` |
| | `action` | `text` | no | `CHECK (action IN ('apply','remove'))` |
| | `status` | `text` | no | `CHECK (status IN ('running','succeeded','failed','unknown'))` |
| | `step` | `text` | yes | `prepare`, `pull`, `up`, `down`, `done`; the last step reached |
| | `graph` | `jsonb` | yes | the graph document; `CHECK ((action = 'apply') = (graph IS NOT NULL))` |
| | `plan_digest` | `text` | yes | `sha256:<hex>`; apply only |
| | `delete_data` | `boolean` | no | default false; true only on a remove |
| | `rollback_of` | `uuid` | yes | FK to `operations(id)`; set on a rollback apply |
| | `placed_supplied` | `text[]` | no | default `'{}'`; paths written by this apply, never values |
| | `started_by_iss`, `started_by_sub`, `started_by_email` | `text` | no | from the session |
| | `deployer_operation_id`, `deployer_boot_id` | `text` | yes | set once the deployer accepted |
| | `error` | `text` | yes | a problem code plus at most the last 16 KB of Compose output |
| | `started_at` | `timestamptz` | no | default `now()` |
| | `finished_at` | `timestamptz` | yes | set with a final status |

Indexes: `operations (deployment_id, started_at DESC)`; partial unique `operations (deployment_id) WHERE status = 'running'` (the 409 `busy` rule in the database); `sessions (expires_at)`.

Relationships: deployment 1:N operations; deployment N:1 its latest operation (nullable); operation N:1 the operation it rolls back (nullable). Sessions and the admin binding meet by `iss` plus `sub`, with no foreign key, so a reset deletes both independently.

Retention: `api` deletes sessions past `expires_at` or idle every 10 minutes. Deployments and operations are never updated after their final status and never deleted in thread 1 (#4 and #15 decide retention).

### State transitions

Operation: `running` → `succeeded` | `failed` | `unknown`. Final states never change. `step` moves `prepare` → `pull` → `up` → `done` for an apply and `prepare` → `down` → `done` for a remove.

Deployment state is derived from its latest operation at read time, never stored:

| Latest operation | Deployment state | Banner actions |
|---|---|---|
| `running` apply, `rollback_of` null | `applying` | none |
| `running` apply, `rollback_of` set | `rolling-back` | none |
| `running` remove | `removing` | none |
| `succeeded` apply, `rollback_of` null | `running` | none |
| `succeeded` apply, `rollback_of` set | `rolled-back` (links the failed operation) | none |
| `succeeded` remove | `removed` | none; *Deploy* on the graph screen brings it back (AC-15) |
| `failed` apply | `failed` (with `step`; before `up` the editor says the previous version keeps running) | *Apply again* |
| `failed` remove | `failed` (with `step` and error; the editor says the remove did not finish) | *Remove again*, *Apply again* |
| `unknown` apply | `unknown` | *Apply again* |
| `unknown` remove | `unknown` (the editor says the remove did not finish) | *Remove again*, *Apply again* |

*Apply again* opens the same review dialog as *Deploy*, so 409 `busy` (the deployer still runs the operation `api` marked `unknown`), `plan_changed` and 422 plan problems show exactly as they do there. During the 30 second retry window of a failed rollback, *Apply again* reviews the failed rollback's graph (the last good one), and applying it cancels the retry like any apply; the banner shows no countdown, because an `api` restart drops the timer. The graph of an apply that failed at `up` is reachable after a rollback only from the graph screen, by deploying it again; that is intended, since it is known to fail at start.

Rollback (driven by `api`, in `internal/deploy`):
1. The poller sees an apply end `failed` with `step = up`.
2. If that apply is not itself a rollback, `api` looks for the most recent `succeeded` operation of the deployment before it. If that is an apply, `api` inserts a rollback apply (`rollback_of` = the failed operation, its graph, the failed operation's `started_by_*`, a fresh `plan_digest`) and sets it as latest, in one transaction, then sends it like any apply: the plan is still checked (a plan error or a guard refusal ends the rollback as `failed` with that problem), only the digest compare is skipped, no placed values are sent, and existing files are reused. If it is a remove or there is none, nothing more happens; the `failed` banner then says no earlier version exists to return to (also after an `unknown` apply, which never triggers a rollback).
3. If a rollback apply fails at any step and fewer than two operations carry its `rollback_of`, `api` arms a 30 second timer. When it fires, and the latest operation is still that failed rollback, `api` starts a second rollback the same way. A user apply or remove makes the latest operation change, so the timer does nothing. An `api` restart drops the timer; the deployment stays `failed`.

### API surface (`contracts/openapi/api.yaml`)

All paths sit under `<base>/api/v1` (base empty in thread 1). "Admin" means a valid session (cookie plus a matching, unexpired row). Every mutating route also needs the `Origin` check (AC-5). Errors are `ProblemList`; codes in the last column.

| Endpoint | Method | Key inputs | Key outputs | Auth | Key errors |
|---|---|---|---|---|---|
| `/auth/login` | GET | `return_to`: path (opt, must start with `<base>/`, default `<base>/`) | 302 to the provider; sets `__Host-mp_login` (state, PKCE verifier, nonce, `return_to`; 10 minutes) | public | 503 `provider_unavailable` while discovery has not succeeded |
| `/auth/callback` | GET | `code`, `state` | session cookie; 302 to `return_to`; on refusal 302 to `<base>/?signed_out=<reason>` | public (state cookie) | reasons `not_admin`, `email_unverified`, `state_mismatch`, `provider_error` |
| `/auth/logout` | POST | none | `{redirect: url}`; clears both cookies; sets `__Host-mp_relogin` (a browser session cookie, no `Max-Age`), so the next login sends `prompt=login` and clears it | admin | 401, 403 `bad_origin` |
| `/session` | GET | none | `{email, expiresAt, idleExpiresAt}` | admin | 401 `unauthenticated` |
| `/catalog`, `/plan`, `/render` | GET, POST, POST | as in `planner.yaml` | as in `planner.yaml` (`/render` the static zip) | admin | as the planner, 401, 403 |
| `/deployments` | GET | `limit`: int (opt, default 50, max 200), `cursor`: string (opt, the last name seen) | `{items: [DeploymentSummary], next}` sorted by name; summary: `name`, `state`, `latestOperation` | admin | 401 |
| `/deployments/{name}` | GET | `name` | `Deployment`: summary fields, `project`, `createdAt`, `graph` (of the latest succeeded apply), `latestApplyGraph` (of the newest apply, any status; *Apply again* reviews it), `rolledBackFrom` (operation, when `rolled-back`), `modules: [ModuleState]` or `stateError` | admin | 401, 404 `not_found` |
| `/deployments/{name}/operations` | GET | `limit` (default 50, max 200), `cursor` (operation id) | `{items: [Operation], next}` newest first | admin | 401, 404 |
| `/deployments/{name}/review` | POST | body `{graph}` (1 MB max) | `Review`: `plan` (with `digest`), `modules: [{instance, module, image}]`, `bindings`, `publishedPorts: []`, `placed: [{path, instance, capability, field, exists}]` (`field` is `password` or `secret_key`) | admin | 422 `name_mismatch`, `reserved_name`, plan problems, `host_access`; 503 `deployer_unavailable`; 413 |
| `/deployments/{name}/apply` | POST | body `{graph, planDigest, placed: [{path, value, replace}]}` | 202 `Operation` | admin | 409 `busy`, `plan_changed`; 422 as review plus `placed_missing`, `placed_invalid`; 413 |
| `/deployments/{name}/remove` | POST | body `{deleteData: bool, confirmName?: string}` | 202 `Operation` | admin | 404; 409 `busy`, `already_removed`; 422 `confirm_mismatch` |
| `/events` | GET | `deployment`: name (opt) | `text/event-stream`: `snapshot` first, then `deployment`, `operation`, `modules` (only with the filter), `signed-out`; a `: ping` comment every 15 seconds | admin | 401; 429 `too_many_streams` (more than 8 per session) |

`Operation`: `id`, `action`, `status`, `step`, `startedAt`, `finishedAt`, `startedBy` (email), `error`, `rollbackOf`, `deleteData`, `placedSupplied`, `planDigest`. `ModuleState`: `instance`, `module`, `state`, `exitCode`, `lastExitCode`, `health`, `restartCount`, `image`, `since`.

**Apply dispatch order** (in `api`): plan and compare the digest; `INSERT ... ON CONFLICT (name) DO NOTHING` the deployment and read its id; ask the planner's managed render with that id; insert the operation as `running` and set it as latest (one transaction; the partial unique index answers `busy`); then call the deployer. A synchronous refusal (409 or 422) leaves no trace: `api` runs every check it can before inserting, and when the deployer itself refuses, `api` deletes the operation row it just inserted, and the deployment row too if this request created it, before answering.

| Deployer answer | `api` answer |
|---|---|
| 202 | 202 with the operation |
| 409 `busy` (the deployer still runs an operation `api` marked `unknown`) | 409 `busy` |
| 422 `host_access`, `invalid_project`, `invalid_path`, `placed_missing`, `placed_invalid` | 422 with the same problems |
| 401, 404 on a remove | 502 `deployer_error` (a wiring fault, logged), resp. 404 |
| no answer, 5xx, timeout (10 seconds) | operation recorded `failed` with `deployer_unavailable`, 202 with it |

**Polling failures**: a poll that fails is retried at the normal interval; after 60 seconds of failed polls, or 20 minutes after `started_at`, `api` marks the operation `unknown`.

**Background caller**: calls without a request (poller, rollback, retry) carry an `MP-Caller` for the operation's `started_by_iss` and `started_by_sub`; state reads for a watched deployment carry the `sub` of the session that opened the oldest stream. Event payloads (each a schema in `api.yaml`, `data:` is one JSON object): `snapshot` without the filter is `{deployments: [DeploymentSummary]}`, with the filter it is the full `Deployment` with `modules`; `deployment` is a `DeploymentSummary`; `operation` is `{deployment, operation: Operation}`; `modules` is `{deployment, modules: [ModuleState]}`; `signed-out` is `{reason: expired | signed_out}`. The event stream never replays; a reconnect gets a new `snapshot`. A stream does not count as activity for `session-idle`; it checks its session every 30 seconds and ends with `signed-out`. The SSE handler is a plain `net/http` handler mounted beside the strict server, since an endless response does not fit a strict handler.

The `planner.yaml` changes and `deploy-target.yaml` are in *Cross child contracts* and child [0007](0007-deploy-target-deployer.md).

### Value sourcing

| Action | Value produced / displayed | Source |
|---|---|---|
| Sign in | provider address | `MP_OIDC_ISSUER` = `${PLATFORM_ORIGIN}/auth` (spec 0001); inside Docker the name resolves to `ingress` through its network alias `PLATFORM_DOMAIN` on `edge`; with `PLATFORM_TLS=internal` `MP_OIDC_CA_FILE` is Caddy's local root from `caddy_data`, mounted read only (spec 0001) |
| Sign in | `PLATFORM_ORIGIN` | written by `setup.sh`: `https://${PLATFORM_DOMAIN}`, plus `:${PLATFORM_HTTPS_PORT}` when not 443; also `MP_PUBLIC_ORIGIN` |
| Sign in | admin match | `email` and `email_verified` from the provider's userinfo answer (Authelia puts them there, not in the ID token), else from the ID token, against `MP_ADMIN_EMAIL`; then `admin_binding.iss` plus `sub` |
| Sign in | session token, `expires_at` | 32 bytes from `crypto/rand`; `now()` + `session-max` (config) |
| Sign in | `prompt=login` | presence of the `__Host-mp_relogin` cookie set by sign out |
| Any admin route | session still valid | `sessions.expires_at`, and `last_seen_at` + `session-idle` (config) |
| `GET /session` | `email`, `expiresAt`, `idleExpiresAt` | `sessions.email`, `sessions.expires_at`, `last_seen_at` + `session-idle` |
| Sign out | next URL | discovery `end_session_endpoint` (may be absent; Authelia lists none), `sessions.id_token`, `MP_PUBLIC_ORIGIN` + `MP_BASE_PATH` + `/` |
| Signed out view | `index` or `sign-in` | `<meta name="mp-signed-out">` written by the `web` image from `MP_SIGNED_OUT`, from `.env` `PLATFORM_SIGNED_OUT` |
| Signed out view | reason text | `signed_out` query value from the callback redirect |
| Review | modules and images | planner managed render `images`, from each catalog entry's `image` |
| Review | bindings | `plan.bindings` of the planner's answer |
| Review | published ports | the rendered Compose project (always empty in `v0alpha1`) |
| Review | placed list and `exists` | managed render `secrets` entries with `kind: placed` (each carries `path`, `instance`, `capability`, `field`), against the deployer's `GET /v1/projects/{project}/secrets`, whose paths use the same `secrets/<instance>/<name>` form |
| Review, apply | `digest` | `libs/planner.Digest`: SHA-256 over the canonical graph, the sorted bindings and the catalog entries the graph uses |
| Apply | deployment id, `project` | existing `deployments` row, else `uuidv7()`; `'mp-' \|\| name` |
| Apply | `started_by_*` | the session's `iss`, `sub`, `email` |
| Apply | files and secret list | planner `POST /v1/render/managed` with `{graph, deployment}` |
| Apply | secret owner | deployer config `platform-uid`, `platform-gid`, from `PLATFORM_UID`, `PLATFORM_GID` that `setup.sh` writes (`id -u`, `id -g`) |
| Apply, review | reserved ports | planner and deployer config `platform-http-port`, `platform-https-port`, from `PLATFORM_HTTP_PORT`, `PLATFORM_HTTPS_PORT` |
| Progress | `status`, `step`, `error` | deployer `GET /v1/operations/{id}`, copied into the operation row |
| Progress | `unknown` | deployer `bootId` differs from `operations.deployer_boot_id`, or a deployer 404 for the operation under the same boot id, or `api` start |
| Deployment view | `state` | derived from the latest operation (table above) |
| Deployment view | module states | deployer `GET /v1/projects/{project}/state`, mapped as in [0007](0007-deploy-target-deployer.md); expected instances from the graph of the latest apply that reached step `up` (running, succeeded or failed); none while `removed` |
| Background calls | `MP-Caller` subject | the operation's `started_by_iss`, `started_by_sub`; for watched state reads, the session of the oldest open stream |
| Deployment view | `graph` | `graph` of the most recent `succeeded` apply (`LatestSucceededApply`); absent when none succeeded |
| Deployment view | `latestApplyGraph` | `graph` of the newest `operations` row of the deployment with `action = 'apply'`, any status, ordered by `started_at DESC, id DESC` (new query `LatestApply`); a rollback apply counts, so after a failed rollback it is the last good graph |
| *Apply again* | graph in review | `Deployment.latestApplyGraph` from a fresh `GET /deployments/{name}` made on click (never the cached copy, which another tab may have outdated), sent to `/review` like any graph; the button is hidden when the field is absent (between the deployment insert and the operation insert of a first apply, see *Apply dispatch order*) |
| *Remove again* | *Delete data too* preset | `latestOperation.deleteData`; `confirmName` is still typed by you |
| Deployment view | `rolledBackFrom` with step and error | the operation named by the latest operation's `rollback_of` |
| History | who | `operations.started_by_email` |
| Rollback | graph to apply | `graph` of the most recent `succeeded` apply before the failed one |
| Deployer log | caller | `sub` claim of the `MP-Caller` token |

### Key invariants

- At most one `running` operation per deployment (partial unique index); the deployer also refuses a second operation per project (409).
- An operation's final status never changes; history rows are never updated after it, never deleted.
- No secret or placed value is ever stored by `api`, logged, or sent back in any response; only paths travel back.
- Only the bound `iss` plus `sub` ever gets a session, and `admin_binding` has at most one row.
- A deployment's name equals its graph's `name`, and its project is always `mp-<name>`; the name `modular-platform` never exists.
- The deployer applies only what the guard accepts, whatever `api` or the planner said.
- Nothing outside `<deployments-dir>/<project>/` and the Compose project `<project>` is touched by an operation on that deployment; a deployed project never joins a network or mounts a volume it did not create (no `external` resources).
- `v0alpha1` cannot express a one shot module, so `up --wait` never meets a service meant to exit; #3 must decide how one shot modules wait.

### Security model

- One role in thread 1: the bound admin may do everything under `/api/v1`; nobody else gets a session. Roles arrive with #14.
- The browser holds only the session cookie and the login cookies; tokens from the provider never reach it.
- `Origin` check on every mutating route; SameSite=Lax cookies; `return_to` limited to paths under `<base>/` (no open redirect).
- `planner` and `deployer-docker` sit on `internal` only, take binding tokens (spec 0005), and check `MP-Caller` from `api`. The deployer is the only holder of the Docker socket and runs as root; its guard is the last line against host access by an imported graph.
- Platform services talk TLS with certificates from `cert-issuer` (child [0008](0008-managed-tls-cert-issuer.md)). Known gaps in thread 1: `api` to `postgres` stays plain on `internal` (as 0005 states), and deployed SDK modules run plain with binding tokens, exactly like their static zip.
- Placed credentials travel browser to `api` over HTTPS and `api` to deployer over TLS, once, and land as 0600 files.
- Logs: sign in outcomes with `iss`, `sub` and the reason; operation start and end with deployment, operation id, action, status, step and the caller's `sub`. Never emails in logs (they are personal data and live in the database), never request bodies, never a secret.
- No compliance scope (no payments or health data); emails of the admin are the only personal data, kept in `sessions`, `admin_binding` and `operations`.

### Configuration required

New settings in the managed `.env` (written by `setup.sh`, defaults in brackets):
- `PLATFORM_DIR`: absolute path of the platform folder (written by `setup.sh`); the deployments folder is `${PLATFORM_DIR}/deployments`, mounted at the same path in the deployer.
- `PLATFORM_UID`, `PLATFORM_GID`: the installing user's ids (`id -u`, `id -g`), owner of deployed secret files and `MP_UID`/`MP_GID` of deployed SDK modules.
- `PLATFORM_SESSION_IDLE` [`1h`], `PLATFORM_SESSION_MAX` [`12h`]: session lifetime.
- `PLATFORM_SIGNED_OUT` [`index`]: `index` or `sign-in`.

New module config values (declared in each catalog entry, read only through `sdkgen` getters):
- `catalog/api.yaml`: `oidc-issuer` (`MP_OIDC_ISSUER`, url), `oidc-client-id` (`MP_OIDC_CLIENT_ID`, default `mp-api`), `oidc-client-secret` (`MP_OIDC_CLIENT_SECRET`, secret), `oidc-ca-file` (`MP_OIDC_CA_FILE`, optional path), `public-origin` (`MP_PUBLIC_ORIGIN`, url), `base-path` (`MP_BASE_PATH`, pattern, default empty), `admin-email` (`MP_ADMIN_EMAIL`), `proxy-hops` (`MP_PROXY_HOPS`, default 1), `session-idle` (`MP_SESSION_IDLE`, duration, default 1h, min 5m, max 24h), `session-max` (`MP_SESSION_MAX`, duration, default 12h, max 720h); uses `planner` (http client), `deploy-target` (http client), `postgresql` (pg client). The `session-max` at least `session-idle` rule is checked in `api`'s setup and logged as a config problem line.
- `catalog/deployer-docker.yaml`: `deployments-dir` (`MP_DEPLOYMENTS_DIR`, absolute path), `platform-uid`, `platform-gid` (`MP_PLATFORM_UID`, `MP_PLATFORM_GID`, int, min 0), `platform-http-port`, `platform-https-port` (int, defaults 80 and 443), `pull-timeout` (default 10m), `wait-timeout` (default 5m); provides `deploy-target` with `auth: binding-token`.
- `catalog/planner.yaml`: adds `platform-http-port`, `platform-https-port` (int, defaults 80 and 443) for the guard in managed render.
- `web` image: `MP_SIGNED_OUT` (`index` or `sign-in`) and `MP_MODE` (`online` or `managed`), templated into `<meta name="mp-signed-out">` and `<meta name="mp-mode">` like `MP_BASE_PATH`.

Secrets (created by `setup.sh`, child [0009](0009-managed-zip-install-smoke.md)): the two binding tokens of `api`, the `api` OIDC client secret, the caller key pair, the CA and its password, the Postgres passwords, and Authelia's own.

### Editor screens (managed mode only, on the current styling)

- **Signed out**: on any 401 the editor shows the index with *Sign in* (or goes to `/api/v1/auth/login` with `sign-in`), plus the reason text from `?signed_out=`.
- **Header**: *Deployments* link, the admin's email, *Sign out*.
- **Graph screen**: *Deploy* beside *Download zip*. It opens **plan review** as a dialog: modules with images, bindings, published ports (none), each missing placed credential as a password field, each existing one with *Replace*; *Deploy* is enabled once every missing value is filled. On 409 `plan_changed` the dialog reviews again.
- **`/deployments`**: list with name, state badge, latest operation and time; paginated.
- **`/deployments/$name`**: the graph with a state badge on each module node (text plus color; selecting a node shows image, health and restart count), the running operation's step in a live region, a `rolled-back` banner linking the failed operation with its step and error, a `failed` or `unknown` banner with *Apply again* (opens review with `latestApplyGraph`, the newest apply's graph whatever its status) and, when the latest operation is a remove, *Remove again* beside it (the remove dialog with *Delete data too* preset), the history list, and *Remove* (dialog with *Delete data too*, which asks you to type the name).
- Data: TanStack Query for reads, one `EventSource` per open screen updating the query cache; Zustand only for editor state, as today.

### Cross child contracts

- **Names**: deployment name = graph `name` (0002 rules), never `modular-platform`; project `mp-<name>`; SDK host names in deployed graphs `<instance>.<name>.internal` (0005), on the platform `<instance>.modular-platform.internal`.
- **Paths**: deployment folder `<deployments-dir>/<project>/`, the same absolute path on host and in the deployer; inside it the render's own layout (`docker-compose.yml`, `.env`, `graph.yaml`, `README.md`, `config/`, `secrets/<instance>/`) plus `deploy.env` written by the deployer (`MP_UID`, `MP_GID`).
- **Labels** (prefix `io.github.racherom.modular-platform.`): `graph` (graph name), `instance`, `module` (catalog module name), `deployment` (deployment id; `platform` on the platform's own services).
- **Managed render** (`libs/render`, pure, new `RenderManaged(plan, catalog, deployment)` beside `Render`): the static output plus the labels on every service, `restart: unless-stopped` on every service, no `MP_UID`/`MP_GID` lines in `.env` (the deployer adds `deploy.env`), and a machine readable list of secrets `[{path, kind: generated | placed}]`. `render.Guard(project, reservedPorts) []Problem` is pure and shared by the planner and the deployer.
- **`planner.yaml`**: `PlanResult.digest` (`sha256:<hex>`, set when the plan is ok); `POST /v1/render/managed` with `{graph, deployment}` answers `RenderedProject {project, plan, images, publishedPorts, secrets: [{path, kind, instance, capability, field}], files: [{path, mode, data (base64)}]}` or 422 `PlanResult` (plan problems or `host_access`); internal mode only, 404 in public mode. `capability` and `field` are set for placed entries only.
- **`plan_changed` message**: the editor says the plan changed since review (for example after a platform upgrade changed the catalog) and reviews again.

### Critical test scenarios

- Happy path: the `smoke-managed` job installs, signs in, reviews and deploys the sample, sees every module `running`, removes with *Delete data too*, verifies **AC-1**, **AC-2**, **AC-7**, **AC-9**, **AC-13**, **AC-15**, **AC-19**.
- Rollback: an integration test applies the sample, then a changed graph whose new module never gets healthy (a test image), and expects the failed apply, one rollback and `rolled-back`; a second case makes the rollback fail and expects exactly one retry after 30 seconds, verifies **AC-14**.
- Concurrency: two `api` processes on one database start an apply of the same deployment at once; exactly one gets 202, verifies **AC-9**.
- Restarts: kill the deployer mid apply and expect `unknown`; restart `api` mid apply and expect it to resume polling and see the outcome; stop the deployer for 60 seconds and expect `unknown`; after an `unknown` apply of a changed graph, *Apply again* reviews the changed graph (not the running one) and succeeds; after a failed first apply it reviews that graph; after a failed rollback it reviews the last good graph; a failed remove offers *Remove again* and *Apply again*; after a failed *Delete data too* remove, *Apply again* lists the lost placed credentials as missing in review; while the deployer still runs an `unknown` apply, *Apply again* answers 409 `busy`, and once that apply has finished it succeeds; *Apply again* inside a failed rollback's 30 second window cancels the retry, verifies **AC-14**, **AC-16**.
- Guard: table tests of `render.Guard` with one case per refused key (including an `external` network named `modular-platform_internal`), a golden test that every rendered key passes, plus a direct deployer call with a crafted project, verifies **AC-8**.
- Secrets: no generated or placed value in logs, responses or the database (a test greps all of them for the values), verifies **AC-11**.
- Auth: a second identity at Authelia gets `not_admin`; a mutating call without `Origin` gets 403; an expired session gets 401 and its stream gets `signed-out`; `admin reset` lets the email bind again, verifies **AC-2**, **AC-3**, **AC-5**, **AC-6**.

## Build plan

Tracer Bullet: milestone 1 runs the whole done line on the thinnest path (platform services plain, with binding tokens and `MP-Caller`, as deployed graphs run); each later milestone thickens one strand of it. TLS for the platform's own services has its own milestone, so the unverified issuer port never blocks the first thread.

**Milestone 1: the thread runs (install, sign in, deploy the sample, see it running)**
1. Spike first: Authelia's `email` and `email_verified` in userinfo for file users, `prompt=login`, `authorization_policy: one_factor` with `consent_mode: implicit` for `mp-api`, and `compose up --wait` for services without a healthcheck on the pinned Compose; record the results in [rationale.md](rationale.md), satisfies **AC-2**, **AC-9**.
2. Contracts: `api.yaml` (auth routes, `/session`, the planner operations, deployments read, review, apply, `/events` with its payloads), `deploy-target.yaml` (apply, operation, state, secrets), the `planner.yaml` additions; `task generate`, satisfies **AC-18**.
3. `libs/planner.Digest`, `libs/render.RenderManaged` with labels, restart policy and the secrets list (with `instance`, `capability`, `field`), golden files, satisfies **AC-10**, **AC-12**.
4. `catalog/api.yaml`, `catalog/deployer-docker.yaml`, the planner's new values; `services/api` on `sdk.Run` with OIDC sign in (claims from userinfo), the atomic admin binding, sessions with idle and max, the Origin check, `MP-Caller` on every call, and the one migration for all four tables, satisfies **AC-2**, **AC-3**, **AC-5**.
5. `services/deployer-docker`: apply (prepare, pull, up with `--wait`), generated secrets, operation status with boot id, state read, the caller required on mutating routes, satisfies **AC-5**, **AC-9**, **AC-11**, **AC-13**.
6. `deploy/managed/` (plain mode for now) with `setup.sh` writing `PLATFORM_ORIGIN`, the caller key pair, literal image tags, `task dev:managed`, `task managed:zip` (child 0009), satisfies **AC-1**, **AC-19**.
7. Editor: managed mode meta, signed out view, *Deploy* with a first review dialog, the deployment screen over SSE with module badges, satisfies **AC-2**, **AC-7**, **AC-13**.
8. `smoke-managed` CI job for the done line, satisfies **AC-19**.

**Milestone 2: review you can trust**
9. Full review answer (placed list with `exists`, published ports), `plan_changed` with its message, `name_mismatch`, `reserved_name`, the deployer error table, satisfies **AC-7**, **AC-10**.
10. `render.Guard` as an allow list in the planner's managed render and again in the deployer, with the golden "every rendered key passes" test, `invalid_project` from labels, satisfies **AC-8**.
11. Placed credentials end to end (dialog fields, *Replace* written only after the pull, path rules, size and NUL rules, written once), satisfies **AC-11**.

**Milestone 3: lifecycle and recovery**
12. Remove with *Delete data too*, `confirm_mismatch`, `already_removed`, `busy` across processes, satisfies **AC-9**, **AC-15**.
13. `unknown` on a new boot id, after 60 seconds of failed polls or 20 minutes, resumed polling at `api` start, `deployer_unavailable`, `Deployment.latestApplyGraph` in `api.yaml` (then `task generate`) with the `LatestApply` query, *Apply again* on it and *Remove again*, files restored after a failure before `up`, satisfies **AC-14**, **AC-16**.
14. Automatic rollback (plan checked, digest skipped) with one retry after 30 seconds, the `rolled-back` and "no earlier version" banners and history, satisfies **AC-14**.

**Milestone 4: TLS for the platform's own services**
15. Spike: step-ca validating tls-alpn-01 on 8443 as a binary, else the small issuer image from step-ca's Go packages; its answer to a lost ACME account and its `NotBefore` backdate (spec 0005); record in [rationale.md](rationale.md), satisfies **AC-17**.
16. `cert-issuer` with its `cert_issuer_db` volume, the CA in `setup.sh`, TLS mode for `api`, `planner` and `deployer-docker`, Caddy to `api` over TLS (child 0008), satisfies **AC-17**.

**Milestone 5: live state, sessions, the admin and the release**
17. Poll cadence (1 second while an operation runs, 5 seconds while watched, none otherwise), one poller per deployment shared by streams, the full badge set with restart count and last exit code (the deployer's `die` event stream and its map, child 0007; the `lastExitCode` descriptions in `deploy-target.yaml` and `api.yaml` say "the code of the most recent exit, any cause; absent when unknown", then `task generate`), list and history pagination, the 8 stream cap, satisfies **AC-9**, **AC-13**.
18. Sign out with `end_session_endpoint` or `prompt=login`, `signed-out` events, the session sweep, `PLATFORM_SIGNED_OUT`, `admin reset` (an `sdk.Run` option for a command that loads config and bindings, then runs instead of serving), satisfies **AC-3**, **AC-4**, **AC-6**.
19. *Download zip* in managed mode, Docker restart check, satisfies **AC-12**, **AC-20**.
20. Release attaches the managed zip (release-please extra files bump the literal image tags in `deploy/managed/compose.yaml`); the managed README (plain language), satisfies **AC-19**, **AC-1**.

## Consequences

**Positive**:
- The whole managed promise (install, sign in, deploy, see state) runs end to end before the real contract (#3) is written, so #3 is designed against a working platform.
- The deployer stays a small executor: no database, no planning, no history; most bugs land in `api`, which holds no host power.
- Plan review shows what will run, and the digest proves it is what runs.
- A failed change heals itself, and the history explains what happened.
- The platform's own services talk TLS from day one, through a module you can swap later (#23).

**Negative / tradeoffs**:
- Eight long running containers plus `db-init`, one more than spec 0004 planned, because `cert-issuer` is its own module.
- Automatic rollback runs code paths you rarely see; a rollback that applies an old graph against a newer catalog can fail in surprising ways (it is retried once, then shown).
- With the bundled Authelia there is no protocol level sign out; `prompt=login` forces the password again but leaves the Authelia session cookie until it expires.
- tls-alpn-01 on 8443 is outside RFC 8737's port 443; if step-ca cannot do it as a binary, the fallback is a small issuer image built from step-ca's Go packages, which the project then maintains.
- Placed credentials pass through `api`'s memory once; a flaw there could log or leak them, so tests grep for them.
- Polling misses crashes while nobody watches (accepted by spec 0004), and in flight progress is lost on a deployer restart. So are the last exit codes the deployer saw: after its restart a badge shows the restart count but no last exit code until the next exit.
- Deployed SDK modules run plain with binding tokens; only isolation per project network protects their traffic until a later feature brings TLS to deployed graphs.
- The guard is an allow list and blunt by design: any key `RenderManaged` does not emit is refused, including legitimate ones, and every new render feature must widen the list on purpose, until #8 and #13.
- Until milestone 4 the platform's own services run plain on `internal`; the first thread is proven without TLS, and AC-1 and AC-17 hold in full only once milestone 4 lands.
- After *Delete data too* the rendered files stay in the deployment folder but its secrets are gone, so a hand run `docker compose` there fails; the README says so.

**Neutral**:
- `libs/render` gains a managed flavor; static output does not change (golden files stay).
- `planner.yaml` changes are additive; the online editor ignores `digest`.
- The SDK gains a command option for `api admin reset`.
- Specs 0001, 0004 and 0005 need small pointer updates (Follow-up).

## Follow-up

- [x] (done 2026-11-03) On confirmation, update spec 0001 (*Managed platform* and *Managed mode wiring*: `cert-issuer` as a module, eight containers, the deployments folder under `PLATFORM_DIR` instead of `/var/lib/modular-platform`), spec 0004 (*First managed thread* points here; seven becomes eight; `api` at start now resumes polling an operation whose boot id still matches instead of marking every running operation `unknown`) and spec 0005 (its #16 follow ups answered here: a separate `cert-issuer`, not embedded in the deployer; deployed graphs stay plain).
- [x] (enrolled as scope #31, 2026-11-03) Enroll a scope feature for TLS in deployed graphs (an issuer per deployment or one joined to each project network), after #3.
- [ ] Spike results (Build plan tasks 1 and 15) go into [rationale.md](rationale.md): Authelia userinfo claims, `prompt=login`, `one_factor` and implicit consent, Compose `--wait` behavior; step-ca on 8443, its answer to a lost ACME account and its `NotBefore` backdate (spec 0005 asked).
- [x] (answered by spec [0011](../0011-capability-module-contract/0014-provisioners-jobs.md), 2026-11-04: only provision and revoke jobs run once; consumers wait with `service_completed_successfully`; the `--wait` behavior is build task 1 of 0011) #3 decides how one shot modules (jobs that exit) are expressed and waited for, since `up --wait` treats an exited service as a failure.
- [ ] #4 inherits the four tables and decides retention for `operations` and whether files or the database are the source of truth.
- [ ] #14 replaces `admin_binding` with roles; #15 builds the audit log on `operations` and sign in events.
- [ ] #19 owns real rollback semantics for live changes; this spec's rollback is a whole graph reapply.
- [ ] `/sync` should add `services/api/AGENTS.md` and `services/deployer-docker/AGENTS.md` once they exist, and note `deploy/managed/` in root `AGENTS.md`.
