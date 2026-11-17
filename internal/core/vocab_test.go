package core

import "testing"

// Per-app keymap matching is a plain case-insensitive substring test on the
// focused window class (X11 WM_CLASS res_class).
func TestMatchWindowClass(t *testing.T) {
	cases := []struct {
		class, pattern string
		want           bool
	}{
		{"Navigator/firefox", "firefox", true},
		{"Code", "code", true},
		{"kitty", "firefox", false},
		{"anything", "", false},
	}
	for _, tc := range cases {
		if got := MatchWindowClass(tc.class, tc.pattern); got != tc.want {
			t.Fatalf("MatchWindowClass(%q,%q) = %v", tc.class, tc.pattern, got)
		}
	}
}

func TestCopyMappingIsIndependent(t *testing.T) {
	src := map[string]string{"power": "esc", "up": "up"}
	out := CopyMapping(src)
	out["power"] = "f13"
	if src["power"] != "esc" || out["up"] != "up" || out["power"] != "f13" {
		t.Fatalf("src = %v, out = %v", src, out)
	}
}

func TestDeviceProfileCapabilityAndIdentity(t *testing.T) {
	p := DeviceProfile{ID: "rc003", Name: "Remote 2 Pro", Transport: "hidraw",
		Capabilities: []string{"voice"}, Match: DeviceMatch{Vendor: "2717", Product: "32b8"}}
	if !p.HasCapability("voice") || p.HasCapability("nfc") {
		t.Fatalf("capabilities = %v", p.Capabilities)
	}
	if err := (DeviceProfile{}).Validate(); err == nil {
		t.Fatal("empty profile must not validate")
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !p.MatchInputBlock("I: Bus=0005 Vendor=2717 Product=32b8\nN: Name=\"bluez-hog-device\"") {
		t.Fatal("vendor/product block must match")
	}
}

func TestClientConfigSnippetShape(t *testing.T) {
	snippet := ClientConfigSnippet("", "/usr/local/bin/xiaoyao", "tok")
	entry, ok := snippet.MCPServers["xiaoyao"]
	if !ok {
		t.Fatal("xiaoyao stanza missing from the snippet")
	}
	if entry.Command != "/usr/local/bin/xiaoyao" {
		t.Fatalf("command = %v", entry.Command)
	}
}
