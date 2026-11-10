package remap

import "testing"

// SEC-01: the root helper must not hand its privileged commands to every
// process that can reach the group-writable socket. A peer that grabs the
// keyboard (capture_start) or rewrites /etc mappings (apply) can keylog.
func TestHelperSocketPeerAuth(t *testing.T) {
	const agentUID = 1000
	cases := []struct {
		name string
		uid  int32
		cmd  string
		want bool
	}{
		{"root may apply", 0, "apply", true},
		{"root may set devices", 0, "set_devices", true},
		{"root may capture", 0, "capture_start", true},
		{"root may status", 0, "status", true},
		{"root may inject", 0, "inject", true},

		{"agent may apply", agentUID, "apply", true},
		{"agent may set devices", agentUID, "set_devices", true},
		{"agent may capture", agentUID, "capture_start", true},
		{"agent may capture stop", agentUID, "capture_stop", true},
		{"agent may status", agentUID, "status", true},
		{"agent may inject", agentUID, "inject", true},

		{"group peer may only inject", 1001, "inject", true},
		{"group peer may not apply", 1001, "apply", false},
		{"group peer may not set devices", 1001, "set_devices", false},
		{"group peer may not capture", 1001, "capture_start", false},
		{"group peer may not capture stop", 1001, "capture_stop", false},
		{"group peer may not status", 1001, "status", false},

		{"unknown uid may not inject", -1, "inject", false},
		{"unknown uid may not apply", -1, "apply", false},
		{"unknown uid may not capture", -1, "capture_start", false},
		{"unknown uid may not status", -1, "status", false},

		{"unknown command denied for root", 0, "rm -rf", false},
		{"unknown command denied for agent", agentUID, "evil", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := authorizeCommand(tc.uid, agentUID, tc.cmd); got != tc.want {
				t.Fatalf("authorizeCommand(%d, %d, %q) = %v, want %v", tc.uid, agentUID, tc.cmd, got, tc.want)
			}
		})
	}
}

// Without a configured agent uid only root may run privileged commands;
// inject stays open to peers that passed the socket's group check.
func TestHelperSocketPeerAuthWithoutAgentUID(t *testing.T) {
	if !authorizeCommand(0, 0, "apply") {
		t.Fatal("root must stay trusted without a configured agent uid")
	}
	if authorizeCommand(1000, 0, "apply") {
		t.Fatal("no agent uid configured: privileged command must require root")
	}
	if !authorizeCommand(1000, 0, "inject") {
		t.Fatal("inject must stay open to group peers")
	}
}
