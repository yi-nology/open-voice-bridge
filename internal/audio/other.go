//go:build !linux

package audio

import "context"

type unsupportedSink struct{}

func newSink(cfg Config) (Sink, error) { return unsupportedSink{}, nil }

func (unsupportedSink) Start(ctx context.Context) error { return ErrUnsupported }
func (unsupportedSink) Write(samples []int16) error     { return ErrUnsupported }
func (unsupportedSink) Close(ctx context.Context) error { return nil }
func (unsupportedSink) Name() string                    { return "unsupported" }
func (unsupportedSink) Healthy() bool                   { return false }
