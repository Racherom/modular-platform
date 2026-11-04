# 0011. Capability and module contract: rationale

The decision record behind [index.md](index.md). `/develop` does not need this file.

## Context

The platform runs end to end on the provisional `v0alpha1` format of spec 0002, extended by spec 0005 (the SDK's `runtime`, `config`, `client` and `auth` fields) and stretched by spec 0006 (a full managed control plane). That format was written to be thrown away: a capability is a bare name matched exactly, a module can need a capability only once, a binding delivers a host and a port and nothing else, and database credentials are typed in by hand into `config/<consumer>.env` (0005). Spec 0001 promised more and pointed every open part at #3: provisioning capabilities and their one shot services (principle 4), per binding credentials (principle 8), setup fragments and secret declarations (principle 9), the platform's own services as catalog modules (principle 2), and binding fields for an issuer URL, a CA bundle and a public origin. Spec 0006 added two more: how one shot jobs are expressed and waited for, and certificates for unchanged providers so `api` to `postgres` can leave plain text.

The forces: #3 is the public contract, so anything it publishes is hard to take back once third party modules exist (#13 brings shared catalogs). The format must keep 0002's strictness (the planner never guesses, both writers produce identical text) and 0005's running config declaration. The managed platform exists today as a hand written folder (`deploy/managed/compose.yaml`, `db-init/init.sh`, a 206 line `setup.sh`) that drifts from the catalog by hand. Secrets must never reach the planner, a graph or a zip (principle 6), yet #26 promises that no string is typed twice. The team is small and the tier is GA, so every rule needs a test that CI runs. The release is still `0.0.0`: no published module or zip exists outside this repository, which makes this the cheapest moment to change shapes.

Left undecided, #4, #6, #7, #10, #23 and #26 would each extend `v0alpha1` in their own direction, and the hand written managed folder would keep growing beside the catalog.

Engineer's choices in this session: a new umbrella spec superseding 0002 in part; #3 bumps both Module and Graph to v1; the managed zip rendered from a graph; the TLS contract in #3, used by the platform; a Capability kind in the catalog; version on provides, range on uses, dotted numbers with comparisons; named slots and named provides; bindings as `instance` or `instance.provide`; env per field with secrets as files; a declared source per field; postgresql split into a database capability and a provisioner capability; one provision job per binding, run at every `up`; resource names chosen in the graph with defaults the planner writes back; provision steps are the only one shot jobs; logins revoked but data kept on unbind; provision and revoke commands on the provisioner capability; setup steps with declared outputs; asked config shared through bindings; externals as graph instances; host access as declared fields refused outside the platform graph; rendered templates over graph facts only; secure hardening defaults with opt outs; internal plus egress networks; `per_caller` per slot, sdk only; strict decoding with additive minors; v0alpha1 graphs converted on read; certificates as a capability; the editor reads, shows and saves v1; fields may flow to the provider.

## Options considered

### Option 1: Fix in place (grow `v0alpha1`)

Keep the format and add fields as each later feature needs them: version keys on `provides`, a `credentials` block, a `jobs` list.

**Pros**: no migration; each addition is small and lands with the feature that needs it.
**Cons**: the shapes that hurt most (one need per capability, exact name matching, credentials typed by hand) are structural, so later features would bolt around them; every addition would still be "provisional", so module authors get no stable target; the hand written managed folder keeps drifting.

### Option 2: Replace directly with v1, one thin thread first (chosen)

Design the whole contract now, cut the catalog over in one change, convert graphs on read, and build it as a Tracer Bullet: the sample first, the platform last.

**Pros**: one coherent contract that later features extend additively; pre release, so the cut is cheap; the read time converter keeps stored deployments and saved graphs working; the platform render proves the contract against eight real modules.
**Cons**: a large spec and a large first milestone; the planner, render, SDK generator and editor change together, so the first milestone is a wide merge.

### Option 3: Replace with a strangler (v1 beside `v0alpha1`)

Run both formats side by side: the planner reads either, the render handles both, entries migrate one by one.

**Pros**: each module moves alone; nothing breaks at any step.
**Cons**: two matching engines and two render paths to keep correct and tested under GA rigor, for a catalog of eight built in modules that one commit can convert; the strangler pattern pays off for live traffic and third party data, neither of which exists yet.

## Rationale

Option 2 fits the forces. The structural gaps (needs per capability, exact matching, hand typed credentials) cannot be fixed by adding fields, so Option 1 only postpones the break to a time when third party modules exist and it costs more. Option 3's safety is real for a running fleet, but here every entry lives in this repository and the only stored data is graphs, which a pure converter handles; two engines would double the test surface the GA tier must cover for no user. Building it as a Tracer Bullet keeps the risk of the large cut small: the sample stack runs end to end on v1 (with a real provisioned database) before anything else thickens, and the platform graph, the hardest case, comes last on top of proven pieces.

Within Option 2, the sub decisions are argued in each child's short rationale. Three calls I made that you did not explicitly choose, each with its runner up:
- **Backends bind to the ingress** (0015), forced by the planner's cycle rule once `authelia` and `api` need the origin from the ingress. Runner up: cycle exempt routing bindings, a new rule only for one case.
- **A per graph marker role** for revoke (0014), so two stacks on one server never disable each other's logins. Runner up: one global `mp_provisioned` role, simpler but unsafe for externals.
- **Every instance gets a host name** `<i>.<graph>.internal`, not only sdk ones (0013), so a certificate for an unchanged provider has a name to carry. Runner up: bare service names for unchanged images, which no certificate policy can scope per stack.

## Spike results

**Build task 1, `up --wait` with jobs (2026-11-04, Docker Compose v5.6.1, Docker Engine 29.9.2).** CI does not pin a Compose version; it takes the one the `ubuntu-24.04` runner ships, so this ran on the newest local release. A project with a job (`restart: "no"`, `sleep 2; exit <n>`) and a long running service that depends on it with `service_completed_successfully`:

- Job exits 0: `docker compose up -d --wait --wait-timeout 60` exits 0; the job is `exited 0`, the service `running`. A second `up` on the running project starts the job again, waits for it, and exits 0, so the provision job runs on every `up` as 0014 needs.
- Job exits 1: `up` exits 1 with `service "job" didn't complete successfully: exit 1`; the service stays `created` and never starts.

So `--wait` already gives 0014's *Waiting* rule. No fallback is needed, and the README, `scripts/smoke*.sh` and the deployer keep `up -d --wait`.

To be filled by build task 20: `postgres` running as the setup user with its key file written by the certificate job, and its reload of a reissued certificate.

## References

**Project sources**:
- Spec [0001](../0001-platform-stack-architecture/index.md): principles 2, 4, 6, 8, 9 and 12; its follow up for #3.
- Spec [0002](../0001-platform-stack-architecture/0002-provisional-graph-format.md): naming rules, strict decoding, canonical text, matching rules, secret naming; its follow up for #3.
- Spec [0005](../0005-module-sdk/index.md): catalog fields, binding keys, clients directory, `MP-Caller`, the render's env files; its follow up for #3.
- Spec [0006](../0006-managed-mode-control-plane/index.md) and children 0007, 0008, 0009: the managed render, the guard, the cert issuer, the hand written folder; the one shot job follow up.
- Scope #3 and the features it hands off to (#4, #6, #7, #8, #9, #10, #21, #23, #25, #26, #31) in `docs/scope/scope.md`.
- `deploy/managed/compose.yaml`, `deploy/managed/setup.sh`, `deploy/managed/db-init/init.sh`, `libs/contract/`, `libs/render/`.
- Your answers in this `/architect` session (2026-11-04), listed in Context.

**Practices & standards**:
- Semantic versioning ranges as a matching rule, limited to dotted numbers
- Least privilege per binding (one job sees one password)
- Idempotent provisioning (ensure, never drop)
- Ingress controllers that learn routes from the services behind them
- Strict decoding with explicit format versions

**Links** (web verified on 2026-11-04):
- [Docker Compose `up` reference (`--wait`, `--wait-timeout`)](https://docs.docker.com/reference/cli/docker/compose/up/); it does not say how `--wait` treats a job that exits, hence build task 1
- [Compose services reference (per network `aliases`)](https://docs.docker.com/reference/compose-file/services/)
- [PostgreSQL 18 `ALTER ROLE`](https://www.postgresql.org/docs/18/sql-alterrole.html)
- [PostgreSQL 18 `ALTER DATABASE` (`OWNER TO`)](https://www.postgresql.org/docs/18/sql-alterdatabase.html)
- [PostgreSQL 18 psql (variable interpolation, `\gexec`)](https://www.postgresql.org/docs/18/app-psql.html)
- [step-ca provisioners (per provisioner `claims`)](https://smallstep.com/docs/step-ca/provisioners/)
- [`step ca certificate` reference](https://smallstep.com/docs/step-cli/reference/ca/certificate/)
- [Go `text/template` (`Option("missingkey=error")`)](https://pkg.go.dev/text/template)
