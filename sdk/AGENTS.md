# sdk

The module SDK: a Go kit for building a module container (spec [0005](../docs/specs/0005-module-sdk/index.md)). A module declares its config values and bindings once, in its catalog entry; `sdkgen` turns that into typed getters; `sdk.Run` loads the values, refuses a bad start, checks binding tokens, serves health and readiness, and shuts down gracefully. Its own Go module, `github.com/racherom/modular-platform/sdk`, in `go.work`.

## Layout

- [sdk.go](sdk.go), [values.go](values.go): `Run`, the declaration types, `Secret`, and `Value` / `Lookup`, which only the generated getters call
- [manifest/](manifest/): the catalog entry fields the SDK reads (`runtime`, `config`, `uses.<cap>.client`, `provides.<cap>.auth`) and their rules; also called by `libs/contract`
- [envfile/](envfile/): the strict env file parser (no `export`, no `${VAR}`, no multi line values, no inline comments)
- [internal/load/](internal/load/): source precedence, parsing by type, the problem codes and lines
- [httpkit/](httpkit/): the old `libs/platform` service kit (logger, problems, health, access log, rate limit, client IP, base path, Serve)
- [auth/](auth/): the per consumer bearer check against a clients directory
- [caller/](caller/): mints and checks the `MP-Caller` token (which user a call between modules is for); only api mints
- [httpclient/](httpclient/): the http binding client, sends the binding token as `Authorization: Bearer`, never retries
- [pg/](pg/): the postgresql binding client, a pgx pool from the binding's url, user, password file and `ca_file`
- [s3/](s3/): the s3 binding client, MinIO with path style addressing and the binding's keys and `ca_file`
- [tlsacme/](tlsacme/): TLS mode, orders and renews the module's certificate through ACME and checks it against `MP_CA_FILE`
- [sdktest/](sdktest/): fakes of the pluggable sources and helpers for tests of modules on the sdk
- [cmd/sdkgen/](cmd/sdkgen/): the generator
- [internal/testmodule/](internal/testmodule/): a module the SDK's own tests run, one value of every type in `testmodule.yaml`

## Conventions

- `libs` depends on `sdk` (for the `manifest` rules), never the other way round. `manifest` imports only the standard library and `sigs.k8s.io/yaml`.
- Only `sdk/` reads env vars or `*_FILE` paths. A module adds a value to the `config:` section of `catalog/<module>.yaml`, never parsing code of its own. Bootstrap names (`MP_INSTANCE`, `MP_ENV_FILE`, `MP_ACME_URL`, `MP_CA_FILE`, the caller key files) are read from the process env only and are never declared.
- A module wires the generator with `//go:generate go tool sdkgen -entry <path to its catalog entry> -package config -out config.gen.go` in `internal/config/generate.go`, and its module lists `tool github.com/racherom/modular-platform/sdk/cmd/sdkgen`. `task generate` runs it; never hand edit `config.gen.go`.
- A new catalog field or rule lands in `manifest` and in the OpenAPI `CatalogEntry` schema together, with a case in [contracts/fixtures/catalog/sdk-fields.yaml](../contracts/fixtures/catalog/sdk-fields.yaml); `libs/contract`'s `TestSDKFieldFixtures` fails when the two disagree.
- Secrets never show: `Secret` and `Values` hide themselves from `fmt` and `slog`, and problem lines and env file errors name the value, never its content.
- Tests run the generated loader of `internal/testmodule`, so a change to `sdkgen` output is covered by `go test -race ./sdk/...`.

## Agent skills

- MCP servers: `mcp-postgres-server` (PyPI, third party, recommended) for the `pg` client

_Drafted by /sync from the introducing change, worth a quick human pass._
