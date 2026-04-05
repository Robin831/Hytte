package stride

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/go-chi/chi/v5"
)

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
// Optional query param: plan_id (integer).
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

		notes, err := ListNotes(db, user.ID, planID)
		if err != nil {
			log.Printf("stride: list notes: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list notes"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"notes": notes})
	}
}

// CreateNoteHandler creates a new note.
// Expects JSON body: {"content":"...","plan_id":null}
func CreateNoteHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		var body struct {
			Content string `json:"content"`
			PlanID  *int64 `json:"plan_id"`
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

		note, err := CreateNote(db, user.ID, body.PlanID, body.Content)
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
