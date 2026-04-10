package stride

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/training"
	"github.com/go-chi/chi/v5"
)

// RaceMatchResult describes the outcome of automatic race matching for a
// workout classified as a race.
type RaceMatchResult struct {
	Status     string `json:"status"`      // "linked", "ambiguous", "no_match"
	RaceID     int64  `json:"race_id"`     // set when Status == "linked"
	RaceName   string `json:"race_name"`   // set when Status == "linked"
	Candidates int    `json:"candidates"`  // number of matching races found
}

// TryMatchRaceForWorkout attempts to auto-link a workout classified as a race
// to a matching entry in the user's race calendar. It uses date and distance to
// find candidates. If exactly one race matches, it links the workout and
// populates the race's result_time. If multiple races match, it returns an
// ambiguous result so the caller can flag it for user confirmation.
func TryMatchRaceForWorkout(db *sql.DB, workoutID, userID int64, workoutDate string, distanceMeters float64) (*RaceMatchResult, error) {
	// Extract YYYY-MM-DD from an RFC3339 or date-only string.
	if len(workoutDate) > 10 {
		workoutDate = workoutDate[:10]
	}

	candidates, err := FindMatchingRaces(db, userID, workoutDate, distanceMeters)
	if err != nil {
		return nil, err
	}

	if len(candidates) == 0 {
		return &RaceMatchResult{Status: "no_match"}, nil
	}

	if len(candidates) == 1 {
		if err := LinkWorkoutToRace(db, workoutID, candidates[0].ID, userID); err != nil {
			return nil, err
		}
		return &RaceMatchResult{
			Status:     "linked",
			RaceID:     candidates[0].ID,
			RaceName:   candidates[0].Name,
			Candidates: 1,
		}, nil
	}

	// Multiple matches — ambiguous; caller should flag for user confirmation.
	return &RaceMatchResult{
		Status:     "ambiguous",
		Candidates: len(candidates),
	}, nil
}

// ListEvaluationsHandler returns stride evaluations for the authenticated user.
// Optional query params:
//   - plan_id (integer) — filters to evaluations for that plan
//   - workout_id (integer) — filters to evaluations for that workout
//
// GET /api/stride/evaluations?plan_id=X
// GET /api/stride/evaluations?workout_id=X
// Response: {"evaluations": [...]}
func ListEvaluationsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		var planID *int64
		if raw := r.URL.Query().Get("plan_id"); raw != "" {
			pid, err := strconv.ParseInt(raw, 10, 64)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid plan_id"})
				return
			}
			planID = &pid
		}

		var workoutID *int64
		if raw := r.URL.Query().Get("workout_id"); raw != "" {
			wid, err := strconv.ParseInt(raw, 10, 64)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid workout_id"})
				return
			}
			workoutID = &wid
		}

		if planID != nil && workoutID != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "plan_id and workout_id are mutually exclusive"})
			return
		}

		records, err := ListEvaluations(db, user.ID, planID, workoutID)
		if err != nil {
			log.Printf("stride: list evaluations for user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list evaluations"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"evaluations": records})
	}
}

// TriggerEvaluationHandler manually triggers evaluation of the authenticated user's
// unevaluated workouts from the past 24 hours via the stride AI engine.
// POST /api/stride/evaluate
func TriggerEvaluationHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		evaluated, err := RunUserEvaluation(r.Context(), db, http.DefaultClient, user.ID)
		if err != nil {
			if errors.Is(err, training.ErrClaudeNotEnabled) {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			log.Printf("stride: trigger evaluation for user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "evaluation failed"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"evaluated": evaluated,
			"status":    "ok",
		})
	}
}

// PlanHistoryHandler returns past weeks' training plans with completion metadata
// and monthly compliance rollups.
//
// GET /api/stride/history?limit=12
// Response: {"weeks": [...], "months": [...]}
func PlanHistoryHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		limit := 12
		if raw := r.URL.Query().Get("limit"); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil || n < 1 || n > 52 {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "limit must be between 1 and 52"})
				return
			}
			limit = n
		}

		weeks, months, err := GetPlanHistory(db, user.ID, limit)
		if err != nil {
			log.Printf("stride: plan history for user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load plan history"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"weeks":  weeks,
			"months": months,
		})
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("stride: writeJSON encode error: %v", err)
	}
}

// ListRacesHandler returns all races for the authenticated user.
func ListRacesHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		races, err := ListRaces(db, user.ID)
		if err != nil {
			log.Printf("stride: list races: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list races"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"races": races})
	}
}

// GetRaceHandler returns a single race by ID for the authenticated user.
// GET /api/stride/races/{id}
func GetRaceHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid race ID"})
			return
		}

		race, err := GetRaceByID(db, id, user.ID)
		if err != nil {
			if err == sql.ErrNoRows {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "race not found"})
				return
			}
			log.Printf("stride: get race %d: %v", id, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to get race"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{"race": race})
	}
}

// CreateRaceHandler creates a new race in the race calendar.
// Expects JSON body: {"name":"...","date":"YYYY-MM-DD","distance_m":42195,"target_time":null,"priority":"A","notes":"..."}
func CreateRaceHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		var body struct {
			Name       string  `json:"name"`
			Date       string  `json:"date"`
			DistanceM  float64 `json:"distance_m"`
			TargetTime *int    `json:"target_time"`
			Priority   string  `json:"priority"`
			Notes      string  `json:"notes"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}

		body.Name = strings.TrimSpace(body.Name)
		if body.Name == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
			return
		}
		if body.Date == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "date is required"})
			return
		}
		if _, err := time.Parse("2006-01-02", body.Date); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "date must be in YYYY-MM-DD format"})
			return
		}
		if body.DistanceM <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "distance_m must be positive"})
			return
		}
		if body.Priority == "" {
			body.Priority = "B"
		}
		if body.Priority != "A" && body.Priority != "B" && body.Priority != "C" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "priority must be A, B, or C"})
			return
		}
		if body.TargetTime != nil && *body.TargetTime < 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "target_time must not be negative"})
			return
		}

		race, err := CreateRace(db, user.ID, body.Name, body.Date, body.DistanceM, body.TargetTime, body.Priority, body.Notes)
		if err != nil {
			log.Printf("stride: create race: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create race"})
			return
		}

		writeJSON(w, http.StatusCreated, map[string]any{"race": race})
	}
}

// UpdateRaceHandler updates an existing race.
// Expects JSON body with the same fields as CreateRaceHandler, plus optional "result_time".
func UpdateRaceHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid race ID"})
			return
		}

		var body struct {
			Name       string  `json:"name"`
			Date       string  `json:"date"`
			DistanceM  float64 `json:"distance_m"`
			TargetTime *int    `json:"target_time"`
			Priority   string  `json:"priority"`
			Notes      string  `json:"notes"`
			ResultTime *int    `json:"result_time"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}

		body.Name = strings.TrimSpace(body.Name)
		if body.Name == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
			return
		}
		if body.Date == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "date is required"})
			return
		}
		if _, err := time.Parse("2006-01-02", body.Date); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "date must be in YYYY-MM-DD format"})
			return
		}
		if body.DistanceM <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "distance_m must be positive"})
			return
		}
		if body.Priority == "" {
			body.Priority = "B"
		}
		if body.Priority != "A" && body.Priority != "B" && body.Priority != "C" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "priority must be A, B, or C"})
			return
		}
		if body.TargetTime != nil && *body.TargetTime < 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "target_time must not be negative"})
			return
		}
		if body.ResultTime != nil && *body.ResultTime < 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "result_time must not be negative"})
			return
		}

		race, err := UpdateRace(db, id, user.ID, body.Name, body.Date, body.DistanceM, body.TargetTime, body.Priority, body.Notes, body.ResultTime)
		if err != nil {
			if err == sql.ErrNoRows {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "race not found"})
				return
			}
			log.Printf("stride: update race %d: %v", id, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update race"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{"race": race})
	}
}

// DeleteRaceHandler deletes a race owned by the authenticated user.
func DeleteRaceHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid race ID"})
			return
		}

		if err := DeleteRace(db, id, user.ID); err != nil {
			if err == sql.ErrNoRows {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "race not found"})
				return
			}
			log.Printf("stride: delete race %d: %v", id, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete race"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// ListNotesHandler returns notes for the authenticated user.
// Optional query params: plan_id (integer), status ("active", "consumed", "all").
func ListNotesHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		var planID *int64
		if raw := r.URL.Query().Get("plan_id"); raw != "" {
			pid, err := strconv.ParseInt(raw, 10, 64)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid plan_id"})
				return
			}
			planID = &pid
		}

		status := r.URL.Query().Get("status")

		notes, err := ListNotes(db, user.ID, planID, status)
		if err != nil {
			log.Printf("stride: list notes: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list notes"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"notes": notes})
	}
}

// CreateNoteHandler creates a new note.
// Expects JSON body: {"content":"...","plan_id":null,"target_date":"YYYY-MM-DD"}
func CreateNoteHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		var body struct {
			Content    string `json:"content"`
			PlanID     *int64 `json:"plan_id"`
			TargetDate string `json:"target_date"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}

		body.Content = strings.TrimSpace(body.Content)
		if body.Content == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "content is required"})
			return
		}

		if body.TargetDate != "" {
			if _, err := time.Parse("2006-01-02", body.TargetDate); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "target_date must be in YYYY-MM-DD format"})
				return
			}
		}

		if body.PlanID != nil {
			var exists int
			err := db.QueryRow("SELECT 1 FROM stride_plans WHERE id = ? AND user_id = ?", *body.PlanID, user.ID).Scan(&exists)
			if err == sql.ErrNoRows {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "plan not found"})
				return
			}
			if err != nil {
				log.Printf("stride: check plan ownership: %v", err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to validate plan"})
				return
			}
		}

		note, err := CreateNote(db, user.ID, body.PlanID, body.Content, body.TargetDate)
		if err != nil {
			log.Printf("stride: create note: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create note"})
			return
		}

		writeJSON(w, http.StatusCreated, map[string]any{"note": note})
	}
}

// DeleteNoteHandler deletes a note owned by the authenticated user.
func DeleteNoteHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid note ID"})
			return
		}

		if err := DeleteNote(db, id, user.ID); err != nil {
			if err == sql.ErrNoRows {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "note not found"})
				return
			}
			log.Printf("stride: delete note %d: %v", id, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete note"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// ListPlansHandler returns paginated stride plans for the authenticated user, newest first.
// Query params: limit (default 10, max 50), offset (default 0).
func ListPlansHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		limit := 10
		offset := 0
		if raw := r.URL.Query().Get("limit"); raw != "" {
			if n, err := strconv.Atoi(raw); err == nil && n > 0 {
				limit = n
				if limit > 50 {
					limit = 50
				}
			}
		}
		if raw := r.URL.Query().Get("offset"); raw != "" {
			if n, err := strconv.Atoi(raw); err == nil && n >= 0 {
				offset = n
			}
		}

		plans, total, err := ListPlans(db, user.ID, limit, offset)
		if err != nil {
			log.Printf("stride: list plans: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list plans"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"plans":  plans,
			"total":  total,
			"limit":  limit,
			"offset": offset,
		})
	}
}

// GetCurrentPlanHandler returns the plan for the current week, or 404 if none exists.
func GetCurrentPlanHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		today := time.Now().UTC().Format("2006-01-02")

		plan, err := GetCurrentPlan(db, user.ID, today)
		if err != nil {
			log.Printf("stride: get current plan: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to get current plan"})
			return
		}
		if plan == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "no plan for current week"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"plan": plan})
	}
}

// GetPlanHandler returns a single plan by ID.
func GetPlanHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid plan ID"})
			return
		}

		plan, err := GetPlanByID(db, id, user.ID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "plan not found"})
				return
			}
			log.Printf("stride: get plan %d: %v", id, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to get plan"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"plan": plan})
	}
}

// GeneratePlanHandler triggers synchronous plan generation via Claude AI and returns the new plan.
// POST /api/stride/plans/generate
func GeneratePlanHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		weekMode := r.URL.Query().Get("week")
		if weekMode == "" {
			weekMode = "next"
		} else if weekMode != "current" && weekMode != "next" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": `invalid week: must be "current" or "next"`})
			return
		}

		if err := GeneratePlan(r.Context(), db, user.ID, weekMode); err != nil {
			if errors.Is(err, training.ErrClaudeNotEnabled) {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			log.Printf("stride: generate plan for user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to generate plan"})
			return
		}

		var weekStart, weekEnd string
		if weekMode == "current" {
			weekStart, weekEnd = currentWeek()
		} else {
			weekStart, weekEnd = upcomingWeek()
		}
		plan, err := getPlanByWeekStart(db, user.ID, weekStart)
		if err != nil {
			// GeneratePlan returned nil but no plan found — stride may not be enabled.
			if errors.Is(err, sql.ErrNoRows) {
				writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "stride is not enabled — enable it in settings"})
				return
			}
			log.Printf("stride: fetch generated plan for user %d week %s..%s: %v", user.ID, weekStart, weekEnd, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "plan generated but failed to retrieve"})
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"plan": plan})
	}
}
