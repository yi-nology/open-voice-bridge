package device

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestBuiltinRC003(t *testing.T) {
	registry := Builtin()
	profile, ok := registry.Get("rc003")
	if !ok {
		t.Fatal("rc003 profile missing")
	}
	if !profile.HasCapability("voice") || !profile.HasCapability("keys") {
		t.Fatalf("capabilities = %v", profile.Capabilities)
	}
}

func TestMatchGenericBlueZHogName(t *testing.T) {
	block := `I: Bus=0005 Vendor=2717 Product=32b8 Version=00a4
N: Name="bluez-hog-device"
H: Handlers=sysrq kbd event12`
	profile, ok := Builtin().MatchInputBlock(block)
	if !ok || profile.ID != "rc003" {
		t.Fatalf("profile = %+v ok=%v", profile, ok)
	}
}

func TestMatchNamedRemote(t *testing.T) {
	block := `I: Bus=0005 Vendor=2717 Product=32b8
N: Name="小米蓝牙语音遥控器"`
	profile, ok := Builtin().MatchInputBlock(block)
	if !ok || profile.ID != "rc003" {
		t.Fatalf("profile = %+v ok=%v", profile, ok)
	}
}

func TestUnrelatedBlockDoesNotMatch(t *testing.T) {
	block := `I: Bus=0003 Vendor=046d Product=c52b
N: Name="USB Keyboard"`
	if _, ok := Builtin().MatchInputBlock(block); ok {
		t.Fatal("USB keyboard must not match")
	}
}

func TestParseInputDevices(t *testing.T) {
	data := `I: Bus=0005 Vendor=2717 Product=32b8 Version=00a4
N: Name="bluez-hog-device"
U: Uniq=aa:bb:cc:dd:ee:ff
H: Handlers=sysrq kbd event12

I: Bus=0003 Vendor=046d Product=c52b
N: Name="USB Keyboard"
H: Handlers=sysrq kbd event3`
	items := parseInputDevices(data)
	if len(items) != 1 {
		t.Fatalf("items = %+v", items)
	}
	if items[0].Address != "AA:BB:CC:DD:EE:FF" || items[0].Profile != "rc003" || items[0].Product != "32b8" {
		t.Fatalf("item = %+v", items[0])
	}
}

func TestXiaomiModelAliases(t *testing.T) {
	for _, id := range []string{"rc001", "rc002", "arn9", "xiaomi-remote-1", "xiaomi-remote-2"} {
		profile, ok := Builtin().Get(id)
		if !ok || profile.ID != "rc003" {
			t.Fatalf("alias %q -> %+v ok=%v", id, profile, ok)
		}
	}
	profile, _ := Builtin().Get("rc003")
	if len(profile.Models) != 3 {
		t.Fatalf("models = %v", profile.Models)
	}
}

func TestGenericATVVProfile(t *testing.T) {
	profile, ok := Builtin().Get("generic-atvv")
	if !ok || !profile.HasCapability("voice") {
		t.Fatalf("profile = %+v ok=%v", profile, ok)
	}
}

func TestSuggestATVVProfiles(t *testing.T) {
	for _, name := range []string{"Google TV Remote", "Chromecast Remote", "Fire TV Remote", "NVIDIA Shield Remote"} {
		profile, ok := Builtin().SuggestProfile(name)
		if !ok || profile.ID != "generic-atvv" {
			t.Fatalf("%q -> %+v ok=%v", name, profile, ok)
		}
	}
}

func TestMatchAppleAndGenericATVVByID(t *testing.T) {
	profile, ok := Builtin().MatchIDs("004c", "026d")
	if !ok || profile.ID != "apple-siri-remote-1" {
		t.Fatalf("apple match = %+v ok=%v", profile, ok)
	}
}

// A preset may only carry what the runtime reads: an ignored field looks like
// a supported knob to whoever edits the table next (DOM-12).
func TestBuiltinPresetsCarryNoUnreadFields(t *testing.T) {
	entries, err := builtinFS.ReadDir("profiles")
	if err != nil {
		t.Fatalf("read presets: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := builtinFS.ReadFile("profiles/" + entry.Name())
		if err != nil {
			t.Fatalf("read %s: %v", entry.Name(), err)
		}
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		var profile Profile
		if err := decoder.Decode(&profile); err != nil {
			t.Fatalf("%s carries a field the runtime ignores: %v", entry.Name(), err)
		}
	}
}
