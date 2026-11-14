# 架构设计：分层、边界与不变量

- 日期：2026-11-10
- 状态：已采纳；随 v0.2.x 演进逐波落地
- 范围：整仓架构。[`external-input-sources.md`](external-input-sources.md) 只谈"手机作为外置输入源"这一件事的功能设计，本文谈的是所有输入源共用的骨架
- 依据：五个方向的代码审计（分层与依赖、并发与生命周期、安全与隐私、领域正确性、运维与产品），逐条发现与定位见 [`evolution-roadmap.md`](evolution-roadmap.md)
- 关联代码：`main.go`、`internal/*`、`biz/*`、`web/*`

## 1. 这份文档解决什么

审计发现的三十多条缺陷，根因集中在四个结构性缺口，不是散落的 bug：

1. **配置包反向依赖子系统**。`internal/config` import 了 `device`/`llm`/`asr`/`remap` 并在 `Validate` 里调用它们（`internal/config/config.go:73,179,303,421`），校验逻辑长在叶子层之外，新增子系统就得改 config，方向是反的，且随时可能成环。
2. **同一真相有多份副本**。16 kHz 有四处定义（`internal/asr/mimo.go:93`、`openai.go:67`、`whispercpp.go:56`、`command.go:55`）；"输入源/状态"有四处互不相干的模型（`agent.Status` map、`input.Source`、`phone` 设备视图、thrift `api.StatusResp`）；BLE 重连退避抄了三遍（`internal/agent/agent.go:343-356,357-370,385-392`）；原子写 tmp+rename 抄了五遍。每份副本都是一处未来会走偏的地方——已经走偏了：ATVV 报 8 kHz 时音频仍被贴上 16 kHz 的标签（`internal/atvv/protocol.go:72-77` 对上 `internal/agent/pipeline.go:63`）。
3. **网络边界没有共享的守卫**。管理端与手机端各自实现 TLS/头/origin/静态资源（`biz/handler/ui/server.go:88-94,145-157` 与 `internal/phone/server.go:100-102,210-243`），于是"origin 为空就放行"这类只在一份里出现的漏洞（`biz/handler/ui/server.go:126-143`）无人认领；`internal/phone` 还 import 了 `web`，把表现层塞进了传输层。
4. **阻塞调用没有截止时间**。持锁期间做同步 IO 是本仓的默认写法：转写链在整个 ASR 窗口里握着 `p.mu` 且用 `context.Background()`（`internal/agent/pipeline.go:56-64`）、管理端在 `s.mu` 里调用 root helper（`biz/handler/ui/server.go:238` + `internal/helper/client.go:98` 无读超时）、`pw-cat` 写 stdin 无截止（`internal/audio/pipewire_linux.go:214`）、`store.persistLocked` 在 `store.mu` 里落盘（`internal/phone/store.go:215,318`）。任一处都能把用户可见的行为冻住，而 SIGINT 也救不回来。

本文给出目标骨架（§3–§5）、每个单元的边界（§4）、必须成立的不变量（§5）、明确不做的取舍（§6），以及能机械验收的标准（§10）。

## 2. 现状（as-is）

```
main.go ──┬──> internal/agent ──┬──> internal/bluez ──> internal/atvv
          │                     ├──> internal/asr / llm / text / audio
          │                     └──> internal/input (Hub)
          ├──> internal/phone ──┬──> internal/atvv (常量) / input / web (embed!)
          │                     └──> (转写经 app/runtime 的 PhoneSpeech 适配器)
          ├──> biz/{service,handler/ui,handler/api,model} ──> internal/*（全部）
          ├──> internal/remap ──> internal/helper ──> (Unix socket) root 助手进程
          └──> internal/config ──> internal/{device,llm,asr,remap}   ← 反向
```

规模失衡（LOC）：`biz/service/service.go` 863、`web/ui.html` 3541、`biz/handler/ui/backend.go` 672、`internal/agent/agent.go` 601、`internal/remap/daemon.go` 599、`internal/phone/server.go` 517、`main.go` 的 `runAgent` 单函数 275 行（`main.go:179-453`）外加 115 行 doctor。非测试代码里 `map[string]any` 出现 121 次。

## 3. 目标（to-be）分层

```
 ┌────────────────────────────────────────────────────────────────────┐
 │ edge（表现层，只做协议/鉴权/头/静态资源/事件流）                     │
 │   edge/admin   127.0.0.1:8787 Hertz + SSE + 管理 UI 资源            │
 │   edge/phone   0.0.0.0:8788 TLS + 配对 + PWA 资源 + 手机 WS         │
 │   edge/netguard 一份 origin 检查、一份安全头、一份整形后的画布式地址  │
 └───────────────────────────────┬────────────────────────────────────┘
                                 │ 只经接口与 core 类型对话
 ┌───────────────────────────────▼────────────────────────────────────┐
 │ app（应用层）                                                       │
 │   app/runtime    生命周期、接线、状态视图的唯一生产者、优雅关闭次序   │
 │   app/speech     转写链：ASR→规则→改写→字幕/历史/粘贴（唯一一条）    │
 └──────┬──────────────┬──────────────┬──────────────┬────────────────┘
        │              │              │              │
 ┌──────▼─────┐ ┌──────▼─────┐ ┌──────▼─────┐ ┌──────▼──────┐  领域层
 │ source/ble │ │ source/pc  │ │ actuate    │ │ input       │
 │ ATVV+BlueZ │ │ 手机/未来源 │ │ paste+keys │ │ 麦克风仲裁   │
 └──────┬─────┘ └──────┬─────┘ └──────┬─────┘ └──────┬──────┘
        └──────────────┴──────────────┴──────────────┘
 ┌────────────────────────────────────────────────────────────────────┐
 │ foundation（叶子，不得反向依赖任何东西）                             │
 │   core      领域类型/常量/事件（采样率、帧、Utterance、SourceRef、   │
 │             StatusView/ReadyView/DevicesView）                      │
 │   config    纯数据 + 结构校验，不 import 任何子系统                  │
 │   fsutil    一份原子写 + 一份 JSONL 追加（带 Sync）                  │
 │   transport D-Bus/evdev/uinput 等 OS 适配，只被 source/* 使用        │
 └────────────────────────────────────────────────────────────────────┘
```

### 依赖规则（可机械检查，CI 里用 `go list -deps` 断言）

- **R1** `core` 不 import 任何内部包；`fsutil` 不 import 任何内部包；`config` 只允许 import `core` 与 `fsutil`。子系统的词汇（ASR 目录、keymap 动作、LLM 预置、设备预置）一律经 `appconfig` 接入，`config` 不得反向 import 子系统；段类型本身声明在 `config`，子系统反过来用它。
- **R2** 领域层不 import `edge`/`app`；`source/*` 不互相 import。
- **R3** 只有 `app/runtime`（原 `main.go`）允许同时 import 具体实现。
- **R4** `edge/admin` 与 `edge/phone` 共用 `edge/netguard` 的 origin 检查与安全头；两个边界的策略差异只能是参数，不能是两份实现。
- **R5** 采样率、帧长、时长上下限、最小可转写时长只定义在 `core/audiofmt.go` 一处；`asr` 与 `source/*` 都从这里取。
- **R6** 状态模型只有一份：`core` 里的 typed view 由 `app/runtime` 生成，`edge` 只负责序列化。`map[string]any` 只允许出现在真正的自由格式字段（用户自定义 keymap、LLM 预设参数）。
- **R7** 落盘只走 `fsutil`；任何包不得自己拼 tmp+rename。
- **R8** 任何会阻塞超过 100 ms 的调用必须收 `context.Context`，且调用点必须在锁外。

## 4. 单元职责

每个单元都要能被一句话说清，且改内部不破接口。

| 包 | 职责（一句话） | 对外接口（关键） | 依赖 | 规模上限 |
| --- | --- | --- | --- | --- |
| `core` | 定义领域词汇，不做任何事 | `AudioFormat`、`Utterance`、`SourceRef`、`Transcript`、`AgentView`、`StatusView`、`ReadyView`、`DevicesView` | 无 | 400 LOC |
| `core/audiofmt` | 16k/单声道/Int16 的唯一真相 | `SampleRate`、`FrameSamples`、`Cap()`、`MinUtterance()` | 无 | 60 LOC |
| `config` | 装载/校验/保存纯数据，并持有各段的类型与默认值 | `Load`/`Save`/`Validate`（只做结构校验）、`ASRConfig`/`LLMConfig`/`TextConfig`/`DefaultKeymap` | `core`、`fsutil` | 750 LOC（含 `sections.go`） |
| `appconfig` | 唯一把 config 与子系统词汇接起来的地方 | `Load`/`Save`/`Normalize`/`Validate`（语义校验） | `config`、`asr`、`llm`、`text`、`remap`、`device` | 120 LOC |
| `fsutil` | 原子写、JSONL 追加、0600 | `WriteAtomic`/`AppendJSONL`/`RewriteJSONL` | 无 | 120 LOC |
| `input` | 麦克风归属仲裁 + 输入源注册 | `Acquire`/`Release`/`AcquireForce`/`Watch`/`Sources` | `core` | 200 LOC |
| `speech` | 一条转写链，源无关 | `Transcribe(ctx, Utterance) (Transcript, bool)` | `core`、`asr`、`llm`、`text`、`actuate` | 200 LOC |
| `source/ble` | ATVV 遥控器：连接、退避、解码、缓冲 | `Run(ctx)`、`Status()` | `core`、`transport`、`input`、`speech` | 300 LOC/文件 |
| `source/phone` | 手机源：配对、令牌、WS、PCM 累积 | `Run(ctx)`、`Pair`/`Revoke` | `core`、`input`、`speech` | 300 LOC/文件 |
| `actuate` | 文本与按键落到桌面 | `Paste(ctx,text)`/`Inject(ctx,key)`/`Subtitle(t)` | `core`、`keys` | 250 LOC |
| `edge/netguard` | 一份 origin/安全头/回环保证 | `SameOrigin(method,host,origin,site)`/`IsLoopback(host)`/`WriteHeaders(policy,set)`、`Admin`/`Phone` 两份 `Policy` | 无 | 120 LOC |
| `edge/admin` | 管理 UI 的 HTTP 面 | `Handler() http.Handler` | `core`、`config`、speech/源接口 | 300 LOC/文件 |
| `edge/phone` | 手机 PWA 的 HTTP 面（含 CSP 与资源） | `Handler() http.Handler` | `core`、`edge/netguard` | 300 LOC/文件 |
| `app/runtime` | 组合根：构造、启动、关闭、状态视图 | `PhoneSpeech`、`PhoneAdmin`（已落地）；`New(cfg) (*Runtime, error)`、`Start(ctx)`/`Stop(ctx)`（ARC-07） | 全部具体实现 | 400 LOC |

约定继续沿用现状里已经验证有效的做法：**接口由消费者声明**（`asr.Engine`、`llm.Processor`、`audio.Sink`、`input.Source`、`phone.Transcriber`、`api.Backend`），而不是在被实现方定义一个大接口。这条不改。

## 5. 不变量

代码注释里引用编号，测试名对齐编号；破坏任一条都算回归。

- **I1 麦克风声明 = 采集窗口开着**，不是转写窗口。声明在采集关闭时释放（BLE `EventAudioStop`、手机 `end`/超限、任一源断开），不等 ASR——转写已由 `speech` 自己的串行锁排队。被抢占的一方必须被通知停手（见 I8）。
- **I2 一次采集只属于一个 utterance 世代**。`start` 递增 `gen` 并新建缓冲；`finalize` 只能排掉自己那一代。`stop→start→stop` 必须产出两段互不污染的文本。重复的 `start`（丢到 `stop` 的情形）重置缓冲但不重置归属。
- **I3 阻塞调用一律带截止**。helper 请求、`pw-cat` 写、ASR、LLM、WS `Read`、HTTP server 都有超时或可取消 ctx；任何锁的持有时间里不做无界 IO。
- **I4 用户可见事件可丢但要能自愈**。SSE/WS 建立连接即发一份完整状态快照；缓冲满时丢最旧并计数上报，不静默。
- **I5 断开不丢音频**。BLE 掉线、手机断链、令牌被撤销时，先取走已缓冲样本交给 `speech`，再释放归属。
- **I6 边界默认安全**。非回环地址不允许 `tls: off`；root helper socket 只认 `SO_PEERCRED`（写配置/改设备/按键抓取这类动词要求 uid 0 或 agent uid，`inject` 才允许组内）；转写文本、音频、令牌、配对码不进日志。
- **I7 隐私最小化**。配对码只出现在 URL fragment；ticket/token 一次性且服务端只存哈希；LAN IP 与设备名可以入库但可关，字幕/历史默认不显示地址细节。
- **I8 只有一条转写链**。任何新输入源都调 `speech.Transcribe`，不得自带 ASR；源差异只体现在 `SourceRef` 的标注上。
- **I9 管理面只在回环**。配置装载与保存都强制 `127.0.0.1:8787`，请求期再校验 `Host` 与 `Origin`（空 `Origin` 不算同源，除非 `Sec-Fetch-Site: same-origin`）。
- **I10 单一生成物真相**。`biz/model`、`biz/router`、`docs/openapi.yaml` 必须等于 `make idl && make openapi` 的输出，CI 做 diff；手写路由要么进 IDL，要么显式标注为非 thrift 面并在 CI 白名单里说明。

## 6. 关键取舍：明确不做什么

- **不合并 Hertz/thrift 生成层**。`hz` 生成的 `biz/model`、`biz/router` 体量可观，手工合并会在下次 `make idl` 时被冲掉，收益为负。改用 I10 的 CI diff 把"IDL 是唯一真相"变成真话，而不是把它拆掉。
- **不引入 DI 框架**。组合根是显式构造函数，一眼能看完；`fx`/`wire` 只会把依赖图从代码搬到注解里。
- **不为 `map[string]any` 上 codegen**。手写三个 typed view（`StatusView`/`ReadyView`/`DevicesView`），一次替换掉状态路径上的四处重复断言，其余自由格式字段维持 map。
- **不把 `web/ui.html` 换成前端框架**。它今天能零构建、单二进制分发，这是真价值。演进方向是拆文件（CSS/JS/HTML 分离）、两 UI 共享设计 token 与事件处理，并把"PWA"这个说法兑现成真 manifest + service worker，或者把措辞降级。
- **不做多用户/多租户鉴权**。这是单人本地设备：边界靠回环、配对码与 `SO_PEERCRED`。加 OAuth/用户表是过度设计。
- **不把 OS 适配抽成插件 SDK**。`transport` 边界够用；等平台数量超过三个再谈抽象。
- **不改配置键名做产品更名**。`xiaomi_*` 前缀、socket 路径这类用户可见名字要改，但必须同波带上 unit、doctor 与迁移提示，不能只改代码。

## 7. 数据流

**手机语音（BLE 同构，只列差异）：**

```
按住说话  AudioWorklet(源采样率) → 主线程线性重采样 → 320 样本/20ms Int16
  → edge/phone WS(binary) → source/phone 累积（上限 core/audiofmt.Cap）
  → 松开：`end` → I5 取缓冲 → speech.Transcribe(ctx, Utterance{SourceRef{phone,addr}})
       → asr → text 规则/LLM 改写 → actuate：字幕 + JSONL 历史 + 粘贴焦点窗口
       → WS 回推手机（同一条转写与耗时）
       → app/runtime 的 Watch 通道 → edge/admin SSE（listening/转写事件）
```

**控制流（管理端）：**

```
浏览器(edge/admin, 回环 + Host/Origin) → api.Backend → app/runtime
  → 只读：core.StatusView 直接序列化（I9/R6，不再 map 断言）
  → 写：config 装载校验 → 落盘(fsutil, 0600) → 通知源热生效 → 返回新视图
```

## 8. 错误处理与可观测性口径

- 每个 goroutine 有唯一 owner，owner 负责在 ctx 取消时收摊；`async.GoSafe` 只用于 owner 已登记的任务。
- 结构化日志字段白名单：`chars`、`language`、`provider`、`source`、`latency`、`rate`、`code`、`device`、`path`、`retry_in`、`err`。黑名单：转写正文、音频字节、token、ticket、配对码、完整蓝牙地址、（默认）LAN 地址。三条落地规则：地址只以 `redact.MAC` 的脱敏形态出现（厂商段 + 末段）；提供商响应体一律经 `redact.ProviderSuffix` 收敛成 `type/code`，散文丢掉、状态码留下，因为被拒的请求常会引用它拒绝的那段话，而那段话就是转写；用户自己配置的 ASR 命令的 stderr 只写 debug，错误串里只留字节数。转写正文还有一道兜底：`agent.redactText` 在记录 LLM 失败前把正在处理的那句话从错误串里挖掉（SEC-02）。
- 指标统一前缀并补齐手机面：现在 6 个 `xiaomi_remote_*` 指标里手机相关为零；`readyz` 与 `doctor` 共享同一张检查表，避免"人说没就绪、机器说就绪"。
- 失败要分层可读：`readyz` 返回核心必需项与可选项两组（手机-only 用户不该因为没 root helper 就永久 503）。

## 9. 迁移策略

四波，每波结束必须过验证门（本机 `gofmt -l . && go vet -tags stdjson ./... && go test -tags stdjson -count=1 ./...`，CI 补 `-race` 与交叉编译）。波内按"每条改动都能单独提交、单独回滚"切分，不做大爆炸重写。

- **波 0（安全与正确性，不动结构）**：`SO_PEERCRED`、日志脱敏、TLS 强制、phone 配置无条件校验、ATVV 采样率与 `FrameSize` 边界、世代计数器、撤销即断链、并发 R1–R5、预鉴权 map 与转写队列有界。
- **波 1（骨架）**：`core` + `core/audiofmt` + `fsutil` 抽出；`internal/config` 变叶子（子系统校验回各子系统，由 runtime 调用）；`edge/netguard` 抽出并让两个边界共用；`internal/phone` 去掉 `web` import；typed view 端到端替换 `map[string]any`；`main.go` 的 `runAgent`/doctor 搬进 `app/runtime`；`agent.go` 三处退避合一 + `TransportFactory` 注入使 `connectLoop` 可测。
- **波 2（产品与运维）**：手机端可在管理 UI 开关并落盘 + README 手机章节；doctor 补齐真实依赖且可执行建议；`readyz` 分层；release 产物带 unit 与安装脚本；`version` 子命令与 CHANGELOG 版本标题；CI 加 `make idl` diff 与 `govulncheck`；手机指标；日志级别开关。
- **波 3（打磨）**：`ui.html` 拆分与设计 token 共享、真 manifest/SW、可访问性、`power` 等危险键的手机侧开关、evdev 长按重复、设备 profile 表真正生效、历史文件 `Sync` 与保留策略、依赖升级（`apache/thrift`）。

顺序理由：波 0 里多数项是"现在正在漏"的（隐私、丢音频、冻住、可被局域网白嫖），且不需要结构改动，风险最低；波 1 是骨架，必须在功能继续增长前立住，否则每个新功能都要抄一遍副本；波 2 决定外部用户能不能跑起来，但不影响正确性；波 3 是可维护性长尾。

兼容性约束：socket/二进制/unit 改名属于用户可见变更，必须与 unit、doctor、README、迁移提示同波走；配置键只增不改义；`v0.2.0` 标签必须等手机硬件验收通过后再打（`release.yml` 由 tag 触发并公开发布）。

## 10. 验收标准（怎么算"架构清楚了"）

全部可机械检查，不靠感觉：

1. `go list -deps` 断言 R1–R3 成立，且 `internal/config` 的 import 列表里不再出现任何子系统。
2. CI 有一条依赖规则检查（脚本 30 行以内），违规即红。
3. 非测试代码里 `map[string]any` 少于 20 处，且全部集中在真正的自由格式字段。
4. 没有任何 Go 文件超过 400 LOC，`main.go` 不再包含业务逻辑；`agent.go` 的退避只有一处。
5. 16 kHz/帧长/时长上限/最小可转写时长各自只有一个定义点（`grep` 结果唯一）。
6. 原子写与 JSONL 追加只有 `fsutil` 一份实现。
7. 新增一个输入源只需：实现 `input.Source`、调 `speech.Transcribe`、在 `app/runtime` 加一行接线——不碰 config、不碰 edge 的守卫、不碰日志与指标口径。
8. CI `-race` 全绿，且波 0 列的每条缺陷都有对应回归测试（测试名可追溯到不变量编号）。
9. 外部用户从 GitHub Release 下载后，按 README 一条路径能跑到"焦点窗口出字"，且 doctor 能指出缺的每一步。
