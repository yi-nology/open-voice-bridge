package diagbox

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestSealOpenRoundTrip(t *testing.T) {
	plain := []byte(`{"ok":true,"note":"诊断摘要，已脱敏"}`)
	pack, err := Seal(plain, "correct horse")
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if bytes.Contains(pack, plain) {
		t.Fatal("ciphertext must not contain the plaintext")
	}
	if !bytes.HasPrefix(pack, []byte("OVBDIA1\n")) {
		t.Fatalf("magic = %q", pack[:8])
	}
	out, err := Open(pack, "correct horse")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if !bytes.Equal(out, plain) {
		t.Fatalf("round trip = %q, want %q", out, plain)
	}
}

func TestOpenWithHeaderIterations(t *testing.T) {
	// seal() with a cheap work factor still opens via the header count.
	pack, err := seal([]byte("tiny"), "pw", 1000)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	out, err := Open(pack, "pw")
	if err != nil || string(out) != "tiny" {
		t.Fatalf("open = %q, %v", out, err)
	}
}

func TestWrongPassphraseIsRejected(t *testing.T) {
	pack, err := Seal([]byte("secret notes"), "right")
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if _, err := Open(pack, "wrong"); !errors.Is(err, ErrWrongPassphrase) {
		t.Fatalf("err = %v, want ErrWrongPassphrase", err)
	}
}

func TestTamperedCiphertextIsRejected(t *testing.T) {
	pack, err := Seal([]byte("secret notes"), "right")
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	pack[len(pack)-1] ^= 0xff
	if _, err := Open(pack, "right"); !errors.Is(err, ErrWrongPassphrase) {
		t.Fatalf("err = %v, want ErrWrongPassphrase", err)
	}
}

func TestForeignBytesAreRejected(t *testing.T) {
	for _, pack := range [][]byte{
		[]byte("not a pack at all"),
		make([]byte, 10),
		append([]byte("OVBDIA1\n"), make([]byte, 32)...),
	} {
		if _, err := Open(pack, "pw"); err == nil {
			t.Fatalf("open(%q…) = nil error", pack[:min(8, len(pack))])
		}
	}
}

func TestEmptyPassphraseIsRejected(t *testing.T) {
	if _, err := Seal([]byte("x"), ""); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("err = %v, want empty passphrase", err)
	}
}
