package gtvkeys

import "testing"

// Payloads below are the documented wire formats (onn-remote-linux,
// google-tv-remote-bridge): LE consumer usages, the fw 26.0 index reports,
// keyboard volume reports, and all-zero releases.

func TestDecodeUsageReports(t *testing.T) {
	cases := []struct {
		report []byte
		want   string
	}{
		{[]byte{0x42, 0x00}, "up"},   // Consumer Up
		{[]byte{0x24, 0x02}, "back"}, // Consumer Back
		{[]byte{0x21, 0x02}, "assistant"},
		{[]byte{0x30, 0x00, 0x00, 0x00}, "power"}, // 4-byte form
	}
	for _, tc := range cases {
		events := NewDecoder().Handle("h1", tc.report)
		if len(events) != 1 || events[0].Button != tc.want || !events[0].Down {
			t.Fatalf("Handle(%x) = %+v, want a %q press", tc.report, events, tc.want)
		}
	}
}

func TestDecodeChromecastIndexReports(t *testing.T) {
	// fw 26.0 sends ONE byte: a 1-based index into the report map's list.
	cases := []struct {
		report []byte
		want   string
	}{
		{[]byte{0x01}, "power"},     // 0x19E
		{[]byte{0x03}, "up"},        // 0x42
		{[]byte{0x07}, "select"},    // 0x41
		{[]byte{0x09}, "assistant"}, // 0x221
		{[]byte{0x11}, "input"},     // 0x89, last entry
	}
	for _, tc := range cases {
		events := NewDecoder().Handle("h1", tc.report)
		if len(events) != 1 || events[0].Button != tc.want || !events[0].Down {
			t.Fatalf("Handle(%x) = %+v, want a %q press", tc.report, events, tc.want)
		}
	}
}

func TestDecodeKeyboardVolumeReport(t *testing.T) {
	events := NewDecoder().Handle("h2", []byte{0x00, 0x00, 0x80, 0x00, 0x00, 0x00, 0x00, 0x00})
	if len(events) != 1 || events[0].Button != "volup" || !events[0].Down {
		t.Fatalf("events = %+v, want a volup press", events)
	}
}

func TestZeroReportReleases(t *testing.T) {
	d := NewDecoder()
	if events := d.Handle("h1", []byte{0x42, 0x00}); len(events) != 1 || !events[0].Down {
		t.Fatalf("press = %+v", events)
	}
	events := d.Handle("h1", []byte{0x00, 0x00, 0x00, 0x00})
	if len(events) != 1 || events[0].Down || events[0].Button != "up" {
		t.Fatalf("release = %+v", events)
	}
	// A second release with nothing held is a no-op.
	if events := d.Handle("h1", []byte{0x00}); len(events) != 0 {
		t.Fatalf("spurious release = %+v", events)
	}
}

func TestHeldTrackingAcrossReports(t *testing.T) {
	d := NewDecoder()
	d.Handle("h1", []byte{0x42, 0x00}) // up down
	if events := d.Handle("h1", []byte{0x42, 0x00}); len(events) != 0 {
		t.Fatalf("auto-repeat produced %+v", events)
	}
	// A different button while one is held: old key releases, new one presses.
	events := d.Handle("h1", []byte{0x43, 0x00}) // down
	if len(events) != 2 || events[0].Button != "up" || events[0].Down ||
		events[1].Button != "down" || !events[1].Down {
		t.Fatalf("switch = %+v", events)
	}
	// Sources are independent: h2's release never frees h1's key.
	if events := d.Handle("h2", []byte{0x00, 0x00}); len(events) != 0 {
		t.Fatalf("other-source release = %+v", events)
	}
}

func TestIndexReportIsNotConfusedWithUsageCodes(t *testing.T) {
	// 0x0221 (assistant) has low byte 0x21 — outside 1..17, so the LE usage
	// path must win; the index path only claims tiny payloads.
	events := NewDecoder().Handle("h1", []byte{0x21, 0x02})
	if len(events) != 1 || events[0].Button != "assistant" {
		t.Fatalf("events = %+v, want assistant", events)
	}
}

func TestToLogical(t *testing.T) {
	cases := map[string]struct {
		want string
		ok   bool
	}{
		"select":    {"enter", true},
		"volup":     {"volumeup", true},
		"voldown":   {"volumedown", true},
		"up":        {"up", true},
		"mute":      {"mute", true},
		"playpause": {"playpause", true},
		"power":     {"power", true},
		"settings":  {"compose", true},
		"tv":        {"grave", true},
		"assistant": {"", false}, // the ATVV control channel owns the mic
		"youtube":   {"", false},
		"bookmark":  {"", false},
		"":          {"", false},
	}
	for button, want := range cases {
		got, ok := ToLogical(button)
		if got != want.want || ok != want.ok {
			t.Fatalf("ToLogical(%q) = %q,%v want %q,%v", button, got, ok, want.want, want.ok)
		}
	}
}
