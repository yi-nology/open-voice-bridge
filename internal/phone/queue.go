package phone

// maxQueuedUtterances bounds how many finished utterances may wait for ASR at
// once (SEC-08). Each holds up to ~960 KB of PCM behind a slow provider; the
// cap trades a busy reply for unbounded memory growth and remote dictation
// being starved by a phone backlog.
const maxQueuedUtterances = 4

// utteranceJob is one finished (or dropped mid-sentence) capture waiting for
// the shared speech chain. notify is nil when the connection is already gone:
// the words still go to the focused window, the subtitles and the history (I5),
// nothing is echoed back.
type utteranceJob struct {
	samples   []int16
	meta      UtteranceMeta
	truncated bool
	notify    *activeConn
}

// utteranceQueue is a bounded FIFO. Jobs leave in the order they were offered,
// so two sentences can no longer reach ASR swapped (DOM-04 follow-up).
type utteranceQueue struct {
	ch chan utteranceJob
}

func newUtteranceQueue(capacity int) *utteranceQueue {
	if capacity <= 0 {
		capacity = maxQueuedUtterances
	}
	return &utteranceQueue{ch: make(chan utteranceJob, capacity)}
}

// offer queues a job without blocking; false means the queue is full and the
// caller must report busy instead of buffering another utterance.
func (q *utteranceQueue) offer(job utteranceJob) bool {
	select {
	case q.ch <- job:
		return true
	default:
		return false
	}
}

func (q *utteranceQueue) len() int { return len(q.ch) }

func (q *utteranceQueue) full() bool { return len(q.ch) == cap(q.ch) }
