package familychat

import (
	"database/sql"
	"encoding/json"
	"fmt"
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

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// StreamHandler returns an SSE handler at
// GET /api/familychat/conversations/{id}/stream that pushes message_new,
// message_deleted, and read_receipt events as they are published into hub.
//
// Membership is re-verified at subscribe time; non-members get 404 (to avoid
// leaking conversation existence).
//
// hub is the pub/sub the handler subscribes to (callers wiring this into a
// router should pass DefaultHub() so it shares state with the message and
// read-receipt handlers). membership is the function used to confirm the
// requester belongs to the conversation.
func StreamHandler(hub *Hub, membership MembershipFn) http.HandlerFunc {
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
			// Treat membership check errors as not-a-member to avoid leaking
			// conversation existence. Log so the operator can investigate
			// genuine failures (e.g. missing table before schema is migrated).
			log.Printf("familychat: membership check failed for user %d conv %d: %v", user.ID, convID, err)
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
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

		sub := hub.Subscribe(convID)
		defer hub.Unsubscribe(convID, sub)

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
				data, err := json.Marshal(evt.Data)
				if err != nil {
					log.Printf("familychat: marshal event %s for conv %d: %v", evt.Type, convID, err)
					continue
				}
				if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", evt.Type, data); err != nil {
					return
				}
				flusher.Flush()
			}
		}
	}
}

// StreamHandlerWithDB is a convenience wrapper that pairs DefaultHub with a
// DB-backed membership checker. Use this from router.go; tests should call
// StreamHandler directly with a fake MembershipFn.
func StreamHandlerWithDB(db *sql.DB) http.HandlerFunc {
	return StreamHandler(DefaultHub(), DefaultMembership(db))
}
