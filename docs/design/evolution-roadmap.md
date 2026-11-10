# 演进路线图（v0.2 → v0.3）

- 日期：2026-11-10
- 状态：执行中
- 依据：五轴只读审计（分层与依赖、并发与生命周期、安全与隐私、领域正确性、运维与产品），全部结论都带 `file:line` 证据
- 目标架构与不变量：见 [`architecture.md`](architecture.md)
- 编号规则：`SEC`=安全隐私、`DOM`=领域正确性、`CON`=并发生命周期、`ARC`=架构分层、`OPS`=运维产品；括号里是不变量/依赖规则编号，动工作时提交信息引用编号

进度勾选表示"代码已落地并通过本机 `gofmt`/`vet`/`test` 三件套"；标 **CI** 的还需要 `-race` 或交叉编译证据。

## 波 0 · 安全与正确性（不动结构，一条一提交）

### 正在漏的（Critical/High）

- [ ] **SEC-01** root helper socket 无对端凭据校验 → 组内任意进程可让 root 改写 `/etc` 映射、独占抓取键盘（`EVIOCGRAB`）、把原始按键回流给自己（等价键盘记录）。证据 `internal/remap/daemon.go:150-161,200-254,328-344,436-467`；全仓 `SO_PEERCRED` 零命中。动作：接受连接后取 `ucred`，`apply`/`set_devices`/`capture_start` 要求 uid 0 或 agent uid，仅 `inject` 允许组内；判定逻辑做成纯函数便于单测。**验证**：`TestHelperSocketPeerAuth`（表驱动，含 uid=-1、非授权 uid、agent uid、root）
- [x] **SEC-02** 转写正文进日志，且日志落在 git 工作区内。证据 `internal/agent/pipeline.go:77-81`（`text=<全文>`）、`deploy/xiaomi-remote-go.service:19-20` + Makefile logrotate 把 `agent.log` 存在 checkout 里；与 `SECURITY.md:29` 的承诺直接冲突。动作：只记长度/语言/提供商，地址脱敏；顺手审一遍其它 `log.*` 站点。**验证**：日志断言测试（正文不出现在 sink 中）。**已做**：`recognized` 改记 `chars/language/provider/source/latency`（`internal/agent/pipeline.go:99`）；蓝牙地址走新的 `internal/redact`（`MAC` 留厂商段 + 末段，`agent.go:370/409`）；日志文件从 checkout 搬到 `~/.local/state/xiaomi-remote/agent.log`（`@LOG_DIR@`/`@LOG_FILE@` 占位符 + Makefile 按服务用户的 home 解析，`install-logrotate` 走 sudo 也不会算成 `/root`；`ExecStartPre=/bin/mkdir -p`）。**同批审出来的另外两条正文外泄路径**：提供商 4xx 的响应体原来整段拼进 error（`internal/llm/openai.go:92`、`internal/asr/{mimo,openai}.go`），内容策略拒绝会引用被拒的那句话——改成只留状态码 + `error.type/code`（`redact.ProviderSuffix`）；`asr.command` 的 stderr 整段进 error，而 ASR 前端会把识别结果打到 stderr——改成 error 只留字节数，正文降级到 debug 级。断言测试在 `internal/agent/log_test.go`（自建 slog sink 捕获全进程日志，含"LLM 报错时正文仍不出现"这条兜底，`redactText`）。
- [ ] **SEC-03** `phone.tls: "off"` 被允许，非回环地址上配对码与长效令牌走明文（`internal/config/config.go:503-507`、`internal/phone/util.go:80-90`）。动作：装载与保存都拒绝"非回环 + 关闭 TLS"（I6）。
- [ ] **SEC-04** 自签叶子证书被签成 CA：`IsCA:true` + `KeyUsageCertSign`、825 天、无 pinning（`internal/phone/tls.go:119-122`），而文档让用户信任它（`docs/design/external-input-sources.md:268`）——信任之后这台 Pi 的 key 就成了通用 CA。动作：去掉 `IsCA`/`CertSign`，寿命缩短，文档同步。
- [x] **SEC-05** 撤销设备不断现有会话：`Store.Revoke` 只清令牌/ticket（`internal/phone/store.go:304-319`），而 `serveConn` 只在 ticket 兑换时鉴权一次（`internal/phone/server.go:279`）→ 被撤销的手机还能继续按键与听写。动作：撤销即丢弃该 device 的活动连接（与 DOM-04/I5 同批做）。**已做**：`Server.Disconnect(deviceID)` 摘注册表 + `hub.Unregister` + 在锁外 `CloseNow()`；`main.go` 的 `PhoneRevoke` 在 Revoke 成功后立刻调用并回 `disconnected` 字段。撤销是有意切断，所以那半句只丢弃、不进转写（不会把文字打到别人眼前）。
- [ ] **SEC-06** 管理端 origin 检查在 `Origin` 为空且无 `Sec-Fetch-Site` 时直接放行（`biz/handler/ui/server.go:126-143`），而 `asr.command.binary` 会原样执行（`internal/asr/command.go:81`）→ 摸到管理 API ≈ 本机 RCE。动作：收紧到"必须有同源证据"（I9），并把 `binary` 路径限定为白名单目录/要求可执行位与属主检查。
- [ ] **SEC-07** 预鉴权 map 无界增长：配对尝试计数的 `ip` 项从不清理、ticket 表从不清理（`internal/phone/store.go:86,246-258,276-286`），任何局域网 TCP 源都能灌。动作：过期清扫 + 条目上限。
- [ ] **SEC-08** 转写任务无并发上限：每句起一个 goroutine 并各持约 960 KB PCM（`internal/phone/server.go:396-436`、`server.go:81`），而 `speech` 用一把锁串行（`internal/agent/pipeline.go:56`）→ 内存增长 + 遥控器听写被饿死。动作：有界队列/在途上限，超出即回 `busy`。**范围追加**：队列要按采集顺序消费——DOM-04 把排空改成同步取、异步跑之后，两句同时在飞时到达 ASR 的先后取决于谁先抢到 `p.mu`，字幕与历史会偶发调换顺序。

### 会丢用户数据或冻住服务的（High/Medium）

- [x] **DOM-01** ATVV caps 报 8 kHz 时代码不认采样率（`internal/atvv/protocol.go:72-77`），但转写链永远贴 16 kHz（`internal/agent/pipeline.go:63`）→ WAV 头说谎（`internal/asr/wav.go:20-21`）、MiMo 的 16k 守卫永不触发（`mimo.go:90`）。动作：未选中 codec `0x02` 就拒绝起流并给出可读原因；`core/audiofmt` 成为唯一真相（R5）。**已做**：`Session` 记住协商速率（`SampleRate()`）、`agent.Utterance` 带上 `SampleRate` 并把 250 ms 地板改成按速率算（`Rate()/MinSamples()`），遥控器与手机两条路都传真实值；协商到非 16 kHz 时 `handleControl` 记一条 warn（不静默）。**剩余**：把四处 16 kHz 常量收进 `core/audiofmt`（R5，随 Wave 1 的 ARC 批次）。
- [x] **DOM-02** 异步 `finalize` 能吃掉下一句的开头：`EventAudioStop → async.GoSafe(finalize)`（`internal/agent/agent.go:462-465`）与 `EventAudioStart → resetDictation`（:458-461）共用一份 `a.dictation`（:499-514）且无世代号。松手立刻再按就丢字。动作：`gen` 计数器，finalize 只排自己那代（I2）。**验证**：`TestStartStopInterleavingYieldsTwoUtterances`。**已做（不引入世代号）**：采集结束改成同步排空——`closeCapture` 在分发协程里 `takeDictation()` 拿走缓冲区所有权，只有请求进后台；`resetDictation` 换成 `nil` 而非 `[:0]`，已交出的数组不会被下一次采集复用。回归测试见 `internal/agent/capture_test.go`（`TestInterleavedCapturesBecomeSeparateUtterances`、`TestDrainedSamplesSurviveTheNextCapture`）：把两处改回旧写法后两条都稳定失败（前者整句丢失，后者取到别人的样本），改回新写法 `-count=4` 全绿。
- [x] **DOM-03** 重复 `EventAudioStart`（丢了 stop 的真实情形，`protocol.go:268-270`）在 `handleControl` 里没有 case → 既不重置缓冲也不改归属，两句合成一句。动作：dup 时重置缓冲、保留归属（I2）。**已做**：`case atvv.EventAudioStartDup` 只 `closeCapture()`（排空上一句、不释放麦克风归属），新采集从空缓冲开始；`TestDuplicateStartClosesTheOpenCapture` 覆盖归属仍在 + 后续 stop 不再产出空句子。
- [x] **DOM-04** 任一源断开都会丢掉正在录的音频：`markDisconnected`（`internal/agent/agent.go:414-425`）与手机断链（`internal/phone/server.go:462-477`）只释放麦克风、不取缓冲。动作：先 finalize 再释放（I5）。**已做**：遥控器侧 `markDisconnected` 先 `closeCapture()` 再 `releaseMic()`，并只在 `a.ctx` 仍活着时排空（退出时不把字打给正在关的桌面）；手机侧 `unregister` 在锁内只做一次 `buffer.take()` 拷贝、锁外异步转写（R8），结果不再回写已死的 socket。**同批发现**：排空改成异步后，两句并发在飞的句子到达 ASR 的先后不再确定（`p.mu` 谁先抢到谁先走，`internal/agent/pipeline.go:56`）——桌面字幕与历史会偶发调换顺序，收进 SEC-08 的有界在途队列一起解；测试侧已按"集合"而非"顺序"断言（`capture_test.go` 的 `collect/pick`）。
- [x] **DOM-05** 设备可控的 `FrameSize` 未校验：`protocol.go:262` 取 16 位字段（最大 65535），`FrameAccumulator.Append`（:200-213）会为了凑齐一帧永远缓冲 → 静默丢全部音频。动作：上限 + 超限重同步。**已做**：`atvv.MaxFrameSize = 4096`——`ParseCaps` 把 0 或超限的设备值退回 `DefaultFrame`，`FrameAccumulator` 拒绝以不可用帧长启动，并在输入长度超过上限时丢弃待定字节重新同步（`Dropped()` 计数可观测）。
- [ ] **DOM-06** 仲裁只是"开门检查"不是串行：`AcquireAudioForce`（`internal/input/hub.go:123`）抢走 owner 后手机的 `speaking` 仍为真，继续缓冲并在 BLE 之后并发转写（`internal/phone/server.go:366,396-405`），且被抢占方收不到通知。动作：抢占要通知到客户端并停其采集（I1）。**验证**：双源并发端到端测试
- [x] **CON-01** `bluez` transport 的回调与关闭不安全：`Connect` 在 :126 就起了分发协程，`Subscribe` 到 :190-191 才裸写 `onAudio/onControl`（读点 :264/:267）；`Close` 用 `select` 而非已有的 `closeOnce` 关 `done`（:283-289）→ CI `-race` 命中面 + 双重 close panic 风险（`agent.Close` 对上 `connectLoop`）。动作：回调读写上锁、`done` 走 `closeOnce`。**同批发现并修复**：`Close` 与 `Connect` 并发时 D-Bus 句柄字段也无同步（改由 `publish` 单点安装，且关闭后拒绝安装）；`Subscribe`/`WriteTX` 在未连接时对 nil 接口发起调用（改为返回错误）。
- [ ] **CON-02** 管理端在 `s.mu` 里同步调 root helper，而 helper 客户端无读超时（`biz/handler/ui/server.go:238`、`internal/helper/client.go:98`）→ 卡住 `broadcast`（:197-207）并拖垮优雅关闭。动作：调用挪到锁外 + 加 deadline（I3/R8）。
- [ ] **CON-03** 转写链在整个 ASR 窗口持 `p.mu` 且用 `context.Background()`+180 s（`internal/agent/pipeline.go:56-64`；手机侧同样 `phone/server.go:414-415`）→ 手机句子排在 BLE 后面且 SIGINT 救不回。动作：调用方 ctx 传入、超时按音频长度给（I3）。
- [ ] **CON-04** `pw-cat` 写 stdin 无截止且持 `p.mu`（`internal/audio/pipewire_linux.go:214`）→ `handleAudio` 停摆、256 槽 D-Bus 通道填满、godbus 无界起 `deferredDeliver`。动作：带 deadline 的写 + 失败即弃。
- [ ] **CON-05** PipeWire watcher 泄漏：`watchOnce` 绑在 Start 的 ctx 上（:56），`RestartAudio` 只关 sink（`internal/agent/agent.go:524-532`），而 `check` 在已关闭时返回 true（:97-99）→ 每 15 s 永久 fork 一次 `pw-dump`。动作：重启时取消 watcher。
- [ ] **CON-06** 用户可见事件静默可丢且无快照：`transcripts` 环 16 非阻塞丢（`agent.go:180-186`）、SSE 客户端环 16 丢（`server.go:201-205`）、SSE 建连时不发状态快照（:159-195）→ 丢一条 `listening` 药丸就永久错位。动作：建连即快照（I4）。
- [ ] **CON-07** 手机 HTTP 只设了 `ReadHeaderTimeout`（`internal/phone/server.go:88`），WS `Read` 无 deadline（:281）→ slow-loris 与 4 个槽位被赖住；优雅关闭被吃掉 5 s（10 s 预算）。动作：读写/idle 超时 + 服务端 ping。
- [ ] **CON-08** `store.persistLocked` 在 `store.mu` 里落盘（`internal/phone/store.go:215,318,170-173`）→ 阻塞 `Authenticate`。动作：IO 挪出锁（R8）。

### 波 0 附带的小硬伤（同批清）

- [ ] **DOM-07** 手机按键通道同步调 helper（10 s 超时，`internal/phone/server.go:329`、`internal/helper/client.go:28`），卡住时同一连接的 PCM 也停。
- [ ] **DOM-08** `validatePhone` 只在 `Phone.Enabled` 时跑（`internal/config/config.go:488-491`），负数 `max_utterance_seconds` 能过校验；`NewServer` 只夹 `<=0`（`internal/phone/server.go:71-74`）→ `maxSamples` 变负、`room<=0`、每帧都"满"。动作：无条件校验 + 显式拒绝负数。
- [ ] **DOM-09** 手机句子丢身份：`SourceRef.Kind` 写了却没人读，`main.go:139` 忽略 `meta.Name`/`meta.DeviceID` → 字幕/历史只有 IP；两源上限口径还不同（`config.MaxUtterance` vs `Phone.MaxUtteranceSeconds`），BLE 超限静默截断而手机有 `too_long`（`agent.go:489-496` vs `phone/server.go`）。动作：统一标注与超限语义（I8）。
- [ ] **DOM-10** 字典替换把 `To` 当正则模板（`internal/text/text.go:93`），含 `$1` 的替换文本会展开成不存在的分组；规则还会链式二次改写。动作：字面替换 + 规则不叠加。
- [ ] **DOM-11** keymap 应用与落盘不原子：`biz/service/service.go:138-223` 先动 helper 再 `config.Save`，保存可能失败 → 运行中的守护进程对应一份存不出去的配置。
- [ ] **DOM-12** `internal/device` 的 `Profile.Keys`/`Voice` 是死字段（`internal/device/device.go:26-37`），按型号预置表从未参与优先级；长按重复被丢（`internal/remap/daemon.go:405-407` 跳过 `value==2`）。
- [ ] **DOM-13** `Store.Pair` 在 `MaxDevices` 拒绝时不消耗配对码（`internal/phone/store.go:199`），也不防 deviceID 撞车（:204）；`serveConn` 退出时不关 ws（`internal/phone/server.go:276-292`）；`asrTimeout` 固定 180 s 而三提供商链可能更久（`pipeline.go:122`）。
- [ ] **DOM-14** `Agent.SetHub` 裸写 `a.hub`（`internal/agent/agent.go:148`）对上周期的读；`history.rewrite` 改名缺 `Sync`（`internal/history/history.go:154-181`），掉电可清空历史，且 ID 用墙上时钟使 `Delete` 可能删多条。

## 波 1 · 架构骨架（§3 依赖规则与 §4 单元）

- [ ] **ARC-01** 抽 `core` + `core/audiofmt`：16k/帧长/上下限/最小可转写时长单点定义，`asr` 四处副本删除（R5）。
- [ ] **ARC-02** 抽 `fsutil`：五处原子写与 JSONL 追加合一（R7），带 `Sync`。
- [ ] **ARC-03** `internal/config` 变叶子：删掉对 `device`/`llm`/`asr`/`remap` 的 import，结构校验留在 config，语义校验交回各子系统、由 runtime 在构造时调用（R1）。
- [ ] **ARC-04** 抽 `edge/netguard`：一份 origin 检查、一份安全头、一份地址整形；`internal/phone` 删掉 `web` import 与 mime/CSP 段（`internal/phone/server.go:100-102,210-243`），管理端复用同一套（R4）。
- [ ] **ARC-05** typed view 端到端：`core.StatusView`/`ReadyView`/`DevicesView` 由 runtime 生成，替换 agent Status → service → ui → `api.StatusResp` 的四段重复断言；`map[string]any` 降到自由格式字段（R6）。
- [ ] **ARC-06** 消全局：`customUI`（`biz/router/router.go:12`、`main.go:368`、`biz/router/router_gen.go:14`）、`apihandler.SetBackend`；`phoneSpeech` 适配器与配对/撤销 payload 从 `main.go:330-363` 搬进 `app/runtime`，让它可在包内测。
- [ ] **ARC-07** 拆 `main.go`：`runAgent` 275 行（`main.go:179-453`）与 115 行 doctor 进 `app/runtime` + `app/doctor`，`main.go` 只做解析与分派（§10 第 4 条）。
- [ ] **ARC-08** `agent.go` 三处退避合一（`internal/agent/agent.go:343-356,357-370,385-392`）；注入 `TransportFactory`（现硬编 `bluez.New`/`atvv.NewSession`，:325/:336）使 `connectLoop` 可单测。
- [ ] **ARC-09** 拆 `internal/phone/server.go`（517 LOC / 7 个关注点）为 transport/handlers/audio/keys；拆 `biz/service/service.go`（863）按"配置/设备/映射/状态"边界。
- [ ] **ARC-10** 抽 `speech` 包（现 `internal/agent/pipeline.go`），BLE 与手机都只是调用方（I8）。
- [ ] **ARC-11** CI 依赖规则检查：`go list -deps` 断言 R1–R3，违规即红（§10 第 2 条）。
- [ ] **ARC-12** 手机按键注入白名单里的 `power` 类危险键收进显式开关（`internal/remap/remap.go:162-166`），默认关。

## 波 2 · 产品与运维（外部用户能否跑起来）

- [ ] **OPS-01** 手机功能从任何已文档化入口都打不开：`phone.*` 只在启动读一次（`internal/config/config.go:39-46`、`main.go:266`），UI 只显示 `config.phone.enabled = false`（`web/ui.html:2196`），没有子命令、README 无章节。动作：管理 UI 开关 + 落盘 + 提示重启，README 补"配对手机"整节（含自签证书警告步骤、两台设备同局域网要求）。
- [ ] **OPS-02** 没有下载即跑路径：`release.yml:47-48` 只带 binary/README/LICENSE/CHANGELOG/example 配置，unit 要 `@ROOT@` 替换（`Makefile:44-58`、`deploy/xiaomi-remote-go.service:11-17`），`make install-user` 不依赖 `build`。动作：tarball 带 unit 与安装脚本。
- [ ] **OPS-03** `doctor` 自己造配置并放过占位 MAC：README 让"先跑 doctor"（`README.md:95`），而 `config.Load` 会写默认值（`internal/config/config.go:246-250`）、`Validate` 只按正则放行 `AA:BB:CC:DD:EE:FF`（:400）。动作：示例地址视为未配置；doctor 不再写文件。
- [ ] **OPS-04** `doctor` 缺真实依赖检查：只看 `bluetoothctl`/`xsel` 是否存在（`main.go:564-566`），不查 BlueZ 守护进程/适配器状态、PipeWire、`wtype`、8787/8788 端口、evdev 读权限、LLM key、`phone.*`；措辞只诊断不建议动作。动作：与 `readyz` 共享检查表，每条给可执行命令。
- [ ] **OPS-05** 语音键静默失败：开 `/dev/input/eventN` 遇 EACCES 每 2 s 重试且不打日志（`internal/keys/f24_linux.go:77-83`），全仓没有 udev 规则或加 `input` 组这一步。动作：首次失败即记一次可操作日志 + doctor 覆盖。
- [ ] **OPS-06** `/readyz` 与 UI 的"就绪"要求 root helper + BLE（`biz/service/service.go:352`、`web/ui.html:2950-2953`）→ 只用手机的用户永久 503/"待检查"。动作：核心必需项与可选项分组。
- [ ] **OPS-07** IDL 真相已失真：4 条管理/手机路由手写注册（`biz/router/router_gen.go:27-30`），不在 `idl/xiaomi_remote.thrift` 与 `docs/openapi.yaml`，CI 不重新生成也不 diff；`Makefile:34` 硬编 `-version 0.1.0`（I10）。
- [ ] **OPS-08** 成功播报早于验证：`main.go:294` 在 `Serve` 真正 bind 前就打"phone input listener started"，端口被占只体现为稍后的 journal Error。
- [ ] **OPS-09** 版本号与叙事：`CHANGELOG.md` 的 Unreleased 没有 `## 0.2.0` 标题（release notes 落回 `--generate-notes`）；`docs/design/external-input-sources.md:4-5` 仍写"待评审/不含实现"而阶段 1/2 已完成，§10 列的五个手机指标并不存在；`README.md:3` 与路线图仍把手机列为未来；架构图没有手机路径。
- [ ] **OPS-10** 被静默忽略的配置：`api_key_file`/`asr_model`/`asr_language`/`asr_base_url` 仅在 `asr.providers.mimo` 为空时生效（`internal/config/config.go:368-397`）；未知 JSON 键直接丢（:260，无 `DisallowUnknownFields`）；约 58 个键里 13 个不在 `README.md:162-176`。
- [ ] **OPS-11** 产品更名没到用户可见名字：二进制/目录/socket/unit/指标仍是 `xiaomi-*`，`conf/config.example.json:5` 还留着已废弃的 `keyd_config`（`internal/remap/remap.go:1` 说明 keyd 已被取代），UI 里仍列 keyd/Python 服务（`web/ui.html:1144-1145`）。改名要与 unit/doctor/README/迁移提示同波。
- [ ] **OPS-12** 手机指标为零（现有 6 个 `xiaomi_remote_*`）；没有日志级别开关、没有卸载/重置命令、没有 `version` 子命令，用户很难把可用日志附到 issue 上。
- [ ] **OPS-13** 测试真实面：`biz/handler/api`、`biz/model/api`、`web/`、`runDoctor`/`runAgent`、`bluez/transport_linux.go`(312 LOC)、`remap/daemon.go` 事件循环、ATVV ADPCM（只有 2 字节零例，`protocol_test.go:34`）、X11 粘贴都无测试；`keys_test.go:5`/`paste_test.go:8`/`bluez/status_test.go:5` 只断言"没报错"。CI 绿在整个未测的硬件路径上。动作：补波 0/1 每条缺陷的回归测试 + `pipeline_test.go:56` 补 `Kind` 断言 + `mic_test.go`/`audio_test.go` 用信号替代时间轮询。
- [ ] **OPS-14** CI 缺 `govulncheck`/密钥扫描/lint，release 不以测试为门（`ci.yml`、`release.yml`）；`apache/thrift v0.13.0`（2019）是直接依赖。

## 波 3 · 打磨（可维护性长尾）

- [ ] `web/ui.html` 拆分（9-829 行 CSS、约 2250 行 JS、82 个函数）并与手机 UI 共享设计 token（现在两套冲突的 `:root`：`ui.html:10` 亮色 vs `phone.css:1` 暗色）；a11y 从 6 处 `aria/role` 起步，手机页为 0。
- [ ] 兑现"PWA"：manifest + service worker（`CHANGELOG.md:33` 已这么写），否则改口径。
- [ ] Wayland/非 GNOME 环境验证：unit 硬编 `DISPLAY=:0`/`XAUTHORITY`（`deploy/xiaomi-remote-go.service:14-15`）无 `WAYLAND_DISPLAY`。
- [ ] 历史文件保留/大小策略与脱敏开关；二维码配对、多手机、重连体验、横屏布局。
- [ ] 32 位 Pi / riscv 构建面与限制说明（当前只 linux amd64/arm64）。

## 未验证 / 需要真机

- 手机硬件验收（波 0 之后必须重跑）：配对的手机按住说话 → 焦点窗口出字 + 字幕 + 历史 + 转写回显；遥控器抢占正在录音的手机。
- 自签证书在手机浏览器上的例外流程与缓存行为（MitM 窗口）。
- `SO_PEERCRED` 的 uid 白名单在 systemd user unit 下是否仍然可用（需要 Pi）。
- IPv6 扩散下 `Store.ip` 增长与转写队列内存，是推理不是实测。
- 只按遥控器语音键（`agent.go:449`）在没有 root helper 时能否独立驱动听写——决定 OPS-06 的分组怎么切。
