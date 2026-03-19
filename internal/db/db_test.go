package db

import (
	"testing"
)

func TestOrphanCleanup(t *testing.T) {
	database, err := Init(":memory:")
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	defer database.Close()

	// Insert a user and a workout.
	_, err = database.Exec(`INSERT INTO users (id, email, name, google_id) VALUES (1, 'test@example.com', 'Test', 'g1')`)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	_, err = database.Exec(`INSERT INTO workouts (id, user_id, sport, started_at, fit_file_hash, created_at)
		VALUES (100, 1, 'running', '2025-01-01T00:00:00Z', 'hash1', '2025-01-01T00:00:00Z')`)
	if err != nil {
		t.Fatalf("insert workout: %v", err)
	}

	// Insert valid child rows (parent workout_id=100 exists).
	_, err = database.Exec(`INSERT INTO workout_laps (workout_id, lap_number) VALUES (100, 1)`)
	if err != nil {
		t.Fatalf("insert valid lap: %v", err)
	}
	_, err = database.Exec(`INSERT INTO workout_samples (workout_id, data) VALUES (100, '[]')`)
	if err != nil {
		t.Fatalf("insert valid sample: %v", err)
	}
	_, err = database.Exec(`INSERT INTO workout_tags (workout_id, tag) VALUES (100, 'easy')`)
	if err != nil {
		t.Fatalf("insert valid tag: %v", err)
	}

	// Simulate orphaned rows by disabling foreign keys temporarily.
	_, err = database.Exec(`PRAGMA foreign_keys = OFF`)
	if err != nil {
		t.Fatalf("disable FK: %v", err)
	}
	_, err = database.Exec(`INSERT INTO workout_laps (workout_id, lap_number) VALUES (999, 1)`)
	if err != nil {
		t.Fatalf("insert orphan lap: %v", err)
	}
	_, err = database.Exec(`INSERT INTO workout_samples (workout_id, data) VALUES (999, '[]')`)
	if err != nil {
		t.Fatalf("insert orphan sample: %v", err)
	}
	_, err = database.Exec(`INSERT INTO workout_tags (workout_id, tag) VALUES (999, 'orphan')`)
	if err != nil {
		t.Fatalf("insert orphan tag: %v", err)
	}
	_, err = database.Exec(`PRAGMA foreign_keys = ON`)
	if err != nil {
		t.Fatalf("enable FK: %v", err)
	}

	// Run createSchema again — orphan cleanup should remove the bad rows.
	if err := createSchema(database); err != nil {
		t.Fatalf("createSchema: %v", err)
	}

	// Verify orphaned rows are gone.
	for _, q := range []struct {
		table string
		query string
	}{
		{"workout_laps", "SELECT COUNT(*) FROM workout_laps WHERE workout_id = 999"},
		{"workout_samples", "SELECT COUNT(*) FROM workout_samples WHERE workout_id = 999"},
		{"workout_tags", "SELECT COUNT(*) FROM workout_tags WHERE workout_id = 999"},
	} {
		var count int
		if err := database.QueryRow(q.query).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", q.table, err)
		}
		if count != 0 {
			t.Errorf("expected 0 orphaned rows in %s, got %d", q.table, count)
		}
	}

	// Verify valid rows are still present.
	for _, q := range []struct {
		table string
		query string
	}{
		{"workout_laps", "SELECT COUNT(*) FROM workout_laps WHERE workout_id = 100"},
		{"workout_samples", "SELECT COUNT(*) FROM workout_samples WHERE workout_id = 100"},
		{"workout_tags", "SELECT COUNT(*) FROM workout_tags WHERE workout_id = 100"},
	} {
		var count int
		if err := database.QueryRow(q.query).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", q.table, err)
		}
		if count != 1 {
			t.Errorf("expected 1 valid row in %s, got %d", q.table, count)
		}
	}
}

func TestOrphanCleanupIdempotent(t *testing.T) {
	database, err := Init(":memory:")
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	defer database.Close()

	// Running createSchema on a clean DB should succeed without errors.
	if err := createSchema(database); err != nil {
		t.Fatalf("second createSchema should be idempotent: %v", err)
	}
}
