# 0022. Linux capabilities a deployed module may add

**Date**: 2026-11-05
**Parent**: [0011. Capability and module contract](index.md)
**Extends**: [0013](0013-module-manifest-v1.md) (*Hardening*, catalog rules, *Host access*) and [0007](../0006-managed-mode-control-plane/0007-deploy-target-deployer.md) (the guard)

## Summary

A Linux capability is one slice of root's power, for example "may bind a port below 1024". Every rendered service drops all of them (`cap_drop: [ALL]`), and a module entry may add some back with `hardening.cap_add`. This spec decides that a module in a deployed graph may add back exactly one, `NET_BIND_SERVICE`. That is enough for Caddy and step-ca, whose binaries need it to start, and it cannot reach the host. Any other name still works in a zip you download and run yourself. A deployed graph that asks for one gets the `host_access` warning at check time and is refused at managed render and in the guard. The guard also starts requiring `cap_drop: [ALL]` and `no-new-privileges` on every deployed service, so a Compose file that leaves them out can never bring back Docker's default Linux capabilities.

Throughout this spec, "Linux capability" means the kernel's privilege slice, and "capability" alone keeps its domain meaning (what a module provides or uses). Go names keep them apart too: `LinuxCap`, never `Cap` or `Capability`.

## The rule

- **Deployable set**: `NET_BIND_SERVICE`, nothing else. Widening it is a change to this spec.
- **Static and platform flavors** copy every valid name as written (0013, unchanged). The static zip runs on your own host. The platform flavor renders release reviewed entries, and today none of them adds more than `NET_BIND_SERVICE` except `deployer-docker`, which adds `CHOWN`, `DAC_OVERRIDE` and `FOWNER` (0015, *Milestone 4 build findings*).
- **Managed flavor** refuses an instance whose entry adds any name outside the deployable set, the same way it refuses `publish` and `host_mounts` (AC-11).
- **Every deployed service** (long running services, provision and revoke jobs, the `login` job of 0020, the wait anchor `mp_jobs`) must carry `cap_drop: [ALL]` and `security_opt: [no-new-privileges:true]`. The managed render already emits both on every service, because neither is an opt out in 0013.

## Catalog check (`libs/contract/linuxcap.go`, every flavor)

New catalog rule code `invalid_hardening` (`CodeInvalidHardening` in `catalog.go`), at `hardening.cap_add.<n>`, checked in `module.go` beside the `tmpfs` checks:

| Case | Message |
|---|---|
| Not one of the 41 kernel names, written upper case without the `CAP_` prefix | `"%s" is not a Linux capability name` and, when it matches a known name ignoring case or a `CAP_` prefix, `; write %s` |
| `ALL` | `ALL is not allowed here; name each Linux capability` |
| A name listed twice | `%s is already listed at hardening.cap_add.%d` |

An empty list is fine (it adds nothing). The kernel names are the fixed list `CHOWN` through `CHECKPOINT_RESTORE` (bits 0 to 40), kept in `linuxcap.go` and updated by hand when a kernel adds one. 0013's catalog rules list gains `invalid_hardening`.

`libs/contract` exports the single source the other three places read:
- `IsLinuxCap(name string) bool`: the name is one of the 41.
- `DeployableLinuxCap(name string) bool`: the name is `NET_BIND_SERVICE`.
- `(Module) UndeployableLinuxCaps() []string`: every `cap_add` item of the entry for which `DeployableLinuxCap` is false, in entry order, invalid names included (the catalog refuses those first in practice); nil when there are none.

Contract first: the `Hardening.cap_add` description in `contracts/openapi/renderer.yaml` says "Linux capability names, upper case without `CAP_` (spec 0022); a deployed graph may add only `NET_BIND_SERVICE`", then `task generate`. No schema shape changes.

## Check warning and managed refusal (`libs/check/install.go`, `libs/render/managed.go`)

Both keep code `host_access`, path `modules.<i>`, and one problem per instance. Each message names every reason the instance has, in this order: `publishes host ports`, `mounts host paths`, `adds the Linux capability A` (or `adds the Linux capabilities A and B`, or `A, B and C`, from `UndeployableLinuxCaps`). Two reasons are joined by ` and `, three by `, ` and then ` and `.

- Check warning: `Instance %s <reasons>, so it reaches the host it runs on; a deployed graph refuses this.`
- Managed refusal (422): `Instance %s <reasons>, which a deployed graph never gets; download the zip to run it.`

For example: `Instance edge publishes host ports and adds the Linux capability SYS_ADMIN, so it reaches the host it runs on; a deployed graph refuses this.` An instance that adds only `NET_BIND_SERVICE` gets no problem. External instances are skipped, as today.

Tests that compare `host_access` message text move with the new wording: `libs/check/platform_test.go` and the `host access` cases in `libs/render/render_test.go` (they compare code and path today; any message assertion changes with them).

## Guard (`render.Guard`, 0007)

The loaded model is not normalized, so a list is `[]any` and a key with no value is `nil`.

- `cap_add`: a list of strings, else `host_access` at `services.<i>.cap_add` (this covers `cap_add:` with no value). Each item that is not deployable gives `host_access` at `services.<i>.cap_add.<n>`. The compare is exact, so `net_bind_service`, `CAP_NET_BIND_SERVICE` and `ALL` all fail.
- `cap_drop`: required on every service, equal to `[ALL]`. A missing key gives `host_access` at `services.<i>.cap_drop`. Any other value is refused as today.
- `security_opt`: required on every service, equal to `[no-new-privileges:true]`. A missing key gives `host_access` at `services.<i>.security_opt`. Any other value (`seccomp=unconfined`, `apparmor=unconfined`) is refused as today.
- `privileged`, `profiles` (until 0020 adds it with the `login` job), `devices`, `sysctls`, `x-*` and every other key stay refused by the default branch. A `profiles` service that 0020 admits gets the same two required keys.

Why other bypasses stay closed (no change, recorded here): the deployer runs compose with the fixed `composeEnv`, so no `COMPOSE_FILE` or `COMPOSE_PROFILES` from its own process reaches it; it passes `--file` explicitly; and the guard refuses any `COMPOSE_*` or `DOCKER_*` name in `.env` and `deploy.env`.

## Value sourcing

| Action | Value | Source |
|---|---|---|
| Catalog check | the known Linux capability names | `libs/contract`'s fixed list of the 41 kernel names |
| Catalog check | the suggested spelling | the known name that matches ignoring case or a `CAP_` prefix |
| Check warning, managed refusal | the names a deployed graph refuses | `Module.UndeployableLinuxCaps()` of the instance's entry |
| Check warning, managed refusal | the other reasons | the entry's `publish` and `host_mounts` (unchanged) |
| Guard | whether a `cap_add` item may pass | `contract.DeployableLinuxCap` (decided here: `NET_BIND_SERVICE`) |
| Guard | whether a service is missing a required key | the service's keys in the loaded model |
| e2e | the expected `CapEff` | `0000000000000400` (bit 10, `NET_BIND_SERVICE`) for the root fixture service adding it; `NoNewPrivs: 1` |

## Critical test scenarios

- Catalog fixtures under `libs/contract/testdata/`, one each (AC-2): `cap_add: [net_bind_service]` (suggests `NET_BIND_SERVICE`), `[CAP_NET_BIND_SERVICE]`, `[ALL]`, `[NET_BIND_SERVICE, NET_BIND_SERVICE]`, `[NOT_A_CAP]`, all `invalid_hardening` at the item's path.
- Check: an instance adding `SYS_ADMIN` gets warning `host_access` with the message above; one adding only `NET_BIND_SERVICE` gets nothing; one with `publish` and `SYS_ADMIN` gets one problem naming both. The platform graph's warnings stay exactly `deployer-docker` and `ingress` (AC-11).
- Managed render: the `SYS_ADMIN` graph answers 422 `host_access` and renders nothing; the bindings graph (`notifier`: `NET_BIND_SERVICE`) renders (AC-11).
- Guard, in `libs/render/guard_test.go` (AC-21):
  - `TestGuardAcceptsEveryRender` passes with no exception left (the `services.notifier.cap_add` entry goes away), which also shows every managed render emits both required keys.
  - `[NET_BIND_SERVICE]` passes. `[SYS_ADMIN]`, `[net_bind_service]`, `[CAP_NET_BIND_SERVICE]` and `[ALL]` fail at `services.app.cap_add.0`; `[NET_BIND_SERVICE, SYS_ADMIN]` fails at `.1`; a string and a null fail at `services.app.cap_add`.
  - A service without `cap_drop` fails at `services.app.cap_drop`, one without `security_opt` at `services.app.security_opt`, including a job and `mp_jobs`; `cap_drop: [NET_RAW]` and `security_opt: [seccomp=unconfined]` fail.
  - `privileged: true` and `profiles: [x]` still fail.
- e2e (`e2e` tag, Docker, new `TestLinuxCapsEndToEnd` in `libs/render/linuxcap_e2e_test.go`): a one module graph from a fixture catalog under `libs/render/testdata/linuxcap/` (a pinned `busybox` tag running `sleep`, `hardening.user: root`, `cap_add: [NET_BIND_SERVICE]`) is rendered with `Managed`, passes `Guard`, and comes up with `docker compose up -d --wait`. `/proc/1/status` in it shows `CapEff: 0000000000000400` and `NoNewPrivs: 1`, so the one name the guard admits really grants only bit 10 (AC-21).
- `verify.md`: the guard step that names the `services.notifier.cap_add` exception, the step that says the guard "still refuses `cap_add`", and the `Open` note on `cap_add` are replaced by steps for the guard tests above and the `CapEff` e2e.

## Rationale (short)

- **Only `NET_BIND_SERVICE`**: every entry that adds anything needs exactly this one, because the Caddy and step-ca binaries carry it as a file capability and fail at exec without it (0006 rationale). It grants nothing beyond low ports inside the container's own network namespace, and publishing to the host stays limited by `ports` and the reserved ports. Runner up: a reviewed list with `CHOWN`, `SETUID`, `SETGID`, `DAC_OVERRIDE` and `FOWNER`, for images that start as root and step down. Rejected for now: no deployable entry needs them, and they make a root container much stronger. (Since milestone 4 the platform's own `deployer-docker` adds `CHOWN`, `DAC_OVERRIDE` and `FOWNER`, 0015; it is rendered by the platform flavor, never deployed, so this rule stands.) An operator setting was rejected because the platform's promise would then depend on each install's config.
- **Same path as `publish`**: the editor and the deploy flow already handle `host_access`. A warning at check time tells you before you deploy, and the zip keeps working for any name. Refusing at catalog load was rejected because it would block static zip users on their own host.
- **One spelling**: the guard compares exact strings, so it never has to know how Docker normalizes names. Accepting every spelling was rejected because it is a second form to handle in three places.
- **Required keys, not an override file**: an allow list for `cap_add` means nothing if a service can leave out `cap_drop` and get Docker's default set back. First you chose to keep the keys optional and have the deployer merge its own `deploy.compose.yml` that drops everything. The cross check showed that the guard already stops a caller who omits them, before anything runs, so the override only guarded against a future guard regression. It cost a new file, a snapshot change, a `Compose.Run` change, and trust in Compose's merge rules in the CLI the deployer image pins, which CI does not test. You then chose to require the keys. Runner up: that override file.

## Consequences

- A deployed graph can now run Caddy, step-ca or any image whose binary carries `NET_BIND_SERVICE`. The `notifier` fixture deploys.
- An image that needs any other Linux capability runs only from the zip. Its author learns that at check time, not at deploy.
- The guard's allow list grows a "required" notion for the first time. A future render that drops one of the two keys must change this spec and the guard in the same change.
- The `host_access` messages change wording for every case, so tests that compare message text move with them.
- A root user (`user: "0:0"`) under `cap_drop: [ALL]` has no DAC override (it cannot bypass file permissions) and no other Linux capability beyond what `cap_add` admits, so root in a deployed container stays much weaker than root on the host. It still owns files as host uid 0 in its `data/` folders and gains more from a kernel bug.

## Out of scope

- `user: "0:0"` (`hardening.user: root`) and `writable_root` on deployed graphs: the guard still accepts both. That is a separate decision (umbrella *Follow-up*).
