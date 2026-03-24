package training

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"
)

// defaultMaxHR is used when no max_hr user preference is set.
const defaultMaxHR = 190

// UpsertWeeklyLoad inserts or updates a WeeklyLoad record for the given
// (user_id, week_start) pair.
func UpsertWeeklyLoad(db *sql.DB, load WeeklyLoad) error {
	_, err := db.Exec(`
		INSERT INTO weekly_load (user_id, week_start, easy_load, hard_load, total_load, workout_count, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id, week_start) DO UPDATE SET
			easy_load     = excluded.easy_load,
			hard_load     = excluded.hard_load,
			total_load    = excluded.total_load,
			workout_count = excluded.workout_count,
			updated_at    = excluded.updated_at`,
		load.UserID, load.WeekStart, load.EasyLoad, load.HardLoad,
		load.TotalLoad, load.WorkoutCount, load.UpdatedAt,
	)
	return err
}

// GetWeeklyLoads returns up to n WeeklyLoad records for the given user,
// ordered by week_start descending (most recent first).
func GetWeeklyLoads(db *sql.DB, userID int64, n int) ([]WeeklyLoad, error) {
	rows, err := db.Query(`
		SELECT user_id, week_start, easy_load, hard_load, total_load, workout_count, updated_at
		FROM weekly_load
		WHERE user_id = ?
		ORDER BY week_start DESC
		LIMIT ?`,
		userID, n,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var loads []WeeklyLoad
	for rows.Next() {
		var wl WeeklyLoad
		if err := rows.Scan(&wl.UserID, &wl.WeekStart, &wl.EasyLoad, &wl.HardLoad,
			&wl.TotalLoad, &wl.WorkoutCount, &wl.UpdatedAt); err != nil {
			return nil, err
		}
		loads = append(loads, wl)
	}
	return loads, rows.Err()
}

// RefreshWeeklyLoad recomputes the WeeklyLoad for the 7-day window starting
// at weekStart (UTC midnight) from the workouts table. Workouts without a
// training_load value are excluded. Easy vs hard effort is determined by
// comparing avg_heart_rate against 80% of the user's max_hr preference
// (defaultMaxHR if the preference is absent). The result is upserted into
// the weekly_load table and returned.
func RefreshWeeklyLoad(db *sql.DB, userID int64, weekStart time.Time) (*WeeklyLoad, error) {
	ws := weekStart.UTC()
	ws = time.Date(ws.Year(), ws.Month(), ws.Day(), 0, 0, 0, 0, time.UTC)
	we := ws.AddDate(0, 0, 7)

	maxHR := defaultMaxHR
	var prefVal string
	err := db.QueryRow(
		`SELECT value FROM user_preferences WHERE user_id = ? AND key = 'max_hr'`, userID,
	).Scan(&prefVal)
	switch {
	case err == nil:
		if v, parseErr := strconv.Atoi(prefVal); parseErr == nil && v > 0 {
			maxHR = v
		}
	case errors.Is(err, sql.ErrNoRows):
		// No max_hr preference set; use default.
	default:
		return nil, fmt.Errorf("query max_hr preference: %w", err)
	}
	threshold := float64(maxHR) * 0.8

	rows, err := db.Query(`
		SELECT avg_heart_rate, training_load
		FROM workouts
		WHERE user_id = ?
		  AND training_load IS NOT NULL
		  AND started_at >= ?
		  AND started_at < ?`,
		userID,
		ws.Format(time.RFC3339),
		we.Format(time.RFC3339),
	)
	if err != nil {
		return nil, fmt.Errorf("query workouts: %w", err)
	}
	defer rows.Close()

	var easyLoad, hardLoad float64
	var count int
	for rows.Next() {
		var avgHR sql.NullInt64
		var load float64
		if err := rows.Scan(&avgHR, &load); err != nil {
			return nil, fmt.Errorf("scan workout: %w", err)
		}
		count++
		if avgHR.Valid && float64(avgHR.Int64) >= threshold {
			hardLoad += load
		} else {
			easyLoad += load
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	wl := &WeeklyLoad{
		UserID:       userID,
		WeekStart:    ws.Format("2006-01-02"),
		EasyLoad:     easyLoad,
		HardLoad:     hardLoad,
		TotalLoad:    easyLoad + hardLoad,
		WorkoutCount: count,
		UpdatedAt:    time.Now().UTC().Format(time.RFC3339),
	}

	if err := UpsertWeeklyLoad(db, *wl); err != nil {
		return nil, fmt.Errorf("upsert weekly load: %w", err)
	}
	return wl, nil
}

// UpsertTrainingSummary inserts or updates a TrainingSummary record (load/status data only).
// If Period is empty it defaults to "week" for backward compatibility.
func UpsertTrainingSummary(db *sql.DB, s TrainingSummary) error {
	if s.Period == "" {
		s.Period = "week"
	}
	_, err := db.Exec(`
		INSERT INTO training_summaries (user_id, period, week_start, status, acr, acute_load, chronic_load, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id, period, week_start) DO UPDATE SET
			status       = excluded.status,
			acr          = excluded.acr,
			acute_load   = excluded.acute_load,
			chronic_load = excluded.chronic_load,
			updated_at   = excluded.updated_at`,
		s.UserID, s.Period, s.WeekStart, s.Status, s.ACR, s.AcuteLoad, s.ChronicLoad, s.UpdatedAt,
	)
	return err
}

// UpsertTrainingSummaryAnalysis inserts or updates a TrainingSummary record including
// AI-generated analysis fields (prompt, response_json, model).
// If Period is empty it defaults to "week".
func UpsertTrainingSummaryAnalysis(db *sql.DB, s TrainingSummary) error {
	if s.Period == "" {
		s.Period = "week"
	}
	_, err := db.Exec(`
		INSERT INTO training_summaries
			(user_id, period, week_start, status, acr, acute_load, chronic_load, prompt, response_json, model, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id, period, week_start) DO UPDATE SET
			status        = excluded.status,
			acr           = excluded.acr,
			acute_load    = excluded.acute_load,
			chronic_load  = excluded.chronic_load,
			prompt        = excluded.prompt,
			response_json = excluded.response_json,
			model         = excluded.model,
			updated_at    = excluded.updated_at`,
		s.UserID, s.Period, s.WeekStart, s.Status, s.ACR, s.AcuteLoad, s.ChronicLoad,
		s.Prompt, s.ResponseJSON, s.Model, s.UpdatedAt,
	)
	return err
}

// GetLatestTrainingSummary returns the most recently stored weekly TrainingSummary
// for the given user. Returns sql.ErrNoRows if no summary has been stored yet.
func GetLatestTrainingSummary(db *sql.DB, userID int64) (*TrainingSummary, error) {
	var s TrainingSummary
	var acr sql.NullFloat64
	err := db.QueryRow(`
		SELECT user_id, period, week_start, status, acr, acute_load, chronic_load, updated_at
		FROM training_summaries
		WHERE user_id = ? AND period = 'week'
		ORDER BY week_start DESC
		LIMIT 1`,
		userID,
	).Scan(&s.UserID, &s.Period, &s.WeekStart, &s.Status, &acr, &s.AcuteLoad, &s.ChronicLoad, &s.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if acr.Valid {
		s.ACR = &acr.Float64
	}
	return &s, nil
}

// getTrainingSummaryByPeriod retrieves a TrainingSummary by (user_id, period, week_start).
// Returns sql.ErrNoRows if no matching record exists.
func getTrainingSummaryByPeriod(db *sql.DB, userID int64, period, weekStart string) (*TrainingSummary, error) {
	var s TrainingSummary
	var acr sql.NullFloat64
	err := db.QueryRow(`
		SELECT user_id, period, week_start, status, acr, acute_load, chronic_load, prompt, response_json, model, updated_at
		FROM training_summaries
		WHERE user_id = ? AND period = ? AND week_start = ?`,
		userID, period, weekStart,
	).Scan(&s.UserID, &s.Period, &s.WeekStart, &s.Status, &acr,
		&s.AcuteLoad, &s.ChronicLoad, &s.Prompt, &s.ResponseJSON, &s.Model, &s.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if acr.Valid {
		s.ACR = &acr.Float64
	}
	return &s, nil
}
