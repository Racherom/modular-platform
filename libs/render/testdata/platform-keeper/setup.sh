#!/bin/sh
# Installs the platform modular-platform and starts it from one container (spec 0016). Run it as root:
#
#	sudo sh setup.sh
#
# At a fresh install it asks a few values, copies this folder into the state folder
# ($MP_STATE_DIR, default /var/lib/modular-platform) and starts the seed. The seed creates every
# secret, runs every setup step and brings the platform up, then hands over to the keeper. Run
# again, setup.sh asks nothing and changes no file; it starts the seed only when no keeper runs.
set -eu

if [ "$(id -u)" -ne 0 ]; then
	echo "run as root: sudo sh setup.sh" >&2
	exit 1
fi
here=$(cd "$(dirname "$0")" && pwd -P)
umask 077

# The state folder holds the platform's files, secrets and data, for root only. Compose and the
# keeper see it at the same path, so it holds only plain characters.
dir=${MP_STATE_DIR:-/var/lib/modular-platform}
case $dir in
/*) ;;
*)
	echo "setup.sh: MP_STATE_DIR=$dir is not an absolute path" >&2
	exit 1
	;;
esac
case $dir in
*[!A-Za-z0-9._/-]*)
	echo "setup.sh: the state folder $dir may hold only A to Z, a to z, 0 to 9, ., _, - and /; set MP_STATE_DIR to another path" >&2
	exit 1
	;;
esac
seed='modular-platform-seed'

# env_get NAME prints the value of NAME in .env, with each $$ read back as $.
env_get() {
	sed -n "s/^$1=//p" .env 2>/dev/null | tail -n 1 | sed 's/\$\$/$/g'
}

# env_set NAME VALUE adds NAME to .env unless it is there already: unquoted, with each $ written
# $$, which Compose reads back as one $.
env_set() {
	if grep -q "^$1=" .env 2>/dev/null; then
		echo "kept    $1 in .env"
		return 0
	fi
	printf '%s=%s\n' "$1" "$(printf '%s' "$2" | sed 's/\$/$$/g')" >> .env
	echo "added   $1 to .env"
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
		*[\'\"\`\\#]* | [[:space:]]* | *[[:space:]])
			retry_or_stop "$var" "holds a quote, \\ or #, or starts or ends with a space, which .env cannot keep"
			continue
			;;
		esac
		if valid "$type" "$answer" "$min" "$max" "$pattern" "$@"; then
			break
		fi
		retry_or_stop "$var" "$why"
	done
	# unquoted, with each $ written $$, which Compose reads back as one $
	printf '%s=%s\n' "$var" "$(printf '%s' "$answer" | sed 's/\$/$$/g')" >> .env
	echo "added   $var to .env"
}

if [ ! -f "$dir/docker-compose.yml" ]; then
	echo "installing the platform into $dir"
	mkdir -p "$dir"
	chmod 0700 "$dir"
	# Every file of this folder but setup.sh, with its mode from the zip. docker-compose.yml comes
	# last, so a run that stops early is a fresh install again; answers it gave are kept.
	umask 022
	(cd "$here" && find . -type f ! -path ./setup.sh ! -path ./docker-compose.yml) |
		while IFS= read -r file; do
			mkdir -p "$dir/$(dirname "$file")"
			cp "$here/$file" "$dir/$file"
		done
	umask 077
	cd "$dir"
	env_set MP_DIR "$dir"
	env_set MP_UID 65532
	env_set MP_GID 65532
	# dev and CI only: the Compose files of the platform project, and the keeper image
	if [ -n "${COMPOSE_FILE:-}" ]; then
		env_set COMPOSE_FILE "$COMPOSE_FILE"
	fi
	if [ -n "${MP_KEEPER_IMAGE:-}" ]; then
		env_set MP_KEEPER_IMAGE "$MP_KEEPER_IMAGE"
	fi

	# Values the modules ask you, written into .env.
	ask MP_INGRESS__DOMAIN 'string' '' '' '^(([0-9a-z-])+((\\.([0-9a-z-])+))+)$' false 'The domain the platform answers on, with at least one dot (Authelia needs one).' ""
	ask MP_INGRESS__ORIGIN 'string' '' '' '^(http(s)?://(([]\001-\011\013-,.-\\_-\177^-]|[\302-\364][\200-\277]{1,3}))+)$' false 'Scheme, host and port the browser sees.' "https://$(env_get MP_INGRESS__DOMAIN)"
	umask 022
	cp "$here/docker-compose.yml" "$dir/docker-compose.yml"
	umask 077
else
	echo "the platform is installed in $dir: nothing is asked and no file changes"
	cd "$dir"
fi

# keeper_running [FILTER...] succeeds when a keeper container runs: the keeper instance's labels
# and the Compose project of a keeper definition. A seed never carries the instance label.
keeper_running() {
	docker ps "$@" --filter "label=io.github.racherom.modular-platform.stack=platform" --filter "label=io.github.racherom.modular-platform.instance=deployer-docker" \
		--format '{{.Label "com.docker.compose.project"}}' | grep -q '^modular-platform-keeper-'
}

if keeper_running; then
	echo "already running"
elif [ "$(docker inspect -f '{{.State.Running}}' "$seed" 2>/dev/null)" = true ]; then
	echo "the seed $seed is still starting the platform"
else
	docker rm -f "$seed" >/dev/null 2>&1 || true
	image=$(sed -n 's/^MP_KEEPER_IMAGE=//p' "$dir/.env" | tail -n 1)
	[ -n "$image" ] || image='ghcr.io/racherom/modular-platform/deployer-docker:0.0.0-test'
	echo "starting the seed $seed from $image"
	docker run -d --name "$seed" --restart unless-stopped \
		--label "io.github.racherom.modular-platform.stack=platform" --label "io.github.racherom.modular-platform.module=deployer-docker" \
		-v /var/run/docker.sock:/var/run/docker.sock -v "$dir:$dir" \
		"$image" 'seed' --state "$dir" >/dev/null
fi

if [ -e .shown ]; then
	echo "The platform runs from $dir."
	exit 0
fi
# The first start: wait until a keeper is healthy and the seed is gone, then print what only this
# run shows. A run that stops before the print prints it next time.
echo "waiting for the platform's first start (up to 15 minutes)"
waited=0
until keeper_running --filter health=healthy && ! docker inspect "$seed" >/dev/null 2>&1; do
	restarts=$(docker inspect -f '{{.RestartCount}}' "$seed" 2>/dev/null || echo 0)
	if [ "$waited" -ge 900 ] || [ "${restarts:-0}" -gt 3 ]; then
		echo "setup.sh: the platform did not finish its first start; see: docker logs $seed" >&2
		exit 1
	fi
	sleep 5
	waited=$((waited + 5))
done
echo
printf '%s: %s\n' 'The setup code the first visitor of the web setup enters once.' "$(cat secrets/api/setup/code)"
echo "Open $(env_get MP_INGRESS__ORIGIN) and sign in."
: > .shown
