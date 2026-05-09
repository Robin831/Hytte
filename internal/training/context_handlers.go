package training

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/encryption"
	"github.com/go-chi/chi/v5"
)

// GetWorkoutContext handles GET /api/training/workouts/{id}/context.
// Returns 404 when the workout doesn't exist, isn't owned by the user, or has
// no context row yet.
func GetWorkoutContext(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		workoutID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
			return
		}

		if err := assertWorkoutOwnedBy(db, workoutID, user.ID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "workout not found"})
				return
			}
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load workout"})
			return
		}

		ctx, err := loadWorkoutContext(db, workoutID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "context not found"})
				return
			}
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load context"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"context": ctx})
	}
}

// putWorkoutContextRequest is the request body for PUT /context. All fields are
// pointers so that absent JSON fields are distinguishable from zero values.
// Omitted fields leave the existing persisted value unchanged.
type putWorkoutContextRequest struct {
	Surface     *string         `json:"surface"`
	RunType     *string         `json:"run_type"`
	HRSource    *string         `json:"hr_source"`
	FeelNotes   *string         `json:"feel_notes"`
	SpeedPlan   *[]SpeedSegment `json:"speed_plan"`
	CompletedAt *time.Time      `json:"completed_at"`
}

// PutWorkoutContext handles PUT /api/training/workouts/{id}/context.
// Creates the context row if it doesn't exist, otherwise merges the provided
// fields into the existing row so that omitted fields are not wiped.
// Returns the persisted context (after encryption round-trip).
func PutWorkoutContext(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		workoutID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
			return
		}

		if err := assertWorkoutOwnedBy(db, workoutID, user.ID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "workout not found"})
				return
			}
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load workout"})
			return
		}

		var req putWorkoutContextRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}

		// Load existing context or start with defaults so omitted request
		// fields preserve their previously saved values.
		ctx, err := loadWorkoutContext(db, workoutID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load context"})
			return
		}
		if ctx == nil {
			ctx = &WorkoutContext{WorkoutID: workoutID, SpeedPlan: []SpeedSegment{}}
		}

		if req.Surface != nil {
			ctx.Surface = *req.Surface
		}
		if req.RunType != nil {
			ctx.RunType = *req.RunType
		}
		if req.HRSource != nil {
			ctx.HRSource = *req.HRSource
		}
		if req.FeelNotes != nil {
			ctx.FeelNotes = *req.FeelNotes
		}
		if req.SpeedPlan != nil {
			ctx.SpeedPlan = *req.SpeedPlan
		}
		if req.CompletedAt != nil {
			ctx.CompletedAt = req.CompletedAt
		}
		if ctx.SpeedPlan == nil {
			ctx.SpeedPlan = []SpeedSegment{}
		}

		if err := saveWorkoutContext(db, ctx); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save context"})
			return
		}

		saved, err := loadWorkoutContext(db, workoutID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load context"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"context": saved})
	}
}

// assertWorkoutOwnedBy returns sql.ErrNoRows when the workout doesn't exist or
// isn't owned by the given user. Used as a precondition for context reads/writes.
func assertWorkoutOwnedBy(db *sql.DB, workoutID, userID int64) error {
	var exists int
	return db.QueryRow(
		`SELECT 1 FROM workouts WHERE id = ? AND user_id = ?`,
		workoutID, userID,
	).Scan(&exists)
}

// loadWorkoutContext fetches and decrypts the workout_context row for a workout.
// Returns sql.ErrNoRows when no context exists for the workout.
func loadWorkoutContext(db *sql.DB, workoutID int64) (*WorkoutContext, error) {
	var (
		feelEnc, planEnc string
		completedAt      string
	)
	ctx := &WorkoutContext{WorkoutID: workoutID, SpeedPlan: []SpeedSegment{}}
	err := db.QueryRow(`
		SELECT surface, run_type, hr_source, feel_notes, speed_plan, completed_at
		FROM workout_context
		WHERE workout_id = ?`, workoutID).Scan(
		&ctx.Surface, &ctx.RunType, &ctx.HRSource, &feelEnc, &planEnc, &completedAt,
	)
	if err != nil {
		return nil, err
	}

	feelNotes, err := encryption.DecryptField(feelEnc)
	if err != nil {
		return nil, err
	}
	ctx.FeelNotes = feelNotes

	planJSON, err := encryption.DecryptField(planEnc)
	if err != nil {
		return nil, err
	}
	if planJSON != "" {
		var segments []SpeedSegment
		if err := json.Unmarshal([]byte(planJSON), &segments); err != nil {
			return nil, err
		}
		if segments == nil {
			segments = []SpeedSegment{}
		}
		ctx.SpeedPlan = segments
	}

	if completedAt != "" {
		if parsed, err := time.Parse(time.RFC3339, completedAt); err == nil {
			ctx.CompletedAt = &parsed
		}
	}

	return ctx, nil
}

// saveWorkoutContext upserts the workout_context row. feel_notes and the
// JSON-encoded speed_plan are encrypted before storage.
func saveWorkoutContext(db *sql.DB, ctx *WorkoutContext) error {
	feelEnc, err := encryption.EncryptField(ctx.FeelNotes)
	if err != nil {
		return err
	}

	var planJSON string
	if len(ctx.SpeedPlan) > 0 {
		raw, err := json.Marshal(ctx.SpeedPlan)
		if err != nil {
			return err
		}
		planJSON = string(raw)
	}
	planEnc, err := encryption.EncryptField(planJSON)
	if err != nil {
		return err
	}

	var completedAt string
	if ctx.CompletedAt != nil {
		completedAt = ctx.CompletedAt.UTC().Format(time.RFC3339)
	}

	_, err = db.Exec(`
		INSERT INTO workout_context (workout_id, surface, run_type, hr_source, feel_notes, speed_plan, completed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(workout_id) DO UPDATE SET
			surface      = excluded.surface,
			run_type     = excluded.run_type,
			hr_source    = excluded.hr_source,
			feel_notes   = excluded.feel_notes,
			speed_plan   = excluded.speed_plan,
			completed_at = excluded.completed_at`,
		ctx.WorkoutID, ctx.Surface, ctx.RunType, ctx.HRSource, feelEnc, planEnc, completedAt,
	)
	return err
}
