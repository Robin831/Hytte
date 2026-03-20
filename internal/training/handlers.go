package training

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/go-chi/chi/v5"
)

const maxUploadSize = 50 << 20 // 50 MB

// claudeSemaphore caps the number of concurrent background Claude CLI processes.
var claudeSemaphore = make(chan struct{}, 3)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// UploadHandler handles POST /api/training/upload for .fit file imports.
func UploadHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			var maxBytesErr *http.MaxBytesError
			if errors.As(err, &maxBytesErr) {
				writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "request too large"})
			} else {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid multipart form"})
			}
			return
		}
		defer r.MultipartForm.RemoveAll()

		files := r.MultipartForm.File["files"]
		if len(files) == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no files provided"})
			return
		}

		var imported []Workout
		var errs []string

		for _, fh := range files {
			if !strings.HasSuffix(strings.ToLower(fh.Filename), ".fit") {
				errs = append(errs, fmt.Sprintf("%s: not a .fit file", fh.Filename))
				continue
			}

			f, err := fh.Open()
			if err != nil {
				errs = append(errs, fmt.Sprintf("%s: failed to open", fh.Filename))
				continue
			}

			pw, hash, err := ParseFIT(f)
			f.Close()
			if err != nil {
				errs = append(errs, fmt.Sprintf("%s: %v", fh.Filename, err))
				continue
			}

			// Check for duplicates.
			exists, err := HashExists(db, user.ID, hash)
			if err != nil {
				errs = append(errs, fmt.Sprintf("%s: database error", fh.Filename))
				continue
			}
			if exists {
				errs = append(errs, fmt.Sprintf("%s: already imported", fh.Filename))
				continue
			}

			workout, err := Create(db, user.ID, pw, hash)
			if err != nil {
				errs = append(errs, fmt.Sprintf("%s: %v", fh.Filename, err))
				continue
			}
			// Don't include samples in upload response.
			workout.Samples = nil
			imported = append(imported, *workout)
		}

		scheduleBackgroundAnalysis(db, user.ID, user.IsAdmin, imported)

		writeJSON(w, http.StatusCreated, map[string]any{
			"imported": imported,
			"errors":   errs,
		})
	}
}

// scheduleBackgroundAnalysis triggers async Claude analysis for each imported workout
// when the user has admin access and the claude_ai feature flag is active.
// RunClaudeAnalysis handles config loading and skips gracefully when Claude is disabled.
func scheduleBackgroundAnalysis(db *sql.DB, userID int64, isAdmin bool, workouts []Workout) {
	if !isAdmin || len(workouts) == 0 {
		return
	}
	features, err := auth.GetUserFeatures(db, userID, isAdmin)
	if err != nil {
		log.Printf("Failed to load user features for Claude trigger (user %d): %v", userID, err)
		return
	}
	if !features["claude_ai"] {
		return
	}
	for _, w := range workouts {
		workoutID := w.ID
		if err := UpdateAnalysisStatus(db, workoutID, "pending"); err != nil {
			log.Printf("Failed to set pending analysis status for workout %d: %v", workoutID, err)
			continue
		}
		go func() {
			claudeSemaphore <- struct{}{} // blocks until capacity is available
			defer func() { <-claudeSemaphore }()
			bgCtx := context.Background()
			if err := RunClaudeAnalysis(bgCtx, db, workoutID, userID); err != nil {
				if !errors.Is(err, ErrClaudeNotEnabled) {
					log.Printf("Background Claude analysis failed for workout %d: %v", workoutID, err)
				}
				if updateErr := UpdateAnalysisStatus(db, workoutID, "failed"); updateErr != nil {
					log.Printf("Failed to set failed analysis status for workout %d: %v", workoutID, updateErr)
				}
			} else {
				if updateErr := UpdateAnalysisStatus(db, workoutID, "completed"); updateErr != nil {
					log.Printf("Failed to set completed analysis status for workout %d: %v", workoutID, updateErr)
				}
			}
		}()
	}
}

// ListHandler handles GET /api/training/workouts.
func ListHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		workouts, err := List(db, user.ID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load workouts"})
			return
		}
		if workouts == nil {
			workouts = []Workout{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"workouts": workouts})
	}
}

// GetHandler handles GET /api/training/workouts/{id}.
func GetHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
			return
		}

		workout, err := GetByID(db, id, user.ID)
		if err == sql.ErrNoRows {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "workout not found"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load workout"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"workout": workout})
	}
}

// DeleteHandler handles DELETE /api/training/workouts/{id}.
func DeleteHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
			return
		}

		if err := Delete(db, id, user.ID); err == sql.ErrNoRows {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "workout not found"})
			return
		} else if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete workout"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// UpdateHandler handles PUT /api/training/workouts/{id} for title and tags.
func UpdateHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
			return
		}

		var body struct {
			Title string   `json:"title"`
			Tags  []string `json:"tags"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}

		if body.Title != "" {
			if err := UpdateTitle(db, id, user.ID, body.Title); err == sql.ErrNoRows {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "workout not found"})
				return
			} else if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update title"})
				return
			}
		}

		if body.Tags != nil {
			if err := UpdateTags(db, id, user.ID, body.Tags); err == sql.ErrNoRows {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "workout not found"})
				return
			} else if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update tags"})
				return
			}
		}

		workout, err := GetByID(db, id, user.ID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load workout"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"workout": workout})
	}
}

// CompareHandler handles GET /api/training/compare?a={id}&b={id}[&laps_a=0,1,3&laps_b=0,2,4].
// The optional laps_a and laps_b query params are comma-separated 0-based lap
// indices specifying which laps to pair for comparison. Both must be provided
// together and have the same length.
func CompareHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		idA, errA := strconv.ParseInt(r.URL.Query().Get("a"), 10, 64)
		idB, errB := strconv.ParseInt(r.URL.Query().Get("b"), 10, 64)
		if errA != nil || errB != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "both 'a' and 'b' workout IDs are required"})
			return
		}

		hasLapsA := r.URL.Query().Has("laps_a")
		hasLapsB := r.URL.Query().Has("laps_b")
		// Both must be provided together or both omitted.
		if hasLapsA != hasLapsB {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "laps_a and laps_b must both be provided or both omitted"})
			return
		}
		var lapsA, lapsB []int
		if hasLapsA {
			rawA := r.URL.Query().Get("laps_a")
			rawB := r.URL.Query().Get("laps_b")
			if rawA == "" || rawB == "" {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "laps_a and laps_b must not be empty when provided"})
				return
			}
			var errLA, errLB error
			lapsA, errLA = parseIntList(rawA)
			lapsB, errLB = parseIntList(rawB)
			if errLA != nil || errLB != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "laps_a and laps_b must be comma-separated integers"})
				return
			}
		}

		result, err := CompareWorkouts(db, idA, idB, user.ID, lapsA, lapsB)
		if err == sql.ErrNoRows {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "workout not found"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "comparison failed"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"comparison": result})
	}
}

// parseIntList parses a comma-separated string of integers. Returns nil, nil for empty input.
func parseIntList(s string) ([]int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	parts := strings.Split(s, ",")
	result := make([]int, len(parts))
	for i, p := range parts {
		v, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			return nil, err
		}
		result[i] = v
	}
	return result, nil
}

// SimilarHandler handles GET /api/training/workouts/{id}/similar.
func SimilarHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
			return
		}

		similar, err := FindSimilarWorkouts(db, id, user.ID)
		if err == sql.ErrNoRows {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "workout not found"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to find similar workouts"})
			return
		}
		if similar == nil {
			similar = []Workout{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"similar": similar})
	}
}

// SummaryHandler handles GET /api/training/summary (weekly aggregates).
func SummaryHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		summaries, err := WeeklySummaries(db, user.ID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load summaries"})
			return
		}
		if summaries == nil {
			summaries = []WeeklySummary{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"summaries": summaries})
	}
}

// ProgressionHandler handles GET /api/training/progression.
func ProgressionHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		groups, err := GetProgression(db, user.ID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load progression data"})
			return
		}
		if groups == nil {
			groups = []ProgressionGroup{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"groups": groups})
	}
}

// ZonesHandler handles GET /api/training/workouts/{id}/zones?threshold_hr=N.
func ZonesHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
			return
		}

		thresholdHR := 180
		if v := r.URL.Query().Get("threshold_hr"); v != "" {
			if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
				thresholdHR = parsed
			}
		}

		zones, err := GetZoneDistribution(db, id, user.ID, thresholdHR)
		if err == sql.ErrNoRows {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "workout not found"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to calculate zones"})
			return
		}
		if zones == nil {
			zones = []ZoneDistribution{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"zones": zones})
	}
}
