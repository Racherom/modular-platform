#!/bin/sh
# Prepares the stack install: the user it runs as, the values it asks you, its secret files, its setup steps and its data folders. Run
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

# nanoseconds DURATION prints a duration like 1m30s in nanoseconds, or fails.
nanoseconds() {
	printf '%s\n' "$1" | LC_ALL=C awk '{
		s = $0; sign = 1
		if (s ~ /^[-+]/) { if (substr(s, 1, 1) == "-") sign = -1; s = substr(s, 2) }
		if (s == "0") { print 0; exit 0 }
		if (s == "") exit 1
		total = 0
		while (s != "") {
			if (!match(s, /^([0-9]+(\.[0-9]*)?|\.[0-9]+)/)) exit 1
			n = substr(s, 1, RLENGTH); s = substr(s, RLENGTH + 1)
			if (match(s, /^(ns|us|ms|h|m|s)/)) { u = substr(s, 1, RLENGTH) }
			else if (substr(s, 1, 3) == "\302\265s" || substr(s, 1, 3) == "\316\274s") { u = "us"; RLENGTH = 3 }
			else exit 1
			s = substr(s, RLENGTH + 1)
			total += n * (u == "ns" ? 1 : u == "us" ? 1e3 : u == "ms" ? 1e6 : u == "s" ? 1e9 : u == "m" ? 6e10 : 3.6e12)
		}
		printf "%.0f\n", sign * total
	}'
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
	duration)
		if ! ns=$(nanoseconds "$value"); then
			why="is not a duration like 10s"
			return 1
		fi
		if [ -n "$min" ] && awk -v a="$ns" -v b="$min" 'BEGIN { exit !(a + 0 < b + 0) }'; then
			why="is below the minimum"
			return 1
		fi
		if [ -n "$max" ] && awk -v a="$ns" -v b="$max" 'BEGIN { exit !(a + 0 > b + 0) }'; then
			why="is above the maximum"
			return 1
		fi
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
ask MP_KEEPER__DOMAIN 'string' '' '' '^([0-9a-z]((([.0-9a-z-])*[0-9a-z]))?)$' false 'Domain the vault answers on.' ""
ask MP_KEEPER__MODE 'enum' '' '' '' false 'How strictly the vault checks clients.' "strict" 'strict' 'relaxed'
ask MP_KEEPER__NOTE 'string' '' '' '' true 'A note shown on the vault'\''s page.' ""
ask MP_KEEPER__ORIGIN 'string' '' '' '' false 'Origin clients reach the vault at.' "https://$(env_get MP_KEEPER__DOMAIN)"
ask MP_KEEPER__PORT 'int' '1' '65535' '' false 'Port the vault listens on.' "8200"
ask MP_KEEPER__TIMEOUT 'duration' '1000000000' '3600000000000' '' false 'How long a request may take.' "30s"
ask_secret secrets/keeper/unseal-key false 'Key that unseals the vault.'

# Secrets the modules ask you, written into their files.
ask_secret secrets/keeper/license false 'License key of the vault.'

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

new_secret secrets/keeper/master-key
new_secret secrets/alpha/uses_vault_client_secret
new_secret secrets/beta/uses_vault_client_secret

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

# Setup step root of keeper
if missing secrets/keeper/root secrets/keeper/root-key; then
	echo "running setup step root of keeper"
	step_folder secrets/keeper
	need_read secrets/keeper/master-key
	docker run --rm -i --network none \
		--user "$(id -u):$(id -g)" \
		-v "$PWD/secrets/keeper:/out" \
		-v "$PWD/secrets/keeper/master-key:/in/master-key:ro" \
		-e 'MP_GRAPH=install' \
		-e 'MP_INSTANCE=keeper' \
		-e "MP_ASK_DOMAIN=$(env_get MP_KEEPER__DOMAIN)" \
		-e "MP_ASK_MODE=$(env_get MP_KEEPER__MODE)" \
		-e "MP_ASK_NOTE=$(env_get MP_KEEPER__NOTE)" \
		-e "MP_ASK_ORIGIN=$(env_get MP_KEEPER__ORIGIN)" \
		-e "MP_ASK_PORT=$(env_get MP_KEEPER__PORT)" \
		-e "MP_ASK_TIMEOUT=$(env_get MP_KEEPER__TIMEOUT)" \
		--entrypoint 'sh' \
		'postgres:18.6' '-c' 'printf "root of %s on %s\n" "$MP_INSTANCE.$MP_GRAPH" "$MP_ASK_DOMAIN" > /out/root && head -c 16 /in/master-key > /out/root-key' </dev/null
	wrote 0644 secrets/keeper/root
	wrote 0600 secrets/keeper/root-key
else
	echo "kept    the files of setup step root of keeper"
fi

# Setup step admins of keeper
if missing secrets/keeper/admins; then
	echo "running setup step admins of keeper"
	step_folder secrets/keeper
	answers=
	step_ask 'User name of the vault admin.' false
	step_ask 'Password of the vault admin.' true
	printf '%s' "$answers" | docker run --rm -i --network none \
		--user "$(id -u):$(id -g)" \
		-v "$PWD/secrets/keeper:/out" \
		-e 'MP_GRAPH=install' \
		-e 'MP_INSTANCE=keeper' \
		-e "MP_ASK_DOMAIN=$(env_get MP_KEEPER__DOMAIN)" \
		-e "MP_ASK_MODE=$(env_get MP_KEEPER__MODE)" \
		-e "MP_ASK_NOTE=$(env_get MP_KEEPER__NOTE)" \
		-e "MP_ASK_ORIGIN=$(env_get MP_KEEPER__ORIGIN)" \
		-e "MP_ASK_PORT=$(env_get MP_KEEPER__PORT)" \
		-e "MP_ASK_TIMEOUT=$(env_get MP_KEEPER__TIMEOUT)" \
		--entrypoint 'sh' \
		'postgres:18.6' '-c' 'IFS= read -r user; IFS= read -r pass; printf "%s:%s\n" "$user" "$(printf %s "$pass" | sha256sum | cut -c1-16)" > /out/admins'
	wrote 0600 secrets/keeper/admins
else
	echo "kept    the files of setup step admins of keeper"
fi

# Setup step seal of keeper
step_folder secrets/keeper/sealed
if missing secrets/keeper/sealed/alpha; then
	echo "running setup step seal of keeper for alpha"
	step_folder secrets/keeper
	need_read secrets/alpha/uses_vault_client_secret
	need_read secrets/keeper/root
	docker run --rm -i --network none \
		--user "$(id -u):$(id -g)" \
		-v "$PWD/secrets/keeper:/out" \
		-v "$PWD/secrets/alpha/uses_vault_client_secret:/in/vault/client_secret:ro" \
		-v "$PWD/secrets/keeper/root:/in/root:ro" \
		-e 'MP_GRAPH=install' \
		-e 'MP_INSTANCE=keeper' \
		-e "MP_ASK_DOMAIN=$(env_get MP_KEEPER__DOMAIN)" \
		-e "MP_ASK_MODE=$(env_get MP_KEEPER__MODE)" \
		-e "MP_ASK_NOTE=$(env_get MP_KEEPER__NOTE)" \
		-e "MP_ASK_ORIGIN=$(env_get MP_KEEPER__ORIGIN)" \
		-e "MP_ASK_PORT=$(env_get MP_KEEPER__PORT)" \
		-e "MP_ASK_TIMEOUT=$(env_get MP_KEEPER__TIMEOUT)" \
		-e 'CLIENT=alpha' \
		-e 'GREETING=sealed for alpha' \
		--entrypoint 'sh' \
		'postgres:18.6' '-c' 'printf "%s %s %s\n" "$CLIENT" "${GREETING}" "$(sha256sum < /in/vault/client_secret | cut -c1-16)" > "/out/sealed/alpha"' </dev/null
	wrote 0600 secrets/keeper/sealed/alpha
else
	echo "kept    the files of setup step seal of keeper for alpha"
fi
if missing secrets/keeper/sealed/beta; then
	echo "running setup step seal of keeper for beta"
	step_folder secrets/keeper
	need_read secrets/beta/uses_vault_client_secret
	need_read secrets/keeper/root
	docker run --rm -i --network none \
		--user "$(id -u):$(id -g)" \
		-v "$PWD/secrets/keeper:/out" \
		-v "$PWD/secrets/beta/uses_vault_client_secret:/in/vault/client_secret:ro" \
		-v "$PWD/secrets/keeper/root:/in/root:ro" \
		-e 'MP_GRAPH=install' \
		-e 'MP_INSTANCE=keeper' \
		-e "MP_ASK_DOMAIN=$(env_get MP_KEEPER__DOMAIN)" \
		-e "MP_ASK_MODE=$(env_get MP_KEEPER__MODE)" \
		-e "MP_ASK_NOTE=$(env_get MP_KEEPER__NOTE)" \
		-e "MP_ASK_ORIGIN=$(env_get MP_KEEPER__ORIGIN)" \
		-e "MP_ASK_PORT=$(env_get MP_KEEPER__PORT)" \
		-e "MP_ASK_TIMEOUT=$(env_get MP_KEEPER__TIMEOUT)" \
		-e 'CLIENT=beta' \
		-e 'GREETING=sealed for beta' \
		--entrypoint 'sh' \
		'postgres:18.6' '-c' 'printf "%s %s %s\n" "$CLIENT" "${GREETING}" "$(sha256sum < /in/vault/client_secret | cut -c1-16)" > "/out/sealed/beta"' </dev/null
	wrote 0600 secrets/keeper/sealed/beta
else
	echo "kept    the files of setup step seal of keeper for beta"
fi

# Setup step trust of alpha
if missing secrets/alpha/trusted-root; then
	echo "running setup step trust of alpha"
	step_folder secrets/alpha
	need_read secrets/keeper/root
	docker run --rm -i --network none \
		--user "$(id -u):$(id -g)" \
		-v "$PWD/secrets/alpha:/out" \
		-v "$PWD/secrets/keeper/root:/in/vault/root:ro" \
		-e 'MP_GRAPH=install' \
		-e 'MP_INSTANCE=alpha' \
		'postgres:18.6' 'cp' '/in/vault/root' '/out/trusted-root' </dev/null
	wrote 0644 secrets/alpha/trusted-root
else
	echo "kept    the files of setup step trust of alpha"
fi

# Setup step trust of beta
if missing secrets/beta/trusted-root; then
	echo "running setup step trust of beta"
	step_folder secrets/beta
	need_read secrets/keeper/root
	docker run --rm -i --network none \
		--user "$(id -u):$(id -g)" \
		-v "$PWD/secrets/beta:/out" \
		-v "$PWD/secrets/keeper/root:/in/vault/root:ro" \
		-e 'MP_GRAPH=install' \
		-e 'MP_INSTANCE=beta' \
		'postgres:18.6' 'cp' '/in/vault/root' '/out/trusted-root' </dev/null
	wrote 0644 secrets/beta/trusted-root
else
	echo "kept    the files of setup step trust of beta"
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

new_folder data/keeper/data
