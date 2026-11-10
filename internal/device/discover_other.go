//go:build !linux && !darwin

package device

func DiscoverInputDevices() ([]Discovered, error) { return nil, nil }
