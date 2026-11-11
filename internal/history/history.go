// Package history stores local dictation history as JSON lines.
package history

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/oekit/logx"

	"github.com/yi-nology/open-voice-bridge/internal/fsutil"
)

var log = logx.Delegating("history")

type Entry struct {
	ID         string    `json:"id"`
	Text       string    `json:"text"`
	RawText    string    `json:"raw_text,omitempty"`
	Provider   string    `json:"provider,omitempty"`
	Language   string    `json:"language,omitempty"`
	Source     string    `json:"source,omitempty"`      // "remote" | "phone" (DOM-09)
	SourceName string    `json:"source_name,omitempty"` // friendly device name
	Address    string    `json:"address,omitempty"`
	Processed  bool      `json:"processed"`
	Truncated  bool      `json:"truncated,omitempty"` // capture hit its time cap
	LatencyMS  int64     `json:"latency_ms,omitempty"`
	At         time.Time `json:"at"`
}

type Store struct {
	mu           sync.RWMutex
	path         string
	limit        int
	entries      []Entry
	sinceCompact int
	now          func() time.Time
	lastStamp    string
	seq          int
}

func New(path string, limit int) *Store {
	if limit <= 0 {
		limit = 200
	}
	store := &Store{path: path, limit: limit, now: time.Now}
	store.load()
	return store
}

func (s *Store) load() {
	if s.path == "" {
		return
	}
	file, err := os.Open(s.path)
	if err != nil {
		return
	}
	defer func() {
		if err := file.Close(); err != nil {
			log.Debug("close history file", "err", err)
		}
	}()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		var entry Entry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil || entry.ID == "" {
			continue
		}
		s.entries = append(s.entries, entry)
	}
	if len(s.entries) > s.limit {
		s.entries = s.entries[len(s.entries)-s.limit:]
	}
}

// nextIDLocked returns a strictly unique entry id (DOM-14): two appends in
// one clock tick must never collide, because Delete removes every entry
// carrying the id.
func (s *Store) nextIDLocked() string {
	stamp := s.now().Format("20060102-150405.000000000")
	if stamp == s.lastStamp {
		s.seq++
	} else {
		s.lastStamp, s.seq = stamp, 0
	}
	if s.seq == 0 {
		return stamp
	}
	return fmt.Sprintf("%s-%02d", stamp, s.seq)
}

func (s *Store) Append(entry Entry) error {
	s.mu.Lock()
	if entry.ID == "" {
		entry.ID = s.nextIDLocked()
	}
	if entry.At.IsZero() {
		entry.At = s.now()
	}
	defer s.mu.Unlock()
	s.entries = append(s.entries, entry)
	if len(s.entries) > s.limit {
		s.entries = s.entries[len(s.entries)-s.limit:]
	}
	if s.path == "" {
		return nil
	}
	s.sinceCompact++
	if s.sinceCompact >= 25 {
		s.sinceCompact = 0
		return s.rewrite()
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	return fsutil.AppendJSONL(s.path, entry)
}

func (s *Store) List(limit int) []Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if limit <= 0 || limit > len(s.entries) {
		limit = len(s.entries)
	}
	out := make([]Entry, 0, limit)
	for index := len(s.entries) - limit; index < len(s.entries); index++ {
		out = append(out, s.entries[index])
	}
	for left, right := 0, len(out)-1; left < right; left, right = left+1, right-1 {
		out[left], out[right] = out[right], out[left]
	}
	return out
}

func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	filtered := s.entries[:0]
	for _, entry := range s.entries {
		if entry.ID != id {
			filtered = append(filtered, entry)
		}
	}
	s.entries = filtered
	s.sinceCompact = 0
	return s.rewrite()
}

func (s *Store) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = nil
	s.sinceCompact = 0
	return s.rewrite()
}

// rewrite replaces the JSONL file with the in-memory entries in one durable
// step (tmp + fsync + rename), so a crash can never leave the history empty
// (DOM-14).
func (s *Store) rewrite() error {
	if s.path == "" {
		return nil
	}
	return fsutil.RewriteJSONL(s.path, s.entries)
}
