package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"
)

func runtimeWindows() bool { return runtime.GOOS == "windows" }

// TestRevokePersistFailureKeepsGrantValid pins issue #8: a revoke whose disk
// write fails rolls the in-memory flag back (same contract as Create), so the
// store never disagrees with its own persisted file — nothing to resurrect on
// restart, and the caller gets a loud failure to retry.
func TestRevokePersistFailureKeepsGrantValid(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(filepath.Join(dir, "tokens.json"))
	id, raw, err := store.Create("client")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	if revokeErr := store.Revoke(id); revokeErr == nil {
		t.Fatal("revoke must fail loudly when the persist fails")
	}
	if _, ok := store.Validate(raw); !ok {
		t.Fatal("a failed revoke must leave the grant valid (rolled back)")
	}
	reloaded := NewStore(filepath.Join(dir, "tokens.json"))
	if _, ok := reloaded.Validate(raw); !ok {
		t.Fatal("after a failed revoke the grant must still validate from disk")
	}

	_ = os.Chmod(dir, 0o700)
	if err := store.Revoke(id); err != nil {
		t.Fatalf("revoke retry: %v", err)
	}
	if _, ok := store.Validate(raw); ok {
		t.Fatal("a successful revoke must kill the grant")
	}
	reloaded = NewStore(filepath.Join(dir, "tokens.json"))
	if _, ok := reloaded.Validate(raw); ok {
		t.Fatal("a successfully revoked grant must stay dead after restart")
	}
}

// TestValidateDoesNotHoldLockDuringPersist pins issue #13 (R8/I3/CON-08): the
// per-auth LastUsed flush must run outside s.mu. A FIFO with no reader parks
// the write inside the file open, and List() — pure memory work — must stay
// responsive while the disk is stuck.
func TestValidateDoesNotHoldLockDuringPersist(t *testing.T) {
	if runtimeWindows() {
		t.Skip("fifo")
	}
	dir := t.TempDir()
	store := NewStore(filepath.Join(dir, "tokens.json"))
	_, raw, err := store.Create("client")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	store.path = filepath.Join(dir, "blocked.json")
	fifo := store.path + ".tmp"
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatalf("mkfifo: %v", err)
	}
	done := make(chan struct{})
	go func() {
		store.Validate(raw)
		close(done)
	}()

	// Give Validate a moment to reach the FIFO open, then prove List — pure
	// memory work — stays responsive while the write is parked.
	time.Sleep(100 * time.Millisecond)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got := make(chan struct{}, 1)
		go func() { store.List(); got <- struct{}{} }()
		select {
		case <-got:
		case <-time.After(700 * time.Millisecond):
			t.Fatal("List blocked during Validate persist: the disk write runs under the store lock")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Unblock the writer (a reader completes the FIFO open). The write ends in
	// EINVAL (fsync on a pipe) — what this test guards is the lock discipline,
	// proved above, not the write's outcome on a pipe.
	reader, err := os.Open(fifo)
	if err != nil {
		t.Fatalf("open fifo: %v", err)
	}
	defer reader.Close()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("validate never finished")
	}
}

// TestValidateFlushThrottlesLastUsed pins the hot-path half of issue #13:
// authentication runs on every MCP request, so LastUsed reaches the disk at
// most once per flush interval instead of paying a whole-file rewrite plus
// fsync per call. The file's own last_used is the write counter.
func TestValidateFlushThrottlesLastUsed(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(filepath.Join(dir, "tokens.json"))
	id, raw, err := store.Create("client")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	base := time.Unix(1_700_000_000, 0)
	clock := base
	store.now = func() time.Time { return clock }

	lastUsedOnDisk := func() time.Time {
		t.Helper()
		data, err := os.ReadFile(store.path)
		if err != nil {
			t.Fatalf("read persisted file: %v", err)
		}
		var saved []*storedToken
		if err := json.Unmarshal(data, &saved); err != nil {
			t.Fatalf("parse persisted file: %v", err)
		}
		for _, item := range saved {
			if item != nil && item.ID == id {
				return item.LastUsed
			}
		}
		t.Fatal("token missing from the persisted file")
		return time.Time{}
	}

	store.Validate(raw) // first authentication in the interval: one flush
	if got := lastUsedOnDisk(); !got.Equal(base) {
		t.Fatalf("first flush missing: disk last_used = %v want %v", got, base)
	}
	clock = base.Add(10 * time.Second)
	store.Validate(raw) // same interval: in-memory only
	if got := lastUsedOnDisk(); !got.Equal(base) {
		t.Fatalf("throttle leaked a write inside the interval: disk last_used = %v", got)
	}
	clock = base.Add(61 * time.Second)
	store.Validate(raw) // interval elapsed: flushed again
	if got := lastUsedOnDisk(); !got.Equal(clock) {
		t.Fatalf("interval flush missing: disk last_used = %v want %v", got, clock)
	}
}
