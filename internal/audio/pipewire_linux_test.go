package audio

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestNextWatchDelay(t *testing.T) {
	cases := []struct {
		name    string
		current time.Duration
		healthy bool
		want    time.Duration
	}{
		{"healthy resets", time.Minute, true, healthyWatchDelay},
		{"first failure waits longer", firstWatchDelay, false, healthyWatchDelay},
		{"second failure doubles", healthyWatchDelay, false, 30 * time.Second},
		{"third failure doubles", 30 * time.Second, false, time.Minute},
		{"caps at max", time.Minute, false, maxWatchDelay},
		{"stays capped", maxWatchDelay, false, maxWatchDelay},
	}
	for _, tc := range cases {
		if got := nextWatchDelay(tc.current, tc.healthy); got != tc.want {
			t.Errorf("%s: nextWatchDelay(%s, %v) = %s, want %s", tc.name, tc.current, tc.healthy, got, tc.want)
		}
	}
}

// blockingWriter never completes a Write, like a pw-cat whose stdin pipe has
// stopped draining.
type blockingWriter struct {
	closed chan struct{}
}

func newBlockingWriter() *blockingWriter { return &blockingWriter{closed: make(chan struct{})} }

func (w *blockingWriter) Write(p []byte) (int, error) {
	<-w.closed // hold the write open until Close unblocks it
	return 0, os.ErrClosed
}

func (w *blockingWriter) Close() error {
	close(w.closed)
	return nil
}

// CON-04: a stuck pw-cat must not pin the audio path. The write is bounded;
// on timeout the frame is dropped and the player recycled.
func TestWriteBoundedTimesOutOnStuckPlayer(t *testing.T) {
	sink := &pipeWireSink{ctx: context.Background()}
	sink.stdin = newBlockingWriter()
	done := make(chan error, 1)
	go func() {
		done <- sink.writeBoundedLocked([]byte{1, 2, 3, 4})
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("stuck write must report a timeout error")
		}
	case <-time.After(playerWriteTimeout + 2*time.Second):
		t.Fatal("writeBoundedLocked hung: pw-cat writes have no deadline")
	}
}

// CON-05: closing the sink must stop its watchdog. Without this, every
// restart leaves the old watcher ticking on the old context forever.
func TestCloseStopsWatcher(t *testing.T) {
	sink := &pipeWireSink{cfg: Config{SinkName: "x", SourceName: "y"}, ctx: context.Background(), stop: make(chan struct{})}
	watched := make(chan struct{})
	go func() {
		sink.watch(context.Background())
		close(watched)
	}()
	time.Sleep(20 * time.Millisecond) // let the watch enter its wait
	if err := sink.Close(context.Background()); err != nil {
		t.Fatalf("close: %v", err)
	}
	select {
	case <-watched:
	case <-time.After(1 * time.Second):
		t.Fatal("watcher kept running after Close: restarts leak watch goroutines")
	}
}
