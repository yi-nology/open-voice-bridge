package runtime

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/yi-nology/open-voice-bridge/internal/phone"

	"github.com/yi-nology/open-voice-bridge/internal/agent"
	"github.com/yi-nology/open-voice-bridge/internal/core"
)

// RegisterAgentMetrics installs the agent's collectors into the registry the
// caller owns. The registry is a parameter rather than the process global so
// two runtimes in one process each get their own counters and gauges: a gauge
// closes over its agent, and sharing one registration would silently report
// the first agent's state for both.
func RegisterAgentMetrics(ag *agent.Agent, registry prometheus.Registerer) (prometheus.Counter, prometheus.Histogram) {
	transcriptsTotal := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "xiaomi_remote_transcripts_total",
		Help: "Total number of recognized transcripts.",
	})
	asrLatency := prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "xiaomi_remote_asr_duration_seconds",
		Help:    "ASR latency per transcript.",
		Buckets: []float64{0.1, 0.25, 0.5, 1, 2, 5, 10, 30},
	})
	registry.MustRegister(
		transcriptsTotal,
		asrLatency,
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name: "xiaomi_remote_ble_connected",
			Help: "Whether the remote is connected over BLE (1/0).",
		}, func() float64 { return boolGauge(ag, func(v core.AgentView) bool { return v.Connected }) }),
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name: "xiaomi_remote_atvv_ready",
			Help: "Whether the ATVV voice bridge is ready (1/0).",
		}, func() float64 { return boolGauge(ag, func(v core.AgentView) bool { return v.Ready }) }),
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name: "xiaomi_remote_mic_healthy",
			Help: "Whether the virtual microphone node is present (1/0).",
		}, func() float64 { return boolGauge(ag, func(v core.AgentView) bool { return v.MicHealthy }) }),
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name: "xiaomi_remote_last_asr_latency_seconds",
			Help: "Latency of the most recent transcription in seconds.",
		}, func() float64 {
			return ag.Status().LastLatency.Seconds()
		}),
	)
	return transcriptsTotal, asrLatency
}

func boolGauge(ag *agent.Agent, pick func(core.AgentView) bool) float64 {
	if pick(ag.Status()) {
		return 1
	}
	return 0
}

// PhoneMetrics are the phone-facing instruments (R10 / OPS-12). They are
// created empty and registered by RegisterPhoneMetrics once the listener
// exists, so the transcription adapter can carry them before the server is
// built.
type PhoneMetrics struct {
	AudioSeconds prometheus.Counter
	Utterances   *prometheus.CounterVec
	Keys         prometheus.Counter
	InjectErrors prometheus.Counter
}

// NewPhoneMetrics builds unregistered phone instruments.
func NewPhoneMetrics() *PhoneMetrics {
	return &PhoneMetrics{
		AudioSeconds: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "xiaomi_remote_phone_audio_seconds_total",
			Help: "Seconds of phone microphone audio accepted for transcription.",
		}),
		Utterances: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "xiaomi_remote_phone_utterances_total",
			Help: "Phone utterances by result (ok: transcribed, miss: no usable text).",
		}, []string{"result"}),
		Keys: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "xiaomi_remote_phone_keys_total",
			Help: "Key presses injected on behalf of paired phones.",
		}),
		InjectErrors: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "xiaomi_remote_phone_inject_errors_total",
			Help: "Failed key injections from paired phones.",
		}),
	}
}

// RegisterPhoneMetrics registers the phone instruments plus a client gauge.
func RegisterPhoneMetrics(registry prometheus.Registerer, metrics *PhoneMetrics, connected func() int) {
	registry.MustRegister(
		metrics.AudioSeconds,
		metrics.Utterances,
		metrics.Keys,
		metrics.InjectErrors,
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name: "xiaomi_remote_phone_clients",
			Help: "Paired phones with a live session (1/0 per connected device).",
		}, func() float64 { return float64(connected()) }),
	)
}

// countingTransport wraps the helper transport so phone key presses are
// observable without the phone package knowing about metrics.
type countingTransport struct {
	inner phone.KeyTransport
	mux   *PhoneMetrics
}

func (c countingTransport) Inject(ctx context.Context, key string, value int) error {
	err := c.inner.Inject(ctx, key, value)
	if c.mux == nil {
		return err
	}
	if err != nil {
		c.mux.InjectErrors.Inc()
	} else if value == 1 {
		c.mux.Keys.Inc()
	}
	return err
}
