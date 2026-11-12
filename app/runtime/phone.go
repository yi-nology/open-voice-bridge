// Package runtime is the composition root: it wires the concrete subsystems
// together and owns the payloads the admin edge serves.
package runtime

import (
	"context"
	"time"

	"github.com/yi-nology/open-voice-bridge/internal/agent"
	"github.com/yi-nology/open-voice-bridge/internal/input"
	"github.com/yi-nology/open-voice-bridge/internal/phone"
)

// PhoneSpeech adapts the shared transcription chain to the phone listener's
// narrow interface, so internal/phone never imports internal/agent and both
// input sources provably run one chain.
type PhoneSpeech struct {
	Pipeline *agent.SpeechPipeline
}

func (s PhoneSpeech) TranscribeUtterance(ctx context.Context, samples []int16, meta phone.UtteranceMeta) (phone.UtteranceResult, bool) {
	transcript, ok := s.Pipeline.Transcribe(ctx, agent.Utterance{
		Samples:    samples,
		SampleRate: meta.SampleRate,
		Ref: agent.SourceRef{
			Kind:    string(input.KindPhone),
			Name:    meta.Name,
			Address: meta.Address,
		},
		Truncated: meta.Truncated,
	})
	if !ok {
		return phone.UtteranceResult{}, false
	}
	text := transcript.Text
	if transcript.Command != "" {
		text = "〔指令〕" + text
	}
	return phone.UtteranceResult{
		Text:      text,
		RawText:   transcript.RawText,
		Provider:  transcript.Provider,
		Processed: transcript.Processed,
		LatencyMS: transcript.Latency.Milliseconds(),
		At:        transcript.At,
	}, true
}

// PhoneAdmin serves the desktop's phone-input management payloads. Store and
// Server stay nil while the listener is disabled, so every method answers from
// Enabled first. Configured reads the saved document, which is what the panel
// shows beside the running state until a restart lands (OPS-01).
type PhoneAdmin struct {
	Enabled    bool
	Configured func() bool
	Store      *phone.Store
	Server     *phone.Server
	Hub        *input.Hub
	TLSMode    string
	Addr       string
}

func (a PhoneAdmin) Inputs(context.Context) map[string]any {
	return map[string]any{"enabled": a.Enabled, "configured": a.Configured(), "sources": a.Hub.Sources()}
}

func (a PhoneAdmin) Pairing(context.Context) map[string]any {
	if !a.Enabled {
		return map[string]any{"enabled": false}
	}
	code, expires := a.Store.GeneratePairCode()
	scheme, host, port := phone.PairingBase(a.TLSMode, a.Addr)
	out := map[string]any{
		"enabled":    true,
		"code":       code,
		"expires_at": expires.Format(time.RFC3339),
		"scheme":     scheme,
		"port":       port,
	}
	if host != "" {
		out["host"] = host
		out["link"] = scheme + "://" + host + ":" + port + "/phone"
	}
	return out
}

func (a PhoneAdmin) Devices(context.Context) map[string]any {
	if !a.Enabled {
		return map[string]any{"enabled": false, "devices": []phone.Device{}}
	}
	return map[string]any{"enabled": true, "devices": a.Store.ListDevices(), "connected": a.Server.ConnectedDevices()}
}

func (a PhoneAdmin) Revoke(_ context.Context, deviceID string) map[string]any {
	if !a.Enabled {
		return map[string]any{"ok": false, "error": "phone input disabled"}
	}
	if err := a.Store.Revoke(deviceID); err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	// The token is gone, but the WebSocket that is already open needs closing
	// or a revoked phone keeps speaking until it reconnects.
	disconnected := a.Server.Disconnect(deviceID)
	return map[string]any{"ok": true, "disconnected": disconnected}
}
