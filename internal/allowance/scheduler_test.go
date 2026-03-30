package allowance

import (
	"context"
	"database/sql"
	"net/http"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/encryption"
	_ "modernc.org/sqlite"
)

// setupSchedulerTestDB creates an in-memory DB with the tables needed for
// scheduler tests: users, family_links, allowance tables, notification_log,
// user_features, user_preferences, vapid_keys, and push_subscriptions.
func setupSchedulerTestDB(t *testing.T) *sql.DB {
	t.Helper()
	t.Setenv("ENCRYPTION_KEY", "test-encryption-key-scheduler-tests")
	encryption.ResetEncryptionKey()
	t.Cleanup(func() { encryption.ResetEncryptionKey() })

	db, err := sql.Open("sqlite", "file::memory:?_pragma=foreign_keys(ON)&_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	t.Cleanup(func() { db.Close() })

	schema := `
	CREATE TABLE IF NOT EXISTS users (
		id        INTEGER PRIMARY KEY,
		email     TEXT UNIQUE NOT NULL,
		name      TEXT NOT NULL,
		picture   TEXT NOT NULL DEFAULT '',
		google_id TEXT UNIQUE NOT NULL,
		is_admin  INTEGER NOT NULL DEFAULT 0
	);

	CREATE TABLE IF NOT EXISTS family_links (
		id           INTEGER PRIMARY KEY,
		parent_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		child_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		nickname     TEXT NOT NULL DEFAULT '',
		avatar_emoji TEXT NOT NULL DEFAULT '⭐',
		created_at   TEXT NOT NULL DEFAULT '',
		UNIQUE(parent_id, child_id),
		UNIQUE(child_id)
	);

	CREATE TABLE IF NOT EXISTS allowance_chores (
		id                INTEGER PRIMARY KEY,
		parent_id         INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		child_id          INTEGER REFERENCES users(id) ON DELETE CASCADE,
		name              TEXT NOT NULL DEFAULT '',
		description       TEXT NOT NULL DEFAULT '',
		amount            REAL NOT NULL DEFAULT 0,
		currency          TEXT NOT NULL DEFAULT 'NOK',
		frequency         TEXT NOT NULL DEFAULT 'daily',
		icon              TEXT NOT NULL DEFAULT '🧹',
		requires_approval INTEGER NOT NULL DEFAULT 1,
		active            INTEGER NOT NULL DEFAULT 1,
		created_at        TEXT NOT NULL DEFAULT '',
		completion_mode   TEXT NOT NULL DEFAULT 'solo',
		min_team_size     INTEGER NOT NULL DEFAULT 2,
		team_bonus_pct    REAL NOT NULL DEFAULT 10.0
	);

	CREATE TABLE IF NOT EXISTS allowance_completions (
		id            INTEGER PRIMARY KEY,
		chore_id      INTEGER NOT NULL REFERENCES allowance_chores(id) ON DELETE CASCADE,
		child_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		date          TEXT NOT NULL,
		status        TEXT NOT NULL DEFAULT 'pending',
		approved_by   INTEGER REFERENCES users(id),
		approved_at   TEXT,
		notes         TEXT NOT NULL DEFAULT '',
		quality_bonus REAL NOT NULL DEFAULT 0,
		photo_path    TEXT NOT NULL DEFAULT '',
		created_at    TEXT NOT NULL DEFAULT '',
		UNIQUE(chore_id, child_id, date)
	);

	CREATE TABLE IF NOT EXISTS allowance_extras (
		id           INTEGER PRIMARY KEY,
		parent_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		child_id     INTEGER REFERENCES users(id) ON DELETE CASCADE,
		name         TEXT NOT NULL DEFAULT '',
		amount       REAL NOT NULL DEFAULT 0,
		currency     TEXT NOT NULL DEFAULT 'NOK',
		status       TEXT NOT NULL DEFAULT 'open',
		claimed_by   INTEGER REFERENCES users(id),
		completed_at TEXT,
		approved_at  TEXT,
		expires_at   TEXT,
		created_at   TEXT NOT NULL DEFAULT ''
	);

	CREATE TABLE IF NOT EXISTS allowance_bonus_rules (
		id          INTEGER PRIMARY KEY,
		parent_id   INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		type        TEXT NOT NULL,
		multiplier  REAL NOT NULL DEFAULT 1.0,
		flat_amount REAL NOT NULL DEFAULT 0,
		active      INTEGER NOT NULL DEFAULT 1,
		UNIQUE(parent_id, type)
	);

	CREATE TABLE IF NOT EXISTS allowance_payouts (
		id           INTEGER PRIMARY KEY,
		parent_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		child_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		week_start   TEXT NOT NULL,
		base_amount  REAL NOT NULL DEFAULT 0,
		bonus_amount REAL NOT NULL DEFAULT 0,
		total_amount REAL NOT NULL DEFAULT 0,
		currency     TEXT NOT NULL DEFAULT 'NOK',
		paid_out     INTEGER NOT NULL DEFAULT 0,
		paid_at      TEXT,
		created_at   TEXT NOT NULL DEFAULT '',
		UNIQUE(parent_id, child_id, week_start)
	);

	CREATE TABLE IF NOT EXISTS allowance_settings (
		id                 INTEGER PRIMARY KEY,
		parent_id          INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		child_id           INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		base_weekly_amount REAL NOT NULL DEFAULT 0,
		currency           TEXT NOT NULL DEFAULT 'NOK',
		auto_approve_hours INTEGER NOT NULL DEFAULT 24,
		updated_at         TEXT NOT NULL DEFAULT '',
		UNIQUE(parent_id, child_id)
	);

	CREATE TABLE IF NOT EXISTS allowance_team_completions (
		id            INTEGER PRIMARY KEY,
		completion_id INTEGER NOT NULL REFERENCES allowance_completions(id) ON DELETE CASCADE,
		child_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		joined_at     TEXT NOT NULL DEFAULT '',
		UNIQUE(completion_id, child_id)
	);

	CREATE TABLE IF NOT EXISTS allowance_savings_goals (
		id             INTEGER PRIMARY KEY,
		parent_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		child_id       INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		name           TEXT NOT NULL DEFAULT '',
		target_amount  REAL NOT NULL DEFAULT 0,
		current_amount REAL NOT NULL DEFAULT 0,
		currency       TEXT NOT NULL DEFAULT 'NOK',
		deadline       TEXT,
		created_at     TEXT NOT NULL DEFAULT '',
		updated_at     TEXT NOT NULL DEFAULT ''
	);

	CREATE TABLE IF NOT EXISTS user_features (
		user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		feature_key TEXT NOT NULL,
		enabled     INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY (user_id, feature_key)
	);

	CREATE TABLE IF NOT EXISTS user_preferences (
		user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		key     TEXT NOT NULL,
		value   TEXT NOT NULL DEFAULT '',
		PRIMARY KEY (user_id, key)
	);

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

	CREATE TABLE IF NOT EXISTS notification_log (
		id         INTEGER PRIMARY KEY,
		user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		notif_type TEXT NOT NULL,
		reference  TEXT NOT NULL DEFAULT '',
		sent_at    TEXT NOT NULL DEFAULT ''
	);

	CREATE INDEX IF NOT EXISTS idx_notification_log_lookup
		ON notification_log(user_id, notif_type, reference, sent_at);

	CREATE TABLE IF NOT EXISTS allowance_bingo_cards (
		id              INTEGER PRIMARY KEY,
		child_id        INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		parent_id       INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		week_start      TEXT NOT NULL,
		cells           TEXT NOT NULL DEFAULT '[]',
		completed_lines INTEGER NOT NULL DEFAULT 0,
		full_card       INTEGER NOT NULL DEFAULT 0,
		bonus_earned    REAL NOT NULL DEFAULT 0,
		created_at      TEXT NOT NULL DEFAULT '',
		updated_at      TEXT NOT NULL DEFAULT '',
		UNIQUE(child_id, week_start)
	);
	`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}

	if _, err := db.Exec(`
		INSERT INTO users (id, email, name, google_id) VALUES
		(1, 'parent@test.com', 'Parent', 'gp1'),
		(2, 'child@test.com',  'Child',  'gc2')
	`); err != nil {
		t.Fatalf("seed users: %v", err)
	}

	return db
}

func seedSchedulerLink(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO family_links (parent_id, child_id, created_at) VALUES (1, 2, '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("seed family link: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO user_features (user_id, feature_key, enabled) VALUES (1, 'kids_allowance', 1)`); err != nil {
		t.Fatalf("seed user feature: %v", err)
	}
}

// TestGenerateWeeklyPayouts_Idempotent verifies that calling GenerateWeeklyPayouts
// twice for the same week produces exactly one payout row (UpsertPayout semantics).
func TestGenerateWeeklyPayouts_Idempotent(t *testing.T) {
	db := setupSchedulerTestDB(t)
	seedSchedulerLink(t, db)

	client := &http.Client{}
	GenerateWeeklyPayouts(db, client)
	GenerateWeeklyPayouts(db, client)

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM allowance_payouts`).Scan(&count); err != nil {
		t.Fatalf("count payouts: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 payout row after two calls, got %d", count)
	}
}

// TestSendWeeklyPayoutNotification_QuietHours verifies that the notification is
// not logged when the parent has quiet hours active (always-on window).
func TestSendWeeklyPayoutNotification_QuietHours(t *testing.T) {
	db := setupSchedulerTestDB(t)

	// Set parent's quiet hours to cover all 24 hours so IsActive always returns true.
	if _, err := db.Exec(`
		INSERT INTO user_preferences (user_id, key, value) VALUES
		(1, 'quiet_hours_enabled',  'true'),
		(1, 'quiet_hours_start',    '00:00'),
		(1, 'quiet_hours_end',      '23:59'),
		(1, 'quiet_hours_timezone', 'UTC')
	`); err != nil {
		t.Fatalf("seed quiet hours prefs: %v", err)
	}

	weekStart := MondayOf(time.Now().UTC())
	earnings := &WeeklyEarnings{
		WeekStart:     weekStart,
		TotalAmount:   100,
		BaseAllowance: 100,
		Currency:      "NOK",
	}

	sendWeeklyPayoutNotification(db, &http.Client{}, 1, 2, earnings)

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM notification_log WHERE user_id = 1`).Scan(&count); err != nil {
		t.Fatalf("count notification_log: %v", err)
	}
	if count != 0 {
		t.Errorf("expected no notification_log entry during quiet hours, got %d", count)
	}
}

// TestSendWeeklyPayoutNotification_DedupByLog verifies that a second call for
// the same (parent, child, week) is skipped when an entry already exists in
// notification_log.
func TestSendWeeklyPayoutNotification_DedupByLog(t *testing.T) {
	db := setupSchedulerTestDB(t)

	weekStart := MondayOf(time.Now().UTC())
	earnings := &WeeklyEarnings{
		WeekStart:     weekStart,
		TotalAmount:   50,
		BaseAllowance: 50,
		Currency:      "NOK",
	}

	// Pre-seed a notification_log entry as if a successful delivery already happened.
	reference := "2-" + weekStart
	if _, err := db.ExecContext(context.Background(), `
		INSERT INTO notification_log (user_id, notif_type, reference, sent_at)
		VALUES (1, 'allowance-payout', ?, ?)
	`, reference, time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatalf("seed notification_log: %v", err)
	}

	// A second send attempt should be deduped — no new log rows.
	sendWeeklyPayoutNotification(db, &http.Client{}, 1, 2, earnings)

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM notification_log WHERE user_id = 1`).Scan(&count); err != nil {
		t.Fatalf("count notification_log: %v", err)
	}
	// Should still be exactly 1 (the pre-seeded row, not a new one).
	if count != 1 {
		t.Errorf("expected 1 notification_log row after dedup, got %d", count)
	}
}

// TestSendWeeklyPayoutNotification_NoLogWhenNoSubscriptions verifies that
// notification_log is NOT written when the user has no push subscriptions
// (SendToUser returns zero results → no successful delivery).
func TestSendWeeklyPayoutNotification_NoLogWhenNoSubscriptions(t *testing.T) {
	db := setupSchedulerTestDB(t)
	// Parent (user 1) has no push subscriptions.

	weekStart := MondayOf(time.Now().UTC())
	earnings := &WeeklyEarnings{
		WeekStart:     weekStart,
		TotalAmount:   75,
		BaseAllowance: 75,
		Currency:      "NOK",
	}

	sendWeeklyPayoutNotification(db, &http.Client{}, 1, 2, earnings)

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM notification_log WHERE user_id = 1`).Scan(&count); err != nil {
		t.Fatalf("count notification_log: %v", err)
	}
	if count != 0 {
		t.Errorf("expected no notification_log entry when no subscriptions delivered, got %d", count)
	}
}
