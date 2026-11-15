package paste

import (
	"context"
	"testing"
)

func TestTextEmptyIsNoop(t *testing.T) {
	if err := Text(context.Background(), ""); err != nil {
		t.Fatalf("empty text should be a no-op: %v", err)
	}
}

// The backend decides which OS path types the transcript, so an unknown value
// has to land on auto rather than reach a path that does not exist.
func TestNormalizeBackend(t *testing.T) {
	cases := map[string]string{
		"":         BackendAuto,
		"auto":     BackendAuto,
		"wayland":  BackendWayland,
		" X11 ":    BackendX11,
		"wtype":    BackendAuto,
		"libinput": BackendAuto,
	}
	for in, want := range cases {
		if got := NormalizeBackend(in); got != want {
			t.Errorf("NormalizeBackend(%q) = %q, want %q", in, got, want)
		}
	}
	if Injector("x11") == nil || Injector("") == nil {
		t.Fatal("Injector must always return a function")
	}
}
