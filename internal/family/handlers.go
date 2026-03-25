package family

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/go-chi/chi/v5"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("family: writeJSON encode error: %v", err)
	}
}

// StatusHandler returns the family role of the authenticated user.
// GET /api/family/status
func StatusHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		isParent, err := IsParent(db, user.ID)
		if err != nil {
			log.Printf("family: is_parent check user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to check family status"})
			return
		}

		isChild, err := IsChild(db, user.ID)
		if err != nil {
			log.Printf("family: is_child check user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to check family status"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"is_parent": isParent,
			"is_child":  isChild,
		})
	}
}

// ListChildrenHandler returns all children linked to the authenticated parent.
// GET /api/family/children
func ListChildrenHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		children, err := GetChildren(db, user.ID)
		if err != nil {
			log.Printf("family: list children user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list children"})
			return
		}
		if children == nil {
			children = []FamilyLink{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"children": children})
	}
}

// UnlinkChildHandler removes a child link by child user ID.
// DELETE /api/family/children/{id}
func UnlinkChildHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		childID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid child ID"})
			return
		}

		if err := RemoveChild(db, user.ID, childID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "child link not found"})
				return
			}
			log.Printf("family: unlink child %d for parent %d: %v", childID, user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to unlink child"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// UpdateChildHandler updates the nickname and avatar emoji for a linked child.
// PUT /api/family/children/{id}
func UpdateChildHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		childID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid child ID"})
			return
		}

		var body struct {
			Nickname    string `json:"nickname"`
			AvatarEmoji string `json:"avatar_emoji"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}

		link, err := UpdateChild(db, user.ID, childID, body.Nickname, body.AvatarEmoji)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "child link not found"})
				return
			}
			log.Printf("family: update child %d for parent %d: %v", childID, user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update child"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{"link": link})
	}
}

// GenerateInviteHandler generates an invite code for the authenticated parent.
// POST /api/family/invite
func GenerateInviteHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		// Only users who are not already linked as a child may generate invites.
		isChild, err := IsChild(db, user.ID)
		if err != nil {
			log.Printf("family: is_child check user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to check family status"})
			return
		}
		if isChild {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "child accounts cannot generate invite codes"})
			return
		}

		invite, err := GenerateInviteCode(db, user.ID)
		if err != nil {
			log.Printf("family: generate invite user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to generate invite code"})
			return
		}

		writeJSON(w, http.StatusCreated, map[string]any{"invite": invite})
	}
}

// AcceptInviteHandler accepts an invite code, linking the authenticated user as a child.
// POST /api/family/invite/accept
func AcceptInviteHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		// Users who already have linked children cannot become a child themselves.
		isParent, err := IsParent(db, user.ID)
		if err != nil {
			log.Printf("family: is_parent check user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to check family status"})
			return
		}
		if isParent {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "accounts with linked children cannot be linked as a child"})
			return
		}

		var body struct {
			Code string `json:"code"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		body.Code = strings.TrimSpace(strings.ToUpper(body.Code))
		if body.Code == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "code is required"})
			return
		}

		link, err := AcceptInviteCode(db, body.Code, user.ID)
		if err != nil {
			switch {
			case errors.Is(err, ErrInvalidCode):
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "invalid invite code"})
			case errors.Is(err, ErrCodeAlreadyUsed):
				writeJSON(w, http.StatusConflict, map[string]string{"error": "invite code has already been used"})
			case errors.Is(err, ErrCodeExpired):
				writeJSON(w, http.StatusGone, map[string]string{"error": "invite code has expired"})
			case errors.Is(err, ErrAlreadyLinked):
				writeJSON(w, http.StatusConflict, map[string]string{"error": "account is already linked to a parent"})
			case errors.Is(err, ErrSelfLink):
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "cannot link to your own account"})
			default:
				log.Printf("family: accept invite user %d: %v", user.ID, err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to accept invite code"})
			}
			return
		}

		writeJSON(w, http.StatusCreated, map[string]any{"link": link})
	}
}
