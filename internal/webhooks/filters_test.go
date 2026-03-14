package webhooks

import (
	"testing"

	"github.com/Robin831/Hytte/internal/auth"
)

func TestIsFilteredOut_NoPreferences(t *testing.T) {
	d := setupTestDB(t)
	uid := createTestUser(t, d)

	prefs, _ := auth.GetPreferences(d, uid)

	// No preferences set — everything should pass through.
	if isFilteredOut(prefs, "github", "push") {
		t.Error("expected github/push to pass through with no filters")
	}
	if isFilteredOut(prefs, "", "webhook") {
		t.Error("expected generic to pass through with no filters")
	}
}

func TestIsFilteredOut_SourceDisabled(t *testing.T) {
	d := setupTestDB(t)
	uid := createTestUser(t, d)

	auth.SetPreference(d, uid, "notification_filter_sources", `{"github":false,"generic":true}`)
	prefs, _ := auth.GetPreferences(d, uid)

	if !isFilteredOut(prefs, "github", "push") {
		t.Error("expected github source to be filtered out")
	}
	if isFilteredOut(prefs, "", "") {
		t.Error("expected generic source to pass through")
	}
}

func TestIsFilteredOut_EventDisabled(t *testing.T) {
	d := setupTestDB(t)
	uid := createTestUser(t, d)

	auth.SetPreference(d, uid, "notification_filter_events", `{"push":false,"pull_request":true,"release":true}`)
	prefs, _ := auth.GetPreferences(d, uid)

	if !isFilteredOut(prefs, "github", "push") {
		t.Error("expected github/push to be filtered out")
	}
	if isFilteredOut(prefs, "github", "pull_request") {
		t.Error("expected github/pull_request to pass through")
	}
	if isFilteredOut(prefs, "github", "release") {
		t.Error("expected github/release to pass through")
	}
}

func TestIsFilteredOut_UnknownEventPassesThrough(t *testing.T) {
	d := setupTestDB(t)
	uid := createTestUser(t, d)

	auth.SetPreference(d, uid, "notification_filter_events", `{"push":false}`)
	prefs, _ := auth.GetPreferences(d, uid)

	if isFilteredOut(prefs, "github", "issues") {
		t.Error("expected unknown github event type to pass through")
	}
}

func TestIsFilteredOut_EventFilterIgnoredForGeneric(t *testing.T) {
	d := setupTestDB(t)
	uid := createTestUser(t, d)

	auth.SetPreference(d, uid, "notification_filter_events", `{"push":false}`)
	prefs, _ := auth.GetPreferences(d, uid)

	// Event filters only apply to GitHub source — generic should pass.
	if isFilteredOut(prefs, "", "") {
		t.Error("expected generic source to ignore event filters")
	}
}

func TestIsFilteredOut_BothSourceAndEventEnabled(t *testing.T) {
	d := setupTestDB(t)
	uid := createTestUser(t, d)

	auth.SetPreference(d, uid, "notification_filter_sources", `{"github":true,"generic":true}`)
	auth.SetPreference(d, uid, "notification_filter_events", `{"push":true,"pull_request":true,"release":true}`)
	prefs, _ := auth.GetPreferences(d, uid)

	if isFilteredOut(prefs, "github", "push") {
		t.Error("expected fully enabled filters to pass through")
	}
}

func TestIsFilteredOut_NilPrefs(t *testing.T) {
	// nil prefs (DB error case) should fail open.
	if isFilteredOut(nil, "github", "push") {
		t.Error("expected nil prefs to pass through (fail open)")
	}
}
