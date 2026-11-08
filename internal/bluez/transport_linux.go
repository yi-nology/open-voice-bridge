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

type Transport struct {
	address      string
	conn         *dbus.Conn
	signals      chan *dbus.Signal
	tx           dbus.BusObject
	audio        dbus.BusObject
	control      dbus.BusObject
	txPath       dbus.ObjectPath
	audioPath    dbus.ObjectPath
	controlPath  dbus.ObjectPath
	devicePath   dbus.ObjectPath
	onAudio      func([]byte)
	onControl    func([]byte)
	disconnected chan struct{}
	closeOnce    sync.Once
	done         chan struct{}
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
	t.conn = conn

	var deviceProps map[string]map[string]dbus.Variant
	for attempt := 0; attempt < 20; attempt++ {
		objects, err := t.managedObjects()
		if err != nil {
			return err
		}
		for path, ifaces := range objects {
			dev, ok := ifaces["org.bluez.Device1"]
			if !ok {
				continue
			}
			addr, _ := dev["Address"].Value().(string)
			if strings.EqualFold(addr, t.address) {
				t.devicePath = path
				deviceProps = ifaces
				break
			}
		}
		if t.devicePath != "" {
			break
		}
		if err := sleepCtx(ctx, 200*time.Millisecond); err != nil {
			return err
		}
	}
	if t.devicePath == "" {
		return oeerrors.Errorf("device %s not found in BlueZ", t.address)
	}

	device := conn.Object("org.bluez", t.devicePath)
	connected := false
	if dev, ok := deviceProps["org.bluez.Device1"]; ok {
		connected, _ = dev["Connected"].Value().(bool)
	}
	if !connected {
		call := device.Call("org.bluez.Device1.Connect", 0)
		if call.Err != nil && !strings.Contains(call.Err.Error(), "AlreadyConnected") {
			return oeerrors.Errorf("connect device: %w", call.Err)
		}
	}

	// Wait for the GATT tree (ServicesResolved) after connecting.
	for attempt := 0; attempt < 25; attempt++ {
		objects, err := t.managedObjects()
		if err != nil {
			return err
		}
		if t.findATVV(objects) {
			break
		}
		if err := sleepCtx(ctx, 200*time.Millisecond); err != nil {
			return err
		}
	}
	if t.tx == nil || t.audio == nil || t.control == nil {
		return oeerrors.Errorf("ATVV characteristics not found; is the remote bonded?")
	}

	if err := conn.AddMatchSignal(
		dbus.WithMatchInterface("org.freedesktop.DBus.Properties"),
		dbus.WithMatchMember("PropertiesChanged"),
	); err != nil {
		return oeerrors.Errorf("subscribe D-Bus signals: %w", err)
	}
	t.signals = make(chan *dbus.Signal, 256)
	conn.Signal(t.signals)
	async.GoSafe(t.loop)
	return nil
}

func (t *Transport) managedObjects() (map[dbus.ObjectPath]map[string]map[string]dbus.Variant, error) {
	var objects map[dbus.ObjectPath]map[string]map[string]dbus.Variant
	call := t.conn.Object("org.bluez", "/").Call(
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

func (t *Transport) findATVV(objects map[dbus.ObjectPath]map[string]map[string]dbus.Variant) bool {
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
		return false
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
		obj := t.conn.Object("org.bluez", path)
		switch {
		case strings.EqualFold(uuid, atvv.TXUUID):
			t.tx, t.txPath = obj, path
		case strings.EqualFold(uuid, atvv.AudioUUID):
			t.audio, t.audioPath = obj, path
		case strings.EqualFold(uuid, atvv.ControlUUID):
			t.control, t.controlPath = obj, path
		}
	}
	return t.tx != nil && t.audio != nil && t.control != nil
}

func (t *Transport) Subscribe(audio, control func([]byte)) error {
	t.onAudio = audio
	t.onControl = control
	for _, char := range []dbus.BusObject{t.audio, t.control} {
		call := char.Call("org.bluez.GattCharacteristic1.StartNotify", 0)
		if call.Err != nil && !strings.Contains(call.Err.Error(), "Already") {
			return oeerrors.Errorf("StartNotify: %w", call.Err)
		}
	}
	return nil
}

func (t *Transport) WriteTX(data []byte) error {
	options := map[string]dbus.Variant{
		"type": dbus.MakeVariant("command"),
	}
	call := t.tx.Call("org.bluez.GattCharacteristic1.WriteValue", 0, data, options)
	if call.Err != nil {
		// Some BlueZ versions require an explicit request write.
		options["type"] = dbus.MakeVariant("request")
		call = t.tx.Call("org.bluez.GattCharacteristic1.WriteValue", 0, data, options)
	}
	if call.Err != nil {
		return oeerrors.Errorf("WriteValue: %w", call.Err)
	}
	return nil
}

func (t *Transport) loop() {
	for {
		select {
		case <-t.done:
			return
		case sig, ok := <-t.signals:
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
	if iface == "org.bluez.Device1" && sig.Path == t.devicePath {
		if value, ok := changed["Connected"]; ok {
			if connected, _ := value.Value().(bool); !connected {
				t.closeOnce.Do(func() { close(t.disconnected) })
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
	case t.audioPath:
		if t.onAudio != nil {
			t.onAudio(data)
		}
	case t.controlPath:
		if t.onControl != nil {
			t.onControl(data)
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

func (t *Transport) Close(ctx context.Context) error {
	t.closeOnce.Do(func() { close(t.disconnected) })
	select {
	case <-t.done:
	default:
		close(t.done)
	}
	if t.conn != nil {
		for _, char := range []dbus.BusObject{t.audio, t.control} {
			if char != nil {
				if err := char.Call("org.bluez.GattCharacteristic1.StopNotify", 0).Err; err != nil {
					log.Debug("StopNotify failed", "err", err)
				}
			}
		}
		return t.conn.Close()
	}
	return nil
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
