package phone

import (
	"context"
	"errors"
	"testing"
)

type fakeTransport struct {
	calls []string
	err   error
}

func (f *fakeTransport) Inject(_ context.Context, key string, value int) error {
	f.calls = append(f.calls, key)
	if f.err != nil {
		return f.err
	}
	_ = value
	return nil
}

func TestKeyInjectorForwardsAllowedKey(t *testing.T) {
	transport := &fakeTransport{}
	injector := NewKeyInjector(transport, false)
	if err := injector.Inject(context.Background(), "up", 1); err != nil {
		t.Fatalf("inject up: %v", err)
	}
	if len(transport.calls) != 1 || transport.calls[0] != "up" {
		t.Fatalf("calls = %v", transport.calls)
	}
}

func TestKeyInjectorRejectsNonWhitelist(t *testing.T) {
	transport := &fakeTransport{}
	injector := NewKeyInjector(transport, false)
	for _, key := range []string{"playpause", "ok", "siri", "", "UP"} {
		err := injector.Inject(context.Background(), key, 1)
		if !errors.Is(err, errKeyNotAllowed) {
			t.Fatalf("key %q: err = %v, want not-allowed", key, err)
		}
	}
	if len(transport.calls) != 0 {
		t.Fatalf("non-whitelisted keys reached helper: %v", transport.calls)
	}
}

func TestKeyInjectorRejectsBadValue(t *testing.T) {
	injector := NewKeyInjector(&fakeTransport{}, false)
	if err := injector.Inject(context.Background(), "enter", 7); !errors.Is(err, errInvalidValue) {
		t.Fatalf("value 7: err = %v, want invalid", err)
	}
	if err := injector.Inject(context.Background(), "enter", 0); err != nil {
		t.Fatalf("value 0 (release) should pass: %v", err)
	}
}

func TestKeyInjectorNoHelper(t *testing.T) {
	injector := NewKeyInjector(nil, false)
	if err := injector.Inject(context.Background(), "home", 1); !errors.Is(err, errNoHelper) {
		t.Fatalf("err = %v, want no-helper", err)
	}
}

func TestKeyInjectorTransportErrorPropagates(t *testing.T) {
	sentinel := errors.New("socket down")
	injector := NewKeyInjector(&fakeTransport{err: sentinel}, false)
	if err := injector.Inject(context.Background(), "home", 1); !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want wrapped helper error", err)
	}
}

// ARC-12: the agent-side gate must refuse power too, so a paired phone never
// even reaches the helper socket with it.
func TestKeyInjectorRefusesPowerByDefault(t *testing.T) {
	transport := &fakeTransport{}
	injector := NewKeyInjector(transport, false)
	if injector.Allowed("power") {
		t.Fatal("power is allowed by default")
	}
	if err := injector.Inject(context.Background(), "power", 1); !errors.Is(err, errKeyNotAllowed) {
		t.Fatalf("err = %v, want not-allowed", err)
	}
	if len(transport.calls) != 0 {
		t.Fatalf("power reached the helper: %v", transport.calls)
	}
}

func TestKeyInjectorAllowedMatchesWhitelist(t *testing.T) {
	injector := NewKeyInjector(nil, false)
	for _, key := range []string{"up", "down", "left", "right", "enter", "back", "home", "menu", "volumeup", "volumedown", "f5"} {
		if !injector.Allowed(key) {
			t.Fatalf("%q should be allowed", key)
		}
	}
	for _, key := range []string{"ok", "homepage", "playpause", "power"} {
		if injector.Allowed(key) {
			t.Fatalf("%q should not be allowed", key)
		}
	}
}

// ARC-12: the opt-in is per injector, so a runtime built with the flag off
// cannot forward power even if another runtime in the same process allows it.
func TestKeyInjectorPowerFollowsTheOptIn(t *testing.T) {
	transport := &fakeTransport{}
	injector := NewKeyInjector(transport, true)
	if !injector.Allowed("power") {
		t.Fatal("power should be allowed once the user opts in")
	}
	if err := injector.Inject(context.Background(), "power", 1); err != nil {
		t.Fatalf("inject power with opt-in: %v", err)
	}
	if len(transport.calls) != 1 || transport.calls[0] != "power" {
		t.Fatalf("calls = %v", transport.calls)
	}
	off := NewKeyInjector(transport, false)
	if err := off.Inject(context.Background(), "power", 1); !errors.Is(err, errKeyNotAllowed) {
		t.Fatalf("a second injector without the opt-in: err = %v", err)
	}
	if len(transport.calls) != 1 {
		t.Fatalf("the opted-out injector reached the helper: %v", transport.calls)
	}
}
