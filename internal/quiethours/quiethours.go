package quiethours

import (
	"database/sql"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
)

// IsActive checks whether the given user currently has quiet hours active.
// It reads the user's quiet hours preferences and compares against the current
// time in the user's configured timezone.
//
// Returns true if notifications should be suppressed (quiet hours are active).
// Returns false if quiet hours are disabled, misconfigured, or it's outside
// the quiet window.
func IsActive(db *sql.DB, userID int64) bool {
	return isActiveAt(db, userID, time.Now())
}

// IsActiveWithPrefs is like IsActive but accepts pre-fetched preferences,
// avoiding a redundant DB query when the caller already has them.
func IsActiveWithPrefs(prefs map[string]string) bool {
	return isActiveWithPrefsAt(prefs, time.Now())
}

// isActiveAt is the testable core — accepts an explicit "now" time.
func isActiveAt(db *sql.DB, userID int64, now time.Time) bool {
	prefs, err := auth.GetPreferences(db, userID)
	if err != nil {
		return false
	}
	return isActiveWithPrefsAt(prefs, now)
}

// isActiveWithPrefsAt is the testable core that accepts both prefs and time.
func isActiveWithPrefsAt(prefs map[string]string, now time.Time) bool {
	if prefs == nil {
		return false
	}

	if prefs["quiet_hours_enabled"] != "true" {
		return false
	}

	startStr := prefs["quiet_hours_start"]
	endStr := prefs["quiet_hours_end"]
	tz := prefs["quiet_hours_timezone"]

	if startStr == "" || endStr == "" || tz == "" {
		return false
	}

	loc, err := time.LoadLocation(tz)
	if err != nil {
		return false
	}

	// Parse HH:MM times.
	startTime, err := time.Parse("15:04", startStr)
	if err != nil {
		return false
	}
	endTime, err := time.Parse("15:04", endStr)
	if err != nil {
		return false
	}

	// Convert "now" to the user's timezone and extract hour:minute.
	userNow := now.In(loc)
	userMinutes := userNow.Hour()*60 + userNow.Minute()
	startMinutes := startTime.Hour()*60 + startTime.Minute()
	endMinutes := endTime.Hour()*60 + endTime.Minute()

	if startMinutes == endMinutes {
		// Identical start and end: treat as disabled (zero-width window).
		// Selecting the same time for both boundaries is likely a misconfiguration;
		// silently producing "always on" or "always off" behaviour would surprise
		// the user, so we return false so they still receive notifications.
		return false
	}

	if startMinutes < endMinutes {
		// Same-day range, e.g. 09:00–17:00
		return userMinutes >= startMinutes && userMinutes < endMinutes
	}
	// Overnight range, e.g. 22:00–07:00
	return userMinutes >= startMinutes || userMinutes < endMinutes
}
