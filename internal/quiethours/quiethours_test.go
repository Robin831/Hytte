package quiethours

import (
	"database/sql"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	dbpkg "github.com/Robin831/Hytte/internal/db"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := dbpkg.Init(":memory:")
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	t.Cleanup(func() { db.Close() })
	return db
}

func createTestUser(t *testing.T, db *sql.DB) int64 {
	t.Helper()
	_, err := db.Exec(
		"INSERT INTO users (google_id, email, name) VALUES ('g123', 'test@example.com', 'Test')",
	)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	var id int64
	err = db.QueryRow("SELECT id FROM users WHERE google_id = 'g123'").Scan(&id)
	if err != nil {
		t.Fatalf("select user: %v", err)
	}
	return id
}

func setPrefs(t *testing.T, db *sql.DB, userID int64, prefs map[string]string) {
	t.Helper()
	for k, v := range prefs {
		if err := auth.SetPreference(db, userID, k, v); err != nil {
			t.Fatalf("set preference %s: %v", k, err)
		}
	}
}

func TestIsActive_Disabled(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)

	// No preferences set — quiet hours disabled.
	if isActiveAt(db, userID, time.Now()) {
		t.Error("expected quiet hours to be inactive when no preferences are set")
	}

	// Explicitly disabled.
	setPrefs(t, db, userID, map[string]string{
		"quiet_hours_enabled":  "false",
		"quiet_hours_start":    "22:00",
		"quiet_hours_end":      "07:00",
		"quiet_hours_timezone": "Europe/Oslo",
	})
	if isActiveAt(db, userID, time.Now()) {
		t.Error("expected quiet hours to be inactive when explicitly disabled")
	}
}

func TestIsActive_OvernightRange(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)

	setPrefs(t, db, userID, map[string]string{
		"quiet_hours_enabled":  "true",
		"quiet_hours_start":    "22:00",
		"quiet_hours_end":      "07:00",
		"quiet_hours_timezone": "Europe/Oslo",
	})

	oslo, _ := time.LoadLocation("Europe/Oslo")

	tests := []struct {
		name   string
		hour   int
		minute int
		want   bool
	}{
		{"before start", 21, 30, false},
		{"at start", 22, 0, true},
		{"late night", 23, 30, true},
		{"midnight", 0, 0, true},
		{"early morning", 5, 0, true},
		{"before end", 6, 59, true},
		{"at end", 7, 0, false},
		{"afternoon", 14, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Date(2026, 3, 14, tt.hour, tt.minute, 0, 0, oslo)
			got := isActiveAt(db, userID, now)
			if got != tt.want {
				t.Errorf("at %02d:%02d got %v, want %v", tt.hour, tt.minute, got, tt.want)
			}
		})
	}
}

func TestIsActive_SameDayRange(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)

	setPrefs(t, db, userID, map[string]string{
		"quiet_hours_enabled":  "true",
		"quiet_hours_start":    "09:00",
		"quiet_hours_end":      "17:00",
		"quiet_hours_timezone": "America/New_York",
	})

	ny, _ := time.LoadLocation("America/New_York")

	tests := []struct {
		name   string
		hour   int
		minute int
		want   bool
	}{
		{"before start", 8, 30, false},
		{"at start", 9, 0, true},
		{"midday", 12, 0, true},
		{"before end", 16, 59, true},
		{"at end", 17, 0, false},
		{"evening", 20, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Date(2026, 3, 14, tt.hour, tt.minute, 0, 0, ny)
			got := isActiveAt(db, userID, now)
			if got != tt.want {
				t.Errorf("at %02d:%02d got %v, want %v", tt.hour, tt.minute, got, tt.want)
			}
		})
	}
}

func TestIsActive_TimezoneConversion(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)

	// Quiet hours 22:00–07:00 in Oslo (UTC+1 in winter, UTC+2 in summer).
	setPrefs(t, db, userID, map[string]string{
		"quiet_hours_enabled":  "true",
		"quiet_hours_start":    "22:00",
		"quiet_hours_end":      "07:00",
		"quiet_hours_timezone": "Europe/Oslo",
	})

	// 23:00 UTC on a winter day = 00:00 CET (Oslo) → within quiet hours.
	utcTime := time.Date(2026, 1, 15, 23, 0, 0, 0, time.UTC)
	if !isActiveAt(db, userID, utcTime) {
		t.Error("expected quiet hours active at 23:00 UTC (00:00 Oslo)")
	}

	// 08:00 UTC on a winter day = 09:00 CET (Oslo) → outside quiet hours.
	utcTime = time.Date(2026, 1, 15, 8, 0, 0, 0, time.UTC)
	if isActiveAt(db, userID, utcTime) {
		t.Error("expected quiet hours inactive at 08:00 UTC (09:00 Oslo)")
	}
}

func TestIsActive_MissingFields(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)

	// Enabled but missing start time.
	setPrefs(t, db, userID, map[string]string{
		"quiet_hours_enabled":  "true",
		"quiet_hours_end":      "07:00",
		"quiet_hours_timezone": "Europe/Oslo",
	})
	if isActiveAt(db, userID, time.Now()) {
		t.Error("expected inactive when start time is missing")
	}
}

func TestIsActive_InvalidTimezone(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)

	setPrefs(t, db, userID, map[string]string{
		"quiet_hours_enabled":  "true",
		"quiet_hours_start":    "22:00",
		"quiet_hours_end":      "07:00",
		"quiet_hours_timezone": "Invalid/Zone",
	})
	if isActiveAt(db, userID, time.Now()) {
		t.Error("expected inactive with invalid timezone")
	}
}

func TestIsActive_InvalidTimeFormat(t *testing.T) {
	db := setupTestDB(t)
	userID := createTestUser(t, db)

	setPrefs(t, db, userID, map[string]string{
		"quiet_hours_enabled":  "true",
		"quiet_hours_start":    "not-a-time",
		"quiet_hours_end":      "07:00",
		"quiet_hours_timezone": "Europe/Oslo",
	})
	if isActiveAt(db, userID, time.Now()) {
		t.Error("expected inactive with invalid time format")
	}
}
