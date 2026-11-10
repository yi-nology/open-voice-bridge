# Changelog

## Unreleased

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