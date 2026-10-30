# 0004. Managed mode first build order

**Date**: 2026-10-30
**Parent**: [0001. Platform stack and architecture](index.md)

## Summary

The first thing we take end to end is now managed mode (the platform runs on the user's own host and deploys there), not the hosted online tool. The online tool is only useful once someone runs it as a public service (domain, open ports, TLS, flood protection), while managed mode runs where the user already is, at `platform.localhost` with a local certificate. Everything built so far stays and gets reused; public hosting (#22) is deferred and the online stack leaves CI until it comes back. The first managed thread keeps full sign in and Postgres, leaves out the NATS event bus for now, and runs on the provisional graph format of [0002](0002-provisional-graph-format.md).

## Context

> ⚠️ Premise note: the first usable thread gets heavier. Static slice 1 needed three containers and no sign in; the first managed thread needs seven (plus the one shot `db-init`), OIDC sign in and a deployer that holds the Docker socket (root on the host). The first release anyone can use moves later, and the provisional formats carry more weight for longer. A lighter framing exists (ship the online tool as something each user runs locally), but that is managed mode without the deploy, so it buys little. The decision below accepts the heavier thread on purpose and cuts what thread 1 does not need.

Spec 0001 put the static path first: slice 1 is the online tool (`ingress`, `web`, `planner` in public mode) plus a downloaded zip, and managed mode starts at scope #16 in slice 8. The scaffold for that order is mostly built: the planner service in both modes, the editor showing the catalog and any graph it opens (the sample, or a `graph.yaml` file), the zip render with `setup.sh`, `deploy/online/` and a CI smoke job that runs the online stack.

The static path has a precondition outside the code. Nobody can use it until the online tool runs as a public service on your network (#22): a public domain, ports 80 and 443 open to the ingress, ACME renewal, patching, protection against floods, a privacy note, and an ongoing duty to keep it up. For a small open source team that is operations work before any user gets value, and it stays work as long as the tool is online. Managed mode has no such precondition: the user downloads the platform and runs it on a host they already control.

The forces: the build approach is Tracer Bullet (every slice runs end to end, later slices thicken a thread that already works), and the workflow tier is GA (the platform handles sign in and secrets). Managed mode as 0001 pins it is eight containers: `ingress`, `web`, `api`, `planner`, `deployer-docker`, `postgres`, `nats`, `authelia`. The module contract (#3) and the platform data model (#4) are not written yet; 0001 already allows a hand written `deploy/managed/` graph until #3 exists.

Left undecided, the next slice either waits on public hosting or builds managed features on top of a static thread no user can reach.

## Options considered

### Option 1: Keep 0001's order, host the online tool

Finish slice 1 as planned, then run the online tool publicly (#22) so users can design and download zips; managed mode follows in slice 8.

**Pros**:
- The thinnest first thread (three containers, no sign in, no database), already mostly built.
- Users need no install to try the planner.

**Cons**:
- Nobody uses it until you operate a public service, and you keep operating it.
- Your planner sees every user's stack design (never secrets), so the privacy promise has to hold from day one.
- Managed mode, the part bigger setups need, waits seven slices.

### Option 2: Managed mode first, full 0001 wiring

Move #16 to slice 1 and build all eight containers as 0001 pins them, NATS and JetStream included.

**Pros**:
- The final shape from the start; no wiring to add back later.
- Deploy progress survives restarts through JetStream.

**Cons**:
- Nothing in thread 1 needs events that survive restarts; NATS adds a container, one credential per binding and subject permissions for no visible gain yet.
- The heaviest possible first thread.

### Option 3: Managed mode first, a narrowed first thread

Move #16 to slice 1 with full sign in and Postgres, leave NATS out (the api polls the deployer and relays state to the editor over SSE), run on the provisional `v0alpha1` format and a hand written `deploy/managed/` graph, and deploy any graph the editor can open. Defer #22 and take the online stack out of CI.

**Pros**:
- No public service to run before users get value; the user's host is the whole runtime.
- Seven long running containers instead of eight; the sign in and secrets design stays exactly as 0001 pins it, so no second security design to retire.
- Reuses everything built: planner internal mode, render, the editor's open and view.

**Cons**:
- In flight deploy progress is lost if the deployer restarts mid apply.
- NATS returns later (#17 to #19), with its wiring added then.
- The frozen online tool can break without anyone noticing.

### Option 4: Managed mode first, loopback only without sign in

Like Option 3, but publish the ingress only on 127.0.0.1 with no sign in for thread 1, guarded by `Origin` and `Host` checks; sign in follows as its own slice.

**Pros**:
- The thinnest managed thread: no Authelia, no OIDC on the critical path.

**Cons**:
- Any local process or user can drive the deployer, which is root on the host.
- A security model built to be thrown away, and a gap a GA tier project should not ship even once.
- A middle path (one admin token or password that `setup.sh` generates and `api` checks itself, OIDC later) would close the hole and save Authelia, its database and the CA plumbing, but it is still a second auth design to build, secure and then retire.

## Decision

**Chosen option**: Option 3: managed mode first, a narrowed first thread.

Build scope #16 as the first slice, with full OIDC sign in and Postgres, without NATS, on the provisional formats, deploying any graph the editor opens; defer public hosting (#22) and remove the online stack from CI.

## Rationale

The hurdle is operations, not code. Option 1 makes value depend on you running a public service forever, which is the one cost a small team cannot shrink by writing better code. Managed mode puts the runtime on the user's host, where the user already accepts the work of running containers.

Between the managed options, the cut follows what thread 1 has to prove: install, sign in, deploy, see state (the #16 done line). Sign in stays because the deployer is root equivalent on the host; Option 4's saving is real, but it ships a root level hole and then a second auth design (basis: OWASP Docker Security Cheat Sheet, Rule 1). Postgres stays because sessions, the bootstrap admin binding and Authelia all need durable storage, and the one shot `db-init` proves provisioning (0001 principle 4) early. NATS goes because nothing in thread 1 needs events that outlive a restart; polling a deployer on the same host is simple and enough, and a broker added before a need is measured is a known failure pattern (basis: premature optimisation). Option 2 would carry that cost from day one.

Running on the provisional format first and then writing #3 means the public contract is designed against a platform that actually runs, instead of on paper.

## Design

### Revised build order (for `/scope` to apply)

| Order | Feature | Change |
|---|---|---|
| Foundations | #1 Stack & architecture | Finishes without the online smoke job in CI (see *Online tool while deferred*) |
| Foundations | #2 Coding standards, #5 Design system & graph editor foundation | Unchanged |
| Slice 1 | #16 Managed mode control plane | Moved from slice 8; narrowed as in *First managed thread* |
| Slice 2 | #3 Capability and module contract, #4 Platform data model | Move after #16; replace `v0alpha1`, the hand written `deploy/managed/` graph and the thread 1 tables |
| Slice 3 | #6 Design online, download, compose up | Becomes graph editing in the editor; in managed mode it deploys, and download stays |
| Slice 4 | #23 Replace the platform's own modules, #24 Planner website standalone or in your site | Right after the real contract, which #23 needs |
| Later | #7, #8, #9, #10, #12, #13, #14, #15, #17 to #21 | Keep their relative order |
| Deferred | #22 Run the public online tool | Moved to Deferred until you choose to host it |

### First managed thread (constraints for the #16 spec)

**In thread 1**: `ingress` (Caddy), `web` (managed build), `api`, `planner` (internal mode), `deployer-docker`, `postgres` with the one shot `db-init`, `authelia`. Sign in, cookies, the bootstrap admin, per binding credentials, networks, secrets and the security baseline exactly as 0001's *Managed mode wiring* pins them.

**Left out of thread 1**:
- The `nats` module, its `setup.sh` fragment, its bindings from `api` and `deployer-docker`, and the `nats_data` volume. They return with the first feature that needs events which survive restarts (#17 or #19 decides).
- Graph editing (place, bind, remove): stays with #6.
- A sub path placement and replacing platform modules: stay with #24 and #23. Thread 1 runs at the site root only (`PLATFORM_BASE_PATH` empty), so Authelia under a nested base path is not on its critical path.
- An upgrade path: none in thread 1. A newer managed zip is installed over the old folder, keeping `secrets/` and the volumes; goose migrations run at `api` start as pinned. A real upgrade path is its own scope feature (see Follow-up).

**What the user deploys**: any graph the editor opens (the built in sample, or a `graph.yaml` file), planned by the internal planner with 0002's matching rules and applied by the deployer as one Compose project per deployment. The editor in managed mode also keeps *Download zip*, since the render path is shared.

**Refused graphs and plan review** (an interim guard until #8 and #13; imported graph files are untrusted input and the deployer is root on the host):
- The internal planner refuses, with a plan problem the editor shows, any graph whose rendered project asks for `privileged`, a host bind mount, host networking (`network_mode: host`), the Docker socket, or a published port equal to `PLATFORM_HTTP_PORT` or `PLATFORM_HTTPS_PORT`. The deployer checks the same rules again before it applies, so a bypassed planner cannot get past them.
- A deployment's Compose project name can never be `modular-platform` (the platform's own project) and never starts with it; the deployer refuses such a name.
- The editor shows the plan (modules, bindings, published ports, images) and applies only after you confirm it.
- Other clashes (a host port already in use, a pull failure) surface as a failed apply with Compose's error text.

**Deployment lifecycle in thread 1**:
- Actions: apply (create, or redeploy the same deployment with a changed graph) and remove (`docker compose down` for that project; volumes are kept unless you choose to delete them, and #16 names that option).
- One apply or remove at a time per deployment; a second request while one runs gets 409. Different deployments may apply in parallel.
- Each apply runs `docker compose up -d --remove-orphans` within that deployment's own project only, so a module dropped from the graph is removed and nothing outside the project is touched.
- `docker compose up -d` is idempotent, so applying the same graph again is always safe; that is the recovery for any failed or unknown apply.

**Secrets for deployed graphs** (needed by the sample's Postgres password before #9 exists): the deployer creates each missing plain random secret at apply time, with the same rule as `setup.sh` (32 random bytes as hex, mode 0600, never overwrites an existing file), in `secrets/<module>/` inside that deployment's folder under the deployer's render directory on the host. No value ever leaves the deployer: not to `api`, not to the planner, not to logs. Remove keeps the secrets folder with the volumes. A graph that needs a special format secret (a setup fragment, principle 9) is refused in thread 1 with a plan problem; #9 brings fragments, user supplied secrets and rotation.

**Deploy progress and state without NATS**:
- The `deploy-target` REST contract carries everything `api` needs: start an apply or remove (returns an operation id at once), read an operation's status, and read the container state of a deployment. Every response carries the deployer's boot id (a random value it picks at each start). `api` polls these and relays changes to the editor over the SSE stream at `<base>/api/v1/events`.
- `api` runs one poller per deployment, shared by every open SSE stream, and polls only while an operation runs or at least one stream is open. While nobody watches, a container that crashes goes unnoticed until someone looks (accepted for thread 1). The interval, the status values and the endpoint shapes are #16's to fix.
- The deployer keeps in flight status in memory only and runs with `restart: unless-stopped`. When `api` sees a new boot id, or on its own start, it marks every operation it has not seen finish as `unknown`; the editor shows that state with *Apply again*.

**Storage**: Postgres with goose migrations and sqlc, as pinned. Thread 1 creates only the tables it needs: server side sessions, the bootstrap admin binding (`iss` plus `sub`), deployments, and an append only record per apply or remove holding the graph document it used and its outcome (history is never overwritten; the deployment points to its latest apply). #16 fixes their columns; #4 designs everything beyond them, including whether files or the database are the source of truth.

**Editor screens**: #16 builds its own screens on the current editor styling, without waiting for #5: the sign in redirect and signed out state, the deployments list, plan review and confirm, apply and remove with progress, the `unknown` state, and each module's state drawn on the graph. #6 later adds graph editing to the same editor.

**Managed zip in thread 1**: the hand written `deploy/managed/` folder (Compose file, config templates, `versions.env`, and a hand written `setup.sh` that follows principle 9's shape: platform questions first, then the bundled `postgres` and `authelia` steps run inside their own images), packed into `modular-platform-<version>.zip` by a task and attached to releases by CI. The platform's own modules are described in the provisional `v0alpha1` catalog format where they can be. After #3, `libs/render` produces the same folder from the platform graph (0001, Neutral consequences).

**Dev loop**: `task dev` keeps running the online graph (0001 AC-2); #16 adds `task dev:managed`, the managed graph with Caddy routing `<base>/` to the Vite dev server on the host, as `task dev` does.

**Done line for #16** (unchanged in substance, NATS dropped from it): you install the managed zip on a clean host, run `setup.sh` and `docker compose up -d`, sign in as the admin, deploy a graph from the editor to that host, and the editor shows the running graph with each module's state. For thread 1 the managed zip may be built locally by a task; CI attaches it to releases.

**CI for thread 1**: #16 adds a managed smoke job: start `deploy/managed/` with `PLATFORM_TLS=internal`, sign in as the admin headlessly (Playwright, as in `web/smoke/`), deploy the sample graph, and wait until the editor reports every module running.

### Online tool while deferred

- The code stays and keeps compiling and passing unit tests: planner public mode, the rate limiter, `deploy/online/`, the base path support in `web`.
- The CI `smoke` job that starts `deploy/online/` is removed from `.github/workflows/ci.yml`. `task smoke` stays in the Taskfile for manual runs; nothing runs it automatically.
- Release images for `web` and `planner` keep being built; the `web` image serves both placements, so managed mode uses it.
- When #22 comes back, its spec restores the online smoke job first.

### Changes to spec 0001 (made with this spec)

- `## Structure` lists this spec.
- `## Summary`: slice 1 is managed mode; public hosting of the online tool is deferred.
- **AC-6**: the release part stays; the CI smoke job part moves out (online smoke frozen, managed smoke comes with #16). **AC-7**: checked by `task smoke` locally while frozen; #24 checks the sub path again for managed mode.
- *Managed mode wiring*: a note that thread 1 leaves out `nats` per this spec.
- `## Follow-up`: NATS subjects and streams move from #16 to the feature that brings NATS back.
- Every other place that assumed NATS in the first managed thread, the online smoke job in CI, a static first slice, or eight containers now points here.

## Consequences

**Positive**:
- No public service to operate before anyone gets value; nothing about users' stacks passes through your network.
- The part bigger setups need (deploy to the host, sign in, live state) arrives first.
- All scaffold work is reused: internal planner, `libs/render`, `libs/target`, the editor, the `web` image.
- The real contract (#3) is designed against a running platform.

**Negative / tradeoffs**:
- The first usable thread is about twice the size of static slice 1: seven long running containers plus `db-init`, OIDC on the critical path, a root equivalent deployer, two new Go services (`api`, `deployer-docker`) and two new contracts.
- Users must install the platform to try anything; there is no zero install path until #22 returns.
- A deployer restart mid apply loses that apply's progress; the editor can show only the outcome `api` stored, or that it is unknown.
- The frozen online tool may break unnoticed (public mode, sub path checks); #22 pays that back when it returns.
- NATS wiring (fragment, per binding users, subject permissions) is added later, on a running platform, instead of at install design time.
- The provisional `v0alpha1` format now carries a full control plane; #3 has more to migrate, and the hand written `deploy/managed/` folder must be kept in line with it by hand until then.
- The interim refusal rules are deliberately blunt: some legitimate modules (one that needs a host mount) cannot be deployed until #8 and #13 replace them with real trust rules.
- The deployer now owns secret creation for deployed graphs until #9; a flaw there exposes those secrets on the host (they never leave it).
- No upgrade path: moving to a newer platform version is a manual reinstall over the old folder until the upgrade feature lands.

**Neutral**:
- Scope #1's milestone "CI with the smoke job" changes meaning: it ticks with generate, lint, test, build and release images; the smoke job belongs to #16.
- The CI change and the scope reorder are follow ups for `/develop` and `/scope`; this spec writes neither.

## Follow-up

- [ ] Run `/scope` to apply the revised build order above (move #16 to slice 1, #3 and #4 after it, then #6, then #23 and #24; #22 to Deferred; reword #1's CI milestone).
- [ ] `/develop stack & architecture`: remove the `smoke` job from `.github/workflows/ci.yml` and keep `task smoke` for manual runs.
- [ ] `/architect managed mode control plane`: design #16 within the constraints above (`api.yaml` and `deploy-target.yaml` with the polling endpoints and boot id, the thread 1 tables, the refusal rules as plan problems, label keys, Compose project naming, the admin reset, the managed smoke job, plus 0001's existing #16 follow ups except NATS).
- [ ] Enroll a scope feature for upgrading a managed install (new images, migrations, `setup.sh` steps added by a newer zip), placed after #23.
- [ ] The feature that brings NATS back (#17 or #19) owns the NATS subject names, streams and per binding permissions that 0001 had assigned to #16.

## References

**Project sources**:
- Spec 0001 (`index.md`): the managed mode wiring, principles 4 and 9, the Neutral note allowing a hand written `deploy/managed/` graph.
- Spec 0002: the provisional `v0alpha1` format and matching rules thread 1 runs on.
- `docs/scope/scope.md`: Build approach Tracer Bullet, Workflow GA, the #16 and #22 rows.
- Your answers in this `/architect` session (2026-10-30): hosting is the hurdle; child spec of 0001; keep the code and park hosting; full OIDC from day one; defer NATS and poll the deployer; provisional format now and #3 after; deploy any graph the editor opens; Postgres with a minimal schema; #3, then #6, then #23 and #24; freeze the online tool without CI.

**Practices & standards**:
- Tracer bullet development, walking skeleton
- OWASP Docker Security Cheat Sheet, Rule 1 (socket access is root on the host)
- Premature optimisation (no broker before a measured need)
- Server Sent Events for one way push to the browser

**Links**:
- [Docker Security, OWASP Cheat Sheet Series (Rule 1: do not expose the Docker daemon socket)](https://cheatsheetseries.owasp.org/cheatsheets/Docker_Security_Cheat_Sheet.html)
- [Docker Engine security (Docker daemon attack surface)](https://docs.docker.com/engine/security/)
- [OpenID Connect 1.0 configuration, Authelia](https://www.authelia.com/configuration/identity-providers/openid-connect/)
- [Define services in Docker Compose (`depends_on`, `service_completed_successfully`)](https://docs.docker.com/reference/compose-file/services/)
- [Server sent events, WHATWG HTML Living Standard](https://html.spec.whatwg.org/multipage/server-sent-events.html)
- [JetStream, NATS Documentation](https://docs.nats.io/nats-concepts/jetstream)
- [Tracer Bullets and Prototypes, interview with Andy Hunt and Dave Thomas](https://www.artima.com/articles/tracer-bullets-and-prototypes) (verified 2026-10-29 for the scope)
