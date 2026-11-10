# 0017. External capability providers

**Date**: 2026-11-05
**Status**: In Progress
**Scope**: [#10 External capability providers](../../scope/scope.md)
**Builds on**: [0011](../0011-capability-module-contract/index.md) (the v1 contract and its external instances), [0005](../0005-module-sdk/index.md) (readiness), [0006](../0006-managed-mode-control-plane/index.md) and [0007](../0006-managed-mode-control-plane/0007-deploy-target-deployer.md) (apply, live state, the deployer)
**Supersedes in part**: [0014](../0011-capability-module-contract/0014-provisioners-jobs.md): `yields.login`, the revoke job at `up`, the wait anchor `mp_jobs`, and the `postgresql-provisioner` scripts

## Summary

A service outside the stack (an AWS account, a database server you already run) can now be a provider that consumers bind to exactly like a module in the stack. Making a resource and making a login become two jobs: the resource provider makes the bucket or database, and an identity provider (bound through an `identity` slot) makes one login per consumer and grants it that resource. The same S3 bucket and IAM scripts run on the bundled SeaweedFS module and on a real AWS account. Managed mode records which module uses which resource, revokes logins nobody uses any more, and shows what each provider holds; a consumer built on the SDK names the binding it waits on, so an unreachable provider is flagged on the graph. Each consumer declares the actions it needs, and a binding grants only what its provider offers. A lost key is replaced without cutting the consumer off: the SDK takes new keys while it runs, and the old key is turned off only once the consumer is on the new one.

## Structure

- [0018. Identity providers and grants](0018-identity-providers-grants.md): the `identity` section of a capability, `{identity: f}` field sources, the `identity` slot (which may bind its own instance), the login job, adoption, sharing, external `uses` and `same_as`, and the Postgres split; supports AC-2, AC-3, AC-6, AC-7, AC-14.
- [0019. The S3 family on SeaweedFS and AWS](0019-s3-seaweedfs-aws.md): the `s3`, `s3-provisioner` and `iam` capabilities, their scripts, the `seaweedfs` module, the AWS external, and Moto in tests; supports AC-1, AC-4, AC-5.
- [0020. Holdings, list and managed revoke](0020-holdings-list-revoke.md): the list jobs, static mode without revoke, the `holdings` tables, the step after apply, revoke on remove, the deployer run endpoint, the list API and the editor panel; supports AC-5, AC-8, AC-9, AC-10, AC-11, AC-15.
- [0021. The waiting flag on the graph](0021-waiting-flag.md): `/readyz` names the waiting slots, the deployer reads them from the health log, `api` maps them to providers, and the editor badges the edge and the provider; supports AC-12, AC-13.
- [0023. Access actions](0023-access-actions.md): the `access` vocabulary on a capability, `offers` on identity capabilities and provides, required, optional and `one_of` actions on a consumer's `uses`, `drop` on a binding, and the grant set in the resolved graph; supports AC-3, AC-16.
- [0024. Key rotation and live reload](0024-key-rotation-reload.md): SDK reload of keys, secrets and config files, the date free key rules, the retire job, and the confirmation that a consumer is on its new key; supports AC-4, AC-17, AC-18.

## Requirements

**User stories**:
- As a self hoster, I want to bind a module to my AWS account or to a server I already run, so that my stack uses it without the module knowing.
- As a self hoster, I want the bundled S3 module and an AWS account to make buckets and logins the same way, so that switching between them is an ordinary exchange.
- As an admin, I want to see what a provider holds and which module uses which bucket or database, so that I can adopt an existing one to move a running service into the network.
- As an admin, I want logins that no module uses any more to be closed, with their data kept, so that a removed module leaves no open door.
- As an admin, I want an unreachable provider flagged on the graph, so that I see which binding is broken without reading logs.

**Acceptance criteria**:
- **AC-1**: A consumer entry with a required `s3` slot binds, unchanged, either through the bundled `seaweedfs` module or through an external `aws` instance (an `s3-provisioner` provide whose `identity` slot binds an `iam` provide). Both renders give the consumer the same env names, `access_key_id` and `secret_access_key` only as file paths; after `setup.sh` and `up -d --wait` the consumer writes and reads an object in its bucket (AWS through Moto in tests).
- **AC-2**: For every binding through a resource provisioner, the render has a resource job `<p>_provision_<c>_<s>` and then a login job `<i>_login_<c>_<s>` on the bound identity provider; the consumer starts only after both exit 0. A consumer's login reaches its own bucket or database and is refused by every other one. A second `up` adds or removes no bucket, database, user or role, and every consumer still signs in with the credentials it has. This holds for S3 on SeaweedFS and AWS, and for Postgres after the split (`postgresql-provisioner` makes databases, `postgresql-roles` makes logins).
- **AC-3**: The catalog and the renderer carry the new contract: a capability's `identity` section, `{identity: f}` sources in `yields.fields`, `provides.<q>.identity` naming a slot, an identity slot that may bind its own instance (and the checks recommend it when that instance has a fitting provide), external `uses` for identity slots only, `same_as` fields and `adopt` on a binding. Each rule has its problem code (0018) and a check test.
- **AC-4**: On AWS (Moto in tests) the `iam` login job makes each user at path `/modular-platform/<marker>/` with one inline policy granting only its bucket and only its grant set, keeps the access key it wrote while that key is active, never holds more than two keys, never deletes or turns off an active key itself, and refuses a named user that exists outside that path with "user <name> exists and was not made by the platform". On `seaweedfs` the same holds with the marker as the name prefix `mp-<8 hex>-` instead of the path.
- **AC-5**: `docker compose --profile mp-list run --rm <p>_list_<q>` prints one JSON line per bucket, database, user or role the provider holds, in the same row shape for `seaweedfs`, AWS (Moto) and `postgres`; a row this stack made carries its marker in `stack`; the output holds no secret.
- **AC-6**: A binding with `adopt: true` naming an existing bucket or database the platform did not make: the resource job accepts it and leaves it untagged, the consumer gets a fresh login granted that resource, and (Postgres) the new login reads and writes every table the database held while nothing outside that database changes owner and no other database's `CONNECT` grants change. Adoption takes ownership of the database's objects away from their old owner; the README says so. The same binding without `adopt` fails its job naming the resource. Two consumers may share one resource when the later binding says `adopt: true`: check warning `shared_resource`, each with its own login.
- **AC-7**: An external field may say `{same_as: <instance>.<provide>.<field>}`. Linked secret fields share one placed file, which `setup.sh` lists once and the managed review asks once; a link to a missing field, a field of another type, or a cycle is `invalid_same_as`.
- **AC-8**: The static render has no revoke job and no `mp_jobs` service; unbinding a consumer and running `up` again leaves its login usable, which the README states. The managed and platform renders have no `mp_jobs` either.
- **AC-9**: Before `up`, `api` records each resource and login the apply's bindings name in `holdings` (state `pending` when new) and `holding_uses`, so a login made by a failed or rolled back apply is never lost. After the apply (and after a rollback apply, against its own graph), `api` runs every list job of the stack, turns the holdings it finds into `active`, and every login holding of this stack that no binding names any more is revoked (IAM keys set inactive, Postgres role `NOLOGIN`), its data kept, state `revoked`. When the revoke fails, the apply stays `succeeded`, the holding is `revoke_pending` and the stack screen says so naming the provider, and the next apply tries again.
- **AC-10**: Removing a stack first revokes every active login it holds on an external provider; a failure becomes a warning on the remove naming the provider, never a secret; the stack's holdings go with it. The remove dialog says that on an external only the keys or logins are disabled (buckets, databases, users and roles stay), and that without "delete data" logins inside kept volumes stay usable.
- **AC-11**: On the managed stack screen, selecting a provisioner node opens a panel that runs its list jobs live (at most 60 seconds) and shows each row with name, type, made by (this stack, another stack, not the platform), adopted, used by, and login state, with loading, empty and error states and a refresh button; only a signed in admin may call it.
- **AC-12**: An SDK consumer whose readiness waits on a binding answers `/readyz` 503 naming only the waiting slots; its `healthcheck` prints `mp-waiting: <slots>`; the deployer state carries those slots; the managed graph shows the edge as `waiting` and the provider node (external or not) as `unreachable from <consumer>`, in text. A failed resource or login job flags its provider as `job failed: <service>`. When the apply fails, the deployer samples the state before the rollback and `api` keeps those flags on the failed operation, which shows them against the graph that failed. Output of other healthchecks never becomes a flag.
- **AC-13**: When an external is unreachable or refuses the given login at `up`, its SDK consumers never become healthy and `up -d --wait` exits non zero (a managed apply fails and rolls back, 0006); behind an external provisioner, the resource or login job fails first and the consumer never starts.
- **AC-14**: Switching a consumer between an internal and an external provider changes only the graph's binding (and the external instance); the consumer's entry and env names stay the same.
- **AC-15**: The deployer's run endpoint runs only services labelled `job: list` or `job: revoke` of the named project, accepts only the env key `MP_REVOKE` and only for a revoke job, and is callable only with `api`'s binding token; list rows, problems and log lines never carry a secret value.
- **AC-16**: A consumer's `uses` on a capability with an `access` vocabulary declares required, optional and `one_of` actions. The binding grants exactly the declared actions its identity provider offers (the first offered alternative of a group), minus the optional ones the binding drops. A required action or group the provider does not offer is `access_not_offered` in the resolved graph. The same consumer entry gets `s3:ListParts` on `seaweedfs` and `s3:ListMultipartUploadParts` on AWS (Moto), and an action outside its grant set is refused on `seaweedfs`.
- **AC-17**: An SDK module takes changed binding credential files, `_FILE` values, env files and its clients directory while it runs, without a restart. A credential set is swapped in only after its files are stable over two polls and the binding's readiness check passes with it; until then the old client serves and `/readyz` answers 503 naming the slot with reason `credentials`. A changed value is checked and swapped or refused whole like a pushed snapshot. Bootstrap names and the process env never reload.
- **AC-18**: When a login's key file is lost, the next `up` makes a new key and leaves the old one active. The old key is turned off only by the retire job `<i>_retire_<c>_<s>` (profile `mp-retire`), and in managed mode only after the consumer is confirmed on the new key: it started after the key file was written, or it runs on the SDK and its newest health check prints (`mp-credentials:`) the digest of that file's content. Managed mode restarts a consumer on an unchanged image to confirm it. A consumer that is not confirmed keeps its old key, and the apply logs a warning naming it. Static mode names the restart and retire commands in the job's log and the README. No rule depends on a key's creation date.

## Decision

**Chosen option**: Option 1: split resource and login into two provisioners linked by an `identity` slot, with the AWS IAM API as the shared login protocol, SeaweedFS as the bundled S3 server, and the platform database as managed mode's record.

A resource provisioner makes the resource and an identity provisioner, bound through the resource provider's `identity` slot, makes one login per consumer and grants it that resource; S3 buckets and IAM logins use one script set for SeaweedFS and AWS; static mode provisions but never revokes; managed mode records holdings and revokes after apply; SDK readiness names the waiting slot for the graph. Each binding grants only the actions its consumer declares and its identity provider offers (0023). A lost key is rotated by making the new one first and turning the old one off only once the consumer is confirmed on it, with the SDK reloading files while it runs (0024).

**Implementation skills**: `golang-error-handling` (`samber/cc-skills-golang`, `.agents/skills/golang-error-handling/`) · `golang-testing` (`samber/cc-skills-golang`, `.agents/skills/golang-testing/`) · `golang-security` (`samber/cc-skills-golang`, `.agents/skills/golang-security/`) · `golang-database` (`samber/cc-skills-golang`, `.agents/skills/golang-database/`) · `golang-observability` (`samber/cc-skills-golang`, `.agents/skills/golang-observability/`) · `supabase-postgres-best-practices` (`supabase/agent-skills`, `.agents/skills/supabase-postgres-best-practices/`) · `openapi-spec-generation` (`wshobson/agents`, `.agents/skills/openapi-spec-generation/`) · `aws-iam` (`aws/agent-toolkit-for-aws`, `.agents/skills/aws-iam/`) · `securing-s3-buckets` (`aws/agent-toolkit-for-aws`, `.agents/skills/securing-s3-buckets/`) · `seaweedfs` (`itechmeat/llm-code`, `.agents/skills/seaweedfs/`)

## Rationale

Reasoning and options: see [rationale.md](rationale.md).

## Feature design (cross child contract)

The children hold the detail; these pieces cross them.

**Names**: resource job `<p>_provision_<c>_<s>` (0014, kept), login job `<i>_login_<c>_<s>`, list job `<x>_list_<q>` per provisioner provide (resource or identity), revoke job `<i>_revoke_<r>` per identity provide (managed flavor only, run by `api`). Instance names hold no `_`, so none of these meets an instance name or another job.

**Order per binding**: provider (or nothing, when external) → resource job → login job → consumer. Every job has a dependent, so no wait anchor is needed.

**Marker** (which stack made it): static mode `mp_provisioned_` plus the first 8 hex digits of SHA-256 of the graph name (0014, kept); managed mode the same prefix plus the first 8 hex digits of SHA-256 of the stack's uuid, computed once when the stack row is created, stored in `stacks.marker` and never computed again, so no later change (origin, name) moves it (settles 0014's open question for #26). The render takes it from a new `marker` field of `ManagedRenderRequest.stack`; it is a render input, not part of the resolved graph, so the digest does not include it. Written as a tag `modular-platform-stack=<marker>` on buckets and IAM users, as the IAM path `/modular-platform/<marker>/`, and as membership of the marker role in Postgres. SeaweedFS ignores IAM paths and user tags (spike), so its users carry the marker as the name prefix `mp-<8 hex>-`, selected by the `iam` admin field `marker_style: prefix`. Its bucket tags work, so buckets everywhere keep the tag (0019).

**List row** (one JSON object per stdout line, the same for every provisioner):

| Key | Type | Meaning |
|---|---|---|
| `kind` | `resource` or `login` | what the row is |
| `type` | text | from the capability: `s3-bucket`, `iam-user`, `postgresql-database`, `postgresql-role` |
| `name` | text | bucket, database, user or role name |
| `stack` | text | the marker found on it, or `""` when it carries none |
| `active` | bool | logins only: can sign in (an active key, `LOGIN`) |
| `keys` | int, optional | logins only: the number of active keys, for identities that hold keys (`iam`); above 1 means a retire is owed (0024) |
| `grants` | text array | logins only: the resource names it is granted |

**Data model additions beyond the two confirmed tables** (from the cross check): `stacks.marker` (`text`, not null, unique), `operations.flags` (`jsonb`, null), and the holding state `pending` (0020).

**Problem codes added** (renderer, 0018): `invalid_identity`, `identity_not_granting`, `identity_credentials_mismatch`, `missing_identity_slot`, `invalid_external_use`, `invalid_same_as`, warning `shared_resource`; (catalog and renderer, 0023): `unknown_access`, `invalid_access`, `missing_access`, `invalid_offers`, `access_not_offered`, `invalid_drop`, warning `access_not_enforced`. API (0020, 0024): `unknown_job`, `list_timeout`, `provider_unreachable`, `not_deployed`, `retire_timeout`, and the retire result `not_confirmed`.

**Grant set** (0023): the resolved graph carries each binding's granted actions per scope at `modules.<c>.uses.<s>.access`, and the login job gets them as `MP_GRANT_<SCOPE>`. Consumers name raw action names; dialects between servers are covered by `one_of` groups, never by a branch in the scripts.

**Configuration required**: no new `MP_*` setting. Fixed values: list run 60 seconds, revoke and retire runs 120 seconds, list stdout at most 4 MB, SDK reload poll 5 seconds (two stable polls), credential digest 8 hex digits of SHA-256 per file, SDK confirmation wait 60 seconds, retire step budget 5 minutes per apply, old client closed 30 seconds after a swap. Pinned images (exact tags, at least two weeks old, picked in the spike): `amazon/aws-cli` (the S3 and IAM job image), `chrislusf/seaweedfs:4.48` (run with `-s3.iam.readOnly=false`), and `motoserver/moto` for tests only.

## Build plan

Tracer Bullet: the first milestone runs one S3 binding end to end on the bundled module in a static zip; each later milestone thickens one strand (AWS, Postgres, managed record, the flag).

**Milestone 1: spikes and the S3 thread on SeaweedFS**
1. [x] Spike (done 2026-11-05, results in rationale.md): SeaweedFS 4.48 in one container serves S3 and IAM on 8333 with `-s3.iam.readOnly=false`, persists and enforces user policies, keeps bucket tags, ignores IAM paths and user tags (so users take the name prefix), answers `NotImplemented` to `PutPublicAccessBlock`, refuses `s3:ListMultipartUploadParts` (it takes `s3:ListParts`), and gives keys no `CreateDate`; Moto, `amazon/aws-cli` under the hardening defaults and Docker's health log behave as needed. The two gaps are decided in 0023 and 0024, satisfies **AC-1**, **AC-2**, **AC-12**
2. Contract: the `identity` section with `grants` as a map, `{identity: f}` sources, `yields.resource`, named field templates, `provides.<q>.identity`, identity slots and self binding, the `access` vocabulary, `offers`, `uses.<s>.access`, `drop` and the resolved graph's grant set (0023), all problem codes, in `contracts/openapi/renderer.yaml`, `libs/contract` and `libs/check`; `task generate`, satisfies **AC-3**, **AC-16**
3. Catalog: `s3` (with its `access` vocabulary), `s3-provisioner`, `iam` (default offers, `marker_style`) capability files with their scripts; the `seaweedfs` module (`-s3.iam.readOnly=false`, `marker_style: prefix`, its `offers`); the e2e consumer's `access` declaration; `sdk/s3` and the sdk manifest read `access_key_id` as a file, satisfies **AC-1**, **AC-2**, **AC-16**
4. Render: the login job with `MP_GRANT_<SCOPE>`, the new order, the identity env (`MP_RESOURCE_*`, `MP_ADOPT`), golden files for a consumer bound through `seaweedfs`, satisfies **AC-2**, **AC-16**
5. e2e (`e2e` tag): two consumers on `seaweedfs`, each writes and reads its own bucket and is refused by the other's; users carry the prefix; a 16 MB multipart upload works; a dropped `s3:DeleteObject` is refused; a second `up` changes nothing, satisfies **AC-1**, **AC-2**, **AC-16**

**Milestone 2: AWS, rotation, adoption, links**
6. The AWS external: a graph with one external `aws` (provides `buckets` and `iam`, `identity` bound to `aws.iam`), the path fence and the date free key rules of 0024 in the `iam` scripts; e2e against Moto, the same consumer as step 5, with its grant set in two statements, satisfies **AC-1**, **AC-4**, **AC-14**, **AC-16**
7. SDK live reload: polling the files read at start, the stable pair rule, the checked swap of a binding's client, value snapshots from files (only getters follow), the clients directory, `/readyz` 503 while credentials are pending, the `mp-credentials:` line from `healthcheck`; unit tests with a fake clock, satisfies **AC-17**, **AC-18**
8. The retire job behind `mp-retire` in the static and managed flavors (env `MP_USER`, `MP_MARKER`, admin), the `iam` retire script, `identity.job.retire` in the contract, the guard rule, the login job's log line and the README section as golden files; e2e static on `seaweedfs`: empty a key file, `up`, the SDK consumer moves to the new key, retire turns the old one off, the consumer still works, satisfies **AC-18**, **AC-4**
9. External `uses` and `same_as` in renderer, render (one placed file per link chain) and the managed review, satisfies **AC-3**, **AC-7**
10. `adopt` and `shared_resource` for S3; e2e: adopt an untagged bucket holding an object, the consumer reads it; without `adopt` the job fails, satisfies **AC-6**

**Milestone 3: the Postgres split and no revoke at up**
11. `postgresql-provisioner` makes databases only, new `postgresql-roles` makes logins (owner role, grants, adopt); the `postgres` module provides both and binds its identity slot to itself; the platform graph (0015) follows; the 0014 e2e scenarios pass on the new shape, plus taking over a database made by the 0014 shape without data loss, satisfies **AC-2**, **AC-6**
12. Remove the revoke job from `up` and the wait anchor `mp_jobs` from render, guard, deployer live state and goldens; README text for static mode, satisfies **AC-8**
13. List jobs behind the `mp-list` profile for every provisioner provide, with `keys` for `iam`; e2e: the same row shape from `seaweedfs`, Moto and `postgres`, satisfies **AC-5**

**Milestone 4: the managed record**
14. Migration `00002_holdings.sql` (both tables, `stacks.marker` with its backfill, `operations.flags`), sqlc queries, the store, satisfies **AC-9**, **AC-12**
15. Deployer `POST /v1/stacks/{stack}/jobs/{service}/run` with its allow list, timeouts and output cap; the revoke services behind the `mp-revoke` profile in the managed flavor; the guard accepts `profiles`; the retire endpoint with its digest confirmation, one restart per consumer and the 5 minute budget (0024); the render's `runtime: sdk` label on SDK consumers in the managed flavor (golden), satisfies **AC-15**, **AC-9**, **AC-18**
16. `api`: `pending` holdings before `up`, the `holdings` step after apply and after a rollback, revoke before `up` for providers the graph drops, revoke of unused logins, `revoke_pending` and its retry, retire of logins with more than one key, revoke before remove and the remove dialog text; integration test with `seaweedfs`, including a rotation retired by the apply and an unchanged image consumer restarted first, satisfies **AC-9**, **AC-10**, **AC-18**
17. `api` list endpoint and the editor panel on the stack screen, satisfies **AC-11**, **AC-15**

**Milestone 5: the waiting flag**
18. SDK: `/readyz` body with `waiting` and its `reason` (`binding` or `credentials`), the `mp-waiting:` line from `healthcheck`, satisfies **AC-12**, **AC-13**, **AC-17**
19. Deployer state `waiting`, the sample before rollback, `api` mapping to providers and `operations.flags`, job failure flags, editor badges on edges and provider nodes (live state and failed operation); integration test: an external that refuses the login, satisfies **AC-12**, **AC-13**

## Consequences

**Positive**:
- A consumer never knows whether its bucket or database is in the stack or outside it, and an AWS account and the bundled server run the same scripts (#7 gets this for free).
- Logins become exchangeable on their own: an LDAP or certificate based identity provider later is one more identity capability, not a new provisioner.
- No idle wait anchor container, and `up` runs no admin job that touches logins of other bindings.
- Managed mode knows which module uses which resource, the basis #26 needs for "reuse a fitting resource".

**Negative / tradeoffs**:
- Static mode never closes a login: an unbound consumer's credentials keep working until you disable them by hand.
- One more job per binding (the login job), and the Postgres provisioner shipped this week is reworked.
- Unchanged container images get no start check; only SDK consumers flag a waiting binding (AC-12). An unchanged consumer of a broken external shows only its own state.
- `access_key_id` reaches the consumer as a file, so an unchanged image that only reads it from an env var needs a wrapper.
- The managed marker lives in the database: restoring an install without its database makes every holding look like another stack's (static mode's marker still follows the graph name).
- The least privilege AWS admin policy is checked only against real AWS (Moto and SeaweedFS do not enforce policies), so it stays marked unverified until the manual check runs.
- AWS bucket names are global, so a default bucket name can collide with another account's bucket; the job then fails and you name it.
- Consumer authors name raw IAM actions, so they must know that a server dialect exists (`s3:ListParts` on SeaweedFS) and cover it with `one_of`; a consumer that forgets gets `access_not_offered` on that server, never a silent wider grant.
- After a rotation an old key stays active until retire runs: in static mode until you run it, in managed mode until the consumer is confirmed. A second key file loss before then stops the login job with a message.
- The two key files are written one after the other; a crash between them leaves a mismatched pair. An SDK module keeps its old client (the check fails), and its digest never matches, so nothing is retired; an unchanged image that restarts on it fails. You fix it by emptying `uses_<s>_access_key_id` and running `up` again.
- On `seaweedfs` a login name over 52 characters is refused in the resolved graph, because the prefix takes 12 of IAM's 64.
- A value a module used in setup does not follow a reload; only getters do, until the module restarts.
- Every SDK module polls the files it read at start every 5 seconds, a small constant cost.

**Neutral**:
- Contract `modular-platform/v1` changes before it is published, so no module author sees the old shape.
- Moto stands in for AWS in CI; a run against a real account stays manual.

## Migration plan

**Strategy**: in place change on unreleased v1, with a take over rule for databases already made.
**Phases**:
1. Milestones 1 and 2 add the new capabilities beside the current Postgres shape; nothing existing changes.
2. Milestone 4's migration adds `stacks.marker`; existing rows get the marker they already use (0014's, from the graph name), so nothing they made changes owner or path; only new stacks use the uuid.
3. Milestone 3 switches `postgres` to the split. The `postgresql-provisioner` resource job treats a database owned by a member of the marker role (the 0014 shape) as made by this stack: it moves ownership to the new owner role and the login job grants it to the same login name, so data and passwords stay.
4. Milestone 3 removes `mp_jobs`; a stack that ran it loses the container through `--remove-orphans` (README, deployer).
**Rollback**: revert the milestone's commits; databases taken over keep working with the 0014 scripts, because the login stays the database's effective owner through its owner role membership.
**Risks**: the take over misses an object type (sequences, schemas) and the login loses access; the step 11 e2e covers tables, sequences and schemas made by the 0014 shape.

## Follow-up

- [x] (done 2026-11-05) On confirmation, set 0014's status line to `Superseded in part by [0017](../../0017-external-capability-providers/index.md) (yields.login, revoke at up, mp_jobs, postgresql-provisioner scripts)`, and add pointers in 0012 (external `uses`, `same_as`, the shared placed path), 0005 (`/readyz` body, `access_key_id` as file), 0007 (the run endpoint, `profiles` in the guard, `waiting` in state, the `mp_jobs` rules removed) and 0006 (the `holdings` step).
- [ ] #6 takes the slot picker as a requirement: on a consumer slot, add an external (url and login), or pick a provider and then an existing resource from its list (sets `adopt: true`) or a new one.
- [ ] #26 builds "reuse a fitting resource" on `holdings` and the list API; #9 decides reuse of an external's admin settings across stacks.
- [ ] #36 decides whether a running stack re checks providers on a timer; this feature flags only at start.
- [ ] #8 grows SDK readiness into the full capability conformance check.
- [ ] More identity capabilities: LDAP or OIDC backed logins, and certificate logins (SeaweedFS, Postgres `cert` auth), each one more identity capability granting existing resource types; widening an identity slot to "any identity capability granting this type".
- [ ] #8 should weigh narrowing `egress` for jobs and consumers of an external to that server alone (0011's follow up still stands).
- [ ] A manual job against a real AWS account, to catch what Moto does not model.
- [ ] Record the new tools in the right `AGENTS.md` (`/sync`): SeaweedFS, the AWS CLI job image and Moto in `libs/render/AGENTS.md`; the skills `aws-iam`, `securing-s3-buckets` and `seaweedfs` (installed 2026-11-05) in `## Agent skills`; MCP servers: the AWS API MCP Server (`awslabs`, for the manual real AWS check, connected by you); offered but not chosen: IAM Policy Autopilot, `mastering-aws-cli`.
- [ ] Optional `tls` slot on `seaweedfs`, so consumers reach it over TLS like `postgres` (0014).
- [ ] A check job per binding for consumers on unchanged images, if their silent failures hurt in practice (rationale.md).
- [ ] Record in rationale.md the exact values the spike settled but this update does not hold: the `seaweedfs` command line and healthcheck, and the `amazon/aws-cli` and `motoserver/moto` tags.
- [ ] Add pointers on confirmation: 0005 (live reload of files, the `credentials` reason on `/readyz`, 0024), 0012 (`drop` on a binding, `offers` on an external provide, 0023), 0013 (`uses.<s>.access` in a module entry, `provides.<q>.offers`), 0007 (the retire endpoint, `mp-retire` in the guard).
- [ ] #6's slot picker shows a binding's grant set and lets the admin drop optional actions (0023).
- [ ] An access vocabulary for `postgresql` (privileges such as `SELECT`, `INSERT`, DDL), so Postgres logins follow the same declarations.
- [ ] Recheck the admin policy for AWS against the grant set: the admin must be allowed to grant every action an identity offers.
