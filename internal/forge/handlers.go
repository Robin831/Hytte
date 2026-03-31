package forge

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
)

var validBeadID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9\-]{1,63}$`)

// validWorkerID accepts UUIDs, short test IDs, and any alphanumeric-with-dash identifier.
var validWorkerID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9\-]{0,127}$`)

// IPCClient is the interface satisfied by *Client, allowing handlers to be
// tested with stub implementations without a live Unix socket.
type IPCClient interface {
	Health() error
	SendCommand(cmd string) ([]byte, error)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// StatusHandler returns daemon health combined with summary statistics from
// the forge state database: worker summary counts, full worker list, open PR
// count, ready queue size, beads needing human attention, and the stuck bead list.
func StatusHandler(db *DB, ipc IPCClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		type workerSummary struct {
			Active    int `json:"active"`
			Completed int `json:"completed"`
		}
		type resp struct {
			DaemonHealthy bool          `json:"daemon_healthy"`
			DaemonError   string        `json:"daemon_error,omitempty"`
			Workers       workerSummary `json:"workers"`
			WorkerList    []Worker      `json:"worker_list"`
			PRsOpen       int           `json:"prs_open"`
			QueueReady    int           `json:"queue_ready"`
			NeedsHuman    int           `json:"needs_human"`
			Stuck         []Retry       `json:"stuck"`
		}

		var out resp
		out.WorkerList = []Worker{}
		out.Stuck = []Retry{}

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
		out.WorkerList = workers

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
		out.Stuck = retries

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

		const maxLimit = 500
		limit := 50
		if s := r.URL.Query().Get("limit"); s != "" {
			if n, err := strconv.Atoi(s); err == nil && n > 0 {
				limit = n
				if limit > maxLimit {
					limit = maxLimit
				}
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

// MergePRHandler signals the forge daemon to merge a pull request.
// It sends a "merge-pr <id>" command over the IPC socket, where id is the
// integer database ID of the PR record.
func MergePRHandler(ipc IPCClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		prID := chi.URLParam(r, "id")
		if prID == "" {
			writeError(w, http.StatusBadRequest, "PR ID required")
			return
		}
		if _, err := strconv.Atoi(prID); err != nil {
			writeError(w, http.StatusBadRequest, "invalid PR ID")
			return
		}
		if ipc == nil {
			writeError(w, http.StatusServiceUnavailable, "IPC client not available")
			return
		}
		if _, err := ipc.SendCommand("merge-pr " + prID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to send merge command")
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
}

// RetryBeadHandler signals the forge daemon to retry a bead that needs human
// attention. It sends a "retry <bead_id>" command over the IPC socket.
func RetryBeadHandler(ipc IPCClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		beadID := chi.URLParam(r, "id")
		if beadID == "" {
			writeError(w, http.StatusBadRequest, "bead ID required")
			return
		}
		if !validBeadID.MatchString(beadID) {
			writeError(w, http.StatusBadRequest, "invalid bead ID")
			return
		}
		if ipc == nil {
			writeError(w, http.StatusServiceUnavailable, "IPC client not available")
			return
		}
		if _, err := ipc.SendCommand("retry " + beadID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to send retry command")
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
}

// KillWorkerHandler signals the forge daemon to kill a running worker.
// It sends a "kill <worker_id>" command over the IPC socket.
func KillWorkerHandler(ipc IPCClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		workerID := chi.URLParam(r, "id")
		if workerID == "" {
			writeError(w, http.StatusBadRequest, "worker ID required")
			return
		}
		if !validWorkerID.MatchString(workerID) {
			writeError(w, http.StatusBadRequest, "invalid worker ID")
			return
		}
		if ipc == nil {
			writeError(w, http.StatusServiceUnavailable, "IPC client not available")
			return
		}
		if _, err := ipc.SendCommand("kill " + workerID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to send kill command")
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
}

// RefreshHandler signals the forge daemon to trigger an immediate poll cycle.
// It sends a "refresh" command over the IPC socket.
func RefreshHandler(ipc IPCClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if ipc == nil {
			writeError(w, http.StatusServiceUnavailable, "IPC client not available")
			return
		}
		if _, err := ipc.SendCommand("refresh"); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to send refresh command")
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
}

// RestartForgeHandler runs ~/.forge/restart.sh to rebuild and restart the forge
// daemon. This allows deploying forge updates from a mobile device without SSH.
// The script is executed asynchronously; the handler returns 202 Accepted so the
// response is delivered before the restart potentially kills the process.
func RestartForgeHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		home, err := os.UserHomeDir()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to resolve home directory")
			return
		}
		scriptPath := filepath.Join(home, ".forge", "restart.sh")
		fi, err := os.Lstat(scriptPath)
		if err != nil {
			writeError(w, http.StatusNotFound, "restart script not found at ~/.forge/restart.sh")
			return
		}
		if !fi.Mode().IsRegular() {
			writeError(w, http.StatusBadRequest, "restart script is not a regular file")
			return
		}
		// Return before executing so the response reaches the client even if
		// the script restarts the process hosting this handler.
		writeJSON(w, http.StatusAccepted, map[string]bool{"ok": true})
		go func() {
			time.Sleep(200 * time.Millisecond)
			cmd := exec.Command("/bin/sh", scriptPath) //nolint:gosec
			cmd.Run()                                  //nolint:errcheck
		}()
	}
}
