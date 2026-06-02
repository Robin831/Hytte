package training

import (
	"testing"
	"time"
)

// subscriberCount and userCount are test-only helpers for verifying hub
// cleanup. They live here so the production build does not carry them.

func (h *Hub) subscriberCount(userID int64) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subs[userID])
}

func (h *Hub) userCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subs)
}

// recvWithin waits up to d for an event on sub.Events(). It fails the test if
// nothing arrives in time.
func recvWithin(t *testing.T, sub *Subscriber, d time.Duration) Event {
	t.Helper()
	select {
	case evt, ok := <-sub.Events():
		if !ok {
			t.Fatal("subscriber channel closed unexpectedly")
		}
		return evt
	case <-time.After(d):
		t.Fatalf("no event received within %s", d)
	}
	return Event{}
}

func TestHub_PublishFanout(t *testing.T) {
	h := NewHub()
	a := h.Subscribe(1)
	defer h.Unsubscribe(1, a)
	b := h.Subscribe(1)
	defer h.Unsubscribe(1, b)

	h.Publish(1, Event{Type: EventWorkoutNew, LatestID: 42})

	gotA := recvWithin(t, a, time.Second)
	gotB := recvWithin(t, b, time.Second)
	if gotA.Type != EventWorkoutNew || gotB.Type != EventWorkoutNew {
		t.Fatalf("unexpected event types: a=%q b=%q", gotA.Type, gotB.Type)
	}
	if gotA.LatestID != 42 || gotB.LatestID != 42 {
		t.Fatalf("unexpected latest ids: a=%d b=%d", gotA.LatestID, gotB.LatestID)
	}
}

func TestHub_IsolationBetweenUsers(t *testing.T) {
	h := NewHub()
	a := h.Subscribe(10)
	defer h.Unsubscribe(10, a)
	b := h.Subscribe(11)
	defer h.Unsubscribe(11, b)

	// Publish only to user 11 — user 10 must not see it.
	h.Publish(11, Event{Type: EventWorkoutNew, LatestID: 7})

	gotB := recvWithin(t, b, time.Second)
	if gotB.Type != EventWorkoutNew || gotB.LatestID != 7 {
		t.Fatalf("b expected workout_new latest=7, got %+v", gotB)
	}

	select {
	case evt := <-a.Events():
		t.Fatalf("a received cross-user event: %+v", evt)
	case <-time.After(50 * time.Millisecond):
		// good — no event leaked across users
	}
}

func TestHub_UnsubscribeCleanup(t *testing.T) {
	h := NewHub()
	a := h.Subscribe(1)
	b := h.Subscribe(1)

	if got := h.subscriberCount(1); got != 2 {
		t.Fatalf("expected 2 subscribers, got %d", got)
	}

	h.Unsubscribe(1, a)
	if got := h.subscriberCount(1); got != 1 {
		t.Fatalf("after first unsubscribe expected 1, got %d", got)
	}

	h.Unsubscribe(1, b)
	if got := h.subscriberCount(1); got != 0 {
		t.Fatalf("after second unsubscribe expected 0, got %d", got)
	}
	if got := h.userCount(); got != 0 {
		t.Fatalf("expected map key reclaimed, userCount=%d", got)
	}

	// Subscriber channels must be closed after unsubscribe.
	if _, open := <-a.Events(); open {
		t.Fatal("expected a.Events() channel closed after unsubscribe")
	}
	if _, open := <-b.Events(); open {
		t.Fatal("expected b.Events() channel closed after unsubscribe")
	}

	// Idempotent: unsubscribing again should not panic.
	h.Unsubscribe(1, a)
}

func TestHub_UnsubscribeUnknownSubscriberNoPanic(t *testing.T) {
	h := NewHub()
	// Unsubscribe with a nil subscriber and one from an unknown user should
	// both be no-ops.
	h.Unsubscribe(9, nil)
	stranger := &Subscriber{ch: make(chan Event, 1)}
	h.Unsubscribe(9, stranger)
}

func TestHub_SlowSubscriberDoesNotBlock(t *testing.T) {
	h := NewHub()
	slow := h.Subscribe(1)
	defer h.Unsubscribe(1, slow)

	// Saturate the slow subscriber's buffer so any further publish would block
	// if Publish were a synchronous send.
	for i := 0; i < SubscriberBufferSize; i++ {
		h.Publish(1, Event{Type: EventWorkoutNew, LatestID: int64(i)})
	}

	// Add a fast subscriber with an empty buffer.
	fast := h.Subscribe(1)
	defer h.Unsubscribe(1, fast)

	// Publish must return promptly even though slow is full.
	done := make(chan struct{})
	go func() {
		h.Publish(1, Event{Type: EventWorkoutNew, LatestID: 999})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Publish blocked when slow subscriber's buffer was full")
	}

	evt := recvWithin(t, fast, time.Second)
	if evt.LatestID != 999 {
		t.Fatalf("fast subscriber got unexpected event: %+v", evt)
	}
}

func TestHub_NoSubscribersIsHarmless(t *testing.T) {
	h := NewHub()
	// Publish to a user that has no subscribers — should be a no-op.
	h.Publish(99, Event{Type: EventWorkoutNew, LatestID: 1})
	if h.userCount() != 0 {
		t.Fatalf("expected no users, got %d", h.userCount())
	}
}
