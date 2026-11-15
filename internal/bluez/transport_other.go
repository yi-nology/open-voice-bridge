//go:build !linux && !darwin

package bluez

import (
	"context"
)

type Transport struct {
	disconnected chan struct{}
}

func New(address string) *Transport {
	return &Transport{disconnected: make(chan struct{})}
}

func (t *Transport) Connect(ctx context.Context) error { return ErrUnsupported }
func (t *Transport) Subscribe(audio, control func([]byte)) error {
	return ErrUnsupported
}

// SubscribeKeys reports the platform gap honestly: CoreBluetooth hides the
// HID service (0x1812) from ordinary apps, so the Google TV family's button
// reports are unreadable on macOS (KeySubscribable is Linux-only).
func (t *Transport) SubscribeKeys(func(source string, data []byte)) error {
	return ErrUnsupported
}

func (t *Transport) WriteTX(data []byte) error       { return ErrUnsupported }
func (t *Transport) Close(ctx context.Context) error { return nil }
func (t *Transport) Disconnected() <-chan struct{}   { return t.disconnected }
