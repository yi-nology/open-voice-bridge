//go:build linux

package remap

import "testing"

func TestAppleButtonTransitions(t *testing.T) {
	transitions := appleTransitions(0, 0x80)
	if len(transitions) != 1 || transitions[0].name != "enter" || transitions[0].value != 1 {
		t.Fatalf("click press = %+v", transitions)
	}
	transitions = appleTransitions(0x80, 0)
	if len(transitions) != 1 || transitions[0].name != "enter" || transitions[0].value != 0 {
		t.Fatalf("click release = %+v", transitions)
	}
	transitions = appleTransitions(0, 0x22)
	if len(transitions) != 2 {
		t.Fatalf("menu+volume = %+v", transitions)
	}
}
