package stride

import (
	"database/sql"
	"testing"

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
