package lactate

import (
	"database/sql"
	"testing"

	"github.com/Robin831/Hytte/internal/encryption"
	_ "modernc.org/sqlite"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	t.Setenv("ENCRYPTION_KEY", "test-key-for-lactate-tests")
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
			is_admin INTEGER NOT NULL DEFAULT 0
		);
		CREATE TABLE lactate_tests (
			id                  INTEGER PRIMARY KEY,
			user_id             INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
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

func sampleTest() *Test {
	rpe := 12
	return &Test{
		Date:              "2026-03-14",
		Comment:           "Morning test",
		ProtocolType:      "standard",
		WarmupDurationMin: 10,
		StageDurationMin:  5,
		StartSpeedKmh:     11.5,
		SpeedIncrementKmh: 0.5,
		Stages: []Stage{
			{StageNumber: 0, SpeedKmh: 10.0, LactateMmol: 1.2, HeartRateBpm: 130},
			{StageNumber: 1, SpeedKmh: 11.5, LactateMmol: 1.5, HeartRateBpm: 145, RPE: &rpe},
			{StageNumber: 2, SpeedKmh: 12.0, LactateMmol: 2.1, HeartRateBpm: 155},
			{StageNumber: 3, SpeedKmh: 12.5, LactateMmol: 3.4, HeartRateBpm: 165},
			{StageNumber: 4, SpeedKmh: 13.0, LactateMmol: 5.2, HeartRateBpm: 175},
		},
	}
}

func TestCreateAndGetByID(t *testing.T) {
	db := setupTestDB(t)

	created, err := Create(db, 1, sampleTest())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.Date != "2026-03-14" {
		t.Errorf("date = %q, want %q", created.Date, "2026-03-14")
	}
	if created.Comment != "Morning test" {
		t.Errorf("comment = %q, want %q", created.Comment, "Morning test")
	}
	if len(created.Stages) != 5 {
		t.Fatalf("stages len = %d, want 5", len(created.Stages))
	}
	if created.Stages[1].RPE == nil || *created.Stages[1].RPE != 12 {
		t.Errorf("stage 1 RPE = %v, want 12", created.Stages[1].RPE)
	}
	if created.Stages[0].RPE != nil {
		t.Errorf("stage 0 RPE = %v, want nil", created.Stages[0].RPE)
	}
	if created.CreatedAt == "" {
		t.Error("created_at should be set")
	}

	got, err := GetByID(db, created.ID, 1)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("id = %d, want %d", got.ID, created.ID)
	}
	if len(got.Stages) != 5 {
		t.Errorf("stages len = %d, want 5", len(got.Stages))
	}
}

func TestGetByID_WrongUser(t *testing.T) {
	db := setupTestDB(t)

	created, err := Create(db, 1, sampleTest())
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	_, err = GetByID(db, created.ID, 999)
	if err != sql.ErrNoRows {
		t.Errorf("expected ErrNoRows for wrong user, got %v", err)
	}
}

func TestList_Empty(t *testing.T) {
	db := setupTestDB(t)

	tests, err := List(db, 1)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(tests) != 0 {
		t.Errorf("expected 0 tests, got %d", len(tests))
	}
}

func TestList_WithTests(t *testing.T) {
	db := setupTestDB(t)

	if _, err := Create(db, 1, sampleTest()); err != nil {
		t.Fatalf("create 1: %v", err)
	}

	st2 := sampleTest()
	st2.Date = "2026-03-15"
	if _, err := Create(db, 1, st2); err != nil {
		t.Fatalf("create 2: %v", err)
	}

	tests, err := List(db, 1)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(tests) != 2 {
		t.Fatalf("expected 2 tests, got %d", len(tests))
	}
	// Should be ordered by date DESC.
	if tests[0].Date != "2026-03-15" {
		t.Errorf("first test date = %q, want %q", tests[0].Date, "2026-03-15")
	}
}

func TestUpdate(t *testing.T) {
	db := setupTestDB(t)

	created, err := Create(db, 1, sampleTest())
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	updated, err := Update(db, created.ID, 1, &Test{
		Date:              "2026-03-15",
		Comment:           "Evening retest",
		ProtocolType:      "custom",
		WarmupDurationMin: 5,
		StageDurationMin:  3,
		StartSpeedKmh:     10.0,
		SpeedIncrementKmh: 1.0,
		Stages: []Stage{
			{StageNumber: 0, SpeedKmh: 9.0, LactateMmol: 1.0, HeartRateBpm: 120},
			{StageNumber: 1, SpeedKmh: 10.0, LactateMmol: 1.8, HeartRateBpm: 140},
		},
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Comment != "Evening retest" {
		t.Errorf("comment = %q, want %q", updated.Comment, "Evening retest")
	}
	if len(updated.Stages) != 2 {
		t.Errorf("stages len = %d, want 2", len(updated.Stages))
	}
	if updated.ProtocolType != "custom" {
		t.Errorf("protocol_type = %q, want %q", updated.ProtocolType, "custom")
	}
}

func TestUpdate_WrongUser(t *testing.T) {
	db := setupTestDB(t)

	created, err := Create(db, 1, sampleTest())
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	_, err = Update(db, created.ID, 999, sampleTest())
	if err != sql.ErrNoRows {
		t.Errorf("expected ErrNoRows for wrong user, got %v", err)
	}
}

func TestDelete(t *testing.T) {
	db := setupTestDB(t)

	created, err := Create(db, 1, sampleTest())
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := Delete(db, created.ID, 1); err != nil {
		t.Fatalf("delete: %v", err)
	}

	_, err = GetByID(db, created.ID, 1)
	if err != sql.ErrNoRows {
		t.Errorf("expected ErrNoRows after delete, got %v", err)
	}
}

func TestDelete_WrongUser(t *testing.T) {
	db := setupTestDB(t)

	created, err := Create(db, 1, sampleTest())
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	err = Delete(db, created.ID, 999)
	if err != sql.ErrNoRows {
		t.Errorf("expected ErrNoRows for wrong user, got %v", err)
	}
}

func TestCascadeDeleteUser(t *testing.T) {
	db := setupTestDB(t)

	if _, err := Create(db, 1, sampleTest()); err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := db.Exec("DELETE FROM users WHERE id = 1"); err != nil {
		t.Fatalf("delete user: %v", err)
	}

	tests, err := List(db, 1)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(tests) != 0 {
		t.Errorf("expected 0 tests after user delete, got %d", len(tests))
	}

	// Stages should also be gone (cascade from test).
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM lactate_test_stages").Scan(&count); err != nil {
		t.Fatalf("count stages: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 stages after user delete, got %d", count)
	}
}

func TestStagesOrderedByNumber(t *testing.T) {
	db := setupTestDB(t)

	// Insert stages out of order.
	test := &Test{
		Date:              "2026-03-14",
		ProtocolType:      "standard",
		WarmupDurationMin: 10,
		StageDurationMin:  5,
		StartSpeedKmh:     11.5,
		SpeedIncrementKmh: 0.5,
		Stages: []Stage{
			{StageNumber: 3, SpeedKmh: 12.5, LactateMmol: 3.4, HeartRateBpm: 165},
			{StageNumber: 1, SpeedKmh: 11.5, LactateMmol: 1.5, HeartRateBpm: 145},
			{StageNumber: 0, SpeedKmh: 10.0, LactateMmol: 1.2, HeartRateBpm: 130},
			{StageNumber: 2, SpeedKmh: 12.0, LactateMmol: 2.1, HeartRateBpm: 155},
		},
	}

	created, err := Create(db, 1, test)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	for i, s := range created.Stages {
		if s.StageNumber != i {
			t.Errorf("stage[%d].StageNumber = %d, want %d", i, s.StageNumber, i)
		}
	}
}
