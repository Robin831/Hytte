package stars

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/encryption"
	"github.com/go-chi/chi/v5"
)

func newRequest(method, path string) *http.Request {
	r := httptest.NewRequest(method, path, nil)
	r.Header.Set("Content-Type", "application/json")
	return r
}

func withUser(r *http.Request, user *auth.User) *http.Request {
	return r.WithContext(auth.ContextWithUser(r.Context(), user))
}

func decode(t *testing.T, body []byte, v any) {
	t.Helper()
	if err := json.Unmarshal(body, v); err != nil {
		t.Fatalf("decode response: %v (body: %s)", err, body)
	}
}

func TestBalanceHandler_NoBalance(t *testing.T) {
	db := setupTestDB(t)
	userID := insertUser(t, db, "solo@test.com")
	user := &auth.User{ID: userID, Email: "solo@test.com", Name: "Solo"}

	handler := BalanceHandler(db)
	r := withUser(newRequest(http.MethodGet, "/api/stars/balance"), user)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp BalanceResponse
	decode(t, w.Body.Bytes(), &resp)

	if resp.CurrentBalance != 0 {
		t.Errorf("expected 0 balance for new user, got %d", resp.CurrentBalance)
	}
	if resp.Level != 1 {
		t.Errorf("expected level 1 for new user, got %d", resp.Level)
	}
	if resp.Title != "Rookie Runner" {
		t.Errorf("expected 'Rookie Runner' title for new user, got %q", resp.Title)
	}
	// New fields: level 1 default should include emoji, xp_for_next_level, and progress_percent.
	if resp.Emoji != "🐣" {
		t.Errorf("expected '🐣' emoji for new user at level 1, got %q", resp.Emoji)
	}
	if resp.XPForNextLevel != 50 {
		t.Errorf("expected xp_for_next_level=50 for new user at level 1, got %d", resp.XPForNextLevel)
	}
	if resp.ProgressPercent != 0 {
		t.Errorf("expected progress_percent=0 for new user at level 1, got %f", resp.ProgressPercent)
	}
}

func TestBalanceHandler_WithBalance(t *testing.T) {
	db := setupTestDB(t)
	parentID := insertUser(t, db, "parent@test.com")
	childID := insertUser(t, db, "child@test.com")
	linkChild(t, db, parentID, childID)

	// Seed a balance directly.
	if _, err := db.Exec(`
		INSERT INTO star_balances (user_id, total_earned, total_spent)
		VALUES (?, 42, 10)
	`, childID); err != nil {
		t.Fatalf("seed balance: %v", err)
	}

	user := &auth.User{ID: childID, Email: "child@test.com", Name: "Child"}
	handler := BalanceHandler(db)
	r := withUser(newRequest(http.MethodGet, "/api/stars/balance"), user)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp BalanceResponse
	decode(t, w.Body.Bytes(), &resp)

	if resp.TotalEarned != 42 {
		t.Errorf("total_earned = %d, want 42", resp.TotalEarned)
	}
	if resp.TotalSpent != 10 {
		t.Errorf("total_spent = %d, want 10", resp.TotalSpent)
	}
	if resp.CurrentBalance != 32 {
		t.Errorf("current_balance = %d, want 32", resp.CurrentBalance)
	}
}

func TestBalanceHandler_WithLevel(t *testing.T) {
	db := setupTestDB(t)
	userID := insertUser(t, db, "user@test.com")
	user := &auth.User{ID: userID, Email: "user@test.com", Name: "User"}

	// Seed a balance and level row.
	// Level 3 = "Steady Stepper" with emoji "🚶"; XP=200 puts progress at (200-150)/(300-150)*100 ≈ 33.3%.
	if _, err := db.Exec(`
		INSERT INTO star_balances (user_id, total_earned) VALUES (?, 100)
	`, userID); err != nil {
		t.Fatalf("seed balance: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO user_levels (user_id, xp, level, title) VALUES (?, 200, 3, 'Steady Stepper')
	`, userID); err != nil {
		t.Fatalf("seed level: %v", err)
	}

	handler := BalanceHandler(db)
	r := withUser(newRequest(http.MethodGet, "/api/stars/balance"), user)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp BalanceResponse
	decode(t, w.Body.Bytes(), &resp)

	if resp.Level != 3 {
		t.Errorf("level = %d, want 3", resp.Level)
	}
	if resp.XP != 200 {
		t.Errorf("xp = %d, want 200", resp.XP)
	}
	if resp.Title != "Steady Stepper" {
		t.Errorf("title = %q, want 'Steady Stepper'", resp.Title)
	}
	// New fields added by this bead.
	if resp.Emoji != "🚶" {
		t.Errorf("emoji = %q, want '🚶' (level 3)", resp.Emoji)
	}
	if resp.XPForNextLevel != 300 {
		t.Errorf("xp_for_next_level = %d, want 300 (level 4 threshold)", resp.XPForNextLevel)
	}
	// progress = (200-150)/(300-150)*100 = 33.33...
	if resp.ProgressPercent < 33.0 || resp.ProgressPercent > 34.0 {
		t.Errorf("progress_percent = %.2f, want ~33.33", resp.ProgressPercent)
	}
}

func TestTransactionsHandler_Empty(t *testing.T) {
	db := setupTestDB(t)
	userID := insertUser(t, db, "solo@test.com")
	user := &auth.User{ID: userID, Email: "solo@test.com", Name: "Solo"}

	handler := TransactionsHandler(db)
	r := withUser(newRequest(http.MethodGet, "/api/stars/transactions"), user)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Transactions    []Transaction `json:"transactions"`
		WeeklyStars     int           `json:"weekly_stars"`
		WeeklyStarredWorkouts  int           `json:"weekly_starred_workouts"`
	}
	decode(t, w.Body.Bytes(), &resp)

	if resp.Transactions == nil {
		t.Error("expected non-nil transactions slice")
	}
	if len(resp.Transactions) != 0 {
		t.Errorf("expected 0 transactions, got %d", len(resp.Transactions))
	}
	if resp.WeeklyStars != 0 {
		t.Errorf("expected 0 weekly stars, got %d", resp.WeeklyStars)
	}
}

func TestTransactionsHandler_WithData(t *testing.T) {
	db := setupTestDB(t)
	userID := insertUser(t, db, "user@test.com")
	user := &auth.User{ID: userID, Email: "user@test.com", Name: "User"}

	now := time.Now().UTC().Format(time.RFC3339)
	// Insert a few transactions.
	for _, args := range []struct {
		amount int
		reason string
		desc   string
	}{
		{2, "showed_up", "Showed up and worked out!"},
		{3, "duration_bonus", "30 minute workout"},
		{1, "effort_bonus", "Zone 2 effort"},
	} {
		if _, err := db.Exec(`
			INSERT INTO star_transactions (user_id, amount, reason, description, reference_id, created_at)
			VALUES (?, ?, ?, ?, NULL, ?)
		`, userID, args.amount, args.reason, args.desc, now); err != nil {
			t.Fatalf("insert transaction: %v", err)
		}
	}

	handler := TransactionsHandler(db)
	r := withUser(newRequest(http.MethodGet, "/api/stars/transactions"), user)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Transactions   []Transaction `json:"transactions"`
		WeeklyStars    int           `json:"weekly_stars"`
		WeeklyStarredWorkouts int           `json:"weekly_starred_workouts"`
	}
	decode(t, w.Body.Bytes(), &resp)

	if len(resp.Transactions) != 3 {
		t.Errorf("expected 3 transactions, got %d", len(resp.Transactions))
	}
	// Transactions are ordered DESC by created_at — order may vary for same timestamp.
	totalAmount := 0
	for _, tx := range resp.Transactions {
		totalAmount += tx.Amount
	}
	if totalAmount != 6 {
		t.Errorf("total transaction amount = %d, want 6", totalAmount)
	}
	if resp.WeeklyStars != 6 {
		t.Errorf("weekly_stars = %d, want 6", resp.WeeklyStars)
	}
}

func TestTransactionsHandler_PaginationLimit(t *testing.T) {
	db := setupTestDB(t)
	userID := insertUser(t, db, "user@test.com")
	user := &auth.User{ID: userID, Email: "user@test.com", Name: "User"}

	now := time.Now().UTC().Format(time.RFC3339)
	for i := range 10 {
		if _, err := db.Exec(`
			INSERT INTO star_transactions (user_id, amount, reason, description, reference_id, created_at)
			VALUES (?, 1, 'showed_up', '', NULL, ?)
		`, userID, now); err != nil {
			t.Fatalf("insert transaction %d: %v", i, err)
		}
	}

	handler := TransactionsHandler(db)
	r := withUser(newRequest(http.MethodGet, "/api/stars/transactions?limit=3"), user)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Transactions []Transaction `json:"transactions"`
	}
	decode(t, w.Body.Bytes(), &resp)

	if len(resp.Transactions) != 3 {
		t.Errorf("expected 3 transactions with limit=3, got %d", len(resp.Transactions))
	}
}

func TestTransactionsHandler_InvalidLimitIgnored(t *testing.T) {
	db := setupTestDB(t)
	userID := insertUser(t, db, "user@test.com")
	user := &auth.User{ID: userID, Email: "user@test.com", Name: "User"}

	handler := TransactionsHandler(db)
	// Limit of 999 exceeds max (200) — should use default 50.
	r := withUser(newRequest(http.MethodGet, "/api/stars/transactions?limit=999"), user)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestStreaksHandler_ZeroCase(t *testing.T) {
	db := setupTestDB(t)
	userID := insertUser(t, db, "user@test.com")
	user := &auth.User{ID: userID, Email: "user@test.com", Name: "User"}

	handler := StreaksHandler(db)
	r := withUser(newRequest(http.MethodGet, "/api/stars/streaks"), user)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp StreaksResponse
	decode(t, w.Body.Bytes(), &resp)

	if resp.DailyWorkout.CurrentCount != 0 {
		t.Errorf("daily_workout.current_count = %d, want 0", resp.DailyWorkout.CurrentCount)
	}
	if resp.DailyWorkout.LongestCount != 0 {
		t.Errorf("daily_workout.longest_count = %d, want 0", resp.DailyWorkout.LongestCount)
	}
	if resp.WeeklyWorkout.CurrentCount != 0 {
		t.Errorf("weekly_workout.current_count = %d, want 0", resp.WeeklyWorkout.CurrentCount)
	}
	if resp.WeeklyWorkout.LongestCount != 0 {
		t.Errorf("weekly_workout.longest_count = %d, want 0", resp.WeeklyWorkout.LongestCount)
	}
}

func TestStreaksHandler_WithData(t *testing.T) {
	db := setupTestDB(t)
	userID := insertUser(t, db, "user@test.com")
	user := &auth.User{ID: userID, Email: "user@test.com", Name: "User"}

	if _, err := db.Exec(`
		INSERT INTO streaks (user_id, streak_type, current_count, longest_count, last_activity)
		VALUES
			(?, 'daily_workout',  5, 12, '2026-03-24'),
			(?, 'weekly_workout', 3,  7, '2026-03-22')
	`, userID, userID); err != nil {
		t.Fatalf("seed streaks: %v", err)
	}

	handler := StreaksHandler(db)
	r := withUser(newRequest(http.MethodGet, "/api/stars/streaks"), user)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp StreaksResponse
	decode(t, w.Body.Bytes(), &resp)

	if resp.DailyWorkout.CurrentCount != 5 {
		t.Errorf("daily_workout.current_count = %d, want 5", resp.DailyWorkout.CurrentCount)
	}
	if resp.DailyWorkout.LongestCount != 12 {
		t.Errorf("daily_workout.longest_count = %d, want 12", resp.DailyWorkout.LongestCount)
	}
	if resp.DailyWorkout.LastActivity != "2026-03-24" {
		t.Errorf("daily_workout.last_activity = %q, want '2026-03-24'", resp.DailyWorkout.LastActivity)
	}
	if resp.WeeklyWorkout.CurrentCount != 3 {
		t.Errorf("weekly_workout.current_count = %d, want 3", resp.WeeklyWorkout.CurrentCount)
	}
	if resp.WeeklyWorkout.LongestCount != 7 {
		t.Errorf("weekly_workout.longest_count = %d, want 7", resp.WeeklyWorkout.LongestCount)
	}
	if resp.WeeklyWorkout.LastActivity != "2026-03-22" {
		t.Errorf("weekly_workout.last_activity = %q, want '2026-03-22'", resp.WeeklyWorkout.LastActivity)
	}
}

func TestBadgesHandler_Empty(t *testing.T) {
	db := badgeTestDB(t)
	userID := insertUser(t, db, "user@test.com")
	user := &auth.User{ID: userID, Email: "user@test.com", Name: "User"}

	handler := BadgesHandler(db)
	r := withUser(newRequest(http.MethodGet, "/api/stars/badges"), user)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp []BadgeResponse
	decode(t, w.Body.Bytes(), &resp)

	if resp == nil {
		t.Error("expected non-nil badges slice")
	}
	if len(resp) != 0 {
		t.Errorf("expected 0 badges, got %d", len(resp))
	}
}

func TestBadgesHandler_WithEarnedBadge(t *testing.T) {
	db := badgeTestDB(t)
	userID := insertUser(t, db, "user@test.com")
	user := &auth.User{ID: userID, Email: "user@test.com", Name: "User"}

	earnedAt := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(`
		INSERT INTO badge_definitions (key, name, description, category, tier, icon, xp_reward)
		VALUES ('badge_first_km', 'First Kilometer', 'Complete your first 1km workout.', 'distance', 'bronze', '🏃', 5)
	`); err != nil {
		t.Fatalf("seed badge_definitions: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO user_badges (user_id, badge_key, earned_at)
		VALUES (?, 'badge_first_km', ?)
	`, userID, earnedAt); err != nil {
		t.Fatalf("seed user_badges: %v", err)
	}

	handler := BadgesHandler(db)
	r := withUser(newRequest(http.MethodGet, "/api/stars/badges"), user)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp []BadgeResponse
	decode(t, w.Body.Bytes(), &resp)

	if len(resp) != 1 {
		t.Fatalf("expected 1 badge, got %d", len(resp))
	}
	b := resp[0]
	if b.Key != "badge_first_km" {
		t.Errorf("key = %q, want 'badge_first_km'", b.Key)
	}
	if b.Tier != "bronze" {
		t.Errorf("tier = %q, want 'bronze'", b.Tier)
	}
	if b.AwardedAt != earnedAt {
		t.Errorf("awarded_at = %q, want %q", b.AwardedAt, earnedAt)
	}
}

func TestAvailableBadgesHandler_Empty(t *testing.T) {
	db := badgeTestDB(t)
	userID := insertUser(t, db, "user@test.com")
	user := &auth.User{ID: userID, Email: "user@test.com", Name: "User"}

	handler := AvailableBadgesHandler(db)
	r := withUser(newRequest(http.MethodGet, "/api/stars/badges/available"), user)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp []AvailableBadgeResponse
	decode(t, w.Body.Bytes(), &resp)

	if resp == nil {
		t.Error("expected non-nil available badges slice")
	}
	if len(resp) != 0 {
		t.Errorf("expected 0 available badges, got %d", len(resp))
	}
}

func TestAvailableBadgesHandler_EarnedAndUnearnedAndSecret(t *testing.T) {
	db := badgeTestDB(t)
	userID := insertUser(t, db, "user@test.com")
	user := &auth.User{ID: userID, Email: "user@test.com", Name: "User"}

	earnedAt := time.Now().UTC().Format(time.RFC3339)
	// Seed: one public earned, one public unearned, one secret unearned.
	if _, err := db.Exec(`
		INSERT INTO badge_definitions (key, name, description, category, tier, icon, xp_reward)
		VALUES
			('badge_first_km',      'First Kilometer', 'desc', 'distance', 'bronze', '🏃', 5),
			('badge_5k',            '5K Finisher',     'desc', 'distance', 'bronze', '🥈', 10),
			('badge_secret_one',    'Secret One',      'desc', 'secret',   'silver', '🤫', 20)
	`); err != nil {
		t.Fatalf("seed badge_definitions: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO user_badges (user_id, badge_key, earned_at)
		VALUES (?, 'badge_first_km', ?)
	`, userID, earnedAt); err != nil {
		t.Fatalf("seed user_badges: %v", err)
	}

	handler := AvailableBadgesHandler(db)
	r := withUser(newRequest(http.MethodGet, "/api/stars/badges/available"), user)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp []AvailableBadgeResponse
	decode(t, w.Body.Bytes(), &resp)

	// Secret unearned badge must be filtered out; two public badges remain.
	if len(resp) != 2 {
		t.Fatalf("expected 2 badges (secret filtered), got %d", len(resp))
	}

	byKey := make(map[string]AvailableBadgeResponse)
	for _, b := range resp {
		byKey[b.Key] = b
	}

	earned, ok := byKey["badge_first_km"]
	if !ok {
		t.Fatal("badge_first_km missing from response")
	}
	if !earned.Earned {
		t.Error("badge_first_km should be earned=true")
	}
	if earned.AwardedAt != earnedAt {
		t.Errorf("badge_first_km awarded_at = %q, want %q", earned.AwardedAt, earnedAt)
	}
	if earned.Tier != "bronze" {
		t.Errorf("badge_first_km tier = %q, want 'bronze'", earned.Tier)
	}

	unearned, ok := byKey["badge_5k"]
	if !ok {
		t.Fatal("badge_5k missing from response")
	}
	if unearned.Earned {
		t.Error("badge_5k should be earned=false")
	}
	if unearned.AwardedAt != "" {
		t.Errorf("badge_5k awarded_at should be empty, got %q", unearned.AwardedAt)
	}
}

// ── Reward/claim handler helpers ────────────────────────────────────────────

// withChiParam injects a chi route parameter into the request context.
func withChiParam(r *http.Request, key, value string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, value)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

// setupRewardsTestDB extends setupTestDB with family_rewards + reward_claims tables.
func setupRewardsTestDB(t *testing.T) *sql.DB {
	t.Helper()
	t.Setenv("ENCRYPTION_KEY", "test-encryption-key-stars-rewards")
	encryption.ResetEncryptionKey()
	t.Cleanup(func() { encryption.ResetEncryptionKey() })

	db := setupTestDB(t)

	schema := `
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
	`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("create rewards schema: %v", err)
	}
	return db
}

// insertReward inserts an active reward for the given parent and returns its ID.
func insertReward(t *testing.T, db *sql.DB, parentID int64, title string, starCost int) int64 {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := db.Exec(`
		INSERT INTO family_rewards (parent_id, title, description, star_cost, icon_emoji, is_active, created_at, updated_at)
		VALUES (?, ?, '', ?, '🎁', 1, ?, ?)
	`, parentID, "enc:"+title, starCost, now, now)
	if err != nil {
		t.Fatalf("insertReward: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

// seedBalance sets a star balance for a user.
func seedBalance(t *testing.T, db *sql.DB, userID int64, earned, spent int) {
	t.Helper()
	if _, err := db.Exec(`
		INSERT INTO star_balances (user_id, total_earned, total_spent) VALUES (?, ?, ?)
	`, userID, earned, spent); err != nil {
		t.Fatalf("seedBalance: %v", err)
	}
}

// ── KidRewardsHandler tests ─────────────────────────────────────────────────

func TestKidRewardsHandler_NoParent(t *testing.T) {
	db := setupRewardsTestDB(t)
	userID := insertUser(t, db, "kid@test.com")
	user := &auth.User{ID: userID}

	handler := KidRewardsHandler(db)
	r := withUser(newRequest(http.MethodGet, "/api/stars/rewards"), user)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Rewards []any `json:"rewards"`
	}
	decode(t, w.Body.Bytes(), &resp)
	if len(resp.Rewards) != 0 {
		t.Errorf("expected empty rewards for kid with no parent, got %d", len(resp.Rewards))
	}
}

func TestKidRewardsHandler_WithRewards(t *testing.T) {
	db := setupRewardsTestDB(t)
	parentID := insertUser(t, db, "parent@test.com")
	childID := insertUser(t, db, "child@test.com")
	linkChild(t, db, parentID, childID)
	seedBalance(t, db, childID, 20, 0)

	insertReward(t, db, parentID, "Ice Cream", 10)
	insertReward(t, db, parentID, "Movie Night", 25) // can't afford

	child := &auth.User{ID: childID}
	handler := KidRewardsHandler(db)
	r := withUser(newRequest(http.MethodGet, "/api/stars/rewards"), child)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Rewards []struct {
			StarCost    int  `json:"star_cost"`
			CanAfford   bool `json:"can_afford"`
			TimesClaimed int  `json:"times_claimed"`
		} `json:"rewards"`
	}
	decode(t, w.Body.Bytes(), &resp)
	if len(resp.Rewards) != 2 {
		t.Fatalf("expected 2 rewards, got %d", len(resp.Rewards))
	}
	// First reward costs 10; balance=20 → can afford.
	affordable := 0
	for _, rw := range resp.Rewards {
		if rw.CanAfford {
			affordable++
		}
	}
	if affordable != 1 {
		t.Errorf("expected 1 affordable reward, got %d", affordable)
	}
}

// ── KidClaimsHandler tests ───────────────────────────────────────────────────

func TestKidClaimsHandler_Empty(t *testing.T) {
	db := setupRewardsTestDB(t)
	userID := insertUser(t, db, "kid@test.com")
	user := &auth.User{ID: userID}

	handler := KidClaimsHandler(db)
	r := withUser(newRequest(http.MethodGet, "/api/stars/claims"), user)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Claims []any `json:"claims"`
	}
	decode(t, w.Body.Bytes(), &resp)
	if len(resp.Claims) != 0 {
		t.Errorf("expected 0 claims, got %d", len(resp.Claims))
	}
}

// ── ClaimRewardHandler tests ─────────────────────────────────────────────────

func TestClaimRewardHandler_NotFound(t *testing.T) {
	db := setupRewardsTestDB(t)
	userID := insertUser(t, db, "kid@test.com")
	user := &auth.User{ID: userID}

	handler := ClaimRewardHandler(db)
	r := withUser(withChiParam(newRequest(http.MethodPost, "/api/stars/rewards/999/claim"), "id", "999"), user)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for missing reward, got %d", w.Code)
	}
}

// ── GetChildSettingsHandler tests ────────────────────────────────────────────

func TestGetChildSettingsHandler_Defaults(t *testing.T) {
	db := setupTestDB(t)
	parentID := insertUser(t, db, "parent@test.com")
	childID := insertUser(t, db, "child@test.com")
	linkChild(t, db, parentID, childID)

	parent := &auth.User{ID: parentID}
	handler := GetChildSettingsHandler(db)
	r := withUser(withChiParam(newRequest(http.MethodGet, "/api/family/children/1/settings"), "id", fmt.Sprint(childID)), parent)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp ChildWeeklySettings
	decode(t, w.Body.Bytes(), &resp)

	if resp.WeeklyDistanceTargetKm != defaultWeeklyDistanceTargetKm {
		t.Errorf("distance_target = %v, want %v", resp.WeeklyDistanceTargetKm, defaultWeeklyDistanceTargetKm)
	}
	if resp.WeeklyDurationTargetMin != defaultWeeklyDurationTargetMin {
		t.Errorf("duration_target = %d, want %d", resp.WeeklyDurationTargetMin, defaultWeeklyDurationTargetMin)
	}
}

func TestGetChildSettingsHandler_Forbidden(t *testing.T) {
	db := setupTestDB(t)
	otherID := insertUser(t, db, "other@test.com")
	childID := insertUser(t, db, "child@test.com")
	// No family link — otherID is not the parent.

	other := &auth.User{ID: otherID}
	handler := GetChildSettingsHandler(db)
	r := withUser(withChiParam(newRequest(http.MethodGet, "/api/family/children/1/settings"), "id", fmt.Sprint(childID)), other)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 for non-parent, got %d", w.Code)
	}
}

func TestGetChildSettingsHandler_InvalidID(t *testing.T) {
	db := setupTestDB(t)
	parentID := insertUser(t, db, "parent@test.com")

	parent := &auth.User{ID: parentID}
	handler := GetChildSettingsHandler(db)
	r := withUser(withChiParam(newRequest(http.MethodGet, "/api/family/children/abc/settings"), "id", "abc"), parent)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid ID, got %d", w.Code)
	}
}

// ── PutChildSettingsHandler tests ────────────────────────────────────────────

func TestPutChildSettingsHandler_UpdateBoth(t *testing.T) {
	db := setupTestDB(t)
	parentID := insertUser(t, db, "parent@test.com")
	childID := insertUser(t, db, "child@test.com")
	linkChild(t, db, parentID, childID)

	parent := &auth.User{ID: parentID}
	handler := PutChildSettingsHandler(db)
	body := `{"weekly_distance_target_km": 15.5, "weekly_duration_target_min": 200}`
	req := httptest.NewRequest(http.MethodPut, "/api/family/children/1/settings", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r := withUser(withChiParam(req, "id", fmt.Sprint(childID)), parent)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp ChildWeeklySettings
	decode(t, w.Body.Bytes(), &resp)

	if resp.WeeklyDistanceTargetKm != 15.5 {
		t.Errorf("distance_target = %v, want 15.5", resp.WeeklyDistanceTargetKm)
	}
	if resp.WeeklyDurationTargetMin != 200 {
		t.Errorf("duration_target = %d, want 200", resp.WeeklyDurationTargetMin)
	}
}

func TestPutChildSettingsHandler_PartialUpdate(t *testing.T) {
	db := setupTestDB(t)
	parentID := insertUser(t, db, "parent@test.com")
	childID := insertUser(t, db, "child@test.com")
	linkChild(t, db, parentID, childID)

	parent := &auth.User{ID: parentID}
	handler := PutChildSettingsHandler(db)
	// Only update distance; duration should remain at default.
	body := `{"weekly_distance_target_km": 20.0}`
	req := httptest.NewRequest(http.MethodPut, "/api/family/children/1/settings", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r := withUser(withChiParam(req, "id", fmt.Sprint(childID)), parent)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp ChildWeeklySettings
	decode(t, w.Body.Bytes(), &resp)

	if resp.WeeklyDistanceTargetKm != 20.0 {
		t.Errorf("distance_target = %v, want 20.0", resp.WeeklyDistanceTargetKm)
	}
	if resp.WeeklyDurationTargetMin != defaultWeeklyDurationTargetMin {
		t.Errorf("duration_target = %d, want default %d", resp.WeeklyDurationTargetMin, defaultWeeklyDurationTargetMin)
	}
}

func TestPutChildSettingsHandler_NegativeDistance(t *testing.T) {
	db := setupTestDB(t)
	parentID := insertUser(t, db, "parent@test.com")
	childID := insertUser(t, db, "child@test.com")
	linkChild(t, db, parentID, childID)

	parent := &auth.User{ID: parentID}
	handler := PutChildSettingsHandler(db)
	body := `{"weekly_distance_target_km": -5.0}`
	req := httptest.NewRequest(http.MethodPut, "/api/family/children/1/settings", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r := withUser(withChiParam(req, "id", fmt.Sprint(childID)), parent)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for negative distance, got %d", w.Code)
	}
}

func TestPutChildSettingsHandler_ZeroDuration(t *testing.T) {
	db := setupTestDB(t)
	parentID := insertUser(t, db, "parent@test.com")
	childID := insertUser(t, db, "child@test.com")
	linkChild(t, db, parentID, childID)

	parent := &auth.User{ID: parentID}
	handler := PutChildSettingsHandler(db)
	body := `{"weekly_duration_target_min": 0}`
	req := httptest.NewRequest(http.MethodPut, "/api/family/children/1/settings", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r := withUser(withChiParam(req, "id", fmt.Sprint(childID)), parent)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for zero duration, got %d", w.Code)
	}
}

func TestPutChildSettingsHandler_Forbidden(t *testing.T) {
	db := setupTestDB(t)
	otherID := insertUser(t, db, "other@test.com")
	childID := insertUser(t, db, "child@test.com")

	other := &auth.User{ID: otherID}
	handler := PutChildSettingsHandler(db)
	body := `{"weekly_distance_target_km": 10.0}`
	req := httptest.NewRequest(http.MethodPut, "/api/family/children/1/settings", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r := withUser(withChiParam(req, "id", fmt.Sprint(childID)), other)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 for non-parent, got %d", w.Code)
	}
}

func TestPutChildSettingsHandler_InvalidJSON(t *testing.T) {
	db := setupTestDB(t)
	parentID := insertUser(t, db, "parent@test.com")
	childID := insertUser(t, db, "child@test.com")
	linkChild(t, db, parentID, childID)

	parent := &auth.User{ID: parentID}
	handler := PutChildSettingsHandler(db)
	req := httptest.NewRequest(http.MethodPut, "/api/family/children/1/settings", strings.NewReader("not-json"))
	req.Header.Set("Content-Type", "application/json")
	r := withUser(withChiParam(req, "id", fmt.Sprint(childID)), parent)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid JSON, got %d", w.Code)
	}
}

func TestWeeklyBonusSummaryHandler_Empty(t *testing.T) {
	db := setupTestDB(t)
	userID := insertUser(t, db, "user@test.com")
	user := &auth.User{ID: userID, Email: "user@test.com", Name: "User"}

	handler := WeeklyBonusSummaryHandler(db)
	r := withUser(newRequest(http.MethodGet, "/api/stars/weekly-bonus-summary"), user)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp WeeklyBonusSummaryResponse
	decode(t, w.Body.Bytes(), &resp)

	if resp.Bonuses == nil {
		t.Error("expected non-nil bonuses slice")
	}
	if len(resp.Bonuses) != 0 {
		t.Errorf("expected 0 bonuses for new user, got %d", len(resp.Bonuses))
	}
	if resp.TotalStars != 0 {
		t.Errorf("expected 0 total_stars, got %d", resp.TotalStars)
	}
	if resp.PerfectWeek {
		t.Error("expected perfect_week=false for new user")
	}
}

func TestWeeklyBonusSummaryHandler_WithData(t *testing.T) {
	db := setupTestDB(t)
	userID := insertUser(t, db, "user@test.com")
	user := &auth.User{ID: userID, Email: "user@test.com", Name: "User"}

	lastWeekKey := weekKey(time.Now().UTC().AddDate(0, 0, -7))

	// Insert bonus transactions for last week using expected reason keys.
	for _, args := range []struct {
		reason string
		desc   string
		amount int
	}{
		{"active_every_day_" + lastWeekKey, "Active every day", 5},
		{"distance_goal_" + lastWeekKey, "Distance goal reached", 3},
		{"perfect_week_" + lastWeekKey, "Perfect week!", 10},
	} {
		if _, err := db.Exec(`
			INSERT INTO star_transactions (user_id, amount, reason, description, reference_id, created_at)
			VALUES (?, ?, ?, ?, NULL, datetime('now'))
		`, userID, args.amount, args.reason, args.desc); err != nil {
			t.Fatalf("insert transaction %q: %v", args.reason, err)
		}
	}

	handler := WeeklyBonusSummaryHandler(db)
	r := withUser(newRequest(http.MethodGet, "/api/stars/weekly-bonus-summary"), user)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp WeeklyBonusSummaryResponse
	decode(t, w.Body.Bytes(), &resp)

	if len(resp.Bonuses) != 3 {
		t.Errorf("expected 3 bonuses, got %d", len(resp.Bonuses))
	}
	if resp.TotalStars != 18 {
		t.Errorf("expected total_stars=18, got %d", resp.TotalStars)
	}
	if !resp.PerfectWeek {
		t.Error("expected perfect_week=true when perfect_week_ transaction present")
	}
	if resp.WeekKey != lastWeekKey {
		t.Errorf("expected week_key=%q, got %q", lastWeekKey, resp.WeekKey)
	}
}

func TestClaimRewardHandler_InsufficientStars(t *testing.T) {
	db := setupRewardsTestDB(t)
	parentID := insertUser(t, db, "parent@test.com")
	childID := insertUser(t, db, "child@test.com")
	linkChild(t, db, parentID, childID)
	seedBalance(t, db, childID, 3, 0) // only 3 stars

	rewardID := insertReward(t, db, parentID, "Expensive Prize", 10)

	child := &auth.User{ID: childID}
	handler := ClaimRewardHandler(db)
	r := withUser(withChiParam(
		newRequest(http.MethodPost, "/api/stars/rewards/1/claim"),
		"id", fmt.Sprint(rewardID),
	), child)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected 422 for insufficient stars, got %d: %s", w.Code, w.Body.String())
	}
}
