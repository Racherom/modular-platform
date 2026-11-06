# 0024. Key rotation and live reload

**Parent**: [0017. External capability providers](index.md)

## Summary

When a login job has to make a new access key, it now makes it and leaves the old keys active. A consumer built on the SDK picks up new keys, secrets and config files while it runs, without a restart, and its healthcheck reports a short digest of the credentials it uses. Once that digest matches the key file (or, for an unchanged image, once the container has started after the file was written), a retire job turns every other key of that user off. Managed mode does this after each apply, restarting a consumer on an unchanged image when it has to. Static mode tells you the two commands. No step depends on when a key was made, because SeaweedFS keys carry no creation date.

## SDK: live reload (0005 extension)

- **What reloads.** Every file the SDK read at start: a binding's credential files (`<KEY>_FILE`), declared values given by `<ENV>_FILE`, the env files, and on a provider the binding token clients directory (0005 AC-7, which then refuses a removed consumer without a restart). The process env, bootstrap names, `MP_DOMAIN` and the TLS settings never reload. With a push source set (#17), files hold only bootstrap names, so only binding credentials and the clients directory reload.
- **How.** The SDK polls every 5 seconds and compares each file's SHA-256 with the content in use. It uses no inotify, because in place writes to single file bind mounts are not reported reliably on every Docker host. A change counts once the content has stayed the same over two polls in a row, so a pair of files written one after the other is read as a pair.
- **Binding credentials.** The SDK builds a new client from the new set, runs that binding's readiness check once (S3: bucket exists, PostgreSQL: ping, 0005), and on success swaps it in atomically. The old client finishes calls in flight and closes 30 seconds later. On failure the old client stays and the check runs again at each poll, since AWS can take seconds to accept a new key. The SDK logs one warning per new content, naming the slot, never a credential.
- **Values.** A change gives a new snapshot that is checked whole with the start rules and swapped or refused exactly like a pushed one (0005): `config loaded` per swap, source labels `file:<path>` and `env-file:<path>:<line>`, and the previous snapshot kept on any problem. Setup does not run again. Only getters return the new value. Anything setup built from a value at start (a pool, a URL it assembled) keeps the old one until a restart, and the module README of 0005 says so.
- **Readiness.** From the moment the SDK sees changed credential content until it swaps it in (or the content goes back), `/readyz` answers 503 and names the slot as waiting with reason `credentials` (0021's body gains `reason` per waiting slot: `binding` or `credentials`). 0021's `mp-waiting:` line stays as it is. The reason shows only in the body, and the graph badge says `waiting` either way.
- **Credential digest.** The `healthcheck` subcommand prints one more line after any `mp-waiting:` line: `mp-credentials: <slot>.<field>=<h> ...`, one pair per binding credential file in use, sorted, where `<h>` is the first 8 hex digits of SHA-256 of that file's content as the client in use read it. Fields that are not files are left out. The line is printed whether the check passes or not. It is what proves the switch (below).

## Login job: key rules for `iam` (replaces 0019's key step)

The user exists and its policy is written before these rules run (0019 steps 1 and 2), so a new key never works before its policy does.

1. **The key file names a key the user holds.** If it's `Active`, keep it. If it's `Inactive`, run `update-access-key --status Active` (a binding made again after revoke). Then delete every other `Inactive` key. No new key is made, and another `Active` key stays as it is (after a rotation, until retire).
2. **Otherwise** (the file is empty, or its key is gone on the provider): delete every `Inactive` key. If two or more `Active` keys remain, fail with "user <name> has <n> active keys (<ids>) and its key file is lost; turn the unused one off with `aws iam update-access-key --user-name <name> --access-key-id <id> --status Inactive`, then run up again". Key ids are not secret. Otherwise run `create-access-key`, write both values to `/tmp`, then copy them into `/out` in place (id first, then secret), and log `new key for <c>.<s>` with the follow up commands (static text below). This line is logged only when a key is made.

The job never deletes or deactivates an `Active` key. Only retire and revoke turn keys off. AWS's limit of two keys per user holds, because a second loss before retire stops at rule 2.

## The retire job

- Service `<i>_retire_<c>_<s>` per binding whose identity capability has a `retire` script (`iam` has one; `postgresql-roles` has none, because a role has one password that the login job sets in place). It takes the login job's shape (image, entrypoint, hardening, networks) with `profiles: [mp-retire]` (so `up` never starts it), `restart: "no"`, and no `depends_on`. Its env is `MP_USER` (the binding's login name, unprefixed), `MP_MARKER`, and the admin env and files of `i`. The binding's `output` credential files are mounted read only at `/out/<f>`. Labels: `job: retire`, `instance`. It's rendered in the static and managed flavors, not in the platform flavor.
- Script (`iam`): finds the user by the same shared name function as the login job (path with `marker_style: tag`, prefix with `prefix`, 0019). An empty `/out/access_key_id` → exit 0 and change nothing. The file names no key of the user → fail with "the key file names no key of <user>; run up first". Otherwise set every other `Active` key `Inactive` and print the count. It never deletes anything. The next login run deletes the inactive keys (rule 1).
- `identity.job.retire` becomes an optional script of an identity capability (0018). It is required when any credential is `output` and the capability makes more than one key per login.

## When a binding is retired

**Which files.** The binding's `output` files are exactly the `/out` mounts of its retire service in the rendered project. The deployer reads them from there, never by a glob, so a rewritten CA or URL file never counts.

**Confirmed on the new key** means one of:
- the consumer container started (`State.StartedAt`) after the newest write time of those files, or
- the consumer runs on the SDK (label `runtime: sdk`) and the newest entry of `State.Health.Log` has an `mp-credentials:` line whose digest for each of those fields equals the SHA-256 prefix of the file on disk.

The label `runtime: sdk` sits beside `job` and `instance` in the same label set. The render adds it in the managed flavor to every module instance whose catalog entry has `runtime: sdk` (a golden test covers it).

**Managed** (`api`, in the `holdings` step of 0020, after the list jobs, inside the apply's lock; nothing here fails the apply). It runs after `up` returned, so every login job of the apply has exited and its files are final.
1. Candidates: each login holding of the deployment whose list row has `keys` above 1, and each of its uses. They are grouped by consumer and handled one consumer after another, with at most one restart per consumer for all its slots. The whole step has a budget of 5 minutes; the bindings still waiting when it runs out get the warning below.
2. For each consumer, `api` calls the deployer's retire endpoint per slot. The deployer works through the cases:
   - Confirmed: run the retire job.
   - Not confirmed, not on the SDK: restart the consumer (`docker compose restart <c>`, once per consumer), wait until it is healthy (its `start_period` plus 60 seconds, or 60 seconds after start when it has no healthcheck), then run the retire job.
   - Not confirmed, on the SDK: sample the health log for up to 60 seconds. Confirmed in that time → retire. Otherwise answer `not_confirmed`.
3. A retire that fails or ends `not_confirmed` adds a warning to the apply's log naming the consumer and slot ("<c> has not taken its new key for <s>; the old key stays active"), and the next apply tries again.

**Static**: the login job's log line and the README (a section per binding with a login job, held as a golden file) say: "<c>.<s> has a new key. A module on the SDK takes it on its own within about 10 seconds; restart any other module with `docker compose restart <c>`. Then turn the old key off from this folder: `docker compose --profile mp-retire run --rm <i>_retire_<c>_<s>`." Until you do, the old key stays active, consistent with static mode never revoking (0020), and the list shows the user with `keys: 2`.

## Changes to 0020

- List row: new optional key `keys` (logins only): the number of `Active` keys, which the `iam` list prints. Identities without keys leave it out. `HoldingRow` carries it, and the panel shows "2 active keys" in text.
- Deployer guard: also accepts `profiles: [mp-retire]` with label `job: retire`. Live state leaves it out like the other profiled services.

## API surface

| Endpoint | Method | Key inputs | Key outputs | Auth | Key errors |
|---|---|---|---|---|---|
| deployer `/v1/projects/{project}/bindings/{consumer}/{slot}/retire` | POST | `restart: bool` (false when this consumer was already restarted in this step) | 200 `{retired: bool, restarted: bool, reason?: "not_confirmed"}` | binding token, caller `api` only | 404 `unknown_job` (no retire service for that binding), 409 `not_deployed`, 409 `busy` (a run of that service is in progress), 504 `retire_timeout` (the restart wait or the 120 second job run), 503 `docker_unavailable` |

It runs the retire job like the run endpoint of 0020 (fixed container name, `--rm --no-deps`, stdout never returned, stderr's last 4 KB logged at warn on a non zero exit). The run endpoint of 0020 still refuses `job: retire`, so a retire runs only through this check.

## Value sourcing

| Action | Value | Source |
|---|---|---|
| SDK reload | content in use, new content | SHA-256 of each file read at start, polled every 5 seconds |
| `/readyz` waiting `credentials` | the slot | the binding whose files changed and are not swapped in |
| `mp-credentials:` line | digest per field | first 8 hex of SHA-256 of the content the client in use read |
| login job rules | the key the consumer holds | `/out/access_key_id`; the user's keys from `list-access-keys` |
| retire candidates (managed) | logins with more than one active key | list row `keys` from the `holdings` step |
| binding `output` files | which files | the `/out` mounts of `<i>_retire_<c>_<s>` in the rendered project |
| confirmed | write time, container start, digests | file mtime; Docker `State.StartedAt`; the newest `State.Health.Log` entry's `mp-credentials:` line against SHA-256 of the files |
| SDK consumer | runs on the SDK | label `runtime: sdk`, rendered (managed) from the catalog entry's `runtime` |
| retire job | user, key to keep | `MP_USER`, `MP_MARKER`; `/out/access_key_id`, read only |

## Security model

- A key is turned off only after the consumer is proven on the new one, so a rotation never cuts a running module off. An old key stays usable until then, and the list shows it (`keys: 2`).
- The retire endpoint takes only the path and one flag. It runs only the labelled retire service of that binding and restarts only that binding's consumer.
- The `mp-credentials:` line holds 32 bits of a SHA-256 per file, readable only where the file itself is (Docker access on that host); it gives nothing usable about a random key. Reload logs and problem lines name slots and files, never content.

## Critical test scenarios

- SDK unit (fake clock and files): a changed key pair is swapped after two stable polls; a pair written 1 second apart is read as one pair; a failing check keeps the old client and retries; `/readyz` is 503 with `credentials` from first sight until the swap; `healthcheck` prints the digests of the old content until the swap and of the new content after; a changed `_FILE` value with a bad value is refused whole; the bootstrap names never reload; a removed clients directory entry refuses that consumer without a restart (**AC-17**).
- Login job on Moto and `seaweedfs`: an emptied key file gives a second key with the first still `Active`; with two `Active` keys and an emptied file the job fails naming both ids and the command, and deletes nothing; a file naming an `Inactive` key reactivates it and deletes the other inactive keys (**AC-4**, **AC-18**).
- e2e static on `seaweedfs`: empty the consumer's key file, `up`: the SDK consumer keeps serving, its `mp-credentials:` digest moves to the new file, and `--profile mp-retire run` then turns the old key off; the consumer still writes and reads (**AC-17**, **AC-18**).
- Integration managed: the same rotation is retired by the apply's `holdings` step; an unchanged image consumer with two slots is restarted once; an SDK consumer that never takes the key (its new key refused) ends `not_confirmed` with a warning, and the old key stays `Active` (**AC-18**).
- Golden: the README section and the `runtime: sdk` label (**AC-18**).
- Handlers (`httptest`): the retire endpoint returns 404 for a binding without a retire service, 409 `busy`, and only `api`'s token passes; the run endpoint refuses a retire service (**AC-15**, **AC-18**).

## Rationale (short)

Age can't pick the key to delete. SeaweedFS keys have no `CreateDate`, and even on AWS a crash between `create-access-key` and the file write leaves the newer key with a secret nobody has. Then "delete the older" deletes the very key the consumer uses. You chose to make the new key, deploy it, and only then turn the old ones off, with the SDK taking keys, config and secrets while it runs. The digest on the healthcheck makes "deployed" checkable from Docker alone, and it also catches a pair read half old and half new. A time margin on the health log was the first draft, but the cross check showed that a passing check after the margin can still have run on the old key. The runners up: stopping with a message whenever two keys exist (safe, but manual on every rotation), and a retire job at every `up` (automatic, but it turns off the key of a consumer that `up` did not recreate). Mounting a directory so one credentials file can be renamed into place would remove torn pairs too, but it changes the one file per field layout of 0013 and 0014, and the digest already covers it.
