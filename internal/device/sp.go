package device

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
)

// SPDevice is one Bluetooth device as reported by system_profiler
// (see sp_darwin.go for the platform command).
type SPDevice struct {
	Address   string
	Name      string
	Vendor    string
	Product   string
	Connected bool
}

// parseSPBluetooth reads the system_profiler JSON shape: each device is a
// single-key object whose key is the device name and whose value carries
// device_address / device_vendorID / device_productID fields.
func parseSPBluetooth(data []byte) ([]SPDevice, error) {
	var root struct {
		DataType []map[string]json.RawMessage `json:"SPBluetoothDataType"`
	}
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, err
	}
	if len(root.DataType) == 0 {
		return nil, errors.New("no SPBluetoothDataType section")
	}
	section := root.DataType[0]
	var out []SPDevice
	for group, connected := range map[string]bool{
		"device_connected":     true,
		"device_not_connected": false,
	} {
		raw, ok := section[group]
		if !ok {
			continue
		}
		var entries []map[string]map[string]any
		if err := json.Unmarshal(raw, &entries); err != nil {
			continue
		}
		for _, entry := range entries {
			for name, details := range entry {
				if details == nil {
					continue
				}
				device := SPDevice{
					Address:   spString(details, "device_address"),
					Name:      strings.TrimSpace(name),
					Vendor:    spHex(details, "device_vendorID"),
					Product:   spHex(details, "device_productID"),
					Connected: connected,
				}
				if device.Address == "" {
					continue
				}
				out = append(out, device)
			}
		}
	}
	return out, nil
}

func spString(item map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := item[key].(string); ok {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// spHex normalizes the "0x2717" style VID/PID strings to lowercase bare hex.
func spHex(item map[string]any, keys ...string) string {
	value := spString(item, keys...)
	value = strings.TrimPrefix(value, "0x")
	value = strings.TrimPrefix(value, "0X")
	return strings.ToLower(value)
}

// InputBlock renders the device as the pseudo /proc/bus/input/devices block
// consumed by Profile.MatchInputBlock, so profile matching stays identical
// across platforms.
func (d SPDevice) InputBlock() string {
	var builder bytes.Buffer
	builder.WriteString(`N: Name="` + d.Name + `"` + "\n")
	builder.WriteString("H: Handlers=\n")
	builder.WriteString("U: Uniq=" + strings.ToUpper(d.Address) + "\n")
	builder.WriteString("B: vendor=" + d.Vendor + " product=" + d.Product + "\n")
	return builder.String()
}

// discoverFromEntries maps system_profiler entries onto Discovered using the
// same profile matching as the Linux /proc parser.
func discoverFromEntries(entries []SPDevice) []Discovered {
	var out []Discovered
	seen := map[string]bool{}
	for _, entry := range entries {
		address := upperMAC(entry.Address)
		if address == "" || seen[address] {
			continue
		}
		seen[address] = true
		item := Discovered{
			Address: address,
			Name:    entry.Name,
			Vendor:  entry.Vendor,
			Product: entry.Product,
		}
		if profile, ok := Builtin().MatchInputBlock(entry.InputBlock()); ok {
			item.Profile = profile.ID
			item.Capabilities = profile.Capabilities
		}
		out = append(out, item)
	}
	return out
}
