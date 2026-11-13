//go:build linux

package remap

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/yi-nology/open-voice-bridge/internal/fsutil"
)

// Key counts ("按键次数统计"): presses are counted at the single applyKey
// funnel — physical evdev events and phone injection alike — and persisted
// next to the mapping so the lifetime total survives a daemon restart.
// value==1 counts once per press; repeats and releases do not.

const keyCountsFile = "keycounts.json"

func (d *Daemon) loadKeyCounts() {
	d.keyMu.Lock()
	defer d.keyMu.Unlock()
	if d.keyCounts == nil {
		d.keyCounts = map[string]int64{}
	}
	data, err := os.ReadFile(d.keyCountsPath())
	if err != nil {
		return
	}
	var saved struct {
		Total int64            `json:"total"`
		Per   map[string]int64 `json:"per"`
	}
	if err := json.Unmarshal(data, &saved); err != nil {
		return
	}
	d.keyTotal = saved.Total
	for name, count := range saved.Per {
		d.keyCounts[name] = count
	}
}

func (d *Daemon) keyCountsPath() string {
	return filepath.Join(filepath.Dir(d.cfg.MappingPath), keyCountsFile)
}

// countPress records one key press. It returns true when a flush is due.
func (d *Daemon) countPress(name string) bool {
	d.keyMu.Lock()
	defer d.keyMu.Unlock()
	d.keyTotal++
	if d.keyCounts == nil {
		d.keyCounts = map[string]int64{}
	}
	d.keyCounts[name]++
	d.keyDirty = true
	d.keySince++
	if d.keySince >= 50 {
		d.keySince = 0
		return true
	}
	return false
}

func (d *Daemon) flushKeyCounts() error {
	d.keyMu.Lock()
	snapshot := struct {
		Total int64            `json:"total"`
		Per   map[string]int64 `json:"per"`
	}{Total: d.keyTotal, Per: map[string]int64{}}
	for name, count := range d.keyCounts {
		snapshot.Per[name] = count
	}
	dirty := d.keyDirty
	d.keyDirty = false
	d.keyMu.Unlock()
	if !dirty {
		return nil
	}
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	path := d.keyCountsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return fsutil.WriteAtomic(path, append(data, '\n'), 0o600)
}

// keySnapshot renders the counts for the status response.
func (d *Daemon) keySnapshot() (int64, map[string]int64) {
	d.keyMu.Lock()
	defer d.keyMu.Unlock()
	per := make(map[string]int64, len(d.keyCounts))
	for name, count := range d.keyCounts {
		per[name] = count
	}
	return d.keyTotal, per
}
