package infra

import (
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// UptimeRecord represents a single check result recorded in history.
type UptimeRecord struct {
	ID        int64  `json:"id"`
	Module    string `json:"module"`
	Target    string `json:"target"`
	Status    string `json:"status"`
	Message   string `json:"message"`
	CheckedAt string `json:"checked_at"`
}

// UptimeStats holds uptime percentages for different time windows.
type UptimeStats struct {
	Uptime24h   float64 `json:"uptime_24h"`
	Uptime7d    float64 `json:"uptime_7d"`
	Uptime30d   float64 `json:"uptime_30d"`
	TotalChecks int     `json:"total_checks"`
}

// UptimeModule provides uptime history and statistics.
type UptimeModule struct {
	db *sql.DB
}

// NewUptimeModule creates an uptime history module.
func NewUptimeModule(db *sql.DB) *UptimeModule {
	return &UptimeModule{db: db}
}

func (m *UptimeModule) Name() string        { return "uptime" }
func (m *UptimeModule) DisplayName() string { return "Uptime History" }
func (m *UptimeModule) Description() string {
	return "Track and display service uptime over time"
}

// Check returns uptime statistics.
func (m *UptimeModule) Check() ModuleResult {
	stats, err := GetUptimeStats(m.db)
	if err != nil {
		return ModuleResult{
			Name:      m.Name(),
			Status:    StatusUnknown,
			Message:   "Failed to compute uptime stats",
			CheckedAt: time.Now().UTC(),
		}
	}

	if stats.TotalChecks == 0 {
		return ModuleResult{
			Name:      m.Name(),
			Status:    StatusUnknown,
			Message:   "No check history yet",
			CheckedAt: time.Now().UTC(),
			Details:   stats,
		}
	}

	overall := StatusOK
	msg := fmt.Sprintf("%.1f%% uptime (24h)", stats.Uptime24h)

	if stats.Uptime24h < 50 {
		overall = StatusDown
	} else if stats.Uptime24h < 90 {
		overall = StatusDegraded
	}

	recent, _ := GetRecentChecks(m.db, 20)
	if recent == nil {
		recent = []UptimeRecord{}
	}

	return ModuleResult{
		Name:      m.Name(),
		Status:    overall,
		Message:   msg,
		CheckedAt: time.Now().UTC(),
		Details: map[string]any{
			"stats":  stats,
			"recent": recent,
		},
	}
}

// --- Database operations ---

// RecordCheck inserts a check result into the uptime history.
func RecordCheck(db *sql.DB, module, target string, status ModuleStatus, message string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(
		`INSERT INTO infra_uptime_history (module, target, status, message, checked_at) VALUES (?, ?, ?, ?, ?)`,
		module, target, string(status), message, now,
	)
	return err
}

// GetUptimeStats computes uptime percentages for 24h, 7d, and 30d windows.
func GetUptimeStats(db *sql.DB) (UptimeStats, error) {
	var stats UptimeStats

	now := time.Now().UTC()
	t24h := now.Add(-24 * time.Hour).Format(time.RFC3339)
	t7d := now.Add(-7 * 24 * time.Hour).Format(time.RFC3339)
	t30d := now.Add(-30 * 24 * time.Hour).Format(time.RFC3339)

	// Total checks in 30d window.
	err := db.QueryRow(`SELECT COUNT(*) FROM infra_uptime_history WHERE checked_at >= ?`, t30d).Scan(&stats.TotalChecks)
	if err != nil {
		return stats, err
	}

	stats.Uptime24h, err = uptimePercentage(db, t24h)
	if err != nil {
		return stats, err
	}
	stats.Uptime7d, err = uptimePercentage(db, t7d)
	if err != nil {
		return stats, err
	}
	stats.Uptime30d, err = uptimePercentage(db, t30d)
	if err != nil {
		return stats, err
	}

	return stats, nil
}

func uptimePercentage(db *sql.DB, since string) (float64, error) {
	var total, ok int
	err := db.QueryRow(
		`SELECT COUNT(*), COALESCE(SUM(CASE WHEN status = 'ok' THEN 1 ELSE 0 END), 0)
		 FROM infra_uptime_history WHERE checked_at >= ?`, since,
	).Scan(&total, &ok)
	if err != nil {
		return 0, err
	}
	if total == 0 {
		return 100, nil
	}
	return float64(ok) / float64(total) * 100, nil
}

// GetRecentChecks returns the most recent uptime records.
func GetRecentChecks(db *sql.DB, limit int) ([]UptimeRecord, error) {
	rows, err := db.Query(
		`SELECT id, module, target, status, message, checked_at
		 FROM infra_uptime_history ORDER BY checked_at DESC LIMIT ?`, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	records := make([]UptimeRecord, 0)
	for rows.Next() {
		var r UptimeRecord
		if err := rows.Scan(&r.ID, &r.Module, &r.Target, &r.Status, &r.Message, &r.CheckedAt); err != nil {
			return nil, err
		}
		records = append(records, r)
	}
	return records, rows.Err()
}

// --- HTTP handlers ---

// UptimeHistoryHandler returns uptime history with optional filtering.
func UptimeHistoryHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limitStr := r.URL.Query().Get("limit")
		limit := 50
		if limitStr != "" {
			if n, err := strconv.Atoi(limitStr); err == nil && n > 0 && n <= 500 {
				limit = n
			}
		}

		records, err := GetRecentChecks(db, limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load uptime history")
			return
		}
		if records == nil {
			records = []UptimeRecord{}
		}

		stats, err := GetUptimeStats(db)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to compute uptime stats")
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"stats":   stats,
			"records": records,
		})
	}
}

// ClearUptimeHistoryHandler deletes all uptime history records.
func ClearUptimeHistoryHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, err := db.Exec(`DELETE FROM infra_uptime_history`)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to clear history")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}
