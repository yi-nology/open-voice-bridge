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
