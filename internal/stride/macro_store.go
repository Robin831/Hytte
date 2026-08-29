package stride

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/Robin831/Hytte/internal/encryption"
)

// ErrMacroPlanNotFound is returned when a macro plan lookup targets a row that
// does not exist or does not belong to the requesting user.
var ErrMacroPlanNotFound = errors.New("macro plan not found")

// ErrMacroWeekNotFound is returned when a macro week lookup or update targets a
// row that does not exist or does not belong to the requesting user.
var ErrMacroWeekNotFound = errors.New("macro week not found")

// ErrForeignReference is returned when a macro plan or one of its goal
// revisions points at a stride_races or stride_workouts row owned by somebody
// else (or one that does not exist).
// Handlers should surface it as a 400, not a 500.
var ErrForeignReference = errors.New("macro plan references a row the user does not own")

// ErrOverlappingMacroPlan is returned when a new block would leave the athlete
// with two active blocks covering the same week: some active block overlaps the
// new horizon and was not among the ones the write was told to retire. Callers
// resolve that set from the whole horizon before the Claude call, so in
// practice this only fires when a concurrent generation committed a block
// while the call was in flight. Re-resolving the athlete's active blocks and
// generating again clears it; repeating the same write blindly does not.
var ErrOverlappingMacroPlan = errors.New("another active macro plan already covers these weeks")

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

// validateMacroPlanRow checks the enum columns that the UI and the weekly
// generator branch on. Free-text and AI-authored fields are not validated.
func validateMacroPlanRow(p *MacroPlan) error {
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

// validateMacroWeekRow checks the queryable enum columns of a macro week. Empty
// means "use the column default" for LoadLevel (normal) and Status (planned),
// matching how validateMacroPlanRow treats Status and GeneratedBy; CreateMacroPlan
// fills both in. Phase is the one enum with no default — it is the contract the
// weekly generator branches on and the column has no meaningful default of its
// own — so an empty phase stays an error.
func validateMacroWeekRow(w *MacroWeek) error {
	if w.WeekStart == "" {
		return errors.New("macro week week_start must be set")
	}
	switch w.Phase {
	case MacroPhaseBase, MacroPhaseBuild, MacroPhasePeak, MacroPhaseTaper, MacroPhaseRace, MacroPhaseRecovery:
	default:
		return fmt.Errorf("invalid macro week phase %q", w.Phase)
	}
	switch w.LoadLevel {
	case "", LoadLevelDeload, LoadLevelNormal, LoadLevelBuild, LoadLevelPeak, LoadLevelTaper:
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

// ValidateStaleReason returns nil when reason is a known stale reason. An empty
// reason is allowed and means "not stale" — SetMacroPlanStale uses it to clear
// the flag.
func ValidateStaleReason(reason string) error {
	switch reason {
	case "", MacroStaleRacesChanged:
		return nil
	default:
		return fmt.Errorf("invalid macro plan stale_reason %q: must be empty or %s", reason, MacroStaleRacesChanged)
	}
}

// normaliseTimestamp parses a caller-supplied created_at and re-renders it as
// UTC RFC3339. The created_at columns are sorted with ORDER BY on a TEXT
// column, which is only chronological when every stored value shares one
// offset and one precision — so "2026-08-26T15:00:00+02:00" has to become
// "2026-08-26T13:00:00Z" on the way in, and a value that is not a timestamp at
// all is rejected at write time rather than blowing up on some later read.
// An empty value means "now".
func normaliseTimestamp(value, field string) (string, error) {
	if value == "" {
		return time.Now().UTC().Format(time.RFC3339), nil
	}
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return "", fmt.Errorf("invalid %s %q: must be an RFC3339 timestamp", field, value)
	}
	return t.UTC().Format(time.RFC3339), nil
}

// collectIDs appends the non-nil ids to set.
func collectIDs(set map[int64]struct{}, ids ...*int64) {
	for _, id := range ids {
		if id != nil {
			set[*id] = struct{}{}
		}
	}
}

// verifyOwnership fails with ErrForeignReference unless every id in set names a
// row of table owned by userID. table is always a package-level literal, never
// caller input.
func verifyOwnership(ctx context.Context, tx *sql.Tx, table string, userID int64, set map[int64]struct{}) error {
	ids := make([]int64, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	for _, id := range ids {
		var one int
		err := tx.QueryRowContext(ctx, `SELECT 1 FROM `+table+` WHERE id = ? AND user_id = ?`, id, userID).Scan(&one)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: %s %d", ErrForeignReference, table, id)
		}
		if err != nil {
			return fmt.Errorf("check %s ownership: %w", table, err)
		}
	}
	return nil
}

// verifyMacroReferences checks every row id a macro plan points at — the weeks'
// race_id, the goal's anchor race, the periodisation's per-mesocycle race, and
// the key sessions' library workout. The FK columns are plain (id) references,
// and the goal/key-session ids ride along inside encrypted blobs where the DB
// cannot check anything at all, so this is the only thing stopping a caller
// from pinning another user's race or library workout into a block.
func verifyMacroReferences(ctx context.Context, tx *sql.Tx, plan *MacroPlan, weeks []MacroWeek) error {
	races := map[int64]struct{}{}
	library := map[int64]struct{}{}

	collectIDs(races, plan.Goal.AnchorRaceID)
	for i := range plan.Periodisation {
		collectIDs(races, plan.Periodisation[i].RaceID)
	}
	for i := range weeks {
		collectIDs(races, weeks[i].RaceID)
		for _, ks := range weeks[i].KeySessions {
			collectIDs(library, ks.LibraryID)
		}
	}

	if err := verifyOwnership(ctx, tx, "stride_races", plan.UserID, races); err != nil {
		return err
	}
	return verifyOwnership(ctx, tx, "stride_workouts", plan.UserID, library)
}

// CreateMacroPlan writes a macro plan, all of its weeks, and the block's
// initial goal revision in a single transaction — a partially written block
// (a plan row with no weeks, or weeks with no goal history) is never visible.
// On success plan.ID, plan.CreatedAt and plan.Weeks (each with its inserted ID)
// are filled in; the weeks slice the caller passed in is left untouched.
//
// Every race and library-workout id the block references must belong to
// plan.UserID, otherwise the whole write fails with ErrForeignReference.
//
// Only one *active* plan may start on a given Monday, so a Regenerate has to
// demote the block it replaces. That demotion is part of this transaction: set
// plan.PreviousPlanID and the old block is superseded in the same commit that
// inserts the new one, so a failed create (a foreign reference, a duplicate
// week, an encryption error, a crash) can never leave the user with zero active
// blocks. The superseded row and its goal history stay put. PreviousPlanID must
// name a plan owned by plan.UserID, otherwise the write fails with
// ErrMacroPlanNotFound.
//
// Which blocks are active is re-checked inside the transaction rather than
// trusted from PreviousPlanID, because the caller resolved that id before a
// Claude call that takes minutes. If any *other* active block still overlaps
// [StartWeek, EndWeek] once the demotion has been applied, the write is
// rejected with ErrOverlappingMacroPlan: the alternative is an athlete left
// with two active blocks prescribing different things for the same week.
//
// A new horizon can overlap more than one active block — PreviousPlanID names
// only the one the block descends from, so use CreateMacroPlanReplacing to
// retire the rest in the same commit.
func CreateMacroPlan(ctx context.Context, db *sql.DB, plan *MacroPlan, weeks []MacroWeek, initialGoalReason string) error {
	return CreateMacroPlanReplacing(ctx, db, plan, weeks, initialGoalReason, nil)
}

// CreateMacroPlanReplacing is CreateMacroPlan for a block that retires more
// than its own lineage parent. supersedePlanIDs lists every *other* active
// block the new horizon overlaps; each is demoted in the same transaction as
// the insert, before the overlap check runs, and each must belong to
// plan.UserID or the whole write fails with ErrMacroPlanNotFound. Naming
// PreviousPlanID again in the list is harmless — the demotion is idempotent.
//
// The list exists because PreviousPlanID is a single lineage pointer while the
// overlap check spans the whole 26-week horizon. An athlete can hold an active
// block that starts *after* the new block does but still inside its horizon —
// an extension scheduled ahead of time does exactly that — and such a block is
// not the new one's parent yet must still be retired for the athlete to be left
// with one plan per week. Without it that block could never be replaced: every
// generation would pay its Claude call and then fail the overlap check.
func CreateMacroPlanReplacing(ctx context.Context, db *sql.DB, plan *MacroPlan, weeks []MacroWeek, initialGoalReason string, supersedePlanIDs []int64) error {
	if err := validateMacroPlanRow(plan); err != nil {
		return err
	}
	if len(weeks) == 0 {
		return errors.New("macro plan must have at least one week")
	}
	for i := range weeks {
		if err := validateMacroWeekRow(&weeks[i]); err != nil {
			return fmt.Errorf("week %d: %w", i, err)
		}
	}

	if plan.Status == "" {
		plan.Status = MacroPlanStatusActive
	}
	if plan.GeneratedBy == "" {
		plan.GeneratedBy = MacroGeneratedByScheduled
	}
	createdAt, err := normaliseTimestamp(plan.CreatedAt, "macro plan created_at")
	if err != nil {
		return err
	}

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

	if err := verifyMacroReferences(ctx, tx, plan, weeks); err != nil {
		return err
	}

	// Demote the blocks this one replaces before inserting, both to free their
	// Monday slot in idx_stride_macro_plans_active_start and so the handover is
	// all-or-nothing with the rest of the write.
	for _, id := range supersedeIDs(plan.PreviousPlanID, supersedePlanIDs) {
		if err := supersedeMacroPlanTx(ctx, tx, id, plan.UserID); err != nil {
			return err
		}
	}

	// The demotion above is applied, so anything still active here genuinely
	// competes with the new block.
	if err := verifyNoOverlappingActivePlan(ctx, tx, plan); err != nil {
		return err
	}

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
		if w.LoadLevel == "" {
			w.LoadLevel = LoadLevelNormal
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
	plan.CreatedAt = createdAt
	return nil
}

// scanMacroPlan reads a stride_macro_plans row selected with macroPlanColumns,
// decrypting the AI-authored blobs.
func scanMacroPlan(scanner interface{ Scan(...any) error }) (MacroPlan, error) {
	var p MacroPlan
	var goalJSON, periodisationJSON, prompt, response string
	if err := scanner.Scan(&p.ID, &p.UserID, &p.StartWeek, &p.EndWeek, &p.Status, &p.StaleReason,
		&goalJSON, &periodisationJSON, &prompt, &response, &p.Model, &p.GeneratedBy,
		&p.PreviousPlanID, &p.CreatedAt); err != nil {
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
	return markMacroWeekStatusTx(ctx, db, id, userID, status)
}

// markMacroWeekStatusTx is MarkMacroWeekStatus over any execer, so the weekly
// write can flip a week to 'materialised' inside the same transaction that
// stores the plan materialising it.
func markMacroWeekStatusTx(ctx context.Context, q execer, id, userID int64, status string) error {
	if err := ValidateMacroWeekStatus(status); err != nil {
		return err
	}
	res, err := q.ExecContext(ctx, `
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

// execer is the subset of *sql.DB and *sql.Tx the write helpers need, so the
// same statement can run standalone or inside CreateMacroPlan's transaction.
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// supersedeMacroPlanTx demotes a plan to 'superseded' via q, scoped to the
// owning user. Idempotent: an already superseded plan still matches the WHERE.
func supersedeMacroPlanTx(ctx context.Context, q execer, planID, userID int64) error {
	res, err := q.ExecContext(ctx, `
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

// supersedeIDs is the ordered, duplicate-free list of blocks a create retires:
// the new block's lineage parent first, then every other active block the
// caller resolved as overlapping the new horizon. Repeating the parent in that
// list is allowed and collapses here rather than costing a second UPDATE.
func supersedeIDs(previousPlanID *int64, others []int64) []int64 {
	ids := make([]int64, 0, len(others)+1)
	seen := make(map[int64]struct{}, len(others)+1)
	add := func(id int64) {
		if _, dup := seen[id]; dup {
			return
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if previousPlanID != nil {
		add(*previousPlanID)
	}
	for _, id := range others {
		add(id)
	}
	return ids
}

// macroPlanSpan is one block's horizon and nothing else — enough to decide what
// a new block overlaps and has to retire, without decrypting the AI-authored
// blobs a full scanMacroPlan would.
type macroPlanSpan struct {
	ID        int64
	StartWeek string
	EndWeek   string
}

// querier is the subset of *sql.DB and *sql.Tx the span lookup needs, so the
// same statement serves a caller resolving what to replace and the re-check
// inside CreateMacroPlan's transaction.
type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// listActiveMacroPlanSpans returns the user's active blocks whose horizon
// overlaps [startWeek, endWeek], earliest first. Two horizons overlap when each
// starts on or before the other ends — both ends are inclusive, matching
// GetActiveMacroPlan's lookup.
//
// This is deliberately the same query verifyNoOverlappingActivePlan rejects on.
// Resolving what to replace through a narrower window than the write checks is
// what made a block starting mid-horizon impossible to replace: it could never
// be named, so it was never demoted, and every generation failed the check.
func listActiveMacroPlanSpans(ctx context.Context, q querier, userID int64, startWeek, endWeek string) ([]macroPlanSpan, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT id, start_week, end_week
		FROM stride_macro_plans
		WHERE user_id = ? AND status = ? AND start_week <= ? AND end_week >= ?
		ORDER BY start_week ASC, id ASC
	`, userID, MacroPlanStatusActive, endWeek, startWeek)
	if err != nil {
		return nil, fmt.Errorf("query overlapping macro plans: %w", err)
	}
	defer rows.Close()

	spans := []macroPlanSpan{}
	for rows.Next() {
		var span macroPlanSpan
		if err := rows.Scan(&span.ID, &span.StartWeek, &span.EndWeek); err != nil {
			return nil, fmt.Errorf("scan overlapping macro plan: %w", err)
		}
		spans = append(spans, span)
	}
	return spans, rows.Err()
}

// verifyNoOverlappingActivePlan fails when the user still has an active block
// whose horizon overlaps the one being inserted.
//
// The partial unique index only covers an identical start_week, so it catches a
// double Regenerate but not a block that starts mid-horizon; and it fires as a
// constraint error rather than something a handler can explain. Checking here
// covers both, inside the same transaction that did the demotions.
func verifyNoOverlappingActivePlan(ctx context.Context, tx *sql.Tx, plan *MacroPlan) error {
	spans, err := listActiveMacroPlanSpans(ctx, tx, plan.UserID, plan.StartWeek, plan.EndWeek)
	if err != nil {
		return fmt.Errorf("check overlapping macro plans: %w", err)
	}
	if len(spans) == 0 {
		return nil
	}
	return fmt.Errorf("%w: plan %d covers %s to %s",
		ErrOverlappingMacroPlan, spans[0].ID, spans[0].StartWeek, spans[0].EndWeek)
}

// SupersedeMacroPlan marks a macro plan as superseded, retiring a block without
// putting anything in its place. Idempotent: superseding an already superseded
// plan is not an error.
//
// This is *not* the Regenerate path — replacing a block means one
// CreateMacroPlan call with PreviousPlanID set, which does the demotion in the
// same transaction as the insert. Calling this first and creating afterwards
// leaves a window (and, if the create fails, a permanent state) where the user
// has no active block at all.
func SupersedeMacroPlan(ctx context.Context, db *sql.DB, planID, userID int64) error {
	return supersedeMacroPlanTx(ctx, db, planID, userID)
}

// SetMacroPlanStale records why a plan no longer matches reality (for example
// "races_changed"), which the UI surfaces as a banner with a Regenerate button.
// The plan stays active — nothing is auto-regenerated. Passing an empty reason
// clears the flag.
func SetMacroPlanStale(ctx context.Context, db *sql.DB, planID, userID int64, reason string) error {
	if err := ValidateStaleReason(reason); err != nil {
		return err
	}
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
// is append-only — existing revisions are never rewritten. The plan must belong
// to rev.UserID; otherwise nothing is written and ErrMacroPlanNotFound is
// returned. On success rev.ID and rev.CreatedAt are filled in.
//
// The revised goal goes through the same reference check CreateMacroPlan
// applies, so "a block can never pin another user's race" holds for every write
// to the goal, not just the first: an AnchorRaceID the user does not own fails
// with ErrForeignReference and nothing is inserted.
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

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin goal revision tx: %w", err)
	}
	defer tx.Rollback()

	// Plan ownership is checked before the goal's ids so a revision aimed at
	// somebody else's plan still reports ErrMacroPlanNotFound rather than
	// leaking which races that plan happens to reference.
	var one int
	err = tx.QueryRowContext(ctx, `
		SELECT 1 FROM stride_macro_plans WHERE id = ? AND user_id = ?
	`, rev.MacroPlanID, rev.UserID).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrMacroPlanNotFound
	}
	if err != nil {
		return fmt.Errorf("check macro plan ownership: %w", err)
	}

	// The anchor race id rides along inside the encrypted goal blob, where the
	// DB can check nothing — this is the only thing stopping a revision from
	// pinning a race the user does not own.
	races := map[int64]struct{}{}
	collectIDs(races, rev.Goal.AnchorRaceID)
	if err := verifyOwnership(ctx, tx, "stride_races", rev.UserID, races); err != nil {
		return err
	}

	if err := insertGoalRevisionTx(ctx, tx, rev); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit goal revision tx: %w", err)
	}
	return nil
}

// insertGoalRevisionTx validates, encrypts and appends one goal revision
// through q, filling in rev.ID and rev.CreatedAt on success.
//
// It performs NO ownership or foreign-reference check. AddGoalRevision does
// those before calling it, because its revision comes from an HTTP caller;
// saveWeeklyPlan does not, because its revision is the athlete's own current
// block goal with only the target time moved. A new caller taking a goal from
// anywhere else must check ownership the way AddGoalRevision does.
func insertGoalRevisionTx(ctx context.Context, q execer, rev *GoalRevision) error {
	if rev == nil {
		return errors.New("goal revision must not be nil")
	}
	if err := ValidateGoalRevisionSource(rev.Source); err != nil {
		return err
	}
	if rev.WeekStart == "" {
		return errors.New("goal revision week_start must be set")
	}
	createdAt, err := normaliseTimestamp(rev.CreatedAt, "goal revision created_at")
	if err != nil {
		return err
	}
	goalJSON, err := encryptJSON(rev.Goal, "goal_json")
	if err != nil {
		return err
	}
	encReason, err := encryption.EncryptField(rev.Reason)
	if err != nil {
		return fmt.Errorf("encrypt goal revision reason: %w", err)
	}

	res, err := q.ExecContext(ctx, `
		INSERT INTO stride_goal_revisions
			(macro_plan_id, user_id, week_start, goal_json, reason, source, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, rev.MacroPlanID, rev.UserID, rev.WeekStart, goalJSON, encReason, rev.Source, createdAt)
	if err != nil {
		return fmt.Errorf("insert goal revision: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("goal revision id: %w", err)
	}

	rev.ID = id
	rev.CreatedAt = createdAt
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
		var goalJSON, reason string
		if err := rows.Scan(&r.ID, &r.MacroPlanID, &r.UserID, &r.WeekStart, &goalJSON, &reason,
			&r.Source, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan goal revision: %w", err)
		}
		if err := decryptJSON(goalJSON, &r.Goal, "goal_json"); err != nil {
			return nil, err
		}
		if r.Reason, err = encryption.DecryptField(reason); err != nil {
			return nil, fmt.Errorf("decrypt goal revision reason: %w", err)
		}
		revs = append(revs, r)
	}
	return revs, rows.Err()
}
