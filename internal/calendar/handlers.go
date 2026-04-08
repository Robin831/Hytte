package calendar

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("writeJSON encode error: %v", err)
	}
}

// EventsHandler returns cached calendar events for the authenticated user.
// Query params:
//   - start: RFC3339 or YYYY-MM-DD (required)
//   - end:   RFC3339 or YYYY-MM-DD (required)
//   - sync:  if "true", triggers a background sync before returning cached data
func EventsHandler(db *sql.DB, client *Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		start := strings.TrimSpace(r.URL.Query().Get("start"))
		end := strings.TrimSpace(r.URL.Query().Get("end"))
		if start == "" || end == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "start and end query parameters are required"})
			return
		}

		// Parse start/end to validate and normalize.
		startTime, err := parseFlexibleTime(start)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid start time: " + err.Error()})
			return
		}
		endTime, err := parseFlexibleTime(end)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid end time: " + err.Error()})
			return
		}

		doSync := r.URL.Query().Get("sync") == "true"

		// Load user's visible calendars from preferences.
		calendarIDs := loadVisibleCalendars(db, user.ID)

		if doSync {
			hasToken, _ := auth.HasGoogleToken(db, user.ID)
			if hasToken {
				syncCalendars := calendarIDs
				if len(syncCalendars) == 0 {
					// If no preference set, sync primary calendar.
					syncCalendars = []string{"primary"}
				}
				for _, calID := range syncCalendars {
					if err := client.FetchAndCacheEvents(r.Context(), user.ID, calID, startTime, endTime); err != nil {
						log.Printf("calendar: sync failed for user %d calendar %s: %v", user.ID, calID, err)
					}
				}
			}
		}

		events, err := QueryEvents(db, user.ID, calendarIDs, startTime.Format(time.RFC3339), endTime.Format(time.RFC3339))
		if err != nil {
			log.Printf("calendar: query events for user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to query events"})
			return
		}
		if events == nil {
			events = []Event{}
		}

		writeJSON(w, http.StatusOK, map[string]any{"events": events})
	}
}

// CalendarsHandler returns the user's Google Calendar list.
func CalendarsHandler(db *sql.DB, client *Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		hasToken, err := auth.HasGoogleToken(db, user.ID)
		if err != nil {
			log.Printf("calendar: check token for user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to check calendar connection"})
			return
		}
		if !hasToken {
			writeJSON(w, http.StatusOK, map[string]any{
				"calendars": []CalendarInfo{},
				"connected": false,
			})
			return
		}

		calendars, err := client.ListCalendars(r.Context(), user.ID)
		if err != nil {
			log.Printf("calendar: list calendars for user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list calendars"})
			return
		}

		// Mark which calendars are selected by the user.
		visible := loadVisibleCalendars(db, user.ID)
		visibleSet := make(map[string]bool, len(visible))
		for _, id := range visible {
			visibleSet[id] = true
		}
		for i := range calendars {
			calendars[i].Selected = visibleSet[calendars[i].ID]
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"calendars": calendars,
			"connected": true,
		})
	}
}

// SettingsHandler saves which calendars the user wants to see.
// Expects JSON body: {"calendar_ids": ["id1", "id2", ...]}
func SettingsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		var body struct {
			CalendarIDs []string `json:"calendar_ids"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}

		// Store as comma-separated list in user_preferences.
		value := strings.Join(body.CalendarIDs, ",")
		if err := auth.SetPreference(db, user.ID, "calendar_visible_ids", value); err != nil {
			log.Printf("calendar: save settings for user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save settings"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// SyncHandler triggers a full re-sync of calendar events for the user.
// Clears sync tokens to force a full fetch.
func SyncHandler(db *sql.DB, client *Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		hasToken, err := auth.HasGoogleToken(db, user.ID)
		if err != nil || !hasToken {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "calendar not connected"})
			return
		}

		// Clear sync state to force full sync.
		if err := ClearSyncState(db, user.ID); err != nil {
			log.Printf("calendar: clear sync state for user %d: %v", user.ID, err)
		}

		// Clear existing cached events.
		if err := DeleteAllEvents(db, user.ID); err != nil {
			log.Printf("calendar: clear events for user %d: %v", user.ID, err)
		}

		calendarIDs := loadVisibleCalendars(db, user.ID)
		if len(calendarIDs) == 0 {
			calendarIDs = []string{"primary"}
		}

		// Sync 3 months back and 6 months forward.
		now := time.Now()
		timeMin := now.AddDate(0, -3, 0)
		timeMax := now.AddDate(0, 6, 0)

		var syncErrors int
		for _, calID := range calendarIDs {
			if err := client.FetchAndCacheEvents(r.Context(), user.ID, calID, timeMin, timeMax); err != nil {
				log.Printf("calendar: full sync failed for user %d calendar %s: %v", user.ID, calID, err)
				syncErrors++
			}
		}

		if syncErrors > 0 && syncErrors == len(calendarIDs) {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "sync failed for all calendars"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// loadVisibleCalendars reads the user's calendar_visible_ids preference.
func loadVisibleCalendars(db *sql.DB, userID int64) []string {
	prefs, err := auth.GetPreferences(db, userID)
	if err != nil {
		return nil
	}
	raw := prefs["calendar_visible_ids"]
	if raw == "" {
		return nil
	}
	ids := strings.Split(raw, ",")
	result := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id != "" {
			result = append(result, id)
		}
	}
	return result
}

// parseFlexibleTime parses a time string in either RFC3339 or YYYY-MM-DD format.
func parseFlexibleTime(s string) (time.Time, error) {
	// Try RFC3339 first.
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	// Try date-only format.
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("unsupported time format: %s", s)
}
