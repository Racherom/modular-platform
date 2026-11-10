# 0020. Holdings, list and managed revoke

**Parent**: [0017. External capability providers](index.md)

## Summary

Every provisioner gets a list job that prints what it holds in one row shape. Static mode provisions at `up` and never revokes. Managed mode records, after each apply, which module uses which bucket, database or login (the holdings), revokes logins nobody uses any more, and revokes a stack's external logins before it is removed. The editor shows what a provider holds, live, when you select it.

## Render

- **List job** per provisioner provide (resource and identity), every flavor except platform: service `<x>_list_<q>`, `profiles: [mp-list]` (so `up` never starts it), the job image, entrypoint and hardening of 0014, `command: [<list script>]`, `restart: "no"`, the admin env and files of that provide, `MP_MARKER`, networks as its jobs (`egress` when external), no `depends_on`. Managed labels: `job: list`, `instance`, `provide`.
- **Revoke job** per identity provide, managed flavor only: service `<i>_revoke_<r>`, `profiles: [mp-revoke]`, the list job's shape (no `depends_on`; `api` runs it only while its provider is up) with the revoke script, env `MP_REVOKE` set only at run time (empty in the file). Labels `job: revoke`, `instance`, `provide`.
- The static flavor has no revoke job; the platform flavor has neither list nor revoke jobs (the keeper applies it, 0016, and records nothing). No flavor has `mp_jobs`.
- README (static): "To see what a provider holds: `docker compose --profile mp-list run --rm <service>`. Unbinding a module does not close its login; disable it on the provider if you need to."
- Deployer guard (0007): accepts `profiles` only as `[mp-list]` with label `job: list`, `[mp-revoke]` with `job: revoke`, or `[mp-retire]` with `job: retire` (0024); drops the `mp_jobs` rules (`init`, `network_mode: none`, `job: wait`). Live state leaves out services with a profile.

## Data model (migration `00002_holdings.sql`, written only by `api`)

| Table | Column | Type | Null | Notes |
|---|---|---|---|---|
| `holdings` | `id` | `uuid` | no | PK, default `uuidv7()` |
| | `stack_id` | `uuid` | no | FK `stacks(id)` ON DELETE CASCADE |
| | `provider` | `text` | no | provider instance name in the graph |
| | `provide` | `text` | no | provide name on it |
| | `kind` | `text` | no | `CHECK (kind IN ('resource','login'))` |
| | `type` | `text` | no | `s3-bucket`, `iam-user`, `postgresql-database`, `postgresql-role` |
| | `name` | `text` | no | |
| | `created_by_us` | `boolean` | no | the list saw this stack's marker on it |
| | `adopted` | `boolean` | no | default false; the binding said `adopt: true` |
| | `state` | `text` | no | `CHECK (state IN ('pending','active','revoke_pending','revoked'))` |
| | `first_seen_at`, `last_seen_at` | `timestamptz` | no | |
| | `revoked_at` | `timestamptz` | yes | |
| | | | | UNIQUE `(stack_id, provider, provide, kind, name)`; index `(stack_id, state)` |
| `holding_uses` | `holding_id` | `uuid` | no | FK `holdings(id)` ON DELETE CASCADE |
| | `consumer`, `slot` | `text` | no | |
| | `since` | `timestamptz` | no | default `now()` |
| | | | | PK `(holding_id, consumer, slot)` |
| `stacks` (existing) | `marker` | `text` | no | unique; set at insert to `mp_provisioned_` + first 8 hex of SHA-256 of `id`, never updated; the migration fills existing rows with 0014's graph name marker |
| `operations` (existing) | `flags` | `jsonb` | yes | the flags sampled before a rollback (0021); set only on a failed apply |

**State**: `pending` (written before `up`; the apply may or may not have made it) → `active` (seen by the list after an apply) → `revoke_pending` (unused, revoke failed or its provider left the graph) → `revoked` (revoke succeeded) → `active` again only after the next apply that names it (its login job reactivates it, the list then sees it). Resources stay `active`; only logins are revoked. A `pending` login with no use is revoked like an `active` one.

## Around apply (`api`, inside the apply's busy lock; nothing here fails the apply)

**Before `up`** (new step `record`, after `prepare`):
1. From the resolved graph's bindings, the expected set: per binding `c.s` through `p.q` with identity `i.r`, the resource `(p, q, resource, <named resource value>)` with `adopted` from the binding, and the login `(i, r, login, <login name>)`, each used by `(c, s)`.
2. Insert every expected holding that is missing with state `pending`; replace the stack's `holding_uses` with the expected uses. So a login made by an apply that then fails or rolls back is still known.
3. Logins whose identity provider the new graph drops while it is still running (an internal module about to go): run its revoke job now, with the old project files, for every login holding of that provide; then mark them as in step 6 below.

**After `up`** (new step `holdings`, before `done`), after every apply that reached `up`, succeeded or not, and after a rollback apply against its own graph (whose `record` step ran first):
4. For every provisioner provide of the graph now running, run its list job through the deployer (in parallel, 60 seconds each). A failed or timed out list logs one warning (provider, provide, problem code) and leaves that provide's rows as they are.
5. Every holding found in a list row: `created_by_us` from the row, `last_seen_at = now()`, `pending` → `active`. A `pending` holding not found stays `pending` (the job never got to make it).
6. Every login holding of the stack with no use and state `pending`, `active` or `revoke_pending`: grouped by `(provider, provide)`, when that identity provide is in the stack run its revoke job with `MP_REVOKE` = the names, sorted, joined by `,`, in batches of at most 64 KB. Exit 0 → `revoked`, `revoked_at = now()`; otherwise `revoke_pending` and a warning log line. When the provide left the graph and step 3 could not reach it (an external removed from the graph): `revoke_pending`, and the stack screen says "provider <p> is no longer in the graph; disable its logins there".
7. List rows carrying this stack's marker that are no holding at all (made outside the record, for example before this feature): shown in the panel as `this_stack` with `state: null`; never revoked automatically.
8. Every login holding whose list row has `keys` above 1: retire through the deployer once the consumer is confirmed on its new key ([0024](0024-key-rotation-reload.md)); a failure or `not_confirmed` is a warning, never a failed apply.

**Remove** (0006): before `down`, for each identity provide in the last applied graph that is external, run its revoke job with every `active` login holding of the stack; success marks them `revoked`, a failure adds a warning naming the provider to the remove operation's log and the screen. Holdings go when the stack row goes (cascade). The remove dialog states: "On external providers only keys and logins are disabled; buckets, databases, users and roles stay. Without *delete data*, logins inside kept volumes stay usable."

## API surface

| Endpoint | Method | Key inputs | Key outputs | Auth | Key errors |
|---|---|---|---|---|---|
| deployer `/v1/stacks/{stack}/jobs/{service}/run` | POST | `{env?: {MP_REVOKE: string}}` | 200 `{exitCode, output, truncated}`; `output` is stdout of a list job (at most 4 MB), empty for revoke | binding token, caller `api` only | 404 `unknown_job` (no such service, or label not `list`/`revoke`), 422 `invalid_env` (another key, `MP_REVOKE` on a list job, a `,` separated name breaking `^[A-Za-z0-9_+=.@-]+$`, over 64 KB, which `api` avoids by batching), 409 `not_deployed`, 409 `busy` (a run of that service is in progress), 504 `list_timeout` (60 s list, 120 s revoke), 503 `docker_unavailable` |
| `/api/v1/stacks/{name}/providers/{instance}/holdings` | GET | `name`, `instance` | `Holdings {instance, refreshedAt, provides: [{provide, capability, error?: Problem, incomplete, skipped, rows: [HoldingRow]}]}` | signed in admin | 404 unknown stack or instance (or not a provisioner), 409 `not_deployed`; per provide `provider_unreachable` or `list_timeout` inside `error`, so the rest still shows |

The deployer runs `docker compose -p <project> --profile <mp-list|mp-revoke> run --rm --no-deps --name <project>-run-<service> [-e MP_REVOKE=<names>] <service>` with the project's files, returns stdout only for list jobs, never returns stderr, and logs its last 4 KB at warn on a non zero exit (scripts never print a secret, 0014). The fixed name allows one run per service at a time (a second call gets 409 `busy`); on a timeout, and for every `<project>-run-*` container at deployer start, it runs `docker rm -f`. Run containers are left out of live state and are not orphans for `--remove-orphans` (Compose labels them as one off). Runs do not take the project's apply lock; `api` runs revoke only inside an apply or remove. Concurrent list calls for one instance share one run (single flight in `api`).

`api` parses list output line by line: a line that is not one JSON object with the keys of the list row (0017) is skipped and counted; a `truncated` answer drops the last line. Either sets that provide's `incomplete: true` with the count, and the panel says "list incomplete". Rows keep the job's order (by `kind`, then `name`).

`HoldingRow`: `kind`, `type`, `name`, `madeBy` (`this_stack` when `stack` equals the stack's marker, `other_stack` when it is another non empty value, `not_platform` when empty), `adopted`, `active` (logins; `null` when the provider would not say), `keys` (logins, when the list prints it, 0024) and `grants` (logins), `usedBy: [{consumer, slot}]`, `state` (null when the row is not a holding of this stack), `tagMissing` (a holding with `created_by_us` whose list row shows no marker). The merge joins list rows and holdings on `(provide, kind, name)`, preferring the row whose `stack` is this stack's marker when two rows share a name (`marker_style: prefix`, 0019); a holding with no list row is shown with `madeBy: null` and its `state`, so a resource that vanished from the provider is visible.

## Editor (managed stack screen)

Selecting a node with a provisioner provide opens **What it holds**: one section per provide, a table with name, type, made by, adopted, used by, and (logins) state; a loading state while the list runs, an empty state ("holds nothing yet"), an inline error per provide (unreachable, timed out) with the time of the last good refresh from `last_seen_at`, and a refresh button. A banner on the stack screen names every `revoke_pending` login and why. Text labels, never color alone. The slot picker that adopts from this list is #6's.

## Value sourcing

| Action | Value | Source |
|---|---|---|
| list job | admin fields, marker | the provide's fields (0012, 0018 `same_as`), `MP_MARKER` (0017) |
| list row `stack` | marker on the resource or login | bucket tag, IAM path, Postgres marker role membership (0018, 0019) |
| `holdings.created_by_us` | this stack made it | list row `stack` equals the stack's marker |
| `holdings.adopted` | adopted | the binding's `adopt` in the applied graph |
| `holding_uses` | which module uses which | the resolved graph's bindings of the applied graph |
| `MP_REVOKE` | logins to close | `holdings` of the stack with no use and state `active` or `revoke_pending` |
| managed marker | `mp_provisioned_` + 8 hex | `stacks.marker`, set once at insert from the row's uuid, passed in `ManagedRenderRequest.stack.marker` (new field) |
| `operations.flags` | flags of a failed apply | the deployer state sampled before rollback, mapped by `api` (0021) |
| `HoldingRow.madeBy` | made by | list row `stack` against the stack's marker |
| panel "last refreshed" | time | `holdings.last_seen_at` |

## Security model

- Only a signed in admin calls the holdings endpoint (the one role today; #14 may narrow it). The panel shows names of every bucket, user and role the admin login can see on that provider; that is the admin's own account.
- The run endpoint is a fixed allow list (labelled list and revoke services of one project, one env key), so a caller cannot run another service or inject a command.
- No list row, problem, log line or response carries a secret; list scripts print names and flags only.
- Revoke never deletes data or logins, only disables them, so a wrong revoke is fixed by binding again.

## Critical test scenarios

- Golden: list jobs with `profiles: [mp-list]` in static and managed renders; revoke jobs only in managed with `profiles: [mp-revoke]`; no `mp_jobs`; the guard accepts both and refuses a profile on any other service (**AC-8**, **AC-15**).
- e2e (static, `seaweedfs`): unbind a consumer, `up`: its key still works; `--profile mp-list run` shows its user `active` (**AC-8**, **AC-5**).
- Integration (managed, `seaweedfs` and `postgres`): apply two consumers → two resource and two login holdings with one use each; apply without one → its login `revoked`, its key refused, its bucket and object still there; bind it again → `active`, the key works (**AC-9**).
- Revoke failure: break the admin secret, unbind, apply → apply `succeeded`, holding `revoke_pending`, banner shown; fix it, apply → `revoked` (**AC-9**).
- Failed apply: a graph whose second consumer never gets healthy: its login exists on `seaweedfs`, the holding is `pending`, the rollback apply's `holdings` step revokes it (**AC-9**).
- Dropped provider: remove an internal `seaweedfs` with bound logins from the graph: step 3 revokes them before `up` (**AC-9**).
- Run endpoint: a list run killed at its timeout leaves no `<project>-run-*` container; a deployer restart removes a leftover one; a second concurrent run of one service gets 409 (**AC-15**).
- `api` parse: a broken line and a truncated output mark the provide `incomplete` and keep the good rows (**AC-11**).
- Remove with an external (Moto): keys become inactive before `down`; with Moto stopped, remove succeeds with a warning naming it (**AC-10**).
- Handlers (`httptest`): holdings endpoint 401 without a session, 404, 409, a partial result with one provide timed out; run endpoint 404 `unknown_job` for a provision job and for `mp_jobs`, 422 for `MP_REVOKE` on a list job and for a second env key (**AC-11**, **AC-15**).
- Editor (vitest): loading, empty, error and refresh states of the panel; badges as text (**AC-11**).

## Rationale (short)

You chose that static mode never revokes and that the database records which module uses which bucket, with the tag only saying whether the platform made it. So revoke moved out of `up` into a step `api` runs after apply, which needs no wait anchor and decides by the record instead of a scan of every marked login. Running list after apply (your choice) reuses one job for the panel, the record and revoke. Revoke failures warn instead of failing the apply (your choice), since a rollback cannot bring an unbound need back. Runners up: keep the revoke job at `up` in managed only (smaller change, but decides by tag, not by record); jobs writing a report file (exact, but a second output format for every script).
