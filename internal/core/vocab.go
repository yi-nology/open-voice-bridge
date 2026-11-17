package core

// Shared vocabulary for the engine/keymap/token surfaces (core 类型迁移,
// ARC-09 记账①): these types used to live inside their subsystems, which
// made the state owner import concrete implementations just to name them.
// They are pure data and pure functions, so they belong on the foundation
// (R1); behaviour that actually talks to a provider, the OS or a file stays
// in its subsystem and is injected instead.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// --- ASR -------------------------------------------------------------------

// ASRResult is one transcription attempt's outcome.
type ASRResult struct {
	Text     string
	Language string
	Provider string
	Latency  time.Duration
}

// DefaultWakeWord is what the S7 listener hears before it captures. The
// config normaliser fills it in and the agent falls back to the same word.
const DefaultWakeWord = "小遥小遥"

// ASRAudio is the PCM one transcription consumes.
type ASRAudio struct {
	Samples    []int16
	SampleRate int
}

// ASREngine is the transcriber slice every input source shares.
type ASREngine interface {
	Name() string
	Local() bool
	Transcribe(ctx context.Context, audio ASRAudio) (ASRResult, error)
	Close(ctx context.Context) error
}

// ASRDiagnostic is one line of the ASR self-check report.
type ASRDiagnostic struct {
	Provider string
	OK       bool
	Detail   string
}

// --- LLM -------------------------------------------------------------------

// LLMProcessor is the optional post-processor that rewrites transcripts.
type LLMProcessor interface {
	Name() string
	Process(ctx context.Context, text string) (string, error)
	Ping(ctx context.Context) (time.Duration, error)
	Close(ctx context.Context) error
}

// LLMDiagnostic is one line of the LLM self-check report.
type LLMDiagnostic struct {
	OK     bool
	Detail string
}

// --- Text ------------------------------------------------------------------

// TextRules is the dictionary/snippets/commands ruleset the chain applies
// after ASR; *text.Rules implements it.
type TextRules interface {
	Command(utterance string) (string, bool)
	SnippetAt(slot int) (string, bool)
	Snippet(input string) (string, bool)
	Dictionary(input string) string
}

// --- Device ----------------------------------------------------------------

// DeviceMatch is the kernel-level identity a profile matches on.
type DeviceMatch struct {
	Vendor  string   `json:"vendor"`
	Product string   `json:"product"`
	Names   []string `json:"names"`
}

// DeviceProfile is one known remote's template.
type DeviceProfile struct {
	ID           string      `json:"id"`
	Name         string      `json:"name"`
	Transport    string      `json:"transport"`
	Capabilities []string    `json:"capabilities"`
	Models       []string    `json:"models,omitempty"`
	Aliases      []string    `json:"aliases,omitempty"`
	Match        DeviceMatch `json:"match"`
}

// HasCapability reports whether the profile advertises the capability.
func (p DeviceProfile) HasCapability(capability string) bool {
	for _, item := range p.Capabilities {
		if item == capability {
			return true
		}
	}
	return false
}

// Validate checks the profile has the identity a matcher needs.
func (p DeviceProfile) Validate() error {
	if strings.TrimSpace(p.ID) == "" {
		return errors.New("device profile id is required")
	}
	if strings.TrimSpace(p.Name) == "" {
		return fmt.Errorf("device profile %q: name is required", p.ID)
	}
	if strings.TrimSpace(p.Transport) == "" {
		return fmt.Errorf("device profile %q: transport is required", p.ID)
	}
	return nil
}

// MatchInputBlock reports whether a /proc/bus/input/devices block belongs to
// this profile. Vendor/product matching wins even when BlueZ renames the
// device to a generic "bluez-hog-device".
func (p DeviceProfile) MatchInputBlock(block string) bool {
	lower := strings.ToLower(block)
	vendor := strings.ToLower(strings.TrimSpace(p.Match.Vendor))
	product := strings.ToLower(strings.TrimSpace(p.Match.Product))
	if vendor != "" && product != "" &&
		strings.Contains(lower, "vendor="+vendor) &&
		strings.Contains(lower, "product="+product) {
		return true
	}
	for _, name := range p.Match.Names {
		if name != "" && strings.Contains(block, `Name="`+name+`"`) {
			return true
		}
	}
	return false
}

// DeviceCatalog is the builtin profile registry's lookup surface.
type DeviceCatalog interface {
	Get(id string) (DeviceProfile, bool)
	SuggestProfile(name string) (DeviceProfile, bool)
	MatchIDs(vendor, product string) (DeviceProfile, bool)
	MatchName(name string) (DeviceProfile, bool)
}

// --- Keymap ----------------------------------------------------------------

// AgentAction is a deferred step the agent executes on the user side (S3).
type AgentAction struct {
	Kind string // "type" | "open"
	Arg  string
}

// CopyMapping clones a keymap so callers can mutate the copy.
func CopyMapping(mapping map[string]string) map[string]string {
	out := make(map[string]string, len(mapping))
	for key, value := range mapping {
		out[key] = value
	}
	return out
}

// MatchWindowClass reports whether a focused window class matches a
// per-app rule: case-insensitive substring, so "firefox" finds
// "Navigator/firefox".
func MatchWindowClass(class, pattern string) bool {
	class = strings.TrimSpace(class)
	pattern = strings.TrimSpace(pattern)
	if class == "" || pattern == "" {
		return false
	}
	return strings.Contains(strings.ToLower(class), strings.ToLower(pattern))
}

// --- Tokens ----------------------------------------------------------------

// TokenInfo is one MCP client grant's public face.
type TokenInfo struct {
	ID        string    `json:"id"`
	Label     string    `json:"label"`
	CreatedAt time.Time `json:"created_at"`
	LastUsed  time.Time `json:"last_used,omitempty"`
	Revoked   bool      `json:"revoked,omitempty"`
}

// AuditEntry is one counted agent query (never speech, never search text).
type AuditEntry struct {
	At      time.Time `json:"at"`
	TokenID string    `json:"token_id"`
	Label   string    `json:"label"`
	Tool    string    `json:"tool"`
	Items   int       `json:"items"`
}

// TokenStore is the grant store's surface as the state owner needs it.
type TokenStore interface {
	Create(label string) (id, raw string, err error)
	List() []TokenInfo
	Revoke(id string) error
	Validate(raw string) (TokenInfo, bool)
}

// MCPServerStanza is one stdio server entry in the MCP client config format
// (the ecosystem's shape: command + args).
type MCPServerStanza struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

// ClientConfig is the stdio MCP stanza a client config needs. The server
// table is keyed by server name (the label) — the one free-format seam the
// MCP ecosystem's config format leaves us (ARC-11 棘轮文档点位).
type ClientConfig struct {
	MCPServers map[string]MCPServerStanza `json:"mcpServers"`
}

// ClientConfigSnippet renders the stdio MCP stanza a client config needs.
func ClientConfigSnippet(label, binary, token string) ClientConfig {
	if label == "" {
		label = "xiaoyao"
	}
	return ClientConfig{MCPServers: map[string]MCPServerStanza{
		label: {Command: binary, Args: []string{"mcp", "--token", token}},
	}}
}
