package pokemon

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"time"
)

// AdminSyncHandler kicks off a full SyncAll in a detached goroutine and
// returns 202 Accepted. Wrap with auth.RequireAdmin() at the router level.
func AdminSyncHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
			defer cancel()
			if err := SyncAll(ctx, db, NewClient()); err != nil {
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
