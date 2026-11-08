//go:build !linux

package remap

import (
	"context"

	oeerrors "github.com/oekit/pkg/errors"
)

var ErrUnsupported = oeerrors.New("key remapping is not implemented on this platform")

type Config struct {
	MappingPath string
	SocketPath  string
	SocketGroup int
}

type Daemon struct{}

func New(cfg Config) *Daemon { return &Daemon{} }

func (d *Daemon) Run(ctx context.Context) error { return ErrUnsupported }

func (d *Daemon) Close(ctx context.Context) error { return nil }

func RemotePresent() bool { return false }
