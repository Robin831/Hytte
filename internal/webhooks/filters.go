package webhooks

import (
	"encoding/json"
)

// isFilteredOut checks the user's notification filter preferences to determine
// whether a notification for the given source and event type should be
// suppressed. Returns true if the notification should NOT be sent.
//
// The caller must supply the user's preferences (pre-fetched from the DB) so
// that the filter check does not issue its own query on every dispatch.
//
// Filter preferences are stored as JSON objects in user_preferences:
//   - notification_filter_sources: {"github": true, "generic": false}
//   - notification_filter_events:  {"push": true, "pull_request": false, "release": true}
//
// Missing keys or absent preferences default to enabled (all notifications
// pass through when no filters are configured).
func isFilteredOut(prefs map[string]string, source, eventType string) bool {
	if prefs == nil {
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

	// Check event type filter.
	if eventType != "" {
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
