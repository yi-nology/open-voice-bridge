// Package gtvkeys decodes the raw HID button reports of the Google TV /
// Chromecast voice remote family (Onn 同族). The family's HID descriptor is
// rejected by the kernel input stack, so buttons never reach evdev — the
// reports are read straight from the GATT HID Report characteristic
// (0x2A4D) instead. Formats seen in the wild (onn-remote-linux,
// google-tv-remote-bridge):
//
//	2 or 4 bytes: 16-bit Consumer usage, little endian (42 00 = Up)
//	1–2 bytes:    a 1-based index into the Chromecast report map's usage list
//	              (fw 26.0 sends one byte though the map declares two)
//	8 bytes:      keyboard report, key in byte 2 (80 = Volume Up)
//	all zeros:    release
package gtvkeys

// chromecastUsages is the Chromecast remote (18D1:9450) report map's Consumer
// usage list, in its declared order — the 1-byte reports index into this.
var chromecastUsages = [...]int{
	0x19E, 0xCD, 0x42, 0x43, 0x44, 0x45, 0x41, 0xE2, 0x221,
	0x223, 0x224, 0xE9, 0xEA, 0x77, 0x78, 0x79, 0x89,
}

// Event is one decoded button transition.
type Event struct {
	Button string // canonical name: power, up, select, volup, assistant, ...
	Down   bool
}

// Button maps a HID usage to the canonical button name the remote family
// uses (page 0x07 keyboard, 0x0C consumer, 0x01 generic desktop). Unknown
// usages return "" so callers drop them instead of guessing.
func Button(page, usage int) string {
	switch page {
	case 0x07: // keyboard page
		switch usage {
		case 0x52:
			return "up"
		case 0x51:
			return "down"
		case 0x50:
			return "left"
		case 0x4F:
			return "right"
		case 0x28, 0x58:
			return "select"
		case 0x29, 0x2A:
			return "back"
		case 0x4A:
			return "home"
		case 0x80:
			return "volup"
		case 0x81:
			return "voldown"
		case 0x7F:
			return "mute"
		}
	case 0x0C: // consumer page
		switch usage {
		case 0x42:
			return "up"
		case 0x43:
			return "down"
		case 0x44:
			return "left"
		case 0x45:
			return "right"
		case 0x41:
			return "select"
		case 0x224:
			return "back"
		case 0x223, 0x40:
			return "home"
		case 0xE2:
			return "mute"
		case 0xE9:
			return "volup"
		case 0xEA:
			return "voldown"
		case 0xCD:
			return "playpause"
		case 0x221, 0xCF:
			return "assistant"
		case 0x77:
			return "youtube"
		case 0x78:
			return "netflix"
		case 0x79:
			return "app3"
		case 0x7A:
			return "app4"
		case 0x89, 0x1BB:
			return "input"
		case 0x8D:
			return "tv"
		case 0x96:
			return "settings"
		case 0x9C:
			return "channelup"
		case 0x9D:
			return "channeldown"
		case 0x19E, 0x30:
			return "power"
		case 0x22A:
			return "bookmark"
		}
	case 0x01: // generic desktop D-pad
		switch usage {
		case 0x90:
			return "up"
		case 0x91:
			return "down"
		case 0x92:
			return "right"
		case 0x93:
			return "left"
		}
	}
	return ""
}

// ToLogical maps a remote-family button onto the logical key vocabulary the
// helper inject whitelist speaks (internal/remap). Buttons outside the
// whitelist — assistant (the ATVV control channel owns the mic), the app
// launcher keys and the TV extras — report false and are dropped.
func ToLogical(button string) (string, bool) {
	switch button {
	case "select":
		return "enter", true
	case "volup":
		return "volumeup", true
	case "voldown":
		return "volumedown", true
	case "up", "down", "left", "right", "back", "home", "menu", "mute", "playpause", "power":
		return button, true
	default:
		return "", false
	}
}

// Decoder turns raw HID report bytes into button transitions. Reports arrive
// per GATT characteristic (the family exposes several); the held state is
// tracked per source so one channel's release never frees another's key.
type Decoder struct {
	held map[string]string
}

// NewDecoder returns a decoder with empty held state.
func NewDecoder() *Decoder { return &Decoder{held: map[string]string{}} }

// Handle decodes one report from one source. A press of a different button
// while one is held releases the old one first (the family never sends an
// explicit release between them).
func (d *Decoder) Handle(source string, report []byte) []Event {
	button, pressed := decode(report)
	return d.track(source, button, pressed)
}

func (d *Decoder) track(source, button string, pressed bool) []Event {
	current := d.held[source]
	if current == "" && !pressed {
		return nil // spurious release
	}
	if current == button && pressed {
		return nil // auto-repeat: same key still held
	}
	var events []Event
	if current != "" {
		events = append(events, Event{Button: current, Down: false})
		delete(d.held, source)
	}
	if pressed && button != "" {
		d.held[source] = button
		events = append(events, Event{Button: button, Down: true})
	}
	return events
}

// decode reports the button a payload announces and whether it is a press.
func decode(report []byte) (button string, pressed bool) {
	allZero := true
	for _, b := range report {
		if b != 0 {
			allZero = false
			break
		}
	}
	if allZero || len(report) == 0 {
		return "", false
	}
	switch {
	case len(report) == 8:
		// Keyboard report: modifiers, reserved, key in byte 2.
		if report[2] == 0 {
			return "", false
		}
		return Button(0x07, int(report[2])), true
	case len(report) <= 2 && indexReport(report):
		// Chromecast fw 26.0: 1-based index into the report map's usage list.
		usage := chromecastUsages[int(report[0])-1]
		return Button(0x0C, usage), true
	case len(report) >= 2:
		usage := int(report[0]) | int(report[1])<<8
		return Button(0x0C, usage), true
	default:
		return Button(0x0C, int(report[0])), true
	}
}

// indexReport tells a 1-based usage index (every byte within 1..17) from a
// little-endian usage code whose low byte alone could look like one.
func indexReport(report []byte) bool {
	if report[0] < 1 || report[0] > byte(len(chromecastUsages)) {
		return false
	}
	for _, b := range report {
		if b > byte(len(chromecastUsages)) {
			return false
		}
	}
	return true
}
