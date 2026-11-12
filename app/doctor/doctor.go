// Package doctor reports whether this host can run the agent: the config
// document, the ASR provider chain, the key files and the platform
// prerequisites. It only reads and prints — it never starts a subsystem.
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
)

// Run prints one line per check and returns 1 when any of them failed.
// Warnings do not fail the run: they are things the user may not need.
func Run(configPath string, logger logx.Logger, stdout io.Writer) int {
	cfg, err := appconfig.Load(configPath)
	if err != nil {
		logger.Error("load config", "err", err)
		return 1
	}
	fmt.Fprintln(stdout, "config:", configPath)
	fmt.Fprintln(stdout, "address:", cfg.Address)
	fmt.Fprintln(stdout, "ui_addr:", cfg.UIAddr)
	fmt.Fprintln(stdout, "helper_socket:", cfg.HelperSocket)

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

func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}
