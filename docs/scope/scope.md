# Scope: Modular Platform

A platform that builds a microservice stack from exchangeable modules. Each module wraps an existing open source container image and declares the capabilities it provides and the capabilities it uses; a capability can also come from an external service. You describe a stack in a graph; checks at every stage make sure every need has a provider, and a renderer turns the graph into a zip or into the files the deployer applies, in one of two modes. **Static mode:** you design the stack in the online editor, download a zip (a Compose file, config files and env vars, a setup script) and run it with standard tools alone, for a small self hosted stack. **Managed mode:** for bigger setups, you download the platform itself and it runs inside the network as a control plane with sign in and deploy to the host; every module it ships with, down to its own services, is only a default you can replace, and its stack editor runs standalone or under a path of a website you already have; config, users, groups and permissions come from exchangeable modules, the running stack can change at runtime, and an aware module can use different providers for different users. Open source, built by a small team, for self hosters, platform teams and module authors alike.

**Build approach:** Tracer Bullet (each slice runs end to end through catalog, planner, editor and deploy; every later slice thickens one strand of a thread that already works). (basis: vertical slices ship real value early)
**Workflow:** GA (after develop: check verify, test, a fresh model check review, then document). The project default level of rigor. `/architect` is the recommended first stop for a feature with a real decision, but skippable when you already know the build. Any feature can carry its own tag (e.g. `· Beta`) to do more or less. (basis: small team codebase handling sign in and secrets, with a public module contract)

_These are recommendations to keep your build orderly, not requirements. Skip anything that does not fit: if you already know how to build a feature, use `/develop` and skip `/architect`. You decide when a feature is `done`._

## Words

One name for each thing (your answers of 2026-11-10). #41 brings code, contracts and specs in line; older feature text below may still say planner until then.

| Word | What it names |
|------|---------------|
| stack | the modules that run together, while you design them and while they run. A network stays the Docker network that render makes for a binding |
| graph | the file that describes a stack: its modules, config values, capabilities and bindings. It may be incomplete; shared or ready (#40) |
| zip | the ready to deploy files of a stack, made by the renderer; it carries its graph |
| online editor | the editor before anything runs: open a graph, a zip (#42), a sample or a shared graph, edit it, download a new graph or a zip |
| stack editor | the same editor on a running stack (managed mode): health, the edit layer, adding a graph (#43) and apply |
| renderer | turns a graph into a zip, or into the files the deployer applies: downloads what each module needs, unpacks it, and generates files from capabilities and config values |
| apply | starting the renderer's files on a running stack, module by module so the providers each module uses stay up (#19) |
| checks | the rules a graph must meet, run at several stages (the editors, the renderer, apply); not one component |
| retired | planner, planner website, managed editor, public planner, running graph |

## At a glance

| # | Feature | Phase | Status |
|---|---------|-------|--------|
| 1 | Stack & architecture | Foundation | in-progress |
| 2 | Coding standards & tooling | Foundation | done |
| 5 | Design system & graph editor foundation | Foundation | in-progress |
| 41 | One vocabulary | Foundation | in-progress |
| 44 | Skills from a list, not from git | Foundation | in-progress |
| 27 | Module SDK | Slice 1 | done |
| 16 | Managed mode control plane | Slice 1 | in-progress |
| 3 | Capability and module contract | Slice 2 | in-progress |
| 10 | External capability providers | Slice 2 | in-progress |
| 4 | Platform data model | Slice 2 | planned |
| 6 | Design online, download, compose up | Slice 3 | planned |
| 42 | Open a zip in the editor | Slice 3 | planned |
| 40 | Shared graphs and ready graphs | Slice 3 | planned |
| 34 | Edit layer and gated apply | Slice 3 | planned |
| 43 | Add a graph to a running stack | Slice 3 | planned |
| 35 | Apply history and diff | Slice 3 | planned |
| 32 | Start the platform from one container | Slice 4 | in-progress |
| 23 | Replace the platform's own modules | Slice 4 | planned |
| 24 | Stack editor standalone or in your site | Slice 4 | planned |
| 25 | Upgrade a managed install | Slice 4 | planned |
| 28 | Exchangeable sign in methods | Slice 4 | planned |
| 37 | First run setup on the web | Slice 4 | planned |
| 30 | Sign in protocol an app requires | Slice 4 | planned |
| 31 | TLS for deployed stacks | Slice 4 | planned |
| 7 | Exchange a provider | Slice 5 | planned |
| 8 | Capability conformance checks | Slice 6 | planned |
| 9 | Secrets handling | Slice 7 | planned |
| 26 | Config and secrets wired from the network | Slice 7 | planned |
| 39 | Static content on a shared host module | Slice 8 | planned |
| 12 | Render from files and command line | Slice 9 | planned |
| 13 | Shared catalog repositories | Slice 10 | planned |
| 14 | Roles and permissions | Slice 11 | planned |
| 15 | Audit log | Slice 11 | planned |
| 17 | Config provider module | Slice 12 | planned |
| 18 | Users, groups and permissions modules | Slice 13 | planned |
| 29 | Sign in policy and tenant issuers | Slice 13 | planned |
| 19 | Live graph changes | Slice 14 | planned |
| 36 | Live health and binding statistics | Slice 14 | planned |
| 20 | Scale modules | Slice 15 | planned |
| 21 | Per user provider routing | Slice 16 | planned |
| 22 | Run the public online editor | Deferred | planned |
| 33 | Managed network on Kubernetes | Deferred | planned |
| 38 | Sample graphs for Nextcloud and WordPress | Deferred | planned |
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

### 2. Coding standards & tooling · done
Capture conventions, then install lint, format and pre commit enforcement from the real scaffolded project.
**Done when:** root `AGENTS.md` reflects the real stack, and lint, format and pre commit checks run clean.
code in `.golangci.yml`, `web/biome.json`, `lefthook.yml`, `Taskfile.yml` (`lint`, `fmt`, `hooks`)
- [x] Capture conventions and tooling choices: `/audit`
- [x] Install the tooling: `/develop tooling`
- [x] Check it runs clean: `/test`

### 5. Design system & graph editor foundation · in-progress
Visual language, layout primitives and the base graph canvas (modules as nodes, capabilities as ports, bindings as edges, selection) that every planner screen builds on.
**Done when:** `design.md` covers type, color, spacing and components, and the canvas can show, select and connect modules and capabilities.
spec [0010](../specs/0010-design-system-graph-editor/index.md) · code in `web/src/styles/`, `web/src/components/`, `web/src/graph/`, `web/src/lib/`, `web/a11y/`, `web/design.md`
- [x] Design it (spec): `/architect design system & graph editor foundation`
- [x] Build it: `/develop design system & graph editor foundation`
   - [x] Thin thread: tokens and themes, the app shell, drag connect through the planner into the working copy, the browser check harness (AC-2, AC-3, AC-6, AC-7, AC-9, AC-12, AC-16)
   - [x] Editing: rebind and unbind, selection and the inspector, problem marks and panel, plan failures and the download gate (AC-8, AC-9, AC-10, AC-11, AC-13, AC-14)
   - [x] Session: undo and redo, Tidy layout, the edited flag and replace Dialog, the sample copy, the catalog filter, unknown modules (AC-15, AC-16, AC-17, AC-18, AC-19, AC-20)
   - [x] Design system complete: the component set, the dev only `/design` route, `web/design.md`, the full axe and scale checks, the smoke additions (AC-1, AC-3, AC-4, AC-5, AC-7)
- [ ] Verify it: `/check verify design system & graph editor foundation`
- [ ] Test it: `/test design system & graph editor foundation`
- [ ] Review it (fresh model): `/check review design system & graph editor foundation`
- [x] Document it: `/document design system & graph editor foundation`

### 41. One vocabulary · in-progress
One name for each thing, the same in code, contracts, specs, editor text and this scope, taken from the Words table above. Today "planner" names the matching step, the service that also renders, the managed website and the public tool, and the running stack has five names. Its spec writes the glossary (spec 0001 and the ubiquitous language in `AGENTS.md`) and decides what `libs/planner` and `services/planner` become (the renderer and checks shared by the editors, the renderer and apply), where binding a need automatically lives, whether "plan" stays a word, and how renamed contract paths (such as `/api/v1/deployments`) stay compatible.
**Done when:** the glossary lists each word once with what it names; no code identifier, contract path, spec, editor text or scope row uses a retired name for these things; the renamed packages, services and routes pass `task generate`, lint and tests; and an old contract path either still answers or was removed on purpose with a note in the changelog.
spec [0027](../specs/0027-one-vocabulary/index.md) · code in `libs/planner` → `libs/check`, `services/planner` → `services/renderer`, `contracts/openapi/`, `services/api/`, `services/deployer-docker/`, `libs/render/`, `catalog/`, `web/src/`, `deploy/`, `scripts/vocabulary.sh`
- [x] Design it (spec): `/architect one vocabulary`
- [ ] Build it: `/develop one vocabulary`
   - [x] Glossary in spec 0001 and the retired word check, report only (AC-1, AC-12)
   - [x] planner to renderer, plan to check and resolved graph: service, catalog, settings, image, CI, contracts, `libs/check`, editor text (AC-2, AC-3, AC-6)
   - [x] deployments and projects to stacks: both contracts, migration `00002`, store, deployer folder, managed label, editor routes (AC-4, AC-5, AC-6)
   - [ ] Recommended bindings instead of automatic binding, every shipped graph explicit (spec 0028; AC-7, AC-8, AC-9, AC-10, AC-11)
   - [ ] Living text rewritten, `/sync` and `/scope` run, the check turned on in `task lint` and lefthook (AC-12, AC-13, AC-14)
- [ ] Verify it: `/check verify one vocabulary`
- [ ] Test it: `/test one vocabulary`
- [ ] Review it (fresh model): `/check review one vocabulary`
- [ ] Document it: `/document one vocabulary`

### 44. Skills from a list, not from git · Alpha · in-progress
Stop tracking the installed skill folders (`.agents/skills/`, `.claude/skills/`) in git and commit only `skills-lock.json`, the list of recommended skills with source and hash. A fresh clone installs the skills from that list.
**Done when:** git tracks no file under `.agents/skills/` or `.claude/skills/` and ignores both; `skills-lock.json` lists every recommended skill; on a fresh clone one documented command restores them all, including the workflow skills; and `AGENTS.md` points to that command.
code in `.gitignore`, `Taskfile.yml` (`skills`), `skills-lock.json`, `AGENTS.md` (`## Agent skills`), `README.md`
- [x] Build it: `/develop skills from a list`
- [ ] Verify it: `/check verify skills from a list` (a fresh clone, then `task skills`)

## Slice 1: Managed mode, the first thread

### 27. Module SDK · done
A kit for building a new module container, so a module only describes what it needs and the SDK handles config, connections and protection. The platform's own modules (api, planner, deployer) are built on it first, then any aware module a module author writes. The module declares the config values it needs. The SDK fills them from env vars, files and secrets today, and later from a config server (#17), a vault (#9), and per request lookups, where the caller's token says who is calling (user, groups) and the SDK picks the values for that caller (#21). It opens the connections to the capabilities the module uses, from its bindings, and protects traffic between modules (credentials per binding, encrypted connections). It does not check end users. It grows from the existing service kit in `libs/platform`. It comes before #16 because the platform's own modules need it, so it is built against the provisional format of spec 0002, and #3 should adopt its config declaration into the real contract, not replace it.
**Done when:** a module declares its config in one place and starts with values from env vars, files or secrets, with no parsing code of its own; a missing or invalid required value stops it at start with a clear error naming that value; the module reaches a bound capability through the SDK using that binding's credentials; a call from a module without valid credentials for the binding is refused; and a new source (config server, vault, per request token) plugs in without changing the modules that use the SDK.
spec [0005](../specs/0005-module-sdk/index.md) · code in `sdk/`, `catalog/planner.yaml`, `libs/contract/`, `libs/planner/`, `libs/render/`, `services/planner/`
- [x] Design it (spec): `/architect module SDK`
- [x] Build it: `/develop module SDK`
   - [x] Planner on a thin SDK: the `sdk` module absorbs `libs/platform`, the new catalog fields and rules, `sdkgen`, the config loader with env files, `sdk.Run`, `catalog/planner.yaml` (AC-1, AC-2, AC-3, AC-4, AC-5, AC-13)
   - [x] Bindings and protection: per consumer tokens, the HTTP client, readiness, the render additions and the rendered end to end test (AC-6, AC-7, AC-9, AC-12)
   - [x] TLS and the data clients: dotted host names (`MP_DOMAIN`, a network alias per SDK service), ACME TLS on the SDK's own `x/crypto/acme` client and renewal (replacing the autocert build) tested against `pebble`, the PostgreSQL and S3 clients (AC-6, AC-8, AC-9, AC-12)
   - [x] Caller token and pluggable sources: `MP-Caller` mint and check, push, per caller and binding resolver sources (AC-10, AC-11)
   - [x] The written contract `sdk/CONTRACT.md`, each statement backed by a test (AC-14)
- [x] Verify it: `/check verify module SDK`
- [x] Test it: `/test module SDK`
- [x] Review it (fresh model): `/check review module SDK`
- [x] Document it: `/document module SDK`

### 16. Managed mode control plane · in-progress
You download the platform itself as a zip, run `setup.sh` and start it next to your stack, and it becomes the control plane: sign in (one admin to start), plan in your own planner, deploy to the host, and see the live state of every module. Sign in and host deploy live here, not in static mode. This is now the first thread through the whole product (spec 0004), so it stays narrow: full sign in and the database, no event bus yet (the api polls the deployer), the provisional graph format of spec 0002 and a hand written managed zip, deploying any graph the editor can open after you review the plan, with blunt interim refusal rules for host level access. It builds its own screens on the current editor styling without waiting for #5, and runs at the site root only. Spec 0001 and 0004 fix its overall shape; this spec settles the API and deployer contracts, polling and state, labels, the first tables and the managed smoke job. It ships the bundled defaults, but keeps image specific choices behind their capabilities (#23), does not tie the editor to the site root (#24), and keeps its one sign in method behind a seam so methods can be exchanged later (#28).
**Done when:** you install the managed zip on a clean host, sign in, deploy a stack from the editor to that host, and the editor shows the running graph with each module's state.
spec [0006](../specs/0006-managed-mode-control-plane/index.md) · [0007](../specs/0006-managed-mode-control-plane/0007-deploy-target-deployer.md) (deploy target and deployer) · [0008](../specs/0006-managed-mode-control-plane/0008-managed-tls-cert-issuer.md) (managed TLS and the certificate issuer) · [0009](../specs/0006-managed-mode-control-plane/0009-managed-zip-install-smoke.md) (managed zip, install and smoke job) · code in `contracts/openapi/`, `catalog/`, `libs/planner/`, `libs/render/`, `libs/target/deploytarget/`, `services/api/`, `services/deployer-docker/`, `services/planner/`, `deploy/managed/`, `web/`, `scripts/smoke-managed.sh`
- [x] Design it (spec): `/architect managed mode control plane`
- [x] Build it: `/develop managed mode control plane`
   - [x] The thread runs: contracts, `api` with sign in, sessions and the four tables, the deployer applying the sample, the managed render and digest, `deploy/managed/` with `setup.sh`, the editor's deploy and live state, the `smoke-managed` CI job; platform services plain for now (AC-1, AC-2, AC-3, AC-5, AC-9, AC-11, AC-13, AC-18, AC-19)
   - [x] Review you can trust: the full review answer, `plan_changed`, the allow list guard in planner and deployer, placed credentials end to end (AC-7, AC-8, AC-10, AC-11)
   - [x] Lifecycle and recovery: remove with delete data, `busy` across processes, `unknown` and resumed polling, automatic rollback with one retry (AC-9, AC-14, AC-15, AC-16)
   - [x] TLS for the platform's own services: the step-ca spike, `cert-issuer`, TLS mode for `api`, `planner` and `deployer-docker` (AC-17)
   - [x] Live state, sessions, the admin and the release: poll cadence and badges, sign out and `prompt=login`, `admin reset`, download zip, Docker restart, the zip on releases (AC-3, AC-4, AC-6, AC-12, AC-13, AC-19, AC-20)
- [ ] Verify it: `/check verify managed mode control plane`
- [ ] Test it: `/test managed mode control plane`
- [ ] Review it (fresh model): `/check review managed mode control plane`
- [ ] Document it: `/document managed mode control plane`

## Slice 2: The real contract and data model

### 3. Capability and module contract · in-progress
The public contract every module author builds against: how a capability is named and versioned (protocol plus version), how a module manifest wraps an existing container image and declares what it provides and uses, how a binding reaches the container (address, config, credentials), and how an external provider is described. It also covers provisioning capabilities (a provider that creates, for example, a database and role per consumer), per binding credentials, how a module declares the secrets it needs so `setup.sh` can create them, and the platform's own services as catalog modules (asked by spec 0001), replaceable through the same contract, so nothing in the platform assumes a specific image behind a capability (#23). Config and identity (users, groups, permissions) are capabilities like any other, and the contract plans for managed mode from day one. Comes after #16 (spec 0004), so it is designed against a running platform and replaces the provisional format and the hand written managed zip.
**Done when:** two different modules can declare the same capability and a consumer binds to either without changing; config reaches an unchanged container as files and env vars; a module can declare that it is aware of per user providers; the format carries its own version so it can grow without breaking published modules. (basis: standard container image format; semantic versioning)
spec [0011](../specs/0011-capability-module-contract/index.md) · [0012](../specs/0011-capability-module-contract/0012-capabilities-versions-matching.md) (capabilities, versions, matching) · [0013](../specs/0011-capability-module-contract/0013-module-manifest-v1.md) (module manifest v1 and render) · [0014](../specs/0011-capability-module-contract/0014-provisioners-jobs.md) (provisioners and jobs) · [0015](../specs/0011-capability-module-contract/0015-platform-graph-managed-zip.md) (platform graph and rendered managed zip) · [0022](../specs/0011-capability-module-contract/0022-deployed-cap-add.md) (Linux capabilities on deployed graphs) · [0025](../specs/0011-capability-module-contract/0025-slot-fields-config-delivery.md) (slot fields in the SDK, config values for every module) · code in `catalog/`, `libs/contract/`, `libs/planner/`, `libs/render/`, `sdk/`, `services/api/`, `services/planner/`, `services/deployer-docker/`, `deploy/managed/`, `web/src/graph/`
- [x] Design it (spec): `/architect capability and module contract`
- [x] Build it: `/develop capability and module contract`
   - [x] v1 thread on the sample: the `--wait` spike, v1 contract and catalog layout, matching with versions, slots and named provides, the provision job, the v0alpha1 converter, `sdkgen` and the editor on v1, the paper platform (AC-1, AC-2, AC-3, AC-4, AC-5, AC-7, AC-8, AC-12, AC-14, AC-16, AC-19, AC-20)
   - [x] Bindings in full: the revoke job, external instances, fixed path and `to_provider` fields, `per_caller`, networks, hardening, labels and `environment` (AC-1, AC-4, AC-6, AC-8, AC-12, AC-13, AC-14, AC-15, AC-18, AC-19)
   - [x] Install time: secrets, asked values, setup steps and the `setup.sh` assembly, templates, host access and the managed refusals (AC-9, AC-10, AC-11, AC-12)
   - [x] Linux capabilities on deployed graphs (0022): only `NET_BIND_SERVICE`, the `invalid_hardening` rule, `cap_add` in `host_access`, the guard requiring `cap_drop` and `no-new-privileges`, the `CapEff` e2e (AC-2, AC-11, AC-21)
   - [x] Slot fields and config values (0025): typed SDK getters for slots with no client and `planner`, `deployer-docker`, `api` on them; non SDK values as `.env` variables with the `setup.sh` check; graph `config` values through planner, render and the editor's inspector; the paper platform entries (AC-10, AC-11, AC-22 to AC-30)
   - [x] The platform is a graph: the eight platform entries, per consumer caller keys, the platform render flavor, the release zip and `smoke-managed` on it (AC-16, AC-18)
   - [x] TLS for unchanged providers: the postgres spike, the certificate provisioner, `api` with `verify-full` (AC-17)
- [ ] Verify it: `/check verify capability and module contract`
- [ ] Test it: `/test capability and module contract`
- [ ] Review it (fresh model): `/check review capability and module contract`
- [ ] Document it: `/document capability and module contract`

### 10. External capability providers · in-progress
Register a service outside the deployment as a provider: its endpoint, its credentials, and a health check that it is reachable and passes its capability check. Consumers cannot tell it apart from an internal module. Its spec decides where the health check runs: the public planner must never call addresses that users supply. A provisioning provider offers the same commands to list, add and remove what it holds (for S3: buckets and their logins), whether it runs in the stack (MinIO) or outside it (an AWS S3 account). The only difference is where its admin settings come from: an internal module's are generated, while for an external you enter them once and the platform reuses them (#26). Today's provision and revoke jobs (spec 0014) cover add and remove for Postgres; this adds list, and providers whose admin APIs differ (MinIO and AWS make logins in different ways) behind one interface. Designed with #3, so the capability file format settles before modules are published against it.
**Done when:** a consumer binds to an external provider exactly like to a module; the same list, add and remove commands work on the bundled S3 module and on an AWS S3 account, and give the same result shape; the only extra step for the external is entering its admin settings once; an unreachable or failing external is flagged on the graph; switching between an external and an internal provider is an ordinary exchange. (basis: health check API pattern)
spec [0017](../specs/0017-external-capability-providers/index.md) · [0018](../specs/0017-external-capability-providers/0018-identity-providers-grants.md) (identity providers and grants) · [0019](../specs/0017-external-capability-providers/0019-s3-seaweedfs-aws.md) (S3 on SeaweedFS and AWS) · [0020](../specs/0017-external-capability-providers/0020-holdings-list-revoke.md) (holdings, list and managed revoke) · [0021](../specs/0017-external-capability-providers/0021-waiting-flag.md) (the waiting flag) · [0023](../specs/0017-external-capability-providers/0023-access-actions.md) (access actions) · [0024](../specs/0017-external-capability-providers/0024-key-rotation-reload.md) (key rotation and live reload) · code in `catalog/`, `libs/contract/`, `libs/planner/`, `libs/render/`, `sdk/`, `services/api/`, `services/deployer-docker/`, `web/src/`
- [x] Design it (spec): `/architect external capability providers`
- [ ] Build it: `/develop external capability providers`
   - [ ] S3 thread on SeaweedFS: the spikes, the identity contract, `s3`, `s3-provisioner`, `iam`, the `seaweedfs` module, access declarations and grants, the login job, two consumers in a static zip (AC-1, AC-2, AC-3, AC-16) · spikes done 2026-11-05
   - [ ] AWS, rotation, adoption and links: the AWS external on Moto, the path fence and date free key rules, SDK live reload, the retire job, external `uses`, `same_as`, `adopt` and sharing (AC-1, AC-3, AC-4, AC-6, AC-7, AC-14, AC-16, AC-17, AC-18)
   - [ ] Postgres split and no revoke at `up`: `postgresql-provisioner` and `postgresql-roles`, the 0014 take over, `mp_jobs` removed, list jobs (AC-2, AC-5, AC-6, AC-8)
   - [ ] The managed record: holdings tables and marker, the deployer run endpoint, pending holdings, revoke after apply and before remove, retire after a confirmed rotation, the list API and the editor panel (AC-9, AC-10, AC-11, AC-15, AC-18)
   - [ ] The waiting flag: `/readyz` names waiting slots, deployer `waiting`, flags on the graph and on failed operations (AC-12, AC-13)
- [ ] Verify it: `/check verify external capability providers`
- [ ] Test it: `/test external capability providers`
- [ ] Review it (fresh model): `/check review external capability providers`
- [ ] Document it: `/document external capability providers`

### 4. Platform data model · needs a decision
The graph text file format that the editor saves and every zip carries (replacing the provisional one from spec 0002), then how the managed platform stores catalogs, plans, deployments, users and roles, secret references and audit entries, and whether files or the database are the source of truth. Static mode stores nothing on the platform side. In managed mode it also tracks the running graph, and the per group and per user provider mappings, while users and groups may live in an identity module instead of the platform. It builds on the few tables #16 creates (sessions, the admin binding, deployments, apply history) without breaking them.
**Done when:** the entities support every planned slice without a breaking migration, and a graph saved as text files round trips through the platform unchanged. (basis: data model is the costliest thing to redo)
- [ ] Design it (spec): `/architect platform data model`

## Slice 3: Edit the graph

### 6. Design online, download, compose up · needs a decision
Graph editing in the editor (spec 0004): load the catalog, place a consumer module and a provider module on the graph, let the checks bind the needed capability, then either deploy it (managed mode, #16) or download the zip, run `setup.sh` and `docker compose up` and see both running. The zip is the standalone export (the former #11): it runs with standard open source tools alone. The online editor and the stack editor are one tool with the same screens; the stack editor adds health, the edit layer and apply on a running stack (#34, #36, #43). The walking skeleton is now #16; this adds drawing, deleting and renaming on top of it. Its spec decides how needs are resolved (matching, version rules, what happens when two providers fit) on top of the rules pinned in spec 0002.
**Done when:** you build a two module graph in the online editor, the checks reject an unmet need and accept a bound one, the graph saves as a text file, the same graph deploys from the managed editor, and the downloaded zip starts both containers wired together on a clean host without the platform, with secrets created by `setup.sh` and never inside the zip.
- [ ] Design it (spec): `/architect design online, download, compose up`

### 42. Open a zip in the editor · needs a decision
The online editor opens two kinds of file: a graph, which may be incomplete, and a ready to deploy zip. From a zip it reads the graph the zip carries and shows it like any graph you open, so you can change a stack you only kept as a zip and download a new graph or a new zip. The stack editor takes the same two kinds when you add a graph to a running stack (#43). Its spec decides what happens when a zip's files no longer match its graph (edited by hand), which zips it accepts (a stack's and the platform's), the limits that keep an untrusted zip safe, and how a zip made before #41 shows recommended bindings for its gaps (spec 0027).
**Done when:** you open a zip the editor gave out and see the same modules, bindings and config values it was made from; downloading it again without an edit gives the same files; a zip changed by hand is flagged and names what differs; a zip that is not a stack's zip, is too large, or holds unsafe paths is refused with a clear message; and nothing from a zip runs in the browser.
- [ ] Design it (spec): `/architect open a zip in the editor`

### 40. Shared graphs and ready graphs · needs a decision
A graph is either shared (generic, with open values) or ready (every value set, so it can deploy). Values that overlap are linked in a shared graph: the whole graph runs under one domain, every config that uses the domain links to it, and each host name links to it as `{sub}.{domain}` (Authelia's `auth.{domain}`, spec 0026, is the first). So every domain and host name a graph needs is planned before setup, and you see the full list of DNS names up front. You take a shared graph from a file or the samples route and make it ready in the editor by filling its last open values. A ready graph always gives a zip. A graph with open values gives one only when the core of its managed stack can start without them, with a warning naming each value you then set in the stack editor; any other graph with an open value gives no zip. The released platform graph is the first shared graph: at install `setup.sh` asks whether to continue without a domain, the core then starts on a self signed certificate, and once you set the domain in the editor the deployment finishes (with #37, which owns the setup site before the domain is known). Its spec decides how a graph marks itself shared or ready, how a link and a `{sub}.{domain}` pattern are written, which values stay open, what counts as the core of a managed stack, how a ready graph turns back into a shared one, and whether a ready graph may keep an open recommended binding (spec 0027 says render refuses it). Publishing shared graphs from catalog repositories waits for #13.
**Done when:** a shared graph opens from a file and from the samples route with its open values listed and its linked values shown as links; filling the domain once fills every config and host name linked to it; a graph with open values downloads as a zip only when its managed core can start without them, with a warning naming each open value; otherwise download and deploy refuse it and name each open value; the platform installs without a domain on a self signed certificate and finishes its deployment after you set the domain in the editor; and the README of a ready graph lists every DNS name it needs.
- [ ] Design it (spec): `/architect shared graphs and ready graphs`

### 34. Edit layer and gated apply · needs a decision
The stack editor shows the running stack by default. You switch on an edit layer to change it: add, remove or rebind modules as a draft drawn on top of the running stack, with every change marked against what runs. Switching the layer off shows the running stack again, untouched by the draft. Apply stays locked until every capability each module uses is bound to a provider, and the editor names what is still missing. Until #19 lands, apply deploys the whole draft as #16 does today; #19 later applies only the difference and shows its impact on this layer. Managed mode only: the online editor has no running stack. Its spec decides where the draft lives (one browser or the platform, one per admin or shared) and what happens when the running stack changes under an open draft.
**Done when:** in managed mode you switch the edit layer on, change the graph, and see each added, removed and rebound part marked against the running stack; switching it off shows the running stack unchanged; apply is disabled while any need is unbound and names the missing capabilities; once every need is bound, apply deploys the draft, and with the layer off the graph then shows the new running stack.
- [ ] Design it (spec): `/architect edit layer and gated apply`

### 43. Add a graph to a running stack · needs a decision
In the stack editor you upload a graph (or a zip, #42) and add it to the running stack as a draft on the edit layer (#34). Its modules join the stack, and a module that already exists in the stack is replaced by the uploaded one. Each config value can be linked to a value the stack already has instead of being set again (links from #40). Apply then runs the same checks as any draft. Managed mode only. Its spec decides when a module counts as already existing (same name, same module, or same capability), what happens to the bindings and data of a replaced module (the data warning of #7), how a config link is chosen (automatic for matching values, or picked by you), and how conflicts show before apply.
**Done when:** you upload a graph into a running stack and see each added, replaced and linked part marked on the edit layer before apply; a replaced module's consumers stay bound to it; a config value linked to an existing one takes that value and follows it when it changes; apply is refused while a check fails and names what is missing; and every module the upload did not touch keeps running.
- [ ] Design it (spec): `/architect add a graph to a running stack`

### 35. Apply history and diff · needs a decision
Every apply writes one history entry: who applied it, when, the full graph and whether it succeeded. You pick any two entries and see the difference between them (modules, bindings and config added, removed or changed) on the graph and as a list. Restoring an entry loads it as the draft in the edit layer (#34), so it passes the same checks and the same apply button. It builds on the apply history table #16 creates; the audit log (#15) records that an apply happened, this keeps the graphs themselves. Managed mode only.
**Done when:** each apply adds one entry with who, when, the graph and the result; you compare any two entries and see every added, removed and changed module, binding and config value, with secret values never shown; restoring an entry opens it as the draft, and applying it runs the same checks as any other draft.
- [ ] Design it (spec): `/architect apply history and diff`

## Slice 4: Own the platform

### 32. Start the platform from one container · in-progress
The managed platform starts from one container, most likely the deployer, instead of one `docker compose up` for every service. It reads the platform's starting state from its files (the platform graph and render of spec 0015, plus what `setup.sh` wrote). Then it looks for the platform's containers on the host: it starts the ones that exist and match, recreates the ones that no longer match the files, and creates the ones that are missing. Only the platform's own modules: deployed stacks keep relying on Docker's restart policy. Comes after #3's milestone 4 (the platform is a graph), and gives #23, #25 and #19 one entry point that brings the platform up from its files. Its spec decides how this first container is started and restarted, how it finds the platform's containers (labels), what counts as no longer matching, and how it keeps from stopping itself.
**Done when:** on a clean host, `setup.sh` and starting the one container bring up the whole platform, and you sign in and deploy as today; on a host where the platform already ran, starting it again reuses the existing containers and volumes and creates no duplicates; a container whose definition no longer matches the files (Compose's rule, so not a `docker update`), or one that was deleted, is recreated from the files at the next start; and no deployed stack's container is touched.
spec [0016](../specs/0016-platform-one-container/index.md) · code in `services/deployer-docker/`, `libs/render/`, `libs/contract/`, `contracts/openapi/planner.yaml`, `catalog/`, `deploy/managed/`, `scripts/smoke-managed.sh`
- [x] Design it (spec): `/architect start the platform from one container`
- [ ] Build it: `/develop start the platform from one container`
   - [ ] Spike and contract: `COMPOSE_FILE` from the state `.env`, a second keeper project, self removal; `keeper`, `show: once`, `format: password` and the platform refusals; the `deployer-docker` and `authelia` entries (AC-3, AC-5, AC-6, AC-10, AC-12)
   - [ ] Render: `docker-compose.yml` without the keeper, `keeper.yml`, `setup.json` and the root `setup.sh` with the seed and the one time print (AC-1, AC-10, AC-11)
   - [ ] The keeper thread: seed, lock, atomic prepare, reconcile, self check and handover, then dev and smoke switched to it (AC-1, AC-2, AC-3, AC-12)
   - [ ] Starting again, drift, untouched stacks, failure paths and the README (AC-3, AC-4, AC-5, AC-6, AC-7, AC-8, AC-9)
- [ ] Verify it: `/check verify start the platform from one container`
- [ ] Test it: `/test start the platform from one container`
- [ ] Review it (fresh model): `/check review start the platform from one container`
- [ ] Document it: `/document start the platform from one container`

### 23. Replace the platform's own modules · needs a decision
Every module the managed platform ships with is only a default: ingress, database, event bus, identity provider, deployer, and even web, planner and api. At install you choose another provider of the same capability (for example your own reverse proxy, an existing database, or another sign in provider) and the managed zip is rendered with your choice; setup steps that belong to one provider (such as a password hash) travel with that module. Swapping on a running platform comes with #19.
**Done when:** you render the managed zip with at least two bundled modules replaced by other providers of the same capabilities, `setup.sh` and `docker compose up` bring it up, sign in and deploy work as with the defaults, no platform service changed to accept the replacement, and the planner refuses a replacement that does not provide what the platform needs.
- [ ] Design it (spec): `/architect replace the platform's own modules`

### 24. Stack editor standalone or in your site · needs a decision
In managed mode the stack editor runs on its own domain, or under a path of a website you already have (for example `example.com/platform`) behind your own reverse proxy, which can stand in for the bundled ingress (#23). Sign in, cookies, the origin check and live updates work in both placements. A widget inside another site's page is deferred.
**Done when:** the same managed install works standalone and mounted under a sub path of an existing site; in both you can sign in, plan, deploy and see live state, and the platform's cookies and routes never clash with the rest of the site.
spec [0026](../specs/0001-platform-stack-architecture/0026-identity-provider-own-host.md) (the identity provider on its own host, one part of this feature) · code in `libs/contract/`, `libs/planner/`, `libs/render/`, `catalog/`, `contracts/openapi/planner.yaml`, `deploy/managed/`, `scripts/smoke-managed.sh`, `web/smoke/`
- [ ] Design it (spec): `/architect stack editor standalone or in your site` (0026 settles Authelia's placement; your own proxy and the rest of the placement work stay open)
- [ ] Build the identity provider's own host: `/develop the identity provider on its own host`
   - [ ] Spike and contract: Authelia on `auth.platform.localhost` by hand, then graph set `host` with `{field}` tokens and the planner refusals (AC-1, AC-3, AC-8, AC-10)
   - [ ] Render and entries: token filling, aliases, the Caddy host site, the `authelia` entry and the platform graph, goldens (AC-5, AC-6, AC-8)
   - [ ] Smoke at the root and under `/platform` in CI, then the README and the 0001, 0015 doc updates (AC-1, AC-2, AC-3, AC-4, AC-7, AC-9)
- [ ] Verify it: `/check verify the identity provider on its own host`
- [ ] Test it: `/test the identity provider on its own host`
- [ ] Review it (fresh model): `/check review the identity provider on its own host`
- [ ] Document it: `/document the identity provider on its own host`

### 25. Upgrade a managed install · needs a decision
Move a running managed platform to a newer version: new images, database migrations, and setup steps a newer zip adds, while your secrets, volumes, deployments and replaced modules (#23) stay. Until this lands, an upgrade means installing the new zip over the old folder by hand. Surfaced by spec 0004.
**Done when:** you upgrade an install with deployed stacks to a newer version, new setup steps run once, migrations apply, sign in and every deployment keep working, and a failed upgrade leaves the old version running.
- [ ] Design it (spec): `/architect upgrade a managed install`

### 28. Exchangeable sign in methods · needs a decision
How someone proves who they are is exchangeable, not only the container behind it (#23). Each sign in method (passkey, FIDO security key, username and password, OIDC through another identity provider, OID4VP with a wallet credential) comes from a module that provides a sign in method capability, and one module may provide several methods. You enable several at once, each user picks one, and two can stack as two factors (#14). The managed platform's own sign in uses them, and apps in the stack can bind to the same capability. A method the platform does not ship with plugs in as a new module, with no platform change. Its spec decides the seam: what the platform and apps learn after a sign in (who, by which methods, how strong), and how methods relate to the identity modules of #18.
**Done when:** you sign in to the managed platform with at least two different methods enabled side by side; a user can combine two methods as two factors; you remove one method module and add another with no change to the platform or any app; an app in the stack signs its users in through the same capability; a fresh install starts with one bundled method, and the planner refuses to remove the last one or a module that does not provide what sign in needs.
- [ ] Design it (spec): `/architect exchangeable sign in methods`

### 37. First run setup on the web · needs a decision · from spec 0016
All install setup moves to the web. After the first start, the platform shows a one time setup site, protected by a generated setup code with no user name (a `show: once` secret that `setup.sh` prints, spec 0016). It collects the install answers `setup.sh` asks today (domain, origin, admin email), and the first admin enrolls through whichever identity module is installed, so it works when the replacement for Authelia offers no username and password, only OID4VP with a wallet. It replaces the generated Authelia admin password of spec 0016, and `setup.sh` then asks nothing. Designed together with #28, so the enrollment seam and the sign in method seam are decided once. Its spec decides where the site runs before the domain is known, how the code is checked and retired, and the seam an identity module offers for enrolling its first admin.
**Done when:** on a clean host `setup.sh` asks nothing and prints only the setup code; the setup site accepts that code once and refuses it afterwards; you enter the install answers there and enroll the first admin with the bundled module, and with a second identity module that has no password; you then sign in and deploy as today.
- [ ] Design it (spec): `/architect first run setup on the web`

### 30. Sign in protocol an app requires · needs a decision
Some apps in the stack speak only one sign in protocol to their users (for example only LDAP, only SAML, only OIDC, or only forward auth headers from the ingress). Such a module declares that it requires exactly that protocol, and the planner binds it only to a provider that speaks it. By default the app shares the users of the platform's own sign in, reached through a provider that bridges to that protocol (for example LDAP served from the identity module, #18). Per app you can instead point it at its own separate provider with its own users, apart from the platform login. Its spec decides how a required protocol is named in the contract (#3), what a bridge provider is, which sign in methods (#28) a protocol can carry, and how the shared and the separate choice meet policy (#29) and roles (#14).
**Done when:** a module that requires LDAP binds only to a provider that speaks LDAP, and the planner refuses one that speaks only another protocol, naming the protocol needed; that app signs in the users of the platform's sign in by default, with no change to the app; you switch it to its own separate provider and users, and the platform's sign in and every other app keep working unchanged; and two apps that require different protocols (for example LDAP and OIDC) share the same users in one stack.
- [ ] Design it (spec): `/architect sign in protocol an app requires`

### 31. TLS for deployed stacks · needs a decision
In managed mode the platform's own services talk TLS through the internal certificate issuer (spec 0006), but SDK modules in the stacks you deploy still run plain with a token per binding, exactly like their static zip. This brings TLS to deployed stacks without putting a platform service on the networks of untrusted graphs: for example one issuer per deployment, or a separate issuer joined to each project network. Comes after #3, which gives unchanged providers certificates. From spec 0006.
**Done when:** SDK modules in a deployed stack serve only TLS with certificates for their own host names, an issuer for one deployment cannot issue names of another, no platform service joins a deployed stack's network, and the static zip of the same graph still runs plain.
- [ ] Design it (spec): `/architect TLS for deployed stacks`

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

## Slice 8: Static hosting

_External providers, once planned here, moved into Slice 2 (#10), so they are designed together with the contract._

### 39. Static content on a shared host module · needs a decision
A module no longer has to serve its own web files. A capability carries its static content (a site, or an app's web front end), and in planning you link it to a static host module that serves it; one host serves the content of many capabilities, each on its own host name or path. The host is an ordinary exchangeable module, so the web server behind it can be replaced like any provider. Its spec decides how content travels with a capability, how a link names its host name or path, how new content reaches a running host, and the shared mechanism that PHP apps on one PHP module (#38) reuse.
**Done when:** you link the content of two capabilities to one static host module in the editor; the static zip and the managed deploy both serve each under its own host name or path; no module in the graph serves web files of its own; and swapping the host module for another provider of the same capability serves the same content unchanged.
- [ ] Design it (spec): `/architect static content on a shared host module`

## Slice 9: Files and command line

### 12. Render from files and command line · needs a decision
The editor already saves and opens graph text files (slice 1). This decides first whether a command line is worth having at all, and if so runs the checks and the renderer straight from those files so a stack can live in version control and run in automation.
**Done when:** the same graph files give the same check results and the same zip from automation and from the editor, and an invalid file gives a clear error that points at the place to fix.
- [ ] Design it (spec): `/architect render from files and command line`

## Slice 10: Shared catalogs

### 13. Shared catalog repositories · needs a decision
Add community or company catalogs from a repository next to the built in one, with versions and a way to trust or pin a source. Catalog trust flags or refuses modules asking for privileged mode, host mounts or host networking (asked by spec 0001).
**Done when:** you can add a catalog source, its modules and capabilities appear in the planner with their origin shown, and pinned versions never change under you.
- [ ] Design it (spec): `/architect shared catalog repositories`

## Slice 11: Roles and audit

### 14. Roles and permissions · needs a decision
Managed mode starts with a single admin (#16); this adds roles that decide who may view, plan or deploy, and moves the bundled sign in to two factors (the factors themselves are exchangeable sign in methods, #28). Managed mode only: the online tool has no accounts. These built in roles stay the bootstrap fallback once users, groups and permissions come from modules (#18).
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

### 29. Sign in policy and tenant issuers · needs a decision
Config decides which sign in methods (#28) count for what: per app in the stack, per action (viewing needs one factor, deploying or changing secrets steps up to a stronger method or a second one), and per tenant. A tenant is a group or org of users from the identity modules (#18), and each tenant trusts its own OID4VP credential issuers, so a wallet credential from one tenant's issuer never signs a user into another tenant. The policy is config, so it changes without a platform change. Its spec decides how app, action and tenant rules combine, where the policy lives (with #17 or #18), and what a refused or step up sign in tells the user.
**Done when:** two apps in the stack accept different methods for the same user; deploying asks for a step up that viewing does not; two tenants each trust only their own OID4VP issuer, and a credential from tenant A's issuer is refused for tenant B; a policy change takes effect without a redeploy; and the planner refuses a policy that leaves an app, action or tenant with no enabled method.
- [ ] Design it (spec): `/architect sign in policy and tenant issuers`

## Slice 14: Live graph changes and health

### 19. Live graph changes · needs a decision
Change the running stack without a full redeploy: swap a provider, add a module or remove one. The platform compares the running stack to the new graph and applies only the difference, rewiring consumers as it goes. Apply works module by module, the way the renderer works for a zip but one module at a time: a module starts only once the providers it uses are up, and a provider stops only after its consumers have moved or stopped, so every module keeps what it depends on. This includes the platform's own graph (#23), so the deployer applies such a change without stopping itself mid apply.
**Done when:** in managed mode a swap, an add and a remove each apply while the rest of the stack keeps running, a platform module (for example the ingress) can be swapped on a running platform, the stack editor shows the impact before you apply, no module runs while a provider it uses is down during an apply, and a failed change leaves the previous stack running.
- [ ] Design it (spec): `/architect live graph changes`

### 36. Live health and binding statistics · needs a decision
The graph shows how the running stack is doing: each module's health, and on each binding its request rate, the share of requests that fail, and how slow they are, updated while you watch. Modules wrap existing container images, so the numbers must come without changing those containers. It thickens the per module state #16 already shows, and replaces the deferred Observability idea for health and metrics (logs stay deferred). Managed mode only. Its spec decides where the numbers come from, how often they refresh, and how long they are kept.
**Done when:** in managed mode the graph shows each module's health and each binding's request rate, error share and latency, refreshing on its own; an unhealthy module or a binding with a high error share stands out without opening anything; the numbers stay visible on the running parts while the edit layer (#34) is on; and no module's container changed to report them.
- [ ] Design it (spec): `/architect live health and binding statistics`

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

### 22. Run the public online editor · needs a decision
Operate the online editor on your own network as a public service: choose the public domain, open only 80 and 443 to the ingress, decide on protection in front against floods, keep TLS renewal and patching going, and publish a short privacy note (nothing stored, request bodies not logged). Surfaced by spec 0001. Deferred by spec 0004 until you choose to host it: the online tool's code keeps compiling and passing unit tests, but nothing runs its smoke check automatically. When it returns, its spec restores the online smoke job in CI first.
**Done when:** the online editor answers on its public domain over TLS, only the ingress is reachable from outside, a flood is stopped before it reaches the renderer, and the privacy note is published and true.
- [ ] Design it (spec): `/architect run the public online editor`

### 33. Managed network on Kubernetes · needs a decision
A managed network runs on a Kubernetes cluster, not only on one Docker host. You install the platform itself on the cluster, and it deploys your stacks to the same cluster, spread across its machines; Kubernetes is a second deploy target beside the Docker host, behind the same target seam (spec 0001), so no module and no graph changes to move between the two. Static mode stays a Compose zip. Replaces the deferred idea of a cluster deploy target. Its spec decides how the platform is installed and upgraded on a cluster (#25), how the starting state from files (#32) maps onto it, how modules, bindings, config, secrets and volumes become cluster resources, how ingress and TLS (#31) work there, how host level refusals (#8) translate, and how it meets scaling (#20) and live changes (#19).
**Done when:** you install the managed platform on a Kubernetes cluster, sign in, deploy a stack to that cluster from the editor and see each module's live state; the same graph deploys unchanged to a Docker host install; a platform module or a module of a stack keeps working when its machine in the cluster goes away; and removing a deployment leaves nothing of it on the cluster.
- [ ] Design it (spec): `/architect managed network on Kubernetes`

### 38. Sample graphs for Nextcloud and WordPress · needs a decision
Two real world sample graphs beside today's one edge sample, so you can open a familiar app in the editor, see how its needs are bound, and run it. Neither app gets a module of its own: each is a capability that carries its PHP files, and in planning you link both to one shared PHP module that runs them side by side. This is the mechanism of static content (#39) with a PHP runtime as the host, and it builds on #39. Each sample also binds the providers its app needs (a database, ingress, and for Nextcloud a cache and storage). They show the contract on apps nobody built for the platform. Its spec decides how PHP files travel with a capability, how apps on one PHP module stay apart (files, config, the PHP extensions and version each needs), which capabilities and provider modules each app needs, how much setup each sample carries, and whether both ship together or one at a time.
**Done when:** both samples open from the editor's samples route with every need bound; one PHP module in the graph runs Nextcloud and WordPress together, each on its own host name or path; each downloads as a zip that starts on a clean host with `setup.sh` and `docker compose up` and serves its app's first page; the same graphs deploy from the managed editor; removing one app from the PHP module leaves the other running; and swapping one of their providers for another of the same capability works without a change to the app capability.
- [ ] Design it (spec): `/architect sample graphs for nextcloud and wordpress`

Other deferred ideas, not yet numbered:
- **Live control plane**: now planned as managed mode, starting with #16 in slice 1
- **Save and share graphs in the online tool**: the online tool has no accounts, so graphs stay in one browser or in files you keep; no sync across devices · needs a decision
- **Planner widget in another site**: embed the managed planner inside a page of another website (iframe or web component), possibly cross origin, where third party cookie limits hit sign in · needs a decision
- **Per user providers for unchanged containers**: a routing step between consumer and providers that reads the user from each request · needs a decision
- **Cluster deploy target**: now #33, the managed network on Kubernetes (deferred)
- **Live swap without downtime**: replace a running provider with little or no interruption · needs a decision
- **Observability**: health and metrics are now #36 in slice 14; logs of each module shown on the graph stay here · needs a decision
- **External to internal failover**: fall back to an internal module when an external provider fails · needs a decision
- **More identity providers**: LDAP or OIDC backed logins and certificate logins (SeaweedFS, Postgres `cert` auth), each one more identity capability granting existing resource types · from spec 0017 · needs a decision
- **Start check for unchanged images**: a check job per binding for consumers not built on the SDK, so their broken bindings are flagged too · from spec 0017 · needs a decision
- **TLS for the bundled S3 module**: an optional `tls` slot on `seaweedfs`, like `postgres` · from spec 0017
- **Real AWS check**: a manual job against a real AWS account that verifies the least privilege admin policy and what Moto does not model · from spec 0017
- **Accessibility**: the graph editor works by keyboard and with screen readers (WCAG AA) · needs a decision
- **Multiple languages**: translatable UI text · needs a decision
- **Usage analytics**: opt in, anonymous counts of feature use · needs a decision
- **Private registries for platform images**: registry credentials in the keeper, so setup steps and the keeper image can come from a private registry · from spec 0016 · needs a decision
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
- Your answers in the `/scope` add pass (2026-10-31): the sign in method is exchangeable, not only its container (passkey, FIDO, username and password, OIDC, OID4VP); used by the platform's own sign in and by apps in the stack; several methods at once, combinable as two factors; a new method is a module with no platform change; new #28 in slice 4, next to #23, with #16 keeping its one method behind a seam.
- Your answers in the `/scope` add pass (2026-10-31, second): some sign in modules offer several methods; config decides which methods count per app, per action (step up) and per tenant; a tenant is a group or org of users from #18, each trusting its own OID4VP issuers; new #29 in slice 13, after #18, keeping #28 small.
- Your answers in the `/scope` add pass (2026-11-03): a module can require one sign in protocol for its users when the protocol allows no other (LDAP, SAML, OIDC, forward auth); by default it shares the platform's users through a bridge, and per app you can separate it onto its own provider and users; new #30 in slice 4, after #28.
- Your answers in the `/scope` add pass (2026-11-05): the managed platform starts from one container (most likely the deployer) that reads its starting state from files, starts existing containers, recreates ones that no longer match the files, and creates missing ones; platform modules only, deployed stacks keep Docker's restart policy; new #32 first in slice 4, after #3's milestone 4.
- Your answers in the `/scope` add pass (2026-11-05, second): a managed network must also run on a Kubernetes cluster; both the platform and the stacks it deploys run on the cluster; static mode stays Compose only; numbered as #33 but kept deferred, replacing the unnumbered cluster deploy target idea.
- Your answers in the `/scope` add pass (2026-11-05, third): the graph shows the running stack's health and per binding requests, errors and latency; the admin edits on a switchable layer over the same view and can apply only when every need is bound; every apply is kept in a history where any two entries can be compared and one restored as the draft; split into three features: #34 and #35 at the end of slice 3, #36 in slice 14 after #19, replacing the Observability idea for health and metrics.
- Your answers in the `/scope` add pass (2026-11-10): every domain and host name is planned before setup; a graph is shared (open values) or ready (deployable), and the admin makes a shared one ready by filling its last open values; overlapping values are linked, host names as `{sub}.{domain}`; shared graphs travel as files and samples, catalog repositories wait for #13; the platform graph is the first shared graph, and its core starts without a domain on a self signed certificate until the domain is set in the editor; new #40 in slice 3, after #6.
- Your answers in the `/scope` pass (2026-11-10, second): one name per thing (the Words table); the running whole is a stack, network stays the Docker network; one editor in two modes, the online editor and the stack editor; the renderer turns a graph into a zip or the deployer's files; checks run at several stages and are not one component; "planner" retires as a name. A complete graph always gives a zip; an incomplete one gives a zip with a warning only when the core of its managed stack can start, so the rest is set in the stack editor (#40 updated). New #41 (foundation), #42 (slice 3, after #6) and #43 (slice 3, after #34); module by module apply folded into #19; #24, #12 and #22 renamed.
- Your answers in the `/scope` replan (2026-11-10, third): #27 done; #32's done line follows Compose's rule (spec 0016); #40 and #42 carry spec 0027's follow up items; #44 gets its Alpha verify box; next is #41, before more #10 and #32 code, so the rename meets few open branches.

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
