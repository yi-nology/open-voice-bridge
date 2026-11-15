// Code split from service.go (ARC-09): one boundary per file.
package service

import (
	"context"
	oeerrors "github.com/oekit/pkg/errors"
	"github.com/yi-nology/open-voice-bridge/internal/core"
	"strings"
)

func (s *Service) Apply(ctx context.Context, mapping map[string]string) (core.HelperResponse, error) {
	merged := s.ops.MergeDefaults(mapping)
	s.cfgMu.Lock()
	if s.cfg.Keymap.Profiles == nil {
		s.cfg.Keymap.Profiles = map[string]map[string]string{}
	}
	snapshot := s.configSnapshotLocked()
	snapshot.Keymap.Profiles["custom"] = merged
	snapshot.Keymap.Profile = "custom"
	s.cfgMu.Unlock()
	if err := s.persistConfig(snapshot); err != nil {
		return core.HelperResponse{}, err
	}
	s.cfgMu.Lock()
	if s.cfg.Keymap.Profiles == nil {
		s.cfg.Keymap.Profiles = map[string]map[string]string{}
	}
	s.cfg.Keymap.Profiles["custom"] = merged
	s.cfg.Keymap.Profile = "custom"
	s.cfgMu.Unlock()
	// DOM-11: the helper only hears about a mapping that is already on disk;
	// a failed save must not leave the daemon running a config that exists
	// nowhere.
	return s.applyMapping(ctx, mapping)
}

// applyMapping hands a mapping to the remapper after swapping every
// type()/open() seq step for an agent(n) key the user-level agent watches —
// the root daemon only ever presses keys (S3).
func (s *Service) applyMapping(ctx context.Context, mapping map[string]string) (core.HelperResponse, error) {
	rewritten, actions := s.ops.RewriteActions(mapping)
	s.agent.SetSeqActions(actions)
	return s.helper.Call(ctx, core.HelperRequest{Cmd: "apply", Mapping: rewritten})
}

func (s *Service) KeymapProfiles(ctx context.Context) (string, map[string]map[string]string) {
	s.cfgMu.RLock()
	defer s.cfgMu.RUnlock()
	profiles := make(map[string]map[string]string, len(s.cfg.Keymap.Profiles))
	for name, mapping := range s.cfg.Keymap.Profiles {
		profiles[name] = core.CopyMapping(mapping)
	}
	return s.cfg.Keymap.Profile, profiles
}

func (s *Service) ApplyKeymapProfile(ctx context.Context, name string) (core.HelperResponse, error) {
	s.cfgMu.RLock()
	mapping, ok := s.cfg.Keymap.Profiles[name]
	if ok {
		mapping = core.CopyMapping(mapping)
	}
	s.cfgMu.RUnlock()
	if !ok {
		return core.HelperResponse{}, oeerrors.Errorf("unknown keymap profile %q", name)
	}
	s.cfgMu.Lock()
	snapshot := s.configSnapshotLocked()
	snapshot.Keymap.Profile = name
	s.cfgMu.Unlock()
	if err := s.persistConfig(snapshot); err != nil {
		return core.HelperResponse{}, err
	}
	s.cfgMu.Lock()
	s.cfg.Keymap.Profile = name
	s.cfgMu.Unlock()
	return s.applyMapping(ctx, mapping)
}

func (s *Service) SaveKeymapProfile(ctx context.Context, name string, mapping map[string]string) (core.HelperResponse, error) {
	name = strings.TrimSpace(name)
	if !profileNamePattern.MatchString(name) {
		return core.HelperResponse{}, oeerrors.Errorf("profile name must be 1-32 chars of a-z, 0-9, _ or -")
	}
	if builtinProfile(name) {
		return core.HelperResponse{}, oeerrors.Errorf("%q is a built-in profile and cannot be overwritten", name)
	}
	cleaned := s.ops.MergeDefaults(mapping)
	if err := s.ops.ValidateMapping(cleaned); err != nil {
		return core.HelperResponse{}, err
	}
	s.cfgMu.Lock()
	snapshot := s.configSnapshotLocked()
	if snapshot.Keymap.Profiles == nil {
		snapshot.Keymap.Profiles = map[string]map[string]string{}
	}
	snapshot.Keymap.Profiles[name] = cleaned
	snapshot.Keymap.Profile = name
	s.cfgMu.Unlock()
	if err := s.persistConfig(snapshot); err != nil {
		return core.HelperResponse{}, err
	}
	s.cfgMu.Lock()
	if s.cfg.Keymap.Profiles == nil {
		s.cfg.Keymap.Profiles = map[string]map[string]string{}
	}
	s.cfg.Keymap.Profiles[name] = cleaned
	s.cfg.Keymap.Profile = name
	s.cfgMu.Unlock()
	return s.applyMapping(ctx, cleaned)
}

func (s *Service) DeleteKeymapProfile(ctx context.Context, name string) error {
	name = strings.TrimSpace(name)
	if builtinProfile(name) {
		return oeerrors.Errorf("%q is a built-in profile and cannot be deleted", name)
	}
	s.cfgMu.Lock()
	if s.cfg.Keymap.Profile == name {
		s.cfgMu.Unlock()
		return oeerrors.Errorf("switch to another profile before deleting %q", name)
	}
	if _, ok := s.cfg.Keymap.Profiles[name]; !ok {
		s.cfgMu.Unlock()
		return oeerrors.Errorf("unknown keymap profile %q", name)
	}
	snapshot := s.configSnapshotLocked()
	delete(snapshot.Keymap.Profiles, name)
	s.cfgMu.Unlock()
	if err := s.persistConfig(snapshot); err != nil {
		return err
	}
	s.cfgMu.Lock()
	delete(s.cfg.Keymap.Profiles, name)
	s.cfgMu.Unlock()
	return nil
}
