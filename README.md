# Modular Platform

Builds a microservice stack from exchangeable modules. Each module wraps an existing open source
container image and declares the capabilities it provides and uses; the planner checks that every
need has a provider and renders the stack as a zip you run with `docker compose`.

This is an early scaffold: the formats are provisional (`modular-platform/v0alpha1`). The
architecture lives in [docs/specs/0001-platform-stack-architecture](docs/specs/0001-platform-stack-architecture/index.md).

## Layout

| Path | What |
|---|---|
| `contracts/openapi/planner.yaml` | The planner API, the source of the Go and TypeScript types |
| `libs/` | Go: `contract` (formats, catalog rules), `planner`, `render`, `target`, `platform` (service kit) |
| `services/planner/` | The planner service, public or internal mode |
| `catalog/` | The built in catalog, embedded into the planner |
| `web/` | The editor (React, Vite), and its `web` image |
| `samples/` | The sample graph used by the editor and the smoke job |
| `deploy/online/` | The online tool: ingress, web and planner on one origin |
| `deploy/managed/` | The managed platform's own graph, which `task managed:render` renders into the release folder, and its dev overlays |

## Develop

You need [Task](https://taskfile.dev), Go 1.26 or newer, Node 26 with pnpm 12, and Docker with
the Compose plugin.

```sh
task generate   # Go and TypeScript code from the contract (committed)
task build      # Go, the editor, and the web and planner images
task test
task lint
task dev        # the online tool on https://platform.localhost, editor with hot reload
task smoke      # the CI smoke job: sample zip end to end, and the editor under /platform
```

`task dev` and `task smoke` bind ports 80 and 443 and use Caddy's internal certificate
authority, so your browser warns about the certificate on first visit.

`task online:up` pulls the release images named by `MP_VERSION` in `deploy/online/versions.env`.
Until the first release exists (it reads `0.0.0`), run the images you built yourself instead:

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

No release exists yet (the version still reads `0.0.0`), so for now you can start the platform from
this checkout instead.

### From this checkout

```sh
task dev:managed
```

This builds the `api`, `planner` and `deployer-docker` images from your checkout, starts the
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

release-please opens the release pull request. A pull request opened with the default
`GITHUB_TOKEN` starts no other workflow, so CI would not check it. Add a repository secret
`RELEASE_PLEASE_TOKEN` (a fine grained token, or a GitHub App token, with contents and pull
request write access) and the release workflow uses it, so CI runs on the release pull request too.

## License

[Apache 2.0](LICENSE)
