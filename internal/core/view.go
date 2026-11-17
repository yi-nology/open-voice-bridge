package core

import "time"

// AgentView is the BLE agent's own snapshot. It is typed because four layers
// used to re-assert the same map keys, and a key that vanished on the way
// became a silent zero rather than a compile error (R6, ARC-05).
//
// Durations and timestamps stay in domain units here; milliseconds and RFC3339
// are wire formats and belong to the edge that serialises them.
type AgentView struct {
	Connected   bool
	Ready       bool
	MicHealthy  bool
	ASR         string
	ASRLocal    bool
	LastText    string
	LastAt      time.Time
	LastLatency time.Duration
}

// ReadyView is the readiness read model: one bool per dependency, plus the two
// error strings that explain a false. An empty error means none, which is how
// the edge decides whether to emit the optional wire field.
//
// OK follows CoreOK, not the conjunction of every dependency: the ASR chain is
// what dictation needs, while the root remapper, BLE and the virtual mic are
// optional gaps a phone-only setup never fills (OPS-06).
//
// The json tags are the diagnostics report's shape (I7). That report embeds the
// view verbatim, so the keys stay the snake_case ones it has always carried.
type ReadyView struct {
	OK            bool   `json:"ok"`
	CoreOK        bool   `json:"core_ok"`
	OptionalOK    bool   `json:"optional_ok"`
	KeydActive    bool   `json:"keyd_active"`
	RemotePresent bool   `json:"remote_present"`
	BleConnected  bool   `json:"ble_connected"`
	AtvvReady     bool   `json:"atvv_ready"`
	AsrReady      bool   `json:"asr_ready"`
	MicHealthy    bool   `json:"mic_healthy"`
	HelperError   string `json:"helper_error,omitempty"`
	AsrError      string `json:"asr_error,omitempty"`
}

// StatusView is the dashboard's read model. Unlike AgentView it is already in
// wire units -- milliseconds and RFC3339 -- because the edge's whole job is to
// copy it into the response, and the diagnostics report embeds it verbatim.
// The producer converts once, here.
//
// Mapping and KeyCounts stay maps: a user's keymap and the per-key counters are
// genuinely open-ended, which is what R6 allows a map for.
type StatusView struct {
	OK             bool              `json:"ok"`
	Address        string            `json:"address"`
	Platform       string            `json:"platform"`
	KeydActive     bool              `json:"keyd_active"`
	RemotePresent  bool              `json:"remote_present"`
	Mapping        map[string]string `json:"mapping"`
	Config         string            `json:"config"`
	MicActive      bool              `json:"mic_active"`
	MicHealthy     bool              `json:"mic_healthy"`
	BleConnected   bool              `json:"ble_connected"`
	AtvvReady      bool              `json:"atvv_ready"`
	AsrBackend     string            `json:"asr_backend"`
	AsrLocal       bool              `json:"asr_local"`
	AsrProvider    string            `json:"asr_provider"`
	AsrProviders   []string          `json:"asr_providers"`
	AsrError       string            `json:"asr_error,omitempty"`
	KeymapProfile  string            `json:"keymap_profile"`
	KeymapProfiles []string          `json:"keymap_profiles"`
	AgentAutostart bool              `json:"agent_autostart"`
	RemapAutostart bool              `json:"remap_autostart"`
	HelperError    string            `json:"helper_error,omitempty"`
	LastText       string            `json:"last_text"`
	LastAt         string            `json:"last_at,omitempty"`
	LastLatencyMS  int64             `json:"last_latency_ms"`
	KeyTotal       int64             `json:"key_total"`
	KeyCounts      map[string]int64  `json:"key_counts"`
}

// DeviceView is one configured input device as the device manager sees it.
// Battery is already the wire's int32 percentage; a device BlueZ knows nothing
// about reports 0, which is what the map path produced too.
type DeviceView struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Address      string   `json:"address"`
	Profile      string   `json:"profile"`
	Enabled      bool     `json:"enabled"`
	Active       bool     `json:"active"`
	Connected    bool     `json:"connected"`
	Capabilities []string `json:"capabilities"`
	Vendor       string   `json:"vendor"`
	Product      string   `json:"product"`
	NameMatch    []string `json:"name_match"`
	Paired       bool     `json:"paired"`
	Battery      int32    `json:"battery"`
}

// ProfileView is one builtin device profile: the template a configured device
// points at, not a device itself.
type ProfileView struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Transport    string   `json:"transport"`
	Capabilities []string `json:"capabilities"`
}

// DevicesView is the device manager's read model: the configured devices, the
// builtin profiles they can point at, and which device is active.
type DevicesView struct {
	OK       bool          `json:"ok"`
	Active   string        `json:"active"`
	Devices  []DeviceView  `json:"devices"`
	Profiles []ProfileView `json:"profiles"`
}

// DiscoveredItem is one scanned input device: what the OS saw, plus the
// profile the matcher suggests for it and its current bluetooth link state.
type DiscoveredItem struct {
	Address      string   `json:"address"`
	Name         string   `json:"name"`
	Vendor       string   `json:"vendor"`
	Product      string   `json:"product"`
	Profile      string   `json:"profile"`
	Capabilities []string `json:"capabilities"`
	Configured   bool     `json:"configured"`
	Paired       bool     `json:"paired"`
	Connected    bool     `json:"connected"`
	Battery      int32    `json:"battery"`
}

// DiscoveredView is the scan result the discovery walk produces (ARC-09).
type DiscoveredView struct {
	OK      bool             `json:"ok"`
	Error   string           `json:"error,omitempty"`
	Devices []DiscoveredItem `json:"devices"`
}

// Permission status values for the permissions page's live board.
const (
	PermOK      = "ok"      // prerequisite met
	PermTodo    = "todo"    // user action needed
	PermNA      = "na"      // does not apply on this platform
	PermUnknown = "unknown" // cannot be probed from here
)

// PermissionItem is one host permission/service row with its live probe
// result (design-qa P2: 授权状态展示).
type PermissionItem struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
	Detail string `json:"detail"`
	Fix    string `json:"fix,omitempty"`
}

// PermissionsView is the permissions page's status board: what the host
// grants, what still needs the user's hand, and what never applies here.
type PermissionsView struct {
	OK       bool             `json:"ok"`
	Platform string           `json:"platform"`
	Items    []PermissionItem `json:"items"`
}

// MCPTokenView is one freshly minted client grant: the raw token appears here
// exactly once (ARC-11 map 棘轮; keys match the map shape it replaced).
type MCPTokenView struct {
	OK      bool         `json:"ok"`
	ID      string       `json:"id"`
	Label   string       `json:"label"`
	Token   string       `json:"token"`
	Snippet ClientConfig `json:"snippet"`
}

// MCPTokenListView is the grants table (never verifier material).
type MCPTokenListView struct {
	OK     bool        `json:"ok"`
	Tokens []TokenInfo `json:"tokens"`
}

// MCPTokenRevokedView reports a revocation. Disconnected is omitted when the
// grant was unknown (the old map carried no such key on that branch), and
// Error is omitted on success — same union shape, now typed.
type MCPTokenRevokedView struct {
	OK           bool   `json:"ok"`
	Error        string `json:"error,omitempty"`
	Disconnected bool   `json:"disconnected,omitempty"`
}

// AgentIdentityView tells a fresh MCP session whose grant it is using.
type AgentIdentityView struct {
	OK    bool   `json:"ok"`
	ID    string `json:"id"`
	Label string `json:"label"`
}

// AgentEntryView is one history row as the authorised AI client sees it:
// the spoken text and its metadata, device address redacted (SEC-02).
type AgentEntryView struct {
	Text      string    `json:"text"`
	At        time.Time `json:"at"`
	Provider  string    `json:"provider"`
	Language  string    `json:"language"`
	Processed bool      `json:"processed"`
	Address   string    `json:"address"`
}

// AgentHistoryView is the client-facing history list.
type AgentHistoryView struct {
	OK      bool             `json:"ok"`
	Entries []AgentEntryView `json:"entries"`
}

// ASRCheckView is one provider row in the diagnostics report's ASR section.
type ASRCheckView struct {
	Provider string `json:"provider"`
	OK       bool   `json:"ok"`
	Detail   string `json:"detail"`
}

// DiagnosticsView is the shareable troubleshooting report (I7). It embeds the
// readiness and status views verbatim — the report's keys have always been
// the views' json tags — and every string that can carry a home path arrives
// already scrubbed (I7: a shared attachment must not leak personal paths).
type DiagnosticsView struct {
	OK          bool           `json:"ok"`
	GeneratedAt string         `json:"generated_at"`
	Version     string         `json:"version"`
	Platform    string         `json:"platform"`
	Ready       ReadyView      `json:"ready"`
	Status      StatusView     `json:"status"`
	ASR         []ASRCheckView `json:"asr"`
}
