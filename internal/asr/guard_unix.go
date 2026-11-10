//go:build unix

package asr

import (
	"os"
	"syscall"
)

// fileOwnerUID reports the file owner's uid. The second result is false when
// the platform does not expose ownership through os.FileInfo.
func fileOwnerUID(info os.FileInfo) (int, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return int(stat.Uid), true
}
