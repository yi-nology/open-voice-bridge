package core

// The admin SSE wire (runtime event fan-out → the UI server). Typed so the
// event loop and the UI speak the same envelopes without free-format maps
// (map 棘轮第四次清偿; every shape is byte-identical to the map it replaced).

// SSEEvent is the sealed set of envelopes the runtime publishes over the
// admin SSE. Capture frames (HelperEvent) and the stream_gap marker ride the
// same stream as their own types.
type SSEEvent interface{ sseEvent() }

// TranscriptPartialEvent is the throttled in-progress line (S6 流式字幕):
// a hint beside the final result, never history.
type TranscriptPartialEvent struct {
	Event   string `json:"event"` // "transcript"
	Partial bool   `json:"partial"`
	Text    string `json:"text"`
	At      string `json:"at"`
}

// TranscriptFinalEvent is the settled transcript row: subtitle, history and
// the store all read this shape.
type TranscriptFinalEvent struct {
	Event      string `json:"event"` // "transcript"
	Text       string `json:"text"`
	RawText    string `json:"raw_text"`
	Language   string `json:"language"`
	Provider   string `json:"provider"`
	Source     string `json:"source"`
	SourceName string `json:"source_name"`
	Address    string `json:"address"`
	Processed  bool   `json:"processed"`
	Truncated  bool   `json:"truncated"`
	LatencyMS  int64  `json:"latency_ms"`
	At         string `json:"at"`
}

// CommandEvent reports a recognised voice command (S1): the subtitle prints
// 〔指令〕 and the keymap executes the combo.
type CommandEvent struct {
	Event  string `json:"event"` // "command"
	Text   string `json:"text"`
	Combo  string `json:"combo"`
	Source string `json:"source"`
	At     string `json:"at"`
}

// PhrasePickerEvent opens the common-phrase picker (S2 常用语浮层).
type PhrasePickerEvent struct {
	Event string `json:"event"` // "phrase_picker"
	At    string `json:"at"`
}

// CommonPhraseEvent is a common-phrase insert (S2). User-authored text: it
// shows live but never enters history or the log body.
type CommonPhraseEvent struct {
	Event string `json:"event"` // "phrase"
	Slot  int    `json:"slot"`
	Text  string `json:"text"`
	At    string `json:"at"`
}

// ListeningState is the mic state the subtitle stage shows: which source is
// talking and whether anything is active right now.
type ListeningState struct {
	Active     bool   `json:"active"`
	Source     string `json:"source"`
	SourceName string `json:"source_name"`
}

// ListeningEvent publishes a mic-state change.
type ListeningEvent struct {
	Event string `json:"event"` // "listening"
	ListeningState
}

// SnapshotEvent is the first frame every SSE subscriber receives (I4): the
// live state at subscribe time. Listening rides along only when the runtime
// wires the mic-state seam — same key absence as the old merged map.
type SnapshotEvent struct {
	Event     string          `json:"event"` // "snapshot"
	Capturing bool            `json:"capturing"`
	Listening *ListeningState `json:"listening,omitempty"`
}

func (TranscriptPartialEvent) sseEvent() {}
func (TranscriptFinalEvent) sseEvent()   {}
func (CommandEvent) sseEvent()           {}
func (PhrasePickerEvent) sseEvent()      {}
func (CommonPhraseEvent) sseEvent()      {}
func (ListeningEvent) sseEvent()         {}
func (SnapshotEvent) sseEvent()          {}
