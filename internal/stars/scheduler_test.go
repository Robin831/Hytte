package stars

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// setupSchedulerTestDB creates an in-memory SQLite database with all tables
// needed by the scheduler functions.
func setupSchedulerTestDB(t *testing.T) *sql.DB {
	t.Helper()
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
		avatar_emoji TEXT NOT NULL DEFAULT '',
		created_at   TEXT NOT NULL DEFAULT '',
		UNIQUE(parent_id, child_id),
		UNIQUE(child_id)
	);
	CREATE TABLE IF NOT EXISTS workouts (
		id                  INTEGER PRIMARY KEY,
		user_id             INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		sport               TEXT NOT NULL DEFAULT 'other',
		started_at          TEXT NOT NULL DEFAULT '',
		duration_seconds    INTEGER NOT NULL DEFAULT 0,
		distance_meters     REAL NOT NULL DEFAULT 0,
		avg_heart_rate      INTEGER NOT NULL DEFAULT 0,
		max_heart_rate      INTEGER NOT NULL DEFAULT 0,
		avg_pace_sec_per_km REAL NOT NULL DEFAULT 0,
		calories            INTEGER NOT NULL DEFAULT 0,
		ascent_meters       REAL NOT NULL DEFAULT 0,
		fit_file_hash       TEXT NOT NULL DEFAULT '',
		created_at          TEXT NOT NULL DEFAULT ''
	);
	CREATE TABLE IF NOT EXISTS star_transactions (
		id           INTEGER PRIMARY KEY,
		user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		amount       INTEGER NOT NULL,
		reason       TEXT NOT NULL,
		description  TEXT NOT NULL DEFAULT '',
		reference_id INTEGER,
		created_at   TEXT NOT NULL DEFAULT ''
	);
	CREATE TABLE IF NOT EXISTS streaks (
		user_id       INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		streak_type   TEXT NOT NULL,
		current_count INTEGER NOT NULL DEFAULT 0,
		longest_count INTEGER NOT NULL DEFAULT 0,
		last_activity TEXT NOT NULL DEFAULT '',
		PRIMARY KEY (user_id, streak_type)
	);
	CREATE TABLE IF NOT EXISTS user_preferences (
		user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		key     TEXT NOT NULL,
		value   TEXT NOT NULL DEFAULT '',
		PRIMARY KEY (user_id, key)
	);
	CREATE TABLE IF NOT EXISTS daemon_notification_sent (
		user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		key     TEXT NOT NULL,
		sent_at TEXT NOT NULL,
		PRIMARY KEY (user_id, key)
	);
	CREATE TABLE IF NOT EXISTS family_rewards (
		id           INTEGER PRIMARY KEY,
		parent_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		title        TEXT NOT NULL DEFAULT '',
		description  TEXT NOT NULL DEFAULT '',
		star_cost    INTEGER NOT NULL DEFAULT 0,
		icon_emoji   TEXT NOT NULL DEFAULT '',
		is_active    INTEGER NOT NULL DEFAULT 1,
		max_claims   INTEGER,
		parent_note  TEXT NOT NULL DEFAULT '',
		created_at   TEXT NOT NULL DEFAULT '',
		updated_at   TEXT NOT NULL DEFAULT ''
	);
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
	CREATE TABLE IF NOT EXISTS challenge_participants (
		id           INTEGER PRIMARY KEY,
		challenge_id INTEGER NOT NULL REFERENCES family_challenges(id) ON DELETE CASCADE,
		child_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		added_at     TEXT NOT NULL DEFAULT '',
		completed_at TEXT NOT NULL DEFAULT '',
		UNIQUE(challenge_id, child_id)
	);`

	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	return db
}

func insertSchedulerUser(t *testing.T, db *sql.DB, email string) int64 {
	t.Helper()
	res, err := db.Exec(
		`INSERT INTO users (email, name, picture, google_id) VALUES (?, 'Test', '', ?)`,
		email, email,
	)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	id, err2 := res.LastInsertId()
	if err2 != nil {
		t.Fatalf("insert user LastInsertId: %v", err2)
	}
	return id
}

// --- schedulerISOWeekMonday ---

func TestSchedulerISOWeekMonday(t *testing.T) {
	tests := []struct {
		name string
		year int
		week int
		want time.Time
	}{
		{
			name: "2025-W11 starts on 2025-03-10",
			year: 2025, week: 11,
			want: time.Date(2025, 3, 10, 0, 0, 0, 0, time.UTC),
		},
		{
			// 2026-01-04 is a Sunday; ISO week 1 of 2026 starts on 2025-12-29.
			name: "2026-W01 starts on 2025-12-29 (Jan 4 is Sunday)",
			year: 2026, week: 1,
			want: time.Date(2025, 12, 29, 0, 0, 0, 0, time.UTC),
		},
		{
			name: "2026-W02 starts on 2026-01-05",
			year: 2026, week: 2,
			want: time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := schedulerISOWeekMonday(tt.year, tt.week)
			if !got.Equal(tt.want) {
				t.Errorf("schedulerISOWeekMonday(%d, %d) = %v, want %v", tt.year, tt.week, got, tt.want)
			}
		})
	}
}

// --- schedulerShouldFireStreakWarning ---

func TestSchedulerShouldFireStreakWarning(t *testing.T) {
	utc := time.UTC
	// 2025-03-10 19:00 UTC
	base := time.Date(2025, 3, 10, 19, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		loc      *time.Location
		lastSent string
		now      time.Time
		wantFire bool
		wantKey  string
	}{
		{
			name: "fires at 19:xx UTC",
			loc: utc, lastSent: "", now: base,
			wantFire: true, wantKey: "2025-03-10",
		},
		{
			name: "already sent today",
			loc: utc, lastSent: "2025-03-10", now: base,
			wantFire: false,
		},
		{
			name: "18:xx does not fire",
			loc: utc, lastSent: "", now: base.Add(-1 * time.Hour),
			wantFire: false,
		},
		{
			name: "20:xx does not fire",
			loc: utc, lastSent: "", now: base.Add(1 * time.Hour),
			wantFire: false,
		},
		{
			name: "fires again on new day",
			loc: utc, lastSent: "2025-03-09", now: base,
			wantFire: true, wantKey: "2025-03-10",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotFire, gotKey := schedulerShouldFireStreakWarning(tt.loc, tt.lastSent, tt.now)
			if gotFire != tt.wantFire {
				t.Errorf("fire = %v, want %v", gotFire, tt.wantFire)
			}
			if tt.wantFire && gotKey != tt.wantKey {
				t.Errorf("key = %q, want %q", gotKey, tt.wantKey)
			}
		})
	}
}

// --- schedulerShouldFireWeeklySummary ---

func TestSchedulerShouldFireWeeklySummary(t *testing.T) {
	utc := time.UTC
	// 2025-03-10 is a Monday, 08:00 UTC
	monday8 := time.Date(2025, 3, 10, 8, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		loc      *time.Location
		lastSent string
		now      time.Time
		wantFire bool
		wantKey  string
	}{
		{
			name: "Monday 08:xx fires",
			loc: utc, lastSent: "", now: monday8,
			wantFire: true, wantKey: "2025-W11",
		},
		{
			name: "already sent this week",
			loc: utc, lastSent: "2025-W11", now: monday8,
			wantFire: false,
		},
		{
			name: "Monday 09:xx does not fire",
			loc: utc, lastSent: "", now: monday8.Add(time.Hour),
			wantFire: false,
		},
		{
			name: "Tuesday 08:xx does not fire",
			loc: utc, lastSent: "", now: monday8.AddDate(0, 0, 1),
			wantFire: false,
		},
		{
			name: "fires again in a new week",
			loc: utc, lastSent: "2025-W10", now: monday8,
			wantFire: true, wantKey: "2025-W11",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotFire, gotKey := schedulerShouldFireWeeklySummary(tt.loc, tt.lastSent, tt.now)
			if gotFire != tt.wantFire {
				t.Errorf("fire = %v, want %v", gotFire, tt.wantFire)
			}
			if tt.wantFire && gotKey != tt.wantKey {
				t.Errorf("key = %q, want %q", gotKey, tt.wantKey)
			}
		})
	}
}

// --- maybeWarnStreakAtRisk (CheckStreakWarnings) integration tests ---

// lastMonday8 returns the most recent past Monday at 08:00 UTC (or today if today is Monday).
func lastMonday8() time.Time {
	now := time.Now().UTC()
	daysSince := (int(now.Weekday()) - int(time.Monday) + 7) % 7
	return now.Truncate(24 * time.Hour).AddDate(0, 0, -daysSince).Add(8 * time.Hour)
}

func TestMaybeWarnStreakAtRisk_SendsWhenAtRisk(t *testing.T) {
	db := setupSchedulerTestDB(t)
	ctx := context.Background()

	userID := insertSchedulerUser(t, db, "user@example.com")

	yesterday := time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")
	if _, err := db.Exec(`
		INSERT INTO streaks (user_id, streak_type, current_count, last_activity)
		VALUES (?, 'daily_workout', 5, ?)`, userID, yesterday); err != nil {
		t.Fatalf("insert streak: %v", err)
	}

	// Simulate 19:00 UTC today.
	now := time.Now().UTC().Truncate(24 * time.Hour).Add(19 * time.Hour)

	var delivered []int64
	deliver := func(uid int64, _ []byte) { delivered = append(delivered, uid) }

	maybeWarnStreakAtRisk(ctx, db, deliver, userID, now)

	if len(delivered) != 1 || delivered[0] != userID {
		t.Errorf("expected delivery for user %d, got %v", userID, delivered)
	}
}

func TestMaybeWarnStreakAtRisk_NoSendWhenAlreadyWorkedOut(t *testing.T) {
	db := setupSchedulerTestDB(t)
	ctx := context.Background()

	userID := insertSchedulerUser(t, db, "safe@example.com")

	today := time.Now().UTC().Format("2006-01-02")
	if _, err := db.Exec(`
		INSERT INTO streaks (user_id, streak_type, current_count, last_activity)
		VALUES (?, 'daily_workout', 3, ?)`, userID, today); err != nil {
		t.Fatalf("insert streak: %v", err)
	}

	now := time.Now().UTC().Truncate(24 * time.Hour).Add(19 * time.Hour)
	var delivered []int64
	deliver := func(uid int64, _ []byte) { delivered = append(delivered, uid) }

	maybeWarnStreakAtRisk(ctx, db, deliver, userID, now)

	if len(delivered) != 0 {
		t.Errorf("expected no delivery when workout logged today, got %v", delivered)
	}
}

func TestMaybeWarnStreakAtRisk_DedupPreventsDoubleSend(t *testing.T) {
	db := setupSchedulerTestDB(t)
	ctx := context.Background()

	userID := insertSchedulerUser(t, db, "dedup@example.com")

	yesterday := time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")
	if _, err := db.Exec(`
		INSERT INTO streaks (user_id, streak_type, current_count, last_activity)
		VALUES (?, 'daily_workout', 2, ?)`, userID, yesterday); err != nil {
		t.Fatalf("insert streak: %v", err)
	}

	now := time.Now().UTC().Truncate(24 * time.Hour).Add(19 * time.Hour)
	todayKey := now.UTC().Format("2006-01-02")

	if _, err := db.Exec(
		`INSERT INTO daemon_notification_sent (user_id, key, sent_at) VALUES (?, ?, ?)`,
		userID, "streak:"+todayKey, time.Now().UTC().Format(time.RFC3339),
	); err != nil {
		t.Fatalf("insert dedup: %v", err)
	}

	var delivered []int64
	deliver := func(uid int64, _ []byte) { delivered = append(delivered, uid) }

	maybeWarnStreakAtRisk(ctx, db, deliver, userID, now)

	if len(delivered) != 0 {
		t.Errorf("expected no duplicate delivery, got %v", delivered)
	}
}

// --- maybeSendWeeklySummary (SendWeeklySummaries) integration tests ---

func TestMaybeSendWeeklySummary_SendsOnMonday(t *testing.T) {
	db := setupSchedulerTestDB(t)
	ctx := context.Background()

	parentID := insertSchedulerUser(t, db, "parent@example.com")
	childID := insertSchedulerUser(t, db, "child@example.com")

	if _, err := db.Exec(`
		INSERT INTO family_links (parent_id, child_id, nickname) VALUES (?, ?, 'Kid')`,
		parentID, childID); err != nil {
		t.Fatalf("insert family_link: %v", err)
	}

	monday8 := lastMonday8()

	var delivered []int64
	deliver := func(uid int64, _ []byte) { delivered = append(delivered, uid) }

	maybeSendWeeklySummary(ctx, db, deliver, parentID, monday8)

	if len(delivered) != 1 || delivered[0] != parentID {
		t.Errorf("expected delivery to parent %d on Monday, got %v", parentID, delivered)
	}
}

// --- CheckChallengeExpiry integration tests ---

// insertChallengeWithParticipant creates a family_challenge with a single
// participant and returns the challenge ID. endDate must be in "2006-01-02" form.
func insertChallengeWithParticipant(t *testing.T, db *sql.DB, creatorID, childID int64, endDate string) int64 {
	t.Helper()
	res, err := db.Exec(`
		INSERT INTO family_challenges (creator_id, title, end_date, is_active, created_at, updated_at)
		VALUES (?, 'Test Challenge', ?, 1, '', '')`, creatorID, endDate)
	if err != nil {
		t.Fatalf("insert challenge: %v", err)
	}
	challengeID, err2 := res.LastInsertId()
	if err2 != nil {
		t.Fatalf("insert challenge LastInsertId: %v", err2)
	}
	if _, err := db.Exec(`
		INSERT INTO challenge_participants (challenge_id, child_id, added_at, completed_at)
		VALUES (?, ?, '', '')`, challengeID, childID); err != nil {
		t.Fatalf("insert participant: %v", err)
	}
	return challengeID
}

func TestMaybeSendChallengeExpiryReminder_Sends2dWarning(t *testing.T) {
	db := setupSchedulerTestDB(t)
	ctx := context.Background()

	parentID := insertSchedulerUser(t, db, "parent-ce@example.com")
	childID := insertSchedulerUser(t, db, "child-ce@example.com")
	if _, err := db.Exec(`INSERT INTO family_links (parent_id, child_id, nickname) VALUES (?, ?, 'Kid')`, parentID, childID); err != nil {
		t.Fatalf("insert family_link: %v", err)
	}

	// Challenge expires in 2 days from now (UTC).
	now := time.Now().UTC().Truncate(24 * time.Hour).Add(10 * time.Hour)
	twoDaysLater := now.AddDate(0, 0, 2).Format("2006-01-02")
	insertChallengeWithParticipant(t, db, parentID, childID, twoDaysLater)

	var delivered []int64
	deliver := func(uid int64, _ []byte) { delivered = append(delivered, uid) }

	maybeSendChallengeExpiryReminder(ctx, db, deliver, childID, now)

	if len(delivered) != 1 || delivered[0] != childID {
		t.Errorf("expected delivery to child %d for 2d warning, got %v", childID, delivered)
	}
}

func TestMaybeSendChallengeExpiryReminder_Sends1dWarning(t *testing.T) {
	db := setupSchedulerTestDB(t)
	ctx := context.Background()

	parentID := insertSchedulerUser(t, db, "parent-ce1d@example.com")
	childID := insertSchedulerUser(t, db, "child-ce1d@example.com")
	if _, err := db.Exec(`INSERT INTO family_links (parent_id, child_id, nickname) VALUES (?, ?, 'Kid')`, parentID, childID); err != nil {
		t.Fatalf("insert family_link: %v", err)
	}

	// Challenge expires tomorrow (1 day from now).
	now := time.Now().UTC().Truncate(24 * time.Hour).Add(10 * time.Hour)
	oneDayLater := now.AddDate(0, 0, 1).Format("2006-01-02")
	insertChallengeWithParticipant(t, db, parentID, childID, oneDayLater)

	var delivered []int64
	deliver := func(uid int64, _ []byte) { delivered = append(delivered, uid) }

	maybeSendChallengeExpiryReminder(ctx, db, deliver, childID, now)

	if len(delivered) != 1 || delivered[0] != childID {
		t.Errorf("expected delivery to child %d for 1d warning, got %v", childID, delivered)
	}
}

func TestMaybeSendChallengeExpiryReminder_NoSendOutsideHour(t *testing.T) {
	db := setupSchedulerTestDB(t)
	ctx := context.Background()

	parentID := insertSchedulerUser(t, db, "parent-ceh@example.com")
	childID := insertSchedulerUser(t, db, "child-ceh@example.com")
	if _, err := db.Exec(`INSERT INTO family_links (parent_id, child_id, nickname) VALUES (?, ?, 'Kid')`, parentID, childID); err != nil {
		t.Fatalf("insert family_link: %v", err)
	}

	// Not the 10:xx hour.
	now := time.Now().UTC().Truncate(24 * time.Hour).Add(9 * time.Hour)
	twoDaysLater := now.AddDate(0, 0, 2).Format("2006-01-02")
	insertChallengeWithParticipant(t, db, parentID, childID, twoDaysLater)

	var delivered []int64
	deliver := func(uid int64, _ []byte) { delivered = append(delivered, uid) }

	maybeSendChallengeExpiryReminder(ctx, db, deliver, childID, now)

	if len(delivered) != 0 {
		t.Errorf("expected no delivery outside 10:xx hour, got %v", delivered)
	}
}

func TestMaybeSendChallengeExpiryReminder_DedupPreventsDoubleSend(t *testing.T) {
	db := setupSchedulerTestDB(t)
	ctx := context.Background()

	parentID := insertSchedulerUser(t, db, "parent-cedup@example.com")
	childID := insertSchedulerUser(t, db, "child-cedup@example.com")
	if _, err := db.Exec(`INSERT INTO family_links (parent_id, child_id, nickname) VALUES (?, ?, 'Kid')`, parentID, childID); err != nil {
		t.Fatalf("insert family_link: %v", err)
	}

	now := time.Now().UTC().Truncate(24 * time.Hour).Add(10 * time.Hour)
	twoDaysLater := now.AddDate(0, 0, 2).Format("2006-01-02")
	challengeID := insertChallengeWithParticipant(t, db, parentID, childID, twoDaysLater)

	// Pre-insert the dedup record to simulate a previous send.
	dedupKey := fmt.Sprintf("challenge_expiry:%d-2d", challengeID)
	if _, err := db.Exec(
		`INSERT INTO daemon_notification_sent (user_id, key, sent_at) VALUES (?, ?, ?)`,
		childID, dedupKey, time.Now().UTC().Format(time.RFC3339),
	); err != nil {
		t.Fatalf("insert dedup: %v", err)
	}

	var delivered []int64
	deliver := func(uid int64, _ []byte) { delivered = append(delivered, uid) }

	maybeSendChallengeExpiryReminder(ctx, db, deliver, childID, now)

	if len(delivered) != 0 {
		t.Errorf("expected no duplicate delivery, got %v", delivered)
	}
}

func TestMaybeSendChallengeExpiryReminder_NoSendForCompletedChallenge(t *testing.T) {
	db := setupSchedulerTestDB(t)
	ctx := context.Background()

	parentID := insertSchedulerUser(t, db, "parent-cecomp@example.com")
	childID := insertSchedulerUser(t, db, "child-cecomp@example.com")
	if _, err := db.Exec(`INSERT INTO family_links (parent_id, child_id, nickname) VALUES (?, ?, 'Kid')`, parentID, childID); err != nil {
		t.Fatalf("insert family_link: %v", err)
	}

	now := time.Now().UTC().Truncate(24 * time.Hour).Add(10 * time.Hour)
	twoDaysLater := now.AddDate(0, 0, 2).Format("2006-01-02")

	// Insert challenge with completed_at set — should be excluded.
	res, err := db.Exec(`
		INSERT INTO family_challenges (creator_id, title, end_date, is_active, created_at, updated_at)
		VALUES (?, 'Done Challenge', ?, 1, '', '')`, parentID, twoDaysLater)
	if err != nil {
		t.Fatalf("insert challenge: %v", err)
	}
	challengeID, err2 := res.LastInsertId()
	if err2 != nil {
		t.Fatalf("insert challenge LastInsertId: %v", err2)
	}
	if _, err := db.Exec(`
		INSERT INTO challenge_participants (challenge_id, child_id, added_at, completed_at)
		VALUES (?, ?, '', '2026-01-01T00:00:00Z')`, challengeID, childID); err != nil {
		t.Fatalf("insert participant: %v", err)
	}

	var delivered []int64
	deliver := func(uid int64, _ []byte) { delivered = append(delivered, uid) }

	maybeSendChallengeExpiryReminder(ctx, db, deliver, childID, now)

	if len(delivered) != 0 {
		t.Errorf("expected no delivery for completed challenge, got %v", delivered)
	}
}

// TestCheckChallengeExpiry_EndToEnd calls the exported CheckChallengeExpiry
// loop via the internal checkChallengeExpiryAt helper (same package) with a
// controlled clock so the hour-gate is always satisfied.
func TestCheckChallengeExpiry_EndToEnd(t *testing.T) {
	db := setupSchedulerTestDB(t)
	ctx := context.Background()

	parentID := insertSchedulerUser(t, db, "parent-e2e@example.com")
	childID := insertSchedulerUser(t, db, "child-e2e@example.com")
	// No family_link row — child is only in challenge_participants.
	now := time.Now().UTC().Truncate(24 * time.Hour).Add(10 * time.Hour)
	twoDaysLater := now.AddDate(0, 0, 2).Format("2006-01-02")
	insertChallengeWithParticipant(t, db, parentID, childID, twoDaysLater)

	var delivered []int64
	deliver := func(uid int64, _ []byte) { delivered = append(delivered, uid) }

	checkChallengeExpiryAt(ctx, db, deliver, now)

	if len(delivered) != 1 || delivered[0] != childID {
		t.Errorf("expected delivery to child %d, got %v", childID, delivered)
	}
}

// TestCheckChallengeExpiry_NoFamilyLinkStillDelivers verifies that a child
// retains challenge-expiry reminders even after their family_link is removed.
func TestCheckChallengeExpiry_NoFamilyLinkStillDelivers(t *testing.T) {
	db := setupSchedulerTestDB(t)
	ctx := context.Background()

	parentID := insertSchedulerUser(t, db, "parent-nfl@example.com")
	childID := insertSchedulerUser(t, db, "child-nfl@example.com")
	// Deliberately omit the family_link row to simulate an unlinked child.

	now := time.Now().UTC().Truncate(24 * time.Hour).Add(10 * time.Hour)
	twoDaysLater := now.AddDate(0, 0, 2).Format("2006-01-02")
	insertChallengeWithParticipant(t, db, parentID, childID, twoDaysLater)

	var delivered []int64
	deliver := func(uid int64, _ []byte) { delivered = append(delivered, uid) }

	checkChallengeExpiryAt(ctx, db, deliver, now)

	if len(delivered) != 1 || delivered[0] != childID {
		t.Errorf("expected delivery to unlinked child %d, got %v", childID, delivered)
	}
}

// TestCheckChallengeExpiry_DedupEndToEnd exercises the dedup path through the
// full loop to confirm already-sent milestones are not re-delivered.
func TestCheckChallengeExpiry_DedupEndToEnd(t *testing.T) {
	db := setupSchedulerTestDB(t)
	ctx := context.Background()

	parentID := insertSchedulerUser(t, db, "parent-dedup-e2e@example.com")
	childID := insertSchedulerUser(t, db, "child-dedup-e2e@example.com")

	now := time.Now().UTC().Truncate(24 * time.Hour).Add(10 * time.Hour)
	twoDaysLater := now.AddDate(0, 0, 2).Format("2006-01-02")
	challengeID := insertChallengeWithParticipant(t, db, parentID, childID, twoDaysLater)

	dedupKey := fmt.Sprintf("challenge_expiry:%d-2d", challengeID)
	if _, err := db.Exec(
		`INSERT INTO daemon_notification_sent (user_id, key, sent_at) VALUES (?, ?, ?)`,
		childID, dedupKey, time.Now().UTC().Format(time.RFC3339),
	); err != nil {
		t.Fatalf("insert dedup: %v", err)
	}

	var delivered []int64
	deliver := func(uid int64, _ []byte) { delivered = append(delivered, uid) }

	checkChallengeExpiryAt(ctx, db, deliver, now)

	if len(delivered) != 0 {
		t.Errorf("expected no duplicate delivery, got %v", delivered)
	}
}

func TestMaybeSendWeeklySummary_DedupPreventsDoubleSend(t *testing.T) {
	db := setupSchedulerTestDB(t)
	ctx := context.Background()

	parentID := insertSchedulerUser(t, db, "parent2@example.com")
	childID := insertSchedulerUser(t, db, "child2@example.com")

	if _, err := db.Exec(`
		INSERT INTO family_links (parent_id, child_id, nickname) VALUES (?, ?, 'Kid2')`,
		parentID, childID); err != nil {
		t.Fatalf("insert family_link: %v", err)
	}

	monday8 := lastMonday8()
	y, w := monday8.ISOWeek()
	weekKey := fmt.Sprintf("%d-W%02d", y, w)

	if _, err := db.Exec(
		`INSERT INTO daemon_notification_sent (user_id, key, sent_at) VALUES (?, ?, ?)`,
		parentID, "weekly:"+weekKey, time.Now().UTC().Format(time.RFC3339),
	); err != nil {
		t.Fatalf("insert dedup: %v", err)
	}

	var delivered []int64
	deliver := func(uid int64, _ []byte) { delivered = append(delivered, uid) }

	maybeSendWeeklySummary(ctx, db, deliver, parentID, monday8)

	if len(delivered) != 0 {
		t.Errorf("expected no duplicate delivery, got %v", delivered)
	}
}

// --- CheckCloseToReward ---

// insertFamilyReward inserts a family reward for the given parent and returns its ID.
func insertFamilyReward(t *testing.T, db *sql.DB, parentID int64, title string, starCost int, isActive bool) int64 {
	t.Helper()
	active := 0
	if isActive {
		active = 1
	}
	res, err := db.Exec(
		`INSERT INTO family_rewards (parent_id, title, star_cost, is_active, created_at, updated_at) VALUES (?, ?, ?, ?, '', '')`,
		parentID, title, starCost, active,
	)
	if err != nil {
		t.Fatalf("insert family_reward: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("insert family_reward LastInsertId: %v", err)
	}
	return id
}

func TestCheckCloseToReward_NonChildNoDelivery(t *testing.T) {
	db := setupSchedulerTestDB(t)
	ctx := context.Background()

	// Standalone user (not a child in any family_link).
	userID := insertSchedulerUser(t, db, "standalone@example.com")

	var delivered []int64
	deliver := func(uid int64, _ []byte) { delivered = append(delivered, uid) }

	CheckCloseToReward(ctx, db, userID, 80, deliver)

	if len(delivered) != 0 {
		t.Errorf("expected no delivery for non-child user, got %v", delivered)
	}
}

func TestCheckCloseToReward_WithinThresholdDelivers(t *testing.T) {
	db := setupSchedulerTestDB(t)
	ctx := context.Background()

	parentID := insertSchedulerUser(t, db, "parent-ctr@example.com")
	childID := insertSchedulerUser(t, db, "child-ctr@example.com")
	if _, err := db.Exec(`INSERT INTO family_links (parent_id, child_id, nickname) VALUES (?, ?, 'Kid')`, parentID, childID); err != nil {
		t.Fatalf("insert family_link: %v", err)
	}

	// Reward costs 100 stars. 80% threshold = 80. Balance of 85 is within range.
	insertFamilyReward(t, db, parentID, "New Bike", 100, true)

	var delivered []int64
	deliver := func(uid int64, _ []byte) { delivered = append(delivered, uid) }

	CheckCloseToReward(ctx, db, childID, 85, deliver)

	if len(delivered) != 1 || delivered[0] != childID {
		t.Errorf("expected delivery to child %d, got %v", childID, delivered)
	}
}

func TestCheckCloseToReward_AtExactThresholdDelivers(t *testing.T) {
	db := setupSchedulerTestDB(t)
	ctx := context.Background()

	parentID := insertSchedulerUser(t, db, "parent-thresh@example.com")
	childID := insertSchedulerUser(t, db, "child-thresh@example.com")
	if _, err := db.Exec(`INSERT INTO family_links (parent_id, child_id, nickname) VALUES (?, ?, 'Kid')`, parentID, childID); err != nil {
		t.Fatalf("insert family_link: %v", err)
	}

	// Reward costs 100, threshold = 80. Balance of exactly 80 triggers notification.
	insertFamilyReward(t, db, parentID, "Toy", 100, true)

	var delivered []int64
	deliver := func(uid int64, _ []byte) { delivered = append(delivered, uid) }

	CheckCloseToReward(ctx, db, childID, 80, deliver)

	if len(delivered) != 1 {
		t.Errorf("expected delivery at exact threshold, got %v", delivered)
	}
}

func TestCheckCloseToReward_BelowThresholdNoDelivery(t *testing.T) {
	db := setupSchedulerTestDB(t)
	ctx := context.Background()

	parentID := insertSchedulerUser(t, db, "parent-below@example.com")
	childID := insertSchedulerUser(t, db, "child-below@example.com")
	if _, err := db.Exec(`INSERT INTO family_links (parent_id, child_id, nickname) VALUES (?, ?, 'Kid')`, parentID, childID); err != nil {
		t.Fatalf("insert family_link: %v", err)
	}

	// Reward costs 100. Balance of 79 is below the 80% threshold.
	insertFamilyReward(t, db, parentID, "Book", 100, true)

	var delivered []int64
	deliver := func(uid int64, _ []byte) { delivered = append(delivered, uid) }

	CheckCloseToReward(ctx, db, childID, 79, deliver)

	if len(delivered) != 0 {
		t.Errorf("expected no delivery below threshold, got %v", delivered)
	}
}

func TestCheckCloseToReward_CanAlreadyAffordNoDelivery(t *testing.T) {
	db := setupSchedulerTestDB(t)
	ctx := context.Background()

	parentID := insertSchedulerUser(t, db, "parent-afford@example.com")
	childID := insertSchedulerUser(t, db, "child-afford@example.com")
	if _, err := db.Exec(`INSERT INTO family_links (parent_id, child_id, nickname) VALUES (?, ?, 'Kid')`, parentID, childID); err != nil {
		t.Fatalf("insert family_link: %v", err)
	}

	// Reward costs 100. Balance of 100 means they can already afford it.
	insertFamilyReward(t, db, parentID, "Game", 100, true)

	var delivered []int64
	deliver := func(uid int64, _ []byte) { delivered = append(delivered, uid) }

	CheckCloseToReward(ctx, db, childID, 100, deliver)

	if len(delivered) != 0 {
		t.Errorf("expected no delivery when balance >= cost, got %v", delivered)
	}
}

func TestCheckCloseToReward_InactiveRewardNoDelivery(t *testing.T) {
	db := setupSchedulerTestDB(t)
	ctx := context.Background()

	parentID := insertSchedulerUser(t, db, "parent-inactive@example.com")
	childID := insertSchedulerUser(t, db, "child-inactive@example.com")
	if _, err := db.Exec(`INSERT INTO family_links (parent_id, child_id, nickname) VALUES (?, ?, 'Kid')`, parentID, childID); err != nil {
		t.Fatalf("insert family_link: %v", err)
	}

	// Inactive reward should be ignored.
	insertFamilyReward(t, db, parentID, "Inactive Reward", 100, false)

	var delivered []int64
	deliver := func(uid int64, _ []byte) { delivered = append(delivered, uid) }

	CheckCloseToReward(ctx, db, childID, 85, deliver)

	if len(delivered) != 0 {
		t.Errorf("expected no delivery for inactive reward, got %v", delivered)
	}
}

func TestCheckCloseToReward_CooldownPreventsRepeat(t *testing.T) {
	db := setupSchedulerTestDB(t)
	ctx := context.Background()

	parentID := insertSchedulerUser(t, db, "parent-cooldown@example.com")
	childID := insertSchedulerUser(t, db, "child-cooldown@example.com")
	if _, err := db.Exec(`INSERT INTO family_links (parent_id, child_id, nickname) VALUES (?, ?, 'Kid')`, parentID, childID); err != nil {
		t.Fatalf("insert family_link: %v", err)
	}

	rewardID := insertFamilyReward(t, db, parentID, "Trip", 100, true)

	// Pre-insert a dedup record sent 3 days ago (within 7-day window).
	threeDaysAgo := time.Now().UTC().Add(-3 * 24 * time.Hour).Format(time.RFC3339)
	key := fmt.Sprintf("close_reward:%d", rewardID)
	if _, err := db.Exec(
		`INSERT INTO daemon_notification_sent (user_id, key, sent_at) VALUES (?, ?, ?)`,
		childID, key, threeDaysAgo,
	); err != nil {
		t.Fatalf("insert dedup: %v", err)
	}

	var delivered []int64
	deliver := func(uid int64, _ []byte) { delivered = append(delivered, uid) }

	CheckCloseToReward(ctx, db, childID, 85, deliver)

	if len(delivered) != 0 {
		t.Errorf("expected no delivery within 7-day cooldown, got %v", delivered)
	}
}

func TestCheckCloseToReward_ResendsAfterCooldown(t *testing.T) {
	db := setupSchedulerTestDB(t)
	ctx := context.Background()

	parentID := insertSchedulerUser(t, db, "parent-resend@example.com")
	childID := insertSchedulerUser(t, db, "child-resend@example.com")
	if _, err := db.Exec(`INSERT INTO family_links (parent_id, child_id, nickname) VALUES (?, ?, 'Kid')`, parentID, childID); err != nil {
		t.Fatalf("insert family_link: %v", err)
	}

	rewardID := insertFamilyReward(t, db, parentID, "Concert Tickets", 100, true)

	// Pre-insert a dedup record sent 8 days ago (outside 7-day window).
	eightDaysAgo := time.Now().UTC().Add(-8 * 24 * time.Hour).Format(time.RFC3339)
	key := fmt.Sprintf("close_reward:%d", rewardID)
	if _, err := db.Exec(
		`INSERT INTO daemon_notification_sent (user_id, key, sent_at) VALUES (?, ?, ?)`,
		childID, key, eightDaysAgo,
	); err != nil {
		t.Fatalf("insert dedup: %v", err)
	}

	var delivered []int64
	deliver := func(uid int64, _ []byte) { delivered = append(delivered, uid) }

	CheckCloseToReward(ctx, db, childID, 85, deliver)

	if len(delivered) != 1 || delivered[0] != childID {
		t.Errorf("expected delivery after 7-day cooldown expired, got %v", delivered)
	}
}
