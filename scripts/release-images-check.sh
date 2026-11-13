#!/bin/sh
# Checks that every platform image of a release is in the registry for linux/amd64 and
# linux/arm64 (spec 0030, AC-9). The managed zip job of release.yml runs it after the images job
# pushed them and before it attaches the zip, so a release never ships a zip that names a
# missing image. Run it by hand to check a push that ended half done (README, Releases).
#
# Usage: scripts/release-images-check.sh <version> [image prefix]
# The prefix defaults to ghcr.io/racherom/modular-platform. To try the check without a release,
# point it at a local registry (verify.md of spec 0030 shows how).
set -eu

version=${1:?usage: release-images-check.sh <version> [image prefix]}
prefix=${2:-ghcr.io/racherom/modular-platform}

for service in renderer web api deployer-docker; do
	image="$prefix/$service:$version"
	manifest=$(docker buildx imagetools inspect "$image") || {
		echo "::error::$image is not in the registry; did the images job of the Release workflow push it?"
		exit 1
	}
	# an index may list arm64 as linux/arm64/v8, the same platform
	for platform in linux/amd64 linux/arm64; do
		printf '%s\n' "$manifest" | grep -Eq "Platform: +$platform(/v8)?\$" || {
			echo "::error::$image has no $platform image"
			exit 1
		}
	done
done
echo "every platform image of $version is in $prefix for linux/amd64 and linux/arm64"
