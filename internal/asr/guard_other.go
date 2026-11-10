//go:build !unix

package asr

import "os"

func fileOwnerUID(info os.FileInfo) (int, bool) { return 0, false }
