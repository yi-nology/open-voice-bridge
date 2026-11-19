package speech

import (
	"context"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/oekit/logx"
	oeerrors "github.com/oekit/pkg/errors"

	"github.com/yi-nology/open-voice-bridge/internal/asr"
	"github.com/yi-nology/open-voice-bridge/internal/core"
	"github.com/yi-nology/open-voice-bridge/internal/llm"
)

var log = logx.Delegating("speech")

// SourceRef labels which input source produced an utterance so the shared
// transcription chain can stamp subtitles and history consistently (I8).
type SourceRef struct {
	Kind    string
	Name    string
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
	// Truncated marks a capture that hit its time cap instead of ending on
	// the user's release (DOM-09): both sources label it the same way.
	Truncated bool
}

// Rate is the capture rate, defaulting to the negotiated remote rate when a
// source did not measure one.
func (u Utterance) Rate() int {
	if u.SampleRate > 0 {
		return u.SampleRate
	}
	return core.SampleRate
}

// MinSamples is the shortest utterance worth a request: a tap shorter than
// 250 ms has never carried a word.
func (u Utterance) MinSamples() int { return core.MinUtterance(u.Rate()) }

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
	Partial    bool   // live in-flight recognition for the subtitle line (S6)
	Latency    time.Duration
	Duration   time.Duration // length of the spoken audio
	At         time.Time
}

// Pipeline is the single transcription chain used by every input source:
// mono PCM to ASR, then text rules and LLM rewrite, then a Transcript
// event and optional desktop paste. The BLE remote drives it through finalize;
// phone sources call Transcribe directly, so the two paths can never diverge.
//
// Live collaborators are read through accessors, letting ASR/LLM/rule switches
// take effect without rebuilding the pipeline. Collaborator closures are
// expected to be safe for concurrent use; the pipeline itself serialises only
// the desktop paste, never the ASR request (CON-03).
type Pipeline struct {
	pasteMu      sync.Mutex
	engine       func() asr.Engine
	rules        func() core.TextRules
	processor    func() llm.Processor
	pasteEnabled func() bool
	pasteText    func(context.Context, string) error
	sendKeys     func(context.Context, string) error
	// rewriteMode resolves the app-aware rewrite mode (S5): "" follows the
	// global config, "off" keeps the words literal.
	rewriteMode func() string
	sink        func(Transcript)
	asrTimeout  time.Duration
}

// Options wires the pipeline to its live collaborators; the producer of a
// Pipeline is the composition root (or a test), never the pipeline itself.
type Options struct {
	Engine       func() asr.Engine
	Rules        func() core.TextRules
	Processor    func() llm.Processor
	PasteEnabled func() bool
	PasteText    func(context.Context, string) error
	SendKeys     func(context.Context, string) error
	RewriteMode  func() string
	Sink         func(Transcript)
	ASRTimeout   time.Duration
}

// New builds a pipeline from live collaborator accessors.
func New(opts Options) *Pipeline {
	return &Pipeline{
		engine:       opts.Engine,
		rules:        opts.Rules,
		processor:    opts.Processor,
		pasteEnabled: opts.PasteEnabled,
		pasteText:    opts.PasteText,
		sendKeys:     opts.SendKeys,
		rewriteMode:  opts.RewriteMode,
		sink:         opts.Sink,
		asrTimeout:   opts.ASRTimeout,
	}
}

// emptyRules is the rulebook used when none is configured yet. The shared
// chain must not crash on a nil rules face — an agent assembled before the
// dictionary lands used to panic here, and the async recovery dropped the
// whole sentence silently.
type emptyRules struct{}

func (emptyRules) Command(string) (string, bool)  { return "", false }
func (emptyRules) SnippetAt(int) (string, bool)   { return "", false }
func (emptyRules) Snippet(string) (string, bool)  { return "", false }
func (emptyRules) Dictionary(input string) string { return input }

// textRules reads the live rules face; nil reads as the empty rulebook.
func (p *Pipeline) textRules() core.TextRules {
	if rules := p.rules(); rules != nil {
		return rules
	}
	return emptyRules{}
}

// budgetFor scales the ASR deadline with the captured audio: a one-second tap
// must not inherit the whole long-dictation window, and a hung provider must
// not hold the serial chain that long (CON-03). asrTimeout is the hard cap.
func (p *Pipeline) budgetFor(u Utterance) time.Duration {
	rate := u.Rate()
	audio := time.Duration(len(u.Samples)) * time.Second / time.Duration(rate)
	budget := 20*time.Second + 8*audio
	if budget > p.asrTimeout {
		budget = p.asrTimeout
	}
	return budget
}

// Transcribe processes one utterance. It reports false (without publishing)
// when the audio is too short, no engine is set, ASR found no speech, the
// recognised text is empty, or the caller's context ended (CON-03).
//
// The request runs without holding a pipeline-wide lock: a wedged provider
// costs its own deadline, never the other input sources'. Each call builds its
// own engine request, so two sources never share one.
func (p *Pipeline) Transcribe(parent context.Context, u Utterance) (Transcript, bool) {
	if parent == nil {
		parent = context.Background()
	}
	if len(u.Samples) < u.MinSamples() {
		return Transcript{}, false
	}
	// A capture that never rises above room tone has nothing to transcribe.
	// Cloud engines answer dead air with a confident short line ("你好。",
	// "1234567890"), so a mis-tap would otherwise turn into a wrong sentence.
	if !core.CarriesSpeech(u.Samples) {
		// Level and duration, never the samples: this is what separates "the
		// user tapped" from "the capture is broken".
		log.Info("capture below the speech floor; not sending it to ASR",
			"seconds", secondsOf(u), "peak", core.Peak(u.Samples))
		return Transcript{}, false
	}
	engine := p.engine()
	if engine == nil {
		return Transcript{}, false
	}

	ctx, cancel := context.WithTimeout(parent, p.budgetFor(u))
	defer cancel()
	samples, rate := engineDoor(u)
	result, err := engine.Transcribe(ctx, asr.Audio{
		Samples:    samples,
		SampleRate: rate,
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
	// 语音命令: an exact command utterance runs its key combo instead of
	// becoming text — no rules, no rewrite, no paste, no history. The match
	// is on the raw recognition so the dictionary cannot rewrite a command.
	if combo, ok := p.textRules().Command(rawText); ok {
		if err := p.sendKeys(ctx, combo); err != nil {
			log.Warn("voice command failed; treating as dictation", "combo", combo, "err", err)
		} else {
			log.Info("voice command", "combo", combo, "chars", len([]rune(rawText)))
			command := Transcript{
				Text:     strings.TrimSpace(rawText),
				RawText:  rawText,
				Provider: result.Provider,
				Source:   u.Ref.Kind,
				Latency:  result.Latency,
				Command:  combo,
				At:       time.Now(),
			}
			if p.sink != nil {
				p.sink(command)
			}
			return command, true
		}
	}
	processed := p.applyText(ctx, rawText)
	log.Info("recognized",
		"chars", len([]rune(processed)),
		"seconds", secondsOf(u),
		"peak", core.Peak(u.Samples),
		"language", result.Language,
		"provider", result.Provider,
		"source", u.Ref.Kind,
		"latency", result.Latency.Round(time.Millisecond),
	)
	transcript := Transcript{
		Text:       processed,
		RawText:    rawText,
		Language:   result.Language,
		Provider:   result.Provider,
		Source:     u.Ref.Kind,
		SourceName: u.Ref.Name,
		Address:    u.Ref.Address,
		Processed:  processed != rawText,
		Truncated:  u.Truncated,
		Latency:    result.Latency,
		Duration:   time.Duration(len(u.Samples)) * time.Second / time.Duration(u.Rate()),
		At:         time.Now(),
	}
	if p.sink != nil {
		p.sink(transcript)
	}
	p.applyPaste(ctx, processed)
	return transcript, true
}

// TranscribePartial recognises the capture so far for the live subtitle line
// (S6). It never rewrites, pastes or publishes history — the final
// Transcribe after release owns all of that.
func (p *Pipeline) TranscribePartial(parent context.Context, u Utterance) (string, error) {
	if parent == nil {
		parent = context.Background()
	}
	if len(u.Samples) < u.MinSamples() {
		return "", asr.ErrNoSpeech
	}
	// Live partials run on short in-flight buffers, which is exactly where dead
	// air turns into a hallucinated digit run; the same floor applies here.
	if !core.CarriesSpeech(u.Samples) {
		return "", asr.ErrNoSpeech
	}
	engine := p.engine()
	if engine == nil {
		return "", asr.ErrNoSpeech
	}
	ctx, cancel := context.WithTimeout(parent, 8*time.Second)
	defer cancel()
	samples, rate := engineDoor(u)
	result, err := engine.Transcribe(ctx, asr.Audio{Samples: samples, SampleRate: rate})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(result.Text), nil
}

// engineDoor is where the chain's honest rate meets the engines' geometry:
// an 8 kHz remote keeps 8 kHz everywhere else (duration floors, diagnostics),
// but MiMo refuses anything but 16 kHz and the other engines prefer it — so
// the samples are resampled at the door, once.
func engineDoor(u Utterance) ([]int16, int) {
	rate := u.Rate()
	if rate == core.SampleRate {
		return u.Samples, rate
	}
	return core.Resample(u.Samples, rate, core.SampleRate), core.SampleRate
}

// secondsOf is the capture length rounded to a tenth, for the diagnostics
// line: duration and level say whether a capture carried speech without ever
// carrying the words (SEC-02).
func secondsOf(u Utterance) float64 {
	if u.Rate() <= 0 {
		return 0
	}
	return math.Round(float64(len(u.Samples))*10/float64(u.Rate())) / 10
}

// applyText runs voice snippets, then LLM rewrite, then the user dictionary.
func (p *Pipeline) applyText(parent context.Context, input string) string {
	rules := p.textRules()
	if snippet, ok := rules.Snippet(input); ok {
		log.Info("voice snippet matched")
		return snippet
	}
	output := input
	mode := ""
	if p.rewriteMode != nil {
		mode = p.rewriteMode()
	}
	if processor := p.processor(); processor != nil && mode != "off" {
		ctx, cancel := context.WithTimeout(parent, 25*time.Second)
		var processed string
		var err error
		if mp, ok := processor.(llm.ModeProcessor); ok {
			processed, err = mp.ProcessMode(ctx, output, mode)
		} else {
			processed, err = processor.Process(ctx, output)
		}
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

func (p *Pipeline) applyPaste(_ context.Context, processed string) {
	if p.pasteEnabled == nil || !p.pasteEnabled() || p.pasteText == nil {
		return
	}
	// Keystrokes are a shared physical resource: two concurrent recognitions
	// must not interleave characters in the focused window, so the paste
	// itself is serialised even though ASR is not.
	p.pasteMu.Lock()
	defer p.pasteMu.Unlock()
	pasteCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := p.pasteText(pasteCtx, processed); err != nil {
		log.Warn("paste failed", "err", err)
	}
}

// InsertPhrase pastes the phrase bound to slot n (1-based) into the focused
// window — the "one key, one sentence" path of the common-phrase feature. It
// reports false when the slot is unbound so a stale keymap stays a no-op.
func (p *Pipeline) InsertPhrase(slot int) (string, bool) {
	phrase, ok := p.textRules().SnippetAt(slot)
	if !ok || phrase == "" {
		return "", false
	}
	p.applyPaste(context.Background(), phrase)
	return phrase, true
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
