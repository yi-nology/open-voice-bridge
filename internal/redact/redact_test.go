package redact

import "testing"

func TestMACDropsTheDeviceSpecificOctets(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"AA:BB:CC:DD:EE:FF", "AA:BB:CC:**:**:FF"},
		{"  aa:bb:cc:dd:ee:ff  ", "aa:bb:cc:**:**:ff"},
		// Anything that isn't an address must not be echoed back: the field may
		// hold a host name, a serial or a value nobody predicted.
		{"not-an-address", ""},
		{"AA:BB:CC:DD:EE", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := MAC(c.in); got != c.want {
			t.Errorf("MAC(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestProviderCodeKeepsIdentifiersNotProse(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"openai style", `{"error":{"type":"invalid_request_error","code":"content_filter"}}`, "invalid_request_error/content_filter"},
		{"type only", `{"error":{"type":"authentication_error"}}`, "authentication_error"},
		{"code only", `{"error":{"code":"model_not_found"}}`, "model_not_found"},
		// message is provider prose and frequently quotes the text that tripped
		// the filter, which here is a transcript.
		{"message ignored", `{"error":{"message":"rejected: 你好世界"}}`, ""},
		{"empty error", `{"error":{}}`, ""},
		{"no error field", `{"choices":[]}`, ""},
		{"html page", `<html><body>502 Bad Gateway</body></html>`, ""},
		{"empty", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ProviderCode([]byte(c.in)); got != c.want {
				t.Fatalf("ProviderCode(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}
