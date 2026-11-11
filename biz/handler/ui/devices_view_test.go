package ui

import (
	"context"
	"testing"

	"github.com/yi-nology/open-voice-bridge/internal/core"
)

func TestDevicesSerialisesTheTypedView(t *testing.T) {
	server := New(Deps{
		Devices: func(context.Context) core.DevicesView {
			return core.DevicesView{
				OK:     true,
				Active: "rc003",
				Devices: []core.DeviceView{{
					ID:           "rc003",
					Name:         "Xiaomi Remote",
					Address:      "AA:BB:CC:DD:EE:FF",
					Profile:      "rc003",
					Enabled:      true,
					Active:       true,
					Connected:    true,
					Capabilities: []string{"voice", "keys"},
					Vendor:       "045e",
					Product:      "02fd",
					NameMatch:    []string{"Xiaomi"},
					Paired:       true,
					Battery:      87,
				}},
				Profiles: []core.ProfileView{{
					ID:           "rc003",
					Name:         "RC003",
					Transport:    "ble",
					Capabilities: []string{"voice"},
				}},
			}
		},
	})
	resp, err := server.GetDevices(context.Background())
	if err != nil {
		t.Fatalf("GetDevices: %v", err)
	}
	if !resp.Ok || resp.Active != "rc003" {
		t.Fatalf("resp = %+v", resp)
	}
	if len(resp.Devices) != 1 || len(resp.Profiles) != 1 {
		t.Fatalf("resp = %+v", resp)
	}
	got := resp.Devices[0]
	if got.ID != "rc003" || got.Name != "Xiaomi Remote" || got.Address != "AA:BB:CC:DD:EE:FF" {
		t.Fatalf("device = %+v", got)
	}
	if got.Profile != "rc003" || !got.Enabled || !got.Active || !got.Connected || !got.Paired {
		t.Fatalf("device flags = %+v", got)
	}
	if got.Battery != 87 || got.Vendor != "045e" || got.Product != "02fd" {
		t.Fatalf("device = %+v", got)
	}
	if len(got.Capabilities) != 2 || got.Capabilities[0] != "voice" {
		t.Fatalf("capabilities = %v", got.Capabilities)
	}
	if len(got.NameMatch) != 1 || got.NameMatch[0] != "Xiaomi" {
		t.Fatalf("name_match = %v", got.NameMatch)
	}
	profile := resp.Profiles[0]
	if profile.ID != "rc003" || profile.Name != "RC003" || profile.Transport != "ble" {
		t.Fatalf("profile = %+v", profile)
	}
	if len(profile.Capabilities) != 1 || profile.Capabilities[0] != "voice" {
		t.Fatalf("profile capabilities = %v", profile.Capabilities)
	}
}

func TestDevicesWithoutProducerFails(t *testing.T) {
	server := New(Deps{})
	if _, err := server.GetDevices(context.Background()); err == nil {
		t.Fatal("expected an error when no producer is wired")
	}
}
