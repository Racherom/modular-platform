# 0031. Frozen image versions: rationale

## Context

In managed mode a module names its image by tag (`postgres:18.6-alpine3.24`, `ghcr.io/racherom/modular-platform/api:0.0.0`). A tag is a pointer that the publisher can move: a re pushed patch build, or a platform image rebuilt from a new commit. The deployer's apply pipeline (spec 0007) runs `docker compose pull` on every apply, so each apply takes whatever the tag points to at that moment.

The automatic rollback of spec 0006 (AC-14) renders the graph of the last succeeded apply again and sends it like any apply. When the failed apply broke because the tag moved to a broken image, the rollback pulls the same moved tag and fails the same way, then fails its one retry. The stack ends `failed` with no path back except a hand edit on the host. The rollback also needs the registry, so a registry outage during an incident blocks recovery too.

Nothing today records which image an apply actually ran: the `operations` table keeps the graph and the plan digest, which covers catalog entries but not the content behind a tag. "Applying the same graph again" can therefore change what runs without any change you made, and the review cannot show it.

Forces: the deployer trusts no caller (spec 0006, AC-8) and owns the host side files; `libs/render` and `libs/check` must stay pure; the platform's own images are sometimes built on the host and have no registry digest at all (spec 0030); the static zip must keep plain tags so it stays portable; private registries are not supported anywhere yet; and #19 will replace the whole stack apply with a module by module apply soon after this lands.

## Options considered

### Option 1: digests per operation in the api database

The api records the frozen digest of every reference on each apply operation (a new table beside `operations`), resolves new references at review, and passes the lock to the renderer. Rollback reads the digests of the last succeeded operation.

**Pros**:
- The history (#35) can show and diff image versions of every apply directly.
- Restoring any older history entry can run its exact digests.

**Cons**:
- The record of what runs lives away from the host that runs it; a hand change on the host or an api database restore makes them disagree silently.
- The keeper (spec 0016) has no database, so the platform needs a second mechanism anyway.
- Needs a migration and a new table for data the deployer already knows best after `up`.

### Option 2: a lock file per stack, owned by the deployer (chosen)

The deployer writes `images.lock` after each successful apply from what actually started, keeps one previous generation, and serves the lock to the api, which folds it into review, apply and rollback. The renderer takes the lock as input.

**Pros**:
- The lock describes what runs, written by the only service that sees it run; a failed apply cannot touch it, so rollback reads the last good state without any history query.
- The same file format serves the keeper's state folder for the platform.
- No database change; adoption from running containers covers stacks deployed before this feature.

**Cons**:
- Only two generations exist, and the api keeps no copy, so older history entries cannot show or restore their exact digests.
- One more deploy target call on every review, apply and rollback.

### Option 3: both, the api records and the deployer writes a lock

**Pros**:
- Full history in the api and a host side truth.

**Cons**:
- Two copies that can disagree, and every rule (adoption, rotation, remove) has to be kept in line in two places.

### Option 4: the deployer pins at apply time, with no review step

The deployer pulls the tag, records the digest it got, and the review only says "resolved at apply".

**Pros**:
- No resolve endpoint and no change to the review digest.

**Cons**:
- The review no longer shows what will run, and a tag moving between review and apply goes unnoticed; this fails the scope's "taking a newer version is a choice you make, shown before apply".

## Rationale

The bug is a host side fact (which content ran) recorded nowhere, so the fix records it where it happens: the deployer, after `up` passes. Writing the lock only on success makes the rollback rule trivial and robust: the current lock is by construction the last good apply, with no query for "the newest succeeded operation before X" and no chance that a crash mid apply leaves a lock for something that never ran. The engineer chose the deployer's files over my first recommendation (the api database). That choice is sound: it keeps one truth beside the Compose files, and it is the only option the keeper can use unchanged for the platform. The cost is the history gap for #35, which is recorded as a follow up rather than solved by a second copy.

Resolving at review, not at apply, is what makes "shown before apply" true: the review digest covers the lock, so apply builds the same lock again and any difference, whether a moved tag or another apply in between, becomes the existing `graph_changed` answer that the editor already handles. Resolving through the Engine API `DistributionInspect` keeps to the moby client the deployer already has and lets the daemon apply its mirrors and proxy, instead of a second HTTP stack for registries. `compose pull --policy missing` with `reference@digest` makes "no registry needed" a property of the pull step rather than new code; the spike in task 1 checks it before anything builds on it.

Freezing per reference rather than per instance keeps the lock small and the rule obvious (one tag, one digest per stack), and it matches how Compose and Docker store images. The deployer accepts a Compose file with plain tags (the engineer's choice over a hard `image_not_frozen` refusal): the lock then still records the truth from `RepoDigests`, so freezing degrades to "recorded" rather than failing, at the cost that only the api enforces the freeze. Keeping two generations with keep tags gives a rollback its images offline, which is the case the scope names, without letting disk grow with the history.

## Spike results (task 1)

_Filled in by `/develop` when task 1 runs: `--policy missing` behavior offline, `DistributionInspect` digest versus `RepoDigests` for an image built for several platforms, and whether a digest only image with a keep tag survives `docker image prune -f`._
