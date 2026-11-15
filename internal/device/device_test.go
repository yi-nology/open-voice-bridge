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
	// The Google TV / Chromecast family carries its own profile; the rest of
	// the ATVV crowd still lands on the generic one.
	for name, want := range map[string]string{
		"Google TV Remote":        "google-tv-remote",
		"Chromecast Voice Remote": "google-tv-remote",
		"Onn-Remote":              "google-tv-remote",
		"Fire TV Remote":          "generic-atvv",
		"NVIDIA Shield Remote":    "generic-atvv",
		"Android TV Voice Remote": "generic-atvv",
	} {
		profile, ok := Builtin().SuggestProfile(name)
		if !ok || profile.ID != want {
			t.Fatalf("%q -> %+v ok=%v, want %q", name, profile, ok, want)
		}
	}
}

// The Chromecast / Google TV voice remote speaks the same ATVV voice service
// as the Xiaomi remotes (ab5e0001-…), but its HID descriptor never reaches
// the kernel input stack — so the profile promises voice only, and matches
// by Google's USB-IF ids (18D1:9450) and the names the family advertises.
func TestMatchGoogleTVRemote(t *testing.T) {
	profile, ok := Builtin().Get("google-tv-remote")
	if !ok || !profile.HasCapability("voice") || profile.HasCapability("keys") {
		t.Fatalf("profile = %+v ok=%v, want voice-only", profile, ok)
	}
	if p, ok := Builtin().MatchIDs("18d1", "9450"); !ok || p.ID != "google-tv-remote" {
		t.Fatalf("vid/pid match = %+v ok=%v", p, ok)
	}
	for _, name := range []string{"Onn-Remote", "Google TV Remote", "Chromecast Remote"} {
		if p, ok := Builtin().MatchName(name); !ok || p.ID != "google-tv-remote" {
			t.Fatalf("name %q -> %+v ok=%v", name, p, ok)
		}
	}
	if p, ok := Builtin().Get("chromecast-remote"); !ok || p.ID != "google-tv-remote" {
		t.Fatalf("alias = %+v ok=%v", p, ok)
	}
	block := `I: Bus=0005 Vendor=18d1 Product=9450
N: Name="Google TV Remote"`
	if p, ok := Builtin().MatchInputBlock(block); !ok || p.ID != "google-tv-remote" {
		t.Fatalf("input block = %+v ok=%v", p, ok)
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
