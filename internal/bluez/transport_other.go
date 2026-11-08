//go:build !linux

package bluez

import (
	"context"
	oeerrors "github.com/oekit/pkg/errors"
)

var ErrUnsupported = oeerrors.New("Bluetooth transport not implemented on this platform")

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
func (t *Transport) WriteTX(data []byte) error       { return ErrUnsupported }
func (t *Transport) Close(ctx context.Context) error { return nil }
func (t *Transport) Disconnected() <-chan struct{}   { return t.disconnected }
