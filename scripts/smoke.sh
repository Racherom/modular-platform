#!/bin/sh
# The smoke job (AC-3, AC-4, AC-7): start the online tool, download the sample zip through the
# editor, run it with setup.sh and docker compose, then check the editor under a sub path.
# Needs the web and planner images tagged dev (task images) and Chromium for Playwright.
set -eu

repo=$(cd "$(dirname "$0")/.." && pwd)
work=$(mktemp -d)
online="$repo/deploy/online"
export MP_VERSION=dev

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

[ -f "$online/.env" ] || cp "$online/.env.example" "$online/.env"

echo "== online tool at the site root"
online -f "$online/compose.yaml" up -d --wait --wait-timeout 120
SMOKE_BASE='' SMOKE_ZIP="$work/sample.zip" pnpm --dir "$repo/web" run smoke:browser

echo "== the downloaded zip (AC-3, AC-4)"
mkdir "$work/stack"
cd "$work/stack"
unzip -q "$work/sample.zip"
for f in docker-compose.yml setup.sh .env graph.yaml README.md; do
	[ -f "$f" ] || { echo "smoke: the zip has no $f" >&2; exit 1; }
done
if grep -rq "POSTGRES_PASSWORD=" .; then
	echo "smoke: the zip holds a password" >&2
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
