// Package bluez is the BLE ATVV transport: BlueZ D-Bus on Linux,
// CoreBluetooth (see ble_bridge_darwin.m) on macOS.
package bluez

import oeerrors "github.com/oekit/pkg/errors"

// ErrUnsupported reports a platform feature that is not available on the
// running GOOS (e.g. battery status or pair control on macOS).
var ErrUnsupported = oeerrors.New("Bluetooth transport not implemented on this platform")
