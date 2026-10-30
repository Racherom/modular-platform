# 0001. Platform stack and architecture

**Date**: 2026-10-29
**Status**: In Progress

## Structure

- [0002. Provisional graph file, catalog entry and planner matching rules](0002-provisional-graph-format.md): the `v0alpha1` `graph.yaml` and catalog entry shapes, the planner's `uses` to `provides` rules, and the sample's one edge; supports the catalog, graph files and sample rows below and AC-2, AC-3, AC-4, AC-7 (adds AC-8 to AC-12).

## Summary

The platform comes in two forms built from the same Go services. In static mode, users design their stack in an online tool you host on your own network (the editor plus a stateless planner service that stores nothing), download a zip with a Compose file and config files, run a setup script and `docker compose up`. In managed mode, users download a ready made zip of the platform itself (the same planner plus an API and a deployer, with Postgres, NATS, Authelia and Caddy), start it on their host, and plan and deploy their apps in their own planner. Because both forms run the same `planner` service behind the same contract, a stack planned online and one deployed by the platform come out the same; slice 1 is the static path, the rest of the managed services are scaffolded with feature #16. Every module the platform ships is only a default: platform services learn everything about a provider through its binding, and a provider's setup steps travel with its module, so you can swap any of them (#23). The website also works under a path of a site you already have (one base path setting, #24).

## Decision

**Chosen option**: Option 1: shared Go services, a hosted stateless planner for static mode, the platform as a graph for managed mode (see [rationale.md](rationale.md)).

Build the core (`libs/planner`, `libs/render`, `libs/contract`) in Go and serve it through one `planner` service that runs publicly (stateless, rate limited) behind the online tool and internally inside the managed platform, where `api` and `deployer-docker` join it and the platform is itself a graph of exchangeable modules wired over standard protocols.

**Implementation skills**: `golang-project-layout` · `golang-code-style` · `golang-naming` · `golang-error-handling` · `golang-context` · `golang-concurrency` · `golang-structs-interfaces` · `golang-database` · `golang-security` · `golang-observability` · `golang-lint` · `golang-testing` · `golang-cli` · `golang-continuous-integration` (all `samber/cc-skills-golang`, `.claude/skills/<name>/`) · `supabase-postgres-best-practices` (`supabase/agent-skills`, `.claude/skills/supabase-postgres-best-practices/`) · `openapi-spec-generation` · `github-actions-templates` · `react-state-management` (all `wshobson/agents`, `.claude/skills/<name>/`) · `vercel-react-best-practices` (`vercel-labs/agent-skills`, `.claude/skills/vercel-react-best-practices/`) · `vite` (`antfu/skills`, `.claude/skills/vite/`) · `react-flow` (`existential-birds/beagle`, `.claude/skills/react-flow/`) · `tanstack-query` · `tanstack-router` (both `tanstack-skills/tanstack-skills`, `.claude/skills/<name>/`) · `multi-stage-dockerfile` (`github/awesome-copilot`, `.claude/skills/multi-stage-dockerfile/`)

Skill caveats: `golang-continuous-integration` mentions GoReleaser; releases here use release-please. `vercel-react-best-practices` partly targets Next.js; this editor is a plain Vite single page app, so skip its server rendering advice. `golang-cli` is installed but unused until #12 decides on a command line.

## Rationale

Reasoning and options: see [rationale.md](rationale.md).

## Architecture principles

These hold for every later spec. Detailed formats belong to the specs named in brackets.

1. **One planner service, two placements.** Planning (`libs/planner`) and rendering (`libs/render`: plan to Compose project, config files, setup script, zip) are pure Go libraries. The `planner` service wraps them behind one OpenAPI contract and runs in **public mode** behind the online tool and in **internal mode** inside the managed platform. The same graph gives the same plan and the same rendered files in both.
2. **The platform is a graph.** The online tool (`ingress`, `web`, `planner`) and the managed platform (adding `api`, `deployer-docker` and backing modules) are graphs of modules with manifests in the built in catalog, declaring what they provide and use, like any third party module. Their Compose files are rendered graphs. (manifest format: spec #3)
3. **Standard protocol per interaction.** Each connection uses the existing protocol that fits it (REST for requests, NATS for events, PostgreSQL wire for data, OIDC for sign in). No house protocol.
4. **Provisioning capabilities.** A provider can offer a capability that creates sub capabilities, for example `postgresql-provisioner` creates a database plus role and yields a `postgresql` capability for one consumer. The **planner plans** the provisioning step (pure, no side effects); it **runs at deploy time** as a one shot init service in the rendered Compose project (`depends_on: condition: service_completed_successfully`), so a downloaded zip and a managed deploy behave identically. (capability format: spec #3; matching rules: spec #6)
5. **The deploy target is swappable.** `libs/target` defines one Go interface. Implementations: `zip` (static mode, served by `planner` as the render endpoint) and `deployer-docker` (managed mode, a service behind the `deploy-target` REST contract that applies the same rendered project to the host). A cluster target is another implementation.
6. **Secrets are born on the user's host.** No secret value is ever in a zip, in a graph file, or seen by the planner. The zip's `setup.sh` creates them; Compose mounts each service's own secret files only.
7. **The public planner keeps nothing.** In public mode the planner has no accounts, no database, no disk writes, and never logs request bodies; each request carries the whole graph.
8. **Per binding credentials (managed).** Every binding between platform modules carries a generated credential (bearer token, NATS user, Postgres role). No managed service accepts unauthenticated calls from the network; network isolation is a second layer.
9. **Every platform module is a default.** No platform service names an image, a host name or a provider specific file. Addresses, the OIDC issuer URL, an optional CA bundle (the certificate authority file used to trust a private TLS certificate) and credentials reach a service only as settings filled from its bindings. Provider specific setup (a password hash, a provisioning script, an ingress config) ships in that provider's module manifest: `libs/render` assembles `setup.sh` and the one shot init services from per module fragments. A fragment is a command run inside the module's own image (`docker run --rm`, no terminal, as your own user via `--user $(id -u):$(id -g)`) with its own `secrets/<module>/` folder mounted read write plus, read only, just the secret files its bindings declare (for example Authelia reads the `api` client secret to compute its digest); module authors never contribute raw host shell. `setup.sh` itself asks every question a fragment declares (secret answers without echo, passed to the fragment on stdin, never as environment variables). Order: platform level secrets first, then fragments in graph order; any failure stops the script (`set -e`). (fragment and binding format: spec #3; install time replacement: spec #23)
10. **Mountable under one base path.** Every browser facing platform service (`web`, `api`, the bundled identity provider) takes the public origin and one base path (empty for the site root, else like `/platform`) and serves, links, redirects and scopes cookies under it. Proxies forward the path unchanged (no prefix stripping) to those services, so redirects and OIDC callback URLs are right in both placements. Services that build no URLs (`planner` in public mode) may sit behind a stripped prefix. (detail: spec #24)
11. **Client IP by proxy hop count.** A service that needs the client IP (rate limits, logs) never trusts the leftmost `X-Forwarded-For` entry (a client can forge it) or a specific ingress product's settings. The algorithm: join every `X-Forwarded-For` header line, split on commas, trim; the client IP is the entry `MP_PROXY_HOPS` places from the right (default 1, the ingress, so the last entry); if there are fewer entries than that, or the value does not parse as an IP, use the TCP peer address.
12. **Origin from configuration only.** Services derive their public origin and scheme from configuration (`MP_PUBLIC_ORIGIN`), never from `Host`, `X-Forwarded-Host` or `X-Forwarded-Proto`, and always set `Secure` on cookies.

## Proposed stack

### Shared core and planner service (slice 1)

| Layer | Choice | Reason |
|---|---|---|
| Core language | Go 1.26 or newer | Container ecosystem libraries (compose-go, moby client) are Go; static binaries; nats.go v1.54+ requires 1.26 |
| Planner library | `libs/planner`, pure Go, no I/O | Shared by the planner service in both placements |
| Renderer | `libs/render`: `compose-spec/compose-go` v2 types for the Compose file, `text/template` for config files and `setup.sh`, `archive/zip` for the zip | Zip export and managed deploy share one render path by construction |
| Deploy target interface | `libs/target` with implementations `zip` (now) and `deployer-docker` (from #16) | The swappable target the scope asks for |
| Planner service | `services/planner`: `GET /v1/catalog`, `POST /v1/plan`, `POST /v1/render` (returns the zip), `/healthz`, `/readyz`; contract `contracts/openapi/planner.yaml` | One contract for the online tool and the managed API |
| Planner modes | `MP_PLANNER_MODE=public`: no auth, per client IP rate limit (`golang.org/x/time/rate` token bucket refilling `MP_PLANNER_RATE` tokens per minute, burst 10; keyed by the full IPv4 address or the IPv6 /64 network, client IP per principle 11; idle entries dropped after 10 minutes, in memory only; `/healthz` and `/readyz` exempt), request body limit 1 MB (413), 10 second deadline per request, no body logging. `MP_PLANNER_MODE=internal`: requires `MP_PLANNER_TOKEN_FILE` and refuses to start without it | Safe on the internet and safe inside the platform, with no silent open mode |
| Built in catalog | `catalog/` source compiled at build time into the planner (embedded) and served by `GET /v1/catalog`; managed requests may add shared catalog entries (#13). Until spec #3 exists, the scaffold uses the provisional `v0alpha1` entry format pinned in child spec [0002](0002-provisional-graph-format.md), replaced by #3 | The catalog is versioned with the release |
| Graph files | `graph.yaml` text files saved and opened by the editor, included in every zip; provisional `v0alpha1` format and planner matching rules in child spec [0002](0002-provisional-graph-format.md), replaced by #3, #4 and #6 | Reopen and change a stack later; ready for version control (#12) |
| HTTP | `net/http` ServeMux (method and path patterns), no framework | Standard library is enough for generated handlers |
| API contracts | OpenAPI 3.1, spec first, in `contracts/openapi/` | Reviewed as contracts; public for automation |
| Codegen | `oapi-codegen` (strict server on std `net/http`, typed Go clients); `openapi-typescript` plus `openapi-fetch` for the editor | Typed on both sides from one contract |
| Observability | JSON logs via `log/slog`; `/healthz` and `/readyz` per Go service; a `healthcheck` subcommand in each binary for container healthchecks; OpenTelemetry later as an optional OTLP capability | Distroless images have no curl |
| Images | Go services on `gcr.io/distroless/static-debian12:nonroot`, multi stage builds from the repo root so `go.work` applies | Small, nonroot |
| License | Apache 2.0 | Permissive with a patent grant; the container ecosystem norm |

### Online tool (static mode, slice 1)

| Layer | Choice | Reason |
|---|---|---|
| Shape | Graph of three modules on one origin: `ingress` (Caddy) routes `/` to `web` and `/planner/*` to `planner` (public mode) | Same origin, so no CORS; the same shape as the managed platform |
| Hosting | Your own network, as a Compose stack in `deploy/online/`; public domain via `PLATFORM_DOMAIN`, TLS via ACME (`PLATFORM_TLS=acme`); only the ingress exposes 80 and 443 | You control the infrastructure; the online tool dogfoods a rendered graph |
| Frontend | React with TypeScript (strict), built by Vite, managed with pnpm | Largest ecosystem for node editors and contributors |
| Web module | `caddy:<version>-alpine` based image, nonroot on 8080, serving the built app under `MP_BASE_PATH` (empty means the site root; `<base>` without the trailing slash redirects to `<base>/`). Caddy's `templates` directive applies to `index.html` only, filling `{{env "MP_BASE_PATH"}}` and `{{env "MP_API_ROOT"}}`; `index.html` is served `Cache-Control: no-cache`, hashed files under `assets/` as immutable; a missing file under `assets/` is a 404, never the app shell; every other path falls back to `index.html` | Same server software as the ingress; client side routes fall back to the app shell; one image works at the root and under a sub path |
| Base path in the editor | No `<base href>` (it breaks SVG `url(#id)` references such as React Flow edge markers, and hash links). Vite `base: './'` keeps JS and CSS references relative to their own file; a small Vite `transformIndexHtml` plugin writes the asset tags in `index.html` as `{{env "MP_BASE_PATH"}}/assets/...`. The template also writes `<meta name="mp-base-path">` and `<meta name="mp-api-root">`; TanStack Router `basepath` and the API client root read them at start. Planning paths (`/v1/catalog`, `/v1/plan`, `/v1/render`) are identical under either root | No rebuild per placement; the same bundle serves the online tool (`MP_API_ROOT=<base>/planner`) and managed mode (`MP_API_ROOT=<base>/api`) |
| Graph canvas | React Flow (`@xyflow/react` v12) | Standard node editor library: custom nodes, ports as handles, edges, selection |
| UI routing | TanStack Router, file based routes, browser history | Type safe routes and search params |
| UI state | Zustand for graph and editor state; TanStack Query for catalog, plan and render calls | Small, well known; React Flow already uses Zustand inside |
| Saving | Autosave to the browser (IndexedDB); explicit save and open as graph text files; the zip includes the graph file | A refresh loses nothing; nothing is stored on your side |
| Styling and components | Decided in spec #5 (design system) | Out of scope here |

### Downloaded zip (both modes)

| Item | Choice | Reason |
|---|---|---|
| Contents | `docker-compose.yml`, config files per module, the graph file, `setup.sh`, `.env` (non secret settings), `README.md`; never a secret value | Runs with standard open source tools alone |
| Secret creation | `setup.sh` (POSIX shell only): creates each missing secret file in `secrets/<module>/` with mode 0600, never overwrites an existing one. Plain random secrets it makes itself (32 random bytes as hex: `od -An -tx1 -N32 /dev/urandom \| tr -d ' \n'`); only special formats (for example Authelia's argon2 hash) run as a module fragment (principle 9), so the host needs only Docker | Secrets are born on the host; a second run is safe |
| Safety net | Every secret is a Compose `secrets:` file entry, so `docker compose up` fails with "file not found" if `setup.sh` was skipped | No silent start with empty passwords |
| Windows | Run `setup.sh` in WSL or Git Bash | One script to maintain; documented in the zip's README |

### Managed platform (scaffolded with #16)

| Layer | Choice | Reason |
|---|---|---|
| Architecture | `planner` (internal mode) plus `api` and `deployer-docker`, and the `web` module in its managed build, each its own image | Splits on real seams: stateless planning, privileged deploy (only holder of the Docker socket), all state in one place |
| Platform database | PostgreSQL protocol as a `postgresql` capability; default provider the official `postgres` image | Relational integrity for identity, roles, audit, history; JSONB for graph documents |
| DB access | `pgx` v5 plus `sqlc` generated queries | Plain reviewable SQL with compile time checked Go types |
| Migrations | `goose` v3, SQL embedded in `api`, run at startup through goose's Provider API with the Postgres session locker (the default provider does not lock) | sqlc reads the same files; concurrent starts are safe |
| Event bus | NATS with JetStream as a `nats` capability; official `nats` image; client `nats.go` | Exchangeable; progress survives restarts; reused by #17 to #19 |
| Events to browser | Server Sent Events from `api` | One way push over plain HTTP through the ingress |
| Sign in | OIDC Authorization Code with PKCE; `api` is a confidential client (`coreos/go-oidc/v3`, `golang.org/x/oauth2`) | The platform never stores passwords |
| Identity provider | Authelia as an `oidc` capability, users in its file backend, storage in a provisioned Postgres database | Light, full OIDC provider; the bundled default is the fallback for #14 and #18 |
| Sessions | Server side sessions in Postgres; cookies HttpOnly, Secure, SameSite=Lax, names prefixed `mp_`; `__Host-` prefix with `Path=/` at the site root, `__Secure-` prefix with `Path=<base>/` under a sub path; `api` cookies never carry a `Domain` attribute (the bundled Authelia sets its own cookie domain, see Sign in); tokens never reach the browser | Backend for frontend pattern; `__Host-` requires `Path=/`, which would spread platform cookies over the rest of your site |
| Ingress | Caddy as `http-ingress` (the bundled default): `<base>/` to `web`, `<base>/api/*` to `api`, `<base>/auth/*` to Authelia, prefix kept; `tls internal` by default | One origin for cookies and OIDC; your own reverse proxy can front it or replace it (#23, #24) |
| Editor in managed mode | The same app; `api` re-exposes the planner operations under `<base>/api/v1/` with the schemas of `planner.yaml`, so the editor's planning code differs only by its API root (`MP_API_ROOT`) | One editor codebase, one generated client shape |
| Deployer apply | `libs/render` output applied with the official `docker compose` CLI plugin in the deployer image | Identical artifact to the static zip |
| Deployer live operations | `github.com/moby/moby/client` (the old `github.com/docker/docker` path is deprecated) for state reads now, fine grained live changes when #19 needs them | Fills what Compose cannot do |
| Container runtime | Docker Engine tested in CI; Podman via its Docker compatible socket, best effort | Narrow first target |
| Docker socket | Mounted only into `deployer-docker`, no ingress route, binding credential required | Isolation by who can reach the deployer |
| Deployer image | `docker:<version>-cli` with an explicit `ENTRYPOINT` (the image's wrapper otherwise treats arguments as docker subcommands), running as root | Needs the CLI and compose plugin; root equivalent anyway through the socket |
| Distribution | The managed zip (`modular-platform-<version>.zip`, the platform graph rendered by `libs/render`) attached to each GitHub release and linked from the online tool | One download, same shape as a static zip |

### Repository, CI and releases

| Layer | Choice | Reason |
|---|---|---|
| Repository | One monorepo `github.com/racherom/modular-platform`; Go workspace (`go.work`); `web/` as a pnpm workspace; Taskfile for cross language tasks | One place for core, contracts and codegen |
| Codegen output | Generated Go and TypeScript is committed; CI runs `task generate` and fails on a diff | Reviewable diffs, builds without generator tools |
| CI | GitHub Actions on every pull request: generate diff, lint, test, build, and a `smoke` job (start `deploy/online/`, render the sample zip through the planner, `sh setup.sh`, `docker compose up -d`, wait for healthy) | A broken planner or zip is caught before merge |
| Releases | SemVer with release-please (manifest mode, one shared version); the tag builds multi arch images (`linux/amd64`, `linux/arm64`) to `ghcr.io/racherom/modular-platform/<service>` (`web` and `planner` from slice 1, the rest from #16) and, from #16, attaches the managed zip | One version pins core, images and zips together |
| Command line | None for now; feature #12 decides whether a CLI is worth it | Neither install path needs one |

### Repository layout

```
modular-platform/
  go.work
  Taskfile.yml                   generate, build, test, lint, dev, smoke, images
  release-please-config.json
  .release-please-manifest.json
  contracts/openapi/             planner.yaml (api.yaml and deploy-target.yaml added with #16)
  libs/                          Go module github.com/racherom/modular-platform/libs
    contract/                    module, capability, binding types (shapes from spec #3); until then the `v0alpha1` types, strict decoding, canonical writer and catalog loader (child spec 0002)
    planner/                     pure planning library, no I/O
    render/                      plan to Compose project, config files, setup.sh, zip
    target/                      Target interface; zip implementation
    platform/                    service kit: slog setup, health endpoints, env config, bearer auth, rate limit, client IP by hop count, base path
  services/
    planner/                     Go module, cmd/planner, internal/...
  web/                           pnpm workspace: the editor app
  catalog/                       built in catalog source, embedded into the planner at build
  samples/                       the fixed sample graph used by the scaffold and the smoke job: `traefik/whoami` plus the official `postgres` image with one generated password (`POSTGRES_PASSWORD_FILE`)
  deploy/
    online/                      the online tool graph: compose.yaml, compose.dev.yaml, Caddyfile, versions.env
  .github/workflows/
  LICENSE                        Apache 2.0 text
  SECURITY.md                    how to report a flaw privately
  .gitignore                     node_modules/, build output, any secrets/ directory
```

Added with #16: `contracts/openapi/api.yaml` and `deploy-target.yaml`, `services/api/`, `services/deployer-docker/` (each its own Go module), and `deploy/managed/` (the platform graph and its config templates). The root `package.json`, `package-lock.json` and `node_modules/` (a placeholder `nodejs` dependency) are replaced by the pnpm workspace in `web/`.

### Online tool wiring (slice 1, pinned)

- `deploy/online/compose.yaml`: project name `modular-platform-online`; services `ingress`, `web`, `planner`; one network `edge`; only `ingress` publishes ports; volumes `caddy_data`, `caddy_config`. Image versions pinned to exact tags in `versions.env`; never `latest`.
- Caddy routes `<base>/planner/*` to `planner:8080` with `<base>/planner` stripped, everything else under `<base>/` to `web:8080` with the path unchanged. The editor's API root is `<base>/planner`.
- Settings in `deploy/online/.env`: `PLATFORM_DOMAIN` (your public host name; `platform.localhost` for local runs), `PLATFORM_TLS` (`acme` in production, `internal` locally), `PLATFORM_ACME_EMAIL` (optional contact for the certificate authority), `PLATFORM_HTTPS_PORT` (443), `PLATFORM_HTTP_PORT` (80), `MP_PLANNER_RATE` (requests per minute per client IP, default 60), `MP_BASE_PATH` (empty).
- Container settings: `planner` gets `MP_PLANNER_MODE=public`, `MP_PLANNER_RATE`, `MP_PROXY_HOPS`; `web` gets `MP_BASE_PATH` and `MP_API_ROOT=${MP_BASE_PATH}/planner`.
- The planner takes the client IP per principle 11 with `MP_PROXY_HOPS=1`; set it to 2 when you add flood protection in front of the ingress (#22). The planner is reachable only through the ingress, so the counted hop is always yours.
- The online tool runs at the site root (`MP_BASE_PATH` empty); the `web` image's base path support is still exercised by the smoke job (AC-7) so #24 reuses it unchanged. For that, `deploy/online/compose.subpath.yaml` overrides `MP_BASE_PATH=/platform`, and the ingress then serves only `/platform/...` (the editor at `/platform/`, the catalog at `/platform/planner/v1/catalog`).
- Healthchecks: `ingress` and `web` with `wget -q -O /dev/null http://127.0.0.1:2019/config/` (the Caddy admin endpoint, left on its default loopback address and never published; Alpine variants); `planner` with its `healthcheck` subcommand against `/readyz`.
- The CI `smoke` job runs with `PLATFORM_TLS=internal` and calls the ingress with `curl -k`, in CI only; documentation never tells you to skip certificate checks.
- `task dev` runs this stack with `compose.dev.yaml`, where Caddy routes `/` to the Vite dev server on the host, so hot reload runs on the same origin as production.

### Managed mode wiring (pinned now, built with #16)

**Install**
- The user unzips the managed zip and runs `sh setup.sh`. The platform part asks for domain, base path and admin email; then it asks the questions each module's fragment declares and runs the fragments (principle 9), which create their own items, never printing a secret, one file per secret in `secrets/<module>/` (mode 0600). Platform level: bearer tokens for `planner` and `deployer-docker`, the `api` OIDC client secret. Bundled module fragments: `postgres` (admin and per role passwords), `nats` (one user password per binding), `authelia` (asks admin user name and password; writes session secret, storage encryption key, OIDC signing key, the pbkdf2 digest of the `api` client secret, and `users.yml` with the admin's argon2id hash and the admin email). Non secret settings go to `.env`. Existing files are never overwritten.
- Then `docker compose up -d`. The one shot `db-init` service (contributed by the `postgres` module as `postgresql-provisioner`) provisions databases before `api` and `authelia` start. With an existing database instead, you give each consumer a ready `postgresql` binding and no init service runs (#10, #23).
- Settings in `.env`: `PLATFORM_DOMAIN` (default `platform.localhost`; must contain a dot, which Authelia requires), `PLATFORM_BASE_PATH` (default empty for the site root; otherwise starts with `/`, no trailing slash, like `/platform`), `PLATFORM_ADMIN_EMAIL`, `PLATFORM_TLS` (`internal` or `acme`, default `internal`), `PLATFORM_HTTPS_PORT` (443), `PLATFORM_HTTP_PORT` (80). `PLATFORM_ACME_EMAIL` (optional). `PLATFORM_ORIGIN` (scheme, host and port the browser sees) defaults to `https://${PLATFORM_DOMAIN}` plus the port when not 443; set it explicitly when your own proxy's public port differs from the bundled ingress port (#24).

**Platform graph** (capability names are working names; spec #3 fixes the format). Every image below is the bundled default, replaceable by any module that provides the same capability (#23).

| Module | Image | Provides | Uses |
|---|---|---|---|
| `ingress` | `caddy` (Alpine variant) | `http-ingress` | `http` from `web`, `platform-api` from `api`, `oidc` from `authelia` |
| `web` | `ghcr.io/racherom/modular-platform/web` | `http` (editor) | none |
| `api` | `ghcr.io/racherom/modular-platform/api` | `platform-api` | `postgresql`, `nats`, `oidc`, `planner`, `deploy-target` |
| `planner` | `ghcr.io/racherom/modular-platform/planner` (internal mode) | `planner` | none (stateless) |
| `deployer-docker` | `ghcr.io/racherom/modular-platform/deployer-docker` | `deploy-target` | `nats`, host Docker socket |
| `postgres` | `postgres` (official) | `postgresql-provisioner` | none |
| `nats` | `nats` (Alpine variant, JetStream on) | `nats` | none |
| `authelia` | `authelia/authelia` | `oidc` | `postgresql` (provisioned) |

Provisioned at start: databases `platform_api` and `authelia`, each with its own role, by `db-init` (the `postgres` image running a versioned, idempotent psql script that creates each role and database only if missing; runs on every `up`).

**Interactions**

| From | To | Protocol | Purpose |
|---|---|---|---|
| Browser | `ingress` | HTTPS | One origin for everything |
| Browser | `api` (via ingress) | REST JSON under `/api/v1/`, SSE at `/api/v1/events` | Editor reads and writes; live progress and state |
| Browser, `api` | `authelia` (via ingress, `/auth`) | OIDC Authorization Code with PKCE | Sign in |
| `api` | `planner`, `deployer-docker` | REST JSON, bearer credential | Plan and render; apply and read state |
| `deployer-docker` | `nats` | NATS (JetStream for progress) | Publish deploy progress and container state |
| `api` | `nats` | NATS | Subscribe, persist history, relay to SSE |
| `api`, `authelia` | `postgres` | PostgreSQL wire | Own provisioned database each |
| `deployer-docker` | Docker Engine | Unix socket via `docker compose` and the moby client | Run and inspect containers |

**Sign in**
- `api` settings, filled from its bindings (principle 9): `MP_OIDC_ISSUER` (issuer URL), `MP_OIDC_CLIENT_ID` (`mp-api`), `MP_OIDC_CLIENT_SECRET_FILE`, optional `MP_OIDC_CA_FILE` (CA bundle to trust the issuer's TLS), `MP_PUBLIC_ORIGIN`, `MP_BASE_PATH`, `MP_ADMIN_EMAIL`, `MP_PROXY_HOPS`. `api` never assumes which module provides any of them.
- Routes `GET <base>/api/v1/auth/login`, `GET <base>/api/v1/auth/callback` (the registered redirect URI is `${PLATFORM_ORIGIN}${PLATFORM_BASE_PATH}/api/v1/auth/callback`), `POST <base>/api/v1/auth/logout`; scopes `openid profile email groups`; the user's stable ID is `sub`.
- Bootstrap admin: until #14 adds roles, `api` admits one admin. While none is bound, the first identity whose `email` claim equals `MP_ADMIN_EMAIL` (whole address, case insensitive) with `email_verified` true becomes the admin, and `api` stores its `iss` plus `sub` in the platform database. From then on only that `iss` plus `sub` is admitted, so a later email change at the provider cannot take over the admin. Any other signed in identity gets 403. A replacement identity provider may hold many users (a company directory), so "anyone who can sign in" is never the rule.
- Bundled Authelia: issuer `${PLATFORM_ORIGIN}${PLATFORM_BASE_PATH}/auth`; `server.address: tcp://:9091<base>/auth` (the ingress keeps the prefix); session cookie name `mp_authelia_session` with cookie domain `PLATFORM_DOMAIN` (Authelia requires a domain; the `api` rule of no `Domain` attribute does not cover it); implicit consent, `one_factor` for `mp-api` (moves to `two_factor` with #14), filesystem notifier, storage in the `authelia` database.
- Reaching the issuer from inside: the bundled ingress module gives its service a Compose network alias equal to `PLATFORM_DOMAIN`, and with `PLATFORM_TLS=internal` its binding supplies Caddy's local root CA (from `caddy_data`, mounted read only) as `MP_OIDC_CA_FILE`. Both are part of the ingress module's wiring, not of `api`; a replacement with a public certificate supplies no CA file.
- `api` retries OIDC discovery with backoff, reports `/readyz` false until it succeeds, never exits for it, and depends on its `oidc` and `http-ingress` providers being healthy.
- All `api` cookies, including the state and PKCE cookie before sign in, follow the Sessions row: `__Host-mp_*` with `Path=/` at the site root, `__Secure-mp_*` with `Path=<base>/` under a sub path, never a `Domain` attribute.

**Health and layout**
- Healthchecks: `postgres` `pg_isready`; `authelia` `authelia healthcheck`; `nats` `wget` on monitoring port 8222 `/healthz`; `ingress` and `web` `wget` on the Caddy admin endpoint; Go services their `healthcheck` subcommand against `/readyz`.
- Compose project `modular-platform`. Networks `edge` (`ingress`, `web`, `api`, `authelia`) and `internal` (`api`, `planner`, `deployer-docker`, `postgres`, `nats`, `authelia`, `db-init`); only `ingress` publishes ports. Volumes `pg_data`, `nats_data`, `caddy_data`, `caddy_config`, `authelia_data`.
- Secrets reach containers only as Compose secrets read through `*_FILE` variables; each service lists only its own; no shared `env_file`. Image versions pinned to exact tags; never `latest`.
- NATS: one user per binding (`api`, `deployer`); permissions start wide (`>`) and #16 narrows them per subject; no streams at boot.
- Bearer credentials: `Authorization: Bearer`, read from `MP_PLANNER_TOKEN_FILE` and `MP_DEPLOYER_TOKEN_FILE`; `/healthz` and `/readyz` exempt.
- Container labels under the prefix `io.github.racherom.modular-platform.` name graph, module and deployment (exact keys in #16).

**Security baseline**
- `deployer-docker`, `planner`, `postgres` and `nats` are reachable on `internal` only; `deployer-docker` has no ingress route.
- Mutating `api` requests need a valid session and an `Origin` header exactly equal to `MP_PUBLIC_ORIGIN` (scheme, host and port, from `PLATFORM_ORIGIN`), in both placements; a missing `Origin` is a 403.
- `api` takes the client IP per principle 11 and its origin per principle 12.
- Secret values never appear in logs, graph files or zips.

## Requirements

The scope's done line for this feature, made concrete for the scaffold (`/develop stack & architecture`), which builds the shared core, the planner service and the online tool. The rest of the managed platform is scaffolded with #16. Scaffold steps are derived at build time, not listed here.

**User stories**:
- As a platform contributor, I want a clean clone to generate, build and run the online tool end to end, so that every later feature starts from a thread that already works.

**Acceptance criteria**:
- **AC-1**: `task generate` and `task build` succeed from a clean clone, producing the `web` and `planner` images; CI runs the same plus lint and test, and fails on a codegen diff.
- **AC-2**: `task dev` runs the online tool graph (`ingress`, `web`, `planner`) on one origin; the editor loads and shows the built in catalog fetched from `GET /planner/v1/catalog`.
- **AC-3**: Loading the fixed sample graph (`samples/`: `traefik/whoami` plus `postgres` with one generated password) and choosing download calls `POST /planner/v1/render` and saves a zip containing `docker-compose.yml`, `setup.sh`, `.env`, the graph file and `README.md`, and no secret value.
- **AC-4**: On a clean Linux host with Docker, `sh setup.sh && docker compose up -d` in the unzipped folder brings the sample stack to healthy. Running `setup.sh` again changes no existing secret file, and `docker compose up -d` without running `setup.sh` first fails with a missing secret file error.
- **AC-5**: In public mode the planner answers 429 once a client IP exceeds `MP_PLANNER_RATE` (after the burst of 10), 413 for a body over 1 MB, and never writes a request body to its logs; a client that varies a forged leftmost `X-Forwarded-For` entry is still limited as one client; in internal mode it refuses to start without `MP_PLANNER_TOKEN_FILE` and answers 401 to a call without the token.
- **AC-6**: The CI `smoke` job runs AC-3 and AC-4 headlessly on every pull request, and a release tag pushes multi arch `web` and `planner` images to GHCR.
- **AC-7**: With `deploy/online/compose.subpath.yaml` (`MP_BASE_PATH=/platform`, same `web` image), the editor loads at `/platform/`, fetches every asset and the catalog (`/platform/planner/v1/catalog`) from under `/platform/`, reloads a nested deep link under `/platform/` to the right screen with edge markers drawn on the sample graph, and requests nothing outside `/platform/`; `/platform` redirects to `/platform/`; the `smoke` job checks this.
- **AC-8 to AC-12**: planner rules, YAML round trip, the sample's rendered binding, catalog checks and the sample route; see child spec [0002](0002-provisional-graph-format.md#requirements). "Healthy" in AC-4 follows 0002's ready rule (a service without a healthcheck counts once running), and the nested deep link in AC-7 is the sample route `/platform/samples/whoami-postgres`.

## Consequences

**Positive**:
- Slice 1 is thin: no sign in, database, event bus or deployer is needed to go from design to a running stack.
- The managed `planner` service runs in production from slice 1, behind the same contract the managed API will call.
- `libs/` has no WebAssembly limits, so network features later (#8 conformance checks, #10 external health checks, #13 shared catalogs) can run in the planner.
- Zip export and managed deploy share `libs/render`, so they cannot drift; the online tool and the managed platform are themselves rendered graphs.
- Because platform services learn providers only through bindings and setup travels with modules, #23 needs no change to `web`, `planner` or `api`, only other module manifests.
- The base path lives in the `web` image and the service kit from slice 1, so #24 is configuration, not a rework.

**Negative / tradeoffs**:
- You operate a public service on your own network: uptime, patching, TLS renewal, abuse and denial of service exposure. Rate limits help; a determined flood needs protection in front of your network.
- Users' stack designs pass through your planner (never their secrets). Keeping nothing and logging no bodies must stay true as features grow.
- The online tool works only while your service is up, and not offline.
- No accounts: graphs live in one browser or in files the user keeps; there is no cross device sync or sharing.
- `setup.sh` is POSIX only; Windows users need WSL or Git Bash, and some users will skip it (Compose then fails loudly rather than starting insecurely).
- Managed mode still carries eight containers, three versioned contracts, distributed failure modes and OIDC on the critical path; that cost moves to #16 rather than disappearing.
- `deployer-docker` runs as root with the Docker socket in managed mode; any flaw in it is host root.
- sqlc's release pace has slowed (last release April 2026); generated code keeps working if it stalls.
- Under a sub path the platform shares a browser origin with the rest of your site: a script flaw anywhere on that site can act as a signed in platform admin, and cookie paths do not stop it. Your own domain (or subdomain) is the safer placement; the README for #24 must say so.
- Every browser facing service must honor the base path; one hardcoded `/` link or redirect is a bug that only shows under a sub path, so AC-7 and later #24 tests carry that weight.
- Setup fragments are code from module authors that `setup.sh` runs on your host through `docker run`; containing them to their own image and secrets folder limits, but does not remove, the trust you place in a module (catalog trust: #13).
- The bootstrap admin rule needs the identity provider to send a verified `email` claim for the first sign in; a provider that does not cannot bind an admin until #14 or #18 offer another mapping. Once bound, losing access to that `sub` (for example, the account is deleted at the provider) needs a documented reset (#16).

**Neutral**:
- Managed mode's `deploy/managed/` graph is hand written until #3 and #11 exist, then rendered by `libs/render`; expect one conversion step at #16. The same holds for `deploy/online/`.
- The "built in sign in fallback" in scope features #14 and #18 means the bundled Authelia module of the managed platform.
- Node is needed only to build the editor; nothing in production runs Node.

## Follow-up

- [x] Scope rework (static mode is the online tool plus a zip; #11 folded into #6; sign in and host deploy moved to #16; #14, #15, #18 managed only): done by `/scope` on 2026-10-29.
- [x] Hosting the online tool on your network: enrolled as scope #22.
- [ ] Spec #3 (contract) must cover: provisioning capabilities and their one shot init services, per binding credentials, how secrets are declared so `setup.sh` can create them, the capability names used here, the platform's own services as catalog modules, setup fragments (principle 9: inputs they ask for, the command run in the module's image, the secrets folder they own), and the binding fields that carry an issuer URL, a CA bundle and a public origin.
- [ ] Spec #4 (data model): the graph text file format, and in managed mode whether files or the database are the source of truth.
- [ ] Spec #16: endpoint details of `api.yaml` and `deploy-target.yaml`, NATS subject names and streams, label keys, Compose project naming per deployment, the stored admin binding (`iss` plus `sub`) and how you reset it; the deployer must never stop itself mid apply when the platform graph is redeployed; bind mount paths are resolved by the host daemon, so the deployer's render directory must sit at the same path on host and container (for example `/var/lib/modular-platform`).
- [ ] Specs #23 and #24: how your own reverse proxy fronts the bundled ingress (one upstream) versus replaces it (routes to `web`, `api` and the identity provider, which the render then publishes on loopback ports named in the zip's README), and the `MP_PROXY_HOPS` value for each. With your own proxy, `api` must still reach the public issuer URL from inside Docker: through normal DNS, or an `extra_hosts` entry pointing at your proxy, plus `MP_OIDC_CA_FILE` if your proxy uses a private CA; the README says which. Your proxy must pass SSE unbuffered with long read timeouts and must not rewrite the path under `<base>/`.
- [ ] Specs #8 and #13: catalog trust and plan validation must flag or refuse modules asking for privileged mode, host mounts or host networking, in both the online tool and managed mode (already in the scope rows).
- [ ] Verify at scaffold time: current stable React, Vite and Node LTS majors; Caddy's `templates` directive rendering an environment value into `index.html` only; React Flow edge markers drawing under a sub path. Verify at #16: Authelia under `<base>/auth` with a nested base path and `platform.localhost` as cookie domain, Authelia's `session.name` setting and its `email_verified` claim for file backend users, the `docker:<version>-cli` image including the compose plugin, Caddy passing SSE unbuffered.
- [ ] The repository exists (`github.com/Racherom/modular-platform`) but has no commits yet; the scaffold makes the first one. Go module paths and GHCR image names stay lowercase (`github.com/racherom/modular-platform`).
- [ ] `AGENTS.md` does not exist. `/audit` (scope feature #2) should record this stack, the 23 installed Agent Skills (project wide), `MCP servers: github`, and the skill caveats under Decision.
- [ ] Connect the official GitHub MCP server (`github/github-mcp-server`) in your MCP settings, for example with `claude mcp add`, once the repository exists.
