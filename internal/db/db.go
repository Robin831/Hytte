package db

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// Init opens a SQLite database at the given path with WAL mode enabled and
// creates the schema if it does not already exist.
func Init(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// Enable WAL mode for better concurrent read performance.
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable WAL mode: %w", err)
	}

	// Enable foreign key enforcement (required for ON DELETE CASCADE).
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}

	if err := createSchema(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("create schema: %w", err)
	}

	return db, nil
}

func createSchema(db *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS users (
		id         INTEGER PRIMARY KEY,
		email      TEXT UNIQUE NOT NULL,
		name       TEXT NOT NULL,
		picture    TEXT NOT NULL DEFAULT '',
		google_id  TEXT UNIQUE NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS sessions (
		token      TEXT PRIMARY KEY,
		user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		expires_at DATETIME NOT NULL
	);

	CREATE TABLE IF NOT EXISTS user_preferences (
		user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		key     TEXT NOT NULL,
		value   TEXT NOT NULL DEFAULT '',
		PRIMARY KEY (user_id, key)
	);

	CREATE TABLE IF NOT EXISTS short_links (
		id         INTEGER PRIMARY KEY,
		user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		code       TEXT UNIQUE NOT NULL,
		target_url TEXT NOT NULL,
		title      TEXT NOT NULL DEFAULT '',
		clicks     INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS webhook_endpoints (
		id         TEXT PRIMARY KEY,
		user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		name       TEXT NOT NULL DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS webhook_requests (
		id          INTEGER PRIMARY KEY,
		endpoint_id TEXT NOT NULL REFERENCES webhook_endpoints(id) ON DELETE CASCADE,
		method      TEXT NOT NULL,
		headers     TEXT NOT NULL DEFAULT '{}',
		body        TEXT NOT NULL DEFAULT '',
		query       TEXT NOT NULL DEFAULT '',
		remote_addr TEXT NOT NULL DEFAULT '',
		received_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_webhook_requests_endpoint_received
		ON webhook_requests(endpoint_id, received_at);

	CREATE TABLE IF NOT EXISTS notes (
		id         INTEGER PRIMARY KEY,
		user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		title      TEXT NOT NULL DEFAULT '',
		content    TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL DEFAULT '',
		updated_at TEXT NOT NULL DEFAULT ''
	);

	CREATE TABLE IF NOT EXISTS note_tags (
		note_id INTEGER NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
		tag     TEXT NOT NULL,
		PRIMARY KEY (note_id, tag)
	);

	CREATE INDEX IF NOT EXISTS idx_notes_user_id ON notes(user_id);
	CREATE INDEX IF NOT EXISTS idx_note_tags_tag ON note_tags(tag);

	CREATE TABLE IF NOT EXISTS vapid_keys (
		id          INTEGER PRIMARY KEY,
		public_key  TEXT NOT NULL,
		private_key TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS push_subscriptions (
		id         INTEGER PRIMARY KEY,
		user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		endpoint   TEXT NOT NULL,
		p256dh     TEXT NOT NULL,
		auth       TEXT NOT NULL,
		created_at TEXT NOT NULL DEFAULT '',
		UNIQUE(user_id, endpoint)
	);

	CREATE TABLE IF NOT EXISTS lactate_tests (
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

	CREATE INDEX IF NOT EXISTS idx_lactate_tests_user_id ON lactate_tests(user_id);

	CREATE TABLE IF NOT EXISTS lactate_test_stages (
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

	CREATE INDEX IF NOT EXISTS idx_lactate_test_stages_test_id ON lactate_test_stages(test_id);

	CREATE TABLE IF NOT EXISTS workouts (
		id                  INTEGER PRIMARY KEY,
		user_id             INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		sport               TEXT NOT NULL DEFAULT 'other',
		title               TEXT NOT NULL DEFAULT '',
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
		fit_file_hash       TEXT NOT NULL DEFAULT '',
		created_at          TEXT NOT NULL DEFAULT '',
		UNIQUE(user_id, fit_file_hash)
	);

	CREATE INDEX IF NOT EXISTS idx_workouts_user_id ON workouts(user_id);
	CREATE INDEX IF NOT EXISTS idx_workouts_started_at ON workouts(user_id, started_at);

	CREATE TABLE IF NOT EXISTS workout_laps (
		id                  INTEGER PRIMARY KEY,
		workout_id          INTEGER NOT NULL REFERENCES workouts(id) ON DELETE CASCADE,
		lap_number          INTEGER NOT NULL,
		start_offset_ms     INTEGER NOT NULL DEFAULT 0,
		duration_seconds    REAL NOT NULL DEFAULT 0,
		distance_meters     REAL NOT NULL DEFAULT 0,
		avg_heart_rate      INTEGER NOT NULL DEFAULT 0,
		max_heart_rate      INTEGER NOT NULL DEFAULT 0,
		avg_pace_sec_per_km REAL NOT NULL DEFAULT 0,
		avg_cadence         INTEGER NOT NULL DEFAULT 0,
		UNIQUE(workout_id, lap_number)
	);

	CREATE INDEX IF NOT EXISTS idx_workout_laps_workout_id ON workout_laps(workout_id);

	CREATE TABLE IF NOT EXISTS workout_samples (
		workout_id INTEGER PRIMARY KEY REFERENCES workouts(id) ON DELETE CASCADE,
		data       TEXT NOT NULL DEFAULT '[]'
	);

	CREATE TABLE IF NOT EXISTS workout_tags (
		workout_id INTEGER NOT NULL REFERENCES workouts(id) ON DELETE CASCADE,
		tag        TEXT NOT NULL,
		PRIMARY KEY (workout_id, tag)
	);

	CREATE INDEX IF NOT EXISTS idx_workout_tags_tag ON workout_tags(tag);

	CREATE TABLE IF NOT EXISTS infra_module_config (
		user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		module     TEXT NOT NULL,
		enabled    BOOLEAN NOT NULL DEFAULT 1,
		updated_at TEXT NOT NULL DEFAULT '',
		PRIMARY KEY (user_id, module)
	);

	CREATE INDEX IF NOT EXISTS idx_infra_module_config_user_id ON infra_module_config(user_id);

	CREATE TABLE IF NOT EXISTS infra_health_services (
		id         INTEGER PRIMARY KEY,
		name       TEXT NOT NULL,
		url        TEXT NOT NULL,
		created_at TEXT NOT NULL DEFAULT ''
	);

	CREATE TABLE IF NOT EXISTS infra_ssl_hosts (
		id         INTEGER PRIMARY KEY,
		name       TEXT NOT NULL,
		hostname   TEXT NOT NULL,
		port       INTEGER NOT NULL DEFAULT 443,
		created_at TEXT NOT NULL DEFAULT ''
	);

	CREATE TABLE IF NOT EXISTS infra_uptime_history (
		id         INTEGER PRIMARY KEY,
		module     TEXT NOT NULL,
		target     TEXT NOT NULL,
		status     TEXT NOT NULL,
		message    TEXT NOT NULL DEFAULT '',
		checked_at TEXT NOT NULL DEFAULT ''
	);

	CREATE INDEX IF NOT EXISTS idx_infra_uptime_checked
		ON infra_uptime_history(module, target, checked_at);`

	_, err := db.Exec(schema)
	return err
}
