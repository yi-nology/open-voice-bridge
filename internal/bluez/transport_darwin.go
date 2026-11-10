//go:build darwin && cgo

package bluez

/*
#cgo CFLAGS: -fobjc-arc
#cgo LDFLAGS: -framework CoreBluetooth -framework Foundation
#include "ble_bridge_darwin.h"
#include <stdlib.h>

// Declarations for the //export functions below. cgo needs them in the
// preamble to hand their addresses to the bridge; the shapes must match
// cgo's generated _cgo_export.h exactly (no const), and the call sites cast
// to the const-qualified callback typedefs.
extern void goAtvvAudio(uint8_t *data, size_t len, void *user);
extern void goAtvvControl(uint8_t *data, size_t len, void *user);
extern void goAtvvEvent(int event, char *message, void *user);
*/
import "C"

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime/cgo"
	"strings"
	"sync"
	"unsafe"

	"github.com/oekit/concurrency/async"
)

// Event kinds mirror ATVV_EVENT_* in ble_bridge_darwin.h.
const (
	eventDisconnected    = 1
	eventGATTReady       = 2
	eventConnectFailed   = 3
	eventSubscribed      = 4
	eventSubscribeFailed = 5
)

type bleEvent struct {
	kind    int
	message string
}

// Transport is one CoreBluetooth GATT connection to a remote. The bridge in
// ble_bridge_darwin.m owns the native side; every callback is mirrored onto
// Go channels and consumed by pump so agent code never runs on the
// CoreBluetooth queue (and can never deadlock a dispatch_sync call into the
// bridge). One Transport covers one connection lifetime, matching the Linux
// transport.
type Transport struct {
	address string

	mu        sync.RWMutex
	handle    unsafe.Pointer // atvv bridge handle
	userSlot  unsafe.Pointer // C-allocated slot holding the cgo.Handle
	handleVal cgo.Handle
	onAudio   func([]byte)
	onControl func([]byte)
	closed    bool

	events      chan bleEvent
	results     chan bleEvent
	audioFrames chan []byte
	ctlFrames   chan []byte

	disconnected   chan struct{}
	disconnectOnce sync.Once
	done           chan struct{}
	doneOnce       sync.Once
	cleanupOnce    sync.Once
}

//export goAtvvAudio
func goAtvvAudio(data *C.uint8_t, length C.size_t, user unsafe.Pointer) {
	t := transportOf(user)
	if t == nil {
		return
	}
	payload := C.GoBytes(unsafe.Pointer(data), C.int(length))
	select {
	case t.audioFrames <- payload:
	default:
	}
}

//export goAtvvControl
func goAtvvControl(data *C.uint8_t, length C.size_t, user unsafe.Pointer) {
	t := transportOf(user)
	if t == nil {
		return
	}
	payload := C.GoBytes(unsafe.Pointer(data), C.int(length))
	select {
	case t.ctlFrames <- payload:
	default:
	}
}

//export goAtvvEvent
func goAtvvEvent(event C.int, message *C.char, user unsafe.Pointer) {
	t := transportOf(user)
	if t == nil {
		return
	}
	text := ""
	if message != nil {
		text = C.GoString(message)
	}
	select {
	case t.events <- bleEvent{kind: int(event), message: text}:
	default:
	}
}

// transportOf resolves the cgo.Handle stashed in the bridge's user slot. The
// bridge only echoes the slot pointer back; it never dereferences Go memory.
func transportOf(user unsafe.Pointer) *Transport {
	if user == nil {
		return nil
	}
	raw := *(*C.uintptr_t)(user)
	if raw == 0 {
		return nil
	}
	value, ok := cgo.Handle(raw).Value().(*Transport)
	if !ok {
		return nil
	}
	return value
}

func New(address string) *Transport {
	return &Transport{
		address:      strings.ToUpper(strings.TrimSpace(address)),
		events:       make(chan bleEvent, 16),
		results:      make(chan bleEvent, 4),
		audioFrames:  make(chan []byte, 32),
		ctlFrames:    make(chan []byte, 32),
		disconnected: make(chan struct{}),
		done:         make(chan struct{}),
	}
}

func (t *Transport) Disconnected() <-chan struct{} { return t.disconnected }

func (t *Transport) Connect(ctx context.Context) error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return errors.New("transport closed")
	}
	if t.handle != nil {
		t.mu.Unlock()
		return errors.New("already connected")
	}
	t.mu.Unlock()

	id := loadBLEID(t.address)
	var cID, cName *C.char
	if id != "" {
		cID = C.CString(id)
		defer C.free(unsafe.Pointer(cID))
	}
	if hint := nameHintFor(t.address); hint != "" {
		cName = C.CString(hint)
		defer C.free(unsafe.Pointer(cName))
	}

	registryKey := cgo.NewHandle(t)
	userSlot := (*C.uintptr_t)(C.malloc(C.size_t(unsafe.Sizeof(C.uintptr_t(0)))))
	*userSlot = C.uintptr_t(registryKey)

	var errbuf [256]C.char
	handle := C.atvv_open(cID, cName,
		(C.atvv_data_cb)(C.goAtvvAudio),
		(C.atvv_data_cb)(C.goAtvvControl),
		(C.atvv_event_cb)(C.goAtvvEvent),
		unsafe.Pointer(userSlot),
		&errbuf[0], C.size_t(len(errbuf)))
	if handle == nil {
		C.free(unsafe.Pointer(userSlot))
		registryKey.Delete()
		return errors.New("open BLE bridge: " + C.GoString(&errbuf[0]))
	}

	if C.atvv_start_connect(handle, &errbuf[0], C.size_t(len(errbuf))) != 0 {
		C.atvv_close(handle)
		C.free(unsafe.Pointer(userSlot))
		registryKey.Delete()
		return errors.New("start connect: " + C.GoString(&errbuf[0]))
	}

	t.mu.Lock()
	t.handle = handle
	t.userSlot = unsafe.Pointer(userSlot)
	t.handleVal = registryKey
	t.mu.Unlock()
	async.GoSafe(t.pump)

	for {
		select {
		case <-ctx.Done():
			_ = t.Close(context.Background())
			return ctx.Err()
		case <-t.done:
			return errors.New("transport closed")
		case event := <-t.results:
			switch event.kind {
			case eventGATTReady:
				if id := C.atvv_identifier(handle); id != nil {
					saveBLEID(t.address, C.GoString(id))
				}
				return nil
			case eventConnectFailed:
				_ = t.Close(context.Background())
				return errors.New(event.message)
			case eventDisconnected:
				_ = t.Close(context.Background())
				return errors.New("disconnected during connect")
			}
		}
	}
}

func (t *Transport) Subscribe(audio, control func([]byte)) error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return errors.New("transport closed")
	}
	if t.handle == nil {
		t.mu.Unlock()
		return errors.New("not connected")
	}
	t.onAudio = audio
	t.onControl = control
	handle := t.handle
	t.mu.Unlock()

	var errbuf [256]C.char
	if C.atvv_start_subscribe(handle, &errbuf[0], C.size_t(len(errbuf))) != 0 {
		return errors.New("start subscribe: " + C.GoString(&errbuf[0]))
	}

	for {
		select {
		case <-t.done:
			return errors.New("transport closed")
		case event := <-t.results:
			switch event.kind {
			case eventSubscribed:
				return nil
			case eventSubscribeFailed:
				return errors.New(event.message)
			case eventDisconnected:
				return errors.New("disconnected during subscribe")
			}
		}
	}
}

func (t *Transport) WriteTX(data []byte) error {
	t.mu.RLock()
	handle := t.handle
	closed := t.closed
	t.mu.RUnlock()
	if closed {
		return errors.New("transport closed")
	}
	if handle == nil {
		return errors.New("not connected")
	}
	if len(data) == 0 {
		return nil
	}
	var errbuf [256]C.char
	if C.atvv_write(handle, (*C.uint8_t)(unsafe.Pointer(&data[0])), C.size_t(len(data)),
		&errbuf[0], C.size_t(len(errbuf))) != 0 {
		return errors.New("write TX: " + C.GoString(&errbuf[0]))
	}
	return nil
}

func (t *Transport) Close(ctx context.Context) error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil
	}
	t.closed = true
	handle := t.handle
	userSlot := t.userSlot
	handleVal := t.handleVal
	t.mu.Unlock()

	if handle != nil {
		C.atvv_close(handle)
	}
	t.cleanupOnce.Do(func() {
		if handleVal != 0 {
			handleVal.Delete()
		}
		if userSlot != nil {
			C.free(userSlot)
		}
	})
	t.doneOnce.Do(func() { close(t.done) })
	t.disconnectOnce.Do(func() { close(t.disconnected) })
	return nil
}

// pump runs until done, mirroring bridge callbacks into the agent-facing
// callbacks and routing completion events to whoever is waiting in
// Connect/Subscribe.
func (t *Transport) pump() {
	for {
		select {
		case <-t.done:
			return
		case event := <-t.events:
			switch event.kind {
			case eventDisconnected:
				t.disconnectOnce.Do(func() { close(t.disconnected) })
			default:
				select {
				case t.results <- event:
				default:
				}
			}
		case payload := <-t.audioFrames:
			t.mu.RLock()
			callback := t.onAudio
			t.mu.RUnlock()
			if callback != nil {
				callback(payload)
			}
		case payload := <-t.ctlFrames:
			t.mu.RLock()
			callback := t.onControl
			t.mu.RUnlock()
			if callback != nil {
				callback(payload)
			}
		}
	}
}

// --- peripheral identifier persistence -------------------------------------

// CoreBluetooth does not expose BLE MAC addresses. After the first successful
// connection we persist address → peripheral identifier so later connects
// resolve deterministically (see docs/design/macos-compat.md).
func bleIDMapPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "state", "xiaomi-remote", "ble-id-map.json")
}

func loadBLEID(address string) string {
	path := bleIDMapPath()
	if path == "" || address == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var mapping map[string]string
	if err := json.Unmarshal(data, &mapping); err != nil {
		return ""
	}
	return mapping[address]
}

func saveBLEID(address, id string) {
	path := bleIDMapPath()
	if path == "" || address == "" || id == "" {
		return
	}
	mapping := map[string]string{}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &mapping)
	}
	mapping[address] = id
	payload, err := json.MarshalIndent(mapping, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	_ = os.WriteFile(path, payload, 0o600)
}

// nameHintFor disambiguates multiple remotes during auto-resolution. The
// transport only sees the address, so v1 resolves by ATVV/HID service and
// persists the winner; the hint stays empty.
func nameHintFor(string) string { return "" }
