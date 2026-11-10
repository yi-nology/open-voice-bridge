package phone

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestStore(t *testing.T, cfg Config) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "phone_tokens.json")
	store, err := NewStore(path, cfg)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return store
}

func fixedClock(t *testing.T) (*Store, *time.Time) {
	t.Helper()
	clock := time.Unix(1_700_000_000, 0)
	store := &Store{
		path:    filepath.Join(t.TempDir(), "phone_tokens.json"),
		now:     func() time.Time { return clock },
		codes:   map[string]*pairCode{},
		tokens:  map[string]string{},
		devices: map[string]*Device{},
		tickets: map[string]ticket{},
		ip:      map[string]*ipWindow{},
	}
	return store, &clock
}

func TestPairAndAuthenticate(t *testing.T) {
	store := newTestStore(t, DefaultConfig())
	code, _ := store.GeneratePairCode()
	if len(code) != 6 {
		t.Fatalf("code = %q, want 6 digits", code)
	}
	deviceID, token, err := store.Pair(code, "Pixel", "10.0.0.5")
	if err != nil {
		t.Fatalf("Pair: %v", err)
	}
	if deviceID == "" || token == "" {
		t.Fatalf("empty ids: %q %q", deviceID, token)
	}
	if id, ok := store.Authenticate(token); !ok || id != deviceID {
		t.Fatalf("Authenticate = (%q,%v)", id, ok)
	}
	if _, ok := store.Authenticate("deadbeef"); ok {
		t.Fatal("unknown token accepted")
	}
	// Token hash must not appear in the persisted file.
	data, _ := os.ReadFile(store.path)
	if strings.Contains(string(data), token) {
		t.Fatal("raw token leaked to persistence")
	}
}

func TestPairCodeIsOneTime(t *testing.T) {
	store := newTestStore(t, DefaultConfig())
	code, _ := store.GeneratePairCode()
	if _, _, err := store.Pair(code, "A", ""); err != nil {
		t.Fatalf("first Pair: %v", err)
	}
	if _, _, err := store.Pair(code, "B", ""); err == nil {
		t.Fatal("reused pairing code succeeded")
	}
}

func TestPairCodeExpires(t *testing.T) {
	store, clock := fixedClock(t)
	store.cfg = Config{PairCodeTTL: time.Minute, TicketTTL: time.Minute, MaxAttemptsPerCode: 6, MaxAttemptsPerIP: 10, MaxDevices: 16}
	code, _ := store.GeneratePairCode()
	*clock = clock.Add(2 * time.Minute)
	if _, _, err := store.Pair(code, "A", ""); err == nil {
		t.Fatal("expired code accepted")
	}
}

func TestPairCodeAttemptLimit(t *testing.T) {
	store := newTestStore(t, Config{PairCodeTTL: time.Hour, TicketTTL: time.Minute, MaxAttemptsPerCode: 3, MaxAttemptsPerIP: 100, MaxDevices: 16})
	code, _ := store.GeneratePairCode()
	for i := 0; i < 3; i++ {
		if _, _, err := store.Pair("000000", "x", ""); err == nil {
			t.Fatal("wrong code accepted")
		}
	}
	if _, _, err := store.Pair(code, "x", ""); err == nil {
		t.Fatal("code survived attempt limit")
	}
}

func TestPairRateLimitsPerIP(t *testing.T) {
	store := newTestStore(t, Config{PairCodeTTL: time.Hour, TicketTTL: time.Minute, MaxAttemptsPerCode: 100, MaxAttemptsPerIP: 2, MaxDevices: 16})
	for i := 0; i < 2; i++ {
		code, _ := store.GeneratePairCode()
		store.Pair(code, "x", "1.2.3.4")
	}
	code, _ := store.GeneratePairCode()
	if _, _, err := store.Pair(code, "x", "1.2.3.4"); err == nil {
		t.Fatal("IP rate limit not enforced")
	}
	// A different IP is unaffected.
	if _, _, err := store.Pair(code, "x", "5.6.7.8"); err != nil {
		t.Fatalf("other IP wrongly limited: %v", err)
	}
}

func TestTicketIsOneTimeAndExpires(t *testing.T) {
	store, clock := fixedClock(t)
	store.cfg = Config{PairCodeTTL: time.Hour, TicketTTL: 30 * time.Second, MaxAttemptsPerCode: 6, MaxAttemptsPerIP: 10, MaxDevices: 16}
	code, _ := store.GeneratePairCode()
	deviceID, _, err := store.Pair(code, "A", "")
	if err != nil {
		t.Fatalf("Pair: %v", err)
	}
	ticket, _, err := store.IssueTicket(deviceID)
	if err != nil {
		t.Fatalf("IssueTicket: %v", err)
	}
	if id, ok := store.RedeemTicket(ticket); !ok || id != deviceID {
		t.Fatalf("first redeem = (%q,%v)", id, ok)
	}
	if _, ok := store.RedeemTicket(ticket); ok {
		t.Fatal("ticket reused")
	}
	ticket2, _, _ := store.IssueTicket(deviceID)
	*clock = clock.Add(time.Minute)
	if _, ok := store.RedeemTicket(ticket2); ok {
		t.Fatal("expired ticket accepted")
	}
	if _, _, err := store.IssueTicket("nope"); err == nil {
		t.Fatal("ticket issued for unknown device")
	}
}

func TestRevokeInvalidates(t *testing.T) {
	store := newTestStore(t, DefaultConfig())
	code, _ := store.GeneratePairCode()
	deviceID, token, _ := store.Pair(code, "A", "")
	if err := store.Revoke(deviceID); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if _, ok := store.Authenticate(token); ok {
		t.Fatal("revoked token still authenticates")
	}
	if _, _, err := store.IssueTicket(deviceID); err == nil {
		t.Fatal("ticket issued for revoked device")
	}
	if err := store.Revoke(deviceID); err == nil {
		t.Fatal("double revoke should error")
	}
}

func TestMaxDevices(t *testing.T) {
	store := newTestStore(t, Config{PairCodeTTL: time.Hour, TicketTTL: time.Minute, MaxAttemptsPerCode: 6, MaxAttemptsPerIP: 100, MaxDevices: 2})
	for i := 0; i < 2; i++ {
		code, _ := store.GeneratePairCode()
		if _, _, err := store.Pair(code, "x", ""); err != nil {
			t.Fatalf("pair %d: %v", i, err)
		}
	}
	code, _ := store.GeneratePairCode()
	if _, _, err := store.Pair(code, "x", ""); err == nil {
		t.Fatal("max devices limit not enforced")
	}
}

func TestPersistenceRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "phone_tokens.json")
	store, err := NewStore(path, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	code, _ := store.GeneratePairCode()
	deviceID, token, _ := store.Pair(code, "Relo", "")
	reloaded, err := NewStore(path, DefaultConfig())
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if id, ok := reloaded.Authenticate(token); !ok || id != deviceID {
		t.Fatalf("reloaded authenticate = (%q,%v)", id, ok)
	}
	devices := reloaded.ListDevices()
	if len(devices) != 1 || devices[0].Name != "Relo" {
		t.Fatalf("reloaded devices = %+v", devices)
	}
	for _, d := range devices {
		if d.tokenHash != "" {
			t.Fatal("ListDevices leaked token hash")
		}
	}
}
