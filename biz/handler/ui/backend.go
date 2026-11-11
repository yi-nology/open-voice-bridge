package ui

import (
	"context"
	"time"

	oeerrors "github.com/oekit/pkg/errors"
	"github.com/oekit/pkg/ptrx"

	api "github.com/yi-nology/open-voice-bridge/biz/model/api"
	"github.com/yi-nology/open-voice-bridge/internal/asr"
	"github.com/yi-nology/open-voice-bridge/internal/config"
	"github.com/yi-nology/open-voice-bridge/internal/llm"
	"github.com/yi-nology/open-voice-bridge/internal/text"
)

var errCaptureRunning = oeerrors.New("capture already running")

func (s *Server) Health(ctx context.Context) (*api.HealthResp, error) {
	return &api.HealthResp{Ok: true}, nil
}

func (s *Server) Ready(ctx context.Context) (*api.ReadyResp, error) {
	checks := map[string]any{}
	if s.deps.Ready != nil {
		checks = s.deps.Ready(ctx)
	}
	resp := &api.ReadyResp{
		Ok:            boolValue(checks["ok"]),
		KeydActive:    boolValue(checks["keyd_active"]),
		RemotePresent: boolValue(checks["remote_present"]),
		BleConnected:  boolValue(checks["ble_connected"]),
		AtvvReady:     boolValue(checks["atvv_ready"]),
		AsrReady:      boolValue(checks["asr_ready"]),
		MicHealthy:    boolValue(checks["mic_healthy"]),
	}
	if value := stringValue(checks["helper_error"]); value != "" {
		resp.HelperError = ptrx.Of(value)
	}
	if value := stringValue(checks["asr_error"]); value != "" {
		resp.AsrError = ptrx.Of(value)
	}
	return resp, nil
}

func (s *Server) Status(ctx context.Context) (*api.StatusResp, error) {
	status := map[string]any{}
	if s.deps.Status != nil {
		status = s.deps.Status(ctx)
	}
	resp := &api.StatusResp{
		Ok:             boolValue(status["ok"]),
		Address:        stringValue(status["address"]),
		KeydActive:     boolValue(status["keyd_active"]),
		RemotePresent:  boolValue(status["remote_present"]),
		Mapping:        stringMap(status["mapping"]),
		Config:         stringValue(status["config"]),
		MicActive:      boolValue(status["mic_active"]),
		BleConnected:   boolValue(status["ble_connected"]),
		AtvvReady:      boolValue(status["atvv_ready"]),
		AsrBackend:     stringValue(status["asr_backend"]),
		AsrLocal:       boolValue(status["asr_local"]),
		LastText:       stringValue(status["last_text"]),
		AsrProvider:    stringValue(status["asr_provider"]),
		AsrProviders:   stringSlice(status["asr_providers"]),
		KeymapProfile:  stringValue(status["keymap_profile"]),
		KeymapProfiles: stringSlice(status["keymap_profiles"]),
		AgentAutostart: boolValue(status["agent_autostart"]),
		RemapAutostart: boolValue(status["remap_autostart"]),
		Capturing:      s.IsCapturing(),
		MicHealthy:     boolValue(status["mic_healthy"]),
		LastLatencyMs:  int64Value(status["last_latency_ms"]),
		Platform:       stringValue(status["platform"]),
		KeyTotal:       int64Value(status["key_total"]),
		KeyCounts:      int64Map(status["key_counts"]),
	}
	if value := stringValue(status["helper_error"]); value != "" {
		resp.HelperError = ptrx.Of(value)
	}
	if value := stringValue(status["asr_error"]); value != "" {
		resp.AsrError = ptrx.Of(value)
	}
	return resp, nil
}

func (s *Server) Apply(ctx context.Context, mapping map[string]string) (*api.OperationResp, error) {
	if s.deps.Apply == nil {
		return nil, oeerrors.New("apply not available")
	}
	result, err := s.deps.Apply(ctx, mapping)
	if err != nil {
		return nil, err
	}
	return operationResult(result), nil
}

func (s *Server) CaptureStart(ctx context.Context) (*api.OperationResp, error) {
	event, err := s.startCapture(ctx)
	if err != nil {
		if oeerrors.Is(err, errCaptureRunning) {
			return &api.OperationResp{Ok: false, Error: ptrx.Of(err.Error())}, nil
		}
		return nil, err
	}
	return &api.OperationResp{Ok: true, Event: ptrx.Of(event)}, nil
}

func (s *Server) CaptureStop(ctx context.Context) (*api.OperationResp, error) {
	event, err := s.stopCapture()
	if err != nil {
		return nil, err
	}
	return &api.OperationResp{Ok: true, Event: ptrx.Of(event)}, nil
}

func (s *Server) Reconnect(ctx context.Context) (*api.OperationResp, error) {
	if s.deps.Reconnect == nil {
		return nil, oeerrors.New("reconnect not available")
	}
	if err := s.deps.Reconnect(ctx); err != nil {
		return nil, err
	}
	return &api.OperationResp{Ok: true}, nil
}

func (s *Server) RestartMic(ctx context.Context) (*api.OperationResp, error) {
	if s.deps.RestartMic == nil {
		return nil, oeerrors.New("restart-mic not available")
	}
	if err := s.deps.RestartMic(ctx); err != nil {
		return nil, err
	}
	return &api.OperationResp{Ok: true}, nil
}

func (s *Server) SwitchASR(ctx context.Context, provider string) (*api.OperationResp, error) {
	if s.deps.SwitchASR == nil {
		return nil, oeerrors.New("switch-asr not available")
	}
	if err := s.deps.SwitchASR(ctx, provider); err != nil {
		return nil, err
	}
	return &api.OperationResp{Ok: true}, nil
}

func (s *Server) GetASRConfig(ctx context.Context) (*api.ASRConfigResp, error) {
	if s.deps.ASRConfig == nil {
		return nil, oeerrors.New("asr config not available")
	}
	data := s.deps.ASRConfig(ctx)
	resp := &api.ASRConfigResp{
		Ok:        boolValue(data["ok"]),
		Provider:  stringValue(data["provider"]),
		Fallback:  stringSlice(data["fallback"]),
		Language:  stringValue(data["language"]),
		Providers: providerModels(data["providers"]),
	}
	if value := stringValue(data["asr_error"]); value != "" {
		resp.AsrError = ptrx.Of(value)
	}
	return resp, nil
}

func (s *Server) UpdateASRConfig(ctx context.Context, next *api.ASRConfig) (*api.OperationResp, error) {
	if s.deps.UpdateASRConfig == nil {
		return nil, oeerrors.New("asr config not available")
	}
	if next == nil {
		return nil, oeerrors.New("missing asr config")
	}
	providers := make(map[string]asr.ProviderConfig, len(next.Providers))
	for name, provider := range next.Providers {
		if provider == nil {
			continue
		}
		providers[name] = asr.ProviderConfig{
			Type:           provider.Type,
			APIKeyFile:     provider.APIKeyFile,
			BaseURL:        provider.BaseURL,
			Model:          provider.Model,
			Language:       provider.Language,
			Binary:         provider.Binary,
			ModelPath:      provider.ModelPath,
			Threads:        int(provider.Threads),
			Args:           provider.Args,
			TimeoutSeconds: int(provider.TimeoutSeconds),
		}
	}
	cfg := asr.Config{
		Provider:  next.Provider,
		Fallback:  next.Fallback,
		Language:  next.Language,
		Providers: providers,
	}
	if err := s.deps.UpdateASRConfig(ctx, cfg); err != nil {
		return nil, err
	}
	return &api.OperationResp{Ok: true}, nil
}

func (s *Server) TestASR(ctx context.Context, provider string) (*api.ASRTestResp, error) {
	if s.deps.TestASR == nil {
		return nil, oeerrors.New("asr test not available")
	}
	diagnostics := s.deps.TestASR(ctx, provider)
	resp := &api.ASRTestResp{
		Ok:          true,
		Provider:    provider,
		Diagnostics: make([]*api.Diagnostic, 0, len(diagnostics)),
	}
	for _, diagnostic := range diagnostics {
		resp.Diagnostics = append(resp.Diagnostics, &api.Diagnostic{
			Provider: diagnostic.Provider,
			Ok:       diagnostic.OK,
			Detail:   diagnostic.Detail,
		})
	}
	return resp, nil
}

func (s *Server) KeymapProfiles(ctx context.Context) (*api.KeymapProfilesResp, error) {
	if s.deps.KeymapProfiles == nil {
		return nil, oeerrors.New("keymap profiles not available")
	}
	profile, profiles := s.deps.KeymapProfiles(ctx)
	return &api.KeymapProfilesResp{Ok: true, Profile: profile, Profiles: profiles}, nil
}

func (s *Server) ApplyKeymapProfile(ctx context.Context, profile string) (*api.OperationResp, error) {
	if s.deps.ApplyKeymapProfile == nil {
		return nil, oeerrors.New("keymap apply not available")
	}
	result, err := s.deps.ApplyKeymapProfile(ctx, profile)
	if err != nil {
		return nil, err
	}
	return operationResult(result), nil
}

func (s *Server) SaveKeymapProfile(ctx context.Context, name string, mapping map[string]string) (*api.OperationResp, error) {
	if s.deps.SaveKeymapProfile == nil {
		return nil, oeerrors.New("keymap save not available")
	}
	result, err := s.deps.SaveKeymapProfile(ctx, name, mapping)
	if err != nil {
		return nil, err
	}
	return operationResult(result), nil
}

func (s *Server) DeleteKeymapProfile(ctx context.Context, name string) (*api.OperationResp, error) {
	if s.deps.DeleteKeymapProfile == nil {
		return nil, oeerrors.New("keymap delete not available")
	}
	if err := s.deps.DeleteKeymapProfile(ctx, name); err != nil {
		return nil, err
	}
	return &api.OperationResp{Ok: true}, nil
}

func operationResult(result map[string]any) *api.OperationResp {
	resp := &api.OperationResp{Ok: true}
	if result == nil {
		return resp
	}
	if value, ok := result["ok"].(bool); ok {
		resp.Ok = value
	}
	if value := stringValue(result["config"]); value != "" {
		resp.Config = ptrx.Of(value)
	}
	if value := stringValue(result["error"]); value != "" {
		resp.Error = ptrx.Of(value)
	}
	if value := stringValue(result["event"]); value != "" {
		resp.Event = ptrx.Of(value)
	}
	if value := stringValue(result["output"]); value != "" {
		resp.Output = ptrx.Of(value)
	}
	return resp
}

func providerModels(value any) map[string]*api.ProviderConfig {
	providers, ok := value.(map[string]asr.ProviderConfig)
	if !ok {
		return nil
	}
	out := make(map[string]*api.ProviderConfig, len(providers))
	for name, provider := range providers {
		out[name] = &api.ProviderConfig{
			Type:           provider.Type,
			APIKeyFile:     provider.APIKeyFile,
			BaseURL:        provider.BaseURL,
			Model:          provider.Model,
			Language:       provider.Language,
			Binary:         provider.Binary,
			ModelPath:      provider.ModelPath,
			Threads:        int32(provider.Threads),
			Args:           provider.Args,
			TimeoutSeconds: int32(provider.TimeoutSeconds),
		}
	}
	return out
}

func boolValue(value any) bool {
	result, _ := value.(bool)
	return result
}

func stringValue(value any) string {
	result, _ := value.(string)
	return result
}

func stringSlice(value any) []string {
	switch typed := value.(type) {
	case []string:
		return typed
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}

func stringMap(value any) map[string]string {
	switch typed := value.(type) {
	case map[string]string:
		return typed
	case map[string]any:
		out := make(map[string]string, len(typed))
		for key, item := range typed {
			if text, ok := item.(string); ok {
				out[key] = text
			}
		}
		return out
	default:
		return nil
	}
}

func int64Map(value any) map[string]int64 {
	switch typed := value.(type) {
	case map[string]int64:
		return typed
	case map[string]any:
		out := make(map[string]int64, len(typed))
		for key, item := range typed {
			out[key] = int64Value(item)
		}
		return out
	default:
		return nil
	}
}

func (s *Server) GetDevices(ctx context.Context) (*api.DevicesResp, error) {
	if s.deps.Devices == nil {
		return nil, oeerrors.New("devices not available")
	}
	data := s.deps.Devices(ctx)
	resp := &api.DevicesResp{Ok: boolValue(data["ok"]), Active: stringValue(data["active"])}
	if profiles, ok := data["profiles"].([]map[string]any); ok {
		resp.Profiles = make([]*api.ProfileInfo, 0, len(profiles))
		for _, profile := range profiles {
			resp.Profiles = append(resp.Profiles, &api.ProfileInfo{
				ID:           stringValue(profile["id"]),
				Name:         stringValue(profile["name"]),
				Transport:    stringValue(profile["transport"]),
				Capabilities: stringSlice(profile["capabilities"]),
			})
		}
	}
	items, _ := data["devices"].([]map[string]any)
	resp.Devices = make([]*api.DeviceInfo, 0, len(items))
	for _, item := range items {
		resp.Devices = append(resp.Devices, &api.DeviceInfo{
			ID:           stringValue(item["id"]),
			Name:         stringValue(item["name"]),
			Address:      stringValue(item["address"]),
			Profile:      stringValue(item["profile"]),
			Enabled:      boolValue(item["enabled"]),
			Active:       boolValue(item["active"]),
			Connected:    boolValue(item["connected"]),
			Capabilities: stringSlice(item["capabilities"]),
			Vendor:       stringValue(item["vendor"]),
			Product:      stringValue(item["product"]),
			NameMatch:    stringSlice(item["name_match"]),
			Paired:       boolValue(item["paired"]),
			Battery:      int32Value(item["battery"]),
		})
	}
	return resp, nil
}

func (s *Server) DiscoverDevices(ctx context.Context) (*api.DiscoveredResp, error) {
	if s.deps.DiscoverDevices == nil {
		return nil, oeerrors.New("device discovery not available")
	}
	data := s.deps.DiscoverDevices(ctx)
	resp := &api.DiscoveredResp{Ok: boolValue(data["ok"])}
	items, _ := data["devices"].([]map[string]any)
	resp.Devices = make([]*api.DiscoveredDevice, 0, len(items))
	for _, item := range items {
		resp.Devices = append(resp.Devices, &api.DiscoveredDevice{
			Address:      stringValue(item["address"]),
			Name:         stringValue(item["name"]),
			Vendor:       stringValue(item["vendor"]),
			Product:      stringValue(item["product"]),
			Profile:      stringValue(item["profile"]),
			Capabilities: stringSlice(item["capabilities"]),
			Configured:   boolValue(item["configured"]),
			Paired:       boolValue(item["paired"]),
			Connected:    boolValue(item["connected"]),
			Battery:      int32Value(item["battery"]),
		})
	}
	return resp, nil
}

func (s *Server) SelectDevice(ctx context.Context, id string) (*api.OperationResp, error) {
	if s.deps.SelectDevice == nil {
		return nil, oeerrors.New("device selection not available")
	}
	if err := s.deps.SelectDevice(ctx, id); err != nil {
		return nil, err
	}
	return &api.OperationResp{Ok: true}, nil
}

func (s *Server) ToggleDevice(ctx context.Context, id string, enabled bool) (*api.OperationResp, error) {
	if s.deps.SetDeviceEnabled == nil {
		return nil, oeerrors.New("device toggle not available")
	}
	if err := s.deps.SetDeviceEnabled(ctx, id, enabled); err != nil {
		return nil, err
	}
	return &api.OperationResp{Ok: true}, nil
}

func (s *Server) SaveDevice(ctx context.Context, req *api.DeviceSaveReq) (*api.OperationResp, error) {
	if s.deps.SaveDevice == nil {
		return nil, oeerrors.New("device save not available")
	}
	if req == nil {
		return nil, oeerrors.New("missing device")
	}
	if err := s.deps.SaveDevice(ctx, config.DeviceConfig{
		ID:        req.ID,
		Name:      req.Name,
		Address:   req.Address,
		Profile:   req.Profile,
		Enabled:   req.Enabled,
		Vendor:    req.Vendor,
		Product:   req.Product,
		NameMatch: req.NameMatch,
	}); err != nil {
		return nil, err
	}
	return &api.OperationResp{Ok: true}, nil
}

func (s *Server) ConnectDevice(ctx context.Context, id string, connect bool) (*api.OperationResp, error) {
	if s.deps.ConnectDevice == nil {
		return nil, oeerrors.New("device connect not available")
	}
	if err := s.deps.ConnectDevice(ctx, id, connect); err != nil {
		return nil, err
	}
	return &api.OperationResp{Ok: true}, nil
}

func (s *Server) DeleteDevice(ctx context.Context, id string) (*api.OperationResp, error) {
	if s.deps.DeleteDevice == nil {
		return nil, oeerrors.New("device delete not available")
	}
	if err := s.deps.DeleteDevice(ctx, id); err != nil {
		return nil, err
	}
	return &api.OperationResp{Ok: true}, nil
}

func int64Value(value any) int64 {
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int32:
		return int64(typed)
	case int64:
		return typed
	case float64:
		return int64(typed)
	default:
		return 0
	}
}

func int32Value(value any) int32 {
	switch typed := value.(type) {
	case int:
		return int32(typed)
	case int32:
		return typed
	case int64:
		return int32(typed)
	case float64:
		return int32(typed)
	default:
		return -1
	}
}

func (s *Server) SaveLLMKey(ctx context.Context, key string) (*api.OperationResp, error) {
	if s.deps.SaveLLMKey == nil {
		return nil, oeerrors.New("llm key save not available")
	}
	if err := s.deps.SaveLLMKey(ctx, key); err != nil {
		return nil, err
	}
	return &api.OperationResp{Ok: true}, nil
}

func (s *Server) GetLLMConfig(ctx context.Context) (*api.LLMConfigResp, error) {
	if s.deps.LLMConfig == nil {
		return nil, oeerrors.New("llm config not available")
	}
	data := s.deps.LLMConfig(ctx)
	cfg, _ := data["config"].(llm.Config)
	resp := &api.LLMConfigResp{
		Ok: boolValue(data["ok"]),
		Config: &api.LLMConfig{
			Enabled:        cfg.Enabled,
			Provider:       cfg.Provider,
			BaseURL:        cfg.BaseURL,
			Model:          cfg.Model,
			APIKeyFile:     cfg.APIKeyFile,
			Mode:           cfg.Mode,
			Prompt:         cfg.Prompt,
			TimeoutSeconds: int32(cfg.TimeoutSeconds),
		},
	}
	if presets, ok := data["presets"].([]llm.Preset); ok {
		resp.Presets = make([]*api.LLMPreset, 0, len(presets))
		for _, preset := range presets {
			resp.Presets = append(resp.Presets, &api.LLMPreset{
				ID:          preset.ID,
				Name:        preset.Name,
				BaseURL:     preset.BaseURL,
				Model:       preset.Model,
				ModelHint:   preset.ModelHint,
				KeyHint:     preset.KeyHint,
				Description: preset.Description,
			})
		}
	}
	return resp, nil
}

func (s *Server) UpdateLLMConfig(ctx context.Context, req *api.LLMConfig) (*api.OperationResp, error) {
	if s.deps.UpdateLLMConfig == nil {
		return nil, oeerrors.New("llm config not available")
	}
	if req == nil {
		return nil, oeerrors.New("missing llm config")
	}
	if err := s.deps.UpdateLLMConfig(ctx, llm.Config{
		Enabled:        req.Enabled,
		Provider:       req.Provider,
		BaseURL:        req.BaseURL,
		Model:          req.Model,
		APIKeyFile:     req.APIKeyFile,
		Mode:           req.Mode,
		Prompt:         req.Prompt,
		TimeoutSeconds: int(req.TimeoutSeconds),
	}); err != nil {
		return nil, err
	}
	return &api.OperationResp{Ok: true}, nil
}

func (s *Server) TestLLM(ctx context.Context, provider string, live bool) (*api.LLMTestResp, error) {
	if s.deps.TestLLM == nil {
		return nil, oeerrors.New("llm test not available")
	}
	diagnostics := s.deps.TestLLM(ctx, provider, live)
	resp := &api.LLMTestResp{Ok: true, Diagnostics: make([]*api.LLMDiagnostic, 0, len(diagnostics))}
	for _, diagnostic := range diagnostics {
		resp.Diagnostics = append(resp.Diagnostics, &api.LLMDiagnostic{Ok: diagnostic.OK, Detail: diagnostic.Detail})
	}
	return resp, nil
}

func (s *Server) GetHistory(ctx context.Context) (*api.HistoryResp, error) {
	if s.deps.History == nil {
		return nil, oeerrors.New("history not available")
	}
	entries := s.deps.History(ctx)
	resp := &api.HistoryResp{Ok: true, Entries: make([]*api.HistoryEntry, 0, len(entries))}
	for _, entry := range entries {
		resp.Entries = append(resp.Entries, &api.HistoryEntry{
			ID:         entry.ID,
			Text:       entry.Text,
			RawText:    entry.RawText,
			Provider:   entry.Provider,
			Language:   entry.Language,
			Source:     entry.Source,
			SourceName: entry.SourceName,
			Address:    entry.Address,
			Processed:  entry.Processed,
			Truncated:  entry.Truncated,
			LatencyMs:  entry.LatencyMS,
			At:         entry.At.Format(time.RFC3339),
		})
	}
	return resp, nil
}

func (s *Server) DeleteHistory(ctx context.Context, id string) (*api.OperationResp, error) {
	if s.deps.DeleteHistory == nil {
		return nil, oeerrors.New("history not available")
	}
	if err := s.deps.DeleteHistory(ctx, id); err != nil {
		return nil, err
	}
	return &api.OperationResp{Ok: true}, nil
}

func (s *Server) ClearHistory(ctx context.Context) (*api.OperationResp, error) {
	if s.deps.ClearHistory == nil {
		return nil, oeerrors.New("history not available")
	}
	if err := s.deps.ClearHistory(ctx); err != nil {
		return nil, err
	}
	return &api.OperationResp{Ok: true}, nil
}

func (s *Server) GetTextConfig(ctx context.Context) (*api.TextConfigResp, error) {
	if s.deps.TextConfig == nil {
		return nil, oeerrors.New("text config not available")
	}
	data := s.deps.TextConfig(ctx)
	cfg, _ := data["config"].(text.Config)
	resp := &api.TextConfigResp{
		Ok: boolValue(data["ok"]),
		Config: &api.TextConfig{
			Dictionary: make([]*api.TextReplacement, 0, len(cfg.Dictionary)),
			Snippets:   make([]*api.TextSnippet, 0, len(cfg.Snippets)),
		},
	}
	for _, item := range cfg.Dictionary {
		resp.Config.Dictionary = append(resp.Config.Dictionary, &api.TextReplacement{From: item.From, To: item.To})
	}
	for _, item := range cfg.Snippets {
		resp.Config.Snippets = append(resp.Config.Snippets, &api.TextSnippet{Trigger: item.Trigger, Text: item.Text})
	}
	return resp, nil
}

func (s *Server) UpdateTextConfig(ctx context.Context, req *api.TextConfig) (*api.OperationResp, error) {
	if s.deps.UpdateTextConfig == nil {
		return nil, oeerrors.New("text config not available")
	}
	if req == nil {
		return nil, oeerrors.New("missing text config")
	}
	next := text.Config{}
	for _, item := range req.Dictionary {
		if item == nil {
			continue
		}
		next.Dictionary = append(next.Dictionary, text.Replacement{From: item.From, To: item.To})
	}
	for _, item := range req.Snippets {
		if item == nil {
			continue
		}
		next.Snippets = append(next.Snippets, text.Snippet{Trigger: item.Trigger, Text: item.Text})
	}
	if err := s.deps.UpdateTextConfig(ctx, next); err != nil {
		return nil, err
	}
	return &api.OperationResp{Ok: true}, nil
}
