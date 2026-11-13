package agent

import (
	"context"
	"time"

	"github.com/yi-nology/open-voice-bridge/internal/atvv"
	"github.com/yi-nology/open-voice-bridge/internal/bluez"
)

// Transport is the BLE link to a remote — the consumer-side slice of
// internal/bluez that connectLoop needs (interfaces declared by the
// consumer, architecture §4).
type Transport interface {
	Connect(ctx context.Context) error
	Subscribe(audio, control func([]byte)) error
	WriteTX(data []byte) error
	Close(ctx context.Context) error
	Disconnected() <-chan struct{}
}

// Session is the ATVV protocol state for one connection — the slice of
// internal/atvv the agent drives.
type Session interface {
	Caps() *atvv.Caps
	MicOpenCommand() []byte
	HandleControl(payload []byte) string
	HandleAudio(payload []byte) []int16
	SampleRate() int
}

// TransportFactory opens the transport and its protocol session for one
// address. Injected so connectLoop runs against fakes in tests and harnesses
// (ARC-08) instead of a radio.
type TransportFactory func(addr string) (Transport, Session, error)

// DefaultTransportFactory is the production wiring: BlueZ transport plus the
// ATVV session state machine.
func DefaultTransportFactory(addr string) (Transport, Session, error) {
	return bluez.New(addr), atvv.NewSession(), nil
}

// linkBackoff paces connect retries: it starts at start and doubles up to
// ceiling, and reset restarts the ramp after a successful connect. This is
// the one copy of the pacing that connectLoop used to repeat three times.
type linkBackoff struct {
	d       time.Duration
	start   time.Duration
	ceiling time.Duration
}

func (b *linkBackoff) reset()                 { b.d = b.start }
func (b *linkBackoff) current() time.Duration { return b.d }

func (b *linkBackoff) step() {
	if b.d < b.ceiling {
		b.d *= 2
		if b.d > b.ceiling {
			b.d = b.ceiling
		}
	}
}

// wait sleeps out the current delay and then advances the ramp. False means
// the agent is shutting down.
func (b *linkBackoff) wait(ctx context.Context) bool {
	ok := sleepCtx(ctx, b.d)
	if ok {
		b.step()
	}
	return ok
}
