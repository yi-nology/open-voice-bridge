# Changelog

## Unreleased

- Internal: the oekit family reaches latest (pkg 0.37→0.51, middleware
  0.13→0.19, concurrency 0.28→0.38, logx 0.8→0.11, hertz 0.10→0.12,
  bootstrap 0.6→0.7, metrics 0.1→0.4, core/registry/tracing along) — the
  0.x jump the roadmap left over lands in one batch. The only break was
  `errors.Errorf`, retired upstream by #378: its 138 call sites take
  `fmt.Errorf` with byte-identical messages. All 50 `%w` wraps then join
  the new `errors.Wrap` (nil-safe, kv-ready for structured logs); the two
  mid-message forms in `asr/command.go` / `asr/whispercpp.go` move the
  cause to the end — same facts, cause last. `diagbox` folds into the
  single errors package, and `check-deps.sh` survives macOS bash 3.2 on
  zero-import packages (it used to crash on `internal/gtvkeys` before
  checking anything).

- New: the Google TV family's buttons arrive over GATT (Linux). The family's
  HID descriptor is rejected by the kernel input stack — no `/dev/input`, no
  evdev remap — so the agent now subscribes to the HID Report characteristic
  (0x2A4D) and decodes its raw reports (`internal/gtvkeys`): the 16-bit
  Consumer usage form, the fw 26.0 one-byte index form (17-entry report map),
  the 8-byte keyboard volume form, and all-zero releases with per-channel
  held tracking. 方向/OK/返回/主页/静音/音量/播放暂停 inject through the same
  helper whitelist the phone speaks (mute and playpause join the whitelist —
  media keys, same risk class as volumeup), power rides the
  `phone.allow_power_key` opt-in, and the Assistant key stays out of the way
  (the ATVV control channel already owns the mic). macOS stays honest —
  CoreBluetooth hides the HID service.

- New: the GATT buttons honor the keymap. The helper's `inject` verb already
  runs `applyKey` — one executor for evdev keys and injected keys alike — so
  remapping 上键 to a macro or a common phrase changes what the remote's Up
  button does (`TestInjectAppliesTheKeymap` pins it). The settings and TV
  buttons now land on the 菜单键 / TV键 keymap slots (compose/grave join the
  inject whitelist — plain keys, same risk class), and the pins in the
  remap/phone tests move with them. Assistant stays unmappable by design and
  the app-launcher keys await a keymap slot (roadmap).

- New: the Google TV / Chromecast voice remote family joins (`google-tv-remote`,
  Onn 同族). It speaks the same ATVV voice service as the Xiaomi remotes
  (`ab5e0001-…`) — the existing chain just works — and matches by Google's
  USB-IF ids (`18d1:9450`) and the names the family advertises. Two
  adaptation gaps close with it: the remote negotiates **8 kHz**, so the
  engine door now resamples to the house 16 kHz (`core.Resample`, linear) —
  MiMo refuses anything else; and keys are honestly marked **unavailable** —
  the family's HID descriptor is rejected by the kernel input stack (no
  `/dev/input`), so the evdev remap path has nothing to read. A GATT raw-HID
  key channel is on the roadmap. Pairing: hold Back + Home.

- Fixed: the 粘贴注入 panel found its way home. Its only entry button lived
  inside the legacy `.config-nav` tablist, which the redesign left permanently
  `display: none` — so the injection-backend switch added earlier this cycle
  was unreachable. A sidebar item owns it now (title, `?cfg=paste` and the
  loaders follow), and the hidden tablist is gone with its dead wiring.

- Fixed: two concurrency defects on the capture path. The wake capture now
  claims the microphone as its own hub source (`wakeClaim`, P2-2) — it used
  to force-acquire under the remote's id, so the voice button could not
  preempt it (two captures interleaved text in the focused window) and a
  wake hit could steal the mic from a phone; now a busy mic drops the hit
  and a stronger claim preempts the capture. Commands and partials publish
  transcripts non-blocking like dictation (P2-1) — the bare channel send
  could hang the paste path when the subtitle stream filled or its reader
  exited.

- Changed: detail sweep on the glass console. Dead references to the
  pre-redesign chips / sidebar foot / crumb are deleted (they pointed at
  elements that no longer exist), subtitle inactive lines lift to readable
  contrast, truncated texts (最近识别、常用语项) carry their full string in
  `title`, the remote figure tabs answer `aria-selected`, the phone talk
  button vibrates on press and its transcript is `aria-live`, and the 词句
  floating button grows an icon.

- Changed: the homepage speaks for every input device, not one brand. The
  hero title is now the product verdict (小遥 · 待检查/待连接/语音就绪)
  instead of a hardcoded device name — that string never updated anyway —
  and the sub-line frames the product as 小米 / Google / Android TV 遥控器
  and phones sharing one dictation chain. Onboarding, the subtitle empty
  state, the page title and the README intro/tagline follow; the
  遥控器 meta line reads 语音设备. The PWA manifest drops the last stale
  colour values.

- Changed: the whole admin wears the subtitle stage's glass (深玻璃掌控台).
  Every surface — sidebar, hero, cards, panels, dialogs, the 词句 button —
  is frosted glass over a deep slate/emerald ambient, with the phone page
  on the same palette, so the app finally reads as one product. Dense form
  controls keep a solid-ish fill (readability first), the lyrics stage is a
  cool-dark solid in the same family, heat cells and badges ride emerald
  tints, and hotspots over the remote photo are dark-glass chips.
  `tokens.css` owns the palette; `design-qa.md` marks the light round's
  colour conclusions as history. The phone page follows: its talk slab and
  toast are glass, the recording states ride the warn/bad tokens (no more
  iOS fallbacks), and one theme-color meta remains.

- Fixed: the lookback window and the wake word have one home (S8/S7).
  `config.Validate` rejects `lookback_seconds` outside 5..60 — writing 100
  used to sail through and the ring silently clamped to 60, so the file and
  the UI disagreed. `appconfig.Normalize` fills the defaults (0 → 30, empty
  word → 小遥小遥), `core` owns the constants, and the ring survives a
  re-set of the same window (the compare used to run against the unclamped
  request and rebuilt — dropping the buffered audio — on every call).

- Changed: the fallback chain opens with the last winner. `Chain` already
  recorded which provider last succeeded; `Transcribe` now attempts it
  first, so a dead primary costs one failure per session instead of one per
  utterance. The agent's two LLM locks collapse into one (three fields,
  two locks, zero nesting between them).

- Fixed: the trust trio on the admin UI. 恢复默认 asks before clobbering
  custom mappings; the ASR page's 当前生效 line distinguishes configured
  from running (已配置…但引擎未运行 with the plain-speech reason); the scan
  list's green 添加为遥控器 only stays green for remote-shaped matches —
  keyboards and headsets get a grey 仍要添加… with a confirm.

- Fixed: the dialog and the phone stop cutting corners. 新手引导 closes on
  Esc and manages focus; 字幕复制 and MCP 撤销 report their outcome; the
  手机输入 hint drops the `config.phone.enabled` jargon; the phone page
  allows pinch-zoom, labels its pair inputs, and the talk button takes
  pointer capture so a slide-off no longer ends the recording mid-word.

- Changed: the wake word toggle carries its cost. S7 matches over the
  recognition chain, so a cloud engine is a standing metered request
  stream — the 字幕页 now says so (and points at whisper.cpp) whenever the
  toggle is on and `asr_local` is false.

- Fixed: the dead switches got their wires (易用性评审 P1). `ui.Deps` shipped
  five unwired fields — `LookbackConfig`/`SetLookback`/`RecoverLookback` and
  `HistoryPolicy`/`SetHistoryPolicy` — so `/api/lookback` and
  `/api/history/policy` answered 503 and the 回眸 controls plus the 历史
  保留/脱敏 switches behind them were decoration. All five are connected;
  `TestAdminDepsLeaveNoFieldUnwired` refuses to let another field ship the
  same way. The 手机电源键 checkbox now loads at init instead of only after
  a visit to the 字幕 panel, and every toggle binds its change listener once.

- Fixed: the history policy applies live. `Store.SetPolicy` trims the
  retention window and rewrites the file at once (later appends follow the
  new redaction rule), the composition root moves the running store
  alongside the saved config, and the save reply no longer claims
  `restart_required` — the 『已保存并生效』 receipt is now the truth.

- Fixed: the LLM rewrite mode travels as a parameter (P1 竞态).
  `ProcessMode` used to stash the call's mode into shared `OpenAI.cfg`, so
  concurrent rewrites (remote + phone, or S5 app rules in play) could carry
  each other's system prompt. Also: `go test -race ./internal/agent` is
  green again (the wake spy reads its transcripts under the lock).

- Changed: the admin UI wears its receipts. The three experimental toggles
  (边说边滚字/唤醒词/回眸) report 保存成功/失败 inline instead of fetching
  into a void; every save whose reply carries `restart_required` raises one
  global banner with the restart command to copy (`needsRestart` in
  ui-core.js pins the contract). The hero line speaks plainly — a missing
  key file now says where to put the key (`friendlyAsrError`, node:test
  pinned) instead of quoting `os.ReadFile` — and the 语音识别引擎 card grows
  back the one-step 切换 dropdown (`/api/asr/switch`).

- Changed: labels and docs tell the truth. 生成诊断摘要 → 下载加密诊断包
  (what the button always did); README's 多设备 claim now admits 按键多
  设备、语音单设备, and the roadmap drops S5/S1–S3/S4/S7/S8/S9 which have
  shipped. `--muted`/`--label` lifted to WCAG AA contrast on the light
  theme.

## 0.3.0 — 2026-11-13

- New: QR pairing. The 手机配对 card now renders a QR encoding the
  auto-filling link (`…/phone#pair=NNNNNN`, the fragment never reaches the
  server log) next to the six-digit code — scan on the phone and land on
  the pairing screen with the code already in place.

- New: encrypted diagnostics export (R10 收官). The maintenance page's
  下载诊断 now seals the pack in the browser under a passphrase you set
  (PBKDF2-HMAC-SHA256 600k + AES-256-GCM, header format `OVBDIA1`) — the
  passphrase never reaches the agent and the attachment carries no
  plaintext. `xiaoyao diag <pack.ovbdiag> [-o out.json]` decrypts it
  again; redaction is unchanged.

- Changed: the product ships as `xiaoyao` (小遥). Binary, config/state
  dirs, helper socket, systemd units, logrotate, `XIAOYAO_TOKEN` and every
  metric (`xiaomi_remote_*` → `xiaoyao_*`) were renamed from `xiaomi-*`;
  breaking for existing installs — see README 「从旧名迁移」 for the
  one-shot migration. The example config no longer carries the dead
  `keyd_config` key.

- Fixed: sweep audit defects (#8-#13) — Revoke 落盘失败回滚分裂态（被撤销手机
  活会话切断、重启无复活）；诊断摘要不再带出最后一次转写正文（no speech 承诺）；
  同设备重连把缓冲半句话转写而非丢弃（I5 断开不丢音频）；ASRConfig 深拷贝交付 +
  copy-on-write 发布（并发 map 读写 panic 消除）；按键队列永不 close（send on
  closed channel 结构性消灭）；配置持久化锁外落盘（读侧不再被磁盘 IO 锁死，
  MCP 鉴权免整份 fsync）。


- New: the 常用语浮层 (S2, SayAll 对标收尾). The phrase-picker keymap
  action (F21) opens an eight-slot overlay — click or press 1–8 to insert
  a phrase into the focused window, Esc to close — and a floating 词句
  button opens it without a key. Inserts ride the shared paste path and
  never enter history.

- New: voice commands (语音命令, SayAll 超越). An exactly-matched command
  utterance — 「复制」 — runs its key combo (「ctrl+c」) instead of being
  pasted as dictation; 「复制。」 fires the same. Opt-in per the 词典短语
  page's new section (toggle + editable rows + a one-fill recommended
  list); when off, the same words stay literal text. Commands are spoken
  acts like phrases: no history, no "last text", and the subtitle stream
  announces 〔指令〕复制 → ctrl+c. A failed combo falls back to plain
  dictation. `paste.Key` executes the combo (wtype / xdotool / System
  Events).

- Redesigned the admin web UI as the "一眼掌控台" glance console: one
  readiness verdict with live telemetry, three attention cards (phone,
  ASR engine, key mapping), and a 最近听写 list on the overview; the
  remaining surfaces keep every feature in the new warm-light visual
  language (deep emerald accent, hairline cards, no shadows). Usage
  stats moved to 历史, MCP grants and config backup/diagnostics to 权限.
  See `design-qa.md` for the visual QA record.

## 0.2.0 — 2026-11-12

Security hardening from the wave-0 audit, a real phone input source, and
the first skeleton extraction. Heads-up: the phone hardware path ships
ahead of its on-device acceptance run — pairing and hold-to-talk are
ready for use, but the acceptance checklist (roadmap 未验证) is still
open and a follow-up release will confirm it.

Wave-1 boundary hardening (roadmap R4, ARC-12): the phone's power key is
an explicit opt-in, off by default.

- New: `phone.allow_power_key` (default `false`). The helper's `inject`
  command is open to any peer that passes the socket group check, and
  logind treats KEY_POWER as a shutdown request, so the inject whitelist
  used to let every paired phone power the host off. Both ends now refuse
  `power` independently until the agent pushes the policy over the
  privileged `set_devices` command; a later sync without the flag
  withdraws it. Like the rest of `phone.*`, it applies on restart.
- The phone greeting carries `allow_power`, asked of the same injector
  that decides whether a press passes, and the page disables the 电源
  button (with a one-line note) while the policy is off.
- Fixed: on the phone page, author display rules (`.panel` grid in
  landscape, `.talk` block) outranked the UA's `[hidden]` style, so the
  pairing panel stayed beside the remote in landscape and hold-to-talk
  stayed visible without a microphone. One `[hidden]` rule restores it.

Wave-2 product work first slice (roadmap R3): the phone is reachable.

- New: a 手机输入 switch in the 输入源 panel (`POST /api/phone/enabled`).
  It saves `phone.enabled` through the same validated config write as every
  other setting, answers `restart_required` like a config import (the
  listener starts with the service), and shows running vs saved state until
  the restart lands. The saved intent rides the free-form `/api/inputs`
  payload as `configured` (typed status drops new keys until R5).
- Readiness now answers "can dictation work": the ASR chain is the core,
  and the root remapper, the BLE remote and the virtual mic move to
  `optional_ok` — a phone-only setup is no longer permanently 503 /
  待检查. The quickstart renders 核心必需 / 可选（不影响就绪） groups and
  the hero follows the same rule. The split is provisional until the
  hardware run confirms the voice-key-without-helper yardstick.
- README gains the missing 配对手机 section: same-LAN requirement, the
  pairing-code flow, and the "accept one TLS exception, never install the
  certificate as a root" warning, with troubleshooting pointing at the
  capability matrix.

Wave-1 leaf extraction (roadmap R2): two new foundation packages.

- `internal/core/audiofmt.go` is now the single definition of the shared PCM
  geometry and duration policy: the 16 kHz rate, the default ADPCM frame
  length, the 5–60 s utterance window with its 30 s default, the per-second
  sample cap and the rate-aware 250 ms transcription floor. The scattered
  16000 literals and the `atvv.SampleRate` / `phone.sampleRate` pair are
  gone; production code greps to exactly one rate definition (R5).
- `internal/fsutil` owns every durable file write: `WriteAtomic`
  (tmp + fsync + rename), `AppendJSONL` and `RewriteJSONL`. The eight
  hand-rolled tmp+rename copies and the two JSONL appends all call it, and
  every path now fsyncs before rename, so a crash cannot leave a half-written
  state file (R7).

Wave-0 handover closed — the last two residuals (roadmap R1):

- SEC-06 residual: an ASR `binary` may no longer be a shell or generic
  runtime (sh/bash/dash/zsh/env/busybox/python*/perl/ruby/node). The guard
  checks the basename both before PATH lookup and after symlink resolution,
  so a friendly link name cannot smuggle an interpreter in; the error says
  the ASR command must be the recogniser itself.
- DOM-12 residual: the unused `Profile.Voice` / `Profile.Keys` fields and
  the `VoiceSpec` type are gone, and the embedded device presets no longer
  carry the dead `voice`/`keys` blobs. Older JSON that still has the keys
  keeps loading — they are ignored, as before.

Security, correctness and product work from the wave-0 audit handover
(`docs/design/evolution-roadmap.md`), plus the first SayAll-inspired feature:

- SEC-01: the root helper socket authenticates every peer with `SO_PEERCRED`.
  Mapping/device changes, capture and status are limited to root and the
  configured agent uid (`remap --agent-uid`, wired through the unit template);
  only the key-injection whitelist stays open to the socket group.
- SEC-03/SEC-04/DOM-08: `phone.tls: off` is only accepted on a loopback
  `phone.addr`, phone settings are validated whether or not the phone input is
  enabled, and the self-signed phone certificate is a one-year leaf (never a
  CA) that renews 30 days before expiry. Docs now say "accept one TLS
  exception, do not install this certificate as a trusted root".
- SEC-06: admin POSTs need same-origin evidence (an empty `Origin` no longer
  passes), and `asr.command` / `whisper.cpp` binaries must be owned by root or
  the user, executable, and not writable by group or others.
- SEC-07/SEC-08: pairing rate-limit windows and WebSocket tickets are swept on
  expiry and capped, and phone transcription runs through a bounded FIFO queue
  (default 4, `MaxQueuedUtterances`) consumed in capture order — a full queue
  answers `busy` instead of pinning megabytes of PCM behind a slow provider.
- CON-08: phone token persistence no longer runs under the store lock, so a
  slow disk cannot freeze authentication.
- New: usage statistics ("使用统计") on the overview page. A separate
  `usage-stats.json` keeps lifetime utterance/character/speech-time totals,
  the longest single dictation and a 180-day per-day breakdown; `GET
  /api/stats` returns counts and durations only, never speech content.
- Tests run green on macOS again: `parseModalias` is platform-neutral and the
  helper socket tests use short paths (macOS `sun_path` limit).

Concurrency and lifecycle fixes (CON-02…CON-07) and two more SayAll-inspired
features:

- CON-02/CON-03: helper capture handshakes are deadline-bounded and run
  outside the admin mutex, and the transcription chain carries the caller's
  context with an audio-length-scaled budget instead of a fixed 180 s on
  `context.Background()`.
- CON-04/CON-05: pw-cat writes are deadline-bounded (a wedged player is
  recycled instead of stalling the audio path), closing a sink stops its
  watchdog, and a restarted sink lives on the agent's lifetime context rather
  than the HTTP request that asked for it.
- CON-06: the SSE stream opens with a state snapshot (capture + listening) and
  a full buffer drops the oldest events with a visible `stream_gap` instead of
  dropping silently (I4).
- CON-07: the phone listener gets full HTTP read/write/idle timeouts, a WS
  read idle deadline and a server ping; a silent peer frees its slot.
- DOM-06: preemption now stops the losing source. The hub calls `Preempt` on
  the stolen claim, the phone is notified (`preempted`), and its half-spoken
  utterance still reaches the chain in capture order (I1).
- New: configuration export/import (validate-first, timestamped backup of the
  previous file, restart to apply) and a one-click redacted diagnostics
  summary (version/platform/ready/status/ASR checks; MAC addresses redacted,
  home paths scrubbed).
- New: common phrases ("常用语", SayAll 对标). The snippet library doubles as
  a phrase list in config order; the keymap gains `phrase1`–`phrase8` actions
  injected as private F13–F20 signals (the same channel the voice key already
  uses). Pressing a bound key pastes that phrase into the focused window
  through the same serialised paste as a transcript, announces it on the SSE
  stream and the subtitle panel, and never writes it to history or the log.
  Unbound slots are a no-op. The text panel labels each snippet with its slot
  and the keymap picker offers a 常用语 group.

Small correctness fixes (DOM-10/11/12/14):

- Dictionary replacements insert literally — a "$1" in a target stays
  literal — and rules no longer stack: each input span is rewritten once.
- Keymap changes save the config before touching the root helper, so a
  failed save can no longer leave the daemon on a mapping that exists
  nowhere on disk.
- A held remote key repeats again (kernel `value=2` events): key actions
  re-tap at the source's repeat rate and macros re-execute per repeat.
- Fixed: holding the voice key dictated nothing. The repeat re-tap above also
  applied to F24 and the phrase keys, which are private signals to the agent's
  key watcher rather than keys for the focused app — the watcher only accepts
  `value=1`, so it read every re-tap as a brand-new press. Holding the button
  opened the microphone every 50 ms and cut the capture into fragments below
  the 250 ms transcription floor, all of them silently discarded. Trigger keys
  now fire exactly once per physical press; every other key keeps repeating.
- History entry ids are unique even within one clock tick (Delete removes
  exactly one entry), and the rewrite path fsyncs before the rename so a
  crash cannot leave the history empty. `Agent.SetHub` is mutex-guarded.

SayAll feature ports: local Agent access, the usage calendar, and a
long-session warning.

- New: read-only MCP access for AI clients ("本地 Agent 访问"). The admin UI
  mints one 256-bit token per client (SHA-256 stored, raw shown once) with a
  ready-to-paste stdio config snippet; `xiaomi-remote mcp --token …` serves
  history_list / history_search / usage_stats. Revoking a grant fails the
  next call immediately, device addresses in responses are redacted, and the
  0600 audit log records counts only — never speech or query text.
- The usage panel gains a 26-week calendar heatmap alongside the 14-day bars.
- The phone hold-to-talk button warms to amber and then red as a hold nears
  the server's utterance cap (SayAll 长会话提醒).

DOM-09/13 and two more SayAll ports:

- Source identity now travels with every utterance: history rows and the
  subtitle stream carry the source kind and friendly device name instead of
  a bare address, and a capture cut at its time cap is labelled `truncated`
  uniformly for the remote and the phone (I8).
- Pairing codes are consumed exactly once even when the pair is then
  rejected, generated device ids can never overwrite an existing device,
  and a phone socket closes with its session.
- New: lifetime key-press statistics. The root helper counts presses at the
  single applyKey funnel (physical keys and phone injection alike), keeps
  them in `keycounts.json` across restarts and reports them through status;
  the usage panel shows the total and the three most-used keys.
- New: docs/capability-matrix.md — device × capability, source × behaviour
  and a symptom table for the mismatches that do exist on Linux (SayAll
  能力矩阵移植).

- DOM-07: phone key injection runs on a per-connection ordered queue, so a
  wedged root helper can no longer stall that connection's audio frames.
- New: combo actions (`seq(...)`) — a key can play an ordered sequence of
  taps, chords and pauses (SayAll 组合动作「一键做更多」), replaying on
  hold-repeat.
- New: per-app keymap switching (SayAll 切得准). `keymap.app_rules` matches
  the focused window class (X11 WM_CLASS substring) and applies the mapped
  profile at runtime as a pure overlay — the configured profile is never
  rewritten, and an unmatched app falls back to it.

macOS platform verification and UI adaptation (compat, not a port):

- Deep verification on darwin/arm64 against a real ARN9 remote: CGO and
  nocgo builds both produce a working binary; `parseSPBluetooth` matched the
  remote as `rc003` from live `system_profiler` data; the CoreBluetooth bridge
  negotiated ATVV caps (16 kHz / codec 0x02); `pbcopy` + `osascript` paste
  round-tripped; doctor, status/readyz/metrics/diagnostics, config export and
  the MCP stdio server (token mint → handshake → tools → 401 rejection) all
  pass.
- The admin UI is now platform-aware: `StatusResp` carries `platform`
  (IDL field 24, model and handler in step). On macOS the keymap card reads
  "不适用（原生 HID）", the mic card "直采模式（ASR 直采，无虚拟麦）", the
  autostart card "手动启动（launchd）", the permissions page swaps to the
  Bluetooth/Accessibility list, and the quickstart treats mapping and the
  virtual mic as satisfied by design — `mic_healthy=false` is expected there,
  not a fault. The banner chip says "原生 HID" instead of "映射不可用".

Phone as a second input source — Phase 1 (virtual remote control) and
Phase 2 (phone microphone).

- Input abstraction (`internal/input`): a source Hub over the BLE遥控器,
  wrapped without behavior change, ready to also carry phone sources.
- `SpeechPipeline` extracted in `internal/agent` so the remote's keys, its own
  dictation and phone audio share one ASR / rewrite / paste / history path.
- Phone pairing (`internal/phone`): 6-digit code → 256-bit token (stored as
  SHA-256, returned once) → 60-second one-time WebSocket ticket; self-signed
  ECDSA TLS with LAN-covering SANs; an independent `0.0.0.0:8788` listener
  that serves only the phone page and the WebSocket key channel, never the
  admin API.
- Key injection: whitelisted `inject` command on the root helper's local
  Unix socket, reusing the active keymap profile.
- Config: new `phone` section (disabled by default) with address/TLS/limit
  validation.
- Admin UI + endpoints: 输入源 panel with a live source list, pairing code
  and countdown, a pairing link (`#pair` fragment only, never in query or
  logs), and paired-device revoke. Admin stays on `127.0.0.1:8787`.
- Phone microphone (Phase 2): a paired phone browser can now hold-to-talk.
  AudioWorklet capture, a main-thread 16 kHz mono Int16 resampler and 20 ms
  WebSocket frames feed the same speech chain as the BLE remote, so phone
  dictation gets the same ASR provider, text rules, LLM rewrite, desktop paste,
  subtitle stream and history as the remote. Microphone ownership is
  arbitrated by the input hub over the capture window only: phones are mutually
  exclusive, the BLE remote preempts so RC003 audio is never dropped, a
  disconnect releases, and release happens before ASR so a phone is never held
  busy through a recognition it cannot use.
- The phone page is now the real PWA (pairing, D-pad, hold-to-talk, level
  meter, transcript, reconnect with backoff) served on its own strict CSP that
  permits the microphone, unlike the admin UI that denies it.
- Desktop subtitles name the speaking source: the listening pill and mic chip
  read "客厅手机 · 正在聆听…" for a phone and "遥控器 · 正在聆听…" for the
  remote, driven by one hub state stream instead of a remote-only channel.
- Deferred to Phase 3: pairing QR image, phone metrics and audit counters,
  rate-limit hardening.

Robustness fixes from the architecture audit (`docs/design/architecture.md`,
tracked item-by-item in `docs/design/evolution-roadmap.md`):

- Capture hand-off: ending a capture now drains the buffer on the dispatch
  goroutine instead of inside the background request, and a new capture starts
  from a fresh buffer rather than rewinding the previous one. Releasing the
  button and pressing it again quickly no longer loses the first sentence.
- A duplicate audio-start from the remote (the common case of a lost
  audio-stop) closes the open capture and keeps the microphone claim, so two
  sentences are no longer merged into one utterance.
- The sample rate the remote negotiated in its ATVV caps is now carried on the
  utterance and used for the request header, the provider guard and the
  minimum-length floor; before, every capture was labelled 16 kHz whatever the
  device said, and a non-16 kHz codec is now logged instead of being silently
  transcribed as sped-up speech.
- A device-reported frame size is clamped (`atvv.MaxFrameSize`): an unusable
  value falls back to the default instead of making the accumulator wait
  forever for a frame that never completes, and oversize input resyncs the
  accumulator rather than stalling it.
- The BlueZ transport guards its D-Bus handles and callbacks behind one lock,
  closes idempotently, refuses to install handles after close, and returns an
  error instead of calling a nil characteristic when used before connecting.
- Words spoken when a source disappears are no longer thrown away: a remote that
  drops, or a phone killed while holding to talk, hands its buffered audio to the
  same speech chain, so the text still reaches the focused window, the subtitles
  and the history. At service shutdown the capture is still dropped rather than
  pasted into a desktop that is going away.
- Revoking a paired phone now closes its live session instead of waiting for the
  next reconnect: the keys and microphone stop immediately, the microphone is
  released for other sources, and the admin panel says so. A revoked device's
  in-flight sentence is discarded, not transcribed.

Privacy fixes from the same audit (SEC-02):

- The recognised sentence is no longer written to the log. `recognized` records
  character count, language, provider, input source and latency, and a Bluetooth
  address appears as vendor prefix plus last octet (`AA:BB:CC:**:**:FF`) - enough
  to tell two remotes apart, not enough to identify one.
- Two quieter leak paths closed. An ASR/LLM provider's error body is reduced to
  the HTTP status plus the machine-readable `error.type`/`error.code`, because a
  content-policy rejection quotes the text it rejected; and the stderr of a
  user-configured command ASR front-end, where such tools often print recognised
  speech, is only written at debug level while the error keeps its size.
- `agent.log` moved out of the git checkout to
  `~/.local/state/xiaomi-remote/agent.log`, so it cannot be committed or shipped
  with a repository archive. logrotate follows the same path, resolved for the
  service user even when `make install-logrotate` runs under sudo.
- `internal/agent/log_test.go` asserts the promise against a captured log sink:
  the transcript must stay out of it, including when the LLM stage fails and when
  the BLE address is switched.

Configuration boundaries (ARC-03, internal — the config file format is unchanged):

- Loading or saving a config no longer drags in the ASR, LLM, text-correction
  and key-remapping subsystems. `internal/config` is a leaf again: it owns the
  document shape and the structural rules, and the section types it embeds are
  declared there instead of being borrowed from four subsystems.
- The checks that need a subsystem's own vocabulary — the ASR provider
  catalogue, the keymap action names, the LLM presets, the device profile table
  — moved to `internal/appconfig`, which wraps every load and save so the gate
  cannot be skipped by a new call site.
- The default MiMo model name has one definition (`config.DefaultASRModel`);
  the provider client and the generated config both read it instead of each
  carrying the same literal.

The dashboard shows when the last recognition happened (ARC-05):

- New: `GET /api/status` carries `last_at` (RFC3339, absent until the first
  utterance), and the overview's "最近识别" line now reads
  "最近识别（5 分钟前）：…" so you can tell a fresh transcript from yesterday's.
- Fixed: the Thrift face of the API silently dropped `platform`, `key_total`
  and `key_counts` on status and `source`, `source_name`, `truncated` on history
  entries — the generated codecs had no read/write cases for those field IDs,
  so only the JSON face worked. Regenerating from `idl/xiaomi_remote.thrift`
  restores them (I10).
- Internal: the status read model is a typed `core.StatusView` end to end
  (producer → service → HTTP edge), replacing four layers of `map[string]any`
  key assertions; a renamed key is now a compile error instead of a silent
  zero. The diagnostics report keeps its exact JSON shape and still scrubs
  home paths out of the status strings (I7).

Diagnostics export no longer contains speech (I7):

- Fixed: `GET /api/diagnostics` promised "no device addresses, no home paths, no
  speech" but embedded the whole status object, including `last_text` — the last
  sentence you dictated. Anyone attaching that summary to a public issue was
  publishing it. The transcript is now dropped from the report; the rest of the
  status, the redacted address and the readiness checks are unchanged.

One guard for both network boundaries (ARC-04):

- Fixed: the phone listener did not check the origin of its HTTP POSTs at all.
  Only the WebSocket handshake did. A page on any other site could therefore
  send a pairing code to `/api/phone/pair` and receive a device token, or spend
  a stolen token on `/api/phone/ticket`. The same-origin rule the admin UI
  gained in SEC-06 — an empty `Origin` is not evidence — now applies to the
  phone boundary too, and to PUT/PATCH/DELETE as well as POST.
- The origin rule, the loopback-host rule and the security headers now have one
  implementation (`edge/netguard`) that both boundaries call. The two pages
  keep their own Content-Security-Policy and Permissions-Policy, because the
  admin page denies the microphone and the phone page must allow it; that
  difference is a parameter now, not a second copy of the code.
- If you front the phone listener with a reverse proxy (`phone.tls: off`), the
  proxy must preserve the `Host` header. Otherwise the browser's `Origin` will
  not match it and pairing is refused with 403.
- Internal: the phone listener no longer embeds the web asset tree. The
  composition root injects it, so a LAN-facing transport does not depend on the
  presentation layer.

One composition root, no package globals (ARC-06, internal):

- Fixed: building a second HTTP engine in one process silently rewired the
  first. The generated handlers read a package-level backend variable, so the
  last engine constructed served every route. The backend now travels on the
  request context (`WithBackend` middleware), and the custom routes take the UI
  server as a parameter instead of reading a global. A route whose engine has
  no backend still answers 503 rather than panicking.
- The phone wiring (transcription adapter, pairing/revocation payloads, input
  source listing) moved out of `main.go` into `app/runtime`, where it is tested
  inside its own package. The 输入源 panel keeps distinguishing the running
  listener from the saved `phone.enabled` intent across that move.

Durable writes fail cleanly, and activity data stays private (R7):

- Fixed: a failed atomic replace left its `.tmp` file behind. The next writer
  inherited it, and anything listing the directory saw a state file that was
  never committed. Every failure after the temp file exists — write, fsync,
  close or rename — now removes it, and the original file is still untouched.
- Fixed: `keycounts.json` was the one state file written world-readable (0644
  in a 0755 directory). Key-press counts are activity data — which keys, how
  often — and only the root remapper reads them, so they now land 0600 inside
  0700 like every other state file.
- Two repo-wide guard tests keep the single-definition rules from drifting
  back: no production file outside `internal/core` may hardcode the sample
  rate, and no package outside `internal/fsutil` may rename files itself.

## 0.1.1 — 2026-11-10

Maintenance and quality baseline.

- Release automation: tag-triggered GitHub Release with linux amd64/arm64
  binaries, SHA-256 checksums and changelog-derived notes.
- Community baseline: issue forms, pull request template and Dependabot
  (Go modules + GitHub Actions, weekly).
- CI hardening: race detector and atomic coverage on the Linux test job,
  with a coverage artifact. The race detector surfaced a data race in the
  service test fake (`fakeHelper` shared fields) which is now mutex-guarded;
  production `helper.Client` dials per call and was already safe.
- Core tests: helper Unix-socket protocol (fake server), paste empty/Wayland
  paths, F24 watcher construction, and an `agent.finalize` regression suite
  driven by a fake ASR engine (dictionary, snippets, no-speech, short audio,
  errors and empty text).

## 0.1.0 — 2026-11-10

Initial public release.

- BLE/ATVV voice input for Xiaomi RC003 / RC001 / Remote 2 Pro family, plus
  generic ATVV and Apple Siri Remote (buttons only) profiles.
- Multi-device profiles with BlueZ status, battery and hotplug re-grab.
- Pluggable ASR: MiMo, OpenAI-compatible endpoints, whisper.cpp and local
  commands, with fallback chains.
- Optional LLM post-processing and local dictionary/snippet rules.
- Frosted lyric subtitle view with live listening state.
- Local history (JSONL, 200 entries), search, copy, delete and JSON export.
- Root uinput key remapper with named profiles.
- Embedded web UI: guided overview, grouped settings, real-time status,
  recent transcripts, ASR latency and health indicators.
- Prometheus metrics for BLE, ATVV, microphone health, transcripts and ASR
  latency.
- Automated tests, Linux CI and macOS/Windows cross-build checks.
- Design note for phone/virtual input sources: `docs/design/external-input-sources.md`.