package phone

// The phone app's wire (this server ↔ the phone page): one typed body per
// message, with "type" discriminating the WS frames (map 棘轮第四次清偿 —
// every shape is byte-identical to the map it replaced). The receive side
// has always been typed (controlMessage); these are the send side.

// readyBody opens the WS session: who the device is and what the channel
// will accept (the injector answers for the key policy, R5/ARC-12).
type readyBody struct {
	Type string `json:"type"` // "ready"

	DeviceID        string `json:"device_id"`
	Name            string `json:"name"`
	MicAvailable    bool   `json:"mic_available"`
	MaxAudioSeconds int    `json:"max_audio_seconds"`
	AllowPower      bool   `json:"allow_power"`
}

// pongBody answers the app's heartbeat, echoing its timestamp.
type pongBody struct {
	Type string `json:"type"` // "pong"
	T    int64  `json:"t"`
}

// keyAckBody confirms an injected key press reached the channel.
type keyAckBody struct {
	Type string `json:"type"` // "key_ack"

	Key   string `json:"key"`
	Value int    `json:"value"`
}

// listeningBody tells the app whether the microphone is live (S6 subtitles
// on the phone mirror the desktop's listening pill).
type listeningBody struct {
	Type string `json:"type"` // "listening"

	Active bool `json:"active"`
}

// transcriptBody pushes one recognised sentence to the app.
type transcriptBody struct {
	Type string `json:"type"` // "transcript"

	Text      string `json:"text"`
	RawText   string `json:"raw_text"`
	Provider  string `json:"provider"`
	Processed bool   `json:"processed"`
	LatencyMS int64  `json:"latency_ms"`
	At        string `json:"at"`
	Truncated bool   `json:"truncated"`
}

// errorBody is a failed operation on either channel.
type errorBody struct {
	Type string `json:"type"` // "error"

	Code    string `json:"code"`
	Message string `json:"message"`
}

// errorFrame builds an error body with its type tag filled in.
func errorFrame(code, message string) errorBody {
	return errorBody{Type: "error", Code: code, Message: message}
}

// pairBody hands the fresh device its id and raw token — the token appears
// exactly once, like the MCP grant's (SEC-02).
type pairBody struct {
	DeviceID string `json:"device_id"`
	Token    string `json:"token"`
}

// ticketBody is the short-lived WS ticket an authenticated device fetches.
type ticketBody struct {
	Ticket    string `json:"ticket"`
	ExpiresIn int    `json:"expires_in"`
}

// pingBody is the authenticated liveness answer.
type pingBody struct {
	OK       bool   `json:"ok"`
	DeviceID string `json:"device_id"`
}

// errorReply is the HTTP error envelope; the WS frames use errorBody.
type errorReply struct {
	OK    bool   `json:"ok"`
	Error string `json:"error"`
}
