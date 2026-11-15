package web

import (
	"os/exec"
	"testing"
)

// TestUICoreJS runs the web core's node:test suite when node is available
// (波 3 JS 测试基建). CI runners ship node; dev machines without it skip.
func TestUICoreJS(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not installed; skipping the web core suite")
	}
	out, err := exec.Command("node", "--test", "ui-core.test.mjs").CombinedOutput()
	if err != nil {
		t.Fatalf("node --test: %v\n%s", err, out)
	}
	t.Logf("%s", out)
}
