# libs/render

## Overview

Turns a resolved graph into the files of a runnable Compose project: `docker-compose.yml`, `graph.yaml`, `setup.sh`, `.env`, `README.md`, and a `config/<instance>.env` per module built on the sdk. The zip download and the managed deploy both go through it, so they cannot drift (spec 0002 render rules, spec 0005 render additions). Pure domain: no network, disk, clock or env.

## Key files

| File | Owns |
|---|---|
| `render.go` | `Render`, the Compose project built with compose-go types, the zip with fixed timestamps |
| `sdk.go` | The additions for `runtime: sdk` modules: bootstrap env, network alias, `${MP_UID}:${MP_GID}` user, the module env file mount, generated secrets |
| `bind.go` | Bindings, networks, jobs (provision, revoke) and the wait anchor labels (spec 0013, 0014) |
| `install.go` | Install time: secrets, asked values, setup steps and the `setup.sh` assembly |
| `template.go` | A module's `*.tmpl` files rendered into `config/<instance>/` after its bindings |
| `platform.go` | `Platform`, the platform render flavor into the folder the managed zip ships (spec 0015); run by `catalog/cmd/render-platform` |
| `testdata/<case>/golden/`, `testdata/platform/` | One golden folder per render case, and the rendered platform folder |
| `templates/*.tmpl` | `setup.sh`, `.env` and `README.md`, embedded with `text/template` |
| `testdata/whoami-postgres/`, `testdata/sdk-pair/golden/` | Golden files: every rendered file, byte for byte |
| `internal/sdkpair/` | Two tiny sdk modules (provider and consumer) that only the e2e test runs |

## Commands

```bash
go test -race ./libs/render/ -update    # rewrite the golden files after an intended change
go test -race -tags e2e ./libs/render/  # build the sdkpair images, render, compose up (needs Docker)
```

## Conventions

- Output must be deterministic: the same resolved graph gives the same bytes (files, instances and secrets sorted by path, the 1980 zip time). A golden diff you did not intend is a bug, not a file to update.
- Review the golden diff in your commit like code; it is the render's contract.
- A check result that is not ok is never rendered (`ErrGraphHasErrors`).

## Gotchas

- `internal/sdkpair/{consumer,provider}/config.gen.go` come from `sdkgen` over the catalog entries in `testdata/sdk-pair/catalog/`; change the entry and run `task generate`, never the generated file.
- The sdkpair `Dockerfile` builds from the repo root so `go.work` applies; the e2e test does that for you.
- The `-update` flag is shared with a library the e2e build links in, so `render_test.go` defines it only when it is not defined yet.

## Related specs

- [0002 Provisional graph format](../../docs/specs/0001-platform-stack-architecture/0002-provisional-graph-format.md), the render rules
- [0005 Module SDK](../../docs/specs/0005-module-sdk/index.md), "Render additions"

_Drafted by /audit from the repo, worth a quick human pass. Edit freely: once a line stops matching this draft, later runs treat it as curated and will flag rather than overwrite it._
