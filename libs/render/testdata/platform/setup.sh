#!/bin/sh
# Prepares the stack modular-platform: the user it runs as, the values it asks you, its secret files, its setup steps and its data folders. Run
# it once before `docker compose up`. It never changes a file that already exists, so running it
# again is safe.
set -eu
cd "$(dirname "$0")"
umask 077

# Every module runs as you, so it can read its secret files and write its data folders.
set_id() {
	if grep -q "^$1=" .env 2>/dev/null; then
		echo "kept    $1 in .env"
		return 0
	fi
	printf '%s=%s\n' "$1" "$2" >> .env
	echo "added   $1 to .env"
}
set_id MP_UID "$(id -u)"
set_id MP_GID "$(id -g)"

# The install folder, as an absolute path: the platform mounts its deployments folder at the same
# path inside the deployer, so the host's Docker finds every file a deployment names.
case $(pwd) in
*"'"* | *\\* | *'$'*)
	echo "setup.sh: the path of this folder holds ', \\ or \$, which .env cannot keep; move the folder" >&2
	exit 1
	;;
esac
set_id MP_DIR "'$(pwd)'"

# env_get NAME prints the value of NAME in .env, without the quotes setup.sh writes around it.
env_get() {
	sed -n "s/^$1=//p" .env 2>/dev/null | tail -n 1 | sed "s/^'\(.*\)'\$/\1/"
}

# valid TYPE VALUE MIN MAX PATTERN [VALUES...] checks an answer by the value's type and rules,
# as the module itself does; a failure sets $why.
valid() {
	type=$1 value=$2 min=$3 max=$4 pattern=$5
	shift 5
	case $type in
	string)
		if [ -n "$pattern" ] && ! printf '%s\n' "$value" | LC_ALL=C grep -Eq -e "$(printf "$pattern")"; then
			why="does not match the pattern the module needs"
			return 1
		fi
		;;
	int)
		if ! printf '%s\n' "$value" | LC_ALL=C grep -Eq '^[-+]?[0-9]+$'; then
			why="is not a whole number"
			return 1
		fi
		value=${value#+}
		if [ -n "$min" ] && ! [ "$value" -ge "$min" ] 2>/dev/null; then
			why="is below the minimum $min"
			return 1
		fi
		if [ -n "$max" ] && ! [ "$value" -le "$max" ] 2>/dev/null; then
			why="is above the maximum $max"
			return 1
		fi
		;;
	bool)
		case $value in
		true | false) ;;
		*)
			why="is not true or false"
			return 1
			;;
		esac
		;;
	url)
		if ! printf '%s\n' "$value" | LC_ALL=C grep -Eq '^[A-Za-z][A-Za-z0-9+.-]*://[^/?#]+'; then
			why="is not an absolute URL with a scheme and a host"
			return 1
		fi
		;;
	enum)
		for allowed; do
			[ "$value" = "$allowed" ] && return 0
		done
		why="is not one of: $*"
		return 1
		;;
	esac
	return 0
}

# check_env checks every MP_<instance>__<value> line already in .env by that value's rules, before
# anything is asked or created, so a value you edited by hand never reaches a module unchecked.
check_env() {
	[ -f .env ] || return 0
	for var in $(sed -n 's/^\(MP_[A-Z0-9_]*\)=.*/\1/p' .env | awk '!seen[$0]++'); do
		case $var in
		MP_API__PROXY_HOPS) ;;
		MP_API__SESSION_IDLE) ;;
		MP_API__SESSION_MAX) ;;
		MP_AUTHELIA__ADMIN_EMAIL) check_value MP_AUTHELIA__ADMIN_EMAIL 'string' '' '' '^((([]\001-\010\013\016-\037!-,.-?A-\\_-\177^-]|[\302-\364][\200-\277]{1,3}))+@(([]\001-\010\013\016-\037!-,.-?A-\\_-\177^-]|[\302-\364][\200-\277]{1,3}))+)$' false ;;
		MP_DEPLOYER_DOCKER__DEPLOYMENTS_DIR) ;;
		MP_DEPLOYER_DOCKER__PLATFORM_GID) ;;
		MP_DEPLOYER_DOCKER__PLATFORM_UID) ;;
		MP_DEPLOYER_DOCKER__PULL_TIMEOUT) ;;
		MP_DEPLOYER_DOCKER__WAIT_TIMEOUT) ;;
		MP_INGRESS__ACME_EMAIL) check_value MP_INGRESS__ACME_EMAIL 'string' '' '' '' true ;;
		MP_INGRESS__BASE_PATH) check_value MP_INGRESS__BASE_PATH 'string' '' '' '^(((/([.0-9A-Z_a-z~-])+))+)$' true ;;
		MP_INGRESS__DOMAIN) check_value MP_INGRESS__DOMAIN 'string' '' '' '^(([0-9a-z-])+((\\.([0-9a-z-])+))+)$' false ;;
		MP_INGRESS__HTTP_PORT) check_value MP_INGRESS__HTTP_PORT 'int' '1' '65535' '' false ;;
		MP_INGRESS__HTTPS_PORT) check_value MP_INGRESS__HTTPS_PORT 'int' '1' '65535' '' false ;;
		MP_INGRESS__ORIGIN) check_value MP_INGRESS__ORIGIN 'string' '' '' '^(http(s)?://(([]\001-\011\013-,.-\\_-\177^-]|[\302-\364][\200-\277]{1,3}))+)$' false ;;
		MP_INGRESS__TLS_MODE) check_value MP_INGRESS__TLS_MODE 'enum' '' '' '' false 'internal' 'acme' ;;
		MP_PLANNER__PLANNER_MODE) ;;
		MP_PLANNER__PLANNER_RATE) ;;
		MP_PLANNER__PROXY_HOPS) ;;
		MP_WEB__SIGNED_OUT) check_value MP_WEB__SIGNED_OUT 'enum' '' '' '' false 'index' 'sign-in' ;;
		MP_API__*) echo "setup.sh: warning: $var in .env names no value of api" >&2 ;;
		MP_AUTHELIA__*) echo "setup.sh: warning: $var in .env names no value of authelia" >&2 ;;
		MP_CERT_ISSUER__*) echo "setup.sh: warning: $var in .env names no value of cert-issuer" >&2 ;;
		MP_DEPLOYER_DOCKER__*) echo "setup.sh: warning: $var in .env names no value of deployer-docker" >&2 ;;
		MP_INGRESS__*) echo "setup.sh: warning: $var in .env names no value of ingress" >&2 ;;
		MP_PLANNER__*) echo "setup.sh: warning: $var in .env names no value of planner" >&2 ;;
		MP_POSTGRES__*) echo "setup.sh: warning: $var in .env names no value of postgres" >&2 ;;
		MP_WEB__*) echo "setup.sh: warning: $var in .env names no value of web" >&2 ;;
		esac
	done
}

# check_value VAR TYPE MIN MAX PATTERN OPTIONAL [VALUES...] stops setup.sh when the last VAR line
# of .env breaks the value's rules; an empty value passes only when the value is optional.
check_value() {
	var=$1 rule_type=$2 rule_min=$3 rule_max=$4 rule_pattern=$5 optional=$6
	shift 6
	value=$(env_get "$var")
	if [ -z "$value" ]; then
		[ "$optional" = true ] && return 0
		echo "setup.sh: $var in .env: the answer is empty" >&2
		exit 1
	fi
	if ! valid "$rule_type" "$value" "$rule_min" "$rule_max" "$rule_pattern" "$@"; then
		echo "setup.sh: $var in .env: the answer $why" >&2
		exit 1
	fi
}
check_env

# A question is asked once: an answer already in .env, or a secret file that exists, is kept.
# With answers piped in, one per line, a wrong answer stops setup.sh; on a terminal it asks again.
retry_or_stop() {
	if [ ! -t 0 ]; then
		echo "setup.sh: $1: the answer $2" >&2
		exit 1
	fi
	echo "The answer $2; try again." >&2
}

# read_answer PROMPT SECRET reads one line into $answer, without echo for a secret.
read_answer() {
	printf '%s: ' "$1" >&2
	if [ "$2" = true ] && [ -t 0 ]; then
		stty -echo
		trap 'stty echo' EXIT
	fi
	answer=
	IFS= read -r answer || true
	if [ "$2" = true ] && [ -t 0 ]; then
		stty echo
		trap - EXIT
		echo >&2
	fi
}

# ask VAR TYPE MIN MAX PATTERN OPTIONAL DESCRIPTION DEFAULT [VALUES...] asks a value and writes
# it into .env as VAR. An empty answer takes the default.
ask() {
	var=$1 type=$2 min=$3 max=$4 pattern=$5 optional=$6 description=$7 default=$8
	shift 8
	if grep -q "^$var=" .env 2>/dev/null; then
		echo "kept    $var in .env"
		return 0
	fi
	while :; do
		if [ -n "$default" ]; then
			read_answer "$description [$default]" false
		else
			read_answer "$description" false
		fi
		[ -n "$answer" ] || answer=$default
		if [ -z "$answer" ]; then
			[ "$optional" = true ] && break
			retry_or_stop "$var" "is required"
			continue
		fi
		case $answer in
		*"'"* | *\\*)
			retry_or_stop "$var" "holds ' or \\, which .env cannot keep"
			continue
			;;
		esac
		if valid "$type" "$answer" "$min" "$max" "$pattern" "$@"; then
			break
		fi
		retry_or_stop "$var" "$why"
	done
	printf "%s='%s'\n" "$var" "$answer" >> .env
	echo "added   $var to .env"
}

# ask_secret PATH OPTIONAL DESCRIPTION asks a secret without echo and writes it into PATH.
ask_secret() {
	if [ -e "$1" ]; then
		echo "kept    $1"
		return 0
	fi
	while :; do
		read_answer "$3" true
		if [ -n "$answer" ] || [ "$2" = true ]; then
			break
		fi
		retry_or_stop "$1" "is required"
	done
	mkdir -p "$(dirname "$1")"
	(set -C && printf '%s' "$answer" > "$1")
	echo "created $1"
}

# Values the modules ask you, written into .env.
ask MP_INGRESS__DOMAIN 'string' '' '' '^(([0-9a-z-])+((\\.([0-9a-z-])+))+)$' false 'The domain the platform answers on, with at least one dot (Authelia needs one).' ""
ask MP_INGRESS__ORIGIN 'string' '' '' '^(http(s)?://(([]\001-\011\013-,.-\\_-\177^-]|[\302-\364][\200-\277]{1,3}))+)$' false 'Scheme, host and port the browser sees.' "https://$(env_get MP_INGRESS__DOMAIN)"
ask MP_AUTHELIA__ADMIN_EMAIL 'string' '' '' '^((([]\001-\010\013\016-\037!-,.-?A-\\_-\177^-]|[\302-\364][\200-\277]{1,3}))+@(([]\001-\010\013\016-\037!-,.-?A-\\_-\177^-]|[\302-\364][\200-\277]{1,3}))+)$' false 'Email of the admin (the first verified sign in with it becomes the admin).' ""

new_secret() {
	if [ -e "$1" ]; then
		echo "kept    $1"
		return 0
	fi
	mkdir -p "$(dirname "$1")"
	value=$(od -An -tx1 -N32 /dev/urandom | tr -d ' \n')
	if [ "${#value}" -ne 64 ]; then
		echo "setup.sh: could not read random bytes for $1" >&2
		exit 1
	fi
	# noclobber: the shell refuses to replace a file another run created meanwhile
	(set -C && printf '%s' "$value" > "$1")
	echo "created $1"
}

# A file a provision job writes is made empty, mode 0600, so Compose finds it at the first up; the job
# fills it in place at every up.
new_output() {
	if [ -e "$1" ]; then
		echo "kept    $1"
		return 0
	fi
	mkdir -p "$(dirname "$1")"
	(set -C && : > "$1")
	echo "created $1"
}

new_secret secrets/authelia/jwt-secret
new_secret secrets/authelia/oidc-hmac
new_secret secrets/authelia/session-secret
new_secret secrets/authelia/storage-key
new_secret secrets/postgres/password
new_secret secrets/api/uses_deploy-target_token
new_secret secrets/api/uses_main-db_password
new_secret secrets/api/uses_oidc_client_secret
new_secret secrets/api/uses_planner_token
new_secret secrets/authelia/uses_db_password
new_output secrets/postgres/uses_tls_cert
new_output secrets/postgres/uses_tls_key

# Setup steps run once each, in the module's own image, as you and with no network. A step whose
# files all exist is skipped; one that fails, or leaves a file missing, stops setup.sh.
missing() {
	for path; do
		[ -e "$path" ] || return 0
	done
	return 1
}

need_read() {
	if [ ! -e "$1" ]; then
		echo "setup.sh: $1 is missing, but a setup step reads it" >&2
		exit 1
	fi
}

# step_ask DESCRIPTION SECRET adds one answer, on a line of its own, to $answers.
step_ask() {
	read_answer "$1" "$2"
	answers="$answers$answer
"
}

# wrote MODE PATH stops setup.sh when the step left PATH missing, else gives it MODE.
wrote() {
	if [ ! -e "$2" ]; then
		echo "setup.sh: the setup step left $2 missing" >&2
		exit 1
	fi
	chmod "$1" "$2"
	echo "created $2"
}

step_folder() {
	if [ ! -d "$1" ]; then
		mkdir -p "$1"
		chmod 0700 "$1"
	fi
}

# Setup step ca of cert-issuer
if missing secrets/cert-issuer/password secrets/cert-issuer/root-ca secrets/cert-issuer/root-ca-key secrets/cert-issuer/intermediate-ca secrets/cert-issuer/intermediate-ca-key; then
	echo "running setup step ca of cert-issuer"
	step_folder secrets/cert-issuer
	docker run --rm -i --network none \
		--user "$(id -u):$(id -g)" \
		-v "$PWD/secrets/cert-issuer:/out" \
		-e 'MP_GRAPH=modular-platform' \
		-e 'MP_INSTANCE=cert-issuer' \
		-e 'STEPPATH=/tmp/step' \
		--entrypoint 'sh' \
		'smallstep/step-ca:0.30.2' '-c' 'set -eu
umask 077
od -An -tx1 -N32 /dev/urandom | tr -d '\'' \n'\'' > /out/password
step ca init --pki --name "Modular Platform internal CA" --password-file /out/password >/dev/null 2>&1
cp "$STEPPATH/certs/root_ca.crt" /out/root-ca
cp "$STEPPATH/secrets/root_ca_key" /out/root-ca-key
cp "$STEPPATH/certs/intermediate_ca.crt" /out/intermediate-ca
cp "$STEPPATH/secrets/intermediate_ca_key" /out/intermediate-ca-key
' </dev/null
	wrote 0600 secrets/cert-issuer/password
	wrote 0644 secrets/cert-issuer/root-ca
	wrote 0600 secrets/cert-issuer/root-ca-key
	wrote 0644 secrets/cert-issuer/intermediate-ca
	wrote 0600 secrets/cert-issuer/intermediate-ca-key
else
	echo "kept    the files of setup step ca of cert-issuer"
fi

# Setup step config of cert-issuer
if missing secrets/cert-issuer/jobs-password secrets/cert-issuer/ca-json; then
	echo "running setup step config of cert-issuer"
	step_folder secrets/cert-issuer
	docker run --rm -i --network none \
		--user "$(id -u):$(id -g)" \
		-v "$PWD/secrets/cert-issuer:/out" \
		-e 'MP_GRAPH=modular-platform' \
		-e 'MP_INSTANCE=cert-issuer' \
		-e 'STEPPATH=/tmp/step' \
		--entrypoint 'sh' \
		'smallstep/step-ca:0.30.2' '-c' 'set -eu
umask 077
od -An -tx1 -N32 /dev/urandom | tr -d '\'' \n'\'' > /out/jobs-password
step crypto jwk create /tmp/jobs.pub /tmp/jobs.key --kty EC --crv P-256 --use sig --password-file /out/jobs-password >/dev/null 2>&1
key=$(step crypto jose format < /tmp/jobs.key)
pub=$(tr -d '\''\n'\'' < /tmp/jobs.pub)
cat > /out/ca-json <<JSON
{
  "root": "/home/step/certs/root_ca.crt",
  "crt": "/home/step/certs/intermediate_ca.crt",
  "key": "/home/step/secrets/intermediate_ca_key",
  "address": ":9000",
  "dnsNames": ["$MP_INSTANCE.$MP_GRAPH.internal", "localhost"],
  "logger": {"format": "json"},
  "db": {"type": "badgerv2", "dataSource": "/home/step/db"},
  "authority": {
    "policy": {"x509": {"allow": {"dns": ["*.$MP_GRAPH.internal"]}}},
    "claims": {"minTLSCertDuration": "24h", "maxTLSCertDuration": "24h", "defaultTLSCertDuration": "24h"},
    "provisioners": [
      {"type": "ACME", "name": "acme", "challenges": ["tls-alpn-01"]},
      {"type": "JWK", "name": "jobs", "key": $pub, "encryptedKey": "$key",
       "claims": {"maxTLSCertDuration": "2160h", "defaultTLSCertDuration": "2160h"}}
    ]
  },
  "tls": {"minVersion": 1.3, "maxVersion": 1.3}
}
JSON
' </dev/null
	wrote 0600 secrets/cert-issuer/jobs-password
	wrote 0600 secrets/cert-issuer/ca-json
else
	echo "kept    the files of setup step config of cert-issuer"
fi

# Setup step users of authelia
if missing secrets/authelia/users; then
	echo "running setup step users of authelia"
	step_folder secrets/authelia
	answers=
	step_ask 'Admin user name (you sign in with it)' false
	step_ask 'Admin password (at least 12 characters, not shown)' true
	printf '%s' "$answers" | docker run --rm -i --network none \
		--user "$(id -u):$(id -g)" \
		-v "$PWD/secrets/authelia:/out" \
		-e 'MP_GRAPH=modular-platform' \
		-e 'MP_INSTANCE=authelia' \
		-e "MP_ASK_ADMIN_EMAIL=$(env_get MP_AUTHELIA__ADMIN_EMAIL)" \
		--entrypoint 'sh' \
		'authelia/authelia:4.39.28' '-c' 'set -eu
IFS= read -r user
IFS= read -r pass
case $user in
*[!a-z0-9._-]* | "") echo "the admin user name may hold only a to z, 0 to 9, dot, underscore and hyphen" >&2; exit 1 ;;
esac
if [ "${#pass}" -lt 12 ]; then echo "the admin password must have at least 12 characters" >&2; exit 1; fi
hash=$(authelia crypto hash generate argon2 --password "$pass" | sed -n '\''s/^Digest: //p'\'')
[ -n "$hash" ]
printf "users:\n  %s:\n    displayname: %s\n    password: '\''%s'\''\n    email: %s\n" "$user" "$user" "$hash" "$MP_ASK_ADMIN_EMAIL" > /out/users
'
	wrote 0600 secrets/authelia/users
else
	echo "kept    the files of setup step users of authelia"
fi

# Setup step signing-key of authelia
if missing secrets/authelia/signing-key; then
	echo "running setup step signing-key of authelia"
	step_folder secrets/authelia
	docker run --rm -i --network none \
		--user "$(id -u):$(id -g)" \
		-v "$PWD/secrets/authelia:/out" \
		-e 'MP_GRAPH=modular-platform' \
		-e 'MP_INSTANCE=authelia' \
		-e "MP_ASK_ADMIN_EMAIL=$(env_get MP_AUTHELIA__ADMIN_EMAIL)" \
		'authelia/authelia:4.39.28' 'authelia' 'crypto' 'pair' 'rsa' 'generate' '--bits' '2048' '--directory' '/out' '--file.private-key' 'signing-key' '--file.public-key' 'signing-key.pub' </dev/null
	wrote 0600 secrets/authelia/signing-key
else
	echo "kept    the files of setup step signing-key of authelia"
fi

# Setup step client-digest of authelia
step_folder secrets/authelia/client-digests
if missing secrets/authelia/client-digests/api; then
	echo "running setup step client-digest of authelia for api"
	step_folder secrets/authelia
	need_read secrets/api/uses_oidc_client_secret
	docker run --rm -i --network none \
		--user "$(id -u):$(id -g)" \
		-v "$PWD/secrets/authelia:/out" \
		-v "$PWD/secrets/api/uses_oidc_client_secret:/in/oidc/client_secret:ro" \
		-e 'MP_GRAPH=modular-platform' \
		-e 'MP_INSTANCE=authelia' \
		-e "MP_ASK_ADMIN_EMAIL=$(env_get MP_AUTHELIA__ADMIN_EMAIL)" \
		-e 'CLIENT=api' \
		--entrypoint 'sh' \
		'authelia/authelia:4.39.28' '-c' 'set -eu
digest=$(authelia crypto hash generate pbkdf2 --variant sha512 --password "$(cat /in/oidc/client_secret)" | sed -n '\''s/^Digest: //p'\'')
[ -n "$digest" ]
printf '\''%s'\'' "$digest" > "/out/client-digests/$CLIENT"
' </dev/null
	wrote 0600 secrets/authelia/client-digests/api
else
	echo "kept    the files of setup step client-digest of authelia for api"
fi

# Setup step caller-key of api
if missing secrets/api/caller-signing-key secrets/api/caller-public-key; then
	echo "running setup step caller-key of api"
	step_folder secrets/api
	docker run --rm -i --network none \
		--user "$(id -u):$(id -g)" \
		-v "$PWD/secrets/api:/out" \
		-e 'MP_GRAPH=modular-platform' \
		-e 'MP_INSTANCE=api' \
		'ghcr.io/racherom/modular-platform/api:0.0.0-test' 'caller-key' 'generate' </dev/null
	wrote 0600 secrets/api/caller-signing-key
	wrote 0644 secrets/api/caller-public-key
else
	echo "kept    the files of setup step caller-key of api"
fi

new_folder() {
	if [ -d "$1" ]; then
		echo "kept    $1"
		return 0
	fi
	mkdir -p "$1"
	chmod 0700 "$1"
	echo "created $1"
}

new_folder data/cert-issuer/db
new_folder data/ingress/config
new_folder data/ingress/data
new_folder data/ingress/data/caddy/pki/authorities/local
new_folder data/postgres/data
new_folder deployments

echo
echo "Done. Start the platform with: docker compose up -d --wait"
echo "Then open the origin you gave and sign in as the admin."
