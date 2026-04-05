package stride

import (
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/encryption"
	_ "modernc.org/sqlite"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	t.Setenv("ENCRYPTION_KEY", "test-key-for-stride-tests")
	encryption.ResetEncryptionKey()
	t.Cleanup(func() { encryption.ResetEncryptionKey() })

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		t.Fatalf("enable fk: %v", err)
	}

	_, err = db.Exec(`
		CREATE TABLE users (
			id         INTEGER PRIMARY KEY,
			email      TEXT UNIQUE NOT NULL,
			name       TEXT NOT NULL,
			picture    TEXT NOT NULL DEFAULT '',
			google_id  TEXT UNIQUE NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			is_admin   INTEGER NOT NULL DEFAULT 0
		);
		CREATE TABLE stride_races (
			id          INTEGER PRIMARY KEY,
			user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			name        TEXT NOT NULL,
			date        TEXT NOT NULL,
			distance_m  REAL NOT NULL,
			target_time INTEGER,
			priority    TEXT NOT NULL DEFAULT 'B',
			notes       TEXT NOT NULL DEFAULT '',
			result_time INTEGER,
			created_at  TEXT NOT NULL DEFAULT ''
		);
		CREATE TABLE stride_plans (
			id          INTEGER PRIMARY KEY,
			user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			week_start  TEXT NOT NULL,
			week_end    TEXT NOT NULL,
			phase       TEXT NOT NULL DEFAULT '',
			plan_json   TEXT NOT NULL,
			prompt      TEXT NOT NULL DEFAULT '',
			response    TEXT NOT NULL DEFAULT '',
			model       TEXT NOT NULL DEFAULT '',
			created_at  TEXT NOT NULL DEFAULT '',
			UNIQUE(user_id, week_start)
		);
		CREATE TABLE stride_notes (
			id          INTEGER PRIMARY KEY,
			user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			plan_id     INTEGER REFERENCES stride_plans(id) ON DELETE SET NULL,
			content     TEXT NOT NULL,
			created_at  TEXT NOT NULL DEFAULT ''
		);
		CREATE TABLE workouts (
			id                  INTEGER PRIMARY KEY,
			user_id             INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			sport               TEXT NOT NULL DEFAULT 'other',
			sub_sport           TEXT NOT NULL DEFAULT '',
			is_indoor           INTEGER NOT NULL DEFAULT 0,
			title               TEXT NOT NULL DEFAULT '',
			title_source        TEXT NOT NULL DEFAULT '',
			started_at          TEXT NOT NULL DEFAULT '',
			duration_seconds    INTEGER NOT NULL DEFAULT 0,
			distance_meters     REAL NOT NULL DEFAULT 0,
			avg_heart_rate      INTEGER NOT NULL DEFAULT 0,
			max_heart_rate      INTEGER NOT NULL DEFAULT 0,
			avg_pace_sec_per_km REAL NOT NULL DEFAULT 0,
			avg_cadence         INTEGER NOT NULL DEFAULT 0,
			calories            INTEGER NOT NULL DEFAULT 0,
			ascent_meters       REAL NOT NULL DEFAULT 0,
			descent_meters      REAL NOT NULL DEFAULT 0,
			analysis_status     TEXT NOT NULL DEFAULT '',
			fit_file_hash       TEXT NOT NULL DEFAULT '',
			created_at          TEXT NOT NULL DEFAULT '',
			training_load       REAL,
			hr_drift_pct        REAL,
			pace_cv_pct         REAL,
			UNIQUE(user_id, fit_file_hash)
		);
		CREATE TABLE stride_evaluations (
			id          INTEGER PRIMARY KEY,
			user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			plan_id     INTEGER NOT NULL REFERENCES stride_plans(id) ON DELETE CASCADE,
			workout_id  INTEGER REFERENCES workouts(id) ON DELETE SET NULL,
			eval_json   TEXT NOT NULL,
			created_at  TEXT NOT NULL DEFAULT ''
		);
	`)
	if err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	_, err = db.Exec("INSERT INTO users (id, email, name, google_id) VALUES (1, 'test@example.com', 'Test', 'g123')")
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	return db
}

func TestCreateAndGetRace(t *testing.T) {
	db := setupTestDB(t)

	race, err := CreateRace(db, 1, "Bergen City Marathon", "2026-10-18", 42195, nil, "A", "Goal race")
	if err != nil {
		t.Fatalf("create race: %v", err)
	}
	if race.Name != "Bergen City Marathon" {
		t.Errorf("name = %q, want %q", race.Name, "Bergen City Marathon")
	}
	if race.Priority != "A" {
		t.Errorf("priority = %q, want %q", race.Priority, "A")
	}
	if race.Notes != "Goal race" {
		t.Errorf("notes = %q, want %q", race.Notes, "Goal race")
	}

	got, err := GetRaceByID(db, race.ID, 1)
	if err != nil {
		t.Fatalf("get race: %v", err)
	}
	if got.ID != race.ID {
		t.Errorf("id = %d, want %d", got.ID, race.ID)
	}
	if got.Name != "Bergen City Marathon" {
		t.Errorf("name = %q, want %q", got.Name, "Bergen City Marathon")
	}
}

func TestGetRaceWrongUser(t *testing.T) {
	db := setupTestDB(t)

	race, err := CreateRace(db, 1, "Private Race", "2026-05-01", 10000, nil, "C", "")
	if err != nil {
		t.Fatalf("create race: %v", err)
	}

	_, err = GetRaceByID(db, race.ID, 999)
	if err != sql.ErrNoRows {
		t.Errorf("expected ErrNoRows for wrong user, got %v", err)
	}
}

func TestListRaces(t *testing.T) {
	db := setupTestDB(t)

	if _, err := CreateRace(db, 1, "Race A", "2026-05-01", 5000, nil, "C", ""); err != nil {
		t.Fatalf("create A: %v", err)
	}
	if _, err := CreateRace(db, 1, "Race B", "2026-10-18", 42195, nil, "A", ""); err != nil {
		t.Fatalf("create B: %v", err)
	}

	races, err := ListRaces(db, 1)
	if err != nil {
		t.Fatalf("list races: %v", err)
	}
	if len(races) != 2 {
		t.Fatalf("got %d races, want 2", len(races))
	}
	// Should be ordered by date ascending.
	if races[0].Name != "Race A" {
		t.Errorf("first race = %q, want %q", races[0].Name, "Race A")
	}
}

func TestUpdateRace(t *testing.T) {
	db := setupTestDB(t)

	race, err := CreateRace(db, 1, "Old Name", "2026-05-01", 10000, nil, "C", "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	target := 3600
	updated, err := UpdateRace(db, race.ID, 1, "New Name", "2026-05-02", 21097.5, &target, "B", "Updated notes", nil)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Name != "New Name" {
		t.Errorf("name = %q, want %q", updated.Name, "New Name")
	}
	if updated.Priority != "B" {
		t.Errorf("priority = %q, want %q", updated.Priority, "B")
	}
	if updated.TargetTime == nil || *updated.TargetTime != 3600 {
		t.Errorf("target_time = %v, want 3600", updated.TargetTime)
	}
}

func TestUpdateRaceWrongUser(t *testing.T) {
	db := setupTestDB(t)

	race, err := CreateRace(db, 1, "Mine", "2026-05-01", 5000, nil, "C", "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	_, err = UpdateRace(db, race.ID, 999, "Hacked", "2026-05-01", 5000, nil, "C", "", nil)
	if err != sql.ErrNoRows {
		t.Errorf("expected ErrNoRows for wrong user, got %v", err)
	}
}

func TestDeleteRace(t *testing.T) {
	db := setupTestDB(t)

	race, err := CreateRace(db, 1, "Delete Me", "2026-05-01", 5000, nil, "C", "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := DeleteRace(db, race.ID, 1); err != nil {
		t.Fatalf("delete: %v", err)
	}

	_, err = GetRaceByID(db, race.ID, 1)
	if err != sql.ErrNoRows {
		t.Errorf("expected ErrNoRows after delete, got %v", err)
	}
}

func TestDeleteRaceWrongUser(t *testing.T) {
	db := setupTestDB(t)

	race, err := CreateRace(db, 1, "Keep", "2026-05-01", 5000, nil, "C", "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	err = DeleteRace(db, race.ID, 999)
	if err != sql.ErrNoRows {
		t.Errorf("expected ErrNoRows for wrong user, got %v", err)
	}
}

func TestCreateAndListNotes(t *testing.T) {
	db := setupTestDB(t)

	note, err := CreateNote(db, 1, nil, "Feeling tired this week")
	if err != nil {
		t.Fatalf("create note: %v", err)
	}
	if note.Content != "Feeling tired this week" {
		t.Errorf("content = %q, want %q", note.Content, "Feeling tired this week")
	}
	if note.PlanID != nil {
		t.Errorf("plan_id = %v, want nil", note.PlanID)
	}

	notes, err := ListNotes(db, 1, nil)
	if err != nil {
		t.Fatalf("list notes: %v", err)
	}
	if len(notes) != 1 {
		t.Fatalf("got %d notes, want 1", len(notes))
	}
	if notes[0].Content != "Feeling tired this week" {
		t.Errorf("content = %q, want %q", notes[0].Content, "Feeling tired this week")
	}
}

func TestDeleteNote(t *testing.T) {
	db := setupTestDB(t)

	note, err := CreateNote(db, 1, nil, "Delete me")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := DeleteNote(db, note.ID, 1); err != nil {
		t.Fatalf("delete: %v", err)
	}

	notes, err := ListNotes(db, 1, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(notes) != 0 {
		t.Errorf("expected 0 notes after delete, got %d", len(notes))
	}
}

func TestDeleteNoteWrongUser(t *testing.T) {
	db := setupTestDB(t)

	note, err := CreateNote(db, 1, nil, "Keep me")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	err = DeleteNote(db, note.ID, 999)
	if err != sql.ErrNoRows {
		t.Errorf("expected ErrNoRows for wrong user, got %v", err)
	}
}

func TestRaceCascadeDeleteUser(t *testing.T) {
	db := setupTestDB(t)

	if _, err := CreateRace(db, 1, "Cascade Race", "2026-05-01", 10000, nil, "C", ""); err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := db.Exec("DELETE FROM users WHERE id = 1"); err != nil {
		t.Fatalf("delete user: %v", err)
	}

	races, err := ListRaces(db, 1)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(races) != 0 {
		t.Errorf("expected 0 races after user delete, got %d", len(races))
	}
}

// insertTestPlan inserts a plan row directly and returns its ID.
func insertTestPlan(t *testing.T, db *sql.DB, userID int64, weekStart, weekEnd, planJSON string) int64 {
	t.Helper()
	res, err := db.Exec(`
		INSERT INTO stride_plans (user_id, week_start, week_end, phase, plan_json, model, created_at)
		VALUES (?, ?, ?, '', ?, 'test-model', '2026-04-05T00:00:00Z')
	`, userID, weekStart, weekEnd, planJSON)
	if err != nil {
		t.Fatalf("insertTestPlan: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

// --- Plan DB function tests ---

func TestListPlans_Empty(t *testing.T) {
	db := setupTestDB(t)
	plans, total, err := ListPlans(db, 1, 10, 0)
	if err != nil {
		t.Fatalf("ListPlans: %v", err)
	}
	if total != 0 {
		t.Errorf("total = %d, want 0", total)
	}
	if len(plans) != 0 {
		t.Errorf("len(plans) = %d, want 0", len(plans))
	}
}

func TestListPlans_Pagination(t *testing.T) {
	db := setupTestDB(t)

	insertTestPlan(t, db, 1, "2026-04-07", "2026-04-13", `[]`)
	insertTestPlan(t, db, 1, "2026-04-14", "2026-04-20", `[]`)
	insertTestPlan(t, db, 1, "2026-04-21", "2026-04-27", `[]`)

	// All plans.
	plans, total, err := ListPlans(db, 1, 10, 0)
	if err != nil {
		t.Fatalf("ListPlans: %v", err)
	}
	if total != 3 {
		t.Errorf("total = %d, want 3", total)
	}
	if len(plans) != 3 {
		t.Errorf("len(plans) = %d, want 3", len(plans))
	}
	// Newest first.
	if plans[0].WeekStart != "2026-04-21" {
		t.Errorf("plans[0].WeekStart = %q, want 2026-04-21", plans[0].WeekStart)
	}

	// Paginated.
	paged, total2, err := ListPlans(db, 1, 2, 1)
	if err != nil {
		t.Fatalf("ListPlans paginated: %v", err)
	}
	if total2 != 3 {
		t.Errorf("total2 = %d, want 3", total2)
	}
	if len(paged) != 2 {
		t.Errorf("len(paged) = %d, want 2", len(paged))
	}
}

func TestGetPlanByID_Found(t *testing.T) {
	db := setupTestDB(t)
	id := insertTestPlan(t, db, 1, "2026-04-07", "2026-04-13", `[]`)

	plan, err := GetPlanByID(db, id, 1)
	if err != nil {
		t.Fatalf("GetPlanByID: %v", err)
	}
	if plan.ID != id {
		t.Errorf("plan.ID = %d, want %d", plan.ID, id)
	}
	if plan.WeekStart != "2026-04-07" {
		t.Errorf("plan.WeekStart = %q, want 2026-04-07", plan.WeekStart)
	}
}

func TestGetPlanByID_WrongUser(t *testing.T) {
	db := setupTestDB(t)
	id := insertTestPlan(t, db, 1, "2026-04-07", "2026-04-13", `[]`)

	_, err := GetPlanByID(db, id, 999)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected ErrNoRows for wrong user, got %v", err)
	}
}

func TestGetCurrentPlan_Found(t *testing.T) {
	db := setupTestDB(t)
	insertTestPlan(t, db, 1, "2026-04-07", "2026-04-13", `[]`)

	// "2026-04-10" falls within 2026-04-07..2026-04-13.
	plan, err := GetCurrentPlan(db, 1, "2026-04-10")
	if err != nil {
		t.Fatalf("GetCurrentPlan: %v", err)
	}
	if plan == nil {
		t.Fatal("expected plan, got nil")
	}
	if plan.WeekStart != "2026-04-07" {
		t.Errorf("plan.WeekStart = %q, want 2026-04-07", plan.WeekStart)
	}
}

func TestGetCurrentPlan_NotFound(t *testing.T) {
	db := setupTestDB(t)
	insertTestPlan(t, db, 1, "2026-04-07", "2026-04-13", `[]`)

	// "2026-04-20" is outside the only plan's range.
	plan, err := GetCurrentPlan(db, 1, "2026-04-20")
	if err != nil {
		t.Fatalf("GetCurrentPlan: %v", err)
	}
	if plan != nil {
		t.Errorf("expected nil, got plan with week_start=%q", plan.WeekStart)
	}
}

func TestGetPlanByWeekStart_Found(t *testing.T) {
	db := setupTestDB(t)
	insertTestPlan(t, db, 1, "2026-04-07", "2026-04-13", `[]`)

	plan, err := getPlanByWeekStart(db, 1, "2026-04-07")
	if err != nil {
		t.Fatalf("getPlanByWeekStart: %v", err)
	}
	if plan == nil {
		t.Fatal("expected plan, got nil")
	}
}

func TestGetPlanByWeekStart_NotFound(t *testing.T) {
	db := setupTestDB(t)

	_, err := getPlanByWeekStart(db, 1, "2026-04-07")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected ErrNoRows, got %v", err)
	}
}

func TestNextStrideRun(t *testing.T) {
	oslo, err := time.LoadLocation("Europe/Oslo")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}

	tests := []struct {
		name          string
		now           time.Time
		want          time.Time
		maxFutureDiff time.Duration
	}{
		{
			name:          "Monday returns next Sunday",
			now:           time.Date(2026, 4, 6, 10, 0, 0, 0, oslo),  // Monday
			want:          time.Date(2026, 4, 12, 18, 0, 0, 0, oslo), // Next Sunday 18:00
			maxFutureDiff: 7 * 24 * time.Hour,
		},
		{
			name:          "Sunday before 18:00 returns same day",
			now:           time.Date(2026, 4, 5, 12, 0, 0, 0, oslo), // Sunday noon
			want:          time.Date(2026, 4, 5, 18, 0, 0, 0, oslo), // Same Sunday 18:00
			maxFutureDiff: 7 * 24 * time.Hour,
		},
		{
			name:          "Sunday exactly 18:00 returns next Sunday",
			now:           time.Date(2026, 4, 5, 18, 0, 0, 0, oslo),  // Sunday 18:00
			want:          time.Date(2026, 4, 12, 18, 0, 0, 0, oslo), // Next Sunday 18:00
			maxFutureDiff: 7 * 24 * time.Hour,
		},
		{
			name:          "Sunday after 18:00 returns next Sunday",
			now:           time.Date(2026, 4, 5, 19, 0, 0, 0, oslo),  // Sunday 19:00
			want:          time.Date(2026, 4, 12, 18, 0, 0, 0, oslo), // Next Sunday 18:00
			maxFutureDiff: 7 * 24 * time.Hour,
		},
		{
			name:          "Saturday returns next Sunday",
			now:           time.Date(2026, 4, 4, 23, 59, 0, 0, oslo), // Saturday
			want:          time.Date(2026, 4, 5, 18, 0, 0, 0, oslo),  // Next day Sunday 18:00
			maxFutureDiff: 7 * 24 * time.Hour,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := NextStrideRun(tc.now, oslo)
			if !got.Equal(tc.want) {
				t.Errorf("NextStrideRun(%v) = %v, want %v", tc.now, got, tc.want)
			}
			if !got.After(tc.now) {
				t.Errorf("next run %v is not after now %v", got, tc.now)
			}
			if got.Sub(tc.now) > tc.maxFutureDiff {
				t.Errorf("next run %v is more than %v after now %v", got, tc.maxFutureDiff, tc.now)
			}
		})
	}
}
