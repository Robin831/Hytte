package netatmo

import (
	"database/sql"
	"testing"
	"time"
)

// setupStorageTestDB opens an in-memory SQLite database with the tables
// needed to test the netatmo storage layer.
func setupStorageTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	_, err = db.Exec(`
		CREATE TABLE users (
			id      INTEGER PRIMARY KEY,
			email   TEXT NOT NULL,
			name    TEXT NOT NULL DEFAULT ''
		);
		CREATE TABLE netatmo_readings (
			id          INTEGER PRIMARY KEY,
			user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			timestamp   TEXT NOT NULL,
			module_type TEXT NOT NULL,
			metric      TEXT NOT NULL,
			value       REAL NOT NULL
		);
		CREATE INDEX idx_netatmo_readings_user_ts ON netatmo_readings(user_id, timestamp);
	`)
	if err != nil {
		t.Fatalf("create schema: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	return db
}

func insertStorageTestUser(t *testing.T, db *sql.DB) int64 {
	t.Helper()
	res, err := db.Exec(`INSERT INTO users (email, name) VALUES ('store@example.com', 'Store User')`)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

func TestStoreAndQueryReadings(t *testing.T) {
	db := setupStorageTestDB(t)
	defer db.Close()
	userID := insertStorageTestUser(t, db)

	now := time.Now().UTC().Truncate(time.Second)
	readings := ModuleReadings{
		Indoor: &IndoorReadings{
			Temperature: 21.5,
			Humidity:    45,
			CO2:         800,
			Noise:       35,
			Pressure:    1013.2,
		},
		Outdoor: &OutdoorReadings{
			Temperature: 5.3,
			Humidity:    82,
		},
		Wind: &WindReadings{
			Speed:     12.4,
			Gust:      18.0,
			Direction: 270,
		},
		FetchedAt: now,
	}

	if err := StoreReadings(db, userID, readings); err != nil {
		t.Fatalf("StoreReadings: %v", err)
	}

	results, err := QueryHistory(db, userID, 24)
	if err != nil {
		t.Fatalf("QueryHistory: %v", err)
	}

	// 5 indoor + 2 outdoor + 3 wind = 10 rows
	if len(results) != 10 {
		t.Fatalf("expected 10 readings, got %d", len(results))
	}

	// Verify all results have the correct timestamp and belong to the right modules.
	metricCount := map[string]int{}
	for _, r := range results {
		if !r.Timestamp.UTC().Truncate(time.Second).Equal(now) {
			t.Errorf("unexpected timestamp %v, want %v", r.Timestamp, now)
		}
		metricCount[r.ModuleType+"."+r.Metric]++
	}

	expected := map[string]int{
		"indoor.temperature":  1,
		"indoor.humidity":     1,
		"indoor.co2":          1,
		"indoor.noise":        1,
		"indoor.pressure":     1,
		"outdoor.temperature": 1,
		"outdoor.humidity":    1,
		"wind.speed":          1,
		"wind.gust":           1,
		"wind.direction":      1,
	}
	for key, want := range expected {
		if got := metricCount[key]; got != want {
			t.Errorf("metric %q: got %d rows, want %d", key, got, want)
		}
	}
}

func TestQueryHistoryRespectsWindow(t *testing.T) {
	db := setupStorageTestDB(t)
	defer db.Close()
	userID := insertStorageTestUser(t, db)

	old := time.Now().UTC().Add(-10 * 24 * time.Hour) // 10 days ago — outside 7-day window
	recent := time.Now().UTC().Add(-1 * time.Hour)

	for _, ts := range []time.Time{old, recent} {
		r := ModuleReadings{
			Indoor:    &IndoorReadings{Temperature: 20.0},
			FetchedAt: ts,
		}
		// Bypass deleteOldReadings by inserting directly for the old row.
		if ts == old {
			_, err := db.Exec(`INSERT INTO netatmo_readings (user_id, timestamp, module_type, metric, value)
				VALUES (?, ?, 'indoor', 'temperature', 20.0)`, userID, ts.Format(time.RFC3339))
			if err != nil {
				t.Fatalf("direct insert: %v", err)
			}
		} else {
			if err := StoreReadings(db, userID, r); err != nil {
				t.Fatalf("StoreReadings: %v", err)
			}
		}
	}

	results, err := QueryHistory(db, userID, 24)
	if err != nil {
		t.Fatalf("QueryHistory: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result within 24h window, got %d", len(results))
	}
	if results[0].Metric != "temperature" {
		t.Errorf("unexpected metric %q", results[0].Metric)
	}
}

func TestStoreReadingsNilModules(t *testing.T) {
	db := setupStorageTestDB(t)
	defer db.Close()
	userID := insertStorageTestUser(t, db)

	readings := ModuleReadings{
		Indoor:    nil,
		Outdoor:   nil,
		Wind:      nil,
		FetchedAt: time.Now().UTC(),
	}
	if err := StoreReadings(db, userID, readings); err != nil {
		t.Fatalf("StoreReadings with nil modules: %v", err)
	}

	results, err := QueryHistory(db, userID, 24)
	if err != nil {
		t.Fatalf("QueryHistory: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results for nil modules, got %d", len(results))
	}
}

func TestDeleteOldReadings(t *testing.T) {
	db := setupStorageTestDB(t)
	defer db.Close()
	userID := insertStorageTestUser(t, db)

	// Insert a row with a timestamp far in the past.
	oldTS := time.Now().UTC().Add(-8 * 24 * time.Hour).Format(time.RFC3339)
	_, err := db.Exec(`INSERT INTO netatmo_readings (user_id, timestamp, module_type, metric, value)
		VALUES (?, ?, 'indoor', 'temperature', 19.0)`, userID, oldTS)
	if err != nil {
		t.Fatalf("insert old row: %v", err)
	}

	// StoreReadings triggers cleanup.
	if err := StoreReadings(db, userID, ModuleReadings{
		Indoor:    &IndoorReadings{Temperature: 22.0},
		FetchedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("StoreReadings: %v", err)
	}

	// The 8-day-old row should be gone.
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM netatmo_readings WHERE timestamp = ?`, oldTS).Scan(&count); err != nil {
		t.Fatalf("count old rows: %v", err)
	}
	if count != 0 {
		t.Errorf("expected old row to be deleted, got %d rows", count)
	}
}

func TestQueryHistoryEmpty(t *testing.T) {
	db := setupStorageTestDB(t)
	defer db.Close()
	userID := insertStorageTestUser(t, db)

	results, err := QueryHistory(db, userID, 24)
	if err != nil {
		t.Fatalf("QueryHistory on empty table: %v", err)
	}
	if results != nil {
		t.Fatalf("expected nil slice for empty result, got %v", results)
	}
}
