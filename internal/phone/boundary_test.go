package phone

import (
	"go/parser"
	"go/token"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
)

// R4: the phone listener is a transport. Embedding the presentation tree here
// is what made a LAN-facing package depend on web/, so the assets are injected
// by the composition root instead (ARC-04).
func TestPhoneDoesNotImportTheWebTree(t *testing.T) {
	const banned = "github.com/yi-nology/open-voice-bridge/web"
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	fset := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, spec := range file.Imports {
			path, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				t.Fatalf("%s: bad import %s: %v", name, spec.Path.Value, err)
			}
			if path == banned {
				t.Errorf("%s imports %s; inject the asset tree instead (R4)", name, path)
			}
		}
	}
}

// The admin boundary learned in SEC-06 that an empty Origin is not evidence.
// The phone boundary is the one exposed to the LAN, so the same rule has to
// reach it: a foreign page must not be able to spend a pairing code.
func TestPhoneRejectsCrossOriginPairing(t *testing.T) {
	e := newTestEnv(t, ServerConfig{TLSMode: "off", MaxClients: 4})
	code, _ := e.store.GeneratePairCode()

	status, _ := postJSON(t, e.http.URL+"/api/phone/pair", pairRequest{Code: code, Name: "Phone"},
		map[string]string{"Origin": "http://evil.example", "Sec-Fetch-Site": "cross-site"})
	if status != http.StatusForbidden {
		t.Fatalf("cross-origin pair = %d, want 403", status)
	}

	status, _ = postJSON(t, e.http.URL+"/api/phone/pair", pairRequest{Code: code, Name: "Phone"},
		map[string]string{"Sec-Fetch-Site": "none"})
	if status != http.StatusForbidden {
		t.Fatalf("pair without origin evidence = %d, want 403", status)
	}

	// The code was not consumed by either rejected attempt.
	status, _ = postJSON(t, e.http.URL+"/api/phone/pair", pairRequest{Code: code, Name: "Phone"}, nil)
	if status != http.StatusOK {
		t.Fatalf("same-origin pair = %d, want 200", status)
	}
}

func TestPhoneRejectsCrossOriginTicket(t *testing.T) {
	e := newTestEnv(t, ServerConfig{TLSMode: "off", MaxClients: 4})
	token, _, _ := pairAndTicket(t, e, "Phone")
	status, _ := postJSON(t, e.http.URL+"/api/phone/ticket", map[string]any{},
		map[string]string{"Authorization": "Bearer " + token, "Origin": "http://evil.example"})
	if status != http.StatusForbidden {
		t.Fatalf("cross-origin ticket = %d, want 403", status)
	}
}
