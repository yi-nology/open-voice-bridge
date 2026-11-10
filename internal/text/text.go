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

type Rules struct {
	pattern  *regexp.Regexp
	repls    map[string]string // lowercased From -> To (literal)
	snippets map[string]string
	// phraseTexts keeps snippet texts in config order so the phrase actions
	// (phrase1..phrase8) can address them by position.
	phraseTexts []string
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
	// Longest first: at each position the longest entry wins.
	sort.Slice(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })
	quoted := make([]string, 0, len(keys))
	repls := make(map[string]string, len(keys))
	for _, key := range keys {
		quoted = append(quoted, regexp.QuoteMeta(key))
		repls[strings.ToLower(key)] = seen[key]
	}
	// One alternation over all entries, applied in a single pass: every input
	// span is rewritten exactly once, so rules can never stack (DOM-10).
	var pattern *regexp.Regexp
	if len(quoted) > 0 {
		pattern = regexp.MustCompile("(?i)(" + strings.Join(quoted, "|") + ")")
	}
	snippets := map[string]string{}
	phraseTexts := make([]string, 0, len(cfg.Snippets))
	for _, item := range cfg.Snippets {
		snippets[normalizeTrigger(item.Trigger)] = item.Text
		phraseTexts = append(phraseTexts, item.Text)
	}
	return &Rules{pattern: pattern, repls: repls, snippets: snippets, phraseTexts: phraseTexts}, nil
}

// SnippetAt returns the phrase bound to slot n (1-based), which is what the
// phrase1..phrase8 key actions insert. It reports false when the slot is out
// of range so an unbound key stays a no-op instead of pasting junk.
func (r *Rules) SnippetAt(slot int) (string, bool) {
	if r == nil || slot < 1 || slot > len(r.phraseTexts) {
		return "", false
	}
	return r.phraseTexts[slot-1], true
}

func (r *Rules) Snippet(input string) (string, bool) {
	if r == nil {
		return "", false
	}
	value, ok := r.snippets[normalizeTrigger(input)]
	return value, ok
}

// Dictionary rewrites the recognised sentence. Replacements are inserted
// literally — "$1" in a target is the characters, not a capture reference —
// and each span is rewritten once (DOM-10).
func (r *Rules) Dictionary(input string) string {
	if r == nil || r.pattern == nil {
		return input
	}
	return r.pattern.ReplaceAllStringFunc(input, func(match string) string {
		if repl, ok := r.repls[strings.ToLower(match)]; ok {
			return repl
		}
		return match
	})
}

func normalizeTrigger(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimRight(value, "。！？!?.,， ")
	value = strings.Join(strings.Fields(value), " ")
	return strings.ToLower(value)
}
