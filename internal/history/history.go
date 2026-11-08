// Package history stores local dictation history as JSON lines.
package history

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/oekit/logx"
)

var log = logx.Delegating("history")

type Entry struct {
	ID        string    `json:"id"`
	Text      string    `json:"text"`
	RawText   string    `json:"raw_text,omitempty"`
	Provider  string    `json:"provider,omitempty"`
	Language  string    `json:"language,omitempty"`
	Address   string    `json:"address,omitempty"`
	Processed bool      `json:"processed"`
	LatencyMS int64     `json:"latency_ms,omitempty"`
	At        time.Time `json:"at"`
}

type Store struct {
	mu           sync.RWMutex
	path         string
	limit        int
	entries      []Entry
	sinceCompact int
}

func New(path string, limit int) *Store {
	if limit <= 0 {
		limit = 200
	}
	store := &Store{path: path, limit: limit}
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

func (s *Store) Append(entry Entry) error {
	if entry.ID == "" {
		entry.ID = time.Now().Format("20060102-150405.000000000")
	}
	if entry.At.IsZero() {
		entry.At = time.Now()
	}
	s.mu.Lock()
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
	file, err := os.OpenFile(s.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		if err := file.Close(); err != nil {
			log.Debug("close history file", "err", err)
		}
	}()
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	if _, err := file.Write(append(data, '\n')); err != nil {
		return err
	}
	return file.Sync()
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

func (s *Store) rewrite() error {
	if s.path == "" {
		return nil
	}
	tmp := s.path + ".tmp"
	file, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	writer := bufio.NewWriter(file)
	for _, entry := range s.entries {
		data, err := json.Marshal(entry)
		if err != nil {
			continue
		}
		if _, err := writer.Write(append(data, '\n')); err != nil {
			file.Close()
			return err
		}
	}
	if err := writer.Flush(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
