#!/usr/bin/env bash
# Smoke test for `reindex --force` on a real spool.
#
# The property under test is that a forced rebuild never leaves search
# answering nothing. Unit tests assert it between committed batches; this
# asserts it from a separate process against a real database, which is the
# thing a user would actually notice.
#
# Usage: scripts/smoke-reindex-force.sh <path-to-spool-copy> [search-term]
# The spool is written to, so pass a copy.
set -euo pipefail

DB="${1:?usage: $0 <path-to-spool-copy> [search-term]}"
TERM_="${2:-the}"
BIN="${BIN:-./feedspool}"

query() { "$BIN" --database "$DB" items --search "$TERM_" --format json 2>/dev/null | jq 'length'; }

before=$(query)
echo "before rebuild: $TERM_ matches $before items"
[ "$before" -gt 0 ] || { echo "FAIL: search returned nothing before the rebuild"; exit 1; }

"$BIN" --database "$DB" reindex --force >/tmp/fsp-reindex.log 2>&1 &
rebuild=$!
echo "rebuild running as pid $rebuild; sampling search..."

lowest=$before
samples=0
while kill -0 "$rebuild" 2>/dev/null; do
	n=$(query || echo -1)
	samples=$((samples + 1))
	[ "$n" -lt "$lowest" ] && lowest=$n
	if [ "$n" -le 0 ]; then
		echo "FAIL: search returned $n during the rebuild (sample $samples)"
		wait "$rebuild" || true
		exit 1
	fi
done
wait "$rebuild"

after=$(query)
echo "samples during rebuild: $samples, lowest result count seen: $lowest"
echo "after rebuild:  $TERM_ matches $after items"
[ "$samples" -ge 2 ] || { echo "FAIL: only $samples samples; rebuild finished too fast to prove anything"; exit 1; }
[ "$after" -gt 0 ] || { echo "FAIL: search returned nothing after the rebuild"; exit 1; }
echo "PASS: search answered on every one of $samples samples throughout the rebuild"
