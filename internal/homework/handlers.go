package homework

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/go-chi/chi/v5"
)

// maxProfileBodySize is the maximum allowed request body size for profile updates (64 KB).
const maxProfileBodySize = 64 << 10

// maxConversationBodySize is the maximum allowed request body size for new conversations (8 KB).
const maxConversationBodySize = 8 << 10

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("writeJSON encode error: %v", err)
	}
}

// verifyParentChild returns nil if a family_links row exists for parentID -> childID.
func verifyParentChild(db *sql.DB, parentID, childID int64) error {
	var id int64
	return db.QueryRow(
		`SELECT id FROM family_links WHERE parent_id = ? AND child_id = ?`,
		parentID, childID,
	).Scan(&id)
}

// parseChildID extracts the child ID from the URL and verifies the parent-child relationship.
// Returns the child ID or writes an error response and returns 0.
func parseChildID(w http.ResponseWriter, r *http.Request, db *sql.DB, userID int64) (int64, bool) {
	childID, err := strconv.ParseInt(chi.URLParam(r, "childId"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid child ID"})
		return 0, false
	}

	if err := verifyParentChild(db, userID, childID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "not authorized to access this child's data"})
		} else {
			log.Printf("homework: verify parent-child user %d child %d: %v", userID, childID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		}
		return 0, false
	}
	return childID, true
}

// HandleGetProfile returns the homework profile for a child.
// GET /api/homework/children/{childId}/profile
func HandleGetProfile(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		childID, ok := parseChildID(w, r, db, user.ID)
		if !ok {
			return
		}

		profile, err := GetProfileByKidID(db, childID)
		if err != nil {
			log.Printf("homework: get profile kid %d: %v", childID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to get profile"})
			return
		}
		if profile == nil {
			writeJSON(w, http.StatusOK, map[string]any{"profile": nil})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"profile": profile})
	}
}

// HandleUpdateProfile creates or updates the homework profile for a child.
// PUT /api/homework/children/{childId}/profile
func HandleUpdateProfile(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		childID, ok := parseChildID(w, r, db, user.ID)
		if !ok {
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, maxProfileBodySize)

		var body struct {
			Age               int      `json:"age"`
			GradeLevel        string   `json:"grade_level"`
			Subjects          []string `json:"subjects"`
			PreferredLanguage string   `json:"preferred_language"`
			SchoolName        string   `json:"school_name"`
			CurrentTopics     []string `json:"current_topics"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			var maxBytesErr *http.MaxBytesError
			if errors.As(err, &maxBytesErr) {
				writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "request body too large"})
				return
			}
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}

		body.GradeLevel = strings.TrimSpace(body.GradeLevel)
		body.PreferredLanguage = strings.TrimSpace(body.PreferredLanguage)
		body.SchoolName = strings.TrimSpace(body.SchoolName)
		if body.Subjects == nil {
			body.Subjects = []string{}
		}
		if body.CurrentTopics == nil {
			body.CurrentTopics = []string{}
		}

		if body.Age < 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "age must be non-negative"})
			return
		}

		profile := HomeworkProfile{
			KidID:             childID,
			Age:               body.Age,
			GradeLevel:        body.GradeLevel,
			Subjects:          body.Subjects,
			PreferredLanguage: body.PreferredLanguage,
			SchoolName:        body.SchoolName,
			CurrentTopics:     body.CurrentTopics,
		}

		// Try update first; if no row exists, create.
		err := UpdateProfile(db, profile)
		if errors.Is(err, sql.ErrNoRows) {
			created, createErr := CreateProfile(db, profile)
			if createErr != nil {
				log.Printf("homework: create profile kid %d: %v", childID, createErr)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create profile"})
				return
			}
			writeJSON(w, http.StatusCreated, map[string]any{"profile": created})
			return
		}
		if err != nil {
			log.Printf("homework: update profile kid %d: %v", childID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update profile"})
			return
		}

		// Re-read the updated profile.
		updated, err := GetProfileByKidID(db, childID)
		if err != nil {
			log.Printf("homework: re-read profile kid %d: %v", childID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read updated profile"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"profile": updated})
	}
}

// HandleListConversations returns all homework conversations for a child.
// GET /api/homework/children/{childId}/conversations
func HandleListConversations(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		childID, ok := parseChildID(w, r, db, user.ID)
		if !ok {
			return
		}

		convos, err := ListConversationsByKid(db, childID)
		if err != nil {
			log.Printf("homework: list conversations kid %d: %v", childID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list conversations"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"conversations": convos})
	}
}

// HandleGetConversation returns a single homework conversation with its messages.
// GET /api/homework/children/{childId}/conversations/{id}
func HandleGetConversation(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		childID, ok := parseChildID(w, r, db, user.ID)
		if !ok {
			return
		}

		convID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid conversation ID"})
			return
		}

		conv, err := GetConversation(db, convID, childID)
		if err != nil {
			log.Printf("homework: get conversation %d kid %d: %v", convID, childID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to get conversation"})
			return
		}
		if conv == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "conversation not found"})
			return
		}

		msgs, err := GetMessages(db, convID, childID)
		if err != nil {
			log.Printf("homework: get messages conv %d kid %d: %v", convID, childID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to get messages"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"conversation": conv,
			"messages":     msgs,
		})
	}
}

// HandleNewConversation starts a new homework conversation for a child.
// Accepts an optional "subject" field; if absent, defaults to empty string.
// POST /api/homework/children/{childId}/conversations
func HandleNewConversation(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		childID, ok := parseChildID(w, r, db, user.ID)
		if !ok {
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, maxConversationBodySize)

		var body struct {
			Subject string `json:"subject"`
		}
		// Subject is optional — tolerate an empty body, including whitespace-only bodies.
		if data, err := io.ReadAll(r.Body); err != nil {
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "request body too large"})
				return
			}
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "failed to read request body"})
			return
		} else if trimmed := strings.TrimSpace(string(data)); trimmed != "" {
			if err := json.Unmarshal([]byte(trimmed), &body); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
				return
			}
		}

		body.Subject = strings.TrimSpace(body.Subject)

		conv, err := CreateConversation(db, HomeworkConversation{
			KidID:   childID,
			Subject: body.Subject,
		})
		if err != nil {
			log.Printf("homework: create conversation kid %d: %v", childID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create conversation"})
			return
		}

		writeJSON(w, http.StatusCreated, map[string]any{"conversation": conv})
	}
}
