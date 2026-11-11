// Package fsutil is the only place that writes files durably (R7): one
// atomic replace (tmp + fsync + rename), one JSONL append and one JSONL
// rewrite. No other package assembles tmp+rename, so a crash can never leave
// a half-written file where a complete one is expected (DOM-14).
package fsutil

import (
	"bytes"
	"encoding/json"
	"os"
)

// WriteAtomic replaces path with data in one durable step: the payload goes
// to a sibling tmp file, is fsynced, and only then renamed over path. perm
// applies when the file is created; the parent directory must already exist.
func WriteAtomic(path string, data []byte, perm os.FileMode) error {
	tmp := path + ".tmp"
	file, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, perm)
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// AppendJSONL marshals entry as one JSON line and appends it to path with a
// trailing newline, creating the file 0600. The line is fsynced so an
// acknowledged entry survives a crash.
func AppendJSONL(path string, entry any) error {
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(append(data, '\n')); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

// RewriteJSONL replaces path with the JSONL encoding of entries, durably.
func RewriteJSONL[T any](path string, entries []T) error {
	var buf bytes.Buffer
	for _, entry := range entries {
		data, err := json.Marshal(entry)
		if err != nil {
			return err
		}
		buf.Write(data)
		buf.WriteByte('\n')
	}
	return WriteAtomic(path, buf.Bytes(), 0o600)
}
