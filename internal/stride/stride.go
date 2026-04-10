package stride

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/encryption"
)

// Race represents an upcoming race in the user's race calendar.
type Race struct {
	ID         int64   `json:"id"`
	UserID     int64   `json:"user_id"`
	Name       string  `json:"name"`       // encrypted at rest
	Date       string  `json:"date"`       // YYYY-MM-DD
	DistanceM  float64 `json:"distance_m"` // meters
	TargetTime *int    `json:"target_time"` // seconds, nullable
	Priority   string  `json:"priority"`   // A, B, or C
	Notes      string  `json:"notes"`      // encrypted at rest
	ResultTime *int    `json:"result_time"` // seconds, nullable
	CreatedAt  string  `json:"created_at"`
}

// Note represents a short free-text note from the user that feeds into the
// next Stride plan generation.
type Note struct {
	ID         int64      `json:"id"`
	UserID     int64      `json:"user_id"`
	PlanID     *int64     `json:"plan_id"`      // nullable — linked to plan when created during a plan week
	Content    string     `json:"content"`      // encrypted at rest
	TargetDate string     `json:"target_date"`  // YYYY-MM-DD — which date this note applies to
	ConsumedAt *time.Time `json:"consumed_at"`  // nullable — set when consumed by a process (e.g. plan generation)
	ConsumedBy *string    `json:"consumed_by"`  // nullable — identifier of the consuming process
	CreatedAt  string     `json:"created_at"`
}

// NextStrideRun returns the next time the weekly Stride cron should fire
// (Sundays at 18:00 in the given location). If now is Sunday before 18:00,
// that same day is returned; otherwise the following Sunday is returned.
func NextStrideRun(now time.Time, loc *time.Location) time.Time {
	if loc == nil {
		loc = time.UTC
	}
	now = now.In(loc)
	daysUntilSunday := (7 - int(now.Weekday())) % 7
	if daysUntilSunday == 0 {
		todayRun := time.Date(now.Year(), now.Month(), now.Day(), 18, 0, 0, 0, loc)
		if now.Before(todayRun) {
			return todayRun
		}
		return todayRun.AddDate(0, 0, 7)
	}
	return time.Date(now.Year(), now.Month(), now.Day()+daysUntilSunday, 18, 0, 0, 0, loc)
}

// scanNote scans a note row into a Note struct, handling nullable consumed_at/consumed_by
// columns and decrypting the content field.
func scanNote(scanner interface{ Scan(...any) error }, n *Note) error {
	var consumedAt sql.NullString
	var consumedBy sql.NullString
	if err := scanner.Scan(&n.ID, &n.UserID, &n.PlanID, &n.Content, &n.TargetDate, &consumedAt, &consumedBy, &n.CreatedAt); err != nil {
		return err
	}
	if consumedAt.Valid {
		t, err := time.Parse(time.RFC3339, consumedAt.String)
		if err != nil {
			return fmt.Errorf("parse consumed_at: %w", err)
		}
		n.ConsumedAt = &t
	}
	if consumedBy.Valid {
		n.ConsumedBy = &consumedBy.String
	}
	var err error
	if n.Content, err = encryption.DecryptField(n.Content); err != nil {
		return fmt.Errorf("decrypt note content: %w", err)
	}
	return nil
}

// ListRaces returns all races for a user ordered by date ascending.
func ListRaces(db *sql.DB, userID int64) ([]Race, error) {
	rows, err := db.Query(`
		SELECT id, user_id, name, date, distance_m, target_time, priority, notes, result_time, created_at
		FROM stride_races
		WHERE user_id = ?
		ORDER BY date ASC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var races []Race
	for rows.Next() {
		var r Race
		if err := rows.Scan(
			&r.ID, &r.UserID, &r.Name, &r.Date, &r.DistanceM,
			&r.TargetTime, &r.Priority, &r.Notes, &r.ResultTime, &r.CreatedAt,
		); err != nil {
			return nil, err
		}
		if r.Name, err = encryption.DecryptField(r.Name); err != nil {
			return nil, fmt.Errorf("decrypt race name: %w", err)
		}
		if r.Notes, err = encryption.DecryptField(r.Notes); err != nil {
			return nil, fmt.Errorf("decrypt race notes: %w", err)
		}
		races = append(races, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if races == nil {
		races = []Race{}
	}
	return races, nil
}

// CreateRace inserts a new race into the race calendar.
func CreateRace(db *sql.DB, userID int64, name, date string, distanceM float64, targetTime *int, priority, notes string) (*Race, error) {
	now := time.Now().UTC().Format(time.RFC3339)

	encName, err := encryption.EncryptField(name)
	if err != nil {
		return nil, fmt.Errorf("encrypt race name: %w", err)
	}
	encNotes, err := encryption.EncryptField(notes)
	if err != nil {
		return nil, fmt.Errorf("encrypt race notes: %w", err)
	}

	res, err := db.Exec(`
		INSERT INTO stride_races (user_id, name, date, distance_m, target_time, priority, notes, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, userID, encName, date, distanceM, targetTime, priority, encNotes, now)
	if err != nil {
		return nil, fmt.Errorf("insert race: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("last insert id: %w", err)
	}

	return GetRaceByID(db, id, userID)
}

// GetRaceByID returns a single race by ID, scoped to the given user.
func GetRaceByID(db *sql.DB, id, userID int64) (*Race, error) {
	var r Race
	err := db.QueryRow(`
		SELECT id, user_id, name, date, distance_m, target_time, priority, notes, result_time, created_at
		FROM stride_races
		WHERE id = ? AND user_id = ?
	`, id, userID).Scan(
		&r.ID, &r.UserID, &r.Name, &r.Date, &r.DistanceM,
		&r.TargetTime, &r.Priority, &r.Notes, &r.ResultTime, &r.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	if r.Name, err = encryption.DecryptField(r.Name); err != nil {
		return nil, fmt.Errorf("decrypt race name: %w", err)
	}
	if r.Notes, err = encryption.DecryptField(r.Notes); err != nil {
		return nil, fmt.Errorf("decrypt race notes: %w", err)
	}
	return &r, nil
}

// UpdateRace updates an existing race owned by the given user.
func UpdateRace(db *sql.DB, id, userID int64, name, date string, distanceM float64, targetTime *int, priority, notes string, resultTime *int) (*Race, error) {
	encName, err := encryption.EncryptField(name)
	if err != nil {
		return nil, fmt.Errorf("encrypt race name: %w", err)
	}
	encNotes, err := encryption.EncryptField(notes)
	if err != nil {
		return nil, fmt.Errorf("encrypt race notes: %w", err)
	}

	res, err := db.Exec(`
		UPDATE stride_races
		SET name = ?, date = ?, distance_m = ?, target_time = ?, priority = ?, notes = ?, result_time = ?
		WHERE id = ? AND user_id = ?
	`, encName, date, distanceM, targetTime, priority, encNotes, resultTime, id, userID)
	if err != nil {
		return nil, fmt.Errorf("update race: %w", err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return nil, sql.ErrNoRows
	}

	return GetRaceByID(db, id, userID)
}

// LinkWorkoutToRace sets workouts.race_id to the given race and populates
// stride_races.result_time from the workout's duration_seconds. Both the
// workout and race must belong to userID (consistent with other stride helpers
// that always take a userID for ownership enforcement).
func LinkWorkoutToRace(db *sql.DB, workoutID, raceID, userID int64) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin link tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	// Verify the race belongs to the user (inside tx to avoid TOCTOU).
	var raceExists int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM stride_races WHERE id = ? AND user_id = ?`, raceID, userID).Scan(&raceExists); err != nil {
		return fmt.Errorf("check race ownership: %w", err)
	}
	if raceExists == 0 {
		return fmt.Errorf("race %d not found for user %d", raceID, userID)
	}

	// Read the workout's duration to use as the race result time.
	var durationSeconds int
	err = tx.QueryRow(`SELECT duration_seconds FROM workouts WHERE id = ? AND user_id = ?`, workoutID, userID).Scan(&durationSeconds)
	if err == sql.ErrNoRows {
		return fmt.Errorf("workout %d not found for user %d", workoutID, userID)
	}
	if err != nil {
		return fmt.Errorf("get workout duration: %w", err)
	}

	if _, err := tx.Exec(`UPDATE workouts SET race_id = ? WHERE id = ? AND user_id = ?`, raceID, workoutID, userID); err != nil {
		return fmt.Errorf("set workout race_id: %w", err)
	}

	// Only populate result_time when duration is known; a zero duration would
	// overwrite an existing result with an invalid value.
	if durationSeconds > 0 {
		res, err := tx.Exec(`UPDATE stride_races SET result_time = ? WHERE id = ? AND user_id = ?`, durationSeconds, raceID, userID)
		if err != nil {
			return fmt.Errorf("set race result_time: %w", err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return fmt.Errorf("race %d not found for user %d", raceID, userID)
		}
	}

	return tx.Commit()
}

// FindMatchingRace queries stride_races for the single best-matching race for the
// given date (±1 day) and distance (within 20%), ordered by closest date.
// Returns nil if no race matches. Only one result is returned (LIMIT 1); callers
// that need multiple candidates should query directly.
func FindMatchingRace(db *sql.DB, userID int64, date string, distanceMeters float64) (*Race, error) {
	parsedDate, err := time.Parse("2006-01-02", date)
	if err != nil {
		return nil, fmt.Errorf("parse date %q: %w", date, err)
	}
	dayBefore := parsedDate.AddDate(0, 0, -1).Format("2006-01-02")
	dayAfter := parsedDate.AddDate(0, 0, 1).Format("2006-01-02")

	minDist := distanceMeters * 0.8
	maxDist := distanceMeters * 1.2

	var r Race
	err = db.QueryRow(`
		SELECT id, user_id, name, date, distance_m, target_time, priority, notes, result_time, created_at
		FROM stride_races
		WHERE user_id = ?
		  AND date >= ? AND date <= ?
		  AND distance_m >= ? AND distance_m <= ?
		ORDER BY ABS(julianday(date) - julianday(?)) ASC
		LIMIT 1
	`, userID, dayBefore, dayAfter, minDist, maxDist, date).Scan(
		&r.ID, &r.UserID, &r.Name, &r.Date, &r.DistanceM,
		&r.TargetTime, &r.Priority, &r.Notes, &r.ResultTime, &r.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query matching race: %w", err)
	}
	if r.Name, err = encryption.DecryptField(r.Name); err != nil {
		return nil, fmt.Errorf("decrypt race name: %w", err)
	}
	if r.Notes, err = encryption.DecryptField(r.Notes); err != nil {
		return nil, fmt.Errorf("decrypt race notes: %w", err)
	}
	return &r, nil
}

// FindMatchingRaces returns all races that match the given date (±1 day) and
// distance (within 20%). Unlike FindMatchingRace which returns at most one,
// this returns all candidates so the caller can detect ambiguous matches.
func FindMatchingRaces(db *sql.DB, userID int64, date string, distanceMeters float64) ([]Race, error) {
	parsedDate, err := time.Parse("2006-01-02", date)
	if err != nil {
		return nil, fmt.Errorf("parse date %q: %w", date, err)
	}
	dayBefore := parsedDate.AddDate(0, 0, -1).Format("2006-01-02")
	dayAfter := parsedDate.AddDate(0, 0, 1).Format("2006-01-02")

	minDist := distanceMeters * 0.8
	maxDist := distanceMeters * 1.2

	rows, err := db.Query(`
		SELECT id, user_id, name, date, distance_m, target_time, priority, notes, result_time, created_at
		FROM stride_races
		WHERE user_id = ?
		  AND date >= ? AND date <= ?
		  AND distance_m >= ? AND distance_m <= ?
		ORDER BY ABS(julianday(date) - julianday(?)) ASC
	`, userID, dayBefore, dayAfter, minDist, maxDist, date)
	if err != nil {
		return nil, fmt.Errorf("query matching races: %w", err)
	}
	defer rows.Close()

	races := []Race{}
	for rows.Next() {
		var r Race
		if err := rows.Scan(&r.ID, &r.UserID, &r.Name, &r.Date, &r.DistanceM,
			&r.TargetTime, &r.Priority, &r.Notes, &r.ResultTime, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan race row: %w", err)
		}
		if r.Name, err = encryption.DecryptField(r.Name); err != nil {
			return nil, fmt.Errorf("decrypt race name: %w", err)
		}
		if r.Notes, err = encryption.DecryptField(r.Notes); err != nil {
			return nil, fmt.Errorf("decrypt race notes: %w", err)
		}
		races = append(races, r)
	}
	return races, rows.Err()
}

// DeleteRace removes a race owned by the given user.
func DeleteRace(db *sql.DB, id, userID int64) error {
	res, err := db.Exec("DELETE FROM stride_races WHERE id = ? AND user_id = ?", id, userID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ListNotes returns notes for a user, optionally filtered by plan_id and status.
// When planID is nil, notes are not filtered by plan. The status parameter controls
// consumption filtering: "active" returns only unconsumed notes (consumed_at IS NULL),
// "consumed" returns only consumed notes, and "all" (or empty string) returns everything.
func ListNotes(db *sql.DB, userID int64, planID *int64, status string) ([]Note, error) {
	query := `
		SELECT id, user_id, plan_id, content, target_date, consumed_at, consumed_by, created_at
		FROM stride_notes
		WHERE user_id = ?`
	args := []any{userID}

	if planID != nil {
		query += ` AND plan_id = ?`
		args = append(args, *planID)
	}

	switch status {
	case "active":
		query += ` AND consumed_at IS NULL`
	case "consumed":
		query += ` AND consumed_at IS NOT NULL`
	}

	query += ` ORDER BY created_at DESC`

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var notes []Note
	for rows.Next() {
		var n Note
		if err := scanNote(rows, &n); err != nil {
			return nil, err
		}
		notes = append(notes, n)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if notes == nil {
		notes = []Note{}
	}
	return notes, nil
}

// CreateNote inserts a new note.
func CreateNote(db *sql.DB, userID int64, planID *int64, content, targetDate string) (*Note, error) {
	now := time.Now().UTC().Format(time.RFC3339)

	if targetDate == "" {
		targetDate = now[:10]
	}

	encContent, err := encryption.EncryptField(content)
	if err != nil {
		return nil, fmt.Errorf("encrypt note content: %w", err)
	}

	res, err := db.Exec(`
		INSERT INTO stride_notes (user_id, plan_id, content, target_date, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, userID, planID, encContent, targetDate, now)
	if err != nil {
		return nil, fmt.Errorf("insert note: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("last insert id: %w", err)
	}

	return getNoteByID(db, id, userID)
}

// getNoteByID returns a single note by ID, scoped to the given user.
func getNoteByID(db *sql.DB, id, userID int64) (*Note, error) {
	var n Note
	row := db.QueryRow(`
		SELECT id, user_id, plan_id, content, target_date, consumed_at, consumed_by, created_at
		FROM stride_notes
		WHERE id = ? AND user_id = ?
	`, id, userID)
	if err := scanNote(row, &n); err != nil {
		return nil, err
	}
	return &n, nil
}

// DeleteNote removes a note owned by the given user.
func DeleteNote(db *sql.DB, id, userID int64) error {
	res, err := db.Exec("DELETE FROM stride_notes WHERE id = ? AND user_id = ?", id, userID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// MarkNotesConsumed sets consumed_at and consumed_by for the given note IDs
// within the provided transaction. This marks notes as having been processed
// by a consuming process (e.g. weekly plan generation, nightly evaluation).
func MarkNotesConsumed(ctx context.Context, tx *sql.Tx, noteIDs []int64, consumedBy string) error {
	if len(noteIDs) == 0 {
		return nil
	}

	now := time.Now().UTC().Format(time.RFC3339)

	placeholders := make([]string, len(noteIDs))
	args := make([]any, 0, len(noteIDs)+2)
	args = append(args, now, consumedBy)
	for i, id := range noteIDs {
		placeholders[i] = "?"
		args = append(args, id)
	}

	query := `UPDATE stride_notes SET consumed_at = ?, consumed_by = ? WHERE id IN (` + strings.Join(placeholders, ",") + `)`
	_, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("mark notes consumed: %w", err)
	}
	return nil
}

// GetNotesByTargetDate returns all notes for a user targeting a specific date.
func GetNotesByTargetDate(db *sql.DB, userID int64, date string) ([]Note, error) {
	rows, err := db.Query(`
		SELECT id, user_id, plan_id, content, target_date, consumed_at, consumed_by, created_at
		FROM stride_notes
		WHERE user_id = ? AND target_date = ?
		ORDER BY created_at ASC
	`, userID, date)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var notes []Note
	for rows.Next() {
		var n Note
		if err := scanNote(rows, &n); err != nil {
			return nil, err
		}
		notes = append(notes, n)
	}
	return notes, rows.Err()
}

// Plan represents a generated weekly training plan from stride_plans.
// plan_json is returned as structured JSON ([]DayPlan); prompt and response
// are encrypted-at-rest and are not included in API responses.
type Plan struct {
	ID        int64           `json:"id"`
	UserID    int64           `json:"user_id"`
	WeekStart string          `json:"week_start"`
	WeekEnd   string          `json:"week_end"`
	Phase     string          `json:"phase"`
	Plan      json.RawMessage `json:"plan"` // decoded from plan_json column
	Model     string          `json:"model"`
	CreatedAt string          `json:"created_at"`
}

// scanPlan reads a plan row from a sql.Scanner (row or rows.Next).
func scanPlan(scanner interface {
	Scan(...any) error
}) (Plan, error) {
	var p Plan
	var planJSON string
	if err := scanner.Scan(&p.ID, &p.UserID, &p.WeekStart, &p.WeekEnd, &p.Phase, &planJSON, &p.Model, &p.CreatedAt); err != nil {
		return Plan{}, err
	}
	p.Plan = json.RawMessage(planJSON)
	return p, nil
}

// ListPlans returns paginated plans for a user, ordered by week_start descending.
// Returns the plans slice, the total count, and any error.
func ListPlans(db *sql.DB, userID int64, limit, offset int) ([]Plan, int, error) {
	var total int
	if err := db.QueryRow("SELECT COUNT(*) FROM stride_plans WHERE user_id = ?", userID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count plans: %w", err)
	}

	rows, err := db.Query(`
		SELECT id, user_id, week_start, week_end, phase, plan_json, model, created_at
		FROM stride_plans
		WHERE user_id = ?
		ORDER BY week_start DESC
		LIMIT ? OFFSET ?
	`, userID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("query plans: %w", err)
	}
	defer rows.Close()

	var plans []Plan
	for rows.Next() {
		p, err := scanPlan(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("scan plan: %w", err)
		}
		plans = append(plans, p)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if plans == nil {
		plans = []Plan{}
	}
	return plans, total, nil
}

// GetPlanByID returns a single plan by ID, scoped to the given user.
func GetPlanByID(db *sql.DB, id, userID int64) (*Plan, error) {
	row := db.QueryRow(`
		SELECT id, user_id, week_start, week_end, phase, plan_json, model, created_at
		FROM stride_plans
		WHERE id = ? AND user_id = ?
	`, id, userID)
	p, err := scanPlan(row)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// GetCurrentPlan returns the plan whose week contains today's date, or nil if none.
func GetCurrentPlan(db *sql.DB, userID int64, today string) (*Plan, error) {
	row := db.QueryRow(`
		SELECT id, user_id, week_start, week_end, phase, plan_json, model, created_at
		FROM stride_plans
		WHERE user_id = ? AND week_start <= ? AND week_end >= ?
		ORDER BY week_start DESC
		LIMIT 1
	`, userID, today, today)
	p, err := scanPlan(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// EvaluationRecord represents a stored evaluation from stride_evaluations.
type EvaluationRecord struct {
	ID        int64      `json:"id"`
	UserID    int64      `json:"user_id"`
	PlanID    int64      `json:"plan_id"`
	WorkoutID *int64     `json:"workout_id"`
	Eval      Evaluation `json:"eval"`
	CreatedAt string     `json:"created_at"`
}

// ListEvaluations returns evaluation records for a user from stride_evaluations.
// If planID is non-nil, results are filtered to that plan.
// If workoutID is non-nil, results are filtered to that workout.
// Records are ordered by created_at DESC.
func ListEvaluations(db *sql.DB, userID int64, planID *int64, workoutID *int64) ([]EvaluationRecord, error) {
	var (
		rows *sql.Rows
		err  error
	)
	switch {
	case planID != nil:
		rows, err = db.Query(`
			SELECT id, user_id, plan_id, workout_id, eval_json, created_at
			FROM stride_evaluations
			WHERE user_id = ? AND plan_id = ?
			ORDER BY created_at DESC
		`, userID, *planID)
	case workoutID != nil:
		rows, err = db.Query(`
			SELECT id, user_id, plan_id, workout_id, eval_json, created_at
			FROM stride_evaluations
			WHERE user_id = ? AND workout_id = ?
			ORDER BY created_at DESC
		`, userID, *workoutID)
	default:
		rows, err = db.Query(`
			SELECT id, user_id, plan_id, workout_id, eval_json, created_at
			FROM stride_evaluations
			WHERE user_id = ?
			ORDER BY created_at DESC
		`, userID)
	}
	if err != nil {
		return nil, fmt.Errorf("query evaluations: %w", err)
	}
	defer rows.Close()

	var records []EvaluationRecord
	for rows.Next() {
		var rec EvaluationRecord
		var encEvalJSON string
		var workoutID sql.NullInt64
		if err := rows.Scan(&rec.ID, &rec.UserID, &rec.PlanID, &workoutID, &encEvalJSON, &rec.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan evaluation: %w", err)
		}
		if workoutID.Valid {
			rec.WorkoutID = &workoutID.Int64
		}
		decJSON, err := encryption.DecryptField(encEvalJSON)
		if err != nil {
			return nil, fmt.Errorf("decrypt eval_json for record %d: %w", rec.ID, err)
		}
		if err := json.Unmarshal([]byte(decJSON), &rec.Eval); err != nil {
			return nil, fmt.Errorf("unmarshal eval for record %d: %w", rec.ID, err)
		}
		records = append(records, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if records == nil {
		records = []EvaluationRecord{}
	}
	return records, nil
}

// WeekSummary holds completion statistics for a single historical training week.
type WeekSummary struct {
	PlanID            int64   `json:"plan_id"`
	WeekStart         string  `json:"week_start"`
	WeekEnd           string  `json:"week_end"`
	Phase             string  `json:"phase"`
	SessionsPlanned   int     `json:"sessions_planned"`
	SessionsCompleted int     `json:"sessions_completed"`
	CompletionRate    float64 `json:"completion_rate"`
}

// MonthSummary aggregates completion data across all weeks in a calendar month.
type MonthSummary struct {
	Month             string  `json:"month"` // YYYY-MM
	SessionsPlanned   int     `json:"sessions_planned"`
	SessionsCompleted int     `json:"sessions_completed"`
	ComplianceRate    float64 `json:"compliance_rate"`
}

// GetPlanHistory returns past weeks' plans with per-week completion metadata
// and per-month compliance rollups. Plans for the current week are excluded.
// limit caps the number of weeks returned (most recent first).
func GetPlanHistory(db *sql.DB, userID int64, limit int) ([]WeekSummary, []MonthSummary, error) {
	// Compute the Monday of the current week so we exclude the current plan
	// even on Sunday (when week_end == today). Plans whose week_end falls on
	// or after this Monday are still active or in the current week.
	now := time.Now().UTC()
	weekday := int(now.Weekday())
	if weekday == 0 {
		weekday = 7 // Sunday → 7, so Monday offset is correct
	}
	currentWeekStart := now.AddDate(0, 0, -(weekday - 1)).Format("2006-01-02")

	rows, err := db.Query(`
		SELECT id, week_start, week_end, phase, plan_json
		FROM stride_plans
		WHERE user_id = ? AND week_end < ?
		ORDER BY week_start DESC
		LIMIT ?
	`, userID, currentWeekStart, limit)
	if err != nil {
		return nil, nil, fmt.Errorf("query plan history: %w", err)
	}
	defer rows.Close()

	type planRow struct {
		id        int64
		weekStart string
		weekEnd   string
		phase     string
		planJSON  string
	}
	var planRows []planRow
	for rows.Next() {
		var pr planRow
		if err := rows.Scan(&pr.id, &pr.weekStart, &pr.weekEnd, &pr.phase, &pr.planJSON); err != nil {
			return nil, nil, fmt.Errorf("scan plan row: %w", err)
		}
		planRows = append(planRows, pr)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	// Collect all plan IDs for a single evaluations query.
	if len(planRows) == 0 {
		return []WeekSummary{}, []MonthSummary{}, nil
	}

	planIDs := make([]int64, len(planRows))
	for i, pr := range planRows {
		planIDs[i] = pr.id
	}

	// Load all evaluations for these plans in one query.
	completedByPlan := make(map[int64]int, len(planRows))
	for _, id := range planIDs {
		completedByPlan[id] = 0
	}

	// Build IN clause for the query.
	inClause := make([]any, len(planIDs)+1)
	inClause[0] = userID
	placeholders := make([]byte, 0, len(planIDs)*2)
	for i, id := range planIDs {
		inClause[i+1] = id
		if i > 0 {
			placeholders = append(placeholders, ',')
		}
		placeholders = append(placeholders, '?')
	}
	evalRows, err := db.Query(
		`SELECT plan_id, workout_id, eval_json FROM stride_evaluations WHERE user_id = ? AND plan_id IN (`+string(placeholders)+`)`,
		inClause...,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("query evaluations for history: %w", err)
	}
	defer evalRows.Close()

	// Track seen workout IDs per plan to de-duplicate multiple evaluations for the same workout.
	seenWorkout := make(map[int64]map[int64]bool)

	for evalRows.Next() {
		var planID int64
		var workoutID sql.NullInt64
		var encJSON string
		if err := evalRows.Scan(&planID, &workoutID, &encJSON); err != nil {
			return nil, nil, fmt.Errorf("scan evaluation row: %w", err)
		}
		// Skip duplicate evaluations for the same workout within a plan.
		if workoutID.Valid {
			if seenWorkout[planID] == nil {
				seenWorkout[planID] = make(map[int64]bool)
			}
			if seenWorkout[planID][workoutID.Int64] {
				continue
			}
			seenWorkout[planID][workoutID.Int64] = true
		}
		decJSON, err := encryption.DecryptField(encJSON)
		if err != nil {
			return nil, nil, fmt.Errorf("decrypt eval_json: %w", err)
		}
		var eval Evaluation
		if err := json.Unmarshal([]byte(decJSON), &eval); err != nil {
			return nil, nil, fmt.Errorf("unmarshal eval: %w", err)
		}
		// Only count planned sessions completed as compliant or partial; exclude bonus workouts.
		if (eval.Compliance == "compliant" || eval.Compliance == "partial") && eval.PlannedType != "none" {
			completedByPlan[planID]++
		}
	}
	if err := evalRows.Err(); err != nil {
		return nil, nil, err
	}

	weeks := make([]WeekSummary, 0, len(planRows))
	for _, pr := range planRows {
		var days []DayPlan
		if err := json.Unmarshal([]byte(pr.planJSON), &days); err != nil {
			return nil, nil, fmt.Errorf("decode stride plan_json for plan %d: %w", pr.id, err)
		}
		sessionsPlanned := 0
		for _, d := range days {
			if !d.RestDay && d.Session != nil {
				sessionsPlanned++
			}
		}
		completed := completedByPlan[pr.id]
		rate := 0.0
		if sessionsPlanned > 0 {
			rate = float64(completed) / float64(sessionsPlanned) * 100
		}
		weeks = append(weeks, WeekSummary{
			PlanID:            pr.id,
			WeekStart:         pr.weekStart,
			WeekEnd:           pr.weekEnd,
			Phase:             pr.phase,
			SessionsPlanned:   sessionsPlanned,
			SessionsCompleted: completed,
			CompletionRate:    rate,
		})
	}

	// Build monthly rollups (weeks are newest-first, so iterate to group by month).
	monthOrder := []string{}
	monthMap := map[string]*MonthSummary{}
	for _, w := range weeks {
		if len(w.WeekStart) < 7 {
			continue
		}
		month := w.WeekStart[:7]
		if _, ok := monthMap[month]; !ok {
			monthOrder = append(monthOrder, month)
			monthMap[month] = &MonthSummary{Month: month}
		}
		m := monthMap[month]
		m.SessionsPlanned += w.SessionsPlanned
		m.SessionsCompleted += w.SessionsCompleted
	}
	months := make([]MonthSummary, 0, len(monthOrder))
	for _, month := range monthOrder {
		m := monthMap[month]
		if m.SessionsPlanned > 0 {
			m.ComplianceRate = float64(m.SessionsCompleted) / float64(m.SessionsPlanned) * 100
		}
		months = append(months, *m)
	}

	return weeks, months, nil
}

// getPlanByWeekStart returns the plan for a specific week_start, scoped to the user.
func getPlanByWeekStart(db *sql.DB, userID int64, weekStart string) (*Plan, error) {
	row := db.QueryRow(`
		SELECT id, user_id, week_start, week_end, phase, plan_json, model, created_at
		FROM stride_plans
		WHERE user_id = ? AND week_start = ?
	`, userID, weekStart)
	p, err := scanPlan(row)
	if err != nil {
		return nil, err
	}
	return &p, nil
}
