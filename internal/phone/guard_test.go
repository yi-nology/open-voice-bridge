package phone

import (
	"os"
	"testing"

	"github.com/yi-nology/open-voice-bridge/edge/netguard"
)

// TestMain lends the tests the production boundary: the seams in guard.go
// default to fail-closed and the composition root wires edge/netguard in —
// tests want the same admission logic the real surface runs. (Test files
// may import the edge; the R2 guard parses non-test sources only.)
func TestMain(m *testing.M) {
	SameOrigin = netguard.SameOrigin
	WriteSecHeaders = func(set func(key, value string)) {
		netguard.WriteHeaders(netguard.Phone, set)
	}
	os.Exit(m.Run())
}
