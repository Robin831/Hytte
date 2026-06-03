package familychat

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/go-chi/chi/v5"
)

// heartbeatInterval is how often the SSE handler writes a comment-only
// heartbeat line to keep proxies and load balancers from idling out the
// connection.
const heartbeatInterval = 30 * time.Second

// BackfillFn returns the catch-up messages a reconnecting client missed, given
// the highest message id it has already seen (sinceID). It is backed by
// EventsSince in production; tests inject a fake. A nil BackfillFn (or a
// sinceID <= 0) disables replay so the stream behaves as a plain live feed.
type BackfillFn func(convID, userID, sinceID int64) ([]Message, error)

// StreamHandler returns an SSE handler at
// GET /api/familychat/conversations/{id}/stream that pushes message_new,
// message_edited, message_deleted, read_receipt, typing, and call signalling
// events as they are published into hub.
//
// Resume: a reconnecting client supplies the id of the last message it saw via
// the standard Last-Event-ID header (set automatically by the browser's native
// EventSource) or a since_message_id query param. When present and valid, the
// handler replays every newer message plus any edit/delete of an older message
// (via backfill) before attaching to the live feed, so a client whose buffer
// overflowed or who dropped offline converges to the same state as one that
// never disconnected. An absent/invalid id is not an error — the stream simply
// starts at the live feed, exactly as before.
//
// Each message-bearing event (new/edited/deleted), live or replayed, is written
// with an `id:` line carrying the message id so the browser resends the correct
// Last-Event-ID on auto-reconnect. Ephemeral events (typing, read receipts,
// call signalling) carry no `id:` line and never become a resume point.
//
// Membership is re-verified at subscribe time; non-members get 404 (to avoid
// leaking conversation existence).
//
// Ordering guarantee: the handler subscribes to the live hub BEFORE running the
// backfill, so any event published while the backfill query runs lands in the
// subscriber buffer rather than being lost. The cost is that such an event may
// be delivered twice (once via backfill, once live); the frontend applies all
// replayed events idempotently, so a duplicate is a no-op.
func StreamHandler(hub *Hub, membership MembershipFn, backfill BackfillFn) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if user == nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}

		convID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil || convID <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid conversation ID"})
			return
		}

		ok, err := membership(user.ID, convID)
		if err != nil {
			log.Printf("familychat: membership check failed for user %d conv %d: %v", user.ID, convID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
			return
		}
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}

		flusher, ok := w.(http.Flusher)
		if !ok {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "streaming not supported"})
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")
		flusher.Flush()

		// Subscribe before backfilling so events published during the backfill
		// query are buffered, not dropped (see the ordering note in the doc).
		sub := hub.Subscribe(user.ID, convID)
		defer hub.Unsubscribe(convID, sub)

		// Replay anything the client missed before streaming live events. An
		// invalid or absent resume id yields sinceID == 0, which disables replay.
		sinceID := resumeID(r)
		if backfill != nil && sinceID > 0 {
			msgs, err := backfill(convID, user.ID, sinceID)
			if err != nil {
				// A backfill failure must not abort the stream — fall through to
				// the live feed so the client still receives new events (and can
				// recover the gap via a manual refresh).
				log.Printf("familychat: backfill conv=%d user=%d since=%d: %v", convID, user.ID, sinceID, err)
			} else {
				for _, m := range msgs {
					if r.Context().Err() != nil {
						return
					}
					if err := writeEvent(w, backfillEvent(m, sinceID)); err != nil {
						if err != errMarshal {
							return
						}
						log.Printf("familychat: backfill marshal event for msg %d conv %d: %v", m.ID, convID, err)
						continue
					}
				}
				if len(msgs) > 0 {
					flusher.Flush()
				}
			}
		}

		ticker := time.NewTicker(heartbeatInterval)
		defer ticker.Stop()

		ctx := r.Context()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, err := fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
					return
				}
				flusher.Flush()
			case evt, open := <-sub.Events():
				if !open {
					return
				}
				if err := writeEvent(w, evt); err != nil {
					if err != errMarshal {
						return
					}
					log.Printf("familychat: marshal event %s for conv %d: %v", evt.Type, convID, err)
					continue
				}
				flusher.Flush()
			}
		}
	}
}

// errMarshal is returned by writeEvent when the event payload cannot be
// JSON-encoded, distinguishing a (recoverable) bad payload from an I/O failure
// on the connection (which should tear the stream down).
var errMarshal = fmt.Errorf("familychat: event payload not marshalable")

// writeEvent serializes one SSE event. Message-bearing events (those with a
// non-zero ID) get an `id:` line so the browser tracks Last-Event-ID; ephemeral
// events omit it. Returns errMarshal if the payload can't be encoded, or the
// underlying write error otherwise.
func writeEvent(w io.Writer, evt Event) error {
	data, err := json.Marshal(evt.Data)
	if err != nil {
		return errMarshal
	}
	if evt.ID > 0 {
		if _, err := fmt.Fprintf(w, "id: %d\n", evt.ID); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", evt.Type, data); err != nil {
		return err
	}
	return nil
}

// resumeID extracts the last-seen message id from the request, preferring the
// standard Last-Event-ID header (which native EventSource resends on
// auto-reconnect) and falling back to the since_message_id query param (used by
// the fetch-based reader, which can't set the header). A missing or unparseable
// value yields 0, which disables replay — i.e. behaves as a fresh live feed.
func resumeID(r *http.Request) int64 {
	raw := r.Header.Get("Last-Event-ID")
	if raw == "" {
		raw = r.URL.Query().Get("since_message_id")
	}
	if raw == "" {
		return 0
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id < 0 {
		return 0
	}
	return id
}

// backfillEvent maps a catch-up Message (current on-disk state) to the SSE event
// the client expects, matching the shapes the live handlers publish:
//
//   - id > sinceID: the client never saw this message, so deliver the whole row
//     as message_new. The row already reflects any later edit (new body) or
//     delete (tombstone), so a single event converges the client correctly.
//   - id <= sinceID, deleted: a delete of a message the client already has →
//     message_deleted.
//   - id <= sinceID, edited: an edit of a message the client already has →
//     message_edited.
func backfillEvent(m Message, sinceID int64) Event {
	switch {
	case m.ID > sinceID:
		return Event{Type: EventMessageNew, ID: m.ID, Data: map[string]any{"message": m}}
	case m.DeletedAt != nil:
		var deletedBy int64
		if m.DeletedBy != nil {
			deletedBy = *m.DeletedBy
		}
		return Event{Type: EventMessageDeleted, ID: m.ID, Data: map[string]any{
			"message_id":      m.ID,
			"conversation_id": m.ConversationID,
			"deleted_by":      deletedBy,
		}}
	default:
		var editedAt string
		if m.EditedAt != nil {
			editedAt = *m.EditedAt
		}
		return Event{Type: EventMessageEdited, ID: m.ID, Data: map[string]any{
			"message_id":      m.ID,
			"conversation_id": m.ConversationID,
			"body":            m.Body,
			"edited_at":       editedAt,
		}}
	}
}

// StreamHandlerWithDB is a convenience wrapper that pairs DefaultHub with a
// DB-backed membership checker and backfill. Use this from router.go; tests
// should call StreamHandler directly with fakes.
func StreamHandlerWithDB(db *sql.DB) http.HandlerFunc {
	return StreamHandler(DefaultHub(), DefaultMembership(db), func(convID, userID, sinceID int64) ([]Message, error) {
		return EventsSince(db, convID, userID, sinceID, maxBackfillLimit)
	})
}
