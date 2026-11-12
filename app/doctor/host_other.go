//go:build !linux && !darwin

package doctor

import "github.com/yi-nology/open-voice-bridge/internal/config"

func hostChecks(cfg config.Config) []check {
	return []check{warn("host checks not implemented on this platform", "")}
}
