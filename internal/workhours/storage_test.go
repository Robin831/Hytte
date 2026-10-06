package workhours

import (
	"context"
	"database/sql"
	"errors"
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
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
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
		id               INTEGER PRIMARY KEY,
		day_id           INTEGER NOT NULL REFERENCES work_days(id) ON DELETE CASCADE,
		start_time       TEXT NOT NULL,
		end_time         TEXT NOT NULL,
		sort_order       INTEGER NOT NULL DEFAULT 0,
		is_internal      INTEGER NOT NULL DEFAULT 0,
		crosses_midnight INTEGER NOT NULL DEFAULT 0
	);

	CREATE TABLE IF NOT EXISTS work_deductions (
		id        INTEGER PRIMARY KEY,
		day_id    INTEGER NOT NULL REFERENCES work_days(id) ON DELETE CASCADE,
		name      TEXT NOT NULL,
		minutes   INTEGER NOT NULL,
		preset_id INTEGER REFERENCES work_deduction_presets(id) ON DELETE SET NULL
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

	CREATE TABLE IF NOT EXISTS work_leave_days (
		id         INTEGER PRIMARY KEY,
		user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		date       TEXT NOT NULL,
		leave_type TEXT NOT NULL,
		note       TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL DEFAULT '',
		UNIQUE(user_id, date)
	);

	CREATE TABLE IF NOT EXISTS work_open_sessions (
		id         INTEGER PRIMARY KEY,
		user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		date       TEXT NOT NULL,
		start_time TEXT NOT NULL,
		punched_at TEXT NOT NULL,
		UNIQUE(user_id)
	);

	CREATE TABLE IF NOT EXISTS work_flex_redemptions (
		id         INTEGER PRIMARY KEY,
		user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		date       TEXT NOT NULL,
		minutes    INTEGER NOT NULL,
		created_at TEXT NOT NULL DEFAULT ''
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

	session, err := AddSession(db, day.ID, 1, "09:00", "17:00", 0, false, false)
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
	session, _ := AddSession(db, day.ID, 1, "09:00", "17:00", 0, false, false)

	if err := UpdateSession(db, session.ID, 1, "08:30", "16:30", 1, false, false); err != nil {
		t.Fatalf("update session: %v", err)
	}

	fetched, _ := GetDay(db, 1, "2026-03-27")
	if fetched.Sessions[0].StartTime != "08:30" {
		t.Errorf("start_time after update: got %q, want %q", fetched.Sessions[0].StartTime, "08:30")
	}
	if fetched.Sessions[0].EndTime != "16:30" {
		t.Errorf("end_time after update: got %q, want %q", fetched.Sessions[0].EndTime, "16:30")
	}

	// Verify toggling is_internal is persisted and round-trips via GetDay.
	if err := UpdateSession(db, session.ID, 1, "08:30", "16:30", 1, true, false); err != nil {
		t.Fatalf("update session with is_internal=true: %v", err)
	}
	fetched, _ = GetDay(db, 1, "2026-03-27")
	if !fetched.Sessions[0].IsInternal {
		t.Errorf("is_internal after update to true: got false, want true")
	}

	if err := UpdateSession(db, session.ID, 1, "08:30", "16:30", 1, false, false); err != nil {
		t.Fatalf("update session with is_internal=false: %v", err)
	}
	fetched, _ = GetDay(db, 1, "2026-03-27")
	if fetched.Sessions[0].IsInternal {
		t.Errorf("is_internal after update to false: got true, want false")
	}
}

func TestSessionCrossesMidnightRoundTrip(t *testing.T) {
	db := setupTestDB(t)

	day, err := UpsertDay(db, 1, "2026-03-27", false, "")
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}

	// A session written without the flag reads back as false.
	plain, err := AddSession(db, day.ID, 1, "09:00", "17:00", 0, false, false)
	if err != nil {
		t.Fatalf("add plain session: %v", err)
	}
	if plain.CrossesMidnight {
		t.Error("crosses_midnight on returned session: got true, want false")
	}

	// A session written with the flag reads back as true.
	wrapped, err := AddSession(db, day.ID, 1, "22:00", "02:00", 1, false, true)
	if err != nil {
		t.Fatalf("add wrapped session: %v", err)
	}

	fetched, err := GetDay(db, 1, "2026-03-27")
	if err != nil {
		t.Fatalf("get day: %v", err)
	}
	if len(fetched.Sessions) != 2 {
		t.Fatalf("sessions count: got %d, want 2", len(fetched.Sessions))
	}
	if fetched.Sessions[0].CrossesMidnight {
		t.Error("plain session crosses_midnight after read: got true, want false")
	}
	if !fetched.Sessions[1].CrossesMidnight {
		t.Error("wrapped session crosses_midnight after read: got false, want true")
	}

	// Two crosses-midnight sessions on one day always overlap, so remove the
	// wrapped one before toggling the flag on the plain session.
	if err := DeleteSession(db, wrapped.ID, 1); err != nil {
		t.Fatalf("delete wrapped session: %v", err)
	}

	// The flag can be toggled off and on again via UpdateSession.
	if err := UpdateSession(db, plain.ID, 1, "22:00", "02:00", 0, false, true); err != nil {
		t.Fatalf("update session with crosses_midnight=true: %v", err)
	}
	fetched, _ = GetDay(db, 1, "2026-03-27")
	if !fetched.Sessions[0].CrossesMidnight {
		t.Error("crosses_midnight after update to true: got false, want true")
	}

	if err := UpdateSession(db, plain.ID, 1, "09:00", "17:00", 0, false, false); err != nil {
		t.Fatalf("update session with crosses_midnight=false: %v", err)
	}
	fetched, _ = GetDay(db, 1, "2026-03-27")
	if fetched.Sessions[0].CrossesMidnight {
		t.Error("crosses_midnight after update to false: got true, want false")
	}
}

func TestListDaysInRange_CrossesMidnight(t *testing.T) {
	db := setupTestDB(t)

	day, err := UpsertDay(db, 1, "2026-03-27", false, "")
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if _, err := AddSession(db, day.ID, 1, "22:00", "02:00", 0, false, true); err != nil {
		t.Fatalf("add wrapped session: %v", err)
	}

	days, err := ListDaysInRange(db, 1, "2026-03-27", "2026-03-27")
	if err != nil {
		t.Fatalf("list days in range: %v", err)
	}
	if len(days) != 1 || len(days[0].Sessions) != 1 {
		t.Fatalf("unexpected result: %+v", days)
	}
	if !days[0].Sessions[0].CrossesMidnight {
		t.Error("crosses_midnight from batched load: got false, want true")
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

	updated, err := UpdatePreset(db, preset.ID, 1, "Doctor visit", 90, "stethoscope", true)
	if err != nil {
		t.Fatalf("update preset: %v", err)
	}
	if updated.Name != "Doctor visit" {
		t.Errorf("returned name: got %q", updated.Name)
	}
	if updated.DefaultMinutes != 90 {
		t.Errorf("returned minutes: got %d", updated.DefaultMinutes)
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

// badCiphertext is a syntactically valid enc: value whose AES-GCM authentication
// will always fail, used to exercise decrypt-error handling paths.
const badCiphertext = "enc:dGhpcyBpcyBub3QgYSB2YWxpZCBjaXBoZXJ0ZXh0"

func TestGetDay_DecryptFailure_Notes(t *testing.T) {
	db := setupTestDB(t)

	// Insert a day with corrupted notes ciphertext directly.
	_, err := db.Exec(
		`INSERT INTO work_days (id, user_id, date, lunch, notes, created_at) VALUES (100, 1, '2026-04-01', 0, ?, '2026-04-01T00:00:00Z')`,
		badCiphertext,
	)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	day, err := GetDay(db, 1, "2026-04-01")
	if err != nil {
		t.Fatalf("GetDay with bad notes ciphertext should not error: %v", err)
	}
	if day == nil {
		t.Fatal("expected day, got nil")
	}
	if day.Notes != "" {
		t.Errorf("notes with bad ciphertext: got %q, want empty string", day.Notes)
	}
}

func TestGetDay_DecryptFailure_DeductionName(t *testing.T) {
	db := setupTestDB(t)

	day, err := UpsertDay(db, 1, "2026-04-01", false, "")
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}

	// Insert a deduction with corrupted name ciphertext directly.
	_, err = db.Exec(
		`INSERT INTO work_deductions (day_id, name, minutes) VALUES (?, ?, 15)`,
		day.ID, badCiphertext,
	)
	if err != nil {
		t.Fatalf("insert deduction: %v", err)
	}

	fetched, err := GetDay(db, 1, "2026-04-01")
	if err != nil {
		t.Fatalf("GetDay with bad deduction ciphertext should not error: %v", err)
	}
	if len(fetched.Deductions) != 1 {
		t.Fatalf("expected 1 deduction, got %d", len(fetched.Deductions))
	}
	if fetched.Deductions[0].Name != "" {
		t.Errorf("deduction name with bad ciphertext: got %q, want empty string", fetched.Deductions[0].Name)
	}
}

func TestListPresets_DecryptFailure(t *testing.T) {
	db := setupTestDB(t)

	// Insert a preset with corrupted name ciphertext directly.
	_, err := db.Exec(
		`INSERT INTO work_deduction_presets (id, user_id, name, default_minutes, icon, sort_order, active) VALUES (100, 1, ?, 15, 'clock', 0, 1)`,
		badCiphertext,
	)
	if err != nil {
		t.Fatalf("insert preset: %v", err)
	}

	presets, err := ListPresets(db, 1)
	if err != nil {
		t.Fatalf("ListPresets with bad ciphertext should not error: %v", err)
	}
	if len(presets) != 1 {
		t.Fatalf("expected 1 preset, got %d", len(presets))
	}
	if presets[0].Name != "" {
		t.Errorf("preset name with bad ciphertext: got %q, want empty string", presets[0].Name)
	}
}

func TestListDaysInRange_DecryptFailure(t *testing.T) {
	db := setupTestDB(t)

	// Insert a day with corrupted notes ciphertext directly.
	_, err := db.Exec(
		`INSERT INTO work_days (id, user_id, date, lunch, notes, created_at) VALUES (100, 1, '2026-04-01', 0, ?, '2026-04-01T00:00:00Z')`,
		badCiphertext,
	)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	days, err := ListDaysInRange(db, 1, "2026-04-01", "2026-04-01")
	if err != nil {
		t.Fatalf("ListDaysInRange with bad notes ciphertext should not error: %v", err)
	}
	if len(days) != 1 {
		t.Fatalf("expected 1 day, got %d", len(days))
	}
	if days[0].Notes != "" {
		t.Errorf("notes with bad ciphertext: got %q, want empty string", days[0].Notes)
	}
}

func TestListDaysInRange_DecryptFailure_DeductionName(t *testing.T) {
	db := setupTestDB(t)

	// Insert a valid day then a deduction with corrupted name ciphertext.
	_, err := db.Exec(
		`INSERT INTO work_days (id, user_id, date, lunch, notes, created_at) VALUES (100, 1, '2026-04-01', 0, '', '2026-04-01T00:00:00Z')`,
	)
	if err != nil {
		t.Fatalf("insert day: %v", err)
	}
	_, err = db.Exec(
		`INSERT INTO work_deductions (day_id, name, minutes) VALUES (100, ?, 15)`,
		badCiphertext,
	)
	if err != nil {
		t.Fatalf("insert deduction: %v", err)
	}

	days, err := ListDaysInRange(db, 1, "2026-04-01", "2026-04-01")
	if err != nil {
		t.Fatalf("ListDaysInRange with bad deduction ciphertext should not error: %v", err)
	}
	if len(days) != 1 {
		t.Fatalf("expected 1 day, got %d", len(days))
	}
	if len(days[0].Deductions) != 1 {
		t.Fatalf("expected 1 deduction, got %d", len(days[0].Deductions))
	}
	if days[0].Deductions[0].Name != "" {
		t.Errorf("deduction name with bad ciphertext: got %q, want empty string", days[0].Deductions[0].Name)
	}
}

func TestSessionCascadeDeleteWithDay(t *testing.T) {
	db := setupTestDB(t)

	day, _ := UpsertDay(db, 1, "2026-03-27", false, "")
	AddSession(db, day.ID, 1, "09:00", "17:00", 0, false, false)

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

func TestUpsertAndGetLeaveDay(t *testing.T) {
	db := setupTestDB(t)

	ld, err := UpsertLeaveDay(db, 1, "2026-07-01", LeaveTypeVacation, "summer vacation")
	if err != nil {
		t.Fatalf("upsert leave day: %v", err)
	}
	if ld == nil {
		t.Fatal("expected leave day, got nil")
	}
	if ld.Date != "2026-07-01" {
		t.Errorf("date: got %q, want %q", ld.Date, "2026-07-01")
	}
	if ld.LeaveType != LeaveTypeVacation {
		t.Errorf("leave_type: got %q, want %q", ld.LeaveType, LeaveTypeVacation)
	}
	if ld.Note != "summer vacation" {
		t.Errorf("note: got %q, want %q", ld.Note, "summer vacation")
	}

	// Fetch back.
	fetched, err := GetLeaveDay(db, 1, "2026-07-01")
	if err != nil {
		t.Fatalf("get leave day: %v", err)
	}
	if fetched == nil {
		t.Fatal("expected leave day, got nil")
	}
	if fetched.Note != "summer vacation" {
		t.Errorf("note round-trip: got %q, want %q", fetched.Note, "summer vacation")
	}

	// Update via upsert.
	updated, err := UpsertLeaveDay(db, 1, "2026-07-01", LeaveTypeSick, "sick day")
	if err != nil {
		t.Fatalf("upsert update leave day: %v", err)
	}
	if updated.LeaveType != LeaveTypeSick {
		t.Errorf("updated leave_type: got %q, want %q", updated.LeaveType, LeaveTypeSick)
	}
}

func TestDeleteLeaveDay(t *testing.T) {
	db := setupTestDB(t)

	if _, err := UpsertLeaveDay(db, 1, "2026-07-02", LeaveTypePersonal, ""); err != nil {
		t.Fatalf("upsert leave day: %v", err)
	}
	if err := DeleteLeaveDay(db, 1, "2026-07-02"); err != nil {
		t.Fatalf("delete leave day: %v", err)
	}
	fetched, err := GetLeaveDay(db, 1, "2026-07-02")
	if err != nil {
		t.Fatalf("get after delete: %v", err)
	}
	if fetched != nil {
		t.Error("expected nil after delete")
	}

	// Delete non-existent returns ErrNoRows.
	if err := DeleteLeaveDay(db, 1, "2026-07-02"); err == nil {
		t.Error("expected ErrNoRows for missing leave day")
	}
}

func TestListLeaveDaysAndBalance(t *testing.T) {
	db := setupTestDB(t)

	for _, args := range []struct {
		date string
		lt   LeaveType
	}{
		{"2026-07-01", LeaveTypeVacation},
		{"2026-07-02", LeaveTypeVacation},
		{"2026-07-03", LeaveTypeSick},
		{"2026-08-01", LeaveTypePersonal},
	} {
		if _, err := UpsertLeaveDay(db, 1, args.date, args.lt, ""); err != nil {
			t.Fatalf("upsert leave day %s: %v", args.date, err)
		}
	}

	days, err := ListLeaveDays(db, 1, "2026-07-01", "2026-07-31")
	if err != nil {
		t.Fatalf("list leave days: %v", err)
	}
	if len(days) != 3 {
		t.Errorf("list: got %d days, want 3", len(days))
	}

	balance, err := GetLeaveBalance(db, 1, 2026, 25)
	if err != nil {
		t.Fatalf("get leave balance: %v", err)
	}
	if balance.VacationAllowance != 25 {
		t.Errorf("allowance: got %d, want 25", balance.VacationAllowance)
	}
	if balance.VacationUsed != 2 {
		t.Errorf("vacation_used: got %d, want 2", balance.VacationUsed)
	}
	if balance.SickUsed != 1 {
		t.Errorf("sick_used: got %d, want 1", balance.SickUsed)
	}
	if balance.PersonalUsed != 1 {
		t.Errorf("personal_used: got %d, want 1", balance.PersonalUsed)
	}
}

// --- OpenSession (punch-in persistence) ---

func TestCreateAndGetOpenSession(t *testing.T) {
	db := setupTestDB(t)

	// No session yet.
	s, err := GetOpenSession(db, 1)
	if err != nil {
		t.Fatalf("GetOpenSession (empty): %v", err)
	}
	if s != nil {
		t.Fatal("expected nil session before any punch-in")
	}

	// Create a session.
	created, err := CreateOpenSession(db, 1, "2026-03-30", "08:00")
	if err != nil {
		t.Fatalf("CreateOpenSession: %v", err)
	}
	if created == nil {
		t.Fatal("CreateOpenSession returned nil")
	}
	if created.Date != "2026-03-30" {
		t.Errorf("Date: got %q, want %q", created.Date, "2026-03-30")
	}
	if created.StartTime != "08:00" {
		t.Errorf("StartTime: got %q, want %q", created.StartTime, "08:00")
	}
	if created.UserID != 1 {
		t.Errorf("UserID: got %d, want 1", created.UserID)
	}

	// Get should return the same session.
	got, err := GetOpenSession(db, 1)
	if err != nil {
		t.Fatalf("GetOpenSession: %v", err)
	}
	if got == nil {
		t.Fatal("expected session, got nil")
	}
	if got.StartTime != "08:00" {
		t.Errorf("StartTime: got %q, want %q", got.StartTime, "08:00")
	}
}

func TestCreateOpenSession_ReplacesExisting(t *testing.T) {
	db := setupTestDB(t)

	if _, err := CreateOpenSession(db, 1, "2026-03-30", "08:00"); err != nil {
		t.Fatalf("first CreateOpenSession: %v", err)
	}
	// Replace with a new time.
	if _, err := CreateOpenSession(db, 1, "2026-03-30", "09:30"); err != nil {
		t.Fatalf("second CreateOpenSession: %v", err)
	}

	got, err := GetOpenSession(db, 1)
	if err != nil {
		t.Fatalf("GetOpenSession: %v", err)
	}
	if got.StartTime != "09:30" {
		t.Errorf("expected updated StartTime 09:30, got %q", got.StartTime)
	}
}

func TestDeleteOpenSession(t *testing.T) {
	db := setupTestDB(t)

	if _, err := CreateOpenSession(db, 1, "2026-03-30", "08:00"); err != nil {
		t.Fatalf("CreateOpenSession: %v", err)
	}

	if err := DeleteOpenSession(db, 1); err != nil {
		t.Fatalf("DeleteOpenSession: %v", err)
	}

	got, err := GetOpenSession(db, 1)
	if err != nil {
		t.Fatalf("GetOpenSession after delete: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil after delete, got %+v", got)
	}
}

func TestDeleteOpenSession_NoSession(t *testing.T) {
	db := setupTestDB(t)
	// Deleting when nothing exists should not return an error.
	if err := DeleteOpenSession(db, 1); err != nil {
		t.Errorf("DeleteOpenSession (none): unexpected error: %v", err)
	}
}

func TestUpdateOpenSessionStartTime(t *testing.T) {
	db := setupTestDB(t)

	// No session → sql.ErrNoRows
	_, err := UpdateOpenSessionStartTime(db, 1, "07:30")
	if err != sql.ErrNoRows {
		t.Fatalf("expected sql.ErrNoRows, got %v", err)
	}

	// Create a session and update it
	if _, err := CreateOpenSession(db, 1, "2026-03-30", "08:00"); err != nil {
		t.Fatalf("CreateOpenSession: %v", err)
	}
	s, err := UpdateOpenSessionStartTime(db, 1, "07:30")
	if err != nil {
		t.Fatalf("UpdateOpenSessionStartTime: %v", err)
	}
	if s.StartTime != "07:30" {
		t.Errorf("StartTime: got %q, want %q", s.StartTime, "07:30")
	}
	if s.Date != "2026-03-30" {
		t.Errorf("Date: got %q, want %q", s.Date, "2026-03-30")
	}

	// Verify persistence via GetOpenSession
	s2, err := GetOpenSession(db, 1)
	if err != nil {
		t.Fatalf("GetOpenSession after update: %v", err)
	}
	if s2.StartTime != "07:30" {
		t.Errorf("GetOpenSession StartTime: got %q, want %q", s2.StartTime, "07:30")
	}
}

// --- Flex Redemptions ---

func TestCreateAndGetFlexRedemptions(t *testing.T) {
	db := setupTestDB(t)

	r, err := CreateFlexRedemption(context.Background(), db, 1, "2026-04-01", 30)
	if err != nil {
		t.Fatalf("create flex redemption: %v", err)
	}
	if r.Minutes != 30 {
		t.Errorf("minutes: got %d, want 30", r.Minutes)
	}
	if r.Date != "2026-04-01" {
		t.Errorf("date: got %q, want %q", r.Date, "2026-04-01")
	}

	redemptions, err := GetFlexRedemptions(db, 1, "2026-01-01")
	if err != nil {
		t.Fatalf("get flex redemptions: %v", err)
	}
	if len(redemptions) != 1 {
		t.Fatalf("count: got %d, want 1", len(redemptions))
	}
	if redemptions[0].Minutes != 30 {
		t.Errorf("minutes: got %d, want 30", redemptions[0].Minutes)
	}
}

func TestSumFlexRedemptions(t *testing.T) {
	db := setupTestDB(t)

	// No redemptions => 0.
	total, err := SumFlexRedemptions(db, 1, "2026-01-01")
	if err != nil {
		t.Fatalf("sum empty: %v", err)
	}
	if total != 0 {
		t.Errorf("empty sum: got %d, want 0", total)
	}

	// Add two redemptions.
	if _, err := CreateFlexRedemption(context.Background(), db, 1, "2026-04-01", 30); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := CreateFlexRedemption(context.Background(), db, 1, "2026-04-02", 30); err != nil {
		t.Fatalf("create: %v", err)
	}

	total, err = SumFlexRedemptions(db, 1, "2026-01-01")
	if err != nil {
		t.Fatalf("sum: %v", err)
	}
	if total != 60 {
		t.Errorf("sum: got %d, want 60", total)
	}

	// With a fromDate that excludes the first redemption.
	total, err = SumFlexRedemptions(db, 1, "2026-04-02")
	if err != nil {
		t.Fatalf("sum: %v", err)
	}
	if total != 30 {
		t.Errorf("sum from 04-02: got %d, want 30", total)
	}
}

func TestGetFlexRedemptions_FiltersByFromDate(t *testing.T) {
	db := setupTestDB(t)

	if _, err := CreateFlexRedemption(context.Background(), db, 1, "2026-03-01", 30); err != nil {
		t.Fatalf("create first redemption: %v", err)
	}
	if _, err := CreateFlexRedemption(context.Background(), db, 1, "2026-04-01", 30); err != nil {
		t.Fatalf("create second redemption: %v", err)
	}

	redemptions, err := GetFlexRedemptions(db, 1, "2026-04-01")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(redemptions) != 1 {
		t.Errorf("count: got %d, want 1", len(redemptions))
	}
}

func TestSessionsOverlap(t *testing.T) {
	tests := []struct {
		name         string
		aStart, aEnd string
		aCross       bool
		bStart, bEnd string
		bCross       bool
		want         bool
	}{
		{"exact match", "08:00", "12:00", false, "08:00", "12:00", false, true},
		{"partial overlap at front", "07:00", "09:00", false, "08:00", "12:00", false, true},
		{"partial overlap at back", "11:00", "13:00", false, "08:00", "12:00", false, true},
		{"a contains b", "08:00", "16:00", false, "10:00", "12:00", false, true},
		{"b contains a", "10:00", "12:00", false, "08:00", "16:00", false, true},
		{"adjacent a before b", "08:00", "12:00", false, "12:00", "16:00", false, false},
		{"adjacent b before a", "12:00", "16:00", false, "08:00", "12:00", false, false},
		{"disjoint", "08:00", "10:00", false, "13:00", "16:00", false, false},
		{"crosses midnight overlaps late session", "22:00", "02:00", true, "23:00", "23:30", false, true},
		{"crosses midnight adjacent to evening session", "22:00", "02:00", true, "18:00", "22:00", false, false},
		{"two crosses midnight sessions overlap", "22:00", "02:00", true, "23:00", "01:00", true, true},
		// Both sessions sit on the timeline of the day they started on: the
		// post-midnight tail (D+1) of 22:00-02:00 is not compared with an
		// early-morning session on D.
		{"crosses midnight tail vs same-day morning", "22:00", "02:00", true, "01:00", "03:00", false, false},
		{"same-day morning vs crosses midnight tail", "01:00", "03:00", false, "22:00", "02:00", true, false},
		// A legacy row with end <= start but no crosses_midnight flag counts
		// as 0 minutes in CalculateDay, so it must not block anything.
		{"unflagged end before start is empty", "22:00", "02:00", false, "23:00", "23:30", false, false},
		{"unflagged end before start is empty (reversed)", "23:00", "23:30", false, "22:00", "02:00", false, false},
		{"zero-length session", "10:00", "10:00", false, "08:00", "12:00", false, false},
		{"malformed time", "bad", "12:00", false, "08:00", "12:00", false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := sessionsOverlap(tc.aStart, tc.aEnd, tc.aCross, tc.bStart, tc.bEnd, tc.bCross); got != tc.want {
				t.Errorf("sessionsOverlap(%s-%s/%v, %s-%s/%v) = %v, want %v", tc.aStart, tc.aEnd, tc.aCross, tc.bStart, tc.bEnd, tc.bCross, got, tc.want)
			}
		})
	}
}

// insertSecondUser adds a second user (id 2) for cross-user isolation tests.
func insertSecondUser(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec(
		`INSERT INTO users (id, email, name, google_id) VALUES (2, 'other@example.com', 'Other', 'google-2')`,
	); err != nil {
		t.Fatalf("insert user 2: %v", err)
	}
}

func TestFindOverlappingSession(t *testing.T) {
	db := setupTestDB(t)
	insertSecondUser(t, db)

	day, err := UpsertDay(db, 1, "2026-03-27", false, "")
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	emptyDay, err := UpsertDay(db, 1, "2026-03-28", false, "")
	if err != nil {
		t.Fatalf("upsert empty day: %v", err)
	}
	otherUserDay, err := UpsertDay(db, 2, "2026-03-27", false, "")
	if err != nil {
		t.Fatalf("upsert other user day: %v", err)
	}

	morning, err := AddSession(db, day.ID, 1, "08:00", "12:00", 0, false, false)
	if err != nil {
		t.Fatalf("add morning: %v", err)
	}
	meeting, err := AddSession(db, day.ID, 1, "13:00", "14:00", 1, true, false)
	if err != nil {
		t.Fatalf("add meeting: %v", err)
	}
	if _, err := AddSession(db, otherUserDay.ID, 2, "15:00", "17:00", 0, false, false); err != nil {
		t.Fatalf("add other user session: %v", err)
	}

	tests := []struct {
		name       string
		dayID      int64
		start, end string
		excludeID  int64
		wantID     int64 // 0 = no conflict
	}{
		{"empty day", emptyDay.ID, "08:00", "12:00", 0, 0},
		{"overlaps morning", day.ID, "11:00", "16:00", 0, morning.ID},
		{"self exclusion on update", day.ID, "09:00", "11:00", morning.ID, 0},
		{"excluding one still finds another", day.ID, "11:00", "13:30", morning.ID, meeting.ID},
		{"non-internal overlaps internal session", day.ID, "13:30", "15:00", 0, meeting.ID},
		{"adjacent is allowed", day.ID, "12:00", "13:00", 0, 0},
		{"other user's sessions ignored", day.ID, "15:00", "17:00", 0, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := FindOverlappingSession(db, 1, tc.dayID, tc.start, tc.end, false, tc.excludeID)
			if err != nil {
				t.Fatalf("find overlap: %v", err)
			}
			if tc.wantID == 0 {
				if got != nil {
					t.Fatalf("expected no conflict, got session %d (%s-%s)", got.ID, got.StartTime, got.EndTime)
				}
				return
			}
			if got == nil {
				t.Fatalf("expected conflict with session %d, got nil", tc.wantID)
			}
			if got.ID != tc.wantID {
				t.Errorf("conflict id: got %d, want %d", got.ID, tc.wantID)
			}
		})
	}

	// A day owned by another user is reported as not found.
	if _, err := FindOverlappingSession(db, 1, otherUserDay.ID, "15:00", "17:00", false, 0); err != sql.ErrNoRows {
		t.Errorf("other user's day: expected sql.ErrNoRows, got %v", err)
	}
}

func TestGetSessionDayID(t *testing.T) {
	db := setupTestDB(t)
	insertSecondUser(t, db)

	day, err := UpsertDay(db, 1, "2026-03-27", false, "")
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	session, err := AddSession(db, day.ID, 1, "08:00", "12:00", 0, false, false)
	if err != nil {
		t.Fatalf("add session: %v", err)
	}

	got, err := GetSessionDayID(db, session.ID, 1)
	if err != nil {
		t.Fatalf("get day id: %v", err)
	}
	if got != day.ID {
		t.Errorf("day id: got %d, want %d", got, day.ID)
	}
	if _, err := GetSessionDayID(db, session.ID, 2); err != sql.ErrNoRows {
		t.Errorf("other user: expected sql.ErrNoRows, got %v", err)
	}
}

func TestFindOverlappingSessionCrossesMidnightNotCheckedAgainstNextDay(t *testing.T) {
	db := setupTestDB(t)

	nextDay, err := UpsertDay(db, 1, "2026-03-28", false, "")
	if err != nil {
		t.Fatalf("upsert next day: %v", err)
	}
	if _, err := AddSession(db, nextDay.ID, 1, "01:00", "03:00", 0, false, false); err != nil {
		t.Fatalf("add next-day session: %v", err)
	}
	day, err := UpsertDay(db, 1, "2026-03-27", false, "")
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}

	// Deliberate limitation: overlap is checked within one work day only, so
	// the tail of a 22:00-02:00 session on D is not compared with D+1.
	got, err := FindOverlappingSession(db, 1, day.ID, "22:00", "02:00", true, 0)
	if err != nil {
		t.Fatalf("find overlap: %v", err)
	}
	if got != nil {
		t.Errorf("expected no conflict across days, got session %d", got.ID)
	}
}

func TestFindOverlappingSessionUsesStoredCrossesMidnightFlag(t *testing.T) {
	db := setupTestDB(t)

	day, err := UpsertDay(db, 1, "2026-03-27", false, "")
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	// Legacy row: end <= start without the flag. CalculateDay counts it as 0
	// minutes, so it must not block new sessions either.
	if _, err := db.Exec(
		`INSERT INTO work_sessions (day_id, start_time, end_time, sort_order, is_internal, crosses_midnight) VALUES (?, '22:00', '02:00', 0, 0, 0)`,
		day.ID,
	); err != nil {
		t.Fatalf("insert legacy row: %v", err)
	}
	got, err := FindOverlappingSession(db, 1, day.ID, "23:00", "23:30", false, 0)
	if err != nil {
		t.Fatalf("find overlap: %v", err)
	}
	if got != nil {
		t.Errorf("legacy unflagged row should not conflict, got session %d", got.ID)
	}

	// The same times stored with the flag do conflict.
	flagged, err := AddSession(db, day.ID, 1, "18:00", "20:00", 1, false, false)
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := UpdateSession(db, flagged.ID, 1, "22:00", "02:00", 1, false, true); err != nil {
		t.Fatalf("update to crosses midnight: %v", err)
	}
	got, err = FindOverlappingSession(db, 1, day.ID, "23:00", "23:30", false, 0)
	if err != nil {
		t.Fatalf("find overlap: %v", err)
	}
	if got == nil || got.ID != flagged.ID {
		t.Errorf("expected conflict with flagged session %d, got %+v", flagged.ID, got)
	}
}

func TestAddSessionRejectsOverlap(t *testing.T) {
	db := setupTestDB(t)

	day, err := UpsertDay(db, 1, "2026-03-27", false, "")
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	existing, err := AddSession(db, day.ID, 1, "08:00", "12:00", 0, false, false)
	if err != nil {
		t.Fatalf("add: %v", err)
	}

	_, err = AddSession(db, day.ID, 1, "11:00", "13:00", 1, false, false)
	var overlap *OverlapError
	if !errors.As(err, &overlap) {
		t.Fatalf("expected *OverlapError, got %v", err)
	}
	if overlap.Conflict.ID != existing.ID {
		t.Errorf("conflict id: got %d, want %d", overlap.Conflict.ID, existing.ID)
	}

	got, err := GetDay(db, 1, "2026-03-27")
	if err != nil {
		t.Fatalf("get day: %v", err)
	}
	if len(got.Sessions) != 1 {
		t.Errorf("sessions after rejected add: got %d, want 1", len(got.Sessions))
	}
}

func TestUpdateSessionRejectsOverlap(t *testing.T) {
	db := setupTestDB(t)

	day, err := UpsertDay(db, 1, "2026-03-27", false, "")
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	morning, err := AddSession(db, day.ID, 1, "08:00", "12:00", 0, false, false)
	if err != nil {
		t.Fatalf("add morning: %v", err)
	}
	afternoon, err := AddSession(db, day.ID, 1, "13:00", "16:00", 1, false, false)
	if err != nil {
		t.Fatalf("add afternoon: %v", err)
	}

	err = UpdateSession(db, afternoon.ID, 1, "11:00", "16:00", 1, false, false)
	var overlap *OverlapError
	if !errors.As(err, &overlap) {
		t.Fatalf("expected *OverlapError, got %v", err)
	}
	if overlap.Conflict.ID != morning.ID {
		t.Errorf("conflict id: got %d, want %d", overlap.Conflict.ID, morning.ID)
	}

	// Moving a session within its own old range is not a self-conflict.
	if err := UpdateSession(db, morning.ID, 1, "09:00", "12:00", 0, false, false); err != nil {
		t.Errorf("self update: %v", err)
	}
	if err := UpdateSession(db, 99999, 1, "09:00", "12:00", 0, false, false); err != sql.ErrNoRows {
		t.Errorf("missing session: expected sql.ErrNoRows, got %v", err)
	}
}
