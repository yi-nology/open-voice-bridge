# Contributing

Thanks for taking the time to improve Open Voice Bridge.

## Development setup

Requirements:

- Linux (PipeWire + BlueZ) for the full agent/remap path
- Go 1.27 or newer
- `xsel` for X11 paste; `wtype` is optional on Wayland
- Root access only for installing the remap helper service

```bash
git clone https://github.com/yi-nology/open-voice-bridge.git
cd open-voice-bridge
make build
make check
```

`make check` runs `gofmt`, `go test` and `go vet`. Hardware-dependent tests must
skip gracefully so that CI does not require a physical remote.

## Project layout

- `main.go` — CLI entry point (`agent`, `remap`, `doctor`) and runtime wiring
- `biz/` — CloudWeGo Hertz handlers, routes and generated API models
- `internal/` — agent, ASR, BLE, audio, config, history, remap, text processing
- `idl/` — Thrift IDL, the source of truth for the HTTP API
- `web/` — embedded desktop UI (`ui.html`) and static assets
- `conf/` — example configuration
- `deploy/` — systemd and logrotate templates (`@ROOT@`, `@USER@` placeholders)
- `docs/` — OpenAPI, design notes and screenshots
- `script/` — install, bootstrap and soak helpers

## IDL and generated code

Do not hand-edit `biz/model/api`, `biz/router` or `docs/openapi.yaml` unless you
are fixing a generator bug. Change `idl/xiaomi_remote.thrift` instead, then run:

```bash
make idl
make openapi
```

`make idl` requires `hz` and `thriftgo`; `make openapi` requires
`thrift2openapi`. Install them with `go install` if needed.

## Frontend

`web/ui.html` is a single self-contained page with no build step. Keep it
dependency-free and keep the admin UI loopback-only. Static assets live under
`web/assets/`.

## Pull requests

- Keep changes scoped and explain the user-visible behavior.
- Add or update tests for logic changes.
- Run `make check` before opening a PR.
- Do not commit API keys, tokens, certificates, `agent.log`, `bin/` or personal
  device addresses. Use `AA:BB:CC:DD:EE:FF` in examples and tests.
- For hardware behavior changes, describe the tested device and distribution.

## Security

Please report vulnerabilities privately as described in [SECURITY.md](SECURITY.md).