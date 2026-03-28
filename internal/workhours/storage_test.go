package workhours

import (
	"database/sql"
	"os"
	"testing"

	_ "modernc.org/sqlite"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()

	// Ensure the encryption key is set for tests.
	if os.Getenv("ENCRYPTION_KEY") == "" {
		t.Setenv("ENCRYPTION_KEY", "dGVzdGtleXRlc3RrZXl0ZXN0a2V5dGVzdGtleXQ=")
	}

	db, err := sql.Open("sqlite", "file::memory:?_pragma=foreign_keys(ON)")
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	schema := `
	CREATE TABLE IF NOT EXISTS users (
		id         INTEGER PRIMARY KEY,
		email      TEXT UNIQUE NOT NULL,
		name       TEXT NOT NULL DEFAULT '',
		picture    TEXT NOT NULL DEFAULT '',
		google_id  TEXT UNIQUE NOT NULL,
		is_admin   INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS user_preferences (
		user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		key     TEXT NOT NULL,
		value   TEXT NOT NULL DEFAULT '',
		PRIMARY KEY (user_id, key)
	);

	CREATE TABLE IF NOT EXISTS work_days (
		id         INTEGER PRIMARY KEY,
		user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		date       TEXT NOT NULL,
		lunch      INTEGER NOT NULL DEFAULT 0,
		notes      TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL DEFAULT '',
		UNIQUE(user_id, date)
	);

	CREATE TABLE IF NOT EXISTS work_sessions (
		id         INTEGER PRIMARY KEY,
		day_id     INTEGER NOT NULL REFERENCES work_days(id) ON DELETE CASCADE,
		start_time TEXT NOT NULL,
		end_time   TEXT NOT NULL,
		sort_order INTEGER NOT NULL DEFAULT 0
	);

	CREATE TABLE IF NOT EXISTS work_deductions (
		id        INTEGER PRIMARY KEY,
		day_id    INTEGER NOT NULL REFERENCES work_days(id) ON DELETE CASCADE,
		name      TEXT NOT NULL,
		minutes   INTEGER NOT NULL,
		preset_id INTEGER REFERENCES work_deduction_presets(id)
	);

	CREATE TABLE IF NOT EXISTS work_deduction_presets (
		id              INTEGER PRIMARY KEY,
		user_id         INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		name            TEXT NOT NULL,
		default_minutes INTEGER NOT NULL DEFAULT 15,
		icon            TEXT NOT NULL DEFAULT 'clock',
		sort_order      INTEGER NOT NULL DEFAULT 0,
		active          INTEGER NOT NULL DEFAULT 1
	);
	`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}

	// Insert a test user.
	if _, err := db.Exec(
		`INSERT INTO users (id, email, name, google_id) VALUES (1, 'test@example.com', 'Test', 'google-1')`,
	); err != nil {
		t.Fatalf("insert user: %v", err)
	}

	return db
}

func TestUpsertAndGetDay(t *testing.T) {
	db := setupTestDB(t)

	day, err := UpsertDay(db, 1, "2026-03-27", true, "test notes")
	if err != nil {
		t.Fatalf("upsert day: %v", err)
	}
	if day == nil {
		t.Fatal("expected day, got nil")
	}
	if day.Date != "2026-03-27" {
		t.Errorf("date: got %q, want %q", day.Date, "2026-03-27")
	}
	if !day.Lunch {
		t.Error("lunch: expected true")
	}
	if day.Notes != "test notes" {
		t.Errorf("notes: got %q, want %q", day.Notes, "test notes")
	}

	// Fetch it back.
	fetched, err := GetDay(db, 1, "2026-03-27")
	if err != nil {
		t.Fatalf("get day: %v", err)
	}
	if fetched == nil {
		t.Fatal("expected day, got nil")
	}
	if fetched.Notes != "test notes" {
		t.Errorf("notes round-trip: got %q, want %q", fetched.Notes, "test notes")
	}

	// Update via upsert.
	updated, err := UpsertDay(db, 1, "2026-03-27", false, "updated notes")
	if err != nil {
		t.Fatalf("upsert update: %v", err)
	}
	if updated.Lunch {
		t.Error("lunch after update: expected false")
	}
	if updated.Notes != "updated notes" {
		t.Errorf("notes after update: got %q, want %q", updated.Notes, "updated notes")
	}
}

func TestGetDay_NotFound(t *testing.T) {
	db := setupTestDB(t)

	day, err := GetDay(db, 1, "2026-01-01")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if day != nil {
		t.Errorf("expected nil for missing day, got %+v", day)
	}
}

func TestDeleteDay(t *testing.T) {
	db := setupTestDB(t)

	if _, err := UpsertDay(db, 1, "2026-03-27", false, ""); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := DeleteDay(db, 1, "2026-03-27"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	day, err := GetDay(db, 1, "2026-03-27")
	if err != nil {
		t.Fatalf("get after delete: %v", err)
	}
	if day != nil {
		t.Error("expected nil after delete")
	}
}

func TestDeleteDay_NotFound(t *testing.T) {
	db := setupTestDB(t)
	if err := DeleteDay(db, 1, "2026-01-01"); err != sql.ErrNoRows {
		t.Errorf("expected ErrNoRows, got %v", err)
	}
}

func TestAddAndDeleteSession(t *testing.T) {
	db := setupTestDB(t)

	day, err := UpsertDay(db, 1, "2026-03-27", false, "")
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}

	session, err := AddSession(db, day.ID, 1, "09:00", "17:00", 0)
	if err != nil {
		t.Fatalf("add session: %v", err)
	}
	if session.StartTime != "09:00" || session.EndTime != "17:00" {
		t.Errorf("session times: got %s-%s", session.StartTime, session.EndTime)
	}

	// Verify it's included in GetDay.
	fetched, err := GetDay(db, 1, "2026-03-27")
	if err != nil {
		t.Fatalf("get day: %v", err)
	}
	if len(fetched.Sessions) != 1 {
		t.Errorf("sessions count: got %d, want 1", len(fetched.Sessions))
	}

	if err := DeleteSession(db, session.ID, 1); err != nil {
		t.Fatalf("delete session: %v", err)
	}
	fetched2, err := GetDay(db, 1, "2026-03-27")
	if err != nil {
		t.Fatalf("get day after delete: %v", err)
	}
	if len(fetched2.Sessions) != 0 {
		t.Errorf("sessions after delete: got %d, want 0", len(fetched2.Sessions))
	}
}

func TestUpdateSession(t *testing.T) {
	db := setupTestDB(t)

	day, _ := UpsertDay(db, 1, "2026-03-27", false, "")
	session, _ := AddSession(db, day.ID, 1, "09:00", "17:00", 0)

	if err := UpdateSession(db, session.ID, 1, "08:30", "16:30", 1); err != nil {
		t.Fatalf("update session: %v", err)
	}

	fetched, _ := GetDay(db, 1, "2026-03-27")
	if fetched.Sessions[0].StartTime != "08:30" {
		t.Errorf("start_time after update: got %q, want %q", fetched.Sessions[0].StartTime, "08:30")
	}
	if fetched.Sessions[0].EndTime != "16:30" {
		t.Errorf("end_time after update: got %q, want %q", fetched.Sessions[0].EndTime, "16:30")
	}
}

func TestAddAndDeleteDeduction(t *testing.T) {
	db := setupTestDB(t)

	day, _ := UpsertDay(db, 1, "2026-03-27", false, "")
	deduction, err := AddDeduction(db, day.ID, 1, "Kindergarten", 15, nil)
	if err != nil {
		t.Fatalf("add deduction: %v", err)
	}
	if deduction.Name != "Kindergarten" {
		t.Errorf("name: got %q, want %q", deduction.Name, "Kindergarten")
	}
	if deduction.Minutes != 15 {
		t.Errorf("minutes: got %d, want 15", deduction.Minutes)
	}

	fetched, _ := GetDay(db, 1, "2026-03-27")
	if len(fetched.Deductions) != 1 {
		t.Errorf("deductions count: got %d, want 1", len(fetched.Deductions))
	}
	if fetched.Deductions[0].Name != "Kindergarten" {
		t.Errorf("deduction name round-trip: got %q, want %q", fetched.Deductions[0].Name, "Kindergarten")
	}

	if err := DeleteDeduction(db, deduction.ID, 1); err != nil {
		t.Fatalf("delete deduction: %v", err)
	}
	fetched2, _ := GetDay(db, 1, "2026-03-27")
	if len(fetched2.Deductions) != 0 {
		t.Errorf("deductions after delete: got %d, want 0", len(fetched2.Deductions))
	}
}

func TestCreateAndListPresets(t *testing.T) {
	db := setupTestDB(t)

	preset, err := CreatePreset(db, 1, "Kindergarten drop-off", 15, "car")
	if err != nil {
		t.Fatalf("create preset: %v", err)
	}
	if preset.Name != "Kindergarten drop-off" {
		t.Errorf("name: got %q", preset.Name)
	}

	presets, err := ListPresets(db, 1)
	if err != nil {
		t.Fatalf("list presets: %v", err)
	}
	if len(presets) != 1 {
		t.Fatalf("preset count: got %d, want 1", len(presets))
	}
	if presets[0].Name != "Kindergarten drop-off" {
		t.Errorf("preset name round-trip: got %q", presets[0].Name)
	}
}

func TestUpdatePreset(t *testing.T) {
	db := setupTestDB(t)

	preset, _ := CreatePreset(db, 1, "Doctor", 60, "stethoscope")

	if err := UpdatePreset(db, preset.ID, 1, "Doctor visit", 90, "stethoscope", true); err != nil {
		t.Fatalf("update preset: %v", err)
	}

	presets, _ := ListPresets(db, 1)
	if presets[0].Name != "Doctor visit" {
		t.Errorf("name after update: got %q", presets[0].Name)
	}
	if presets[0].DefaultMinutes != 90 {
		t.Errorf("minutes after update: got %d", presets[0].DefaultMinutes)
	}
}

func TestDeletePreset(t *testing.T) {
	db := setupTestDB(t)

	preset, _ := CreatePreset(db, 1, "Gym", 45, "dumbbell")
	if err := DeletePreset(db, preset.ID, 1); err != nil {
		t.Fatalf("delete preset: %v", err)
	}

	presets, _ := ListPresets(db, 1)
	if len(presets) != 0 {
		t.Errorf("presets after delete: got %d, want 0", len(presets))
	}
}

func TestListDaysInRange(t *testing.T) {
	db := setupTestDB(t)

	UpsertDay(db, 1, "2026-03-25", false, "")
	UpsertDay(db, 1, "2026-03-26", true, "")
	UpsertDay(db, 1, "2026-03-27", false, "")
	UpsertDay(db, 1, "2026-03-28", false, "") // out of range

	days, err := ListDaysInRange(db, 1, "2026-03-25", "2026-03-27")
	if err != nil {
		t.Fatalf("list days: %v", err)
	}
	if len(days) != 3 {
		t.Errorf("days count: got %d, want 3", len(days))
	}
	if days[1].Date != "2026-03-26" {
		t.Errorf("second day date: got %q", days[1].Date)
	}
	if !days[1].Lunch {
		t.Error("second day lunch: expected true")
	}
}

func TestSessionCascadeDeleteWithDay(t *testing.T) {
	db := setupTestDB(t)

	day, _ := UpsertDay(db, 1, "2026-03-27", false, "")
	AddSession(db, day.ID, 1, "09:00", "17:00", 0)

	// Deleting the day should cascade-delete the session.
	if err := DeleteDay(db, 1, "2026-03-27"); err != nil {
		t.Fatalf("delete day: %v", err)
	}

	var count int
	db.QueryRow("SELECT COUNT(*) FROM work_sessions WHERE day_id = ?", day.ID).Scan(&count)
	if count != 0 {
		t.Errorf("sessions after day cascade delete: got %d, want 0", count)
	}
}
