// Package device defines remote profiles and input matching rules.
package device

import (
	"embed"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/yi-nology/open-voice-bridge/internal/core"
)

//go:embed profiles/*.json
var builtinFS embed.FS

// Match/Profile are core vocabulary (core 类型迁移); the registry builds and
// looks them up here.
type Match = core.DeviceMatch

type Profile = core.DeviceProfile

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
