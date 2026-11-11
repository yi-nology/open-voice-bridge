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
