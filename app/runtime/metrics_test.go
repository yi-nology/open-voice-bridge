package runtime

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/yi-nology/open-voice-bridge/internal/agent"
	"github.com/yi-nology/open-voice-bridge/internal/asr"
	"github.com/yi-nology/open-voice-bridge/internal/config"
)

type idleSink struct{ healthy bool }

func (idleSink) Start(context.Context) error { return nil }
func (idleSink) Write([]int16) error         { return nil }
func (idleSink) Close(context.Context) error { return nil }
func (idleSink) Name() string                { return "idle" }
func (s idleSink) Healthy() bool             { return s.healthy }

func testAgent(t *testing.T, micHealthy bool) *agent.Agent {
	t.Helper()
	cfg, err := config.Load(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatalf("load defaults: %v", err)
	}
	return agent.New(cfg, asr.NewUnavailable(errors.New("no provider")), idleSink{healthy: micHealthy})
}

func gaugeValue(t *testing.T, registry *prometheus.Registry, name string) float64 {
	t.Helper()
	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	for _, family := range families {
		if family.GetName() != name {
			continue
		}
		for _, metric := range family.GetMetric() {
			if metric.Gauge != nil {
				return metric.GetGauge().GetValue()
			}
		}
	}
	t.Fatalf("%s was not registered", name)
	return 0
}

// ARC-07: the composition root has to be constructible more than once in a
// process, otherwise nothing in it can be tested. Registering into the process
// global panicked on the second construction.
func TestAgentMetricsCanBeRegisteredTwice(t *testing.T) {
	RegisterAgentMetrics(testAgent(t, true), prometheus.NewRegistry())
	RegisterAgentMetrics(testAgent(t, true), prometheus.NewRegistry())
}

// Each runtime's gauges must read its own agent. Sharing one registration
// would report the first agent's microphone health for both — the same silent
// rewiring the backend global used to do (ARC-06).
func TestAgentMetricsGaugesFollowTheirOwnAgent(t *testing.T) {
	healthy := prometheus.NewRegistry()
	broken := prometheus.NewRegistry()
	RegisterAgentMetrics(testAgent(t, true), healthy)
	RegisterAgentMetrics(testAgent(t, false), broken)

	if got := gaugeValue(t, healthy, "xiaomi_remote_mic_healthy"); got != 1 {
		t.Errorf("healthy runtime mic gauge = %v, want 1", got)
	}
	if got := gaugeValue(t, broken, "xiaomi_remote_mic_healthy"); got != 0 {
		t.Errorf("broken runtime mic gauge = %v, want 0", got)
	}
}

// The writable collectors are returned so the event loop can record into them;
// they must land in the caller's registry, not a private one.
func TestAgentMetricsCollectorsAreScrapeable(t *testing.T) {
	registry := prometheus.NewRegistry()
	transcripts, latency := RegisterAgentMetrics(testAgent(t, true), registry)
	transcripts.Inc()
	latency.Observe(0.5)

	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	seen := map[string]bool{}
	for _, family := range families {
		seen[family.GetName()] = true
	}
	for _, name := range []string{
		"xiaomi_remote_transcripts_total",
		"xiaomi_remote_asr_duration_seconds",
		"xiaomi_remote_ble_connected",
		"xiaomi_remote_atvv_ready",
		"xiaomi_remote_last_asr_latency_seconds",
	} {
		if !seen[name] {
			t.Errorf("%s missing from the registry", name)
		}
	}
}
