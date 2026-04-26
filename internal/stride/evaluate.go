package stride

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/encryption"
	"github.com/Robin831/Hytte/internal/push"
	"github.com/Robin831/Hytte/internal/training"
)

// ErrNoStridePlan is returned by re-evaluation operations when no stride plan
// covers the requested date. Handlers can match on this to return 404 without
// string-matching the error message.
var ErrNoStridePlan = errors.New("no stride plan covers this date")

// Evaluation note templates for rest days and missed sessions.
const (
	noteRestDay        = "Rest day taken as planned \u2014 good recovery."
	noteMissedSession  = "Planned %s was not completed. Consider adjusting the remaining week if needed."
)

// criticalFlags is the set of evaluation flag values that warrant an immediate push notification.
var criticalFlags = map[string]bool{
	"overtraining": true,
	"injury_risk":  true,
	"hr_too_high":  true,
}

// Evaluation holds the AI-generated assessment of a completed workout against its planned session.
type Evaluation struct {
	PlannedType string   `json:"planned_type"` // session type that was planned (e.g. "threshold", "easy", "long_run", "none")
	ActualType  string   `json:"actual_type"`  // session type that was performed
	Compliance  string   `json:"compliance"`   // "compliant", "partial", "missed", "bonus", or "rest_day"
	Notes       string   `json:"notes"`        // narrative assessment
	Flags       []string `json:"flags"`        // warning flags, e.g. "hr_too_high", "overtraining"
	Adjustments string   `json:"adjustments"`  // suggested adjustments to upcoming training
	Date        string   `json:"date,omitempty"` // date (YYYY-MM-DD) for rest_day/missed evaluations without a workout
}

// NextNightlyEvaluationRun returns the next time the nightly evaluation cron should fire
// (daily at 03:00 in the given location). If today's target time is still in the future,
// it returns today's run; otherwise it returns the next day's run.
func NextNightlyEvaluationRun(now time.Time, loc *time.Location) time.Time {
	if loc == nil {
		loc = time.UTC
	}
	now = now.In(loc)
	todayRun := time.Date(now.Year(), now.Month(), now.Day(), 3, 0, 0, 0, loc)
	if now.Before(todayRun) {
		return todayRun
	}
	return todayRun.AddDate(0, 0, 1)
}

// EvaluateWorkout calls Claude to assess how well a completed workout matched its planned session.
// matchedSession may be nil for bonus (unplanned) workouts or when no plan exists.
// plan is used for weekly context; an empty Plan (ID == 0) is acceptable.
// profile carries the athlete's HR zones and training context.
// notes are unconsumed user notes providing context about sickness, fatigue, etc.
func EvaluateWorkout(
	ctx context.Context,
	cfg *training.ClaudeConfig,
	workout training.Workout,
	matchedSession *PlannedSession,
	plan Plan,
	profile training.UserTrainingProfile,
	notes []Note,
) (*Evaluation, error) {
	prompt := buildEvalPrompt(workout, matchedSession, plan, profile, notes)

	response, err := runPromptFunc(ctx, cfg, prompt)
	if err != nil {
		return nil, fmt.Errorf("claude prompt: %w", err)
	}

	eval, err := parseEvalResponse(response)
	if err != nil {
		return nil, fmt.Errorf("parse eval response: %w", err)
	}

	return eval, nil
}

// RunNightlyEvaluation queries all users with stride enabled, finds workouts from the
// past day that have not yet been evaluated, evaluates each one using Claude, stores the
// result, and sends push notifications for any critical flags.
func RunNightlyEvaluation(ctx context.Context, db *sql.DB, httpClient *http.Client) error {
	rows, err := db.QueryContext(ctx,
		`SELECT DISTINCT user_id FROM user_preferences WHERE key='stride_enabled' AND value='true'`)
	if err != nil {
		return fmt.Errorf("query stride users: %w", err)
	}
	var userIDs []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			log.Printf("stride eval: scan user id: %v", err)
			continue
		}
		userIDs = append(userIDs, id)
	}
	if err := rows.Err(); err != nil {
		log.Printf("stride eval: rows error: %v", err)
	}
	rows.Close()

	// Evaluate yesterday using explicit UTC day boundaries. The nightly job runs
	// at 03:00, but the evaluation target is the full UTC day that just ended,
	// not the last 24 hours from the current time.
	todayStart := time.Now().UTC().Truncate(24 * time.Hour)
	yesterdayStart := todayStart.AddDate(0, 0, -1)
	since := yesterdayStart.Format(time.RFC3339)
	targetDate := yesterdayStart.Format("2006-01-02")

	for _, userID := range userIDs {
		if err := evaluateUserWorkouts(ctx, db, httpClient, userID, since, targetDate); err != nil {
			log.Printf("stride eval: user %d: %v", userID, err)
		}
	}
	return nil
}

// RunUserEvaluation evaluates unevaluated workouts for a single user from the past 24 hours.
// It returns the number of workouts successfully evaluated, and any fatal error.
func RunUserEvaluation(ctx context.Context, db *sql.DB, httpClient *http.Client, userID int64) (int, error) {
	since := time.Now().UTC().Add(-24 * time.Hour).Format(time.RFC3339)

	claudeCfg, err := training.LoadClaudeConfig(db, userID)
	if err != nil {
		return 0, fmt.Errorf("load claude config: %w", err)
	}
	if !claudeCfg.Enabled {
		return 0, training.ErrClaudeNotEnabled
	}

	workouts, err := queryUnevaluatedWorkouts(ctx, db, userID, since)
	if err != nil {
		return 0, fmt.Errorf("query unevaluated workouts: %w", err)
	}

	profile := training.BuildUserTrainingProfile(db, userID)
	evaluated := 0
	for _, workout := range workouts {
		evalCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
		if err := evaluateSingleWorkout(evalCtx, db, httpClient, userID, workout, claudeCfg, profile, nil); err != nil {
			log.Printf("stride eval: workout %d for user %d: %v", workout.ID, userID, err)
		} else {
			evaluated++
		}
		cancel()
	}
	return evaluated, nil
}

// evaluateUserWorkouts processes all unevaluated workouts for a single user since the given timestamp,
// then checks for rest days and missed sessions on the target date (yesterday for nightly runs).
func evaluateUserWorkouts(ctx context.Context, db *sql.DB, httpClient *http.Client, userID int64, since string, targetDate string) error {
	claudeCfg, err := training.LoadClaudeConfig(db, userID)
	if err != nil {
		return fmt.Errorf("load claude config: %w", err)
	}
	if !claudeCfg.Enabled {
		return nil
	}

	workouts, err := queryUnevaluatedWorkouts(ctx, db, userID, since)
	if err != nil {
		return fmt.Errorf("query unevaluated workouts: %w", err)
	}

	// Fetch unconsumed notes routed to the nightly evaluation (or 'any').
	userNotes, err := ListNotes(db, userID, nil, "active", NoteScopeNightly)
	if err != nil {
		log.Printf("stride eval: fetch notes for user %d: %v", userID, err)
		// Non-fatal — continue without notes.
	}

	if len(workouts) > 0 {
		profile := training.BuildUserTrainingProfile(db, userID)

		anySuccess := false
		for _, workout := range workouts {
			evalCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
			if err := evaluateSingleWorkout(evalCtx, db, httpClient, userID, workout, claudeCfg, profile, userNotes); err != nil {
				log.Printf("stride eval: workout %d for user %d: %v", workout.ID, userID, err)
			} else {
				anySuccess = true
			}
			cancel()
		}

		// Mark notes consumed once per user per nightly run, only when notes were
		// actually sent to Claude (workouts existed and at least one succeeded).
		if anySuccess && len(userNotes) > 0 {
			noteIDs := make([]int64, len(userNotes))
			for i, n := range userNotes {
				noteIDs[i] = n.ID
			}
			tx, err := db.BeginTx(ctx, nil)
			if err != nil {
				log.Printf("stride eval: begin tx to mark notes consumed for user %d: %v", userID, err)
			} else {
				defer tx.Rollback()
				if err := MarkNotesConsumed(ctx, tx, userID, noteIDs, "nightly"); err != nil {
					log.Printf("stride eval: mark notes consumed for user %d: %v", userID, err)
				} else if err := tx.Commit(); err != nil {
					log.Printf("stride eval: commit notes consumed for user %d: %v", userID, err)
				}
			}
		}
	}

	// Check for rest days and missed sessions on the target date.
	if err := evaluateRestDaysAndMissedSessions(ctx, db, claudeCfg, userID, targetDate); err != nil {
		log.Printf("stride eval: rest/missed check for user %d: %v", userID, err)
	}

	return nil
}

// queryWorkoutsOnDate returns all workouts for a user that started on the given
// YYYY-MM-DD date. Workout titles are decrypted.
func queryWorkoutsOnDate(ctx context.Context, db *sql.DB, userID int64, date string) ([]training.Workout, error) {
	t, err := time.Parse("2006-01-02", date)
	if err != nil {
		return nil, fmt.Errorf("parse date %q: %w", date, err)
	}
	dayStart := date + "T00:00:00Z"
	dayEnd := t.AddDate(0, 0, 1).Format("2006-01-02") + "T00:00:00Z"

	rows, err := db.QueryContext(ctx, `
		SELECT w.id, w.user_id, w.sport, w.sub_sport, w.is_indoor, w.title, w.started_at,
		       w.duration_seconds, w.distance_meters, w.avg_heart_rate, w.max_heart_rate,
		       w.avg_pace_sec_per_km, w.avg_cadence, w.calories,
		       w.ascent_meters, w.descent_meters, w.fit_file_hash, w.analysis_status, w.title_source,
		       w.created_at, w.training_load, w.hr_drift_pct, w.pace_cv_pct
		FROM workouts w
		WHERE w.user_id = ?
		  AND w.started_at >= ?
		  AND w.started_at < ?
		ORDER BY w.started_at ASC
	`, userID, dayStart, dayEnd)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var workouts []training.Workout
	for rows.Next() {
		var w training.Workout
		var isIndoor int
		var trainingLoad, hrDriftPct, paceCVPct sql.NullFloat64
		if err := rows.Scan(
			&w.ID, &w.UserID, &w.Sport, &w.SubSport, &isIndoor, &w.Title, &w.StartedAt,
			&w.DurationSeconds, &w.DistanceMeters, &w.AvgHeartRate, &w.MaxHeartRate,
			&w.AvgPaceSecPerKm, &w.AvgCadence, &w.Calories,
			&w.AscentMeters, &w.DescentMeters, &w.FitFileHash, &w.AnalysisStatus, &w.TitleSource,
			&w.CreatedAt, &trainingLoad, &hrDriftPct, &paceCVPct,
		); err != nil {
			log.Printf("stride eval: scan workout: %v", err)
			continue
		}
		w.IsIndoor = isIndoor != 0
		if trainingLoad.Valid {
			w.TrainingLoad = &trainingLoad.Float64
		}
		if hrDriftPct.Valid {
			w.HRDriftPct = &hrDriftPct.Float64
		}
		if paceCVPct.Valid {
			w.PaceCVPct = &paceCVPct.Float64
		}
		if decTitle, decErr := encryption.DecryptField(w.Title); decErr != nil {
			log.Printf("stride eval: workout %d: failed to decrypt title: %v; omitting from prompt", w.ID, decErr)
			w.Title = ""
		} else {
			w.Title = decTitle
		}
		workouts = append(workouts, w)
	}
	return workouts, rows.Err()
}

// reEvalRecord is a Claude-produced evaluation held in memory by ReEvaluateDate
// before any DB mutation. workoutID == nil indicates a date-only (rest day or
// missed session) evaluation.
type reEvalRecord struct {
	workoutID *int64
	eval      *Evaluation
}

// ReEvaluateDate re-runs the coach evaluation for a single date for one user.
// It first calls Claude to produce all new evaluations in memory, then atomically
// swaps them in (delete existing + insert new + mark notes consumed) inside a
// single transaction. If Claude fails for any workout, no DB rows are touched
// so the prior coach output is preserved. Active notes used during the re-run
// are marked consumed_by="manual" only when the new evaluations actually persist.
// Returns the number of new evaluations produced and ErrNoStridePlan when no
// plan covers the date.
func ReEvaluateDate(ctx context.Context, db *sql.DB, httpClient *http.Client, userID int64, date string) (int, error) {
	if _, err := time.Parse("2006-01-02", date); err != nil {
		return 0, fmt.Errorf("parse date %q: %w", date, err)
	}

	claudeCfg, err := training.LoadClaudeConfig(db, userID)
	if err != nil {
		return 0, fmt.Errorf("load claude config: %w", err)
	}
	if !claudeCfg.Enabled {
		return 0, training.ErrClaudeNotEnabled
	}

	plan, err := getPlanContainingDate(ctx, db, userID, date)
	if err != nil {
		return 0, fmt.Errorf("find plan for date %s: %w", date, err)
	}
	if plan == nil {
		return 0, ErrNoStridePlan
	}

	workouts, err := queryWorkoutsOnDate(ctx, db, userID, date)
	if err != nil {
		return 0, fmt.Errorf("query workouts for date %s: %w", date, err)
	}

	// Gather active notes targeting this date so the re-run picks up any
	// correcting context the user added after the original evaluation.
	allNotes, err := GetNotesByTargetDate(db, userID, date)
	if err != nil {
		log.Printf("stride eval: fetch notes for user %d date %s: %v", userID, date, err)
	}
	notes := make([]Note, 0, len(allNotes))
	for _, n := range allNotes {
		if n.ConsumedAt == nil {
			notes = append(notes, n)
		}
	}

	// Phase 1: build all new evaluations in memory by calling Claude. If anything
	// fails we return without touching the existing rows, so the prior coach
	// output for this date stays intact.
	profile := training.BuildUserTrainingProfile(db, userID)
	sessions := extractPlannedSessions(*plan)
	var newRecords []reEvalRecord
	for _, workout := range workouts {
		matchedSession := MatchWorkoutToSession(workout, sessions)
		evalCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
		eval, err := EvaluateWorkout(evalCtx, claudeCfg, workout, matchedSession, *plan, profile, notes)
		cancel()
		if err != nil {
			return 0, fmt.Errorf("evaluate workout %d: %w", workout.ID, err)
		}
		wid := workout.ID
		newRecords = append(newRecords, reEvalRecord{workoutID: &wid, eval: eval})
	}

	if len(workouts) == 0 {
		evalCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
		eval, err := buildDateEval(evalCtx, claudeCfg, *plan, date, notes)
		cancel()
		if err != nil {
			return 0, fmt.Errorf("evaluate rest/missed for %s: %w", date, err)
		}
		if eval != nil {
			newRecords = append(newRecords, reEvalRecord{workoutID: nil, eval: eval})
		}
	}

	if len(newRecords) == 0 {
		return 0, nil
	}

	// Phase 2: encrypt new payloads up-front so any encryption error fails before
	// we delete anything.
	type insertPayload struct {
		workoutID *int64
		encJSON   string
	}
	inserts := make([]insertPayload, 0, len(newRecords))
	for _, rec := range newRecords {
		evalBytes, err := json.Marshal(rec.eval)
		if err != nil {
			return 0, fmt.Errorf("marshal eval: %w", err)
		}
		encEval, err := encryption.EncryptField(string(evalBytes))
		if err != nil {
			return 0, fmt.Errorf("encrypt eval: %w", err)
		}
		inserts = append(inserts, insertPayload{workoutID: rec.workoutID, encJSON: encEval})
	}

	// Phase 3: atomically swap in the new records in a single transaction.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if len(workouts) > 0 {
		args := make([]any, 0, len(workouts)+1)
		args = append(args, userID)
		placeholders := make([]string, len(workouts))
		for i, w := range workouts {
			placeholders[i] = "?"
			args = append(args, w.ID)
		}
		q := `DELETE FROM stride_evaluations WHERE user_id = ? AND workout_id IN (` + strings.Join(placeholders, ",") + `)`
		if _, err := tx.ExecContext(ctx, q, args...); err != nil {
			return 0, fmt.Errorf("delete existing workout evaluations: %w", err)
		}
	}
	if err := deleteDateEvaluationsForPlanTx(ctx, tx, userID, plan.ID, date); err != nil {
		return 0, fmt.Errorf("delete existing date evaluations: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	for _, ins := range inserts {
		if ins.workoutID != nil {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO stride_evaluations (user_id, plan_id, workout_id, eval_json, created_at)
				VALUES (?, ?, ?, ?, ?)
			`, userID, plan.ID, *ins.workoutID, ins.encJSON, now); err != nil {
				return 0, fmt.Errorf("insert eval: %w", err)
			}
		} else {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO stride_evaluations (user_id, plan_id, workout_id, eval_json, created_at)
				VALUES (?, ?, NULL, ?, ?)
			`, userID, plan.ID, ins.encJSON, now); err != nil {
				return 0, fmt.Errorf("insert date eval: %w", err)
			}
		}
	}

	if len(notes) > 0 {
		noteIDs := make([]int64, len(notes))
		for i, n := range notes {
			noteIDs[i] = n.ID
		}
		if err := MarkNotesConsumed(ctx, tx, userID, noteIDs, "manual"); err != nil {
			return 0, fmt.Errorf("mark notes consumed: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}
	committed = true

	// Push notifications fire only after commit, so a flag-laden evaluation that
	// failed to persist does not page the user.
	for _, rec := range newRecords {
		if rec.workoutID == nil || !hasCriticalFlag(rec.eval.Flags) {
			continue
		}
		notif := push.Notification{
			Title: "Stride Alert",
			Body:  buildCriticalNotifBody(rec.eval),
			Tag:   "stride-eval-alert",
		}
		payload, err := json.Marshal(notif)
		if err != nil {
			log.Printf("stride eval: marshal notification for user %d: %v", userID, err)
			continue
		}
		if _, err := push.SendToUser(db, httpClient, userID, payload); err != nil {
			log.Printf("stride eval: push notification for user %d: %v", userID, err)
		}
	}

	return len(newRecords), nil
}

// buildDateEval constructs an Evaluation in memory for a date with no workout.
// Returns nil, nil when the date has neither a planned session nor a rest day
// (nothing to evaluate). When the user left notes for the date and Claude is
// available, it asks Claude for a contextual evaluation; otherwise it returns
// the static template used by the nightly job.
func buildDateEval(ctx context.Context, claudeCfg *training.ClaudeConfig, plan Plan, date string, notes []Note) (*Evaluation, error) {
	session, isRestDay := PlannedSessionForDate(plan, date)
	if !isRestDay && session == nil {
		return nil, nil
	}

	if isRestDay {
		if len(notes) > 0 && claudeCfg != nil && claudeCfg.Enabled {
			return evaluateDateWithNotes(ctx, claudeCfg, plan, date, nil, notes, true)
		}
		return &Evaluation{
			PlannedType: "rest",
			ActualType:  "rest",
			Compliance:  "rest_day",
			Notes:       noteRestDay,
			Flags:       []string{},
			Adjustments: "",
			Date:        date,
		}, nil
	}

	sessionType := "session"
	if session.Session != nil && session.Session.Description != "" {
		sessionType = session.Session.Description
	}
	if len(notes) > 0 && claudeCfg != nil && claudeCfg.Enabled {
		return evaluateDateWithNotes(ctx, claudeCfg, plan, date, session, notes, false)
	}
	return &Evaluation{
		PlannedType: sessionType,
		ActualType:  "none",
		Compliance:  "missed",
		Notes:       fmt.Sprintf(noteMissedSession, sessionType),
		Flags:       []string{},
		Adjustments: "",
		Date:        date,
	}, nil
}

// deleteDateEvaluationsForPlanTx scans non-workout evaluations for the given plan
// inside a transaction and removes any whose decrypted Evaluation.Date matches
// the target date. Used by ReEvaluateDate to clear prior rest-day /
// missed-session records inside the same transaction that inserts the new ones.
func deleteDateEvaluationsForPlanTx(ctx context.Context, tx *sql.Tx, userID, planID int64, date string) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT id, eval_json FROM stride_evaluations
		WHERE user_id = ? AND plan_id = ? AND workout_id IS NULL
	`, userID, planID)
	if err != nil {
		return err
	}
	var toDelete []int64
	for rows.Next() {
		var id int64
		var encJSON string
		if err := rows.Scan(&id, &encJSON); err != nil {
			rows.Close()
			return err
		}
		decJSON, derr := encryption.DecryptField(encJSON)
		if derr != nil {
			log.Printf("stride eval: decrypt date-eval %d: %v", id, derr)
			continue
		}
		var e Evaluation
		if err := json.Unmarshal([]byte(decJSON), &e); err != nil {
			log.Printf("stride eval: unmarshal date-eval %d: %v", id, err)
			continue
		}
		if e.Date == date {
			toDelete = append(toDelete, id)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, id := range toDelete {
		if _, err := tx.ExecContext(ctx, `DELETE FROM stride_evaluations WHERE id = ?`, id); err != nil {
			return err
		}
	}
	return nil
}

// queryUnevaluatedWorkouts returns workouts for a user started at or after since
// that do not yet have a stride_evaluation record.
func queryUnevaluatedWorkouts(ctx context.Context, db *sql.DB, userID int64, since string) ([]training.Workout, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT w.id, w.user_id, w.sport, w.sub_sport, w.is_indoor, w.title, w.started_at,
		       w.duration_seconds, w.distance_meters, w.avg_heart_rate, w.max_heart_rate,
		       w.avg_pace_sec_per_km, w.avg_cadence, w.calories,
		       w.ascent_meters, w.descent_meters, w.fit_file_hash, w.analysis_status, w.title_source,
		       w.created_at, w.training_load, w.hr_drift_pct, w.pace_cv_pct
		FROM workouts w
		WHERE w.user_id = ?
		  AND w.started_at >= ?
		  AND NOT EXISTS (
		      SELECT 1 FROM stride_evaluations e
		      WHERE e.workout_id = w.id
		  )
		ORDER BY w.started_at ASC
	`, userID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var workouts []training.Workout
	for rows.Next() {
		var w training.Workout
		var isIndoor int
		var trainingLoad, hrDriftPct, paceCVPct sql.NullFloat64
		if err := rows.Scan(
			&w.ID, &w.UserID, &w.Sport, &w.SubSport, &isIndoor, &w.Title, &w.StartedAt,
			&w.DurationSeconds, &w.DistanceMeters, &w.AvgHeartRate, &w.MaxHeartRate,
			&w.AvgPaceSecPerKm, &w.AvgCadence, &w.Calories,
			&w.AscentMeters, &w.DescentMeters, &w.FitFileHash, &w.AnalysisStatus, &w.TitleSource,
			&w.CreatedAt, &trainingLoad, &hrDriftPct, &paceCVPct,
		); err != nil {
			log.Printf("stride eval: scan workout: %v", err)
			continue
		}
		w.IsIndoor = isIndoor != 0
		if trainingLoad.Valid {
			w.TrainingLoad = &trainingLoad.Float64
		}
		if hrDriftPct.Valid {
			w.HRDriftPct = &hrDriftPct.Float64
		}
		if paceCVPct.Valid {
			w.PaceCVPct = &paceCVPct.Float64
		}
		// Decrypt title. If decryption fails the value is a ciphertext that
		// could not be decoded — clear it rather than leaking ciphertext into the AI prompt.
		if decTitle, decErr := encryption.DecryptField(w.Title); decErr != nil {
			log.Printf("stride eval: workout %d: failed to decrypt title: %v; omitting from prompt", w.ID, decErr)
			w.Title = ""
		} else {
			w.Title = decTitle
		}
		workouts = append(workouts, w)
	}
	return workouts, rows.Err()
}

// evaluateSingleWorkout finds the matching plan+session for a workout, evaluates it via
// Claude, stores the result, and sends a push notification for critical flags.
// notes are unconsumed user notes providing context about sickness, fatigue, etc.
func evaluateSingleWorkout(
	ctx context.Context,
	db *sql.DB,
	httpClient *http.Client,
	userID int64,
	workout training.Workout,
	cfg *training.ClaudeConfig,
	profile training.UserTrainingProfile,
	notes []Note,
) error {
	workoutDate := extractDate(workout.StartedAt)
	if workoutDate == "" {
		return fmt.Errorf("invalid workout started_at: %s", workout.StartedAt)
	}

	plan, err := getPlanContainingDate(ctx, db, userID, workoutDate)
	if err != nil {
		return fmt.Errorf("find plan for date %s: %w", workoutDate, err)
	}

	var matchedSession *PlannedSession
	var planForEval Plan
	if plan != nil {
		planForEval = *plan
		sessions := extractPlannedSessions(planForEval)
		matchedSession = MatchWorkoutToSession(workout, sessions)
	}

	eval, err := EvaluateWorkout(ctx, cfg, workout, matchedSession, planForEval, profile, notes)
	if err != nil {
		return fmt.Errorf("evaluate workout: %w", err)
	}

	// Send push notification for critical flags regardless of whether a plan was matched.
	if hasCriticalFlag(eval.Flags) {
		notif := push.Notification{
			Title: "Stride Alert",
			Body:  buildCriticalNotifBody(eval),
			Tag:   "stride-eval-alert",
		}
		payload, err := json.Marshal(notif)
		if err != nil {
			log.Printf("stride eval: marshal notification for user %d: %v", userID, err)
		} else if _, err := push.SendToUser(db, httpClient, userID, payload); err != nil {
			log.Printf("stride eval: push notification for user %d: %v", userID, err)
		}
	}

	if plan == nil {
		// Cannot store evaluation without a plan_id (NOT NULL constraint).
		log.Printf("stride eval: workout %d on %s has no matching plan, skipping storage", workout.ID, workoutDate)
		return nil
	}

	if err := storeEvaluation(ctx, db, userID, workout.ID, plan.ID, eval); err != nil {
		return fmt.Errorf("store evaluation: %w", err)
	}

	return nil
}

// getPlanContainingDate finds the stride plan whose week spans the given date.
// Returns nil, nil when no matching plan exists.
func getPlanContainingDate(ctx context.Context, db *sql.DB, userID int64, date string) (*Plan, error) {
	row := db.QueryRowContext(ctx, `
		SELECT id, user_id, week_start, week_end, phase, plan_json, model, created_at
		FROM stride_plans
		WHERE user_id = ? AND week_start <= ? AND week_end >= ?
		ORDER BY week_start DESC
		LIMIT 1
	`, userID, date, date)
	p, err := scanPlan(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// storeEvaluation encrypts and inserts an Evaluation record into stride_evaluations.
func storeEvaluation(ctx context.Context, db *sql.DB, userID, workoutID, planID int64, eval *Evaluation) error {
	evalBytes, err := json.Marshal(eval)
	if err != nil {
		return fmt.Errorf("marshal eval: %w", err)
	}
	encEval, err := encryption.EncryptField(string(evalBytes))
	if err != nil {
		return fmt.Errorf("encrypt eval: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)

	_, err = db.ExecContext(ctx, `
		INSERT INTO stride_evaluations (user_id, plan_id, workout_id, eval_json, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, userID, planID, workoutID, encEval, now)
	return err
}

// storeEvaluationForDate encrypts and inserts an Evaluation record with no associated workout.
// Used for rest day and missed session evaluations.
func storeEvaluationForDate(ctx context.Context, db *sql.DB, userID, planID int64, eval *Evaluation) error {
	evalBytes, err := json.Marshal(eval)
	if err != nil {
		return fmt.Errorf("marshal eval: %w", err)
	}
	encEval, err := encryption.EncryptField(string(evalBytes))
	if err != nil {
		return fmt.Errorf("encrypt eval: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)

	_, err = db.ExecContext(ctx, `
		INSERT INTO stride_evaluations (user_id, plan_id, workout_id, eval_json, created_at)
		VALUES (?, ?, NULL, ?, ?)
	`, userID, planID, encEval, now)
	return err
}

// hasDateEvaluation checks whether a non-workout evaluation already exists for
// the given user and plan created on or after the given timestamp. This prevents
// duplicate rest_day/missed evaluations when nightly eval runs more than once.
func hasDateEvaluation(ctx context.Context, db *sql.DB, userID, planID int64, since string) (bool, error) {
	var count int
	err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM stride_evaluations
		WHERE user_id = ? AND plan_id = ? AND workout_id IS NULL AND created_at >= ?
	`, userID, planID, since).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// evaluateRestDaysAndMissedSessions checks whether the target date was a rest day or had a
// planned session with no matching workout, and creates an appropriate evaluation.
// When user notes exist for the target date, it calls Claude for a contextual evaluation
// instead of using template strings.
func evaluateRestDaysAndMissedSessions(ctx context.Context, db *sql.DB, claudeCfg *training.ClaudeConfig, userID int64, date string) error {
	plan, err := getPlanContainingDate(ctx, db, userID, date)
	if err != nil {
		return fmt.Errorf("find plan for date %s: %w", date, err)
	}
	if plan == nil {
		return nil
	}

	// Check if we already created a non-workout evaluation for this date (idempotency).
	dateStart := date + "T00:00:00Z"
	exists, err := hasDateEvaluation(ctx, db, userID, plan.ID, dateStart)
	if err != nil {
		return fmt.Errorf("check existing date evaluation: %w", err)
	}
	if exists {
		return nil
	}

	session, isRestDay := PlannedSessionForDate(*plan, date)

	// Check if any workout exists for this date.
	hasWorkout, err := hasWorkoutOnDate(ctx, db, userID, date)
	if err != nil {
		return fmt.Errorf("check workouts for date %s: %w", date, err)
	}

	if hasWorkout {
		// A workout was uploaded for this date — the regular evaluation path handles it.
		return nil
	}

	// Fetch user notes targeting this date for contextual evaluation.
	userNotes, err := GetNotesByTargetDate(db, userID, date)
	if err != nil {
		log.Printf("stride eval: fetch notes for user %d date %s: %v", userID, date, err)
		// Non-fatal — fall through to template evaluation.
	}

	if isRestDay {
		eval := &Evaluation{
			PlannedType: "rest",
			ActualType:  "rest",
			Compliance:  "rest_day",
			Notes:       noteRestDay,
			Flags:       []string{},
			Adjustments: "",
			Date:        date,
		}
		// If the user left notes on a rest day, use Claude for contextual evaluation.
		if len(userNotes) > 0 && claudeCfg != nil && claudeCfg.Enabled {
			if aiEval, err := evaluateDateWithNotes(ctx, claudeCfg, *plan, date, nil, userNotes, true); err != nil {
				log.Printf("stride eval: Claude contextual rest-day eval for user %d date %s: %v; falling back to template", userID, date, err)
			} else {
				eval = aiEval
			}
		}
		return storeEvaluationForDate(ctx, db, userID, plan.ID, eval)
	}

	if session != nil {
		sessionType := "session"
		if session.Session != nil && session.Session.Description != "" {
			sessionType = session.Session.Description
		}
		eval := &Evaluation{
			PlannedType: sessionType,
			ActualType:  "none",
			Compliance:  "missed",
			Notes:       fmt.Sprintf(noteMissedSession, sessionType),
			Flags:       []string{},
			Adjustments: "",
			Date:        date,
		}
		// If the user left notes explaining the miss, use Claude for contextual evaluation.
		if len(userNotes) > 0 && claudeCfg != nil && claudeCfg.Enabled {
			if aiEval, err := evaluateDateWithNotes(ctx, claudeCfg, *plan, date, session, userNotes, false); err != nil {
				log.Printf("stride eval: Claude contextual missed-session eval for user %d date %s: %v; falling back to template", userID, date, err)
			} else {
				eval = aiEval
			}
		}
		return storeEvaluationForDate(ctx, db, userID, plan.ID, eval)
	}

	return nil
}

// evaluateDateWithNotes calls Claude to produce a contextual evaluation for a date
// where the user left notes but did not complete a workout. It considers the planned
// session, the user's notes, and whether it was a rest day.
func evaluateDateWithNotes(
	ctx context.Context,
	cfg *training.ClaudeConfig,
	plan Plan,
	date string,
	session *PlannedSession,
	notes []Note,
	isRestDay bool,
) (*Evaluation, error) {
	prompt := buildNoteEvalPrompt(plan, date, session, notes, isRestDay)

	evalCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	response, err := runPromptFunc(evalCtx, cfg, prompt)
	if err != nil {
		return nil, fmt.Errorf("claude prompt: %w", err)
	}

	eval, err := parseEvalResponse(response)
	if err != nil {
		return nil, fmt.Errorf("parse eval response: %w", err)
	}

	if eval.Date == "" {
		eval.Date = date
	}
	return eval, nil
}

// buildNoteEvalPrompt creates a Claude prompt for evaluating a date where the user
// left notes but did not complete a workout.
func buildNoteEvalPrompt(plan Plan, date string, session *PlannedSession, notes []Note, isRestDay bool) string {
	var sb strings.Builder

	sb.WriteString("You are an expert running coach. Evaluate the following training day based on the user's notes.\n\n")

	sb.WriteString("## Date\n")
	fmt.Fprintf(&sb, "%s\n\n", date)

	sb.WriteString("## Planned Session\n")
	if isRestDay {
		sb.WriteString("Rest day (no workout planned).\n")
	} else if session != nil && session.Session != nil {
		s := session.Session
		if s.Description != "" {
			fmt.Fprintf(&sb, "- Purpose: %s\n", s.Description)
		}
		fmt.Fprintf(&sb, "- Main Set: %s\n", s.MainSet)
		if s.TargetHRCap > 0 {
			fmt.Fprintf(&sb, "- Target HR Cap: %d bpm\n", s.TargetHRCap)
		}
	} else {
		sb.WriteString("Unknown.\n")
	}
	sb.WriteString("\n")

	sb.WriteString("## User Notes for This Day\n")
	for _, n := range notes {
		fmt.Fprintf(&sb, "- %s\n", n.Content)
	}
	sb.WriteString("\n")

	sb.WriteString("## No Workout Was Recorded\n")
	sb.WriteString("The user did not upload any workout for this day. Use their notes to understand why and provide an empathetic, contextual evaluation.\n\n")

	sb.WriteString(`## Output Format
Return ONLY a JSON object with these fields:
- "planned_type": string — type of planned session (e.g. "threshold", "easy", "rest", "none")
- "actual_type": string — "rest" or "none"
- "compliance": string — one of "compliant", "partial", "missed", "rest_day"
- "notes": string — 2-4 sentence contextual evaluation considering the user's notes
- "flags": array of strings — zero or more warning flags (empty array if none)
- "adjustments": string — 1-2 sentences of suggested adjustments based on the situation
- "date": string — "` + date + `"

Output ONLY the JSON object, no other text.
`)

	return sb.String()
}

// hasWorkoutOnDate checks whether any workout exists for the given user on the specified date.
func hasWorkoutOnDate(ctx context.Context, db *sql.DB, userID int64, date string) (bool, error) {
	dateStart := date + "T00:00:00Z"
	// Use exclusive upper bound (start of next day) to cover the full 24 hours.
	t, err := time.Parse("2006-01-02", date)
	if err != nil {
		return false, fmt.Errorf("parse date %q: %w", date, err)
	}
	nextDay := t.AddDate(0, 0, 1).Format("2006-01-02") + "T00:00:00Z"
	var count int
	err = db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM workouts
		WHERE user_id = ? AND started_at >= ? AND started_at < ?
	`, userID, dateStart, nextDay).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// hasCriticalFlag returns true if any flag in the list is considered critical.
func hasCriticalFlag(flags []string) bool {
	for _, f := range flags {
		if criticalFlags[f] {
			return true
		}
	}
	return false
}

// buildCriticalNotifBody formats a short notification body listing the critical flags.
func buildCriticalNotifBody(eval *Evaluation) string {
	var critical []string
	for _, f := range eval.Flags {
		if criticalFlags[f] {
			critical = append(critical, f)
		}
	}
	if len(critical) == 0 {
		return "Check your latest workout evaluation."
	}
	return fmt.Sprintf("Flags: %s", strings.Join(critical, ", "))
}

// buildEvalPrompt assembles the Claude prompt for evaluating a single workout.
// notes are unconsumed user notes providing context about sickness, fatigue, etc.
func buildEvalPrompt(
	workout training.Workout,
	matchedSession *PlannedSession,
	plan Plan,
	profile training.UserTrainingProfile,
	notes []Note,
) string {
	var sb strings.Builder

	sb.WriteString("You are an expert running coach applying the Marius Bakken threshold-dominant model.\n")
	sb.WriteString("Evaluate the completed workout against the planned session and provide a structured JSON assessment.\n\n")

	if profile.Block != "" {
		sb.WriteString("## Athlete Profile\n")
		sb.WriteString(profile.Block)
		sb.WriteString("\n")
	}

	sb.WriteString("## Planned Session\n")
	if matchedSession == nil || matchedSession.Session == nil {
		sb.WriteString("None — this was a bonus/unplanned workout.\n")
	} else {
		s := matchedSession.Session
		fmt.Fprintf(&sb, "- Date: %s\n", matchedSession.Date)
		if s.Description != "" {
			fmt.Fprintf(&sb, "- Purpose: %s\n", s.Description)
		}
		if s.Warmup != "" {
			fmt.Fprintf(&sb, "- Warmup: %s\n", s.Warmup)
		}
		fmt.Fprintf(&sb, "- Main Set: %s\n", s.MainSet)
		if s.Cooldown != "" {
			fmt.Fprintf(&sb, "- Cooldown: %s\n", s.Cooldown)
		}
		if s.Strides != "" {
			fmt.Fprintf(&sb, "- Strides: %s\n", s.Strides)
		}
		if s.TargetHRCap > 0 {
			fmt.Fprintf(&sb, "- Target HR Cap: %d bpm\n", s.TargetHRCap)
		}
	}
	sb.WriteString("\n")

	sb.WriteString("## Completed Workout\n")
	if workout.Title != "" {
		fmt.Fprintf(&sb, "- Title: %s\n", workout.Title)
	}
	fmt.Fprintf(&sb, "- Sport: %s\n", workout.Sport)
	fmt.Fprintf(&sb, "- Date: %s\n", extractDate(workout.StartedAt))
	if workout.DurationSeconds > 0 {
		fmt.Fprintf(&sb, "- Duration: %s\n", formatDurationSecs(workout.DurationSeconds))
	}
	if workout.DistanceMeters > 0 {
		fmt.Fprintf(&sb, "- Distance: %.2f km\n", workout.DistanceMeters/1000)
	}
	if workout.AvgHeartRate > 0 {
		fmt.Fprintf(&sb, "- Avg HR: %d bpm\n", workout.AvgHeartRate)
	}
	if workout.MaxHeartRate > 0 {
		fmt.Fprintf(&sb, "- Max HR: %d bpm\n", workout.MaxHeartRate)
	}
	if workout.AvgPaceSecPerKm > 0 {
		paceMin := int(workout.AvgPaceSecPerKm) / 60
		paceSec := int(workout.AvgPaceSecPerKm) % 60
		fmt.Fprintf(&sb, "- Avg Pace: %d:%02d /km\n", paceMin, paceSec)
	}
	if workout.AvgCadence > 0 {
		fmt.Fprintf(&sb, "- Avg Cadence: %d spm\n", workout.AvgCadence)
	}
	if workout.TrainingLoad != nil {
		fmt.Fprintf(&sb, "- Training Load: %.1f\n", *workout.TrainingLoad)
	}
	if workout.HRDriftPct != nil {
		fmt.Fprintf(&sb, "- HR Drift: %.1f%%\n", *workout.HRDriftPct)
	}
	sb.WriteString("\n")

	if plan.ID > 0 {
		sb.WriteString("## Weekly Plan Context\n")
		fmt.Fprintf(&sb, "- Week: %s to %s\n", plan.WeekStart, plan.WeekEnd)
		var dayPlans []DayPlan
		if err := json.Unmarshal(plan.Plan, &dayPlans); err == nil {
			for _, dp := range dayPlans {
				if dp.RestDay {
					fmt.Fprintf(&sb, "- %s: Rest\n", dp.Date)
				} else if dp.Session != nil {
					fmt.Fprintf(&sb, "- %s: %s\n", dp.Date, dp.Session.Description)
				}
			}
		}
		sb.WriteString("\n")
	}

	if len(notes) > 0 {
		sb.WriteString("## User Notes\n")
		sb.WriteString("The athlete left the following notes. Consider these for context about sickness, fatigue, skipped workouts, or other factors:\n")
		for _, n := range notes {
			fmt.Fprintf(&sb, "- [%s] %s\n", n.TargetDate, n.Content)
		}
		sb.WriteString("\n")
	}

	sb.WriteString(`## Output Format
Return ONLY a JSON object with these fields:
- "planned_type": string — type of planned session (e.g. "threshold", "easy", "long_run", "none")
- "actual_type": string — type of session that was performed
- "compliance": string — one of "compliant", "partial", "missed", "bonus"
- "notes": string — 2-4 sentence narrative assessment of the workout compliance and quality
- "flags": array of strings — zero or more warning flags from: "hr_too_high", "hr_too_low", "too_short", "too_long", "overtraining", "injury_risk", "pacing_issue"
- "adjustments": string — 1-2 sentences of suggested adjustments to the next session(s) based on this result

Output ONLY the JSON object, no other text.
`)

	return sb.String()
}

// parseEvalResponse strips optional markdown fences and unmarshals the Claude response
// into a validated Evaluation struct.
func parseEvalResponse(response string) (*Evaluation, error) {
	response = strings.TrimSpace(response)

	if strings.HasPrefix(response, "```") {
		lines := strings.Split(response, "\n")
		if len(lines) >= 3 {
			response = strings.Join(lines[1:len(lines)-1], "\n")
		}
	}

	var eval Evaluation
	if err := json.Unmarshal([]byte(response), &eval); err != nil {
		return nil, fmt.Errorf("unmarshal eval JSON: %w", err)
	}

	switch eval.Compliance {
	case "compliant", "partial", "missed", "bonus", "rest_day":
	default:
		return nil, fmt.Errorf("invalid compliance value: %q", eval.Compliance)
	}

	if eval.Flags == nil {
		eval.Flags = []string{}
	}

	return &eval, nil
}
