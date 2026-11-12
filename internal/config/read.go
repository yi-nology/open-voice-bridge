package config

import (
	"encoding/json"
	"os"

	oeerrors "github.com/oekit/pkg/errors"
)

// Read parses and validates without creating or migrating anything. An absent
// file is reported as os.ErrNotExist so the caller can say "not configured"
// instead of writing defaults on the user's behalf.
func Read(path string) (Config, error) {
	cfg, state, err := readConfig(path)
	if err != nil {
		return cfg, err
	}
	if state == stateMissing {
		if path == "" {
			path = DefaultPath()
		}
		return cfg, &os.PathError{Op: "read", Path: path, Err: os.ErrNotExist}
	}
	if err := cfg.Validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

type loadState int

const (
	statePresent loadState = iota
	stateMissing
	stateOutdated
)

// readConfig parses and normalises the document. It never writes; the state
// tells Load which write, if any, the document still needs.
func readConfig(path string) (Config, loadState, error) {
	cfg := Default()
	if path == "" {
		path = DefaultPath()
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return cfg, stateMissing, nil
	}
	if err != nil {
		return cfg, statePresent, err
	}
	// Maps merge on unmarshal; clear them so the file is authoritative.
	cfg.ASR.Providers = nil
	cfg.Keymap.Profiles = nil
	cfg.Devices = nil
	cfg.ActiveDevice = ""
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, statePresent, oeerrors.Errorf("parse config: %w", err)
	}
	cfg.normalizeASR()
	cfg.normalizeLLM()
	cfg.normalizeText()
	cfg.normalizeKeymap()
	cfg.normalizePhone()
	cfg.normalizeDevices()
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return cfg, statePresent, oeerrors.Errorf("parse config: %w", err)
	}
	_, hasASR := raw["asr"]
	_, hasKeymap := raw["keymap"]
	_, hasDevices := raw["devices"]
	_, hasLLM := raw["llm"]
	_, hasPhone := raw["phone"]
	if !hasASR || !hasKeymap || !hasDevices || !hasLLM || !hasPhone {
		return cfg, stateOutdated, nil
	}
	return cfg, statePresent, nil
}
