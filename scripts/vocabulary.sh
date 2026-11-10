#!/bin/sh
# The retired word check (spec 0027, AC-12): fail on any word that scripts/vocabulary.txt
# retires, in the content or the path of a tracked or staged file. A line that carries the
# marker "vocabulary: allow" is skipped. With --report it prints the hits and their count and
# exits 0, so a rename in progress can watch the count fall.
set -eu

repo=$(cd "$(dirname "$0")/.." && pwd)
cd "$repo"
rules=scripts/vocabulary.txt
marker='vocabulary: allow'

report=0
if [ "${1:-}" = "--report" ]; then
	report=1
fi

# Paths that keep the words of their time, or that name the old words on purpose.
set -f
skip=":(exclude).agents :(exclude).claude :(exclude,glob)docs/specs/**/rationale.md :(exclude,glob)docs/reviews/** :(exclude,glob)**/CHANGELOG.md :(exclude,glob)**/pnpm-lock.yaml :(exclude,glob)**/go.sum :(exclude,glob)**/go.work.sum :(exclude)services/api/internal/store/migrations/00001_first_tables.sql :(exclude)$rules"

out=$(mktemp)
trap 'rm -f "$out" "$out.allow"' EXIT
trap 'exit 1' INT TERM HUP

# file:line of every marked line, so content hits there are dropped
# shellcheck disable=SC2086 # the pathspecs split on spaces on purpose
git grep -n -I -F -e "$marker" -- . $skip | cut -d: -f1,2 >"$out.allow" || true

while IFS= read -r line; do
	case $line in '' | '#'*) continue ;; esac
	pattern=${line%% | *}
	rest=${line#* | }
	globs=${rest%% | *}
	hint=${rest#* | }

	# smart case: a capital letter in the pattern means it is meant case sensitively
	case $pattern in
		*[A-Z]*) icase= ;;
		*) icase=-i ;;
	esac

	# content: one output line per match (file:line:word), minus the marked lines
	# shellcheck disable=SC2086
	git grep -n -I -o $icase -E -e "$pattern" -- $globs $skip |
		awk -F: -v hint="$hint" -v allow="$out.allow" '
			BEGIN { while ((getline l < allow) > 0) skip[l] = 1 }
			!(($1 ":" $2) in skip) {
				word = $0; sub(/^[^:]*:[^:]*:/, "", word)
				printf "%s:%s: \"%s\" is retired, use %s\n", $1, $2, word, hint
			}' >>"$out" || true

	# paths: tracked and staged files whose path holds the word
	# shellcheck disable=SC2086
	git ls-files -c -- $globs $skip | { grep $icase -E -e "$pattern" || true; } | while IFS= read -r path; do
		word=$(printf '%s\n' "$path" | grep -o $icase -E -e "$pattern" | head -n 1)
		printf '%s: "%s" in the path is retired, use %s\n' "$path" "$word" "$hint" >>"$out"
	done
done <"$rules"

count=$(wc -l <"$out" | tr -d ' ')
cat "$out"
if [ "$count" -eq 0 ]; then
	echo "vocabulary: no retired words"
	exit 0
fi
echo "vocabulary: $count retired words (see spec 0027, the glossary in spec 0001)"
if [ "$report" -eq 1 ]; then
	exit 0
fi
exit 1
