package history

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// UsageStats is the cumulative dictation record ("使用统计"). It lives in its
// own file, never inside the settings: a settings rewrite must not be able to
// wipe the user's numbers, and the history JSONL only keeps the last 200
// utterances, which is not a lifetime record.
type UsageStats struct {
	Utterances int64              `json:"utterances"`
	Characters int64              `json:"characters"`
	SpeechMS   int64              `json:"speech_ms"`
	LongestMS  int64              `json:"longest_ms"`
	Days       map[string]DayStat `json:"days"`
}

// DayStat is one day of dictation, keyed "YYYY-MM-DD" (local time).
type DayStat struct {
	Utterances int   `json:"utterances"`
	Characters int   `json:"characters"`
	SpeechMS   int64 `json:"speech_ms"`
}

// statsDayLimit bounds the per-day map (~half a year), so the file cannot grow
// without limit on a machine that talks every day.
const statsDayLimit = 180

// Stats accumulates UsageStats and persists them atomically after each record.
type Stats struct {
	mu   sync.Mutex
	path string
	now  func() time.Time
	cur  UsageStats
}

func NewStats(path string) *Stats {
	s := &Stats{path: path, now: time.Now}
	s.load()
	return s
}

func (s *Stats) load() {
	if s.path == "" {
		return
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		return
	}
	var saved UsageStats
	if err := json.Unmarshal(data, &saved); err != nil {
		log.Warn("parse usage stats", "err", err)
		return
	}
	if saved.Days == nil {
		saved.Days = map[string]DayStat{}
	}
	s.cur = saved
}

// Record adds one finished utterance to the cumulative totals.
func (s *Stats) Record(speech time.Duration, chars int) {
	ms := speech.Milliseconds()
	if ms < 0 {
		ms = 0
	}
	day := s.now().Format("2006-01-02")
	s.mu.Lock()
	s.cur.Utterances++
	s.cur.Characters += int64(chars)
	s.cur.SpeechMS += ms
	if ms > s.cur.LongestMS {
		s.cur.LongestMS = ms
	}
	if s.cur.Days == nil {
		s.cur.Days = map[string]DayStat{}
	}
	stat := s.cur.Days[day]
	stat.Utterances++
	stat.Characters += chars
	stat.SpeechMS += ms
	s.cur.Days[day] = stat
	s.pruneDaysLocked()
	saved := s.snapshotLocked()
	path := s.path
	s.mu.Unlock()

	if path == "" {
		return
	}
	data, err := json.MarshalIndent(saved, "", "  ")
	if err != nil {
		log.Warn("marshal usage stats", "err", err)
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		log.Warn("create stats dir", "err", err)
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		log.Warn("write usage stats", "err", err)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		log.Warn("replace usage stats", "err", err)
	}
}

// Snapshot returns a copy safe to hand to callers without the lock.
func (s *Stats) Snapshot() UsageStats {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshotLocked()
}

func (s *Stats) snapshotLocked() UsageStats {
	out := s.cur
	out.Days = make(map[string]DayStat, len(s.cur.Days))
	for day, stat := range s.cur.Days {
		out.Days[day] = stat
	}
	return out
}

// pruneDaysLocked drops days older than the window ("YYYY-MM-DD" compares
// chronologically); lifetime totals survive.
func (s *Stats) pruneDaysLocked() {
	cutoff := s.now().AddDate(0, 0, -statsDayLimit).Format("2006-01-02")
	for day := range s.cur.Days {
		if day < cutoff {
			delete(s.cur.Days, day)
		}
	}
}
