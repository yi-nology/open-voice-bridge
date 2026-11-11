package asr

import (
	"context"

	oeerrors "github.com/oekit/pkg/errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/yi-nology/open-voice-bridge/internal/config"
)

type Diagnostic struct {
	Provider string
	OK       bool
	Detail   string
}

func providerType(name string, cfg config.ASRProviderConfig) string {
	value := strings.ToLower(strings.TrimSpace(cfg.Type))
	if value == "" && name == "mimo" {
		return "mimo"
	}
	return value
}

func referencedProviders(cfg config.ASRConfig) []string {
	names := make([]string, 0, 1+len(cfg.Fallback))
	seen := map[string]bool{}
	for _, name := range append([]string{cfg.Provider}, cfg.Fallback...) {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	return names
}

func Validate(cfg config.ASRConfig) error {
	if len(cfg.Providers) == 0 {
		return oeerrors.Errorf("asr.providers is empty")
	}
	names := referencedProviders(cfg)
	if len(names) == 0 {
		return oeerrors.Errorf("asr.provider is empty")
	}
	for _, name := range names {
		provider, ok := cfg.Providers[name]
		if !ok {
			return oeerrors.Errorf("asr provider %q is referenced but not defined", name)
		}
		if provider.Threads < 0 {
			return oeerrors.Errorf("asr provider %q: threads must be >= 0", name)
		}
		if provider.TimeoutSeconds < 0 {
			return oeerrors.Errorf("asr provider %q: timeout_seconds must be >= 0", name)
		}
		switch providerType(name, provider) {
		case "mimo":
			if strings.TrimSpace(provider.APIKeyFile) == "" {
				return oeerrors.Errorf("asr provider %q: api_key_file is required", name)
			}
		case "openai":
			// Base URL/model have defaults in NewOpenAI.
		case "whispercpp":
			if strings.TrimSpace(provider.ModelPath) == "" {
				return oeerrors.Errorf("asr provider %q: model_path is required", name)
			}
		case "command":
			if strings.TrimSpace(provider.Binary) == "" {
				return oeerrors.Errorf("asr provider %q: binary is required", name)
			}
		default:
			return oeerrors.Errorf("asr provider %q has unknown type %q", name, provider.Type)
		}
	}
	return nil
}

func Build(cfg config.ASRConfig) (Engine, error) {
	if cfg.Provider == "" {
		cfg.Provider = "mimo"
	}
	if err := Validate(cfg); err != nil {
		return nil, err
	}
	var engines []Engine
	var buildErr error
	primary, err := buildProvider(cfg.Provider, cfg)
	if err != nil {
		buildErr = oeerrors.Append(buildErr, oeerrors.Errorf("primary ASR %q: %w", cfg.Provider, err))
		log.Warn("ASR primary disabled", "provider", cfg.Provider, "err", err)
	} else {
		engines = append(engines, primary)
	}
	seen := map[string]bool{cfg.Provider: true}
	for _, name := range cfg.Fallback {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		engine, err := buildProvider(name, cfg)
		if err != nil {
			buildErr = oeerrors.Append(buildErr, oeerrors.Errorf("ASR fallback %q: %w", name, err))
			log.Warn("ASR fallback disabled", "provider", name, "err", err)
			continue
		}
		engines = append(engines, engine)
	}
	if len(engines) == 0 {
		if buildErr == nil {
			buildErr = oeerrors.New("no ASR providers configured")
		}
		return nil, oeerrors.Errorf("no ASR provider could be built: %w", buildErr)
	}
	log.Info("ASR chain ready", "providers", engineNames(engines))
	return NewChain(engines...), nil
}

func buildProvider(name string, cfg config.ASRConfig) (Engine, error) {
	provider, ok := cfg.Providers[name]
	if !ok {
		return nil, oeerrors.Errorf("provider not defined")
	}
	language := provider.Language
	if language == "" {
		language = cfg.Language
	}
	if language == "" {
		language = "auto"
	}
	timeout := time.Duration(provider.TimeoutSeconds) * time.Second
	switch providerType(name, provider) {
	case "mimo":
		keyFile := expandPath(provider.APIKeyFile)
		return NewMimo(keyFile, provider.Model, language, provider.BaseURL)
	case "openai":
		key, err := readSecret(provider.APIKeyFile)
		if err != nil {
			return nil, err
		}
		return NewOpenAI(OpenAIConfig{
			BaseURL:  provider.BaseURL,
			APIKey:   key,
			Model:    provider.Model,
			Language: language,
			Timeout:  timeout,
		})
	case "whispercpp":
		return NewWhisperCPP(WhisperCPPConfig{
			Binary:    expandPath(provider.Binary),
			ModelPath: expandPath(provider.ModelPath),
			Language:  language,
			Threads:   provider.Threads,
			Timeout:   timeout,
		})
	case "command":
		return NewCommand(CommandConfig{
			Binary:    expandPath(provider.Binary),
			ModelPath: expandPath(provider.ModelPath),
			Language:  language,
			Args:      provider.Args,
			Timeout:   timeout,
		})
	default:
		return nil, oeerrors.Errorf("unknown provider type %q", provider.Type)
	}
}

func Diagnose(cfg config.ASRConfig) []Diagnostic {
	var result []Diagnostic
	if cfg.Provider == "" {
		cfg.Provider = "mimo"
	}
	if len(cfg.Providers) == 0 {
		return []Diagnostic{{Provider: "asr", OK: false, Detail: "no providers configured"}}
	}
	for _, name := range referencedProviders(cfg) {
		provider, ok := cfg.Providers[name]
		if !ok {
			result = append(result, Diagnostic{Provider: name, OK: false, Detail: "referenced but not defined"})
			continue
		}
		switch providerType(name, provider) {
		case "mimo":
			result = append(result, checkFile("MiMo API key", name, provider.APIKeyFile))
		case "openai":
			base := provider.BaseURL
			if base == "" {
				base = "https://api.openai.com/v1"
			}
			if parsed, err := url.Parse(base); err != nil || parsed.Host == "" {
				result = append(result, Diagnostic{Provider: name, OK: false, Detail: "invalid base_url " + base})
			} else {
				result = append(result, Diagnostic{Provider: name, OK: true, Detail: "endpoint " + base})
			}
			if strings.TrimSpace(provider.APIKeyFile) != "" {
				result = append(result, checkFile("API key", name, provider.APIKeyFile))
			}
		case "whispercpp":
			binary := provider.Binary
			if binary == "" {
				binary = "whisper-cli"
			}
			result = append(result, checkBinary(name, binary))
			result = append(result, checkFile("whisper model", name, provider.ModelPath))
		case "command":
			result = append(result, checkBinary(name, provider.Binary))
		default:
			result = append(result, Diagnostic{Provider: name, OK: false, Detail: "unknown type " + provider.Type})
		}
	}
	return result
}

func checkFile(label, provider, path string) Diagnostic {
	path = expandPath(path)
	if path == "" {
		return Diagnostic{Provider: provider, OK: false, Detail: label + " path is empty"}
	}
	info, err := os.Stat(path)
	if err != nil {
		return Diagnostic{Provider: provider, OK: false, Detail: label + ": " + err.Error()}
	}
	detail := label + ": " + path
	if info.Mode().Perm()&0o077 != 0 {
		detail += " (WARN: group/other readable)"
	}
	return Diagnostic{Provider: provider, OK: true, Detail: detail}
}

func checkBinary(provider, binary string) Diagnostic {
	binary = expandPath(binary)
	if binary == "" {
		return Diagnostic{Provider: provider, OK: false, Detail: "binary is empty"}
	}
	if err := guardBinary(binary); err != nil {
		return Diagnostic{Provider: provider, OK: false, Detail: err.Error()}
	}
	return Diagnostic{Provider: provider, OK: true, Detail: "binary: " + binary}
}

func readSecret(path string) (string, error) {
	path = expandPath(path)
	if path == "" {
		return "", nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", oeerrors.Errorf("read API key %s: %w", path, err)
	}
	return strings.TrimSpace(string(data)), nil
}

func expandPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

func engineNames(engines []Engine) []string {
	names := make([]string, 0, len(engines))
	for _, engine := range engines {
		names = append(names, engine.Name())
	}
	return names
}

// unavailable keeps the agent and UI running when no provider can be built.
// Recognition fails with a clear error until the user fixes the config.
type unavailable struct{ err error }

func NewUnavailable(err error) Engine {
	if err == nil {
		err = oeerrors.New("ASR is not configured")
	}
	return &unavailable{err: err}
}

func (u *unavailable) Name() string                { return "unavailable" }
func (u *unavailable) Local() bool                 { return false }
func (u *unavailable) Close(context.Context) error { return nil }

func (u *unavailable) Transcribe(context.Context, Audio) (Result, error) {
	return Result{}, oeerrors.Errorf("ASR unavailable: %w", u.err)
}
