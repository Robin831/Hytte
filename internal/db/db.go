package db

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log"
	"strings"
	"time"

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
	CREATE UNIQUE INDEX IF NOT EXISTS idx_infra_health_services_user_name ON infra_health_services(user_id, name);

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

	CREATE INDEX IF NOT EXISTS idx_user_badges_user_id ON user_badges(user_id);

	-- Family rewards: parent-defined reward catalog (Hytte-jdzp)
	CREATE TABLE IF NOT EXISTS family_rewards (
		id           INTEGER PRIMARY KEY,
		parent_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		title        TEXT NOT NULL DEFAULT '',
		description  TEXT NOT NULL DEFAULT '',
		star_cost    INTEGER NOT NULL DEFAULT 0,
		icon_emoji   TEXT NOT NULL DEFAULT '🎁',
		is_active    INTEGER NOT NULL DEFAULT 1,
		max_claims   INTEGER,
		parent_note  TEXT NOT NULL DEFAULT '',
		created_at   TEXT NOT NULL DEFAULT '',
		updated_at   TEXT NOT NULL DEFAULT ''
	);

	CREATE INDEX IF NOT EXISTS idx_family_rewards_parent ON family_rewards(parent_id);

	-- Reward claims: child requests to redeem a reward (Hytte-jdzp)
	CREATE TABLE IF NOT EXISTS reward_claims (
		id          INTEGER PRIMARY KEY,
		reward_id   INTEGER NOT NULL REFERENCES family_rewards(id) ON DELETE CASCADE,
		child_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		status      TEXT NOT NULL DEFAULT 'pending',
		stars_spent INTEGER NOT NULL DEFAULT 0,
		note        TEXT NOT NULL DEFAULT '',
		resolved_at TEXT,
		created_at  TEXT NOT NULL DEFAULT ''
	);

	CREATE INDEX IF NOT EXISTS idx_reward_claims_reward ON reward_claims(reward_id);
	CREATE INDEX IF NOT EXISTS idx_reward_claims_child ON reward_claims(child_id);
	CREATE INDEX IF NOT EXISTS idx_reward_claims_status ON reward_claims(status);

	-- Streak shields: parent-granted streak protections (Hytte-otrj)
	CREATE TABLE IF NOT EXISTS streak_shields (
		id          INTEGER PRIMARY KEY,
		parent_id   INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		child_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		used_at     TEXT NOT NULL DEFAULT '',
		shield_date TEXT NOT NULL DEFAULT ''
	);

	CREATE INDEX IF NOT EXISTS idx_streak_shields_lookup ON streak_shields(parent_id, child_id, used_at);
	CREATE INDEX IF NOT EXISTS idx_streak_shields_child_date ON streak_shields(child_id, shield_date);

	-- Weekly bonus evaluation idempotency guard (Hytte-z8uu)
	CREATE TABLE IF NOT EXISTS weekly_bonus_evaluations (
		id           INTEGER PRIMARY KEY,
		user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		week_key     TEXT NOT NULL,
		evaluated_at TEXT NOT NULL DEFAULT '',
		UNIQUE(user_id, week_key)
	);

	-- Push notification dedup log (Hytte-17ay): persists sent keys across restarts.
	CREATE TABLE IF NOT EXISTS daemon_notification_sent (
		user_id  INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		key      TEXT NOT NULL,
		sent_at  TEXT NOT NULL,
		PRIMARY KEY (user_id, key)
	);

	-- Family challenges: parent-created challenges for children (Hytte-o9tm)
	CREATE TABLE IF NOT EXISTS family_challenges (
		id             INTEGER PRIMARY KEY,
		creator_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		title          TEXT NOT NULL DEFAULT '',
		description    TEXT NOT NULL DEFAULT '',
		challenge_type TEXT NOT NULL DEFAULT 'custom',
		target_value   REAL NOT NULL DEFAULT 0,
		star_reward    INTEGER NOT NULL DEFAULT 0,
		start_date     TEXT NOT NULL DEFAULT '',
		end_date       TEXT NOT NULL DEFAULT '',
		is_active      INTEGER NOT NULL DEFAULT 1,
		is_system      INTEGER NOT NULL DEFAULT 0,
		created_at     TEXT NOT NULL DEFAULT '',
		updated_at     TEXT NOT NULL DEFAULT ''
	);

	CREATE INDEX IF NOT EXISTS idx_family_challenges_creator ON family_challenges(creator_id);

	-- Challenge participants: children enrolled in a challenge (Hytte-o9tm)
	CREATE TABLE IF NOT EXISTS challenge_participants (
		id           INTEGER PRIMARY KEY,
		challenge_id INTEGER NOT NULL REFERENCES family_challenges(id) ON DELETE CASCADE,
		child_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		added_at     TEXT NOT NULL DEFAULT '',
		completed_at TEXT NOT NULL DEFAULT '',
		UNIQUE(challenge_id, child_id)
	);

	CREATE INDEX IF NOT EXISTS idx_challenge_participants_challenge ON challenge_participants(challenge_id);
	CREATE INDEX IF NOT EXISTS idx_challenge_participants_child ON challenge_participants(child_id);

	-- Story journey: per-user progress through a themed map (Hytte-olyq)
	CREATE TABLE IF NOT EXISTS story_journeys (
		id               INTEGER PRIMARY KEY,
		user_id          INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		theme            TEXT NOT NULL DEFAULT 'middle_earth',
		total_distance_m REAL NOT NULL DEFAULT 0,
		created_at       TEXT NOT NULL DEFAULT '',
		updated_at       TEXT NOT NULL DEFAULT '',
		UNIQUE(user_id)
	);

	-- Star savings account: child piggy bank with 24h withdrawal delay (Hytte-0fda)
	CREATE TABLE IF NOT EXISTS star_savings (
		user_id                 INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
		balance                 INTEGER NOT NULL DEFAULT 0,
		pending_withdrawal      INTEGER NOT NULL DEFAULT 0,
		withdrawal_available_at TEXT NOT NULL DEFAULT '',
		updated_at              TEXT NOT NULL DEFAULT ''
	);

	-- Savings interest payment dedup guard (Hytte-0fda): one row per ISO week prevents double-pay
	CREATE TABLE IF NOT EXISTS savings_interest_payments (
		week_key TEXT NOT NULL PRIMARY KEY,
		paid_at  TEXT NOT NULL DEFAULT ''
	);

	-- Notification deduplication log (Hytte-9kw6): tracks recently sent notifications
	-- to prevent duplicate alerts for the same event within a cooldown window.
	CREATE TABLE IF NOT EXISTS notification_log (
		id         INTEGER PRIMARY KEY,
		user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		notif_type TEXT NOT NULL,
		reference  TEXT NOT NULL DEFAULT '',
		sent_at    TEXT NOT NULL DEFAULT ''
	);

	CREATE INDEX IF NOT EXISTS idx_notification_log_lookup
		ON notification_log(user_id, notif_type, reference, sent_at);

	-- Ensure notification_log does not grow without bound by keeping only the most recent
	-- row per (user_id, notif_type, reference). Older rows for the same logical event
	-- are pruned automatically on insert.
	CREATE TRIGGER IF NOT EXISTS notification_log_prune_duplicates
	AFTER INSERT ON notification_log
	BEGIN
		DELETE FROM notification_log
		WHERE user_id = NEW.user_id
			AND notif_type = NEW.notif_type
			AND reference = NEW.reference
			AND id < NEW.id;
	END;

	-- Workout Bingo: per-user weekly 3x3 bingo cards (Hytte-gt09)
	CREATE TABLE IF NOT EXISTS bingo_cards (
		id              INTEGER PRIMARY KEY,
		user_id         INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		week_key        TEXT NOT NULL,
		cells           TEXT NOT NULL DEFAULT '[]',
		completed_lines TEXT NOT NULL DEFAULT '[]',
		jackpot_awarded INTEGER NOT NULL DEFAULT 0,
		created_at      TEXT NOT NULL DEFAULT '',
		updated_at      TEXT NOT NULL DEFAULT '',
		UNIQUE(user_id, week_key)
	);

	-- Allowance: chore definitions created by parents (Hytte-z0v7)
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

	CREATE INDEX IF NOT EXISTS idx_allowance_chores_parent ON allowance_chores(parent_id);
	CREATE INDEX IF NOT EXISTS idx_allowance_chores_child ON allowance_chores(child_id);

	-- Allowance: chore completions claimed by kids and approved by parents (Hytte-z0v7)
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
		created_at    TEXT NOT NULL DEFAULT '',
		UNIQUE(chore_id, child_id, date)
	);

	CREATE INDEX IF NOT EXISTS idx_allowance_completions_chore ON allowance_completions(chore_id);
	CREATE INDEX IF NOT EXISTS idx_allowance_completions_child ON allowance_completions(child_id, date);

	-- Allowance: one-off extra tasks posted by parents (Hytte-z0v7)
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

	CREATE INDEX IF NOT EXISTS idx_allowance_extras_parent ON allowance_extras(parent_id);
	CREATE INDEX IF NOT EXISTS idx_allowance_extras_status ON allowance_extras(status);

	-- Allowance: configurable bonus rules per parent (Hytte-z0v7)
	CREATE TABLE IF NOT EXISTS allowance_bonus_rules (
		id          INTEGER PRIMARY KEY,
		parent_id   INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		type        TEXT NOT NULL,
		multiplier  REAL NOT NULL DEFAULT 1.0,
		flat_amount REAL NOT NULL DEFAULT 0,
		active      INTEGER NOT NULL DEFAULT 1,
		UNIQUE(parent_id, type)
	);

	CREATE INDEX IF NOT EXISTS idx_allowance_bonus_rules_parent ON allowance_bonus_rules(parent_id);

	-- Allowance: weekly payout summaries per child (Hytte-z0v7)
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

	CREATE INDEX IF NOT EXISTS idx_allowance_payouts_parent ON allowance_payouts(parent_id, week_start);
	CREATE INDEX IF NOT EXISTS idx_allowance_payouts_child ON allowance_payouts(child_id, week_start);

	-- Allowance: per-child configuration (base weekly amount, auto-approve hours) (Hytte-z0v7)
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

	-- Allowance: team completion participants — tracks which children joined a team chore (Hytte-jzbr)
	CREATE TABLE IF NOT EXISTS allowance_team_completions (
		id            INTEGER PRIMARY KEY,
		completion_id INTEGER NOT NULL REFERENCES allowance_completions(id) ON DELETE CASCADE,
		child_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		joined_at     TEXT NOT NULL DEFAULT '',
		UNIQUE(completion_id, child_id)
	);

	CREATE INDEX IF NOT EXISTS idx_allowance_team_completions_child ON allowance_team_completions(child_id);

	-- Allowance: savings goals set for a child (Hytte-g1gs)
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

	CREATE INDEX IF NOT EXISTS idx_allowance_savings_goals_child ON allowance_savings_goals(child_id);
	CREATE INDEX IF NOT EXISTS idx_allowance_savings_goals_parent ON allowance_savings_goals(parent_id);

	-- Allowance bingo: weekly 3x3 bingo cards for children (Hytte-403b)
	CREATE TABLE IF NOT EXISTS allowance_bingo_cards (
		id              INTEGER PRIMARY KEY,
		child_id        INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		parent_id       INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		week_start      TEXT NOT NULL,              -- YYYY-MM-DD (Monday)
		cells           TEXT NOT NULL DEFAULT '[]', -- JSON array of AllowanceBingoCell
		completed_lines INTEGER NOT NULL DEFAULT 0, -- bitmask: bit i set when line i is complete (0-7)
		full_card       INTEGER NOT NULL DEFAULT 0, -- 1 when all 9 cells are complete
		bonus_earned    REAL NOT NULL DEFAULT 0,    -- total NOK awarded for bingo lines + jackpot
		created_at      TEXT NOT NULL DEFAULT '',
		updated_at      TEXT NOT NULL DEFAULT '',
		UNIQUE(child_id, week_start)
	);

	CREATE TABLE IF NOT EXISTS netatmo_oauth_tokens (
		user_id       INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
		access_token  TEXT NOT NULL,
		refresh_token TEXT NOT NULL,
		expiry        TEXT NOT NULL DEFAULT '',
		updated_at    TEXT NOT NULL DEFAULT ''
	);

	CREATE TABLE IF NOT EXISTS netatmo_readings (
		id          INTEGER PRIMARY KEY,
		user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		timestamp   TEXT NOT NULL,
		module_type TEXT NOT NULL,
		metric      TEXT NOT NULL,
		value       REAL NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_netatmo_readings_user_ts ON netatmo_readings(user_id, timestamp);

	-- Work hours: daily time tracking with flex pool calculation.
	CREATE TABLE IF NOT EXISTS work_days (
		id         INTEGER PRIMARY KEY,
		user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		date       TEXT NOT NULL,               -- YYYY-MM-DD
		lunch      INTEGER NOT NULL DEFAULT 0,  -- 1 = deduct lunch_minutes
		notes      TEXT NOT NULL DEFAULT '',    -- encrypted
		created_at TEXT NOT NULL DEFAULT '',
		UNIQUE(user_id, date)                   -- also serves as the (user_id, date) index
	);

	CREATE TABLE IF NOT EXISTS work_deduction_presets (
		id              INTEGER PRIMARY KEY,
		user_id         INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		name            TEXT NOT NULL,          -- encrypted
		default_minutes INTEGER NOT NULL DEFAULT 15,
		icon            TEXT NOT NULL DEFAULT 'clock',
		sort_order      INTEGER NOT NULL DEFAULT 0,
		active          INTEGER NOT NULL DEFAULT 1
	);

	CREATE TABLE IF NOT EXISTS work_sessions (
		id         INTEGER PRIMARY KEY,
		day_id     INTEGER NOT NULL REFERENCES work_days(id) ON DELETE CASCADE,
		start_time TEXT NOT NULL,               -- HH:MM (24h)
		end_time   TEXT NOT NULL,               -- HH:MM (24h)
		sort_order INTEGER NOT NULL DEFAULT 0
	);

	CREATE INDEX IF NOT EXISTS idx_work_sessions_day_id ON work_sessions(day_id);

	CREATE TABLE IF NOT EXISTS work_deductions (
		id        INTEGER PRIMARY KEY,
		day_id    INTEGER NOT NULL REFERENCES work_days(id) ON DELETE CASCADE,
		name      TEXT NOT NULL,                -- encrypted; e.g. "Kindergarten"
		minutes   INTEGER NOT NULL,
		preset_id INTEGER REFERENCES work_deduction_presets(id) ON DELETE SET NULL
	);

	CREATE TABLE IF NOT EXISTS work_leave_days (
		id         INTEGER PRIMARY KEY,
		user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		date       TEXT NOT NULL,              -- YYYY-MM-DD
		leave_type TEXT NOT NULL,              -- 'vacation', 'sick', 'personal', 'public_holiday'
		note       TEXT NOT NULL DEFAULT '',   -- encrypted
		created_at TEXT NOT NULL DEFAULT '',
		UNIQUE(user_id, date)
	);

	-- Persists an in-progress punch-in so state survives page reloads.
	-- At most one open session per user (UNIQUE on user_id).
	CREATE TABLE IF NOT EXISTS work_open_sessions (
		id         INTEGER PRIMARY KEY,
		user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		date       TEXT NOT NULL,        -- YYYY-MM-DD (date the punch-in started)
		start_time TEXT NOT NULL,        -- HH:MM (24h)
		punched_at TEXT NOT NULL,        -- RFC3339 timestamp
		UNIQUE(user_id)
	);

	-- AI prompt templates: editable defaults used by Claude analysis features (Hytte-434x).
	-- prompt_key is the logical identifier ('analysis', 'comparison', 'training_load').
	-- prompt_body stores the instruction text injected into the Claude prompt.
	-- INSERT OR IGNORE on seeding ensures user-customized rows are never overwritten.
	CREATE TABLE IF NOT EXISTS ai_prompts (
		id          INTEGER PRIMARY KEY,
		prompt_key  TEXT NOT NULL UNIQUE,
		prompt_body TEXT NOT NULL DEFAULT '',
		created_at  TEXT NOT NULL DEFAULT '',
		updated_at  TEXT NOT NULL DEFAULT ''
	);

	-- Kiosk tokens: long-lived API tokens for unauthenticated kiosk/display clients (Hytte-1mw8).
	-- token_hash is SHA-256 of the raw bearer token (never stored in plaintext).
	-- name is a human-readable label for the token.
	-- config is a JSON blob of kiosk-specific settings (e.g. which widgets to show).
	-- created_by stores the email/ID of the admin who created the token.
	CREATE TABLE IF NOT EXISTS kiosk_tokens (
		id           INTEGER PRIMARY KEY,
		token_hash   TEXT NOT NULL UNIQUE,
		name         TEXT NOT NULL DEFAULT '',
		config       TEXT NOT NULL DEFAULT '{}',
		created_by   TEXT NOT NULL DEFAULT '',
		created_at   TEXT NOT NULL DEFAULT '',
		expires_at   TEXT,
		last_used_at TEXT
	);

	-- Wordfeud local game tracking: multiplayer score tracking + persistence (Hytte-06rd).
	-- Each game stores two player names, running scores, turn indicator, and
	-- a JSON-encoded 15x15 board state.
	CREATE TABLE IF NOT EXISTS wordfeud_games (
		id          INTEGER PRIMARY KEY,
		user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		player1     TEXT NOT NULL DEFAULT '',
		player2     TEXT NOT NULL DEFAULT '',
		score1      INTEGER NOT NULL DEFAULT 0,
		score2      INTEGER NOT NULL DEFAULT 0,
		current_turn INTEGER NOT NULL DEFAULT 1,
		board_json  TEXT NOT NULL DEFAULT '[]',
		rack1       TEXT NOT NULL DEFAULT '',
		rack2       TEXT NOT NULL DEFAULT '',
		status      TEXT NOT NULL DEFAULT 'active',
		created_at  TEXT NOT NULL DEFAULT '',
		updated_at  TEXT NOT NULL DEFAULT ''
	);
	CREATE INDEX IF NOT EXISTS idx_wordfeud_games_user_id ON wordfeud_games(user_id);

	-- Move history for wordfeud games, enabling undo/redo.
	-- board_before stores the full board state before this move was applied.
	CREATE TABLE IF NOT EXISTS wordfeud_moves (
		id            INTEGER PRIMARY KEY,
		game_id       INTEGER NOT NULL REFERENCES wordfeud_games(id) ON DELETE CASCADE,
		move_number   INTEGER NOT NULL,
		player_turn   INTEGER NOT NULL,
		word          TEXT NOT NULL DEFAULT '',
		position      TEXT NOT NULL DEFAULT '',
		direction     TEXT NOT NULL DEFAULT '',
		score         INTEGER NOT NULL DEFAULT 0,
		move_type     TEXT NOT NULL DEFAULT 'move',
		board_before  TEXT NOT NULL DEFAULT '[]',
		score1_before INTEGER NOT NULL DEFAULT 0,
		score2_before INTEGER NOT NULL DEFAULT 0,
		rack1_before  TEXT NOT NULL DEFAULT '',
		rack2_before  TEXT NOT NULL DEFAULT '',
		created_at    TEXT NOT NULL DEFAULT ''
	);
	CREATE UNIQUE INDEX IF NOT EXISTS idx_wordfeud_moves_game_id_move_num ON wordfeud_moves(game_id, move_number);

	-- Budget: financial accounts owned by a user (Hytte-jas0)
	-- UNIQUE(user_id, id) allows composite FK references from child tables to
	-- enforce that transactions/recurring rules belong to the same user's account.
	CREATE TABLE IF NOT EXISTS budget_accounts (
		id         INTEGER PRIMARY KEY,
		user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		name       TEXT NOT NULL DEFAULT '',
		type       TEXT NOT NULL DEFAULT 'checking',
		currency   TEXT NOT NULL DEFAULT 'NOK',
		balance    REAL NOT NULL DEFAULT 0,
		icon       TEXT NOT NULL DEFAULT '',
		UNIQUE(user_id, id)
	);

	CREATE INDEX IF NOT EXISTS idx_budget_accounts_user_id ON budget_accounts(user_id);

	-- Budget: transaction categories (Hytte-jas0)
	-- UNIQUE(user_id, id) allows composite FK references to enforce category ownership.
	CREATE TABLE IF NOT EXISTS budget_categories (
		id         INTEGER PRIMARY KEY,
		user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		name       TEXT NOT NULL DEFAULT '',
		group_name TEXT NOT NULL DEFAULT '',
		icon       TEXT NOT NULL DEFAULT '',
		color      TEXT NOT NULL DEFAULT '',
		is_income  INTEGER NOT NULL DEFAULT 0,
		UNIQUE(user_id, id)
	);

	CREATE INDEX IF NOT EXISTS idx_budget_categories_user_id ON budget_categories(user_id);

	-- Budget: individual transactions (Hytte-jas0)
	-- Composite FKs on (user_id, account_id) and (user_id, category_id) ensure
	-- that referenced accounts and categories belong to the same user.
	-- date is required (TEXT NOT NULL, no default); end_date/last_generated are optional (TEXT nullable).
	CREATE TABLE IF NOT EXISTS budget_transactions (
		id             INTEGER PRIMARY KEY,
		user_id        INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		account_id     INTEGER NOT NULL,
		category_id    INTEGER,
		amount         REAL NOT NULL DEFAULT 0,
		description    TEXT NOT NULL DEFAULT '',
		date           TEXT NOT NULL,
		tags           TEXT NOT NULL DEFAULT '[]',
		is_transfer    INTEGER NOT NULL DEFAULT 0,
		transfer_to_id INTEGER,
		FOREIGN KEY (user_id, account_id)    REFERENCES budget_accounts(user_id, id)    ON DELETE CASCADE,
		FOREIGN KEY (user_id, category_id)   REFERENCES budget_categories(user_id, id)  ON DELETE SET NULL,
		FOREIGN KEY (user_id, transfer_to_id) REFERENCES budget_accounts(user_id, id)   ON DELETE SET NULL
	);

	CREATE INDEX IF NOT EXISTS idx_budget_transactions_user_id ON budget_transactions(user_id);
	CREATE INDEX IF NOT EXISTS idx_budget_transactions_account_id ON budget_transactions(account_id);
	CREATE INDEX IF NOT EXISTS idx_budget_transactions_date ON budget_transactions(date);

	-- Budget: recurring transaction rules (Hytte-jas0)
	-- start_date is required (TEXT NOT NULL); end_date and last_generated are optional (TEXT nullable).
	CREATE TABLE IF NOT EXISTS budget_recurring (
		id             INTEGER PRIMARY KEY,
		user_id        INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		account_id     INTEGER NOT NULL,
		category_id    INTEGER,
		amount         REAL NOT NULL DEFAULT 0,
		description    TEXT NOT NULL DEFAULT '',
		frequency      TEXT NOT NULL DEFAULT 'monthly',
		day_of_month   INTEGER NOT NULL DEFAULT 1,
		start_date     TEXT NOT NULL,
		end_date       TEXT,
		last_generated TEXT,
		active         INTEGER NOT NULL DEFAULT 1,
		FOREIGN KEY (user_id, account_id)  REFERENCES budget_accounts(user_id, id)   ON DELETE CASCADE,
		FOREIGN KEY (user_id, category_id) REFERENCES budget_categories(user_id, id) ON DELETE SET NULL
	);

	CREATE INDEX IF NOT EXISTS idx_budget_recurring_user_id ON budget_recurring(user_id);

	-- Budget: per-category spending limits (Hytte-mr7t)
	-- effective_from is YYYY-MM-DD (first day of month). Multiple limits per
	-- category are supported to allow changing the budget over time; queries
	-- pick the latest limit whose effective_from <= the requested month.
	CREATE TABLE IF NOT EXISTS budget_limits (
		id             INTEGER PRIMARY KEY,
		user_id        INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		category_id    INTEGER NOT NULL,
		amount         REAL NOT NULL DEFAULT 0,
		period         TEXT NOT NULL DEFAULT 'monthly',
		effective_from TEXT NOT NULL CHECK (
		length(effective_from) = 10 AND
		effective_from GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-01'
	),
		UNIQUE(user_id, category_id, effective_from),
		FOREIGN KEY (user_id, category_id) REFERENCES budget_categories(user_id, id) ON DELETE CASCADE
	);

	-- Budget: loans and mortgages (Hytte-am9i)
	CREATE TABLE IF NOT EXISTS budget_loans (
		id               INTEGER PRIMARY KEY,
		user_id          INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		name             TEXT NOT NULL DEFAULT '',
		principal        REAL NOT NULL DEFAULT 0,
		current_balance  REAL NOT NULL DEFAULT 0,
		annual_rate      REAL NOT NULL DEFAULT 0,
		monthly_payment  REAL NOT NULL DEFAULT 0,
		start_date       TEXT NOT NULL,
		term_months      INTEGER NOT NULL DEFAULT 0,
		property_value   REAL NOT NULL DEFAULT 0,
		property_name    TEXT NOT NULL DEFAULT '',
		notes            TEXT NOT NULL DEFAULT ''
	);

	CREATE INDEX IF NOT EXISTS idx_budget_loans_user_id ON budget_loans(user_id);

	`

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
			return fmt.Errorf("add badge_definitions tier column: %w", err)
		}
	}

	// Add completed_at column to challenge_participants (Hytte-rrpq).
	// Empty string means the participant has not yet completed the challenge.
	var hasCompletedAt int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('challenge_participants') WHERE name = 'completed_at'`).Scan(&hasCompletedAt); err != nil {
		return fmt.Errorf("check challenge_participants completed_at column: %w", err)
	}
	if hasCompletedAt == 0 {
		if _, err := db.Exec(`ALTER TABLE challenge_participants ADD COLUMN completed_at TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("add challenge_participants completed_at column: %w", err)
		}
	}

	// Add is_system column to family_challenges for system-generated weekly challenges (Hytte-cpn4).
	var hasIsSystem int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('family_challenges') WHERE name = 'is_system'`).Scan(&hasIsSystem); err != nil {
		return fmt.Errorf("check family_challenges is_system column: %w", err)
	}
	if hasIsSystem == 0 {
		if _, err := db.Exec(`ALTER TABLE family_challenges ADD COLUMN is_system INTEGER NOT NULL DEFAULT 0`); err != nil {
			return fmt.Errorf("add family_challenges is_system column: %w", err)
		}
	}

	// Ensure the system user (id=0) exists. It acts as the creator for system-generated
	// weekly challenges (Hytte-cpn4). The system user is never a real login account.
	if _, err := db.Exec(`
		INSERT OR IGNORE INTO users (id, email, name, picture, google_id)
		VALUES (0, 'system@hytte.internal', 'System', '', 'system')
	`); err != nil {
		return fmt.Errorf("insert system user: %w", err)
	}

	// Add quality_bonus column to allowance_completions (Hytte-nuqd).
	var hasQualityBonus int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('allowance_completions') WHERE name = 'quality_bonus'`).Scan(&hasQualityBonus); err != nil {
		return fmt.Errorf("check quality_bonus column: %w", err)
	}
	if hasQualityBonus == 0 {
		if _, err := db.Exec(`ALTER TABLE allowance_completions ADD COLUMN quality_bonus REAL NOT NULL DEFAULT 0`); err != nil {
			return fmt.Errorf("add allowance_completions quality_bonus column: %w", err)
		}
	}

	// Add team chore columns to allowance_chores (Hytte-jzbr).
	for _, col := range []struct {
		name string
		ddl  string
	}{
		{"completion_mode", `ALTER TABLE allowance_chores ADD COLUMN completion_mode TEXT NOT NULL DEFAULT 'solo'`},
		{"min_team_size", `ALTER TABLE allowance_chores ADD COLUMN min_team_size INTEGER NOT NULL DEFAULT 2`},
		{"team_bonus_pct", `ALTER TABLE allowance_chores ADD COLUMN team_bonus_pct REAL NOT NULL DEFAULT 10.0`},
	} {
		var has int
		if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('allowance_chores') WHERE name = ?`, col.name).Scan(&has); err != nil {
			return fmt.Errorf("check allowance_chores.%s column: %w", col.name, err)
		}
		if has == 0 {
			if _, err := db.Exec(col.ddl); err != nil {
				return fmt.Errorf("add allowance_chores.%s column: %w", col.name, err)
			}
		}
	}

	// Add photo_path column to allowance_completions (Hytte-jedh).
	var hasPhotoPath int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('allowance_completions') WHERE name = 'photo_path'`).Scan(&hasPhotoPath); err != nil {
		return fmt.Errorf("check allowance_completions photo_path column: %w", err)
	}
	if hasPhotoPath == 0 {
		if _, err := db.Exec(`ALTER TABLE allowance_completions ADD COLUMN photo_path TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("add allowance_completions photo_path column: %w", err)
		}
	}

	// Add description column to budget_recurring (Hytte-mro9).
	var hasRecurringDesc int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('budget_recurring') WHERE name = 'description'`).Scan(&hasRecurringDesc); err != nil {
		return fmt.Errorf("check budget_recurring description column: %w", err)
	}
	if hasRecurringDesc == 0 {
		if _, err := db.Exec(`ALTER TABLE budget_recurring ADD COLUMN description TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("add budget_recurring description column: %w", err)
		}
	}

	// Add active column to budget_recurring (Hytte-mro9).
	var hasRecurringActive int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('budget_recurring') WHERE name = 'active'`).Scan(&hasRecurringActive); err != nil {
		return fmt.Errorf("check budget_recurring active column: %w", err)
	}
	if hasRecurringActive == 0 {
		if _, err := db.Exec(`ALTER TABLE budget_recurring ADD COLUMN active INTEGER NOT NULL DEFAULT 1`); err != nil {
			return fmt.Errorf("add budget_recurring active column: %w", err)
		}
	}

	// Seed default AI prompt templates (Hytte-434x).
	if err := seedDefaultAIPrompts(db); err != nil {
		return fmt.Errorf("seed ai prompts: %w", err)
	}

	return nil
}

// seedDefaultAIPrompts inserts the built-in prompt instruction strings for the four
// Claude analysis features. INSERT OR IGNORE ensures that any user-customized rows
// already in the table are left untouched. All inserts are wrapped in a single
// transaction so a partial failure leaves the table unchanged.
func seedDefaultAIPrompts(db *sql.DB) error {
	now := time.Now().UTC().Format(time.RFC3339)
	// Seed with empty body — the prompt_body column stores only user-added additional
	// context; the hardcoded system prompts live in settings.DefaultPromptBodies and are
	// never stored in the DB. INSERT OR IGNORE so existing custom context is preserved.
	defaults := []string{"analysis", "comparison", "training_load", "insights"}
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin seed ai_prompts tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck
	for _, key := range defaults {
		if _, err := tx.Exec(
			`INSERT OR IGNORE INTO ai_prompts (prompt_key, prompt_body, created_at, updated_at) VALUES (?, ?, ?, ?)`,
			key, "", now, now,
		); err != nil {
			return fmt.Errorf("seed ai_prompt %q: %w", key, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit seed ai_prompts tx: %w", err)
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
