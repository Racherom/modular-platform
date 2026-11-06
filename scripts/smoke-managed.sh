#!/bin/sh
# The smoke-managed job (spec 0006, AC-19; spec 0015, AC-16): install the managed platform from the
# folder task managed:render wrote into build/managed with its setup.sh, start it, sign in as the admin, deploy the sample from the editor and see every module
# running; restart Docker and see it running again without an apply (AC-12); remove it with
# Delete data too; then check that no secret value reached a log. Needs the four platform images
# tagged ci (task images IMAGE_TAG=ci, as a Task variable: an env var loses to the Taskfile's default) and Chromium for Playwright. SMOKE_LOGS, when set, keeps
# the logs. SMOKE_DOCKER_RESTART=1 restarts the Docker daemon with sudo systemctl (CI sets it);
# without it the restart is skipped, so a local run never restarts your Docker.
set -eu
# Nothing in this job may wait for an answer: a question on the terminal (like Compose asking to
# recreate a volume) would hang the run forever. setup.sh still gets its answers through a pipe.
exec </dev/null

repo=$(cd "$(dirname "$0")/.." && pwd)
work=$(mktemp -d)
platform="$work/modular-platform"
password=$(od -An -tx1 -N16 /dev/urandom | tr -d ' \n')

compose() { docker compose --project-directory "$platform" -f "$platform/docker-compose.yml" -f "$repo/deploy/managed/compose.ci.yaml" "$@"; }

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
# a killed run cleans up too, so it leaves no project behind for the next run to trip over
trap 'exit 130' INT TERM HUP

# A run killed earlier, or your own test install, leaves these Compose projects behind, and
# Compose would then ask whether to recreate their volumes. Stop here instead and leave them alone.
for project in modular-platform mp-whoami-postgres; do
	if [ -n "$(docker ps -aq --filter "label=com.docker.compose.project=$project")$(docker volume ls -q --filter "label=com.docker.compose.project=$project")" ]; then
		echo "smoke-managed: the Compose project $project already exists (containers or volumes). Remove it first:" >&2
		echo "  docker rm -f \$(docker ps -aq --filter label=com.docker.compose.project=$project)" >&2
		echo "  docker volume rm \$(docker volume ls -q --filter label=com.docker.compose.project=$project)" >&2
		exit 1
	fi
done

echo "== install with setup.sh"
if [ ! -f "$repo/build/managed/docker-compose.yml" ]; then
	echo "smoke-managed: build/managed is missing; run task managed:render first (task smoke:managed does)" >&2
	exit 1
fi
cp -R "$repo/build/managed" "$platform"
# spec 0015's questions: domain, origin (empty takes https://<domain>), admin email, admin user
# name, admin password
printf 'platform.localhost\n\nadmin@example.com\nadmin\n%s\n' "$password" | sh "$platform/setup.sh"

# wait_healthy waits up to 5 minutes for the eight long running services to be healthy and every job
# of the rendered project (today four: the certificate job of postgres, two database provision jobs
# and the revoke job, spec 0014) to have exited 0. The count comes from the project, so a new binding
# to a provisioner needs no change here.
wait_healthy() {
	deadline=$(($(date +%s) + 300))
	jobs=$(compose config --services | grep -Ec '_(provision|revoke)_')
	until [ "$(compose ps --format '{{.Service}} {{.Health}}' | grep -c ' healthy$')" -ge 8 ] &&
		[ "$(compose ps -a --format '{{.Service}} {{.State}} {{.ExitCode}}' | grep -Ec '_(provision|revoke)_.* exited 0$')" -eq "$jobs" ]; do
		if [ "$(date +%s)" -ge "$deadline" ]; then
			compose ps -a
			echo "the platform did not become healthy within 5 minutes" >&2
			exit 1
		fi
		sleep 3
	done
	compose ps -a
}

# tls_checks proves AC-17 on the running platform (spec 0014): postgres refuses a plain connection,
# api and authelia are on TLS, the certificate has about 90 days, and a certificate with under 30
# days left is reissued by the next up and reaches the running server without a restart.
tls_checks() {
	net=modular-platform_internal
	pgimage=$(docker inspect -f '{{.Config.Image}}' "$(compose ps -aq postgres_revoke_server)")
	stepimage=$(docker inspect -f '{{.Config.Image}}' "$(compose ps -aq cert-issuer_provision_postgres_tls)")
	host=postgres.modular-platform.internal
	secrets="$platform/secrets"
	pg() {
		docker run --rm --network "$net" -e PGPASSWORD="$(cat "$secrets/postgres/password")" -v "$secrets/cert-issuer/root-ca:/ca:ro" "$pgimage" \
			psql -X -tA "host=$host user=postgres $1" -c "$2"
	}
	server_cert() {
		docker run --rm --network "$net" "$pgimage" sh -c "openssl s_client -starttls postgres -connect $host:5432 </dev/null 2>/dev/null | openssl x509 -noout $1"
	}

	if pg "sslmode=disable" "select 1" >/dev/null 2>&1; then
		echo "postgres let a plain connection in" >&2
		exit 1
	fi
	plain=$(pg "sslmode=verify-full sslrootcert=/ca" "select count(*) filter (where s.ssl), count(*) from pg_stat_ssl s join pg_stat_activity a using (pid) where a.usename in ('api', 'authelia')")
	case $plain in
	[1-9]*"|"*) [ "${plain%%|*}" = "${plain##*|}" ] || { echo "api or authelia is connected without TLS: $plain" >&2; exit 1; } ;;
	*) echo "api and authelia are not connected to postgres: $plain" >&2; exit 1 ;;
	esac
	server_cert "-checkend $((60 * 86400))" >/dev/null || { echo "the certificate of postgres has under 60 days left; want about 90" >&2; exit 1; }

	before=$(server_cert -serial)
	started=$(docker inspect -f '{{.State.StartedAt}}' "$(compose ps -aq postgres)")
	# a certificate for the same name with 10 days left, made with the intermediate key, written in place
	docker run --rm --user "$(id -u):$(id -g)" --tmpfs /tmp -e STEPPATH=/tmp/step -v "$secrets/cert-issuer:/ca:ro" -v "$secrets/postgres:/out" \
		--entrypoint sh "$stepimage" -c "
		set -eu
		step certificate create $host /tmp/leaf.crt /tmp/leaf.key --profile leaf --ca /ca/intermediate-ca --ca-key /ca/intermediate-ca-key \
			--ca-password-file /ca/password --no-password --insecure --not-after 240h >/dev/null 2>&1
		cat /tmp/leaf.crt /ca/intermediate-ca > /out/uses_tls_cert
		cat /tmp/leaf.key > /out/uses_tls_key"
	compose up -d --wait
	after=$(server_cert -serial)
	[ "$before" != "$after" ] || { echo "the next up did not reissue the certificate of postgres" >&2; exit 1; }
	server_cert "-checkend $((60 * 86400))" >/dev/null || { echo "the reissued certificate has under 60 days left" >&2; exit 1; }
	[ "$started" = "$(docker inspect -f '{{.State.StartedAt}}' "$(compose ps -aq postgres)")" ] || { echo "postgres restarted to load the certificate" >&2; exit 1; }
	pg "sslmode=verify-full sslrootcert=/ca" "select 1" >/dev/null
}

browser() {
	SMOKE_TLS=internal SMOKE_ADMIN_USER=admin SMOKE_ADMIN_PASSWORD="$password" SMOKE_ZIP="$work/whoami-postgres.zip" \
		pnpm --dir "$repo/web" run smoke:managed --grep "$1"
}

echo "== start"
compose up -d
wait_healthy

echo "== postgres serves TLS only, and a certificate near its end is reissued by up"
tls_checks
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
# certificates and public keys are public and skipped: cert-issuer logs every leaf it issues, and
# a leaf repeats its intermediate's name, so a line of the intermediate certificate can match by
# chance; a setup step writes public material with mode 0644 (spec 0013), and a certificate job
# writes the certificate of a binding to a *_cert file with mode 0600 (spec 0014), beside its
# *_key file, which stays checked
for f in $(find "$platform/secrets" "$platform/deployments" "$work/deployment-secrets" -path '*secrets/*' -type f ! -name '*.crt' ! -name '*_cert' ! -perm -0044); do
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
