#!/usr/bin/env bash
# Secret scan (OPS-14): high-confidence credential markers in tracked files.
# False-positive-resistant on purpose — literal marker shapes only, no entropy
# heuristics. A hit must be revoked upstream first and only then removed here.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

patterns=(
  'BEGIN (RSA |EC |OPENSSH |DSA )?PRIVATE KEY'
  'AKIA[0-9A-Z]{16}'
  'ghp_[A-Za-z0-9]{36}'
  'github_pat_[A-Za-z0-9_]{22,}'
  'xox[baprs]-[A-Za-z0-9-]{10,}'
  'sk-[A-Za-z0-9]{32,}'
)

fail=0
for p in "${patterns[@]}"; do
  if hits=$(git grep -nE "$p" -- . ':!script/check-secrets.sh'); then
    echo "secrets: pattern '$p':" >&2
    echo "$hits" >&2
    fail=1
  fi
done
exit "$fail"
