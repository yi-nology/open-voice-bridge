#!/usr/bin/env bash
# Install from an extracted open-voice-bridge release tarball (OPS-02).
# No make and no Go toolchain needed — the tarball ships the binary, the unit
# templates and this script.
#
#   ./install.sh               install and start the user-level agent
#   sudo ./install.sh --root   install the root remap helper and logrotate
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BIN="$ROOT/bin/xiaoyao"
DEPLOY="$ROOT/deploy"

if [ ! -x "$BIN" ]; then
  echo "install.sh: $BIN is missing or not executable." >&2
  echo "Run this script from the directory you extracted the release into." >&2
  exit 1
fi
if [ ! -d "$DEPLOY" ]; then
  echo "install.sh: $DEPLOY is missing — this does not look like a release tree." >&2
  exit 1
fi

RUN_USER="${SUDO_USER:-$(id -un)}"
RUN_UID="$(id -u "$RUN_USER")"
RUN_GID="$(id -g "$RUN_USER")"
RUN_HOME=""
if command -v getent >/dev/null 2>&1; then
  RUN_HOME="$(getent passwd "$RUN_USER" 2>/dev/null | cut -d: -f6 || true)"
fi
if [ -z "$RUN_HOME" ]; then
  RUN_HOME="$HOME"
fi
LOG_DIR="$RUN_HOME/.local/state/xiaoyao"
LOG_FILE="$LOG_DIR/agent.log"
CONFIG="$RUN_HOME/.config/xiaoyao/config.json"

# render substitutes the same placeholders the Makefile does, so one template
# serves both the checkout install and the release install.
render() {
  sed -e "s|@ROOT@|$ROOT|g" -e "s|@USER@|$RUN_USER|g" \
    -e "s|@UID@|$RUN_UID|g" -e "s|@GID@|$RUN_GID|g" \
    -e "s|@LOG_DIR@|$LOG_DIR|g" -e "s|@LOG_FILE@|$LOG_FILE|g" \
    "$1" > "$2"
}

install_user() {
  unit_dir="$RUN_HOME/.config/systemd/user"
  mkdir -p "$unit_dir"
  render "$DEPLOY/xiaoyao.service" "$unit_dir/xiaoyao.service"
  chmod 644 "$unit_dir/xiaoyao.service"
  systemctl --user daemon-reload
  systemctl --user enable --now xiaoyao.service
  echo "installed and started: $unit_dir/xiaoyao.service"
}

install_root() {
  render "$DEPLOY/xiaoyao-remap.service" /etc/systemd/system/xiaoyao-remap.service
  chmod 644 /etc/systemd/system/xiaoyao-remap.service
  systemctl daemon-reload
  systemctl enable --now xiaoyao-remap.service
  render "$DEPLOY/logrotate-xiaoyao" /etc/logrotate.d/xiaoyao
  chmod 644 /etc/logrotate.d/xiaoyao
  echo "installed and started: xiaoyao-remap.service (+ logrotate)"
}

case "${1:-}" in
  --root)
    if [ "$(id -u)" -ne 0 ]; then
      echo "install.sh --root must run as root: sudo ./install.sh --root" >&2
      exit 1
    fi
    install_root
    ;;
  "")
    install_user
    if [ "$(id -u)" -ne 0 ]; then
      echo "root helper not installed; for key remapping run: sudo ./install.sh --root"
    fi
    ;;
  *)
    echo "usage: ./install.sh [--root]" >&2
    exit 1
    ;;
esac

if [ ! -f "$CONFIG" ]; then
  echo "next: create $CONFIG from $ROOT/config.example.json and set your remote's address"
fi
