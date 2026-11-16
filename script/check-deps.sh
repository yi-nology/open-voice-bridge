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
# 2026-11-15: 波 3 历史策略应答进 typed reply、ARC-09 的 BuildDiscovered 改
# core.DiscoveredView(顺带清掉 map→api.DiscoveredResp 的字段搬运)⇒ 212→196
# (这两批曾把计数推过棘轮,提交前被门禁咬住后就地抵扣)。
# 2026-11-15(二): helper↔daemon 线协议 typed 化(core.HelperRequest/Response/
# Event)⇒ 196→156,基线降到 162(留缓冲)。余债见路线图债务盘点:mcp JSON-RPC
# 31(自由格式,文档点)、SSE 信封/Publish、PhoneAdmin 视图、agent_api、诊断包。
CEILING=162
count=$({ grep -rn --include='*.go' 'map\[string\]any' . || true; } | { grep -v _test || true; } | wc -l | tr -d ' ')
[ "$count" -le "$CEILING" ] || violation "map[string]any count $count exceeds the ratchet ceiling $CEILING (new state goes in core typed views; free-format maps only at the documented sites — see roadmap 债务盘点; lower the ceiling when the count drops)"

# Input sources; only composition roots may combine them (R3).
# biz/service left the list with ARC-09 (construction seam injected; it is a
# state owner again). internal/agent stays: the desktop loop is where the
# remaining source wiring lives (bluez transport factory + atvv), pending the
# recorded core type migration / wiring move to app/runtime.
is_root() { case "$1" in main|app/*|internal/agent) return 0 ;; *) return 1 ;; esac; }

while IFS='|' read -r pkg imports; do
  pkg="${pkg#"$MOD"/}"; [ "$pkg" = "$MOD" ] && pkg=main
  srcs=0
  IFS=',' read -ra imps <<< "$imports"
  # ${arr[@]+"${arr[@]}"} keeps bash 3.2 (macOS) happy under `set -u` when the
  # package has no imports and the array stays empty.
  for imp in ${imps[@]+"${imps[@]}"}; do
    imp="${imp#"$MOD"/}"; [ "$imp" = "$MOD" ] && imp=main
    case "$imp" in main|app/*|biz/*|edge/*|internal/*|web) ;; *) continue ;; esac

    # R1: foundation imports nothing internal except the documented pair.
    case "$pkg" in
      internal/core|internal/fsutil)
        violation "$pkg imports $imp (R1: foundation imports nothing internal)" ;;
      internal/config)
        case "$imp" in internal/core|internal/fsutil) ;; *) violation "$pkg imports $imp (R1: config only core+fsutil)" ;; esac ;;
    esac

    # R2: the domain layer never imports edge/app. The phone surface gets its
    # boundary checks through the seams in internal/phone/guard.go (ARC-09).
    case "$pkg" in internal/*)
      case "$imp" in
        app/*|edge/*) violation "$pkg imports $imp (R2)" ;;
      esac ;;
    esac

    case "$imp" in internal/phone|internal/bluez|internal/atvv|internal/agent) srcs=$((srcs + 1)) ;; esac
  done

  if ! is_root "$pkg" && [ "$srcs" -gt 1 ]; then
    violation "$pkg imports $srcs input sources (R3: only composition roots combine sources)"
  fi
done < <(go list -f '{{.ImportPath}}|{{join .Imports ","}}' ./...)

exit "$fail"
