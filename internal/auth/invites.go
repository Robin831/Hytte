package auth

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/mail"
	"strings"
	"time"
)

// Hytte is a private family app, but its Google sign-in is open to any
// Google account. The invite list is the door: a Google account may sign in
// only if it already has a Hytte user, or its email has been invited by an
// admin (or it is the very first user of a fresh install). Anyone else gets
// no user, no session and no stored token.

// Invite is one invited email that has not signed in yet.
type Invite struct {
	Email     string `json:"email"`
	CreatedAt string `json:"created_at"`
}

// ErrInvalidEmail is returned for an invite that is not an email address.
var ErrInvalidEmail = errors.New("not a valid email address")

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// SignInAllowed reports whether a Google account may sign in.
func SignInAllowed(db *sql.DB, googleID, email string) (bool, error) {
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM users WHERE google_id = ?`, googleID).Scan(&n); err != nil {
		return false, err
	}
	if n > 0 {
		return true, nil
	}
	// A fresh install has no real users yet (id 0 is the system user).
	if err := db.QueryRow(`SELECT COUNT(*) FROM users WHERE id > 0`).Scan(&n); err != nil {
		return false, err
	}
	if n == 0 {
		return true, nil
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM invited_emails WHERE email = ?`, normalizeEmail(email)).Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}

// UseInvite removes an invite once its owner has signed in.
func UseInvite(db *sql.DB, email string) {
	_, _ = db.Exec(`DELETE FROM invited_emails WHERE email = ?`, normalizeEmail(email))
}

// ListInvites returns the pending invites, newest first.
func ListInvites(db *sql.DB) ([]Invite, error) {
	rows, err := db.Query(`SELECT email, created_at FROM invited_emails ORDER BY created_at DESC, email`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Invite{}
	for rows.Next() {
		var i Invite
		if err := rows.Scan(&i.Email, &i.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

// AddInvite invites an email address.
func AddInvite(db *sql.DB, email string, invitedBy int64) error {
	email = normalizeEmail(email)
	if addr, err := mail.ParseAddress(email); err != nil || addr.Address != email || len(email) > 254 || !strings.Contains(email, ".") {
		return ErrInvalidEmail
	}
	_, err := db.Exec(`INSERT OR IGNORE INTO invited_emails (email, invited_by, created_at) VALUES (?, ?, ?)`,
		email, invitedBy, time.Now().UTC().Format(time.RFC3339))
	return err
}

// DeleteInvite withdraws an invite.
func DeleteInvite(db *sql.DB, email string) error {
	_, err := db.Exec(`DELETE FROM invited_emails WHERE email = ?`, normalizeEmail(email))
	return err
}

// AdminListInvitesHandler returns the pending invites.
func AdminListInvitesHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		invites, err := ListInvites(db)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load invites"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"invites": invites})
	}
}

// AdminAddInviteHandler invites an email. POST {"email": "..."}
func AdminAddInviteHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Email string `json:"email"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		user := UserFromContext(r.Context())
		if err := AddInvite(db, in.Email, user.ID); errors.Is(err, ErrInvalidEmail) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		} else if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save invite"})
			return
		}
		invites, _ := ListInvites(db)
		writeJSON(w, http.StatusCreated, map[string]any{"invites": invites})
	}
}

// AdminDeleteInviteHandler withdraws an invite. DELETE ?email=...
func AdminDeleteInviteHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := DeleteInvite(db, r.URL.Query().Get("email")); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to remove invite"})
			return
		}
		invites, _ := ListInvites(db)
		writeJSON(w, http.StatusOK, map[string]any{"invites": invites})
	}
}
