#!/bin/sh
# Creates the secret files of the stack sdk-pair. Run it once before `docker compose up`.
# It never changes a file that already exists, so running it again is safe.
set -eu
cd "$(dirname "$0")"
umask 077

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

new_secret secrets/consumer/uses_greet
new_secret secrets/provider/signing-key

# The modules built on the module SDK run as you, so they can read the secret files above.
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
