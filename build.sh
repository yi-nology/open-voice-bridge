#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$ROOT"
GO_BIN="${GO:-}"
if [ -z "$GO_BIN" ]; then
  if command -v go >/dev/null 2>&1; then
    GO_BIN="$(command -v go)"
  elif [ -x "$HOME/go-toolchain/go/bin/go" ]; then
    GO_BIN="$HOME/go-toolchain/go/bin/go"
  elif [ -x /usr/local/go/bin/go ]; then
    GO_BIN=/usr/local/go/bin/go
  else
    echo "go toolchain not found; set GO=/path/to/go" >&2
    exit 1
  fi
fi
mkdir -p bin
VERSION="${VERSION:-dev}"
"$GO_BIN" build -tags stdjson -trimpath -ldflags "-X main.version=$VERSION" -o bin/xiaomi-remote .
