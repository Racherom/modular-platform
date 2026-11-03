#!/bin/sh
# The smoke-managed job (spec 0006, AC-19): install the managed platform from deploy/managed with
# setup.sh, start it, sign in as the admin, deploy the sample from the editor and see every module
# running; then check that no secret value reached a log. Needs the four platform images tagged
# ci (IMAGE_TAG=ci task images) and Chromium for Playwright. SMOKE_LOGS, when set, keeps the logs.
set -eu

repo=$(cd "$(dirname "$0")/.." && pwd)
work=$(mktemp -d)
platform="$work/modular-platform"
password=$(od -An -tx1 -N16 /dev/urandom | tr -d ' \n')

compose() { docker compose --project-directory "$platform" -f "$platform/compose.yaml" -f "$platform/compose.ci.yaml" "$@"; }

cleanup() {
	status=$?
	if [ -d "$platform" ]; then
		if [ -n "${SMOKE_LOGS:-}" ]; then
			mkdir -p "$SMOKE_LOGS"
			compose logs --no-color > "$SMOKE_LOGS/platform.log" 2>&1 || true
			docker compose -p mp-whoami-postgres logs --no-color > "$SMOKE_LOGS/deployment.log" 2>&1 || true
		fi
		docker compose -p mp-whoami-postgres down --volumes >/dev/null 2>&1 || true
		compose down --volumes >/dev/null 2>&1 || true
	fi
	# the deployer wrote the deployment folder as you, so it can go
	rm -rf "$work"
	exit "$status"
}
trap cleanup EXIT

echo "== install with setup.sh"
cp -R "$repo/deploy/managed" "$platform"
printf 'platform.localhost\nadmin@example.com\nadmin\n%s\n' "$password" | sh "$platform/setup.sh"

echo "== start"
compose up -d
deadline=$(($(date +%s) + 300))
until [ "$(compose ps --format '{{.Service}} {{.Health}}' | grep -c ' healthy$')" -ge 7 ] &&
	[ "$(compose ps -a --format '{{.Service}} {{.State}} {{.ExitCode}}' | grep -c '^db-init exited 0$')" -eq 1 ]; do
	if [ "$(date +%s)" -ge "$deadline" ]; then
		compose ps -a
		echo "the platform did not become healthy within 5 minutes" >&2
		exit 1
	fi
	sleep 3
done
compose ps -a

echo "== sign in, deploy the sample, see it running"
SMOKE_TLS=internal SMOKE_ADMIN_USER=admin SMOKE_ADMIN_PASSWORD="$password" pnpm --dir "$repo/web" run smoke:managed

echo "== no secret value in any log"
logs="$work/all.log"
{ compose logs --no-color; docker compose -p mp-whoami-postgres logs --no-color; } > "$logs" 2>&1
leaks=0
for f in $(find "$platform/secrets" "$platform/deployments" -path '*secrets/*' -type f); do
	# every line of 16 characters or more that is not a PEM armour line
	grep -v -e '^-----' "$f" | while IFS= read -r line; do
		[ "${#line}" -ge 16 ] || continue
		if grep -qF -e "$line" "$logs"; then
			echo "a value of $f appears in the logs" >&2
			exit 1
		fi
	done || leaks=1
done
if printf '%s' "$password" | grep -qF -f - "$logs"; then
	echo "the admin password appears in the logs" >&2
	leaks=1
fi
[ "$leaks" -eq 0 ]
echo "== smoke-managed passed"
