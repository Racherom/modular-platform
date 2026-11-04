# deploy/online

## Overview

The online tool (static mode) as a Compose stack on your own host: `ingress` (Caddy) routes `<base>/planner/*` to `planner` and everything else under `<base>/` to `web`, all on one origin, so there is no CORS. It is the same shape as the managed platform (spec 0001, "Online tool").

## Key files

| File | Owns |
|---|---|
| `compose.yaml` | The three services, hardened (read only root, `cap_drop: [ALL]`, `no-new-privileges`); only the ingress publishes 80 and 443 |
| `compose.dev.yaml` | Overlay for `task dev`: the ingress points at the Vite dev server on the host, the planner builds from this checkout, the web image is not started |
| `compose.subpath.yaml` | Overlay that serves the same images under `/platform` (the sub path smoke test) |
| `Caddyfile` | Routing, the planner prefix strip, shared security headers, `tls-acme` or `tls-internal` by `PLATFORM_TLS` |
| `versions.env` | Exact image tags for Caddy and the platform's own images |
| `.env.example` | Host settings, copied to `.env` by `task online:env` (no secrets) |

## Commands

```bash
task online:up     # run from the images in versions.env (MP_VERSION=dev for your own task images)
task online:down   # stop it
```

## Conventions

- Run it through the Taskfile, not plain `docker compose`: the tasks pass both `--env-file versions.env` and `--env-file .env`.
- Image tags are exact, never `latest`. `MP_VERSION` carries the `# x-release-please-version` marker; release-please bumps that line, so keep the marker on it.
- The planner builds no URLs: the ingress strips `/planner`, and the web module reads its base path and API root from env at serve time.
- Keep the hardening on every service you add here. Caddy keeps `NET_BIND_SERVICE`, because its binary holds that capability as a file capability.

## Gotchas

- `.env` is local and ignored by git; a new setting goes into `.env.example` and the `environment:` block of the service, with a `:?` guard when it is required.
- `task dev` binds the real ports 80 and 443 on your machine.

## Related specs

- [0001 Platform stack and architecture](../../docs/specs/0001-platform-stack-architecture/index.md), the online tool and base path rows

_Drafted by /audit from the repo, worth a quick human pass. Edit freely: once a line stops matching this draft, later runs treat it as curated and will flag rather than overwrite it._
