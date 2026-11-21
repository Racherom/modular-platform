# 0033. First run setup on the web

**Date**: 2026-11-20
**Status**: Proposed
**Scope**: [#37 First run setup on the web](../../scope/scope.md)
**Builds on**: [0016](../0016-platform-one-container/index.md) (the seed and the keeper), [0013](../0011-capability-module-contract/0013-module-manifest-v1.md) (install time, setup steps), [0015](../0011-capability-module-contract/0015-platform-graph-managed-zip.md) (the platform graph), [0026](../0001-platform-stack-architecture/0026-identity-provider-own-host.md) (Authelia on `auth.{domain}`), [0006](../0006-managed-mode-control-plane/index.md) (sign in and the admin binding)
**Amends**: 0016 *Setup mode* (points 1 to 3 replaced, see *What this changes in 0016*)

## Summary

Installing the platform asks nothing on the terminal and prints no secret. Before the seed (the first container, spec 0016) installs anything, it starts a throwaway setup container that serves a setup page on a self signed certificate. You open it with a one time setup code that you read from a root only file. There you enter the install answers and prove that every DNS name reaches this host; then the platform starts with its domain. Once the identity module runs, you register your first user through it on the platform's setup page, sign in once as a test, and that first sign in makes you the admin and retires the setup.

## Structure

The work splits into three child specs, built in this order; each builds on the one before.

| Child | What it is | Decision it supports | AC range |
|---|---|---|---|
| [0034](0034-setup-before-install.md) | Phase 1: the seed's setup container, the setup code, the session, the answers form and the DNS probes, written at install | who serves the setup before the domain is known, how the code is checked, how the answers reach the state folder | AC-10 to AC-19 |
| [0035](0035-enrollment-seam.md) | The enrollment seam: the `enroll` block of an identity module, the keeper's `platform-setup` capability, Authelia's enroll | the seam an identity module offers for enrolling its first admin | AC-20 to AC-29 |
| [0036](0036-first-admin-session.md) | Phase 2: the platform's setup page, recovery with a new code, the admin bound at the first sign in, the setup retired | how the session reaches the platform, how the code is retired | AC-30 to AC-39 |

The sign in seam of #28 (what the platform learns after a sign in) is its own spec, next after this one; *Rules for #28* below lists what it must keep.

## Requirements

**User stories**:
- As a self hoster, I want to install the platform without answering questions on a terminal or seeing a secret printed, so that nothing secret lands in my shell history or a log.
- As a self hoster, I want to enter the install answers in a browser and learn before the install whether my DNS names reach this host, so that a typo does not leave a platform nobody can reach.
- As the first admin, I want to register with whichever identity module is installed and become the admin by my first sign in, so that this works when that module has no password at all.

**Acceptance criteria** (end to end; each child has its own range):
- **AC-1**: On a clean host, the README's commands (spec 0016 AC-1) are all you run: nothing asks on the terminal and nothing prints a secret value. The only secret you read is the setup code, by the path the README and the seed log name (`sudo cat`).
- **AC-2**: The managed smoke (spec 0016 task 4) runs the whole first run in Chromium: read the code, enter it, enter the answers, open each probe link, install, wait for the platform, enroll with Authelia, sign in, deploy the sample and see it running.
- **AC-3**: Each setup code is accepted once, in phase 1 or in phase 2, and refused afterwards everywhere; after the setup is done no code is accepted and none is made again.
- **AC-4**: Nothing in the setup names a specific identity module: the setup works with any module that provides `oidc`, one with an `enroll` block (Authelia, end to end here) and one without (its first sign in with a valid session binds the admin; a unit test here, end to end with #23).

## Decision

**Chosen option**: the seed runs a throwaway setup container for phase 1; the platform's api and editor run phase 2, with one session that moves from the first to the second.

Phase 1 runs before the install in a container the seed starts from its own image (`deployer-docker setup`: no Docker socket, unprivileged). Phase 2 runs in the platform: the identity module's `enroll` block registers the first user through one keeper call, and the api binds the admin at the first successful sign in that carries the setup session. The answers land in `.env` at install, so the platform has no setup mode.

**Implementation skills**: `golang-security` (`samber/cc-skills-golang`, `.agents/skills/golang-security/`) · `golang-error-handling` (`samber/cc-skills-golang`, `.agents/skills/golang-error-handling/`) · `golang-context` (`samber/cc-skills-golang`, `.agents/skills/golang-context/`) · `golang-observability` (`samber/cc-skills-golang`, `.agents/skills/golang-observability/`) · `golang-testing` (`samber/cc-skills-golang`, `.agents/skills/golang-testing/`) · `golang-database` (`samber/cc-skills-golang`, `.agents/skills/golang-database/`) · `openapi-spec-generation` (`wshobson/agents`, `.agents/skills/openapi-spec-generation/`) · `playwright-best-practices` (`.claude/skills/playwright-best-practices/`)

## Rationale

The reasoning, the options weighed and the smaller choices are in [rationale.md](rationale.md).

## Cross child contract

### Terms

| Term | Meaning |
|---|---|
| phase 1 | Everything before the install: the code, the session, the answers, the probes. Served by the setup container. |
| phase 2 | Everything after the platform runs, until the admin is bound: enrollment, the test sign in. Served by `api` and `web`. |
| setup container | `modular-platform-setup`, started by the seed from its own image with the command `setup`. Holds no Docker socket. |
| setup holder | The instance whose module declares the `lifetime: setup` secrets (today `api`). It binds the admin. |
| identity module | The provider of the setup holder's `oidc` binding (today `authelia`). |
| setup session | A signed cookie that proves you redeemed a code. Valid 24 hours, in phase 1 and phase 2. |
| setup done | The admin is bound and the keeper wrote `.setup-done`. |

### Lifecycle

```text
fresh ─▶ code made ─▶ code redeemed (session) ─▶ answers ─▶ names probed ─▶ install ─▶ platform up, no admin
      ─▶ enrolled ─▶ first sign in with a valid session = admin bound ─▶ setup done
recovery, while no admin is bound: delete the code, restart the keeper ─▶ new code and key ─▶ redeemed at <base>/setup
recovery in phase 1 (before Install): delete the code, restart the seed ─▶ new code and key ─▶ redeemed on the setup page
```

The api is the truth for "pending" (no `admin_binding` row); the keeper's `.setup-done` only decides whether a code may be made. When they disagree (after `api admin reset`), `GET /api/v1/setup` says so (0036).

| Transition | Triggered by | Where |
|---|---|---|
| code made | the seed on a fresh state folder; the keeper's prepare when the code is missing and `.setup-done` is not there | 0034, 0036 |
| code redeemed | the right code entered while `code-used` is absent | 0034 (phase 1), 0036 (recovery) |
| names probed | your browser opening each probe link | 0034 |
| install | Install on the confirm page | 0034 |
| enrolled | the keeper's enroll call succeeding (skipped when the identity module has no `enroll`) | 0035 |
| admin bound | the first sign in callback with a valid setup session while no admin is bound | 0036 |
| setup done | the api's done call to the keeper, right after the bind | 0036 |

### State folder (the data model)

New parts beside spec 0016's state folder. No setup file is ever written by a script on the host.

| Path | Written by | Mode, owner | Holds | Lifetime |
|---|---|---|---|---|
| `secrets/api/setup/code` | the seed before phase 1; the keeper's prepare when it is missing and `.setup-done` is absent | 0600, `MP_UID:MP_GID` | 24 characters, `format: password`, `show: once` | deleted at setup done |
| `secrets/api/setup/session-key` | always made together with a new code (it replaces the old key); the files are named after the holder's two secrets (`code`, `session-key`) | 0600, `MP_UID:MP_GID` | 32 random bytes as hex | replaced with each new code, deleted at setup done |
| `secrets/api/setup/code-used` | the seed (from phase 1's output) or the keeper (on the api's recovery call), with a rename that never replaces | 0600, `MP_UID:MP_GID` | SHA 256 hex of the redeemed code | deleted when a new code is made, and at setup done |
| `.env` lines `MP_<I>__<V>` | the seed at install, from phase 1's answers | 0600, root | the install answers | stays |
| asked placed files | the seed at install | 0600, `MP_UID:MP_GID` | external secret values phase 1 asked | stays |
| enroll writes (Authelia: `secrets/authelia/users/users.yml`) | the module's install step (empty), then the keeper's enroll call | 0600, `MP_UID:MP_GID` | the identity module's first user | replaced on each enroll until setup done |
| `.setup-done` | the keeper on the done call | 0600, root | the time, RFC 3339 | stays; no code is made again |
| `.prepare/setup/` | the seed and the setup container | 0700 | `tls/` (the setup certificate), `out/` (answers, placed values, `code-used`, `done`) | emptied at each prepare |

The `api` database changes once: `admin_binding.email` becomes nullable (migration `00003`), so an identity module whose users carry no email can bind (0036).

### Contract additions (0013, `contracts/openapi/renderer.yaml`)

- **Secret field `lifetime`**: `install` (default, as today) or `setup`. A `lifetime: setup` secret must be generated (no `from`), lands in `secrets/<i>/setup/<name>`, and every such secret of a module is mounted as one folder at `/run/secrets/setup/` (a folder mount sees a replaced or deleted file; a single file mount keeps the old one, and Docker turns a missing file source into a folder). It is made only while `.setup-done` is absent, a new code replaces the module's other `lifetime: setup` files, and the keeper deletes them all at setup done. Anything else is `invalid_secret` at catalog load.
- **Module field `enroll`** (0035): the shape of a setup step, run only on the keeper's enroll call.
- **Ask fields `pattern` and `min_length`** on step and enroll asks (0035).
- **Config value `setup: ask`** becomes the source of phase 1's form (0034); caddy's `base-path`, `tls-mode` and `acme-email` gain `setup: ask`, keeping their defaults and `optional`.
- **Capability `platform-setup` 1.0** (0035): provided by the keeper module, used by the setup holder. Contract `contracts/openapi/platform-setup.yaml`.
- **Platform flavor rules** (render errors at `modules`): no instance with `lifetime: setup` secrets is `setup_holder_missing`, more than one is `setup_holder_duplicate`; a setup holder declares exactly two `lifetime: setup` secrets, `code` (`show: once`, `format: password`) and `session-key` (`format: hex`, no `show`), else `setup_code_invalid`; it must use `oidc` and `platform-setup` (`setup_binding_missing`; the `oidc` half is built in 0034, the `platform-setup` half with the capability in 0035); a step with `asks` stays refused (0016), an `enroll` with asks is allowed.

### `setup.json` section `setup` (0016's `render.Setup`)

Rendered by the platform flavor (behind `render.WithKeeper()` until 0016 task 4). Canonical JSON, `formatVersion` stays `1` (nothing reads it yet).

| Field | Holds | Rendered from |
|---|---|---|
| `holder` | the setup holder's instance | the `lifetime: setup` rule above |
| `code`, `sessionKey`, `codeUsed` | the three paths under `secrets/<holder>/setup/` | the holder's secrets |
| `port` | the host port phase 1 publishes | the ingress's `https-port` value in the graph, else its default |
| `asks` | `[{env, instance, name, type, pattern, min, max, values, optional, default, description}]` in instance then name order | every config value with `setup: ask` the graph does not answer; a `type: secret` one stays refused (0016) |
| `placed` | 0016's list, minus `beforeSetup` | unchanged otherwise |
| `names` | `["${MP_INGRESS__DOMAIN}", "auth.${MP_INGRESS__DOMAIN}"]`: every DNS name the platform needs, as `${VAR}` references to `asks` env names; the setup container adds the host of the `origin` answer when it is not in the list | the ingress provider's `domain` field and every `host` on a binding to it (0026) |
| `phase2` | `{origin, basePath}`: the `${VAR}` references of the ingress's `origin` and `base_path` fields | the ingress provider's fields |
| `enroll` | the identity module's enroll, expanded like a step (0035), or `null` | the provider of the holder's `oidc` binding |

### The setup session cookie

- Value: `v1.<expiry>.<mac>`: `expiry` is Unix seconds, `mac` is base64url (no padding) of HMAC SHA 256 with the session key over `mp-setup|v1|<expiry>`. Valid while `now < expiry` and the MAC matches the current `session-key` file (constant time compare). Lifetime 24 hours from the code's redemption; a new code (new key) ends every session.
- Name and path follow spec 0001's cookie rule: `__Host-mp_setup` with `Path=/` at the site root, `__Secure-mp_setup` with `Path=<base>/` under a base path; always `Secure`, `HttpOnly`, `SameSite=Lax` (like the api's login cookie: the sign in callback is a redirect from the identity module's host, which `Strict` may drop), no `Domain`. Phase 1 on the host address sets `__Host-mp_setup`; the probe of the origin's host sets the phase 2 form there (0034). POSTs are guarded by their `Origin` header, not by `SameSite`: in phase 2 it must equal the configured public origin (principle 12, 0036); in phase 1, before that origin is answered, it must equal the setup page's own origin (0034, AC-18).
- Upgraded at the bind: the callback that binds the admin sets the api session cookie and deletes the setup cookie in the same response (0036).
- One Go package verifies and mints it for both services: `libs/setupsession` (pure: key bytes, clock and value in, result out; no file or env access). It is a small shared kernel helper, like `libs/contract`, not a bounded context.

### Rules for #28

The sign in seam of #28 must keep these, so enrollment never changes with it:
- `api` signs in through the identity module's `oidc` binding; whatever #28 adds (which methods, how strong) reaches `api` as claims of that sign in.
- The admin is bound to issuer plus `sub`, never to an email or a method.
- An identity module's first user comes from its `enroll` block or, without one, from its own sign in; the setup never asks for a method by name.

### What this changes in 0016

- *Setup mode* points 1 to 3 go: the platform never reconciles without answers, the ingress has no setup site, and the answers reach `.env` at install, written by the seed from phase 1's output (0034). Point 4 (the setup code) stays, with `lifetime: setup` defining "only while the setup is not done".
- *Install*, step 2 gains phase 1 before the copy (0034); `placed_missing` and `beforeSetup` go away, since phase 1 asks every missing placed file.
- AC-3's "you finish #37's setup with it" is 0033's AC-2; AC-12's "the smoke job finishes #37's setup through #37's own call" becomes the Chromium run of AC-2.
- Prepare's "never deletes or overwrites a file in `secrets/`" gains two exceptions, both limited to the setup files and enroll writes, both only while `.setup-done` is absent: a new code replaces the holder's `lifetime: setup` files, and an enroll replaces its writes. The done call deletes the setup files.
- The `deployer-docker` entry provides `platform-setup`, and its image runs a third command, `setup`.

### Observability (shared rules)

`log/slog` JSON lines. No line ever carries a code, a session value or MAC, a session key, an enroll answer, an install answer of a `secret` ask or a placed value. Each child lists its lines.

## Build plan

Tracer Bullet, one cut over (your choice): the pieces land one after the other, each unit or integration tested; the thread runs end to end for the first time in spec 0016's task 4, which switches 0016 and this spec together. The children's own build plans hold the tasks; this is their order.

1. Spike, recorded in [rationale.md](rationale.md): Authelia 4.39 starts with an empty users file (`users: {}`), reloads a file replaced by rename inside a folder mount with `watch: true`, and the new user signs in within 10 seconds; Chromium in Playwright with `--host-resolver-rules` and `ignoreHTTPSErrors` opens a probe link on `https://platform.localhost:<port>` served by a container that is not Caddy; a `__Host-` or `__Secure-` cookie set with `Path=<base>/` from a request to `/setup/probe`, on a page reached after bypassing the certificate warning, is kept and sent to `<base>/api/v1/setup` once Caddy serves the name, and is still sent on the redirect from `auth.<domain>` to `<base>/api/v1/auth/callback` (with `SameSite=Lax`, also for `platform.localhost`). If the reload fails, the fallback is 0035's restart, satisfies **AC-23**, **AC-13**
2. 0034 tasks 1 to 4 (contract `lifetime`, `libs/setupsession`, render `setup` section, the `setup` command), satisfies **AC-10** to **AC-19**
3. 0035 tasks 1 to 4 (contract `enroll`, `platform-setup` contract, the keeper's endpoints, the Authelia fixture), satisfies **AC-20** to **AC-29**
4. 0036 tasks 1 to 3 (migration, the api's setup package, the editor's setup route), satisfies **AC-30** to **AC-39**
5. Cut over, inside spec 0016's task 4 and in the same change: the real `deployer-docker`, `authelia`, `api` and `caddy` entries, one line in `deploy/managed/graph.yaml` (`api` uses `setup: deployer-docker`), the seed's phase 1, the api wiring, `task dev:managed` (on a fresh `.dev/managed-state` it prints the setup URL and you finish phase 1 and 2 in your browser once; the state folder keeps it) and `scripts/smoke-managed.sh` running AC-2 in Chromium, satisfies **AC-1**, **AC-2**, **AC-3**, **AC-4**
6. Docs: the managed README (open the setup page, read the code, compare the certificate fingerprint, the probe links, DNS first, recovery with a new code, resetting before an admin exists), satisfies **AC-1**, **AC-3**

## Consequences

**Positive**:
- Installing asks nothing and prints nothing; the setup code is the only secret you handle, and it dies with the setup.
- The platform has no setup mode: it always starts with its answers, so 0016's reconcile, Caddy and every service see one shape only.
- The process with the Docker socket never answers the network before an admin exists.
- No module is named in the setup: a replaced identity module (#23) or ingress brings its own questions and enrollment.
- A wrong DNS name is caught before the install, by the browser that will use it.

**Negative / tradeoffs**:
- The cut over of 0016 task 4 grows: four catalog entries, the seed, the api and the editor switch in one change.
- The deployer binary gains a third command and an HTML setup page, served from the image that also runs the keeper.
- Two services check codes and sessions (setup container and `api`), sharing one package and one key file.
- The probe costs one certificate warning per DNS name in phase 1.
- Fixing a wrong answer after the install means a reset (remove the containers and the state folder) until the stack editor can change install values (#40, #25).
- `admin_binding.email` becomes nullable, so code that shows the admin's email must handle none.

**Neutral**:
- The scope's done line drops the second identity module; it moves to #23.
- `deploy/managed/graph.yaml` gains one line, `api` using `setup: deployer-docker`.

## Follow-up

- [ ] The sign in seam of #28 as its own spec, next, keeping *Rules for #28*: `/architect exchangeable sign in methods`.
- [ ] Scope #37's done line: drop "and with a second identity module that has no password", and move that check to #23's done line (your answer, 2026-11-20). Also: `setup.sh` asks nothing becomes "no script runs and nothing asks on the terminal".
- [ ] Scope #37's text names "the keeper call" for the answers and "until #32 `setup.sh` prints it"; both changed (answers written by the seed at install, no platform `setup.sh`). Adjust when the scope is next reconciled.
- [ ] Scope #40 says the platform installs without a domain on a self signed certificate; with this spec the domain is asked in phase 1. #40's spec decides whether "no domain yet" stays a phase 1 choice.
- [ ] Changing an install answer after the install (domain, origin) waits for the stack editor (#40, #25); until then the README's reset is the way.
- [ ] Spec 0016 carries a pointer to *What this changes in 0016* in its *Setup mode* section (done with this spec).
