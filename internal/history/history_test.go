package history

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAppendListPersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	store := New(path, 2, Policy{})
	now := time.Now()
	if err := store.Append(Entry{ID: "a", Text: "one", At: now}); err != nil {
		t.Fatal(err)
	}
	if err := store.Append(Entry{ID: "b", Text: "two", LatencyMS: 420, At: now.Add(time.Second)}); err != nil {
		t.Fatal(err)
	}
	if err := store.Append(Entry{ID: "c", Text: "three", At: now.Add(2 * time.Second)}); err != nil {
		t.Fatal(err)
	}
	list := store.List(10)
	if len(list) != 2 || list[0].ID != "c" || list[1].ID != "b" {
		t.Fatalf("list = %+v", list)
	}
	reloaded := New(path, 2, Policy{})
	if got := reloaded.List(10); len(got) != 2 || got[0].ID != "c" {
		t.Fatalf("reloaded = %+v", got)
	} else if got[1].LatencyMS != 420 {
		t.Fatalf("latency not persisted: %+v", got[1])
	}
}

func TestDeleteAndClear(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	store := New(path, 10, Policy{})
	for _, id := range []string{"a", "b", "c"} {
		if err := store.Append(Entry{ID: id, Text: id}); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Delete("b"); err != nil {
		t.Fatal(err)
	}
	if list := store.List(10); len(list) != 2 || list[0].ID != "c" || list[1].ID != "a" {
		t.Fatalf("after delete = %+v", list)
	}
	if err := store.Clear(); err != nil {
		t.Fatal(err)
	}
	if list := store.List(10); len(list) != 0 {
		t.Fatalf("after clear = %+v", list)
	}
	if reloaded := New(path, 10, Policy{}); len(reloaded.List(10)) != 0 {
		t.Fatal("persisted clear failed")
	}
}

// DOM-14: entry ids must be unique even inside one clock tick — Delete
// removes every entry carrying the id.
func TestHistoryIDsAreUniqueWithinOneTick(t *testing.T) {
	store := New("", 10, Policy{})
	frozen := time.Unix(1_700_000_000, 0)
	store.now = func() time.Time { return frozen }

	if err := store.Append(Entry{Text: "one"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Append(Entry{Text: "two"}); err != nil {
		t.Fatal(err)
	}
	entries := store.List(0)
	if len(entries) != 2 {
		t.Fatalf("entries = %d", len(entries))
	}
	if entries[0].ID == entries[1].ID {
		t.Fatalf("ids collide in one tick: %q", entries[0].ID)
	}
	// List is newest-first: entries[0] is "two", entries[1] is "one".
	if err := store.Delete(entries[0].ID); err != nil {
		t.Fatal(err)
	}
	if remaining := store.List(0); len(remaining) != 1 || remaining[0].Text != "one" {
		t.Fatalf("delete must remove exactly one entry, left %+v", remaining)
	}
}

// 波 3: the policy bounds what the store keeps — rows age out of the
// retention window, and redacted addresses never reach the file.
func TestPolicyRetentionAndRedaction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	store := New(path, 200, Policy{RetentionDays: 30, RedactAddresses: true})
	if err := store.Append(Entry{ID: "fresh", Text: "你好", Address: "AA:BB:CC:DD:EE:FF", At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := store.Append(Entry{ID: "ancient", Text: "旧话", At: time.Now().AddDate(0, 0, -90)}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	if strings.Contains(body, "AA:BB:CC:DD:EE:FF") {
		t.Fatalf("file carries the full address: %s", body)
	}
	if !strings.Contains(body, "AA:BB:CC:**:**:FF") {
		t.Fatalf("file should carry the redacted address: %s", body)
	}
	reloaded := New(path, 200, Policy{RetentionDays: 30})
	if entries := reloaded.List(10); len(entries) != 1 || entries[0].ID != "fresh" {
		t.Fatalf("entries = %+v, want only the fresh row", entries)
	}
}

// TestSetPolicyAppliesLive pins the 波 3 policy contract: swapping the policy
// on a running store must trim the retained rows and re-bind redaction right
// away — no restart, no "takes effect on next load" limbo (the admin API says
// "applies it live" and the UI control must be able to believe it).
func TestSetPolicyAppliesLive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	store := New(path, 200, Policy{})
	now := time.Now()
	if err := store.Append(Entry{ID: "old", Text: "旧话", At: now.AddDate(0, 0, -90)}); err != nil {
		t.Fatal(err)
	}
	if err := store.Append(Entry{ID: "fresh", Text: "新话", At: now}); err != nil {
		t.Fatal(err)
	}
	store.SetPolicy(Policy{RetentionDays: 30, RedactAddresses: true})
	if entries := store.List(10); len(entries) != 1 || entries[0].ID != "fresh" {
		t.Fatalf("retention did not trim live: %+v", entries)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "old") {
		t.Fatalf("file still carries the trimmed row: %s", data)
	}
	if err := store.Append(Entry{ID: "later", Text: "后话", Address: "AA:BB:CC:DD:EE:FF", At: now}); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "AA:BB:CC:DD:EE:FF") {
		t.Fatalf("redaction did not follow the live policy: %s", data)
	}
}
