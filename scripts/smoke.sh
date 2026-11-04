#!/bin/sh
# The smoke job (AC-3, AC-4, AC-7): start the online tool, download the sample zip through the
# editor, run it with setup.sh and docker compose, then check the editor under a sub path.
# Needs the web and planner images tagged dev (task images) and Chromium for Playwright.
set -eu
# Nothing in this job may wait for an answer: a question on the terminal (like Compose asking to
# recreate a volume) would hang the run forever.
exec </dev/null

repo=$(cd "$(dirname "$0")/.." && pwd)
work=$(mktemp -d)
online="$repo/deploy/online"
export MP_VERSION=dev
# the smoke stack uses Caddy's internal certificate authority; Playwright skips certificate checks only then
export SMOKE_TLS=internal

online() {
	docker compose --project-directory "$online" --env-file "$online/versions.env" --env-file "$online/.env" "$@"
}

cleanup() {
	if [ -f "$work/stack/docker-compose.yml" ]; then
		docker compose --project-directory "$work/stack" down --volumes >/dev/null 2>&1 || true
	fi
	online -f "$online/compose.yaml" down --volumes >/dev/null 2>&1 || true
	rm -rf "$work"
}
trap cleanup EXIT
# a killed run cleans up too, so it leaves no project behind for the next run to trip over
trap 'exit 130' INT TERM HUP

[ -f "$online/.env" ] || cp "$online/.env.example" "$online/.env"

echo "== the ingress config with a real certificate (acme), as in production"
docker run --rm -v "$online/Caddyfile:/etc/caddy/Caddyfile:ro" \
	-e PLATFORM_DOMAIN=example.com -e PLATFORM_TLS=acme -e PLATFORM_ACME_EMAIL= -e MP_BASE_PATH= \
	"$(sed -n 's/^CADDY_IMAGE=//p' "$online/versions.env")" caddy validate --config /etc/caddy/Caddyfile >/dev/null

echo "== online tool at the site root"
online -f "$online/compose.yaml" up -d --wait --wait-timeout 120
SMOKE_BASE='' SMOKE_ZIP="$work/sample.zip" pnpm --dir "$repo/web" run smoke:browser

echo "== the downloaded zip (AC-3, AC-4)"
mkdir "$work/stack"
cd "$work/stack"
unzip -q "$work/sample.zip"
# exactly these files and nothing else, so no secrets/ folder or stray file can slip in
files=$(unzip -Z1 "$work/sample.zip" | LC_ALL=C sort | tr '\n' ' ')
[ "$files" = ".env README.md docker-compose.yml graph.yaml setup.sh " ] || {
	echo "smoke: unexpected zip contents: $files" >&2
	exit 1
}
# no secret value: no password setting and nothing shaped like a generated secret (64 hex characters)
if grep -rqE "POSTGRES_PASSWORD=|[0-9a-f]{64}" .; then
	echo "smoke: the zip holds a secret value" >&2
	exit 1
fi

if docker compose up -d >"$work/no-setup.log" 2>&1; then
	echo "smoke: docker compose up started without setup.sh" >&2
	exit 1
fi
grep -q "secrets/postgres/password" "$work/no-setup.log" || { cat "$work/no-setup.log" >&2; exit 1; }
docker compose down --volumes >/dev/null 2>&1
echo "without setup.sh: refused, missing secret file"

sh setup.sh
before=$(cat secrets/postgres/password)
sh setup.sh
[ "$before" = "$(cat secrets/postgres/password)" ] || { echo "smoke: setup.sh changed a secret" >&2; exit 1; }
echo "second setup.sh: secret unchanged"

docker compose up -d --wait --wait-timeout 120
docker compose ps
docker compose down --volumes
cd "$repo"

echo "== online tool under /platform (AC-7)"
online -f "$online/compose.yaml" -f "$online/compose.subpath.yaml" up -d --wait --wait-timeout 120
SMOKE_BASE=/platform pnpm --dir "$repo/web" run smoke:browser

echo "smoke: all checks passed"
