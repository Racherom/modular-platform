# 0006. Managed mode control plane: rationale

Decision record for [index.md](index.md). `/develop` reads `index.md` and the child specs; this file explains why.

## Context

> ⚠️ Premise note: this first thread is large. On top of spec 0004's seven containers it now carries an eighth (`cert-issuer`), automatic rollback, and placed credentials entered in the editor, which pulls a slice of #9 forward. Each one is a real gain, and each adds failure paths to the thread that has to work first. The build plan therefore keeps milestone 1 to the bare done line (sign in, deploy the sample, see it running) with the platform's services still plain, and adds review, recovery, rollback and then TLS as later milestones of the same feature, so the thread runs before it thickens and the unverified issuer port never blocks it.

Scope #16 is the first slice (spec 0004): you download the platform, run it on your own host, sign in as the one admin, deploy a graph and watch its modules. Specs 0001, 0004 and 0005 already fix the outer shape: the services, OIDC through Authelia, server side sessions in Postgres, the bootstrap admin rule, the cookie and Origin rules, no NATS (polling instead), the interim refusal rules, one operation at a time per deployment, the deployer's boot id, and SDK services with binding tokens and an `MP-Caller` token. What remains open is how the pieces split the work: who plans and renders for a deployment, who keeps history, who decides recovery, what crosses each contract, what the tables hold, and how the platform's own services get TLS.

The forces: the deployer holds the Docker socket and is root on the host, so every line of code in it is host level risk. Graph files are untrusted input. The team is small and the workflow tier is GA, so whatever ships must be testable end to end in CI. The `v0alpha1` format is provisional (#3 replaces it), so the design must not lean on anything #3 will remove. Users run this on hosts they own, often at home, where reboots and Docker restarts are normal.

Left undecided, `/develop` would have to invent the API and deployer contracts, the tables and the recovery behavior while building, and the first thread would set precedents (where history lives, what the deployer knows) that later features inherit.

## Options considered

### Option 1: `api` orchestrates, deployer executes files

`api` keeps every record (sessions, deployments, operations), asks the planner for a managed render, sends the files to the deployer, polls it, and decides rollback. The deployer validates and applies files, creates secrets, and reports operation status and container state from memory.

**Pros**:
- The root equivalent service stays small: no database, no planner, no catalog, no recovery logic.
- One place (`api`'s database) answers "what ran, when, by whom"; history survives deployer restarts.
- Principle 5 holds literally: the deployer is a `Target` that applies a `render.Output`.

**Cons**:
- More traffic between services (render through `api`, polling), and `api` owns more logic.
- Progress during an apply is lost on a deployer restart (accepted in spec 0004).

### Option 2: the deployer owns lifecycle and history

The deployer keeps its own store on disk (per deployment folder history), plans through the planner itself, and decides rollback; `api` only proxies and handles sign in.

**Pros**:
- Recovery and history sit next to the thing that applies, so an apply and its record never disagree.
- `api` stays thin.

**Cons**:
- The service that is root on the host now parses history, calls other services and makes decisions; a bigger attack surface exactly where it hurts most.
- Two stores (Postgres for sessions, the deployer's disk for history), against spec 0001's "all state in one place".

### Option 3: desired state folder, reconciling deployer

`api` writes the desired rendered project into each deployment folder; the deployer watches the folders and reconciles (applies whatever changed), reporting status files back.

**Pros**:
- Idempotent by design, close to GitOps; a crashed deployer simply reconciles again on start.
- No apply endpoint at all.

**Cons**:
- `api` needs write access to the deployments folder, so a flaw in `api` can feed the host directly; the guard runs only on the deployer side, after the fact.
- Status through files and watchers is harder to test and slower to report than a request and answer.
- Rollback, `busy` and `unknown` all become file conventions instead of contract fields.

## Rationale

Option 1 follows from the one force that dominates: the deployer is root on the host. Everything that can live elsewhere should, and history and recovery can (basis: OWASP Docker Security, rule 1; spec 0001's "privileged deploy, only holder of the Docker socket"). `api` already has the database, the session and the user, so the record of what happened, and the decision of what to do next, belong there. Option 2 is attractive for consistency, but it moves decisions into the most dangerous process and splits state across two stores. Option 3 is elegant for a cluster target, yet here it hands `api` write access to the host's deployment folders and turns every status into a file protocol.

The remaining calls, each with its runner up:

- **Deployment identity is the graph name, project `mp-<name>`.** Spec 0005 already ties SDK host names to the graph name, so one deployment per name avoids two stacks claiming the same names. Runner up: a separate deployment name, which would need render to swap the graph name everywhere.
- **Apply succeeds only when healthy (`--wait`, 300 seconds) after a separate pull (10 minutes).** "Succeeded" must mean the stack works, not that Compose created containers (basis: the `docker compose up` reference, `--wait`); separating the pull keeps a slow network from eating the health budget. Runner up: plain `up -d`, faster but "succeeded" could be a crash loop.
- **Progress is per module state, not Compose's output.** The state read already exists for the live view; parsing Compose's progress format would tie the deployer to a text format. Runner up: `--progress json` events.
- **Deployment state derived, never stored.** It is computed from the latest operation, so it cannot go stale (basis: append only history and derived state). Runner up: a stored `state` column.
- **Plan digest.** It makes "what you reviewed is what runs" a checked fact for the price of one field. Runner up: replanning without a check, nearly always identical, never proven.
- **Polling at 1 second while busy, 5 seconds while watched, none otherwise.** Fast enough to feel live during an apply, cheap when idle, and spec 0004 already accepts unnoticed crashes while nobody watches. Runner up: a flat 2 seconds.
- **Separate `cert-issuer`, platform services only.** Details and runner ups in child [0008](0008-managed-tls-cert-issuer.md).
- **Planner renders a managed flavor; `api` relays files.** Keeps catalog and render in one service and the deployer free of both. Runner up: the deployer renders, see child [0007](0007-deploy-target-deployer.md).
- **Deployments folder inside the platform folder.** No root needed at install, and one folder to back up. Runner up: `/var/lib/modular-platform`, which spec 0001 suggested, but `setup.sh` would then need sudo.
- **Placed credentials through the editor (your call).** I recommended refusing them in thread 1 and leaving them to #9. You chose to upload them at plan review. That works and removes a confusing failure for modules that bind to an existing database, at the cost of pulling a slice of #9 forward: `api` handles a secret value in memory once, so the spec adds strict rules (never stored, never logged, never returned, a test that greps for them). #9 later adds rotation and other secret kinds on top.
- **Sessions: configurable (your call), default 1 hour idle and 12 hours max, hashed tokens.** You asked for the lifetime to be configurable for the user facing side; the defaults are longer than OWASP's guidance for high value apps (idle 2 to 5 minutes, absolute 4 to 8 hours), chosen for a single admin on their own network, and the bounds (idle at most 24 hours, max at most 30 days) stop absurd values. Hashing the token means a database dump holds no live session (basis: OWASP Session Management Cheat Sheet).
- **Sign out of platform and provider (your call), with `prompt=login` as the fallback.** The fact check found that Authelia lists RP initiated logout as unsupported, so with the bundled provider there is no `end_session_endpoint` (basis: Authelia's OIDC support chart and issue #5057; OpenID Connect RP Initiated Logout 1.0). To keep your intent, the first sign in after a sign out asks the provider to prompt for credentials again; a provider that does list `end_session_endpoint` gets a real sign out.
- **Signed out behavior configurable (your call), landing on the index by default.** Nothing about your stacks is visible without sign in; the setting only decides between a landing page and a direct redirect.
- **Admin reset as a command in the `api` image.** Host shell access is the right gate for recovering the admin of a host level tool. Runner ups: documented SQL (error prone, leaves sessions alive) or an env flag (a forgotten flag unbinds on every restart).
- **`MP-Caller` from `api` on every call.** It exercises spec 0005's token in the first thread and gives the deployer's log a who. Runner up: deployer only.
- **One `operations` table, a single row admin binding, sessions swept every 10 minutes, history kept forever.** Small, matches spec 0004's append only history, and leaves retention to #4 and #15.
- **Automatic rollback (your call), only after a failure at `up`, as its own operation, one retry.** I recommended leaving a failed apply in place with an *Apply this version* action, because rollback before #19 runs a rarely exercised path. You chose automatic rollback with a history that shows the deployment is rolled back and what failed. The design keeps it safe: it runs only when the start step failed (a refused graph or a failed pull changed nothing), it is a normal apply of a graph that already worked, every attempt is its own history row, and a second failure stops with both errors shown. The cost is a recovery path that must be tested on purpose (a dedicated integration test) and that can still fail when an old graph meets a newer catalog.
- **_Apply again_ reviews the newest apply's graph, carried in `Deployment.latestApplyGraph` (decided 2026-11-03).** "Apply again" means retry what you last tried. After an `unknown` apply or a failure at `prepare` or `pull`, the newest apply holds your change; the latest succeeded apply would quietly drop it, or reapply what already runs and change nothing. After a failed first apply nothing has succeeded, so only the newest apply has a graph to offer. After a failed rollback both choices give the same last good graph, because the rollback is itself an apply. Going back to the old version stays something you do on purpose. Runner up: the latest succeeded apply through the existing `Deployment.graph`, with no contract change but with the three gaps above. The field sits on `Deployment`, beside `graph`, and not on `Operation`, so history pages, list rows and `operation` events never carry a graph of up to 1 MB each (runner up: `Operation.graph`).
- **A failed or `unknown` remove offers _Remove again_ and _Apply again_ (your call).** I recommended *Remove again* alone, so the banner repeats what you tried. You chose both, so you can also bring the stack back from the same banner. *Apply again* there reviews the newest apply's graph, as above. The cross check (2026-11-03) added the actions per state, a fresh read on click instead of the cached deployment, and the rule that after a rollback the graph that failed at `up` is reached only by deploying it again from the graph screen, because offering a graph known to fail at start as a one click retry invites a loop of rollbacks.
- **Restart policy `unless-stopped` on deployed services.** Hosts reboot; a managed stack should come back like the platform itself. Runner up: no policy, as in the static zip.
- **One review endpoint.** The editor shows exactly what `api` computed, in one round trip. Runner up: the editor assembles review data from several calls.
- **Deployments routes with a review dialog, badges on nodes.** A dialog keeps the graph being reviewed in memory without storing it; badges with text satisfy accessibility rules that color alone breaks.

## Evidence: fact check (2026-11-03)

Run once during the design conversation by a read only researcher. Items marked "to verify" go to Build plan task 1 (Authelia, Compose) and task 15 (step-ca).

| Fact | Finding | Confidence |
|---|---|---|
| Authelia RP initiated logout | Support chart lists it as "None"; issue #5057 open | confirmed |
| Authelia `email_verified` for file users | Treated as true for file backend users | likely, to verify |
| Authelia session cookie name | `session.name`, overridable per `session.cookies[]` entry | likely, to verify |
| step-ca custom tls-alpn-01 port | No documented flag found; RFC 8737 names port 443 | unknown, to verify (fallback in child 0008) |
| step-ca x509 name policy | `authority.policy.x509.allow.dns` | confirmed |
| step-ca default duration | `authority.claims.defaultTLSCertDuration` | confirmed |
| step-ca lost ACME account | Answers "Account does not exist" (HTTP 400); `accountDoesNotExist` on order creation is an open request (#1472) | likely, to verify (spec 0005 asked) |
| step-ca `NotBefore` backdate | A hardcoded 1 minute in older versions (#927) | likely, to verify |
| Compose `up --wait` | Waits for running or healthy, implies `-d`; `--wait-timeout` in seconds | confirmed |
| Compose `--wait` with one shot services | Behavior differs across versions | unknown; avoided for the platform install |
| `docker:<version>-cli` | Includes the compose plugin | confirmed |
| Caddy and SSE | `text/event-stream` is flushed at once by `reverse_proxy` | confirmed |
| Postgres 18 `uuidv7()` | Built in | confirmed |

## Evidence: cross check (2026-11-03)

A read only review on a second model, applied in full on your pick:
- Named sources for the OIDC issuer address, CA file and `PLATFORM_ORIGIN` (pointers to spec 0001); literal image tags instead of `versions.env`; Authelia claims from userinfo plus `one_factor` and implicit consent.
- Secret entries carry `instance`, `capability`, `field`; `invalid_project` checked through labels; strict secret paths with `O_NOFOLLOW`; replaced placed values written only after the pull.
- Apply dispatch order (deployment upserted first), a deployer error table, `unknown` after 60 seconds of failed polls or 20 minutes, resumed polling at `api` start when the boot id matches (refines spec 0004), a background `MP-Caller` subject.
- Expected modules from the latest apply that reached `up`; `restartCount` and `lastExitCode` so a fast restart still shows; event payload schemas; a session cookie for `prompt=login`; atomic admin binding; `id_token` stored only when it can be used.
- The guard became an allow list (a deny list missed `external` networks, `volumes_from`, container networking and more); TLS moved to its own milestone; `cert_issuer_db` volume; rollback still checks the plan; one shot modules named as out of `v0alpha1`.

## Evidence: spike, Build plan task 1 (2026-11-03)

Run by `/develop` against `authelia/authelia:4.39.28` behind `caddy:2.11.7-alpine` (`tls internal`), file backend users, the `mp-api` client as this spec configures it, and Docker Compose v5.6.1.

| Question | Result |
|---|---|
| `platform.localhost` as Authelia's cookie domain, `server.address: tcp://:9091/auth` | Accepted; Authelia starts and serves under `/auth` |
| `email`, `email_verified` for a file backend user | In the userinfo answer (`email_verified: true`), **not** in the ID token, which holds only `sub`, `amr`, `auth_time` and the standard claims. Reading userinfo first is required |
| `one_factor` with `consent_mode: implicit` | The code is issued right after the password, with no consent screen |
| `end_session_endpoint` in discovery | Absent, as expected; sign out falls back to `<base>/` |
| `prompt=login` with a live Authelia session | Honored. Authelia shows its consent page (*Accept* / *Deny*, plus *Logout* and *Switch user*) even under implicit consent, then asks for the password, then issues the code. So AC-4 holds, at the cost of one extra click. A replay of the API calls without its frontend is refused (`require_login`), so headless tests drive the real page |
| `compose up -d --wait` on a service without a healthcheck | Counted healthy as soon as it starts. A service that exits two seconds later still passes `--wait`. A service with a failing healthcheck fails `--wait` at once with `container ... is unhealthy` and exit 1; a container that cannot start fails it too |

Consequence for the build: a module without a healthcheck that crashes right after start is not caught by `up --wait`; it shows later as `exited` or `restarting` in module state, and the operation stays `succeeded`. This matches AC-9 as written (it can only wait for healthchecks that exist).

## Evidence: spike, Build plan task 15 (2026-11-03)

Run by `/develop` against `smallstep/step-ca:0.30.2` with the hand written `ca.json` of child 0008, a CA from `step ca init --pki`, and the SDK's TLS test module (`sdk/internal/tlsmodule`) as the client, on one Compose network with no route out.

| Question | Result |
|---|---|
| tls-alpn-01 on 8443 with the binary | Works. `step-ca` 0.30.2 has `--acme-tls-port`, which needs `--insecure`; `--insecure` only unlocks the two ACME port flags. The challenge went `valid` on 8443 and the module logged `certificate held`. The fallback image from step-ca's Go packages is not needed |
| A lost ACME account | HTTP 400 `urn:ietf:params:acme:error:accountDoesNotExist` ("Account does not exist") on the next order, probed with a fresh, empty database behind the same name. This is the answer the SDK's client registers again on (spec 0005 asked) |
| `NotBefore` backdate | 1 minute. `NotAfter` is the issue time plus exactly 24 hours, so the leaf lives 24 hours and 1 minute, and `maxTLSCertDuration: 24h` still accepts it. The SDK's 1 minute skew tolerance and 30 second renewal floor cover it |
| Name policy | A module named `tm.other.internal` gets `urn:ietf:params:acme:error:rejectedIdentifier`; `*.modular-platform.internal` names are issued |
| Hardening | The binary carries a file capability (like Caddy's), so under `cap_drop: [ALL]` it fails with `exec ... operation not permitted` unless `cap_add: [NET_BIND_SERVICE]` |
| Healthcheck | The issuer's own certificate names only `cert-issuer.modular-platform.internal`, so `step ca health` must call that name, not `127.0.0.1` |
| Database volume | A fresh named volume at `/home/step/db` (a path the image does not have) comes up owned by root, so a service running as `PLATFORM_UID` cannot write it. `cert_issuer_db` stays a named volume, backed by `${PLATFORM_DIR}/data/cert-issuer`, which `setup.sh` creates and you own |
| Logs | The access log carries each issued leaf in base64 and never a key or the password. A leaf repeats its intermediate's subject as its issuer name, so in the full smoke run one line of `intermediate_ca.crt` matched a logged leaf by chance; the smoke job's secret scan now skips `*.crt`, which are public |

Consequence for the build: `cert-issuer` runs the stock image with `--insecure --acme-tls-port 8443`; the root certificate is made 0644 by `setup.sh`, since it is public and services read it as `PLATFORM_UID` or `nonroot`.

## References

**Project sources**:
- Spec 0001 (`index.md`): principles 5, 8, 9 and 12, *Managed platform*, *Managed mode wiring*, the #16 follow up.
- Spec 0004: the first managed thread constraints (no NATS, polling, boot id, refusal rules, secrets, tables, smoke job).
- Spec 0005: binding tokens, `MP-Caller`, TLS mode and ACME, the render additions, its #16 follow ups.
- Spec 0002: graph names and the `v0alpha1` format.
- `libs/render/render.go` and `sdk.go`: the static render and its placed credential files; `libs/target/target.go`: the `Target` interface.
- `AGENTS.md`: Tracer Bullet, GA, the domain rules (pure `libs/planner` and `libs/render`, contract first, sentinel errors, slog only).
- `docs/scope/scope.md`: the #16 row and its done line.
- Your answers in this `/architect` session (2026-11-03).

**Practices & standards**:
- OWASP Session Management (idle and absolute timeouts, server side enforcement)
- OpenID Connect RP Initiated Logout 1.0 (`end_session_endpoint`, `id_token_hint`, `post_logout_redirect_uri`)
- OWASP Docker Security, rule 1 (the socket is root on the host)
- Backend for frontend sessions (tokens never reach the browser)
- Append only history and derived state (no stored derived values)
- Tracer bullet development

**Links** (web verified on 2026-11-03):
- [OpenID Connect, Authelia](https://www.authelia.com/integration/openid-connect/introduction/)
- [Authelia issue #5057, RP initiated logout](https://github.com/authelia/authelia/issues/5057)
- [step-ca configuration](https://smallstep.com/docs/step-ca/configuration/)
- [step ca init reference](https://smallstep.com/docs/step-cli/reference/ca/init/)
- [step-ca issue #1472, accountDoesNotExist](https://github.com/smallstep/certificates/issues/1472)
- [step-ca issue #927, ACME backdate](https://github.com/smallstep/certificates/issues/927)
- [RFC 8737, ACME TLS ALPN challenge](https://datatracker.ietf.org/doc/html/rfc8737)
- [docker compose up reference](https://docs.docker.com/reference/cli/docker/compose/up/)
- [Docker official image (cli variant)](https://hub.docker.com/_/docker)
- [Caddy reverse_proxy directive](https://caddyserver.com/docs/caddyfile/directives/reverse_proxy)
- [PostgreSQL 18 UUID functions](https://www.postgresql.org/docs/18/functions-uuid.html)
- [OpenID Connect RP Initiated Logout 1.0](https://openid.net/specs/openid-connect-rpinitiated-1_0.html)
- [OWASP Session Management Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html)
- [OWASP Docker Security Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Docker_Security_Cheat_Sheet.html) (verified for spec 0004)
