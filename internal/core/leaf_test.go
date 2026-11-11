package core

import (
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
)

// R1: core is the bottom of the graph. It defines the vocabulary every layer
// shares, so importing anything internal here would drag that layer into every
// consumer of a domain type.
func TestCoreImportsNothingInternal(t *testing.T) {
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
			if strings.HasPrefix(path, module) {
				t.Errorf("%s imports %s; core must stay a leaf (R1)", name, path)
			}
		}
	}
}
