#!/usr/bin/env bash
# ARC-11 — mechanical gates for architecture.md R1–R3 plus the map[string]any
# ratchet (§10-3). Every allowance below is recorded debt with the batch that
# removes it; they are not law. Green means the tree is no dirtier than the
# ledger says.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

MOD=$(go list -m)
fail=0
violation() { echo "deps: $*" >&2; fail=1; }

# §10-3: the target is <20. This ceiling is a ratchet — lower it whenever the
# count drops and never raise it (the debt log in the roadmap tracks the why).
# 2026-11-14: admin UI 应答信封收进 typed reply(#16,biz/handler/ui/reply.go)
# ⇒ 230→199(S6/ARC-08/S8 三批全部抵扣)。基线随降到 205(留缓冲),
# 下一批 typed 收口继续降;目标 <20 不变。
CEILING=205
count=$({ grep -rn --include='*.go' 'map\[string\]any' . || true; } | { grep -v _test || true; } | wc -l | tr -d ' ')
[ "$count" -le "$CEILING" ] || violation "map[string]any count $count exceeds the ratchet ceiling $CEILING (new state goes in core typed views; free-format maps only at the documented sites — see roadmap 债务盘点; lower the ceiling when the count drops)"

# Input sources; only composition roots may combine them (R3).
# biz/service is the second root until ARC-09 dissolves it; internal/agent
# until ARC-08/ARC-10 split the backoff/speech concerns out of it.
is_root() { case "$1" in main|app/*|biz/service|internal/agent) return 0 ;; *) return 1 ;; esac; }

while IFS='|' read -r pkg imports; do
  pkg="${pkg#"$MOD"/}"; [ "$pkg" = "$MOD" ] && pkg=main
  srcs=0
  IFS=',' read -ra imps <<< "$imports"
  for imp in "${imps[@]}"; do
    imp="${imp#"$MOD"/}"; [ "$imp" = "$MOD" ] && imp=main
    case "$imp" in main|app/*|biz/*|edge/*|internal/*|web) ;; *) continue ;; esac

    # R1: foundation imports nothing internal except the documented pair.
    case "$pkg" in
      internal/core|internal/fsutil)
        violation "$pkg imports $imp (R1: foundation imports nothing internal)" ;;
      internal/config)
        case "$imp" in internal/core|internal/fsutil) ;; *) violation "$pkg imports $imp (R1: config only core+fsutil)" ;; esac ;;
    esac

    # R2: the domain layer never imports edge/app. The one transitional edge
    # is internal/phone -> edge/netguard until ARC-09 splits source from edge.
    case "$pkg" in internal/*)
      case "$imp" in
        app/*) violation "$pkg imports $imp (R2)" ;;
        edge/*)
          if [ "$pkg" != "internal/phone" ] || [ "$imp" != "edge/netguard" ]; then
            violation "$pkg imports $imp (R2; only internal/phone->edge/netguard is transitional until ARC-09)"
          fi ;;
      esac ;;
    esac

    case "$imp" in internal/phone|internal/bluez|internal/atvv|internal/agent) srcs=$((srcs + 1)) ;; esac
  done

  if ! is_root "$pkg" && [ "$srcs" -gt 1 ]; then
    violation "$pkg imports $srcs input sources (R3: only composition roots combine sources)"
  fi
done < <(go list -f '{{.ImportPath}}|{{join .Imports ","}}' ./...)

exit "$fail"
