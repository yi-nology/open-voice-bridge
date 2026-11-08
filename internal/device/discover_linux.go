//go:build linux

package device

import "os"

func DiscoverInputDevices() ([]Discovered, error) {
	data, err := os.ReadFile("/proc/bus/input/devices")
	if err != nil {
		return nil, err
	}
	return parseInputDevices(string(data)), nil
}
