# Verify: Stack & architecture · spec 0001 (with children 0002 and 0003) · updated 2026-10-30
_Steps derived from the acceptance criteria and Value sourcing tables of spec 0001 and its children 0002 and 0003, plus the fixes from the review in `docs/reviews/2026-10-30-main.md`. `/check verify` runs these; `/test` locks the durable ones._

_How to read the tags: `AC-1` to `AC-12` are spec 0001's criteria (0002 adds AC-8 to AC-12 to the same list). Spec 0003 numbers its own criteria, so they are tagged `0003 AC-1` to `0003 AC-7`. `review` marks a step that came from the 2026-10-30 review._

## Spec 0001: stack and architecture

### UI / manual
- [x] `task dev`, open `https://platform.localhost/samples/whoami-postgres` → the editor loads, the catalog lists `postgres` and `whoami`, fetched from `GET /renderer/v1/catalog` → AC-2
- [x] On that sample page, click **Download zip** → a `POST /renderer/v1/render` call, and `whoami-postgres.zip` saved with `docker-compose.yml`, `setup.sh`, `.env`, `graph.yaml`, `README.md`, and no secret value → AC-3
- [x] Under the sub path stack (`compose.subpath.yaml`), open `/platform/`, then `/platform/samples/whoami-postgres`, reload → DevTools › Network shows every request under `/platform/` (assets, catalog at `/platform/renderer/v1/catalog`), the edge marker is drawn; `/platform` redirects (308) to `/platform/` → AC-7

### Commands
- [x] From a clean clone: `task generate && git diff --exit-code` → no diff; `task build` → images `ghcr.io/racherom/modular-platform/web:dev` and `renderer:dev` exist → AC-1
- [x] `task lint && task test` → green → AC-1
- [x] Unzip the sample, run `docker compose up -d` before `setup.sh` → fails with a missing `secrets/postgres/password` file error → AC-4
- [x] `sh setup.sh && docker compose up -d --wait --wait-timeout 120` → `postgres` healthy, `whoami` running; `sha256sum secrets/postgres/password`, run `sh setup.sh` again → same hash, file mode 0600 → AC-4
- [x] `task smoke` → passes, including "the ingress config with a real certificate (acme)", the zip holding exactly `.env README.md docker-compose.yml graph.yaml setup.sh`, and no Playwright report of content blocked by the content policy → AC-3, AC-4, AC-7 · review
- [x] Renderer in public mode with `MP_RENDERER_RATE=1`: send 11 `POST /v1/check` with a varying forged leftmost `X-Forwarded-For` → the 11th answers 429 with `Retry-After`; a body over 1 MB → 413; `docker compose logs renderer` holds no request body and no client address → AC-5
- [x] Renderer with `MP_RENDERER_MODE=internal` and no `MP_RENDERER_TOKEN_FILE` → exits 1; with a token file, a call without `Authorization: Bearer` → 401, `/readyz` still 200 → AC-5
- [x] `go test ./services/renderer/internal/api/ -run Timeout` → a request past the 10 second deadline gets 503 as `application/json` (documented as `TimedOut` in `renderer.yaml`) → Renderer modes · review
- [ ] With the online editor up, `curl -skI https://platform.localhost/` → `content-security-policy` with `default-src 'self'`, `x-content-type-options: nosniff`, `referrer-policy: no-referrer`, no `server` header; with `PLATFORM_TLS=acme` on a real host, `strict-transport-security` too → Online editor wiring · review
- [x] `docker inspect modular-platform-online-ingress-1 --format '{{.HostConfig.CapDrop}} {{.HostConfig.ReadonlyRootfs}} {{.HostConfig.SecurityOpt}}'` → `[ALL] true [no-new-privileges:true]`; same for `web` and `renderer` → Online editor wiring · review

### CI and release (run on GitHub, not locally)
- [ ] Open a pull request → the CI `check` and `smoke` jobs run and pass; change `contracts/openapi/renderer.yaml` without `task generate` → `check` fails on the diff → AC-1, AC-6
- [ ] Merge the release-please pull request → the tag pushes `web` and `renderer` for `linux/amd64` and `linux/arm64` to `ghcr.io/racherom/modular-platform/<service>:<version>`; `docker buildx imagetools inspect` lists both platforms → AC-6

## Spec 0002: graph file, catalog entry, renderer and render

### UI / manual
- [x] `/samples/whoami-postgres` → two nodes, `whoami` left of `postgres`, exactly one edge from whoami's `postgresql` port to postgres's `postgresql` port, arrow at postgres; reload → the same picture, and DevTools › Application shows nothing written to IndexedDB → AC-12
- [x] Same page, the whoami `postgresql` port has a dashed outline (optional need) → Editor
- [x] At `/`, **Open file** with a graph of `whoami` plus two `postgres` instances `pg-a` and `pg-b`, no `uses` → the problem list shows one warning for `whoami`: "Instance whoami can use postgresql from pg-a, pg-b; bind it or set it to null."; no node is marked (a warning never marks one) → Editor · problems
- [x] Same file with `whoami` given `uses: {postgresql: nope}` → the problem list shows an `unknown_provider` error for `whoami`, and the `whoami` node is marked → Editor · problems
- [x] Set `MP_RENDERER_RATE=1` in `deploy/online/.env`, restart the stack, then open a file 11 times in a row → the 11th shows "The renderer is busy, try again in a minute." → Editor · problems
- [x] At `/`, open a `graph.yaml` with an unknown module, fix it in your editor, open it again under the same file name → the problem list says "No problems." and the node mark is gone → Editor · review major
- [x] Open `/samples/nope` → "No sample named nope" with a link back to the editor → Editor · sample route
- [x] Open `/samples/constructor` → "No sample named constructor", not a blank screen → Editor · sample route · review
- [x] `task dev`, then request `https://platform.localhost/@fs/<repo>/README.md` → 403, the dev server serves only `web/` and `samples/` → Editor · sample route · review

### Commands
- [x] `go test ./libs/check/...` → every critical scenario row of spec 0002 passes; the same request checked twice gives byte identical JSON → AC-8
- [x] `go test ./libs/contract/...` and `pnpm --dir web test` → both writers give back every file in `contracts/fixtures/graphs/` and the sample byte for byte → AC-9
- [x] Render the sample → `docker-compose.yml` gives whoami `depends_on: {postgres: {condition: service_healthy}}`, `DEMO_POSTGRESQL_HOST=postgres`, `DEMO_POSTGRESQL_PORT=5432`, and postgres only `POSTGRES_PASSWORD_FILE=/run/secrets/postgres_password` with the secret `postgres_password`; whoami has no secret → AC-10
- [x] `go test ./libs/render/ -run TestSecretComposeNamesNeverCollide` → instance `a-b` with secret `c` and instance `a` with secret `b-c` render without error; top level secrets exactly `a-b_c` and `a_b-c`; `a-b` gets only `C_FILE=/run/secrets/a-b_c`, `a` gets only `BC_FILE=/run/secrets/a_b-c` → AC-10
- [x] `go test ./libs/contract/ -run TestNamesNeverContainUnderscore` → `a_b`, `_a`, `a_` are all invalid names, so the `_` separator stays collision free → AC-10
- [x] `grep -rn "share the Compose name" libs/` → no match; the render keeps no collision check → AC-10
- [x] Change `catalog/whoami.yaml` to `image: traefik/whoami:latest`, rebuild and start the renderer → exit code 1, a log line `catalog/whoami.yaml: image: image_latest: ...`; `go test ./catalog/...` fails the same way → AC-11

### Value sourcing
- [x] Catalog: add a third valid entry to `catalog/`, rebuild → `GET /v1/catalog` lists it sorted by name; checks use it without a restart flag → Check · the catalog
- [x] Candidates: `go test ./libs/check/ -run '^TestCheck$/^required-(one|two)$'` (these fixtures use the test modules in `libs/check/testdata/catalog`, which the built in catalog does not have) → `required-one` gives `binding_recommended` at `modules.app.uses.db` with the candidate `db`, and `graph` keeps the use left out; `required-two` gives `ambiguous_provider` at `modules.app.uses.db` listing `db-a, db-b` → Check · candidates, recommendations
- [x] Messages: every problem in a check result carries a non empty English `message`, the same text for the same code and values on every call → Check · problem message
- [x] Project name: rename the sample graph to `my-stack` → the zip's `docker-compose.yml` starts `name: my-stack` and the file is `my-stack.zip` → Render · Compose project name
- [x] Image and healthcheck: change the postgres entry's tag or interval → the rendered service carries exactly that value → Render · image, healthcheck
- [x] Host: rename the `postgres` instance to `db` (and the binding) → `DEMO_POSTGRESQL_HOST=db` → Render · DEMO_POSTGRESQL_HOST
- [x] Port: change the postgres entry's `provides.postgresql.port` → `DEMO_POSTGRESQL_PORT` follows, written as a string → Render · DEMO_POSTGRESQL_PORT
- [x] Secret path and Compose name: rename the postgres instance to `db` → secret file `secrets/db/password`, Compose secret `db_password`, `POSTGRES_PASSWORD_FILE=/run/secrets/db_password`, and `setup.sh` creates that path → Render · secret file path, Compose secret name and env value
- [x] Zip graph: render `sample-no-uses.yaml` edited to a required need → the zip's `graph.yaml` holds the accepted binding in canonical text → Render · graph.yaml in the zip
- [x] Positions: drag a node, reload → the auto layout comes back, the saved file is unchanged → Editor · node positions
- [x] Edges: open `sample-null.yaml` → no edge, no warning; `sample-no-uses.yaml` → no edge, one `optional_unbound` warning → Editor · edges
- [x] Sample graph: edit `samples/whoami-postgres/graph.yaml`, rebuild the web image → the sample route shows the edit, not anything in browser storage → Editor · sample graph

## Spec 0003: rate limiter bucket cap

### Commands
- [x] `go test -race -count=1 ./libs/platform/ -run TestRateLimiterCap` → after 10 keys with a cap of 3, `Len()` is exactly 3 → 0003 AC-1
- [x] `go test ./libs/platform/ -run TestNewRateLimiterNeedsMaxKeys` → `NewRateLimiter` with `maxKeys` 0 panics with `platform: NewRateLimiter maxKeys must be at least 1` → 0003 AC-1
- [x] `go test -race -count=1 ./services/renderer/internal/api/ -run TestPublicClientCap` → with 10,000 clients held, the oldest spent client still gets 429; client 10,001 gets 200 and the next oldest spent client comes back with a fresh burst (200) → 0003 AC-1, AC-4, AC-7
- [x] `go test ./libs/platform/ -run TestRateLimiterFullAllowsNewKey` → at a full map a new key gets 10 allowed requests, the 11th is refused only by its own empty bucket → 0003 AC-2
- [x] `go test ./libs/platform/ -run TestRateLimiterEvictsLeastRecentlySeen` → `k1 k2 k3` added, `k1` used again, `k4` added: `k2` gone, `k1 k3 k4` kept → 0003 AC-3
- [x] `go test ./libs/platform/ -run TestRateLimiterEvictedClientStartsFresh` → a key that spent its burst, once evicted, gets 10 allowed requests again → 0003 AC-4
- [x] `go test ./libs/platform/ -run TestRateLimiterSweepIdle` → idle 10 minutes plus 1 ns is dropped, idle exactly 10 minutes survives one more sweep, recent buckets kept → 0003 AC-5
- [x] `go test ./libs/platform/ -run 'TestRateLimiterSweepCounts|TestRateLimiterWarnsOncePerInterval'` → `Sweep` counts only cap evictions and resets; one `WARN` `rate limiter full` with `evicted`, `buckets`, `max` per tick with evictions, nothing on a quiet tick, no key in the log → 0003 AC-6
- [x] `go test -race -count=1 ./libs/platform/ -run TestRateLimiterConcurrent` and `task test` → concurrent `Allow` and `Sweep` pass the race detector, never over the cap; every older test (burst 10, 429 with `Retry-After`, health paths exempt, internal mode token) still passes → 0003 AC-7

### Value sourcing
- [x] Cap: `grep -n maxClients services/renderer/internal/api/server.go` → `maxClients = 10_000`, passed as the fourth argument of `NewRateLimiter`; `TestPublicClientCap` fails if it is 9,999 or 10,001 → 0003 AC-1
- [x] Bucket to evict: the 0003 AC-3 test above shows it is `order.Back()`, the least recently seen, not the oldest created → 0003 AC-3
- [x] Newcomer's allowance: `TestPublicClientCap` runs with `Rate: 1`; an evicted client gets exactly the burst of 10, not more → 0003 AC-2, AC-4
- [x] `now` read under the lock: read `Allow` in `libs/platform/ratelimit.go`, the clock is read after `l.mu.Lock()`; the concurrent test passes under `-race` → 0003 AC-5, AC-7
- [x] Idle cutoff: the 0003 AC-5 test moves the fake clock across the 10 minute edge by 1 ns → 0003 AC-5
- [x] Warning `evicted`, `buckets`, `max`: the 0003 AC-6 test checks `evicted` 2 then 1, `buckets` 2, `max` 2 against a cap of 2 → 0003 AC-6
- [x] Logger: with the online editor up (`task dev`), `docker logs modular-platform-online-renderer-1 2>&1 | grep 'rate limiter full'` after ordinary use → no line (normal use never fills the map); a line every minute in normal traffic means `MP_PROXY_HOPS` is wrong → 0003 AC-6
- [x] Interval end: the 0003 AC-6 test drives `sweepTicks` with its own channel; evictions after the last tick are not logged → 0003 AC-6

## Acceptance-criteria coverage

| Criterion | Checked by |
|---|---|
| AC-1 | 0001 clean clone build, lint and test, CI pull request |
| AC-2 | 0001 UI: editor loads with the catalog |
| AC-3 | 0001 UI: download zip; `task smoke` |
| AC-4 | 0001 commands: up before setup, setup twice; `task smoke` |
| AC-5 | 0001 commands: public mode limits, internal mode token |
| AC-6 | 0001 CI pull request, release tag |
| AC-7 | 0001 UI: sub path stack; `task smoke` |
| AC-8 | 0002 commands: `go test ./libs/check/...`; value sourcing: candidates |
| AC-9 | 0002 commands: both writers round trip |
| AC-10 | 0002 commands: rendered sample, secret name collision test, `_` guard, no collision check left |
| AC-11 | 0002 commands: `latest` image refused |
| AC-12 | 0002 UI: sample route picture |
| 0003 AC-1 | cap test, panic guard, server cap, cap constant |
| 0003 AC-2 | full map new key, newcomer's allowance |
| 0003 AC-3 | least recently seen eviction |
| 0003 AC-4 | fresh after eviction (unit and server) |
| 0003 AC-5 | idle sweep and its 1 ns edge, clock under the lock |
| 0003 AC-6 | sweep counts, one warning per tick, warning fields, interval end, logger in normal use |
| 0003 AC-7 | concurrent `-race`, server cap, every older test |
