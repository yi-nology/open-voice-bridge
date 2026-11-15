package core

// The root remapper's wire protocol (helper ↔ daemon, one JSON document per
// line on the Unix socket). Typed here so both ends and the state owner
// speak the same shapes without free-format maps (map 棘轮, §10-3).

// HelperRequest is a command from the agent to the root daemon.
type HelperRequest struct {
	Cmd        string            `json:"cmd"`
	Mapping    map[string]string `json:"mapping,omitempty"`
	Devices    []DeviceMatch     `json:"devices,omitempty"`
	Apple      bool              `json:"apple,omitempty"`
	AllowPower bool              `json:"allow_power,omitempty"`
	Key        string            `json:"key,omitempty"`
	Value      int               `json:"value,omitempty"`
}

// HelperEvent is one captured key frame the daemon streams to capture
// clients during exclusive listen (S2 捕获).
type HelperEvent struct {
	Event string `json:"event"`
	Key   string `json:"key"`
	Code  int    `json:"code"`
	Value int    `json:"value"`
}

// HelperResponse is the daemon's reply. The ok/error/config fields are the
// admin API's apply answer too (the UI reads exactly those).
type HelperResponse struct {
	OK            bool              `json:"ok"`
	Error         string            `json:"error,omitempty"`
	Event         string            `json:"event,omitempty"`
	Key           string            `json:"key,omitempty"`
	Value         int               `json:"value,omitempty"`
	Devices       int               `json:"devices,omitempty"`
	Apple         bool              `json:"apple,omitempty"`
	AllowPower    bool              `json:"allow_power,omitempty"`
	KeydActive    bool              `json:"keyd_active,omitempty"`
	RemotePresent bool              `json:"remote_present,omitempty"`
	Mapping       map[string]string `json:"mapping,omitempty"`
	Config        string            `json:"config,omitempty"`
	KeyTotal      int64             `json:"key_total,omitempty"`
	KeyCounts     map[string]int64  `json:"key_counts,omitempty"`
}
