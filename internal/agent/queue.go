package agent

import (
	"sync"

	"github.com/oekit/concurrency/async"
)

// maxQueuedSpeech bounds how many finished utterances may wait behind the one
// in flight (SEC-08 for the BLE path, matching the phone queue's bound). Each
// waits up to ~960 KB of PCM; the cap trades a dropped utterance for
// unbounded goroutines and memory during a slow provider.
const maxQueuedSpeech = 4

// serialQueue runs tasks one at a time, in offer order. The capture paths
// (remote, wake, lookback) used to fan out one goroutine per utterance, so
// recognitions ran concurrently and subtitles could land swapped; the phone
// path already had its own bounded FIFO (DOM-04 follow-up), the agent side
// now matches it. Offer refuses past the bound instead of growing — the
// caller logs the drop.
type serialQueue struct {
	mu      sync.Mutex
	tasks   []func()
	running bool
}

// offer queues a task; false means the queue is full and the caller should
// drop the utterance rather than buffer it. The bound counts what is waiting:
// the running task is popped before this returns, so at most maxQueuedSpeech
// tasks wait behind it.
func (q *serialQueue) offer(task func()) bool {
	q.mu.Lock()
	if len(q.tasks) >= maxQueuedSpeech {
		q.mu.Unlock()
		return false
	}
	q.tasks = append(q.tasks, task)
	if q.running {
		q.mu.Unlock()
		return true
	}
	q.running = true
	first := q.tasks[0]
	q.tasks[0] = nil
	q.tasks = q.tasks[1:]
	q.mu.Unlock()
	async.GoSafe(func() {
		q.runOne(first)
		q.drain()
	})
	return true
}

func (q *serialQueue) drain() {
	for {
		q.mu.Lock()
		if len(q.tasks) == 0 {
			q.running = false
			q.mu.Unlock()
			return
		}
		task := q.tasks[0]
		q.tasks[0] = nil
		q.tasks = q.tasks[1:]
		q.mu.Unlock()
		q.runOne(task)
	}
}

// runOne isolates a panicking task: one broken utterance must not take the
// backlog with it (the old per-utterance goroutine had async's recovery on
// its side; the shared worker has to keep going on its own).
func (q *serialQueue) runOne(task func()) {
	defer func() {
		if r := recover(); r != nil {
			log.Error("transcription task panicked", "err", r)
		}
	}()
	task()
}
