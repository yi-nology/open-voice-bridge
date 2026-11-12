# 演进路线图（v0.2 → v0.3）

- 日期：2026-11-10；第二轮规划 2026-11-11
- 状态：执行中
- 依据：五轴只读审计（分层与依赖、并发与生命周期、安全与隐私、领域正确性、运维与产品），全部结论都带 `file:line` 证据
- 目标架构与不变量：见 [`architecture.md`](architecture.md)
- 编号规则：`SEC`=安全隐私、`DOM`=领域正确性、`CON`=并发生命周期、`ARC`=架构分层、`OPS`=运维产品；括号里是不变量/依赖规则编号，动工作时提交信息引用编号

进度勾选表示"代码已落地并通过本机 `gofmt`/`vet`/`test` 三件套"；标 **CI** 的还需要 `-race` 或交叉编译证据。

## 执行交接（第二轮：骨架与发布就绪，从这里接）

波 0 已收官（2026-11-11）：SEC-01–08、CON-01–08、DOM-01–14 全部落地（两条残余亦于 2026-11-12 随 R1 收口），并带出 SayAll 对标功能增量十余项（使用统计、常用语、组合动作、按 App 键位、本地 Agent 访问（MCP）、热力图、长会话提醒、按键计数、能力矩阵、配置导出导入、诊断摘要）与 macOS 平台兼容。逐项证据见本页「波 0」勾选项与 `CHANGELOG.md` Unreleased 各段。

工作方式已固定，不需要重新商量：**一条缺陷一个提交，TDD 先写失败测试**（失败输出留作证据，实现后同批转绿），每步跑三件套，一个批次做完再推远端。

### 债务盘点（2026-11-11 复测，比 [`architecture.md`](architecture.md) 写作时更糟——功能增量在持续加副本）

- `map[string]any` 非测试代码 **213 处**（architecture.md 记 121；分布前列：`biz/service/service.go` 33、`internal/mcp/server.go` 31、`biz/handler/ui/server.go` 30、`main.go` 20）。
- tmp+rename 原子写 **8 份副本**（architecture.md 记 5；**R2 已收拢进 `fsutil`**）：`internal/remap/{remap,keycounts}.go`、`internal/config/config.go`、`internal/mcp/store.go`、`internal/history/{stats,history}.go`、`internal/phone/store.go`、`biz/service/service.go`。
- 采样率字面量/双常量 **5 处**（**R2 已收拢进 `internal/core/audiofmt.go`**）：`internal/audio/audio.go:34`、`internal/asr/wav.go:10`、`internal/phone/audio.go:10`、`internal/atvv/protocol.go:16,89`；`asr` 其余处在 `audio.SampleRate` 与 `atvv.SampleRate` 两个并存常量间取值。
- 超规模文件 7 个：`web/ui.html` 4021、`biz/service/service.go` 858、`internal/phone/server.go` 734、`internal/remap/daemon.go` 710、`internal/agent/agent.go` 701、`biz/handler/ui/backend.go` 693、`main.go` 685。

结论：**波 1 骨架必须在下一个功能高峰前立住**。下面批次顺序的硬约束是：从 R2 起，新代码不得再自带 tmp+rename、采样率字面量或 `map[string]any` 状态字段；R9 把它变成 CI 门禁。

### 波 0 残余（已收官 2026-11-12，两个小提交）

1. **SEC-06 残余**（已做 2026-11-12）：`asr.command` 的 `binary` 守卫不挡解释器——`binary=/bin/sh args=["-c",…]` 仍可过（SEC-06 批只加了属主/可执行位/防篡改检查）。**已定做法**：在既有守卫上加解释器黑名单（按 basename 拒绝 `sh`/`bash`/`dash`/`zsh`/`env`/`busybox`/`python*`/`perl`/`ruby`/`node`），报错点名「ASR 命令必须是识别器本体，不能是解释器」。不做 args 白名单——`exec.Command` 不经 shell，args 里的元字符无害，攻击面全在「binary 是解释器」这一层。测试：表驱动 `TestCommandBinaryRejectsInterpreters`（含 `env` 垫底用例）。
2. **DOM-12 残余**（已做 2026-11-12）：`Profile.Keys`/`Profile.Voice` 死字段（`internal/device/device.go:35-36`，全仓零读点）。**已定做法**：两个都删。`Voice` 的音频真相随 ARC-01 进 `core/audiofmt`；`Keys` 没有消费方，键位走的是 keymap profile 而非设备 profile，键位图鉴另有渲染。旧 JSON 里的这两个键由装载层静默丢弃（现状本就不 `DisallowUnknownFields`），兼容无损。将来若要「按设备出键名表」，按 typed 字段重新设计，不复活 map。

### 批次顺序（每批 = 一串单缺陷提交，做完推远端再进下一批）

编号 R* 是执行批次；括号里是本文/架构稿的条目号，提交信息继续引用原编号。为避免撞名：**执行批次一律写作 R1–R10（本节），波次清单与缺陷条目里的（R5）/（R7）等是 architecture.md 的依赖规则编号**。批次在 ARC 与 OPS 之间交错，与 architecture.md §9 的纯波次顺序有一处有意偏离：**R3/R6 两个产品面批次插在骨架批次之间**——手机功能目前从任何文档化入口都打不开（OPS-01），把用户可达性排在全部重构之后代价太高；骨架的保护靠「R2 起新代码不加副本」的纪律与 R9 的 CI 门禁，不靠功能冻结。

- **R1 · 波 0 收官**（已完成 2026-11-12）：上文两条残余 + 本页勾选同步（2 个代码提交 + 1 个文档提交）。下一批 R2。
- **R2 · 叶子抽取（ARC-01、ARC-02）**（已完成 2026-11-12）：立 `core` 与 `core/audiofmt`（采样率/帧长/时长上下限/最小可转写时长单点定义，5 处字面量与 `audio`/`atvv` 双常量收拢，architecture.md R5）；`fsutil` 收拢 8 份原子写与 JSONL 追加（带 `Sync`，architecture.md R7）。纯搬运、零行为变化，三件套全绿即过。**落地口径**：`core/audiofmt.go` 是 `internal/core` 包内的文件（R5 的文件名），这样 R1「`config` 只 import `core`」成立；帧长即原 `atvv.DefaultFrame`（ADPCM 线字节，改名 `core.FrameBytes`）。下一批 R3。
- **R3 · 手机可达（OPS-01、OPS-06）**（已完成 2026-11-12）：管理 UI 开关 `phone.enabled`（落盘 + `restart_required` 口径，对齐配置导出导入的既有语义）；`/readyz` 与 UI「就绪」拆核心必需/可选项两组——口径以「无 root helper 时纯语音键能否听写」为准（见「未验证」第 5 条），真机确认前先按「核心 = agent+ASR+粘贴链，可选 = root helper/BLE」切，手机-only 用户不再永久 503；README 补「配对手机」整节（同局域网要求、自签证书「只接受一次例外」警告、排错链到能力矩阵）。下一批 R4。
- **R4 · 边界守卫合一（ARC-04、ARC-12）**：抽 `edge/netguard`（一份 origin 检查、一份安全头、一份地址整形），`internal/phone` 删 `web` import 与 mime/CSP 段，管理端换用同一套（architecture.md R4）；手机按键白名单里的 `power` 类危险键收进显式配置开关，默认关。
- **R5 · 状态视图单点（ARC-05）**：`core.StatusView`/`ReadyView`/`DevicesView` 由 runtime 生成、edge 只序列化，替换 agent Status → service → ui → `api.StatusResp` 四段重复断言（architecture.md R6）；`map[string]any` 收敛到真正的自由格式字段（目标 <20 处，集中在 keymap 自定义动作与 LLM 预设参数）。碰面最广的一批，放在 R2 的 `core` 类型就位之后。
- **R6 · 开箱路径（OPS-02、OPS-03、OPS-04、OPS-05、OPS-08、OPS-12 版本部分）**：release tarball 带 unit 与安装脚本，`make install-user` 依赖 `build`；doctor 不再写配置、示例 MAC 视为未配置、补真实依赖检查（与 `readyz` 共享检查表）且每条给可执行命令；语音键 EACCES 首次失败即记一次可操作日志（含 udev/`input` 组指引）；`Serve` 真正 bind 成功后再播「已启动」；`version` 子命令。
- **R7 · 组合根与转写链（ARC-06、ARC-07、ARC-10）**：消 `customUI`/`apihandler.SetBackend`/`phoneSpeech` 全局，配对/撤销 payload 挪进 `app/runtime` 可包内测；`main.go` 只剩解析与分派（`runAgent` 275 行与 doctor 进 `app/runtime`+`app/doctor`）；抽 `speech` 包（现 `internal/agent/pipeline.go`），BLE 与手机都只是调用方（I8）。此后新增输入源只需架构稿 §10 第 7 条的三步。
- **R8 · 子系统内部（ARC-03、ARC-08、ARC-09）**：`internal/config` 变叶子（删对 `device`/`llm`/`asr`/`remap` 的 import，语义校验交回各子系统、runtime 构造时调用，architecture.md R1）；`agent.go` 三处退避合一 + `TransportFactory` 注入使 `connectLoop` 可单测；拆 `internal/phone/server.go`（按 transport/handlers/audio/keys）与 `biz/service/service.go`（按配置/设备/映射/状态）。排在 R7 之后：runtime 成为组合根后，校验搬迁才有落点。
- **R9 · 机械门禁（ARC-11 + OPS-07、OPS-09、OPS-10、OPS-14）**：CI `go list -deps` 断言 architecture.md R1–R3 违规即红，附 map 计数（<20）与「16k/原子写/JSONL 唯一定义点」grep 门禁；`make idl`/`make openapi` 产物 diff（手写路由进 IDL 或进显式白名单，I10）；CHANGELOG 补 `## 0.2.0` 标题，README/架构图/`external-input-sources.md` 的手机叙事与阶段状态对齐（OPS-09）；配置键文档对齐全量（约 13 个未记载键），未知 JSON 键从静默丢弃改为告警（OPS-10）；`govulncheck`/密钥扫描/lint 进 CI，release 以测试为门；`apache/thrift` 升到受支持版本。
- **R10 · 可观测性（OPS-12 其余）**：手机面指标补齐（现有 6 个 `xiaomi_remote_*` 里手机相关为零）；日志级别开关；产品项「加密诊断日志」并入——诊断包本地加密（导出口令），解密才见明文，内容继续走既有脱敏口径。

### 发布段（v0.2.0，硬件验收通过前不打 tag）

进发布段门槛：R3（手机可达）与 R6（开箱路径）完成，R9 的文档叙事在场（CHANGELOG 标题、README 手机章节）。然后：

1. 手机硬件验收重跑：配对 → 按住说话 → 焦点出字 + 字幕 + 历史 + 转写回显；遥控器抢占正在录音的手机；断开/截断的半句不丢。
2. 本页「未验证 / 需要真机」五条逐条销账。
3. CHANGELOG 写出 `## 0.2.0` 段落（release 工作流已能按标题提取 notes），此时才打 tag。

### 发布之后（v0.3 线）

- **OPS-11 产品更名**（`xiaomi-*` → 用户可见新名）：用户可见变更，必须与 unit/doctor/README/迁移提示同波，单独立项、不进发布关键路径。
- **产品余量（SayAll 对标收尾）**：常用语浮层面板（桌面 overlay 选句）；组合动作的「打开 App/文本」步骤（经合成键通道委托 agent，等 R7 的 `speech`/`actuate` 边界稳了再做）；回眸可选录音（隐私敏感，先出设计稿再动手）；Onboarding 首次运行引导（等 R6 的 doctor 能给出可执行建议后兑现）；稳定/预发布双通道（release 流水线工作，挂 R9 之后）。
- **波 3 打磨**照旧：`ui.html` 拆分与设计 token 统一（亮色 `ui.html:10` vs 暗色 `phone.css:1` 两套 `:root`）、真 manifest/SW 或把「PWA」口径降级、a11y、Wayland/非 GNOME、历史保留与脱敏开关、32 位 Pi/riscv 构建面。
- **macOS** 是兼容不是移植：长线只保证真机验收清单持续通过、粘贴辅助功能提示进 doctor；不承诺 macOS 与 Linux 功能对齐。

硬门禁（不要为了赶进度绕过）：

- 每个 Go 命令前先 `export PATH=$HOME/go-toolchain/go/bin:$PATH`；`build`/`vet`/`test` 一律带 `-tags stdjson`。
- 三件套：`gofmt -l . && go vet -tags stdjson ./... && go test -tags stdjson -count=1 ./...`。输出重定向到临时文件再看退出码，不要把 `go test` 管进 `grep`（会吞掉真实 `$?`）。
- `-race` 在 Pi 上跑不起来：竞争类结论只以 CI 的 `Test (linux)` 作业为证据，勾选时要标 **CI**。macOS 开发机可用 Docker（`golang:1.27`）跑 Linux 侧测试作近似证据。
- 管理端永远只绑 `127.0.0.1:8787`；配对码只进 URL fragment（`#pair=`）；token / ticket / 配对码 / 音频字节 / 转写正文永不进日志与提交物；提交的文档与测试里不写真实 MAC 与机器路径（示例一律 `AA:BB:CC:DD:EE:FF`）。
- 推送用显式 SSH URL（`origin` 是 HTTPS，凭据不在那里）；`git remote set-url` 这条路被拒过，别再试。
- **不打 `v0.2.0` tag**：`release.yml` 由 tag 触发并公开发布，硬件验收通过之前不切版本。
- `docs/superpowers/plans/*.md` 是本地工作稿（含机器绝对路径），已进 `.gitignore`，不要提交。

## 波 0 · 安全与正确性（不动结构，一条一提交）

### 正在漏的（Critical/High）

- [x] **SEC-01** root helper socket 无对端凭据校验 → 组内任意进程可让 root 改写 `/etc` 映射、独占抓取键盘（`EVIOCGRAB`）、把原始按键回流给自己（等价键盘记录）。证据 `internal/remap/daemon.go:150-161,200-254,328-344,436-467`；全仓 `SO_PEERCRED` 零命中。动作：接受连接后取 `ucred`，`apply`/`set_devices`/`capture_start` 要求 uid 0 或 agent uid，仅 `inject` 允许组内；判定逻辑做成纯函数便于单测。**验证**：`TestHelperSocketPeerAuth`（表驱动，含 uid=-1、非授权 uid、agent uid、root）
- [x] **SEC-02** 转写正文进日志，且日志落在 git 工作区内。证据 `internal/agent/pipeline.go:77-81`（`text=<全文>`）、`deploy/xiaomi-remote-go.service:19-20` + Makefile logrotate 把 `agent.log` 存在 checkout 里；与 `SECURITY.md:29` 的承诺直接冲突。动作：只记长度/语言/提供商，地址脱敏；顺手审一遍其它 `log.*` 站点。**验证**：日志断言测试（正文不出现在 sink 中）。**已做**：`recognized` 改记 `chars/language/provider/source/latency`（`internal/agent/pipeline.go:99`）；蓝牙地址走新的 `internal/redact`（`MAC` 留厂商段 + 末段，`agent.go:370/409`）；日志文件从 checkout 搬到 `~/.local/state/xiaomi-remote/agent.log`（`@LOG_DIR@`/`@LOG_FILE@` 占位符 + Makefile 按服务用户的 home 解析，`install-logrotate` 走 sudo 也不会算成 `/root`；`ExecStartPre=/bin/mkdir -p`）。**同批审出来的另外两条正文外泄路径**：提供商 4xx 的响应体原来整段拼进 error（`internal/llm/openai.go:92`、`internal/asr/{mimo,openai}.go`），内容策略拒绝会引用被拒的那句话——改成只留状态码 + `error.type/code`（`redact.ProviderSuffix`）；`asr.command` 的 stderr 整段进 error，而 ASR 前端会把识别结果打到 stderr——改成 error 只留字节数，正文降级到 debug 级。断言测试在 `internal/agent/log_test.go`（自建 slog sink 捕获全进程日志，含"LLM 报错时正文仍不出现"这条兜底，`redactText`）。
- [x] **SEC-03** `phone.tls: "off"` 被允许，非回环地址上配对码与长效令牌走明文（`internal/config/config.go:503-507`、`internal/phone/util.go:80-90`）。动作：装载与保存都拒绝"非回环 + 关闭 TLS"（I6）。**已定做法**：在 `validatePhone` 里判断 `Phone.Addr` 的主机部分，`tls: off` 时只放行 `127.0.0.1`/`localhost`/`::1`（以及回环段 `127.0.0.0/8`），其余（`0.0.0.0`、`[::]`、局域网 IP、主机名）一律报错，错误文案要点名"要么开 `tls: auto`，要么把地址收到回环"；测试写 `TestPhonePlainTLSNeedsLoopback`（表驱动，拒绝列 + 放行列各一组）与 `TestPhoneValidatedEvenWhenDisabled`。`PairingBase` 的 `off → http` 映射保留不动，`internal/phone/util_test.go:13` 已把它钉住——校验层负责堵死非回环明文，不需要改 URL 语义。
- [x] **SEC-04** 自签叶子证书被签成 CA：`IsCA:true` + `KeyUsageCertSign`、825 天、无 pinning（`internal/phone/tls.go:119-122`），而文档让用户信任它（`docs/design/external-input-sources.md:268`）——信任之后这台 Pi 的 key 就成了通用 CA。动作：去掉 `IsCA`/`CertSign`，寿命缩短，文档同步。**已定做法**：`KeyUsage` 从 `DigitalSignature|CertSign` 收成只留 `DigitalSignature`，`IsCA` 置假（`BasicConstraintsValid` 保持真，让扩展以 CA=FALSE 正常编码），`certValidFor` 从 825 天降到一年量级，`stillValid` 增加"剩余寿命不足阈值即重建"的续期余量（现在只按已过期判断，长跑进程会卡在临期证书上）；`internal/phone/tls_test.go:38-40` 现在断言 `IsCA` 为真，要反过来断言"叶子不是 CA、`KeyUsage == DigitalSignature`"，并把临期重建加进 `TestCertificateManagerRebuildsExpired`。文档同步两处口径：`docs/design/external-input-sources.md:32`（"手机首次访问手动信任"）与 `:268`（"iOS 额外提示安装证书"）要改成"接受一次例外即可，不要把这张证书导入为长期信任的根"；`SECURITY.md` 里的 MitM 窗口说明跟着更新。
- [x] **SEC-05** 撤销设备不断现有会话：`Store.Revoke` 只清令牌/ticket（`internal/phone/store.go:304-319`），而 `serveConn` 只在 ticket 兑换时鉴权一次（`internal/phone/server.go:279`）→ 被撤销的手机还能继续按键与听写。动作：撤销即丢弃该 device 的活动连接（与 DOM-04/I5 同批做）。**已做**：`Server.Disconnect(deviceID)` 摘注册表 + `hub.Unregister` + 在锁外 `CloseNow()`；`main.go` 的 `PhoneRevoke` 在 Revoke 成功后立刻调用并回 `disconnected` 字段。撤销是有意切断，所以那半句只丢弃、不进转写（不会把文字打到别人眼前）。
- [x] **SEC-06** 管理端 origin 检查在 `Origin` 为空且无 `Sec-Fetch-Site` 时直接放行（`biz/handler/ui/server.go:126-143`），而 `asr.command.binary` 会原样执行（`internal/asr/command.go:81`）→ 摸到管理 API ≈ 本机 RCE。动作：收紧到"必须有同源证据"（I9），并把 `binary` 路径限定为白名单目录/要求可执行位与属主检查。**残余已补（R1，2026-11-12）**：解释器黑名单落地（basename `sh`/`bash`/`dash`/`zsh`/`env`/`busybox`/`python*`/`perl`/`ruby`/`node`，PATH 解析前与 symlink 解析后各判一次；`TestCommandBinaryRejectsInterpreters` 表驱动 + 符号链接用例，失败输出先留证后同批转绿）。
- [x] **SEC-07** 预鉴权 map 无界增长：配对尝试计数的 `ip` 项从不清理、ticket 表从不清理（`internal/phone/store.go:86,246-258,276-286`），任何局域网 TCP 源都能灌。动作：过期清扫 + 条目上限。
- [x] **SEC-08** 转写任务无并发上限：每句起一个 goroutine 并各持约 960 KB PCM（`internal/phone/server.go:396-436`、`server.go:81`），而 `speech` 用一把锁串行（`internal/agent/pipeline.go:56`）→ 内存增长 + 遥控器听写被饿死。动作：有界队列/在途上限，超出即回 `busy`。**范围追加**：队列要按采集顺序消费——DOM-04 把排空改成同步取、异步跑之后，两句同时在飞时到达 ASR 的先后取决于谁先抢到 `p.mu`，字幕与历史会偶发调换顺序。

### 会丢用户数据或冻住服务的（High/Medium）

- [x] **DOM-01** ATVV caps 报 8 kHz 时代码不认采样率（`internal/atvv/protocol.go:72-77`），但转写链永远贴 16 kHz（`internal/agent/pipeline.go:63`）→ WAV 头说谎（`internal/asr/wav.go:20-21`）、MiMo 的 16k 守卫永不触发（`mimo.go:90`）。动作：未选中 codec `0x02` 就拒绝起流并给出可读原因；`core/audiofmt` 成为唯一真相（R5）。**已做**：`Session` 记住协商速率（`SampleRate()`）、`agent.Utterance` 带上 `SampleRate` 并把 250 ms 地板改成按速率算（`Rate()/MinSamples()`），遥控器与手机两条路都传真实值；协商到非 16 kHz 时 `handleControl` 记一条 warn（不静默）。**剩余**：把四处 16 kHz 常量收进 `core/audiofmt`（R5，随 Wave 1 的 ARC 批次）。
- [x] **DOM-02** 异步 `finalize` 能吃掉下一句的开头：`EventAudioStop → async.GoSafe(finalize)`（`internal/agent/agent.go:462-465`）与 `EventAudioStart → resetDictation`（:458-461）共用一份 `a.dictation`（:499-514）且无世代号。松手立刻再按就丢字。动作：`gen` 计数器，finalize 只排自己那代（I2）。**验证**：`TestStartStopInterleavingYieldsTwoUtterances`。**已做（不引入世代号）**：采集结束改成同步排空——`closeCapture` 在分发协程里 `takeDictation()` 拿走缓冲区所有权，只有请求进后台；`resetDictation` 换成 `nil` 而非 `[:0]`，已交出的数组不会被下一次采集复用。回归测试见 `internal/agent/capture_test.go`（`TestInterleavedCapturesBecomeSeparateUtterances`、`TestDrainedSamplesSurviveTheNextCapture`）：把两处改回旧写法后两条都稳定失败（前者整句丢失，后者取到别人的样本），改回新写法 `-count=4` 全绿。
- [x] **DOM-03** 重复 `EventAudioStart`（丢了 stop 的真实情形，`protocol.go:268-270`）在 `handleControl` 里没有 case → 既不重置缓冲也不改归属，两句合成一句。动作：dup 时重置缓冲、保留归属（I2）。**已做**：`case atvv.EventAudioStartDup` 只 `closeCapture()`（排空上一句、不释放麦克风归属），新采集从空缓冲开始；`TestDuplicateStartClosesTheOpenCapture` 覆盖归属仍在 + 后续 stop 不再产出空句子。
- [x] **DOM-04** 任一源断开都会丢掉正在录的音频：`markDisconnected`（`internal/agent/agent.go:414-425`）与手机断链（`internal/phone/server.go:462-477`）只释放麦克风、不取缓冲。动作：先 finalize 再释放（I5）。**已做**：遥控器侧 `markDisconnected` 先 `closeCapture()` 再 `releaseMic()`，并只在 `a.ctx` 仍活着时排空（退出时不把字打给正在关的桌面）；手机侧 `unregister` 在锁内只做一次 `buffer.take()` 拷贝、锁外异步转写（R8），结果不再回写已死的 socket。**同批发现**：排空改成异步后，两句并发在飞的句子到达 ASR 的先后不再确定（`p.mu` 谁先抢到谁先走，`internal/agent/pipeline.go:56`）——桌面字幕与历史会偶发调换顺序，收进 SEC-08 的有界在途队列一起解；测试侧已按"集合"而非"顺序"断言（`capture_test.go` 的 `collect/pick`）。
- [x] **DOM-05** 设备可控的 `FrameSize` 未校验：`protocol.go:262` 取 16 位字段（最大 65535），`FrameAccumulator.Append`（:200-213）会为了凑齐一帧永远缓冲 → 静默丢全部音频。动作：上限 + 超限重同步。**已做**：`atvv.MaxFrameSize = 4096`——`ParseCaps` 把 0 或超限的设备值退回 `DefaultFrame`，`FrameAccumulator` 拒绝以不可用帧长启动，并在输入长度超过上限时丢弃待定字节重新同步（`Dropped()` 计数可观测）。
- [x] **DOM-06** 仲裁只是"开门检查"不是串行：`AcquireAudioForce`（`internal/input/hub.go:123`）抢走 owner 后手机的 `speaking` 仍为真，继续缓冲并在 BLE 之后并发转写（`internal/phone/server.go:366,396-405`），且被抢占方收不到通知。动作：抢占要通知到客户端并停其采集（I1）。**验证**：双源并发端到端测试
- [x] **CON-01** `bluez` transport 的回调与关闭不安全：`Connect` 在 :126 就起了分发协程，`Subscribe` 到 :190-191 才裸写 `onAudio/onControl`（读点 :264/:267）；`Close` 用 `select` 而非已有的 `closeOnce` 关 `done`（:283-289）→ CI `-race` 命中面 + 双重 close panic 风险（`agent.Close` 对上 `connectLoop`）。动作：回调读写上锁、`done` 走 `closeOnce`。**同批发现并修复**：`Close` 与 `Connect` 并发时 D-Bus 句柄字段也无同步（改由 `publish` 单点安装，且关闭后拒绝安装）；`Subscribe`/`WriteTX` 在未连接时对 nil 接口发起调用（改为返回错误）。
- [x] **CON-02** 管理端在 `s.mu` 里同步调 root helper，而 helper 客户端无读超时（`biz/handler/ui/server.go:238`、`internal/helper/client.go:98`）→ 卡住 `broadcast`（:197-207）并拖垮优雅关闭。动作：调用挪到锁外 + 加 deadline（I3/R8）。
- [x] **CON-03** 转写链在整个 ASR 窗口持 `p.mu` 且用 `context.Background()`+180 s（`internal/agent/pipeline.go:56-64`；手机侧同样 `phone/server.go:414-415`）→ 手机句子排在 BLE 后面且 SIGINT 救不回。动作：调用方 ctx 传入、超时按音频长度给（I3）。
- [x] **CON-04** `pw-cat` 写 stdin 无截止且持 `p.mu`（`internal/audio/pipewire_linux.go:214`）→ `handleAudio` 停摆、256 槽 D-Bus 通道填满、godbus 无界起 `deferredDeliver`。动作：带 deadline 的写 + 失败即弃。
- [x] **CON-05** PipeWire watcher 泄漏：`watchOnce` 绑在 Start 的 ctx 上（:56），`RestartAudio` 只关 sink（`internal/agent/agent.go:524-532`），而 `check` 在已关闭时返回 true（:97-99）→ 每 15 s 永久 fork 一次 `pw-dump`。动作：重启时取消 watcher。
- [x] **CON-06** 用户可见事件静默可丢且无快照：`transcripts` 环 16 非阻塞丢（`agent.go:180-186`）、SSE 客户端环 16 丢（`server.go:201-205`）、SSE 建连时不发状态快照（:159-195）→ 丢一条 `listening` 药丸就永久错位。动作：建连即快照（I4）。
- [x] **CON-07** 手机 HTTP 只设了 `ReadHeaderTimeout`（`internal/phone/server.go:88`），WS `Read` 无 deadline（:281）→ slow-loris 与 4 个槽位被赖住；优雅关闭被吃掉 5 s（10 s 预算）。动作：读写/idle 超时 + 服务端 ping。
- [x] **CON-08** `store.persistLocked` 在 `store.mu` 里落盘（`internal/phone/store.go:215,318,170-173`）→ 阻塞 `Authenticate`。动作：IO 挪出锁（R8）。

### 波 0 附带的小硬伤（同批清）

- [x] **DOM-07** 手机按键通道同步调 helper（10 s 超时，`internal/phone/server.go:329`、`internal/helper/client.go:28`），卡住时同一连接的 PCM 也停。
- [x] **DOM-08** `validatePhone` 只在 `Phone.Enabled` 时跑（`internal/config/config.go:488-491`），手机段关掉时任何非法值都能过装载与保存；`normalizePhone` 只填**零值**（`config.go:319-335`），所以负数与超限值会原样落盘。**订正**：原描述里"`maxSamples` 变负、每帧都满"经复核不成立——`NewServer` 已把 `<=0` 夹成 30（`internal/phone/server.go:70-74`），真正的危害是坏配置被静默持久化，等到用户打开手机输入才在启动时炸出错误，而那时他刚接受完"配好了"的引导。动作：无条件校验 + 显式拒绝负数（与 SEC-03 同一条 `validatePhone` 改，测试见 `TestPhoneValidatedEvenWhenDisabled`）。
- [x] **DOM-09** 手机句子丢身份：`SourceRef.Kind` 写了却没人读，`main.go:139` 忽略 `meta.Name`/`meta.DeviceID` → 字幕/历史只有 IP；两源上限口径还不同（`config.MaxUtterance` vs `Phone.MaxUtteranceSeconds`），BLE 超限静默截断而手机有 `too_long`（`agent.go:489-496` vs `phone/server.go`）。动作：统一标注与超限语义（I8）。
- [x] **DOM-10** 字典替换把 `To` 当正则模板（`internal/text/text.go:93`），含 `$1` 的替换文本会展开成不存在的分组；规则还会链式二次改写。动作：字面替换 + 规则不叠加。
- [x] **DOM-11** keymap 应用与落盘不原子：`biz/service/service.go:138-223` 先动 helper 再 `config.Save`，保存可能失败 → 运行中的守护进程对应一份存不出去的配置。
- [x] **DOM-12** 长按重复已修（`value==2` 触发重按 / 宏重执行，`daemon_inject_test.go` 覆盖）；`Profile.Keys`/`Voice` 死字段已删（R1，2026-11-12）——`VoiceSpec` 类型同删，预置 JSON 的死 `voice`/`keys` 一并清除；旧 JSON 装载时静默丢弃这两键，兼容无损。
- [x] **DOM-13** `Store.Pair` 在 `MaxDevices` 拒绝时不消耗配对码（`internal/phone/store.go:199`），也不防 deviceID 撞车（:204）；`serveConn` 退出时不关 ws（`internal/phone/server.go:276-292`）；`asrTimeout` 固定 180 s 而三提供商链可能更久（`pipeline.go:122`）。
- [x] **DOM-14** `Agent.SetHub` 裸写 `a.hub`（`internal/agent/agent.go:148`）对上周期的读；`history.rewrite` 改名缺 `Sync`（`internal/history/history.go:154-181`），掉电可清空历史，且 ID 用墙上时钟使 `Delete` 可能删多条。

## 波 1 · 架构骨架（§3 依赖规则与 §4 单元）

- [x] **ARC-01** 抽 `core` + `core/audiofmt`（R2，2026-11-12）：16k/帧长/上下限/最小可转写时长单点定义在 `internal/core/audiofmt.go`，`asr` 四处副本与 `atvv.SampleRate`/`phone.sampleRate` 双常量删除，生产代码 `16000` 只剩一处定义（R5）。
- [x] **ARC-02** 抽 `fsutil`（R2，2026-11-12）：8 份原子写与 2 处 JSONL 追加合一为 `WriteAtomic`/`AppendJSONL`/`RewriteJSONL`，全部带 `Sync`；`os.Rename`/tmp 拼接全仓只剩 `fsutil` 一处（R7）。
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

- [x] **OPS-01** 手机功能从任何已文档化入口都打不开（R3，2026-11-12 已修）：输入源面板新增「手机输入」开关（`POST /api/phone/enabled` → `config.Save` 校验落盘，响应 `restart_required`，行内区分运行中/重启后生效；保存意图走 `/api/inputs` 的 `configured` 自由字段，typed status 在 R5 之前会丢新键）；README 补「配对手机」整节（同局域网、自签证书只接受一次例外、排错链能力矩阵）。本机冒烟：开/关双向落盘与 UI 联动均通过。
- [ ] **OPS-02** 没有下载即跑路径：`release.yml:47-48` 只带 binary/README/LICENSE/CHANGELOG/example 配置，unit 要 `@ROOT@` 替换（`Makefile:44-58`、`deploy/xiaomi-remote-go.service:11-17`），`make install-user` 不依赖 `build`。动作：tarball 带 unit 与安装脚本。
- [ ] **OPS-03** `doctor` 自己造配置并放过占位 MAC：README 让"先跑 doctor"（`README.md:95`），而 `config.Load` 会写默认值（`internal/config/config.go:246-250`）、`Validate` 只按正则放行 `AA:BB:CC:DD:EE:FF`（:400）。动作：示例地址视为未配置；doctor 不再写文件。
- [ ] **OPS-04** `doctor` 缺真实依赖检查：只看 `bluetoothctl`/`xsel` 是否存在（`main.go:564-566`），不查 BlueZ 守护进程/适配器状态、PipeWire、`wtype`、8787/8788 端口、evdev 读权限、LLM key、`phone.*`；措辞只诊断不建议动作。动作：与 `readyz` 共享检查表，每条给可执行命令。
- [ ] **OPS-05** 语音键静默失败：开 `/dev/input/eventN` 遇 EACCES 每 2 s 重试且不打日志（`internal/keys/f24_linux.go:77-83`），全仓没有 udev 规则或加 `input` 组这一步。动作：首次失败即记一次可操作日志 + doctor 覆盖。
- [x] **OPS-06** `/readyz` 与 UI 的"就绪"要求 root helper + BLE（R3，2026-11-12 已按临时口径修）：`Ready()` 拆 `core_ok`（ASR 链）/`optional_ok`（root helper/BLE/虚拟麦），`ok` = 核心；快速开始清单分「核心必需 / 可选（不影响就绪）」两组，hero 就绪徽标同口径。手机-only 冒烟：无 helper/无遥控/无麦但 ASR 正常 → 已就绪、`/readyz` 200。**分组是临时口径**，真机结论见「未验证」第 5 条。
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
- 只按遥控器语音键（`agent.go:449`）在没有 root helper 时能否独立驱动听写——决定 OPS-06 的分组怎么切（**OPS-06 已按「核心 = ASR 链」临时口径落地，真机结论不符再调**）。
