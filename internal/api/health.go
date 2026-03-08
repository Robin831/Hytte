package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
)

// HealthHandler returns a handler that responds with the server health status.
func HealthHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		status := "ok"

		if err := db.Ping(); err != nil {
			status = "degraded"
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"status": status,
		})
	}
}
