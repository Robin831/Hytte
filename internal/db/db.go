package db

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log"
	"strings"

	"github.com/Robin831/Hytte/internal/encryption"
	_ "modernc.org/sqlite"
)

// hashSessionToken returns the SHA-256 hex digest of a session token.
// Duplicated from auth package to avoid circular imports.
func hashSessionToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// Init opens a SQLite database at the given path with WAL mode enabled and
// creates the schema if it does not already exist.
func Init(path string) (*sql.DB, error) {
	// Embed PRAGMAs in the DSN so they are applied to every new connection
	// opened by database/sql's pool. This lets the pool hold more than one
	// connection (benefiting WAL concurrent reads) while still guaranteeing
	// foreign_keys=ON on each one.
	dsn := fmt.Sprintf("file:%s?_pragma=foreign_keys(ON)&_pragma=journal_mode(WAL)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	if err := createSchema(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("create schema: %w", err)
	}

	return db, nil
}

func createSchema(db *sql.DB) error {
	schema := `
	-- schema_migrations stores global one-time migration sentinels (e.g. data
	-- encryption). It is separate from user_preferences (which is per-user)
	-- so that database-wide migrations can be tracked independently.
	CREATE TABLE IF NOT EXISTS schema_migrations (
		key   TEXT PRIMARY KEY,
		value TEXT NOT NULL DEFAULT ''
	);

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

	CREATE TABLE IF NOT EXISTS workouts (
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
		UNIQUE(user_id, fit_file_hash)
	);

	CREATE INDEX IF NOT EXISTS idx_workouts_user_id ON workouts(user_id);

	CREATE TABLE IF NOT EXISTS lactate_tests (
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

	CREATE TABLE IF NOT EXISTS workout_analyses (
		id               INTEGER PRIMARY KEY,
		user_id          INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		workout_id       INTEGER NOT NULL REFERENCES workouts(id) ON DELETE CASCADE,
		analysis_type    TEXT NOT NULL DEFAULT 'tag',
		model            TEXT NOT NULL,
		prompt           TEXT NOT NULL,
		response_json    TEXT NOT NULL,
		tags             TEXT NOT NULL DEFAULT '',
		summary          TEXT NOT NULL DEFAULT '',
		title            TEXT NOT NULL DEFAULT '',
		confidence_score REAL NOT NULL DEFAULT 0,
		confidence_note  TEXT NOT NULL DEFAULT '',
		created_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
		UNIQUE(user_id, workout_id, analysis_type)
	);

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
		user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		name       TEXT NOT NULL,
		url        TEXT NOT NULL,
		created_at TEXT NOT NULL DEFAULT ''
	);

	CREATE INDEX IF NOT EXISTS idx_infra_health_services_user_id ON infra_health_services(user_id);

	CREATE TABLE IF NOT EXISTS infra_ssl_hosts (
		id         INTEGER PRIMARY KEY,
		user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		name       TEXT NOT NULL,
		hostname   TEXT NOT NULL,
		port       INTEGER NOT NULL DEFAULT 443,
		created_at TEXT NOT NULL DEFAULT ''
	);

	CREATE INDEX IF NOT EXISTS idx_infra_ssl_hosts_user_id ON infra_ssl_hosts(user_id);

	CREATE TABLE IF NOT EXISTS infra_uptime_history (
		id         INTEGER PRIMARY KEY,
		user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		module     TEXT NOT NULL,
		target     TEXT NOT NULL,
		status     TEXT NOT NULL,
		message    TEXT NOT NULL DEFAULT '',
		checked_at TEXT NOT NULL DEFAULT ''
	);

	CREATE INDEX IF NOT EXISTS idx_infra_uptime_user_checked
		ON infra_uptime_history(user_id, checked_at DESC);

	CREATE TABLE IF NOT EXISTS infra_hetzner_config (
		user_id    INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
		api_token  TEXT NOT NULL,
		updated_at TEXT NOT NULL DEFAULT ''
	);

	CREATE TABLE IF NOT EXISTS infra_docker_hosts (
		id         INTEGER PRIMARY KEY,
		user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		name       TEXT NOT NULL,
		url        TEXT NOT NULL,
		created_at TEXT NOT NULL DEFAULT ''
	);

	CREATE INDEX IF NOT EXISTS idx_infra_docker_hosts_user_id ON infra_docker_hosts(user_id);

	CREATE TABLE IF NOT EXISTS infra_github_config (
		user_id    INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
		api_token  TEXT NOT NULL,
		updated_at TEXT NOT NULL DEFAULT ''
	);

	CREATE TABLE IF NOT EXISTS infra_github_repos (
		id         INTEGER PRIMARY KEY,
		user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		owner      TEXT NOT NULL,
		repo       TEXT NOT NULL,
		created_at TEXT NOT NULL DEFAULT ''
	);

	CREATE INDEX IF NOT EXISTS idx_infra_github_repos_user_id ON infra_github_repos(user_id);

	CREATE TABLE IF NOT EXISTS infra_dns_monitors (
		id          INTEGER PRIMARY KEY,
		user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		name        TEXT NOT NULL,
		hostname    TEXT NOT NULL,
		record_type TEXT NOT NULL DEFAULT 'A',
		created_at  TEXT NOT NULL DEFAULT ''
	);

	CREATE INDEX IF NOT EXISTS idx_infra_dns_monitors_user_id ON infra_dns_monitors(user_id);

	CREATE TABLE IF NOT EXISTS infra_systemd_services (
		id         INTEGER PRIMARY KEY,
		user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		name       TEXT NOT NULL,
		unit       TEXT NOT NULL,
		created_at TEXT NOT NULL DEFAULT ''
	);

	CREATE INDEX IF NOT EXISTS idx_infra_systemd_services_user_id ON infra_systemd_services(user_id);

	CREATE TABLE IF NOT EXISTS infra_module_preferences (
		user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		module     TEXT NOT NULL,
		key        TEXT NOT NULL,
		value      TEXT NOT NULL DEFAULT '',
		updated_at TEXT NOT NULL DEFAULT '',
		PRIMARY KEY (user_id, module, key)
	);

	CREATE INDEX IF NOT EXISTS idx_infra_module_preferences_user_module ON infra_module_preferences(user_id, module);

	CREATE TABLE IF NOT EXISTS training_insights (
		workout_id INTEGER NOT NULL REFERENCES workouts(id) ON DELETE CASCADE,
		user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		response   TEXT NOT NULL DEFAULT '{}',
		model      TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL DEFAULT '',
		PRIMARY KEY (workout_id, user_id)
	);

	CREATE INDEX IF NOT EXISTS idx_training_insights_user_id ON training_insights(user_id);

	CREATE TABLE IF NOT EXISTS comparison_analyses (
		id            INTEGER PRIMARY KEY,
		user_id       INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		workout_id_a  INTEGER NOT NULL REFERENCES workouts(id) ON DELETE CASCADE,
		workout_id_b  INTEGER NOT NULL REFERENCES workouts(id) ON DELETE CASCADE,
		model         TEXT NOT NULL DEFAULT '',
		prompt        TEXT NOT NULL DEFAULT '',
		response_json TEXT NOT NULL DEFAULT '{}',
		created_at    TEXT NOT NULL DEFAULT '',
		UNIQUE(user_id, workout_id_a, workout_id_b)
	);

	CREATE INDEX IF NOT EXISTS idx_comparison_analyses_user_workout_b ON comparison_analyses(user_id, workout_id_b);

	CREATE TABLE IF NOT EXISTS user_features (
		user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		feature_key TEXT NOT NULL,
		enabled     INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY (user_id, feature_key)
	);

	CREATE TABLE IF NOT EXISTS chat_conversations (
		id         INTEGER PRIMARY KEY,
		user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		title      TEXT NOT NULL DEFAULT '',
		model      TEXT NOT NULL,
		created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
		updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
	);

	CREATE INDEX IF NOT EXISTS idx_chat_conversations_user_id ON chat_conversations(user_id);

	CREATE TABLE IF NOT EXISTS chat_messages (
		id              INTEGER PRIMARY KEY,
		conversation_id INTEGER NOT NULL REFERENCES chat_conversations(id) ON DELETE CASCADE,
		role            TEXT NOT NULL,
		content         TEXT NOT NULL,
		created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
	);

	CREATE INDEX IF NOT EXISTS idx_chat_messages_conversation_id ON chat_messages(conversation_id);

	CREATE TABLE IF NOT EXISTS weekly_load (
		user_id       INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		week_start    TEXT NOT NULL,
		easy_load     REAL NOT NULL DEFAULT 0,
		hard_load     REAL NOT NULL DEFAULT 0,
		total_load    REAL NOT NULL DEFAULT 0,
		workout_count INTEGER NOT NULL DEFAULT 0,
		updated_at    TEXT NOT NULL DEFAULT '',
		PRIMARY KEY (user_id, week_start)
	);

	CREATE TABLE IF NOT EXISTS training_summaries (
		user_id       INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		period        TEXT NOT NULL DEFAULT 'week',
		week_start    TEXT NOT NULL,
		status        TEXT NOT NULL DEFAULT '',
		acr           REAL,
		acute_load    REAL NOT NULL DEFAULT 0,
		chronic_load  REAL NOT NULL DEFAULT 0,
		prompt        TEXT NOT NULL DEFAULT '',
		response_json TEXT NOT NULL DEFAULT '',
		model         TEXT NOT NULL DEFAULT '',
		updated_at    TEXT NOT NULL DEFAULT '',
		PRIMARY KEY (user_id, period, week_start)
	);

	CREATE TABLE IF NOT EXISTS vo2max_estimates (
		id           INTEGER PRIMARY KEY,
		user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		workout_id   INTEGER NOT NULL REFERENCES workouts(id) ON DELETE CASCADE,
		vo2max       REAL NOT NULL,
		method       TEXT NOT NULL DEFAULT '',
		estimated_at TEXT NOT NULL DEFAULT '',
		UNIQUE(user_id, workout_id)
	);

	CREATE INDEX IF NOT EXISTS idx_vo2max_estimates_user_estimated ON vo2max_estimates(user_id, estimated_at);

	-- Kids Stars: parent-child account linking (Hytte-29xk)
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

	CREATE INDEX IF NOT EXISTS idx_family_links_parent ON family_links(parent_id);
	CREATE INDEX IF NOT EXISTS idx_family_links_child ON family_links(child_id);

	-- Kids Stars: invite codes for linking child accounts (single-use, 24h TTL)
	CREATE TABLE IF NOT EXISTS invite_codes (
		id         INTEGER PRIMARY KEY,
		code       TEXT NOT NULL UNIQUE,
		parent_id  INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		used       INTEGER NOT NULL DEFAULT 0,
		expires_at TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL DEFAULT ''
	);

	CREATE INDEX IF NOT EXISTS idx_invite_codes_parent ON invite_codes(parent_id);

	-- Kids Stars: immutable ledger of stars earned/spent (Hytte-29xk)
	CREATE TABLE IF NOT EXISTS star_transactions (
		id           INTEGER PRIMARY KEY,
		user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		amount       INTEGER NOT NULL,
		reason       TEXT NOT NULL,
		description  TEXT NOT NULL DEFAULT '',
		reference_id INTEGER,
		created_at   TEXT NOT NULL DEFAULT ''
	);

	CREATE INDEX IF NOT EXISTS idx_star_transactions_user ON star_transactions(user_id, created_at);

	-- Kids Stars: denormalized balance cache (Hytte-29xk)
	CREATE TABLE IF NOT EXISTS star_balances (
		user_id         INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
		total_earned    INTEGER NOT NULL DEFAULT 0,
		total_spent     INTEGER NOT NULL DEFAULT 0,
		current_balance INTEGER GENERATED ALWAYS AS (total_earned - total_spent) STORED
	);

	-- Kids Stars: XP/level tracking (Hytte-29xk)
	CREATE TABLE IF NOT EXISTS user_levels (
		user_id INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
		xp      INTEGER NOT NULL DEFAULT 0,
		level   INTEGER NOT NULL DEFAULT 1,
		title   TEXT NOT NULL DEFAULT 'Rookie Runner'
	);

	-- Kids Stars: workout streak tracking (Hytte-pddz)
	CREATE TABLE IF NOT EXISTS streaks (
		user_id       INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		streak_type   TEXT NOT NULL,
		current_count INTEGER NOT NULL DEFAULT 0,
		longest_count INTEGER NOT NULL DEFAULT 0,
		last_activity TEXT NOT NULL DEFAULT '',
		PRIMARY KEY (user_id, streak_type)
	);

	-- Badge definitions: all available badges seeded at startup (Hytte-w1k4)
	CREATE TABLE IF NOT EXISTS badge_definitions (
		id          INTEGER PRIMARY KEY,
		key         TEXT UNIQUE NOT NULL,
		name        TEXT NOT NULL DEFAULT '',
		description TEXT NOT NULL DEFAULT '',
		category    TEXT NOT NULL DEFAULT '',
		icon        TEXT NOT NULL DEFAULT '🏅',
		xp_reward   INTEGER NOT NULL DEFAULT 0
	);

	-- User badges: records of badges earned by users (Hytte-w1k4)
	CREATE TABLE IF NOT EXISTS user_badges (
		id         INTEGER PRIMARY KEY,
		user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		badge_key  TEXT NOT NULL,
		earned_at  TEXT NOT NULL DEFAULT '',
		workout_id INTEGER REFERENCES workouts(id) ON DELETE SET NULL,
		UNIQUE(user_id, badge_key)
	);

	CREATE INDEX IF NOT EXISTS idx_user_badges_user_id ON user_badges(user_id);`

	_, err := db.Exec(schema)
	if err != nil {
		return err
	}

	// Clean up orphaned workout child rows left behind by deletes that
	// occurred before ON DELETE CASCADE was properly enforced (Hytte-c93).
	// These statements are no-ops on databases without orphans.
	orphanCleanup := `
	DELETE FROM workout_laps WHERE workout_id NOT IN (SELECT id FROM workouts);
	DELETE FROM workout_samples WHERE workout_id NOT IN (SELECT id FROM workouts);
	DELETE FROM workout_tags WHERE workout_id NOT IN (SELECT id FROM workouts);`

	_, err = db.Exec(orphanCleanup)
	if err != nil {
		return err
	}

	// Migrate training_insights to composite PRIMARY KEY (workout_id, user_id) — Hytte-5co review.
	// The original schema used workout_id as the sole PK; SQLite requires a full table
	// recreation to change the primary key.  We detect the old schema by counting
	// PK columns: old = 1 (workout_id only), new = 2 (workout_id + user_id).
	var insightsPKCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('training_insights') WHERE pk > 0`).Scan(&insightsPKCount); err != nil {
		return fmt.Errorf("check training_insights pk: %w", err)
	}
	if insightsPKCount == 1 {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("begin training_insights migration: %w", err)
		}
		migrationSteps := []string{
			`CREATE TABLE training_insights_v2 (
				workout_id INTEGER NOT NULL REFERENCES workouts(id) ON DELETE CASCADE,
				user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
				response   TEXT NOT NULL DEFAULT '{}',
				model      TEXT NOT NULL DEFAULT '',
				created_at TEXT NOT NULL DEFAULT '',
				PRIMARY KEY (workout_id, user_id)
			)`,
			`INSERT OR IGNORE INTO training_insights_v2
				SELECT workout_id, user_id, response, model, created_at FROM training_insights`,
			`DROP TABLE training_insights`,
			`ALTER TABLE training_insights_v2 RENAME TO training_insights`,
			`CREATE INDEX IF NOT EXISTS idx_training_insights_user_id ON training_insights(user_id)`,
		}
		for _, step := range migrationSteps {
			if _, err := tx.Exec(step); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("migrate training_insights pk: %w", err)
			}
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit training_insights migration: %w", err)
		}
	}

	// Add is_admin column to users table (Hytte-2lp). ALTER TABLE is a
	// no-op if the column already exists, but SQLite does not support
	// IF NOT EXISTS for columns, so we check the schema first.
	var hasAdmin int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('users') WHERE name = 'is_admin'`).Scan(&hasAdmin); err != nil {
		return fmt.Errorf("check is_admin column: %w", err)
	}
	if hasAdmin == 0 {
		if _, err := db.Exec(`ALTER TABLE users ADD COLUMN is_admin INTEGER NOT NULL DEFAULT 0`); err != nil {
			return err
		}
		// Promote the earliest registered user to admin (if any exist).
		// On a fresh DB this is a no-op; UpsertUser handles first-user promotion.
		if _, err := db.Exec(`UPDATE users SET is_admin = 1 WHERE id = (SELECT MIN(id) FROM users)`); err != nil {
			return err
		}
	}

	// Add title_source column to workouts table (Hytte-h7v).
	var hasTitleSource int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('workouts') WHERE name = 'title_source'`).Scan(&hasTitleSource); err != nil {
		return fmt.Errorf("check title_source column: %w", err)
	}
	if hasTitleSource == 0 {
		if _, err := db.Exec(`ALTER TABLE workouts ADD COLUMN title_source TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}

	// Add title column to workout_analyses table (Hytte-h7v).
	var hasAnalysisTitle int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('workout_analyses') WHERE name = 'title'`).Scan(&hasAnalysisTitle); err != nil {
		return fmt.Errorf("check workout_analyses title column: %w", err)
	}
	if hasAnalysisTitle == 0 {
		if _, err := db.Exec(`ALTER TABLE workout_analyses ADD COLUMN title TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}

	// Add confidence_score and confidence_note columns to workout_analyses table (Hytte-z952).
	var hasAnalysisConfidenceScore int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('workout_analyses') WHERE name = 'confidence_score'`).Scan(&hasAnalysisConfidenceScore); err != nil {
		return fmt.Errorf("check workout_analyses confidence_score column: %w", err)
	}
	if hasAnalysisConfidenceScore == 0 {
		if _, err := db.Exec(`ALTER TABLE workout_analyses ADD COLUMN confidence_score REAL NOT NULL DEFAULT 0`); err != nil {
			return err
		}
	}
	var hasAnalysisConfidenceNote int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('workout_analyses') WHERE name = 'confidence_note'`).Scan(&hasAnalysisConfidenceNote); err != nil {
		return fmt.Errorf("check workout_analyses confidence_note column: %w", err)
	}
	if hasAnalysisConfidenceNote == 0 {
		if _, err := db.Exec(`ALTER TABLE workout_analyses ADD COLUMN confidence_note TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}

	// Add sub_sport and is_indoor columns to workouts table (Hytte-73t).
	// Check each column independently so a partially-migrated DB gets fully upgraded.
	var hasSubSport int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('workouts') WHERE name = 'sub_sport'`).Scan(&hasSubSport); err != nil {
		return fmt.Errorf("check sub_sport column: %w", err)
	}
	if hasSubSport == 0 {
		if _, err := db.Exec(`ALTER TABLE workouts ADD COLUMN sub_sport TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	var hasIsIndoor int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('workouts') WHERE name = 'is_indoor'`).Scan(&hasIsIndoor); err != nil {
		return fmt.Errorf("check is_indoor column: %w", err)
	}
	if hasIsIndoor == 0 {
		if _, err := db.Exec(`ALTER TABLE workouts ADD COLUMN is_indoor INTEGER NOT NULL DEFAULT 0`); err != nil {
			return err
		}
	}

	// Add analysis_status column to workouts table (Hytte-9ik).
	var hasAnalysisStatus int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('workouts') WHERE name = 'analysis_status'`).Scan(&hasAnalysisStatus); err != nil {
		return fmt.Errorf("check analysis_status column: %w", err)
	}
	if hasAnalysisStatus == 0 {
		if _, err := db.Exec(`ALTER TABLE workouts ADD COLUMN analysis_status TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}

	// Add workout_id column to lactate_tests table (Hytte-f8av).
	var hasWorkoutID int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('lactate_tests') WHERE name = 'workout_id'`).Scan(&hasWorkoutID); err != nil {
		return fmt.Errorf("check workout_id column: %w", err)
	}
	if hasWorkoutID == 0 {
		if _, err := db.Exec(`ALTER TABLE lactate_tests ADD COLUMN workout_id INTEGER REFERENCES workouts(id) ON DELETE SET NULL`); err != nil {
			return err
		}
	}

	// Add training_load, hr_drift_pct, pace_cv_pct columns to workouts table (Hytte-53c7).
	var hasTrainingLoad int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('workouts') WHERE name = 'training_load'`).Scan(&hasTrainingLoad); err != nil {
		return fmt.Errorf("check training_load column: %w", err)
	}
	if hasTrainingLoad == 0 {
		if _, err := db.Exec(`ALTER TABLE workouts ADD COLUMN training_load REAL`); err != nil {
			return err
		}
	}
	var hasHRDriftPct int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('workouts') WHERE name = 'hr_drift_pct'`).Scan(&hasHRDriftPct); err != nil {
		return fmt.Errorf("check hr_drift_pct column: %w", err)
	}
	if hasHRDriftPct == 0 {
		if _, err := db.Exec(`ALTER TABLE workouts ADD COLUMN hr_drift_pct REAL`); err != nil {
			return err
		}
	}
	var hasPaceCVPct int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('workouts') WHERE name = 'pace_cv_pct'`).Scan(&hasPaceCVPct); err != nil {
		return fmt.Errorf("check pace_cv_pct column: %w", err)
	}
	if hasPaceCVPct == 0 {
		if _, err := db.Exec(`ALTER TABLE workouts ADD COLUMN pace_cv_pct REAL`); err != nil {
			return err
		}
	}

	// Migrate training_summaries to composite PK (user_id, period, week_start) and add AI fields — Hytte-g1w9.
	// Old schema: PK (user_id, week_start). New schema: PK (user_id, period, week_start) + prompt/response_json/model.
	var tsSummaryPKCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('training_summaries') WHERE pk > 0`).Scan(&tsSummaryPKCount); err != nil {
		return fmt.Errorf("check training_summaries pk: %w", err)
	}
	if tsSummaryPKCount != 3 {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("begin training_summaries migration: %w", err)
		}
		tsMigrationSteps := []string{
			`CREATE TABLE training_summaries_v2 (
				user_id       INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
				period        TEXT NOT NULL DEFAULT 'week',
				week_start    TEXT NOT NULL,
				status        TEXT NOT NULL DEFAULT '',
				acr           REAL,
				acute_load    REAL NOT NULL DEFAULT 0,
				chronic_load  REAL NOT NULL DEFAULT 0,
				prompt        TEXT NOT NULL DEFAULT '',
				response_json TEXT NOT NULL DEFAULT '',
				model         TEXT NOT NULL DEFAULT '',
				updated_at    TEXT NOT NULL DEFAULT '',
				PRIMARY KEY (user_id, period, week_start)
			)`,
			`INSERT OR IGNORE INTO training_summaries_v2
				(user_id, period, week_start, status, acr, acute_load, chronic_load, updated_at)
				SELECT user_id, 'week', week_start, status, acr, acute_load, chronic_load, updated_at
				FROM training_summaries`,
			`DROP TABLE training_summaries`,
			`ALTER TABLE training_summaries_v2 RENAME TO training_summaries`,
		}
		for _, step := range tsMigrationSteps {
			if _, err := tx.Exec(step); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("migrate training_summaries: %w", err)
			}
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit training_summaries migration: %w", err)
		}
	}

	// Migrate existing session tokens to SHA-256 hashes.
	// Raw tokens are 64-char hex (32 bytes); SHA-256 hex hashes are also 64-char
	// but have different character distribution. We detect unhashed tokens by
	// checking if any token, when hashed, differs from itself (they all will
	// unless already hashed). A simpler signal: run once and mark done with a
	// pragma or sentinel. We use a safe approach: hash all tokens that aren't
	// already hashed. Since we can't distinguish raw from hash by format alone,
	// we use a migration flag in a dedicated table.
	var migrated int
	db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('sessions') WHERE name = 'token'`).Scan(&migrated)
	if migrated > 0 {
		// Check if migration was already done by looking for a sentinel row in
		// schema_migrations (no FK constraints, unlike user_preferences).
		var done int
		if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE key = 'session_hash_migrated'`).Scan(&done); err != nil {
			return fmt.Errorf("check session hash sentinel: %w", err)
		}
		if done == 0 {
			// Hash all existing raw tokens inside a transaction so the sentinel
			// is only written after all updates succeed. Any error rolls back
			// the entire migration, leaving the DB in a consistent state.
			tx, err := db.Begin()
			if err != nil {
				return fmt.Errorf("begin session hash migration tx: %w", err)
			}
			rows, err := tx.Query(`SELECT rowid, token FROM sessions`)
			if err != nil {
				tx.Rollback()
				return fmt.Errorf("query sessions for hash migration: %w", err)
			}
			type row struct {
				rowid int64
				token string
			}
			var toUpdate []row
			for rows.Next() {
				var r row
				if err := rows.Scan(&r.rowid, &r.token); err != nil {
					rows.Close()
					tx.Rollback()
					return fmt.Errorf("scan session row: %w", err)
				}
				toUpdate = append(toUpdate, r)
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				tx.Rollback()
				return fmt.Errorf("iterate sessions for hash migration: %w", err)
			}
			for _, r := range toUpdate {
				h := hashSessionToken(r.token)
				if _, err := tx.Exec(`UPDATE sessions SET token = ? WHERE rowid = ?`, h, r.rowid); err != nil {
					tx.Rollback()
					return fmt.Errorf("update session token hash: %w", err)
				}
			}
			if _, err := tx.Exec(`INSERT OR IGNORE INTO schema_migrations (key, value) VALUES ('session_hash_migrated', '1')`); err != nil {
				tx.Rollback()
				return fmt.Errorf("set session hash sentinel: %w", err)
			}
			if err := tx.Commit(); err != nil {
				return fmt.Errorf("commit session hash migration: %w", err)
			}
		}
	}

	// Encrypt existing plaintext sensitive data in-place (Hytte-5nuh).
	// This mirrors the storage layer encryption (Hytte-to51) which encrypts
	// new data on write; this migration handles pre-existing rows.
	if err := migrateEncryptData(db); err != nil {
		return fmt.Errorf("encrypt data migration: %w", err)
	}

	// Add tier column to badge_definitions (Hytte-0sgn).
	var hasBadgeTier int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('badge_definitions') WHERE name = 'tier'`).Scan(&hasBadgeTier); err != nil {
		return fmt.Errorf("check badge_definitions tier column: %w", err)
	}
	if hasBadgeTier == 0 {
		if _, err := db.Exec(`ALTER TABLE badge_definitions ADD COLUMN tier TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}

	return nil
}

// encryptFieldIfPlaintext returns the encrypted value if the input is plaintext,
// or the original value if it is empty or already encrypted (has "enc:" prefix).
func encryptFieldIfPlaintext(value string) (string, bool, error) {
	if value == "" {
		return value, false, nil
	}
	if strings.HasPrefix(value, "enc:") {
		return value, false, nil
	}
	encrypted, err := encryption.EncryptField(value)
	if err != nil {
		return "", false, err
	}
	return encrypted, true, nil
}

// migrateEncryptData encrypts all existing plaintext sensitive data in-place.
// Uses a sentinel row in schema_migrations to ensure it only runs once.
func migrateEncryptData(db *sql.DB) error {
	var done int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE key = 'data_encryption_migrated'`).Scan(&done); err != nil {
		return fmt.Errorf("check encryption sentinel: %w", err)
	}
	if done > 0 {
		return nil
	}

	// Each table migration is wrapped in its own transaction for atomicity.

	// 1. notes: title, content
	if err := encryptTableColumns(db, "notes",
		"SELECT id, title, content FROM notes",
		[]string{"title", "content"},
		func(tx *sql.Tx, id int64, values []any) error {
			_, err := tx.Exec(`UPDATE notes SET title = ?, content = ? WHERE id = ?`, values[0], values[1], id)
			return err
		},
	); err != nil {
		return fmt.Errorf("encrypt notes: %w", err)
	}

	// 2. lactate_tests: comment
	if err := encryptTableColumns(db, "lactate_tests",
		"SELECT id, comment FROM lactate_tests",
		[]string{"comment"},
		func(tx *sql.Tx, id int64, values []any) error {
			_, err := tx.Exec(`UPDATE lactate_tests SET comment = ? WHERE id = ?`, values[0], id)
			return err
		},
	); err != nil {
		return fmt.Errorf("encrypt lactate_tests: %w", err)
	}

	// 3. lactate_test_stages: notes
	if err := encryptTableColumns(db, "lactate_test_stages",
		"SELECT id, notes FROM lactate_test_stages",
		[]string{"notes"},
		func(tx *sql.Tx, id int64, values []any) error {
			_, err := tx.Exec(`UPDATE lactate_test_stages SET notes = ? WHERE id = ?`, values[0], id)
			return err
		},
	); err != nil {
		return fmt.Errorf("encrypt lactate_test_stages: %w", err)
	}

	// 4. push_subscriptions: p256dh, auth
	if err := encryptTableColumns(db, "push_subscriptions",
		"SELECT id, p256dh, auth FROM push_subscriptions",
		[]string{"p256dh", "auth"},
		func(tx *sql.Tx, id int64, values []any) error {
			_, err := tx.Exec(`UPDATE push_subscriptions SET p256dh = ?, auth = ? WHERE id = ?`, values[0], values[1], id)
			return err
		},
	); err != nil {
		return fmt.Errorf("encrypt push_subscriptions: %w", err)
	}

	// 5. vapid_keys: private_key
	if err := encryptTableColumns(db, "vapid_keys",
		"SELECT id, private_key FROM vapid_keys",
		[]string{"private_key"},
		func(tx *sql.Tx, id int64, values []any) error {
			_, err := tx.Exec(`UPDATE vapid_keys SET private_key = ? WHERE id = ?`, values[0], id)
			return err
		},
	); err != nil {
		return fmt.Errorf("encrypt vapid_keys: %w", err)
	}

	// 6. workout_analyses: prompt, response_json
	if err := encryptTableColumns(db, "workout_analyses",
		"SELECT id, prompt, response_json FROM workout_analyses",
		[]string{"prompt", "response_json"},
		func(tx *sql.Tx, id int64, values []any) error {
			_, err := tx.Exec(`UPDATE workout_analyses SET prompt = ?, response_json = ? WHERE id = ?`, values[0], values[1], id)
			return err
		},
	); err != nil {
		return fmt.Errorf("encrypt workout_analyses: %w", err)
	}

	// 7. comparison_analyses: prompt, response_json
	if err := encryptTableColumns(db, "comparison_analyses",
		"SELECT id, prompt, response_json FROM comparison_analyses",
		[]string{"prompt", "response_json"},
		func(tx *sql.Tx, id int64, values []any) error {
			_, err := tx.Exec(`UPDATE comparison_analyses SET prompt = ?, response_json = ? WHERE id = ?`, values[0], values[1], id)
			return err
		},
	); err != nil {
		return fmt.Errorf("encrypt comparison_analyses: %w", err)
	}

	// Set sentinel to prevent re-running.
	_, err := db.Exec(`INSERT INTO schema_migrations (key, value) VALUES ('data_encryption_migrated', '1')`)
	if err != nil {
		return fmt.Errorf("set encryption sentinel: %w", err)
	}

	return nil
}

// encryptTableColumns reads rows from the given query (which must return id + N value columns),
// encrypts any plaintext values, and updates them in a single transaction.
// NULL column values are preserved as nil in the values slice passed to updateFn.
func encryptTableColumns(
	db *sql.DB,
	tableName string,
	query string,
	columnNames []string,
	updateFn func(tx *sql.Tx, id int64, values []any) error,
) error {
	rows, err := db.Query(query)
	if err != nil {
		return err
	}
	defer rows.Close()

	// Determine which rows need encryption and encrypt their values while streaming.
	type pendingUpdate struct {
		id     int64
		values []any
	}
	var pending []pendingUpdate
	for rows.Next() {
		var id int64
		nullStrings := make([]sql.NullString, len(columnNames))
		scanArgs := make([]any, len(columnNames)+1)
		scanArgs[0] = &id
		for i := range columnNames {
			scanArgs[i+1] = &nullStrings[i]
		}
		if err := rows.Scan(scanArgs...); err != nil {
			return fmt.Errorf("scan %s: %w", tableName, err)
		}

		values := make([]any, len(columnNames))
		needsUpdate := false
		for i, ns := range nullStrings {
			if !ns.Valid {
				// Leave values[i] as nil to preserve NULL on update.
				continue
			}
			result, changed, err := encryptFieldIfPlaintext(ns.String)
			if err != nil {
				return fmt.Errorf("encrypt %s.%s (id=%d): %w", tableName, columnNames[i], id, err)
			}
			values[i] = result
			if changed {
				needsUpdate = true
			}
		}

		if needsUpdate {
			pending = append(pending, pendingUpdate{id: id, values: values})
		}
	}

	if len(pending) == 0 {
		return nil
	}

	log.Printf("Encrypting %d %s rows...", len(pending), tableName)

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin %s encryption tx: %w", tableName, err)
	}
	for _, p := range pending {
		if err := updateFn(tx, p.id, p.values); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("update %s id=%d: %w", tableName, p.id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit %s encryption: %w", tableName, err)
	}

	return nil
}
