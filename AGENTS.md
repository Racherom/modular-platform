# Modular Platform

Builds a microservice stack from exchangeable modules. A graph planner checks that every capability a module uses has a provider, then renders the stack as a Compose zip (static mode) or deploys it (managed mode).

## Stack

- **Language / Runtime**: Go 1.26 or newer (`go.work` pins toolchain go1.27.3); TypeScript (strict) on Node 26
- **Framework**: Go standard library `net/http` ServeMux, no framework; React single page app built by Vite (see [web/AGENTS.md](web/AGENTS.md))
- **Key dependencies**: `compose-spec/compose-go` v2 (render), `oapi-codegen` and `openapi-typescript` from one OpenAPI 3.1 contract, `golang.org/x/time/rate`; from managed mode (#16): `pgx` v5 + `sqlc` + `goose`, Authelia (OIDC), Caddy, NATS later
- **Package manager**: Go modules in a `go.work` workspace (`catalog`, `libs`, `services/planner`); pnpm 12 in `web/`; Task for cross language tasks
- **Images and releases**: distroless static nonroot for Go services, multi stage builds from the repo root; release-please
- Source of truth: [docs/specs/0001-platform-stack-architecture](docs/specs/0001-platform-stack-architecture/index.md)

## Build approach

**Tracer Bullet**: each slice runs end to end through catalog, planner, editor and deploy; every later slice thickens one strand of a thread that already works.

## Commands

```bash
task generate   # Go and TS code from contracts/openapi (output committed; CI fails on a diff)
task dev        # online tool on https://platform.localhost, editor with hot reload (binds 80 and 443)
task build      # Go, the editor, and the web and planner images
task test       # go test -race and vitest (installs web deps itself)
task lint       # gofmt, go vet, tsc (golangci-lint and Biome arrive with /develop tooling)
task smoke      # sample zip end to end plus the editor under /platform (needs Chromium)
```

## Specs

Stored in `docs/specs/`, one folder per spec: `docs/specs/NNNN-title/index.md`, child specs beside it. The 12 architecture principles in spec 0001 bind every change.

## Rules

- **Domain Driven Design, one package per bounded context**: `libs/contract` (module, capability, graph and catalog formats; the shared kernel), `libs/planner` (match uses to provides), `libs/render` (plan to Compose project and zip), `libs/target` (deploy targets), and each `services/<name>`. A context owns its model; others reach it only through its exported API. Follow the existing folder layout; service code lives in `services/<name>/internal/`.
- **Ubiquitous language**: name things with the domain's words: module, capability, provides, uses, binding, graph, catalog, plan, render, target, fragment, provisioner. No synonyms such as plugin or dependency for these.
- **Value objects and aggregates**: domain types are validated when built and then treated as immutable; the graph is the aggregate root, and other contexts refer to its modules and bindings by name. Domain events (once NATS lands) are past tense facts, like `StackDeployed`.
- **Pure domain, I/O at the edges**: `libs/planner` and `libs/render` never touch network, disk, clock or env. Services are application services: parse input, call the domain, map to the contract; no domain logic in handlers. Persistence sits behind interfaces the domain defines; Docker, OIDC and compose-go types stay behind an adapter at the edge.
- **Contract first**: API changes start in `contracts/openapi/*.yaml`, then `task generate`. Never hand edit `*.gen.go`, `*.gen.ts` or `routeTree.gen.ts`. Wire errors use the contract's `Problem` and `ProblemList` schemas.
- **Errors**: return errors, wrap with `fmt.Errorf("...: %w", err)`, use sentinel or typed errors where callers branch; no panics for expected failures.
- **Config**: every `MP_*` setting is parsed and checked once at startup in `internal/config`; a bad value stops the service. Public origin from config only (principle 12).
- **Logs**: `log/slog` JSON only, never `fmt.Print` or `log.Printf` in services; never log request bodies or secret values.
- **Naming and docs**: Go idioms (short receivers, MixedCaps, no stutter like `render.RenderZip`); every exported Go identifier has a doc comment.
- **Tests**: unit plus integration. Go table tests beside the code, `httptest` for handlers, golden files in `testdata/` for render output, always `-race`. Editor rules in [web/AGENTS.md](web/AGENTS.md).
- **Commits**: conventional commits (`feat:`, `fix:`, `chore:`); release-please builds versions and changelogs from them.

## Tooling

Chosen here, installed by `/develop tooling`:

- **Lint and format**: golangci-lint v2 (gofumpt formatter) for Go; Biome for `web/` (lint and format, React hooks rules). Both run through `task lint`.
- **Pre commit**: lefthook runs format, lint and typecheck on staged files; tests stay in CI.
- **CI**: `.github/workflows/ci.yml` already runs generate diff, lint, test, build; new linters reach it through `task lint`.

## Git

- integration: on
- branch prefix: feat/
- commit: per-milestone

## Agent skills

- [golang-project-layout](.agents/skills/golang-project-layout/): `samber/cc-skills-golang`, module and package layout, `go.work`
- [golang-code-style](.agents/skills/golang-code-style/): `samber/cc-skills-golang`, Go style
- [golang-naming](.agents/skills/golang-naming/): `samber/cc-skills-golang`, Go names
- [golang-error-handling](.agents/skills/golang-error-handling/): `samber/cc-skills-golang`, wrapping and error types
- [golang-context](.agents/skills/golang-context/): `samber/cc-skills-golang`, `context.Context` use
- [golang-concurrency](.agents/skills/golang-concurrency/): `samber/cc-skills-golang`, goroutines and sync
- [golang-structs-interfaces](.agents/skills/golang-structs-interfaces/): `samber/cc-skills-golang`, types and interfaces
- [golang-database](.agents/skills/golang-database/): `samber/cc-skills-golang`, pgx and sqlc (managed mode)
- [golang-security](.agents/skills/golang-security/): `samber/cc-skills-golang`, secure Go
- [golang-observability](.agents/skills/golang-observability/): `samber/cc-skills-golang`, slog logs and health
- [golang-lint](.agents/skills/golang-lint/): `samber/cc-skills-golang`, golangci-lint setup
- [golang-testing](.agents/skills/golang-testing/): `samber/cc-skills-golang`, Go tests
- [golang-dependency-management](.agents/skills/golang-dependency-management/): `samber/cc-skills-golang`, `go.mod` and upgrades
- [golang-continuous-integration](.agents/skills/golang-continuous-integration/): `samber/cc-skills-golang`, CI for Go (releases use release-please, not GoReleaser)
- [golang-cli](.agents/skills/golang-cli/): `samber/cc-skills-golang`, unused until #12
- [openapi-spec-generation](.agents/skills/openapi-spec-generation/): `wshobson/agents`, OpenAPI contracts
- [github-actions-templates](.agents/skills/github-actions-templates/): `wshobson/agents`, workflow files
- [multi-stage-dockerfile](.agents/skills/multi-stage-dockerfile/): `github/awesome-copilot`, image builds
- [supabase-postgres-best-practices](.agents/skills/supabase-postgres-best-practices/): `supabase/agent-skills`, Postgres schema and SQL (managed mode)

## Context files

- [web/AGENTS.md](web/AGENTS.md) (the editor: React, Vite, base path handling, its tests and skills)

_Drafted by /audit from the repo, worth a quick human pass. Edit freely: once a line stops matching this draft, later runs treat it as curated and will flag rather than overwrite it._
