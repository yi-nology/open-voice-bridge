package phone

import "github.com/yi-nology/open-voice-bridge/internal/input"

// The phone admin payload views (ARC-11 map 棘轮第四次清偿). They replace the
// free-form maps the desktop panel consumed; json tags match the old keys one
// for one, and omitempty mirrors the old union shapes — the disabled branches
// carried only the keys they set, and the wire must stay that way.

// InputsView is the input-source summary the dashboard shows beside the
// phone switch.
type InputsView struct {
	Enabled    bool           `json:"enabled"`
	Configured bool           `json:"configured"`
	Sources    []input.Status `json:"sources"`
}

// PairingView is the pairing card: code, deep link and QR (波 3 二维码配对).
// Code and friends appear only on the enabled branch — a disabled phone
// answers {"enabled": false} and nothing else.
type PairingView struct {
	Enabled   bool   `json:"enabled"`
	Code      string `json:"code,omitempty"`
	ExpiresAt string `json:"expires_at,omitempty"`
	Scheme    string `json:"scheme,omitempty"`
	Port      string `json:"port,omitempty"`
	Host      string `json:"host,omitempty"`
	Link      string `json:"link,omitempty"`
	QR        string `json:"qr,omitempty"`
}

// DeviceListView is the paired-device table plus the live session state.
// Connected rides a pointer so the enabled branch keeps its key even for an
// empty list, while the disabled branch omits it entirely (as the old map did).
type DeviceListView struct {
	Enabled   bool            `json:"enabled"`
	Devices   []Device        `json:"devices"`
	Connected *[]input.Status `json:"connected,omitempty"`
}

// RevokeView reports a revocation. Disconnected is present on both live
// branches (the WebSocket is cut either way, #8) and absent when the phone
// input is disabled outright.
type RevokeView struct {
	OK           bool   `json:"ok"`
	Error        string `json:"error,omitempty"`
	Disconnected *bool  `json:"disconnected,omitempty"`
}
