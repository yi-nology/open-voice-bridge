// Code split from service.go (ARC-09): one boundary per file.
package service

import (
	"context"
	"github.com/oekit/concurrency/async"
	oeerrors "github.com/oekit/pkg/errors"
	"github.com/yi-nology/open-voice-bridge/internal/bluez"
	"github.com/yi-nology/open-voice-bridge/internal/config"
	"github.com/yi-nology/open-voice-bridge/internal/device"
	"github.com/yi-nology/open-voice-bridge/internal/helper"
	"sort"
	"strings"
	"time"
)

func (s *Service) DiscoverDevices(ctx context.Context) map[string]any {
	items, err := device.DiscoverInputDevices()
	if err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	s.cfgMu.RLock()
	configured := make(map[string]bool, len(s.cfg.Devices))
	for _, item := range s.cfg.Devices {
		configured[strings.ToUpper(item.Address)] = true
	}
	s.cfgMu.RUnlock()
	known := map[string]bluez.DeviceStatus{}
	if list, err := bluez.KnownDevices(ctx); err == nil {
		for _, item := range list {
			known[strings.ToUpper(item.Address)] = item
		}
	}
	merged := map[string]map[string]any{}
	for _, item := range items {
		address := strings.ToUpper(item.Address)
		link := known[address]
		profileID := item.Profile
		capabilities := item.Capabilities
		if profileID == "" {
			if profile, ok := device.Builtin().SuggestProfile(item.Name); ok {
				profileID = profile.ID
				capabilities = profile.Capabilities
			}
		}
		merged[address] = map[string]any{
			"address":      address,
			"name":         item.Name,
			"vendor":       item.Vendor,
			"product":      item.Product,
			"profile":      profileID,
			"capabilities": capabilities,
			"configured":   configured[address],
			"paired":       link.Paired,
			"connected":    true,
			"battery":      link.Battery,
		}
	}
	for address, link := range known {
		if _, ok := merged[address]; ok {
			continue
		}
		profileID := ""
		var capabilities []string
		if profile, ok := device.Builtin().MatchIDs(link.Vendor, link.Product); ok {
			profileID = profile.ID
			capabilities = profile.Capabilities
		} else if profile, ok := device.Builtin().MatchName(link.Name); ok {
			profileID = profile.ID
			capabilities = profile.Capabilities
		} else if profile, ok := device.Builtin().SuggestProfile(link.Name); ok {
			profileID = profile.ID
			capabilities = profile.Capabilities
		}
		merged[address] = map[string]any{
			"address":      address,
			"name":         link.Name,
			"vendor":       link.Vendor,
			"product":      link.Product,
			"profile":      profileID,
			"capabilities": capabilities,
			"configured":   configured[address],
			"paired":       link.Paired,
			"connected":    link.Connected,
			"battery":      link.Battery,
		}
	}
	out := make([]map[string]any, 0, len(merged))
	for _, item := range merged {
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool {
		return helper.StringValue(out[i]["address"]) < helper.StringValue(out[j]["address"])
	})
	return map[string]any{"ok": true, "devices": out}
}

func (s *Service) SelectDevice(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	s.cfgMu.Lock()
	index := -1
	for i := range s.cfg.Devices {
		if s.cfg.Devices[i].ID == id {
			index = i
			break
		}
	}
	if index < 0 {
		s.cfgMu.Unlock()
		return oeerrors.Errorf("unknown device %q", id)
	}
	item := s.cfg.Devices[index]
	if !item.Enabled {
		s.cfgMu.Unlock()
		return oeerrors.Errorf("device %q is disabled", id)
	}
	profile, ok := device.Builtin().Get(item.Profile)
	if !ok || !profile.HasCapability("voice") {
		s.cfgMu.Unlock()
		return oeerrors.Errorf("device %q does not support voice", id)
	}
	snapshot := s.configSnapshotLocked()
	snapshot.ActiveDevice = id
	snapshot.Address = item.Address
	s.cfgMu.Unlock()
	if err := s.persistConfig(snapshot); err != nil {
		return err
	}
	s.cfgMu.Lock()
	s.cfg.ActiveDevice = id
	s.cfg.Address = item.Address
	s.cfgMu.Unlock()
	return s.agent.SetAddress(item.Address)
}

func (s *Service) SetDeviceEnabled(ctx context.Context, id string, enabled bool) error {
	id = strings.TrimSpace(id)
	s.cfgMu.Lock()
	index := -1
	for i := range s.cfg.Devices {
		if s.cfg.Devices[i].ID == id {
			index = i
			break
		}
	}
	if index < 0 {
		s.cfgMu.Unlock()
		return oeerrors.Errorf("unknown device %q", id)
	}
	if !enabled && s.cfg.ActiveDevice == id {
		s.cfgMu.Unlock()
		return oeerrors.Errorf("select another device before disabling %q", id)
	}
	snapshot := s.configSnapshotLocked()
	snapshot.Devices[index].Enabled = enabled
	s.cfgMu.Unlock()
	if err := s.persistConfig(snapshot); err != nil {
		return err
	}
	s.cfgMu.Lock()
	s.cfg.Devices[index].Enabled = enabled
	s.cfgMu.Unlock()
	s.syncDevicesEventually()
	return nil
}

func (s *Service) ConnectDevice(ctx context.Context, id string, connect bool) error {
	id = strings.TrimSpace(id)
	s.cfgMu.RLock()
	var item config.DeviceConfig
	found := false
	for _, candidate := range s.cfg.Devices {
		if candidate.ID == id {
			item = candidate
			found = true
			break
		}
	}
	active := s.cfg.ActiveDevice == id
	s.cfgMu.RUnlock()
	if !found {
		return oeerrors.Errorf("unknown device %q", id)
	}
	if !item.Enabled {
		return oeerrors.Errorf("device %q is disabled", id)
	}
	profile, _ := device.Builtin().Get(item.Profile)
	if connect && active && profile.HasCapability("voice") {
		return s.agent.SetAddress(item.Address)
	}
	if !connect && active {
		return oeerrors.Errorf("select another device before disconnecting %q", id)
	}
	if connect {
		if err := bluez.Connect(ctx, item.Address); err != nil && !strings.Contains(err.Error(), "Already Connected") {
			return err
		}
		return nil
	}
	if err := bluez.Disconnect(ctx, item.Address); err != nil && !strings.Contains(err.Error(), "Not Connected") {
		return err
	}
	return nil
}

func (s *Service) SaveDevice(ctx context.Context, item config.DeviceConfig) error {
	item.ID = strings.TrimSpace(item.ID)
	item.Name = strings.TrimSpace(item.Name)
	item.Address = strings.TrimSpace(item.Address)
	item.Profile = strings.TrimSpace(item.Profile)
	if item.Profile == "" {
		item.Profile = "rc003"
	}
	if _, ok := device.Builtin().Get(item.Profile); !ok {
		return oeerrors.Errorf("unknown device profile %q", item.Profile)
	}
	if item.ID == "" {
		return oeerrors.New("device id is required")
	}
	s.cfgMu.Lock()
	snapshot := s.configSnapshotLocked()
	found := false
	for i := range snapshot.Devices {
		if snapshot.Devices[i].ID == item.ID {
			snapshot.Devices[i] = item
			found = true
			break
		}
	}
	if !found {
		snapshot.Devices = append(snapshot.Devices, item)
	}
	s.cfgMu.Unlock()
	if err := s.persistConfig(snapshot); err != nil {
		return err
	}
	s.cfgMu.Lock()
	found = false
	for i := range s.cfg.Devices {
		if s.cfg.Devices[i].ID == item.ID {
			s.cfg.Devices[i] = item
			found = true
			break
		}
	}
	if !found {
		s.cfg.Devices = append(s.cfg.Devices, item)
	}
	s.cfgMu.Unlock()
	s.syncDevicesEventually()
	return nil
}

func (s *Service) DeleteDevice(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	s.cfgMu.Lock()
	if s.cfg.ActiveDevice == id {
		s.cfgMu.Unlock()
		return oeerrors.Errorf("select another device before deleting %q", id)
	}
	if len(s.cfg.Devices) <= 1 {
		s.cfgMu.Unlock()
		return oeerrors.New("cannot delete the last device")
	}
	found := false
	for i := range s.cfg.Devices {
		if s.cfg.Devices[i].ID == id {
			found = true
			break
		}
	}
	if !found {
		s.cfgMu.Unlock()
		return oeerrors.Errorf("unknown device %q", id)
	}
	snapshot := s.configSnapshotLocked()
	for i := range snapshot.Devices {
		if snapshot.Devices[i].ID == id {
			snapshot.Devices = append(snapshot.Devices[:i], snapshot.Devices[i+1:]...)
			break
		}
	}
	s.cfgMu.Unlock()
	if err := s.persistConfig(snapshot); err != nil {
		return err
	}
	s.cfgMu.Lock()
	for i := range s.cfg.Devices {
		if s.cfg.Devices[i].ID == id {
			s.cfg.Devices = append(s.cfg.Devices[:i], s.cfg.Devices[i+1:]...)
			break
		}
	}
	s.cfgMu.Unlock()
	s.syncDevicesEventually()
	return nil
}

func (s *Service) syncDevicesEventually() {
	async.GoSafe(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 65*time.Second)
		defer cancel()
		s.SyncDevices(ctx)
	})
}
