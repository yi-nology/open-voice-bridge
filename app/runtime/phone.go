// Package runtime is the composition root: it wires the concrete subsystems
// together and owns the payloads the admin edge serves.
package runtime

import (
	"context"
	"encoding/base64"
	"time"

	qrcode "github.com/skip2/go-qrcode"

	"github.com/yi-nology/open-voice-bridge/internal/core"
	"github.com/yi-nology/open-voice-bridge/internal/input"
	"github.com/yi-nology/open-voice-bridge/internal/phone"
	"github.com/yi-nology/open-voice-bridge/internal/speech"
)

// PhoneSpeech adapts the shared transcription chain to the phone listener's
// narrow interface, so internal/phone never imports internal/agent and both
// input sources provably run one chain.
type PhoneSpeech struct {
	Metrics  *PhoneMetrics
	Pipeline *speech.Pipeline
}

func (s PhoneSpeech) TranscribeUtterance(ctx context.Context, samples []int16, meta phone.UtteranceMeta) (phone.UtteranceResult, bool) {
	if s.Metrics != nil {
		s.Metrics.AudioSeconds.Add(float64(len(samples)) / float64(core.SampleRate))
	}
	transcript, ok := s.Pipeline.Transcribe(ctx, speech.Utterance{
		Samples:    samples,
		SampleRate: meta.SampleRate,
		Ref: speech.SourceRef{
			Kind:    string(input.KindPhone),
			Name:    meta.Name,
			Address: meta.Address,
		},
		Truncated: meta.Truncated,
	})
	if !ok {
		if s.Metrics != nil {
			s.Metrics.Utterances.WithLabelValues("miss").Inc()
		}
		return phone.UtteranceResult{}, false
	}
	if s.Metrics != nil {
		s.Metrics.Utterances.WithLabelValues("ok").Inc()
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

func (a PhoneAdmin) Inputs(context.Context) phone.InputsView {
	return phone.InputsView{Enabled: a.Enabled, Configured: a.Configured(), Sources: a.Hub.Sources()}
}

func (a PhoneAdmin) Pairing(context.Context) phone.PairingView {
	if !a.Enabled {
		return phone.PairingView{}
	}
	code, expires := a.Store.GeneratePairCode()
	scheme, host, port := phone.PairingBase(a.TLSMode, a.Addr)
	out := phone.PairingView{
		Enabled:   true,
		Code:      code,
		ExpiresAt: expires.Format(time.RFC3339),
		Scheme:    scheme,
		Port:      port,
	}
	if host != "" {
		out.Host = host
		out.Link = scheme + "://" + host + ":" + port + "/phone"
		// The QR carries the same auto-filling link the card shows — one
		// scan on the phone lands on the pairing screen with the code in the
		// #pair= fragment (波 3 二维码配对).
		if png, err := qrcode.Encode(out.Link+"#pair="+code, qrcode.Medium, 256); err == nil {
			out.QR = "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
		}
	}
	return out
}

func (a PhoneAdmin) Devices(context.Context) phone.DeviceListView {
	if !a.Enabled {
		return phone.DeviceListView{Devices: []phone.Device{}}
	}
	connected := a.Server.ConnectedDevices()
	return phone.DeviceListView{Enabled: true, Devices: a.Store.ListDevices(), Connected: &connected}
}

func (a PhoneAdmin) Revoke(_ context.Context, deviceID string) phone.RevokeView {
	if !a.Enabled {
		return phone.RevokeView{Error: "phone input disabled"}
	}
	err := a.Store.Revoke(deviceID)
	// Revoking is a deliberate cut: the live WebSocket must die whether or not
	// the persist step succeeded. Skipping Disconnect on a failed persist left
	// the revoked phone injecting keys and streaming audio until it happened
	// to reconnect (#8); the operator sees ok:false and can retry.
	disconnected := a.Server.Disconnect(deviceID)
	if err != nil {
		return phone.RevokeView{Error: err.Error(), Disconnected: &disconnected}
	}
	return phone.RevokeView{OK: true, Disconnected: &disconnected}
}
