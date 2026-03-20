package auth

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// validCLIPathRe matches safe CLI paths: alphanumeric, slashes, backslashes,
// dots, hyphens, underscores, colons (for Windows drive letters).
var validCLIPathRe = regexp.MustCompile(`^[a-zA-Z0-9._/\\:-]+$`)

// ValidateCLIPath checks that a CLI path contains only safe characters.
// Empty string is valid (means "use default").
func ValidateCLIPath(path string) error {
	if path == "" {
		return nil
	}
	if !validCLIPathRe.MatchString(path) {
		return fmt.Errorf("invalid CLI path: only alphanumeric characters, slashes, dots, hyphens, underscores, and colons are allowed")
	}
	return nil
}

// EventType describes a notification event type that can be filtered.
type EventType struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

// AllowedEventTypes is the single source of truth for notification event types.
// Both the validation logic and the GET /api/settings/event-types endpoint use this.
var AllowedEventTypes = []EventType{
	{Key: "push", Label: "Push", Description: "Code pushed to a branch"},
	{Key: "pull_request", Label: "Pull Request", Description: "PR opened, closed, or merged"},
	{Key: "release", Label: "Release", Description: "New release published"},
	{Key: "pr_ready_to_merge", Label: "PR Ready to Merge", Description: "PR passed CI and review, ready to merge"},
	{Key: "pr_created", Label: "PR Created", Description: "Smith created a PR"},
	{Key: "bead_failed", Label: "Bead Failed", Description: "Bead exhausted all retry attempts"},
	{Key: "daily_cost", Label: "Daily Cost", Description: "Daily cost limit reached"},
	{Key: "worker_done", Label: "Worker Done", Description: "Worker completed successfully"},
	{Key: "bead_decomposed", Label: "Bead Decomposed", Description: "Schematic decomposed a parent bead into sub-beads"},
	{Key: "release_published", Label: "Release Published", Description: "New Forge release published"},
}

// allowedEventKeys returns a set derived from AllowedEventTypes for fast lookup.
func allowedEventKeys() map[string]bool {
	m := make(map[string]bool, len(AllowedEventTypes))
	for _, et := range AllowedEventTypes {
		m[et.Key] = true
	}
	return m
}

// EventTypesHandler returns the list of allowed notification event types.
func EventTypesHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"event_types": AllowedEventTypes})
	}
}

// PreferencesGetHandler returns all preferences for the authenticated user.
// Claude-related preferences are only visible to admin users.
func PreferencesGetHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := UserFromContext(r.Context())
		prefs, err := GetPreferences(db, user.ID)
		if err != nil {
			log.Printf("Failed to get preferences: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load preferences"})
			return
		}
		if !user.IsAdmin {
			delete(prefs, "claude_enabled")
			delete(prefs, "claude_cli_path")
			delete(prefs, "claude_model")
		}
		writeJSON(w, http.StatusOK, map[string]any{"preferences": prefs})
	}
}

// PreferencesPutHandler updates preferences for the authenticated user.
// Expects JSON body: {"preferences": {"key": "value", ...}}
func PreferencesPutHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := UserFromContext(r.Context())

		var body struct {
			Preferences map[string]string `json:"preferences"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}

		// Only allow known preference keys.
		allowed := map[string]bool{
			"theme":                       true,
			"home_location":               true,
			"weather_location":            true,
			"recent_locations":            true,
			"notifications_enabled":       true,
			"notifications_degraded":      true,
			"quiet_hours_enabled":         true,
			"quiet_hours_start":           true,
			"quiet_hours_end":             true,
			"quiet_hours_timezone":        true,
			"notification_filter_sources": true,
			"notification_filter_events":  true,
			"max_hr":                      true,
			"quick_links":                 true,
			"claude_enabled":              true,
			"claude_cli_path":             true,
			"claude_model":                true,
		}

		allowedEvents := allowedEventKeys()

		claudeKeys := map[string]bool{
			"claude_enabled":  true,
			"claude_cli_path": true,
			"claude_model":    true,
		}

		// Build the set of keys to process (skip unknown keys).
		toWrite := make(map[string]string, len(body.Preferences))
		for k, v := range body.Preferences {
			if allowed[k] {
				toWrite[k] = v
			}
		}

		// Pre-validate all keys before writing any, so the request is atomic:
		// either all accepted preferences are persisted or none are.
		for k, v := range toWrite {
			if claudeKeys[k] && !user.IsAdmin {
				writeJSON(w, http.StatusForbidden, map[string]string{"error": "Claude AI features are restricted to admin users"})
				return
			}
			// Validate quick_links: must be a JSON array of {title, url} with safe URLs.
			if k == "quick_links" {
				var links []struct {
					Title string `json:"title"`
					URL   string `json:"url"`
				}
				if err := json.Unmarshal([]byte(v), &links); err != nil {
					writeJSON(w, http.StatusBadRequest, map[string]string{"error": "quick_links must be a JSON array of {title, url} objects"})
					return
				}
				if len(links) > 50 {
					writeJSON(w, http.StatusBadRequest, map[string]string{"error": "quick_links cannot exceed 50 items"})
					return
				}
				for _, link := range links {
					if strings.TrimSpace(link.Title) == "" || strings.TrimSpace(link.URL) == "" {
						writeJSON(w, http.StatusBadRequest, map[string]string{"error": "each quick link must have a non-empty title and url"})
						return
					}
					if len(link.Title) > 200 {
						writeJSON(w, http.StatusBadRequest, map[string]string{"error": "quick link title must not exceed 200 characters"})
						return
					}
					if len(link.URL) > 2048 {
						writeJSON(w, http.StatusBadRequest, map[string]string{"error": "quick link URL must not exceed 2048 characters"})
						return
					}
					parsed, err := url.Parse(link.URL)
					if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
						writeJSON(w, http.StatusBadRequest, map[string]string{"error": "quick link URLs must use http or https with a valid host"})
						return
					}
				}
			}
			// Validate CLI path to prevent command injection.
			if k == "claude_cli_path" {
				if err := ValidateCLIPath(v); err != nil {
					writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
					return
				}
			}
			// Validate event keys inside notification_filter_events JSON.
			if k == "notification_filter_events" {
				var events map[string]bool
				if err := json.Unmarshal([]byte(v), &events); err != nil {
					writeJSON(w, http.StatusBadRequest, map[string]string{"error": "notification_filter_events must be a JSON object mapping event keys to booleans"})
					return
				}
				for ek := range events {
					if !allowedEvents[ek] {
						writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown event type: " + ek})
						return
					}
				}
			}
		}

		// All keys validated — now persist them.
		for k, v := range toWrite {
			if err := SetPreference(db, user.ID, k, v); err != nil {
				log.Printf("Failed to set preference %s: %v", k, err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save preferences"})
				return
			}
		}

		prefs, err := GetPreferences(db, user.ID)
		if err != nil {
			log.Printf("Failed to get preferences after update: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load preferences"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"preferences": prefs})
	}
}

// SessionsListHandler returns the active sessions for the authenticated user.
func SessionsListHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := UserFromContext(r.Context())

		// Get the current session token hash to mark which one is "current".
		var currentTokenHash string
		if cookie, err := r.Cookie("session"); err == nil {
			currentTokenHash = hashToken(cookie.Value)
		}

		rows, err := db.Query(
			"SELECT token, created_at, expires_at FROM sessions WHERE user_id = ? AND expires_at > ? ORDER BY created_at DESC",
			user.ID, time.Now(),
		)
		if err != nil {
			log.Printf("Failed to list sessions: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list sessions"})
			return
		}
		defer rows.Close()

		type sessionInfo struct {
			ID        string `json:"id"`
			CreatedAt string `json:"created_at"`
			ExpiresAt string `json:"expires_at"`
			Current   bool   `json:"current"`
		}

		var sessions []sessionInfo
		for rows.Next() {
			var token string
			var createdAt, expiresAt time.Time
			if err := rows.Scan(&token, &createdAt, &expiresAt); err != nil {
				log.Printf("Failed to scan session: %v", err)
				continue
			}
			// Use a short prefix as ID so we don't expose the full token.
			// Guard against unexpectedly short tokens.
			displayID := token
			if len(displayID) > 8 {
				displayID = displayID[:8]
			}
			sessions = append(sessions, sessionInfo{
				ID:        displayID,
				CreatedAt: createdAt.Format(time.RFC3339),
				ExpiresAt: expiresAt.Format(time.RFC3339),
				Current:   token == currentTokenHash,
			})
		}
		if sessions == nil {
			sessions = []sessionInfo{}
		}

		writeJSON(w, http.StatusOK, map[string]any{"sessions": sessions})
	}
}

// SignOutEverywhereHandler deletes all sessions for the authenticated user
// except the current one.
func SignOutEverywhereHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := UserFromContext(r.Context())

		var currentTokenHash string
		if cookie, err := r.Cookie("session"); err == nil {
			currentTokenHash = hashToken(cookie.Value)
		}

		_, err := db.Exec(
			"DELETE FROM sessions WHERE user_id = ? AND token != ?",
			user.ID, currentTokenHash,
		)
		if err != nil {
			log.Printf("Failed to sign out everywhere: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to sign out other sessions"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// DeleteAccountHandler removes the user's account and all related data.
func DeleteAccountHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := UserFromContext(r.Context())

		// Delete user — CASCADE will remove sessions and preferences.
		_, err := db.Exec("DELETE FROM users WHERE id = ?", user.ID)
		if err != nil {
			log.Printf("Failed to delete account: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete account"})
			return
		}

		// Clear the session cookie.
		http.SetCookie(w, &http.Cookie{
			Name:     "session",
			Value:    "",
			Path:     "/",
			MaxAge:   -1,
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			Secure:   isSecure(),
		})

		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}
