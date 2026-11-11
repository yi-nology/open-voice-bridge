package ui

import (
	"context"
	"testing"

	"github.com/yi-nology/open-voice-bridge/internal/core"
)

// R6: the status read model crosses the boundary as a typed view. The edge
// copies fields; it no longer re-asserts 26 map keys, so a key that vanishes on
// the way cannot silently become a zero in the browser (ARC-05).
func TestStatusSerialisesTheTypedView(t *testing.T) {
	s := New(Deps{Status: func(context.Context) core.StatusView {
		return core.StatusView{
			OK:             true,
			Address:        "AA:BB:CC:DD:EE:FF",
			Platform:       "linux",
			KeydActive:     true,
			RemotePresent:  true,
			Mapping:        map[string]string{"power": "esc"},
			Config:         "{}",
			MicActive:      true,
			MicHealthy:     true,
			BleConnected:   true,
			AtvvReady:      true,
			AsrBackend:     "mimo",
			AsrLocal:       false,
			AsrProvider:    "mimo",
			AsrProviders:   []string{"mimo"},
			KeymapProfile:  "coding",
			KeymapProfiles: []string{"coding", "media"},
			AgentAutostart: true,
			RemapAutostart: true,
			LastText:       "你好",
			LastLatencyMS:  320,
			KeyTotal:       7,
			KeyCounts:      map[string]int64{"f5": 3},
		}
	}})
	resp, err := s.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !resp.Ok || resp.Address != "AA:BB:CC:DD:EE:FF" || resp.Platform != "linux" {
		t.Fatalf("status = %+v", resp)
	}
	if !resp.KeydActive || !resp.RemotePresent || !resp.MicActive || !resp.MicHealthy ||
		!resp.BleConnected || !resp.AtvvReady || !resp.AgentAutostart || !resp.RemapAutostart {
		t.Fatalf("status lost a flag: %+v", resp)
	}
	if resp.Mapping["power"] != "esc" || resp.Config != "{}" {
		t.Fatalf("mapping/config = %+v / %q", resp.Mapping, resp.Config)
	}
	if resp.AsrBackend != "mimo" || resp.AsrLocal || resp.AsrProvider != "mimo" ||
		len(resp.AsrProviders) != 1 || resp.AsrProviders[0] != "mimo" {
		t.Fatalf("asr = %+v", resp)
	}
	if resp.KeymapProfile != "coding" || len(resp.KeymapProfiles) != 2 {
		t.Fatalf("keymap = %+v", resp)
	}
	if resp.LastText != "你好" || resp.LastLatencyMs != 320 {
		t.Fatalf("last text/latency = %q / %d", resp.LastText, resp.LastLatencyMs)
	}
	if resp.KeyTotal != 7 || resp.KeyCounts["f5"] != 3 {
		t.Fatalf("key counters = %d / %+v", resp.KeyTotal, resp.KeyCounts)
	}
	if resp.HelperError != nil || resp.AsrError != nil {
		t.Fatalf("empty errors must stay absent: %+v / %+v", resp.HelperError, resp.AsrError)
	}
}

// The agent has always recorded when the last utterance finished, and the
// service has always copied it, but no consumer ever read it: the timestamp was
// produced, carried one hop, and dropped. It now reaches the page (ARC-05).
func TestStatusSurfacesLastAt(t *testing.T) {
	s := New(Deps{Status: func(context.Context) core.StatusView {
		return core.StatusView{LastText: "你好", LastAt: "2026-11-11T09:30:00Z"}
	}})
	resp, err := s.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if resp.LastAt == nil || *resp.LastAt != "2026-11-11T09:30:00Z" {
		t.Fatalf("last_at = %+v", resp.LastAt)
	}
}

func TestStatusOmitsLastAtBeforeFirstUtterance(t *testing.T) {
	resp, err := New(Deps{Status: func(context.Context) core.StatusView {
		return core.StatusView{OK: true}
	}}).Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if resp.LastAt != nil {
		t.Fatalf("last_at = %+v, want absent", resp.LastAt)
	}
}

func TestStatusWithoutProducerIsZero(t *testing.T) {
	resp, err := New(Deps{}).Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if resp.Ok || resp.Address != "" || resp.LastAt != nil {
		t.Fatalf("status = %+v", resp)
	}
}
