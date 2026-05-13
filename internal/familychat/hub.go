// Package familychat implements the in-memory pub/sub hub and SSE stream
// handler for the family chat feature's live message delivery.
//
// The hub is independent of the database — it is a pure pub/sub keyed by
// conversation ID. Message and read-receipt handlers (added in companion
// beads) publish events into the hub after persisting the row; subscribed
// SSE clients receive them on their per-connection channel.
package familychat

import (
	"sync"
)

// Event is a single message broadcast to Subscribers of a conversation.
// Type is the SSE `event:` line value (e.g. "message_new"); Data is the JSON
// payload sent on the `data:` line. Data is marshaled lazily by the SSE
// handler so non-streaming callers do not pay the JSON cost.
type Event struct {
	Type string
	Data any
}

// Known event types. Kept as constants so callers cannot typo a name silently.
const (
	EventMessageNew     = "message_new"
	EventMessageDeleted = "message_deleted"
	EventReadReceipt    = "read_receipt"
)

// Subscriber is a single SSE client subscription. The channel is buffered so
// a publisher can fan out without blocking on a slow consumer; if the buffer
// fills up the publish for that Subscriber is dropped (the client will pick
// up the next event or reconnect).
type Subscriber struct {
	ch chan Event
}

// SubscriberBufferSize is the per-Subscriber channel buffer. Sized to absorb
// short bursts (e.g. several messages posted back-to-back) without forcing
// the publisher to block.
const SubscriberBufferSize = 16

// Hub is an in-memory pub/sub broker keyed by conversation ID. Safe for
// concurrent use.
type Hub struct {
	mu   sync.RWMutex
	subs map[int64]map[*Subscriber]struct{}
}

// NewHub returns an empty Hub ready for use.
func NewHub() *Hub {
	return &Hub{
		subs: make(map[int64]map[*Subscriber]struct{}),
	}
}

// Subscribe registers a new Subscriber for the given conversation and returns
// it. The caller must Unsubscribe when finished, typically via defer.
func (h *Hub) Subscribe(convID int64) *Subscriber {
	h.mu.Lock()
	defer h.mu.Unlock()
	s := &Subscriber{ch: make(chan Event, SubscriberBufferSize)}
	if h.subs[convID] == nil {
		h.subs[convID] = make(map[*Subscriber]struct{})
	}
	h.subs[convID][s] = struct{}{}
	return s
}

// Unsubscribe removes the Subscriber and closes its channel. Idempotent and
// safe to call even if the Subscriber was already removed.
func (h *Hub) Unsubscribe(convID int64, s *Subscriber) {
	if s == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	set, ok := h.subs[convID]
	if !ok {
		return
	}
	if _, present := set[s]; !present {
		return
	}
	delete(set, s)
	if len(set) == 0 {
		delete(h.subs, convID)
	}
	close(s.ch)
}

// Publish fans out the event to every Subscriber of convID. Sends are
// non-blocking: if a Subscriber's buffer is full, that Subscriber misses this
// event (and others still receive it). This keeps one stalled client from
// blocking message delivery to the rest of the conversation.
func (h *Hub) Publish(convID int64, evt Event) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for s := range h.subs[convID] {
		select {
		case s.ch <- evt:
		default:
		}
	}
}

// Events returns the receive channel for the Subscriber. Exposed as a method
// so Subscriber's channel field can stay unexported.
func (s *Subscriber) Events() <-chan Event {
	return s.ch
}

// defaultHub is the process-wide hub used by request handlers. Message and
// read-receipt handlers publish into this hub; the SSE stream handler
// subscribes to it. Tests construct their own hubs via NewHub and pass them
// directly to StreamHandler so they do not share state with production code.
var defaultHub = NewHub()

// DefaultHub returns the process-wide hub. Companion handlers (POST
// /messages, POST /read) should call DefaultHub().Publish(...) after
// persisting their row so subscribed SSE clients see the change.
func DefaultHub() *Hub {
	return defaultHub
}
