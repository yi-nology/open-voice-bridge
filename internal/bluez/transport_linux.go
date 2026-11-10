//go:build linux

// Package bluez is the Linux BlueZ D-Bus transport for the ATVV GATT service.
package bluez

import (
	"context"
	oeerrors "github.com/oekit/pkg/errors"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/oekit/concurrency/async"
	"github.com/oekit/logx"

	"github.com/yi-nology/open-voice-bridge/internal/atvv"
)

var (
	ErrUnsupported = oeerrors.New("Bluetooth transport not implemented on this platform")
	log            = logx.Delegating("bluez")
)

// links is the resolved ATVV characteristic set, built by Connect and published
// in one assignment so readers never see a half-connected transport.
type links struct {
	tx          dbus.BusObject
	audio       dbus.BusObject
	control     dbus.BusObject
	txPath      dbus.ObjectPath
	audioPath   dbus.ObjectPath
	controlPath dbus.ObjectPath
	devicePath  dbus.ObjectPath
}

// Transport is one BlueZ GATT connection to a remote. Every mutable field sits
// behind mu: Connect runs on the reconnect goroutine while agent.Close, the
// signal dispatcher and Subscribe can each touch it (CON-01).
type Transport struct {
	address string

	mu        sync.RWMutex
	conn      *dbus.Conn
	links     links
	onAudio   func([]byte)
	onControl func([]byte)
	closed    bool

	signals        chan *dbus.Signal
	disconnected   chan struct{}
	disconnectOnce sync.Once
	done           chan struct{}
	doneOnce       sync.Once
}

func New(address string) *Transport {
	return &Transport{
		address:      address,
		disconnected: make(chan struct{}),
		done:         make(chan struct{}),
	}
}

func (t *Transport) Disconnected() <-chan struct{} { return t.disconnected }

func (t *Transport) Connect(ctx context.Context) error {
	conn, err := dbus.SystemBus()
	if err != nil {
		return oeerrors.Errorf("system bus: %w", err)
	}

	var devicePath dbus.ObjectPath
	var deviceProps map[string]map[string]dbus.Variant
	for attempt := 0; attempt < 20; attempt++ {
		objects, err := managedObjects(conn)
		if err != nil {
			_ = conn.Close()
			return err
		}
		for path, ifaces := range objects {
			dev, ok := ifaces["org.bluez.Device1"]
			if !ok {
				continue
			}
			addr, _ := dev["Address"].Value().(string)
			if strings.EqualFold(addr, t.address) {
				devicePath = path
				deviceProps = ifaces
				break
			}
		}
		if devicePath != "" {
			break
		}
		if err := sleepCtx(ctx, 200*time.Millisecond); err != nil {
			_ = conn.Close()
			return err
		}
	}
	if devicePath == "" {
		_ = conn.Close()
		return oeerrors.Errorf("device %s not found in BlueZ", t.address)
	}

	device := conn.Object("org.bluez", devicePath)
	connected := false
	if dev, ok := deviceProps["org.bluez.Device1"]; ok {
		connected, _ = dev["Connected"].Value().(bool)
	}
	if !connected {
		call := device.Call("org.bluez.Device1.Connect", 0)
		if call.Err != nil && !strings.Contains(call.Err.Error(), "AlreadyConnected") {
			_ = conn.Close()
			return oeerrors.Errorf("connect device: %w", call.Err)
		}
	}

	// Wait for the GATT tree (ServicesResolved) after connecting.
	var lk links
	for attempt := 0; attempt < 25; attempt++ {
		objects, err := managedObjects(conn)
		if err != nil {
			_ = conn.Close()
			return err
		}
		if lk = findATVV(conn, objects); lk.ready() {
			break
		}
		if err := sleepCtx(ctx, 200*time.Millisecond); err != nil {
			_ = conn.Close()
			return err
		}
	}
	if !lk.ready() {
		_ = conn.Close()
		return oeerrors.Errorf("ATVV characteristics not found; is the remote bonded?")
	}
	lk.devicePath = devicePath

	if err := conn.AddMatchSignal(
		dbus.WithMatchInterface("org.freedesktop.DBus.Properties"),
		dbus.WithMatchMember("PropertiesChanged"),
	); err != nil {
		_ = conn.Close()
		return oeerrors.Errorf("subscribe D-Bus signals: %w", err)
	}

	// Publishing happens last and under mu: agent.Close may have run during the
	// retries above, and installing a connection it already tore down would
	// strand the D-Bus socket and its dispatcher.
	signals := make(chan *dbus.Signal, 256)
	conn.Signal(signals)
	if err := t.publish(conn, lk, signals); err != nil {
		_ = conn.Close()
		return err
	}
	async.GoSafe(t.loop)
	return nil
}

// publish installs the resolved connection and characteristic set, or reports
// that Close already ran so the caller can drop the fresh connection.
func (t *Transport) publish(conn *dbus.Conn, lk links, signals chan *dbus.Signal) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return oeerrors.Errorf("transport closed")
	}
	t.conn = conn
	t.links = lk
	t.signals = signals
	return nil
}

func (l links) ready() bool {
	return l.tx != nil && l.audio != nil && l.control != nil
}

func managedObjects(conn *dbus.Conn) (map[dbus.ObjectPath]map[string]map[string]dbus.Variant, error) {
	var objects map[dbus.ObjectPath]map[string]map[string]dbus.Variant
	call := conn.Object("org.bluez", "/").Call(
		"org.freedesktop.DBus.ObjectManager.GetManagedObjects", 0,
	)
	if call.Err != nil {
		return nil, oeerrors.Errorf("GetManagedObjects: %w", call.Err)
	}
	if err := call.Store(&objects); err != nil {
		return nil, err
	}
	return objects, nil
}

func findATVV(conn *dbus.Conn, objects map[dbus.ObjectPath]map[string]map[string]dbus.Variant) links {
	var lk links
	var servicePath dbus.ObjectPath
	for path, ifaces := range objects {
		svc, ok := ifaces["org.bluez.GattService1"]
		if !ok {
			continue
		}
		uuid, _ := svc["UUID"].Value().(string)
		if strings.EqualFold(uuid, atvv.ServiceUUID) {
			servicePath = path
			break
		}
	}
	if servicePath == "" {
		return lk
	}
	for path, ifaces := range objects {
		ch, ok := ifaces["org.bluez.GattCharacteristic1"]
		if !ok {
			continue
		}
		var charService dbus.ObjectPath
		switch value := ch["Service"].Value().(type) {
		case string:
			charService = dbus.ObjectPath(value)
		case dbus.ObjectPath:
			charService = value
		}
		if charService != servicePath {
			continue
		}
		uuid, _ := ch["UUID"].Value().(string)
		obj := conn.Object("org.bluez", path)
		switch {
		case strings.EqualFold(uuid, atvv.TXUUID):
			lk.tx, lk.txPath = obj, path
		case strings.EqualFold(uuid, atvv.AudioUUID):
			lk.audio, lk.audioPath = obj, path
		case strings.EqualFold(uuid, atvv.ControlUUID):
			lk.control, lk.controlPath = obj, path
		}
	}
	return lk
}

func (t *Transport) Subscribe(audio, control func([]byte)) error {
	t.mu.Lock()
	t.onAudio = audio
	t.onControl = control
	audioChar, controlChar := t.links.audio, t.links.control
	t.mu.Unlock()
	if audioChar == nil || controlChar == nil {
		return oeerrors.Errorf("not connected")
	}
	for _, char := range []dbus.BusObject{audioChar, controlChar} {
		call := char.Call("org.bluez.GattCharacteristic1.StartNotify", 0)
		if call.Err != nil && !strings.Contains(call.Err.Error(), "Already") {
			return oeerrors.Errorf("StartNotify: %w", call.Err)
		}
	}
	return nil
}

func (t *Transport) WriteTX(data []byte) error {
	t.mu.RLock()
	tx := t.links.tx
	t.mu.RUnlock()
	if tx == nil {
		return oeerrors.Errorf("not connected")
	}
	options := map[string]dbus.Variant{
		"type": dbus.MakeVariant("command"),
	}
	call := tx.Call("org.bluez.GattCharacteristic1.WriteValue", 0, data, options)
	if call.Err != nil {
		// Some BlueZ versions require an explicit request write.
		options["type"] = dbus.MakeVariant("request")
		call = tx.Call("org.bluez.GattCharacteristic1.WriteValue", 0, data, options)
	}
	if call.Err != nil {
		return oeerrors.Errorf("WriteValue: %w", call.Err)
	}
	return nil
}

func (t *Transport) loop() {
	t.mu.RLock()
	signals := t.signals
	t.mu.RUnlock()
	for {
		select {
		case <-t.done:
			return
		case sig, ok := <-signals:
			if !ok {
				return
			}
			if sig == nil {
				continue
			}
			t.handleSignal(sig)
		}
	}
}

func (t *Transport) handleSignal(sig *dbus.Signal) {
	if sig.Name != "org.freedesktop.DBus.Properties.PropertiesChanged" || len(sig.Body) < 2 {
		return
	}
	iface, _ := sig.Body[0].(string)
	changed, _ := sig.Body[1].(map[string]dbus.Variant)
	if changed == nil {
		return
	}
	t.mu.RLock()
	lk := t.links
	audio, control := t.onAudio, t.onControl
	t.mu.RUnlock()

	if iface == "org.bluez.Device1" && sig.Path == lk.devicePath {
		if value, ok := changed["Connected"]; ok {
			if connected, _ := value.Value().(bool); !connected {
				t.disconnectOnce.Do(func() { close(t.disconnected) })
			}
		}
		return
	}
	if iface != "org.bluez.GattCharacteristic1" {
		return
	}
	value, ok := changed["Value"]
	if !ok {
		return
	}
	data := variantBytes(value)
	if data == nil {
		return
	}
	switch sig.Path {
	case lk.audioPath:
		if audio != nil {
			audio(data)
		}
	case lk.controlPath:
		if control != nil {
			control(data)
		}
	}
}

func variantBytes(value dbus.Variant) []byte {
	switch v := value.Value().(type) {
	case []byte:
		return v
	default:
		return nil
	}
}

// Close is idempotent: agent.Close and the reconnect loop both reach it, and a
// second close of t.done used to panic on the select-based guard.
func (t *Transport) Close(ctx context.Context) error {
	t.disconnectOnce.Do(func() { close(t.disconnected) })
	t.doneOnce.Do(func() { close(t.done) })

	t.mu.Lock()
	conn, audio, control := t.conn, t.links.audio, t.links.control
	t.conn = nil
	t.closed = true
	t.mu.Unlock()

	if conn == nil {
		return nil
	}
	for _, char := range []dbus.BusObject{audio, control} {
		if char != nil {
			if err := char.Call("org.bluez.GattCharacteristic1.StopNotify", 0).Err; err != nil {
				log.Debug("StopNotify failed", "err", err)
			}
		}
	}
	return conn.Close()
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
