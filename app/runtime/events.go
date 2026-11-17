package runtime

import (
	"context"
	"github.com/yi-nology/open-voice-bridge/internal/speech"
	"time"

	"github.com/oekit/logx"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/yi-nology/open-voice-bridge/internal/agent"
	"github.com/yi-nology/open-voice-bridge/internal/core"
	"github.com/yi-nology/open-voice-bridge/internal/history"
	"github.com/yi-nology/open-voice-bridge/internal/input"
)

// HistorySink is the history store as the event loop uses it.
type HistorySink interface {
	Append(history.Entry) error
}

// UsageSink accumulates speaking time and character counts.
type UsageSink interface {
	Record(speech time.Duration, chars int)
}

// Events fans the agent's three streams out to the durable stores, the metrics
// and the desktop's live event feed. It is the only place a transcript becomes
// history, so the privacy rules for each stream live here.
type Events struct {
	Transcripts      <-chan speech.Transcript
	Microphone       <-chan input.AudioState
	Phrases          <-chan agent.PhraseEvent
	Pickers          <-chan agent.PickerEvent
	Sources          func() []input.Status
	History          HistorySink
	Usage            UsageSink
	Publish          func(core.SSEEvent)
	TranscriptsTotal prometheus.Counter
	ASRLatency       prometheus.Histogram
	Logger           logx.Logger
}

// Run consumes until ctx ends.
func (e Events) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case transcript := <-e.Transcripts:
			if transcript.Partial {
				e.Publish(core.TranscriptPartialEvent{
					Event:   "transcript",
					Partial: true,
					Text:    transcript.Text,
					At:      transcript.At.Format(time.RFC3339),
				})
				continue
			}
			if transcript.Command != "" {
				e.Publish(core.CommandEvent{
					Event:  "command",
					Text:   transcript.Text,
					Combo:  transcript.Command,
					Source: transcript.Source,
					At:     transcript.At.Format(time.RFC3339),
				})
				continue
			}
			e.TranscriptsTotal.Inc()
			if transcript.Latency > 0 {
				e.ASRLatency.Observe(transcript.Latency.Seconds())
			}
			// ID stays empty: the store mints it and guards same-tick uniqueness (DOM-14).
			if err := e.History.Append(history.Entry{
				Text:       transcript.Text,
				RawText:    transcript.RawText,
				Provider:   transcript.Provider,
				Language:   transcript.Language,
				Source:     transcript.Source,
				SourceName: transcript.SourceName,
				Address:    transcript.Address,
				Processed:  transcript.Processed,
				Truncated:  transcript.Truncated,
				LatencyMS:  transcript.Latency.Milliseconds(),
				At:         transcript.At,
			}); err != nil {
				e.Logger.Warn("append history", "err", err)
			}
			e.Usage.Record(transcript.Duration, len([]rune(transcript.Text)))
			e.Publish(core.TranscriptFinalEvent{
				Event:      "transcript",
				Text:       transcript.Text,
				RawText:    transcript.RawText,
				Language:   transcript.Language,
				Provider:   transcript.Provider,
				Source:     transcript.Source,
				SourceName: transcript.SourceName,
				Address:    transcript.Address,
				Processed:  transcript.Processed,
				Truncated:  transcript.Truncated,
				LatencyMS:  transcript.Latency.Milliseconds(),
				At:         transcript.At.Format(time.RFC3339),
			})
		case picker := <-e.Pickers:
			e.Publish(core.PhrasePickerEvent{
				Event: "phrase_picker",
				At:    picker.At.Format(time.RFC3339),
			})
		case state := <-e.Microphone:
			active, source, name := SpeakingLabel(state, e.Sources())
			e.Publish(core.ListeningEvent{
				Event: "listening",
				ListeningState: core.ListeningState{
					Active:     active,
					Source:     source,
					SourceName: name,
				},
			})
		case phrase := <-e.Phrases:
			// Common phrases are user-authored text, not speech: they
			// show up live but never enter history or the log body.
			e.Publish(core.CommonPhraseEvent{
				Event: "phrase",
				Slot:  phrase.Slot,
				Text:  phrase.Text,
				At:    phrase.At.Format(time.RFC3339),
			})
		}
	}
}

// SpeakingLabel turns a microphone transition into the desktop pill's copy.
func SpeakingLabel(state input.AudioState, devices []input.Status) (bool, string, string) {
	if !state.Speaking || state.Owner == "" {
		return false, "", ""
	}
	if state.Owner == input.RemoteID {
		for _, device := range devices {
			if device.ID == input.RemoteID {
				return true, "remote", device.Name
			}
		}
		return true, "remote", "遥控器"
	}
	for _, device := range devices {
		if device.ID == state.Owner {
			return true, "phone", device.Name
		}
	}
	// An owner that dropped out of the list still counts as speaking; the pill
	// must not go quiet just because a label is missing.
	return true, "phone", state.Owner
}
