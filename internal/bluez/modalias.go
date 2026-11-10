package bluez

import (
	"regexp"
	"strings"
)

var modaliasPattern = regexp.MustCompile(`v([0-9A-Fa-f]{4})p([0-9A-Fa-f]{4})`)

func parseModalias(value string) (string, string) {
	match := modaliasPattern.FindStringSubmatch(value)
	if len(match) != 3 {
		return "", ""
	}
	return strings.ToLower(match[1]), strings.ToLower(match[2])
}
