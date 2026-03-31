package forge

import (
	"encoding/json"
	"net/http"
	"strconv"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// StatusHandler returns daemon health combined with summary statistics from
// the forge state database: active/completed worker counts, open PR count,
// ready queue size, and beads needing human attention.
func StatusHandler(db *DB, ipc *Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		type workerSummary struct {
			Active    int `json:"active"`
			Completed int `json:"completed"`
		}
		type resp struct {
			DaemonHealthy bool          `json:"daemon_healthy"`
			DaemonError   string        `json:"daemon_error,omitempty"`
			Workers       workerSummary `json:"workers"`
			PRsOpen       int           `json:"prs_open"`
			QueueReady    int           `json:"queue_ready"`
			NeedsHuman    int           `json:"needs_human"`
		}

		var out resp

		if ipc != nil {
			if err := ipc.Health(); err != nil {
				out.DaemonError = err.Error()
			} else {
				out.DaemonHealthy = true
			}
		} else {
			out.DaemonError = "IPC client not available"
		}

		if db == nil {
			writeError(w, http.StatusServiceUnavailable, "forge state database not available")
			return
		}

		workers, err := db.Workers()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load workers")
			return
		}
		for _, wk := range workers {
			if wk.Status == "pending" || wk.Status == "running" {
				out.Workers.Active++
			} else {
				out.Workers.Completed++
			}
		}

		prs, err := db.PRs()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load PRs")
			return
		}
		out.PRsOpen = len(prs)

		queue, err := db.QueueCache()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load queue")
			return
		}
		out.QueueReady = len(queue)

		retries, err := db.Retries()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load retries")
			return
		}
		out.NeedsHuman = len(retries)

		writeJSON(w, http.StatusOK, out)
	}
}

// WorkersHandler returns active workers (pending/running) and workers that
// completed within the last 24 hours.
func WorkersHandler(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if db == nil {
			writeError(w, http.StatusServiceUnavailable, "forge state database not available")
			return
		}
		workers, err := db.Workers()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load workers")
			return
		}
		writeJSON(w, http.StatusOK, workers)
	}
}

// QueueHandler returns all ready beads from the queue cache, ordered by
// anvil, priority, and update time.
func QueueHandler(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if db == nil {
			writeError(w, http.StatusServiceUnavailable, "forge state database not available")
			return
		}
		entries, err := db.QueueCache()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load queue")
			return
		}
		writeJSON(w, http.StatusOK, entries)
	}
}

// PRsHandler returns open pull requests tracked by forge.
func PRsHandler(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if db == nil {
			writeError(w, http.StatusServiceUnavailable, "forge state database not available")
			return
		}
		prs, err := db.PRs()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load PRs")
			return
		}
		writeJSON(w, http.StatusOK, prs)
	}
}

// EventsHandler returns recent forge events.
// Query params:
//   - limit: maximum number of events to return (default 50)
//   - type:  filter by event type
//   - anvil: filter by anvil name
func EventsHandler(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if db == nil {
			writeError(w, http.StatusServiceUnavailable, "forge state database not available")
			return
		}

		limit := 50
		if s := r.URL.Query().Get("limit"); s != "" {
			if n, err := strconv.Atoi(s); err == nil && n > 0 {
				limit = n
			}
		}
		eventType := r.URL.Query().Get("type")
		anvil := r.URL.Query().Get("anvil")

		events, err := db.Events(limit, eventType, anvil)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load events")
			return
		}
		writeJSON(w, http.StatusOK, events)
	}
}

// CostsHandler returns aggregated token usage and cost for the given period.
// Query param: period — "today" (default), "week", or "month".
func CostsHandler(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if db == nil {
			writeError(w, http.StatusServiceUnavailable, "forge state database not available")
			return
		}
		period := r.URL.Query().Get("period")
		costs, err := db.Costs(period)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load costs")
			return
		}
		writeJSON(w, http.StatusOK, costs)
	}
}
