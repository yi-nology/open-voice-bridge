package keys

import "testing"

func TestNewF24Watcher(t *testing.T) {
	watcher := NewF24Watcher()
	if watcher == nil {
		t.Fatal("nil watcher")
	}
	if watcher.C() == nil {
		t.Fatal("nil event channel")
	}
}
