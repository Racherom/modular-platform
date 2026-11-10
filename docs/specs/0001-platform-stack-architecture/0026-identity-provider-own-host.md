# 0026. The bundled identity provider on its own host

**Date**: 2026-11-10
**Parent**: [0001. Platform stack and architecture](index.md)
**Amends**: principle 10 and the *Ingress* row and *Sign in* bullets of [0001](index.md); the `ingress` and `authelia` rows of [0015](../0011-capability-module-contract/0015-platform-graph-managed-zip.md); the `http-ingress` capability and graph bindings of [0012](../0011-capability-module-contract/0012-capabilities-versions-matching.md) and [0013](../0011-capability-module-contract/0013-module-manifest-v1.md).
**Part of**: scope #24 (planner website standalone or in your site), which still needs its own spec for the rest of the placement work.

## Summary

The bundled sign in service, Authelia, moves out from under the platform's path (today `<base>/auth`) to the root of its own host name, `auth.<your domain>` by default. Authelia cannot limit its login cookie to a path, so under a shared site such as `example.com/platform` that cookie would reach every page of `example.com` and every subdomain. On its own host the cookie stays on that one name, and its address (the issuer) no longer changes when the base path does. You link the host on Authelia's binding to the ingress in the graph; it follows your domain answer, so install asks nothing new, but every install now needs a second DNS name.

## Context

Principle 10 says every browser facing platform service, the bundled identity provider included, serves its pages, redirects and cookies under one base path, so the platform can sit at the root of its own domain or under a path of a site you already have (scope #24). Today Authelia lives at `<base>/auth` on the platform's origin: `server.address` is `tcp://:9091<base>/auth`, the issuer (the URL that names the provider in sign in tokens) is `{origin}{base_path}/auth`, and its session cookie `mp_authelia_session` carries `Domain=<domain>`.

A read only check of Authelia 4.39.28 (source and docs, 2026-11-10) settled what Authelia can do: its session cookie has no path setting (the `SessionCookie` schema has no path field); the cookie domain is required and is sent as a `Domain` attribute, so the cookie reaches that name and all its subdomains; the domain must contain a dot, must not be a public suffix (a shared ending like `co.uk`), and must contain `authelia_url`. The docs show `server.address` only with one path segment (`/authelia`), so a nested `/platform/auth` is unproven. Authelia builds its issuer from the request (`X-Forwarded-Proto` and `X-Forwarded-Host`); whether that issuer carries the path is not documented, and `api` compares the issuer exactly.

So principle 10 cannot hold for Authelia under a sub path: on a shared site its cookie, which lets the holder get a sign in code for `api` without a password, is sent to every app on that site and its subdomains. A second effect follows from the issuer moving with the base path: `api` pins its admin to issuer plus `sub` (the provider's ID for a user), so moving the base path today locks the admin out.

Required placements (your answers): the platform at the root of its own host, and under a sub path of a shared site. Behind your own reverse proxy and replacing the ingress stay with #23 and #24. Moving a module and keeping its users' identities waits until after #14 (roles), where the admin becomes a role given to the setup user and, later, other identities. Sign in already needs the browser's HTTPS port to be 443: with an origin naming another port, today's issuer `https://<domain>:<port>/auth` is unreachable for `api` inside Docker, where Caddy listens on 443.

## Requirements

**User stories**:
- As an operator, I want the platform under `example.com/platform` without Authelia's login cookie reaching the rest of `example.com`, so that another app on my site cannot sign in as my admin.
- As an operator, I want sign in to keep working when I change the base path, without rebinding the admin.
- As an operator, I want to give Authelia another host name by editing the graph, without a new install question.

**Acceptance criteria**:
- **AC-1**: A fresh install with domain `platform.localhost` and the base path unset serves Authelia at `https://auth.platform.localhost/`; its discovery document (`/.well-known/openid-configuration`) names the issuer `https://auth.platform.localhost` exactly, and `api`'s `MP_OIDC_ISSUER` is the same string. Sign in, deploy and live state work (the managed smoke).
- **AC-2**: The same install with `MP_INGRESS__BASE_PATH=/platform` serves `web` and `api` under `https://platform.localhost/platform/`, while Authelia stays at `https://auth.platform.localhost/` with the same issuer. Sign in returns to `https://platform.localhost/platform/api/v1/auth/callback` and lands under `/platform/`. A second CI smoke run proves it.
- **AC-3**: In a fresh browser context, after sign in, `mp_authelia_session` carries `Domain=auth.platform.localhost`, `Secure` and `HttpOnly`; no cookie the platform sets has `Domain=platform.localhost` or any broader domain (`api` cookies stay host only with `Path=<base>/`, unchanged).
- **AC-4**: The platform host no longer routes `<base>/auth` to Authelia: `GET https://platform.localhost/auth/` (and `/platform/auth/` under a base path) returns the same status and body as `GET <base>/does-not-exist` (`web`'s answer), and sets no cookie.
- **AC-5**: The auth host follows the domain answer with no new question: installing with domain `x.test` gives `auth.x.test` in the Caddy site, Authelia's cookie domain and `authelia_url`, the issuer, `api`'s `MP_OIDC_ISSUER`, and the ingress's network alias; `setup.sh`'s questions and their order are unchanged.
- **AC-6**: Changing the host on Authelia's binding in the graph (`host: login.example.org`) and rendering again puts that literal name everywhere AC-5 lists, with no other edit.
- **AC-7**: The rendered README says sign in needs HTTPS on port 443 for both host names, so an origin naming another port is not supported (as before this spec).
- **AC-8**: `api` reaches the issuer from inside Docker through the ingress alias for the auth host, trusts it with the same CA file as today under `tls-mode: internal`, and turns ready.
- **AC-9**: The rendered README says the auth host needs its own DNS name (public for `tls-mode: acme`), that Authelia's cookie covers that host and its subdomains, that Authelia refuses a public suffix host at start, and that changing the domain or origin needs `docker compose exec api /api admin reset` until identities survive a move (after #14).
- **AC-10**: The planner accepts `host` on a platform graph binding to an `http-ingress` provider and refuses, each with a problem at `modules.<i>.uses.<s>.host`: a value that is not a DNS name once each `{field}` token is replaced by `x.example` (`invalid_named_value`); a token naming no text field the provider gives (`invalid_binding`); `host` together with a `route` (`invalid_binding`); `host`, `route` or any other field the capability does not mark `graph: true` (`invalid_binding`); and `host` in a static or deployed graph (`invalid_binding`, "an ingress host is for the platform graph only until the exposure spec").

## Options considered

### Option 1: Keep Authelia at `<base>/auth`

Fix in place: verify the nested path, document that Authelia's cookie reaches the whole host and its subdomains, and amend principle 10 to exempt the cookie.

**Pros**: one host name and one certificate per install; no contract change.
**Cons**: on a shared site any app on the host or a subdomain receives a cookie that signs in as the admin; the issuer moves with the base path, so changing it locks the admin out; two Authelia behaviours (nested path, issuer path) stay unproven and sit on the critical path.

### Option 2: `<base>/auth` by default, own host when the graph names one

**Pros**: simple installs keep one host name; isolation for whoever wants it.
**Cons**: two placements to build, test and document; the issuer source must branch, which the field grammar cannot express; the unsafe placement stays the default.

### Option 3: Authelia always at the root of its own host (chosen)

**Pros**: one placement; the cookie is confined to the auth host; the issuer is independent of the base path; Authelia serves at its root, so neither unproven path behaviour matters.
**Cons**: every install needs a second DNS name (and, with ACME, a second public certificate); a small contract addition (graph set `host` with `{field}` tokens); existing installs get a new issuer once.

## Decision

**Chosen option**: Option 3: Authelia always serves at the root of its own host, `auth.{domain}` by default, linked on its `public` binding in the platform graph.

`web` and `api` keep principle 10 unchanged; the identity provider leaves it. Principle 10 is amended to read: "Every browser facing platform service (`web`, `api`) takes the public origin and one base path … The bundled identity provider serves at the root of its own host (child spec 0026), because its session cookie cannot be scoped to a path."

## Rationale (short)

The forces are the shared site placement you require and what Authelia can actually do. Its cookie cannot be scoped below a host, and that cookie is a bearer credential for the admin, so the only way to keep it off the rest of a shared site is a host of its own. Choosing that placement always, rather than as an option, follows from your call ("own host, always"): one placement is what gets tested, and the safe placement is the one everyone runs. It also removes the two unproven Authelia behaviours (a nested path and a path in the issuer) from the critical path, and makes the issuer independent of the base path, which shrinks the admin lock out problem to a domain or origin change. The second DNS name is the real cost; under `tls-mode: internal` and `*.localhost` it costs nothing, and the README states it for public installs. Runner up: Option 2, rejected because it keeps the unsafe placement as the default and needs a branching issuer source.

Writing the host on the graph binding (your choice) matches how you want exposures linked later: a module declares that it wants to be reached, and the admin links it to a host. This spec builds only Authelia's case of that, in the platform graph; the general model (any module, any graph, host and path, static content) is a later spec.

The auth URL is `https://<host>` with no port. Carrying the origin's port over would need a value derived after the render (the origin is asked at install), a new setup rule and a 0016 change, and it would still not work: inside Docker Caddy listens on 443, so `api` cannot reach a `:8443` issuer, which is already true for today's issuer. Runner up: a derived `port_suffix` written by `setup.sh`, rejected for that reason.

## Design

### Contract additions

| Where | Change |
|---|---|
| `catalog/capabilities/http-ingress.yaml` | New field `host`: `{type: string, direction: to_provider, optional: true, graph: true, description: "Host name the consumer is served at, at its root, instead of under the base path."}` |
| Capability format (0012) | A field may say `graph: true` (only with `direction: to_provider`): the graph may set it on a binding. Until now only a provisioner's `named` values were set in the graph. `contracts/openapi/planner.yaml` gains `graph` on the capability field schema, then `task generate`. |
| Graph bindings (0012) | The object form `{provider: <p>, <field>: <value>}` also works for a direct binding and may set any `graph: true` field; any other key is `invalid_binding`. In `libs/contract` the binding's `Named` map becomes `Values` (named values and graph set fields alike), and `MarshalGraph` writes the object form whenever `Values` is not empty; the editor's writer matches (0025 AC-19), and `planner.yaml`'s binding schema names it. A graph value replaces the consumer entry's source for that field. |
| Graph value tokens (0012) | A graph value may hold `{<field>}` tokens, each naming a text field the provider gives on that binding (here `{domain}`); any other token is `invalid_binding`. The planner checks a value's pattern with each token replaced by `x.example`. The render fills each token once per target: `${MP_INGRESS__DOMAIN}` in Compose (environment, aliases), `{$INGRESS_DOMAIN}` in the Caddyfile; a value with no token stays literal everywhere. Templates receive the filled string in `.Values`. |
| Flavors (0013) | Platform flavor only: a static or deployed graph setting `host` is `invalid_binding` (AC-10) until the exposure spec. |
| Consumer sources (0013) | `{uses: <slot>, format}` and the `{<slot>.<field>}` environment tokens may name the consumer's own `to_provider` fields of that binding (so `authelia` reads its `host`). Lookup: the fields the provider gives first, then the consumer's own `to_provider` fields; a capability declaring one name in both directions is a catalog error. |
| Aliases (0013) | The render gives a provider instance, beside its entry's `network.aliases`, one alias per distinct filled `host` among its bindings, deduplicated, on each network it joins. |

### Module and graph changes

| Instance | Change |
|---|---|
| `ingress` (`caddy`) | `Caddyfile.tmpl`: the main site's global options, `tls-…` import and `header` block move into a snippet the main site and each host site import. One site block per `.Clients.public` item with a `host`, addressed by that filled value, with `reverse_proxy` of every path to its `upstream` (with the same `tls_trust_pool` and `server_name` rule as routes); such an item is skipped in the main site's route list. The main site's `handle { respond 404 }` stays. The dev hook `MP_WEB_UPSTREAM` stays on the main site's `/` route only. |
| `authelia` | `uses.public.fields`: drop `route`, add `host: {value: "auth.{domain}"}` (the default the graph may replace). `provides.oidc.fields.issuer: {uses: public, format: "https://{host}"}`. `environment`: add `AUTH_HOST: "{public.host}"`; keep `ORIGIN`, `BASE_PATH` (they still build the redirect URIs of `api` and other clients, which live on the platform's origin); drop `DOMAIN`. The healthcheck comment drops "base path included". |
| `configuration.yml.tmpl` | `server.address: 'tcp://:[[ .Provides.oidc.Port ]]/'`; `session.cookies[0].domain: '{{ env "AUTH_HOST" }}'`, `authelia_url: 'https://{{ env "AUTH_HOST" }}'`; no `default_redirection_url` (none today either); `redirect_uris` unchanged (`ORIGIN` plus `BASE_PATH` plus the client's `redirect_path`). |
| `deploy/managed/graph.yaml` | `authelia.uses.public: {provider: ingress, host: "auth.{domain}"}`, written out so the admin sees where Authelia is linked. |

Unaffected, stated so the build does not look: `deploy/online/` (hand written, no Authelia), `deploy/managed/compose.dev.yaml` (its `MP_WEB_UPSTREAM` touches only the main site), `api`, `web` and the SDK. `api`'s `depends_on` already covers `ingress` through its `public` binding. Chromium and curl resolve `*.localhost`, `auth.platform.localhost` included, to loopback, so `task dev:managed` and the smoke need no hosts file or resolver flags; inside Docker the alias covers `api`.

### Value sourcing

| Action | Value | Source |
|---|---|---|
| Caddy serves the auth site | auth host name | `authelia`'s `public` binding `host` (graph, default `auth.{domain}`), `{domain}` from `ingress` config `domain` (asked), filled as `auth.{$INGRESS_DOMAIN}` |
| Authelia session cookie | cookie domain | `AUTH_HOST` = the same binding `host`, filled as `auth.${MP_INGRESS__DOMAIN}` |
| Authelia `authelia_url`, issuer, `api`'s `MP_OIDC_ISSUER` | `https://<host>` | the binding `host` through `{uses: public, format: "https://{host}"}` |
| OIDC client redirect URI | `<origin><base_path>/api/v1/auth/callback` | `ingress` `origin` and `base_path` (unchanged) plus `api`'s `redirect_path` |
| `api` reaching the issuer inside Docker | network alias for the auth host | render, one alias per distinct filled `host` among `ingress`'s bindings |
| TLS for the auth host | certificate | Caddy, by `tls-mode` (internal authority or ACME) per site |
| `api` trusting that certificate | `MP_OIDC_CA_FILE` | `authelia`'s `oidc` field `ca` = `{uses: public, field: ca}` (unchanged; Caddy's local root signs both names) |
| Plan time check of `host` | a DNS name | the value with each token replaced by `x.example`, against the pattern `[a-z0-9-]+(\.[a-z0-9-]+)+` |

### Key invariants

- Authelia's cookie domain, `authelia_url`, the issuer and `api`'s `MP_OIDC_ISSUER` come from the one binding `host`; no second place names the auth host.
- The base path never reaches Authelia's own address, cookie or issuer; it reaches only the redirect URIs, which point at `api`.
- An `http-ingress` binding has a `host` or a `route` (or neither, ports only), never both.
- The graph sets only fields marked `graph: true`.

### Security model

- The Authelia session cookie (a credential that yields a sign in code for `api` without the password) reaches only the auth host and its subdomains, never the platform's host or a shared site.
- The auth host is a separate browser origin from the platform: a script flaw on the shared site cannot read Authelia's pages, which closes part of 0001's shared origin risk for sign in (the platform's own pages keep it, as 0001 states).
- Sign in starts as a plain link to `api`'s login route, which redirects to the auth host; the web CSP's `form-action 'self'` does not apply to that navigation, so no CSP change. The OIDC state cookie is `SameSite=Lax` and the callback is a top level `GET`, so it is sent.
- The admin pin (issuer plus `sub`) is unchanged.
- Authelia refuses a public suffix cookie domain at start; the planner does not check the suffix list (the README says so).

### Critical test scenarios

- Smoke at the site root: install, Authelia at `https://auth.platform.localhost/` (asserted on the page URL after the sign in link), sign in, deploy, live state; cookies read with `context.cookies()` from a fresh context (**AC-1**, **AC-3**, **AC-8**).
- Smoke under `/platform` in CI: `scripts/smoke-managed.sh` writes `MP_INGRESS__BASE_PATH=/platform` before `up`, and the Playwright `baseURL` becomes `https://platform.localhost/platform/` from an env set by the script; the same flow, plus `/platform/auth/` matches `/platform/does-not-exist` and sets no cookie (**AC-2**, **AC-3**, **AC-4**).
- Render golden for the platform graph: Caddy auth site, aliases, Authelia env and template, `api`'s issuer (**AC-5**); a second golden with the graph host `login.example.org` (**AC-6**).
- README golden contains the port 443, DNS, cookie scope, public suffix and admin reset lines (**AC-7**, **AC-9**).
- Planner and contract tests: `host` accepted on the platform graph; each refusal in AC-10; `MarshalGraph` round trips a binding with `host` byte for byte, and the editor's writer matches (**AC-10**).

## Build plan

Tracer Bullet: the first task gets Authelia answering on its own host end to end in the smoke, then the contract work turns the hand written parts into the graph driven form, then the second placement and the docs.

1. Spike, then thread: in a scratch copy of `build/managed`, hand edit Authelia to `tcp://:9091/` with cookie domain and `authelia_url` on `auth.platform.localhost`, a Caddy site for it and the alias; confirm the discovery issuer is exactly `https://auth.platform.localhost` (no trailing slash, no path, no port) both from the browser and from `api` through the alias, and the cookie attributes; then sign in once. Record the result as build findings in this spec. Satisfies **AC-1**, **AC-3**, **AC-8** (evidence).
2. Contract: `graph: true` fields, `Named` to `Values` with the direct binding object form and `MarshalGraph`, `{field}` tokens and their plan time check, the flavor refusal, consumer sources reading own `to_provider` fields with the stated lookup order, the `host` or `route` rule and the AC-10 problems; `http-ingress` gains `host`; `planner.yaml` and `task generate`; the editor's graph writer. Satisfies **AC-10**.
3. Render: token filling per target, aliases from binding hosts; then the `caddy` entry and `Caddyfile.tmpl` (snippet, host sites), the `authelia` entry and template, `deploy/managed/graph.yaml`; refresh the platform goldens and add the `login.example.org` golden. Satisfies **AC-5**, **AC-6**, **AC-8**.
4. Smoke: `scripts/smoke-managed.sh` and `web/smoke/managed.spec.ts` check the auth host, the cookie domain from a fresh context, and that `<base>/auth` no longer reaches Authelia; a second CI job runs it with `MP_INGRESS__BASE_PATH=/platform`. Satisfies **AC-1**, **AC-2**, **AC-3**, **AC-4**.
5. Docs: the rendered README lines; principle 10, the *Ingress* row and the *Sign in* bullets of 0001; the `ingress` and `authelia` rows of 0015 (pointing here); 0011 `verify.md`'s issuer item; a line in 0001's `## Structure` for this child. Satisfies **AC-7**, **AC-9**.

## Migration plan

**Strategy**: big bang with the release, since the managed platform has no upgrade path yet (#25).
**Phases**:
1. The release that ships this renders Authelia on `auth.<domain>`; its release note says: add the DNS name, unzip over the install as today, `docker compose up -d`, then run `docker compose exec api /api admin reset` once, because the issuer changed. The old `mp_authelia_session` cookie on the platform host stays in browsers until it expires; it is no longer read.
**Rollback**: unzip the previous release over the folder and `up` again; the admin binding was reset, so the next sign in with the admin email binds again.
**Risks**: an install whose DNS has no record for the auth host cannot sign in until it has one; with ACME, Caddy retries the auth certificate until the name resolves, and the main site keeps working.

## Consequences

**Positive**:
- Authelia's cookie stays off a shared site and off the platform's own host.
- Changing the base path no longer touches the issuer or the admin binding.
- The graph set `host` is the first piece of "the admin links an exposure to a host", which the later exposure spec extends to every module and graph.

**Negative / tradeoffs**:
- Every install needs a second DNS name and, with ACME, a second public certificate; a host with a wildcard record has it for free, others must add it.
- The contract grows by graph set fields with `{field}` tokens, a consumer side lookup in `format`, and aliases from bindings, each with its own tests; `Named` becomes `Values` across `libs/contract`, the planner and the editor.
- Existing installs get a new issuer once and need `admin reset`.
- A domain or origin change still locks the admin out until identities survive a move (after #14).
- Sign in still needs HTTPS on 443; the auth host makes that a requirement on two names.
- Your own reverse proxy in front (#24) must route two names to the bundled ingress, not one.

**Neutral**:
- `web` and `api` keep principle 10; only the identity provider leaves it.
- `api`, `web` and the SDK need no code change.

## Follow-up

- [ ] After #14 (roles): let identities, and so the admin role, survive moving the identity provider (a stable provider key instead of the issuer URL), so a domain or origin change keeps the admin.
- [ ] The exposure model for every module (any consumer, in any graph, links a host and a path; the ingress writes it), as a later spec, likely under #24; it lifts AC-10's platform only refusal.
- [ ] An origin on a port other than 443 (sign in needs `api` to reach the issuer at the browser's port inside Docker), with #24's own proxy work.
- [ ] Enroll a new scope feature with `/scope`: modules that host static web content hand it to a hosting module or the ingress instead of running their own server (`web` would be the first).
- [ ] #24's own spec: your reverse proxy fronting or replacing the ingress, now with the auth host as a second name, and the `MP_PROXY_HOPS` value for each.
