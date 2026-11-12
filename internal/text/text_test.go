package text

import (
	"testing"

	"github.com/yi-nology/open-voice-bridge/internal/config"
)

func TestDictionaryLongestFirst(t *testing.T) {
	rules, err := New(config.TextConfig{Dictionary: []config.Replacement{
		{From: "米授权", To: "MiniMax"},
		{From: "米", To: "Xiaomi"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got := rules.Dictionary("米授权很好，米也好"); got != "MiniMax很好，Xiaomi也好" {
		t.Fatalf("got %q", got)
	}
}

func TestSnippetNormalization(t *testing.T) {
	rules, err := New(config.TextConfig{Snippets: []config.Snippet{{Trigger: "收货地址", Text: "北京市海淀区"}}})
	if err != nil {
		t.Fatal(err)
	}
	if value, ok := rules.Snippet(" 收货地址。 "); !ok || value != "北京市海淀区" {
		t.Fatalf("value=%q ok=%v", value, ok)
	}
	if _, ok := rules.Snippet("我的收货地址"); ok {
		t.Fatal("partial trigger must not match")
	}
}

func TestValidate(t *testing.T) {
	if err := Validate(config.TextConfig{Dictionary: []config.Replacement{{To: "x"}}}); err == nil {
		t.Fatal("empty from should fail")
	}
	if err := Validate(config.TextConfig{Snippets: []config.Snippet{{Trigger: "x"}}}); err == nil {
		t.Fatal("empty snippet text should fail")
	}
}

// DOM-10: replacements are literal — a "$1" in the target must not be
// expanded as a regex template — and rules must not stack: the output of one
// entry is never re-matched by another.
func TestDictionaryReplacementsAreLiteralAndDoNotStack(t *testing.T) {
	rules, err := New(config.TextConfig{Dictionary: []config.Replacement{
		{From: "graft", To: "移植物$1"},
		{From: "price", To: "$5"},
		{From: "alpha", To: "beta"},
		{From: "beta", To: "gamma"},
	}})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	cases := []struct{ in, want string }{
		{"a graft here", "a 移植物$1 here"},
		{"cost is $5? no, price", "cost is $5? no, $5"},
		{"alpha beta", "beta gamma"}, // alpha→beta is not re-matched to gamma
		{"ALPHA", "beta"},            // matching stays case-insensitive
		{"alphaalpha", "betabeta"},   // every occurrence rewrites once
	}
	for _, tc := range cases {
		if got := rules.Dictionary(tc.in); got != tc.want {
			t.Fatalf("Dictionary(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// 语音命令: an exactly-matched command utterance names a key combo and is
// never dictation text. Matching trims one trailing punctuation run so
// "复制。" fires like "复制"; anything longer stays literal.
func TestCommandMatchesExactUtterances(t *testing.T) {
	rules, err := New(config.TextConfig{
		CommandsEnabled: true,
		Commands: map[string]string{
			"复制":  "ctrl+c",
			"粘贴。": "ctrl+v",
			"回车":  "enter",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		in    string
		combo string
		ok    bool
	}{
		{"复制", "ctrl+c", true},
		{"  复制  ", "ctrl+c", true},
		{"复制。", "ctrl+c", true},
		{"粘贴", "ctrl+v", true},
		{"回车!", "enter", true},
		{"请复制", "", false},
		{"复制一下", "", false},
		{"", "", false},
	}
	for _, tc := range cases {
		combo, ok := rules.Command(tc.in)
		if ok != tc.ok || combo != tc.combo {
			t.Fatalf("Command(%q) = %q,%v want %q,%v", tc.in, combo, ok, tc.combo, tc.ok)
		}
	}
}

func TestCommandDisabledKeepsDictationLiteral(t *testing.T) {
	rules, err := New(config.TextConfig{
		Commands: map[string]string{"复制": "ctrl+c"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if combo, ok := rules.Command("复制"); ok || combo != "" {
		t.Fatalf("Command fired while disabled: %q,%v", combo, ok)
	}
}
