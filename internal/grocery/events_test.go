package grocery

import (
	"testing"
	"time"
)

func recv(t *testing.T, ch <-chan GroceryEvent) (GroceryEvent, bool) {
	t.Helper()
	select {
	case ev, ok := <-ch:
		return ev, ok
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
		return GroceryEvent{}, false
	}
}

func TestBrokerFanOut(t *testing.T) {
	b := NewBroker()
	a, unsubA := b.Subscribe(1)
	defer unsubA()
	c, unsubC := b.Subscribe(1)
	defer unsubC()

	b.Publish(1, GroceryEvent{Type: EventItemAdded, Payload: "x"})

	for _, ch := range []<-chan GroceryEvent{a, c} {
		ev, ok := recv(t, ch)
		if !ok {
			t.Fatal("channel closed unexpectedly")
		}
		if ev.Type != EventItemAdded {
			t.Errorf("got type %q, want %q", ev.Type, EventItemAdded)
		}
	}
}

func TestBrokerHouseholdIsolation(t *testing.T) {
	b := NewBroker()
	h1, unsub1 := b.Subscribe(1)
	defer unsub1()
	h2, unsub2 := b.Subscribe(2)
	defer unsub2()

	b.Publish(1, GroceryEvent{Type: EventItemChanged})

	if ev, _ := recv(t, h1); ev.Type != EventItemChanged {
		t.Errorf("household 1 got type %q, want %q", ev.Type, EventItemChanged)
	}

	select {
	case ev := <-h2:
		t.Fatalf("household 2 received unexpected event: %+v", ev)
	case <-time.After(100 * time.Millisecond):
		// Expected: no cross-household delivery.
	}
}

func TestBrokerUnsubscribeStopsDelivery(t *testing.T) {
	b := NewBroker()
	ch, unsub := b.Subscribe(1)
	unsub()

	// The channel must be closed by unsubscribe.
	if _, ok := <-ch; ok {
		t.Fatal("expected channel to be closed after unsubscribe")
	}

	// Publishing after unsubscribe must not panic and reaches no one.
	b.Publish(1, GroceryEvent{Type: EventItemAdded})

	// Unsubscribing twice must be safe (idempotent).
	unsub()
}

func TestBrokerSlowConsumerDoesNotBlock(t *testing.T) {
	b := NewBroker()
	ch, unsub := b.Subscribe(1)
	defer unsub()

	// Flood far past the buffer capacity. A full subscriber must be skipped,
	// never block Publish.
	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			b.Publish(1, GroceryEvent{Type: EventItemAdded, Payload: i})
		}
		close(done)
	}()

	select {
	case <-done:
		// Publish never blocked despite the unread channel.
	case <-time.After(2 * time.Second):
		t.Fatal("Publish blocked on a slow consumer")
	}

	// The buffered events that did fit are still readable.
	if _, ok := recv(t, ch); !ok {
		t.Fatal("expected at least one buffered event")
	}
}
