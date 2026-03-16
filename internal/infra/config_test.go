package infra

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	ResetEncryptionKey()
	t.Cleanup(func() { ResetEncryptionKey() })
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	_, err = db.Exec("PRAGMA foreign_keys=ON")
	if err != nil {
		t.Fatalf("enable fk: %v", err)
	}
	_, err = db.Exec(`
		CREATE TABLE users (
			id INTEGER PRIMARY KEY,
			email TEXT UNIQUE NOT NULL,
			name TEXT NOT NULL,
			picture TEXT NOT NULL DEFAULT '',
			google_id TEXT UNIQUE NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE infra_module_config (
			user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			module     TEXT NOT NULL,
			enabled    BOOLEAN NOT NULL DEFAULT 1,
			updated_at TEXT NOT NULL DEFAULT '',
			PRIMARY KEY (user_id, module)
		);
		CREATE TABLE infra_health_services (
			id         INTEGER PRIMARY KEY,
			user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			name       TEXT NOT NULL,
			url        TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT ''
		);
		CREATE TABLE infra_ssl_hosts (
			id         INTEGER PRIMARY KEY,
			user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			name       TEXT NOT NULL,
			hostname   TEXT NOT NULL,
			port       INTEGER NOT NULL DEFAULT 443,
			created_at TEXT NOT NULL DEFAULT ''
		);
		CREATE TABLE infra_uptime_history (
			id         INTEGER PRIMARY KEY,
			user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			module     TEXT NOT NULL,
			target     TEXT NOT NULL,
			status     TEXT NOT NULL,
			message    TEXT NOT NULL DEFAULT '',
			checked_at TEXT NOT NULL DEFAULT ''
		);
		CREATE TABLE infra_hetzner_config (
			user_id    INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
			api_token  TEXT NOT NULL,
			updated_at TEXT NOT NULL DEFAULT ''
		);
		CREATE TABLE infra_docker_hosts (
			id         INTEGER PRIMARY KEY,
			user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			name       TEXT NOT NULL,
			url        TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT ''
		);
		INSERT INTO users (id, email, name, google_id) VALUES (1, 'test@example.com', 'Test', 'g1');
	`)
	if err != nil {
		t.Fatalf("create schema: %v", err)
	}
	return db
}

func TestIsModuleEnabled_Default(t *testing.T) {
	db := setupTestDB(t)
	enabled, err := IsModuleEnabled(db, 1, "health_checks")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !enabled {
		t.Error("expected module to be enabled by default")
	}
}

func TestSetModuleEnabled_DisableAndEnable(t *testing.T) {
	db := setupTestDB(t)

	if err := SetModuleEnabled(db, 1, "health_checks", false); err != nil {
		t.Fatalf("disable: %v", err)
	}
	enabled, err := IsModuleEnabled(db, 1, "health_checks")
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if enabled {
		t.Error("expected module to be disabled")
	}

	if err := SetModuleEnabled(db, 1, "health_checks", true); err != nil {
		t.Fatalf("enable: %v", err)
	}
	enabled, err = IsModuleEnabled(db, 1, "health_checks")
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if !enabled {
		t.Error("expected module to be enabled after re-enable")
	}
}

func TestGetModuleConfigs_Empty(t *testing.T) {
	db := setupTestDB(t)
	configs, err := GetModuleConfigs(db, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(configs) != 0 {
		t.Errorf("expected 0 configs, got %d", len(configs))
	}
}

func TestGetModuleConfigs_Multiple(t *testing.T) {
	db := setupTestDB(t)

	if err := SetModuleEnabled(db, 1, "health_checks", true); err != nil {
		t.Fatal(err)
	}
	if err := SetModuleEnabled(db, 1, "ssl_certs", false); err != nil {
		t.Fatal(err)
	}

	configs, err := GetModuleConfigs(db, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(configs) != 2 {
		t.Fatalf("expected 2 configs, got %d", len(configs))
	}
	// Ordered by module name
	if configs[0].Module != "health_checks" {
		t.Errorf("expected health_checks first, got %s", configs[0].Module)
	}
	if configs[1].Module != "ssl_certs" {
		t.Errorf("expected ssl_certs second, got %s", configs[1].Module)
	}
	if !configs[0].Enabled {
		t.Error("expected health_checks to be enabled")
	}
	if configs[1].Enabled {
		t.Error("expected ssl_certs to be disabled")
	}
}
