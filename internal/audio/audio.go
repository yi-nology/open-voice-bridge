// Package audio writes decoded RC003 microphone samples to the platform's
// virtual microphone device.
package audio

import (
	"context"
	oeerrors "github.com/oekit/pkg/errors"
)

var ErrUnsupported = oeerrors.New("audio backend not implemented on this platform")

type Config struct {
	SinkName   string
	SourceName string
	Rate       int
}

type Sink interface {
	Start(ctx context.Context) error
	Write(samples []int16) error
	Close(ctx context.Context) error
	Name() string
	Healthy() bool
}

func New(cfg Config) (Sink, error) {
	if cfg.SinkName == "" {
		cfg.SinkName = "xiaomi_mic_sink"
	}
	if cfg.SourceName == "" {
		cfg.SourceName = "xiaomi_mic"
	}
	if cfg.Rate == 0 {
		cfg.Rate = 16000
	}
	return newSink(cfg)
}
