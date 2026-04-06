package stride

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/encryption"
	"github.com/Robin831/Hytte/internal/push"
	"github.com/Robin831/Hytte/internal/training"
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
	Compliance  string   `json:"compliance"`   // "compliant", "partial", "missed", or "bonus"
	Notes       string   `json:"notes"`        // narrative assessment
	Flags       []string `json:"flags"`        // warning flags, e.g. "hr_too_high", "overtraining"
	Adjustments string   `json:"adjustments"`  // suggested adjustments to upcoming training
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
func EvaluateWorkout(
	ctx context.Context,
	cfg *training.ClaudeConfig,
	workout training.Workout,
	matchedSession *PlannedSession,
	plan Plan,
	profile training.UserTrainingProfile,
) (*Evaluation, error) {
	prompt := buildEvalPrompt(workout, matchedSession, plan, profile)

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

	since := time.Now().UTC().Add(-24 * time.Hour).Format(time.RFC3339)

	for _, userID := range userIDs {
		if err := evaluateUserWorkouts(ctx, db, httpClient, userID, since); err != nil {
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
		if err := evaluateSingleWorkout(evalCtx, db, httpClient, userID, workout, claudeCfg, profile); err != nil {
			log.Printf("stride eval: workout %d for user %d: %v", workout.ID, userID, err)
		} else {
			evaluated++
		}
		cancel()
	}
	return evaluated, nil
}

// evaluateUserWorkouts processes all unevaluated workouts for a single user since the given timestamp.
func evaluateUserWorkouts(ctx context.Context, db *sql.DB, httpClient *http.Client, userID int64, since string) error {
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
	if len(workouts) == 0 {
		return nil
	}

	profile := training.BuildUserTrainingProfile(db, userID)

	for _, workout := range workouts {
		evalCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
		if err := evaluateSingleWorkout(evalCtx, db, httpClient, userID, workout, claudeCfg, profile); err != nil {
			log.Printf("stride eval: workout %d for user %d: %v", workout.ID, userID, err)
		}
		cancel()
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
func evaluateSingleWorkout(
	ctx context.Context,
	db *sql.DB,
	httpClient *http.Client,
	userID int64,
	workout training.Workout,
	cfg *training.ClaudeConfig,
	profile training.UserTrainingProfile,
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

	eval, err := EvaluateWorkout(ctx, cfg, workout, matchedSession, planForEval, profile)
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
func buildEvalPrompt(
	workout training.Workout,
	matchedSession *PlannedSession,
	plan Plan,
	profile training.UserTrainingProfile,
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
	case "compliant", "partial", "missed", "bonus":
	default:
		return nil, fmt.Errorf("invalid compliance value: %q", eval.Compliance)
	}

	if eval.Flags == nil {
		eval.Flags = []string{}
	}

	return &eval, nil
}
