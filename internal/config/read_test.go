package config

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// OPS-03: Read is the diagnostic path. It must report a missing document
// instead of creating one, because doctor runs before the user has configured
// anything and must not leave a half-default file behind.
func TestReadDoesNotCreateTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent.json")
	cfg, err := Read(path)
	if !os.IsNotExist(err) {
		t.Fatalf("err = %v, want a not-exist error", err)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("Read created the file (stat err = %v)", statErr)
	}
	if cfg.Address != PlaceholderAddress {
		t.Fatalf("address = %q, want the placeholder %q", cfg.Address, PlaceholderAddress)
	}
}

// A legacy document is missing whole sections, which Load migrates by
// rewriting it. Read must parse the same values and leave the bytes alone.
func TestReadParsesALegacyDocumentWithoutRewritingIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.json")
	legacy := []byte(`{"address":"11:22:33:44:55:66","asr_model":"mimo-v2.5-asr"}`)
	if err := os.WriteFile(path, legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	read, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(legacy, after) {
		t.Fatalf("Read rewrote the document:\nbefore %s\nafter  %s", legacy, after)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if read.Address != loaded.Address || read.Keymap.Profile != loaded.Keymap.Profile ||
		read.ASR.Provider != loaded.ASR.Provider || read.Phone.Addr != loaded.Phone.Addr {
		t.Fatalf("Read and Load disagree:\nread  %+v\nloaded %+v", read, loaded)
	}
	if read.Keymap.Profile != "coding" {
		t.Fatalf("profile = %q, want the normalized default coding", read.Keymap.Profile)
	}
}

// Load migrates before it validates, so an invalid legacy document is rewritten
// and then rejected. Read must reject it without touching the file.
func TestReadDoesNotWriteAnInvalidDocument(t *testing.T) {
	path := filepath.Join(t.TempDir(), "invalid.json")
	invalid := []byte(`{"address":"not-a-mac"}`)
	if err := os.WriteFile(path, invalid, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(path); err == nil {
		t.Fatal("Read accepted an invalid Bluetooth address")
	} else if !strings.Contains(err.Error(), "invalid Bluetooth address") {
		t.Fatalf("err = %v, want the address rejection", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(invalid, after) {
		t.Fatalf("Read rewrote an invalid document:\nbefore %s\nafter  %s", invalid, after)
	}
}
