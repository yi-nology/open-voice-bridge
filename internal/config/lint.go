package config

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
)

// LintUnknown reports JSON keys the loader would silently drop (OPS-10).
// Unknown keys are a compat trap: a typo'd option looks saved but does
// nothing. Only the top level and the major sections are inspected —
// array elements and free-form maps (asr.providers, keymap.profiles) are
// deliberately left alone.
func LintUnknown(data []byte) []string {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		return nil
	}
	var out []string
	root := knownKeys(reflect.TypeOf(Config{}))
	for key := range top {
		if !root[key] {
			out = append(out, key)
		}
	}
	sections := []struct {
		name   string
		sample any
	}{
		{"phone", PhoneConfig{}},
		{"asr", ASRConfig{}},
		{"llm", LLMConfig{}},
		{"text", TextConfig{}},
		{"keymap", KeymapConfig{}},
	}
	for _, section := range sections {
		raw, ok := top[section.name]
		if !ok {
			continue
		}
		var body map[string]json.RawMessage
		if err := json.Unmarshal(raw, &body); err != nil {
			continue
		}
		known := knownKeys(reflect.TypeOf(section.sample))
		for key := range body {
			if !known[key] {
				out = append(out, section.name+"."+key)
			}
		}
	}
	sort.Strings(out)
	return out
}

// knownKeys lists the json names a struct decodes.
func knownKeys(t reflect.Type) map[string]bool {
	keys := map[string]bool{}
	for index := 0; index < t.NumField(); index++ {
		tag := t.Field(index).Tag.Get("json")
		name := strings.Split(tag, ",")[0]
		if name == "" || name == "-" {
			continue
		}
		keys[name] = true
	}
	return keys
}
