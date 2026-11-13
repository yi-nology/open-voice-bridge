package phone

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestPhonePageAndAssetsAreServed(t *testing.T) {
	e := newTestEnv(t, ServerConfig{MaxClients: 4})
	for _, route := range []struct{ path, contentType string }{
		{"/phone", "text/html"},
		{"/phone/app.css", "text/css"},
		{"/phone/app.js", "text/javascript"},
		{"/phone/worklet.js", "text/javascript"},
	} {
		resp, err := http.Get(e.http.URL + route.path)
		if err != nil {
			t.Fatalf("GET %s: %v", route.path, err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s = %d, want 200", route.path, resp.StatusCode)
		}
		if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, route.contentType) {
			t.Fatalf("GET %s content type = %q, want %q", route.path, got, route.contentType)
		}
		if len(body) == 0 {
			t.Fatalf("GET %s returned an empty body", route.path)
		}
	}
}

func TestPhonePageAllowsMicrophone(t *testing.T) {
	e := newTestEnv(t, ServerConfig{MaxClients: 4})
	resp, err := http.Get(e.http.URL + "/phone")
	if err != nil {
		t.Fatalf("GET /phone: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	csp := resp.Header.Get("Content-Security-Policy")
	policy := resp.Header.Get("Permissions-Policy")
	if csp == "" || policy == "" {
		t.Fatalf("phone page must set CSP and Permissions-Policy, got %q / %q", csp, policy)
	}
	// The whole reason the phone listener has its own port and headers: the
	// admin UI deliberately denies the microphone, this page must not.
	if strings.Contains(policy, "microphone=()") {
		t.Fatalf("Permissions-Policy must permit the microphone, got %q", policy)
	}
	if !strings.Contains(csp, "default-src 'self'") || strings.Contains(csp, "unsafe-inline") {
		t.Fatalf("phone CSP must be self-only with no inline scripts or styles, got %q", csp)
	}
	if !strings.Contains(csp, "wss:") || !strings.Contains(csp, "ws:") {
		t.Fatalf("phone CSP must allow same-origin WebSocket, got %q", csp)
	}
}

func TestPhonePageCarriesNoSecrets(t *testing.T) {
	e := newTestEnv(t, ServerConfig{MaxClients: 4})
	resp, err := http.Get(e.http.URL + "/phone")
	if err != nil {
		t.Fatalf("GET /phone: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	text := string(body)
	for _, marker := range []string{"token", "ticket", "code"} {
		if strings.Contains(strings.ToLower(strings.Split(text, "<script")[0]), marker+"=") {
			t.Fatalf("phone page markup must not embed a %s value", marker)
		}
	}
}
