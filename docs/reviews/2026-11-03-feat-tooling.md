# Review, feat/tooling (module SDK, scope #27), 2026-11-03

**Reviewed by**: fresh model reviewer, Sonnet 5.5 (author on a different model)
**Scope**: 123 files in the diff command's paths (about 14,900 lines added), branch vs main, uncommitted test and AGENTS edits included. Web, lefthook, Biome and skills files left out.
**Verdict**: Approve with nits

## Summary

You added the module SDK: a `manifest` package shared with `libs/contract`, a strict config loader with one problem line per value, `sdk.Run` with health, readiness, graceful shutdown and plain or TLS serving, binding tokens, the `MP-Caller` token, an ACME certificate manager, three client packages, pluggable sources, `sdkgen`, the render additions, and the planner moved onto it with `libs/platform` deleted. I read the loader, `Run` and `build`, the auth, caller and tlsacme packages, the resolver, values and cache code, the render and planner changes, and `CONTRACT.md` with its test. I found no blockers and no majors. `go test -race ./...` passes in `sdk`, `libs` and `services/planner` (I did not run the Docker tests). What is left is small: a few unbounded or unguarded spots and some untested helper packages.

## Minor

### 🟡 The resolver keeps every client it opens, `sdk/resolver.go:96-122`
**Problem**: `resolver.clients` gets one entry per distinct binding key (URL plus credentials hash) and nothing ever removes one. The answer cache (`answers`) is an LRU capped at 10,000 and expires after 30 seconds, but the clients it points at stay open until shutdown.
**Why it matters**: A resolver that hands out many different bindings over time (for example one per user) keeps a growing set of open pgx pools and HTTP transports. That is a slow leak of connections and memory in a long running module.
**Suggested fix**: Cap `clients` and close the least recently used one when it is evicted, or close a client when no cached answer points at it any more. If you leave it as is, say so in the doc comment and in `CONTRACT.md`.

### 🟡 A nil `Opened.Ready` panics in a background goroutine, `sdk/sdk.go:318-322` and `sdk/ready.go:31`
**Problem**: `build` calls `r.wait(..., opened[b.Capability].Ready)`, and `readiness.wait` calls `ready(attempt)` without a nil check. `Opened` is an exported type, so a hand written `Opener` (or a test fake) that leaves `Ready` unset crashes the whole module after it started, not at start.
**Why it matters**: The failure shows up late and as a panic, which goes against "no panics for expected failures".
**Suggested fix**: Treat a nil `Ready` as ready at once, or turn it into an `invalid_binding` problem in `open`.

### 🟡 `sdktest`, `testpki` and the two test modules have no tests of their own, `sdk/sdktest/sdktest.go`
**Problem**: `sdktest` is an exported package that other modules will build their tests on, and it has no `_test.go`. It is exercised only indirectly by the SDK's own tests.
**Why it matters**: A regression in a fake source or in the env file helpers would break every module's tests and the cause would be hard to see.
**Suggested fix**: Add a short table test per exported helper (env file and clients directory builders, fake sources).

### 🟡 The deleted planner `main_test.go` is replaced, but the push source and `Run` path of the planner have no end to end check outside Docker, `services/planner/internal/api/server_test.go:316-355`
**Problem**: `TestStartRefused` and `TestBadCatalogStopsStart` cover `Setup` through `sdk.Build`. That covers AC-13 well, but `sdk.Run` itself (healthcheck subcommand, exit codes) is only covered by `task smoke`.
**Why it matters**: Low risk. It is noted so you know the gap is covered by the smoke job and not by a Go test.
**Suggested fix**: None needed if `task smoke` stays in CI. Otherwise add a small subprocess test of `planner healthcheck`.

## Nits

- ⚪ `services/planner/go.mod`, `github.com/racherom/modular-platform/sdk v0.0.0` sits in the `// indirect` block but is a direct dependency. `go mod tidy` or moving the line fixes the grouping.
- ⚪ `go.work:3`, the `go` line moved from `1.26.0` to `1.26.8` while `AGENTS.md` still says "Go 1.26 or newer" and `sdk/go.mod` says `1.26.0`. Pick one story and keep the files in line.
- ⚪ `sdk/internal/load/load.go:116`, `Problem.String` and `SortProblems` have doc comments, but the `Warning` fields `At` and `Value` are explained only in one line; fine, but `Problem.File` and `Line` are described as "for sorting" only, while they also carry env file problems into the message. A clearer comment helps the next reader.
- ⚪ `sdk/internal/load/load.go:572`, the `//nolint:errorlint` type assertion `err.(*manifest.RuleError)` panics if `Parse` ever returns another error type. `errors.As` with a fallback problem line costs nothing.
- ⚪ `libs/go.mod`, the integration test dependencies (`testcontainers-go` and its compose module, about 150 lines) now live in `libs/go.mod`. Production builds do not link them, but every `go mod download` of `libs` pulls them. Consider keeping the e2e test in its own module if this grows.

## Strengths

- `CONTRACT.md` is backed by `TestContract`, which fails when a statement names no test, a named test does not exist, or an id repeats. It also lists honestly the spec rules that no test proves yet.
- Secret handling is consistent: `Secret`, `Values`, `Bindings`, `httpclient.Client` and `caller.Signer` hide themselves from `fmt` and `slog`, source errors are never printed, and problem lines never carry values.
- The middleware order is right and checked: the binding token runs before the `MP-Caller` check, the `iss` must match the proven calling module, health paths bypass everything, and tokens are compared against every entry in constant time.
- `pg.config` resets every field pgx would read from `PG*` env vars, so only the SDK's own bootstrap names reach the process (this keeps the "only sdk/ reads env" rule honest).
- The TLS manager is careful: it never orders from a handshake, checks the issued leaf against the CA, name, usage and key pair, keeps serving the held certificate while retrying with backoff, and drops readiness under two hours left.
- `libs/render` and `libs/planner` stay pure, and the render golden files for the SDK pair plus the `missing_token_mapping` planner rule have fixtures that both `libs/contract` and `sdk/manifest` check.

## Test coverage

Coverage is strong for a change of this size. The loader has table tests for every source level, type and problem code. `sdk_test.go`, `bindings_test.go`, `sources_test.go`, `tls_test.go` and `caller_test.go` cover startup refusal, readiness, push snapshots, caller sources, resolvers, middleware order and every `MP-Caller` refusal. The TLS manager has fake clock tests for renewal, backoff, timeout and readiness, and there are Pebble, Postgres and MinIO tests behind the `integration` and `e2e` tags, with a CI job to run them. The gaps are the ones listed under Minor: `sdktest` and the helper modules have no direct tests, and the resolver's client retention is not tested for growth. Run `task test:integration` before merge, since I did not run the Docker tests.
