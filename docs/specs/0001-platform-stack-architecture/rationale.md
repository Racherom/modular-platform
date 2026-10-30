# 0001. Platform stack and architecture: rationale

Decision record for [index.md](index.md). Read by humans and by `/architect` on update or supersede; `/develop` skips it.

## Context

> ⚠️ Premise note: managed mode uses separate services and OIDC sign in for a small team. Splitting into services before a bottleneck or ownership boundary exists usually costs far more build and operating time than one well structured service. The static first slice keeps that cost out of slices 1 to 9, but it lands in full at #16 (eight containers, three versioned contracts). Hold the managed split to the fewest services on real seams, and revisit merging `planner` into `api` when #16 is designed. (basis: monolith first, extract on proven seams) Update: scope #23 now asks that `web`, `planner` and `api` each be replaceable on their own, which turns their seams into product requirements and weakens the case for merging them.

The platform composes a stack of exchangeable modules from a catalog, checks with a graph based planner that every need has a provider, and produces a running stack. It reaches users in two forms. **Static mode:** a user designs a stack in an online tool hosted by the project on its own network, downloads a zip (Compose file plus config files) and runs it with Docker alone; no platform runs on their host. **Managed mode:** a user downloads a ready made zip of the platform itself, starts it, and plans and deploys their apps in their own planner, which later owns the running graph, live config, identity modules and runtime changes. A cluster target is deferred but must not need a rewrite. (basis: your `/scope` answers and this session's clarification, 2026-10-29)

Forces: an open source project with a public module contract, built by a small team, audiences from home lab self hosters to platform teams. The managed platform drives a container runtime, so it holds root equivalent power on the host, and it handles sign in and secrets. The build approach is Tracer Bullet with a GA workflow tier, so slice 1 must run end to end. There is no code yet and no `AGENTS.md`.

The engineer set principles during the design conversation that shape everything: interactions use the existing standard protocol that fits them; backing stores are modules with capabilities, including providers that create sub capabilities for consumers; and the platform describes itself as a graph.

Two scope additions after the first version shape the platform's edges. Every module the managed platform ships, down to `web`, `planner` and `api`, is only a default you can replace at install (#23). The managed website must also work under a path of a site you already have, behind your own reverse proxy (#24). So no platform service may depend on a specific provider image, and nothing may assume it owns the site root.

Not deciding now means every foundation spec (#3 contract, #4 data model, #5 editor) and slice 1 would pick a language, runtime and delivery form on its own.

## Options considered

### Option 1: Shared Go services, hosted stateless planner for static mode

Pure Go planner and renderer libraries behind one `planner` service. The online tool is that service in public mode (stateless, rate limited) plus the editor, hosted on the project's own network; managed mode runs the same service internally next to `api` and `deployer-docker`, with Postgres, NATS, Authelia and Caddy as exchangeable default modules.

**Pros**:
- One service, one contract and one runtime for planning in both forms; the editor has one client shape.
- No WebAssembly limits on `libs/`, so network features (#8, #10, #13) can live in the planner.
- Go matches the container ecosystem's libraries. (basis: compose-go, the moby client and nats.go are first party Go)

**Cons**:
- The project operates a public endpoint: uptime, patching, abuse exposure.
- Stack designs pass through the project's server, and the tool does not work offline.

### Option 2: Planner as WebAssembly in a static site

The same libraries compiled to WebAssembly and run in a Web Worker; the online tool is a static site on a CDN with no backend at all.

**Pros**:
- No server to run; designs never leave the browser; works offline once loaded.
- Hosting is nearly free.

**Cons**:
- Every library in `libs/` must build for `js/wasm` (no network, no filesystem); compose-go support is unverified.
- Network features (#8, #10, #13) hit browser limits (CORS), and the editor needs two adapters (worker and REST).

### Option 3: Go modular monolith for managed mode

Managed mode as one Go binary holding API, planner, deploy driver and the embedded editor, events in process, built in accounts; the online tool as in Option 1.

**Pros**:
- The thinnest managed platform: one image, one process to debug.
- Least operating cost for a small team. (basis: monolith first, extract on proven seams)

**Cons**:
- The platform does not run on its own contract, so it cannot be described and exchanged as a graph.
- The database, event bus and sign in are not exchangeable without later extraction.

### Option 4: Hosted platform with accounts for static mode

The full `api`, `planner` and `web` set hosted by the project, with accounts, saved graphs and sharing, and an export target that returns a zip.

**Pros**:
- Saved graphs, sharing and cross device access from day one.
- One runtime for planning, like Option 1.

**Cons**:
- The project operates sign in, a database and personal data duties (GDPR) on top of a public endpoint.
- Much thicker slice 1; users must trust the project with stored designs.

TypeScript across the stack was also weighed and set aside: it lacks first party Compose and Engine clients for the managed deployer.

## Rationale

Option 1 fits how the product reaches users and how it will grow. Static mode is "design, download, run". A static WebAssembly site (Option 2) would deliver that with no server, and it was the first draft of this spec. The engineer chose to host the planner instead, so that static and managed mode share the service and contract, not only the library. The deciding force is the roadmap: conformance checks (#8), external provider health checks (#10) and shared catalog repositories (#13) all need network access that a browser blocks, so a browser only planner would be pushed to a server later anyway, and the `js/wasm` limit would constrain `libs/` for every feature until then. Keeping the public planner stateless (no accounts, no storage, no body logging) holds the operating cost to one small container behind the ingress, far below Option 4's accounts and database. Secrets still never reach the server, because `setup.sh` creates them on the user's host. Running one planner service in both placements is what keeps a downloaded zip and a managed deploy identical.

For managed mode, Option 1 honours the engineer's principles (standard protocols per interaction, backing services as exchangeable modules with provisioning, the platform as a graph). The engineer chose separate services over the single binary recommended during the conversation; the premise note records that Option 3 is cheaper to operate. The split is held to three services on real seams: the deployer is the only privileged component, the planner is pure, the API owns all state. Static first slicing defers that cost to #16.

Go over TypeScript follows from the deploy side and the shared core: compose-go, the moby client and nats.go are first party Go. The editor stays TypeScript, bridged by OpenAPI codegen in both modes.

The engineer chose a setup script in the zip over a first start init container; it is the more familiar pattern, and Compose secret file entries make a skipped script fail loudly instead of starting with empty secrets. The cost is POSIX only setup on Windows. A command line was dropped because neither install path needs it any more; #12 decides if one is worth adding.

Within managed mode, Postgres beats document or key value stores because identity, roles, audit and deployment history are relational, while live state is better served by the event bus. (basis: relational database as the default) REST with OpenAPI everywhere keeps one contract toolchain. OIDC from day one keeps passwords out of the platform, at the cost of Authelia and HTTPS on the critical path of every managed install. (basis: never build authentication yourself)

### Placement and replacement (update, 2026-10-29)

These calls keep #23 and #24 from forcing a rework later, and cost little now:
- **Keep the path prefix through proxies** over stripping it. A service that knows its own base path builds correct redirects, OIDC callback URLs and cookie paths; one behind a stripped prefix gets them wrong unless every proxy rewrites headers. Runner up: strip and send `X-Forwarded-Prefix`, which not every proxy sets and every service must then honor. (basis: OIDC redirect URIs must match exactly)
- **`__Host-` at the root, `__Secure-` with a scoped path under a sub path** over `__Host-` everywhere. `__Host-` requires `Path=/`, so under a sub path it would place platform cookies on every page of your site. Runner up: always `__Secure-`, which gives up `__Host-`'s protection against cookies set from sibling subdomains in the common root placement. (basis: cookie name prefixes, RFC 6265bis)
- **Runtime base path in the `web` image** (Caddy `templates` on `index.html` plus relative references inside JS and CSS) over one build per placement. One image and one release serve every install. No `<base href>`: it would be simpler, but it breaks SVG `url(#id)` references (React Flow edge markers) and hash links. Runner up: rewriting `index.html` with `sed` at container start, which needs a writable filesystem and a shell in the image.
- **Reaching the issuer: network alias plus CA bundle** over a separate internal issuer URL for the back channel. The cross check suggested the internal URL, but Authelia derives its issuer and endpoint URLs from the incoming request, so ID tokens and the browser's sign in redirect would carry the internal address. With your own proxy, `api` reaches the public URL through DNS or `extra_hosts` instead.
- **Setup fragments run in the module's own image** over modules shipping host shell snippets. Secrets stay born on your host, `setup.sh` still needs only Docker, and a module cannot touch other modules' secrets. Runner up: a first start init container per module, already set aside in favour of `setup.sh` in the first version.
- **Bootstrap admin by verified email, then pinned to `iss` plus `sub`** over trusting every signed in identity. With the bundled Authelia only the admin exists, but a replacement provider (a company directory) would let everyone in. Email is what you know at install time; `sub` is stable and cannot be renamed, so the first match is pinned to it (trust on first use). Runner up: email only, which a provider that lets users change their email would expose.
- **Client IP by proxy hop count** over each ingress product's trusted proxy setting. It works the same behind the bundled Caddy, your own proxy, or flood protection in front, and the leftmost `X-Forwarded-For` entry is client controlled. (basis: forwarded header handling, count trusted hops from the right)

## Landscape check (2026-10-29)

One web check ran before the stack questions (five searches, eight pages):
- `@xyflow/react` 12.12 and `@xyflow/svelte` 1.7, both released in September 2026, actively maintained. Rete.js active but release dates not shown. Vue Flow and JointJS not checked.
- Docker Engine Go client lives at `github.com/moby/moby/client`; the `github.com/docker/docker` path is deprecated since Docker v29 (from a search summary, not a fetched page). `compose-spec/compose-go` v2.16.2 released October 2026. A supported Compose Go SDK could not be confirmed, which is why the deployer uses the CLI.
- `connectrpc/connect-go` v2.0.2 released October 2026 (not chosen; REST was).
- `sqlc` v1.31.1 released April 2026 with nothing since, so its pace is slowing. `goose` v3.29.0 released October 2026 on a steady cadence.
- `nats.go` v1.55.0 released October 2026; v1.54.0 raised the minimum Go version to 1.26.
- Not checked on the web (from knowledge, verify at scaffold): Authelia, Caddy, oapi-codegen, `golang.org/x/time/rate`, TanStack Router and Query, Zustand, React and Vite majors.

## References

**Project sources**:
- [docs/scope/scope.md](../../scope/scope.md): feature #1 Stack & architecture, the build approach (Tracer Bullet), the workflow tier (GA), and the managed mode features #16 to #21
- Your answers in this `/architect` session (2026-10-29): the static and managed delivery model, services split, standard protocol per interaction, databases as provisioned capabilities, platform as a graph, OIDC from day one, and every stack pick
- Scope features #23 (replace the platform's own modules) and #24 (planner website standalone or in your site), applied by the in place update of 2026-10-29
- The read only cross check of the first draft (Sonnet), whose gaps were resolved with your answers
- Installed Agent Skills listed under Decision in [index.md](index.md)

**Practices & standards**:
- Monolith first, extract services on proven seams
- Relational database as the default for relational data
- Never build authentication yourself; OIDC Authorization Code with PKCE; backend for frontend token handling
- Spec first API contracts (OpenAPI 3.1)
- Semantic versioning
- Least privilege: only the deployer holds the Docker socket
- Never keep secrets in plain text config or in shared artifacts
- Cookie name prefixes (`__Host-`, `__Secure-`), RFC 6265bis
- OIDC redirect URIs must match exactly
- Forwarded header handling: count trusted proxy hops from the right

**Links** (web verified on 2026-10-29):
- [xyflow releases (React Flow, Svelte Flow)](https://github.com/xyflow/xyflow/releases)
- [Rete.js repository](https://github.com/retejs/rete)
- [moby client package](https://pkg.go.dev/github.com/moby/moby/client)
- [compose-go releases](https://github.com/compose-spec/compose-go/releases)
- [connect-go releases](https://github.com/connectrpc/connect-go/releases)
- [sqlc releases](https://github.com/sqlc-dev/sqlc/releases)
- [goose releases](https://github.com/pressly/goose/releases)
- [nats.go releases](https://github.com/nats-io/nats.go/releases)
- [GitHub MCP server](https://github.com/github/github-mcp-server)
