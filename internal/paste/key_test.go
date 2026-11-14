package paste

import "testing"

// OPS-13: the Wayland combo translator is pure — pin how friendly combo
// names become wtype flags.
func TestWtypeCombo(t *testing.T) {
	cases := []struct {
		combo string
		args  []string
	}{
		{"ctrl+c", []string{"-M", "ctrl", "-k", "c", "-m", "ctrl"}},
		{"enter", []string{"-k", "Return"}},
		{"ctrl+shift+s", []string{"-M", "ctrl", "-M", "shift", "-k", "s", "-m", "ctrl", "-m", "shift"}},
		{"super+a", []string{"-M", "logo", "-k", "a", "-m", "logo"}},
		{"backspace", []string{"-k", "BackSpace"}},
	}
	for _, tc := range cases {
		got := wtypeCombo(tc.combo)
		if len(got) != len(tc.args) {
			t.Fatalf("%s: args = %v, want %v", tc.combo, got, tc.args)
		}
		for index := range got {
			if got[index] != tc.args[index] {
				t.Fatalf("%s: args = %v, want %v", tc.combo, got, tc.args)
			}
		}
	}
	if wtypeCombo("ctrl+nosuchmod+x") != nil {
		t.Fatal("unknown modifier must fail closed")
	}
}
