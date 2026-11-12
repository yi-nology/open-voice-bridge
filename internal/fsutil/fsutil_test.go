package fsutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// R7: every durable write goes through this package — tmp+fsync+rename,
// never a bare write that a crash can truncate.

func TestWriteAtomicReplacesContents(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	if err := os.WriteFile(path, []byte("old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteAtomic(path, []byte("new\n"), 0o600); err != nil {
		t.Fatalf("WriteAtomic: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new\n" {
		t.Fatalf("content = %q, want %q", data, "new\n")
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("tmp file left behind, stat err = %v", err)
	}
}

func TestWriteAtomicCreatesFileWithPerm(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fresh.json")
	if err := WriteAtomic(path, []byte("x\n"), 0o644); err != nil {
		t.Fatalf("WriteAtomic: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Fatalf("perm = %04o, want 0644", info.Mode().Perm())
	}
}

func TestWriteAtomicLeavesTargetAloneWhenTmpFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	if err := os.WriteFile(path, []byte("keep\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A directory in the tmp path makes the tmp write fail before rename.
	if err := os.Mkdir(path+".tmp", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := WriteAtomic(path, []byte("new\n"), 0o600); err == nil {
		t.Fatal("WriteAtomic succeeded despite unusable tmp path")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "keep\n" {
		t.Fatalf("content = %q, want the original %q", data, "keep\n")
	}
}

// A failed replace must not leave a stray tmp beside the target: the next
// writer inherits it, and anything globbing the directory sees a state file
// that was never committed (R7).
func TestWriteAtomicCleansTmpWhenRenameFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	// A non-empty directory at the target fails the rename after the tmp file
	// has been written and synced.
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "occupied"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteAtomic(path, []byte("new\n"), 0o600); err == nil {
		t.Fatal("WriteAtomic succeeded despite an unreplaceable target")
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("tmp file left behind, stat err = %v", err)
	}
}

func TestAppendJSONLAddsOneLinePerEntry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "log.jsonl")
	if err := AppendJSONL(path, map[string]any{"n": 1}); err != nil {
		t.Fatalf("first append: %v", err)
	}
	if err := AppendJSONL(path, map[string]any{"n": 2}); err != nil {
		t.Fatalf("second append: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("lines = %d (%q), want 2", len(lines), data)
	}
	for _, line := range lines {
		if !strings.HasPrefix(line, "{") || !strings.HasSuffix(line, "}") {
			t.Fatalf("line %q is not a JSON object", line)
		}
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("perm = %04o, want 0600", info.Mode().Perm())
	}
}

func TestRewriteJSONLReplacesAllLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "log.jsonl")
	if err := AppendJSONL(path, map[string]any{"n": 1}); err != nil {
		t.Fatal(err)
	}
	if err := AppendJSONL(path, map[string]any{"n": 2}); err != nil {
		t.Fatal(err)
	}
	if err := RewriteJSONL(path, []map[string]any{{"n": 9}}); err != nil {
		t.Fatalf("RewriteJSONL: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimRight(string(data), "\n"); got != `{"n":9}` {
		t.Fatalf("content = %q, want %q", got, `{"n":9}`)
	}
}

// R7: persistence goes through this package only. A second tmp+rename is how
// the history rewrite got a Sync while seven other writers did not (DOM-14).
func TestNoOtherPackageRollsItsOwnAtomicWrite(t *testing.T) {
	root := filepath.Join("..", "..")
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "node_modules", "fsutil":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(data), "os.Rename(") {
			t.Errorf("%s renames files itself; use internal/fsutil instead (R7)", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
}
