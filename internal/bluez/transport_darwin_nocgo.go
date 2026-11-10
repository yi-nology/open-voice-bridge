//go:build darwin && !cgo

package bluez

import (
	"context"
	"errors"
)

// ErrUnsupported is returned when the binary was built without cgo: the
// CoreBluetooth bridge in ble_bridge_darwin.m needs it. Build with the
// default toolchain settings (CGO_ENABLED=1 on macOS) for real BLE support.

type Transport struct {
	disconnected chan struct{}
}

func New(address string) *Transport {
	return &Transport{disconnected: make(chan struct{})}
}

func (t *Transport) Connect(ctx context.Context) error {
	return errors.New("BLE transport requires cgo on macOS")
}
func (t *Transport) Subscribe(audio, control func([]byte)) error {
	return errors.New("BLE transport requires cgo on macOS")
}
func (t *Transport) WriteTX(data []byte) error {
	return errors.New("BLE transport requires cgo on macOS")
}
func (t *Transport) Close(ctx context.Context) error { return nil }
func (t *Transport) Disconnected() <-chan struct{}   { return t.disconnected }
