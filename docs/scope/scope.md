# Scope: Modular Platform

A platform that builds a microservice stack from exchangeable modules. Each module wraps an existing open source container image and declares the capabilities it provides and the capabilities it uses; a capability can also come from an external service. A graph based planner checks that every need has a provider and deploys the result in one of two modes. **Static mode:** you design the stack in a hosted online tool, download a zip (a Compose file, config files and env vars, a setup script) and run it with standard tools alone, for a small self hosted stack. **Managed mode:** for bigger setups, you download the platform itself and it runs inside the network as a control plane with sign in and deploy to the host; every module it ships with, down to its own services, is only a default you can replace, and its planner website runs standalone or under a path of a website you already have; config, users, groups and permissions come from exchangeable modules, the running stack can change at runtime, and an aware module can use different providers for different users. Open source, built by a small team, for self hosters, platform teams and module authors alike.

**Build approach:** Tracer Bullet (each slice runs end to end through catalog, planner, editor and deploy; every later slice thickens one strand of a thread that already works). (basis: vertical slices ship real value early)
**Workflow:** GA (after develop: check verify, test, a fresh model check review, then document). The project default level of rigor. `/architect` is the recommended first stop for a feature with a real decision, but skippable when you already know the build. Any feature can carry its own tag (e.g. `· Beta`) to do more or less. (basis: small team codebase handling sign in and secrets, with a public module contract)

_These are recommendations to keep your build orderly, not requirements. Skip anything that does not fit: if you already know how to build a feature, use `/develop` and skip `/architect`. You decide when a feature is `done`._

## At a glance

| # | Feature | Phase | Status |
|---|---------|-------|--------|
| 1 | Stack & architecture | Foundation | in-progress |
| 2 | Coding standards & tooling | Foundation | planned |
| 5 | Design system & graph editor foundation | Foundation | planned |
| 27 | Module SDK | Slice 1 | in-progress |
| 16 | Managed mode control plane | Slice 1 | planned |
| 3 | Capability and module contract | Slice 2 | planned |
| 4 | Platform data model | Slice 2 | planned |
| 6 | Design online, download, compose up | Slice 3 | planned |
| 23 | Replace the platform's own modules | Slice 4 | planned |
| 24 | Planner website standalone or in your site | Slice 4 | planned |
| 25 | Upgrade a managed install | Slice 4 | planned |
| 7 | Exchange a provider | Slice 5 | planned |
| 8 | Capability conformance checks | Slice 6 | planned |
| 9 | Secrets handling | Slice 7 | planned |
| 26 | Config and secrets wired from the network | Slice 7 | planned |
| 10 | External capability providers | Slice 8 | planned |
| 12 | Plan from files and command line | Slice 9 | planned |
| 13 | Shared catalog repositories | Slice 10 | planned |
| 14 | Roles and permissions | Slice 11 | planned |
| 15 | Audit log | Slice 11 | planned |
| 17 | Config provider module | Slice 12 | planned |
| 18 | Users, groups and permissions modules | Slice 13 | planned |
| 19 | Live graph changes | Slice 14 | planned |
| 20 | Scale modules | Slice 15 | planned |
| 21 | Per user provider routing | Slice 16 | planned |
| 22 | Run the public online tool | Deferred | planned |
| 11 | Standalone export | | dropped |

## Foundations

### 1. Stack & architecture · in-progress
Decide the languages, runtime and service layout of the platform itself, including how deploy targets plug in (single host now, cluster and live control plane later), then scaffold a runnable project.
**Done when:** the decision is recorded in a spec, the deploy target is a swappable part of the architecture, and the empty scaffold boots locally and passes build. (basis: foundations before features)
spec [0001](../specs/0001-platform-stack-architecture/index.md) · [0002](../specs/0001-platform-stack-architecture/0002-provisional-graph-format.md) (provisional graph format and matching rules) · [0003](../specs/0001-platform-stack-architecture/0003-rate-limiter-bucket-cap.md) (rate limiter bucket cap) · [0004](../specs/0001-platform-stack-architecture/0004-managed-mode-first.md) (managed mode first build order) · code in `contracts/`, `libs/`, `catalog/`, `services/planner/`, `web/`, `deploy/online/`, `.github/workflows/`
- [x] Decide the stack (spec): `/architect stack & architecture`
- [ ] Scaffold from the decision: `/develop stack & architecture`
   - [x] Shared core and planner service: libraries, contract and codegen, public and internal modes, provisional graph and catalog format with the matching rules (AC-1, AC-5, AC-8, AC-9, AC-11)
   - [x] Online tool graph on one origin, editor shows the built in catalog, the sample route with its one edge, also under a sub path (AC-2, AC-7, AC-12)
   - [x] Zip render with `setup.sh`; the sample stack comes up healthy (AC-3, AC-4, AC-10)
   - [ ] CI with generate, lint, test and build, release images to GHCR (AC-1, AC-6); the online smoke job leaves CI and `task smoke` stays for manual runs (spec 0004) · workflows written, `task smoke` green locally, smoke job still in `ci.yml`; ticks after the first green run on GitHub
   - [x] Rate limiter bucket cap: at most 10,000 buckets, evict the least recently seen, one warning per minute (spec 0003, its AC-1 to AC-7) · from the review of 2026-10-30
   - [x] Collision free Compose secret names: `<instance>_<name>`, drop the render's collision check, guard test that names never contain `_` (spec 0002, AC-10) · from the review of 2026-10-30
- [ ] Verify it: `/check verify stack & architecture`
- [ ] Test it: `/test stack & architecture`
- [x] Review it: `/check review stack & architecture`
- [ ] Document it: `/document`

### 2. Coding standards & tooling
Capture conventions, then install lint, format and pre commit enforcement from the real scaffolded project.
**Done when:** root `AGENTS.md` reflects the real stack, and lint, format and pre commit checks run clean.
- [ ] Capture conventions and tooling choices: `/audit`
- [ ] Install the tooling: `/develop tooling`
- [ ] Check it runs clean: `/test`

### 5. Design system & graph editor foundation · needs a decision
Visual language, layout primitives and the base graph canvas (modules as nodes, capabilities as ports, bindings as edges, selection) that every planner screen builds on.
**Done when:** `design.md` covers type, color, spacing and components, and the canvas can show, select and connect modules and capabilities.
- [ ] Design it (spec): `/architect design system & graph editor foundation`

## Slice 1: Managed mode, the first thread

### 27. Module SDK · in-progress
A kit for building a new module container, so a module only describes what it needs and the SDK handles config, connections and protection. The platform's own modules (api, planner, deployer) are built on it first, then any aware module a module author writes. The module declares the config values it needs. The SDK fills them from env vars, files and secrets today, and later from a config server (#17), a vault (#9), and per request lookups, where the caller's token says who is calling (user, groups) and the SDK picks the values for that caller (#21). It opens the connections to the capabilities the module uses, from its bindings, and protects traffic between modules (credentials per binding, encrypted connections). It does not check end users. It grows from the existing service kit in `libs/platform`. It comes before #16 because the platform's own modules need it, so it is built against the provisional format of spec 0002, and #3 should adopt its config declaration into the real contract, not replace it.
**Done when:** a module declares its config in one place and starts with values from env vars, files or secrets, with no parsing code of its own; a missing or invalid required value stops it at start with a clear error naming that value; the module reaches a bound capability through the SDK using that binding's credentials; a call from a module without valid credentials for the binding is refused; and a new source (config server, vault, per request token) plugs in without changing the modules that use the SDK.
spec [0005](../specs/0005-module-sdk/index.md) · code in `sdk/`, `catalog/planner.yaml`, `libs/contract/`, `libs/render/`, `services/planner/`
- [x] Design it (spec): `/architect module SDK`
- [ ] Build it: `/develop module SDK`
   - [ ] Planner on a thin SDK: the `sdk` module absorbs `libs/platform`, the new catalog fields and rules, `sdkgen`, the config loader with env files, `sdk.Run`, `catalog/planner.yaml` (AC-1, AC-2, AC-3, AC-4, AC-5, AC-13)
   - [ ] Bindings and protection: per consumer tokens, the HTTP client, readiness, the render additions and the rendered end to end test (AC-6, AC-7, AC-9, AC-12)
   - [ ] TLS and the data clients: ACME TLS tested against `pebble`, the PostgreSQL and S3 clients (AC-6, AC-8, AC-9)
   - [ ] Caller token and pluggable sources: `MP-Caller` mint and check, push, per caller and binding resolver sources (AC-10, AC-11)
   - [ ] The written contract `sdk/CONTRACT.md`, each statement backed by a test (AC-14)
- [ ] Verify it: `/check verify module SDK`
- [ ] Test it: `/test module SDK`
- [ ] Review it (fresh model): `/check review module SDK`
- [ ] Document it: `/document module SDK`

### 16. Managed mode control plane · needs a decision
You download the platform itself as a zip, run `setup.sh` and start it next to your stack, and it becomes the control plane: sign in (one admin to start), plan in your own planner, deploy to the host, and see the live state of every module. Sign in and host deploy live here, not in static mode. This is now the first thread through the whole product (spec 0004), so it stays narrow: full sign in and the database, no event bus yet (the api polls the deployer), the provisional graph format of spec 0002 and a hand written managed zip, deploying any graph the editor can open after you review the plan, with blunt interim refusal rules for host level access. It builds its own screens on the current editor styling without waiting for #5, and runs at the site root only. Spec 0001 and 0004 fix its overall shape; this spec settles the API and deployer contracts, polling and state, labels, the first tables and the managed smoke job. It ships the bundled defaults, but keeps image specific choices behind their capabilities (#23) and does not tie the editor to the site root (#24).
**Done when:** you install the managed zip on a clean host, sign in, deploy a stack from the editor to that host, and the editor shows the running graph with each module's state.
- [ ] Design it (spec): `/architect managed mode control plane`

## Slice 2: The real contract and data model

### 3. Capability and module contract · needs a decision
The public contract every module author builds against: how a capability is named and versioned (protocol plus version), how a module manifest wraps an existing container image and declares what it provides and uses, how a binding reaches the container (address, config, credentials), and how an external provider is described. It also covers provisioning capabilities (a provider that creates, for example, a database and role per consumer), per binding credentials, how a module declares the secrets it needs so `setup.sh` can create them, and the platform's own services as catalog modules (asked by spec 0001), replaceable through the same contract, so nothing in the platform assumes a specific image behind a capability (#23). Config and identity (users, groups, permissions) are capabilities like any other, and the contract plans for managed mode from day one. Comes after #16 (spec 0004), so it is designed against a running platform and replaces the provisional format and the hand written managed zip.
**Done when:** two different modules can declare the same capability and a consumer binds to either without changing; config reaches an unchanged container as files and env vars; a module can declare that it is aware of per user providers; the format carries its own version so it can grow without breaking published modules. (basis: standard container image format; semantic versioning)
- [ ] Design it (spec): `/architect capability and module contract`

### 4. Platform data model · needs a decision
The graph text file format that the editor saves and every zip carries (replacing the provisional one from spec 0002), then how the managed platform stores catalogs, plans, deployments, users and roles, secret references and audit entries, and whether files or the database are the source of truth. Static mode stores nothing on the platform side. In managed mode it also tracks the running graph, and the per group and per user provider mappings, while users and groups may live in an identity module instead of the platform. It builds on the few tables #16 creates (sessions, the admin binding, deployments, apply history) without breaking them.
**Done when:** the entities support every planned slice without a breaking migration, and a graph saved as text files round trips through the platform unchanged. (basis: data model is the costliest thing to redo)
- [ ] Design it (spec): `/architect platform data model`

## Slice 3: Edit the graph

### 6. Design online, download, compose up · needs a decision
Graph editing in the editor (spec 0004): load the catalog, place a consumer module and a provider module on the graph, let the planner bind the needed capability, then either deploy it (managed mode, #16) or download the zip, run `setup.sh` and `docker compose up` and see both running. The zip is the standalone export (the former #11): it runs with standard open source tools alone. The walking skeleton is now #16; this adds drawing, deleting and renaming on top of it. Its spec decides how the planner resolves needs (matching, version rules, what happens when two providers fit) on top of the rules pinned in spec 0002.
**Done when:** you build a two module graph in the editor, the planner rejects an unmet need and accepts a bound one, the graph saves as a text file, the same graph deploys from the managed editor, and the downloaded zip starts both containers wired together on a clean host without the platform, with secrets created by `setup.sh` and never inside the zip.
- [ ] Design it (spec): `/architect design online, download, compose up`

## Slice 4: Own the platform

### 23. Replace the platform's own modules · needs a decision
Every module the managed platform ships with is only a default: ingress, database, event bus, identity provider, deployer, and even web, planner and api. At install you choose another provider of the same capability (for example your own reverse proxy, an existing database, or another sign in provider) and the managed zip is rendered with your choice; setup steps that belong to one provider (such as a password hash) travel with that module. Swapping on a running platform comes with #19.
**Done when:** you render the managed zip with at least two bundled modules replaced by other providers of the same capabilities, `setup.sh` and `docker compose up` bring it up, sign in and deploy work as with the defaults, no platform service changed to accept the replacement, and the planner refuses a replacement that does not provide what the platform needs.
- [ ] Design it (spec): `/architect replace the platform's own modules`

### 24. Planner website standalone or in your site · needs a decision
In managed mode the planner website runs on its own domain, or under a path of a website you already have (for example `example.com/platform`) behind your own reverse proxy, which can stand in for the bundled ingress (#23). Sign in, cookies, the origin check and live updates work in both placements. A widget inside another site's page is deferred.
**Done when:** the same managed install works standalone and mounted under a sub path of an existing site; in both you can sign in, plan, deploy and see live state, and the platform's cookies and routes never clash with the rest of the site.
- [ ] Design it (spec): `/architect planner website standalone or in your site`

### 25. Upgrade a managed install · needs a decision
Move a running managed platform to a newer version: new images, database migrations, and setup steps a newer zip adds, while your secrets, volumes, deployments and replaced modules (#23) stay. Until this lands, an upgrade means installing the new zip over the old folder by hand. Surfaced by spec 0004.
**Done when:** you upgrade an install with deployed stacks to a newer version, new setup steps run once, migrations apply, sign in and every deployment keep working, and a failed upgrade leaves the old version running.
- [ ] Design it (spec): `/architect upgrade a managed install`

## Slice 5: Exchange a provider

### 7. Exchange a provider · needs a decision
Replace a module with another that provides the same capability, plan again and download a new zip (or redeploy, once managed mode exists), with consumers rewired by the planner. This is the core promise of the platform.
**Done when:** swapping one provider for another needs no change to any consumer, the planner shows what will change before deploy, and you get a warning when the old provider holds data that does not move by itself.
- [ ] Design it (spec): `/architect exchange a provider`

## Slice 6: Conformance checks

### 8. Capability conformance checks · needs a decision
A small check per capability version that proves a module (or external provider) really speaks the declared protocol, run before the planner trusts it. Plan validation also flags or refuses modules asking for privileged mode, host mounts or host networking, in the online tool and in managed mode (asked by spec 0001), replacing the blunt interim refusal rules of #16 (spec 0004).
**Done when:** a module that fails its capability check cannot be bound, a module asking for host level access is flagged or refused, and the check result is shown on the graph. (basis: contract testing)
- [ ] Design it (spec): `/architect capability conformance checks`

## Slice 7: Secrets

### 9. Secrets handling · needs a decision
Store credentials for modules and external providers and inject them at deploy, never writing them in plain text into graph files or exports. Until this lands, the managed deployer creates plain random secrets itself and refuses graphs that need any other kind (spec 0004); this brings setup fragments, secrets you enter, and rotation.
**Done when:** generated and entered secrets reach the right containers, graph files and exports carry only references, and a secret can be rotated with a redeploy. (basis: never keep secrets in plain text config)
- [ ] Design it (spec): `/architect secrets handling`

### 26. Config and secrets wired from the network · needs a decision
In managed mode, a compatible module's config and secrets come from the network itself, never from strings you copy. A module's contract says what a binding needs (for a Postgres database: host, port, user, password). On the config page you pick a provider, and the platform uses that provider's provisioning capability (for example, create database) to reuse a fitting resource or create a new one, plus a separate login for each consumer module, then writes the values into that module's config. Passwords and tokens between modules are generated and stored (#9) and never shown to you, and no string is ever typed twice. An external provider with a provisioning capability (for example S3 on a cloud account) works the same way: you give the platform its admin login once, and every bucket and module login after that comes from it (#10).
**Done when:** you bind a module that needs a database to the bundled Postgres module, and the platform creates the database and a login for that module and fills its config, with no value typed or copied; a second module gets its own database and login; no screen, log, graph file or export shows a password or token between modules; a value you do enter (such as an external provider's admin login) is asked for once and reused everywhere it is needed.
- [ ] Design it (spec): `/architect config and secrets wired from the network`

## Slice 8: External providers

### 10. External capability providers · needs a decision
Register a service outside the deployment as a provider: its endpoint, its credentials, and a health check that it is reachable and passes its capability check. Consumers cannot tell it apart from an internal module. Its spec decides where the health check runs: the public planner must never call addresses that users supply. An external that offers a provisioning capability follows #26: you give its admin login once, and the platform reuses or creates resources and module logins from it.
**Done when:** a consumer binds to an external provider exactly like to a module, an unreachable or failing external is flagged on the graph, and switching between an external and an internal provider is an ordinary exchange. (basis: health check API pattern)
- [ ] Design it (spec): `/architect external capability providers`

## Slice 9: Files and command line

### 12. Plan from files and command line · needs a decision
The editor already saves and opens graph text files (slice 1). This decides first whether a command line is worth having at all, and if so validates, plans and renders straight from those files so a stack can live in version control and run in automation.
**Done when:** the same graph files give the same plan from automation and from the editor, and an invalid file gives a clear error that points at the place to fix.
- [ ] Design it (spec): `/architect plan from files and command line`

## Slice 10: Shared catalogs

### 13. Shared catalog repositories · needs a decision
Add community or company catalogs from a repository next to the built in one, with versions and a way to trust or pin a source. Catalog trust flags or refuses modules asking for privileged mode, host mounts or host networking (asked by spec 0001).
**Done when:** you can add a catalog source, its modules and capabilities appear in the planner with their origin shown, and pinned versions never change under you.
- [ ] Design it (spec): `/architect shared catalog repositories`

## Slice 11: Roles and audit

### 14. Roles and permissions · needs a decision
Managed mode starts with a single admin (#16); this adds roles that decide who may view, plan or deploy, and moves the bundled sign in to two factors. Managed mode only: the online tool has no accounts. These built in roles stay the bootstrap fallback once users, groups and permissions come from modules (#18).
**Done when:** a viewer cannot change a graph, a planner cannot deploy, and every action checks the role on the server. (basis: role based access control)
- [ ] Design it (spec): `/architect roles and permissions`

### 15. Audit log
Record every plan, deploy, exchange, secret change and config change with who and when, viewable in the platform. Managed mode only, since the online tool keeps nothing. Storage rests on the data model spec.
**Done when:** each of those actions writes one entry, entries cannot be edited from the UI, and secret values never appear in them. (basis: audit logging practice)
- [ ] Build it: `/develop audit log`

## Slice 12: Live config

### 17. Config provider module · needs a decision
Config becomes a capability that an exchangeable module provides. Unchanged containers get files and env vars rendered from it and reload or restart on change; aware modules read it directly and update live. If this is the first feature that needs events which survive a restart, it brings back the event bus left out of #16 (spec 0004), with its names, streams and per binding permissions; otherwise #19 does.
**Done when:** a changed value reaches an unchanged container (by reload or restart) and an aware module (live), without a full redeploy, and the config provider itself can be exchanged like any module.
- [ ] Design it (spec): `/architect config provider module`

## Slice 13: Identity modules

### 18. Users, groups and permissions modules · needs a decision
Users, groups and permissions become capabilities provided by exchangeable modules (one module or separate ones). Managed mode only: the platform's own sign in and roles use them, and apps in the stack can bind to the same capabilities. The built in sign in and roles (#14) stay as the bootstrap fallback, and the identity provider behind that fallback is itself replaceable at install (#23).
**Done when:** in managed mode you sign in to the platform through an identity module, its groups and permissions decide what you may do, an app in the stack can bind to the same capability, and a fresh install still starts on the built in fallback.
- [ ] Design it (spec): `/architect users, groups and permissions modules`

## Slice 14: Live graph changes

### 19. Live graph changes · needs a decision
Change the running stack without a full redeploy: swap a provider, add a module or remove one. The platform compares the running graph to the new plan and applies only the difference, rewiring consumers as it goes. This includes the platform's own graph (#23), so the deployer applies such a change without stopping itself mid apply.
**Done when:** in managed mode a swap, an add and a remove each apply while the rest of the stack keeps running, a platform module (for example the ingress) can be swapped on a running platform, the planner shows the impact before you apply, and a failed change leaves the previous graph running.
- [ ] Design it (spec): `/architect live graph changes`

## Slice 15: Scaling

### 20. Scale modules · needs a decision
Run more or fewer copies of a module on the host while the stack runs, with consumers spread across the copies.
**Done when:** you can scale a module up and down at runtime, consumers keep working through the change, and a module that declares it cannot run as several copies is refused.
- [ ] Design it (spec): `/architect scale modules`

## Slice 16: Per user providers

### 21. Per user provider routing · needs a decision
An aware consumer module asks the platform which provider to use for the current user. Each group gets a provider, and a single user can be pointed elsewhere; groups come from the identity modules (#18). Unchanged containers keep one provider per capability.
**Done when:** two groups using the same aware consumer reach different providers, a user override wins over the group, a mapping change takes effect at runtime, and the planner flags a per user mapping on a module that is not aware.
- [ ] Design it (spec): `/architect per user provider routing`

## Dropped

### 11. Standalone export · dropped
Folded into #6 by spec 0001: the zip that slice 1 downloads is the standalone export.

## Deferred
Out of scope for the current build pass, kept so the plan stays honest.

### 22. Run the public online tool · needs a decision
Operate the online tool on your own network as a public service: choose the public domain, open only 80 and 443 to the ingress, decide on protection in front against floods, keep TLS renewal and patching going, and publish a short privacy note (nothing stored, request bodies not logged). Surfaced by spec 0001. Deferred by spec 0004 until you choose to host it: the online tool's code keeps compiling and passing unit tests, but nothing runs its smoke check automatically. When it returns, its spec restores the online smoke job in CI first.
**Done when:** the online tool answers on its public domain over TLS, only the ingress is reachable from outside, a flood is stopped before it reaches the planner, and the privacy note is published and true.
- [ ] Design it (spec): `/architect run the public online tool`

Other deferred ideas, not yet numbered:
- **Live control plane**: now planned as managed mode, starting with #16 in slice 1
- **Save and share graphs in the online tool**: the online tool has no accounts, so graphs stay in one browser or in files you keep; no sync across devices · needs a decision
- **Planner widget in another site**: embed the managed planner inside a page of another website (iframe or web component), possibly cross origin, where third party cookie limits hit sign in · needs a decision
- **Per user providers for unchanged containers**: a routing step between consumer and providers that reads the user from each request · needs a decision
- **Cluster deploy target**: deploy one graph across several machines · needs a decision
- **Live swap without downtime**: replace a running provider with little or no interruption · needs a decision
- **Observability**: health, logs and metrics of each module shown on the graph · needs a decision
- **External to internal failover**: fall back to an internal module when an external provider fails · needs a decision
- **Accessibility**: the graph editor works by keyboard and with screen readers (WCAG AA) · needs a decision
- **Multiple languages**: translatable UI text · needs a decision
- **Usage analytics**: opt in, anonymous counts of feature use · needs a decision
- **Planner CPU protection**: a cap on requests in flight or a global limit for the public planner, against floods from many addresses; would add a 503 to the planner contract · from spec 0003 · needs a decision

## Legend

**The decision box.** Every feature carries exactly one, the sub task whose label ends with `(spec)`. Its wording varies (`Design it (spec)` normally, `Decide the stack (spec)` on Stack & architecture), so skills locate it by that `(spec)` suffix, never by an exact label. Every other box is an execution box and `/architect` never ticks one.

**Feature lifecycle**: the scope updates as a feature moves; each row is what it shows and who sets it:

| State | Set by | The feature shows |
|---|---|---|
| `planned` · needs a decision | `/scope` | one box: `Design it (spec): /architect <feature>` |
| `in-progress` (designed) | **`/architect` at spec capture** | `Design it` ticked; spec linked; `Build it: /develop <feature>` with **2 to 5 milestones**; the tier's closing boxes (`Verify it` Alpha and up, `Test it` Beta and up, `Review it` and `Document it` GA); any surfaced follow up enrolled |
| `in-progress` (building) | `/develop` | milestone sub boxes tick one by one; code pointer filled |
| `in-progress` (verified) | `/check verify` | `Build it` and milestones ticked; `Verify it` ticked |
| `done` | **you, when you decide it is** (any skill sets it when you say so); `/sync` reconciles | boxes you ran ticked, skipped ones marked skipped; the tier's last stage (`Prototype` after `/develop`; `Alpha` after `/check verify`; `Beta`/`GA` after `/test`) is the suggested point to call it done; `/sync` captures conventions |

- **Next step** = the first unticked box (always a command or a tracked milestone).
- **needs a decision** = run `/architect` first; otherwise straight to `/develop` (or `/audit` for standards & tooling). The tag drops once the spec is captured.
- **Atomic build tasks live in the spec's `## Build plan`, not here**: the scope carries only the milestone rollup.
- **Status** `planned` → `in-progress` → `done`, plus `existing` (predates the workflow) and `dropped` (taken out of scope, kept for history).
- **Approach tag** beside a heading (e.g. `· Facade`) overrides the project default for that feature; no tag means it inherits.
- **Workflow tier tag** beside a heading (e.g. `· Beta`, `· Prototype`) sets that one feature's rigor above or below the project default; no tag inherits the default. It decides the feature's check boxes and each skill's next suggestion.
- **Workflow** (header line) is the project default, what runs after `/develop`: **Prototype** = nothing (trust develop's own build time self check); **Alpha** = `/check verify`; **Beta** = `/check verify` then `/test`; **GA** = adds a fresh model `/check review` then `/document`. A feature built on an unratified decision (an `Assumed` spec) stays flagged, but that never blocks `done`.
- **Pointer line** (`spec <n> · code in <path>`): the spec link added by `/architect`, the code path by `/develop`.

## References

**Project sources**
- Your answers in the `/scope` planning session (2026-10-29): platform composition planner, all audiences, plan and deploy MVP, single host first, declared capabilities plus conformance checks, local catalog plus shared repos, externals with endpoint, credentials and health, visual editor plus files, standalone export, open source, small team.
- Your answers in the second `/scope` pass (2026-10-29): static mode config as files and env vars; managed mode after the static path, with a config provider module and identity modules (used by the platform and apps, with a built in fallback); unchanged containers must keep working; runtime changes for config, swap, add or remove, and scaling; per user providers by group with user overrides, for aware modules only.
- Spec 0001 follow up, applied by `/scope` replan (2026-10-29): static mode is the hosted online tool plus a downloaded zip; #11 folded into #6; sign in and host deploy moved to managed mode (#16); #14, #15 and #18 managed mode only; new #22 for running the public online tool.
- Your answers in the `/scope` add pass (2026-10-29): every preset module of the managed platform is replaceable, including web, planner and api; replaced at install first, at runtime with #19; the managed planner website runs standalone or under a sub path of your site, a widget in another site is deferred; both new features land right after #16.
- Spec 0004 follow up, applied by `/scope` replan (2026-10-30): managed mode first; #16 moved to slice 1, #3 and #4 after it, then #6 as graph editing, then #23 and #24; #22 deferred; #1's CI milestone no longer carries the online smoke job; new #25 for upgrading a managed install.
- Your answers in the `/scope` add pass (2026-10-30): in managed mode, config and secrets come from the network, provisioned through capabilities such as create database, with a login for each module; secrets between modules are never shown and no string is typed twice; an external with a provisioning capability takes its admin login once; new #26 in slice 7, after #9.
- Your answers in the `/scope` add pass (2026-10-30): a module SDK handles config, connections and protection for a new module container; the module declares its config and the SDK reads it from env vars, secrets, a config server, a vault, or per request (the caller's token picks the values); protection covers traffic between modules only; it comes before the platform's own modules because they are built on it; new #27 at the start of slice 1, before #16.

**Practices & standards**
- Vertical slices ship real value early (tracer bullet development)
- Walking skeleton, foundations before features
- Data model is the costliest thing to redo
- Standard container image format (OCI image spec)
- Semantic versioning for capability contract versions
- Contract testing (consumer driven contracts) for conformance checks
- Health check API pattern for external providers
- Never keep secrets in plain text config
- Audit logging practice
- Role based access control

**Links** (web verified on 2026-10-29)
- [Tracer Bullets and Prototypes, interview with Andy Hunt and Dave Thomas](https://www.artima.com/articles/tracer-bullets-and-prototypes)
- [Forget the Walking Skeleton, Put It on Crutches (quotes Cockburn's definition)](https://gojko.net/2014/06/09/forget-the-walking-skeleton-put-it-on-crutches/)
- [OCI Image Format Specification](https://github.com/opencontainers/image-spec)
- [Semantic Versioning 2.0.0](https://semver.org/)
- [Consumer-Driven Contracts: A Service Evolution Pattern](https://martinfowler.com/articles/consumerDrivenContracts.html)
- [Pattern: Health Check API](https://microservices.io/patterns/observability/health-check-api.html)
- [OWASP Secrets Management Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Secrets_Management_Cheat_Sheet.html)
- [OWASP Logging Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Logging_Cheat_Sheet.html)
- [Role Based Access Control, NIST CSRC](https://csrc.nist.gov/projects/role-based-access-control)
