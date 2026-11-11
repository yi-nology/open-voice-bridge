// Package mcp grants AI clients read-only access to dictation history through
// a local stdio MCP server ("本地 Agent 访问", SayAll 对标). Access is explicit:
// the user mints one 256-bit token per client, can revoke it at any time, and
// queries are audited without recording speech content.
package mcp

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	oeerrors "github.com/oekit/pkg/errors"

	"github.com/yi-nology/open-voice-bridge/internal/fsutil"
)

// TokenInfo is the displayable record of one client grant. The raw token and
// its hash never leave the store.
type TokenInfo struct {
	ID        string    `json:"id"`
	Label     string    `json:"label"`
	CreatedAt time.Time `json:"created_at"`
	LastUsed  time.Time `json:"last_used,omitempty"`
	Revoked   bool      `json:"revoked,omitempty"`
}

type storedToken struct {
	TokenInfo
	Hash string `json:"hash"`
}

// Store persists client grants as SHA-256 hashes in a 0600 JSON file.
type Store struct {
	mu     sync.Mutex
	path   string
	now    func() time.Time
	tokens map[string]*storedToken
}

func NewStore(path string) *Store {
	store := &Store{
		path:   path,
		now:    time.Now,
		tokens: map[string]*storedToken{},
	}
	store.load()
	return store
}

func (s *Store) load() {
	if s.path == "" {
		return
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		return
	}
	var saved []*storedToken
	if err := json.Unmarshal(data, &saved); err != nil {
		return
	}
	for _, item := range saved {
		if item != nil && item.ID != "" {
			s.tokens[item.ID] = item
		}
	}
}

func (s *Store) persistLocked() error {
	if s.path == "" {
		return nil
	}
	items := make([]*storedToken, 0, len(s.tokens))
	for _, item := range s.tokens {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	data, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	return fsutil.WriteAtomic(s.path, append(data, '\n'), 0o600)
}

// Create mints a grant and returns the raw token exactly once; only its hash
// is stored.
func (s *Store) Create(label string) (id, raw string, err error) {
	raw, err = randomHex(32)
	if err != nil {
		return "", "", err
	}
	suffix, err := randomHex(4)
	if err != nil {
		return "", "", err
	}
	id = "mcp-" + suffix
	entry := &storedToken{
		TokenInfo: TokenInfo{
			ID:        id,
			Label:     label,
			CreatedAt: s.now(),
		},
		Hash: hashToken(raw),
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens[id] = entry
	if err := s.persistLocked(); err != nil {
		delete(s.tokens, id)
		return "", "", err
	}
	return id, raw, nil
}

// List returns the displayable records (never hashes or raw tokens).
func (s *Store) List() []TokenInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]TokenInfo, 0, len(s.tokens))
	for _, item := range s.tokens {
		out = append(out, item.TokenInfo)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}

// Revoke retires a grant; validation fails immediately afterwards.
func (s *Store) Revoke(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.tokens[id]
	if !ok {
		return oeerrors.Errorf("unknown token %q", id)
	}
	if item.Revoked {
		return oeerrors.Errorf("token %q is already revoked", id)
	}
	item.Revoked = true
	return s.persistLocked()
}

// Validate checks a raw token and refreshes LastUsed. Revoked or unknown
// tokens report false (SayAll 撤销即失效).
func (s *Store) Validate(raw string) (TokenInfo, bool) {
	if raw == "" {
		return TokenInfo{}, false
	}
	hash := hashToken(raw)
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, item := range s.tokens {
		if item.Hash == hash && !item.Revoked {
			item.LastUsed = s.now()
			_ = s.persistLocked()
			return item.TokenInfo, true
		}
	}
	return TokenInfo{}, false
}

func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func randomHex(bytes int) (string, error) {
	buf := make([]byte, bytes)
	if _, err := rand.Read(buf); err != nil {
		return "", oeerrors.Wrap(err, "token entropy")
	}
	return hex.EncodeToString(buf), nil
}
