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
