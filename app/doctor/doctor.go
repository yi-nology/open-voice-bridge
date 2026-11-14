// Package doctor reports whether this host can run the agent: the config
// document, the ASR provider chain, the key files and the platform
// prerequisites. It only reads and prints — it never writes the config and
// never starts a subsystem.
//
// The exit code follows the same yardstick as /readyz: non-zero means the core
// transcription chain is broken. Everything else is a warning carrying the
// command that fixes it, because a phone-only or headless host legitimately
// leaves those gaps open (OPS-04, OPS-06).
package doctor

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/oekit/logx"

	"github.com/yi-nology/open-voice-bridge/internal/appconfig"
	"github.com/yi-nology/open-voice-bridge/internal/config"
)

// Run prints one line per check and returns 1 when a core dependency failed.
func Run(configPath string, logger logx.Logger, stdout io.Writer) int {
	cfg, err := appconfig.Read(configPath)
	if err != nil {
		logger.Error("load config", "err", err)
		if os.IsNotExist(err) {
			_, _ = fmt.Fprintf(stdout, "FAIL: config %s not found; fix: copy conf/config.example.json there, or run the agent once to write the defaults\n", configPath)
		} else {
			_, _ = fmt.Fprintf(stdout, "FAIL: config %s: %v\n", configPath, err)
		}
		return 1
	}
	_, _ = fmt.Fprintln(stdout, "config:", configPath)
	_, _ = fmt.Fprintln(stdout, "address:", cfg.Address)
	_, _ = fmt.Fprintln(stdout, "ui_addr:", cfg.UIAddr)
	_, _ = fmt.Fprintln(stdout, "helper_socket:", cfg.HelperSocket)

	checks := addressCheck(cfg)
	checks = append(checks, asrChecks(cfg, configPath)...)
	checks = append(checks, configChecks(cfg, configPath)...)
	checks = append(checks, hostChecks(cfg)...)
	if report(stdout, checks) > 0 {
		return 1
	}
	return 0
}

// The example MAC passes the address format check, so a fresh document looks
// configured. Say it is not. A warning rather than a failure: a phone-only
// setup legitimately has no Bluetooth remote (OPS-06).
func addressCheck(cfg config.Config) []check {
	if !strings.EqualFold(cfg.Address, config.PlaceholderAddress) {
		return nil
	}
	return []check{warn(fmt.Sprintf(
		"address is still the example %s (no remote configured); put the remote's Bluetooth MAC in devices[].address, or pair a phone as the input source",
		cfg.Address), "")}
}
