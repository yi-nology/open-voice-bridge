# Changelog

## Unreleased

Phone as a second input source — Phase 1 (virtual remote control).

- Input abstraction (`internal/input`): a source Hub over the BLE遥控器,
  wrapped without behavior change, ready to also carry phone sources.
- `SpeechPipeline` extracted in `internal/agent` so keys and future phone
  audio share one ASR / rewrite / paste / history path.
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

Phone microphone is deferred to Phase 2.

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