package training

import (
	"sync"
)

// Event is a single signal broadcast to Subscribers of a user. Type is the SSE
// `event:` line value (e.g. "workout_new"); LatestID carries the highest
// workout id known at publish time so a freshly (re)connected client can
// reconcile against its own last-seen id. The event deliberately carries no
// workout payload — clients fetch /api/training/workouts/latest (or the full
// list) when they need details.
type Event struct {
	Type     string `json:"-"`
	LatestID int64  `json:"latest_id"`
}

// EventWorkoutNew is the only event type currently published. Kept as a
// constant so callers cannot typo the name silently.
const EventWorkoutNew = "workout_new"

// Subscriber is a single SSE client subscription. The channel is buffered so a
// publisher can fan out without blocking on a slow consumer; if the buffer
// fills up the publish for that Subscriber is dropped (the client will pick up
// the next event or do a /latest fetch on reconnect).
type Subscriber struct {
	ch chan Event
}

// SubscriberBufferSize is the per-Subscriber channel buffer. Workout inserts
// are infrequent, but a small buffer absorbs back-to-back imports from a
// multi-file upload without forcing the publisher to block.
const SubscriberBufferSize = 8

// Hub is an in-memory pub/sub broker keyed by user ID. Safe for concurrent
// use. Each user's open Training tabs subscribe; the upload/import path
// publishes a workout_new event for that user after the row is committed.
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

// Subscribe registers a new Subscriber for the given user and returns it. The
// caller must Unsubscribe when finished, typically via defer.
func (h *Hub) Subscribe(userID int64) *Subscriber {
	h.mu.Lock()
	defer h.mu.Unlock()
	s := &Subscriber{ch: make(chan Event, SubscriberBufferSize)}
	if h.subs[userID] == nil {
		h.subs[userID] = make(map[*Subscriber]struct{})
	}
	h.subs[userID][s] = struct{}{}
	return s
}

// Unsubscribe removes the Subscriber and closes its channel. Idempotent and
// safe to call even if the Subscriber was already removed.
func (h *Hub) Unsubscribe(userID int64, s *Subscriber) {
	if s == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	set, ok := h.subs[userID]
	if !ok {
		return
	}
	if _, present := set[s]; !present {
		return
	}
	delete(set, s)
	if len(set) == 0 {
		delete(h.subs, userID)
	}
	close(s.ch)
}

// Publish fans out the event to every Subscriber of userID. Sends are
// non-blocking: if a Subscriber's buffer is full, that Subscriber misses this
// event (and others still receive it). This keeps one stalled client from
// blocking delivery to the user's other tabs. Publishing to a user with no
// subscribers is a harmless no-op.
func (h *Hub) Publish(userID int64, evt Event) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for s := range h.subs[userID] {
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

// defaultHub is the process-wide hub used by request handlers. The upload
// handler publishes into this hub after committing a workout; the SSE stream
// handler subscribes to it. Tests construct their own hubs via NewHub and pass
// them directly to StreamHandler so they do not share state with production
// code.
var defaultHub = NewHub()

// DefaultHub returns the process-wide hub. The upload/import path calls
// DefaultHub().Publish(...) after persisting a workout so subscribed SSE
// clients see the change.
func DefaultHub() *Hub {
	return defaultHub
}
