// Package doctor reports whether this host can run the agent: the config
// document, the ASR provider chain, the key files and the platform
// prerequisites. It only reads and prints — it never writes the config and
// never starts a subsystem.
package doctor

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/oekit/logx"

	"github.com/yi-nology/open-voice-bridge/internal/appconfig"
	"github.com/yi-nology/open-voice-bridge/internal/asr"
	"github.com/yi-nology/open-voice-bridge/internal/config"
)

// Run prints one line per check and returns 1 when any of them failed.
// Warnings do not fail the run: they are things the user may not need.
func Run(configPath string, logger logx.Logger, stdout io.Writer) int {
	cfg, err := appconfig.Read(configPath)
	if err != nil {
		logger.Error("load config", "err", err)
		if os.IsNotExist(err) {
			fmt.Fprintf(stdout, "FAIL: config %s not found; copy conf/config.example.json there, or run the agent once to write the defaults\n", configPath)
		} else {
			fmt.Fprintf(stdout, "FAIL: config %s: %v\n", configPath, err)
		}
		return 1
	}
	fmt.Fprintln(stdout, "config:", configPath)
	fmt.Fprintln(stdout, "address:", cfg.Address)
	fmt.Fprintln(stdout, "ui_addr:", cfg.UIAddr)
	fmt.Fprintln(stdout, "helper_socket:", cfg.HelperSocket)
	warnPlaceholderAddress(stdout, cfg)

	failures := 0
	for _, diagnostic := range asr.Diagnose(cfg.ASR) {
		if diagnostic.OK {
			fmt.Fprintf(stdout, "ok: asr/%s: %s\n", diagnostic.Provider, diagnostic.Detail)
		} else {
			fmt.Fprintf(stdout, "FAIL: asr/%s: %s\n", diagnostic.Provider, diagnostic.Detail)
			failures++
		}
	}
	if info, err := os.Stat(cfg.APIKeyFile); err == nil {
		perm := info.Mode().Perm()
		if perm&0o077 != 0 {
			fmt.Fprintln(stdout, "WARN: legacy api_key_file is readable by group/other; chmod 600 recommended")
		}
	}

	primary := cfg.ASR.Providers[cfg.ASR.Provider]
	providerType := strings.ToLower(primary.Type)
	if providerType == "" && cfg.ASR.Provider == "mimo" {
		providerType = "mimo"
	}
	if providerType == "mimo" || providerType == "openai" {
		keyFile := expandHome(primary.APIKeyFile)
		if keyFile == "" {
			fmt.Fprintln(stdout, "FAIL: api_key_file missing for provider", cfg.ASR.Provider)
			failures++
		} else if info, err := os.Stat(keyFile); err != nil {
			fmt.Fprintf(stdout, "FAIL: API key %s: %v\n", keyFile, err)
			failures++
		} else {
			perm := info.Mode().Perm()
			fmt.Fprintf(stdout, "ok: API key %s (%d bytes, %04o)\n", keyFile, info.Size(), perm)
			if perm&0o077 != 0 {
				fmt.Fprintln(stdout, "WARN: API key is readable by group/other; chmod 600 recommended")
			}
		}
	}
	failures += hostChecks(stdout, cfg)
	if failures > 0 {
		return 1
	}
	return 0
}

// The example MAC passes the address format check, so a fresh document looks
// configured. Say it is not. A warning rather than a failure: a phone-only
// setup legitimately has no Bluetooth remote (OPS-06).
func warnPlaceholderAddress(stdout io.Writer, cfg config.Config) {
	if strings.EqualFold(cfg.Address, config.PlaceholderAddress) {
		fmt.Fprintf(stdout, "WARN: address is still the example %s (no remote configured); put the remote's Bluetooth MAC in devices[].address, or pair a phone as the input source\n", cfg.Address)
	}
}

func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}
