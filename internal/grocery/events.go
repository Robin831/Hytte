package grocery

import "sync"

// Event type constants for grocery list mutations pushed over SSE.
const (
	EventItemAdded     = "item_added"
	EventItemChanged   = "item_changed"
	EventItemRemoved   = "item_removed"
	EventItemReordered = "item_reordered"
)

// GroceryEvent is a single mutation pushed to subscribers of a household's list.
// Payload carries the minimal data the client needs to patch local state:
//   - item_added / item_changed / item_reordered: the affected GroceryItem
//   - item_removed: a struct with the removed item's ID(s)
type GroceryEvent struct {
	Type    string `json:"type"`
	Payload any    `json:"payload"`
}

// Broker is an in-process, per-household pub/sub registry for grocery events.
// It fans out events to all connected SSE subscribers of a household. Sends are
// non-blocking: a subscriber whose buffer is full is skipped (slow-consumer
// drop) rather than blocking the publisher. There is no cross-process fan-out —
// this is intentionally scoped to a single Hytte process.
type Broker struct {
	mu          sync.Mutex
	subscribers map[int64]map[chan GroceryEvent]struct{}
}

// NewBroker creates an empty Broker.
func NewBroker() *Broker {
	return &Broker{subscribers: make(map[int64]map[chan GroceryEvent]struct{})}
}

// DefaultBroker is the package-level broker shared by the grocery handlers.
var DefaultBroker = NewBroker()

// Subscribe registers a new subscriber for the given household and returns a
// receive-only channel plus an unsubscribe function. The channel is buffered so
// brief bursts don't drop events; the caller must invoke the returned function
// (e.g. via defer) to release the subscription and avoid leaks.
func (b *Broker) Subscribe(householdID int64) (<-chan GroceryEvent, func()) {
	ch := make(chan GroceryEvent, 16)

	b.mu.Lock()
	subs := b.subscribers[householdID]
	if subs == nil {
		subs = make(map[chan GroceryEvent]struct{})
		b.subscribers[householdID] = subs
	}
	subs[ch] = struct{}{}
	b.mu.Unlock()

	var once sync.Once
	unsubscribe := func() {
		once.Do(func() {
			b.mu.Lock()
			if subs := b.subscribers[householdID]; subs != nil {
				delete(subs, ch)
				if len(subs) == 0 {
					delete(b.subscribers, householdID)
				}
			}
			b.mu.Unlock()
			close(ch)
		})
	}
	return ch, unsubscribe
}

// Publish fans an event out to every subscriber of the household. Delivery is
// non-blocking: subscribers whose buffers are full are skipped so a slow or
// stalled client can never block a write handler. Sends happen under the lock —
// the send itself never blocks (select/default), and holding the lock serialises
// against unsubscribe so we never send on a channel that is being closed.
func (b *Broker) Publish(householdID int64, event GroceryEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subscribers[householdID] {
		select {
		case ch <- event:
		default:
			// Slow consumer: drop the event for this subscriber. The client's
			// reconnect/refetch path resyncs missed state.
		}
	}
}
