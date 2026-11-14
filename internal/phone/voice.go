// Code split from server.go (ARC-09): same package, one concern per file.
package phone

import (
	"context"
	"github.com/yi-nology/open-voice-bridge/internal/core"
	"time"
)

// handleStart opens one hold-to-talk utterance: it validates the format, wins
// the microphone through the hub and clears the buffer before any audio lands.
func (s *Server) handleStart(ctx context.Context, active *activeConn, msg controlMessage) {
	if s.transcriber == nil {
		active.send(ctx, errorFrame("not_ready", "microphone input is not enabled yet"))
		return
	}
	if active.source.isSpeaking() {
		active.send(ctx, errorFrame("bad_state", "an utterance is already in progress"))
		return
	}
	// A full queue means earlier speech is still being transcribed. Refuse
	// here so the user never speaks into an utterance that cannot be queued
	// (SEC-08).
	if s.queue.full() {
		active.send(ctx, errorFrame("busy", "still catching up on earlier speech"))
		return
	}
	if msg.Format != "" && msg.Format != "pcm16" {
		active.send(ctx, errorFrame("bad_format", "only pcm16 is supported"))
		return
	}
	if msg.Rate != 0 && msg.Rate != core.SampleRate {
		active.send(ctx, errorFrame("bad_format", "audio must be 16 kHz"))
		return
	}
	if msg.Channels != 0 && msg.Channels != 1 {
		active.send(ctx, errorFrame("bad_format", "audio must be mono"))
		return
	}
	// Only one source owns the microphone at a time; the BLE remote may
	// preempt this claim, which is why the buffer is cleared once we own it.
	if s.hub != nil && !s.hub.AcquireAudio(active.deviceID) {
		active.send(ctx, errorFrame("busy", "another source is speaking"))
		return
	}
	active.buffer.reset()
	active.source.setSpeaking(true)
	active.send(ctx, map[string]any{"type": "listening", "active": true})
}

// handleAudio buffers one PCM frame. Frames arriving outside a recording
// session are ignored: a client must not be able to inject audio that was
// never requested.
func (s *Server) handleAudio(ctx context.Context, active *activeConn, data []byte) {
	if !active.source.isSpeaking() {
		return
	}
	ok, full := active.buffer.append(data)
	if !ok {
		active.send(ctx, errorFrame("bad_frame", "malformed pcm frame"))
		return
	}
	if full {
		active.send(ctx, errorFrame("too_long", "utterance truncated at the maximum length"))
		s.finishUtterance(ctx, active, true)
	}
}

// finishUtterance closes the recording, releases the microphone and hands the
// samples to the bounded transcription queue, so a slow provider never stops
// the phone from pressing keys and two sentences can never reach ASR swapped
// (SEC-08).
// preempted handles losing the microphone to a stronger source (DOM-06): the
// phone is told so its UI stops showing "listening", and the words already
// spoken still reach the chain (DOM-04), marked as cut short.
func (s *Server) preempted(active *activeConn, samples []int16) {
	sendCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	active.send(sendCtx, map[string]any{"type": "listening", "active": false})
	active.send(sendCtx, errorFrame("preempted", "another source took the microphone"))
	if len(samples) == 0 || s.transcriber == nil {
		return
	}
	s.queue.offer(utteranceJob{samples: samples, meta: utteranceMeta(active), truncated: true})
}

func (s *Server) finishUtterance(ctx context.Context, active *activeConn, truncated bool) {
	if !active.source.isSpeaking() {
		// Preempted or double "end": the capture is already owned elsewhere.
		// No reset here — the buffer may hold the samples a preemption is
		// about to deliver, and handleStart always starts clean.
		return
	}
	samples := active.buffer.take()
	active.source.setSpeaking(false)
	if s.hub != nil {
		s.hub.ReleaseAudio(active.deviceID)
	}
	active.send(ctx, map[string]any{"type": "listening", "active": false})

	if !s.queue.offer(utteranceJob{
		samples:   samples,
		meta:      utteranceMeta(active),
		truncated: truncated,
		notify:    active,
	}) {
		active.send(ctx, errorFrame("busy", "still catching up on earlier speech"))
	}
}

// runTranscriptionQueue drains finished utterances in capture order. It is the
// only caller of the transcriber for phone audio, which is what makes the
// subtitles and history keep the order the words were spoken in. Jobs run with
// a worker-lifetime context so shutdown can cut an in-flight request (CON-03).
func (s *Server) runTranscriptionQueue() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for {
		select {
		case <-s.closed:
			return
		case job := <-s.queue.ch:
			s.processUtterance(ctx, job)
		}
	}
}

func (s *Server) processUtterance(ctx context.Context, job utteranceJob) {
	meta := job.meta
	meta.Truncated = job.truncated
	result, ok := s.transcriber.TranscribeUtterance(ctx, job.samples, meta)
	if job.notify == nil {
		return
	}
	// A fresh detached context: by the time ASR returns the read loop may
	// already be gone, and a write on a closed connection is a no-op that
	// writeControl logs at debug rather than a panic.
	sendCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if !ok {
		job.notify.send(sendCtx, errorFrame("no_speech", "nothing recognised"))
		return
	}
	job.notify.send(sendCtx, map[string]any{
		"type":       "transcript",
		"text":       result.Text,
		"raw_text":   result.RawText,
		"provider":   result.Provider,
		"processed":  result.Processed,
		"latency_ms": result.LatencyMS,
		"at":         result.At.Format(time.RFC3339),
		"truncated":  job.truncated,
	})
}

// --- connection registry (max clients, duplicate replacement) --------------
