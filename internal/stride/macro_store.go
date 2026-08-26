package stride

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Robin831/Hytte/internal/encryption"
)

// ErrMacroPlanNotFound is returned when a macro plan lookup targets a row that
// does not exist or does not belong to the requesting user.
var ErrMacroPlanNotFound = errors.New("macro plan not found")

// ErrMacroWeekNotFound is returned when a macro week lookup or update targets a
// row that does not exist or does not belong to the requesting user.
var ErrMacroWeekNotFound = errors.New("macro week not found")

// macroPlanColumns is the shared SELECT list for stride_macro_plans, kept in
// one place so it cannot drift from scanMacroPlan's argument order.
const macroPlanColumns = `id, user_id, start_week, end_week, status, stale_reason,
		goal_json, periodisation_json, prompt, response, model, generated_by,
		previous_plan_id, created_at`

// macroWeekColumns is the shared SELECT list for stride_macro_weeks, matching
// scanMacroWeek's argument order.
// The columns are qualified with the `w` alias so the list can be reused in the
// join against stride_macro_plans without ambiguity — every query below aliases
// stride_macro_weeks as `w`.
const macroWeekColumns = `w.id, w.macro_plan_id, w.user_id, w.week_start, w.seq, w.phase, w.mesocycle,
		w.load_level, w.target_km, w.target_sessions, w.race_id, w.key_sessions_json, w.intent, w.status`

// encryptJSON marshals v and encrypts the result for storage in an ENC column.
func encryptJSON(v any, field string) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("marshal %s: %w", field, err)
	}
	enc, err := encryption.EncryptField(string(raw))
	if err != nil {
		return "", fmt.Errorf("encrypt %s: %w", field, err)
	}
	return enc, nil
}

// decryptJSON decrypts an ENC column and unmarshals it into out. An empty
// column is treated as "not set" and leaves out untouched, so rows written
// before a blob field existed decode instead of erroring.
func decryptJSON(stored string, out any, field string) error {
	if stored == "" {
		return nil
	}
	plain, err := encryption.DecryptField(stored)
	if err != nil {
		return fmt.Errorf("decrypt %s: %w", field, err)
	}
	if plain == "" {
		return nil
	}
	if err := json.Unmarshal([]byte(plain), out); err != nil {
		return fmt.Errorf("unmarshal %s: %w", field, err)
	}
	return nil
}

// validateMacroPlan checks the enum columns that the UI and the weekly
// generator branch on. Free-text and AI-authored fields are not validated.
func validateMacroPlan(p *MacroPlan) error {
	if p == nil {
		return errors.New("macro plan must not be nil")
	}
	if p.UserID == 0 {
		return errors.New("macro plan user_id must be set")
	}
	if p.StartWeek == "" || p.EndWeek == "" {
		return errors.New("macro plan start_week and end_week must be set")
	}
	if p.EndWeek < p.StartWeek {
		return fmt.Errorf("macro plan end_week %q is before start_week %q", p.EndWeek, p.StartWeek)
	}
	switch p.Status {
	case "", MacroPlanStatusActive, MacroPlanStatusSuperseded:
	default:
		return fmt.Errorf("invalid macro plan status %q", p.Status)
	}
	switch p.GeneratedBy {
	case "", MacroGeneratedByScheduled, MacroGeneratedByManual, MacroGeneratedByExtension:
	default:
		return fmt.Errorf("invalid macro plan generated_by %q", p.GeneratedBy)
	}
	return nil
}

// validateMacroWeek checks the queryable enum columns of a macro week.
func validateMacroWeek(w *MacroWeek) error {
	if w.WeekStart == "" {
		return errors.New("macro week week_start must be set")
	}
	switch w.Phase {
	case MacroPhaseBase, MacroPhaseBuild, MacroPhasePeak, MacroPhaseTaper, MacroPhaseRace, MacroPhaseRecovery:
	default:
		return fmt.Errorf("invalid macro week phase %q", w.Phase)
	}
	switch w.LoadLevel {
	case LoadLevelDeload, LoadLevelNormal, LoadLevelBuild, LoadLevelPeak, LoadLevelTaper:
	default:
		return fmt.Errorf("invalid macro week load_level %q", w.LoadLevel)
	}
	switch w.Status {
	case "", MacroWeekStatusPlanned, MacroWeekStatusMaterialised, MacroWeekStatusSkipped:
	default:
		return fmt.Errorf("invalid macro week status %q", w.Status)
	}
	return nil
}

// ValidateMacroWeekStatus returns nil when status is one of the allowed macro
// week statuses.
func ValidateMacroWeekStatus(status string) error {
	switch status {
	case MacroWeekStatusPlanned, MacroWeekStatusMaterialised, MacroWeekStatusSkipped:
		return nil
	default:
		return fmt.Errorf("invalid macro week status %q: must be one of planned, materialised, skipped", status)
	}
}

// ValidateGoalRevisionSource returns nil when source is one of the allowed
// goal revision sources.
func ValidateGoalRevisionSource(source string) error {
	switch source {
	case GoalRevisionSourceInitial, GoalRevisionSourceWeekly, GoalRevisionSourceManual:
		return nil
	default:
		return fmt.Errorf("invalid goal revision source %q: must be one of initial, weekly, manual", source)
	}
}

// CreateMacroPlan writes a macro plan, all of its weeks, and the block's
// initial goal revision in a single transaction — a partially written block
// (a plan row with no weeks, or weeks with no goal history) is never visible.
// On success plan.ID, plan.CreatedAt, plan.Weeks and each week's ID are filled
// in from the inserted rows.
func CreateMacroPlan(ctx context.Context, db *sql.DB, plan *MacroPlan, weeks []MacroWeek, initialGoalReason string) error {
	if err := validateMacroPlan(plan); err != nil {
		return err
	}
	if len(weeks) == 0 {
		return errors.New("macro plan must have at least one week")
	}
	for i := range weeks {
		if err := validateMacroWeek(&weeks[i]); err != nil {
			return fmt.Errorf("week %d: %w", i, err)
		}
	}

	if plan.Status == "" {
		plan.Status = MacroPlanStatusActive
	}
	if plan.GeneratedBy == "" {
		plan.GeneratedBy = MacroGeneratedByScheduled
	}
	if plan.CreatedAt.IsZero() {
		plan.CreatedAt = time.Now().UTC()
	}
	createdAt := plan.CreatedAt.UTC().Format(time.RFC3339)

	goalJSON, err := encryptJSON(plan.Goal, "goal_json")
	if err != nil {
		return err
	}
	periodisationJSON, err := encryptJSON(plan.Periodisation, "periodisation_json")
	if err != nil {
		return err
	}
	encPrompt, err := encryption.EncryptField(plan.Prompt)
	if err != nil {
		return fmt.Errorf("encrypt macro plan prompt: %w", err)
	}
	encResponse, err := encryption.EncryptField(plan.Response)
	if err != nil {
		return fmt.Errorf("encrypt macro plan response: %w", err)
	}
	encReason, err := encryption.EncryptField(initialGoalReason)
	if err != nil {
		return fmt.Errorf("encrypt initial goal revision reason: %w", err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin macro plan tx: %w", err)
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx, `
		INSERT INTO stride_macro_plans
			(user_id, start_week, end_week, status, stale_reason, goal_json,
			 periodisation_json, prompt, response, model, generated_by,
			 previous_plan_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, plan.UserID, plan.StartWeek, plan.EndWeek, plan.Status, plan.StaleReason,
		goalJSON, periodisationJSON, encPrompt, encResponse, plan.Model,
		plan.GeneratedBy, plan.PreviousPlanID, createdAt)
	if err != nil {
		return fmt.Errorf("insert macro plan: %w", err)
	}
	planID, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("macro plan id: %w", err)
	}

	inserted := make([]MacroWeek, len(weeks))
	for i, w := range weeks {
		w.MacroPlanID = planID
		w.UserID = plan.UserID
		if w.Status == "" {
			w.Status = MacroWeekStatusPlanned
		}
		keySessions, err := encryptJSON(w.KeySessions, "key_sessions_json")
		if err != nil {
			return err
		}
		encIntent, err := encryption.EncryptField(w.Intent)
		if err != nil {
			return fmt.Errorf("encrypt macro week intent: %w", err)
		}
		wres, err := tx.ExecContext(ctx, `
			INSERT INTO stride_macro_weeks
				(macro_plan_id, user_id, week_start, seq, phase, mesocycle, load_level,
				 target_km, target_sessions, race_id, key_sessions_json, intent, status)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, w.MacroPlanID, w.UserID, w.WeekStart, w.Seq, w.Phase, w.Mesocycle, w.LoadLevel,
			w.TargetKm, w.TargetSessions, w.RaceID, keySessions, encIntent, w.Status)
		if err != nil {
			return fmt.Errorf("insert macro week %s: %w", w.WeekStart, err)
		}
		if w.ID, err = wres.LastInsertId(); err != nil {
			return fmt.Errorf("macro week id: %w", err)
		}
		inserted[i] = w
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO stride_goal_revisions
			(macro_plan_id, user_id, week_start, goal_json, reason, source, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, planID, plan.UserID, plan.StartWeek, goalJSON, encReason, GoalRevisionSourceInitial, createdAt); err != nil {
		return fmt.Errorf("insert initial goal revision: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit macro plan tx: %w", err)
	}

	plan.ID = planID
	plan.Weeks = inserted
	return nil
}

// scanMacroPlan reads a stride_macro_plans row selected with macroPlanColumns,
// decrypting the AI-authored blobs.
func scanMacroPlan(scanner interface{ Scan(...any) error }) (MacroPlan, error) {
	var p MacroPlan
	var goalJSON, periodisationJSON, prompt, response, createdAt string
	if err := scanner.Scan(&p.ID, &p.UserID, &p.StartWeek, &p.EndWeek, &p.Status, &p.StaleReason,
		&goalJSON, &periodisationJSON, &prompt, &response, &p.Model, &p.GeneratedBy,
		&p.PreviousPlanID, &createdAt); err != nil {
		return MacroPlan{}, err
	}
	if err := decryptJSON(goalJSON, &p.Goal, "goal_json"); err != nil {
		return MacroPlan{}, err
	}
	if err := decryptJSON(periodisationJSON, &p.Periodisation, "periodisation_json"); err != nil {
		return MacroPlan{}, err
	}
	var err error
	if p.Prompt, err = encryption.DecryptField(prompt); err != nil {
		return MacroPlan{}, fmt.Errorf("decrypt macro plan prompt: %w", err)
	}
	if p.Response, err = encryption.DecryptField(response); err != nil {
		return MacroPlan{}, fmt.Errorf("decrypt macro plan response: %w", err)
	}
	if createdAt != "" {
		t, err := time.Parse(time.RFC3339, createdAt)
		if err != nil {
			return MacroPlan{}, fmt.Errorf("parse macro plan created_at: %w", err)
		}
		p.CreatedAt = t
	}
	return p, nil
}

// scanMacroWeek reads a stride_macro_weeks row selected with macroWeekColumns,
// decrypting key_sessions_json and intent.
func scanMacroWeek(scanner interface{ Scan(...any) error }) (MacroWeek, error) {
	var w MacroWeek
	var keySessions, intent string
	if err := scanner.Scan(&w.ID, &w.MacroPlanID, &w.UserID, &w.WeekStart, &w.Seq, &w.Phase,
		&w.Mesocycle, &w.LoadLevel, &w.TargetKm, &w.TargetSessions, &w.RaceID,
		&keySessions, &intent, &w.Status); err != nil {
		return MacroWeek{}, err
	}
	if err := decryptJSON(keySessions, &w.KeySessions, "key_sessions_json"); err != nil {
		return MacroWeek{}, err
	}
	var err error
	if w.Intent, err = encryption.DecryptField(intent); err != nil {
		return MacroWeek{}, fmt.Errorf("decrypt macro week intent: %w", err)
	}
	return w, nil
}

// GetActiveMacroPlan returns the user's active macro plan whose horizon covers
// coveringWeek (a Monday, inclusive at both ends), with its weeks loaded in seq
// order. Returns nil, nil when no such plan exists, matching GetCurrentPlan.
func GetActiveMacroPlan(ctx context.Context, db *sql.DB, userID int64, coveringWeek string) (*MacroPlan, error) {
	row := db.QueryRowContext(ctx, `
		SELECT `+macroPlanColumns+`
		FROM stride_macro_plans
		WHERE user_id = ? AND status = ? AND start_week <= ? AND end_week >= ?
		ORDER BY start_week DESC
		LIMIT 1
	`, userID, MacroPlanStatusActive, coveringWeek, coveringWeek)
	p, err := scanMacroPlan(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	weeks, err := listMacroWeeks(ctx, db, p.ID)
	if err != nil {
		return nil, err
	}
	p.Weeks = weeks
	return &p, nil
}

// GetMacroPlanByID returns a macro plan by id, scoped to the user, with its
// weeks loaded. Returns ErrMacroPlanNotFound when absent.
func GetMacroPlanByID(ctx context.Context, db *sql.DB, id, userID int64) (*MacroPlan, error) {
	row := db.QueryRowContext(ctx, `
		SELECT `+macroPlanColumns+`
		FROM stride_macro_plans
		WHERE id = ? AND user_id = ?
	`, id, userID)
	p, err := scanMacroPlan(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrMacroPlanNotFound
	}
	if err != nil {
		return nil, err
	}
	weeks, err := listMacroWeeks(ctx, db, p.ID)
	if err != nil {
		return nil, err
	}
	p.Weeks = weeks
	return &p, nil
}

// listMacroWeeks returns a plan's weeks in seq order.
func listMacroWeeks(ctx context.Context, db *sql.DB, macroPlanID int64) ([]MacroWeek, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT `+macroWeekColumns+`
		FROM stride_macro_weeks w
		WHERE w.macro_plan_id = ?
		ORDER BY w.seq ASC, w.week_start ASC
	`, macroPlanID)
	if err != nil {
		return nil, fmt.Errorf("query macro weeks: %w", err)
	}
	defer rows.Close()

	weeks := []MacroWeek{}
	for rows.Next() {
		w, err := scanMacroWeek(rows)
		if err != nil {
			return nil, fmt.Errorf("scan macro week: %w", err)
		}
		weeks = append(weeks, w)
	}
	return weeks, rows.Err()
}

// GetMacroWeek returns the week of the user's active macro plan that starts on
// weekStart. Returns nil, nil when the user has no active plan covering that
// week. Only active plans are consulted so a superseded block's weeks never
// drive plan generation.
func GetMacroWeek(ctx context.Context, db *sql.DB, userID int64, weekStart string) (*MacroWeek, error) {
	row := db.QueryRowContext(ctx, `
		SELECT `+macroWeekColumns+`
		FROM stride_macro_weeks w
		JOIN stride_macro_plans p ON p.id = w.macro_plan_id
		WHERE w.user_id = ? AND w.week_start = ? AND p.status = ?
		ORDER BY p.start_week DESC
		LIMIT 1
	`, userID, weekStart, MacroPlanStatusActive)
	w, err := scanMacroWeek(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &w, nil
}

// MarkMacroWeekStatus sets a macro week's status (planned, materialised or
// skipped), scoped to the owning user. Returns ErrMacroWeekNotFound when no row
// matched.
func MarkMacroWeekStatus(ctx context.Context, db *sql.DB, id, userID int64, status string) error {
	if err := ValidateMacroWeekStatus(status); err != nil {
		return err
	}
	res, err := db.ExecContext(ctx, `
		UPDATE stride_macro_weeks SET status = ? WHERE id = ? AND user_id = ?
	`, status, id, userID)
	if err != nil {
		return fmt.Errorf("update macro week status: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrMacroWeekNotFound
	}
	return nil
}

// SupersedeMacroPlan marks a macro plan as superseded, so a newly generated
// block takes over as the active one. Idempotent: superseding an already
// superseded plan is not an error.
func SupersedeMacroPlan(ctx context.Context, db *sql.DB, planID, userID int64) error {
	res, err := db.ExecContext(ctx, `
		UPDATE stride_macro_plans SET status = ? WHERE id = ? AND user_id = ?
	`, MacroPlanStatusSuperseded, planID, userID)
	if err != nil {
		return fmt.Errorf("supersede macro plan: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrMacroPlanNotFound
	}
	return nil
}

// SetMacroPlanStale records why a plan no longer matches reality (for example
// "races_changed"), which the UI surfaces as a banner with a Regenerate button.
// The plan stays active — nothing is auto-regenerated. Passing an empty reason
// clears the flag.
func SetMacroPlanStale(ctx context.Context, db *sql.DB, planID, userID int64, reason string) error {
	res, err := db.ExecContext(ctx, `
		UPDATE stride_macro_plans SET stale_reason = ? WHERE id = ? AND user_id = ?
	`, reason, planID, userID)
	if err != nil {
		return fmt.Errorf("set macro plan stale: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrMacroPlanNotFound
	}
	return nil
}

// AddGoalRevision appends a goal revision to a macro plan's history. The table
// is append-only — existing revisions are never rewritten. On success rev.ID
// and rev.CreatedAt are filled in.
func AddGoalRevision(ctx context.Context, db *sql.DB, rev *GoalRevision) error {
	if rev == nil {
		return errors.New("goal revision must not be nil")
	}
	if err := ValidateGoalRevisionSource(rev.Source); err != nil {
		return err
	}
	if rev.WeekStart == "" {
		return errors.New("goal revision week_start must be set")
	}
	if rev.CreatedAt.IsZero() {
		rev.CreatedAt = time.Now().UTC()
	}
	goalJSON, err := encryptJSON(rev.Goal, "goal_json")
	if err != nil {
		return err
	}
	encReason, err := encryption.EncryptField(rev.Reason)
	if err != nil {
		return fmt.Errorf("encrypt goal revision reason: %w", err)
	}
	res, err := db.ExecContext(ctx, `
		INSERT INTO stride_goal_revisions
			(macro_plan_id, user_id, week_start, goal_json, reason, source, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, rev.MacroPlanID, rev.UserID, rev.WeekStart, goalJSON, encReason, rev.Source,
		rev.CreatedAt.UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("insert goal revision: %w", err)
	}
	if rev.ID, err = res.LastInsertId(); err != nil {
		return fmt.Errorf("goal revision id: %w", err)
	}
	return nil
}

// ListGoalRevisions returns a macro plan's goal history oldest first, scoped to
// the owning user.
func ListGoalRevisions(ctx context.Context, db *sql.DB, macroPlanID, userID int64) ([]GoalRevision, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, macro_plan_id, user_id, week_start, goal_json, reason, source, created_at
		FROM stride_goal_revisions
		WHERE macro_plan_id = ? AND user_id = ?
		ORDER BY created_at ASC, id ASC
	`, macroPlanID, userID)
	if err != nil {
		return nil, fmt.Errorf("query goal revisions: %w", err)
	}
	defer rows.Close()

	revs := []GoalRevision{}
	for rows.Next() {
		var r GoalRevision
		var goalJSON, reason, createdAt string
		if err := rows.Scan(&r.ID, &r.MacroPlanID, &r.UserID, &r.WeekStart, &goalJSON, &reason,
			&r.Source, &createdAt); err != nil {
			return nil, fmt.Errorf("scan goal revision: %w", err)
		}
		if err := decryptJSON(goalJSON, &r.Goal, "goal_json"); err != nil {
			return nil, err
		}
		if r.Reason, err = encryption.DecryptField(reason); err != nil {
			return nil, fmt.Errorf("decrypt goal revision reason: %w", err)
		}
		if createdAt != "" {
			t, perr := time.Parse(time.RFC3339, createdAt)
			if perr != nil {
				return nil, fmt.Errorf("parse goal revision created_at: %w", perr)
			}
			r.CreatedAt = t
		}
		revs = append(revs, r)
	}
	return revs, rows.Err()
}
