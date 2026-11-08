package ui

import (
	"context"
	"testing"
)

func TestSaveKeymapProfileBackend(t *testing.T) {
	var gotName string
	var gotMapping map[string]string
	server := New(Deps{
		SaveKeymapProfile: func(ctx context.Context, name string, mapping map[string]string) (map[string]any, error) {
			gotName = name
			gotMapping = mapping
			return map[string]any{"ok": true, "config": "{}"}, nil
		},
	})
	resp, err := server.SaveKeymapProfile(context.Background(), "studio", map[string]string{"power": "esc"})
	if err != nil {
		t.Fatalf("SaveKeymapProfile: %v", err)
	}
	if !resp.Ok {
		t.Fatalf("resp = %+v", resp)
	}
	if gotName != "studio" || gotMapping["power"] != "esc" {
		t.Fatalf("unexpected save call: name=%q mapping=%v", gotName, gotMapping)
	}
	if resp.Config == nil || *resp.Config != "{}" {
		t.Fatalf("config = %v", resp.Config)
	}
}

func TestDeleteKeymapProfileBackend(t *testing.T) {
	var gotName string
	server := New(Deps{
		DeleteKeymapProfile: func(ctx context.Context, name string) error {
			gotName = name
			return nil
		},
	})
	resp, err := server.DeleteKeymapProfile(context.Background(), "studio")
	if err != nil {
		t.Fatalf("DeleteKeymapProfile: %v", err)
	}
	if !resp.Ok || gotName != "studio" {
		t.Fatalf("resp=%+v name=%q", resp, gotName)
	}
}

func TestStatusBackendConvertsMapping(t *testing.T) {
	server := New(Deps{
		Status: func(context.Context) map[string]any {
			return map[string]any{
				"ok":             true,
				"address":        "AA:BB:CC:DD:EE:FF",
				"mapping":        map[string]string{"power": "esc"},
				"asr_providers":  []string{"mimo"},
				"keymap_profile": "coding",
				"asr_error":      "missing key",
			}
		},
	})
	resp, err := server.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if resp.Mapping["power"] != "esc" || resp.KeymapProfile != "coding" {
		t.Fatalf("unexpected status: %+v", resp)
	}
	if resp.AsrError == nil || *resp.AsrError != "missing key" {
		t.Fatalf("asr_error = %v", resp.AsrError)
	}
}
