//go:build darwin

package bluez

import (
	"context"
	"strings"

	"github.com/yi-nology/open-voice-bridge/internal/device"
)

// Device status on macOS is best-effort: system_profiler exposes the paired
// HID device list (name, address), but there is no CLI-safe equivalent of
// BlueZ's Device1 interface for pair/trust control, and battery needs a GATT
// read this build does not do yet. Battery is reported as -1 (unknown).

type DeviceStatus struct {
	Address   string
	Name      string
	Connected bool
	Paired    bool
	Trusted   bool
	Battery   int
	Vendor    string
	Product   string
}

func KnownDevices(ctx context.Context) ([]DeviceStatus, error) {
	entries, err := device.SPBluetoothEntries(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]DeviceStatus, 0, len(entries))
	for _, entry := range entries {
		out = append(out, DeviceStatus{
			Address:   strings.ToUpper(entry.Address),
			Name:      entry.Name,
			Connected: entry.Connected,
			Paired:    true,
			Trusted:   true,
			Battery:   -1,
			Vendor:    entry.Vendor,
			Product:   entry.Product,
		})
	}
	return out, nil
}

func Status(ctx context.Context, address string) (DeviceStatus, error) {
	devices, err := KnownDevices(ctx)
	if err != nil {
		return DeviceStatus{Address: address, Battery: -1}, err
	}
	for _, item := range devices {
		if strings.EqualFold(item.Address, address) {
			return item, nil
		}
	}
	return DeviceStatus{Address: address, Battery: -1}, ErrUnsupported
}

func Connect(ctx context.Context, address string) error    { return ErrUnsupported }
func Disconnect(ctx context.Context, address string) error { return ErrUnsupported }
