package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/yi-nology/open-voice-bridge/internal/asr"
	"github.com/yi-nology/open-voice-bridge/internal/config"
	"github.com/yi-nology/open-voice-bridge/internal/core"
)

// asrChecks reports the transcription chain: the provider diagnostics and the
// key file the primary provider reads. This is the one core dependency, so a
// gap here fails the run.
func asrChecks(cfg config.Config, configPath string) []check {
	var checks []check
	for _, diagnostic := range asr.Diagnose(cfg.ASR) {
		text := fmt.Sprintf("asr/%s: %s", diagnostic.Provider, diagnostic.Detail)
		if diagnostic.OK {
			checks = append(checks, ok(text))
			continue
		}
		checks = append(checks, gap(core.DepASR, text,
			fmt.Sprintf("edit asr.providers.%s in %s", diagnostic.Provider, configPath)))
	}
	if info, err := os.Stat(cfg.APIKeyFile); err == nil && info.Mode().Perm()&0o077 != 0 {
		checks = append(checks, warn("legacy api_key_file is readable by group/other",
			"chmod 600 "+cfg.APIKeyFile))
	}
	checks = append(checks, primaryKeyCheck(cfg)...)
	return checks
}

// primaryKeyCheck reports the key file the primary provider actually reads. The
// diagnostics above already name a missing file; this adds the byte count and
// mode, which is what tells the user the right file was found.
func primaryKeyCheck(cfg config.Config) []check {
	primary := cfg.ASR.Providers[cfg.ASR.Provider]
	providerType := strings.ToLower(primary.Type)
	if providerType == "" && cfg.ASR.Provider == "mimo" {
		providerType = "mimo"
	}
	if providerType != "mimo" && providerType != "openai" {
		return nil
	}
	keyFile := expandHome(primary.APIKeyFile)
	if keyFile == "" {
		return []check{gap(core.DepASR,
			fmt.Sprintf("api_key_file missing for provider %s", cfg.ASR.Provider),
			fmt.Sprintf("set asr.providers.%s.api_key_file in the config", cfg.ASR.Provider))}
	}
	info, err := os.Stat(keyFile)
	if err != nil {
		return []check{gap(core.DepASR, fmt.Sprintf("API key %s: %v", keyFile, err),
			fmt.Sprintf("install -m 600 /dev/null %s  # then write the provider key into it", keyFile))}
	}
	checks := []check{ok(fmt.Sprintf("API key %s (%d bytes, %04o)", keyFile, info.Size(), info.Mode().Perm()))}
	if info.Mode().Perm()&0o077 != 0 {
		checks = append(checks, warn("API key is readable by group/other", "chmod 600 "+keyFile))
	}
	return checks
}

func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}
