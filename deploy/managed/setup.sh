#!/bin/sh
# Prepares this folder to run the managed platform: asks four questions, writes .env and creates
# every secret in secrets/ (spec 0006, child 0009). It needs only Docker. It never overwrites a
# file and never prints a secret, so running it again changes nothing.
#
# When stdin is not a terminal it reads the answers one per line, in the order asked: domain,
# admin email, admin user name, admin password.
set -eu

cd "$(dirname "$0")"
PLATFORM_DIR=$(pwd)
STEP_IMAGE=smallstep/step-ca:0.30.2
AUTHELIA_IMAGE=authelia/authelia:4.39.28
umask 077

say() { printf '%s\n' "$*" >&2; }
fail() { say "setup.sh: $*"; exit 1; }

command -v docker >/dev/null 2>&1 || fail "Docker is not installed or not on your PATH."

# ask PROMPT DEFAULT: one answer, from the terminal or the next line of stdin
ask() {
	if [ -t 0 ]; then
		printf '%s' "$1${2:+ [$2]}: " >&2
	fi
	IFS= read -r answer || answer=""
	[ -n "$answer" ] || answer=$2
	printf '%s' "$answer"
}

# ask_secret PROMPT: like ask, without echo on a terminal
ask_secret() {
	if [ -t 0 ]; then
		printf '%s: ' "$1" >&2
		stty -echo
		IFS= read -r answer || answer=""
		stty echo
		printf '\n' >&2
	else
		IFS= read -r answer || answer=""
	fi
	printf '%s' "$answer"
}

random_hex() { od -An -tx1 -N32 /dev/urandom | tr -d ' \n'; }

# new_file PATH: true when PATH does not exist yet; existing files are never touched
new_file() { [ ! -e "$1" ]; }

# write_secret PATH: stdin into PATH, mode 0600, unless PATH exists
write_secret() {
	if new_file "$1"; then
		mkdir -p "$(dirname "$1")"
		chmod 700 "$(dirname "$1")"
		cat > "$1"
		chmod 600 "$1"
	else
		cat > /dev/null
	fi
}

random_secret() { random_hex | write_secret "$1"; }

# fragment MODULE IMAGE ARGS...: runs a command in a module's own image as you, with only that
# module's secrets folder mounted read write at /out (spec 0001, principle 9). It reads stdin, so
# give it its input explicitly, or </dev/null: the answers to later questions wait there.
fragment() {
	module=$1 image=$2
	shift 2
	mkdir -p "secrets/$module"
	chmod 700 "secrets/$module"
	docker run --rm -i --network none --user "$(id -u):$(id -g)" \
		-v "$PLATFORM_DIR/secrets/$module:/out" "$image" "$@"
}

mkdir -p secrets
chmod 700 secrets

# 1 and 2: the platform questions and .env
if new_file .env; then
	domain=$(ask "Domain the platform answers on" "platform.localhost")
	case $domain in
		*.*) ;;
		*) fail "the domain must contain a dot, like platform.localhost (Authelia requires one)." ;;
	esac
	email=$(ask "Admin email (the first verified sign in with it becomes the admin)" "")
	case $email in
		?*@?*) ;;
		*) fail "the admin email must look like name@example.com." ;;
	esac
	https_port=443
	origin="https://$domain"
	cat > .env <<EOF
# Settings of the managed platform, written by setup.sh. No secret ever goes here: secrets live
# in secrets/. See .env.example for what each line does.
PLATFORM_DOMAIN=$domain
PLATFORM_ORIGIN=$origin
PLATFORM_ADMIN_EMAIL=$email
PLATFORM_TLS=internal
PLATFORM_HTTP_PORT=80
PLATFORM_HTTPS_PORT=$https_port
PLATFORM_BASE_PATH=
PLATFORM_DIR=$PLATFORM_DIR
PLATFORM_UID=$(id -u)
PLATFORM_GID=$(id -g)
PLATFORM_SESSION_IDLE=1h
PLATFORM_SESSION_MAX=12h
PLATFORM_SIGNED_OUT=index
EOF
	chmod 644 .env
	say "Wrote .env."
fi

# 3: platform secrets, plain random
random_secret secrets/api/uses_planner
random_secret secrets/api/uses_deploy-target
random_secret secrets/api/oidc_client_secret

# 4: the cert-issuer fragment: the key pair api signs MP-Caller tokens with
if new_file secrets/api/caller_signing_key; then
	docker run --rm --network none --user "$(id -u):$(id -g)" -v "$PLATFORM_DIR/secrets/api:/out" \
		--entrypoint step "$STEP_IMAGE" crypto keypair /out/caller_public_key /out/caller_signing_key \
		--kty OKP --crv Ed25519 --no-password --insecure >/dev/null 2>&1 ||
		fail "creating the caller key pair failed."
	chmod 600 secrets/api/caller_public_key secrets/api/caller_signing_key
fi

# 5: the postgres fragment: the superuser password and one password per role
random_secret secrets/postgres/password
random_secret secrets/postgres/api_password
random_secret secrets/postgres/authelia_password

# 6: the authelia fragment
random_secret secrets/authelia/session_secret
random_secret secrets/authelia/storage_encryption_key
random_secret secrets/authelia/jwt_secret
random_secret secrets/authelia/oidc_hmac_secret
if new_file secrets/authelia/oidc_signing_key; then
	fragment authelia "$AUTHELIA_IMAGE" authelia crypto pair rsa generate --bits 2048 --directory /out \
		--file.private-key oidc_signing_key --file.public-key oidc_signing_key.pub </dev/null >/dev/null ||
		fail "creating Authelia's OIDC signing key failed."
fi
if new_file secrets/authelia/api_client_digest; then
	# the client secret goes in on stdin; the fragment hands it to Authelia's hasher
	fragment authelia "$AUTHELIA_IMAGE" sh -c \
		'IFS= read -r s; authelia crypto hash generate pbkdf2 --variant sha512 --password "$s" | sed -n "s/^Digest: //p"' \
		< secrets/api/oidc_client_secret | write_secret secrets/authelia/api_client_digest
	[ -s secrets/authelia/api_client_digest ] || fail "hashing the api client secret failed."
fi
if new_file secrets/authelia/users.yml; then
	# the admin's email comes from .env, so a second run with a missing users.yml still matches it
	email=$(sed -n 's/^PLATFORM_ADMIN_EMAIL=//p' .env)
	user=$(ask "Admin user name (you sign in with it)" "admin")
	case $user in
		*[!a-z0-9._-]* | "") fail "the user name may hold only a to z, 0 to 9, dot, underscore and hyphen." ;;
	esac
	password=$(ask_secret "Admin password (at least 12 characters, not shown)")
	[ "${#password}" -ge 12 ] || fail "the password must have at least 12 characters."
	hash=$(printf '%s\n' "$password" | docker run --rm -i --network none --user "$(id -u):$(id -g)" "$AUTHELIA_IMAGE" \
		sh -c 'IFS= read -r p; authelia crypto hash generate argon2 --password "$p" | sed -n "s/^Digest: //p"')
	password=""
	[ -n "$hash" ] || fail "hashing the admin password failed."
	write_secret secrets/authelia/users.yml <<EOF
users:
  $user:
    displayname: $user
    password: '$hash'
    email: $email
EOF
fi

# 7: the deployments folder, owned by you; the deployer writes each deployment below it
mkdir -p deployments
chmod 700 deployments

origin=$(sed -n 's/^PLATFORM_ORIGIN=//p' .env)
say ""
say "Done. Start the platform with:"
say "  docker compose up -d"
say "then open $origin/ and sign in."
