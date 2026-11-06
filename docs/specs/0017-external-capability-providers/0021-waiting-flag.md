# 0021. The waiting flag on the graph

**Parent**: [0017. External capability providers](index.md)

## Summary

A module built on the SDK already checks, at start, that each binding answers and accepts its login, and stays not ready until it does (0005). Now it also says which slots it waits on. Docker keeps that line from the healthcheck, the deployer passes it on, and `api` turns each waiting slot into a badge on the edge and on the provider it is bound to, whether that provider runs in the stack or outside it. The public planner never calls an address you supply; only the consumer does, from inside the stack.

## SDK (`sdk`, spec 0005 readiness)

- `/readyz` answers 503 with `{"status":"waiting","waiting":["<slot>", ...]}` (slot names only, sorted; never an address, error text or credential) while any required binding has not answered once, and 200 `{"status":"ready"}` after. Other 503 reasons (TLS not ready) keep their own `status` and an empty `waiting`.
- The `healthcheck` subcommand prints exactly one line `mp-waiting: <slot>,<slot>` to stdout when `/readyz` says `waiting` with a non empty list, then exits 1; it prints nothing else on stdout.
- Each failed attempt keeps logging its warning line naming the slot and the capability (0005).
- Unchanged images get no check (your choice); their state shows as today.
- Default healthcheck an SDK entry gets when it declares none (and the values the built in entries use): `interval: 5s`, `timeout: 3s`, `retries: 6`, `start_period: 30s`. A binding that never answers turns the service `unhealthy` about 60 seconds after start, so `up -d --wait` exits non zero well inside the deployer's 300 second wait (0007).

## Deployer (0007)

- `GET /v1/projects/{project}/state` gains `waiting: [string]` per service: from the newest entry of Docker's `State.Health.Log`, the slots of a line matching `^mp-waiting: ([a-z0-9-]+(,[a-z0-9-]+)*)$` when the service is `starting` or `unhealthy`, else empty. Any other output is ignored, so an unchanged image's healthcheck text never becomes a flag.

## `api` and the contract

- `ModuleState` gains `waiting: [string]` (copied).
- The deployment view gains `flags: [{provider, consumer, slot, reason, service?}]`, computed by `api`'s application layer from the bindings of the graph whose containers are running (during an apply, the graph being applied; otherwise the last succeeded one): `reason: waiting` for each waiting slot (its bound provider; a binding through a provisioner flags the resource provider), and `reason: job_failed` with the service for a resource or login job whose `lastExitCode` is non zero (flags that job's provider). External providers are flagged the same way, though they have no container.
- **A failed apply**: when `up` fails, `api` asks the deployer for state once more before it starts the rollback (the deployer keeps the failed containers until `down` of the rollback runs), maps it against the failed graph, and stores the result in `operations.flags`. The `Operation` contract gains `flags`; the failed operation's view draws the failed graph with those badges. Since checks run only at start (#36 decides later re checks), this is where a broken external shows.

## Editor

- An edge whose slot waits shows the text badge `waiting`.
- A provider node with flags shows `unreachable from <consumer>` (one per consumer, `+N more` past three) or `job failed: <service>`; external nodes included. Selecting it lists every flag. Text, never color alone, and announced in the screen's live region like other state changes (0006).

## Value sourcing

| Action | Value | Source |
|---|---|---|
| `/readyz` body | waiting slots | the SDK's readiness poll per required binding (0005) |
| deployer `waiting` | slots | the newest `State.Health.Log` output line `mp-waiting:` |
| `operations.flags` | flags of a failed apply | deployer state sampled after `up` failed and before the rollback, mapped against the failed graph |
| `flags[].provider` | which provider | the applied graph's binding of `consumer.slot` |
| `flags[].reason` `job_failed` | failed job | deployer state `lastExitCode` of a `job: provision` or `job: login` service |

## Critical test scenarios

- SDK (unit): `/readyz` lists only the waiting slots and turns 200 once both answer; `healthcheck` prints the one line and exits 1 (**AC-12**).
- Deployer (unit): a health log with the line gives the slots; text from another image, a malformed line, or a healthy service give none (**AC-12**).
- Integration (managed): an external Postgres with a wrong password placed: the consumer turns `unhealthy` within about 60 seconds, the apply fails and rolls back (0006), and the failed operation's `flags` (and its graph view) show the edge `waiting` and the external `unreachable from <consumer>`; behind an external provisioner, the login job fails and the external shows `job failed: <service>` (**AC-12**, **AC-13**).
- Static: the same graph's `up -d --wait` exits non zero and `docker compose ps` shows the consumer unhealthy (**AC-13**).

## Rationale (short)

You chose that the module which connects to the provider checks it, inside the consumer and for every binding, only at start. The SDK already does that check, so the new work is only naming the slot and carrying it to the graph. Docker's health log is the one channel from a container to the deployer that needs no new port or file, and parsing one fixed line keeps other images' output out. Runners up: a probe container per external (continuous, but an idle container each and the probe, not the consumer, checks); a check job per binding (works for unchanged images too, but you preferred the consumer itself).
