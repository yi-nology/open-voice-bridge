package history

import (
	"path/filepath"
	"testing"
	"time"
)

func TestAppendListPersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	store := New(path, 2)
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
	reloaded := New(path, 2)
	if got := reloaded.List(10); len(got) != 2 || got[0].ID != "c" {
		t.Fatalf("reloaded = %+v", got)
	} else if got[1].LatencyMS != 420 {
		t.Fatalf("latency not persisted: %+v", got[1])
	}
}

func TestDeleteAndClear(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	store := New(path, 10)
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
	if reloaded := New(path, 10); len(reloaded.List(10)) != 0 {
		t.Fatal("persisted clear failed")
	}
}

// DOM-14: entry ids must be unique even inside one clock tick — Delete
// removes every entry carrying the id.
func TestHistoryIDsAreUniqueWithinOneTick(t *testing.T) {
	store := New("", 10)
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
