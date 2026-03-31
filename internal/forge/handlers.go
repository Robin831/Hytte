package forge

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
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

// ActivityStreamHandler streams forge events to the browser via SSE.
// It sends an initial batch of recent events, then polls the database every
// 2 seconds and pushes any new events (identified by ID > last seen ID).
//
// Event shape:
//
//	{ "id": int, "timestamp": RFC3339, "type": string, "message": string,
//	  "bead_id": string, "anvil": string }
func ActivityStreamHandler(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if db == nil {
			writeError(w, http.StatusServiceUnavailable, "forge state database not available")
			return
		}

		flusher, ok := w.(http.Flusher)
		if !ok {
			writeError(w, http.StatusInternalServerError, "streaming not supported")
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")
		fmt.Fprintf(w, "retry: 3000\n\n")
		flusher.Flush()

		// Send initial batch of recent events, oldest first.
		var lastID int
		initial, err := db.Events(50, "", "")
		if err == nil {
			for i := len(initial) - 1; i >= 0; i-- {
				e := initial[i]
				if e.ID > lastID {
					lastID = e.ID
				}
				data, _ := json.Marshal(e)
				fmt.Fprintf(w, "data: %s\n\n", data)
			}
			flusher.Flush()
		}

		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		keepalive := time.NewTicker(30 * time.Second)
		defer keepalive.Stop()

		for {
			select {
			case <-r.Context().Done():
				return
			case <-keepalive.C:
				fmt.Fprintf(w, ": keepalive\n\n")
				flusher.Flush()
			case <-ticker.C:
				newEvents, err := db.EventsSince(lastID, 100)
				if err != nil {
					continue
				}
				for _, e := range newEvents {
					if e.ID > lastID {
						lastID = e.ID
					}
					data, _ := json.Marshal(e)
					fmt.Fprintf(w, "data: %s\n\n", data)
				}
				if len(newEvents) > 0 {
					flusher.Flush()
				}
			}
		}
	}
}

// WorkerLogHandler streams a worker's log file via SSE.
// It sends the existing file content line by line, then polls for new lines
// every 500 ms using the file size to detect growth.
//
// Log line shape:
//
//	{ "line": string, "timestamp": RFC3339 }
//
// The log file path is read from the worker record in the forge state database.
// If the path is relative it is resolved relative to ~/.forge/.
func WorkerLogHandler(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		workerID := chi.URLParam(r, "id")
		if workerID == "" || !validWorkerID.MatchString(workerID) {
			writeError(w, http.StatusBadRequest, "invalid worker ID")
			return
		}
		if db == nil {
			writeError(w, http.StatusServiceUnavailable, "forge state database not available")
			return
		}

		worker, err := db.WorkerByID(workerID)
		if err != nil {
			writeError(w, http.StatusNotFound, "worker not found")
			return
		}

		logPath := worker.LogPath
		if logPath == "" {
			writeError(w, http.StatusNotFound, "worker has no log file")
			return
		}
		if !filepath.IsAbs(logPath) {
			home, err := os.UserHomeDir()
			if err != nil {
				writeError(w, http.StatusInternalServerError, "failed to resolve home directory")
				return
			}
			logPath = filepath.Join(home, ".forge", logPath)
		}

		flusher, ok := w.(http.Flusher)
		if !ok {
			writeError(w, http.StatusInternalServerError, "streaming not supported")
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")
		fmt.Fprintf(w, "retry: 3000\n\n")
		flusher.Flush()

		f, err := os.Open(logPath) //nolint:gosec
		if err != nil {
			fmt.Fprintf(w, "event: error\ndata: {\"error\":\"log file not accessible\"}\n\n")
			flusher.Flush()
			return
		}
		defer f.Close()

		// Send existing content line by line.
		existing, err := io.ReadAll(f)
		if err == nil && len(existing) > 0 {
			for _, line := range strings.Split(string(existing), "\n") {
				if line == "" {
					continue
				}
				entry := map[string]string{"line": line, "timestamp": time.Now().UTC().Format(time.RFC3339)}
				data, _ := json.Marshal(entry)
				fmt.Fprintf(w, "data: %s\n\n", data)
			}
			flusher.Flush()
		}
		offset := int64(len(existing))

		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-r.Context().Done():
				return
			case <-ticker.C:
				fi, err := f.Stat()
				if err != nil || fi.Size() <= offset {
					continue
				}
				buf := make([]byte, fi.Size()-offset)
				n, err := f.ReadAt(buf, offset)
				if n == 0 {
					continue
				}
				if err != nil && err != io.EOF {
					continue
				}
				offset += int64(n)
				flushed := false
				for _, line := range strings.Split(string(buf[:n]), "\n") {
					if line == "" {
						continue
					}
					entry := map[string]string{"line": line, "timestamp": time.Now().UTC().Format(time.RFC3339)}
					data, _ := json.Marshal(entry)
					fmt.Fprintf(w, "data: %s\n\n", data)
					flushed = true
				}
				if flushed {
					flusher.Flush()
				}
			}
		}
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
			if os.IsNotExist(err) {
				writeError(w, http.StatusNotFound, "restart script not found at ~/.forge/restart.sh")
			} else if os.IsPermission(err) {
				writeError(w, http.StatusInternalServerError, "permission denied accessing restart script")
			} else {
				writeError(w, http.StatusInternalServerError, "failed to stat restart script")
			}
			return
		}
		if !fi.Mode().IsRegular() {
			writeError(w, http.StatusInternalServerError, "restart script is not a regular file")
			return
		}
		// Return before executing so the response reaches the client even if
		// the script restarts the process hosting this handler.
		writeJSON(w, http.StatusAccepted, map[string]bool{"ok": true})
		go func() {
			time.Sleep(200 * time.Millisecond)
			cmd := exec.Command("/bin/sh", scriptPath) //nolint:gosec
			if err := cmd.Run(); err != nil {
				log.Printf("forge: restart script failed: %v", err)
			}
		}()
	}
}
