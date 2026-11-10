//go:build darwin

package audio

import "context"

// macOS needs an AudioServerPlugin for a real virtual microphone (out of
// scope for v1; see docs/design/macos-compat.md). The ASR chain consumes the
// decoded samples directly and does not depend on a sink, so the null sink
// simply accepts and discards them. Healthy stays false: there is no virtual
// mic node, and the UI should say so.

type nullSink struct{}

func newSink(cfg Config) (Sink, error) { return nullSink{}, nil }

func (nullSink) Start(ctx context.Context) error { return nil }
func (nullSink) Write(samples []int16) error     { return nil }
func (nullSink) Close(ctx context.Context) error { return nil }
func (nullSink) Name() string                    { return "null" }
func (nullSink) Healthy() bool                   { return false }
