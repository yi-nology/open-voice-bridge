//go:build darwin

package device

import (
	"context"
)

// DiscoverInputDevices lists Bluetooth input devices visible to macOS and
// matches them against the built-in profiles, mirroring the Linux
// /proc/bus/input/devices flow.
func DiscoverInputDevices() ([]Discovered, error) {
	entries, err := SPBluetoothEntries(context.Background())
	if err != nil {
		return nil, err
	}
	return discoverFromEntries(entries), nil
}
