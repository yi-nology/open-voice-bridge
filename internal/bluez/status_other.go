//go:build !linux && !darwin

package bluez

import "context"

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

func KnownDevices(ctx context.Context) ([]DeviceStatus, error) { return nil, ErrUnsupported }

func Status(ctx context.Context, address string) (DeviceStatus, error) {
	return DeviceStatus{Address: address, Battery: -1}, nil
}

func Connect(ctx context.Context, address string) error { return ErrUnsupported }

func Disconnect(ctx context.Context, address string) error { return ErrUnsupported }
