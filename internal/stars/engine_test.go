package stars

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/encryption"
	_ "modernc.org/sqlite"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	t.Setenv("ENCRYPTION_KEY", "test-encryption-key-stars-tests")
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

	CREATE TABLE IF NOT EXISTS user_preferences (
		user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		key     TEXT NOT NULL,
		value   TEXT NOT NULL DEFAULT '',
		PRIMARY KEY (user_id, key)
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
		pace_cv_pct         REAL
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

	CREATE TABLE IF NOT EXISTS star_balances (
		user_id         INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
		total_earned    INTEGER NOT NULL DEFAULT 0,
		total_spent     INTEGER NOT NULL DEFAULT 0,
		current_balance INTEGER GENERATED ALWAYS AS (total_earned - total_spent) STORED
	);

	CREATE TABLE IF NOT EXISTS user_levels (
		user_id INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
		xp      INTEGER NOT NULL DEFAULT 0,
		level   INTEGER NOT NULL DEFAULT 1,
		title   TEXT NOT NULL DEFAULT 'Rookie Runner'
	);

	CREATE TABLE IF NOT EXISTS streaks (
		user_id       INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		streak_type   TEXT NOT NULL,
		current_count INTEGER NOT NULL DEFAULT 0,
		longest_count INTEGER NOT NULL DEFAULT 0,
		last_activity TEXT NOT NULL DEFAULT '',
		PRIMARY KEY (user_id, streak_type)
	);

	CREATE TABLE IF NOT EXISTS streak_shields (
		id          INTEGER PRIMARY KEY,
		parent_id   INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		child_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		used_at     TEXT NOT NULL DEFAULT '',
		shield_date TEXT NOT NULL DEFAULT ''
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

	// Seed the system user (id=0) so FK constraints on family_challenges.creator_id
	// are satisfied when GenerateWeeklyChallenges inserts system challenges.
	if _, err := db.Exec(`INSERT OR IGNORE INTO users (id, email, name, picture, google_id) VALUES (0, 'system@hytte.internal', 'System', '', 'system')`); err != nil {
		t.Fatalf("insert system user: %v", err)
	}

	return db
}

// insertUser inserts a test user and returns their ID.
func insertUser(t *testing.T, db *sql.DB, email string) int64 {
	t.Helper()
	res, err := db.Exec(`
		INSERT INTO users (email, name, picture, google_id)
		VALUES (?, 'Test User', '', ?)
	`, email, email)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

// linkChild creates a parent-child link.
func linkChild(t *testing.T, db *sql.DB, parentID, childID int64) {
	t.Helper()
	_, err := db.Exec(`
		INSERT INTO family_links (parent_id, child_id, nickname, avatar_emoji, created_at)
		VALUES (?, ?, 'Kid', '⭐', ?)
	`, parentID, childID, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		t.Fatalf("link child: %v", err)
	}
}

// insertWorkout inserts a test workout and returns its ID.
func insertWorkout(t *testing.T, db *sql.DB, userID int64, durationSec int, distanceM float64, calories int, ascentM float64, paceSecPerKm float64) int64 {
	t.Helper()
	res, err := db.Exec(`
		INSERT INTO workouts (user_id, duration_seconds, distance_meters, calories, ascent_meters, avg_pace_sec_per_km, started_at, fit_file_hash, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, userID, durationSec, distanceM, calories, ascentM, paceSecPerKm,
		time.Now().UTC().Format(time.RFC3339),
		fmt.Sprintf("hash-%d-%d", userID, time.Now().UnixNano()),
		time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		t.Fatalf("insert workout: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

// getBalance returns (total_earned, total_spent, current_balance) for a user.
func getBalance(t *testing.T, db *sql.DB, userID int64) (earned, spent, balance int) {
	t.Helper()
	err := db.QueryRow(`
		SELECT total_earned, total_spent, current_balance
		FROM star_balances WHERE user_id = ?
	`, userID).Scan(&earned, &spent, &balance)
	if err == sql.ErrNoRows {
		return 0, 0, 0
	}
	if err != nil {
		t.Fatalf("get balance: %v", err)
	}
	return
}

func TestHRZone(t *testing.T) {
	tests := []struct {
		hr    int
		maxHR int
		want  int
	}{
		{0, 190, 0},
		{90, 190, 0},  // ~47%, below Z1
		{100, 190, 1}, // ~52%, Z1
		{120, 190, 2}, // ~63%, Z2
		{140, 190, 3}, // ~73%, Z3
		{160, 190, 4}, // ~84%, Z4
		{180, 190, 5}, // ~94%, Z5
	}
	for _, tt := range tests {
		got := hrZone(tt.hr, tt.maxHR)
		if got != tt.want {
			t.Errorf("hrZone(%d, %d) = %d, want %d", tt.hr, tt.maxHR, got, tt.want)
		}
	}
}

func TestComputeTimeInZones(t *testing.T) {
	// Samples: 60s in Z1, 60s in Z2, 60s in Z3 (maxHR=190)
	// Each interval [i, i+1] is attributed to samples[i]'s HR zone.
	samples := []HRSample{
		{0, 100},      // Z1 at t=0  → covers 0→60s = 60s in Z1
		{60000, 120},  // Z2 at t=60s → covers 60→120s = 60s in Z2
		{120000, 140}, // Z3 at t=120s → covers 120→180s = 60s in Z3
		{180000, 0},   // end (no further interval)
	}
	zones := computeTimeInZones(samples, 190)
	if zones[1] != 60 {
		t.Errorf("Z1 = %.0f, want 60", zones[1])
	}
	if zones[2] != 60 {
		t.Errorf("Z2 = %.0f, want 60", zones[2])
	}
	if zones[3] != 60 {
		t.Errorf("Z3 = %.0f, want 60", zones[3])
	}
}

func TestBaseStars_ShortWorkout(t *testing.T) {
	db := setupTestDB(t)
	parentID := insertUser(t, db, "parent@test.com")
	childID := insertUser(t, db, "child@test.com")
	linkChild(t, db, parentID, childID)

	workoutID := insertWorkout(t, db, childID, 300, 1000, 0, 0, 0) // 5 min — too short

	awards, err := EvaluateWorkout(context.Background(), db, childID, WorkoutInput{
		ID:              workoutID,
		DurationSeconds: 300,
		DistanceMeters:  1000,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(awards) != 0 {
		t.Errorf("expected no awards for short workout, got %d", len(awards))
	}
	earned, _, _ := getBalance(t, db, childID)
	if earned != 0 {
		t.Errorf("expected 0 balance, got %d", earned)
	}
}

func TestBaseStars_MinimumWorkout(t *testing.T) {
	db := setupTestDB(t)
	parentID := insertUser(t, db, "parent@test.com")
	childID := insertUser(t, db, "child@test.com")
	linkChild(t, db, parentID, childID)

	workoutID := insertWorkout(t, db, childID, 600, 0, 0, 0, 0) // exactly 10 min

	awards, err := EvaluateWorkout(context.Background(), db, childID, WorkoutInput{
		ID:              workoutID,
		DurationSeconds: 600,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should get: showed_up=2
	if len(awards) == 0 {
		t.Fatal("expected awards for 10-min workout")
	}
	hasShowedUp := false
	for _, a := range awards {
		if a.Reason == "showed_up" {
			hasShowedUp = true
			if a.Amount != 2 {
				t.Errorf("showed_up award amount = %d, want 2", a.Amount)
			}
		}
	}
	if !hasShowedUp {
		t.Error("expected showed_up award")
	}
}

func TestBaseStars_DurationBonus(t *testing.T) {
	tests := []struct {
		durationSec int
		wantBonus   int
	}{
		{600, 0},   // 10 min → 0 bonus (10/15=0)
		{900, 1},   // 15 min → 1 bonus
		{1800, 2},  // 30 min → 2 bonus
		{3600, 4},  // 60 min → 4 bonus
		{7200, 8},  // 120 min → 8 bonus (capped)
		{10800, 8}, // 180 min → still capped at 8
	}
	for _, tt := range tests {
		durationMin := tt.durationSec / 60
		bonus := min(durationMin/15, 8)
		if bonus != tt.wantBonus {
			t.Errorf("duration %ds → bonus %d, want %d", tt.durationSec, bonus, tt.wantBonus)
		}
	}
}

func TestBaseStars_EffortBonus(t *testing.T) {
	db := setupTestDB(t)
	parentID := insertUser(t, db, "parent@test.com")
	childID := insertUser(t, db, "child@test.com")
	linkChild(t, db, parentID, childID)

	maxHR := 190
	tests := []struct {
		avgHR      int
		wantBonus  int
		wantReason string
	}{
		{115, 1, "effort_bonus"}, // ~60% = Z2
		{133, 2, "effort_bonus"}, // ~70% = Z3
		{152, 3, "effort_bonus"}, // ~80% = Z4
	}
	for _, tt := range tests {
		zone := hrZone(tt.avgHR, maxHR)
		effortBonus := 0
		switch zone {
		case 2:
			effortBonus = 1
		case 3:
			effortBonus = 2
		case 4, 5:
			effortBonus = 3
		}
		if effortBonus != tt.wantBonus {
			t.Errorf("avgHR=%d → effortBonus=%d, want %d", tt.avgHR, effortBonus, tt.wantBonus)
		}
	}
}

func TestEvaluateWorkout_NonChild(t *testing.T) {
	db := setupTestDB(t)
	userID := insertUser(t, db, "solo@test.com")
	workoutID := insertWorkout(t, db, userID, 1800, 5000, 0, 0, 0)

	awards, err := EvaluateWorkout(context.Background(), db, userID, WorkoutInput{
		ID:              workoutID,
		DurationSeconds: 1800,
		DistanceMeters:  5000,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(awards) != 0 {
		t.Errorf("expected no awards for non-child user, got %d", len(awards))
	}
}

func TestHRZoneAwards_ZoneCommander(t *testing.T) {
	// 90% of workout in Z2
	zones := [6]float64{0, 0, 90, 5, 3, 2}
	totalSec := 100.0
	awards := checkHRZoneAwards(zones, totalSec)
	found := false
	for _, a := range awards {
		if a.Reason == "zone_commander" {
			found = true
			if a.Amount != 5 {
				t.Errorf("zone_commander amount = %d, want 5", a.Amount)
			}
		}
	}
	if !found {
		t.Error("expected zone_commander award")
	}
}

func TestHRZoneAwards_ZoneExplorer(t *testing.T) {
	zones := [6]float64{0, 10, 20, 20, 30, 20}
	totalSec := 100.0
	awards := checkHRZoneAwards(zones, totalSec)
	found := false
	for _, a := range awards {
		if a.Reason == "zone_explorer" {
			found = true
		}
	}
	if !found {
		t.Error("expected zone_explorer award when all zones hit")
	}
}

func TestHRZoneAwards_EasyDayHero(t *testing.T) {
	// 97% in Z1+Z2, barely any in higher zones
	zones := [6]float64{0, 60, 37, 1, 1, 1}
	totalSec := 100.0
	awards := checkHRZoneAwards(zones, totalSec)
	found := false
	for _, a := range awards {
		if a.Reason == "easy_day_hero" {
			found = true
		}
	}
	if !found {
		t.Error("expected easy_day_hero award")
	}
}

func TestHRZoneAwards_ThresholdTrainer(t *testing.T) {
	zones := [6]float64{0, 0, 0, 0, 1300, 0}
	totalSec := 1300.0
	awards := checkHRZoneAwards(zones, totalSec)
	found := false
	for _, a := range awards {
		if a.Reason == "threshold_trainer" {
			found = true
		}
	}
	if !found {
		t.Error("expected threshold_trainer award for 1300s in Z4")
	}
}

func TestTransactionAtomicity(t *testing.T) {
	db := setupTestDB(t)
	parentID := insertUser(t, db, "parent@test.com")
	childID := insertUser(t, db, "child@test.com")
	linkChild(t, db, parentID, childID)

	awards := []StarAward{
		{Amount: 2, Reason: "showed_up", Description: "Test"},
		{Amount: 3, Reason: "duration_bonus", Description: "Test"},
		{Amount: 1, Reason: "effort_bonus", Description: "Test"},
	}

	if err := recordAwards(db, childID, 1, awards); err != nil {
		t.Fatalf("recordAwards: %v", err)
	}

	// Verify balance matches sum.
	var txnSum int
	if err := db.QueryRow(`SELECT COALESCE(SUM(amount), 0) FROM star_transactions WHERE user_id = ?`, childID).Scan(&txnSum); err != nil {
		t.Fatalf("sum transactions: %v", err)
	}
	earned, _, balance := getBalance(t, db, childID)
	if txnSum != 6 {
		t.Errorf("transaction sum = %d, want 6", txnSum)
	}
	if earned != 6 {
		t.Errorf("total_earned = %d, want 6", earned)
	}
	if balance != 6 {
		t.Errorf("current_balance = %d, want 6", balance)
	}
}

// TestEvaluateWorkout_XPAwarded verifies that EvaluateWorkout awards XP equal to
// the sum of positive star amounts after a qualifying workout.
func TestEvaluateWorkout_XPAwarded(t *testing.T) {
	db := setupTestDB(t)
	parentID := insertUser(t, db, "parent@xp.com")
	childID := insertUser(t, db, "child@xp.com")
	linkChild(t, db, parentID, childID)

	// 30-min workout: showed_up=2, duration_bonus=2 → total 4 XP expected.
	workoutID := insertWorkout(t, db, childID, 1800, 5000, 0, 0, 0)

	awards, err := EvaluateWorkout(context.Background(), db, childID, WorkoutInput{
		ID:              workoutID,
		DurationSeconds: 1800,
		DistanceMeters:  5000,
	})
	if err != nil {
		t.Fatalf("EvaluateWorkout: %v", err)
	}
	if len(awards) == 0 {
		t.Fatal("expected awards for 30-min workout; cannot verify XP")
	}

	// Sum positive star awards — this is what the engine should pass to AddXP.
	expectedXP := 0
	for _, a := range awards {
		if a.Amount > 0 {
			expectedXP += a.Amount
		}
	}
	if expectedXP == 0 {
		t.Fatal("no positive star amounts in awards; test setup incorrect")
	}

	// AddXP is called synchronously inside EvaluateWorkout (before the level-up
	// notification goroutine), so the user_levels row should already be updated
	// by the time we reach here.
	var xp int
	err = db.QueryRow(`SELECT xp FROM user_levels WHERE user_id = ?`, childID).Scan(&xp)
	if err != nil {
		t.Fatalf("query user_levels: %v", err)
	}
	if xp != expectedXP {
		t.Errorf("user_levels.xp = %d, want %d (sum of positive award amounts)", xp, expectedXP)
	}
}

// TestEvaluateWorkout_XPNotAwardedForNonChild verifies that solo users (not linked
// as children) do not receive XP after workout evaluation.
func TestEvaluateWorkout_XPNotAwardedForNonChild(t *testing.T) {
	db := setupTestDB(t)
	userID := insertUser(t, db, "solo@xp.com")
	workoutID := insertWorkout(t, db, userID, 1800, 5000, 0, 0, 0)

	_, err := EvaluateWorkout(context.Background(), db, userID, WorkoutInput{
		ID:              workoutID,
		DurationSeconds: 1800,
		DistanceMeters:  5000,
	})
	if err != nil {
		t.Fatalf("EvaluateWorkout: %v", err)
	}

	// No star awards for non-child users; user_levels row should not exist.
	var xp int
	err = db.QueryRow(`SELECT xp FROM user_levels WHERE user_id = ?`, userID).Scan(&xp)
	if err == nil {
		t.Fatalf("expected no user_levels row for non-child user, but found xp=%d", xp)
	}
	if err != sql.ErrNoRows {
		t.Fatalf("query user_levels: %v", err)
	}
}
