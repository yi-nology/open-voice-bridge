package core

// Dependency is one host prerequisite the diagnostics report on. The
// classification is shared by /readyz and the offline doctor so the two cannot
// disagree about what dictation needs: doctor exits non-zero exactly when
// readyz's ok is false (OPS-04).
//
// Only the ASR chain is core. The root remapper, the Bluetooth remote and the
// virtual mic are gaps a phone-only setup never fills; the paste tooling, the
// phone listener and the admin UI are host-side extras readyz has no probe for.
// This is OPS-06's provisional yardstick (roadmap 未验证 #5).
type Dependency string

const (
	DepASR    Dependency = "asr"
	DepHelper Dependency = "helper"
	DepRemote Dependency = "remote"
	DepBLE    Dependency = "ble"
	DepMic    Dependency = "mic"
	DepPaste  Dependency = "paste"
	DepPhone  Dependency = "phone"
	DepAdmin  Dependency = "admin"
)

// Core reports whether dictation cannot work without this dependency. It is
// the rule ReadyView.CoreOK and ReadyView.OK follow.
func (d Dependency) Core() bool { return d == DepASR }
