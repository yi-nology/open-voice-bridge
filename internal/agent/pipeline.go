package agent

import (
	"context"
	"strings"
	"sync"
	"time"

	oeerrors "github.com/oekit/pkg/errors"

	"github.com/yi-nology/open-voice-bridge/internal/asr"
	"github.com/yi-nology/open-voice-bridge/internal/atvv"
	"github.com/yi-nology/open-voice-bridge/internal/llm"
	"github.com/yi-nology/open-voice-bridge/internal/text"
)

// SourceRef labels which input source produced an utterance so the shared
// transcription chain can stamp subtitles and history consistently.
type SourceRef struct {
	Kind    string
	Address string
}

// Utterance is one finished capture handed to the chain: samples plus the rate
// they were captured at. The rate travels with the audio because the remote
// negotiates it in ATVV caps, while a phone resamples to 16 kHz itself; a fixed
// label would misreport an 8 kHz device and fake the minimum-duration floor
// (DOM-01).
type Utterance struct {
	Samples    []int16
	SampleRate int
	Ref        SourceRef
}

// Rate is the capture rate, defaulting to the negotiated remote rate when a
// source did not measure one.
func (u Utterance) Rate() int {
	if u.SampleRate > 0 {
		return u.SampleRate
	}
	return atvv.SampleRate
}

// MinSamples is the shortest utterance worth a request: a tap shorter than
// 250 ms has never carried a word.
func (u Utterance) MinSamples() int { return u.Rate() / 4 }

// SpeechPipeline is the single transcription chain used by every input source:
// mono PCM to ASR, then text rules and LLM rewrite, then a Transcript
// event and optional desktop paste. The BLE remote drives it through finalize;
// phone sources call Transcribe directly, so the two paths can never diverge.
//
// Live collaborators are read through accessors, letting ASR/LLM/rule switches
// take effect without rebuilding the pipeline. Transcribe serialises engine
// calls so two sources never share one request.
type SpeechPipeline struct {
	mu           sync.Mutex
	engine       func() asr.Engine
	rules        func() *text.Rules
	processor    func() llm.Processor
	pasteEnabled func() bool
	pasteText    func(context.Context, string) error
	sink         func(Transcript)
	asrTimeout   time.Duration
}

// Transcribe processes one utterance. It reports false (without publishing)
// when the audio is too short, no engine is set, ASR found no speech, or the
// recognised text is empty.
func (p *SpeechPipeline) Transcribe(u Utterance) (Transcript, bool) {
	if len(u.Samples) < u.MinSamples() {
		return Transcript{}, false
	}
	engine := p.engine()
	if engine == nil {
		return Transcript{}, false
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), p.asrTimeout)
	defer cancel()
	result, err := engine.Transcribe(ctx, asr.Audio{
		Samples:    u.Samples,
		SampleRate: u.Rate(),
	})
	if oeerrors.Is(err, asr.ErrNoSpeech) {
		return Transcript{}, false
	}
	if err != nil {
		log.Warn("ASR failed", "err", err)
		return Transcript{}, false
	}
	rawText := result.Text
	if rawText == "" {
		return Transcript{}, false
	}
	processed := p.applyText(ctx, rawText)
	log.Info("recognized",
		"chars", len([]rune(processed)),
		"language", result.Language,
		"provider", result.Provider,
		"source", u.Ref.Kind,
		"latency", result.Latency.Round(time.Millisecond),
	)
	transcript := Transcript{
		Text:      processed,
		RawText:   rawText,
		Language:  result.Language,
		Provider:  result.Provider,
		Address:   u.Ref.Address,
		Processed: processed != rawText,
		Latency:   result.Latency,
		Duration:  time.Duration(len(u.Samples)) * time.Second / time.Duration(u.Rate()),
		At:        time.Now(),
	}
	if p.sink != nil {
		p.sink(transcript)
	}
	p.applyPaste(ctx, processed)
	return transcript, true
}

// applyText runs voice snippets, then LLM rewrite, then the user dictionary.
func (p *SpeechPipeline) applyText(parent context.Context, input string) string {
	rules := p.rules()
	if snippet, ok := rules.Snippet(input); ok {
		log.Info("voice snippet matched")
		return snippet
	}
	output := input
	if processor := p.processor(); processor != nil {
		ctx, cancel := context.WithTimeout(parent, 25*time.Second)
		processed, err := processor.Process(ctx, output)
		cancel()
		if err != nil {
			log.Warn("LLM post-process failed", "err", redactText(err.Error(), output))
		} else if strings.TrimSpace(processed) != "" {
			log.Info("text post-processed", "original", len(output), "processed", len(processed))
			output = processed
		}
	}
	return rules.Dictionary(output)
}

func (p *SpeechPipeline) applyPaste(_ context.Context, processed string) {
	if p.pasteEnabled == nil || !p.pasteEnabled() || p.pasteText == nil {
		return
	}
	pasteCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := p.pasteText(pasteCtx, processed); err != nil {
		log.Warn("paste failed", "err", err)
	}
}

// redactText cuts the sentence just processed out of a message before it is
// logged. Error strings from a remote service are untrusted text: a rejected
// request often quotes what it rejected, and here that is the transcript, which
// SECURITY.md promises is never written to the log file (SEC-02). The rest of
// the message - status codes, provider wording - stays, because a log line that
// says only "llm failed" gets the transcript added back the next time someone
// debugs.
func redactText(message, transcript string) string {
	transcript = strings.TrimSpace(transcript)
	if transcript == "" {
		return message
	}
	return strings.ReplaceAll(message, transcript, "<transcript>")
}
