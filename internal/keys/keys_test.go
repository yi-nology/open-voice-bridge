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

// S2: F21 is the picker signal beside the F13–F20 phrase slots.
func TestPickerCode(t *testing.T) {
	if PickerKey != 191 {
		t.Fatalf("PickerKey = %d, want 191", PickerKey)
	}
	if _, ok := SlotForCode(PickerKey); ok {
		t.Fatal("picker code must not read as a phrase slot")
	}
}
