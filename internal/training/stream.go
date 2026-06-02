package training

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
)

// streamHeartbeatInterval is how often the SSE handler writes a comment-only
// heartbeat line to keep proxies and load balancers from idling out the
// connection.
const streamHeartbeatInterval = 30 * time.Second

// StreamHandler returns an SSE handler at GET /api/training/events that pushes
// workout_new events as they are published into hub for the authenticated
// user. Only the requesting user's events are delivered — the hub is keyed on
// user ID, so there is no cross-user leakage.
//
// hub is the pub/sub the handler subscribes to; callers wiring this into a
// router should pass DefaultHub() so it shares state with the upload handler.
func StreamHandler(hub *Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if user == nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
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

		sub := hub.Subscribe(user.ID)
		defer hub.Unsubscribe(user.ID, sub)

		ticker := time.NewTicker(streamHeartbeatInterval)
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
				data, err := json.Marshal(evt)
				if err != nil {
					log.Printf("training: marshal event %s for user %d: %v", evt.Type, user.ID, err)
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
