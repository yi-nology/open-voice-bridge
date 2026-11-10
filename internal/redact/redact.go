// Package redact turns values that must not reach the log file into a form
// that is still useful in support: an address keeps enough to correlate two
// lines about the same device, a provider failure keeps enough to look up.
package redact

import (
	"encoding/json"
	"strings"
)

// MAC keeps the three vendor octets (they only say who built the radio) and
// the last one, which is enough to tell two remotes in one house apart. The
// middle octets are the device-specific half, so they are dropped. An input
// that isn't a six-octet address redacts to the empty string rather than to an
// unexpected value.
func MAC(address string) string {
	groups := strings.Split(strings.TrimSpace(address), ":")
	if len(groups) != 6 {
		return ""
	}
	return strings.Join([]string{groups[0], groups[1], groups[2], "**", "**", groups[5]}, ":")
}

// ProviderCode pulls the machine-readable identifiers out of an OpenAI-style
// error body and returns them as "type/code". The prose is deliberately left
// out: a content-filter rejection quotes the text that tripped it, and that
// text is a transcript, which must never be logged (SEC-02). A body that is
// not in that shape - HTML from a proxy, plain text, an empty error object -
// redacts to the empty string, leaving the HTTP status as the only clue.
func ProviderCode(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var parsed struct {
		Error struct {
			Type string `json:"type"`
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return ""
	}
	parts := make([]string, 0, 2)
	if parsed.Error.Type != "" {
		parts = append(parts, parsed.Error.Type)
	}
	if parsed.Error.Code != "" {
		parts = append(parts, parsed.Error.Code)
	}
	return strings.Join(parts, "/")
}

// ProviderSuffix is ProviderCode ready to append to an error string: ": a/b"
// when the body carried codes and "" when it did not, so a caller never prints
// a dangling separator.
func ProviderSuffix(body []byte) string {
	code := ProviderCode(body)
	if code == "" {
		return ""
	}
	return ": " + code
}
