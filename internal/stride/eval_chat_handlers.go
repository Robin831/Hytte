package stride

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/training"
	"github.com/go-chi/chi/v5"
)

// EvalMessagesListHandler returns the coach thread attached to an evaluation.
// GET /api/stride/evaluations/{id}/messages
func EvalMessagesListHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		evalID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil || evalID <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid evaluation id"})
			return
		}
		rec, err := GetEvaluationByID(r.Context(), db, user.ID, evalID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "evaluation not found"})
				return
			}
			log.Printf("stride eval chat: load eval %d: %v", evalID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load evaluation"})
			return
		}
		workoutID, date := evalThreadKey(rec)
		messages, err := listEvalMessages(r.Context(), db, user.ID, rec.PlanID, workoutID, date)
		if err != nil {
			log.Printf("stride eval chat: list messages for eval %d: %v", evalID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load messages"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"messages": messages})
	}
}

// EvalMessagesSendHandler stores an athlete comment and returns the coach's
// reply, applying an in-place evaluation revision when the coach makes one.
// POST /api/stride/evaluations/{id}/messages {"content": "..."}
func EvalMessagesSendHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		evalID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil || evalID <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid evaluation id"})
			return
		}
		var body struct {
			Content string `json:"content"`
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
		if len([]rune(body.Content)) > evalChatMaxMessage {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "message too long"})
			return
		}

		claudeCfg, err := training.LoadClaudeConfig(db, user.ID)
		if err != nil {
			log.Printf("stride eval chat: load claude config for user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load Claude configuration"})
			return
		}
		if !claudeCfg.Enabled {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Claude is not enabled — enable it in settings"})
			return
		}

		reply, err := ReplyToEvaluation(r.Context(), db, claudeCfg, user.ID, evalID, body.Content)
		if err != nil {
			switch {
			case errors.Is(err, sql.ErrNoRows):
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "evaluation not found"})
			default:
				log.Printf("stride eval chat: reply to eval %d: %v", evalID, err)
				writeJSON(w, http.StatusBadGateway, map[string]string{"error": "coach reply failed"})
			}
			return
		}
		writeJSON(w, http.StatusOK, reply)
	}
}
