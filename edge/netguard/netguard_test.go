package netguard

import (
	"net/http"
	"strings"
	"testing"
)

func TestSameOrigin(t *testing.T) {
	const host = "127.0.0.1:8787"
	cases := []struct {
		name   string
		method string
		origin string
		site   string
		want   bool
	}{
		{"get needs no evidence", "GET", "", "", true},
		{"matching origin", "POST", "http://" + host, "same-origin", true},
		{"matching origin without fetch metadata", "POST", "http://" + host, "", true},
		// I9: an empty Origin is not same-origin evidence. Without it a bare
		// request (curl, a foreign process) reaches the POST endpoints.
		{"bare post", "POST", "", "", false},
		{"empty origin with site none", "POST", "", "none", false},
		{"empty origin with same-origin site", "POST", "", "same-origin", true},
		{"foreign origin", "POST", "http://evil.example", "cross-site", false},
		{"foreign origin claiming same-origin", "POST", "http://evil.example", "same-origin", false},
		{"matching origin but cross-site metadata", "POST", "http://" + host, "cross-site", false},
		{"unparseable origin", "POST", "://nope", "", false},
		{"put is unsafe too", "PUT", "", "", false},
		{"patch is unsafe too", "PATCH", "", "", false},
		{"delete is unsafe too", "DELETE", "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SameOrigin(tc.method, host, tc.origin, tc.site); got != tc.want {
				t.Fatalf("SameOrigin(%q, %q, %q, %q) = %v, want %v",
					tc.method, host, tc.origin, tc.site, got, tc.want)
			}
		})
	}
}

func TestIsLoopback(t *testing.T) {
	for _, tc := range []struct {
		host string
		want bool
	}{
		{"127.0.0.1:8787", true},
		{"localhost:8787", true},
		{"[::1]:8787", true},
		{"127.0.0.1", true},
		{"localhost", true},
		{"::1", true},
		{"192.168.1.20:8787", false},
		{"0.0.0.0:8787", false},
		{"evil.example:8787", false},
		{"", false},
	} {
		if got := IsLoopback(tc.host); got != tc.want {
			t.Errorf("IsLoopback(%q) = %v, want %v", tc.host, got, tc.want)
		}
	}
}

func TestWriteHeadersSetsTheSharedGuards(t *testing.T) {
	for _, policy := range []Policy{Admin, Phone} {
		header := http.Header{}
		WriteHeaders(policy, header.Set)
		for key, want := range map[string]string{
			"X-Content-Type-Options":  "nosniff",
			"Referrer-Policy":         "no-referrer",
			"Content-Security-Policy": policy.ContentSecurity,
			"Permissions-Policy":      policy.Permissions,
		} {
			if got := header.Get(key); got != want {
				t.Errorf("%s = %q, want %q", key, got, want)
			}
		}
	}
}

// The two policies are deliberately not one: the admin page denies the
// microphone, the phone page must allow it for getUserMedia, and WebSockets
// need ws:/wss: spelled out because CSP treats them as separate schemes.
func TestPoliciesDifferOnlyWhereTheyMust(t *testing.T) {
	if !strings.Contains(Admin.Permissions, "microphone=()") {
		t.Errorf("admin must deny the microphone, got %q", Admin.Permissions)
	}
	if !strings.Contains(Phone.Permissions, "microphone=(self)") {
		t.Errorf("phone must allow the microphone, got %q", Phone.Permissions)
	}
	if strings.Contains(Phone.ContentSecurity, "unsafe-inline") {
		t.Errorf("phone CSP must not allow inline scripts or styles, got %q", Phone.ContentSecurity)
	}
	for _, want := range []string{"ws:", "wss:", "frame-ancestors 'none'"} {
		if !strings.Contains(Phone.ContentSecurity, want) {
			t.Errorf("phone CSP missing %q: %q", want, Phone.ContentSecurity)
		}
	}
	if strings.Contains(Admin.ContentSecurity, "ws:") {
		t.Errorf("admin CSP must not open a WebSocket scheme, got %q", Admin.ContentSecurity)
	}
}
