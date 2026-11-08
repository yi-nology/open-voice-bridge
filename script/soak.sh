#!/usr/bin/env bash
set -euo pipefail
BASE="${BASE:-http://127.0.0.1:8787}"
DURATION="${DURATION:-86400}"
INTERVAL="${INTERVAL:-10}"
END=$((SECONDS + DURATION))
FAILURES=0
ROUNDS=0

while [ "$SECONDS" -lt "$END" ]; do
  ROUNDS=$((ROUNDS + 1))
  for path in /healthz /readyz /api/status /metrics; do
    code="$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 "$BASE$path" || true)"
    if [ "$code" != "200" ]; then
      FAILURES=$((FAILURES + 1))
      echo "$(date -Is) $path status=$code" >&2
    fi
  done
  ready="$(curl -s --max-time 5 "$BASE/readyz" || true)"
  if ! printf '%s' "$ready" | grep -q '"ok":true'; then
    FAILURES=$((FAILURES + 1))
    echo "$(date -Is) readyz=$ready" >&2
  fi
  sleep "$INTERVAL"
done

echo "soak complete rounds=$ROUNDS failures=$FAILURES"
[ "$FAILURES" -eq 0 ]
