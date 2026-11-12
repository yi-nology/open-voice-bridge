//go:build linux

package keys

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/oekit/concurrency/async"
	"github.com/oekit/logx"
	"golang.org/x/sys/unix"
)

var log = logx.Delegating("keys")

const (
	evKey   = 0x01
	keyF24  = 194
	keyDown = 1
)

type inputEvent struct {
	Sec   int64
	Usec  int64
	Type  uint16
	Code  uint16
	Value int32
}

type f24Watcher struct {
	ch         chan struct{}
	phrases    chan int
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	openFailed map[string]bool
}

func newF24Watcher() F24Watcher {
	return &f24Watcher{
		ch:         make(chan struct{}, 8),
		phrases:    make(chan int, 8),
		openFailed: map[string]bool{},
	}
}

func (w *f24Watcher) C() <-chan struct{}  { return w.ch }
func (w *f24Watcher) Phrases() <-chan int { return w.phrases }

func (w *f24Watcher) Start(ctx context.Context) error {
	w.ctx, w.cancel = context.WithCancel(ctx)
	w.wg.Add(1)
	async.GoSafe(w.loop)
	return nil
}

func (w *f24Watcher) Close(ctx context.Context) error {
	if w.cancel != nil {
		w.cancel()
	}
	w.wg.Wait()
	return nil
}

func (w *f24Watcher) loop() {
	defer w.wg.Done()
	for {
		if w.ctx.Err() != nil {
			return
		}
		path := findEventNode()
		if path == "" {
			if !sleep(w.ctx, 2*time.Second) {
				return
			}
			continue
		}
		fd, err := w.openEventNode(path)
		if err != nil {
			if !sleep(w.ctx, 2*time.Second) {
				return
			}
			continue
		}
		log.Info("voice-key watcher active", "path", path)
		w.readLoop(fd)
		if err := unix.Close(fd); err != nil {
			log.Debug("close evdev", "err", err)
		}
		log.Warn("voice-key watcher lost device", "path", path)
		if !sleep(w.ctx, time.Second) {
			return
		}
	}
}

// openEventNode opens the watcher's device and reports the first failure for
// that node. The retry loop runs every 2s, so a silent EACCES looks like a dead
// voice key with an empty journal (OPS-05).
func (w *f24Watcher) openEventNode(path string) (int, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		w.noteOpenFailure(path, err)
		return 0, err
	}
	w.noteOpenSuccess(path)
	return fd, nil
}

// The map is only reached from loop's goroutine, so it needs no lock. Success
// clears the entry: a later outage is a new event, not a repeat.
func (w *f24Watcher) noteOpenFailure(path string, err error) {
	if w.openFailed[path] {
		return
	}
	w.openFailed[path] = true
	if errors.Is(err, unix.EACCES) || errors.Is(err, unix.EPERM) {
		log.Warn("voice-key watcher cannot read the input device; the voice key does nothing until this is fixed",
			"path", path, "err", err,
			"fix", `sudo usermod -aG input "$USER", then log out and back in`)
		return
	}
	log.Warn("voice-key watcher cannot open the input device", "path", path, "err", err)
}

func (w *f24Watcher) noteOpenSuccess(path string) {
	delete(w.openFailed, path)
}

func (w *f24Watcher) readLoop(fd int) {
	buf := make([]byte, 24*64)
	for {
		if w.ctx.Err() != nil {
			return
		}
		n, err := unix.Read(fd, buf)
		if err != nil {
			if err == unix.EAGAIN || err == unix.EWOULDBLOCK {
				if !sleep(w.ctx, 50*time.Millisecond) {
					return
				}
				continue
			}
			return
		}
		if n == 0 {
			return
		}
		for offset := 0; offset+24 <= n; offset += 24 {
			var ev inputEvent
			if err := binary.Read(bytes.NewReader(buf[offset:offset+24]), binary.LittleEndian, &ev); err != nil {
				continue
			}
			if ev.Type != evKey || ev.Value != keyDown {
				continue
			}
			if ev.Code == keyF24 {
				select {
				case w.ch <- struct{}{}:
				default:
				}
				continue
			}
			if slot, ok := SlotForCode(int(ev.Code)); ok {
				select {
				case w.phrases <- slot:
				default:
				}
			}
		}
	}
}

func findEventNode() string {
	data, err := os.ReadFile("/proc/bus/input/devices")
	if err != nil {
		return ""
	}
	blocks := strings.Split(string(data), "\n\n")
	priorities := []string{
		`Name="xiaomi-remote virtual keyboard"`,
		`Name="keyd virtual keyboard"`,
		`Name="小米蓝牙语音遥控器"`,
	}
	for _, needle := range priorities {
		for _, block := range blocks {
			if !strings.Contains(block, needle) {
				continue
			}
			match := regexp.MustCompile(`Handlers=.*?\b(event\d+)\b`).FindStringSubmatch(block)
			if len(match) == 2 {
				return "/dev/input/" + match[1]
			}
		}
	}
	for _, block := range blocks {
		lower := strings.ToLower(block)
		if !strings.Contains(lower, "vendor=2717") || !strings.Contains(lower, "product=32b8") {
			continue
		}
		match := regexp.MustCompile(`Handlers=.*?\b(event\d+)\b`).FindStringSubmatch(block)
		if len(match) == 2 {
			return "/dev/input/" + match[1]
		}
	}
	return ""
}

func sleep(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
