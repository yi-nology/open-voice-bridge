//go:build !linux

package keys

import "context"

type noopWatcher struct{ ch chan struct{} }

func newF24Watcher() F24Watcher { return &noopWatcher{ch: make(chan struct{})} }

func (w *noopWatcher) Start(ctx context.Context) error { return nil }
func (w *noopWatcher) Close(ctx context.Context) error { return nil }
func (w *noopWatcher) C() <-chan struct{}              { return w.ch }
