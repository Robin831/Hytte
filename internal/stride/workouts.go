package stride

// The workout library: reusable session definitions the weekly plan generator
// draws from, so the coach rotates through curated variety instead of
// re-inventing (in practice: repeating) the same intervals every week. One
// workout is pinned as the weekly reference session — the fixed benchmark the
// plan always includes — and the rest carry ratings, usage counts and the
// training blocks they suit, all of which the plan prompt sees.

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/encryption"
)

// validWorkoutTypes is the closed session-type set, matching the plan
// generator's session vocabulary.
var validWorkoutTypes = map[string]bool{
	"threshold": true,
	"hard":      true,
	"easy":      true,
	"long_run":  true,
	"strides":   true,
}

// validBlocks is the training-block taxonomy, matching TrainingBlockTimeline.
var validBlocks = map[string]bool{
	"base":  true,
	"build": true,
	"peak":  true,
	"taper": true,
}

// LibraryWorkout is one library entry.
type LibraryWorkout struct {
	ID          int64    `json:"id"`
	Name        string   `json:"name"`
	WorkoutType string   `json:"workout_type"`
	Warmup      string   `json:"warmup"`
	MainSet     string   `json:"main_set"`
	Cooldown    string   `json:"cooldown"`
	Strides     string   `json:"strides"`
	TargetHRCap string   `json:"target_hr_cap"`
	Description string   `json:"description"`
	Source      string   `json:"source"` // manual | ai
	Rating      int      `json:"rating"` // 0 = unrated, 1..5
	TimesUsed   int      `json:"times_used"`
	LastUsedAt  string   `json:"last_used_at,omitempty"`
	IsReference bool     `json:"is_reference"`
	Archived    bool     `json:"archived"`
	Blocks      []string `json:"blocks"`
	CreatedAt   string   `json:"created_at"`
}

// ValidateLibraryWorkout normalizes and validates a workout payload. Returns a
// user-facing error string ("" when valid).
func ValidateLibraryWorkout(w *LibraryWorkout) string {
	w.Name = strings.TrimSpace(w.Name)
	w.MainSet = strings.TrimSpace(w.MainSet)
	if w.Name == "" {
		return "name is required"
	}
	if w.MainSet == "" {
		return "main_set is required"
	}
	if w.WorkoutType != "" && !validWorkoutTypes[w.WorkoutType] {
		return "invalid workout_type"
	}
	if w.Source != "manual" && w.Source != "ai" {
		w.Source = "manual"
	}
	if w.Rating < 0 || w.Rating > 5 {
		return "rating must be 0-5"
	}
	seen := map[string]bool{}
	blocks := make([]string, 0, len(w.Blocks))
	for _, b := range w.Blocks {
		b = strings.ToLower(strings.TrimSpace(b))
		if b == "" || seen[b] {
			continue
		}
		if !validBlocks[b] {
			return "invalid training block: " + b
		}
		seen[b] = true
		blocks = append(blocks, b)
	}
	sort.Strings(blocks)
	w.Blocks = blocks
	return ""
}

// encryptField wraps encryption for optional prose fields ("" stays "").
func encryptField(s string) (string, error) {
	if s == "" {
		return "", nil
	}
	return encryption.EncryptField(s)
}

// decryptField tolerates legacy plaintext.
func decryptField(s string) string {
	if s == "" {
		return ""
	}
	if dec, err := encryption.DecryptField(s); err == nil {
		return dec
	}
	return s
}

// InsertLibraryWorkout persists a new workout (and its block links).
func InsertLibraryWorkout(ctx context.Context, db *sql.DB, userID int64, w *LibraryWorkout) error {
	encName, err := encryptField(w.Name)
	if err != nil {
		return fmt.Errorf("encrypt name: %w", err)
	}
	encFields := make([]string, 6)
	for i, v := range []string{w.Warmup, w.MainSet, w.Cooldown, w.Strides, w.TargetHRCap, w.Description} {
		if encFields[i], err = encryptField(v); err != nil {
			return fmt.Errorf("encrypt workout field: %w", err)
		}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if w.IsReference {
		if _, err := tx.ExecContext(ctx, `UPDATE stride_workouts SET is_reference = 0 WHERE user_id = ?`, userID); err != nil {
			return fmt.Errorf("clear reference: %w", err)
		}
	}
	res, err := tx.ExecContext(ctx, `
		INSERT INTO stride_workouts
		  (user_id, name, workout_type, warmup, main_set, cooldown, strides,
		   target_hr_cap, description, source, rating, is_reference, archived, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?)`,
		userID, encName, w.WorkoutType,
		encFields[0], encFields[1], encFields[2], encFields[3], encFields[4], encFields[5],
		w.Source, w.Rating, boolToInt(w.IsReference), now,
	)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	w.ID = id
	w.CreatedAt = now
	for _, b := range w.Blocks {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO stride_workout_blocks (workout_id, block) VALUES (?, ?)`, id, b); err != nil {
			return fmt.Errorf("insert block: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

// UpdateLibraryWorkout rewrites an existing workout the user owns.
func UpdateLibraryWorkout(ctx context.Context, db *sql.DB, userID int64, w *LibraryWorkout) error {
	encName, err := encryptField(w.Name)
	if err != nil {
		return fmt.Errorf("encrypt name: %w", err)
	}
	encFields := make([]string, 6)
	for i, v := range []string{w.Warmup, w.MainSet, w.Cooldown, w.Strides, w.TargetHRCap, w.Description} {
		if encFields[i], err = encryptField(v); err != nil {
			return fmt.Errorf("encrypt workout field: %w", err)
		}
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if w.IsReference {
		if _, err := tx.ExecContext(ctx, `UPDATE stride_workouts SET is_reference = 0 WHERE user_id = ? AND id != ?`, userID, w.ID); err != nil {
			return fmt.Errorf("clear reference: %w", err)
		}
	}
	res, err := tx.ExecContext(ctx, `
		UPDATE stride_workouts
		SET name = ?, workout_type = ?, warmup = ?, main_set = ?, cooldown = ?,
		    strides = ?, target_hr_cap = ?, description = ?, rating = ?,
		    is_reference = ?, archived = ?
		WHERE id = ? AND user_id = ?`,
		encName, w.WorkoutType,
		encFields[0], encFields[1], encFields[2], encFields[3], encFields[4], encFields[5],
		w.Rating, boolToInt(w.IsReference), boolToInt(w.Archived),
		w.ID, userID,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM stride_workout_blocks WHERE workout_id = ?`, w.ID); err != nil {
		return fmt.Errorf("clear blocks: %w", err)
	}
	for _, b := range w.Blocks {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO stride_workout_blocks (workout_id, block) VALUES (?, ?)`, w.ID, b); err != nil {
			return fmt.Errorf("insert block: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

// DeleteLibraryWorkout removes a workout the user owns.
func DeleteLibraryWorkout(ctx context.Context, db *sql.DB, userID, id int64) error {
	res, err := db.ExecContext(ctx, `DELETE FROM stride_workouts WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ListLibraryWorkouts returns the user's workouts (blocks attached), reference
// first, then by rating desc, then newest. includeArchived controls whether
// archived entries appear. The returned slice is never nil.
func ListLibraryWorkouts(ctx context.Context, db *sql.DB, userID int64, includeArchived bool) ([]LibraryWorkout, error) {
	q := `
		SELECT id, name, workout_type, warmup, main_set, cooldown, strides,
		       target_hr_cap, description, source, rating, times_used,
		       COALESCE(last_used_at, ''), is_reference, archived, created_at
		FROM stride_workouts
		WHERE user_id = ?`
	if !includeArchived {
		q += ` AND archived = 0`
	}
	q += ` ORDER BY is_reference DESC, rating DESC, created_at DESC`
	rows, err := db.QueryContext(ctx, q, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]LibraryWorkout, 0)
	ids := make([]int64, 0)
	for rows.Next() {
		var w LibraryWorkout
		var isRef, archived int
		if err := rows.Scan(&w.ID, &w.Name, &w.WorkoutType, &w.Warmup, &w.MainSet,
			&w.Cooldown, &w.Strides, &w.TargetHRCap, &w.Description, &w.Source,
			&w.Rating, &w.TimesUsed, &w.LastUsedAt, &isRef, &archived, &w.CreatedAt); err != nil {
			return nil, err
		}
		w.Name = decryptField(w.Name)
		w.Warmup = decryptField(w.Warmup)
		w.MainSet = decryptField(w.MainSet)
		w.Cooldown = decryptField(w.Cooldown)
		w.Strides = decryptField(w.Strides)
		w.TargetHRCap = decryptField(w.TargetHRCap)
		w.Description = decryptField(w.Description)
		w.IsReference = isRef != 0
		w.Archived = archived != 0
		w.Blocks = []string{}
		out = append(out, w)
		ids = append(ids, w.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return out, nil
	}

	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	bRows, err := db.QueryContext(ctx,
		`SELECT workout_id, block FROM stride_workout_blocks WHERE workout_id IN (`+strings.Join(placeholders, ",")+`) ORDER BY block`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer bRows.Close()
	blocksByID := map[int64][]string{}
	for bRows.Next() {
		var id int64
		var b string
		if err := bRows.Scan(&id, &b); err != nil {
			return nil, err
		}
		blocksByID[id] = append(blocksByID[id], b)
	}
	if err := bRows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		if bs, ok := blocksByID[out[i].ID]; ok {
			out[i].Blocks = bs
		}
	}
	return out, nil
}

// RecordLibraryUsage bumps times_used and last_used_at for the given workout
// ids (deduplicated). Called after a generated plan referencing them is
// stored. weekStart stamps last_used_at so "used three weeks ago" is derived
// from plan weeks, not wall clock.
func RecordLibraryUsage(ctx context.Context, db *sql.DB, userID int64, ids []int64, weekStart string) {
	seen := map[int64]bool{}
	for _, id := range ids {
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		if _, err := db.ExecContext(ctx, `
			UPDATE stride_workouts
			SET times_used = times_used + 1, last_used_at = ?
			WHERE id = ? AND user_id = ?`,
			weekStart, id, userID,
		); err != nil {
			// Best-effort telemetry — never fails a stored plan.
			continue
		}
	}
}

// SeedReferenceWorkout inserts the default weekly reference session for a user
// whose library is empty: 6x6min at threshold with 1min jog recoveries — the
// classic Bakken-style half-marathon benchmark, matching the doctrine in the
// plan prompt. No-op when the user already has any workout.
func SeedReferenceWorkout(ctx context.Context, db *sql.DB, userID int64) error {
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM stride_workouts WHERE user_id = ?`, userID).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	ref := &LibraryWorkout{
		Name:        "6x6min threshold (reference)",
		WorkoutType: "threshold",
		Warmup:      "15min easy jog",
		MainSet:     "6x6min at threshold pace, 1min jog recovery",
		Cooldown:    "10min easy jog",
		TargetHRCap: "below 88% of max HR during reps",
		Description: "The weekly benchmark session: same structure every time so week-to-week pace and HR are directly comparable. Half-marathon anchor workout.",
		Source:      "manual",
		IsReference: true,
		Blocks:      []string{"base", "build", "peak"},
	}
	if msg := ValidateLibraryWorkout(ref); msg != "" {
		return fmt.Errorf("seed reference: %s", msg)
	}
	return InsertLibraryWorkout(ctx, db, userID, ref)
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
