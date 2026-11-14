// Code split from server.go (ARC-09): same package, one concern per file.
package phone

import (
	"context"
	"time"
)

// keyJob is one press/release awaiting the root helper. Jobs leave in order so
// a slow helper can never reorder or block the connection's PCM (DOM-07).
type keyJob struct {
	key   string
	value int
}

func (s *Server) handleKey(ctx context.Context, active *activeConn, msg controlMessage) {
	if msg.Value == nil {
		active.send(ctx, errorFrame("bad_message", "key value required"))
		return
	}
	// A dropped session (reconnect replacement, revocation) has already
	// stopped its worker; answering busy keeps the protocol whole instead of
	// panicking the read loop on a send into a retired queue (#12).
	select {
	case <-active.keyDone:
		active.send(ctx, errorFrame("busy", "key queue is full"))
		return
	default:
	}
	// The helper round-trip runs on its own ordered queue (DOM-07): a wedged
	// root helper must not stall the same connection's audio frames.
	select {
	case active.keyJobs <- keyJob{key: msg.Key, value: *msg.Value}:
		active.source.touch()
	default:
		active.send(ctx, errorFrame("busy", "key queue is full"))
	}
}

// keyWorker injects one connection's keys in order and acknowledges each from
// its own goroutine, so the read loop only ever does non-blocking hand-offs.
// It exits via keyDone, never by draining a closed channel: closing keyJobs
// would turn a key frame racing the teardown into "send on closed channel"
// (#12), and draining would inject every already-queued press after a revoke.
func (s *Server) keyWorker(active *activeConn) {
	for {
		select {
		case <-active.keyDone:
			return
		default:
		}
		select {
		case <-active.keyDone:
			return
		case job := <-active.keyJobs:
			sendCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			if err := s.injector.Inject(sendCtx, job.key, job.value); err != nil {
				// The rejected value is never logged, only the reason.
				log.Warn("phone key injection denied", "device", active.deviceID, "key", job.key, "err", err)
				active.send(sendCtx, errorFrame("denied", err.Error()))
			} else {
				active.send(sendCtx, map[string]any{"type": "key_ack", "key": job.key, "value": job.value})
			}
			cancel()
		}
	}
}

// stopKeyQueue stops the injection worker exactly once. The jobs channel is
// never closed — the send side has no "closed" verdict of its own, so a send
// into a closed channel would panic the read loop. A frame arriving after the
// stop is refused up front, and any frame that slips past that check lands in
// a buffer nobody reads while the socket dies (harmless by construction).
func (active *activeConn) stopKeyQueue() {
	active.keyOnce.Do(func() {
		close(active.keyDone)
	})
}

// --- WebSocket microphone -------------------------------------------------
