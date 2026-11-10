package device

import "testing"

// sampleSPBluetooth mirrors the real `system_profiler SPBluetoothDataType
// -json` shape on macOS: one single-key object per device, keyed by name.
// Addresses and names are placeholders, per the repo rule that committed
// tests never carry real hardware identifiers.
const sampleSPBluetooth = `{
  "SPBluetoothDataType": [
    {
      "controller_properties": { "controller_state": "ON" },
      "device_connected": [
        { "Test Keyboard": {
            "device_address": "AA:BB:CC:11:22:33",
            "device_productID": "0x0267",
            "device_vendorID": "0x004C" } },
        { "\u5c0f\u7c73\u84dd\u7259\u8bed\u97f3\u9065\u63a7\u5668": {
            "device_address": "AA:BB:CC:DD:EE:FF",
            "device_firmwareVersion": "2671",
            "device_productID": "0x32B8",
            "device_services": "0x400020 < HID BLE >",
            "device_vendorID": "0x2717" } }
      ],
      "device_not_connected": [
        { "Test Headphones": {
            "device_address": "AA:BB:CC:44:55:66",
            "device_productID": "0x2010",
            "device_vendorID": "0x004C" } },
        { "\u5ba2\u5385": { "device_address": "AA:BB:CC:77:88:99", "device_rssi": "-74" } }
      ]
    }
  ]
}`

func TestParseSPBluetooth(t *testing.T) {
	items, err := parseSPBluetooth([]byte(sampleSPBluetooth))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(items) != 4 {
		t.Fatalf("got %d devices, want 4: %+v", len(items), items)
	}

	byName := map[string]SPDevice{}
	for _, item := range items {
		byName[item.Name] = item
	}

	remote := byName["小米蓝牙语音遥控器"]
	if remote.Address != "AA:BB:CC:DD:EE:FF" {
		t.Errorf("remote address = %q", remote.Address)
	}
	if remote.Vendor != "2717" || remote.Product != "32b8" {
		t.Errorf("remote ids = %s:%s, want 2717:32b8", remote.Vendor, remote.Product)
	}
	if !remote.Connected {
		t.Error("remote should be connected")
	}

	offline := byName["Test Headphones"]
	if offline.Connected {
		t.Error("headphones should be offline")
	}
	if offline.Vendor != "004c" || offline.Product != "2010" {
		t.Errorf("headphones ids = %s:%s", offline.Vendor, offline.Product)
	}

	bare := byName["客厅"]
	if bare.Address != "AA:BB:CC:77:88:99" || bare.Vendor != "" {
		t.Errorf("bare device = %+v", bare)
	}
}

func TestParseSPBluetoothRejectsEmptySection(t *testing.T) {
	if _, err := parseSPBluetooth([]byte(`{"SPBluetoothDataType": []}`)); err == nil {
		t.Fatal("want error for empty section")
	}
	if _, err := parseSPBluetooth([]byte(`not json`)); err == nil {
		t.Fatal("want error for invalid json")
	}
}

func TestSPDeviceInputBlockMatchesXiaomiProfile(t *testing.T) {
	remote := SPDevice{
		Address: "aa:bb:cc:dd:ee:ff",
		Name:    "小米蓝牙语音遥控器",
		Vendor:  "2717",
		Product: "32b8",
	}
	block := remote.InputBlock()
	profile, ok := Builtin().MatchInputBlock(block)
	if !ok {
		t.Fatalf("profile did not match block:\n%s", block)
	}
	if profile.ID != "rc003" {
		t.Errorf("matched profile %q, want rc003", profile.ID)
	}
}

func TestDiscoverFromEntries(t *testing.T) {
	items, err := parseSPBluetooth([]byte(sampleSPBluetooth))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	found := discoverFromEntries(items)
	if len(found) != 4 {
		t.Fatalf("discovered %d devices, want 4", len(found))
	}
	seen := map[string]Discovered{}
	for _, item := range found {
		seen[item.Address] = item
	}
	remote := seen["AA:BB:CC:DD:EE:FF"]
	if remote.Profile != "rc003" {
		t.Errorf("remote profile = %q, want rc003", remote.Profile)
	}
	if len(remote.Capabilities) == 0 {
		t.Error("remote capabilities empty")
	}
	apple := seen["AA:BB:CC:11:22:33"]
	if apple.Profile != "" {
		t.Errorf("keyboard should have no profile, got %q", apple.Profile)
	}
}
