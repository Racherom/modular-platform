# 0011. Capability and module contract

**Date**: 2026-11-04
**Status**: In Progress

## Summary

This spec writes the public contract every module author builds against (scope #3), replacing the provisional `v0alpha1` format of spec 0002. A capability becomes a file of its own that names a protocol and the fields a binding of it carries, providers state the version they speak and consumers a version range, and a provisioner (a provider that creates things, like a database and its login per consumer) yields a capability per binding through a one shot job. Modules gain named need slots, named provides, install steps, templates and declared host access, so the managed platform's own eight modules become catalog entries and its zip is rendered from a platform graph instead of written by hand. Building it means changing the catalog, planner, render, SDK generator and editor together, one thin end to end thread first.

## Structure

- [0012. Capabilities, versions and matching](0012-capabilities-versions-matching.md): the `Capability` kind, field types and directions, the version grammar, the graph v1 binding text, named provides, external instances, the matching rules and their problem codes; supports AC-1, AC-3, AC-4, AC-7, AC-13, AC-14, AC-15.
- [0013. Module manifest v1 and its render](0013-module-manifest-v1.md): every field of the `Module` kind (field sources, delivery to the container, config, secrets, setup steps, templates, volumes, host access, hardening, networks) and how `libs/render` turns each into Compose; supports AC-2, AC-8 to AC-12, AC-20.
- [0014. Provisioners and their jobs](0014-provisioners-jobs.md): provisioner capabilities with `yields`, the provision and revoke jobs, named field defaults, `postgresql-provisioner` and the certificate provisioner; supports AC-5, AC-6, AC-7, AC-17.
- [0015. Platform graph and rendered managed zip](0015-platform-graph-managed-zip.md): the platform capabilities, the eight platform entries, `deploy/managed/graph.yaml`, the platform render flavor that writes the managed folder and its `setup.sh`, caller keys as a binding field, and retiring the hand written folder; supports AC-16, AC-17, AC-18.
- [0022. Linux capabilities a deployed module may add](0022-deployed-cap-add.md): only `NET_BIND_SERVICE` on a deployed graph, the `invalid_hardening` catalog rule, `cap_add` in the `host_access` warning, refusal and guard, and the guard requiring `cap_drop: [ALL]` and `no-new-privileges` on every deployed service; supports AC-2, AC-11, AC-21.
- [0025. Slot fields in the SDK and config values for every module](0025-slot-fields-config-delivery.md): typed SDK getters for slots with no client, every non SDK config value as an `.env` variable with its default, config values carried by the graph and set in the editor; supports AC-10, AC-11, AC-22 to AC-30.

Contracts shared by the children live in *Cross child contracts* below.

## Requirements

**User stories**:
- As a module author, I want to declare what my image provides and uses in one versioned file, so that it binds to any compatible provider without me knowing which one.
- As a self hoster, I want a consumer to get its own database and login made for it, so that I never type or copy a password between modules.
- As a platform contributor, I want the platform's own modules described like any module and its zip rendered from a graph, so that replacing one of them (#23) is an ordinary graph change.
- As an operator, I want the planner to tell me exactly which version or field is missing, so that I fix a stack before anything starts.

**Acceptance criteria**:
- **AC-1**: A consumer entry with a required `postgresql` slot binds, unchanged, either to an instance of the `postgres` module (through `postgresql-provisioner`) or to an external instance providing `postgresql` directly. In both renders the consumer's service gets the same env names, each secret field only as a file path, and after `setup.sh` (with the external's password file placed) and `docker compose up`, a test module signs in to its database with the delivered values.
- **AC-2**: `GET /v1/catalog` answers `{modules, capabilities}`, each sorted by `name`, from `catalog/modules/*/module.yaml` and `catalog/capabilities/*.yaml`. Every rule in the catalog rule tables of 0012 and 0013 has one refused fixture; a broken built in entry stops the planner at start with one line per problem as `catalog/<path>: <dotted.path>: <code>: <message>`, exit code 1, and a Go test runs the same checks in CI. A `v0alpha1` catalog entry is refused (`unsupported_version`).
- **AC-3**: `POST /v1/plan` gives exactly the bindings and problems listed in 0012's *Critical test scenarios* for each version, slot, named provide and candidate case, sorted by `path` then `code`, and the same request always gets a byte identical answer.
- **AC-4**: A module with two slots of one capability (`main-db` and `audit-db`, both `postgresql`) bound to one `postgres` instance gets two databases and two logins; each login can open only its own database.
- **AC-5**: For a binding to a provisioner, the render adds exactly one provision job; after `setup.sh` and `docker compose up`, the job exits 0, the consumer starts only after it, signs in with its own login to its own database, and a second `up` adds or drops no role or database while the same password still signs in. A failing job (wrong admin password) keeps its consumer from starting, and `up --wait` exits non zero.
- **AC-6**: Removing a binding (or its consumer) and running `up` again leaves that login unable to sign in while its database and data stay; binding it again with the same names restores sign in with the data intact. A login the provisioner did not create is never changed. Every such `up -d --wait` exits 0 only after the revoke job exited 0, and exits non zero whenever it fails, on the first `up` and every later one.
- **AC-7**: Planning a graph whose binding to a provisioner names no resources writes the default names (0014's rule) into the resolved graph in object form, listed with `named: true` in `bindings`; renaming the consumer instance afterwards keeps those names. Two bindings to one provisioner provide with the same login or database name give `duplicate_named_value`; a value breaking the field's pattern gives `invalid_named_value`.
- **AC-8**: In a rendered service, every non secret field arrives as the env value the consumer mapped, every `secret` or `certificate` field arrives only as a mounted file (its env holds the path, or it sits at the declared fixed path), and `docker inspect` of every service shows no secret value.
- **AC-9**: A module template renders with graph facts only (0013's template data); a template that names a missing key, or fails to parse, is a catalog problem (`invalid_template`) found at catalog load, never at render. Rendering the same graph twice gives byte identical zips, templates included.
- **AC-10**: `setup.sh` from a rendered zip asks every asked config value the graph does not set (0025) and every setup step question once, in 0013's order, runs each setup step in its module's own image as you, with `--network none`, its own secrets folder read write and only its declared reads read only, and skips a step whose outputs all exist. A `for_each` step runs once per client still missing its files, and gets the client only where its entry places `{client}`. Running it again asks nothing new and changes no file.
- **AC-11**: A graph with a module declaring `publish`, `host_mounts`, or a `hardening.cap_add` name other than `NET_BIND_SERVICE` (0022) plans with warning `host_access` at that instance, one problem naming every reason. `POST /v1/render/managed` of such a graph answers 422 with error `host_access`; a graph needing a setup step, an asked value the graph does not set (0025) or an asked secret answers 422 with `setup_required` (until #9). The deployer's guard still refuses any key the managed render does not emit.
- **AC-12**: Every rendered service, unless its entry opts out of that item, has `read_only: true`, `cap_drop: [ALL]`, `security_opt: [no-new-privileges:true]` and `user: "${MP_UID}:${MP_GID}"`; joins the `internal` network (`internal: true`) with the alias `<instance>.<graph>.internal` (the wait anchor `mp_jobs` of 0014 is exempt: it has the hardening but `network_mode: none`); and joins `egress` only when its entry declares `network.egress` or `publish`, or one of its slots is bound to an external instance (directly or through an external provisioner). The golden files show each case.
- **AC-13**: `per_caller: true` on a slot is accepted only on a `runtime: sdk` entry (`per_caller_not_allowed` otherwise), and appears in the catalog answer.
- **AC-14**: An entry or graph whose `formatVersion` minor is above what this planner reads gets `format_too_new` naming the needed version. A `v0alpha1` graph sent to plan, render or managed render is converted to v1 (each `uses` key becomes the slot of the same name), planned, answered in v1 with warning `converted_from_v0alpha1`, and digested as v1.
- **AC-15**: An external instance (`external:` instead of `module:`) is a candidate for its declared provides; a required field with no value, or a secret field, makes the render list `secrets/<instance>/<field>` as a file you place; a non secret required field left out is `external_field_missing`. Consumers bind to it exactly like to a module.
- **AC-16**: `task managed:render` renders `deploy/managed/graph.yaml` with `libs/render`'s platform flavor into `build/managed/` (Compose file, templates, `setup.sh`, README); the hand written files 0015 lists as retired are gone; the release zip is built from the rendered folder; and the `smoke-managed` job (install, sign in, deploy a stack, live state) passes on it.
- **AC-17**: In the rendered platform, `postgres` serves TLS with a certificate for `postgres.modular-platform.internal` issued through the certificate provisioner (0014), `api` connects with `sslmode=verify-full` trusting the internal root, and a plain connection to `postgres` is refused. Running `up` reissues the certificate only when under 30 days remain.
- **AC-18**: `api`'s caller public key reaches `planner` and `deployer-docker` as the binding field `caller_key` (direction `to_provider`), mounted in each provider's clients directory; an `MP-Caller` token is accepted only when signed by the key of the consumer whose binding token matched, and `MP_CALLER_KEY_FILE` is gone.
- **AC-19**: The editor reads the v1 catalog and graph: one handle per slot (`uses:<slot>`) and per provide (`provides:<provide>`), an edge per bound slot (string or object form) ending at the named provide, external instances drawn as nodes marked external, and each provide's version shown in the catalog. Its canonical v1 writer and the Go writer give byte identical text for every fixture in `contracts/fixtures/graphs/`, and parsing that text on either side gives back the same graph.
- **AC-20**: `sdkgen` reads a v1 entry plus the capability files and generates the same getters as before for the built in modules; binding env keys are the capability's field names; `sdk/CONTRACT.md` states the v1 catalog fields it reads, each backed by a test; spec 0005's acceptance criteria still hold.
- **AC-21**: The guard accepts a deployed service only when it has `cap_drop: [ALL]` and `security_opt: [no-new-privileges:true]`, and `cap_add` items equal to `NET_BIND_SERVICE` only (0022); a missing key or any other item is `host_access` at that key or item. An e2e shows a deployed root service adding `NET_BIND_SERVICE` with `CapEff` `0x400` and `NoNewPrivs: 1`.
- **AC-22** to **AC-30**: written in full in [0025](0025-slot-fields-config-delivery.md): SDK getters for slots with no client (AC-22, AC-23), the platform services on them (AC-24), non SDK config values as `.env` variables (AC-25) checked by `setup.sh` (AC-26), graph config values (AC-27, AC-28) edited in the inspector (AC-29), and the platform's Caddy and `web` values (AC-30).

## Decision

**Chosen option**: Option 2: a capability file per protocol, versioned provides and ranged needs, field sources on the provider, provisioners that yield a capability through jobs, and the platform rendered from its own graph (see [rationale.md](rationale.md)).

Release `modular-platform/v1` for the `Capability`, `Module` and `Graph` kinds together. A capability file lists the fields a binding carries; a provide states its version and where each field comes from; a slot states a version range and how each field reaches the container; a provisioner capability `yields` another per binding through a provision job, and a revoke job per provisioner provide disables logins no binding names any more. The editor reads, shows and saves v1; editing names and externals stays with #6.

**Implementation skills**: `openapi-spec-generation` (`wshobson/agents`, `.claude/skills/openapi-spec-generation/`) · `golang-structs-interfaces` · `golang-error-handling` · `golang-testing` · `golang-security` · `golang-naming` · `golang-project-layout` (all `samber/cc-skills-golang`, `.agents/skills/<name>/`) · `supabase-postgres-best-practices` (`supabase/agent-skills`, `.agents/skills/supabase-postgres-best-practices/`, for the provision and revoke SQL) · `multi-stage-dockerfile` (`github/awesome-copilot`, `.agents/skills/multi-stage-dockerfile/`) · `react-flow` (`.claude/skills/react-flow/`, handles and edges)

## Rationale

Reasoning and options: see [rationale.md](rationale.md).

## Cross child contracts

### Versions of the format

- Every file starts with `apiVersion: modular-platform/v1` and a `kind` (`Capability`, `Module`, `Graph`). Decoding stays strict (unknown and duplicate keys are errors), as 0002 pinned.
- Optional `formatVersion: "1.<minor>"`, default `"1.0"`. This release reads `1.0`. A later release adds optional fields under a higher minor and reads every lower one. A file whose minor is higher than the planner reads gets `format_too_new` ("needs a planner that reads format 1.<n>") at path `formatVersion`, before strict decoding, so a new field never gets the vague `invalid_entry`. Removing or changing a field needs `v2`.
- A writer (the editor, the Go canonical writer, the planner's resolved graph) writes `formatVersion` only when it is not `1.0`.

### Catalog layout and loading

```text
catalog/
  capabilities/<name>.yaml        kind: Capability, one per capability
  modules/<name>/module.yaml      kind: Module
  modules/<name>/<file>.tmpl      the module's templates, named in module.yaml
  embed.go                        //go:embed capabilities modules
```

- `name` equals the file name (capabilities) or the folder name (modules). Capability and module names share one namespace only by convention; the loader keeps them apart.
- `contract.LoadCatalog(fsys)` returns `Catalog{Modules, Capabilities}` and every problem; capabilities load first, so module rules can check field names. Problem paths are `catalog/capabilities/<name>.yaml` or `catalog/modules/<name>/module.yaml`.
- Templates load with their module and are checked at load (0013, *Templates*). Only the built in catalog exists until #13, so every template is release reviewed code.

### Shared naming rules

0002's naming rule (a DNS label, not a YAML reserved word) applies to capability, module, instance, slot, provide, config value, secret and setup step names; none of them can contain `_`, which every file and Compose name built from them relies on. Field names are snake case (0012). Env names follow 0005. Version strings and ranges follow 0012.

### API surface (planner contract)

| Endpoint | Method | Key inputs | Key outputs | Auth | Key errors |
|---|---|---|---|---|---|
| `/v1/catalog` | GET | none | 200 `Catalog {modules: [Module], capabilities: [Capability]}`, each sorted by name; template bodies are not served, only `templates[*].path` | public mode none; internal binding token | 429, 503 |
| `/v1/plan` | POST | `Graph` v1 or v0alpha1 (JSON) | 200 `PlanResult {ok, graph (v1, resolved), bindings, problems, digest}` | as today | 400 `invalid_graph`, `unsupported_version`, `wrong_kind`, `format_too_new`; 413, 429, 503 |
| `/v1/render` | POST | `Graph` | 200 zip; 422 `PlanResult` with an error | as today | as plan, plus 422 |
| `/v1/render/managed` | POST | `ManagedRenderRequest {graph, deployment}` | 200 `RenderedProject`, now with `publishedPorts` always empty for deployed graphs, `secrets[*].kind` `generated` or `placed` (externals), `field` any capability field name | internal only | 422 also for `host_access`, `setup_required` as errors |

- `Binding` grows to `{consumer, slot, capability, provider, provide, auto, named, provisioned}`: `provide` is the provide the slot bound to; `named` is true when the planner wrote default named values; `provisioned` is true when the provide's capability yields the slot's capability. Sorted by consumer then slot.
- `Problem` keeps its shape; new codes are listed in 0012 and 0013, each with a fixed English message template in `libs/planner`.
- The digest covers the resolved v1 graph, the bindings, the module entries the graph uses, and the capability files those entries name.

### Render flavors

`libs/render` has three flavors over one core: **static** (the zip from `POST /v1/render`), **managed** (a deployed graph for `deployer-docker`, refuses `host_access` and `setup_required`), and **platform** (the managed zip from the platform graph, allows host access and setup steps, adds the platform's own `.env` questions and the release image pins; 0015). Static and platform share `setup.sh` assembly; managed has none (the deployer creates generated secrets, as 0007 says).

### Value sourcing

| Action | Value produced / displayed | Source |
|---|---|---|
| Catalog load | capability fields, types, directions, `yields` | `catalog/capabilities/<name>.yaml` |
| Catalog load | module provides, slots, sources, delivery | `catalog/modules/<name>/module.yaml` |
| Plan | candidates | instances whose provide has the slot's capability, or a capability that yields it, with a version inside the slot's range (0012) |
| Plan | automatic binding, its text | the one candidate; `instance` when that instance has one matching provide, else `instance.provide` |
| Plan | default named values | 0014's rule from consumer instance and slot, written into the resolved graph |
| Plan | problem `message` | fixed template per code in `libs/planner` |
| Render | host name of an instance | `<instance>.<graph>.internal` (every instance now, not only sdk) |
| Render | networks of a service | `internal` always; `egress` from the entry's `network.egress` or `publish`, or from a slot bound to an external instance directly or through an external provisioner (0013) |
| Render | the wait anchor `mp_jobs`, its `depends_on` and image | rendered when a job has no dependent; waits on each such job; the `yields.job.image` of the first such job by service name (0014, *Waiting*) |
| Render | field value from source `platform` | 0013's platform source table (host name, provide port, url by the capability's `scheme`, internal root CA, TLS state) |
| Render | field value from source `value`, `config`, `secret`, `generate` | the literal; the provider's config value (as a Compose variable or env file line); the provider's own secret file; a per binding file `setup.sh` or the deployer creates (32 random bytes, hex) |
| Render | yielded field values | the provisioner capability's `yields.fields` map: admin fields, named values from the graph, per binding generated secrets (0014) |
| Render | external field values | the graph's `external.provides.<p>.fields`, or a file you place for secret fields |
| Render | template output | the template file plus 0013's template data |
| Render | asked value variable names | `MP_<INSTANCE>__<VALUE>`, upper case, `-` as `_` (0013) |
| Render | a non SDK config value's env, an SDK env file line | graph `modules.<i>.config.<v>`, else the catalog `default`, else empty (0025) |
| SDK start | fields of a slot with no client | the env its slot maps, typed by the capability file through `sdkgen` (0025) |
| Setup | asked answers, setup step outputs | you at `setup.sh`; the step's command in the module's image |
| Provision job | admin fields, named values, binding password | the provisioner provide's fields; the graph binding; the per binding generated file |
| Revoke job | logins to keep | the `login` named values of every current binding to that provide |
| Editor | handles, edges, versions, external marks | the v1 catalog answer and the graph |

### Key invariants

- No secret value is ever in a graph, a catalog entry, a template output, a Compose env value or the planner (0001 principle 6); secret and certificate fields travel only as files.
- A provision job only creates or updates what its binding names, a revoke job only disables logins marked as made by the platform, and neither ever drops data.
- The planner never guesses: an ambiguous candidate, provide or version stays an error until the graph says which.
- Host access (`publish`, `host_mounts`) is never rendered for a deployed graph.
- Every rendered value has exactly one source named in this table or a child's.

### Security model

- Templates, job scripts and setup steps come only from the built in catalog (release reviewed) until #13; graph files are untrusted, so every graph value that reaches a template, a job or a Compose field is a validated name, a version, or a named value checked against its field's `pattern`.
- A provision job sees the admin fields and its own binding's files only; a consumer sees only its own binding's files; a provider sees one file per consumer for `to_provider` and `both` fields.
- A provision job refuses to take over a role it did not make (marker membership), and the planner refuses a named login equal to the admin user.
- Host access is a warning in the online tool, an error for deployed graphs, and allowed only in the platform flavor.
- Hardening is on by default and each opt out is visible in the entry; setup steps run with `--network none` as you.
- `MP-Caller` tokens are checked against the key of the consumer whose binding token matched (0015).
- No compliance scope: no personal data beyond the admin email, which moves to the identity provider's binding.

### Configuration required

- `MP_DIR` (`.env`, written once by the platform flavor's `setup.sh`): the install folder, for `{install}` paths.
- `MP_<INSTANCE>__<VALUE>` (`.env`): each asked config value, each config value a Compose field names (0013), and every non secret config value of a non SDK instance (0025).
- `MP_CALLER_KEYS_DIR` (provider env from its `caller_key` field) replaces the bootstrap name `MP_CALLER_KEY_FILE`, which is removed.
- Provision and revoke jobs read `MP_CONSUMER`, `MP_SLOT`, `MP_CONSUMER_HOST`, `MP_MARKER`, `MP_KEEP`, `MP_ADMIN_<F>[_FILE]`, `MP_<F>[_FILE]` (0014); these are job inputs, not settings you set.
- `PLATFORM_*` settings of the hand written managed folder go away with it; their answers become asked values of `ingress` and `authelia` (0015).

## Build plan

Tracer Bullet: milestone 1 runs the whole v1 thread (catalog, planner, render, `setup.sh`, compose, editor, SDK) on the sample with a real provisioned database; later milestones thicken one strand each, and the platform graph comes last because it needs all of them.

**Milestone 1: v1 thread on the sample**
1. [x] Spike: on the Compose version CI pins, run `docker compose up -d --wait` on a project with one job (`restart: "no"`, exit 0) that a service depends on with `service_completed_successfully`, and once with the job exiting 1. Record the result in rationale.md. If `--wait` fails on the exit 0 job, switch the README, `scripts/smoke*.sh` and the deployer to the fallback written there (start, then `docker compose wait` on the job services, then `up -d --wait` on the long running ones), and keep AC-5's observable outcome, satisfies **AC-5**
2. [x] `contracts/openapi/planner.yaml`: `Capability`, `Module` v1, `Graph` v1 (string or object bindings, `external` instances), `Catalog {modules, capabilities}`, the grown `Binding`; `task generate`, satisfies **AC-2**, **AC-3**
3. [x] `libs/contract`: the new catalog layout and loader, capability rules, module rules needed by the sample (provides with versions and field sources, slots with ranges and env delivery), version grammar, `formatVersion`, v0alpha1 graph conversion, canonical v1 writer; move `catalog/` to the new layout with `capabilities/postgresql.yaml`, `capabilities/postgresql-provisioner.yaml`, `capabilities/http.yaml` and the `whoami` and `postgres` modules, satisfies **AC-2**, **AC-14**
4. [x] `libs/planner`: matching by capability or yields and version range, slots, named provides, automatic bindings, default named values written back, the new problem codes, digest over v1, satisfies **AC-3**, **AC-4**, **AC-7**
5. [x] `libs/render`: field sources and delivery, the provision job per provisioned binding, host names for every instance, the `data/` volume folders, `internal` network and hardening defaults for the sample's services; goldens move to v1, satisfies **AC-1**, **AC-5**, **AC-8**, **AC-12**
6. [x] The `postgresql-provisioner` job (image, provision script) and an end to end test (`e2e` tag) where a test module signs in to its provisioned database, and the sample `samples/whoami-postgres/graph.yaml` in v1, satisfies **AC-4**, **AC-5**
7. [x] `sdk/manifest` and `sdkgen` on v1 entries plus capability files; regenerate the three services' config, satisfies **AC-20**
8. [x] Editor: read the v1 catalog and graph, slot and provide handles, string and object form edges, version labels, the v1 YAML writer; shared fixtures in `contracts/fixtures/graphs/` move to v1 plus a `v0alpha1/` conversion set, satisfies **AC-19**, **AC-14**
9. [x] Paper platform: write the eight platform entries of 0015 (and their capabilities) as catalog fixtures under `libs/contract/testdata/platform/`, plus the platform graph, and make the catalog check and the planner accept them, no render yet. A field 0015 needs that v1 cannot say surfaces here, before milestone 2 builds on the format; route it back through `/architect` as a spec change, satisfies **AC-16**, **AC-2**
9b. [x] The client of a `for_each` step (0013, *Install time*, decided 2026-11-05): the `SetupStep` and `SetupWrite` descriptions in `contracts/openapi/planner.yaml` name `{client}` and `<folder>/{client}`, then `task generate`; `libs/contract` accepts `<folder>/{client}` writes under `for_each` and refuses every new `invalid_setup_step` case of 0013, one fixture each; the paper `authelia` entry drops `$MP_CLIENT` for `env: {CLIENT: "{client}"}` and writes `client-digests/{client}`, satisfies **AC-2**, **AC-16**

**Milestone 2: bindings in full**
10. [x] The revoke job for every provisioner provide with a `login` (empty `MP_KEEP` allowed), its per graph marker role, the `postgres` and `template1` connect revoke, and the stack's wait anchor `mp_jobs` (0014, *Waiting*) with one README line saying it only waits for the stack's jobs, the deployer guard's rules for it, and the deployer's live state leaving out `job: wait` containers; e2e for unbind and rebind, including the last binding, and for a failing revoke job on a first and a second `up`; goldens with and without `mp_jobs`, satisfies **AC-6**, **AC-4**
11. [x] External instances in planner, render (placed files, the missing file list in `setup.sh`) and editor (external node), satisfies **AC-15**, **AC-1**, **AC-19**
12. [x] Remaining slot features: fixed path mounts, optional fields, `per_caller`, direction `to_provider` and `both` fields with the clients directory per field, `.Clients` data, `duplicate_binding`, satisfies **AC-8**, **AC-13**, **AC-18**
13. [x] `egress` network, every hardening opt out and `tmpfs`, labels on services and jobs, module `environment`, `formatVersion` checks end to end; golden per case, satisfies **AC-12**, **AC-14**
13b. [x] A consumer with a slot bound to an external instance, directly or through an external provisioner, joins `egress` by itself (0013, *Host name and networks*): the bindings golden's `app` joins it, `pg-roaming` drops its `network.egress` and the external e2e still passes, satisfies **AC-12**, **AC-1**

**Milestone 3: install time**
14. [x] Module `secrets` (`generate`, `ask`, `setup`, folders); config `setup: ask` and the `{uid}`, `{gid}`, `{install}` tokens as Compose variables; setup steps with `entrypoint`, `env`, `for_each` (one run per client, `{client}` replaced in env, command, entrypoint and writes, the client folder made even with no client, skip per client), `writes` modes; `setup.sh` assembly in 0013's order with answer checks and skip by outputs, satisfies **AC-10**
15. [x] Templates: loading, `delims`, the check at catalog load, rendering into `config/<instance>/`, mounts, one golden per template, satisfies **AC-9**
16. [x] `publish`, `host_mounts` (with `create`), `command`, `entrypoint`; warning `host_access` in plan; the managed flavor refuses `host_access` and `setup_required` and the deployer writes `MP_UID` and `MP_GID` into the project `.env`; widen the deployer guard only for keys the managed flavor now emits (networks, jobs, the `job` label, hardening, `data/` folders; the wait anchor's keys land with task 10), satisfies **AC-11**, **AC-12**
16b. [x] Linux capabilities on deployed graphs (0022): the `Hardening.cap_add` description in `contracts/openapi/planner.yaml`, then `task generate`; `libs/contract`'s kernel name list, `IsLinuxCap`, `DeployableLinuxCap`, `Module.UndeployableLinuxCaps` and the `invalid_hardening` rule with one fixture per case; the planner warning and the managed refusal name every reason; the guard's `cap_add` case, so `TestGuardAcceptsEveryRender` drops the `notifier` exception; update the guard steps in `verify.md`, satisfies **AC-2**, **AC-11**, **AC-21**
16c. [x] The guard requires `cap_drop` and `security_opt` on every service, with one test each for a service, a job and `mp_jobs`; the `CapEff` e2e `TestLinuxCapsEndToEnd` with its fixture catalog; 0007's guard list points to 0022, satisfies **AC-21**

**Milestone 3b: slot fields and config values (0025, before task 17)**
16d. [x] 0025 tasks 1 to 4: the Caddy and Compose spike, SDK getters for slots with no client, `planner`, `deployer-docker` and `api` on them, satisfies **AC-22**, **AC-23**, **AC-24**
16e. [x] 0025 tasks 5 to 7: non SDK config values as `.env` variables, the `.env` check, the graph `config` field through contract, planner and render, satisfies **AC-25**, **AC-26**, **AC-27**, **AC-28**, **AC-10**, **AC-11**
16f. [x] 0025 tasks 8 and 9: the inspector's config section, the paper platform entries and graph, satisfies **AC-29**, **AC-30**

**Milestone 4: the platform is a graph**
17. [x] Turn the paper platform into real entries with their templates and setup steps (0015): the Caddyfile, Authelia and Postgres templates, the `cert-issuer` and `authelia` steps, `api caller-key generate`; `deploy/managed/graph.yaml`, satisfies **AC-16**
18. [x] Caller keys as the `caller_key` field: `sdk/caller` checks per consumer keys, `MP_CALLER_KEY_FILE` removed, satisfies **AC-18**
19. [x] The platform render flavor, `task managed:render` into `build/managed/`, the zip task and release on it, the overlays applied on top, `scripts/smoke-managed.sh` fed the new question order; delete the retired files once `smoke-managed` passes on the rendered zip in the same change, satisfies **AC-16**

**Milestone 5: TLS for unchanged providers**
20. [x] Spike: `postgres` running as the setup user with its `data/` folder (initdb as a user with no passwd entry), reading a 0600 key written by the certificate job, and picking up a reissued certificate on `pg_reload_conf()`; record in rationale.md, satisfies **AC-17**
21. [x] The certificate provisioner (the `jobs` provisioner in the setup written `ca.json`, the `tls-certificate-provisioner` job), the `postgres` entry's optional `tls` slot and its two templates, the reload in the provision job, `api` with `verify-full`, and `postgres.tls: cert-issuer.certs` in `deploy/managed/graph.yaml` (milestone 4 ships it `null`, 0015 *Milestone 4 build findings*); replace the `tls-certificate` case of `tlsSlot` in `libs/render/bind.go`, and raise the job count in `scripts/smoke-managed.sh` to four (or derive it from the plan), satisfies **AC-17**

## Migration plan

**Strategy**: one cut per format, with a read time converter for graphs. Pre release (`0.0.0`), so no published module or release zip exists outside this repo.
**Phases**:
1. Milestone 1 lands the v1 catalog, planner, render and editor together; the built in catalog converts in that change, and v0alpha1 entries are refused from then on. Graph files, samples and stored deployments keep working through the converter.
2. Milestone 4 replaces the hand written `deploy/managed/` with the rendered folder. The platform graph keeps today's names (`{database: platform_api, user: api}`, `{database: authelia, user: authelia}`), but a managed install made before it has `pg_data` as a named volume and roles the provision job did not make (it refuses those), so an existing test install is reinstalled, not upgraded (upgrades are #25).
3. The converter stays until #4 sets a removal release for v0alpha1 graphs; the `v0alpha1/` fixtures stay with it.
**Rollback**: each milestone is one revertible merge; milestone 4 keeps the hand written folder until the rendered smoke job passes in the same change.
**Risks**: the `--wait` behavior (task 1, and for jobs nothing depends on, the wait anchor of task 10; a later Compose release that changes how `--wait` treats jobs could make the anchor unneeded, never wrong; one that stops running a finished job again on a later `up` would break the provision jobs too, and the revoke e2e in CI catches it, since CI takes the runner's Compose); a field the platform needs that v1 lacks (task 9 finds it early); postgres as a non root user (task 20); the editor and Go writers drifting on object form bindings (shared fixtures catch it).

## Consequences

**Positive**:
- A consumer never knows its provider: two providers of one capability are interchangeable by construction (#7), and external and internal providers look the same (#10).
- Databases and logins are made per consumer with no string typed twice, which #26 builds on directly.
- The platform is a graph (principle 2): #23 replaces a module by editing `deploy/managed/graph.yaml`, and the managed zip can no longer drift from the render.
- Strict decoding plus `formatVersion` means an old planner refuses a newer file loudly instead of running it wrong.

**Negative / tradeoffs**:
- The contract grows a lot (three kinds, field sources, jobs, steps, templates); module authors have more to learn, and the planner and render more to test.
- One more container per provisioned binding and per provisioner provide on every `up`; a large stack starts slower. A stack with a revoke job also keeps one idle `mp_jobs` container running, a service that is in no graph, only so `up --wait` sees every job finish.
- A consumer bound to an external instance joins `egress`, which opens a route to the whole internet, not only to that server; Compose networks cannot narrow it (#8 follow up).
- A template language joins the public contract; every template field becomes something v1 must keep.
- A `for_each` setup step skips a client whose files exist, so a client secret deleted by hand leaves a digest that no longer matches until you delete it too; a client's files stay after it is unbound. Rotation comes with #25 (0013, *Install time*).
- Default named values written into the graph make graph files longer, and a rename of a provisioned instance no longer follows the instance name.
- Revoking needs a marker in the provider (a role membership in Postgres), so a provisioner capability must define how it marks what it made.
- 90 day certificates on unchanged providers expire if a platform runs over 90 days with no `up`; #25's upgrades and an expiry warning are the mitigation.
- Existing test installs of the managed platform must be reinstalled at milestone 4.
- Dev and CI render the platform with `-image-tag`, so the smoke job tests a folder whose platform image tags differ from the release render's; everything else in the two renders is the same, and the golden pins the rest (0015 *Milestone 4 build findings*).
- Network isolation gets weaker in one way: today `web` and `ingress` sit only on `edge`, away from `postgres` and `deployer-docker`; with one `internal` network per stack every module can reach every other by address, and only binding tokens, database logins and TLS stand between them. Accepted for #3; per binding networks are a follow up.

**Neutral**:
- 0002 stays in force for the graph file outside bindings until #4; 0005's catalog fields move into v1 unchanged in meaning.
- `deploy/online/` stays hand written (#22 is deferred).

## Follow-up

- [x] (done 2026-11-04) On confirmation, set spec 0002's status to `Superseded in part by [0011](../0011-capability-module-contract/index.md) (catalog entry, matching and binding render rules)`, and add pointers in 0001 (principles 2, 4 and 9 now specified here), 0005 (its #3 follow up answered) and 0006 (one shot jobs answered by 0014; `MP_CALLER_KEY_FILE` replaced).
- [ ] #4 builds the graph file on Graph v1 (positions, settings, storage) and decides how long the v0alpha1 converter stays.
- [ ] #6 adds editing of named values and external instances, and preferences between several candidates.
- [ ] #9 lets the managed deployer run setup steps, asked values and asked secrets, lifting `setup_required`.
- [ ] #8 replaces the blunt `host_access` rule and checks that a provider really speaks its capability version; it should also weigh per binding networks, so a module reaches only what it binds.
- [ ] #6's editor errors should say why an instance offering both `postgresql` and `postgresql-provisioner` is ambiguous, until #6 adds preferences.
- [ ] #21 lets a `per_caller` slot hold several providers.
- [ ] #25 runs `up` on upgrade, which also renews provider certificates; until then, consider an expiry warning in `api`.
- [ ] #31 brings the certificate provisioner to deployed stacks.
- [ ] The deployer's live state (0007, `deploy/state.go`) lists every container as a module row by its `instance` label, so provision and revoke jobs show as exited rows under their provisioner (`mp_jobs` is left out, task 10). Decide with task 16 (or in 0007) whether job containers become their own list or are left out too.
- [ ] Decide whether a deployed graph may run a module as root (`hardening.user: root`, rendered `user: "0:0"`) or with `writable_root`; the guard accepts both today. 0022 kept this out of its scope.
- [ ] #8 should weigh narrowing `egress` for consumers of an external instance to that server alone.
- [ ] `sdk/CONTRACT.md` CALL-6 names the problem path `provides.<p>.caller_keys_dir`; the code (`sdk/sdk.go`) uses the capability name, so CALL-6 should say `provides.<capability>.caller_keys_dir` (0015 *Milestone 4 build findings*).
- [ ] `/sync`: note the catalog layout in root `AGENTS.md` and `sdk/AGENTS.md`, and the platform render in `deploy/managed/AGENTS.md` once it exists.
