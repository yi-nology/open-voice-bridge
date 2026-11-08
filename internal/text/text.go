// Package text applies user dictionary corrections and voice snippets.
package text

import (
	"regexp"
	"sort"
	"strings"

	oeerrors "github.com/oekit/pkg/errors"
)

type Replacement struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type Snippet struct {
	Trigger string `json:"trigger"`
	Text    string `json:"text"`
}

type Config struct {
	Dictionary []Replacement `json:"dictionary"`
	Snippets   []Snippet     `json:"snippets"`
}

type rule struct {
	re *regexp.Regexp
	to string
}

type Rules struct {
	rules    []rule
	snippets map[string]string
}

func Validate(cfg Config) error {
	for _, item := range cfg.Dictionary {
		if strings.TrimSpace(item.From) == "" {
			return oeerrors.New("dictionary entry has empty 'from'")
		}
	}
	for _, item := range cfg.Snippets {
		if strings.TrimSpace(item.Trigger) == "" {
			return oeerrors.New("snippet has empty trigger")
		}
		if strings.TrimSpace(item.Text) == "" {
			return oeerrors.New("snippet has empty text")
		}
	}
	return nil
}

func New(cfg Config) (*Rules, error) {
	if err := Validate(cfg); err != nil {
		return nil, err
	}
	seen := map[string]string{}
	for _, item := range cfg.Dictionary {
		seen[strings.TrimSpace(item.From)] = item.To
	}
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })
	rules := make([]rule, 0, len(keys))
	for _, key := range keys {
		pattern := regexp.MustCompile("(?i)" + regexp.QuoteMeta(key))
		rules = append(rules, rule{re: pattern, to: seen[key]})
	}
	snippets := map[string]string{}
	for _, item := range cfg.Snippets {
		snippets[normalizeTrigger(item.Trigger)] = item.Text
	}
	return &Rules{rules: rules, snippets: snippets}, nil
}

func (r *Rules) Snippet(input string) (string, bool) {
	if r == nil {
		return "", false
	}
	value, ok := r.snippets[normalizeTrigger(input)]
	return value, ok
}

func (r *Rules) Dictionary(input string) string {
	if r == nil {
		return input
	}
	out := input
	for _, item := range r.rules {
		out = item.re.ReplaceAllString(out, item.to)
	}
	return out
}

func normalizeTrigger(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimRight(value, "。！？!?.,， ")
	value = strings.Join(strings.Fields(value), " ")
	return strings.ToLower(value)
}
