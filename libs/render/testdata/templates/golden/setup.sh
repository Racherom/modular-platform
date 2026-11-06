#!/bin/sh
# Prepares the stack templates: the user it runs as, its secret files and its data folders. Run
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

# env_get NAME prints the value of NAME in .env, without the quotes setup.sh writes around it.
env_get() {
	sed -n "s/^$1=//p" .env 2>/dev/null | tail -n 1 | sed "s/^'\(.*\)'\$/\1/"
}

# check_env checks every MP_<instance>__<value> line already in .env by that value's rules, before
# anything is asked or created, so a value you edited by hand never reaches a module unchecked.
check_env() {
	[ -f .env ] || return 0
	for var in $(sed -n 's/^\(MP_[A-Z0-9_]*\)=.*/\1/p' .env | awk '!seen[$0]++'); do
		case $var in
		MP_ALPHA__*) echo "setup.sh: warning: $var in .env names no value of alpha" >&2 ;;
		MP_BETA__*) echo "setup.sh: warning: $var in .env names no value of beta" >&2 ;;
		MP_GATEWAY__*) echo "setup.sh: warning: $var in .env names no value of gateway" >&2 ;;
		MP_WHOAMI__*) echo "setup.sh: warning: $var in .env names no value of whoami" >&2 ;;
		esac
	done
}
check_env

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

new_secret secrets/alpha/key
new_secret secrets/beta/key
new_secret secrets/alpha/uses_hooks_token
new_secret secrets/beta/uses_hooks_token
