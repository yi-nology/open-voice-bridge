// Package appconfig is config plus the semantics only a subsystem knows: the
// ASR provider catalogue, the keymap vocabulary, the LLM presets and the device
// profile table. config itself stays a leaf (R1), and every load and save goes
// through here so the gate cannot be forgotten.
package appconfig

import (
	oeerrors "github.com/oekit/pkg/errors"

	"github.com/yi-nology/open-voice-bridge/internal/asr"
	"github.com/yi-nology/open-voice-bridge/internal/config"
	"github.com/yi-nology/open-voice-bridge/internal/device"
	"github.com/yi-nology/open-voice-bridge/internal/llm"
	"github.com/yi-nology/open-voice-bridge/internal/remap"
	"github.com/yi-nology/open-voice-bridge/internal/text"
)

// Load reads the file, then applies the subsystem defaults and checks.
func Load(path string) (config.Config, error) {
	cfg, err := config.Load(path)
	if err != nil {
		return cfg, err
	}
	Normalize(&cfg)
	return cfg, Validate(cfg)
}

// Save normalises and validates before writing, so a rejected document leaves
// the previous file exactly as it was.
func Save(path string, cfg config.Config) error {
	Normalize(&cfg)
	if err := Validate(cfg); err != nil {
		return err
	}
	return config.Save(path, cfg)
}

// Normalize fills the defaults that come from a subsystem's own catalogue.
func Normalize(cfg *config.Config) {
	if preset, ok := llm.PresetByID(cfg.LLM.Provider); ok {
		if cfg.LLM.BaseURL == "" {
			cfg.LLM.BaseURL = preset.BaseURL
		}
		if cfg.LLM.Model == "" && preset.Model != "" {
			cfg.LLM.Model = preset.Model
		}
	}
	for index := range cfg.Devices {
		item := &cfg.Devices[index]
		profile, ok := device.Builtin().Get(item.Profile)
		if !ok {
			continue
		}
		item.Profile = profile.ID
		if item.Name == "" {
			item.Name = profile.Name
		}
	}
}

// Validate rejects what a subsystem would refuse when the runtime builds it.
func Validate(cfg config.Config) error {
	if err := asr.Validate(cfg.ASR); err != nil {
		return oeerrors.Wrap(err, "asr")
	}
	if err := text.Validate(cfg.Text); err != nil {
		return oeerrors.Wrap(err, "text")
	}
	for name, mapping := range cfg.Keymap.Profiles {
		if err := remap.Validate(mapping); err != nil {
			return oeerrors.Wrap(err, "keymap profile "+name)
		}
	}
	for _, item := range cfg.Devices {
		if _, ok := device.Builtin().Get(item.Profile); !ok {
			return oeerrors.Errorf("device %q: unknown profile %q", item.ID, item.Profile)
		}
	}
	return nil
}
