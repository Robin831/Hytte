package stride

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
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/training"
	"github.com/go-chi/chi/v5"
)

// ListWorkoutsHandler handles GET /api/stride/workouts. Seeds the default
// 6x6min reference session on first use so the library (and the coach) always
// have the weekly benchmark.
func ListWorkoutsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if err := SeedReferenceWorkout(r.Context(), db, user.ID); err != nil {
			log.Printf("stride workouts: seed reference for user %d: %v", user.ID, err)
		}
		includeArchived := r.URL.Query().Get("archived") == "true"
		workouts, err := ListLibraryWorkouts(r.Context(), db, user.ID, includeArchived)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load workouts"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"workouts": workouts})
	}
}

// CreateWorkoutHandler handles POST /api/stride/workouts.
func CreateWorkoutHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		var payload LibraryWorkout
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		if msg := ValidateLibraryWorkout(&payload); msg != "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": msg})
			return
		}
		if err := InsertLibraryWorkout(r.Context(), db, user.ID, &payload); err != nil {
			log.Printf("stride workouts: insert for user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save workout"})
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"workout": payload})
	}
}

// UpdateWorkoutHandler handles PUT /api/stride/workouts/{id}.
func UpdateWorkoutHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil || id <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid workout id"})
			return
		}
		var payload LibraryWorkout
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		payload.ID = id
		if msg := ValidateLibraryWorkout(&payload); msg != "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": msg})
			return
		}
		if err := UpdateLibraryWorkout(r.Context(), db, user.ID, &payload); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "workout not found"})
				return
			}
			log.Printf("stride workouts: update %d for user %d: %v", id, user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update workout"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"workout": payload})
	}
}

// DeleteWorkoutHandler handles DELETE /api/stride/workouts/{id}.
func DeleteWorkoutHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil || id <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid workout id"})
			return
		}
		if err := DeleteLibraryWorkout(r.Context(), db, user.ID, id); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "workout not found"})
				return
			}
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete workout"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
	}
}

// workoutChatMessage is one turn of the generator conversation, held entirely
// by the client — the durable artifact is the saved library row, not the chat.
type workoutChatMessage struct {
	Role    string `json:"role"` // "user" | "coach"
	Content string `json:"content"`
}

// GenerateWorkoutHandler handles POST /api/stride/workouts/generate: one
// conversational turn of the AI workout designer. The client sends the whole
// message history; the coach replies with prose plus, when it has enough to
// go on, a structured workout draft the client can save to the library.
func GenerateWorkoutHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		var payload struct {
			Messages []workoutChatMessage `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		if len(payload.Messages) == 0 || len(payload.Messages) > 40 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "messages must contain 1-40 entries"})
			return
		}

		cfg, err := training.LoadClaudeConfig(db, user.ID)
		if err != nil || !cfg.Enabled {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "AI is not enabled"})
			return
		}

		prompt := buildWorkoutChatPrompt(db, user.ID, payload.Messages)
		ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
		defer cancel()
		response, err := runPromptFunc(ctx, cfg, prompt)
		if err != nil {
			log.Printf("stride workouts: generate for user %d: %v", user.ID, err)
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "the coach did not answer; try again"})
			return
		}

		reply, draft := parseWorkoutChatResponse(response)
		resp := map[string]any{"reply": reply}
		if draft != nil {
			if msg := ValidateLibraryWorkout(draft); msg == "" {
				resp["workout"] = draft
			}
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

// buildWorkoutChatPrompt renders the designer conversation: athlete profile
// for grounding, the library (so the coach designs variety, not duplicates),
// the transcript, and the reply contract.
func buildWorkoutChatPrompt(db *sql.DB, userID int64, messages []workoutChatMessage) string {
	var sb strings.Builder
	sb.WriteString(`You are a running coach designing ONE reusable workout for the athlete's workout library, in conversation with the athlete. Sessions follow the Marius Bakken school: controlled threshold work, strict easy-day discipline, quality over heroics.

Reply with a JSON object and nothing else:
{"reply": "your conversational reply to the athlete",
 "workout": {"name": "...", "workout_type": "threshold|hard|easy|long_run|strides", "warmup": "...", "main_set": "...", "cooldown": "...", "strides": "", "target_hr_cap": "...", "description": "why/when to use it", "blocks": ["base","build"]}}

- Include "workout" only when you have enough to propose a concrete session; while you still need answers, ask (briefly) and omit it.
- Refine the previous draft when the athlete asks for changes.
- blocks lists the training blocks the session suits: base, build, peak, taper.
- Keep main_set precise enough to execute (reps, durations/distances, paces or HR anchors, recoveries).

`)
	profile := training.BuildUserProfileBlock(db, userID)
	if profile != "" {
		sb.WriteString("## Athlete Profile\n")
		sb.WriteString(profile)
		sb.WriteString("\n")
	}
	if workouts, err := ListLibraryWorkouts(context.Background(), db, userID, false); err == nil && len(workouts) > 0 {
		sb.WriteString("## Existing Library (design something that adds variety, not a duplicate)\n")
		for _, lw := range workouts {
			fmt.Fprintf(&sb, "- %s (%s): %s\n", lw.Name, lw.WorkoutType, lw.MainSet)
		}
		sb.WriteString("\n")
	}
	sb.WriteString("## Conversation\n")
	for _, m := range messages {
		role := "Athlete"
		if m.Role == "coach" {
			role = "Coach"
		}
		content := m.Content
		if len(content) > 2000 {
			content = content[:2000]
		}
		fmt.Fprintf(&sb, "%s: %s\n", role, strings.ReplaceAll(content, "```", "'''"))
	}
	sb.WriteString("\nRespond with the JSON object now.")
	return sb.String()
}

// parseWorkoutChatResponse extracts the coach reply and optional draft.
func parseWorkoutChatResponse(response string) (string, *LibraryWorkout) {
	response = strings.TrimSpace(response)
	if strings.HasPrefix(response, "```") {
		lines := strings.Split(response, "\n")
		if len(lines) >= 3 {
			response = strings.Join(lines[1:len(lines)-1], "\n")
		}
	}
	if !strings.HasPrefix(strings.TrimSpace(response), "{") {
		start := strings.Index(response, "{")
		end := strings.LastIndex(response, "}")
		if start < 0 || end <= start {
			return response, nil // model answered in prose; surface it as the reply
		}
		response = response[start : end+1]
	}
	var parsed struct {
		Reply   string          `json:"reply"`
		Workout *LibraryWorkout `json:"workout"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(response)), &parsed); err != nil {
		return response, nil
	}
	if parsed.Workout != nil {
		parsed.Workout.Source = "ai"
	}
	return parsed.Reply, parsed.Workout
}
