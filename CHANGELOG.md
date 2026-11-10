# Changelog

## Unreleased

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