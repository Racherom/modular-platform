#!/bin/sh
# Prints the commit the online editor builds web and renderer from while MP_VERSION is 0.0.0
# (spec 0030, AC-8). `task online:up` exports it as MP_SOURCE_REF, so both images carry that
# commit in their revision label and the renderer's zips name it, not a branch that moves.
#
# Input: MP_SOURCE_REF from the shell, else the MP_SOURCE_REF line of deploy/online/versions.env.
# A full commit passes through with no network call; a branch or tag is resolved with git
# ls-remote against MP_SOURCE_REMOTE (default: the repository on GitHub), a branch first, then
# the commit an annotated tag points to, then a lightweight tag. A rerun resolves again, so it
# picks up the newest commit of a branch.
#
# Output: the commit on stdout, messages on stderr; exit 0, or 1 on any stop.
set -eu

remote=${MP_SOURCE_REMOTE:-https://github.com/racherom/modular-platform.git}
versions=deploy/online/versions.env
ref=${MP_SOURCE_REF:-$(sed -n 's/^MP_SOURCE_REF=\([^ #]*\).*/\1/p' "$versions" 2>/dev/null)}

stop() {
	echo "online-source-ref: $1" >&2
	exit 1
}

[ -n "$ref" ] || stop "MP_SOURCE_REF is not set in the shell or in $versions"

# a full commit is already fixed
if printf '%s\n' "$ref" | grep -Eqx '[0-9a-f]{40}'; then
	printf '%s\n' "$ref"
	exit 0
fi
[ "$ref" != local ] ||
	stop "MP_SOURCE_REF=local builds from a checkout, but the online editor builds from GitHub; set a commit, branch or tag"
# a short or uppercase commit (or a name of hex digits only, which looks the same) and any value
# outside the allowed form are refused, as the renderer refuses them
if printf '%s\n' "$ref" | grep -Eqx '[0-9A-Fa-f]{4,40}' || ! printf '%s\n' "$ref" | grep -Eqx '[A-Za-z0-9._/-]+'; then
	stop "MP_SOURCE_REF=$ref is neither a full commit nor a branch or tag name"
fi

status=0
refs=$(GIT_TERMINAL_PROMPT=0 git ls-remote --exit-code "$remote" "refs/heads/$ref" "refs/tags/$ref" "refs/tags/$ref^{}" 2>/dev/null) || status=$?
case $status in
0) ;;
2) stop "$ref is no branch or tag of $remote" ;;
*) stop "could not reach $remote to resolve $ref; check the network, or set MP_SOURCE_REF to a full commit" ;;
esac

# pick by exact name, never by line order: ls-remote sorts by name, so a tag object comes before
# the commit it points to
for want in "refs/heads/$ref" "refs/tags/$ref^{}" "refs/tags/$ref"; do
	commit=$(printf '%s\n' "$refs" | awk -v want="$want" '$2 == want { print $1; exit }')
	if [ -n "$commit" ]; then
		printf '%s\n' "$commit"
		exit 0
	fi
done
stop "$ref is no branch or tag of $remote"
