#!/bin/sh
# Opens the pull request that carries a release commit back into dev (spec 0032, AC-5).
#
# Input: the released version as $1 and the release commit as $2 (the push to main that made the
# release). Environment: GITHUB_REPOSITORY (owner/name) and GH_TOKEN, the release token. GH names
# the gh program (default gh); a test sets it to a stub.
#
# It needs no checkout and no git, only the GitHub API through gh:
#   - dev already holds the commit: nothing to do.
#   - a pull request from back-merge/<version> is open: nothing to do.
#   - the branch exists at the commit and has no open pull request: open the pull request.
#   - the branch exists at another commit: stop with an error, a person decides.
#   - no branch: create it at the commit, then open the pull request.
# So a rerun never opens a second pull request.
#
# Merge the pull request with a merge commit, never a squash: a squash leaves the release commit
# out of dev's history and the next release conflicts.
set -eu

[ $# -eq 2 ] || {
	echo "usage: release-back-merge.sh <version> <sha>" >&2
	exit 2
}
version=$1
sha=$2
: "${GITHUB_REPOSITORY:?GITHUB_REPOSITORY is not set}"
gh=${GH:-gh}
repo=$GITHUB_REPOSITORY
base=dev
branch=back-merge/$version

# both values end up in a ref name and a command line, so only plain ones pass
printf '%s\n' "$version" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+([-+][0-9A-Za-z.+-]+)?$' || {
	echo "::error::'$version' is not a version"
	exit 2
}
printf '%s\n' "$sha" | grep -Eq '^[0-9a-f]{40}$' || {
	echo "::error::'$sha' is not a full commit SHA"
	exit 2
}

# compare dev...sha: the status is relative to dev. behind or identical means dev holds the commit
status=$("$gh" api "repos/$repo/compare/$base...$sha" --jq .status)
case $status in
behind | identical)
	echo "$base already holds $sha; no back merge needed"
	exit 0
	;;
esac

open=$("$gh" pr list --repo "$repo" --head "$branch" --base "$base" --state open --json number --jq length)
if [ "$open" != 0 ]; then
	echo "a pull request from $branch into $base is already open; nothing to do"
	exit 0
fi

if existing=$("$gh" api "repos/$repo/git/ref/heads/$branch" --jq .object.sha 2>&1); then
	if [ "$existing" != "$sha" ]; then
		echo "::error::$branch exists at $existing, not at the release commit $sha; check it, delete it if it is stale, and rerun the job"
		exit 1
	fi
	echo "$branch exists at $sha without a pull request"
else
	case $existing in
	*"HTTP 404"*)
		"$gh" api -X POST "repos/$repo/git/refs" -f "ref=refs/heads/$branch" -f "sha=$sha" >/dev/null
		echo "created $branch at $sha"
		;;
	*)
		printf '%s\n' "$existing"
		echo "::error::could not look up $branch"
		exit 1
		;;
	esac
fi

"$gh" pr create --repo "$repo" --base "$base" --head "$branch" \
	--title "chore(release): back merge $version" \
	--body "Carries the release commit of $version back into $base. Merge it with a merge commit, never a squash, so the next release does not conflict. On a conflict, merge $base into $branch locally and keep the release side for the version lines (MP_VERSION, the four catalog image lines, web/package.json, the manifest, CHANGELOG.md)."
echo "opened the back merge pull request for $version"
