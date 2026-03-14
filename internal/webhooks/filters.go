package webhooks

import (
	"database/sql"
	"encoding/json"

	"github.com/Robin831/Hytte/internal/auth"
)

// isFilteredOut checks the user's notification filter preferences to determine
// whether a notification for the given source and event type should be
// suppressed. Returns true if the notification should NOT be sent.
//
// Filter preferences are stored as JSON objects in user_preferences:
//   - notification_filter_sources: {"github": true, "generic": false}
//   - notification_filter_events:  {"push": true, "pull_request": false, "release": true}
//
// Missing keys or absent preferences default to enabled (all notifications
// pass through when no filters are configured).
func isFilteredOut(db *sql.DB, userID int64, source, eventType string) bool {
	prefs, err := auth.GetPreferences(db, userID)
	if err != nil {
		return false // fail open
	}

	// Normalise source: empty means "generic".
	if source == "" {
		source = "generic"
	}

	// Check source filter.
	if raw, ok := prefs["notification_filter_sources"]; ok {
		var sources map[string]bool
		if json.Unmarshal([]byte(raw), &sources) == nil {
			if enabled, exists := sources[source]; exists && !enabled {
				return true
			}
		}
	}

	// Check event type filter (only for known GitHub event types).
	if source == "github" && eventType != "" {
		if raw, ok := prefs["notification_filter_events"]; ok {
			var events map[string]bool
			if json.Unmarshal([]byte(raw), &events) == nil {
				if enabled, exists := events[eventType]; exists && !enabled {
					return true
				}
			}
		}
	}

	return false
}
