# Verify: Stack & architecture · spec 0001 (with child 0002) · updated 2026-10-29
_Steps derived from spec 0001 and 0002 acceptance criteria. `/check verify` runs these; `/test` locks the durable ones._

## UI / manual
- [ ] `task dev`, open `https://platform.localhost/samples/whoami-postgres` → the editor loads, the catalog lists `postgres` and `whoami`, fetched from `GET /planner/v1/catalog` → AC-2
- [ ] On that sample page, click **Download zip** → a `POST /planner/v1/render` call, and `whoami-postgres.zip` saved with `docker-compose.yml`, `setup.sh`, `.env`, `graph.yaml`, `README.md`, and no secret value → AC-3
- [ ] Same page → two nodes, `whoami` left of `postgres`, exactly one edge from whoami's `postgresql` port to postgres's `postgresql` port, arrow at postgres; reload → the same picture, and DevTools › Application shows nothing written to IndexedDB → AC-12
- [ ] Same page, the whoami `postgresql` port has a dashed outline (optional need) → spec 0002 Editor
- [ ] Open `contracts/fixtures/graphs/required-two.yaml` with **Open file** at `/` → the problem list shows `ambiguous_provider` naming `db-a, db-b`, the `app` node is marked; with the planner rate limit hit, an action shows "The planner is busy, try again in a minute." → spec 0002 Editor
- [ ] Open `/samples/nope` → "No sample named nope" with a link back to the editor → spec 0002 Editor
- [ ] Under the sub path stack (`compose.subpath.yaml`), open `/platform/`, then `/platform/samples/whoami-postgres`, reload → DevTools › Network shows every request under `/platform/` (assets, catalog at `/platform/planner/v1/catalog`), the edge marker is drawn; `/platform` redirects (308) to `/platform/` → AC-7

## Commands
- [ ] From a clean clone: `task generate && git diff --exit-code` → no diff; `task build` → images `ghcr.io/racherom/modular-platform/web:dev` and `planner:dev` exist → AC-1
- [ ] `task lint && task test` → green → AC-1
- [ ] Unzip the sample, run `docker compose up -d` before `setup.sh` → fails with a missing `secrets/postgres/password` file error → AC-4
- [ ] `sh setup.sh && docker compose up -d --wait --wait-timeout 120` → `postgres` healthy, `whoami` running; `sha256sum secrets/postgres/password`, run `sh setup.sh` again → same hash, file mode 0600 → AC-4
- [ ] Planner in public mode with `MP_PLANNER_RATE=1`: send 11 `POST /v1/plan` with a varying forged leftmost `X-Forwarded-For` → the 11th answers 429 with `Retry-After`; a body over 1 MB → 413; `docker compose logs planner` holds no request body and no client address → AC-5
- [ ] Planner with `MP_PLANNER_MODE=internal` and no `MP_PLANNER_TOKEN_FILE` → exits 1; with a token file, a call without `Authorization: Bearer` → 401, `/readyz` still 200 → AC-5
- [ ] Open a pull request → the CI `check` and `smoke` jobs run and pass; change `contracts/openapi/planner.yaml` without `task generate` → `check` fails on the diff → AC-1, AC-6
- [ ] Merge the release-please pull request → the tag pushes `web` and `planner` for `linux/amd64` and `linux/arm64` to `ghcr.io/racherom/modular-platform/<service>:<version>`; `docker buildx imagetools inspect` lists both platforms → AC-6
- [ ] `go test ./libs/planner/...` → every critical scenario row of spec 0002 passes; the same request planned twice gives byte identical JSON → AC-8
- [ ] `go test ./libs/contract/...` and `pnpm --dir web test` → both writers give back every file in `contracts/fixtures/graphs/` and the sample byte for byte → AC-9
- [ ] Render the sample → `docker-compose.yml` gives whoami `depends_on: {postgres: {condition: service_healthy}}`, `DEMO_POSTGRESQL_HOST=postgres`, `DEMO_POSTGRESQL_PORT=5432`, and postgres only `POSTGRES_PASSWORD_FILE=/run/secrets/postgres-password` with the secret `postgres-password`; whoami has no secret → AC-10
- [ ] Change `catalog/whoami.yaml` to `image: traefik/whoami:latest`, rebuild and start the planner → exit code 1, a log line `catalog/whoami.yaml: image: image_latest: ...`; `go test ./catalog/...` fails the same way → AC-11

## Value sourcing (spec 0002)
- [ ] Catalog: add a third valid entry to `catalog/`, rebuild → `GET /v1/catalog` lists it sorted by name; plans use it without a restart flag → Plan · the catalog
- [ ] Candidates: plan `required-one.yaml`, then add a second `db` instance → first auto binds (`auto: true`, written into `graph`), second gives `ambiguous_provider` with both names → Plan · candidates, automatic bindings
- [ ] Messages: every problem in a plan carries a non empty English `message`, the same text for the same code and values on every call → Plan · problem message
- [ ] Project name: rename the sample graph to `my-stack` → the zip's `docker-compose.yml` starts `name: my-stack` and the file is `my-stack.zip` → Render · Compose project name
- [ ] Image and healthcheck: change the postgres entry's tag or interval → the rendered service carries exactly that value → Render · image, healthcheck
- [ ] Host: rename the `postgres` instance to `db` (and the binding) → `DEMO_POSTGRESQL_HOST=db` → Render · DEMO_POSTGRESQL_HOST
- [ ] Port: change the postgres entry's `provides.postgresql.port` → `DEMO_POSTGRESQL_PORT` follows, written as a string → Render · DEMO_POSTGRESQL_PORT
- [ ] Secret path: rename the postgres instance to `db` → secret file `secrets/db/password`, Compose secret `db-password`, `POSTGRES_PASSWORD_FILE=/run/secrets/db-password`, and `setup.sh` creates that path → Render · secret file path and env value
- [ ] Zip graph: render `sample-no-uses.yaml` edited to a required need → the zip's `graph.yaml` holds the automatic binding in canonical text → Render · graph.yaml in the zip
- [ ] Positions: drag a node, reload → the auto layout comes back, the saved file is unchanged → Editor · node positions
- [ ] Edges: open `sample-null.yaml` → no edge, no warning; `sample-no-uses.yaml` → no edge, one `optional_unbound` warning → Editor · edges
- [ ] Sample graph: edit `samples/whoami-postgres/graph.yaml`, rebuild the web image → the sample route shows the edit, not anything in browser storage → Editor · sample graph

## Acceptance-criteria coverage
- AC-1 … Commands 1, 2, 7 · AC-2 … UI 1 · AC-3 … UI 2 · AC-4 … Commands 3, 4 · AC-5 … Commands 5, 6 · AC-6 … Commands 7, 8 · AC-7 … UI 7 · AC-8 … Commands 9 · AC-9 … Commands 10 · AC-10 … Commands 11 · AC-11 … Commands 12 · AC-12 … UI 3
