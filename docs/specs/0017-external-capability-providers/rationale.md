# 0017. External capability providers: rationale

## Context

> ⚠️ Premise note: this topic spans four separate decisions (how logins relate to resources, the S3 family and its bundled server, the managed record with revoke, and the flag on the graph), so it is an umbrella with four children. It also reworks work shipped this week: the Postgres provisioner of 0014, its revoke job at `up`, and the wait anchor `mp_jobs` (commits `babac2f`, `6152026`). That is cheap now, because `modular-platform/v1` is not yet published, and costly after.

Spec 0011 already lets a graph name a service outside the stack (`external:`), with its non secret fields in the graph and its secrets as files you place, and an external provisioner whose jobs reach it over `egress`. Scope row #10 still asks for more: consumers that cannot tell an external from a module, a check that the external is reachable and accepts its login (without the public planner ever calling an address you supply), the same list, add and remove commands on a bundled S3 module and on an AWS account with the same result shape, admin settings entered once, a flag on the graph, and switching between internal and external as an ordinary exchange.

The hard force is that servers make logins in different ways. Buckets use the S3 API everywhere, but AWS makes a login with its IAM API, MinIO with its own admin API, Postgres with SQL roles, and a server can also trust an outside identity source (LDAP, OIDC, certificates). 0014 put the scripts on the provisioner capability, which works while every server of one protocol makes logins the same way. A second force is that static mode has no platform at run time: whatever lists or revokes there must work with standard tools alone, and nothing can record which module uses what. A third is that most modules wrap unchanged images, which cannot run platform code.

Not deciding leaves #26 (config wired from the network) without a way to list or reuse resources, and #7 (exchange a provider) without an external to exchange with.

## Options considered

### Option 1: split resource and login, linked by an `identity` slot

A resource provisioner makes the bucket or database; an identity provisioner, bound through the resource provider's `identity` slot, makes one login per binding and grants it the resource. The AWS IAM API is the shared login protocol for S3 (SeaweedFS serves it), Postgres gets `postgresql-roles`. Managed mode records holdings and revokes after apply; static mode does not revoke. SDK readiness names the waiting slot.

**Pros**:
- Logins become exchangeable on their own; LDAP, OIDC or certificate logins later are one capability each.
- One S3 and one IAM script set serve SeaweedFS and AWS.
- No wait anchor; revoke decides by a record, not by scanning every marked login.

**Cons**:
- The biggest contract change: a new capability section, a slot that may bind its own instance, a login job per binding.
- Reworks the Postgres provisioner shipped this week.
- Static mode never closes a login.

### Option 2: one provisioner capability per admin API

Keep 0014's shape: `s3-provisioner-minio` and `s3-provisioner-aws`, each yielding `s3` with its own scripts; list as a third script on each.

**Pros**:
- No new contract concepts; Postgres untouched.
- Each provisioner's admin fields match its API exactly.

**Cons**:
- Logins are never exchangeable apart from the server.
- Two script sets for S3, more for each new server; MinIO, the natural bundled pick, is archived.

### Option 3: scripts on the providing module, adapters for externals

Each module ships its own provision and list scripts; an external names an adapter entry that carries them.

**Pros**:
- A server's quirks live with its module.
- Capabilities stay plain field lists.

**Cons**:
- Externals gain a new concept (adapters) and every server repeats the same SQL or S3 calls.
- Changes 0014's central rule (scripts on the capability) without making logins exchangeable.

For the start check, the options were a probe container per external (continuous, one idle container each), a check job per binding (works for unchanged images), and the check inside the consumer (the SDK's readiness, no new container).

## Rationale

Option 1, because the forces in Context are about logins, not buckets. Your direction was clear: access should be the same for AWS and the bundled server, identity providers exchangeable, and Postgres split too. The research made that concrete. SeaweedFS serves the IAM API itself, so one access capability really covers both, and MinIO, the runner up you named, is archived and has no IAM API. Option 2 would ship sooner but locks logins to servers, which is exactly what you want to escape. Option 3 adds a concept without solving it.

Three of your choices trade safety or reach for simplicity, and the spec records them as such. **The check lives inside the consumer only.** Unchanged images then get no start check and no flag; the runner up (a check job per binding) would have covered them at one container per binding. I would still recommend that job as a later addition for unchanged consumers. **Static mode never revokes.** An unbound consumer's credentials keep working until you disable them; the README says so. This is consistent with static mode having no record, but it is a real open door on a shared AWS account. **Adoption takes the resource, never a login.** An existing login goes through a direct external instance, so the platform never holds a login it cannot rotate.

The managed marker comes from the deployment's uuid and is stored on the deployment row, so two installs sharing an AWS account never mistake each other's users, and a later change of origin or name never moves it. A first draft computed it from the public origin with nothing stored; the cross check showed that an origin change would then make every later apply fail, with no way back, since adoption never takes over a login.

## Cross check (2026-11-05)

A read only review on another model found 16 gaps; you chose to apply every recommended fix. The ones that changed the design: the marker stored on the deployment row (above); holdings recorded as `pending` before `up`, so a failed or rolled back apply leaves no unknown login; flags of a failed apply kept on the operation, since the rollback removes the containers that showed them; least privilege AWS policy fixed (`s3:ListBucket`, finding users by path instead of reading `AccessDenied` as foreign) and marked unverified until a real AWS check; Postgres adoption never revoking `PUBLIC` grants on an external server's other databases; a decided fallback (name prefix) if SeaweedFS ignores IAM paths or tags; fixed names and cleanup for one off run containers; key rules that never delete a key a consumer may still use. Its soundness notes (job order, dropping `mp_jobs`, profiles with `run --rm --no-deps`, the owner role scheme) found no flaw.

## Spike results (2026-11-05)

Build step 1, run by you. What it found:

| Check | Result | Effect on the spec |
|---|---|---|
| SeaweedFS 4.48 embedded IAM on 8333 | works, but only with `-s3.iam.readOnly=false`; without it IAM refuses every write | the flag goes into the `seaweedfs` command (0019) |
| `CreateUser --path`, `TagUser` | accepted and ignored: users carry no path and no tags | `marker_style: prefix` for SeaweedFS users, the fallback decided before the spike (0019) |
| `PutBucketTagging`, `GetBucketTagging` | work | buckets keep the tag marker everywhere; `s3-provisioner` needs no `marker_style` |
| `PutPublicAccessBlock` | `NotImplemented` | already ignored by the provision script |
| users, keys and policies across a restart | persist in the filer beside `mp-admin` from the config file | none |
| user policy enforcement | enforced: a consumer is refused another's bucket | no route back; AC-2's refusal is proven on SeaweedFS |
| `PutUserPolicy` with `s3:ListMultipartUploadParts` | refused; SeaweedFS takes `s3:ListParts`; Moto takes the AWS name | gap 1, decided in 0023 |
| `ListAccessKeys` | no `CreateDate` on SeaweedFS keys | gap 2, decided in 0024 |
| the one container command and healthcheck, Moto's IAM coverage, `amazon/aws-cli` under the hardening defaults, CMD healthcheck stdout in `State.Health.Log` | settled, as you reported | the exact command line and tags still go into this file (Follow-up) |

## Two gaps after the spike (2026-11-05)

### Gap 1: one action, two names

One fixed user policy can't name an action that one server requires and another refuses. Options:

- **Drop the action from the fixed policy** (my first recommendation). One policy everywhere and less privilege, and plain uploads never list parts. Con: no consumer can ask for more or less than the one fixed set.
- **An admin field per dialect** (`policy_actions: aws | seaweedfs`). Keeps full multipart rights. Con: every server difference becomes another field, and it names a server inside a capability.
- **Access declarations** (your choice). Consumers declare required, optional and `one_of` actions, providers declare what they offer, the binding grants the match, and the admin drops optionals. I offered two vocabularies: rights in domain words mapped per provider (my pick, which keeps dialects out of consumer entries), and raw action names with `one_of` for dialects. You chose raw action names: direct, and exactly what the policy holds. Con: consumer authors must know the dialects.

### Gap 2: no date to pick the older key

The old rule deleted the older of two keys when the key file was lost. SeaweedFS gives no `CreateDate`. Worse, age is wrong even on AWS: a crash between `create-access-key` and the file write leaves the newer key with a lost secret, and the older key is the one the consumer uses. Options:

- **Stop when ambiguous** (my first recommendation). Never delete. With two active keys and a lost file, fail with the key ids. Con: one manual step per such case, on every server.
- **Date when present, else stop.** Con: keeps the crash flaw on AWS.
- **List order as age.** Con: undocumented behaviour, and it keeps the crash flaw.
- **Make the new key, deploy it, then turn the old ones off** (your direction), with the SDK taking keys, config and secrets while it runs (your answer when I asked how "deployed" is known). I made the remaining calls in 0024. Reload works by polling, not inotify, because in place writes to single file bind mounts are not reported reliably. Readiness turns 503 until new credentials are in use. A container start after the write proves it for unchanged images, which managed mode restarts. My first draft proved the switch for SDK modules with a passing health check 15 seconds after the write; the cross check showed that a check can pass on the old key inside that window, so the healthcheck now prints a digest of the credentials in use. Static mode gets commands, not automation, consistent with it never revoking. The runner up, a retire job at every `up`, would turn off the key of a consumer that `up` did not recreate.

## Landscape check (2026-11-05)

Full notes: `docs/.agent-cache/research/external-capability-providers.md`.

| Server | IAM API | License | Status |
|---|---|---|---|
| MinIO | no (own admin API, from memory) | AGPLv3 | repository archived 25 April 2026, community images reportedly stopped late 2025 |
| SeaweedFS | yes, embedded in `weed s3` on 8333 (about 46 actions, including `CreateUser`, `CreateAccessKey`, `UpdateAccessKey`, `PutUserPolicy`, `ListUsers`) | Apache 2.0 (from memory) | maintained |
| Garage | no (admin API v2: `CreateKey`, `AllowBucketKey`) | AGPLv3 | maintained, API may change |
| RustFS | unknown | Apache 2.0 | maturity disputed |

AWS: the secret key is returned only by `CreateAccessKey`; user names up to 64 characters; paths up to 512 characters. Fake AWS: Moto (Apache 2.0, no account); LocalStack needs an auth token from 2026.03.0.

## References

**Project sources**:
- `AGENTS.md`: principles on pure domain, contract first, config only from `internal/config`, no secrets in logs
- Specs 0011, 0012, 0013, 0014 (externals, jobs, markers), 0005 (readiness), 0006 and 0007 (apply, live state, guard), 0016 (the keeper applies the platform graph)
- Installed skills: `aws-iam`, `securing-s3-buckets`, `seaweedfs`, `golang-database`, `supabase-postgres-best-practices`

**Practices & standards**:
- Least privilege per consumer login; disable, never delete, on revoke
- Idempotent "ensure" jobs that a second run leaves unchanged
- Health endpoint that names what it waits on

**Links** (web verified 2026-11-05):
- MinIO repository (archived): https://github.com/minio/minio
- MinIO community status (secondary source): https://glukhov.org/data-infrastructure/object-storage/minio-dead/
- SeaweedFS Amazon IAM API: https://github.com/seaweedfs/seaweedfs/wiki/Amazon-IAM-API
- Garage: https://git.deuxfleurs.fr/Deuxfleurs/garage
- AWS `CreateAccessKey`: https://docs.aws.amazon.com/IAM/latest/APIReference/API_CreateAccessKey.html
- AWS IAM quotas: https://docs.aws.amazon.com/IAM/latest/UserGuide/reference_iam-quotas.html
- Moto: https://github.com/getmoto/moto
- LocalStack 2026.03.0 account requirement: https://blog.localstack.cloud/localstack-for-aws-release-2026-03-0/
- AWS API MCP Server: https://github.com/awslabs/mcp/tree/main/src/aws-api-mcp-server

## Cross check of the update (2026-11-05)

A read only review on another model found 17 issues in 0023 and 0024; you chose to apply every recommended fix. The ones that changed the design: the healthcheck's credential digest replaces the 15 second rule (above); the deployer takes a binding's `output` files from its retire service's mounts, not a glob; the user policy is written before a key is made; the 52 character prefix limit is checked in the plan; the grant set's place in the plan (`modules.<c>.uses.<s>.access`); `offers` required on a grant whose type has a vocabulary; the warning `access_not_enforced` for a direct external; one restart per consumer and a 5 minute budget for the retire step. One suggestion was left out: mounting a directory so one credentials file can be renamed into place, since it changes the one file per field layout of 0013 and 0014 and the digest already catches a torn pair.
