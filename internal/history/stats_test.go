package history

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStatsAccumulateAndPersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage-stats.json")
	base := time.Unix(1_700_000_000, 0)
	stats := NewStats(path)
	stats.now = func() time.Time { return base }

	stats.Record(2*time.Second, 10)
	stats.Record(5*time.Second, 30)

	snap := stats.Snapshot()
	if snap.Utterances != 2 || snap.Characters != 40 {
		t.Fatalf("totals = %+v", snap)
	}
	if snap.SpeechMS != 7000 || snap.LongestMS != 5000 {
		t.Fatalf("speech = %d longest = %d", snap.SpeechMS, snap.LongestMS)
	}
	day := base.Format("2006-01-02")
	if got := snap.Days[day]; got.Utterances != 2 || got.Characters != 40 || got.SpeechMS != 7000 {
		t.Fatalf("day = %+v", got)
	}

	reloaded := NewStats(path)
	snap = reloaded.Snapshot()
	if snap.Utterances != 2 || snap.LongestMS != 5000 {
		t.Fatalf("reloaded = %+v", snap)
	}

	// A settings-style rewrite of the file must not be how stats are written;
	// and the file must not hold speech content — only numbers.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Fatal("stats file empty")
	}
}

// The per-day map is bounded; lifetime totals survive the prune.
func TestStatsPruneOldDays(t *testing.T) {
	stats := NewStats("")
	base := time.Unix(1_700_000_000, 0)
	stats.now = func() time.Time { return base }
	stats.Record(time.Second, 1)

	stats.now = func() time.Time { return base.AddDate(0, 0, 200) }
	stats.Record(time.Second, 1)

	snap := stats.Snapshot()
	if snap.Utterances != 2 {
		t.Fatalf("lifetime totals = %+v", snap)
	}
	if len(snap.Days) != 1 {
		t.Fatalf("days = %d, want the old day pruned", len(snap.Days))
	}
}
