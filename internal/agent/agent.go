// Package agent coordinates BLE, ATVV audio, ASR and text injection.
package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	oeerrors "github.com/oekit/pkg/errors"

	"github.com/oekit/concurrency/async"
	"github.com/oekit/logx"

	"github.com/yi-nology/open-voice-bridge/internal/asr"
	"github.com/yi-nology/open-voice-bridge/internal/atvv"
	"github.com/yi-nology/open-voice-bridge/internal/audio"
	"github.com/yi-nology/open-voice-bridge/internal/bluez"
	"github.com/yi-nology/open-voice-bridge/internal/config"
	"github.com/yi-nology/open-voice-bridge/internal/core"
	"github.com/yi-nology/open-voice-bridge/internal/input"
	"github.com/yi-nology/open-voice-bridge/internal/keys"
	"github.com/yi-nology/open-voice-bridge/internal/llm"
	"github.com/yi-nology/open-voice-bridge/internal/paste"
	"github.com/yi-nology/open-voice-bridge/internal/redact"
	"github.com/yi-nology/open-voice-bridge/internal/text"
)

var log = logx.Delegating("agent")

type Agent struct {
	cfg     config.Config
	asr     asr.Engine
	asrMu   sync.RWMutex
	llm     llm.Processor
	llmMu   sync.RWMutex
	text    *text.Rules
	textMu  sync.RWMutex
	audio   audio.Sink
	audioMu sync.Mutex
	watcher keys.F24Watcher

	hub *input.Hub

	mu          sync.Mutex
	addrMu      sync.RWMutex
	address     string
	transport   *bluez.Transport
	session     *atvv.Session
	connected   bool
	ready       bool
	lastText    string
	lastAt      time.Time
	lastLatency time.Duration

	dictMu        sync.Mutex
	dictation     []int16
	dictTruncated bool // the open capture hit its time cap (DOM-09)
	maxSample     int

	pipeline *SpeechPipeline

	reconnectCh chan struct{}
	transcripts chan Transcript

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	closeOnce sync.Once
	closeErr  error
}

// Transcript is one finalized utterance emitted for live subtitles.
type Transcript struct {
	Text       string
	RawText    string
	Language   string
	Provider   string
	Source     string // "remote" | "phone" (SourceRef.Kind)
	SourceName string
	Address    string
	Processed  bool
	Truncated  bool   // the capture hit its time cap (DOM-09)
	Command    string // non-empty: a voice command ran this combo (语音命令)
	Latency    time.Duration
	Duration   time.Duration // length of the spoken audio
	At         time.Time
}

func New(cfg config.Config, engine asr.Engine, sink audio.Sink) *Agent {
	a := &Agent{
		cfg:         cfg,
		address:     cfg.Address,
		asr:         engine,
		audio:       sink,
		watcher:     keys.NewF24Watcher(),
		reconnectCh: make(chan struct{}, 1),
		transcripts: make(chan Transcript, 16),
		maxSample:   core.Cap(cfg.MaxUtterance),
	}
	a.pipeline = a.newPipeline()
	return a
}

// newPipeline wires the shared transcription chain to the agent's live
// collaborators so provider/rule/LLM switches apply without rebuilding it.
func (a *Agent) newPipeline() *SpeechPipeline {
	return &SpeechPipeline{
		engine: func() asr.Engine {
			a.asrMu.RLock()
			defer a.asrMu.RUnlock()
			return a.asr
		},
		rules: func() *text.Rules {
			a.textMu.RLock()
			defer a.textMu.RUnlock()
			return a.text
		},
		processor: func() llm.Processor {
			a.llmMu.RLock()
			defer a.llmMu.RUnlock()
			return a.llm
		},
		pasteEnabled: func() bool { return a.cfg.PasteEnabled },
		pasteText:    paste.Text,
		sendKeys:     paste.Key,
		sink:         a.recordTranscript,
		asrTimeout:   180 * time.Second,
	}
}

// Speech exposes the shared transcription chain so another input source (the
// phone listener) can run an utterance through exactly the same path as the
// BLE remote: same engine, same rules, same subtitle and history sink.
func (a *Agent) Speech() *SpeechPipeline { return a.pipeline }

// InsertPhrase pastes the phrase bound to slot n via the shared pipeline —
// the UI picker path beside the phrase1..phrase8 key actions.
func (a *Agent) InsertPhrase(slot int) (string, bool) {
	return a.pipeline.InsertPhrase(slot)
}

// recordTranscript updates the agent's last-result fields and forwards the
// utterance to the subtitle stream.
func (a *Agent) recordTranscript(transcript Transcript) {
	if transcript.Command != "" {
		// Voice commands are spoken acts, not dictation: they stay out of the
		// "last text" strip even though the subtitle stream still sees them.
		a.transcripts <- transcript
		return
	}
	a.mu.Lock()
	a.lastText = transcript.Text
	a.lastAt = transcript.At
	a.lastLatency = transcript.Latency
	a.mu.Unlock()
	a.publishTranscript(transcript)
}

// Transcripts yields finalized utterances for the subtitle stream.
func (a *Agent) Transcripts() <-chan Transcript { return a.transcripts }

// SetHub registers the BLE remote as an input source. It is safe to leave the
// hub unset; the agent behaves exactly as before when no hub is provided.
func (a *Agent) SetHub(hub *input.Hub) {
	a.mu.Lock()
	a.hub = hub
	a.mu.Unlock()
	// Register outside the lock: the hub calls back into Status (DOM-14).
	if hub != nil {
		hub.Register(remoteSource{a})
	}
}

// audioActive reports whether the remote is capturing dictation samples now.
func (a *Agent) audioActive() bool {
	a.dictMu.Lock()
	defer a.dictMu.Unlock()
	return len(a.dictation) > 0
}

// currentHub reads the input hub under the agent lock (DOM-14).
func (a *Agent) currentHub() *input.Hub {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.hub
}

// acquireMic claims the microphone for BLE dictation. The remote preempts: a
// phone that started first must not cost the RC003 an utterance.
func (a *Agent) acquireMic() {
	hub := a.currentHub()
	if hub == nil {
		return
	}
	hub.AcquireAudioForce(input.RemoteID)
}

// releaseMic frees the microphone when the capture closes, before ASR runs: the
// pipeline already serializes transcription, so holding the claim across it would
// keep phones at "busy" for the whole recognition window and buy nothing.
func (a *Agent) releaseMic() {
	hub := a.currentHub()
	if hub == nil {
		return
	}
	hub.ReleaseAudio(input.RemoteID)
}

func (a *Agent) publishTranscript(transcript Transcript) {
	select {
	case a.transcripts <- transcript:
	default:
		log.Debug("transcript stream full; dropping subtitle line")
	}
}

func (a *Agent) Start(parent context.Context) error {
	a.ctx, a.cancel = context.WithCancel(parent)
	if err := a.watcher.Start(a.ctx); err != nil {
		return oeerrors.Errorf("start voice-key watcher: %w", err)
	}
	a.wg.Add(2)
	async.GoSafe(func() {
		defer a.wg.Done()
		a.watchF24()
	})
	async.GoSafe(func() {
		defer a.wg.Done()
		a.connectLoop()
	})
	return nil
}

func (a *Agent) Close(ctx context.Context) error {
	a.closeOnce.Do(func() {
		if a.cancel != nil {
			a.cancel()
		}
		var failures []error
		if a.watcher != nil {
			failures = append(failures, a.watcher.Close(ctx))
		}
		a.mu.Lock()
		transport := a.transport
		a.mu.Unlock()
		if transport != nil {
			failures = append(failures, transport.Close(ctx))
		}
		a.audioMu.Lock()
		sink := a.audio
		a.audioMu.Unlock()
		if sink != nil {
			failures = append(failures, sink.Close(ctx))
		}
		a.asrMu.RLock()
		engine := a.asr
		a.asrMu.RUnlock()
		if engine != nil {
			failures = append(failures, engine.Close(ctx))
		}
		a.llmMu.RLock()
		processor := a.llm
		a.llmMu.RUnlock()
		if processor != nil {
			failures = append(failures, processor.Close(ctx))
		}
		a.wg.Wait()
		a.closeErr = joinAgentErrors(failures)
	})
	return a.closeErr
}

func joinAgentErrors(errs []error) error {
	var out error
	for _, err := range errs {
		out = oeerrors.Append(out, err)
	}
	return out
}

func (a *Agent) SetTextRules(rules *text.Rules) {
	a.textMu.Lock()
	a.text = rules
	a.textMu.Unlock()
}

func (a *Agent) SetLLM(processor llm.Processor) {
	a.llmMu.Lock()
	old := a.llm
	a.llm = processor
	a.llmMu.Unlock()
	if old != nil {
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := old.Close(closeCtx); err != nil {
			log.Warn("close previous LLM processor", "err", err)
		}
		cancel()
	}
	if processor != nil {
		log.Info("LLM post-processor switched", "name", processor.Name())
	}
}

func (a *Agent) SetEngine(engine asr.Engine) {
	a.asrMu.Lock()
	old := a.asr
	a.asr = engine
	a.asrMu.Unlock()
	if old != nil {
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := old.Close(closeCtx); err != nil {
			log.Warn("close previous ASR engine", "err", err)
		}
		cancel()
	}
	if engine != nil {
		log.Info("ASR provider switched", "chain", engine.Name())
	}
}

func (a *Agent) watchF24() {
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-a.watcher.C():
			a.micOpen()
		}
	}
}

func (a *Agent) micOpen() {
	a.mu.Lock()
	session := a.session
	transport := a.transport
	a.mu.Unlock()
	if session == nil || transport == nil || session.Caps() == nil {
		return
	}
	log.Info("HID voice key -> MIC_OPEN")
	async.GoSafe(func() {
		if err := transport.WriteTX(session.MicOpenCommand()); err != nil {
			log.Warn("MIC_OPEN failed", "err", err)
		}
	})
}

func (a *Agent) connectLoop() {
	backoff := time.Second
	for {
		if a.ctx.Err() != nil {
			return
		}
		transport := bluez.New(a.currentAddress())
		if err := transport.Connect(a.ctx); err != nil {
			log.Warn("BLE connect failed", "err", err, "retry_in", backoff)
			if !sleepCtx(a.ctx, backoff) {
				return
			}
			if backoff < 30*time.Second {
				backoff *= 2
			}
			continue
		}
		session := atvv.NewSession()
		a.mu.Lock()
		a.transport = transport
		a.session = session
		a.connected = true
		a.ready = false
		a.mu.Unlock()
		if err := transport.Subscribe(a.handleAudio, a.handleControl); err != nil {
			log.Warn("ATVV subscribe failed", "err", err, "retry_in", backoff)
			if err := transport.Close(a.ctx); err != nil {
				log.Warn("close transport", "err", err)
			}
			a.markDisconnected()
			if !sleepCtx(a.ctx, backoff) {
				return
			}
			if backoff < 30*time.Second {
				backoff *= 2
			}
			continue
		}
		if err := transport.WriteTX(atvv.GetCapabilities); err != nil {
			log.Warn("GET_CAPABILITIES failed", "err", err, "retry_in", backoff)
			if err := transport.Close(a.ctx); err != nil {
				log.Warn("close transport", "err", err)
			}
			a.markDisconnected()
			if !sleepCtx(a.ctx, backoff) {
				return
			}
			if backoff < 30*time.Second {
				backoff *= 2
			}
			continue
		}
		log.Info("ATVV bridge ready", "address", redact.MAC(a.cfg.Address))
		backoff = time.Second
		select {
		case <-a.ctx.Done():
			if err := transport.Close(a.ctx); err != nil {
				log.Warn("close transport", "err", err)
			}
			a.markDisconnected()
			return
		case <-transport.Disconnected():
			log.Warn("BLE disconnected")
		case <-a.reconnectCh:
			log.Info("BLE reconnect requested")
		}
		if err := transport.Close(a.ctx); err != nil {
			log.Warn("close transport", "err", err)
		}
		a.markDisconnected()
		if !sleepCtx(a.ctx, 2*time.Second) {
			return
		}
	}
}

func (a *Agent) currentAddress() string {
	a.addrMu.RLock()
	defer a.addrMu.RUnlock()
	return a.address
}

// SetAddress switches the active Bluetooth device and reconnects.
func (a *Agent) SetAddress(address string) error {
	address = strings.TrimSpace(address)
	if address == "" {
		return oeerrors.New("device address is empty")
	}
	a.addrMu.Lock()
	a.address = address
	a.addrMu.Unlock()
	log.Info("BLE address switched", "address", redact.MAC(address))
	return a.Reconnect()
}

func (a *Agent) markDisconnected() {
	// A capture cut short by a disconnect never reaches the audio-stop path, but
	// the samples in hand are words the user already spoke, so they are
	// transcribed before the claim drops (I5). During shutdown the request would
	// paste into a desktop that is going away, so the buffer is simply lost.
	if a.ctx == nil || a.ctx.Err() == nil {
		a.closeCapture()
	}
	// The claim has to go here either way, or phones would find the microphone
	// busy forever.
	a.releaseMic()
	a.mu.Lock()
	a.connected = false
	a.ready = false
	a.transport = nil
	a.session = nil
	a.mu.Unlock()
}

func (a *Agent) handleControl(data []byte) {
	a.mu.Lock()
	session := a.session
	transport := a.transport
	a.mu.Unlock()
	if session == nil {
		return
	}
	switch session.HandleControl(data) {
	case atvv.EventCaps:
		caps := session.Caps()
		if caps != nil {
			log.Info("ATVV caps",
				"version", fmt.Sprintf("0x%04x", caps.Version),
				"codec", fmt.Sprintf("0x%02x", caps.Codecs),
				"frame", caps.FrameSize,
				"rate", caps.SampleRate,
			)
			if caps.SampleRate != core.SampleRate {
				// The decoder is rate-agnostic, but cloud providers are not:
				// an 8 kHz remote will be refused by MiMo instead of being
				// silently transcribed as sped-up speech (DOM-01).
				log.Warn("remote negotiated a non-16 kHz codec", "rate", caps.SampleRate)
			}
		}
		a.mu.Lock()
		a.ready = true
		a.mu.Unlock()
	case atvv.EventMicButton:
		log.Info("ATVV mic button -> MIC_OPEN")
		if transport != nil {
			async.GoSafe(func() {
				if err := transport.WriteTX(session.MicOpenCommand()); err != nil {
					log.Warn("MIC_OPEN failed", "err", err)
				}
			})
		}
	case atvv.EventAudioStart:
		log.Info("audio stream started")
		a.acquireMic()
		a.resetDictation()
	case atvv.EventAudioStartDup:
		// The device started a new capture without our seeing an audio-stop, so
		// the buffered samples belong to the previous sentence. Drain them here
		// and keep the microphone claim, or the two would merge into one
		// utterance (DOM-03).
		log.Info("duplicate audio start -> closing the open capture")
		a.closeCapture()
	case atvv.EventAudioStop:
		log.Info("audio stream stopped")
		a.releaseMic()
		a.closeCapture()
	}
}

func (a *Agent) handleAudio(data []byte) {
	a.mu.Lock()
	session := a.session
	a.mu.Unlock()
	if session == nil {
		return
	}
	samples := session.HandleAudio(data)
	if len(samples) == 0 {
		return
	}
	a.audioMu.Lock()
	sink := a.audio
	a.audioMu.Unlock()
	if sink != nil {
		if err := sink.Write(samples); err != nil {
			log.Warn("virtual mic write failed", "err", err)
		}
	}
	a.dictMu.Lock()
	room := a.maxSample - len(a.dictation)
	if room > 0 {
		if room > len(samples) {
			room = len(samples)
		}
		a.dictation = append(a.dictation, samples[:room]...)
	}
	if room < len(samples) {
		// The capture ran into its time cap (DOM-09): label it, same as the
		// phone's too_long path.
		a.dictTruncated = true
	}
	a.dictMu.Unlock()
}

// resetDictation opens a fresh capture. It must not reuse the previous backing
// array: that array may still be on its way to the ASR request (DOM-02).
func (a *Agent) resetDictation() {
	a.dictMu.Lock()
	a.dictation = nil
	a.dictTruncated = false
	a.dictMu.Unlock()
}

// takeDictation drains the current capture and returns sole ownership of its
// samples together with the truncation flag.
func (a *Agent) takeDictation() ([]int16, bool) {
	a.dictMu.Lock()
	defer a.dictMu.Unlock()
	samples := a.dictation
	truncated := a.dictTruncated
	a.dictation = nil
	a.dictTruncated = false
	return samples, truncated
}

// closeCapture ends the capture that is open now. Draining happens on the
// dispatch goroutine, before any later audio-start can touch the buffer, and
// only the request runs in the background (I2).
func (a *Agent) closeCapture() {
	samples, truncated := a.takeDictation()
	if len(samples) == 0 {
		return
	}
	rate := a.currentSampleRate()
	async.GoSafe(func() { a.runSpeech(samples, rate, truncated) })
}

// finalize drains and transcribes inline; it is the synchronous form used when
// the caller must observe the result before returning.
func (a *Agent) finalize() {
	samples, truncated := a.takeDictation()
	a.runSpeech(samples, a.currentSampleRate(), truncated)
}

func (a *Agent) runSpeech(samples []int16, rate int, truncated bool) {
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	a.pipeline.Transcribe(ctx, Utterance{
		Samples:    samples,
		SampleRate: rate,
		Ref: SourceRef{
			Kind:    string(input.KindRemote),
			Name:    a.cfg.DeviceName(),
			Address: a.currentAddress(),
		},
		Truncated: truncated,
	})
}

// currentSampleRate reports the rate the remote negotiated, defaulting to
// 16 kHz before caps arrive.
func (a *Agent) currentSampleRate() int {
	a.mu.Lock()
	session := a.session
	a.mu.Unlock()
	if session == nil {
		return core.SampleRate
	}
	return session.SampleRate()
}

func (a *Agent) Reconnect() error {
	select {
	case a.reconnectCh <- struct{}{}:
	default:
	}
	return nil
}

func (a *Agent) RestartAudio(ctx context.Context) error {
	a.audioMu.Lock()
	old := a.audio
	a.audioMu.Unlock()
	if old != nil {
		if err := old.Close(ctx); err != nil {
			log.Warn("close previous audio sink", "err", err)
		}
	}
	sink, err := audio.New(audio.Config{
		SinkName:   a.cfg.SinkName,
		SourceName: a.cfg.SourceName,
	})
	if err != nil {
		return err
	}
	// The sink's processes and watchdog live on the agent's lifetime context,
	// not on this request's (CON-05): a restarted sink must survive the HTTP
	// call that asked for it.
	lifeCtx := a.ctx
	if lifeCtx == nil {
		lifeCtx = context.Background()
	}
	if err := sink.Start(lifeCtx); err != nil {
		return err
	}
	a.audioMu.Lock()
	a.audio = sink
	a.audioMu.Unlock()
	return a.Reconnect()
}

func (a *Agent) Status() core.AgentView {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.asrMu.RLock()
	engine := a.asr
	a.asrMu.RUnlock()
	view := core.AgentView{
		Connected:   a.connected,
		Ready:       a.ready,
		LastText:    a.lastText,
		LastAt:      a.lastAt,
		MicHealthy:  a.micHealthyLocked(),
		LastLatency: a.lastLatency,
		ASR:         "unknown",
	}
	if engine != nil {
		view.ASR = engine.Name()
		view.ASRLocal = engine.Local()
	}
	return view
}

func (a *Agent) micHealthyLocked() bool {
	a.audioMu.Lock()
	sink := a.audio
	a.audioMu.Unlock()
	return sink != nil && sink.Healthy()
}

func (a *Agent) ASRName() string {
	a.asrMu.RLock()
	defer a.asrMu.RUnlock()
	if a.asr == nil {
		return "unknown"
	}
	return a.asr.Name()
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
