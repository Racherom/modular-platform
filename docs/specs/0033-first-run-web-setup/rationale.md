# 0033. First run setup on the web: rationale

## Context

Milestone 4's platform `setup.sh` asks the domain, the origin, the admin email, and Authelia's admin user name and password on a terminal. Spec 0016 removes the script: you install with one `docker run` of the seed, and nothing may ask or print a secret. The answers and the first admin therefore have to come from a browser, before and after the platform runs.

Three facts of the current code shape the problem. Authelia lives on `auth.{domain}` (spec 0026) and needs a domain with a dot, so no identity module runs before the domain is known. `api` refuses to start without its public origin and its `oidc` binding. And `api` binds the first admin by matching the admin email answer against a verified email claim, which fails for an identity module whose users carry no email (a wallet with OID4VP, the case scope #37 names).

Constraints from your answers and the project: the keeper is the only writer of the state folder (0016); the process holding the Docker socket is root equivalent; one cut over with 0016's task 4, so no throwaway interim path on milestone 4; every module the platform ships with stays replaceable (#23), so the setup must not name Authelia; Authelia has no sign up page and no user API, and adds a user only through its users file (file backend) or a directory (LDAP backend).

Order mattered too. Spec 0016's task 4 waits for #37, while #37 needs the keeper that task 4 builds. You chose one cut over: #37's pieces land first, tested on their own, and both switch together.

## Options considered

The load bearing choice was who serves phase 1 (the code, the session, the answers) before the domain is known. Phase 2 (enrollment and the admin) always needs the running platform, because the identity module needs the domain.

### Option 1: the seed runs a throwaway setup container (chosen)

Before it installs, the seed starts `deployer-docker setup` from its own image: no socket, unprivileged, read only, publishing the HTTPS port with its own self signed certificate. The answers come back as files the seed writes into `.env` at install.

**Pros**:
- The platform has no setup mode: it always starts with its answers.
- The socket holder never answers the network.
- No new image to build, release or pin.

**Cons**:
- The deployer binary gains a third command and HTML pages.
- The code and session are checked in two services (setup container and `api`).

### Option 2: the seed serves the setup itself

Your first idea: the seed process serves phase 1 on the HTTPS port, then installs.

**Pros**:
- One container less than Option 1; same simple flow.

**Cons**:
- The process with the Docker socket answers the network before anyone is signed in; any bug in that path is root on the host.
- The `docker run` line gains `-p 443:443`.

### Option 3: a setup module in the platform graph (0016 as first written)

A setup module runs in a Compose profile beside a Caddy catch all site on a self signed certificate; the keeper writes the answers through a call and reconciles again.

**Pros**:
- Isolated and replaceable like any module.
- #40 wants a platform core without a domain anyway.

**Cons**:
- The platform must reconcile without answers, Caddy needs a second site shape, and profiles must switch at run time.
- A module you use once stays in the graph for good.

### Option 4: the api in a setup mode

`api` serves both phases, so code, session and binding live in one service.

**Pros**:
- One place for every setup rule.

**Cons**:
- `api` must start without its origin and its `oidc` binding: optional bindings in the SDK, postgres and a Caddy catch all in a setup mode.

## Rationale

Option 1 is the only one that removes the setup mode entirely without putting the socket on a public port. Removing the setup mode deletes three of 0016's four setup mode promises (reconcile without answers, the ingress's setup site, the answers call) and keeps the platform in one shape, which is what makes 0016's reconcile and Compose's rule simple to trust. You raised the concern that a one time setup module is waste; Option 1 meets it the same way your Option 2 does (the throwaway part belongs to the seed), and costs only one container more to keep the socket away from the network. Reusing the deployer image means no extra build line before the first release and a setup that always matches its seed.

Phase 2 follows from the facts in Context. Authelia's normal way to add a user is its users file, which the keeper already writes in a setup step, so the enroll is that step run on demand with its questions asked in the browser. Binding the admin by the setup session instead of by email makes the setup independent of the identity module's claims, and the session moves to the domain through the probe that the blocking DNS check needed anyway.

### Smaller choices

| Choice | Picked | Why | Runner up |
|---|---|---|---|
| Sequencing with 0016 | one cut over in 0016 task 4 | your choice; no throwaway interim path, no service recreating its own project | #37 first on milestone 4 with the deployer writing `.env` |
| Scope of #28 | split: three ordered children here, #28's seam as its own spec next | your choice; each piece buildable after the one before | an umbrella child for #28's seam |
| Setup image | the deployer image, command `setup` | your choice; no new image | its own small distroless image |
| Phase 1 pages | server rendered Go, no JavaScript | your choice; small, no build step, nothing extra in the image | the editor's React build in the deployer image |
| Form values | every `setup: ask` value plus TLS (`tls-mode`, `acme-email`, `base-path` marked asked) | your choice; a replaced module's questions appear by themselves | only today's asked values |
| DNS check | blocking: your browser opens a probe link on each name | your choice; proves what matters, works with hosts files and split DNS, and carries the session to the domain | a server side fetch (fails without NAT hairpin, passes falsely for `*.localhost`) |
| Session lifetime | 24 hours from redemption | your choice; time for DNS and certificates, short for a leaked cookie | until setup done |
| Session format | signed cookie (expiry plus HMAC) with a shared generated key, upgraded at the bind | your choice after the Caddy question: Caddy keeps no sessions; a shared key needs no store or handover; a separate key makes a used code worthless | a key derived from the code (anyone who saw the code could mint sessions) |
| Recovery | delete the code, restart the keeper, redeem the new code at `<base>/setup`; before Install, restart the seed instead | your choice; no reinstall, answers kept | reinstall |
| Cookie `SameSite` | `Lax`, POSTs guarded by `Sec-Fetch-Site` | the cross check: the sign in callback is a redirect from the identity module's host, which `Strict` may drop; the api's login cookie is `Lax` already | `Strict` |
| Code rate limit | 10 a minute per address, no total | the cross check: a total limit lets anyone on the network lock you out, and a 142 bit code needs none | per address plus a total |
| Enroll session check | the keeper also verifies the setup session (`X-Setup-Session`) | the cross check: the internet facing api's token alone cannot replace the users | the api checks it alone |
| Enroll in phase 1 instead | no, enroll stays in phase 2 | your choice after the cross check proposed it: a wrong password is fixed by enrolling again, and the module is ready when you register | ask the enroll questions in phase 1 and run the enroll at install |
| Admin email | moves into enrollment; `admin_email` and the email rule go | your choice; the email belongs to the enrolled user, and a module without email must work | keep it as an install answer |
| Enroll contract | a separate `enroll` block | your choice; its rules differ from prepare's (on demand, replaces its writes, only before setup done) | `enroll: true` on a setup step |
| Authelia enroll | the enroll writes `users.yml`, Authelia reloads it (`watch: true`, folder mount) | your choice; it is Authelia's own way to add a user | an LLDAP directory behind Authelia |
| Keeper calls | new capability `platform-setup` with its own contract | your choice; a future deploy target (#33) need not carry setup | three paths in `deploy-target` |
| Enroll errors | declared `pattern` and `min_length` checked first, then a generic failure | your choice; a step's output could echo an input | the step's last stderr line |
| Without a session | every page leads to `<base>/setup`, code field only | your choice; no dead end sign in | the normal sign in page |
| Retire | at the first successful sign in, which binds the admin | your rule: the admin signs in once as a test, and success finishes the setup | a Finish setup button |
| Re enroll | allowed until an admin is bound | your choice; a typo costs one more form | once per session |
| Second identity module | moved to the spec that makes the identity module replaceable (#23) | your answer | a test OIDC module in this spec |
| Where "used" is kept | one file, `code-used`, written by the seed or the keeper with no replace | one store, atomic "once" across both phases, no migration | a `setup_code_uses` table in `api` |
| Placed files | phase 1 asks every missing one; `beforeSetup` and `placed_missing` go | nothing runs before phase 1, so nothing needs a file earlier | keep 0016's refusal |
| Setup certificate | made by the seed, fingerprint logged | one log to follow (`docker logs modular-platform-seed`), and you can compare it with the browser warning | made inside the setup container |
| Shared session code | `libs/setupsession`, pure | one implementation for both services, testable with a clock argument | a copy in each service |
| Setup files in `api` | SDK getters that read at each call | the files change while `api` runs; only `sdk/` reads files (AGENTS.md) | `api` reads `/run/secrets/setup/` itself |

### Authelia facts used

From what is known of Authelia 4.39 (not web verified; the spike checks the load bearing ones): it has no sign up page and no user management API; the file backend reads a users YAML whose passwords are argon2 or other digests made with `authelia crypto hash generate`; `authentication_backend.file.watch: true` reloads the file on change; password reset needs an existing user and a notifier. A single file bind mount keeps the old inode when the host file is replaced by rename, so the users file needs a folder mount.

### Spike results

To be recorded by umbrella task 1: the empty users file at start, the reload through a folder mount, the probe in Chromium on a container that is not Caddy, and the cookie path from `/setup/probe`.
