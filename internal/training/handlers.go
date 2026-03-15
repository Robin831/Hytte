package training

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/go-chi/chi/v5"
)

const maxUploadSize = 50 << 20 // 50 MB

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
		var errors []string

		for _, fh := range files {
			if !strings.HasSuffix(strings.ToLower(fh.Filename), ".fit") {
				errors = append(errors, fmt.Sprintf("%s: not a .fit file", fh.Filename))
				continue
			}

			f, err := fh.Open()
			if err != nil {
				errors = append(errors, fmt.Sprintf("%s: failed to open", fh.Filename))
				continue
			}

			pw, hash, err := ParseFIT(f)
			f.Close()
			if err != nil {
				errors = append(errors, fmt.Sprintf("%s: %v", fh.Filename, err))
				continue
			}

			// Check for duplicates.
			exists, err := HashExists(db, user.ID, hash)
			if err != nil {
				errors = append(errors, fmt.Sprintf("%s: database error", fh.Filename))
				continue
			}
			if exists {
				errors = append(errors, fmt.Sprintf("%s: already imported", fh.Filename))
				continue
			}

			workout, err := Create(db, user.ID, pw, hash)
			if err != nil {
				errors = append(errors, fmt.Sprintf("%s: %v", fh.Filename, err))
				continue
			}
			// Don't include samples in upload response.
			workout.Samples = nil
			imported = append(imported, *workout)
		}

		writeJSON(w, http.StatusCreated, map[string]any{
			"imported": imported,
			"errors":   errors,
		})
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

// CompareHandler handles GET /api/training/compare?a={id}&b={id}.
func CompareHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		idA, errA := strconv.ParseInt(r.URL.Query().Get("a"), 10, 64)
		idB, errB := strconv.ParseInt(r.URL.Query().Get("b"), 10, 64)
		if errA != nil || errB != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "both 'a' and 'b' workout IDs are required"})
			return
		}

		result, err := CompareWorkouts(db, idA, idB, user.ID)
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
