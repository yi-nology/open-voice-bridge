package phone

import "testing"

func TestPairingBasePortAndScheme(t *testing.T) {
	scheme, _, port := PairingBase("auto", "0.0.0.0:8788")
	if scheme != "https" {
		t.Fatalf("auto scheme = %q, want https", scheme)
	}
	if port != "8788" {
		t.Fatalf("port = %q, want 8788", port)
	}
	scheme, _, _ = PairingBase("off", "127.0.0.1:8788")
	if scheme != "http" {
		t.Fatalf("off scheme = %q, want http", scheme)
	}
}
