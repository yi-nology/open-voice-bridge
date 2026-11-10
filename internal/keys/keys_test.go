package keys

import "testing"

// The phrase slots are the F13–F20 codes the remap daemon injects for the
// phrase1..phrase8 actions; anything outside that band is not a phrase.
func TestSlotForCode(t *testing.T) {
	cases := map[int]int{
		183: 1, 184: 2, 185: 3, 186: 4,
		187: 5, 188: 6, 189: 7, 190: 8,
	}
	for code, want := range cases {
		got, ok := SlotForCode(code)
		if !ok || got != want {
			t.Fatalf("SlotForCode(%d) = %d,%v want %d", code, got, ok, want)
		}
	}
	for _, code := range []int{0, 182, 191, 194, 255} {
		if slot, ok := SlotForCode(code); ok {
			t.Fatalf("SlotForCode(%d) = %d, want not-ok", code, slot)
		}
	}
}
