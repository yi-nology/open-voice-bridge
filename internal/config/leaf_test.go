package config

import (
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
)

// R1: config is a leaf. It owns the document shape and the structural rules;
// anything that needs a subsystem's vocabulary lives in internal/appconfig.
// Importing a subsystem here is what made every load pull in the ASR catalogue,
// the LLM presets and the keymap codes (ARC-03).
func TestConfigImportsNoSubsystem(t *testing.T) {
	const module = "github.com/yi-nology/open-voice-bridge/"
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
			if !strings.HasPrefix(path, module+"internal/") {
				continue
			}
			// fsutil and core are leaves themselves: neither imports a
			// subsystem, so depending on them keeps config loadable alone.
			if path == module+"internal/fsutil" || path == module+"internal/core" {
				continue
			}
			t.Errorf("%s imports %s; config must stay a leaf (R1)", name, path)
		}
	}
}
