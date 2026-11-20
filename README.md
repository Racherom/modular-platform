# Modular Platform

Builds a microservice stack from exchangeable modules. Each module wraps an existing open source
container image and declares the capabilities it provides and uses; the checks make sure every
need has a provider, and the renderer turns the stack into a zip you run with `docker compose`.

The project is young and the first release is still ahead, so the formats are provisional
(`modular-platform/v0alpha1`) and may change. The architecture lives in [docs/specs/0001-platform-stack-architecture](docs/specs/0001-platform-stack-architecture/index.md).

## Why use it

**What you want from it**

- **You want to find a gap before you run.** Hand wired Compose snippets show a missing database or
  identity provider only at runtime. We want the checks to catch every unmet need first, and to
  suggest a provider for the gap.
- **You want one tool for a small and a big setup.** We want one graph to give you a zip you run
  with plain `docker compose up` (static mode), or a platform that signs you in and deploys to your
  host (managed mode).
- **You want your secrets to stay yours.** We want `setup.sh` to create them on your own host, and
  never to put one in a zip, a graph file or the renderer.
- **You want to keep your choices.** We want every module the platform ships, down to its own
  services, to be only a default you can replace.

**What we want it to be**

- **A platform that is itself a graph.** A third party module has the same standing as a shipped one.
- **One renderer, two places.** A stack checked in the online editor renders the same files as one
  the platform deploys.
- **Capabilities, not image names.** A module declares what it provides and uses, so exchanging a
  provider is a change the checks can follow.
- **Standard protocols only.** REST, OIDC, PostgreSQL and NATS, no house protocol.
- **A public renderer that keeps nothing.** No accounts, no database, no request bodies in logs.
- **A stack editor that fits your site.** It runs on its own or under a path of a website you
  already have.
- **Open source** under Apache 2.0, for self hosters, platform teams and module authors.

Some of this is still ahead: exchanging a provider, conformance checks and live changes to a
running stack are planned, not built. [docs/scope/scope.md](docs/scope/scope.md) lists what is done.

## Layout

| Path | What |
|---|---|
| `contracts/openapi/renderer.yaml` | The renderer API, the source of the Go and TypeScript types |
| `libs/` | Go: `contract` (formats, catalog rules), `check`, `render`, `target`, `platform` (service kit) |
| `services/renderer/` | The renderer service, public or internal mode |
| `catalog/` | The built in catalog, embedded into the renderer |
| `web/` | The editor (React, Vite), and its `web` image |
| `samples/` | The sample graph used by the editor and the smoke job |
| `deploy/online/` | The online editor: ingress, web and renderer on one origin |
| `deploy/managed/` | The managed platform's own graph, which `task managed:render` renders into the release folder, and its dev overlays |

## Develop

You need [Task](https://taskfile.dev), Go 1.26 or newer, Node 26 with pnpm 12, and Docker with
the Compose plugin.

```sh
task generate   # Go and TypeScript code from the contract (committed)
task build      # Go, the editor, and the web and renderer images
task test
task lint
task dev        # the online editor on https://platform.localhost, editor with hot reload
task smoke      # the CI smoke job: sample zip end to end, and the editor under /platform
task skills     # the agent skills from skills-lock.json, for AI coding agents (once per clone)
```

`task dev` and `task smoke` bind ports 80 and 443 and use Caddy's internal certificate
authority, so your browser warns about the certificate on first visit.

`task online:up` pulls the release images named by `MP_VERSION` in `deploy/online/versions.env`.
Until the first release exists (it reads `0.0.0`), it builds the web and renderer images from
GitHub at `MP_SOURCE_REF` (also in `versions.env`, `dev` by default) instead, which takes a few
minutes. To run the images you built from your checkout:

```sh
task images
MP_VERSION=dev task online:up
```

While it reads `0.0.0`, you can also build web and renderer from a checkout, with no push and no
GitHub for the source. The images and the renderer's zips name the checkout's commit when its tree
is clean, and `local` when it has changes:

```sh
MP_SOURCE_PATH=. task online:up
```

## Start a managed platform

In managed mode the platform runs on your own host. You sign in as the admin, plan a stack in the
editor, deploy it to that host, and watch each module's state live. You need a Linux host with
Docker Engine and the Docker Compose plugin.

### From a release

Each release carries `modular-platform-<version>.zip`. To install it, we recommend these steps:

1. Unzip it where the platform should live, for example `/opt/modular-platform`, and open that
   folder.
2. Run `sh setup.sh`. It asks for the domain (it must contain a dot, like `platform.localhost`),
   the origin your browser sees (press Enter for `https://<domain>`), the admin email, the admin
   user name, and the admin password (at least 12 characters). It writes `.env`, every secret in
   `secrets/` and every data folder in `data/`, and never prints a secret.
3. Run `docker compose up -d --wait`.
4. Open the origin and sign in with the admin user name and password.

The platform makes its own certificate authority, so your browser warns about the certificate on
first visit. The `README.md` in the unzipped folder covers the rest: a public certificate,
deploying a stack, changing the admin, backups, and upgrades. Every file in that folder is
rendered from [deploy/managed/graph.yaml](deploy/managed/graph.yaml), the platform's own graph,
with the built in catalog (`task managed:render` writes it into `build/managed/`).

No release exists yet (the version still reads `0.0.0`). A zip made now holds the Dockerfile of
each platform image, and `setup.sh` builds them first from the commit that made the zip (it needs
Docker 23 or newer with BuildKit, and GitHub access, or `MP_SOURCE_PATH` set to a checkout). You
can also start the platform from this checkout instead.

### From this checkout

```sh
task dev:managed
```

This builds the `api`, `renderer` and `deployer-docker` images from your checkout, starts the
platform on `https://platform.localhost`, and serves the editor from the Vite dev server with hot
reload. It binds ports 80 and 443. When the task ends, it stops the platform's containers again.

Each run renders the platform into `build/managed`, copies it over `.dev/managed` (outside git, so
no secret lands in a commit; your `.env`, `secrets/` and `data/` there stay), and runs `setup.sh`
there. The first run prints the admin password once. Sign in as `admin` with that
password. To set it up again with a new password, delete `.dev/managed` and run the task again.

To check the whole install the way CI does, you can build the images tagged `ci` and run the
managed smoke job (it needs Chromium). It installs with `setup.sh`, signs in, deploys the sample,
sees every module running, and removes it with its data:

```sh
IMAGE_TAG=ci task images
task smoke:managed
```

## Branches and releases

Two branches matter. `main` holds only released code, so it always shows the last release. `dev`
holds the work for the next release. Feature branches start from `dev` and merge back into it.

| You want to | Open a pull request | Merge method |
|---|---|---|
| Add a feature or a fix | from `feat/...` (or any short branch) into `dev` | squash |
| Start a release | from `dev` into `main` | merge commit, never squash |
| Finish a release | merge the release pull request release-please opens on `main` | merge commit |
| Fix a released version | from `hotfix/...` (cut from `main`) into `main` | merge commit |
| Carry a release back | merge the `back-merge/<version>` pull request into `dev` | merge commit, never squash |

Two checks guard this. The *Branch rules* check fails a pull request into `main` unless it comes
from `dev`, `hotfix/...` or the release branch, and fails a pull request into `dev` unless its title
is a conventional commit (`feat(web): show a failed check`). A squash merge uses the pull request
title as the commit message, and release-please reads those messages for the changelog, so the
title counts. GitHub rulesets enforce both branches; they are kept in `deploy/github/` and
checked against the job names by a test.

A release takes two merges. Merging `dev` into `main` makes release-please open its release pull
request on `main`. Merging that one tags the release, builds the images and attaches the zip. Then
a job opens the pull request `back-merge/<version>` into `dev`, so `dev` gets the release commit.
Merge it with a merge commit, never a squash, or the next release conflicts. If it does conflict,
merge `dev` into `back-merge/<version>` on your machine and keep the release side for the version
lines (`MP_VERSION`, the four catalog image lines, `web/package.json`, the manifest and
`CHANGELOG.md`).

A fix for a released version goes on a `hotfix/<name>` branch cut from `main`, with conventional
commit messages, into `main`. release-please then opens a patch release pull request, and the
release's back merge brings the fix into `dev`. Merge or close a pending release pull request
before you cut a hotfix, or the hotfix ships the unreleased work that is already on `main`.

## Releases

release-please opens the release pull request, but it stays off until a release is near. While
the repository variable `RELEASES_ENABLED` is unset, every push to `main` passes the Release
workflow with its jobs skipped. When you want the first release, we recommend these steps, in
this order:

1. Install the Renovate GitHub App on the repository. It reads `renovate.json` and keeps the
   image digests of the four catalog Dockerfiles fresh; with that file present it opens no
   onboarding pull request.
2. Check two repository settings: *Allow GitHub Actions to create and approve pull requests*
   is off, and *Release images to GHCR* is not a required check on `main` (remove it if it is
   still there, or every pull request waits for a check that never reports).
3. Add the repository secret `RELEASE_PLEASE_TOKEN`: a fine grained token, or a GitHub App
   token, with contents and pull request write access. It is required, there is no fallback to
   the default `GITHUB_TOKEN` (a pull request that token opens starts no CI).
4. Check that both rulesets from `deploy/github/` are active, that the release token also has
   contents and pull request write access for the back merge job, and that you ran release-please
   in dry run mode on a real `dev` into `main` merge. Expect a long first changelog: the first
   release reads the whole history (manifest `0.0.0`) and the version is 0.1.0; accept that, or set
   `bootstrap-sha` first.
5. Set the repository variable `RELEASES_ENABLED` to `true`, then push to `main` or rerun the
   latest Release run. The variable alone starts nothing.

Approving a pull request stays with a person, so the setting in step 2 stays off. If the
variable is `true` and the secret is missing, the step *Release token is set* fails and names
the secret. You can see that gate work before any of these steps: `task test:integration` runs
`release.yml` in local Docker with `act` (`TestReleaseGateAct`), and checks the Renovate rule
with Renovate's local dry run (`TestRenovateDigests`).

Right after the release pull request merges, the Release workflow builds the four platform
images from the release commit and pushes each as `:<version>`, for amd64 and arm64. For those
few minutes, `main` and the release name images that are still being pushed, so a pull fails
until the push ends. Once all four are in GHCR, the workflow checks them again and attaches the
managed zip. A released image is never pushed again: each image job first checks GHCR and stops
if the tag is there.

If one image job fails, use *Re-run failed jobs*, not a full rerun: the images already pushed
would fail their own check, and a full rerun makes release-please report no new release. If a
push ended half done (the tag is in GHCR but its job failed), check it with
`scripts/release-images-check.sh <version>`; when it is complete, rerun only the managed zip
job, else delete the tag in GHCR and rerun the failed job.

## License

[Apache 2.0](LICENSE)
