# 0019. The S3 family on SeaweedFS and AWS

**Parent**: [0017. External capability providers](index.md)

## Summary

Three capabilities make S3 work the same in and outside the stack: `s3` is what a consumer gets, `s3-provisioner` makes buckets through the plain S3 API, and `iam` makes one login per consumer through the AWS IAM API. SeaweedFS serves both APIs, so the bundled `seaweedfs` module and a real AWS account run the very same scripts in an `amazon/aws-cli` job image. Moto stands in for AWS in tests. The spike (rationale.md) showed two differences, and both are settled by data, not by branches per server. SeaweedFS ignores IAM paths and user tags, so its users carry the marker as a name prefix. It also names one action differently, which the consumer's access declaration covers (0023).

## Capabilities

All three: `version: "1"` on every provide (the protocol generation), slots ask `>=1`.

| Capability | Fields | Notes |
|---|---|---|
| `s3` | url (url), region (optional), bucket (pattern `^[a-z0-9][a-z0-9-]{1,61}[a-z0-9]$`), access_key_id (secret), secret_access_key (secret), ca (certificate, optional) | `access_key_id` is a file, because AWS and SeaweedFS make it and the job writes it (`output`) |
| `s3-provisioner` | url (url), region, admin_access_key_id, admin_secret_access_key (secret), ca (certificate, optional) | `yields` `s3`, `resource: s3-bucket`, `named: [bucket]` with `name: "{graph}-{consumer}-{slot}"`; `url`, `region`, `ca` from `admin`; `access_key_id`, `secret_access_key` from `identity` |
| `iam` | url (url), region, admin_access_key_id, admin_secret_access_key (secret), ca (certificate, optional), marker_style (enum `tag`, `prefix`; optional, absent means `tag`) | `identity`: `type: iam-user`, `grants: {s3-bucket: {offers: [...]}}` with the AWS action names as default offers (0023), `named: {user: {pattern: "^[a-z0-9_+=.@-]{1,64}$"}}` (no `,`, so `MP_REVOKE` can always carry it), `login: user`, `credentials: {access_key_id: output, secret_access_key: output}` |

Job image for both: `amazon/aws-cli` at an exact tag. Every script sets `HOME=/tmp`, reads the admin secret inside the script (`AWS_SECRET_ACCESS_KEY="$(cat "$MP_ADMIN_ADMIN_SECRET_ACCESS_KEY_FILE")"`, never on a command line), passes `--endpoint-url "$MP_ADMIN_URL"`, `--region "$MP_ADMIN_REGION"`, and `--ca-bundle "$MP_ADMIN_CA_FILE"` when set. `sdk/s3` and the sdk manifest read `access_key_id` from its file (`ACCESS_KEY_ID_FILE`).

## `s3-provisioner` scripts (normative)

Provision (resource job):
1. `head-bucket MP_BUCKET`. Not found → `create-bucket` (with `LocationConstraint` unless the region is `us-east-1`), `put-bucket-tagging` with `modular-platform-stack=$MP_MARKER`, and `put-public-access-block` with all four settings true (a `NotImplemented` answer is ignored, for servers without it).
2. Found → `get-bucket-tagging`: our marker → done. No marker or another one → with `MP_ADOPT=1` done (no tag written), else fail "s3-bucket <name> exists and was not made by this stack".
3. `403` on `head-bucket` → fail "s3-bucket <name> belongs to another account" (AWS bucket names are global). Never deletes a bucket or an object.

List: `list-buckets`, then per bucket `get-bucket-tagging`; one `resource` row each (`type: s3-bucket`, `stack` from the tag, `""` when none).

## `iam` scripts (normative)

Policy name on every user: `modular-platform`. The marker is written one of two ways, chosen by the admin field `marker_style`:

- `tag` (AWS, Moto): path `/modular-platform/$MP_MARKER/` and tag `modular-platform-stack=$MP_MARKER`; the provider name is `MP_USER`.
- `prefix` (SeaweedFS, which ignores both): no path, no tag; the provider name is `mp-<h>-$MP_USER`, where `<h>` is the 8 hex digits of `MP_MARKER` after `mp_provisioned_`. The planner's names stay unprefixed everywhere else (plan, holdings, `MP_REVOKE`); only the scripts add and strip the prefix. The prefix takes 12 of IAM's 64 characters, so when the bound `iam` provide sets `marker_style` to `prefix` by value, the planner refuses a login name over 52 characters with `invalid_named_value` at the binding. The job still checks the provider name, as a last guard for a value it could not see in the plan. Both scripts use one shared shell function for the name and the lookup.

Provision (login job):
1. Find the user. With `tag`: `list-users --path-prefix /modular-platform/<marker>/` (never `get-user`, which a path scoped admin policy may refuse for a name it does not hold). With `prefix`: `list-users` and an exact match on the provider name. Found → ours. Not found → `create-user` (with `tag`: `--path <path> --tags Key=modular-platform-stack,Value=$MP_MARKER`). An `EntityAlreadyExists` answer means a user with that name exists outside our marker → fail "user <name> exists and was not made by the platform". `AccessDenied` is reported as "the admin login lacks <action>", never read as "foreign".
2. `put-user-policy` with exactly the binding's grant set from `MP_GRANT_BUCKET` and `MP_GRANT_OBJECTS` (0023): one statement on `arn:aws:s3:::<MP_RESOURCE_BUCKET>`, one on `arn:aws:s3:::<MP_RESOURCE_BUCKET>/*`, each only when non empty. Nothing else. Written on every run, before any key is made, so a new key never works before its policy does.
3. Keys: the date free rules of [0024](0024-key-rotation-reload.md). A new key is made only when the key file was lost or the key is gone on the provider, and an old key is turned off only by the retire job, once the consumer is confirmed on the new one. The file is written in place, so the consumer's mount sees it. An SDK consumer reloads it while running (0024). An unchanged image needs a restart, which managed mode does and static mode names in the job's log line and the README.

Revoke (managed only, 0020): for each name in `MP_REVOKE` that is ours (by path with `tag`, by provider name with `prefix`): every key `Inactive`. Never deletes a user, key secret on disk, bucket or object.

Retire: the script of [0024](0024-key-rotation-reload.md).

List: `list-users` (all paths, the CLI pages through them); one `login` row per user. `stack`: with `tag`, from the path when it starts with `/modular-platform/`; with `prefix`, `mp_provisioned_<h>` when the name matches `^mp-[0-9a-f]{8}-`, and then `name` is the rest; else `""` and the full name. `active` when any key is `Active`, `null` when the admin may not read its keys (a user outside our path under the policy below). `keys` is the number of `Active` keys (0024). `grants` is the bucket names in the `modular-platform` policy of our users, `[]` for others. With `prefix`, a foreign user named `x` and ours named `mp-<h>-x` both list as `x`, and 0020's merge joins a holding to the row with this deployment's marker first.

**JSON output**: the image has no `jq`, so scripts build each row from `aws ... --output json --query` results (the CLI escapes names) and print one line per row with `printf`, passing names only through values the CLI already escaped. Rows are sorted by `kind`, then `name`.

**Admin policy** the README gives for an AWS account (least privilege, marked unverified until the manual real AWS check runs, since Moto and SeaweedFS do not enforce policies): `s3:CreateBucket`, `s3:ListAllMyBuckets`, `s3:ListBucket` (needed by `head-bucket`), `s3:GetBucketTagging`, `s3:PutBucketTagging`, `s3:PutBucketPublicAccessBlock` on `*`; `iam:ListUsers` on `*`; `iam:CreateUser`, `iam:TagUser`, `iam:ListAccessKeys`, `iam:CreateAccessKey`, `iam:UpdateAccessKey`, `iam:DeleteAccessKey`, `iam:PutUserPolicy`, `iam:GetUserPolicy` on `arn:aws:iam::<account>:user/modular-platform/*`. The `aws-iam` skill checks it during the build.

## The `seaweedfs` module

- Image `chrislusf/seaweedfs:4.48` (spiked), one container serving S3 and its embedded IAM on 8333 with the command and healthcheck the spike settled, which must include `-s3.iam.readOnly=false` (without it the embedded IAM refuses every write). Data in volume `data`, hardening defaults with the tmpfs the spike needs.
- Secrets: `admin-secret` (generated). A template writes SeaweedFS's S3 identity config with the admin identity `mp-admin`, mounted read only.
- Provides `buckets` (`s3-provisioner`, port 8333, `identity: identity`) and `iam` (`iam`, port 8333), both: `url: {platform: url}`, `region: {value: us-east-1}`, `admin_access_key_id: {value: mp-admin}`, `admin_secret_access_key: {secret: admin-secret}`; `iam` also `marker_style: {value: prefix}` and `offers.s3-bucket` with the default list, `s3:ListParts` in place of `s3:ListMultipartUploadParts` (0023). `uses.identity` (capability `iam`) binds `seaweedfs.iam` automatically.
- TLS through an optional `tls` slot is a follow up; today consumers reach it over plain HTTP on `internal`.
- Spiked: users, keys and policies made through the IAM API persist in the filer across a restart beside the `mp-admin` identity from the config file, and SeaweedFS enforces user policies. Bucket tags work, so buckets keep the tag marker (`s3-provisioner` has no `marker_style`). `put-public-access-block` answers `NotImplemented`, which the provision script already ignores. Access keys carry no `CreateDate`, which 0024's rules never need.

## The AWS external

```yaml
aws:
  external:
    provides:
      buckets:
        capability: s3-provisioner
        version: "1"
        identity: identity
        fields:
          url: https://s3.eu-central-1.amazonaws.com
          region: eu-central-1
          admin_access_key_id: AKIAEXAMPLE
      iam:
        capability: iam
        version: "1"
        fields:
          url: https://iam.amazonaws.com
          region: us-east-1
          admin_access_key_id: {same_as: aws.buckets.admin_access_key_id}
          admin_secret_access_key: {same_as: aws.buckets.admin_secret_access_key}
    uses:
      identity: aws.iam
```

You place `secrets/aws/buckets_admin_secret_access_key` once; both provides use it. Two externals with their own keys (least privilege per role) are just as valid.

## Critical test scenarios

- e2e (`e2e` tag, real `seaweedfs`): two consumers each write and read an object in their own bucket and get `AccessDenied` on the other's; their users are named `mp-<h>-<consumer>_<slot>` and list without the prefix, with `stack` set; a second `up` keeps both key files unchanged and changes no user or bucket (**AC-1**, **AC-2**).
- e2e against Moto (one `motoserver/moto` container serving S3 and IAM, as an external `aws` with both urls pointing at it): the same consumer writes and reads its object; the user sits at `/modular-platform/<marker>/` with exactly its grant set (0023); a second run keeps the key; the key rules of 0024 (**AC-1**, **AC-4**, **AC-14**). A user name that already exists at `/` gives `EntityAlreadyExists` and the "not made by the platform" message (**AC-4**). Moto does not enforce policies, so refusal is proven on SeaweedFS only.
- A named user that exists at `/` on Moto: the login job fails with the "not made by the platform" message, the consumer never starts (**AC-4**).
- Adopt: an untagged bucket holding an object, bound with `adopt: true`: the consumer reads the object, the bucket stays untagged; without `adopt` the resource job fails naming it (**AC-6**).
- Golden: the same consumer entry bound through `seaweedfs` and through the external `aws`: identical env names, both key fields as `_FILE` paths (**AC-1**, **AC-14**).

## Rationale (short)

SeaweedFS (your choice) is the one maintained self hosted S3 server found that serves the AWS IAM API itself (`CreateUser`, `CreateAccessKey`, `UpdateAccessKey`, `PutUserPolicy`, `ListUsers`, confirmed in its wiki), so one access capability fits both, as you asked. MinIO was the runner up you named, but its repository was archived in April 2026 and it makes users through its own admin API, which would have needed a second script set. Garage is maintained but has its own key API and no IAM. Moto (your choice) needs no account; LocalStack now needs an auth token. The spike confirmed the IAM path and found two gaps, each closed without a branch per server: the user marker as a name prefix (the fallback decided before the spike), and the action name dialect through the consumer's access declaration (0023, your choice over dropping the action). The AWS CLI image keeps scripts plain POSIX `sh`, like the Postgres jobs, instead of a Go job binary the platform would have to ship and pin.
