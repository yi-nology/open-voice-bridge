// Package remap replaces keyd with a small self-contained evdev/uinput
// remapper for the RC003 remote.
package remap

import (
	"encoding/json"
	"fmt"
	oeerrors "github.com/oekit/pkg/errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

var OrderedKeys = []string{
	"power", "f5", "up", "down", "left", "right", "enter",
	"back", "home", "compose", "grave", "volumeup", "volumedown",
	"ok", "homepage", "menu", "playpause", "nextsong", "previoussong", "mute", "siri",
}

var DefaultMapping = map[string]string{
	"power":        "esc",
	"f5":           "f24",
	"up":           "native",
	"down":         "native",
	"left":         "native",
	"right":        "native",
	"enter":        "native",
	"back":         "esc",
	"home":         "macro(leftalt+tab)",
	"compose":      "native",
	"grave":        "noop",
	"volumeup":     "native",
	"volumedown":   "native",
	"ok":           "enter",
	"homepage":     "macro(leftalt+tab)",
	"menu":         "compose",
	"playpause":    "playpause",
	"nextsong":     "nextsong",
	"previoussong": "previoussong",
	"mute":         "mute",
}

var SourceCodes = map[string]int{
	"power": 116, "f5": 63, "up": 103, "down": 108, "left": 105,
	"right": 106, "enter": 28, "back": 158, "home": 102,
	"compose": 127, "grave": 41, "volumeup": 115, "volumedown": 114,
	"ok": 352, "homepage": 172, "menu": 139, "playpause": 164,
	"nextsong": 163, "previoussong": 165, "mute": 113, "siri": 0,
}

var ActionCodes = map[string]int{
	"esc": 1, "enter": 28, "tab": 15, "space": 57, "backspace": 14,
	"delete": 111, "leftmeta": 125, "rightmeta": 126, "compose": 127,
	"menu": 139, "playpause": 164, "nextsong": 163, "previoussong": 165,
	"volumeup": 115, "volumedown": 114, "mute": 113, "f24": 194,
	// phrase1..phrase8 are the common-phrase triggers (SayAll 对标). They are
	// injected as F13–F20, the same kind of private signal the voice key
	// already uses (F24): the agent's evdev watcher turns them into "insert
	// the nth phrase", and desktops map them to bare F-keys nothing binds.
	"phrase1": 183, "phrase2": 184, "phrase3": 185, "phrase4": 186,
	"phrase5": 187, "phrase6": 188, "phrase7": 189, "phrase8": 190,
}

var MacroTokens = map[string]int{
	"leftalt": 56, "rightalt": 100, "leftctrl": 29, "rightctrl": 97,
	"leftshift": 42, "rightshift": 54, "leftmeta": 125, "rightmeta": 126,
	"tab": 15, "enter": 28, "esc": 1, "space": 57, "backspace": 14,
	"delete": 111, "compose": 127, "menu": 139,
	"volumeup": 115, "volumedown": 114, "mute": 113,
	"playpause": 164, "nextsong": 163, "previoussong": 165,
	"a": 30, "b": 48, "c": 46, "d": 32, "e": 18, "f": 33, "g": 34,
	"h": 35, "i": 23, "j": 36, "k": 37, "l": 38, "m": 50, "n": 49,
	"o": 24, "p": 25, "q": 16, "r": 19, "s": 31, "t": 20, "u": 22,
	"v": 47, "w": 17, "x": 45, "y": 21, "z": 44,
	"0": 11, "1": 2, "2": 3, "3": 4, "4": 5, "5": 6, "6": 7,
	"7": 8, "8": 9, "9": 10,
}

func Copy(mapping map[string]string) map[string]string {
	out := make(map[string]string, len(mapping))
	for key, value := range mapping {
		out[key] = value
	}
	return out
}

func MergeDefaults(mapping map[string]string) map[string]string {
	out := Copy(DefaultMapping)
	for key, value := range mapping {
		out[key] = value
	}
	return out
}

func Validate(mapping map[string]string) error {
	for key, action := range mapping {
		if _, ok := SourceCodes[key]; !ok {
			return oeerrors.Errorf("unknown key %q", key)
		}
		if err := validateAction(action); err != nil {
			return oeerrors.Errorf("key %q: %w", key, err)
		}
	}
	return nil
}

func validateAction(action string) error {
	switch action {
	case "", "native", "noop":
		return nil
	}
	if _, ok := ActionCodes[action]; ok {
		return nil
	}
	if strings.HasPrefix(action, "macro(") && strings.HasSuffix(action, ")") {
		tokens := macroTokens(action)
		if len(tokens) == 0 {
			return oeerrors.Errorf("empty macro")
		}
		for _, token := range tokens {
			if _, ok := MacroTokens[token]; !ok {
				return oeerrors.Errorf("unknown macro token %q", token)
			}
		}
		return nil
	}
	if strings.HasPrefix(action, "seq(") {
		_, err := ParseSeq(action)
		return err
	}
	return oeerrors.Errorf("unknown action %q", action)
}

func macroTokens(action string) []string {
	body := strings.TrimSuffix(strings.TrimPrefix(action, "macro("), ")")
	parts := strings.Split(body, "+")
	tokens := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			tokens = append(tokens, part)
		}
	}
	return tokens
}

func ActionKeyCode(action string) (int, bool) {
	code, ok := ActionCodes[action]
	return code, ok
}

// isTriggerCode reports whether a code is a private signal for the agent's
// evdev watcher (F24 opens a capture, phrase1..8 insert a common phrase)
// rather than a key for the focused app. The watcher accepts only value=1, so
// these must never be re-tapped on autorepeat.
func isTriggerCode(code int) bool {
	switch code {
	case ActionCodes["f24"], ActionCodes["phrase1"], ActionCodes["phrase2"],
		ActionCodes["phrase3"], ActionCodes["phrase4"], ActionCodes["phrase5"],
		ActionCodes["phrase6"], ActionCodes["phrase7"], ActionCodes["phrase8"]:
		return true
	}
	return false
}

func MacroTokenCodes(action string) ([]int, bool) {
	if !strings.HasPrefix(action, "macro(") {
		return nil, false
	}
	tokens := macroTokens(action)
	codes := make([]int, 0, len(tokens))
	for _, token := range tokens {
		code, ok := MacroTokens[token]
		if !ok {
			return nil, false
		}
		codes = append(codes, code)
	}
	return codes, true
}

// injectWhitelist lists the logical keys a phone remote may inject. It is a
// deliberate subset of SourceCodes: only navigation/media/power keys the
// virtual remote exposes, never arbitrary bindings.
var injectWhitelist = map[string]bool{
	"up": true, "down": true, "left": true, "right": true,
	"enter": true, "back": true, "home": true, "menu": true,
	"volumeup": true, "volumedown": true, "power": true, "f5": true,
}

// InjectAllowed reports whether a logical key name may be injected remotely.
func InjectAllowed(name string) bool { return injectWhitelist[name] }

// InjectKeyCode resolves an allowed logical key to its evdev source code.
// Disallowed or unknown names return ok=false so callers reject them.
func InjectKeyCode(name string) (int, bool) {
	if !injectWhitelist[name] {
		return 0, false
	}
	code, ok := SourceCodes[name]
	if !ok {
		return 0, false
	}
	return code, true
}

func SourceName(code int) string {
	for name, candidate := range SourceCodes {
		if candidate == code {
			return name
		}
	}
	return fmt.Sprintf("KEY_%d", code)
}

func Load(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Copy(DefaultMapping), nil
	}
	if err != nil {
		return nil, err
	}
	var mapping map[string]string
	if err := json.Unmarshal(data, &mapping); err != nil {
		return nil, oeerrors.Errorf("parse mapping: %w", err)
	}
	mapping = MergeDefaults(mapping)
	if err := Validate(mapping); err != nil {
		return nil, err
	}
	return mapping, nil
}

func Save(path string, mapping map[string]string) error {
	mapping = MergeDefaults(mapping)
	if err := Validate(mapping); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(mapping, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func ConfigText(mapping map[string]string) string {
	mapping = MergeDefaults(mapping)
	keys := make([]string, 0, len(mapping))
	for key := range mapping {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	ordered := make(map[string]string, len(mapping))
	for _, key := range keys {
		ordered[key] = mapping[key]
	}
	data, _ := json.MarshalIndent(ordered, "", "  ")
	return string(data)
}

// SeqStep is one stage of a seq(...) combo action (SayAll 组合动作): either a
// pause or a set of key codes tapped together.
type SeqStep struct {
	Delay time.Duration
	Codes []int
}

// ParseSeq interprets "seq(up, 40, macro(leftctrl+c), enter)" into ordered
// steps. Tokens use the same action vocabulary as single mappings; bare
// integers are pauses in milliseconds (0..2000).
func ParseSeq(action string) ([]SeqStep, error) {
	if !strings.HasPrefix(action, "seq(") || !strings.HasSuffix(action, ")") {
		return nil, oeerrors.New("not a seq action")
	}
	body := strings.TrimSuffix(strings.TrimPrefix(action, "seq("), ")")
	parts := strings.Split(body, ",")
	steps := make([]SeqStep, 0, len(parts))
	for _, part := range parts {
		token := strings.TrimSpace(part)
		if token == "" {
			continue
		}
		if ms, err := strconv.Atoi(token); err == nil {
			if ms < 0 || ms > 2000 {
				return nil, oeerrors.Errorf("seq delay %d out of range 0..2000", ms)
			}
			steps = append(steps, SeqStep{Delay: time.Duration(ms) * time.Millisecond})
			continue
		}
		if strings.HasPrefix(token, "macro(") && strings.HasSuffix(token, ")") {
			codes, ok := MacroTokenCodes(token)
			if !ok || len(codes) == 0 {
				return nil, oeerrors.Errorf("seq step %q: bad macro", token)
			}
			steps = append(steps, SeqStep{Codes: codes})
			continue
		}
		code, ok := ActionKeyCode(token)
		if !ok {
			code, ok = SourceCodes[token]
		}
		if !ok {
			return nil, oeerrors.Errorf("seq step %q: unknown action", token)
		}
		steps = append(steps, SeqStep{Codes: []int{code}})
	}
	if len(steps) == 0 {
		return nil, oeerrors.New("empty seq")
	}
	return steps, nil
}

// MatchClass reports whether a focused window class matches a per-app rule
// (SayAll 切得准): case-insensitive substring, so "firefox" finds
// "Navigator/firefox".
func MatchClass(class, pattern string) bool {
	class = strings.TrimSpace(class)
	pattern = strings.TrimSpace(pattern)
	if class == "" || pattern == "" {
		return false
	}
	return strings.Contains(strings.ToLower(class), strings.ToLower(pattern))
}
