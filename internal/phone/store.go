// Package phone implements the standalone phone input listener: pairing,
// bearer tokens, one-time WebSocket tickets, and the virtual-key channel.
package phone

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"os"
	"path/filepath"
	"sync"
	"time"

	oeerrors "github.com/oekit/pkg/errors"
)

// Config tunes the pairing and ticket lifecycle.
type Config struct {
	PairCodeTTL        time.Duration
	TicketTTL          time.Duration
	MaxAttemptsPerCode int
	MaxAttemptsPerIP   int
	MaxDevices         int
}

// DefaultConfig returns sane bounds; callers override from config.Config.
func DefaultConfig() Config {
	return Config{
		PairCodeTTL:        5 * time.Minute,
		TicketTTL:          60 * time.Second,
		MaxAttemptsPerCode: 6,
		MaxAttemptsPerIP:   10,
		MaxDevices:         16,
	}
}

// Device is a paired phone. The raw token is never stored—only its SHA-256.
type Device struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	LastSeen  time.Time `json:"last_seen"`

	tokenHash string
}

type persistedDevice struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	TokenHash string    `json:"token_hash"`
	CreatedAt time.Time `json:"created_at"`
	LastSeen  time.Time `json:"last_seen"`
}

type pairCode struct {
	value    string
	expires  time.Time
	attempts int
}

type ticket struct {
	deviceID string
	expires  time.Time
}

type ipWindow struct {
	count   int
	resetAt time.Time
}

// Pre-authentication tables are bounded (SEC-07). Tickets live ~60 s and
// rate-limit windows ~1 min, so a sweep keeps them small; the caps are the
// backstop for a flood that stays inside the TTL.
const (
	maxTicketEntries = 256
	maxIPWindows     = 512
)

// Store owns pairing codes, device tokens, and WebSocket tickets. It is safe
// for concurrent use and persists only device/token records (never codes or
// tickets, which are ephemeral by design).
type Store struct {
	mu        sync.Mutex
	persistMu sync.Mutex // serialises disk writes; never held together with s.mu
	path      string
	cfg       Config
	now       func() time.Time
	codes     map[string]*pairCode // ephemeral code -> record (keyed by code)
	tokens    map[string]string    // tokenHash hex -> deviceID
	devices   map[string]*Device
	tickets   map[string]ticket
	ip        map[string]*ipWindow
}

// NewStore loads any previously paired devices from path (0600 JSON). A missing
// file is not an error; the first persist creates it.
func NewStore(path string, cfg Config) (*Store, error) {
	if cfg.PairCodeTTL <= 0 {
		cfg.PairCodeTTL = DefaultConfig().PairCodeTTL
	}
	if cfg.TicketTTL <= 0 {
		cfg.TicketTTL = DefaultConfig().TicketTTL
	}
	if cfg.MaxAttemptsPerCode <= 0 {
		cfg.MaxAttemptsPerCode = DefaultConfig().MaxAttemptsPerCode
	}
	if cfg.MaxAttemptsPerIP <= 0 {
		cfg.MaxAttemptsPerIP = DefaultConfig().MaxAttemptsPerIP
	}
	if cfg.MaxDevices <= 0 {
		cfg.MaxDevices = DefaultConfig().MaxDevices
	}
	s := &Store{
		path:    path,
		cfg:     cfg,
		now:     time.Now,
		codes:   map[string]*pairCode{},
		tokens:  map[string]string{},
		devices: map[string]*Device{},
		tickets: map[string]ticket{},
		ip:      map[string]*ipWindow{},
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) load() error {
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return oeerrors.Wrap(err, "load phone tokens")
	}
	var items []persistedDevice
	if err := json.Unmarshal(data, &items); err != nil {
		return oeerrors.Wrap(err, "parse phone tokens")
	}
	for _, item := range items {
		if item.TokenHash == "" || item.ID == "" {
			continue
		}
		s.devices[item.ID] = &Device{
			ID:        item.ID,
			Name:      item.Name,
			CreatedAt: item.CreatedAt,
			LastSeen:  item.LastSeen,
			tokenHash: item.TokenHash,
		}
		s.tokens[item.TokenHash] = item.ID
	}
	return nil
}

// snapshotLocked copies the persisted view of the device set. Callers hold
// s.mu only for this copy — the disk write happens in persistFlush (R8).
func (s *Store) snapshotLocked() []persistedDevice {
	items := make([]persistedDevice, 0, len(s.devices))
	for _, device := range s.devices {
		items = append(items, persistedDevice{
			ID:        device.ID,
			Name:      device.Name,
			TokenHash: device.tokenHash,
			CreatedAt: device.CreatedAt,
			LastSeen:  device.LastSeen,
		})
	}
	return items
}

// persistFlush writes the device set to disk. It must be called without s.mu
// held: the write is serialised by persistMu so concurrent commits cannot
// interleave, and a slow disk cannot freeze Authenticate (CON-08).
func (s *Store) persistFlush(items []persistedDevice) error {
	s.persistMu.Lock()
	defer s.persistMu.Unlock()
	data, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// sweepLocked drops expired tickets, rate-limit windows and pairing codes so
// pre-auth tables cannot grow without bound (SEC-07).
func (s *Store) sweepLocked() {
	now := s.now()
	for value, t := range s.tickets {
		if now.After(t.expires) {
			delete(s.tickets, value)
		}
	}
	for ip, window := range s.ip {
		if now.After(window.resetAt) {
			delete(s.ip, ip)
		}
	}
	for value, pc := range s.codes {
		if now.After(pc.expires) {
			delete(s.codes, value)
		}
	}
}

// evictOldestTicketLocked drops the ticket closest to expiry to make room for
// one new entry. Oldest-first keeps the freshest handshakes alive.
func (s *Store) evictOldestTicketLocked() {
	oldest := ""
	var oldestExp time.Time
	for value, t := range s.tickets {
		if oldest == "" || t.expires.Before(oldestExp) {
			oldest, oldestExp = value, t.expires
		}
	}
	if oldest != "" {
		delete(s.tickets, oldest)
	}
}

// evictOldestWindowLocked drops the rate-limit window closest to reset, the
// least protective entry to give up under pressure.
func (s *Store) evictOldestWindowLocked() {
	oldest := ""
	var oldestReset time.Time
	for ip, window := range s.ip {
		if oldest == "" || window.resetAt.Before(oldestReset) {
			oldest, oldestReset = ip, window.resetAt
		}
	}
	if oldest != "" {
		delete(s.ip, oldest)
	}
}

// GeneratePairCode invalidates any prior code and returns a fresh 6-digit code
// with its expiry. The code is the pairing secret; it is never persisted.
func (s *Store) GeneratePairCode() (string, time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	code := randomDigits(6)
	expires := s.now().Add(s.cfg.PairCodeTTL)
	s.codes = map[string]*pairCode{code: {value: code, expires: expires}}
	return code, expires
}

// Pair redeems a pairing code for a long-lived bearer token. The raw token is
// returned exactly once; the store keeps only its SHA-256 hash.
func (s *Store) Pair(code, name, ip string) (deviceID, token string, err error) {
	s.mu.Lock()
	s.sweepLocked()
	if !s.allowIPLocked(ip) {
		s.mu.Unlock()
		return "", "", oeerrors.New("too many pairing attempts from this address")
	}
	matched, ok := s.matchCodeLocked(code)
	if !ok {
		s.mu.Unlock()
		return "", "", oeerrors.New("invalid or expired pairing code")
	}
	if len(s.devices) >= s.cfg.MaxDevices {
		s.mu.Unlock()
		return "", "", oeerrors.New("maximum paired devices reached")
	}
	rawToken := randomHex(32)
	hash := hashToken(rawToken)
	deviceID = "phone-" + randomHex(4)
	now := s.now()
	s.devices[deviceID] = &Device{
		ID:        deviceID,
		Name:      name,
		CreatedAt: now,
		LastSeen:  now,
		tokenHash: hash,
	}
	s.tokens[hash] = deviceID
	delete(s.codes, matched)
	items := s.snapshotLocked()
	s.mu.Unlock()
	if err := s.persistFlush(items); err != nil {
		s.mu.Lock()
		delete(s.devices, deviceID)
		delete(s.tokens, hash)
		s.mu.Unlock()
		return "", "", oeerrors.Wrap(err, "persist phone device")
	}
	return deviceID, rawToken, nil
}

// matchCodeLocked returns the active code if input matches it (constant time),
// enforcing expiry and per-code attempt limits.
func (s *Store) matchCodeLocked(input string) (string, bool) {
	if len(s.codes) == 0 {
		return "", false
	}
	now := s.now()
	for value, pc := range s.codes {
		if now.After(pc.expires) {
			delete(s.codes, value)
			continue
		}
		if subtle.ConstantTimeCompare([]byte(input), []byte(pc.value)) == 1 {
			return value, true
		}
		pc.attempts++
		if pc.attempts >= s.cfg.MaxAttemptsPerCode {
			delete(s.codes, value)
		}
	}
	return "", false
}

func (s *Store) allowIPLocked(ip string) bool {
	if ip == "" {
		return true
	}
	now := s.now()
	window, ok := s.ip[ip]
	if !ok || now.After(window.resetAt) {
		if !ok && len(s.ip) >= maxIPWindows {
			s.evictOldestWindowLocked()
		}
		s.ip[ip] = &ipWindow{count: 1, resetAt: now.Add(time.Minute)}
		return true
	}
	window.count++
	return window.count <= s.cfg.MaxAttemptsPerIP
}

// Authenticate maps a bearer token to a device id and refreshes LastSeen.
func (s *Store) Authenticate(token string) (string, bool) {
	hash := hashToken(token)
	s.mu.Lock()
	defer s.mu.Unlock()
	deviceID, ok := s.tokens[hash]
	if !ok {
		return "", false
	}
	if device, exists := s.devices[deviceID]; exists {
		device.LastSeen = s.now()
	}
	return deviceID, true
}

// IssueTicket creates a short-lived, single-use WebSocket ticket for a device.
func (s *Store) IssueTicket(deviceID string) (string, time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked()
	if _, ok := s.devices[deviceID]; !ok {
		return "", time.Time{}, oeerrors.Errorf("unknown device %q", deviceID)
	}
	if len(s.tickets) >= maxTicketEntries {
		s.evictOldestTicketLocked()
	}
	value := randomHex(16)
	expires := s.now().Add(s.cfg.TicketTTL)
	s.tickets[value] = ticket{deviceID: deviceID, expires: expires}
	return value, expires, nil
}

// RedeemTicket consumes a ticket exactly once; reuse or expiry returns false.
func (s *Store) RedeemTicket(value string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked()
	t, ok := s.tickets[value]
	if !ok {
		return "", false
	}
	delete(s.tickets, value)
	if s.now().After(t.expires) {
		return "", false
	}
	return t.deviceID, true
}

// Revoke removes a device and any outstanding tickets for it.
func (s *Store) Revoke(deviceID string) error {
	s.mu.Lock()
	device, ok := s.devices[deviceID]
	if !ok {
		s.mu.Unlock()
		return oeerrors.Errorf("unknown device %q", deviceID)
	}
	delete(s.tokens, device.tokenHash)
	delete(s.devices, deviceID)
	for value, t := range s.tickets {
		if t.deviceID == deviceID {
			delete(s.tickets, value)
		}
	}
	items := s.snapshotLocked()
	s.mu.Unlock()
	return s.persistFlush(items)
}

// Rename updates a device's display name.
func (s *Store) Rename(deviceID, name string) error {
	s.mu.Lock()
	device, ok := s.devices[deviceID]
	if !ok {
		s.mu.Unlock()
		return oeerrors.Errorf("unknown device %q", deviceID)
	}
	device.Name = name
	items := s.snapshotLocked()
	s.mu.Unlock()
	return s.persistFlush(items)
}

// DeviceName returns the display name for a paired device id, or "" if unknown.
func (s *Store) DeviceName(deviceID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if device, ok := s.devices[deviceID]; ok {
		return device.Name
	}
	return ""
}

// ListDevices returns sanitized device records without token hashes.

func (s *Store) ListDevices() []Device {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Device, 0, len(s.devices))
	for _, device := range s.devices {
		public := *device
		public.tokenHash = ""
		out = append(out, public)
	}
	return out
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func randomHex(bytes int) string {
	buf := make([]byte, bytes)
	if _, err := rand.Read(buf); err != nil {
		panic(err)
	}
	return hex.EncodeToString(buf)
}

func randomDigits(count int) string {
	buf := make([]byte, count)
	for index := range buf {
		n, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			panic(err)
		}
		buf[index] = byte('0' + n.Int64())
	}
	return string(buf)
}
