package bluez

import "testing"

func TestParseModalias(t *testing.T) {
	vendor, product := parseModalias("bluetooth:v004Cp026Dd0000")
	if vendor != "004c" || product != "026d" {
		t.Fatalf("vendor=%q product=%q", vendor, product)
	}
	if vendor, product := parseModalias("bad"); vendor != "" || product != "" {
		t.Fatalf("unexpected parse: %q %q", vendor, product)
	}
}
