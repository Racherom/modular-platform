# 0023. Access actions: what a consumer asks for, what a provider offers

**Parent**: [0017. External capability providers](index.md)

## Summary

A consumer now says in its catalog entry which actions it needs on a resource: some required, some optional, and some as a choice of alternatives (`one_of`). The capability lists every action that exists, each identity provider says which of them it can grant, and the checks give each binding exactly the match. An admin can drop optional actions on a binding. This replaces the fixed S3 user policy of 0019, and it lets SeaweedFS grant `s3:ListParts` where AWS grants `s3:ListMultipartUploadParts`, with no branch in the scripts.

## Contract

**1. The vocabulary, on the capability a consumer binds** (`catalog/capabilities/s3.yaml`). A new optional `access` section names the scopes (the parts of a resource a policy can point at) and every action with its scope:

```yaml
access:
  scopes: [bucket, objects]
  actions:
    s3:ListBucket: bucket
    s3:GetBucketLocation: bucket
    s3:GetObject: objects
    s3:PutObject: objects
    s3:DeleteObject: objects
    s3:AbortMultipartUpload: objects
    s3:ListMultipartUploadParts: objects   # the AWS name (AWS, Moto)
    s3:ListParts: objects                  # the SeaweedFS name
```

Action names match `^[a-z0-9-]+:[A-Za-z0-9]+$` (no wildcard, so a grant is always a closed list). Scope names follow 0002's name rules. The vocabulary of a resource type (`s3-bucket`) is the `access` section of the capability its provisioners yield (`s3`). A capability without `access` (`postgresql` today) takes no access declarations.

**2. What an identity provider offers.** `identity.grants` (0018) changes from a list of resource types to a map. Each entry may carry `offers`, the default actions that capability can grant on that type:

```yaml
# catalog/capabilities/iam.yaml
identity:
  grants:
    s3-bucket:
      offers: [s3:ListBucket, s3:GetBucketLocation, s3:GetObject, s3:PutObject,
               s3:DeleteObject, s3:AbortMultipartUpload, s3:ListMultipartUploadParts]

# catalog/capabilities/postgresql-roles.yaml
identity:
  grants:
    postgresql-database: {}
```

When the granted type has an `access` vocabulary, `offers` is required (`invalid_offers` at catalog load). `{}` is allowed only for a type without one (`postgresql-database` today). An identity capability also maps each scope it can grant to its own policy target, and the catalog refuses an offered action whose scope the identity does not map (`invalid_offers`). For `iam` the targets are `bucket` → `arn:aws:s3:::<bucket>` and `objects` → `arn:aws:s3:::<bucket>/*`, written in its script (rule 6).

A provide may replace the default for one type with its own `offers`: `provides.<q>.offers.<type>` in a module's catalog entry, `external.provides.<q>.offers.<type>` in a graph. The `seaweedfs` module offers the same list with `s3:ListParts` in place of `s3:ListMultipartUploadParts`. The AWS external and Moto use the default. A provide's `offers` is checked against the vocabulary only, so it may be larger or smaller than the default. For an external it is checked in the resolved graph.

```yaml
# a graph: an S3 compatible external whose IAM speaks the SeaweedFS dialect
other:
  external:
    provides:
      iam:
        capability: iam
        version: "1"
        offers:
          s3-bucket: [s3:ListBucket, s3:GetBucketLocation, s3:GetObject, s3:PutObject, s3:ListParts]
        fields: {...}
```

`contracts/openapi/renderer.yaml` gains `offers` on an external provide (a map from resource type to a list of action names) and `drop` on a binding object (a list of action names).

**3. What a consumer asks for.** On a `uses` entry whose capability has `access`, the section is required:

```yaml
# a consumer's catalog entry
uses:
  files:
    capability: s3
    version: ">=1"
    access:
      required: [s3:ListBucket, s3:GetBucketLocation, s3:GetObject, s3:PutObject]
      optional:
        - s3:DeleteObject
        - s3:AbortMultipartUpload
        - one_of: [s3:ListMultipartUploadParts, s3:ListParts]
```

An item is an action name, or `{one_of: [...]}` with two or more actions in order of preference. `required` holds at least one item. An action appears at most once across all items of a `uses` entry.

**4. What an admin drops.** A binding object (0012) may say `drop: [<action>, ...]`:

```yaml
modules:
  app:
    uses:
      files: {provider: storage.buckets, drop: [s3:DeleteObject]}
```

Each dropped action must appear in an `optional` item of the consumer's declaration, and appear once in `drop`. Dropping an action takes it out of consideration: a plain optional action is not granted, and a `one_of` group falls to its next alternative. To drop a whole group, list every action in it.

**5. The match (renderer, per binding with an identity).** `O` is the offered set: the bound identity provide's `offers` for the resource type, or the identity capability's default when the provide sets none.
- Each required action must be in `O`. Each required group grants its first alternative in `O`. If neither holds, the problem is `access_not_offered`, naming the action or the group.
- Each optional action not dropped is granted when it is in `O`. Each optional group grants its first alternative that is neither dropped nor missing from `O`. When nothing fits, the item is skipped without a problem.
- The result is the binding's grant set, kept in the resolved graph at `modules.<c>.uses.<s>.access`: a map with every scope of the vocabulary as a key and its granted actions as a list sorted in byte order (an empty list when none). It is part of the resolved graph, so the digest covers it. A binding with no login job leaves the key out. The render reads only this key and never computes a grant itself.

A consumer bound to a direct external of `s3` (an existing login, 0011) gets no login job, so its `access` is checked at catalog load but grants nothing. The resolved graph then warns `access_not_enforced` at that binding, because the login you placed may allow far more than the consumer asked for. An exchange between providers (AC-14) changes only the graph. The consumer's entry stays the same, and its grant set follows the new provider's offers.

**6. The login job.** Env `MP_GRANT_<SCOPE>` per scope of the vocabulary (scope in upper case): the granted actions joined by `,`, empty when none. For S3 that's `MP_GRANT_BUCKET` and `MP_GRANT_OBJECTS`. The `iam` script writes the `modular-platform` policy with one statement per non empty scope: `bucket` on `arn:aws:s3:::<MP_RESOURCE_BUCKET>`, `objects` on `arn:aws:s3:::<MP_RESOURCE_BUCKET>/*`. It builds the JSON with `printf` from names the pattern above already fenced. It writes the policy on every run, so a narrower grant set takes effect at the next `up`.

## Problem codes

| Code | Where | When |
|---|---|---|
| `unknown_access` | catalog, `uses.<s>.access` | an action that is not in the vocabulary of the slot's capability |
| `invalid_access` | catalog | `access` on a capability without a vocabulary; a group with fewer than two actions; an action named twice; an empty `required`; a bad action or scope name in a vocabulary |
| `missing_access` | catalog | a `uses` entry on a capability with `access` that declares none |
| `invalid_offers` | catalog (module provide, identity capability), check (external provide) | an offered action outside the vocabulary of that resource type; an action whose scope the identity does not map; `offers` for a type the identity does not grant; no `offers` on a grant whose type has a vocabulary |
| `access_not_offered` | check, `modules.<c>.uses.<s>` | a required action or group that the bound identity provide does not offer |
| `invalid_drop` | check, `modules.<c>.uses.<s>` | a dropped action that is not in an optional item, is named twice, or sits on a slot without `access` |
| `access_not_enforced` (warning) | check, `modules.<c>.uses.<s>` | a slot with `access` bound to a direct external, which has no login job to enforce it |

## Value sourcing

| Action | Value | Source |
|---|---|---|
| catalog load | the vocabulary | `access` of the slot's capability |
| check | offered set `O` | `provides.<q>.offers.<type>` (module catalog, or external in the graph), else the identity capability's `grants.<type>.offers` |
| check | grant set | the match of rule 5 over the consumer's `uses.<s>.access` and the binding's `drop` |
| login job | `MP_GRANT_<SCOPE>` | `modules.<c>.uses.<s>.access` in the resolved graph |
| user policy | statements and resources | `MP_GRANT_*` and `MP_RESOURCE_BUCKET` |

## Critical test scenarios

- Check tables: each problem code above; a required group picks `s3:ListParts` on `seaweedfs` and `s3:ListMultipartUploadParts` on Moto; an optional group with nothing offered is skipped; `drop` of one alternative moves the group to the next; `drop` of a required action is `invalid_drop`; a direct external gives `access_not_enforced` (**AC-16**).
- Golden: one consumer entry bound through `seaweedfs` and through the external `aws` gives the same env names and different `MP_GRANT_OBJECTS` (**AC-16**, **AC-14**).
- e2e on `seaweedfs`: the consumer uploads a 16 MB object with `aws s3 cp` (a multipart upload) and reads it back; with `drop: [s3:DeleteObject]` its delete is refused while its reads still work (**AC-16**, **AC-2**).
- e2e against Moto: the user's `modular-platform` policy holds exactly the granted actions in two statements (**AC-4**, **AC-16**).

## Rationale (short)

The spike showed that SeaweedFS refuses `s3:ListMultipartUploadParts` in a user policy and wants `s3:ListParts`, while AWS and Moto want the AWS name. So one fixed policy can't serve both. You chose to let consumers declare what they need, required, optional and as alternatives, using the raw action names, with providers declaring what they have and the admin able to drop optionals. That gives each consumer least privilege on its own terms instead of one policy for all. The runner up I offered named rights in domain words (`read`, `write`) and mapped them to actions per provider, which keeps server dialects out of consumer entries. You preferred raw actions, which are direct and exactly what the policy holds. The cost is that a consumer author has to know that a dialect exists and cover it with `one_of`. Dropping the action from the fixed policy was the smallest fix, but it would have left no way for a consumer to ask for more or less.
