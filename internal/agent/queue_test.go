package agent

import (
	"sync"
	"testing"
	"time"
)

// The capture paths (remote, wake, lookback) used to fan out one goroutine
// per utterance: concurrent ASR, unbounded PCM in flight, and subtitles that
// could land swapped. The queue runs tasks one at a time, in offer order.
func TestSerialQueueRunsTasksOneAtATimeInOrder(t *testing.T) {
	var mu sync.Mutex
	var order []int
	var concurrent, peak int
	release := make(chan struct{})
	started := make(chan struct{}, 8)

	q := &serialQueue{}
	for i := 1; i <= 3; i++ {
		i := i
		ok := q.offer(func() {
			mu.Lock()
			concurrent++
			if concurrent > peak {
				peak = concurrent
			}
			order = append(order, i)
			mu.Unlock()
			started <- struct{}{}
			<-release
			mu.Lock()
			concurrent--
			mu.Unlock()
		})
		if !ok {
			t.Fatalf("offer %d rejected", i)
		}
	}

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first task never started")
	}
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	c := concurrent
	mu.Unlock()
	if c != 1 {
		t.Fatalf("%d tasks ran at once; want 1", c)
	}

	close(release)
	deadline := time.After(2 * time.Second)
	for {
		mu.Lock()
		done := len(order)
		mu.Unlock()
		if done == 3 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("tasks never finished")
		case <-time.After(10 * time.Millisecond):
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if peak != 1 {
		t.Fatalf("peak concurrency = %d, want 1", peak)
	}
	if order[0] != 1 || order[1] != 2 || order[2] != 3 {
		t.Fatalf("order = %v, want [1 2 3]", order)
	}
}

// Four may wait behind the one in flight; the fifth is refused so a slow
// provider costs a dropped utterance, not unbounded goroutines and PCM.
func TestSerialQueueRefusesInsteadOfGrowing(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	q := &serialQueue{}
	for i := 1; i <= 5; i++ {
		if !q.offer(func() { <-release }) {
			t.Fatalf("offer %d rejected while the queue had room", i)
		}
	}
	if q.offer(func() { <-release }) {
		t.Fatal("offer past the bound must be refused")
	}
}

// A drained queue takes work again — the single worker is per-burst, not
// per-agent-lifetime.
func TestSerialQueueResumesAfterADrain(t *testing.T) {
	q := &serialQueue{}
	ran := make(chan struct{}, 1)
	if !q.offer(func() { ran <- struct{}{} }) {
		t.Fatal("first offer rejected")
	}
	select {
	case <-ran:
	case <-time.After(time.Second):
		t.Fatal("first task never ran")
	}
	// Let the worker notice the queue is empty and park itself.
	time.Sleep(50 * time.Millisecond)
	if !q.offer(func() { ran <- struct{}{} }) {
		t.Fatal("second offer rejected after the drain")
	}
	select {
	case <-ran:
	case <-time.After(time.Second):
		t.Fatal("second task never ran")
	}
}

// One panicking task must not take the rest of the backlog with it — each
// utterance is independent work, and the worker is shared.
func TestSerialQueueSurvivesAPanickingTask(t *testing.T) {
	q := &serialQueue{}
	after := make(chan struct{}, 1)
	if !q.offer(func() { panic("utterance exploded") }) {
		t.Fatal("offer rejected")
	}
	if !q.offer(func() { after <- struct{}{} }) {
		t.Fatal("second offer rejected")
	}
	select {
	case <-after:
	case <-time.After(time.Second):
		t.Fatal("a panicking task killed the queue; the next one never ran")
	}
}
