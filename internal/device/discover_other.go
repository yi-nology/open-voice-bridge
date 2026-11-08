//go:build !linux

package device

func DiscoverInputDevices() ([]Discovered, error) { return nil, nil }
