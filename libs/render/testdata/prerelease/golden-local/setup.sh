#!/bin/sh
# Prepares the stack prerelease: the user it runs as, its secret files and its data folders. Run
# it once before `docker compose up`. It never changes a file that already exists, so running it
# again is safe.
set -eu

# MP_SOURCE_PATH, a checkout of Modular Platform to build the platform images from instead of
# GitHub; resolved before the cd below, so a relative path starts where you ran setup.sh.
source_path=
if [ -n "${MP_SOURCE_PATH:-}" ]; then
	source_path=$(cd "$MP_SOURCE_PATH" 2>/dev/null && pwd -P) || {
		echo "setup.sh: MP_SOURCE_PATH=$MP_SOURCE_PATH is not a folder" >&2
		exit 1
	}
	if [ ! -f "$source_path/go.work" ]; then
		echo "setup.sh: MP_SOURCE_PATH=$source_path holds no go.work, so it is not a Modular Platform checkout" >&2
		exit 1
	fi
fi
cd "$(dirname "$0")"
umask 077

# The platform images this stack uses are not released yet, so no registry holds them: build
# each from Modular Platform at source_ref before anything else is made. A rerun builds again;
# the build cache makes that quick.
source_ref='local'
if [ -z "$source_path" ]; then
	echo "setup.sh: this zip was made by a renderer built from a local checkout, so no commit on GitHub holds its code; set MP_SOURCE_PATH=<a checkout of Modular Platform> and run setup.sh again" >&2
	exit 1
fi
build_images='ghcr.io/racherom/modular-platform/renderer:0.0.0'
# build_stop TEXT stops setup.sh before it makes any file, naming the builds and how to build
# from a local checkout.
build_stop() {
	echo "setup.sh: $1" >&2
	echo "setup.sh: this stack builds $build_images from Modular Platform at $source_ref; MP_SOURCE_PATH=<a checkout of it> builds from that folder instead of GitHub" >&2
	exit 1
}
docker buildx version >/dev/null 2>&1 ||
	build_stop "the platform images build with BuildKit, and docker buildx is not available; install Docker 23 or newer with the buildx plugin"
docker_version=$(docker version --format '{{.Server.Version}}' 2>/dev/null) ||
	build_stop "Docker does not answer; start it and run setup.sh again"
[ "${docker_version%%.*}" -ge 23 ] 2>/dev/null ||
	build_stop "the platform images need Docker 23 or newer, this host runs $docker_version"
# build_image IMAGE MODULE builds IMAGE from build/MODULE/Dockerfile. A build from a local
# checkout is labeled local, never with source_ref, which the checkout may not match.
build_image() {
	if [ -n "$source_path" ]; then
		echo "building $1 from the local checkout $source_path"
		docker buildx build --load -t "$1" --build-arg MP_SOURCE=local --build-arg MP_SOURCE_REF=local \
			-f "build/$2/Dockerfile" "$source_path" ||
			build_stop "could not build $1 from the local checkout $source_path; the build needs Docker Hub (the Dockerfile frontend and base images)"
	else
		echo "building $1 from Modular Platform at $source_ref"
		docker buildx build --load -t "$1" --build-arg MP_SOURCE=git --build-arg MP_SOURCE_REF="$source_ref" \
			-f "build/$2/Dockerfile" "build/$2" ||
			build_stop "could not build $1 at $source_ref; the ref must be pushed, and the build needs GitHub and Docker Hub (the Dockerfile frontend and base images)"
	fi
}
build_image 'ghcr.io/racherom/modular-platform/renderer:0.0.0' 'renderer'

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
		MP_RENDERER__PROXY_HOPS) ;;
		MP_RENDERER__RENDERER_MODE) ;;
		MP_RENDERER__RENDERER_RATE) ;;
		MP_RENDERER_2__PROXY_HOPS) ;;
		MP_RENDERER_2__RENDERER_MODE) ;;
		MP_RENDERER_2__RENDERER_RATE) ;;
		MP_POSTGRES__*) echo "setup.sh: warning: $var in .env names no value of postgres" >&2 ;;
		MP_RENDERER__*) echo "setup.sh: warning: $var in .env names no value of renderer" >&2 ;;
		MP_RENDERER_2__*) echo "setup.sh: warning: $var in .env names no value of renderer-2" >&2 ;;
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

new_secret secrets/postgres/password

new_folder() {
	if [ -d "$1" ]; then
		echo "kept    $1"
		return 0
	fi
	mkdir -p "$1"
	chmod 0700 "$1"
	echo "created $1"
}

new_folder data/postgres/data
