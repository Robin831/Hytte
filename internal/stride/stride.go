package stride

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
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
	ID        int64  `json:"id"`
	UserID    int64  `json:"user_id"`
	PlanID    *int64 `json:"plan_id"` // nullable — linked to plan when created during a plan week
	Content   string `json:"content"` // encrypted at rest
	CreatedAt string `json:"created_at"`
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

// ListNotes returns notes for a user, optionally filtered by plan_id.
// When planID is nil, all notes for the user are returned.
func ListNotes(db *sql.DB, userID int64, planID *int64) ([]Note, error) {
	query := `
		SELECT id, user_id, plan_id, content, created_at
		FROM stride_notes
		WHERE user_id = ?`
	args := []any{userID}

	if planID != nil {
		query += ` AND plan_id = ?`
		args = append(args, *planID)
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
		if err := rows.Scan(&n.ID, &n.UserID, &n.PlanID, &n.Content, &n.CreatedAt); err != nil {
			return nil, err
		}
		if n.Content, err = encryption.DecryptField(n.Content); err != nil {
			return nil, fmt.Errorf("decrypt note content: %w", err)
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
func CreateNote(db *sql.DB, userID int64, planID *int64, content string) (*Note, error) {
	now := time.Now().UTC().Format(time.RFC3339)

	encContent, err := encryption.EncryptField(content)
	if err != nil {
		return nil, fmt.Errorf("encrypt note content: %w", err)
	}

	res, err := db.Exec(`
		INSERT INTO stride_notes (user_id, plan_id, content, created_at)
		VALUES (?, ?, ?, ?)
	`, userID, planID, encContent, now)
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
	err := db.QueryRow(`
		SELECT id, user_id, plan_id, content, created_at
		FROM stride_notes
		WHERE id = ? AND user_id = ?
	`, id, userID).Scan(&n.ID, &n.UserID, &n.PlanID, &n.Content, &n.CreatedAt)
	if err != nil {
		return nil, err
	}
	if n.Content, err = encryption.DecryptField(n.Content); err != nil {
		return nil, fmt.Errorf("decrypt note content: %w", err)
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
// If planID is non-nil, results are filtered to that plan. Records are ordered by created_at DESC.
func ListEvaluations(db *sql.DB, userID int64, planID *int64) ([]EvaluationRecord, error) {
	var (
		rows *sql.Rows
		err  error
	)
	if planID != nil {
		rows, err = db.Query(`
			SELECT id, user_id, plan_id, workout_id, eval_json, created_at
			FROM stride_evaluations
			WHERE user_id = ? AND plan_id = ?
			ORDER BY created_at DESC
		`, userID, *planID)
	} else {
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
		decJSON, decErr := encryption.DecryptField(encEvalJSON)
		if decErr != nil {
			// Legacy plaintext fallback — value may be unencrypted; use as-is.
			log.Printf("stride: decrypt eval_json for record %d: %v; treating as legacy plaintext", rec.ID, decErr)
			decJSON = encEvalJSON
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
