// Package config loads and validates the Go agent configuration.
package config

import (
	"encoding/json"
	oeerrors "github.com/oekit/pkg/errors"
	"maps"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/yi-nology/open-voice-bridge/internal/core"
	"github.com/yi-nology/open-voice-bridge/internal/fsutil"
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
	// AllowPowerKey opts in to injecting KEY_POWER from a paired phone. Off by
	// default: logind acts on that key, so it can shut the host down (ARC-12).
	AllowPowerKey bool `json:"allow_power_key"`
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
	// StreamingSubtitles turns on live partial recognition while the voice
	// key is held (S6 边说边滚字): throttled extra ASR calls, opt-in.
	StreamingSubtitles bool `json:"streaming_subtitles"`
	// WakeEnabled turns on the hands-free wake word over the desktop mic (S7).
	WakeEnabled bool `json:"wake_enabled"`
	// WakeWord is what the listener hears before it captures (default 小遥小遥).
	WakeWord string `json:"wake_word"`
	// LookbackEnabled keeps a bounded in-memory ring of the desktop mic so a
	// missed utterance can be recovered by explicit action (S8, 设计稿
	// docs/design/lookback.md). Memory only — never disk, never logs.
	LookbackEnabled bool `json:"lookback_enabled"`
	// LookbackSeconds is the ring window (5..60, default 30).
	LookbackSeconds int          `json:"lookback_seconds"`
	ASR             ASRConfig    `json:"asr"`
	LLM             LLMConfig    `json:"llm"`
	Text            TextConfig   `json:"text"`
	Keymap          KeymapConfig `json:"keymap"`
	Phone           PhoneConfig  `json:"phone"`
}

var macPattern = regexp.MustCompile(`^([0-9A-Fa-f]{2}:){5}[0-9A-Fa-f]{2}$`)

func DefaultProfiles() map[string]map[string]string {
	coding := maps.Clone(DefaultKeymap)
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

// PlaceholderAddress is the example MAC shipped in the defaults. It passes
// Validate's shape check, so anything reporting "configured" must compare
// against it rather than trust the format.
const PlaceholderAddress = "AA:BB:CC:DD:EE:FF"

func Default() Config {
	home, _ := os.UserHomeDir()
	cfg := Config{
		Address:      PlaceholderAddress,
		APIKeyFile:   filepath.Join(home, ".config", "xiaomi-mimo", "api_key"),
		ASRModel:     DefaultASRModel,
		ASRLanguage:  "auto",
		SinkName:     "xiaomi_mic_sink",
		SourceName:   "xiaomi_mic",
		UIAddr:       "127.0.0.1:8787",
		HelperSocket: "/run/xiaomi-remote-helper.sock",
		KeydConfig:   "/etc/keyd/xiaomi.conf",
		MaxUtterance: core.DefaultUtteranceSeconds,
		PasteEnabled: true,
	}
	cfg.ASR = DefaultASR()
	cfg.ASR.Providers["mimo"] = ASRProviderConfig{
		Type:       "mimo",
		APIKeyFile: cfg.APIKeyFile,
		Model:      cfg.ASRModel,
		Language:   cfg.ASRLanguage,
	}
	cfg.Keymap = KeymapConfig{
		Profile:  "coding",
		Profiles: DefaultProfiles(),
	}
	cfg.LLM = DefaultLLM()
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

// Load reads the document, creating defaults when the file is absent and
// migrating it when whole sections are missing. Diagnostics must use Read
// instead: they report on the user's file, they do not rewrite it.
func Load(path string) (Config, error) {
	cfg, state, err := readConfig(path)
	if err != nil {
		return cfg, err
	}
	if path == "" {
		path = DefaultPath()
	}
	switch state {
	case stateMissing:
		if err := writeDefault(path, cfg); err != nil {
			return cfg, err
		}
	case stateOutdated:
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
		c.Text.Dictionary = []Replacement{}
	}
	if c.Text.Snippets == nil {
		c.Text.Snippets = []Snippet{}
	}
}

func (c *Config) normalizeLLM() {
	if c.LLM.Provider == "" {
		c.LLM.Provider = "minimax"
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
		c.ASR.Providers = map[string]ASRProviderConfig{}
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
		mimo = ASRProviderConfig{Type: "mimo"}
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
	// Write-side discipline (#11): Save receives a value copy whose Providers
	// map is still shared with the caller, so an in-place write here rewrote
	// the running configuration behind the config lock. Publish the adjusted
	// table as a new map instead: the caller's map never changes.
	providers := make(map[string]ASRProviderConfig, len(c.ASR.Providers)+1)
	for name, provider := range c.ASR.Providers {
		providers[name] = provider
	}
	providers["mimo"] = mimo
	c.ASR.Providers = providers
}

// Clone returns a deep copy of the document. The persist path serialises a
// snapshot outside the config lock (R8/I3), so no shared map or slice backing
// array may leak into the writer's hands: the normalisers must be free to
// adjust the copy without touching the running state.
func (c Config) Clone() Config {
	out := c
	out.Devices = slices.Clone(c.Devices)
	out.ASR.Fallback = slices.Clone(c.ASR.Fallback)
	if c.ASR.Providers != nil {
		out.ASR.Providers = make(map[string]ASRProviderConfig, len(c.ASR.Providers))
		for name, provider := range c.ASR.Providers {
			provider.Args = slices.Clone(provider.Args)
			out.ASR.Providers[name] = provider
		}
	}
	if c.Keymap.Profiles != nil {
		out.Keymap.Profiles = make(map[string]map[string]string, len(c.Keymap.Profiles))
		for name, mapping := range c.Keymap.Profiles {
			out.Keymap.Profiles[name] = maps.Clone(mapping)
		}
	}
	out.Keymap.AppRules = slices.Clone(c.Keymap.AppRules)
	out.Text.Dictionary = slices.Clone(c.Text.Dictionary)
	out.Text.Snippets = slices.Clone(c.Text.Snippets)
	return out
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
	for name := range c.Keymap.Profiles {
		if strings.TrimSpace(name) == "" {
			return oeerrors.New("keymap profile name is empty")
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
	return fsutil.WriteAtomic(path, append(data, '\n'), 0o600)
}

func writeDefault(path string, cfg Config) error {
	cfg.normalizeASR()
	cfg.normalizeKeymap()
	cfg.normalizePhone()
	return writeConfig(path, cfg)
}
