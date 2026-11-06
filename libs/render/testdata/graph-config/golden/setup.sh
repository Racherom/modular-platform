#!/bin/sh
# Prepares the stack graphconfig: the user it runs as, the values it asks you, its secret files, its setup steps and its data folders. Run
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

# env_or NAME TEXT prints the value of NAME in .env, or TEXT, the graph's value, when .env has no
# NAME line.
env_or() {
	if grep -q "^$1=" .env 2>/dev/null; then
		env_get "$1"
	else
		printf '%s\n' "$2"
	fi
}

# graph_set VAR TEXT says that the graph answers the asked value VAR with TEXT, or that a VAR line
# in .env with another value overrides it. It writes nothing: a later graph change reaches the
# stack on the next render.
graph_set() {
	if grep -q "^$1=" .env 2>/dev/null && [ "$(env_get "$1")" != "$2" ]; then
		printf '.env overrides the graph  %s\n' "$1"
		return 0
	fi
	printf 'set by the graph  %s=%s\n' "$1" "$2"
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
		MP_TALLY__GREETING) ;;
		MP_TALLY__LEVEL) ;;
		MP_TALLY__NOTE) ;;
		MP_TELLER__BANNER) check_value MP_TELLER__BANNER 'string' '' '' '' false ;;
		MP_TELLER__DOMAIN) check_value MP_TELLER__DOMAIN 'string' '' '' '^([0-9a-z]((([.0-9a-z-])*[0-9a-z]))?)$' false ;;
		MP_TELLER__LEVEL) check_value MP_TELLER__LEVEL 'enum' '' '' '' false 'debug' 'info' ;;
		MP_TELLER__MODE) check_value MP_TELLER__MODE 'enum' '' '' '' false 'strict' 'relaxed' ;;
		MP_TELLER__MOTD) check_value MP_TELLER__MOTD 'string' '' '' '' true ;;
		MP_TELLER__MOTTO) check_value MP_TELLER__MOTTO 'string' '' '' '' true ;;
		MP_TELLER__NOTE) check_value MP_TELLER__NOTE 'string' '' '' '' true ;;
		MP_TELLER__ORIGIN) check_value MP_TELLER__ORIGIN 'string' '' '' '' false ;;
		MP_TELLER__RUN_UID) ;;
		MP_TELLER__TAGLINE) check_value MP_TELLER__TAGLINE 'string' '' '' '' true ;;
		MP_TALLY__*) echo "setup.sh: warning: $var in .env names no value of tally" >&2 ;;
		MP_TELLER__*) echo "setup.sh: warning: $var in .env names no value of teller" >&2 ;;
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

# Values the modules ask you, written into .env, unless the graph answers them.
graph_set MP_TELLER__DOMAIN 'example.org'
ask MP_TELLER__MODE 'enum' '' '' '' false 'How strictly the teller checks clients.' "strict" 'strict' 'relaxed'
graph_set MP_TELLER__MOTTO '$x'
graph_set MP_TELLER__NOTE 'a'\''b'
ask MP_TELLER__ORIGIN 'string' '' '' '' false 'Origin clients reach the teller at.' "https://$(env_or MP_TELLER__DOMAIN 'example.org')"
graph_set MP_TELLER__TAGLINE 'a #b'

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

# Setup step hello of teller
if missing secrets/teller/hello; then
	echo "running setup step hello of teller"
	step_folder secrets/teller
	docker run --rm -i --network none \
		--user "$(id -u):$(id -g)" \
		-v "$PWD/secrets/teller:/out" \
		-e 'MP_GRAPH=graphconfig' \
		-e 'MP_INSTANCE=teller' \
		-e "MP_ASK_DOMAIN=$(env_or MP_TELLER__DOMAIN 'example.org')" \
		-e "MP_ASK_MODE=$(env_get MP_TELLER__MODE)" \
		-e "MP_ASK_MOTTO=$(env_or MP_TELLER__MOTTO '$x')" \
		-e "MP_ASK_NOTE=$(env_or MP_TELLER__NOTE 'a'\''b')" \
		-e "MP_ASK_ORIGIN=$(env_get MP_TELLER__ORIGIN)" \
		-e "MP_ASK_TAGLINE=$(env_or MP_TELLER__TAGLINE 'a #b')" \
		--entrypoint 'sh' \
		'traefik/whoami:v1.11.0' '-c' 'printf "%s|%s|%s|%s|%s\n" "$MP_ASK_DOMAIN" "$MP_ASK_ORIGIN" "$MP_ASK_NOTE" "$MP_ASK_MOTTO" "$MP_ASK_TAGLINE" > /out/hello' </dev/null
	wrote 0600 secrets/teller/hello
else
	echo "kept    the files of setup step hello of teller"
fi
