#!/bin/sh
# Checks that a release image is not in GHCR yet, right before the images job of release.yml
# pushes it (spec 0030, AC-9): a released image is never pushed again.
#
# Input: the full image reference, e.g. ghcr.io/racherom/modular-platform/renderer:1.2.3, as $1.
# It asks `docker buildx imagetools inspect`, so log in to the registry first.
#
# Exit 0 only when the registry says "not found". The tag existing stops with 1, and so does any
# other answer (network, "denied", "unauthorized", "403 Forbidden"), so an unknown state never
# leads to a push. Without a login, GHCR answers 403 even for a package that does not exist, so
# such an answer cannot be read as "absent".
set -eu

[ $# -eq 1 ] && [ -n "$1" ] || {
	echo "usage: release-image-absent.sh <image:tag>" >&2
	exit 2
}
image=$1

if out=$(docker buildx imagetools inspect "$image" 2>&1); then
	echo "::error::$image is already in GHCR; a released image is never pushed again"
	exit 1
fi
case $out in
*"not found"*) echo "$image is not in GHCR yet" ;;
*)
	printf '%s\n' "$out"
	echo "::error::could not check whether $image is in GHCR"
	exit 1
	;;
esac
