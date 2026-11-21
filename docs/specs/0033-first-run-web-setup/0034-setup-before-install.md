# 0034. Phase 1: the setup before the install

**Date**: 2026-11-20
**Parent**: [0033. First run setup on the web](index.md)

## Summary

On a fresh state folder, the seed makes the setup code and starts a throwaway setup container from its own image. That container has no Docker socket and serves a small setup page on a self signed certificate. You enter the code, get a session, enter the install answers, and then open each DNS name the platform needs in your browser to prove that it reaches this host. Then you press Install; the seed writes the answers into `.env` and starts the platform with its domain.

## Requirements

**User stories**:
- As a self hoster, I want to open a setup page right after the `docker run`, so that I answer the install questions in a browser.
- As a self hoster, I want Install to wait until my browser reached every DNS name, so that a wrong name never leaves a platform I cannot reach.

**Acceptance criteria**:
- **AC-10**: On a fresh state folder, after the mounts check and the lock (0016), the seed makes `code` and `session-key` (atomic, as prepare) when missing, logs `show` (the code's path and description) and `setup open` (the port, the URL pattern `https://<this host>:<port>/` with the port filled in, and the SHA 256 fingerprint of the setup certificate; a container cannot reliably list the host's addresses, so the README says to use the host's address or a name that points to it), and creates exactly one container `modular-platform-setup` from its own image ID with the command `setup`: user `65532:65532`, root filesystem read only, every capability dropped, `no-new-privileges`, no Docker socket, no restart policy, publishing `setup.json`'s `port` to the container's `8443` on every host address. No platform service runs, and nothing asks on the terminal.
- **AC-11**: The setup page accepts the content of the code file once. The right code writes `out/code-used` (with no replace), sets `__Host-mp_setup` (24 hours) and shows the answers form; a wrong code shows "This code is not accepted" with no hint; the same code again, in any browser, is refused the same way. More than 10 tries a minute from one remote address answer 429 until the minute passes; there is no total limit (a 142 bit code needs none, and a total limit would let anyone on the network lock you out).
- **AC-12**: The answers form lists every entry of `setup.json` `asks` (today `ingress`'s `domain`, `origin`, `base-path`, `tls-mode` and `acme-email`), prefilled with its default and showing its description, plus one text field (up to 64 KiB) for each missing `placed` file. On submit each value is checked with the catalog's rules (`contract.ConfigValue.Check`); a refusal names the field and the rule and keeps the other answers.
- **AC-13**: The confirm page lists the answers (placed values masked), the port, and every name of `setup.json` `names` with the answers filled in, each with a probe link `https://<name>:<port>/setup/probe?n=<nonce>`. Install stays disabled until each name's probe passed. A probe passes only when it lands on this setup container with that name's unused nonce and a `Host` equal to that name (compared without the port, ignoring case); a used or unknown nonce, one past its session's expiry, or another `Host` passes nothing. Opening the probe of the origin's host (the host of the `origin` answer, the domain by default) also sets the setup cookie in its phase 2 form (name and path by the base path answer) on that host, with the value and expiry of the session the nonce belongs to.
- **AC-14**: Install writes `out/answers.env`, the placed values and `out/done`, shows the phase 2 link (`<origin><base path>/setup`) and a note to wait for the platform, and the container exits 0. The seed checks the answers again against `asks`, writes them into `.env` (0016's quoting rules), the placed values into their files and `code-used` into its path, removes the setup container, and goes on with the install and the pipeline (0016), so the platform comes up with the domain.
- **AC-15**: No code, session value, answer of a placed file or other secret appears in a log line of the seed or the setup container, in any container's env, or in the setup container's command line; install answers reach only `.env`.
- **AC-16**: `docker start` of a stopped seed during phase 1, or a killed setup container, gives exactly one setup container again with the same code and key; a session made before stays valid, and a code redeemed before stays refused (its `code-used` reaches the state folder before the new container starts). Typed answers and passed probes are asked again. A seed stopped after Install but before `docker-compose.yml` was moved in goes on from phase 1's kept output without a new setup container. Recovery in phase 1 (session lost before Install): delete the code file and `docker restart modular-platform-seed`; a new code and key are made, every older session ends. When `port` is in use, the seed logs `install refused` (`port_in_use`, naming the port) and tries again on the backoff.
- **AC-17**: The render (platform flavor, `WithKeeper()`) writes `setup.json`'s `setup` section as the umbrella's *Cross child contract* defines it, and refuses each setup holder rule there (`setup_holder_missing`, `setup_holder_duplicate`, `setup_code_invalid`, and `setup_binding_missing` for a holder with no `oidc` use; the `platform-setup` half of that rule comes with 0035); the catalog refuses a `lifetime: setup` secret that has any `from` (`invalid_secret`).
- **AC-18**: Every POST of the setup page with an `Origin` other than its own (a missing `Origin` included) is refused 403; every page carries `Content-Security-Policy: default-src 'self'`, `X-Frame-Options: DENY`, `Referrer-Policy: no-referrer` and `Cache-Control: no-store`.

## Decision

The seed runs phase 1 in a throwaway container of its own image (`deployer-docker setup`), with server rendered Go pages, before it copies anything; the answers land in `.env` at install.

Short rationale: no setup mode for the platform and no Docker socket on a public port (your choices of 2026-11-20). Runner up: a setup module in a Compose profile with a Caddy catch all site (0016 as first written), more moving parts and a platform that must reconcile without answers.

## Feature design

### Seed: install with phase 1 (amends 0016 *Install*, step 2)

On a fresh state folder (no `docker-compose.yml`), inside the lock:
1. Read `/install/setup.json` (0016: `install_missing` when absent or of an unknown `formatVersion`).
2. Make the holder's `lifetime: setup` files when `code` is missing: remove any old `session-key` and `code-used`, then move in a new `session-key` (`format: hex`), then the new `code` (`format: password`) last, each with no replace. Because the code comes last, a crash in between leaves `code` missing and the next start repeats this step. This is also the phase 1 recovery: delete the code, restart the seed. The state folder is made 0700 first (0016).
3. Make the setup certificate when `.prepare/setup/tls/` holds none: ECDSA P 256, self signed, subject `CN=modular-platform setup`, valid 30 days, no SAN; key 0600 and certificate 0644, owner `65532`. Its fingerprint is logged in `setup open`.
4. When `.prepare/setup/out/done` exists, skip to step 6 (an install that stopped after Install). Otherwise remove a container named `modular-platform-setup` if one exists; move an existing `out/code-used` to `codeUsed` with no replace (dropping it when its hash is not the current code's); empty `out/`; create and start the container (AC-10). Mounts, each a bind at the same path inside as on the host:
   - `secrets/<holder>/setup/` read only (code, key, an existing `code-used`);
   - `.prepare/setup/tls/` read only;
   - `.prepare/setup/out/` read write (made empty, 0700, owner `65532`);
   - the host source of `/install/setup.json`, read only, found by inspecting the seed's own `/install` mount.
   Command: `setup --state <dir>`. Labels: 0016's `stack: platform`, plus `com.modular-platform.role: setup`.
5. Wait for it to exit. Exit 0 with `out/done` present: go on. Any other end: log `setup failed` (exit code), remove it, wait the backoff, go to 4.
6. Read `out/` as untrusted (an unprivileged container wrote it): open each file with `O_NOFOLLOW`, accept only regular files of at most 64 KiB, and accept under `out/placed/` only the paths `setup.json` `placed` names; anything else is `setup failed`. Check `out/answers.env` against `asks` (every name known, every value passing `Check`, every non optional one set); a failure is `setup failed` as in 5. Then 0016's copy and `.env`, with the answers appended; the placed values from `out/placed/` into their paths (0600, `MP_UID:MP_GID`); `out/code-used` moved to `codeUsed` with no replace when not there yet. `docker-compose.yml` still moves in last.
7. Only after `docker-compose.yml` is in place: remove the setup container and empty `.prepare/setup/`. Then 0016's pipeline: check, prepare, reconcile, self check, handover.

An installed state folder (0016 step 3) never runs phase 1.

### The setup container (`deployer-docker setup --state <dir>`)

A Go `net/http` server in `services/deployer-docker/internal/setup`, TLS on `:8443` with the mounted certificate. It reads `setup.json` and the three setup files at start and keeps the rest in memory. No Docker client is built in this command.

**Pages** (html/template plus one CSS file, embedded with `embed`; no JavaScript; text in English):

| Path | Method | Session | Does | Errors |
|---|---|---|---|---|
| `/` | GET | none | the code form; with a valid session, redirect to `/answers` | none |
| `/code` | POST | none | form `code`; rate limited (10 a minute per remote address); constant time compare of SHA 256 with the file; checks `out/code-used` and the mounted `code-used` absent; writes `out/code-used` with `O_EXCL`; sets `__Host-mp_setup`; redirect to `/answers` | 401 page `code_refused`; 429 `too_many_tries`; 403 cross origin |
| `/answers` | GET | required | the form from `asks` and missing `placed` | redirect to `/` without a session |
| `/answers` | POST | required | checks each value; keeps them in memory; redirect to `/confirm` | 422 page naming field and rule |
| `/confirm` | GET | required | answers, port, names with probe links and their state; Install enabled when every probe passed | redirect to `/answers` without answers |
| `/setup/probe` | GET | none | query `n`; on a match (AC-13) marks the name passed and, for the origin's host, sets the phase 2 cookie; a page "This name reaches the setup. Go back to the setup tab." | 404 page for no match |
| `/install` | POST | required | every probe passed: writes `out/answers.env` (one `MP_<I>__<V>=value` per line), `out/placed/<path>`, then `out/done`; the final page; then a graceful shutdown and exit 0 | 409 page when a probe is missing |
| `/healthz` | GET | none | 200 | none |

- One session at a time is not enforced: anyone with a valid cookie may use the pages, and only one person can redeem the code.
- Nonces: 128 random bits per name, made when `/confirm` is first shown for a session. In memory each nonce maps to `{name, session expiry}`; it is single use and valid until that expiry. Changing an answer that changes a name makes new nonces for it.
- The phase 2 cookie: minted by `libs/setupsession` with the expiry the nonce maps to (so it is the same session); name and path from the base path answer by the umbrella's cookie rule; `Max-Age` the rest of that session.
- Rate limits: `golang.org/x/time/rate`, 10 a minute (burst 10) per remote address, on `/code` only. The remote address is the connection's source; Docker's published port keeps the client's address for traffic from other hosts.

### Interface surface

No new HTTP contract: the pages above are HTML for a person, not an API, and stay out of `contracts/openapi/`. The command line gains one row beside 0016's:

| Command | Started by | Inputs | Does | Ends |
|---|---|---|---|---|
| `deployer-docker setup --state <dir>` | the seed (never you) | the mounts of step 4 | serves phase 1 | exit 0 after Install; exit 1 on a bad mount or `setup.json` |

### Value sourcing

| Action | Value | Source |
|---|---|---|
| seed | the setup files' paths | `setup.json` `setup.code`, `sessionKey`, `codeUsed` |
| seed | the setup image | the seed's own container, inspected (image ID) |
| seed | host port | `setup.json` `setup.port` |
| seed | host source of `setup.json` | the seed's own `/install` mount, inspected |
| seed `setup open` | URL | `setup.port` in the fixed pattern `https://<this host>:<port>/` |
| seed `setup open` | fingerprint | SHA 256 of the certificate made in step 3 |
| code check | the code | the mounted `code` file |
| code check | already used | `out/code-used` and the mounted `code-used` |
| session | key | the mounted `session-key` file |
| session | expiry | the clock at redemption plus 24 hours (constant in `libs/setupsession`) |
| answers form | fields, defaults, rules | `setup.json` `asks` and `placed` |
| confirm | names | `setup.json` `names`, `${VAR}` replaced by the answers |
| confirm | port in the probe links | `setup.json` `setup.port` |
| probe | which name it is for | the nonce, mapped in memory |
| phase 2 cookie | name and path | the `base-path` answer (`phase2.basePath`) |
| phase 2 cookie | host it is set on | the host of the `origin` answer (`phase2.origin`) |
| phase 2 cookie | value, expiry | the session the nonce maps to |
| nonce | lifetime | its session's expiry |
| seed, setup container | user | `65532`, which is 0016's fixed `MP_UID` and `MP_GID`, so the setup files are readable |
| final page | phase 2 link | `phase2.origin` and `phase2.basePath` with the answers, plus `/setup` |
| seed install | `.env` lines | `out/answers.env`, checked again against `asks` |

### Key invariants

- The setup container never holds the Docker socket or any secret beyond the code, the session key and its own TLS key.
- The seed copies nothing into the state folder before Install (only the setup files and `.prepare/setup/`).
- A code is redeemed at most once: `code-used` is created with no replace, and both places are checked first.
- Install happens only after every name's probe passed.

### Security model

- Phase 1 is reachable on every host address while it runs, guarded by the code (142 bits), the rate limits and a 24 hour session; the README says to run the setup soon after the `docker run`.
- The certificate is self signed; the README tells you to compare the fingerprint from `docker logs modular-platform-seed` with the one your browser shows.
- The cookie is `HttpOnly`, `Secure`, `SameSite=Lax` (the sign in callback is a redirect from the identity module's host, which `Strict` may drop); POSTs need a same origin request (AC-18).
- The seed treats `out/` as untrusted input (step 6).
- Answers for placed files are secrets: masked on the confirm page, written only into their files.
- Known limits, both in the README: the probe proves that your browser reaches each name, not that an ACME server can, so with `tls-mode: acme` the names must be public and port 80 open; and a browser that keeps HSTS (a "HTTPS only" record) for the domain from an earlier ACME install cannot click through the self signed warning, so the probe of that name needs another browser profile. With `tls-mode: internal`, Caddy's certificate brings a second warning in phase 2 (on the platform host and on `auth.{domain}`).

### Configuration required

None new. The setup command takes only `--state`; every value comes from `setup.json`. Caddy's entry marks `base-path`, `tls-mode` and `acme-email` `setup: ask` (defaults and `optional` unchanged); Authelia's `admin-email` goes (0035).

### Observability

| Message | Level | Fields |
|---|---|---|
| `setup open` | info | `url`, `port`, `fingerprint` |
| `setup code accepted` | info | `remote` (address) |
| `setup code refused` | info | `remote`, `reason` (`wrong`, `used`, `rate`) |
| `setup probe passed` | info | `name` |
| `setup installed` | info | `answers` (env names only) |
| `setup failed` | error | `exit`, `error` |
| `install refused` | error | 0016's codes plus `port_in_use` |

### Critical test scenarios

- Happy path (e2e, Chromium): `docker run` → `setup open` logged → read code → answers → open both probe links → Install → platform healthy with the domain; the phase 2 cookie exists on the domain, verifies **AC-10**, **AC-12**, **AC-13**, **AC-14**
- Code once: right code, then the same code in a second browser context → refused; 11 wrong tries in a minute from one address → 429 (fake clock in the unit test), while another address still gets through, verifies **AC-11**
- Crash after redemption: redeem, `docker kill` the setup container → the new container refuses the same code; stop the seed between `.env` and `docker-compose.yml` → `docker start` goes on with no new setup container, verifies **AC-16**
- Untrusted output: a symlink or an unnamed path in `out/placed/` → `setup failed`, nothing copied, verifies **AC-14**
- Probe: a nonce of another name, a used nonce, a wrong `Host` → nothing passes; Install POST before all pass → 409, verifies **AC-13**
- Restart: `docker kill` the setup container mid form → one new setup container, the old cookie still valid; `docker stop` the seed then `docker start` → the same, verifies **AC-16**
- Port in use: a listener on the port before the `docker run` → `port_in_use`, retry after the listener closes, verifies **AC-16**
- Leaks: grep every container's logs and `docker inspect` env for the code, the cookie value and a placed value → none, verifies **AC-15**
- Render: golden `setup` section in `testdata/platform-keeper/`; each refusal fixture, verifies **AC-17**
- Cross origin: POST `/code` with `Origin: https://evil.test` → 403; headers present on every page, verifies **AC-18**

## Build plan

1. Contract: `lifetime` on secrets in `contracts/openapi/renderer.yaml`, `task generate`, `libs/contract` checks it; caddy's three values gain `setup: ask` (in the task 3 fixture catalog only until the cut over), satisfies **AC-17**
2. `libs/setupsession`: mint and verify, constant time, a clock argument; table tests for expiry, wrong key, malformed values, satisfies **AC-11**
3. Render: the `setup` section of `render.Setup`, the holder rules, `names` and `phase2` from the ingress fields, golden in `testdata/platform-keeper/`, satisfies **AC-17**
4. `services/deployer-docker/internal/setup`: the server, pages, rate limits, probes, outputs; `httptest` tests for every row of the pages table, satisfies **AC-11**, **AC-12**, **AC-13**, **AC-18**
5. Cut over (0016 task 4): the seed's phase 1 orchestration (steps 2 to 7), the `setup` command wired in `main`, `setup open` and `port_in_use`; e2e for the restart, port and leak scenarios, satisfies **AC-10**, **AC-14**, **AC-15**, **AC-16**
