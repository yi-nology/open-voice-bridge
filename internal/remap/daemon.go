//go:build linux

package remap

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	oeerrors "github.com/oekit/pkg/errors"
	"net"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/bendahl/uinput"
	"github.com/oekit/concurrency/async"
	"github.com/oekit/logx"
	"golang.org/x/sys/unix"

	"github.com/yi-nology/open-voice-bridge/internal/device"
)

var log = logx.Delegating("remap")

const EVIOCGRAB = 0x40044590

type Config struct {
	MappingPath string
	SocketPath  string
	SocketGroup int
	// AgentUID is the uid allowed to run privileged commands besides root
	// (SEC-01). 0 means unset: only root is trusted then.
	AgentUID int
}

type Daemon struct {
	cfg Config

	mu            sync.RWMutex
	mapping       map[string]string
	matchers      []device.Match
	appleEnabled  bool
	allowPowerKey bool
	capturing     bool
	remotePresent bool
	clients       map[*captureClient]struct{}

	emitMu   sync.Mutex
	kb       uinput.Keyboard
	listener net.Listener

	keyMu     sync.Mutex
	keyTotal  int64
	keyCounts map[string]int64
	keyDirty  bool
	keySince  int

	closeOnce sync.Once
}

type captureClient struct {
	conn net.Conn
	mu   sync.Mutex
}

type inputEvent struct {
	Sec   int64
	Usec  int64
	Type  uint16
	Code  uint16
	Value int32
}

func New(cfg Config) *Daemon {
	if cfg.MappingPath == "" {
		cfg.MappingPath = "/etc/xiaomi-remote/mapping.json"
	}
	if cfg.SocketPath == "" {
		cfg.SocketPath = "/run/xiaomi-remote-helper.sock"
	}
	if cfg.SocketGroup == 0 {
		cfg.SocketGroup = 1000
	}
	return &Daemon{
		cfg:      cfg,
		clients:  make(map[*captureClient]struct{}),
		matchers: defaultMatchers(),
	}
}

func (d *Daemon) Run(ctx context.Context) error {
	mapping, err := Load(d.cfg.MappingPath)
	if err != nil {
		return oeerrors.Errorf("load mapping: %w", err)
	}
	d.mapping = mapping
	d.loadKeyCounts()
	if _, err := os.Stat(d.cfg.MappingPath); os.IsNotExist(err) {
		if err := Save(d.cfg.MappingPath, mapping); err != nil {
			log.Warn("write default mapping", "err", err)
		}
	}

	keyboard, err := uinput.CreateKeyboard("/dev/uinput", []byte("xiaomi-remote virtual keyboard"))
	if err != nil {
		return oeerrors.Errorf("create uinput keyboard: %w", err)
	}
	d.kb = keyboard
	listener, err := d.listen()
	if err != nil {
		return err
	}
	d.listener = listener
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := d.Close(closeCtx); err != nil {
			log.Warn("remapper close", "err", err)
		}
	}()

	async.GoSafe(func() { d.acceptLoop(ctx, listener) })
	async.GoSafe(func() { d.eventLoop(ctx) })
	async.GoSafe(func() { d.appleLoop(ctx) })
	if d.cfg.AgentUID <= 0 {
		log.Warn("helper agent uid not configured; privileged commands limited to root", "hint", "pass --agent-uid")
	}
	log.Info("remapper started", "socket", d.cfg.SocketPath, "mapping", d.cfg.MappingPath)
	<-ctx.Done()
	return nil
}

func (d *Daemon) Close(ctx context.Context) error {
	d.closeOnce.Do(func() {
		if err := d.flushKeyCounts(); err != nil {
			log.Warn("flush key counts on close", "err", err)
		}
		d.closeClients()
		if d.listener != nil {
			if err := d.listener.Close(); err != nil {
				log.Warn("close remapper listener", "err", err)
			}
		}
		if d.kb != nil {
			// Closing uinput can block in rare kernel/driver states. The device
			// is destroyed by the kernel when the process exits, so close it
			// without delaying shutdown.
			kb := d.kb
			async.GoSafe(func() {
				if err := kb.Close(); err != nil {
					log.Warn("close uinput keyboard", "err", err)
				}
			})
		}
	})
	return nil
}

func (d *Daemon) listen() (net.Listener, error) {
	if err := os.Remove(d.cfg.SocketPath); err != nil && !os.IsNotExist(err) {
		log.Warn("remove stale socket", "err", err)
	}
	listener, err := net.Listen("unix", d.cfg.SocketPath)
	if err != nil {
		return nil, oeerrors.Errorf("listen %s: %w", d.cfg.SocketPath, err)
	}
	if err := os.Chmod(d.cfg.SocketPath, 0o660); err != nil {
		listener.Close()
		return nil, err
	}
	if err := os.Chown(d.cfg.SocketPath, 0, d.cfg.SocketGroup); err != nil {
		log.Warn("chown socket", "err", err)
	}
	return listener, nil
}

// peerUID reads SO_PEERCRED from an accepted Unix socket connection. It
// returns -1 when the credential cannot be established, which the auth rule
// treats as unauthenticated.
func peerUID(conn net.Conn) int32 {
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		return -1
	}
	raw, err := unixConn.SyscallConn()
	if err != nil {
		return -1
	}
	uid := int32(-1)
	if err := raw.Control(func(fd uintptr) {
		cred, err := unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
		if err != nil {
			return
		}
		uid = int32(cred.Uid)
	}); err != nil {
		return -1
	}
	return uid
}

func (d *Daemon) acceptLoop(ctx context.Context, listener net.Listener) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Warn("accept failed", "err", err)
			return
		}
		async.GoSafe(func() { d.handleConn(conn) })
	}
}

func (d *Daemon) handleConn(conn net.Conn) {
	defer func() {
		if err := conn.Close(); err != nil {
			log.Debug("close daemon connection", "err", err)
		}
	}()
	// SO_PEERCRED is fixed at connect time; read it once (SEC-01).
	peerUID := peerUID(conn)
	var client *captureClient
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 0, 4096), 1<<20)
	for scanner.Scan() {
		var command struct {
			Cmd     string            `json:"cmd"`
			Mapping map[string]string `json:"mapping"`
			Devices []device.Match    `json:"devices"`
			Apple   bool              `json:"apple"`
			// AllowPower carries phone.allow_power_key. It rides set_devices
			// because that command is already the agent's policy push and is
			// restricted to root/agent uid — a group peer cannot flip it.
			AllowPower bool   `json:"allow_power"`
			Key        string `json:"key"`
			Value      int    `json:"value"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &command); err != nil {
			d.writeConn(conn, map[string]any{"ok": false, "error": "bad json"})
			continue
		}
		if !authorizeCommand(peerUID, d.cfg.AgentUID, command.Cmd) {
			log.Warn("rejected helper command", "cmd", command.Cmd, "peer_uid", peerUID)
			d.writeConn(conn, map[string]any{"ok": false, "error": "denied"})
			continue
		}
		switch command.Cmd {
		case "status":
			d.writeConn(conn, d.status())
		case "apply":
			mapping := MergeDefaults(command.Mapping)
			if err := Validate(mapping); err != nil {
				d.writeConn(conn, map[string]any{"ok": false, "error": err.Error()})
				continue
			}
			if err := Save(d.cfg.MappingPath, mapping); err != nil {
				d.writeConn(conn, map[string]any{"ok": false, "error": err.Error()})
				continue
			}
			d.mu.Lock()
			d.mapping = mapping
			d.mu.Unlock()
			d.writeConn(conn, map[string]any{
				"ok":     true,
				"config": ConfigText(mapping),
			})
		case "set_devices":
			d.mu.Lock()
			d.matchers = append([]device.Match(nil), command.Devices...)
			d.appleEnabled = command.Apple
			d.allowPowerKey = command.AllowPower
			d.mu.Unlock()
			d.writeConn(conn, map[string]any{"ok": true, "devices": len(command.Devices), "apple": command.Apple, "allow_power": command.AllowPower})
		case "capture_start":
			if client == nil {
				client = &captureClient{conn: conn}
				d.addClient(client)
			}
			d.writeConn(conn, map[string]any{"ok": true, "event": "capture_started"})
		case "capture_stop":
			if client != nil {
				d.removeClient(client)
				client = nil
			}
			d.writeConn(conn, map[string]any{"ok": true, "event": "capture_stopped"})
			return
		case "inject":
			code, ok := InjectKeyCode(command.Key, d.powerAllowed())
			if !ok {
				log.Warn("rejected phone key injection", "key", command.Key)
				d.writeConn(conn, map[string]any{"ok": false, "error": "key not allowed"})
				continue
			}
			if command.Value != 0 && command.Value != 1 {
				d.writeConn(conn, map[string]any{"ok": false, "error": "invalid value"})
				continue
			}
			d.applyKey(code, command.Key, command.Value)
			d.writeConn(conn, map[string]any{"ok": true, "key": command.Key, "value": command.Value})
		default:
			d.writeConn(conn, map[string]any{"ok": false, "error": "unknown command"})
		}
	}
	if client != nil {
		d.removeClient(client)
	}
}

func (d *Daemon) status() map[string]any {
	keyTotal, keyCounts := d.keySnapshot()
	d.mu.RLock()
	defer d.mu.RUnlock()
	return map[string]any{
		"ok":             true,
		"keyd_active":    !d.capturing,
		"remote_present": d.remotePresent,
		"mapping":        Copy(d.mapping),
		"config":         ConfigText(d.mapping),
		"key_total":      keyTotal,
		"key_counts":     keyCounts,
	}
}

func (d *Daemon) writeConn(conn net.Conn, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	if err := conn.SetWriteDeadline(time.Now().Add(2 * time.Second)); err != nil {
		log.Debug("set helper write deadline", "err", err)
	}
	if _, err := conn.Write(append(data, '\n')); err != nil {
		log.Debug("write helper response", "err", err)
	}
}

func (d *Daemon) addClient(client *captureClient) {
	d.mu.Lock()
	d.clients[client] = struct{}{}
	d.capturing = true
	d.mu.Unlock()
}

func (d *Daemon) removeClient(client *captureClient) {
	d.mu.Lock()
	delete(d.clients, client)
	if len(d.clients) == 0 {
		d.capturing = false
	}
	d.mu.Unlock()
}

func (d *Daemon) closeClients() {
	d.mu.Lock()
	defer d.mu.Unlock()
	for client := range d.clients {
		if err := client.conn.Close(); err != nil {
			log.Debug("close capture client", "err", err)
		}
	}
	d.clients = make(map[*captureClient]struct{})
	d.capturing = false
}

func (d *Daemon) eventLoop(ctx context.Context) {
	type grabbedDevice struct {
		fd int
	}
	var mu sync.Mutex
	grabbed := map[string]grabbedDevice{}
	for ctx.Err() == nil {
		for _, path := range d.findRemoteEvents() {
			mu.Lock()
			_, exists := grabbed[path]
			mu.Unlock()
			if exists {
				continue
			}
			fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC, 0)
			if err != nil {
				log.Warn("open remote", "path", path, "err", err)
				continue
			}
			if err := unix.IoctlSetInt(fd, EVIOCGRAB, 1); err != nil {
				log.Warn("grab remote", "path", path, "err", err)
				if closeErr := unix.Close(fd); closeErr != nil {
					log.Debug("close evdev", "err", closeErr)
				}
				continue
			}
			mu.Lock()
			grabbed[path] = grabbedDevice{fd: fd}
			mu.Unlock()
			d.setRemote(true)
			log.Info("remote grabbed", "path", path)
			async.GoSafe(func() {
				d.readLoop(ctx, fd, path)
				if err := unix.IoctlSetInt(fd, EVIOCGRAB, 0); err != nil {
					log.Debug("release evdev grab", "path", path, "err", err)
				}
				if err := unix.Close(fd); err != nil {
					log.Debug("close evdev", "path", path, "err", err)
				}
				mu.Lock()
				delete(grabbed, path)
				remaining := len(grabbed)
				mu.Unlock()
				d.setRemote(remaining > 0)
				log.Warn("remote released; waiting for it to reconnect", "path", path)
			})
		}
		if !sleepCtx(ctx, 2*time.Second) {
			return
		}
	}
}

func (d *Daemon) readLoop(ctx context.Context, fd int, path string) {
	buffer := make([]byte, 24*64)
	pollFds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	for ctx.Err() == nil {
		ready, err := unix.Poll(pollFds, 1000)
		if err != nil {
			if err == unix.EINTR {
				continue
			}
			return
		}
		if ready == 0 {
			// Release devices that were unplugged or removed from the
			// configured matcher set without service restart.
			if !d.pathPresent(path) {
				return
			}
			continue
		}
		n, err := unix.Read(fd, buffer)
		if err != nil {
			if err == unix.EINTR {
				continue
			}
			return
		}
		if n == 0 {
			return
		}
		for offset := 0; offset+24 <= n; offset += 24 {
			var event inputEvent
			if err := binary.Read(bytes.NewReader(buffer[offset:offset+24]), binary.LittleEndian, &event); err != nil {
				continue
			}
			if event.Type != unix.EV_KEY {
				continue
			}
			value := int(event.Value)
			if value < 0 || value > 2 {
				continue
			}
			name := SourceName(int(event.Code))
			d.streamRaw(name, int(event.Code), value)
			if d.isCapturing() {
				continue
			}
			d.applyKey(int(event.Code), name, value)
		}
	}
}

func (d *Daemon) isCapturing() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.capturing
}

func (d *Daemon) appleActive() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.appleEnabled
}

// powerAllowed is false until the agent pushes the policy, so a helper that
// has not heard from the agent yet refuses KEY_POWER (ARC-12).
func (d *Daemon) powerAllowed() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.allowPowerKey
}

func (d *Daemon) setRemote(present bool) {
	d.mu.Lock()
	d.remotePresent = present
	d.mu.Unlock()
}

func (d *Daemon) streamRaw(name string, code, value int) {
	d.mu.RLock()
	clients := make([]*captureClient, 0, len(d.clients))
	for client := range d.clients {
		clients = append(clients, client)
	}
	d.mu.RUnlock()
	if len(clients) == 0 {
		return
	}
	event := map[string]any{
		"event": "key",
		"key":   name,
		"code":  code,
		"value": value,
	}
	for _, client := range clients {
		data, err := json.Marshal(event)
		if err != nil {
			continue
		}
		client.mu.Lock()
		if err := client.conn.SetWriteDeadline(time.Now().Add(500 * time.Millisecond)); err != nil {
			log.Debug("set event write deadline", "err", err)
		}
		_, err = client.conn.Write(append(data, '\n'))
		client.mu.Unlock()
		if err != nil {
			d.removeClient(client)
		}
	}
}

func (d *Daemon) applyKey(sourceCode int, name string, value int) {
	if value == 1 && d.countPress(name) {
		if err := d.flushKeyCounts(); err != nil {
			log.Warn("flush key counts", "err", err)
		}
	}
	d.mu.RLock()
	action := d.mapping[name]
	d.mu.RUnlock()
	if action == "" {
		action = "native"
	}
	d.emitMu.Lock()
	defer d.emitMu.Unlock()
	switch action {
	case "noop":
		return
	case "native", "":
		d.emit(sourceCode, value)
	default:
		if strings.HasPrefix(action, "seq(") {
			// 组合动作: press (and hold-repeat) plays the sequence; release is
			// a no-op because every step is self-contained (SayAll 一键做更多).
			if value == 1 || value == 2 {
				steps, err := ParseSeq(action)
				if err != nil {
					log.Warn("bad seq action", "key", name, "err", err)
					return
				}
				d.runSeq(steps)
			}
			return
		}
		if code, ok := ActionKeyCode(action); ok {
			d.emit(code, value)
			return
		}
		if codes, ok := MacroTokenCodes(action); ok {
			switch value {
			case 1:
				for _, code := range codes {
					d.emit(code, 1)
				}
			case 2:
				// A held key re-executes the chord at the source's repeat
				// rate (DOM-12): release it, then press again.
				for index := len(codes) - 1; index >= 0; index-- {
					d.emit(codes[index], 0)
				}
				for _, code := range codes {
					d.emit(code, 1)
				}
			default:
				for index := len(codes) - 1; index >= 0; index-- {
					d.emit(codes[index], 0)
				}
			}
			return
		}
		log.Warn("unknown mapping action", "key", name, "action", action)
	}
}

// runSeq plays a combo action one step at a time (see ParseSeq). It runs
// under emitMu via applyKey, so concurrent sequences never interleave.
func (d *Daemon) runSeq(steps []SeqStep) {
	for _, step := range steps {
		if step.Delay > 0 {
			time.Sleep(step.Delay)
			continue
		}
		for _, code := range step.Codes {
			d.emit(code, 1)
		}
		if len(step.Codes) > 1 {
			time.Sleep(15 * time.Millisecond) // let the chord land
		}
		for index := len(step.Codes) - 1; index >= 0; index-- {
			d.emit(step.Codes[index], 0)
		}
	}
}

func (d *Daemon) emit(code, value int) {
	if d.kb == nil {
		return
	}
	if value == 2 {
		if isTriggerCode(code) {
			// A trigger fires once per physical press; the agent cannot tell a
			// re-tapped repeat from a new press.
			return
		}
		// uinput has no repeat event and the library cannot write one; a
		// repeat re-taps the key so a held button keeps acting at the
		// source's repeat rate (DOM-12).
		if err := d.kb.KeyUp(code); err != nil {
			log.Warn("uinput release failed", "code", code, "err", err)
		}
		if err := d.kb.KeyDown(code); err != nil {
			log.Warn("uinput emit failed", "code", code, "value", 1, "err", err)
		}
		return
	}
	var err error
	if value == 1 {
		err = d.kb.KeyDown(code)
	} else {
		err = d.kb.KeyUp(code)
	}
	if err != nil {
		log.Warn("uinput emit failed", "code", code, "value", value, "err", err)
	}
}

func (d *Daemon) findRemoteEvents() []string {
	data, err := os.ReadFile("/proc/bus/input/devices")
	if err != nil {
		return nil
	}
	var paths []string
	for _, block := range strings.Split(string(data), "\n\n") {
		if !d.matchesBlock(block) {
			continue
		}
		match := regexp.MustCompile(`Handlers=.*?\b(event\d+)\b`).FindStringSubmatch(block)
		if len(match) == 2 {
			paths = append(paths, "/dev/input/"+match[1])
		}
	}
	return paths
}

func (d *Daemon) pathPresent(path string) bool {
	for _, candidate := range d.findRemoteEvents() {
		if candidate == path {
			return true
		}
	}
	return false
}

func (d *Daemon) matchesBlock(block string) bool {
	d.mu.RLock()
	matchers := append([]device.Match(nil), d.matchers...)
	d.mu.RUnlock()
	for _, matcher := range matchers {
		if matchBlock(matcher, block) {
			return true
		}
	}
	return false
}

func matchBlock(matcher device.Match, block string) bool {
	lower := strings.ToLower(block)
	vendor := strings.ToLower(strings.TrimSpace(matcher.Vendor))
	product := strings.ToLower(strings.TrimSpace(matcher.Product))
	if vendor != "" && product != "" &&
		strings.Contains(lower, "vendor="+vendor) &&
		strings.Contains(lower, "product="+product) {
		return true
	}
	for _, name := range matcher.Names {
		if name != "" && strings.Contains(block, `Name="`+name+`"`) {
			return true
		}
	}
	return false
}

func defaultMatchers() []device.Match {
	if profile, ok := device.Builtin().Get("rc003"); ok {
		return []device.Match{profile.Match}
	}
	return nil
}

// RemotePresent reports whether a matching remote input node exists.
func RemotePresent() bool {
	return len(defaultFinder().findRemoteEvents()) > 0
}

// RemoteEventPaths lists the matching input nodes. Diagnostics need the paths,
// not just their existence: a node can be present and still unreadable by the
// user the agent runs as (OPS-05).
func RemoteEventPaths() []string {
	return defaultFinder().findRemoteEvents()
}

func defaultFinder() *Daemon {
	return &Daemon{matchers: defaultMatchers()}
}
func sleepCtx(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
