package core

import "testing"

// The yardstick is shared with /readyz and the doctor, so a new dependency must
// be classified deliberately: silently making one core would fail the doctor on
// hosts that readyz calls ready (OPS-04, OPS-06).
func TestOnlyTheASRChainIsCore(t *testing.T) {
	all := []Dependency{DepASR, DepHelper, DepRemote, DepBLE, DepMic, DepPaste, DepPhone, DepAdmin}
	for _, dep := range all {
		want := dep == DepASR
		if got := dep.Core(); got != want {
			t.Errorf("%s.Core() = %v, want %v", dep, got, want)
		}
	}
}
