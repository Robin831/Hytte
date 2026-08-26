package stride

import (
	"context"
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
			id              INTEGER PRIMARY KEY,
			user_id         INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			week_start      TEXT NOT NULL,
			week_end        TEXT NOT NULL,
			phase           TEXT NOT NULL DEFAULT '',
			plan_json       TEXT NOT NULL,
			prompt          TEXT NOT NULL DEFAULT '',
			response        TEXT NOT NULL DEFAULT '',
			model           TEXT NOT NULL DEFAULT '',
			chat_session_id TEXT NOT NULL DEFAULT '',
			chat_session_msg_floor INTEGER NOT NULL DEFAULT 0,
			macro_week_id   INTEGER REFERENCES stride_macro_weeks(id) ON DELETE SET NULL,
			adjustment_summary TEXT NOT NULL DEFAULT '',
			created_at      TEXT NOT NULL DEFAULT '',
			UNIQUE(user_id, week_start),
			UNIQUE(user_id, id)
		);
		CREATE TABLE stride_notes (
			id          INTEGER PRIMARY KEY,
			user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			plan_id     INTEGER REFERENCES stride_plans(id) ON DELETE SET NULL,
			content     TEXT NOT NULL,
			target_date TEXT NOT NULL DEFAULT '',
			consumed_at TEXT,
			consumed_by TEXT,
			scope       TEXT NOT NULL DEFAULT 'any',
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
			race_id             INTEGER REFERENCES stride_races(id) ON DELETE SET NULL,
			UNIQUE(user_id, fit_file_hash)
		);
		CREATE TABLE stride_chat_messages (
			id            INTEGER PRIMARY KEY,
			plan_id       INTEGER NOT NULL REFERENCES stride_plans(id) ON DELETE CASCADE,
			user_id       INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			role          TEXT NOT NULL DEFAULT '',
			content       TEXT NOT NULL DEFAULT '',
			plan_modified INTEGER NOT NULL DEFAULT 0,
			created_at    TEXT NOT NULL DEFAULT '',
			FOREIGN KEY (user_id, plan_id) REFERENCES stride_plans(user_id, id)
		);
		CREATE INDEX idx_stride_chat_messages_plan ON stride_chat_messages(plan_id);
		CREATE TABLE stride_eval_messages (
			id           INTEGER PRIMARY KEY,
			user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			plan_id      INTEGER NOT NULL,
			workout_id   INTEGER,
			eval_date    TEXT NOT NULL DEFAULT '',
			role         TEXT NOT NULL CHECK (role IN ('user', 'coach')),
			content      TEXT NOT NULL DEFAULT '',
			eval_revised INTEGER NOT NULL DEFAULT 0,
			created_at   TEXT NOT NULL DEFAULT ''
		);
		CREATE TABLE stride_evaluations (
			id          INTEGER PRIMARY KEY,
			user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			plan_id     INTEGER NOT NULL REFERENCES stride_plans(id) ON DELETE CASCADE,
			workout_id  INTEGER REFERENCES workouts(id) ON DELETE SET NULL,
			eval_json   TEXT NOT NULL,
			created_at  TEXT NOT NULL DEFAULT ''
		);
		CREATE TABLE user_preferences (
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			key     TEXT NOT NULL,
			value   TEXT NOT NULL DEFAULT '',
			PRIMARY KEY (user_id, key)
		);
		CREATE TABLE workout_context (
			workout_id   INTEGER PRIMARY KEY REFERENCES workouts(id) ON DELETE CASCADE,
			surface      TEXT NOT NULL DEFAULT '',
			run_type     TEXT NOT NULL DEFAULT '',
			hr_source    TEXT NOT NULL DEFAULT '',
			feel_notes   TEXT NOT NULL DEFAULT '',
			speed_plan   TEXT NOT NULL DEFAULT '',
			completed_at TEXT NOT NULL DEFAULT ''
		);
		CREATE TABLE stride_workouts (
			id            INTEGER PRIMARY KEY,
			user_id       INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			name          TEXT NOT NULL,
			workout_type  TEXT NOT NULL DEFAULT '',
			warmup        TEXT NOT NULL DEFAULT '',
			main_set      TEXT NOT NULL DEFAULT '',
			cooldown      TEXT NOT NULL DEFAULT '',
			strides       TEXT NOT NULL DEFAULT '',
			target_hr_cap TEXT NOT NULL DEFAULT '',
			description   TEXT NOT NULL DEFAULT '',
			source        TEXT NOT NULL DEFAULT 'manual',
			rating        INTEGER NOT NULL DEFAULT 0,
			times_used    INTEGER NOT NULL DEFAULT 0,
			last_used_at  TEXT,
			is_reference  INTEGER NOT NULL DEFAULT 0,
			archived      INTEGER NOT NULL DEFAULT 0,
			created_at    TEXT NOT NULL
		);
		CREATE TABLE stride_workout_blocks (
			workout_id INTEGER NOT NULL REFERENCES stride_workouts(id) ON DELETE CASCADE,
			block      TEXT NOT NULL,
			UNIQUE(workout_id, block)
		);
		CREATE TABLE stride_macro_plans (
			id                 INTEGER PRIMARY KEY,
			user_id            INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			start_week         TEXT NOT NULL,
			end_week           TEXT NOT NULL,
			status             TEXT NOT NULL DEFAULT 'active',
			stale_reason       TEXT NOT NULL DEFAULT '',
			goal_json          TEXT NOT NULL DEFAULT '',
			periodisation_json TEXT NOT NULL DEFAULT '',
			prompt             TEXT NOT NULL DEFAULT '',
			response           TEXT NOT NULL DEFAULT '',
			model              TEXT NOT NULL DEFAULT '',
			generated_by       TEXT NOT NULL DEFAULT 'scheduled',
			previous_plan_id   INTEGER REFERENCES stride_macro_plans(id) ON DELETE SET NULL,
			created_at         TEXT NOT NULL DEFAULT ''
		);
		CREATE UNIQUE INDEX idx_stride_macro_plans_active_start
			ON stride_macro_plans(user_id, start_week) WHERE status = 'active';
		CREATE TABLE stride_macro_weeks (
			id                INTEGER PRIMARY KEY,
			macro_plan_id     INTEGER NOT NULL REFERENCES stride_macro_plans(id) ON DELETE CASCADE,
			user_id           INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			week_start        TEXT NOT NULL,
			seq               INTEGER NOT NULL DEFAULT 0,
			phase             TEXT NOT NULL DEFAULT '',
			mesocycle         TEXT NOT NULL DEFAULT '',
			load_level        TEXT NOT NULL DEFAULT 'normal',
			target_km         REAL NOT NULL DEFAULT 0,
			target_sessions   INTEGER NOT NULL DEFAULT 0,
			race_id           INTEGER REFERENCES stride_races(id) ON DELETE SET NULL,
			key_sessions_json TEXT NOT NULL DEFAULT '',
			intent            TEXT NOT NULL DEFAULT '',
			status            TEXT NOT NULL DEFAULT 'planned',
			UNIQUE(macro_plan_id, week_start)
		);
		CREATE TABLE lactate_tests (
			id                  INTEGER PRIMARY KEY,
			user_id             INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			workout_id          INTEGER REFERENCES workouts(id) ON DELETE SET NULL,
			date                TEXT NOT NULL DEFAULT '',
			comment             TEXT NOT NULL DEFAULT '',
			protocol_type       TEXT NOT NULL DEFAULT 'standard',
			warmup_duration_min INTEGER NOT NULL DEFAULT 10,
			stage_duration_min  INTEGER NOT NULL DEFAULT 5,
			start_speed_kmh     REAL NOT NULL DEFAULT 11.5,
			speed_increment_kmh REAL NOT NULL DEFAULT 0.5,
			created_at          TEXT NOT NULL DEFAULT '',
			updated_at          TEXT NOT NULL DEFAULT ''
		);
		CREATE TABLE lactate_test_stages (
			id             INTEGER PRIMARY KEY,
			test_id        INTEGER NOT NULL REFERENCES lactate_tests(id) ON DELETE CASCADE,
			stage_number   INTEGER NOT NULL,
			speed_kmh      REAL NOT NULL,
			lactate_mmol   REAL NOT NULL,
			heart_rate_bpm INTEGER NOT NULL DEFAULT 0,
			rpe            INTEGER,
			notes          TEXT NOT NULL DEFAULT '',
			UNIQUE(test_id, stage_number)
		);
		CREATE TABLE vo2max_estimates (
			id           INTEGER PRIMARY KEY,
			user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			workout_id   INTEGER NOT NULL REFERENCES workouts(id) ON DELETE CASCADE,
			vo2max       REAL NOT NULL,
			method       TEXT NOT NULL DEFAULT '',
			estimated_at TEXT NOT NULL DEFAULT '',
			UNIQUE(user_id, workout_id)
		);
		CREATE TABLE race_predictions (
			id               INTEGER PRIMARY KEY,
			user_id          INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			created_at       TEXT NOT NULL,
			method           TEXT NOT NULL DEFAULT '',
			predictions_json TEXT NOT NULL,
			rationale        TEXT NOT NULL DEFAULT '',
			inputs_json      TEXT NOT NULL DEFAULT ''
		);
		CREATE TABLE stride_goal_revisions (
			id            INTEGER PRIMARY KEY,
			macro_plan_id INTEGER NOT NULL REFERENCES stride_macro_plans(id) ON DELETE CASCADE,
			user_id       INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			week_start    TEXT NOT NULL,
			goal_json     TEXT NOT NULL DEFAULT '',
			reason        TEXT NOT NULL DEFAULT '',
			source        TEXT NOT NULL DEFAULT 'weekly',
			created_at    TEXT NOT NULL DEFAULT ''
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

	note, err := CreateNote(db, 1, nil, "Feeling tired this week", "", "")
	if err != nil {
		t.Fatalf("create note: %v", err)
	}
	if note.Content != "Feeling tired this week" {
		t.Errorf("content = %q, want %q", note.Content, "Feeling tired this week")
	}
	if note.PlanID != nil {
		t.Errorf("plan_id = %v, want nil", note.PlanID)
	}
	if note.Scope != NoteScopeAny {
		t.Errorf("scope = %q, want %q (default)", note.Scope, NoteScopeAny)
	}
	// When no target_date is provided, it should default to the note's creation date.
	createdAt, err := time.Parse(time.RFC3339, note.CreatedAt)
	if err != nil {
		t.Fatalf("parse created_at %q: %v", note.CreatedAt, err)
	}
	expectedDate := createdAt.UTC().Format("2006-01-02")
	if note.TargetDate != expectedDate {
		t.Errorf("target_date = %q, want %q (should default to creation date)", note.TargetDate, expectedDate)
	}

	// Create a note with an explicit target_date.
	explicitDate := "2026-04-15"
	note2, err := CreateNote(db, 1, nil, "Planned rest day", explicitDate, "")
	if err != nil {
		t.Fatalf("create note with target_date: %v", err)
	}
	if note2.TargetDate != explicitDate {
		t.Errorf("target_date = %q, want %q", note2.TargetDate, explicitDate)
	}

	notes, err := ListNotes(db, 1, nil, "", "")
	if err != nil {
		t.Fatalf("list notes: %v", err)
	}
	if len(notes) != 2 {
		t.Fatalf("got %d notes, want 2", len(notes))
	}
	if notes[0].Content != "Feeling tired this week" {
		t.Errorf("content = %q, want %q", notes[0].Content, "Feeling tired this week")
	}
}

func TestDeleteNote(t *testing.T) {
	db := setupTestDB(t)

	note, err := CreateNote(db, 1, nil, "Delete me", "", "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := DeleteNote(db, note.ID, 1); err != nil {
		t.Fatalf("delete: %v", err)
	}

	notes, err := ListNotes(db, 1, nil, "", "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(notes) != 0 {
		t.Errorf("expected 0 notes after delete, got %d", len(notes))
	}
}

func TestGetNotesByTargetDate(t *testing.T) {
	db := setupTestDB(t)

	// Create notes for different dates.
	_, err := CreateNote(db, 1, nil, "Note for April 10", "2026-04-10", "")
	if err != nil {
		t.Fatalf("create note 1: %v", err)
	}
	_, err = CreateNote(db, 1, nil, "Another note for April 10", "2026-04-10", "")
	if err != nil {
		t.Fatalf("create note 2: %v", err)
	}
	_, err = CreateNote(db, 1, nil, "Note for April 11", "2026-04-11", "")
	if err != nil {
		t.Fatalf("create note 3: %v", err)
	}

	// Query for April 10 — should return 2 notes.
	notes, err := GetNotesByTargetDate(db, 1, "2026-04-10")
	if err != nil {
		t.Fatalf("GetNotesByTargetDate: %v", err)
	}
	if len(notes) != 2 {
		t.Fatalf("expected 2 notes for 2026-04-10, got %d", len(notes))
	}

	// Query for April 11 — should return 1 note.
	notes, err = GetNotesByTargetDate(db, 1, "2026-04-11")
	if err != nil {
		t.Fatalf("GetNotesByTargetDate: %v", err)
	}
	if len(notes) != 1 {
		t.Fatalf("expected 1 note for 2026-04-11, got %d", len(notes))
	}

	// Query for April 12 — should return 0 notes.
	notes, err = GetNotesByTargetDate(db, 1, "2026-04-12")
	if err != nil {
		t.Fatalf("GetNotesByTargetDate: %v", err)
	}
	if len(notes) != 0 {
		t.Errorf("expected 0 notes for 2026-04-12, got %d", len(notes))
	}
}

func TestDeleteNoteWrongUser(t *testing.T) {
	db := setupTestDB(t)

	note, err := CreateNote(db, 1, nil, "Keep me", "", "")
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
			name:          "Monday before 02:00 returns same day",
			now:           time.Date(2026, 4, 6, 1, 0, 0, 0, oslo), // Monday 01:00
			want:          time.Date(2026, 4, 6, 2, 0, 0, 0, oslo), // Same Monday 02:00
			maxFutureDiff: 7 * 24 * time.Hour,
		},
		{
			name:          "Monday exactly 02:00 returns next Monday",
			now:           time.Date(2026, 4, 6, 2, 0, 0, 0, oslo),  // Monday 02:00
			want:          time.Date(2026, 4, 13, 2, 0, 0, 0, oslo), // Next Monday 02:00
			maxFutureDiff: 7 * 24 * time.Hour,
		},
		{
			name:          "Monday after 02:00 returns next Monday",
			now:           time.Date(2026, 4, 6, 10, 0, 0, 0, oslo), // Monday 10:00
			want:          time.Date(2026, 4, 13, 2, 0, 0, 0, oslo), // Next Monday 02:00
			maxFutureDiff: 7 * 24 * time.Hour,
		},
		{
			name:          "Sunday returns next Monday",
			now:           time.Date(2026, 4, 5, 12, 0, 0, 0, oslo), // Sunday noon
			want:          time.Date(2026, 4, 6, 2, 0, 0, 0, oslo),  // Next day Monday 02:00
			maxFutureDiff: 7 * 24 * time.Hour,
		},
		{
			name:          "Saturday returns next Monday",
			now:           time.Date(2026, 4, 4, 23, 59, 0, 0, oslo), // Saturday
			want:          time.Date(2026, 4, 6, 2, 0, 0, 0, oslo),   // Monday 02:00
			maxFutureDiff: 7 * 24 * time.Hour,
		},
		{
			name:          "Wednesday returns next Monday",
			now:           time.Date(2026, 4, 8, 15, 0, 0, 0, oslo), // Wednesday
			want:          time.Date(2026, 4, 13, 2, 0, 0, 0, oslo), // Next Monday 02:00
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

// --- LinkWorkoutToRace tests ---

func TestLinkWorkoutToRace(t *testing.T) {
	db := setupTestDB(t)

	race, err := CreateRace(db, 1, "Test 10K", "2026-05-01", 10000, nil, "B", "")
	if err != nil {
		t.Fatalf("create race: %v", err)
	}

	// Insert a workout with a known duration.
	if _, err := db.Exec(`
		INSERT INTO workouts (id, user_id, sport, started_at, duration_seconds, distance_meters, fit_file_hash, created_at)
		VALUES (300, 1, 'running', '2026-05-01T08:00:00Z', 2700, 10050, 'hash-link', '2026-05-01T08:00:00Z')
	`); err != nil {
		t.Fatalf("insert workout: %v", err)
	}

	if err := LinkWorkoutToRace(db, 300, race.ID, 1); err != nil {
		t.Fatalf("LinkWorkoutToRace: %v", err)
	}

	// Verify workout.race_id is set.
	var raceID sql.NullInt64
	if err := db.QueryRow(`SELECT race_id FROM workouts WHERE id = 300`).Scan(&raceID); err != nil {
		t.Fatalf("query race_id: %v", err)
	}
	if !raceID.Valid || raceID.Int64 != race.ID {
		t.Errorf("workout race_id = %v, want %d", raceID, race.ID)
	}

	// Verify race.result_time is populated from workout duration.
	updated, err := GetRaceByID(db, race.ID, 1)
	if err != nil {
		t.Fatalf("get race: %v", err)
	}
	if updated.ResultTime == nil || *updated.ResultTime != 2700 {
		t.Errorf("race result_time = %v, want 2700", updated.ResultTime)
	}
}

func TestLinkWorkoutToRace_ZeroDuration(t *testing.T) {
	db := setupTestDB(t)

	race, err := CreateRace(db, 1, "Test 10K", "2026-05-01", 10000, nil, "B", "")
	if err != nil {
		t.Fatalf("create race: %v", err)
	}

	// Insert a workout with duration_seconds = 0 (e.g. a freshly imported FIT file).
	if _, err := db.Exec(`
		INSERT INTO workouts (id, user_id, sport, started_at, duration_seconds, distance_meters, fit_file_hash, created_at)
		VALUES (350, 1, 'running', '2026-05-01T08:00:00Z', 0, 10050, 'hash-zero-dur', '2026-05-01T08:00:00Z')
	`); err != nil {
		t.Fatalf("insert workout: %v", err)
	}

	if err := LinkWorkoutToRace(db, 350, race.ID, 1); err != nil {
		t.Fatalf("LinkWorkoutToRace: %v", err)
	}

	// Verify workout is linked.
	var raceID sql.NullInt64
	if err := db.QueryRow(`SELECT race_id FROM workouts WHERE id = 350`).Scan(&raceID); err != nil {
		t.Fatalf("query race_id: %v", err)
	}
	if !raceID.Valid || raceID.Int64 != race.ID {
		t.Errorf("workout race_id = %v, want %d", raceID, race.ID)
	}

	// result_time should remain NULL — zero duration must not overwrite it.
	updated, err := GetRaceByID(db, race.ID, 1)
	if err != nil {
		t.Fatalf("get race: %v", err)
	}
	if updated.ResultTime != nil {
		t.Errorf("race result_time = %v, want nil (zero duration should not populate result_time)", *updated.ResultTime)
	}
}

func TestLinkWorkoutToRace_WrongUser(t *testing.T) {
	db := setupTestDB(t)

	race, err := CreateRace(db, 1, "My Race", "2026-05-01", 10000, nil, "B", "")
	if err != nil {
		t.Fatalf("create race: %v", err)
	}

	if _, err := db.Exec(`
		INSERT INTO workouts (id, user_id, sport, started_at, duration_seconds, fit_file_hash, created_at)
		VALUES (301, 1, 'running', '2026-05-01T08:00:00Z', 2700, 'hash-wrong-user', '2026-05-01T08:00:00Z')
	`); err != nil {
		t.Fatalf("insert workout: %v", err)
	}

	// User 999 should not be able to link.
	err = LinkWorkoutToRace(db, 301, race.ID, 999)
	if err == nil {
		t.Error("expected error for wrong user, got nil")
	}
}

// --- FindMatchingRace tests ---

func TestFindMatchingRace_ExactMatch(t *testing.T) {
	db := setupTestDB(t)

	race, err := CreateRace(db, 1, "Spring 10K", "2026-05-01", 10000, nil, "A", "")
	if err != nil {
		t.Fatalf("create race: %v", err)
	}

	// Exact date and distance match.
	found, err := FindMatchingRace(db, 1, "2026-05-01", 10000)
	if err != nil {
		t.Fatalf("FindMatchingRace: %v", err)
	}
	if found == nil {
		t.Fatal("expected match, got nil")
	}
	if found.ID != race.ID {
		t.Errorf("found race ID = %d, want %d", found.ID, race.ID)
	}
}

func TestFindMatchingRace_FuzzyDate(t *testing.T) {
	db := setupTestDB(t)

	_, err := CreateRace(db, 1, "Fuzzy Race", "2026-05-02", 10000, nil, "B", "")
	if err != nil {
		t.Fatalf("create race: %v", err)
	}

	// Date ±1 day: workout on May 1, race on May 2.
	found, err := FindMatchingRace(db, 1, "2026-05-01", 10000)
	if err != nil {
		t.Fatalf("FindMatchingRace: %v", err)
	}
	if found == nil {
		t.Fatal("expected fuzzy date match, got nil")
	}
	if found.Name != "Fuzzy Race" {
		t.Errorf("name = %q, want %q", found.Name, "Fuzzy Race")
	}
}

func TestFindMatchingRace_FuzzyDistance(t *testing.T) {
	db := setupTestDB(t)

	_, err := CreateRace(db, 1, "HM Race", "2026-05-01", 21097, nil, "A", "")
	if err != nil {
		t.Fatalf("create race: %v", err)
	}

	// Distance within 20%: 21097 * 0.85 = ~17932, which is within [21097*0.8, 21097*1.2].
	found, err := FindMatchingRace(db, 1, "2026-05-01", 21097*0.85)
	if err != nil {
		t.Fatalf("FindMatchingRace: %v", err)
	}
	if found == nil {
		t.Fatal("expected fuzzy distance match, got nil")
	}
}

func TestFindMatchingRace_NoMatch_DateTooFar(t *testing.T) {
	db := setupTestDB(t)

	if _, err := CreateRace(db, 1, "Far Race", "2026-05-05", 10000, nil, "C", ""); err != nil {
		t.Fatalf("create race: %v", err)
	}

	// Date 3 days away — outside ±1 day window.
	found, err := FindMatchingRace(db, 1, "2026-05-02", 10000)
	if err != nil {
		t.Fatalf("FindMatchingRace: %v", err)
	}
	if found != nil {
		t.Errorf("expected no match for date too far, got %+v", found)
	}
}

func TestFindMatchingRace_NoMatch_DistanceTooFar(t *testing.T) {
	db := setupTestDB(t)

	if _, err := CreateRace(db, 1, "Marathon", "2026-05-01", 42195, nil, "A", ""); err != nil {
		t.Fatalf("create race: %v", err)
	}

	// Distance way off: 10000 vs 42195 — outside 20% window.
	found, err := FindMatchingRace(db, 1, "2026-05-01", 10000)
	if err != nil {
		t.Fatalf("FindMatchingRace: %v", err)
	}
	if found != nil {
		t.Errorf("expected no match for distance too far, got %+v", found)
	}
}

func TestFindMatchingRace_WrongUser(t *testing.T) {
	db := setupTestDB(t)

	if _, err := CreateRace(db, 1, "Private Race", "2026-05-01", 10000, nil, "B", ""); err != nil {
		t.Fatalf("create race: %v", err)
	}

	found, err := FindMatchingRace(db, 999, "2026-05-01", 10000)
	if err != nil {
		t.Fatalf("FindMatchingRace: %v", err)
	}
	if found != nil {
		t.Errorf("expected no match for wrong user, got %+v", found)
	}
}

// --- FindMatchingRaces (plural) tests ---

func TestFindMatchingRaces_MultipleMatches(t *testing.T) {
	db := setupTestDB(t)

	// Create two races on the same day with similar distances.
	if _, err := CreateRace(db, 1, "10K Race A", "2026-05-01", 10000, nil, "A", ""); err != nil {
		t.Fatalf("create race A: %v", err)
	}
	if _, err := CreateRace(db, 1, "10K Race B", "2026-05-01", 10200, nil, "B", ""); err != nil {
		t.Fatalf("create race B: %v", err)
	}

	races, err := FindMatchingRaces(db, 1, "2026-05-01", 10100)
	if err != nil {
		t.Fatalf("FindMatchingRaces: %v", err)
	}
	if len(races) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(races))
	}
}

func TestFindMatchingRaces_NoMatch(t *testing.T) {
	db := setupTestDB(t)

	races, err := FindMatchingRaces(db, 1, "2026-05-01", 10000)
	if err != nil {
		t.Fatalf("FindMatchingRaces: %v", err)
	}
	if len(races) != 0 {
		t.Errorf("expected 0 matches, got %d", len(races))
	}
}

// insertTestWorkout inserts a workout for the given user and returns its auto-generated ID.
func insertTestWorkout(t *testing.T, db *sql.DB, userID int64, startedAt string, durationSec int, distanceM float64, fitHash string) int64 {
	t.Helper()
	res, err := db.Exec(`
		INSERT INTO workouts (user_id, sport, started_at, duration_seconds, distance_meters, fit_file_hash, created_at)
		VALUES (?, 'running', ?, ?, ?, ?, ?)
	`, userID, startedAt, durationSec, distanceM, fitHash, startedAt)
	if err != nil {
		t.Fatalf("insert workout: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

// --- TryMatchRaceForWorkout tests ---

func TestTryMatchRaceForWorkout_SingleMatch(t *testing.T) {
	db := setupTestDB(t)

	race, err := CreateRace(db, 1, "Spring 10K", "2026-05-01", 10000, nil, "A", "")
	if err != nil {
		t.Fatalf("create race: %v", err)
	}

	workoutID := insertTestWorkout(t, db, 1, "2026-05-01T08:00:00Z", 2700, 10050, "hash-try-match")

	result, err := TryMatchRaceForWorkout(db, workoutID, 1, "2026-05-01T08:00:00Z", 10050)
	if err != nil {
		t.Fatalf("TryMatchRaceForWorkout: %v", err)
	}
	if result.Status != "linked" {
		t.Fatalf("status = %q, want %q", result.Status, "linked")
	}
	if result.RaceID != race.ID {
		t.Errorf("race_id = %d, want %d", result.RaceID, race.ID)
	}
	if result.RaceName != "Spring 10K" {
		t.Errorf("race_name = %q, want %q", result.RaceName, "Spring 10K")
	}
	if result.Candidates != 1 {
		t.Errorf("candidates = %d, want 1", result.Candidates)
	}

	// Verify the workout was actually linked.
	var raceID sql.NullInt64
	if err := db.QueryRow(`SELECT race_id FROM workouts WHERE id = ?`, workoutID).Scan(&raceID); err != nil {
		t.Fatalf("query race_id: %v", err)
	}
	if !raceID.Valid || raceID.Int64 != race.ID {
		t.Errorf("workout race_id = %v, want %d", raceID, race.ID)
	}

	// Verify race result_time was populated.
	updated, err := GetRaceByID(db, race.ID, 1)
	if err != nil {
		t.Fatalf("get race: %v", err)
	}
	if updated.ResultTime == nil || *updated.ResultTime != 2700 {
		t.Errorf("race result_time = %v, want 2700", updated.ResultTime)
	}
}

func TestTryMatchRaceForWorkout_Ambiguous(t *testing.T) {
	db := setupTestDB(t)

	if _, err := CreateRace(db, 1, "Race A", "2026-05-01", 10000, nil, "A", ""); err != nil {
		t.Fatalf("create race A: %v", err)
	}
	if _, err := CreateRace(db, 1, "Race B", "2026-05-01", 10200, nil, "B", ""); err != nil {
		t.Fatalf("create race B: %v", err)
	}

	workoutID := insertTestWorkout(t, db, 1, "2026-05-01T08:00:00Z", 2700, 10100, "hash-ambiguous")

	result, err := TryMatchRaceForWorkout(db, workoutID, 1, "2026-05-01T08:00:00Z", 10100)
	if err != nil {
		t.Fatalf("TryMatchRaceForWorkout: %v", err)
	}
	if result.Status != "ambiguous" {
		t.Fatalf("status = %q, want %q", result.Status, "ambiguous")
	}
	if result.Candidates != 2 {
		t.Errorf("candidates = %d, want 2", result.Candidates)
	}

	// Verify the workout was NOT linked (ambiguous should not auto-link).
	var raceID sql.NullInt64
	if err := db.QueryRow(`SELECT race_id FROM workouts WHERE id = ?`, workoutID).Scan(&raceID); err != nil {
		t.Fatalf("query race_id: %v", err)
	}
	if raceID.Valid {
		t.Errorf("workout should not be linked when ambiguous, got race_id = %d", raceID.Int64)
	}
}

func TestTryMatchRaceForWorkout_NoMatch(t *testing.T) {
	db := setupTestDB(t)

	workoutID := insertTestWorkout(t, db, 1, "2026-05-01T08:00:00Z", 2700, 10000, "hash-no-match")

	result, err := TryMatchRaceForWorkout(db, workoutID, 1, "2026-05-01T08:00:00Z", 10000)
	if err != nil {
		t.Fatalf("TryMatchRaceForWorkout: %v", err)
	}
	if result.Status != "no_match" {
		t.Fatalf("status = %q, want %q", result.Status, "no_match")
	}
	if result.Candidates != 0 {
		t.Errorf("candidates = %d, want 0", result.Candidates)
	}
}

func TestTryMatchRaceForWorkout_CrossUserIsolation(t *testing.T) {
	db := setupTestDB(t)

	// Insert a second user.
	if _, err := db.Exec("INSERT INTO users (id, email, name, google_id) VALUES (2, 'other@example.com', 'Other', 'g456')"); err != nil {
		t.Fatalf("insert user 2: %v", err)
	}

	// User 2 has a matching race; user 1 does not.
	if _, err := CreateRace(db, 2, "Other User 10K", "2026-05-01", 10000, nil, "A", ""); err != nil {
		t.Fatalf("create race for user 2: %v", err)
	}

	workoutID := insertTestWorkout(t, db, 1, "2026-05-01T08:00:00Z", 2700, 10050, "hash-cross-user")

	// User 1 should see no match — the race belongs to user 2.
	result, err := TryMatchRaceForWorkout(db, workoutID, 1, "2026-05-01T08:00:00Z", 10050)
	if err != nil {
		t.Fatalf("TryMatchRaceForWorkout: %v", err)
	}
	if result.Status != "no_match" {
		t.Errorf("expected no_match for cross-user, got %q", result.Status)
	}
}

func TestMarkNotesConsumed(t *testing.T) {
	db := setupTestDB(t)

	today := time.Now().Format("2006-01-02")
	tomorrow := time.Now().AddDate(0, 0, 1).Format("2006-01-02")

	// Create a few notes.
	n1, err := CreateNote(db, 1, nil, "Note one", today, "")
	if err != nil {
		t.Fatalf("create note 1: %v", err)
	}
	n2, err := CreateNote(db, 1, nil, "Note two", today, "")
	if err != nil {
		t.Fatalf("create note 2: %v", err)
	}
	n3, err := CreateNote(db, 1, nil, "Note three", tomorrow, "")
	if err != nil {
		t.Fatalf("create note 3: %v", err)
	}

	// Mark first two as consumed.
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if err := MarkNotesConsumed(context.Background(), tx, 1, []int64{n1.ID, n2.ID}, "weekly-plan"); err != nil {
		t.Fatalf("MarkNotesConsumed: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	// Verify consumed notes have consumed_at and consumed_by set.
	allNotes, err := ListNotes(db, 1, nil, "all", "")
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(allNotes) != 3 {
		t.Fatalf("expected 3 notes, got %d", len(allNotes))
	}

	// Active filter should return only the unconsumed note.
	active, err := ListNotes(db, 1, nil, "active", "")
	if err != nil {
		t.Fatalf("list active: %v", err)
	}
	if len(active) != 1 {
		t.Fatalf("expected 1 active note, got %d", len(active))
	}
	if active[0].ID != n3.ID {
		t.Errorf("active note ID = %d, want %d", active[0].ID, n3.ID)
	}
	if active[0].ConsumedAt != nil {
		t.Errorf("active note should have nil ConsumedAt")
	}

	// Consumed filter should return the two consumed notes.
	consumed, err := ListNotes(db, 1, nil, "consumed", "")
	if err != nil {
		t.Fatalf("list consumed: %v", err)
	}
	if len(consumed) != 2 {
		t.Fatalf("expected 2 consumed notes, got %d", len(consumed))
	}
	for _, cn := range consumed {
		if cn.ConsumedAt == nil {
			t.Errorf("consumed note %d should have ConsumedAt set", cn.ID)
		}
		if cn.ConsumedBy == nil || *cn.ConsumedBy != "weekly-plan" {
			t.Errorf("consumed note %d ConsumedBy = %v, want 'weekly-plan'", cn.ID, cn.ConsumedBy)
		}
	}
}

func TestMarkNotesConsumedEmpty(t *testing.T) {
	db := setupTestDB(t)

	// Marking an empty slice should be a no-op.
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if err := MarkNotesConsumed(context.Background(), tx, 1, nil, "test"); err != nil {
		t.Fatalf("MarkNotesConsumed with empty slice: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
}

func TestValidateNoteScope(t *testing.T) {
	for _, ok := range []string{"", "any", "nightly", "weekly"} {
		if err := ValidateNoteScope(ok); err != nil {
			t.Errorf("ValidateNoteScope(%q) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []string{"daily", "monthly", "ANY", " any ", "Nightly", "all"} {
		if err := ValidateNoteScope(bad); err == nil {
			t.Errorf("ValidateNoteScope(%q) returned nil, want error", bad)
		}
	}
}

func TestCreateNote_ScopePersists(t *testing.T) {
	db := setupTestDB(t)

	note, err := CreateNote(db, 1, nil, "scoped to nightly", "2026-04-10", NoteScopeNightly)
	if err != nil {
		t.Fatalf("create note: %v", err)
	}
	if note.Scope != NoteScopeNightly {
		t.Errorf("scope = %q, want %q", note.Scope, NoteScopeNightly)
	}

	// Round-trip through ListNotes to confirm the value persists.
	notes, err := ListNotes(db, 1, nil, "active", "")
	if err != nil {
		t.Fatalf("list notes: %v", err)
	}
	if len(notes) != 1 || notes[0].Scope != NoteScopeNightly {
		t.Errorf("listed scope = %v, want %q", notes, NoteScopeNightly)
	}
}

func TestCreateNote_RejectsInvalidScope(t *testing.T) {
	db := setupTestDB(t)

	if _, err := CreateNote(db, 1, nil, "bad scope", "", "monthly"); err == nil {
		t.Error("expected error for invalid scope, got nil")
	}
}

func TestListNotes_ScopeFilter(t *testing.T) {
	db := setupTestDB(t)

	if _, err := CreateNote(db, 1, nil, "any", "2026-04-10", NoteScopeAny); err != nil {
		t.Fatalf("create any: %v", err)
	}
	if _, err := CreateNote(db, 1, nil, "nightly", "2026-04-10", NoteScopeNightly); err != nil {
		t.Fatalf("create nightly: %v", err)
	}
	if _, err := CreateNote(db, 1, nil, "weekly", "2026-04-10", NoteScopeWeekly); err != nil {
		t.Fatalf("create weekly: %v", err)
	}

	tests := []struct {
		scope string
		want  []string
	}{
		{"", []string{"any", "nightly", "weekly"}},
		{NoteScopeAny, []string{"any", "nightly", "weekly"}},
		{NoteScopeNightly, []string{"any", "nightly"}},
		{NoteScopeWeekly, []string{"any", "weekly"}},
	}
	for _, tc := range tests {
		notes, err := ListNotes(db, 1, nil, "active", tc.scope)
		if err != nil {
			t.Fatalf("ListNotes(scope=%q): %v", tc.scope, err)
		}
		got := make([]string, 0, len(notes))
		for _, n := range notes {
			got = append(got, n.Content)
		}
		// Compare as sets — order is created_at DESC.
		if !sameStringSet(got, tc.want) {
			t.Errorf("ListNotes(scope=%q) = %v, want %v", tc.scope, got, tc.want)
		}
	}
}

func sameStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	m := map[string]int{}
	for _, s := range a {
		m[s]++
	}
	for _, s := range b {
		m[s]--
		if m[s] < 0 {
			return false
		}
	}
	return true
}

func TestUpdateNote_ContentScopeAndDate(t *testing.T) {
	db := setupTestDB(t)

	note, err := CreateNote(db, 1, nil, "original", "2026-04-10", NoteScopeAny)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	newContent := "updated content"
	newDate := "2026-04-12"
	newScope := NoteScopeWeekly
	updated, err := UpdateNote(db, note.ID, 1, &newContent, &newDate, &newScope)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Content != newContent {
		t.Errorf("content = %q, want %q", updated.Content, newContent)
	}
	if updated.TargetDate != newDate {
		t.Errorf("target_date = %q, want %q", updated.TargetDate, newDate)
	}
	if updated.Scope != newScope {
		t.Errorf("scope = %q, want %q", updated.Scope, newScope)
	}

	// Verify content is encrypted at rest (not stored as plaintext).
	var raw string
	if err := db.QueryRow(`SELECT content FROM stride_notes WHERE id = ?`, note.ID).Scan(&raw); err != nil {
		t.Fatalf("query raw: %v", err)
	}
	if raw == newContent {
		t.Error("content should be encrypted at rest, not stored as plaintext")
	}
}

func TestUpdateNote_RejectsConsumedNote(t *testing.T) {
	db := setupTestDB(t)

	note, err := CreateNote(db, 1, nil, "consume me", "2026-04-10", NoteScopeAny)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if err := MarkNotesConsumed(context.Background(), tx, 1, []int64{note.ID}, "weekly"); err != nil {
		t.Fatalf("mark consumed: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	newContent := "should not apply"
	_, err = UpdateNote(db, note.ID, 1, &newContent, nil, nil)
	if !errors.Is(err, ErrNoteConsumed) {
		t.Errorf("expected ErrNoteConsumed, got %v", err)
	}
}

func TestUpdateNote_NotFound(t *testing.T) {
	db := setupTestDB(t)

	newContent := "x"
	_, err := UpdateNote(db, 9999, 1, &newContent, nil, nil)
	if !errors.Is(err, ErrNoteNotFound) {
		t.Errorf("expected ErrNoteNotFound, got %v", err)
	}
}

func TestUpdateNote_RejectsWrongUser(t *testing.T) {
	db := setupTestDB(t)

	note, err := CreateNote(db, 1, nil, "owned by 1", "2026-04-10", NoteScopeAny)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	newContent := "intruder"
	_, err = UpdateNote(db, note.ID, 999, &newContent, nil, nil)
	if !errors.Is(err, ErrNoteNotFound) {
		t.Errorf("expected ErrNoteNotFound for wrong user, got %v", err)
	}
}

func TestUpdateNote_RejectsInvalidScope(t *testing.T) {
	db := setupTestDB(t)

	note, err := CreateNote(db, 1, nil, "scoped", "2026-04-10", NoteScopeAny)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	bad := "monthly"
	if _, err := UpdateNote(db, note.ID, 1, nil, nil, &bad); err == nil {
		t.Error("expected error for invalid scope, got nil")
	}
}
