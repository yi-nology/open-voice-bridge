// Package keys watches for the voice key event that the bridge uses as a
// fallback MIC_OPEN trigger.
package keys

import "context"

// F24Watcher emits a value on C for every F24 key-down.
type F24Watcher interface {
	Start(ctx context.Context) error
	Close(ctx context.Context) error
	C() <-chan struct{}
}

func NewF24Watcher() F24Watcher { return newF24Watcher() }
