#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
make -C "$ROOT" build install-user
if [ "$(id -u)" -eq 0 ]; then
  make -C "$ROOT" install-remap install-logrotate
else
  echo "Remap service still needs root: sudo make -C $ROOT install-remap install-logrotate"
fi
