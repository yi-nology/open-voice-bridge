namespace go api

struct HealthReq {}

struct HealthResp {
  1: bool ok
}

struct ReadyReq {}

struct ReadyResp {
  1: bool ok
  2: bool keyd_active
  3: bool remote_present
  4: bool ble_connected
  5: bool atvv_ready
  6: bool asr_ready
  7: optional string helper_error
  8: optional string asr_error
  9: bool mic_healthy
}

struct StatusReq {}

struct StatusResp {
  1: bool ok
  2: string address
  3: bool keyd_active
  4: bool remote_present
  5: map<string, string> mapping
  6: string config
  7: bool mic_active
  8: bool ble_connected
  9: bool atvv_ready
  10: string asr_backend
  11: bool asr_local
  12: string last_text
  13: string asr_provider
  14: list<string> asr_providers
  15: string keymap_profile
  16: list<string> keymap_profiles
  17: bool agent_autostart
  18: bool remap_autostart
  19: bool capturing
  20: optional string helper_error
  21: optional string asr_error
  22: bool mic_healthy
  23: i64 last_latency_ms
  24: string platform
  25: i64 key_total
  26: map<string, i64> key_counts
}

struct ApplyReq {
  1: map<string, string> mapping
}

struct OperationResp {
  1: bool ok
  2: optional string error
  3: optional string config
  4: optional string event
  5: optional string output
}

struct EmptyReq {}

struct ProfileReq {
  1: string profile
}

struct SaveProfileReq {
  1: string name
  2: map<string, string> mapping
}

struct DeleteProfileReq {
  1: string name
}

struct ProviderReq {
  1: string provider
}

struct Diagnostic {
  1: string provider
  2: bool ok
  3: string detail
}

struct ASRTestResp {
  1: bool ok
  2: string provider
  3: list<Diagnostic> diagnostics
}

struct ProviderConfig {
  1: string type
  2: string api_key_file
  3: string base_url
  4: string model
  5: string language
  6: string binary
  7: string model_path
  8: i32 threads
  9: list<string> args
  10: i32 timeout_seconds
}

struct ASRConfig {
  1: string provider
  2: list<string> fallback
  3: string language
  4: map<string, ProviderConfig> providers
}

struct ASRConfigResp {
  1: bool ok
  2: string provider
  3: list<string> fallback
  4: string language
  5: map<string, ProviderConfig> providers
  6: optional string asr_error
}

struct KeymapProfilesResp {
  1: bool ok
  2: string profile
  3: map<string, map<string, string>> profiles
}

struct DeviceInfo {
  1: string id
  2: string name
  3: string address
  4: string profile
  5: bool enabled
  6: bool active
  7: bool connected
  8: list<string> capabilities
  9: string vendor
  10: string product
  11: list<string> name_match
  12: bool paired
  13: i32 battery
}

struct ProfileInfo {
  1: string id
  2: string name
  3: string transport
  4: list<string> capabilities
}

struct DevicesResp {
  1: bool ok
  2: string active
  3: list<DeviceInfo> devices
  4: list<ProfileInfo> profiles
}

struct DiscoveredDevice {
  1: string address
  2: string name
  3: string vendor
  4: string product
  5: string profile
  6: list<string> capabilities
  7: bool configured
  8: bool paired
  9: bool connected
  10: i32 battery
}

struct DiscoveredResp {
  1: bool ok
  2: list<DiscoveredDevice> devices
}

struct DeviceReq {
  1: string id
}

struct DeviceToggleReq {
  1: string id
  2: bool enabled
}

struct DeviceConnectReq {
  1: string id
  2: bool connect
}

struct DeviceSaveReq {
  1: string id
  2: string name
  3: string address
  4: string profile
  5: bool enabled
  6: string vendor
  7: string product
  8: list<string> name_match
}

struct LLMConfig {
  1: bool enabled
  2: string provider
  3: string base_url
  4: string model
  5: string api_key_file
  6: string mode
  7: string prompt
  8: i32 timeout_seconds
}

struct LLMPreset {
  1: string id
  2: string name
  3: string base_url
  4: string model
  5: string model_hint
  6: string key_hint
  7: string description
}

struct LLMConfigResp {
  1: bool ok
  2: LLMConfig config
  3: list<LLMPreset> presets
}

struct LLMDiagnostic {
  1: bool ok
  2: string detail
}

struct LLMTestResp {
  1: bool ok
  2: list<LLMDiagnostic> diagnostics
}

struct LLMProviderReq {
  1: string provider
  2: bool live
}

struct LLMKeyReq {
  1: string key
}

struct HistoryEntry {
  1: string id
  2: string text
  3: string raw_text
  4: string provider
  5: string language
  6: string address
  7: bool processed
  8: string at
  9: i64 latency_ms
  10: string source
  11: string source_name
  12: bool truncated
}

struct HistoryResp {
  1: bool ok
  2: list<HistoryEntry> entries
}

struct HistoryDeleteReq {
  1: string id
}

struct TextReplacement {
  1: string from
  2: string to
}

struct TextSnippet {
  1: string trigger
  2: string text
}

struct TextConfig {
  1: list<TextReplacement> dictionary
  2: list<TextSnippet> snippets
}

struct TextConfigResp {
  1: bool ok
  2: TextConfig config
}

service XiaomiRemoteApi {
  HealthResp Health(1: HealthReq req) (api.get="/healthz")
  ReadyResp Ready(1: ReadyReq req) (api.get="/readyz")
  StatusResp Status(1: StatusReq req) (api.get="/api/status")
  OperationResp Apply(1: ApplyReq req) (api.post="/api/apply")
  OperationResp CaptureStart(1: EmptyReq req) (api.post="/api/capture/start")
  OperationResp CaptureStop(1: EmptyReq req) (api.post="/api/capture/stop")
  OperationResp Reconnect(1: EmptyReq req) (api.post="/api/reconnect")
  OperationResp RestartMic(1: EmptyReq req) (api.post="/api/restart-mic")
  OperationResp SwitchASR(1: ProviderReq req) (api.post="/api/asr/switch")
  ASRConfigResp GetASRConfig(1: EmptyReq req) (api.get="/api/asr/config")
  OperationResp UpdateASRConfig(1: ASRConfig req) (api.post="/api/asr/config")
  ASRTestResp TestASR(1: ProviderReq req) (api.post="/api/asr/test")
  KeymapProfilesResp KeymapProfiles(1: EmptyReq req) (api.get="/api/keymap/profiles")
  OperationResp ApplyKeymapProfile(1: ProfileReq req) (api.post="/api/keymap/apply")
  OperationResp SaveKeymapProfile(1: SaveProfileReq req) (api.post="/api/keymap/save")
  OperationResp DeleteKeymapProfile(1: DeleteProfileReq req) (api.post="/api/keymap/delete")

  DevicesResp GetDevices(1: EmptyReq req) (api.get="/api/devices")
  DiscoveredResp DiscoverDevices(1: EmptyReq req) (api.get="/api/devices/discover")
  OperationResp SelectDevice(1: DeviceReq req) (api.post="/api/devices/select")
  OperationResp ToggleDevice(1: DeviceToggleReq req) (api.post="/api/devices/toggle")
  OperationResp SaveDevice(1: DeviceSaveReq req) (api.post="/api/devices/save")
  OperationResp ConnectDevice(1: DeviceConnectReq req) (api.post="/api/devices/connect")
  OperationResp DeleteDevice(1: DeviceReq req) (api.post="/api/devices/delete")

  LLMConfigResp GetLLMConfig(1: EmptyReq req) (api.get="/api/llm/config")
  OperationResp UpdateLLMConfig(1: LLMConfig req) (api.post="/api/llm/config")
  LLMTestResp TestLLM(1: LLMProviderReq req) (api.post="/api/llm/test")
  OperationResp SaveLLMKey(1: LLMKeyReq req) (api.post="/api/llm/key")

  HistoryResp GetHistory(1: EmptyReq req) (api.get="/api/history")
  OperationResp DeleteHistory(1: HistoryDeleteReq req) (api.post="/api/history/delete")
  OperationResp ClearHistory(1: EmptyReq req) (api.post="/api/history/clear")

  TextConfigResp GetTextConfig(1: EmptyReq req) (api.get="/api/text/config")
  OperationResp UpdateTextConfig(1: TextConfig req) (api.post="/api/text/config")
}
