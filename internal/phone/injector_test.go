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
	injector := NewKeyInjector(transport)
	if err := injector.Inject(context.Background(), "up", 1); err != nil {
		t.Fatalf("inject up: %v", err)
	}
	if len(transport.calls) != 1 || transport.calls[0] != "up" {
		t.Fatalf("calls = %v", transport.calls)
	}
}

func TestKeyInjectorRejectsNonWhitelist(t *testing.T) {
	transport := &fakeTransport{}
	injector := NewKeyInjector(transport)
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
	injector := NewKeyInjector(&fakeTransport{})
	if err := injector.Inject(context.Background(), "enter", 7); !errors.Is(err, errInvalidValue) {
		t.Fatalf("value 7: err = %v, want invalid", err)
	}
	if err := injector.Inject(context.Background(), "enter", 0); err != nil {
		t.Fatalf("value 0 (release) should pass: %v", err)
	}
}

func TestKeyInjectorNoHelper(t *testing.T) {
	injector := NewKeyInjector(nil)
	if err := injector.Inject(context.Background(), "home", 1); !errors.Is(err, errNoHelper) {
		t.Fatalf("err = %v, want no-helper", err)
	}
}

func TestKeyInjectorTransportErrorPropagates(t *testing.T) {
	sentinel := errors.New("socket down")
	injector := NewKeyInjector(&fakeTransport{err: sentinel})
	if err := injector.Inject(context.Background(), "power", 1); !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want wrapped helper error", err)
	}
}

func TestKeyInjectorAllowedMatchesWhitelist(t *testing.T) {
	injector := NewKeyInjector(nil)
	for _, key := range []string{"up", "down", "left", "right", "enter", "back", "home", "menu", "volumeup", "volumedown", "power", "f5"} {
		if !injector.Allowed(key) {
			t.Fatalf("%q should be allowed", key)
		}
	}
	for _, key := range []string{"ok", "homepage", "playpause"} {
		if injector.Allowed(key) {
			t.Fatalf("%q should not be allowed", key)
		}
	}
}
