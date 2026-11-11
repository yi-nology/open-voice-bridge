package ui

import (
	"context"
	"testing"

	"github.com/yi-nology/open-voice-bridge/internal/core"
)

// R6: readiness crosses the boundary as a typed view. The edge copies fields
// instead of re-asserting map keys, so a renamed check cannot silently become
// false on the way to the browser (ARC-05).
func TestReadySerialisesTheTypedView(t *testing.T) {
	s := New(Deps{Ready: func(context.Context) core.ReadyView {
		return core.ReadyView{
			OK:            true,
			KeydActive:    true,
			RemotePresent: true,
			BleConnected:  true,
			AtvvReady:     true,
			AsrReady:      true,
			MicHealthy:    true,
		}
	}})
	resp, err := s.Ready(context.Background())
	if err != nil {
		t.Fatalf("Ready: %v", err)
	}
	if !resp.Ok || !resp.KeydActive || !resp.RemotePresent || !resp.BleConnected ||
		!resp.AtvvReady || !resp.AsrReady || !resp.MicHealthy {
		t.Fatalf("ready response lost a check: %+v", resp)
	}
	if resp.HelperError != nil || resp.AsrError != nil {
		t.Fatalf("empty errors must stay absent, got %+v / %+v", resp.HelperError, resp.AsrError)
	}
}

func TestReadySurfacesBothErrors(t *testing.T) {
	s := New(Deps{Ready: func(context.Context) core.ReadyView {
		return core.ReadyView{HelperError: "helper socket missing", AsrError: "no api key"}
	}})
	resp, err := s.Ready(context.Background())
	if err != nil {
		t.Fatalf("Ready: %v", err)
	}
	if resp.Ok {
		t.Fatal("a failing readiness view must not report ok")
	}
	if resp.HelperError == nil || *resp.HelperError != "helper socket missing" {
		t.Fatalf("helper error = %+v", resp.HelperError)
	}
	if resp.AsrError == nil || *resp.AsrError != "no api key" {
		t.Fatalf("asr error = %+v", resp.AsrError)
	}
}

// A runtime without a readiness producer must still answer, with every check
// false rather than a nil dereference.
func TestReadyWithoutProducerIsAllFalse(t *testing.T) {
	resp, err := New(Deps{}).Ready(context.Background())
	if err != nil {
		t.Fatalf("Ready: %v", err)
	}
	if resp.Ok || resp.KeydActive || resp.RemotePresent || resp.BleConnected ||
		resp.AtvvReady || resp.AsrReady || resp.MicHealthy {
		t.Fatalf("ready response = %+v", resp)
	}
}
