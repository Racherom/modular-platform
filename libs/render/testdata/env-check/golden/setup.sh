#!/bin/sh
# Prepares the stack envcheck: the user it runs as, its secret files and its data folders. Run
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
		MP_INGRESS__BANNER) check_value MP_INGRESS__BANNER 'string' '' '' '' false ;;
		MP_INGRESS__NOTE) check_value MP_INGRESS__NOTE 'string' '' '' '' true ;;
		MP_INGRESS__PORT) check_value MP_INGRESS__PORT 'int' '1' '65535' '' false ;;
		MP_INGRESS__TLS_MODE) check_value MP_INGRESS__TLS_MODE 'enum' '' '' '' false 'internal' 'acme' ;;
		MP_INGRESS__*) echo "setup.sh: warning: $var in .env names no value of ingress" >&2 ;;
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

echo "This stack needs no secret files."
