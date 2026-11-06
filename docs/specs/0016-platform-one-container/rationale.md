# 0016. Start the platform from one container: rationale

Decision record for [index.md](index.md). `/develop` reads `index.md`; this file explains why.

## Context

> ⚠️ Premise note: Compose's own `docker compose up -d` already keeps matching containers, recreates changed ones and creates missing ones, so most of #32's done line holds without any new code. The value of this feature is elsewhere: one entry point that #23, #25, #19 and #33 can share, and secrets that the platform creates itself instead of a shell script. That value holds only while the keeper stays a thin layer over Compose. If it grows its own idea of drift or its own container model, it competes with the standard tool you can always fall back to. The design keeps Compose's rule for the platform project for this reason.

After #3's milestone 4 (spec 0015) the managed platform is a rendered folder: a Compose file, config templates and a `setup.sh` that asks the install answers, creates every secret, runs the setup steps in the modules' images, and leaves you to run `docker compose up -d`. Every later change to the platform itself (replacing a module at install, #23; upgrading, #25; live changes, #19; a cluster, #33) needs something that takes the platform's files and brings the running containers in line with them. Today that something is you, typing Compose commands.

The forces: the only service with the Docker socket is the deployer, which is root equivalent, so anything added there is host level risk (0007). Secrets made by a shell script run as your user end up owned by your user in the folder you unzipped, readable by anything that runs as that account. The platform must never touch deployed stacks, which rely on Docker's restart policy and their own Compose projects. Hosts reboot and Docker restarts; the platform must come back the same way every time, with no duplicates and no lost volumes. A small team operates and tests all of this in CI.

On a managed network, you said, all secrets are handled internally: `setup.sh` should create nothing, only place the starting files and start the first container, which can then update the configuration and starts itself again with the host.

## Options considered

### Option 1: The deployer as keeper, started from a seed, with a self handover

`setup.sh` copies the rendered files to a root only state folder and creates one seed container with `docker run` (socket, state folder, autostart). The seed runs the keeper's start pipeline: lock, prepare (secrets, setup steps, folders), reconcile the platform project with Compose, then self check against `keeper.yml`, a separate Compose project for the deployer alone. When its own container does not match, it starts a successor, waits for it to be healthy and removes itself. The serving keeper runs the same pipeline at every start.

**Pros**:
- One container to start, exactly as you asked, and it starts again with the host by its restart policy.
- The deployer can never stop itself through Compose, since it is not in the platform project; replacing itself is an explicit, health gated handover.
- Secrets and setup steps move into the service that already creates deployed stacks' secrets (0007).
- The handover is what #25 needs for an upgrade.

**Cons**:
- The most code of all options, in the root equivalent service: a lock, a step runner, a hash, a handover.
- A second rendered Compose file and a new `setup.json` format.
- `setup.sh` needs root.

### Option 2: A Compose managed deployer that reconciles the rest

The deployer stays a service of the platform project. You start it with `docker compose up -d --no-deps deployer-docker`; at each start it runs `docker compose up -d --no-deps` for every other service, never naming itself.

**Pros**:
- No new rendered file; the deployer is defined like every other service, and Compose recreates it when its own definition changes at your next start command.
- Least code: no handover, no seed.

**Cons**:
- Starting still needs Compose and the right flags; `docker compose up -d` without `--no-deps` from anyone would recreate the deployer under its own feet.
- `api` depends on the deployer's health, so `--no-deps` drops those edges by accident rather than by design.
- `setup.sh` still creates the secrets, which you ruled out.

### Option 3: A short lived boot container

A one shot run of the deployer image (`deployer-docker platform up`) runs Compose for every service, the deployer included, then exits.

**Pros**:
- It never stops itself: it is not part of what it reconciles.
- The long running deployer stays as simple as today.

**Cons**:
- A host reboot runs no reconcile; restart policies bring containers back without it, so a deleted container stays deleted until you run the boot again.
- Two entry points (the boot run and the deployer) and two containers holding the socket at times.

### Option 4: A dedicated boot module

A new small image whose only job is the reconcile, separate from the deployer.

**Pros**:
- Clean separation of concerns; the deploy target stays focused on stacks.

**Cons**:
- One more image to build, sign and release, and a second permanent root equivalent container.
- It duplicates the deployer's Compose and secret handling.

## Rationale

Option 1 is the only one that meets all three of your constraints at once: one container to start, no Compose needed to start it, and no secret created by a script. Option 2 is the lightest code but keeps secrets in `setup.sh` and leans on `--no-deps` to keep the deployer from recreating itself, a guard that one hand typed command breaks. Option 3 loses the reconcile at host reboot, so the scope's "recreated at the next start" holds only when someone remembers to run it. Option 4 doubles the root equivalent surface for a job the deployer already half does.

The extra code of Option 1 is bounded on purpose. The platform project uses Compose's own rule, unchanged, so a plain `docker compose up -d` in the state folder still gives the same containers. The only new container logic is the deployer replacing itself, and it borrows Compose's idea of "matches" (a hash of the definition plus the image ID) and Compose's own `up` to create the successor, in a project named by that hash, so two definitions can run side by side for the handover.

The seed exists because the deployer's real definition needs things only the first start creates: its binding token, the CA from `cert-issuer`'s setup step, the `internal` network. A seed with just the socket and the state folder creates them, then hands over to the rendered keeper, which uses the same handover any later definition change uses. One mechanism covers install, a changed keeper definition, and later an upgrade.

The cross check asked about a lighter variant of Option 2: keep the deployer in the platform project and have it start a short lived helper container that runs `docker compose up -d deployer-docker`, so a process that outlives the deployer recreates it. It saves the second project and the hash, but Compose recreates by stopping the old container before the new one runs, so a deployer whose new definition fails stays down with nobody left to undo it. The handover keeps the old keeper until the new one is healthy, and the seed is still needed to create the secrets the deployer's own definition mounts. So Option 1 stays.

### Smaller choices

| Choice | Picked | Why | Runner up |
|---|---|---|---|
| Where the state lives | fixed host folder, root only, `/var/lib/modular-platform` | the host daemon resolves the rendered secret, config and bind paths as they are, deployments stay under it, any backup tool reads it | a Docker named volume (portable, maps to a cluster volume, but every secret and config mount of both renders must become a volume subpath mount) |
| What counts as no longer matching | Compose's rule (config hash, image ID) | your choice; no second notion of drift, and the hand fallback agrees with the keeper | Compose's rule plus a live check of restart policy, networks and pause state |
| Reconcile failure | serve and retry, 1 second doubling to 5 minutes | a registry outage heals itself, and deployed stacks stay visible meanwhile | exit and let Docker restart it (the deployer is down meanwhile) |
| How the deployer gets its own wiring | the render describes it in `keeper.yml`; the deployer creates its own binding tokens and hands them to the containers it deploys, gets its certificate after `cert-issuer` runs, and replaces itself by a second, updated container that takes over once connected | your design; no special config path in the SDK | the deployer joins the network and reads an env file itself at start |
| How `setup.sh` copies | runs as root (`sudo sh setup.sh`) | your choice; no extra subcommand | a `docker run --rm` install subcommand of the keeper image (no sudo) |
| Overlays in dev and CI | in the state folder, named by `COMPOSE_FILE`; keeper image by `MP_KEEPER_IMAGE` | the smoke job tests the real start path | render dev and CI variants of the files |
| Handover with running operations | drain then hand over | your choice; at start nothing runs yet, so it is built when #25 triggers a handover on a serving keeper | stop at once and let `api` treat the new boot id as an unknown outcome |
| First admin | a generated `show: once` password for the bundled Authelia's user `admin`, printed once by `setup.sh` | works end to end now; the web setup feature replaces it | `setup.sh` asks a password and hands it over in a one time file |
| How a module says "show this secret" | generic `show: once` and `format: password` on a generated secret | a replacement identity module (#23) gets the same treatment, and the platform names no image | a special case for Authelia's file |
| Web setup | its own scope feature, designed with #28 | its enrollment seam must work for any sign in method, which #28 decides | an umbrella with this spec |
| Sequencing | after milestone 4, which ships as planned | each merge stays green and smoke proven | fold this into milestone 4 |
| `MP_UID`, `MP_GID` | fixed `65532` | `setup.sh` runs as root, so `id -u` is 0; `65532` is the distroless `nonroot` user | the `SUDO_UID` of the person running it (missing when root logs in directly) |
| Finding itself | the host name (Docker sets it to the short container ID) | no extra setting; the render never sets `hostname` | an env var with the container name, set at create |
| Lock | `flock` on a file in the state folder | every keeper mounts the same host folder on one kernel, so the lock holds across containers and frees itself when a process dies | a Docker label used as a lease (needs expiry handling) |
| Partial writes | write in a scratch folder, move into place with a rename that never replaces | a crash never leaves a cut secret, and a half finished step never swaps a key already in use | write in place and accept the risk |
| Printing the password | a `.shown` marker; print after the keeper is healthy | a rerun after an interrupted install still shows it, and the origin works when you read it | print when the file appears |
| Platform files and the guard | trusted, no `render.Guard` | writing them already takes root; the guard protects the host from `api`'s input, which never reaches this folder | run the guard with a wider allow list (adds code that protects nothing) |

## Spike results

_Task 1 of the build plan writes its findings here: `COMPOSE_FILE` from the project `.env`, a second keeper project beside a running one, removing its own container, and Compose's handling of a changed `configs` file._
