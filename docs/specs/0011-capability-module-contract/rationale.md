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

**Build task 10, jobs nothing depends on (2026-11-05, Docker Compose v5.6.1).** Task 1 only covered a job a service depends on; the revoke job has no dependent, and `up -d --wait` then failed at random in the revoke e2e. A project with a long running service, a job that exits 0 at once and a job that sleeps 3 seconds then exits 1, none depended on: three `up -d --wait` runs each exited 1 with `container ... exited (0)` for the fast job, while the failing job was reported `Healthy` because it was still running at the check; its exit 1 came later and nothing reported it. Whether `up` fails depends on timing, and a failure can pass unseen.

Two fixes were spiked, each over four `up` runs (first `up`, same config again, config changed, job made to fail):

- **Anchor service** (chosen): an idle service that depends on the job with `service_completed_successfully`. Exit 0, 0, 0, 1 as expected; the job ran on every `up`, and with the anchor already running a failing job still made `up` exit 1 with `service "revoke" didn't complete successfully: exit 1`.
- **Job that idles after its work**: the script ends with `touch /run/mp/done; exec sleep infinity`, and a healthcheck (`start_interval: 1s`) tests the file. Exit 0, 0, 0, 1 as expected, but with the same config the second `up` left the running container alone, so the work ran only when the config changed.

A third option, a Compose profile plus `docker compose run --rm` after `up`, was not spiked: it changes the command everyone runs, and a plain `up` would skip the revoke work silently.

You chose the anchor: one meaning for "job" (one shot, runs on every `up`, ends `exited 0`), one idle container per stack instead of one per provisioner provide, and the same anchor covers any later job no module waits for.

**Egress for consumers of an external instance (2026-11-05).** `internal` is `internal: true`, so it has no route out of the host. The bindings golden's `app` is bound to the external `billing` and through the external provisioner `shared`, joined only `internal`, and so could not reach either; the external e2e passed only because its test consumer declares `network.egress`. You chose to have the render join such a consumer to `egress` by itself, matching the rule for provision jobs of an external provisioner, over requiring `network.egress` in the entry (which breaks AC-1's "binds unchanged" and grants the internet to every instance of a module, even one bound only inside the stack).

**The client of a `for_each` step (2026-11-05).** 0013 said `{client}` in a `for_each` step's `writes` names expands to the consumer instance, but the catalog check refused any `{` in a write name, so the paper `authelia` entry wrote the bare folder `client-digests/` and named each file from `$MP_CLIENT` inside its command, an env name no spec defined. The skip rule for that folder ("holds a file per current client") had to guess the file names. Options weighed:

- **`{client}` where the module places it** (chosen): one token, replaced in `env` values, `command` and `entrypoint` items and `writes` names, only under `for_each`. The module names its own env, so an image you do not own reads the client under the name it already uses, and a binary entrypoint gets it as an argument with no shell wrapper. Writes are `<folder>/{client}`, so the skip is exact per client. Con: one more token to learn, and a literal `{client}` cannot be written in a `for_each` step (no case needs one yet).
- **A fixed `MP_CLIENT` env**: what the paper entry already used, beside `MP_INSTANCE` and `MP_GRAPH`. Con: the platform picks the name, so an image expecting another name needs a `sh -c` wrapper, and a binary entrypoint cannot use it at all.
- **`for_each: {provide, env}` or a `client_env` field**: explicit. Con: a second way to name env next to `env`, and it still does not reach arguments or file names.

You also chose: every write of a `for_each` step carries `{client}` (a shared write would be written by the first run and skipped or overwritten by the rest); only the exact text `{client}` expands, so shell and awk braces stay as written; the client folder is made even with no client, so the module's mount exists; a file of an unbound client stays, since `setup.sh` never deletes a secret; and `env` keys starting with `MP_` are refused. `MP_GRAPH`, `MP_INSTANCE` and `MP_ASK_<V>` stay fixed for now. A cross check on a second model then raised five gaps, settled as recommended: no `asks` on a `for_each` step (AC-10 asks once); `{provide}` reads only in a `for_each` step, of its own provide, `to_provider` or `both` fields; the folder of `<folder>/{client}` is a declared `from: setup` folder and the file name is the bare client; a later step reads the whole folder only; and a client secret deleted by hand leaves a stale digest, written down as a known limit until #25 (the runner up, rerunning when a read file is newer than the output, adds mtime logic for a case only hand edits reach).

**Build task 20, `postgres` with a certificate from a job (2026-11-06, Docker 29.9.2, `postgres:18.6-alpine3.24`, `smallstep/step-cli:0.30.2`).** `postgres` ran as the setup user (`--user 1001:1001`, a uid with no passwd entry) with `--read-only`, `--cap-drop ALL`, `no-new-privileges`, tmpfs `/var/run/postgresql` and `/tmp`, an empty `data/` folder owned by that user, and the two templates' settings (`ssl = on`, TLS 1.3 only, `hostssl` lines only):

- First start: `initdb` and the server both came up as that user, and the server accepted a key file of mode 0600 owned by that user, bind mounted as one file at `/run/tls/server.key`. A `step-cli` job running as the same user, `--read-only` with `/tmp` as tmpfs, wrote `/out/cert` and `/out/key` with `cat > file`, so the server's mount saw the files with no restart.
- A client with `sslmode=verify-full` and the root CA signed in over TLSv1.3. A client with `sslmode=disable` got `no pg_hba.conf entry ... no encryption`, which is AC-17's refusal of a plain connection.
- Reissue: after a second job run wrote a new certificate in place, a new connection still saw the old serial. After `SELECT pg_reload_conf()` the next connection saw the new serial and expiry, and the log said `received SIGHUP, reloading configuration files` with no restart. So the reload belongs in the provision job, as 0014 says (it is already the last statement of the `postgresql-provisioner` provision script).
- Not covered by the spike: the real `step ca certificate` call against `cert-issuer`; task 21's e2e does that.

**0025 task 1, default texts in Compose and env files (2026-11-06, compose-go v2.16.2).**

- compose-go: `environment: {E: "${V:-<d>}"}` read back by `loader.LoadWithContext`, with `V` unset and with `V` set to empty (both give `<d>`). These texts round trip: `$` written `$$`, `#`, `"`, `'`, spaces at either end, `{`, `{x}`, `:-`, `=`, `\`, non ASCII letters. A `}` anywhere but at the very end does not: `a}b` reads back as `ab}`, `x}y}z` as `xy}z}`. 0025 already refuses `{` and `}`.
- `sdk/envfile`: a line `K=<text>` round trips every text above, `}` included, except a text that starts and ends with the same quote: `"q"` and `'q'` read back as `q`, as EF-2 says. One sided quotes round trip. So an SDK instance's `config/<i>.env` line `<ENV>=<text>` (AC-27) changes such a text unless it is refused too. You chose to refuse it as `invalid_config` on every instance, SDK or not (0025, *Graph text rules*).
- Also seen, `caddy:2.11.7-alpine`: `{$VAR:default}` takes the default only when the variable is unset; set to empty, Caddy keeps the empty value. This is why 0025 never renders `MP_WEB_UPSTREAM`.

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
