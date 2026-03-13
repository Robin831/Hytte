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
	);`

	_, err := db.Exec(schema)
	return err
}
