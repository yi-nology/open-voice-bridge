package phone

import (
	"context"
	"time"
)

// UtteranceMeta labels which phone produced an utterance.
type UtteranceMeta struct {
	DeviceID string
	Name     string
	Address  string
	// SampleRate states what the buffered samples actually are, so the chain
	// never has to assume a rate on the phone's behalf (DOM-01).
	SampleRate int
	// Truncated marks a capture that hit the server's time cap instead of
	// ending on the user's release (DOM-09).
	Truncated bool
}

// UtteranceResult is the narrow answer the phone page needs echoed back. Only
// primitives and a time, so this package never imports the speech chain.
type UtteranceResult struct {
	Text      string
	RawText   string
	Provider  string
	Processed bool
	LatencyMS int64
	At        time.Time
}

// Transcriber runs one phone utterance through the same chain the BLE remote
// uses. main.go adapts the speech pipeline to it; a nil Transcriber keeps the
// microphone switched off while keys stay live.
type Transcriber interface {
	TranscribeUtterance(ctx context.Context, samples []int16, meta UtteranceMeta) (UtteranceResult, bool)
}
