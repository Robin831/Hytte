package pokemon

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"sync/atomic"
	"time"
)

// AdminSyncHandler kicks off a full SyncAll in a detached goroutine and
// returns 202 Accepted. Returns 409 Conflict if a sync is already running.
// Gated by RequireFeature("pokemon") at the router level.
func AdminSyncHandler(db *sql.DB) http.HandlerFunc {
	return adminSyncHandler(db, func(ctx context.Context, d *sql.DB) error {
		return SyncAll(ctx, d, NewClient())
	})
}

// adminSyncHandler is the testable implementation: it accepts an injectable
// sync function so tests can control timing and avoid real API calls.
func adminSyncHandler(db *sql.DB, doSync func(context.Context, *sql.DB) error) http.HandlerFunc {
	var inProgress atomic.Bool
	return func(w http.ResponseWriter, r *http.Request) {
		if !inProgress.CompareAndSwap(false, true) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "sync already running"})
			return
		}
		go func() {
			defer inProgress.Store(false)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
			defer cancel()
			if err := doSync(ctx, db); err != nil {
				log.Printf("pokemon: admin SyncAll failed: %v", err)
			} else {
				log.Printf("pokemon: admin SyncAll completed")
			}
		}()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "started"})
	}
}
