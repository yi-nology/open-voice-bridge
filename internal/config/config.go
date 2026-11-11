// Package config loads and validates the Go agent configuration.
package config

import (
	"encoding/json"
	oeerrors "github.com/oekit/pkg/errors"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/yi-nology/open-voice-bridge/internal/asr"
	"github.com/yi-nology/open-voice-bridge/internal/core"
	"github.com/yi-nology/open-voice-bridge/internal/device"
	"github.com/yi-nology/open-voice-bridge/internal/llm"
	"github.com/yi-nology/open-voice-bridge/internal/remap"
	"github.com/yi-nology/open-voice-bridge/internal/text"
)

type KeymapConfig struct {
	Profile  string                       `json:"profile"`
	Profiles map[string]map[string]string `json:"profiles"`
	AppRules []AppRule                    `json:"app_rules,omitempty"`
}

// AppRule binds a focused window class (X11 WM_CLASS substring) to a keymap
// profile so the runtime mapping follows the frontmost app (SayAll 切得准).
type AppRule struct {
	Class   string `json:"class"`
	Profile string `json:"profile"`
}

type DeviceConfig struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Address   string   `json:"address"`
	Profile   string   `json:"profile"`
	Enabled   bool     `json:"enabled"`
	Vendor    string   `json:"vendor,omitempty"`
	Product   string   `json:"product,omitempty"`
	NameMatch []string `json:"name_match,omitempty"`
}

// PhoneConfig gates the standalone phone input listener (mic + virtual keys).
// It binds a separate LAN address from the loopback-only management UI.
type PhoneConfig struct {
	Enabled             bool   `json:"enabled"`
	Addr                string `json:"addr"`
	TLS                 string `json:"tls"` // auto | off (off = trusted HTTPS in front)
	PairCodeTTLSeconds  int    `json:"pair_code_ttl_seconds"`
	MaxClients          int    `json:"max_clients"`
	MaxUtteranceSeconds int    `json:"max_utterance_seconds"`
}

type Config struct {
	Address      string         `json:"address"`
	ActiveDevice string         `json:"active_device"`
	Devices      []DeviceConfig `json:"devices"`
	APIKeyFile   string         `json:"api_key_file"`
	ASRModel     string         `json:"asr_model"`
	ASRLanguage  string         `json:"asr_language"`
	ASRBaseURL   string         `json:"asr_base_url"`
	SinkName     string         `json:"sink_name"`
	SourceName   string         `json:"source_name"`
	UIAddr       string         `json:"ui_addr"`
	HelperSocket string         `json:"helper_socket"`
	KeydConfig   string         `json:"keyd_config"`
	MaxUtterance int            `json:"max_utterance_seconds"`
	PasteEnabled bool           `json:"paste_enabled"`
	ASR          asr.Config     `json:"asr"`
	LLM          llm.Config     `json:"llm"`
	Text         text.Config    `json:"text"`
	Keymap       KeymapConfig   `json:"keymap"`
	Phone        PhoneConfig    `json:"phone"`
}

var macPattern = regexp.MustCompile(`^([0-9A-Fa-f]{2}:){5}[0-9A-Fa-f]{2}$`)

func DefaultProfiles() map[string]map[string]string {
	coding := remap.Copy(remap.DefaultMapping)
	media := map[string]string{
		"power":      "esc",
		"f5":         "f24",
		"up":         "native",
		"down":       "native",
		"left":       "native",
		"right":      "native",
		"enter":      "playpause",
		"back":       "esc",
		"home":       "nextsong",
		"compose":    "previoussong",
		"grave":      "mute",
		"volumeup":   "native",
		"volumedown": "native",
	}
	minimal := map[string]string{
		"power":      "esc",
		"f5":         "f24",
		"up":         "native",
		"down":       "native",
		"left":       "native",
		"right":      "native",
		"enter":      "native",
		"back":       "esc",
		"home":       "native",
		"compose":    "native",
		"grave":      "noop",
		"volumeup":   "native",
		"volumedown": "native",
	}
	return map[string]map[string]string{
		"coding":  coding,
		"media":   media,
		"minimal": minimal,
	}
}

func Default() Config {
	home, _ := os.UserHomeDir()
	cfg := Config{
		Address:      "AA:BB:CC:DD:EE:FF",
		APIKeyFile:   filepath.Join(home, ".config", "xiaomi-mimo", "api_key"),
		ASRModel:     "mimo-v2.5-asr",
		ASRLanguage:  "auto",
		SinkName:     "xiaomi_mic_sink",
		SourceName:   "xiaomi_mic",
		UIAddr:       "127.0.0.1:8787",
		HelperSocket: "/run/xiaomi-remote-helper.sock",
		KeydConfig:   "/etc/keyd/xiaomi.conf",
		MaxUtterance: core.DefaultUtteranceSeconds,
		PasteEnabled: true,
	}
	cfg.ASR = asr.DefaultConfig()
	cfg.ASR.Providers["mimo"] = asr.ProviderConfig{
		Type:       "mimo",
		APIKeyFile: cfg.APIKeyFile,
		Model:      cfg.ASRModel,
		Language:   cfg.ASRLanguage,
	}
	cfg.Keymap = KeymapConfig{
		Profile:  "coding",
		Profiles: DefaultProfiles(),
	}
	cfg.LLM = llm.DefaultConfig()
	cfg.Phone = PhoneConfig{
		Enabled:             false,
		Addr:                "0.0.0.0:8788",
		TLS:                 "auto",
		PairCodeTTLSeconds:  300,
		MaxClients:          4,
		MaxUtteranceSeconds: core.DefaultUtteranceSeconds,
	}
	cfg.Devices = []DeviceConfig{{
		ID:      "rc003",
		Name:    "Xiaomi Bluetooth Remote",
		Address: cfg.Address,
		Profile: "rc003",
		Enabled: true,
	}}
	cfg.ActiveDevice = "rc003"
	return cfg
}

func (c *Config) normalizeDevices() {
	if len(c.Devices) == 0 {
		c.Devices = []DeviceConfig{{
			ID:      "rc003",
			Name:    "Xiaomi Bluetooth Remote",
			Address: c.Address,
			Profile: "rc003",
			Enabled: true,
		}}
	}
	seen := map[string]bool{}
	for index := range c.Devices {
		item := &c.Devices[index]
		if strings.TrimSpace(item.ID) == "" {
			item.ID = "device-" + strconv.Itoa(index+1)
		}
		item.ID = strings.TrimSpace(item.ID)
		item.Name = strings.TrimSpace(item.Name)
		item.Profile = strings.TrimSpace(item.Profile)
		if item.Profile == "" {
			item.Profile = "rc003"
		}
		if profile, ok := device.Builtin().Get(item.Profile); ok {
			item.Profile = profile.ID
		}
		if item.Name == "" {
			if profile, ok := device.Builtin().Get(item.Profile); ok {
				item.Name = profile.Name
			} else {
				item.Name = item.ID
			}
		}
		if seen[item.ID] {
			item.ID = item.ID + "-" + strconv.Itoa(index+1)
		}
		seen[item.ID] = true
	}
	if c.ActiveDevice == "" {
		c.ActiveDevice = c.Devices[0].ID
	}
	activeIndex := -1
	for index, item := range c.Devices {
		if item.ID == c.ActiveDevice {
			activeIndex = index
			break
		}
	}
	if activeIndex < 0 || !c.Devices[activeIndex].Enabled {
		for index, item := range c.Devices {
			if item.Enabled {
				activeIndex = index
				c.ActiveDevice = item.ID
				break
			}
		}
	}
	if activeIndex >= 0 {
		c.Address = c.Devices[activeIndex].Address
	}
}

// DeviceName returns the display name of the active device, falling back to
// its address when no friendly name is configured.
func (c Config) DeviceName() string {
	for _, item := range c.Devices {
		if item.ID == c.ActiveDevice {
			if item.Name != "" {
				return item.Name
			}
			return item.Address
		}
	}
	return c.Address
}

func DefaultPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = "."
	}
	return filepath.Join(dir, "xiaomi-remote", "config.json")
}

func Load(path string) (Config, error) {
	cfg := Default()
	if path == "" {
		path = DefaultPath()
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		if err := writeDefault(path, cfg); err != nil {
			return cfg, err
		}
		return cfg, cfg.Validate()
	}
	if err != nil {
		return cfg, err
	}
	// Maps merge on unmarshal; clear them so the file is authoritative.
	cfg.ASR.Providers = nil
	cfg.Keymap.Profiles = nil
	cfg.Devices = nil
	cfg.ActiveDevice = ""
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, oeerrors.Errorf("parse config: %w", err)
	}
	cfg.normalizeASR()
	cfg.normalizeLLM()
	cfg.normalizeText()
	cfg.normalizeKeymap()
	cfg.normalizePhone()
	cfg.normalizeDevices()
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return cfg, oeerrors.Errorf("parse config: %w", err)
	}
	_, hasASR := raw["asr"]
	_, hasKeymap := raw["keymap"]
	_, hasDevices := raw["devices"]
	_, hasLLM := raw["llm"]
	_, hasPhone := raw["phone"]
	if !hasASR || !hasKeymap || !hasDevices || !hasLLM || !hasPhone {
		// Migrate older configs so the new sections are visible/editable.
		if err := writeConfig(path, cfg); err != nil {
			return cfg, oeerrors.Errorf("migrate config: %w", err)
		}
	}
	if err := cfg.Validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func (c *Config) normalizeText() {
	if c.Text.Dictionary == nil {
		c.Text.Dictionary = []text.Replacement{}
	}
	if c.Text.Snippets == nil {
		c.Text.Snippets = []text.Snippet{}
	}
}

func (c *Config) normalizeLLM() {
	if c.LLM.Provider == "" {
		c.LLM.Provider = "minimax"
	}
	if preset, ok := llm.PresetByID(c.LLM.Provider); ok {
		if c.LLM.BaseURL == "" {
			c.LLM.BaseURL = preset.BaseURL
		}
		if c.LLM.Model == "" && preset.Model != "" {
			c.LLM.Model = preset.Model
		}
	}
	if c.LLM.Mode == "" {
		c.LLM.Mode = "cleanup"
	}
	if c.LLM.TimeoutSeconds <= 0 {
		c.LLM.TimeoutSeconds = 20
	}
}

func (c *Config) normalizePhone() {
	if strings.TrimSpace(c.Phone.Addr) == "" {
		c.Phone.Addr = "0.0.0.0:8788"
	}
	if c.Phone.TLS == "" {
		c.Phone.TLS = "auto"
	}
	if c.Phone.PairCodeTTLSeconds == 0 {
		c.Phone.PairCodeTTLSeconds = 300
	}
	if c.Phone.MaxClients == 0 {
		c.Phone.MaxClients = 4
	}
	if c.Phone.MaxUtteranceSeconds == 0 {
		c.Phone.MaxUtteranceSeconds = core.DefaultUtteranceSeconds
	}
}

func (c *Config) normalizeKeymap() {
	defaults := DefaultProfiles()
	if c.Keymap.Profiles == nil {
		c.Keymap.Profiles = map[string]map[string]string{}
	}
	for name, mapping := range defaults {
		if _, ok := c.Keymap.Profiles[name]; !ok {
			c.Keymap.Profiles[name] = mapping
		}
	}
	if c.Keymap.Profile == "" {
		c.Keymap.Profile = "coding"
	}
	if _, ok := c.Keymap.Profiles[c.Keymap.Profile]; !ok {
		c.Keymap.Profile = "coding"
	}
}

func (c *Config) normalizeASR() {
	if c.ASR.Provider == "" {
		c.ASR.Provider = "mimo"
	}
	if c.ASR.Language == "" {
		c.ASR.Language = c.ASRLanguage
		if c.ASR.Language == "" {
			c.ASR.Language = "auto"
		}
	}
	if c.ASR.Providers == nil {
		c.ASR.Providers = map[string]asr.ProviderConfig{}
	}
	mimo, ok := c.ASR.Providers["mimo"]
	if !ok {
		referenced := c.ASR.Provider == "mimo"
		for _, name := range c.ASR.Fallback {
			if name == "mimo" {
				referenced = true
			}
		}
		if !referenced {
			return
		}
		mimo = asr.ProviderConfig{Type: "mimo"}
	}
	if mimo.Type == "" {
		mimo.Type = "mimo"
	}
	if mimo.APIKeyFile == "" {
		mimo.APIKeyFile = c.APIKeyFile
	}
	if mimo.Model == "" {
		mimo.Model = c.ASRModel
	}
	if mimo.Language == "" {
		mimo.Language = c.ASRLanguage
	}
	if mimo.BaseURL == "" {
		mimo.BaseURL = c.ASRBaseURL
	}
	c.ASR.Providers["mimo"] = mimo
}

func (c Config) Validate() error {
	if !macPattern.MatchString(c.Address) {
		return oeerrors.Errorf("invalid Bluetooth address %q", c.Address)
	}
	host, port, err := net.SplitHostPort(c.UIAddr)
	if err != nil {
		return oeerrors.Errorf("invalid ui_addr: %w", err)
	}
	switch host {
	case "127.0.0.1", "localhost", "::1":
	default:
		return oeerrors.Errorf("ui_addr must bind to loopback, got %q", host)
	}
	if value, err := strconv.Atoi(port); err != nil || value < 1 || value > 65535 {
		return oeerrors.Errorf("invalid ui_addr port %q", port)
	}
	if c.MaxUtterance < core.MinUtteranceSeconds || c.MaxUtterance > core.MaxUtteranceSeconds {
		return oeerrors.Errorf("max_utterance_seconds must be %d..%d", core.MinUtteranceSeconds, core.MaxUtteranceSeconds)
	}
	if strings.TrimSpace(c.HelperSocket) == "" {
		return oeerrors.Errorf("helper_socket is required")
	}
	if err := asr.Validate(c.ASR); err != nil {
		return oeerrors.Wrap(err, "asr")
	}
	if err := text.Validate(c.Text); err != nil {
		return oeerrors.Wrap(err, "text")
	}
	if c.LLM.Enabled {
		if strings.TrimSpace(c.LLM.BaseURL) == "" {
			return oeerrors.New("llm.base_url is required when enabled")
		}
		if strings.TrimSpace(c.LLM.Model) == "" {
			return oeerrors.New("llm.model is required when enabled")
		}
	}
	if len(c.Keymap.Profiles) == 0 {
		return oeerrors.New("keymap profiles are empty")
	}
	for name, mapping := range c.Keymap.Profiles {
		if strings.TrimSpace(name) == "" {
			return oeerrors.New("keymap profile name is empty")
		}
		if err := remap.Validate(mapping); err != nil {
			return oeerrors.Wrap(err, "keymap profile "+name)
		}
	}
	if _, ok := c.Keymap.Profiles[c.Keymap.Profile]; !ok {
		return oeerrors.Errorf("keymap profile %q is not defined", c.Keymap.Profile)
	}
	for _, rule := range c.Keymap.AppRules {
		if strings.TrimSpace(rule.Class) == "" {
			return oeerrors.New("keymap app_rules entry has empty class")
		}
		if _, ok := c.Keymap.Profiles[rule.Profile]; !ok {
			return oeerrors.Errorf("keymap app_rules %q: profile %q is not defined", rule.Class, rule.Profile)
		}
	}
	if len(c.Devices) == 0 {
		return oeerrors.New("devices are empty")
	}
	seenDevices := map[string]bool{}
	for _, item := range c.Devices {
		if item.ID == "" {
			return oeerrors.New("device id is empty")
		}
		if seenDevices[item.ID] {
			return oeerrors.Errorf("duplicate device id %q", item.ID)
		}
		seenDevices[item.ID] = true
		if !macPattern.MatchString(item.Address) {
			return oeerrors.Errorf("device %q: invalid Bluetooth address %q", item.ID, item.Address)
		}
		if _, ok := device.Builtin().Get(item.Profile); !ok {
			return oeerrors.Errorf("device %q: unknown profile %q", item.ID, item.Profile)
		}
	}
	if !seenDevices[c.ActiveDevice] {
		return oeerrors.Errorf("active device %q is not defined", c.ActiveDevice)
	}
	activeEnabled := false
	for _, item := range c.Devices {
		if item.ID == c.ActiveDevice && item.Enabled {
			activeEnabled = true
		}
	}
	if !activeEnabled {
		return oeerrors.Errorf("active device %q is disabled", c.ActiveDevice)
	}
	if err := c.validatePhone(); err != nil {
		return err
	}
	return nil
}

// validatePhone enforces the separation between the loopback management UI and
// the standalone phone listener. It runs whether or not phone input is enabled
// so a bad section can never be persisted silently and only explode when the
// user later flips the switch (DOM-08).
func (c Config) validatePhone() error {
	phoneHost, phonePort, err := net.SplitHostPort(c.Phone.Addr)
	if err != nil {
		return oeerrors.Errorf("invalid phone.addr: %w", err)
	}
	if value, err := strconv.Atoi(phonePort); err != nil || value < 1 || value > 65535 {
		return oeerrors.Errorf("invalid phone.addr port %q", phonePort)
	}
	_, uiPort, err := net.SplitHostPort(c.UIAddr)
	if err == nil && phonePort == uiPort {
		return oeerrors.Errorf("phone.addr must use a different port than ui_addr")
	}
	switch c.Phone.TLS {
	case "auto", "off":
	default:
		return oeerrors.Errorf("phone.tls must be auto or off, got %q", c.Phone.TLS)
	}
	// SEC-03: tls=off means pairing codes and long-lived tokens travel in
	// cleartext, which is only tolerable on loopback.
	if c.Phone.TLS == "off" && !isLoopbackHost(phoneHost) {
		return oeerrors.Errorf(
			"phone.tls=off requires a loopback phone.addr, got %q; set phone.tls to auto or bind the address to 127.0.0.1/localhost",
			c.Phone.Addr)
	}
	if c.Phone.PairCodeTTLSeconds < 30 || c.Phone.PairCodeTTLSeconds > 3600 {
		return oeerrors.Errorf("phone.pair_code_ttl_seconds must be 30..3600")
	}
	if c.Phone.MaxClients < 1 || c.Phone.MaxClients > 16 {
		return oeerrors.Errorf("phone.max_clients must be 1..16")
	}
	if c.Phone.MaxUtteranceSeconds < core.MinUtteranceSeconds || c.Phone.MaxUtteranceSeconds > core.MaxUtteranceSeconds {
		return oeerrors.Errorf("phone.max_utterance_seconds must be %d..%d", core.MinUtteranceSeconds, core.MaxUtteranceSeconds)
	}
	return nil
}

// isLoopbackHost reports whether a phone.addr host is loopback: the literal
// localhost name or any address in 127.0.0.0/8 / ::1. Unspecified addresses
// (0.0.0.0, [::]), LAN IPs and other hostnames are not.
func isLoopbackHost(host string) bool {
	if strings.EqualFold(strings.TrimSpace(host), "localhost") {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

func Save(path string, cfg Config) error {
	cfg.normalizeASR()
	cfg.normalizeLLM()
	cfg.normalizeText()
	cfg.normalizeKeymap()
	cfg.normalizePhone()
	cfg.normalizeDevices()
	if err := cfg.Validate(); err != nil {
		return err
	}
	if path == "" {
		path = DefaultPath()
	}
	return writeConfig(path, cfg)
}

func writeConfig(path string, cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func writeDefault(path string, cfg Config) error {
	cfg.normalizeASR()
	cfg.normalizeKeymap()
	cfg.normalizePhone()
	return writeConfig(path, cfg)
}
