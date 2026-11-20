#!/bin/sh
# Checks a pull request against the branch rules (spec 0032, AC-3).
#
# Input: the base branch, the head branch and the title of the pull request, as $1, $2 and $3.
# The caller passes them through env, never through the script text, because the head branch and
# the title are untrusted.
#
# Into main: only dev, hotfix/* and release-please--branches--main may be the head; a hotfix/*
# title must also be a conventional commit. Into dev: the title must be a conventional commit or
# GitHub's Revert "..." form. Every other base passes.
#
# Exit 0 passes. Exit 1 fails with a one line reason. Exit 2 is a usage error.
set -eu

[ $# -eq 3 ] || {
	echo "usage: branch-rules.sh <base> <head> <title>" >&2
	exit 2
}
base=$1
head=$2
title=$3

conventional='^(feat|fix|perf|revert|docs|style|refactor|test|build|ci|chore)(\([a-z0-9._/-]+\))?!?: .+'
revert='^Revert ".+"$'

# matches PATTERN: the whole title is one line and the line fits the pattern (grep alone checks
# line by line, so a bad first line would hide behind a good second one)
matches() {
	case $title in
	*'
'*) return 1 ;;
	esac
	printf '%s\n' "$title" | grep -Eq "$1"
}

is_conventional() {
	matches "$conventional"
}

case $base in
main)
	case $head in
	dev | release-please--branches--main) ;;
	hotfix/*)
		is_conventional || {
			echo "::error::a hotfix pull request needs a conventional commit title, like 'fix(api): handle an empty graph'"
			exit 1
		}
		;;
	*)
		echo "::error::main takes only dev, hotfix/* or the release pull request, not '$head'; open this pull request into dev"
		exit 1
		;;
	esac
	;;
dev)
	if ! is_conventional && ! matches "$revert"; then
		echo "::error::the title must be a conventional commit, like 'feat(web): show a failed check'; the squash commit uses it"
		exit 1
	fi
	;;
esac
echo "branch rules pass: $head into $base"
