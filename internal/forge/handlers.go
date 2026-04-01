package forge

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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
	"gopkg.in/yaml.v3"
)

var validBeadID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9\-]{1,63}$`)

// validWorkerID accepts UUIDs, short test IDs, and any alphanumeric-with-dash identifier.
var validWorkerID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9\-]{0,127}$`)

// validLabel accepts alphanumeric labels with hyphens and underscores.
var validLabel = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9\-_]{0,63}$`)

// validAssignee accepts GitHub/GitLab-style usernames (alphanumeric, hyphens, underscores).
// Empty string is valid and means "unassign".
var validAssignee = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9\-_]{0,62}$`)

// anvilDirForBead returns the working directory for bd commands operating on beadID.
// It derives the anvil name from the bead ID prefix (e.g. "Hytte" from "Hytte-abc1"),
// looks it up in ~/.forge/config.yaml, and returns the configured path.
// Falls back to repoRoot() if the anvil is not found in config.
func anvilDirForBead(beadID string) (string, error) {
	if idx := strings.Index(beadID, "-"); idx > 0 {
		anvilName := beadID[:idx]
		if cfgPath, err := configPath(); err == nil {
			home, _ := os.UserHomeDir()
			forgeDir := filepath.Join(home, ".forge")
			if err := isRegularDir(forgeDir); err == nil {
				if err := isRegularFile(cfgPath); err == nil {
					fi, err := os.Stat(cfgPath)
					if err == nil && fi.Size() <= maxConfigSize {
						if data, err := os.ReadFile(cfgPath); err == nil {
							var cfg ForgeConfig
							if err := yaml.Unmarshal(data, &cfg); err == nil {
								lower := strings.ToLower(anvilName)
								// 1) Try exact match first to preserve previous behavior.
								if anvil, ok := cfg.Anvils[anvilName]; ok && anvil.Path != "" {
									return anvil.Path, nil
								}
								// 2) Try lowercased key for configs that normalize to lowercase.
								if anvil, ok := cfg.Anvils[lower]; ok && anvil.Path != "" {
									return anvil.Path, nil
								}
								// 3) Fall back to a full case-insensitive scan of keys.
								var (
									matchingKeys []string
									matchingPath string
								)
								for name, anvil := range cfg.Anvils {
									if strings.EqualFold(name, anvilName) && anvil.Path != "" {
										matchingKeys = append(matchingKeys, name)
										matchingPath = anvil.Path
									}
								}
								switch len(matchingKeys) {
								case 0:
									// No case-insensitive match; fall through to repoRoot().
								case 1:
									return matchingPath, nil
								default:
									return "", fmt.Errorf("ambiguous anvil config for %q: matching keys %v", anvilName, matchingKeys)
								}
							}
						}
					}
				}
			}
		}
	}
	return repoRoot()
}

// resolveCommand returns the absolute path to a binary. It first tries PATH
// via exec.LookPath, then falls back to ~/.local/bin and ~/bin so that
// user-installed tools are found when running under systemd (which typically
// strips user-specific directories from PATH).
func resolveCommand(name string) string {
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return name
	}
	for _, dir := range []string{
		filepath.Join(home, ".local", "bin"),
		filepath.Join(home, "bin"),
	} {
		candidate := filepath.Join(dir, name)
		if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
			return candidate
		}
	}
	return name
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
// count, full open PR list, ready queue size, beads needing human attention,
// and the stuck bead list.
//
// Daemon health is checked by verifying the PID file rather than dialing the
// IPC socket, which eliminates timeout risk on the dashboard's main data load.
func StatusHandler(db *DB) http.HandlerFunc {
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
			OpenPRs       []PR          `json:"open_prs"`
			QueueReady    int           `json:"queue_ready"`
			NeedsHuman    int           `json:"needs_human"`
			Stuck         []Retry       `json:"stuck"`
		}

		var out resp
		out.WorkerList = []Worker{}
		out.Stuck = []Retry{}
		out.OpenPRs = []PR{}

		if alive, errMsg := daemonAlive(); alive {
			out.DaemonHealthy = true
		} else {
			out.DaemonError = errMsg
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
		out.OpenPRs = prs

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

// FullQueueHandler returns all beads from the queue cache across all sections
// (ready, unlabeled, in-progress, needs-attention), ordered by anvil, section,
// priority, and update time.
func FullQueueHandler(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if db == nil {
			writeError(w, http.StatusServiceUnavailable, "forge state database not available")
			return
		}
		entries, err := db.QueueAll()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load queue")
			return
		}
		writeJSON(w, http.StatusOK, entries)
	}
}

// AddLabelHandler adds a label to a bead by invoking "bd label add" directly.
// This bypasses IPC to avoid the 5-second read timeout (see Hytte-e535).
func AddLabelHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		beadID := chi.URLParam(r, "id")
		if beadID == "" || !validBeadID.MatchString(beadID) {
			writeError(w, http.StatusBadRequest, "invalid bead ID")
			return
		}
		var body struct {
			Label string `json:"label"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if body.Label == "" || !validLabel.MatchString(body.Label) {
			writeError(w, http.StatusBadRequest, "invalid label")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, resolveCommand("bd"), "label", "add", beadID, body.Label)
		if out, err := cmd.CombinedOutput(); err != nil {
			log.Printf("bd label add %s %s failed: %v: %s", beadID, body.Label, err, out)
			writeError(w, http.StatusInternalServerError, "failed to add label")
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
}

// BeadComment represents a single comment on a bead.
type BeadComment struct {
	Author    string `json:"author"`
	Body      string `json:"body"`
	CreatedAt string `json:"created_at"`
}

// BeadDependency represents a dependency or dependent bead in the detail response.
type BeadDependency struct {
	ID             string `json:"id"`
	Title          string `json:"title"`
	Status         string `json:"status"`
	Priority       int    `json:"priority"`
	IssueType      string `json:"issue_type"`
	DependencyType string `json:"dependency_type,omitempty"`
	Direction      string `json:"direction"`
}

// BeadDetail represents the full detail of a single bead, normalized from bd CLI output.
type BeadDetail struct {
	ID                 string           `json:"id"`
	Title              string           `json:"title"`
	Description        string           `json:"description"`
	Notes              string           `json:"notes,omitempty"`
	Design             string           `json:"design,omitempty"`
	AcceptanceCriteria string           `json:"acceptance_criteria,omitempty"`
	Status             string           `json:"status"`
	Priority           int              `json:"priority"`
	IssueType          string           `json:"issue_type"`
	Owner              string           `json:"owner"`
	Assignee           string           `json:"assignee,omitempty"`
	CreatedAt          string           `json:"created_at"`
	CreatedBy          string           `json:"created_by"`
	UpdatedAt          string           `json:"updated_at"`
	ClosedAt           string           `json:"closed_at,omitempty"`
	CloseReason        string           `json:"close_reason,omitempty"`
	Labels             []string         `json:"labels"`
	Comments           []BeadComment    `json:"comments"`
	Dependencies       []BeadDependency `json:"dependencies"`
	Dependents         []BeadDependency `json:"dependents"`
}

// BeadDetailHandler returns full bead details by invoking "bd show <id> --json".
func BeadDetailHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		beadID := chi.URLParam(r, "id")
		if beadID == "" || !validBeadID.MatchString(beadID) {
			writeError(w, http.StatusBadRequest, "invalid bead ID")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, resolveCommand("bd"), "show", beadID, "--json")
		if dir, err := anvilDirForBead(beadID); err == nil {
			cmd.Dir = dir
		}
		out, err := cmd.CombinedOutput()
		if err != nil {
			outStr := strings.TrimSpace(string(out))
			log.Printf("bd show %s --json failed: %v: %s", beadID, err, outStr)
			if strings.Contains(outStr, "not found") || strings.Contains(outStr, "no matching") {
				writeError(w, http.StatusNotFound, "bead not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "failed to fetch bead details")
			return
		}

		var rawBeads []map[string]any
		if err := json.Unmarshal(out, &rawBeads); err != nil {
			log.Printf("bd show %s: failed to parse JSON: %v", beadID, err)
			writeError(w, http.StatusInternalServerError, "failed to parse bead details")
			return
		}
		if len(rawBeads) == 0 {
			writeError(w, http.StatusNotFound, "bead not found")
			return
		}

		raw := rawBeads[0]
		detail := normalizeBeadDetail(raw)
		writeJSON(w, http.StatusOK, detail)
	}
}

func normalizeBeadDetail(raw map[string]any) BeadDetail {
	str := func(key string) string {
		if v, ok := raw[key].(string); ok {
			return v
		}
		return ""
	}
	num := func(key string) int {
		if v, ok := raw[key].(float64); ok {
			return int(v)
		}
		return 0
	}

	detail := BeadDetail{
		ID:                 str("id"),
		Title:              str("title"),
		Description:        str("description"),
		Notes:              str("notes"),
		Design:             str("design"),
		AcceptanceCriteria: str("acceptance_criteria"),
		Status:             str("status"),
		Priority:           num("priority"),
		IssueType:          str("issue_type"),
		Owner:              str("owner"),
		Assignee:           str("assignee"),
		CreatedAt:          str("created_at"),
		CreatedBy:          str("created_by"),
		UpdatedAt:          str("updated_at"),
		ClosedAt:           str("closed_at"),
		CloseReason:        str("close_reason"),
		Labels:             make([]string, 0),
		Comments:           make([]BeadComment, 0),
		Dependencies:       make([]BeadDependency, 0),
		Dependents:         make([]BeadDependency, 0),
	}

	if labels, ok := raw["labels"].([]any); ok {
		for _, l := range labels {
			if s, ok := l.(string); ok {
				detail.Labels = append(detail.Labels, s)
			}
		}
	}

	if comments, ok := raw["comments"].([]any); ok {
		for _, c := range comments {
			if m, ok := c.(map[string]any); ok {
				bc := BeadComment{}
				if v, ok := m["author"].(string); ok {
					bc.Author = v
				}
				if v, ok := m["body"].(string); ok {
					bc.Body = v
				}
				if v, ok := m["created_at"].(string); ok {
					bc.CreatedAt = v
				}
				detail.Comments = append(detail.Comments, bc)
			}
		}
	}

	parseDeps := func(key string) []BeadDependency {
		deps := make([]BeadDependency, 0)
		direction := "dependency"
		if key == "dependents" {
			direction = "dependent"
		}
		if arr, ok := raw[key].([]any); ok {
			for _, item := range arr {
				if m, ok := item.(map[string]any); ok {
					d := BeadDependency{
						Direction: direction,
					}
					if v, ok := m["id"].(string); ok {
						d.ID = v
					}
					if v, ok := m["title"].(string); ok {
						d.Title = v
					}
					if v, ok := m["status"].(string); ok {
						d.Status = v
					}
					if v, ok := m["priority"].(float64); ok {
						d.Priority = int(v)
					}
					if v, ok := m["issue_type"].(string); ok {
						d.IssueType = v
					}
					if v, ok := m["dependency_type"].(string); ok {
						d.DependencyType = v
					}
					deps = append(deps, d)
				}
			}
		}
		return deps
	}

	detail.Dependencies = parseDeps("dependencies")
	detail.Dependents = parseDeps("dependents")

	return detail
}

// RemoveLabelHandler removes a label from a bead by invoking "bd label remove" directly.
// This bypasses IPC to avoid the 5-second read timeout (see Hytte-e535).
func RemoveLabelHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		beadID := chi.URLParam(r, "id")
		label := chi.URLParam(r, "label")
		if beadID == "" || !validBeadID.MatchString(beadID) {
			writeError(w, http.StatusBadRequest, "invalid bead ID")
			return
		}
		if label == "" || !validLabel.MatchString(label) {
			writeError(w, http.StatusBadRequest, "invalid label")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, resolveCommand("bd"), "label", "remove", beadID, label)
		if out, err := cmd.CombinedOutput(); err != nil {
			log.Printf("bd label remove %s %s failed: %v: %s", beadID, label, err, out)
			writeError(w, http.StatusInternalServerError, "failed to remove label")
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
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

// ClosedPRsHandler returns recently merged or closed pull requests, limited to
// 5 per anvil by default. The frontend uses this for the "Recently Closed PRs"
// dashboard panel.
func ClosedPRsHandler(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if db == nil {
			writeError(w, http.StatusServiceUnavailable, "forge state database not available")
			return
		}
		prs, err := db.ClosedPRs(5)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load closed PRs")
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

// CostsTrendHandler returns per-day cost data for trend charts.
// Query param: days — number of days to include (default 7, max 90).
func CostsTrendHandler(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if db == nil {
			writeError(w, http.StatusServiceUnavailable, "forge state database not available")
			return
		}
		days := 7
		if s := r.URL.Query().Get("days"); s != "" {
			if n, err := strconv.Atoi(s); err == nil && n > 0 {
				if n > 90 {
					writeError(w, http.StatusBadRequest, "days must be 90 or fewer")
					return
				}
				days = n
			}
		}
		entries, err := db.CostTrend(days)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load cost trend")
			return
		}
		writeJSON(w, http.StatusOK, entries)
	}
}

// TopBeadCostsHandler returns the most expensive beads for the given period.
// Query params: days (default 7, max 90), limit (default 5, max 20).
func TopBeadCostsHandler(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if db == nil {
			writeError(w, http.StatusServiceUnavailable, "forge state database not available")
			return
		}
		days := 7
		if s := r.URL.Query().Get("days"); s != "" {
			if n, err := strconv.Atoi(s); err == nil && n > 0 {
				if n > 90 {
					writeError(w, http.StatusBadRequest, "days must be 90 or fewer")
					return
				}
				days = n
			}
		}
		limit := 5
		if s := r.URL.Query().Get("limit"); s != "" {
			if n, err := strconv.Atoi(s); err == nil && n > 0 && n <= 20 {
				limit = n
			}
		}
		beads, err := db.TopBeadCosts(days, limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load top bead costs")
			return
		}
		writeJSON(w, http.StatusOK, beads)
	}
}

// MergePRHandler signals the forge daemon to merge a pull request.
// It sends a fire-and-forget "merge-pr <id>" command to the daemon socket,
// avoiding the IPC read timeout (see Hytte-e535).
func MergePRHandler() http.HandlerFunc {
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
		if err := signalDaemon("merge-pr " + prID); err != nil {
			log.Printf("forge: merge-pr %s failed: %v", prID, err)
			writeError(w, http.StatusInternalServerError, "failed to send merge command")
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
}

// RetryBeadHandler retries a bead that needs human attention by invoking
// "forge queue retry" via exec.Command instead of IPC (see Hytte-e535).
func RetryBeadHandler(db *DB) http.HandlerFunc {
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
		if db == nil {
			writeError(w, http.StatusServiceUnavailable, "forge state database not available")
			return
		}
		retry, err := db.RetryByBeadID(beadID)
		if err != nil {
			log.Printf("forge: retry lookup %s: %v", beadID, err)
			if errors.Is(err, sql.ErrNoRows) {
				writeError(w, http.StatusNotFound, "bead not found in retry list")
			} else {
				writeError(w, http.StatusInternalServerError, "failed to look up bead retry state")
			}
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, resolveCommand("forge"), "queue", "retry", beadID, "--anvil", retry.Anvil)
		if out, err := cmd.CombinedOutput(); err != nil {
			log.Printf("forge queue retry %s --anvil %s failed: %v: %s", beadID, retry.Anvil, err, out)
			writeError(w, http.StatusInternalServerError, "failed to retry bead")
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
}

// KillWorkerHandler stops a running worker by invoking "forge queue stop"
// via exec.Command instead of IPC (see Hytte-e535). It looks up the worker
// in state.db to resolve the bead ID and anvil needed by the CLI.
func KillWorkerHandler(db *DB) http.HandlerFunc {
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
		if db == nil {
			writeError(w, http.StatusServiceUnavailable, "forge state database not available")
			return
		}
		worker, err := db.WorkerByID(workerID)
		if err != nil {
			log.Printf("forge: worker lookup %s: %v", workerID, err)
			if errors.Is(err, sql.ErrNoRows) {
				writeError(w, http.StatusNotFound, "worker not found")
			} else {
				writeError(w, http.StatusInternalServerError, "failed to look up worker")
			}
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, resolveCommand("forge"), "queue", "stop", worker.BeadID, "--anvil", worker.Anvil)
		if out, err := cmd.CombinedOutput(); err != nil {
			log.Printf("forge queue stop %s --anvil %s failed: %v: %s", worker.BeadID, worker.Anvil, err, out)
			writeError(w, http.StatusInternalServerError, "failed to stop worker")
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
}

// RefreshHandler signals the forge daemon to trigger an immediate poll cycle.
// Uses a fire-and-forget socket write instead of IPC (see Hytte-e535).
func RefreshHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := signalDaemon("refresh"); err != nil {
			log.Printf("forge: refresh signal failed: %v", err)
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
// NOTE: This implementation uses periodic DB polling rather than subscribing
// to Forge's internal IPC/event bus. IPC subscription is not currently feasible
// because the HTTP server and the forge daemon are separate processes without
// a shared in-process event channel. DB polling adds per-client load (~one
// lightweight query per 2s per open connection) and up to 2s of event latency,
// which is acceptable for a status dashboard.
//
// Event shape:
//
//	{ "id": int, "timestamp": RFC3339, "type": string, "message": string,
//	  "bead_id": string, "anvil": string }
//
// Each SSE event carries an `id:` field so EventSource can send Last-Event-ID
// on reconnect, avoiding duplicates or missed events.
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

		// If the client sends Last-Event-ID (reconnect), resume from that point
		// instead of replaying the initial batch.
		var lastID int
		if s := r.Header.Get("Last-Event-ID"); s != "" {
			if n, err := strconv.Atoi(s); err == nil {
				lastID = n
			}
		}

		if lastID == 0 {
			// No Last-Event-ID — send initial batch of recent events, oldest first.
			initial, err := db.Events(50, "", "")
			if err == nil {
				for i := len(initial) - 1; i >= 0; i-- {
					e := initial[i]
					if e.ID > lastID {
						lastID = e.ID
					}
					data, _ := json.Marshal(e)
					fmt.Fprintf(w, "id: %d\ndata: %s\n\n", e.ID, data)
				}
				flusher.Flush()
			}
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
					fmt.Fprintf(w, "id: %d\ndata: %s\n\n", e.ID, data)
				}
				if len(newEvents) > 0 {
					flusher.Flush()
				}
			}
		}
	}
}

// WorkerLogHandler serves a worker's log file.
//
// Two modes are supported:
//
//  1. Tail mode (recommended): if the `tail` query parameter is provided
//     (e.g. ?tail=N), return the last N log lines as a JSON object:
//     {"lines": ["line1", "line2", ...]}. N must be a positive integer;
//     invalid values default to 100, and values greater than 10000 are clamped.
//     At most 1 MiB of the file is read from the end, so very large files are
//     handled without loading them fully into memory.
//
//  2. SSE streaming mode (legacy): without the tail parameter, the handler
//     streams the file content as Server-Sent Events and polls for new lines
//     every 500 ms using the file size to detect growth.
//     SSE line shape: { "line": string, "timestamp": RFC3339 }
//
// The log file path is read from the worker record in the forge state database.
// If the path is relative it is resolved relative to ~/.forge/.
// Absolute paths are restricted to ~/.forge/ and paths containing a /.workers/
// component (the directories where forge places worker log files).
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
			if errors.Is(err, sql.ErrNoRows) {
				writeError(w, http.StatusNotFound, "worker not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "failed to load worker")
			return
		}

		logPath := worker.LogPath
		if logPath == "" {
			writeError(w, http.StatusNotFound, "worker has no log file")
			return
		}

		// Resolve relative paths against ~/.forge/ and restrict all paths to
		// forge-owned directories to limit blast radius of a poisoned workers table.
		home, err := os.UserHomeDir()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to resolve home directory")
			return
		}
		forgeDir := filepath.Join(home, ".forge")
		if !filepath.IsAbs(logPath) {
			logPath = filepath.Clean(filepath.Join(forgeDir, logPath))
		} else {
			logPath = filepath.Clean(logPath)
		}
		// Allow only ~/.forge/** and paths with a /.workers/ component under $HOME
		// (the two directories where forge places worker log files).
		forgePrefix := forgeDir + string(filepath.Separator)
		workersComponent := string(filepath.Separator) + ".workers" + string(filepath.Separator)
		homePrefix := home + string(filepath.Separator)
		isAllowed := func(p string) bool {
			underForge := p == forgeDir || strings.HasPrefix(p, forgePrefix)
			underWorkers := strings.HasPrefix(p, homePrefix) && strings.Contains(p, workersComponent)
			return underForge || underWorkers
		}
		if !isAllowed(logPath) {
			writeError(w, http.StatusBadRequest, "invalid log path")
			return
		}
		// Verify the path is a regular file (not a symlink, directory, or device).
		fi, statErr := os.Lstat(logPath)
		if statErr != nil {
			if os.IsNotExist(statErr) {
				writeError(w, http.StatusNotFound, "log file not found")
			} else {
				writeError(w, http.StatusInternalServerError, "failed to stat log file")
			}
			return
		}
		if !fi.Mode().IsRegular() {
			writeError(w, http.StatusBadRequest, "log path is not a regular file")
			return
		}
		// Resolve symlinks in parent directories and re-verify the allowlist to
		// prevent bypasses where a path component (e.g. ~/.forge or .workers) is
		// itself a symlink pointing outside the intended roots.
		resolvedPath, resolveErr := filepath.EvalSymlinks(logPath)
		if resolveErr != nil {
			writeError(w, http.StatusInternalServerError, "failed to resolve log path")
			return
		}
		resolvedPath = filepath.Clean(resolvedPath)
		if !isAllowed(resolvedPath) {
			writeError(w, http.StatusBadRequest, "invalid log path")
			return
		}
		logPath = resolvedPath

		// If tail=N is specified, return the last N lines as JSON instead of streaming.
		if tailParam := r.URL.Query().Get("tail"); tailParam != "" {
			n, err := strconv.Atoi(tailParam)
			if err != nil || n <= 0 {
				n = 100
			}
			if n > 10000 {
				n = 10000
			}
			// Read at most 1 MiB from the end of the file to avoid loading large
			// logs fully into memory when only a small tail is needed.
			const maxTailReadBytes int64 = 1 << 20 // 1 MiB
			var data []byte
			var seeked bool
			if fi.Size() <= maxTailReadBytes {
				data, err = os.ReadFile(logPath) //nolint:gosec
			} else {
				seeked = true
				f, ferr := os.Open(logPath) //nolint:gosec
				if ferr != nil {
					writeError(w, http.StatusInternalServerError, "failed to read log file")
					return
				}
				defer f.Close()
				if _, ferr = f.Seek(-maxTailReadBytes, io.SeekEnd); ferr != nil {
					writeError(w, http.StatusInternalServerError, "failed to read log file")
					return
				}
				data, err = io.ReadAll(f)
			}
			if err != nil {
				writeError(w, http.StatusInternalServerError, "failed to read log file")
				return
			}
			// If we seeked into the middle of the file, the first bytes up to the
			// first newline are a partial line — discard them so callers always
			// receive complete lines.
			if seeked {
				if idx := strings.IndexByte(string(data), '\n'); idx >= 0 {
					data = data[idx+1:]
				} else {
					data = nil // entire chunk was one partial line
				}
			}
			raw := strings.TrimRight(string(data), "\n")
			lines := make([]string, 0)
			if raw != "" {
				lines = strings.Split(raw, "\n")
			}
			if len(lines) > n {
				lines = lines[len(lines)-n:]
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{"lines": lines})
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

		f, err := os.Open(logPath) //nolint:gosec
		if err != nil {
			fmt.Fprintf(w, "event: error\ndata: {\"error\":\"log file not accessible\"}\n\n")
			flusher.Flush()
			return
		}
		defer f.Close()

		// Stream existing content line by line using a buffered scanner to avoid
		// allocating the entire file into memory (Comment 4).
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				continue
			}
			entry := map[string]string{"line": line, "timestamp": time.Now().UTC().Format(time.RFC3339)}
			data, _ := json.Marshal(entry)
			fmt.Fprintf(w, "data: %s\n\n", data)
		}
		flusher.Flush()

		// Use the current file size as the tail offset so we don't re-read
		// bytes the scanner already consumed.
		var offset int64
		if fi, err := f.Stat(); err == nil {
			offset = fi.Size()
		}

		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()

		// partial holds an incomplete line carried over from the previous read.
		var partial string

		for {
			select {
			case <-r.Context().Done():
				return
			case <-ticker.C:
				fi, err := f.Stat()
				if err != nil {
					continue
				}
				// Handle truncation/rotation: reopen the file and reset state.
				if fi.Size() < offset {
					f.Close()
					f, err = os.Open(logPath) //nolint:gosec
					if err != nil {
						continue
					}
					offset = 0
					partial = ""
					continue
				}
				if fi.Size() <= offset {
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
				// Prepend any buffered partial line from the previous read.
				chunk := partial + string(buf[:n])
				lines := strings.Split(chunk, "\n")
				// The last element may be an incomplete line (no trailing newline).
				partial = lines[len(lines)-1]
				lines = lines[:len(lines)-1]
				flushed := false
				for _, line := range lines {
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

// BellowsPRHandler signals the forge daemon to assign bellows to monitor a PR.
// Uses a fire-and-forget socket write instead of IPC (see Hytte-e535).
func BellowsPRHandler() http.HandlerFunc {
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
		if err := signalDaemon("bellows " + prID); err != nil {
			log.Printf("forge: bellows %s failed: %v", prID, err)
			writeError(w, http.StatusInternalServerError, "failed to send bellows command")
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
}

// ApprovePRHandler signals the forge daemon to approve a PR as-is, skipping
// warden review. Uses a fire-and-forget socket write instead of IPC (see Hytte-e535).
func ApprovePRHandler() http.HandlerFunc {
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
		if err := signalDaemon("approve-pr " + prID); err != nil {
			log.Printf("forge: approve-pr %s failed: %v", prID, err)
			writeError(w, http.StatusInternalServerError, "failed to send approve command")
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
}

// WorkerParsedLogHandler returns a worker's stream-json log file as a structured
// JSON array of LogEntry objects. Each entry has type ("tool_use", "text", "think"),
// name (tool name for tool_use), content (formatted input/output), and status
// ("success" or "error" for tool_use entries, set by correlating tool results).
// Returns 404 if the worker or its log file is not found. Log parse errors are
// tolerated on a best-effort basis (malformed entries are skipped), and the handler
// returns the successfully parsed entries (or an empty array) with HTTP 200.
func WorkerParsedLogHandler(db *DB) http.HandlerFunc {
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
			if errors.Is(err, sql.ErrNoRows) {
				writeError(w, http.StatusNotFound, "worker not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "failed to load worker")
			return
		}

		logPath := worker.LogPath
		if logPath == "" {
			writeError(w, http.StatusNotFound, "worker has no log file")
			return
		}

		// Resolve relative paths and restrict to forge-owned directories,
		// using the same logic as WorkerLogHandler.
		home, err := os.UserHomeDir()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to resolve home directory")
			return
		}
		forgeDir := filepath.Join(home, ".forge")
		if !filepath.IsAbs(logPath) {
			logPath = filepath.Clean(filepath.Join(forgeDir, logPath))
		} else {
			logPath = filepath.Clean(logPath)
		}
		forgePrefix := forgeDir + string(filepath.Separator)
		workersComponent := string(filepath.Separator) + ".workers" + string(filepath.Separator)
		homePrefix := home + string(filepath.Separator)
		isAllowed := func(p string) bool {
			underForge := p == forgeDir || strings.HasPrefix(p, forgePrefix)
			underWorkers := strings.HasPrefix(p, homePrefix) && strings.Contains(p, workersComponent)
			return underForge || underWorkers
		}
		if !isAllowed(logPath) {
			writeError(w, http.StatusBadRequest, "invalid log path")
			return
		}
		fi, statErr := os.Lstat(logPath)
		if statErr != nil {
			if os.IsNotExist(statErr) {
				writeError(w, http.StatusNotFound, "log file not found")
			} else {
				writeError(w, http.StatusInternalServerError, "failed to stat log file")
			}
			return
		}
		if !fi.Mode().IsRegular() {
			writeError(w, http.StatusBadRequest, "log path is not a regular file")
			return
		}
		resolvedPath, resolveErr := filepath.EvalSymlinks(logPath)
		if resolveErr != nil {
			writeError(w, http.StatusInternalServerError, "failed to resolve log path")
			return
		}
		resolvedPath = filepath.Clean(resolvedPath)
		if !isAllowed(resolvedPath) {
			writeError(w, http.StatusBadRequest, "invalid log path")
			return
		}

		entries, err := ParseWorkerLog(resolvedPath)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to parse log file")
			return
		}
		if entries == nil {
			entries = []LogEntry{}
		}

		// If ?tail=N is provided, return only the last N entries.
		if tailParam := r.URL.Query().Get("tail"); tailParam != "" {
			n, err := strconv.Atoi(tailParam)
			if err != nil || n <= 0 {
				n = 100
			}
			if n > 10000 {
				n = 10000
			}
			if len(entries) > n {
				entries = entries[len(entries)-n:]
			}
		}

		writeJSON(w, http.StatusOK, entries)
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

// beadStatuses is the ordered list of statuses accepted by the status mutation endpoint.
// validStatuses and the 400 error message are both derived from this slice so they
// never drift out of sync with each other or with the test suite.
var beadStatuses = []string{
	"open",
	"in_progress",
	"blocked",
	"deferred",
	"closed",
	"pinned",
	"hooked",
}

// validStatuses provides O(1) membership checks for beadStatuses.
var validStatuses = func() map[string]bool {
	m := make(map[string]bool, len(beadStatuses))
	for _, s := range beadStatuses {
		m[s] = true
	}
	return m
}()

// CommentHandler adds a comment to a bead by invoking "bd comments add".
func CommentHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		beadID := chi.URLParam(r, "id")
		if beadID == "" || !validBeadID.MatchString(beadID) {
			writeError(w, http.StatusBadRequest, "invalid bead ID")
			return
		}
		var body struct {
			Body string `json:"body"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		body.Body = strings.TrimSpace(body.Body)
		if body.Body == "" {
			writeError(w, http.StatusBadRequest, "comment body is required")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, resolveCommand("bd"), "comments", "add", beadID, "--", body.Body)
		if dir, err := anvilDirForBead(beadID); err == nil {
			cmd.Dir = dir
		}
		if out, err := cmd.CombinedOutput(); err != nil {
			log.Printf("bd comments add %s failed: %v: %s", beadID, err, out)
			writeError(w, http.StatusInternalServerError, "failed to add comment")
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
}

// UpdatePriorityHandler updates a bead's priority by invoking "bd update --priority".
func UpdatePriorityHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		beadID := chi.URLParam(r, "id")
		if beadID == "" || !validBeadID.MatchString(beadID) {
			writeError(w, http.StatusBadRequest, "invalid bead ID")
			return
		}
		var body struct {
			Priority *int `json:"priority"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if body.Priority == nil {
			writeError(w, http.StatusBadRequest, "priority is required")
			return
		}
		if *body.Priority < 0 || *body.Priority > 4 {
			writeError(w, http.StatusBadRequest, "priority must be between 0 and 4")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, resolveCommand("bd"), "update", beadID, "--priority", strconv.Itoa(*body.Priority))
		if dir, err := anvilDirForBead(beadID); err == nil {
			cmd.Dir = dir
		}
		if out, err := cmd.CombinedOutput(); err != nil {
			log.Printf("bd update %s --priority failed: %v: %s", beadID, err, out)
			writeError(w, http.StatusInternalServerError, "failed to update priority")
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
}

// UpdateStatusHandler updates a bead's status by invoking "bd update --status".
func UpdateStatusHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		beadID := chi.URLParam(r, "id")
		if beadID == "" || !validBeadID.MatchString(beadID) {
			writeError(w, http.StatusBadRequest, "invalid bead ID")
			return
		}
		var body struct {
			Status string `json:"status"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		body.Status = strings.TrimSpace(body.Status)
		if !validStatuses[body.Status] {
			writeError(w, http.StatusBadRequest, "status must be one of: "+strings.Join(beadStatuses, ", "))
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, resolveCommand("bd"), "update", beadID, "--status", body.Status)
		if dir, err := anvilDirForBead(beadID); err == nil {
			cmd.Dir = dir
		}
		if out, err := cmd.CombinedOutput(); err != nil {
			log.Printf("bd update %s --status %s failed: %v: %s", beadID, body.Status, err, out)
			writeError(w, http.StatusInternalServerError, "failed to update status")
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
}

// UpdateAssigneeHandler updates a bead's assignee by invoking "bd update --assignee".
func UpdateAssigneeHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		beadID := chi.URLParam(r, "id")
		if beadID == "" || !validBeadID.MatchString(beadID) {
			writeError(w, http.StatusBadRequest, "invalid bead ID")
			return
		}
		var body struct {
			Assignee *string `json:"assignee"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if body.Assignee == nil {
			writeError(w, http.StatusBadRequest, "assignee field is required (use empty string to unassign)")
			return
		}
		assignee := strings.TrimSpace(*body.Assignee)
		// Non-empty assignee must be a valid username; empty string means unassign.
		if assignee != "" && !validAssignee.MatchString(assignee) {
			writeError(w, http.StatusBadRequest, "invalid assignee format")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, resolveCommand("bd"), "update", beadID, "--assignee", assignee)
		if dir, err := anvilDirForBead(beadID); err == nil {
			cmd.Dir = dir
		}
		if out, err := cmd.CombinedOutput(); err != nil {
			log.Printf("bd update %s --assignee failed: %v: %s", beadID, err, out)
			writeError(w, http.StatusInternalServerError, "failed to update assignee")
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
}

// SetLabelsHandler replaces all labels on a bead by invoking "bd update --set-labels".
func SetLabelsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		beadID := chi.URLParam(r, "id")
		if beadID == "" || !validBeadID.MatchString(beadID) {
			writeError(w, http.StatusBadRequest, "invalid bead ID")
			return
		}
		var body struct {
			Labels []string `json:"labels"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if body.Labels == nil {
			writeError(w, http.StatusBadRequest, "labels array is required")
			return
		}
		for _, l := range body.Labels {
			if !validLabel.MatchString(l) {
				writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid label: %s", l))
				return
			}
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		args := []string{"update", beadID}
		if len(body.Labels) == 0 {
			args = append(args, "--set-labels", "")
		} else {
			for _, l := range body.Labels {
				args = append(args, "--set-labels", l)
			}
		}
		cmd := exec.CommandContext(ctx, resolveCommand("bd"), args...)
		if dir, err := anvilDirForBead(beadID); err == nil {
			cmd.Dir = dir
		}
		if out, err := cmd.CombinedOutput(); err != nil {
			log.Printf("bd update %s --set-labels failed: %v: %s", beadID, err, out)
			writeError(w, http.StatusInternalServerError, "failed to update labels")
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
}

// CloseBeadHandler closes a bead by invoking "bd close --reason".
func CloseBeadHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		beadID := chi.URLParam(r, "id")
		if beadID == "" || !validBeadID.MatchString(beadID) {
			writeError(w, http.StatusBadRequest, "invalid bead ID")
			return
		}
		var body struct {
			Reason string `json:"reason"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		body.Reason = strings.TrimSpace(body.Reason)
		if body.Reason == "" {
			writeError(w, http.StatusBadRequest, "reason is required")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, resolveCommand("bd"), "close", beadID, "--reason", body.Reason)
		if dir, err := anvilDirForBead(beadID); err == nil {
			cmd.Dir = dir
		}
		if out, err := cmd.CombinedOutput(); err != nil {
			log.Printf("bd close %s failed: %v: %s", beadID, err, out)
			writeError(w, http.StatusInternalServerError, "failed to close bead")
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
}

// DismissBeadHandler marks a stuck bead as handled, removing it from the
// needs-attention list without retrying. Uses "forge queue dismiss" via
// exec.Command (same pattern as RetryBeadHandler, see Hytte-e535).
func DismissBeadHandler(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		beadID := chi.URLParam(r, "id")
		if beadID == "" || !validBeadID.MatchString(beadID) {
			writeError(w, http.StatusBadRequest, "invalid bead ID")
			return
		}
		if db == nil {
			writeError(w, http.StatusServiceUnavailable, "forge state database not available")
			return
		}
		retry, err := db.RetryByBeadID(beadID)
		if err != nil {
			log.Printf("forge: dismiss lookup %s: %v", beadID, err)
			if errors.Is(err, sql.ErrNoRows) {
				writeError(w, http.StatusNotFound, "bead not found in retry list")
			} else {
				writeError(w, http.StatusInternalServerError, "failed to look up bead retry state")
			}
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, resolveCommand("forge"), "queue", "dismiss", beadID, "--anvil", retry.Anvil)
		if out, err := cmd.CombinedOutput(); err != nil {
			log.Printf("forge queue dismiss %s --anvil %s failed: %v: %s", beadID, retry.Anvil, err, out)
			writeError(w, http.StatusInternalServerError, "failed to dismiss bead")
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
}

// ApproveBeadHandler skips warden review and creates a PR from the bead's
// current branch state. Uses "forge queue approve" via exec.Command.
func ApproveBeadHandler(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		beadID := chi.URLParam(r, "id")
		if beadID == "" || !validBeadID.MatchString(beadID) {
			writeError(w, http.StatusBadRequest, "invalid bead ID")
			return
		}
		if db == nil {
			writeError(w, http.StatusServiceUnavailable, "forge state database not available")
			return
		}
		retry, err := db.RetryByBeadID(beadID)
		if err != nil {
			log.Printf("forge: approve lookup %s: %v", beadID, err)
			if errors.Is(err, sql.ErrNoRows) {
				writeError(w, http.StatusNotFound, "bead not found in retry list")
			} else {
				writeError(w, http.StatusInternalServerError, "failed to look up bead retry state")
			}
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, resolveCommand("forge"), "queue", "approve", beadID, "--anvil", retry.Anvil)
		if out, err := cmd.CombinedOutput(); err != nil {
			log.Printf("forge queue approve %s --anvil %s failed: %v: %s", beadID, retry.Anvil, err, out)
			writeError(w, http.StatusInternalServerError, "failed to approve bead")
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
}

// ForceSmithHandler re-runs Smith with a fresh prompt, ignoring previous
// attempts. Uses "forge queue force-smith" via exec.Command.
func ForceSmithHandler(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		beadID := chi.URLParam(r, "id")
		if beadID == "" || !validBeadID.MatchString(beadID) {
			writeError(w, http.StatusBadRequest, "invalid bead ID")
			return
		}
		if db == nil {
			writeError(w, http.StatusServiceUnavailable, "forge state database not available")
			return
		}
		retry, err := db.RetryByBeadID(beadID)
		if err != nil {
			log.Printf("forge: force-smith lookup %s: %v", beadID, err)
			if errors.Is(err, sql.ErrNoRows) {
				writeError(w, http.StatusNotFound, "bead not found in retry list")
			} else {
				writeError(w, http.StatusInternalServerError, "failed to look up bead retry state")
			}
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, resolveCommand("forge"), "queue", "force-smith", beadID, "--anvil", retry.Anvil)
		if out, err := cmd.CombinedOutput(); err != nil {
			log.Printf("forge queue force-smith %s --anvil %s failed: %v: %s", beadID, retry.Anvil, err, out)
			writeError(w, http.StatusInternalServerError, "failed to force-smith bead")
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
}
