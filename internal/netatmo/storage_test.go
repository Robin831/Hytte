package netatmo

import (
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
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
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("get last insert id: %v", err)
	}
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

	old := time.Now().UTC().Add(-10 * 24 * time.Hour) // 10 days ago — outside 24h query window
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
	// StoreReadings inserts all 5 indoor metrics for the recent timestamp.
	// The old direct-insert row is outside the 24h window and must not appear.
	if len(results) != 5 {
		t.Fatalf("expected 5 results within 24h window, got %d", len(results))
	}
	oldCutoff := time.Now().UTC().Add(-2 * 24 * time.Hour)
	for _, r := range results {
		if r.Timestamp.Before(oldCutoff) {
			t.Errorf("result with timestamp %v is outside 24h window", r.Timestamp)
		}
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

func TestStoreZeroValueReadings(t *testing.T) {
	db := setupStorageTestDB(t)
	defer db.Close()
	userID := insertStorageTestUser(t, db)

	now := time.Now().UTC().Truncate(time.Second)
	readings := ModuleReadings{
		Indoor: &IndoorReadings{
			Temperature: 0.0, // 0°C is a valid reading
			Humidity:    0,
			CO2:         0,
			Noise:       0,
			Pressure:    0.0,
		},
		Outdoor: &OutdoorReadings{
			Temperature: 0.0, // 0°C outdoors
			Humidity:    0,
		},
		Wind: &WindReadings{
			Speed:     0.0, // calm wind
			Gust:      0.0,
			Direction: 0, // north
		},
		FetchedAt: now,
	}

	if err := StoreReadings(db, userID, readings); err != nil {
		t.Fatalf("StoreReadings with zero values: %v", err)
	}

	results, err := QueryHistory(db, userID, 24)
	if err != nil {
		t.Fatalf("QueryHistory: %v", err)
	}
	// 5 indoor + 2 outdoor + 3 wind = 10 rows — zero values must be persisted
	if len(results) != 10 {
		t.Fatalf("expected 10 readings (zero values must be stored), got %d", len(results))
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
	if len(results) != 0 {
		t.Fatalf("expected empty slice for empty result, got %v", results)
	}
}

func TestQueryHistoryInvalidHours(t *testing.T) {
	db := setupStorageTestDB(t)
	defer db.Close()
	userID := insertStorageTestUser(t, db)

	if _, err := QueryHistory(db, userID, 0); err == nil {
		t.Error("expected error for hours=0, got nil")
	}
	if _, err := QueryHistory(db, userID, -1); err == nil {
		t.Error("expected error for hours=-1, got nil")
	}
}

func TestQueryHistoryCapAtRetention(t *testing.T) {
	db := setupStorageTestDB(t)
	defer db.Close()
	userID := insertStorageTestUser(t, db)

	// A value larger than retentionDays*24 should be silently capped, not error.
	results, err := QueryHistory(db, userID, retentionDays*24+1)
	if err != nil {
		t.Fatalf("QueryHistory with oversized hours: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results on empty table, got %d", len(results))
	}
}
