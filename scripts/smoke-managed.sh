#!/bin/sh
# The smoke-managed job (spec 0006, AC-19): install the managed platform from deploy/managed with
# setup.sh, start it, sign in as the admin, deploy the sample from the editor and see every module
# running; restart Docker and see it running again without an apply (AC-12); remove it with
# Delete data too; then check that no secret value reached a log. Needs the four platform images
# tagged ci (IMAGE_TAG=ci task images) and Chromium for Playwright. SMOKE_LOGS, when set, keeps
# the logs. SMOKE_DOCKER_RESTART=1 restarts the Docker daemon with sudo systemctl (CI sets it);
# without it the restart is skipped, so a local run never restarts your Docker.
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

# wait_healthy waits up to 5 minutes for the eight long running services to be healthy and
# db-init to have exited 0.
wait_healthy() {
	deadline=$(($(date +%s) + 300))
	until [ "$(compose ps --format '{{.Service}} {{.Health}}' | grep -c ' healthy$')" -ge 8 ] &&
		[ "$(compose ps -a --format '{{.Service}} {{.State}} {{.ExitCode}}' | grep -c '^db-init exited 0$')" -eq 1 ]; do
		if [ "$(date +%s)" -ge "$deadline" ]; then
			compose ps -a
			echo "the platform did not become healthy within 5 minutes" >&2
			exit 1
		fi
		sleep 3
	done
	compose ps -a
}

browser() {
	SMOKE_TLS=internal SMOKE_ADMIN_USER=admin SMOKE_ADMIN_PASSWORD="$password" SMOKE_ZIP="$work/whoami-postgres.zip" \
		pnpm --dir "$repo/web" run smoke:managed --grep "$1"
}

echo "== start"
compose up -d
wait_healthy

echo "== sign in, download the zip, deploy the sample, see it running"
browser @deploy
# Delete data too deletes these at the end; the log check still needs their values
cp -R "$platform/deployments/mp-whoami-postgres/secrets" "$work/deployment-secrets"

if [ "${SMOKE_DOCKER_RESTART:-}" = 1 ]; then
	echo "== restart Docker; restart: unless-stopped brings everything back"
	sudo systemctl restart docker
	wait_healthy
else
	echo "== Docker restart skipped (set SMOKE_DOCKER_RESTART=1 to run it)"
fi

echo "== running again without an apply, then remove with Delete data too"
browser @after-restart
if docker compose -p mp-whoami-postgres ps -a --format '{{.Name}}' | grep -q .; then
	echo "containers of mp-whoami-postgres are left after the remove" >&2
	exit 1
fi
if [ -d "$platform/deployments/mp-whoami-postgres/secrets" ]; then
	echo "Delete data too left the deployment's secrets folder" >&2
	exit 1
fi

echo "== no secret value in any log"
logs="$work/all.log"
{ compose logs --no-color; docker compose -p mp-whoami-postgres logs --no-color; } > "$logs" 2>&1
leaks=0
# certificates are public and skipped: cert-issuer logs every leaf it issues, and a leaf repeats
# its intermediate's name, so a line of intermediate_ca.crt can match by chance
for f in $(find "$platform/secrets" "$platform/deployments" "$work/deployment-secrets" -path '*secrets/*' -type f ! -name '*.crt'); do
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
