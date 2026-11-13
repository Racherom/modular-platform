# Modular Platform

Builds a microservice stack from exchangeable modules. Each module wraps an existing open source
container image and declares the capabilities it provides and uses; the checks make sure every
need has a provider, and the renderer turns the stack into a zip you run with `docker compose`.

This is an early scaffold: the formats are provisional (`modular-platform/v0alpha1`). The
architecture lives in [docs/specs/0001-platform-stack-architecture](docs/specs/0001-platform-stack-architecture/index.md).

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
GitHub at `MP_SOURCE_REF` (also in `versions.env`, `main` by default) instead, which takes a few
minutes. To run the images you built from your checkout:

```sh
task images
MP_VERSION=dev task online:up
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

## Releases

release-please opens the release pull request, but it stays off until a release is near. While
the repository variable `RELEASES_ENABLED` is unset, every push to `main` passes the Release
workflow with its jobs skipped. When you want the first release, we recommend these steps, in
this order:

1. Add the repository secret `RELEASE_PLEASE_TOKEN`: a fine grained token, or a GitHub App
   token, with contents and pull request write access. It is required, there is no fallback to
   the default `GITHUB_TOKEN` (a pull request that token opens starts no CI).
2. Set the repository variable `RELEASES_ENABLED` to `true`, then push to `main` or rerun the
   latest Release run. The variable alone starts nothing.

Leave the repository setting *Allow GitHub Actions to create and approve pull requests* off.
Approving a pull request stays with a person. If the variable is `true` and the secret is
missing, the step *Release token is set* fails and names the secret.

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
