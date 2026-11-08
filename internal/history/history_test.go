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
