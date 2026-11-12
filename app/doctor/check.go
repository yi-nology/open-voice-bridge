package doctor

import (
	"fmt"
	"io"

	"github.com/yi-nology/open-voice-bridge/internal/core"
)

type status int

const (
	statusOK status = iota
	statusWarn
	statusFail
)

// check is one line of the report. text follows the ok:/WARN:/FAIL: prefix and
// fix is the command that resolves the gap: a diagnosis without a next step
// leaves the user guessing (OPS-04).
type check struct {
	state status
	text  string
	fix   string
}

func ok(text string) check { return check{state: statusOK, text: text} }

func warn(text, fix string) check { return check{state: statusWarn, text: text, fix: fix} }

// gap reports a missing dependency. Its severity follows the shared yardstick
// rather than the probe's own opinion, so doctor's exit code and /readyz's ok
// cannot drift apart (OPS-04).
func gap(dep core.Dependency, text, fix string) check {
	if dep.Core() {
		return check{state: statusFail, text: text, fix: fix}
	}
	return warn(text, fix)
}

// report prints one line per check and returns the number of failures. Only a
// core gap fails the run; warnings are things the user may not need.
func report(stdout io.Writer, checks []check) int {
	failures := 0
	for _, item := range checks {
		prefix := "ok: "
		switch item.state {
		case statusWarn:
			prefix = "WARN: "
		case statusFail:
			prefix = "FAIL: "
			failures++
		}
		line := prefix + item.text
		if item.fix != "" && item.state != statusOK {
			line += "; fix: " + item.fix
		}
		fmt.Fprintln(stdout, line)
	}
	return failures
}
