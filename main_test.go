package main

import (
	"strings"
	"testing"

	"github.com/yi-nology/open-voice-bridge/internal/input"
)

func TestSpeakingLabel(t *testing.T) {
	devices := []input.Status{
		{ID: input.RemoteID, Kind: input.KindRemote, Name: "小米遥控器", Speaking: false},
		{ID: "phone-abc", Kind: input.KindPhone, Name: "Pixel", Speaking: true},
	}
	if active, source, name := speakingLabel(input.AudioState{Owner: "phone-abc", Speaking: true}, devices); !active || source != "phone" || name != "Pixel" {
		t.Fatalf("phone owner mapped to (%v, %q, %q)", active, source, name)
	}
	if active, source, name := speakingLabel(input.AudioState{Owner: input.RemoteID, Speaking: true}, devices); !active || source != "remote" || !strings.Contains(name, "遥控") {
		t.Fatalf("remote owner mapped to (%v, %q, %q)", active, source, name)
	}
	if active, _, _ := speakingLabel(input.AudioState{}, devices); active {
		t.Fatal("an idle hub must not report a speaker")
	}
	// An owner we cannot resolve still reports that something is speaking;
	// the pill must not go quiet just because a label is missing.
	if active, source, name := speakingLabel(input.AudioState{Owner: "gone", Speaking: true}, devices); !active || source != "phone" || name != "gone" {
		t.Fatalf("unknown owner mapped to (%v, %q, %q)", active, source, name)
	}
}
