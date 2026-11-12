//go:build linux

package remap

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func newInjectDaemon() *Daemon {
	return &Daemon{
		mapping:  MergeDefaults(nil),
		matchers: defaultMatchers(),
		clients:  map[*captureClient]struct{}{},
		cfg:      Config{AgentUID: os.Getuid()},
	}
}

// serveInject runs the daemon against a real Unix socket so the connection
// carries kernel credentials (SO_PEERCRED), and returns the client side.
func serveInject(t *testing.T, d *Daemon) net.Conn {
	t.Helper()
	dir, err := os.MkdirTemp("", "h")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	listener, err := net.Listen("unix", filepath.Join(dir, "helper.sock"))
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		d.handleConn(conn)
	}()
	conn, err := net.Dial("unix", listener.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func exchange(t *testing.T, conn net.Conn, w *bufio.Writer, r *bufio.Reader, command map[string]any) map[string]any {
	t.Helper()
	data, err := json.Marshal(command)
	if err != nil {
		t.Fatalf("marshal command: %v", err)
	}
	if _, err := w.Write(append(data, '\n')); err != nil {
		t.Fatalf("write command: %v", err)
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("flush command: %v", err)
	}
	line, err := r.ReadBytes('\n')
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	var response map[string]any
	if err := json.Unmarshal(line, &response); err != nil {
		t.Fatalf("decode response %q: %v", line, err)
	}
	return response
}

func TestDaemonInject(t *testing.T) {
	d := newInjectDaemon()
	conn := serveInject(t, d)
	w := bufio.NewWriter(conn)
	r := bufio.NewReader(conn)

	if resp := exchange(t, conn, w, r, map[string]any{"cmd": "inject", "key": "up", "value": 1}); resp["ok"] != true {
		t.Fatalf("allowed key rejected: %+v", resp)
	}
	if resp := exchange(t, conn, w, r, map[string]any{"cmd": "inject", "key": "up", "value": 0}); resp["ok"] != true {
		t.Fatalf("allowed key release rejected: %+v", resp)
	}
	resp := exchange(t, conn, w, r, map[string]any{"cmd": "inject", "key": "ok", "value": 1})
	if resp["ok"] != false || resp["error"] != "key not allowed" {
		t.Fatalf("whitelist miss not rejected: %+v", resp)
	}
	resp = exchange(t, conn, w, r, map[string]any{"cmd": "inject", "key": "down", "value": 7})
	if resp["ok"] != false || resp["error"] != "invalid value" {
		t.Fatalf("invalid value not rejected: %+v", resp)
	}
}

// SEC-01 wiring: a peer without kernel credentials (net.Pipe yields uid -1)
// must not reach privileged commands or even the inject whitelist. Foreign
// uids on a real socket are covered by TestHelperSocketPeerAuth.
func TestDaemonDeniesUnauthenticatedPeer(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	d := newInjectDaemon()
	go d.handleConn(server)
	w := bufio.NewWriter(client)
	r := bufio.NewReader(client)

	for _, cmd := range []map[string]any{
		{"cmd": "inject", "key": "up", "value": 1},
		{"cmd": "apply", "mapping": map[string]string{"up": "up"}},
		{"cmd": "capture_start"},
		{"cmd": "status"},
	} {
		resp := exchange(t, client, w, r, cmd)
		if resp["ok"] != false || resp["error"] != "denied" {
			t.Fatalf("unauthenticated %v not denied: %+v", cmd["cmd"], resp)
		}
	}
}

type fakeKeyboard struct {
	mu     sync.Mutex
	events []string
}

func (f *fakeKeyboard) KeyDown(key int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, fmt.Sprintf("down %d", key))
	return nil
}

func (f *fakeKeyboard) KeyUp(key int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, fmt.Sprintf("up %d", key))
	return nil
}

func (f *fakeKeyboard) KeyPress(key int) error {
	if err := f.KeyDown(key); err != nil {
		return err
	}
	return f.KeyUp(key)
}

func (f *fakeKeyboard) FetchSyspath() (string, error) { return "", nil }

func (f *fakeKeyboard) Close() error { return nil }

func (f *fakeKeyboard) snapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.events...)
}

// DOM-12: the kernel keeps sending value=2 while a key is held. Dropping it
// meant a held remote key never repeated; repeats re-tap the mapped action at
// the source's repeat rate.
func TestApplyKeyRepeatRetriggers(t *testing.T) {
	kb := &fakeKeyboard{}
	d := newInjectDaemon()
	d.kb = kb
	d.mapping = MergeDefaults(map[string]string{"up": "native", "home": "copy"})

	d.applyKey(1, "up", 1)
	d.applyKey(1, "up", 2)
	d.applyKey(1, "up", 2)
	d.applyKey(1, "up", 0)

	want := []string{"down 1", "up 1", "down 1", "up 1", "down 1", "up 1"}
	got := kb.snapshot()
	if len(got) != len(want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("events = %v, want %v", got, want)
		}
	}
}

// The voice key and the phrase keys are private signals for the agent's evdev
// watcher, not keys for the focused app. It accepts only value=1, so a
// re-tapped repeat reads as a brand-new press: holding the voice button fired
// MIC_OPEN every 50 ms and cut the capture into fragments too short to
// transcribe. A trigger fires once per physical press.
func TestTriggerKeysDoNotRepeat(t *testing.T) {
	for _, tc := range []struct{ key, action string }{
		{"f5", "f24"},
		{"compose", "phrase1"},
		{"menu", "phrase8"},
	} {
		t.Run(tc.key, func(t *testing.T) {
			kb := &fakeKeyboard{}
			d := newInjectDaemon()
			d.kb = kb
			d.mapping = MergeDefaults(map[string]string{tc.key: tc.action})
			code := ActionCodes[tc.action]
			source := SourceCodes[tc.key]

			d.applyKey(source, tc.key, 1)
			d.applyKey(source, tc.key, 2)
			d.applyKey(source, tc.key, 2)
			d.applyKey(source, tc.key, 0)

			want := []string{fmt.Sprintf("down %d", code), fmt.Sprintf("up %d", code)}
			got := kb.snapshot()
			if len(got) != len(want) {
				t.Fatalf("events = %v, want %v", got, want)
			}
			for index := range want {
				if got[index] != want[index] {
					t.Fatalf("events = %v, want %v", got, want)
				}
			}
		})
	}
}

// A held macro re-executes (release chord, press chord) on each repeat.
func TestApplyKeyRepeatRetriggersMacro(t *testing.T) {
	kb := &fakeKeyboard{}
	d := newInjectDaemon()
	d.kb = kb
	d.mapping = MergeDefaults(map[string]string{"home": "macro(leftctrl+c)"})

	d.applyKey(2, "home", 1)
	d.applyKey(2, "home", 2)

	events := kb.snapshot()
	downs := 0
	for _, item := range events {
		if strings.HasPrefix(item, "down ") {
			downs++
		}
	}
	if downs != 4 { // two keys, played twice
		t.Fatalf("macro repeat events = %v, want the chord played twice", events)
	}
	if !strings.HasPrefix(events[len(events)-1], "down ") {
		t.Fatalf("repeat should leave the chord pressed, got %v", events)
	}
}

// 按键次数统计: presses are counted at the single applyKey funnel (evdev and
// phone inject alike), exposed through status, and survive a restart.
func TestKeyCountsTrackPressesAndPersist(t *testing.T) {
	dir := t.TempDir()
	kb := &fakeKeyboard{}
	d := newInjectDaemon()
	d.kb = kb
	d.cfg.MappingPath = filepath.Join(dir, "mapping.json")
	d.mapping = MergeDefaults(map[string]string{"up": "native"})
	d.loadKeyCounts()

	d.applyKey(1, "up", 1) // press
	d.applyKey(1, "up", 2) // repeat: not a new press
	d.applyKey(1, "up", 1) // press
	d.applyKey(1, "up", 0) // release
	d.applyKey(2, "home", 1)

	status := d.status()
	if status["key_total"] != int64(3) {
		t.Fatalf("key_total = %v, want 3 presses", status["key_total"])
	}
	counts, _ := status["key_counts"].(map[string]int64)
	if counts["up"] != 2 || counts["home"] != 1 {
		t.Fatalf("key_counts = %v", counts)
	}

	if err := d.flushKeyCounts(); err != nil {
		t.Fatalf("flush: %v", err)
	}
	restarted := newInjectDaemon()
	restarted.cfg.MappingPath = filepath.Join(dir, "mapping.json")
	restarted.loadKeyCounts()
	if restarted.status()["key_total"] != int64(3) {
		t.Fatalf("counts lost across restart: %v", restarted.status()["key_total"])
	}
}

// Key counts are activity data — which keys, how often. Every other durable
// file lands 0600 inside a 0700 directory; this one has to as well.
func TestKeyCountsAreOwnerOnly(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	d := newInjectDaemon()
	d.cfg.MappingPath = filepath.Join(dir, "mapping.json")
	d.countPress("up")
	if err := d.flushKeyCounts(); err != nil {
		t.Fatalf("flush: %v", err)
	}
	info, err := os.Stat(filepath.Join(dir, "keycounts.json"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("keycounts perm = %04o, want 0600", perm)
	}
	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if perm := dirInfo.Mode().Perm(); perm&0o077 != 0 {
		t.Fatalf("state dir perm = %04o, want no group/other access", perm)
	}
}

// 组合动作 executes its steps in order; a repeat replays the whole sequence
// (按住连续执行), and the release is a no-op because every step is a tap.
func TestSeqActionPlaysStepsInOrder(t *testing.T) {
	kb := &fakeKeyboard{}
	d := newInjectDaemon()
	d.kb = kb
	d.mapping = MergeDefaults(map[string]string{"home": "seq(up,enter)"})

	d.applyKey(3, "home", 1)
	events := kb.snapshot()
	up := SourceCodes["up"]
	enter := SourceCodes["enter"]
	want := []string{
		fmt.Sprintf("down %d", up), fmt.Sprintf("up %d", up),
		fmt.Sprintf("down %d", enter), fmt.Sprintf("up %d", enter),
	}
	if len(events) != len(want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
	for i := range want {
		if events[i] != want[i] {
			t.Fatalf("events = %v, want %v", events, want)
		}
	}

	d.applyKey(3, "home", 2) // repeat replays
	if got := len(kb.snapshot()); got != 2*len(want) {
		t.Fatalf("repeat events = %v", kb.snapshot())
	}
	before := len(kb.snapshot())
	d.applyKey(3, "home", 0) // release: nothing to let go
	if len(kb.snapshot()) != before {
		t.Fatalf("release must be a no-op, got %v", kb.snapshot())
	}
}
