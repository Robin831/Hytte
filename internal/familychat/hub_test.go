package familychat

import (
	"testing"
	"time"
)

// subscriberCount and conversationCount are test-only helpers for verifying
// hub cleanup. They live here so the production build does not carry them.

func (h *Hub) subscriberCount(convID int64) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subs[convID])
}

func (h *Hub) conversationCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subs)
}

// recvWithin waits up to d for an event on sub.Events(). It fails the test
// if nothing arrives in time.
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
	a := h.Subscribe(42)
	defer h.Unsubscribe(42, a)
	b := h.Subscribe(42)
	defer h.Unsubscribe(42, b)

	h.Publish(42, Event{Type: EventMessageNew, Data: map[string]any{"id": 1}})

	gotA := recvWithin(t, a, time.Second)
	gotB := recvWithin(t, b, time.Second)
	if gotA.Type != EventMessageNew || gotB.Type != EventMessageNew {
		t.Fatalf("unexpected event types: a=%q b=%q", gotA.Type, gotB.Type)
	}
}

func TestHub_IsolationBetweenConvs(t *testing.T) {
	h := NewHub()
	a := h.Subscribe(1)
	defer h.Unsubscribe(1, a)
	b := h.Subscribe(2)
	defer h.Unsubscribe(2, b)

	h.Publish(2, Event{Type: EventMessageNew, Data: map[string]any{"v": "for-b"}})

	// b should receive; a should not.
	gotB := recvWithin(t, b, time.Second)
	if gotB.Type != EventMessageNew {
		t.Fatalf("b expected message_new, got %q", gotB.Type)
	}

	select {
	case evt := <-a.Events():
		t.Fatalf("a received cross-conversation event: %+v", evt)
	case <-time.After(50 * time.Millisecond):
		// good — no event leaked
	}
}

func TestHub_UnsubscribeCleanup(t *testing.T) {
	h := NewHub()
	a := h.Subscribe(7)
	b := h.Subscribe(7)

	if got := h.subscriberCount(7); got != 2 {
		t.Fatalf("expected 2 subscribers, got %d", got)
	}

	h.Unsubscribe(7, a)
	if got := h.subscriberCount(7); got != 1 {
		t.Fatalf("after first unsubscribe expected 1, got %d", got)
	}

	h.Unsubscribe(7, b)
	if got := h.subscriberCount(7); got != 0 {
		t.Fatalf("after second unsubscribe expected 0, got %d", got)
	}
	if got := h.conversationCount(); got != 0 {
		t.Fatalf("expected map key reclaimed, conversationCount=%d", got)
	}

	// Subscriber channels must be closed after unsubscribe.
	if _, open := <-a.Events(); open {
		t.Fatal("expected a.Events() channel closed after unsubscribe")
	}
	if _, open := <-b.Events(); open {
		t.Fatal("expected b.Events() channel closed after unsubscribe")
	}

	// Idempotent: unsubscribing again should not panic.
	h.Unsubscribe(7, a)
}

func TestHub_UnsubscribeUnknownSubscriberNoPanic(t *testing.T) {
	h := NewHub()
	// Unsubscribe with a nil subscriber and one from an unknown conversation
	// should both be no-ops.
	h.Unsubscribe(9, nil)
	stranger := &Subscriber{ch: make(chan Event, 1)}
	h.Unsubscribe(9, stranger)
}

func TestHub_SlowSubscriberDoesNotBlock(t *testing.T) {
	h := NewHub()
	slow := h.Subscribe(3)
	defer h.Unsubscribe(3, slow)

	// Saturate the slow subscriber's buffer so any further publish would
	// block if Publish were a synchronous send.
	for i := 0; i < SubscriberBufferSize; i++ {
		h.Publish(3, Event{Type: EventMessageNew, Data: i})
	}

	// Add a fast subscriber with an empty buffer.
	fast := h.Subscribe(3)
	defer h.Unsubscribe(3, fast)

	// Publish must return promptly even though slow is full.
	done := make(chan struct{})
	go func() {
		h.Publish(3, Event{Type: EventMessageNew, Data: "for-fast"})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Publish blocked when slow subscriber's buffer was full")
	}

	evt := recvWithin(t, fast, time.Second)
	if s, ok := evt.Data.(string); !ok || s != "for-fast" {
		t.Fatalf("fast subscriber got unexpected event: %+v", evt)
	}
}

func TestHub_NoSubscribersIsHarmless(t *testing.T) {
	h := NewHub()
	// Publish to a conversation that has no subscribers — should be a no-op.
	h.Publish(99, Event{Type: EventMessageNew, Data: nil})
	if h.conversationCount() != 0 {
		t.Fatalf("expected no conversations, got %d", h.conversationCount())
	}
}
