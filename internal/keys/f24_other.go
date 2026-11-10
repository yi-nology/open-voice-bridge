//go:build !linux

package keys

import "context"

type noopWatcher struct {
	ch      chan struct{}
	phrases chan int
}

func newF24Watcher() F24Watcher {
	return &noopWatcher{ch: make(chan struct{}), phrases: make(chan int)}
}

func (w *noopWatcher) Start(ctx context.Context) error { return nil }
func (w *noopWatcher) Close(ctx context.Context) error { return nil }
func (w *noopWatcher) C() <-chan struct{}              { return w.ch }
func (w *noopWatcher) Phrases() <-chan int             { return w.phrases }
