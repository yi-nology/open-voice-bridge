// Package device defines remote profiles and input matching rules.
package device

import (
	"embed"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

//go:embed profiles/*.json
var builtinFS embed.FS

type Match struct {
	Vendor  string   `json:"vendor"`
	Product string   `json:"product"`
	Names   []string `json:"names"`
}

type Profile struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Transport    string   `json:"transport"`
	Capabilities []string `json:"capabilities"`
	Models       []string `json:"models,omitempty"`
	Aliases      []string `json:"aliases,omitempty"`
	Match        Match    `json:"match"`
}

func (p Profile) HasCapability(capability string) bool {
	for _, item := range p.Capabilities {
		if item == capability {
			return true
		}
	}
	return false
}

// MatchInputBlock reports whether a /proc/bus/input/devices block belongs to
// this profile. Vendor/product matching wins even when BlueZ renames the
// device to a generic "bluez-hog-device".
func (p Profile) MatchInputBlock(block string) bool {
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

type Registry struct {
	order []string
	byID  map[string]Profile
}

var (
	builtinOnce sync.Once
	builtin     *Registry
)

// Builtin returns the embedded remote profiles.
func Builtin() *Registry {
	builtinOnce.Do(func() {
		registry, err := loadBuiltin()
		if err != nil {
			panic("device: load built-in profiles: " + err.Error())
		}
		builtin = registry
	})
	return builtin
}

func loadBuiltin() (*Registry, error) {
	entries, err := builtinFS.ReadDir("profiles")
	if err != nil {
		return nil, err
	}
	registry := &Registry{byID: make(map[string]Profile)}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := builtinFS.ReadFile("profiles/" + entry.Name())
		if err != nil {
			return nil, err
		}
		var profile Profile
		if err := json.Unmarshal(data, &profile); err != nil {
			return nil, fmt.Errorf("%s: %w", entry.Name(), err)
		}
		if err := profile.Validate(); err != nil {
			return nil, fmt.Errorf("%s: %w", entry.Name(), err)
		}
		registry.order = append(registry.order, profile.ID)
		registry.byID[profile.ID] = profile
	}
	return registry, nil
}

func (p Profile) Validate() error {
	if strings.TrimSpace(p.ID) == "" {
		return fmt.Errorf("device profile id is required")
	}
	if strings.TrimSpace(p.Name) == "" {
		return fmt.Errorf("device profile %q: name is required", p.ID)
	}
	if strings.TrimSpace(p.Transport) == "" {
		return fmt.Errorf("device profile %q: transport is required", p.ID)
	}
	return nil
}

func (r *Registry) Get(id string) (Profile, bool) {
	if profile, ok := r.byID[id]; ok {
		return profile, true
	}
	for _, profile := range r.byID {
		for _, alias := range profile.Aliases {
			if strings.EqualFold(alias, id) {
				return profile, true
			}
		}
	}
	return Profile{}, false
}

func (r *Registry) List() []Profile {
	out := make([]Profile, 0, len(r.order))
	for _, id := range r.order {
		if profile, ok := r.byID[id]; ok {
			out = append(out, profile)
		}
	}
	return out
}

// MatchName returns the first profile whose advertised name matches.
func (r *Registry) MatchName(name string) (Profile, bool) {
	for _, id := range r.order {
		profile := r.byID[id]
		for _, candidate := range profile.Match.Names {
			if candidate != "" && strings.EqualFold(candidate, name) {
				return profile, true
			}
		}
	}
	return Profile{}, false
}

// MatchIDs returns the first profile matching a Bluetooth VID/PID.
func (r *Registry) MatchIDs(vendor, product string) (Profile, bool) {
	vendor = strings.ToLower(strings.TrimSpace(vendor))
	product = strings.ToLower(strings.TrimSpace(product))
	if vendor == "" || product == "" {
		return Profile{}, false
	}
	for _, id := range r.order {
		profile := r.byID[id]
		if strings.EqualFold(profile.Match.Vendor, vendor) && strings.EqualFold(profile.Match.Product, product) {
			return profile, true
		}
	}
	return Profile{}, false
}

// SuggestProfile maps vendor-neutral product names to a suitable profile.
func (r *Registry) SuggestProfile(name string) (Profile, bool) {
	lower := strings.ToLower(name)
	for _, needle := range []string{"google", "chromecast", "android tv", "fire tv", "firetv", "nvidia", "shield"} {
		if strings.Contains(lower, needle) {
			return r.Get("generic-atvv")
		}
	}
	return Profile{}, false
}

// MatchInputBlock returns the first profile that owns the input device.
func (r *Registry) MatchInputBlock(block string) (Profile, bool) {
	for _, id := range r.order {
		profile := r.byID[id]
		if profile.MatchInputBlock(block) {
			return profile, true
		}
	}
	return Profile{}, false
}
