package netatmo

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strconv"

	"github.com/Robin831/Hytte/internal/auth"
)

// CurrentHandler returns the latest station readings for the authenticated user.
// It fetches from the 5-minute in-memory cache and attempts to persist the
// fresh reading to the historical store before returning (best-effort).
func CurrentHandler(client *Client, db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		readings, err := client.GetStationsData(r.Context(), user.ID)
		if err != nil {
			log.Printf("netatmo: fetch station data for user %d: %v", user.ID, err)
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "failed to fetch station data"})
			return
		}

		if err := StoreReadings(db, user.ID, *readings); err != nil {
			log.Printf("netatmo: store readings for user %d: %v", user.ID, err)
		}

		writeJSON(w, http.StatusOK, readings)
	}
}

// HistoryHandler returns historical sensor readings for the authenticated user.
// It accepts an optional "hours" query parameter (default 24, capped at 168).
// A fresh reading is attempted from the API before querying; if the fetch
// fails it is logged and the handler falls back to existing stored data.
func HistoryHandler(client *Client, db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		hours := 24
		if h := r.URL.Query().Get("hours"); h != "" {
			if parsed, err := strconv.Atoi(h); err == nil && parsed > 0 {
				hours = parsed
			}
		}
		if hours > 168 {
			hours = 168
		}

		// Attempt to refresh from the API before querying history (best-effort).
		if readings, err := client.GetStationsData(r.Context(), user.ID); err != nil {
			log.Printf("netatmo: refresh readings for history (user %d): %v", user.ID, err)
		} else if storeErr := StoreReadings(db, user.ID, *readings); storeErr != nil {
			log.Printf("netatmo: store readings for user %d: %v", user.ID, storeErr)
		}

		history, err := QueryHistory(db, user.ID, hours)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to query history"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{"readings": history})
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
