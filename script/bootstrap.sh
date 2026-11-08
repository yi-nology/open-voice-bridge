#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
"$ROOT/build.sh"
exec "$ROOT/bin/xiaomi-remote" agent "$@"
