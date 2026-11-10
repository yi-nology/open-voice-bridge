package remap

// Commands that change or observe input ownership on behalf of the desktop
// agent: rewriting the /etc mapping, grabbing the evdev keyboard (capture) or
// reading helper status. Only root and the agent uid may run them.
const (
	cmdInject       = "inject"
	cmdApply        = "apply"
	cmdSetDevices   = "set_devices"
	cmdCaptureStart = "capture_start"
	cmdCaptureStop  = "capture_stop"
	cmdStatus       = "status"
)

// authorizeCommand reports whether a peer credential may run a helper command
// (SEC-01). The Unix socket is group-writable, so connecting proves group
// membership and nothing more: privileged commands (apply / set_devices /
// capture / status) are limited to root and the configured agent uid, while
// inject stays open to any peer that passed the socket check because its key
// list is whitelisted. An unknown uid (-1) is treated as unauthenticated.
func authorizeCommand(peerUID int32, agentUID int, cmd string) bool {
	if peerUID < 0 {
		return false
	}
	switch cmd {
	case cmdInject:
		return true
	case cmdApply, cmdSetDevices, cmdCaptureStart, cmdCaptureStop, cmdStatus:
		return peerUID == 0 || (agentUID > 0 && int(peerUID) == agentUID)
	default:
		return false
	}
}
