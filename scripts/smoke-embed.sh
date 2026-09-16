#!/usr/bin/env bash
# Smoke test for `embed` and `related` on a real spool.
#
# The properties under test are the ones unit tests cannot reach, because they
# only show up against a real corpus and a real model:
#
#   1. embed covers the window -- afterwards nothing is outstanding.
#   2. embed is idempotent -- a second run does no work. A broken staleness
#      predicate presents exactly here, as a re-embed of everything.
#   3. --force really re-selects the window, so it is not silently a no-op.
#   4. related returns ranked neighbours with plausible similarities, strictly
#      descending. This is what proves the stored vectors mean something; a
#      scrambled codec still yields numbers, just not ordered ones.
#
# Usage: scripts/smoke-embed.sh <path-to-spool-copy> [model] [window]
# The spool is written to, so pass a copy. Needs a reachable provider.
set -euo pipefail

DB="${1:?usage: $0 <path-to-spool-copy> [model] [window]}"
MODEL="${2:-nomic-embed-text}"
WINDOW="${3:-2d}"
BIN="${BIN:-./feedspool}"

# Fewer than this and the run proves nothing about batching or ranking, the
# same guard smoke-reindex-force.sh applies to its sample count.
MIN_ITEMS=50

run() { "$BIN" --database "$DB" "$@"; }

# outstanding prints how many items embed would process right now. Extra
# arguments (such as --force) are forwarded; "$@" is passed inline because an
# empty array assignment trips `set -u` on the bash 3.2 that ships with macOS.
outstanding() {
	run embed --last "$WINDOW" --model "$MODEL" --dry-run "$@" 2>/dev/null |
		sed -n 's/^Model .*: \([0-9]*\) items to embed$/\1/p'
}

echo "== smoke-embed: $MODEL over the last $WINDOW of $DB =="

before=$(outstanding)
echo "outstanding before: $before"
if [ "$before" -lt "$MIN_ITEMS" ]; then
	# Distinguish "this spool is already embedded" from "this window is empty",
	# because the first is the likely mistake on a re-run and the fix differs.
	if [ "$(outstanding --force)" -ge "$MIN_ITEMS" ]; then
		echo "FAIL: this spool is already embedded for $MODEL, so the coverage and"
		echo "      idempotence assertions below would prove nothing. Pass a fresh copy."
	else
		echo "FAIL: only $before items in the window; too few to prove anything (want >= $MIN_ITEMS)"
	fi
	exit 1
fi

echo "-- embedding --"
run embed --last "$WINDOW" --model "$MODEL" | tail -1

# 1. The window is covered.
after=$(outstanding)
echo "outstanding after: $after"
[ "$after" -eq 0 ] ||
	{ echo "FAIL: $after items still outstanding after embedding; the window was not covered"; exit 1; }

# 2. Idempotence. A second run must report doing nothing at all.
second=$(run embed --last "$WINDOW" --model "$MODEL" | tail -1)
echo "second run said: $second"
case "$second" in
*"Nothing to do"*) ;;
*) echo "FAIL: a second run did work ($second); the staleness predicate is not holding"; exit 1 ;;
esac

# 3. --force must re-select the whole window, not quietly do nothing.
forced=$(outstanding --force)
echo "outstanding with --force: $forced"
[ "$forced" -ge "$MIN_ITEMS" ] ||
	{ echo "FAIL: --force selected only $forced items; force is behaving as a no-op"; exit 1; }

# 4. related must rank. Pick a subject that was definitely embedded.
# The subject window must be bounded at BOTH ends, matching embed's. With an
# open-ended --since, a publisher that future-dates an item puts it first in
# effective-date order while embed has (correctly) skipped it, and related
# then reports it unembedded. Exactly one item in 24,156 on the reference
# spool does this, and an earlier version of this script picked it first try.
now=$(date -u +%Y-%m-%dT%H:%M:%SZ)
ago=$(date -u -v"-${WINDOW}" +%Y-%m-%dT%H:%M:%SZ 2>/dev/null ||
	date -u -d "${WINDOW/d/ days} ago" +%Y-%m-%dT%H:%M:%SZ)
# The subject also has to be a link that appears exactly once. Real feeds
# reuse a URL across GUIDs -- the BBC news feed carries some articles twice --
# and related refuses an ambiguous link by design, the same way `item` does.
subject=$(run items --since "$ago" --until "$now" --format json 2>/dev/null |
	jq -r '
		map(select(.Title != "" and .Link != ""))
		| group_by(.Link)
		| map(select(length == 1) | .[0])
		| .[0].Link
	')
[ -n "$subject" ] && [ "$subject" != "null" ] ||
	{ echo "FAIL: could not pick a subject item from the spool"; exit 1; }
echo "-- related to: $subject --"

neighbors=$(run --json related "$subject" --model "$MODEL" --limit 10 2>/dev/null)
count=$(printf '%s' "$neighbors" | jq '.neighbors | length')
echo "neighbours returned: $count"
[ "$count" -gt 0 ] ||
	{ echo "FAIL: related returned no neighbours despite an embedded window"; exit 1; }

# Two separate properties, deliberately not conflated.
#
# First the mathematical one: cosine similarity is in [-1, 1] and the list must
# descend. Opposed vectors legitimately score negative -- the unit tests cover
# -1 -- so the bound is -1, not 0. Asserting "> 0" here would reject valid
# output.
printf '%s' "$neighbors" | jq -e '
	[.neighbors[].similarity] as $s
	| ([$s[] | select(. > 1 or . < -1)] | length == 0)
	  and ([range(0; ($s | length) - 1) | $s[.] >= $s[. + 1]] | all)
' >/dev/null ||
	{ echo "FAIL: similarities are not all within [-1,1] and descending"; exit 1; }

top=$(printf '%s' "$neighbors" | jq -r '.neighbors[0].similarity')
bottom=$(printf '%s' "$neighbors" | jq -r '.neighbors[-1].similarity')
echo "similarity range over $count neighbours: $top down to $bottom"

# Then a corpus-specific sanity check, which is a heuristic rather than a law:
# on a spool of this size the nearest neighbour of an arbitrary item is always
# well above zero (the measured median between unrelated items is ~0.59 for
# nomic). A top score at or below zero means the vectors are not carrying
# meaning -- a scrambled codec or a crossed model -- even though every value
# would still be inside [-1, 1].
printf '%s' "$neighbors" | jq -e '.neighbors[0].similarity > 0' >/dev/null ||
	{ echo "FAIL: top similarity is $top; on a corpus this size that means the vectors are not meaningful"; exit 1; }

echo "PASS: window covered, idempotent, --force live, and related ranked $count neighbours"
