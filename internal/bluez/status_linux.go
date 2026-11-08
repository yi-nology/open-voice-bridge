//go:build linux

package bluez

import (
	"context"
	"regexp"
	"sort"
	"strings"

	"github.com/godbus/dbus/v5"
)

// DeviceStatus is the BlueZ view of one paired/known device.
type DeviceStatus struct {
	Address   string
	Name      string
	Connected bool
	Paired    bool
	Trusted   bool
	Battery   int
	Vendor    string
	Product   string
	Path      dbus.ObjectPath
}

// KnownDevices lists devices known to the system Bluetooth adapter.
func KnownDevices(ctx context.Context) ([]DeviceStatus, error) {
	conn, err := dbus.SystemBus()
	if err != nil {
		return nil, err
	}
	obj := conn.Object("org.bluez", "/")
	var managed map[dbus.ObjectPath]map[string]map[string]dbus.Variant
	if err := obj.CallWithContext(ctx, "org.freedesktop.DBus.ObjectManager.GetManagedObjects", 0).Store(&managed); err != nil {
		return nil, err
	}
	var out []DeviceStatus
	for path, interfaces := range managed {
		props, ok := interfaces["org.bluez.Device1"]
		if !ok {
			continue
		}
		status := DeviceStatus{
			Address:   strings.ToUpper(stringValue(props["Address"])),
			Name:      stringValue(props["Alias"]),
			Connected: boolValue(props["Connected"]),
			Paired:    boolValue(props["Paired"]),
			Trusted:   boolValue(props["Trusted"]),
			Battery:   -1,
			Path:      path,
		}
		if status.Name == "" {
			status.Name = stringValue(props["Name"])
		}
		status.Vendor, status.Product = parseModalias(stringValue(props["Modalias"]))
		if battery, ok := interfaces["org.bluez.Battery1"]; ok {
			status.Battery = intValue(battery["Percentage"])
		}
		if status.Address == "" {
			continue
		}
		out = append(out, status)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Address < out[j].Address })
	return out, nil
}

// Status returns the BlueZ state for one address, if known.
func Status(ctx context.Context, address string) (DeviceStatus, error) {
	devices, err := KnownDevices(ctx)
	if err != nil {
		return DeviceStatus{}, err
	}
	for _, item := range devices {
		if strings.EqualFold(item.Address, address) {
			return item, nil
		}
	}
	return DeviceStatus{Address: strings.ToUpper(address), Battery: -1}, nil
}

// Connect connects a paired device through BlueZ.
func Connect(ctx context.Context, address string) error {
	status, err := Status(ctx, address)
	if err != nil {
		return err
	}
	if status.Path == "" {
		return ErrUnsupported
	}
	conn, err := dbus.SystemBus()
	if err != nil {
		return err
	}
	return conn.Object("org.bluez", status.Path).CallWithContext(ctx, "org.bluez.Device1.Connect", 0).Err
}

// Disconnect disconnects a BlueZ device.
func Disconnect(ctx context.Context, address string) error {
	status, err := Status(ctx, address)
	if err != nil {
		return err
	}
	if status.Path == "" {
		return ErrUnsupported
	}
	conn, err := dbus.SystemBus()
	if err != nil {
		return err
	}
	return conn.Object("org.bluez", status.Path).CallWithContext(ctx, "org.bluez.Device1.Disconnect", 0).Err
}

var modaliasPattern = regexp.MustCompile(`v([0-9A-Fa-f]{4})p([0-9A-Fa-f]{4})`)

func parseModalias(value string) (string, string) {
	match := modaliasPattern.FindStringSubmatch(value)
	if len(match) != 3 {
		return "", ""
	}
	return strings.ToLower(match[1]), strings.ToLower(match[2])
}

func stringValue(v dbus.Variant) string {
	value, _ := v.Value().(string)
	return value
}

func boolValue(v dbus.Variant) bool {
	value, _ := v.Value().(bool)
	return value
}

func intValue(v dbus.Variant) int {
	switch value := v.Value().(type) {
	case uint8:
		return int(value)
	case int16:
		return int(value)
	case uint16:
		return int(value)
	case int32:
		return int(value)
	case uint32:
		return int(value)
	case int:
		return value
	default:
		return -1
	}
}
