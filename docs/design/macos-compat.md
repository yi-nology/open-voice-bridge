# macOS 平台兼容（ARN9 · Mac mini）

- 日期：2026-11-11
- 状态：执行中
- 口径：**兼容，不是移植**。Linux（BlueZ + PipeWire + evdev/uinput）仍是第一平台，既有行为、部署物与 CI 门禁全部保持不动；macOS 是同一二进制的第二平台，全部新增代码走 `//go:build` 平台文件，Linux 路径零改动或只做等价化小改。
- 目标场景：Mac mini 上已配对的小米蓝牙语音遥控器 ARN9（VID:PID `2717:32b8`，系统名「小米蓝牙语音遥控器」，固件 2671），按住语音键 → ATVV 收音 → ASR → 文字粘贴到焦点窗口，字幕与历史照常。管理 UI 只绑 `127.0.0.1:8787` 的承诺不变。

## 平台矩阵（v1）

| 能力 | Linux（现状） | macOS v1 | 说明 |
| --- | --- | --- | --- |
| BLE/ATVV 传输 | BlueZ D-Bus（`internal/bluez/transport_linux.go`） | CoreBluetooth 桥（新增） | 接口 `Connect/Subscribe/WriteTX/Close/Disconnected` 不变 |
| ASR / 词典 / LLM / 字幕 / 历史 / 指标 / 管理 UI | Go 便携代码 | 原样可用 | 无平台依赖 |
| 粘贴到焦点窗口 | `wtype`（Wayland）/ `xsel`（X11） | `pbcopy` + 合成 ⌘V | 需要「辅助功能」权限 |
| 设备发现 | `/proc/bus/input/devices` | `system_profiler SPBluetoothDataType` 解析 | 解析器做成纯函数可测 |
| 设备状态（连接/电量） | BlueZ D-Bus | v1 只报连接与名称，电量报 -1 | 后续可走 Battery Service `180F` |
| 虚拟麦克风（PipeWire） | `pw-loopback` / `pw-cat` | **不做**（需 AudioServerPlugin） | agent 以 `sink=nil` 降级运行，`mic_healthy=false`；ASR 链不受影响（`agent.handleAudio` 本就容忍 nil sink） |
| 按键重映射 | root helper（evdev `EVIOCGRAB` → uinput） | **不做**（无等价原语） | helper 缺席时 UI 显示 `helper_error`，按键走遥控器原生 HID |
| F24 语音键兜底 | `internal/keys/f24_linux.go` | 不做 | 主路径是 ATVV 控制事件，兜底只是降级保障 |

## Linux 依赖 → macOS 对应

| 依赖 | Linux 载体 | macOS 载体 |
| --- | --- | --- |
| BLE GATT（ATVV 四特征） | `godbus` → BlueZ `org.bluez.GattCharacteristic1` | `ble_bridge_darwin.m`（CoreBluetooth，cgo） |
| 地址语义 | 蓝牙 MAC（BlueZ `Address`） | CBPeripheral `identifier`（UUID），见下文地址解析 |
| 粘贴 | `wtype` / `xsel` 子进程 | `pbcopy` + `osascript` 合成 ⌘V 子进程 |
| 设备枚举 | `/proc/bus/input/devices` | `system_profiler SPBluetoothDataType -json` |
| root helper | `remap` 命令 + unix socket | 不部署；客户端连接失败即降级（现有行为） |
| 音频 sink | PipeWire | 无（nil sink 降级） |

## BLE 传输（唯一硬骨头）

`internal/bluez` 保持包名（接口与调用点零改动），新增：

- `ble_bridge_darwin.h` / `ble_bridge_darwin.m`：ObjC CoreBluetooth 桥。C ABI：
  `atvv_open(address_or_id, callbacks, errbuf)` / `atvv_connect(h)` / `atvv_subscribe(h)` /
  `atvv_write(h, bytes)` / `atvv_close(h)`；回调三个：`on_audio` / `on_control` / `on_disconnect`。
- 线程模型：`CBCentralManager` 挂在私有 serial dispatch queue；所有 delegate 回调在该 queue 触发，
  经 C 函数指针进 Go（`//export`），Go 侧立即投递到 channel，不在回调里做业务。
- `transport_darwin.go`（`//go:build darwin && cgo`）：实现 `Transport`，channel 化回调，语义对齐 Linux 版
  （重复 `Subscribe` 拒绝、`Close` 幂等、断线只关一次 `disconnected`）。
- `transport_darwin_nocgo.go`（`//go:build darwin && !cgo`）：`ErrUnsupported` 桩，保住 CI 的
  `GOOS=darwin` 交叉编译（Linux runner 上 `CGO_ENABLED=0`，桥不会被编译）。
- `make build` 在 macOS 上默认 `CGO_ENABLED=1`，产出的仍是单个 `bin/xiaomi-remote`。

### 地址解析（MAC → CBPeripheral）

CoreBluetooth 不暴露 BLE MAC。v1 解析顺序：

1. 状态目录里持久化的 `address → CBPeripheral.identifier` 映射（`~/.local/state/xiaomi-remote/ble-id-map.json`）；
2. 未命中则扫描广播，按「名字命中 profile 的 `match.names`」或「唯一一个暴露 ATVV 服务
   `ab5e0001-5a21-4f05-bc7d-af01f617b664` 的外设」选定，写回映射；
3. 配置里的 `address` 也允许直接写 CoreBluetooth UUID。

同名多机（两台 ARN9）v1 不做消歧，文档里写明。

## 粘贴

`internal/paste/darwin.go`：文本进 `pbcopy`，再 `osascript -e 'tell application "System Events" to keystroke "v" using command down'`。
与 Linux 版同构（子进程 + 错误上抛），不引入 cgo。权限：系统设置 → 隐私与安全性 → 辅助功能，授予运行该二进制的宿主
（终端或 launchd 服务）。

## 设备发现 / 状态

- `internal/device/discover_darwin.go`：跑 `system_profiler SPBluetoothDataType -json`，解析器
  `parseSPBluetooth(data string) []Discovered` 做成纯函数 + 表驱动测试；VID/PID/name 再走既有
  `Builtin().MatchInputBlock` 同一套 profile 匹配（把字段拼成 input block 形状或加 `MatchName`）。
- `internal/bluez/status_darwin.go`：`KnownDevices` 用同一解析器；`Status` 回 `Connected`/`Name`，
  `Battery=-1`；`Connect`/`Disconnect` v1 回 `ErrUnsupported`（macOS 没有配对级 API 给 CLI）。

## 主程序等价化小改（Linux 行为不变）

- 虚拟麦克风的替代不是「降级拒启」而是 **null sink**（`internal/audio/darwin.go`）：
  `Start/Write/Close` 全成功（Write 直接丢弃样本），`Healthy()` 为假。ASR 链从
  `agent.handleAudio` 直接取样本（那里本就容忍 sink 缺席），`main.go` 一行不改，
  Linux 的 PipeWire 路径零影响。
- `doctor` 的主机检查拆进 `doctorHost_linux.go` / `doctorHost_darwin.go` /
  `doctorHost_other.go`：macOS 检查遥控器是否可见/已连接、提示辅助功能权限；
  不再拿 systemd、PipeWire、xsel、uinput 的缺席误报失败。
- 配置文件位置跟 `os.UserConfigDir()` 走：macOS 上是
  `~/Library/Application Support/xiaomi-remote/config.json`。

## 权限清单（用户侧）

1. **蓝牙**：首次访问 CoreBluetooth 时系统弹窗，或手动在系统设置里允许。
2. **辅助功能**：粘贴合成 ⌘V 需要；不给则语音只出字幕/历史，不上屏。

## 测试与验收

- 纯 Go：粘贴命令拼装、`parseSPBluetooth` 表驱动、darwin 传输的回调投递/幂等关闭（桥后置回调注入）、
  `audio` 降级路径（`sink=nil` 时 `handleAudio` 仍出句子）。
- 门禁：`gofmt -l . && go vet -tags stdjson ./... && go test -tags stdjson -count=1 ./...`（本机 darwin 全绿）；
  CI 的 linux 作业与 darwin 交叉编译不动（`CGO_ENABLED=0` 时编 `transport_darwin_nocgo.go` 桩）。
- 真机验收（ARN9 @ Mac mini，2026-11-11 已做）：`doctor` 识别遥控器「已连接」→ agent 起 UI →
  CoreBluetooth 连上真机、ATVV 协商 16 kHz/ADPCM/帧 120 → 按住语音键松手出句、历史留档
  （`latency_ms` 个位数）。**剩两项用户动作**：辅助功能授权后粘贴上屏；配置 ASR 凭据后真实听写
  （未配时可用 `asr.command` 桩验证管道）。

### 平台适配验证（2026-11-11 第二轮，darwin/arm64）

对 macOS 平台做了一轮系统性验证与界面适配：

- **构建**：`CGO_ENABLED=1`（CoreBluetooth 桥）与 `CGO_ENABLED=0`（nocgo 桩）双路径均产出
  Mach-O arm64；21 个包 `go test` 全绿，关键包 `-race` 通过。
- **蓝牙设备发现**：`system_profiler SPBluetoothDataType -json` 真机数据经 `parseSPBluetooth`
  解析出 11 台设备，ARN9（`2717:32b8`）自动匹配 `rc003` profile、能力 `[keys voice]`；
  `KnownDevices`/`DiscoverInputDevices`/`/api/devices/discover` 三条路径一致。
- **粘贴链路**：`pbcopy`→`pbpaste` 往返一致；`osascript` 合成 ⌘V 在辅助功能已授权的宿主上成功。
- **CoreBluetooth 桥**：agent 连上真机 ARN9，`ATVV bridge ready`、caps 协商
  `codec=0x02 rate=16000 version=0x100`；`/api/status` 的 `ble_connected=true`、`atvv_ready=true`。
- **doctor**：macOS 分支只报平台相关项（遥控器可见/已连接、辅助功能提示），不再误报 systemd、
  PipeWire、xsel、uinput 缺失。
- **UI 平台感知**（本轮修复）：`StatusResp` 新增 `platform` 字段（IDL 24 + 生成模型 + handler 同步），
  界面据此改写——按键映射卡「不适用（原生 HID）」、麦克风卡「直采模式（ASR 直采，无虚拟麦）」、
  自启卡「手动启动（launchd）」、权限页切换为蓝牙/辅助功能清单、快速开始里映射与麦克风两项
  在 macOS 上按设计视为就绪；顶部徽标「映射不可用」改为「原生 HID」。`mic_healthy=false` 在
  macOS 是预期状态，界面不再标红。
- **MCP 本地 Agent 访问**：`xiaomi-remote mcp --token …` 在 macOS 上完成 stdio 握手、
  `tools/list`（`history_list`/`history_search`/`usage_stats`）、真实工具调用与错误令牌 401 拒绝；
  客户端配置里的二进制路径经 `EvalSymlinks` 解析为 `/private/tmp/…` 真实路径。

**仍未在本轮验证**（需要物理按键或用户授权）：按住语音键的完整听写出字（需配 ASR 凭据）、
辅助功能未授权时的降级表现、launchd 常驻部署。

## 明确不做（v1 边界）

- 虚拟音频设备（AudioServerPlugin / BlackHole 式 CABLE Output）；
- macOS 按键重映射与手势（IOHIDManager seize + CGEventPost）；
- F24 兜底触发、root helper 的 macOS 等价物；
- 电量读取、断开/重连按钮（UI 里会如实报不支持）。
