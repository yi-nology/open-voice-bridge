package text

import "testing"

func TestDictionaryLongestFirst(t *testing.T) {
	rules, err := New(Config{Dictionary: []Replacement{
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
	rules, err := New(Config{Snippets: []Snippet{{Trigger: "收货地址", Text: "北京市海淀区"}}})
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
	if err := Validate(Config{Dictionary: []Replacement{{To: "x"}}}); err == nil {
		t.Fatal("empty from should fail")
	}
	if err := Validate(Config{Snippets: []Snippet{{Trigger: "x"}}}); err == nil {
		t.Fatal("empty snippet text should fail")
	}
}
