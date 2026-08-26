#!/usr/bin/env bash
# Throwaway: emit each CI check on PR 61 as it settles, then exit.
prev=""
for _ in $(seq 1 40); do
  status=$(gh pr checks 61 --json name,bucket 2>/dev/null) || true
  if [ -n "$status" ]; then
    current=$(echo "$status" | jq -r '.[] | select(.bucket!="pending") | "\(.name): \(.bucket)"' | sort)
    comm -13 <(echo "$prev") <(echo "$current")
    prev="$current"
    if echo "$status" | jq -e 'length > 0 and all(.bucket!="pending")' >/dev/null 2>&1; then
      echo "ALL CHECKS COMPLETE"
      exit 0
    fi
  fi
  sleep 30
done
echo "TIMED OUT waiting for checks"
