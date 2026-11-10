//go:build darwin

package device

import (
	"context"
	"os/exec"
)

// SPBluetoothEntries lists Bluetooth devices visible to macOS via
// `system_profiler SPBluetoothDataType -json`. Parsing lives in sp.go.
func SPBluetoothEntries(ctx context.Context) ([]SPDevice, error) {
	command := exec.CommandContext(ctx, "system_profiler", "SPBluetoothDataType", "-json")
	output, err := command.Output()
	if err != nil {
		return nil, err
	}
	return parseSPBluetooth(output)
}
