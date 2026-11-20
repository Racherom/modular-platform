# 0035. The enrollment seam

**Date**: 2026-11-20
**Parent**: [0033. First run setup on the web](index.md)

## Summary

An identity module says how its first user comes to exist with an `enroll` block: the shape of a setup step, with questions the platform's setup page asks after the platform runs. The keeper runs it on one call from the api, passes the answers on standard input only, and replaces its output, for as long as no admin is bound. Authelia's `users` step becomes this enroll block: it writes the new user into Authelia's users file, which Authelia reloads. A module with no enroll block needs nothing: its first sign in with the setup session makes the admin.

## Requirements

**User stories**:
- As a module author, I want to declare how my identity module enrolls its first user, so that the platform can install with my module and no change to the platform.
- As the first admin, I want to register with Authelia from the setup page, so that I can sign in without editing a file on the host.

**Acceptance criteria**:
- **AC-20**: The catalog loads `enroll` (`entrypoint`, `command`, `env`, `asks`, `reads`, `writes`, `restart`) and asks with `pattern` (an RE2 pattern the whole value must match) and `min_length`. It refuses with `invalid_enroll`: `enroll` on a module that provides no `oidc`; a write that names no `from: setup` secret of the module; two asks with one name; a pattern that does not compile; `min_length` below 1; `for_each` (an enroll runs once).
- **AC-21**: The platform flavor renders the enroll of the identity module (the provider of the setup holder's `oidc` binding) into `setup.json` `setup.enroll`, expanded like a 0016 step (`instance`, `image`, `entrypoint`, `command`, `env`, `asks`, `reads`, `writes`, `restart`), or `null` when that module has none. The static and managed flavors render a module with `enroll` exactly as without it.
- **AC-22**: The keeper's `POST /v1/setup/enroll` with answers for every ask runs the enroll like a 0016 step (`--rm`, `--network none`, user `MP_UID:MP_GID`, a fresh scratch `/out`, its reads read only), with the answers on standard input, one per line in ask order, and never in the container's env or command line or in a log (what the step's own process does inside its container, with no network and `--rm`, is the module's business, as for today's steps); the keeper first verifies the setup session the api forwards in `X-Setup-Session` with the same key (else 401 `no_session`); when it exits 0 and every write exists it moves each write into place, replacing the old one, and answers 204. A missing, unknown or rule breaking answer, or one holding a newline, answers 422 (`invalid_answer` at `answers.<name>`, naming the rule) and runs nothing. A non zero exit answers 502 `enroll_failed`, moves nothing, and logs the exit code without the output. After `.setup-done` it answers 409 `setup_done`; with no enroll in `setup.json`, 404 `no_enroll`.
- **AC-23**: With the new `authelia` entry, the install writes an empty users file and Authelia starts healthy with no user. After an enroll the new user signs in through Authelia's login within 10 seconds, with no container restart (or, when the spike says the reload fails, after the keeper's restart of `restart: true`); a second enroll replaces the first user, whose password then fails.
- **AC-24**: `POST /v1/setup/code-used` with `codeSha256` equal to the SHA 256 of the current code writes `code-used` with no replace and answers 204; a second call answers 409 `code_used`, another hash 409 `code_changed`, after setup done 409 `setup_done`. `POST /v1/setup/done` writes `.setup-done` when missing, deletes every `lifetime: setup` file of the holder, and answers 204 every time.
- **AC-25**: Every `platform-setup` call without the setup holder's binding token answers 401. The platform flavor refuses a `platform-setup` binding from any instance but the setup holder (`setup_binding_invalid`).
- **AC-26**: The real entries after the cut over: `authelia` has no step with `asks`, no `admin-email` value and no `admin_email` field; `api` uses `platform-setup` and no longer reads `admin_email`; `deployer-docker` provides `platform-setup`. 0016's platform refusals pass on the real catalog.

## Decision

An identity module declares an `enroll` block, run by the keeper through the new `platform-setup` capability; Authelia's enroll writes its users file.

Short rationale: Authelia adds users only through its users file or a directory (it has no sign up page and no user API), and the keeper is the only writer of the state folder, so the enroll is a step the keeper runs on demand (your choices of 2026-11-20). A separate block, not a flag on a setup step, because its rules differ: on demand, may replace its writes, only before setup done. Runners up: an enrollment HTTP capability (needs a companion service for Authelia), an LLDAP directory under Authelia (one more module and database).

## Feature design

### Contract (0013)

```yaml
enroll:
  entrypoint: [sh, -c]          # optional, as a step
  command: [...]
  env: {NAME: value}            # optional, as a step
  asks:                         # asked on the setup page, in this order
    - {name: admin-user, description: ..., pattern: "[a-z0-9._-]+"}
    - {name: admin-password, description: ..., secret: true, min_length: 12}
  reads: [...]                  # optional, as a step
  writes: [{name: users/users.yml}]
  restart: false                # true: the keeper restarts the module's containers after the moves
```

The image is the module's own, as for steps. `pattern` and `min_length` are also allowed on step asks (the static flavor's `setup.sh` checks them too, since the static flavor keeps asks).

### Capability `platform-setup` 1.0

`catalog/capabilities/platform-setup.yaml`, provided by `deployer-docker` as `setup` (`auth: binding-token`, fields `url`, `token`, `ca` like `deploy`, on the same port), used by `api` as `setup`. Contract `contracts/openapi/platform-setup.yaml`; `task generate` writes the server into `services/deployer-docker` and the client into `services/api`. Errors are the contract's `Problem` and `ProblemList`.

| Endpoint | Method | Key inputs | Key outputs | Auth | Key errors |
|---|---|---|---|---|---|
| `/v1/setup` | GET | none | `state` (`pending`, `done`), `enroll` (`asks`: `name`, `description`, `secret`, `pattern`, `minLength`) or null | bearer (holder) | 401 |
| `/v1/setup/enroll` | POST | `answers` (object, name to string, req); header `X-Setup-Session` (the setup cookie's value, req) | none (204) | bearer (holder) and a valid setup session | 401, 401 `no_session`, 404 `no_enroll`, 409 `setup_done`, 422 `invalid_answer`, 502 `enroll_failed`, 503 `busy` |
| `/v1/setup/code-used` | POST | `codeSha256` (64 hex, req) | none (204) | bearer (holder) | 401, 409 `code_used`, `code_changed`, `setup_done` |
| `/v1/setup/done` | POST | none | none (204, idempotent) | bearer (holder) | 401 |

`state` is `done` when `.setup-done` exists. The keeper serves these only after its first pipeline outcome, like the deploy target API (0016).

### The keeper's enroll run

In `services/deployer-docker/internal/keeper`, beside prepare's step runner and sharing it:
1. Take the keeper lock (0016), waiting at most 60 seconds, else 503 `busy` with `Retry-After: 5`.
2. Check `.setup-done` (409), the forwarded setup session against `session-key` with `libs/setupsession` (401 `no_session`), and the answers (422), before anything runs.
3. Run the step: the answers joined with `\n` on standard input, closed after; a 2 minute deadline (a timeout counts as a non zero exit).
4. Exit 0 and every write in the scratch folder: move each into `secrets/<identity module>/` with a plain rename (replacing), mode 0600 unless the write says otherwise, owner `MP_UID:MP_GID`. A crash between two moves leaves each file whole; the next enroll replaces them all.
5. `restart: true`: `docker compose --project-directory <state> restart <instance>` and wait until healthy (`wait-timeout`); a failure answers 502 `enroll_failed` after the moves (the next enroll or a keeper start fixes it).
6. Empty the scratch folder, unlock.

### Authelia (`catalog/modules/authelia/`)

- Secret `users` becomes `{name: users/, from: setup, mount: /config/users, description: The users file.}` (a folder, so a replaced file reaches the container).
- Step `users` loses its `asks` and writes `users/users.yml` holding `users: {}`.
- New `enroll`: asks `admin-user` (pattern `[a-z0-9._-]+`, "User name, you sign in with it"), `admin-email` (pattern `[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+`, "Your email"), `admin-password` (secret, `min_length: 12`, "Password, at least 12 characters"); the command reads the three lines, makes the argon2 digest with `authelia crypto hash generate argon2` (as today's step does, inside its own container), and writes `/out/users/users.yml` with that one user (`displayname` the user name, `email`, `password`), every value single quoted with each `'` doubled, so no answer can change the YAML's shape. The checks inside the command stay as a second guard.
- `configuration.yml.tmpl`: `authentication_backend.file.path: /config/users/users.yml`, `watch: true`.
- Config value `admin-email` and the `oidc` field `admin_email` go away.

Until the cut over, these changes live in the task 3 fixture `libs/render/testdata/platform-keeper/catalog/modules/authelia/` (0016's stand in), so milestone 4's `setup.sh` keeps asking the admin.

### Value sourcing

| Action | Value | Source |
|---|---|---|
| `GET /v1/setup` | `state` | `.setup-done` exists or not |
| `GET /v1/setup` | `enroll.asks` | `setup.json` `setup.enroll.asks` |
| enroll | which module runs | `setup.json` `setup.enroll.instance` |
| enroll | standard input | the request's `answers`, in ask order |
| enroll | session check | header `X-Setup-Session`, verified with the `session-key` file |
| enroll | where writes go | `secrets/<setup.enroll.instance>/` plus each write's name |
| enroll | owner, mode | `MP_UID`, `MP_GID` from `.env`; the write's mode or 0600 |
| enroll | deadline | fixed 2 minutes |
| `code-used` | current code hash | SHA 256 of the file at `setup.code` |
| `done` | which files to delete | `setup.json` `setup.code`, `sessionKey`, `codeUsed` |
| auth | allowed caller | the token file of the holder's `platform-setup` binding (spec 0005) |

### Key invariants

- An enroll runs only while `.setup-done` is absent, and only on the holder's call.
- Answers exist only in the request and the step's standard input; the keeper writes them to no disk, container env, command line or log.
- An enroll needs both the holder's token and a valid setup session, so a stolen api token alone cannot replace the identity module's users.
- Only enroll writes and the holder's setup files are ever replaced or deleted in `secrets/`; prepare's rule holds for every other file.
- `done` is idempotent; `code-used` is created at most once per code.

### Security model

- The `platform-setup` API is reachable only with the setup holder's binding token, over the keeper's internal TLS.
- The enroll runs with no network and as `MP_UID`; it sees only its reads and its scratch `/out`.
- The keeper never returns a step's output; a refusal names only the ask and the rule.

### Observability

| Message | Level | Fields |
|---|---|---|
| `enroll done` | info | `instance`, `duration` |
| `enroll failed` | error | `instance`, `exit` (code, or `timeout`) |
| `setup code used` | info | none |
| `setup done` | info | `deleted` (paths) |

### Critical test scenarios

- Contract: one fixture per `invalid_enroll` refusal; a step ask with `pattern` checked by the static `setup.sh` golden, verifies **AC-20**
- Render: golden `setup.enroll` for Authelia; a fixture identity module without `enroll` → `null`; the static golden unchanged by an `enroll` block, verifies **AC-21**
- Keeper, fake Docker: valid answers → stdin carries them in order, the env and command carry none, writes moved replacing; a newline in an answer → 422 and no run; exit 1 → 502, no move; after `done` → 409, verifies **AC-22**
- Authelia (integration, real image): install, Authelia healthy with `users: {}`; enroll; a sign in through Authelia's first factor API within 10 seconds; enroll again → the old password fails, verifies **AC-23**
- `code-used` twice → 204 then 409; `done` twice → 204 both, files gone, verifies **AC-24**
- A call with the `deploy` binding's token or none → 401; a graph where `renderer` uses `platform-setup` → `setup_binding_invalid`, verifies **AC-25**
- After the cut over, the platform golden renders the real catalog with no refusal, verifies **AC-26**

## Build plan

1. Contract: `enroll`, ask `pattern` and `min_length`, `restart` in `contracts/openapi/renderer.yaml`, `task generate`; `libs/contract` checks (`invalid_enroll`); the static `setup.sh` template checks the two ask rules, satisfies **AC-20**
2. Render: `setup.enroll`, the `setup_binding_invalid` rule and the `platform-setup` half of `setup_binding_missing` (0034 built the `oidc` half); goldens (0034's `setup` golden gains `enroll` here), satisfies **AC-21**, **AC-25**
3. `catalog/capabilities/platform-setup.yaml`, `contracts/openapi/platform-setup.yaml`, `task generate`; the keeper's four handlers and the enroll run in `internal/keeper`, tested with the fake Docker, satisfies **AC-22**, **AC-24**, **AC-25**
4. Authelia: the new entry and template in the task 3 fixture catalog, plus an integration test (build tag `integration`) against the real image for the reload, satisfies **AC-23**
5. Cut over (0016 task 4): the fixture `authelia` moves into `catalog/modules/` (0016's stand in goes), `deployer-docker` provides and `api` uses `platform-setup`, `admin-email` removed, satisfies **AC-26**
