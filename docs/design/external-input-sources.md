# 外部输入源设计：手机 / 虚拟设备作为麦克风与遥控器

- 日期：2026-11-08
- 状态：待评审
- 范围：设计，不含实现
- 关联代码：`internal/agent`、`internal/audio`、`internal/remap`、`biz/handler/ui`、`main.go`

## 1. 背景与目标

当前服务只支持一条物理输入链路：小米蓝牙遥控器 → BLE/ATVV → 音频桥接 → ASR → 文本规则/改写 → 粘贴与字幕。按键则由 root remap 服务独占 evdev 后注入。

希望让手机（真机、模拟器或任何浏览器）成为**第二类输入设备**：

1. **无线麦克风**：手机按住说话，音频实时进入同一套 ASR、改写、历史、字幕与粘贴链路。
2. **虚拟遥控器**：手机提供方向、确认、返回、主页、音量与语音键，按键最终注入系统，与物理遥控器等效。
3. **多源共存**：BLE 遥控器与手机可同时在线；桌面端能看到“谁在说话、谁连上了”。
4. **不牺牲安全边界**：管理 UI 继续只监听 `127.0.0.1`，手机走独立端口、配对鉴权与 TLS。

### 非目标

- 不做手机端原生 App（浏览器 PWA 优先）。
- 不替代物理遥控器，也不改变现有按键映射语义。
- 不做互联网公网暴露；默认只面向同一局域网。
- 不做连续监听/VAD 常开；先用“按住说话”界定一句话，后续可加 VAD。

## 2. 关键约束

### 2.1 浏览器的安全上下文

`getUserMedia` 只在安全上下文可用：`https://` 或 `localhost`。手机通过 `http://192.168.x.x` 打开页面**无法录音**。这是方案成立的前提，必须二选一：

- **自签证书**：服务为手机端口生成自签 ECDSA 证书，手机首次访问手动信任；完全离线。Android Chrome 通常可继续访问；iOS Safari 对自签证书更严格，可能需要安装描述文件或改用可信证书。
- **反向代理/隧道**：Caddy、Tailscale Serve 等提供可信 HTTPS，服务手机端口只监听回环并相信代理头。

设计上两种都支持：`phone.tls = auto | off`。`off` 表示前面已有可信 HTTPS 代理。

### 2.2 现有管理面是刻意只开回环的

`config.Validate` 强制 `ui_addr` 必须是 `127.0.0.1/localhost/::1`。不能为了手机把管理 UI 直接暴露到局域网。

**决定：新增独立监听器**（默认 `0.0.0.0:8788`），只暴露手机页面与手机协议；管理 API、按键配置、历史管理仍只在 `127.0.0.1:8787`。

### 2.3 音频链路要复用而不是复制

`agent.finalize` 目前把“一段 16 kHz 单声道 PCM”送 ASR、文本规则、LLM、粘贴与历史。手机音频也应走同一条路径。需要把这段逻辑从 BLE 会话中抽成可复用的 `SpeechPipeline`，否则会出现两套行为不一致的转写逻辑。

### 2.4 按键注入必须经过 root helper

手机按键不能直接操作 X11/uinput。现有 `xiaomi-remote-remap`（root）持有 uinput 并执行映射；需要给它增加一个**白名单注入命令**，由 agent 在通过配对鉴权后转发。

## 3. 方案对比

| 方案 | 实时性 | 复杂度 | 兼容性 | 结论 |
|---|---|---|---|---|
| **A. WebSocket + PCM16 分片（推荐）** | 好（局域网 <50 ms 传输） | 中 | 所有现代浏览器 | 采用 |
| B. WebRTC（Pion） | 最好，自带抖动/回声 | 高（信令、NAT、ICE） | 好 | 当前场景杀鸡用牛刀 |
| C. 手机原生 App | 最好 | 最高（双端开发/分发） | 需安装 | 后续可选 |
| D. 录完再上传 | 差（一句话结束才上传） | 低 | 好，且不强制 HTTPS | 仅作降级 |

推荐 A：按住说话本身就把一句话切好了，不需要 WebRTC 的连续媒体能力；WebSocket 二进制帧足够，且服务端可直接拿到 PCM，省掉转码依赖。

## 4. 总体架构

```
┌──────────────┐   BLE/ATVV    ┌──────────────────────────┐
│ 物理遥控器    │──────────────▶│ InputHub                 │
└──────────────┘               │  ├─ RemoteSource (现有)   │
                               │  └─ PhoneSource (新增)    │
┌──────────────┐  WSS + token  │        │                 │
│ 手机浏览器    │──────────────▶│        ├─ 音频 → SpeechPipeline → ASR/改写/粘贴/历史/SSE
│ /phone (PWA) │               │        └─ 按键 → root helper.inject → uinput
└──────────────┘               └──────────────────────────┘
        ▲                                   │
        └──── transcript / listening ◀──────┘
```

### 4.1 组件

- **InputHub**：管理所有输入源的生命周期、状态与仲裁（同一时刻只允许一路音频在说话）。
- **RemoteSource**：包装现有 agent 的 BLE/ATVV 链路，行为不变。
- **PhoneSource**：每个已配对手机一个实例，负责 WS 会话、音频拼接、按键转发、心跳与重连。
- **SpeechPipeline**：从 `agent.finalize` 抽出的纯处理链：PCM → ASR → 文本规则 → LLM → 结果事件。粘贴与历史作为订阅者。
- **PhoneServer**：独立的 `net/http` 监听器（不挂在管理 UI 的 Hertz 栈上），负责 TLS、静态手机页、配对与 WebSocket；WS 使用 `github.com/coder/websocket`。这样局域网暴露面与管理面天然隔离，也不会误继承管理中间件。
- **PairingStore**：一次性配对码、设备令牌（哈希存储）、WS 一次性票据；配对关系持久化到配置目录的 `phone_tokens.json`（0600，原子写），重启后免重新配对。
- **KeyInjector**：通过 Unix socket 调用 root remap 的 `inject` 命令，仅允许白名单逻辑键。

### 4.2 核心接口

```go
type Kind string // "remote" | "phone"

type Source interface {
    ID() string
    Kind() Kind
    Name() string
    Status() SourceStatus
}

// AudioSource 由 PhoneSource 实现；RemoteSource 内部仍走现有 handleAudio/finalize。
type AudioSource interface {
    Source
    StartUtterance(ctx context.Context, meta UtteranceMeta) error
    Write(ctx context.Context, pcm []int16) error
    EndUtterance(ctx context.Context) error
}

type SpeechPipeline struct { /* asr, rules, llm, history, paste, events */ }

func (p *SpeechPipeline) Transcribe(ctx context.Context, pcm []int16, source SourceRef) (Transcript, error)
```

BLE 链路把 `agent.finalize` 改为调用 `SpeechPipeline.Transcribe`；手机链路在 `EndUtterance` 时调用同一个方法。粘贴、历史、SSE、指标都订阅统一结果，避免两套行为。

### 4.3 新增配置

```json
"phone": {
  "enabled": false,
  "addr": "0.0.0.0:8788",
  "tls": "auto",                 // auto | off（off 表示前置可信 HTTPS 代理）
  "pair_code_ttl_seconds": 300,
  "max_clients": 4,
  "max_utterance_seconds": 30
}
```

兼容策略：旧配置没有 `phone` 段时默认关闭，不影响现有启动与校验。校验规则：`phone.enabled=false` 时不监听；`phone.addr` 不得与 `ui_addr` 相同；`tls=off` 时必须由前置代理提供 HTTPS，否则手机页只能用于按键、不能录音。

## 5. 数据流

### 5.1 手机麦克风

1. 手机打开 `https://<lan-ip>:8788/phone`，首次接受证书。
2. 桌面端「设备」页生成 6 位配对码与二维码（内含 `#pair=<code>`，放在 fragment 里避免进日志）。
3. 手机用 `POST /api/phone/pair {code, name}` 换取长期令牌；令牌只存手机 localStorage。
4. 建立 WS 前先 `POST /api/phone/ticket`（Bearer token）换 60 秒一次性票据，WS 用 `?ticket=` 连接，避免长期令牌出现在 URL。
5. 按住麦克风按钮：
   - 发送 `{"type":"start","format":"pcm16","rate":16000,"channels":1}`
   - AudioWorklet 采集 → 重采样 16 kHz → Int16 → 每 20 ms（640 B）发一个二进制帧
   - 松开发送 `{"type":"end"}`
6. 服务端拼接为 `[]int16`，超过 `max_utterance_seconds` 自动截断并结束；小于 250 ms 丢弃（与现有 BLE 行为一致，阈值只在 `SpeechPipeline` 里存一份）。
7. 音频进入 `SpeechPipeline`（与遥控器同一条链）。回推顺序固定：先 `{"type":"listening","active":false}` 释放麦克风，再异步送 ASR，完成后回
   `{"type":"transcript","text","raw_text","provider","processed","latency_ms","at","truncated"}`；
   没有识别到内容时回 `{"type":"error","code":"no_speech"}`。
   会话未开启（`start` 被拒或已结束）时到达的二进制帧直接丢弃，不回帧也不转写；转写走 `async.GoSafe`，慢供应商不阻塞按键通道。
   同时走原有的历史、SSE 字幕与桌面粘贴。

### 5.2 虚拟按键

- 手机按键发送 `{"type":"key","key":"up","value":1}`，松开发 `value:0`。
- 允许键：`up/down/left/right/enter/back/home/menu/volumeup/volumedown/power/f5`（与现有逻辑键一致）。
- 服务端经 `KeyInjector` 调用 helper：`{"cmd":"inject","key":"up","value":1}`；helper 依据当前 keymap profile 映射后写入 uinput。
- 手机端 D-pad 不做服务端自动连发；长按连发由手机本地实现（每 120 ms 发一次按下/松开，单键节流），避免服务端状态复杂化。

### 5.3 多源仲裁

- 音频：全局同一时刻只允许一个 `start`；第二路返回 `{"type":"error","code":"busy"}`。桌面状态显示当前说话源。
- 仲裁实现为「手机之间互斥、遥控器可抢占」：手机 `start` 走 `Hub.AcquireAudio`，
  失败即 `busy`；BLE 在 `EventAudioStart` 走 `Hub.AcquireAudioForce`，
  永远不会因为手机正在说话而丢帧（RC003 是主路径）。
  遥控器断线时 `markDisconnected` 也会释放占用，否则一次掉线会让手机永久 `busy`。
- 按键：BLE 与手机可同时注入，不做互斥；按键是瞬时事件。
- 连接：最多 `max_clients` 个手机同时在线；超限拒绝并提示。
- 断线：未结束的音频直接丢弃（不产生半句话），手机重连后需要重新按住说话。

## 6. 接口设计

### 6.1 手机端 HTTP

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/phone` | 手机 PWA 页面（无管理权限） |
| POST | `/api/phone/pair` | `{code, name}` → `{token, device_id}`，限流 |
| POST | `/api/phone/ticket` | Bearer token → `{ticket, expires_in}` |
| GET | `/ws/phone?ticket=...` | WebSocket：音频二进制 + JSON 控制 |
| GET | `/api/phone/ping` | 认证后的健康检查（排障用） |

管理面（仅 127.0.0.1）新增：

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/inputs` | 输入源列表与状态 |
| POST | `/api/phone/pairing` | 生成/刷新配对码（返回码与二维码 URL） |
| GET | `/api/phone/devices` | 已配对设备 |
| POST | `/api/phone/revoke` | `{device_id}` 吊销令牌 |

### 6.2 WebSocket 消息

客户端 → 服务端：

```json
{"type":"start","format":"pcm16","rate":16000,"channels":1}
{"type":"end"}
{"type":"key","key":"enter","value":1}
{"type":"ping","t":1730000000}
```

`start` 的 `format`/`rate`/`channels` 可省略（省略即按 16 kHz 单声道 pcm16 接受），
给了就必须匹配：非 `pcm16`、非 16000、非单声道都返回 `bad_format`，不做静默重采样。

二进制帧：小端 Int16 PCM，单声道 16 kHz；单帧上限 240 ms（3840 采样），超限返回 `bad_frame`。

服务端 → 客户端：

```json
{"type":"ready","device_id":"phone-1","name":"Pixel","mic_available":true,"max_audio_seconds":30}
{"type":"listening","active":true}
{"type":"transcript","text":"...","raw_text":"...","provider":"...","processed":true,"latency_ms":1304,"at":"...","truncated":false}
{"type":"key_ack","key":"enter","value":1}
{"type":"error","code":"busy|too_long|unauthorized|rate_limited|bad_format|bad_frame|bad_state|no_speech|not_ready|denied|bad_message","message":"..."}
{"type":"pong","t":1730000000}
```

`mic_available` 为 false 表示服务端没配转写链，手机页应隐藏按住说话按钮而不是每次按下都报错；
`max_audio_seconds` 是服务端的整句硬上限。
第二次 `start`（上一句未结束）返回 `bad_state`，不静默重开一句。

### 6.3 helper 注入协议

```json
{"cmd":"inject","key":"enter","value":1}
```

- 只接受白名单逻辑键；未知键返回错误。
- 复用当前映射 profile；`noop` 不注入，`native` 保持原键。
- 该命令只在本地 Unix socket 上可用，不对网络暴露。

## 7. 安全设计

- **管理/手机分离**：`8787` 仅回环；`8788` 只挂手机页面与手机协议，绝不挂管理 API。
- **配对**：6 位码 5 分钟有效；每 IP 与每码限制尝试次数；常量时间比较。
- **令牌**：256-bit 随机，服务端只存 SHA-256 哈希；可单独吊销；令牌文件/内存不写日志。
- **WS 鉴权**：短期一次性 ticket，避免长期令牌出现在 URL/访问日志。
- **Origin 校验**：只接受手机端口自身 Origin；拒绝跨站连接。
- **输入限制**：帧大小上限、总时长上限、未配对不可发音频/按键；按键白名单。
- **TLS**：自签私钥 `0600`；证书与私钥放在配置目录（`phone_cert.pem` / `phone_key.pem`）；SAN 覆盖本机名、所有 LAN IPv4/IPv6 与 localhost；IP 变化时自动重建；证书/密钥也可手动删除后重建。
- **限流**：配对、ticket、WS 建连按 IP 限流；慢客户端直接断开。
- **审计**：记录配对、吊销、注入拒绝，不记录音频内容与令牌。

## 8. 桌面端与手机端体验

### 桌面端

- 「设备」页新增「输入源」区：BLE 遥控器 + 已连接手机（名称、IP、最后活跃、当前是否在说话）；已配对设备可吊销。
- 「生成配对码」按钮：显示 6 位码与二维码；到期自动刷新。
- 顶栏语音胶囊与首页麦克风球在手机说话时显示「手机 · 正在聆听」。
- 历史记录增加 `source` 字段（`remote` / `phone:<name>`）；需要同步扩展 `history.Entry`、Thrift `HistoryEntry` 与 OpenAPI。

### 手机端（PWA）

- 单页、无框架、沿用现有设计 token；深色/浅色跟随系统可选。
- 首屏：配对；配对后进入遥控界面。
- 大号「按住说话」按钮 + 实时电平 + 最近一句预览；屏幕常亮（Wake Lock）。
- 下方虚拟遥控：D-pad、OK、返回、主页、音量、语音键；按钮触感反馈。
- 顶部显示桌面端连接状态、当前 ASR provider；断线自动重连。
- 竖屏优先；横屏时左遥控右语音。

## 9. 错误处理与可靠性

- 手机网络抖动：WS 断开后指数退避重连（1s→30s），重连需重新取 ticket。
- 录音权限被拒/被系统打断：页面恢复时提示，不自动开始录音。
- 说话中切换 App：发送 `end` 或超时截断；服务端最多保留 `max_utterance_seconds`。
- TLS 证书不受信任：页面给出明确说明与「继续访问」；iOS 额外提示安装证书。
- 服务重启：手机 token 持久化于本地，重连后重新取 ticket；配对关系保留（令牌哈希落盘 0600）。
- 桌面端退出：手机显示「桌面端离线」，按键与音频进入拒绝态。

## 10. 可观测性

新增指标：

- `xiaomi_remote_phone_clients{state="connected|paired"}`
- `xiaomi_remote_phone_audio_seconds_total`
- `xiaomi_remote_phone_utterances_total{result="ok|empty|busy|error"}`
- `xiaomi_remote_phone_keys_total`
- `xiaomi_remote_phone_inject_errors_total`

状态接口增加输入源列表；日志只记录设备名/IP/事件，不记录音频与令牌。

## 11. 分阶段交付

### 阶段 1：地基（可单独验收）

- `InputHub`/`Source` 抽象；BLE 源接入但不改行为。
- 手机独立监听器、TLS 自签、配对码/令牌/ticket。
- 手机页壳 + 虚拟按键 + helper `inject` 白名单。
- 桌面「输入源」区与配对码。

验收：手机连上后可控制方向/确认/返回；管理 UI 仍只回环；未配对无法注入。

### 阶段 2：麦克风

- AudioWorklet 采集与重采样、WS PCM、服务端拼接。
- `SpeechPipeline` 抽取并接入手机音频；结果回推手机、进历史/字幕/粘贴。
- 输入源仲裁（busy）、超时与最短时长。

验收：手机按住说话 → 桌面焦点窗口出字、字幕与历史出现记录、手机看到同一条转写与耗时。

### 阶段 3：打磨

- 二维码、设备吊销、多手机、重连、Wake Lock、横屏布局。
- 指标、审计、限流、测试补齐。

## 12. 测试计划

- 单元：配对码过期/限流、令牌哈希与吊销、ticket 一次性、帧大小与时长上限、按键白名单、PCM 拼接。
- 集成：TLS 监听 + WS 握手（有效/过期/复用 ticket）、假 ASR 验证转写回推、假 helper 验证注入。
- 手动：Android Chrome 与 iOS Safari 的证书流程、前后台切换、网络断开重连、两台手机、吊销后立即失效、管理 UI 回环限制。
- 回归：现有 BLE 遥控器全链路、按键映射、历史、字幕不变。

## 13. 风险与待确认

1. **iOS 自签证书**：Safari 对自签证书更严格，可能无法直接获得麦克风安全上下文；缓解方案是反向代理/可信证书，或后续原生 App。
2. **按键注入权限**：必须新增 helper 命令并严格白名单；任何绕过配对直接注入都是严重漏洞。
3. **多源冲突**：当前设计是“一次只允许一路音频”，如果希望手机与遥控器同时说，需要引入混音或队列，复杂度显著上升。
4. **局域网发现**：先用二维码包含 IP；mDNS 可选，不作为一期目标。
5. **音频延迟**：传输本身很小，主要延迟仍是云端 ASR（实测约 1.3–1.8 s），手机端应显示真实耗时而不是承诺毫秒级。
6. **助手 socket 权限**：`inject` 只经本地 Unix socket；需要确认 socket 属主/权限仅允许 agent 用户访问，避免同机其他用户注入按键。

## 14. 决策摘要

- 形态：手机 = 麦克风 + 虚拟按键（已确认）。
- 传输：WebSocket + 16 kHz PCM16，按住说话。
- 安全：独立手机端口、配对码 + 令牌 + 一次性 ticket、自签 TLS（可切反向代理）。
- 复用：抽取 SpeechPipeline；BLE 与手机共用 ASR/改写/粘贴/历史。
- 按键：helper 新增白名单 `inject`，复用现有 keymap profile。
- 管理面：保持 127.0.0.1 回环不变。