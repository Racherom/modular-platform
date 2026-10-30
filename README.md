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

## Releases

release-please opens the release pull request. A pull request opened with the default
`GITHUB_TOKEN` starts no other workflow, so CI would not check it. Add a repository secret
`RELEASE_PLEASE_TOKEN` (a fine grained token, or a GitHub App token, with contents and pull
request write access) and the release workflow uses it, so CI runs on the release pull request too.

## License

[Apache 2.0](LICENSE)
