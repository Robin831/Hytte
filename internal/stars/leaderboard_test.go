package stars

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
)

func TestWeekStart(t *testing.T) {
	// 2026-03-26 is a Thursday. Week start (Monday) should be 2026-03-23.
	thursday := time.Date(2026, 3, 26, 14, 30, 0, 0, time.UTC)
	got := weekStart(thursday)
	want := time.Date(2026, 3, 23, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("weekStart(%v) = %v, want %v", thursday, got, want)
	}

	// Monday should return itself.
	monday := time.Date(2026, 3, 23, 8, 0, 0, 0, time.UTC)
	got = weekStart(monday)
	if !got.Equal(want) {
		t.Errorf("weekStart(monday %v) = %v, want %v", monday, got, want)
	}

	// Sunday should return the previous Monday.
	sunday := time.Date(2026, 3, 29, 23, 59, 0, 0, time.UTC)
	wantSunday := time.Date(2026, 3, 23, 0, 0, 0, 0, time.UTC)
	got = weekStart(sunday)
	if !got.Equal(wantSunday) {
		t.Errorf("weekStart(sunday %v) = %v, want %v", sunday, got, wantSunday)
	}
}

func TestMonthStart(t *testing.T) {
	mid := time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC)
	got := monthStart(mid)
	want := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("monthStart(%v) = %v, want %v", mid, got, want)
	}

	first := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	got = monthStart(first)
	if !got.Equal(want) {
		t.Errorf("monthStart(first %v) = %v, want %v", first, got, want)
	}
}

func TestGetWeeklyLeaderboard_NoChildren(t *testing.T) {
	db := setupTestDB(t)
	parentID := insertUser(t, db, "parent@test.com")

	lb, err := GetWeeklyLeaderboard(context.Background(), db, parentID, false)
	if err != nil {
		t.Fatalf("GetWeeklyLeaderboard: %v", err)
	}
	if lb.Period != "weekly" {
		t.Errorf("Period = %q, want %q", lb.Period, "weekly")
	}
	if len(lb.Entries) != 0 {
		t.Errorf("expected 0 entries for parent with no children, got %d", len(lb.Entries))
	}
}

func TestGetWeeklyLeaderboard_RankedByStars(t *testing.T) {
	db := setupTestDB(t)
	parentID := insertUser(t, db, "parentlb@test.com")
	child1ID := insertUser(t, db, "child1lb@test.com")
	child2ID := insertUser(t, db, "child2lb@test.com")

	// Link children to parent with legacy plaintext nicknames; leaderboard uses family.decryptOrPlaintext,
	// which falls back to plaintext when values aren't encrypted.
	linkAt := time.Date(2026, 3, 23, 0, 0, 0, 0, time.UTC).Format(time.RFC3339)
	if _, err := db.Exec(`
		INSERT INTO family_links (parent_id, child_id, nickname, avatar_emoji, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, parentID, child1ID, "Alice", "⭐", linkAt); err != nil {
		t.Fatalf("link child1: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO family_links (parent_id, child_id, nickname, avatar_emoji, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, parentID, child2ID, "Bob", "🌟", linkAt); err != nil {
		t.Fatalf("link child2: %v", err)
	}

	// Use a fixed timestamp in the middle of the week (Wednesday 2026-03-25) so the
	// test is not sensitive to week-boundary crossings at runtime.
	fixedNow := time.Date(2026, 3, 25, 12, 0, 0, 0, time.UTC)
	since := weekStart(fixedNow)
	txStr := fixedNow.Format(time.RFC3339)
	if _, err := db.Exec(`
		INSERT INTO star_transactions (user_id, amount, reason, reference_id, created_at)
		VALUES (?, 10, 'workout', 1, ?), (?, 30, 'workout', 2, ?)
	`, child1ID, txStr, child2ID, txStr); err != nil {
		t.Fatalf("insert transactions: %v", err)
	}

	lb, err := buildLeaderboard(context.Background(), db, parentID, "weekly", since, false)
	if err != nil {
		t.Fatalf("GetWeeklyLeaderboard: %v", err)
	}

	if len(lb.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(lb.Entries))
	}
	// First entry should be child2 (30 stars).
	if lb.Entries[0].Stars != 30 {
		t.Errorf("entries[0].Stars = %d, want 30", lb.Entries[0].Stars)
	}
	if lb.Entries[0].Rank != 1 {
		t.Errorf("entries[0].Rank = %d, want 1", lb.Entries[0].Rank)
	}
	// Second entry should be child1 (10 stars).
	if lb.Entries[1].Stars != 10 {
		t.Errorf("entries[1].Stars = %d, want 10", lb.Entries[1].Stars)
	}
	if lb.Entries[1].Rank != 2 {
		t.Errorf("entries[1].Rank = %d, want 2", lb.Entries[1].Rank)
	}
}

func TestGetAllTimeLeaderboard_UsesBalance(t *testing.T) {
	db := setupTestDB(t)
	parentID := insertUser(t, db, "parentalt@test.com")
	childID := insertUser(t, db, "childalt@test.com")

	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(`
		INSERT INTO family_links (parent_id, child_id, nickname, avatar_emoji, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, parentID, childID, "Kid", "🎯", now); err != nil {
		t.Fatalf("link child: %v", err)
	}

	// Insert balance row.
	if _, err := db.Exec(`
		INSERT INTO star_balances (user_id, total_earned, total_spent)
		VALUES (?, 150, 30)
	`, childID); err != nil {
		t.Fatalf("insert balance: %v", err)
	}

	lb, err := GetAllTimeLeaderboard(context.Background(), db, parentID, false)
	if err != nil {
		t.Fatalf("GetAllTimeLeaderboard: %v", err)
	}

	if len(lb.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(lb.Entries))
	}
	// All-time uses total_earned, not current_balance.
	if lb.Entries[0].Stars != 150 {
		t.Errorf("Stars = %d, want 150 (total_earned)", lb.Entries[0].Stars)
	}
}

func TestGetWeeklyLeaderboard_TiedEntriesAreDeterministic(t *testing.T) {
	db := setupTestDB(t)
	parentID := insertUser(t, db, "parenttied@test.com")
	child1ID := insertUser(t, db, "child1tied@test.com")
	child2ID := insertUser(t, db, "child2tied@test.com")

	linkAt := time.Date(2026, 3, 23, 0, 0, 0, 0, time.UTC).Format(time.RFC3339)
	// Insert "Zara" first so insertion order would naturally put her first.
	if _, err := db.Exec(`
		INSERT INTO family_links (parent_id, child_id, nickname, avatar_emoji, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, parentID, child1ID, "Zara", "⭐", linkAt); err != nil {
		t.Fatalf("link child1: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO family_links (parent_id, child_id, nickname, avatar_emoji, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, parentID, child2ID, "Alice", "🌟", linkAt); err != nil {
		t.Fatalf("link child2: %v", err)
	}

	// Both children earn the same number of stars. Use a fixed timestamp so the
	// test is not sensitive to week-boundary crossings at runtime.
	fixedNow := time.Date(2026, 3, 25, 12, 0, 0, 0, time.UTC)
	since := weekStart(fixedNow)
	txStr := fixedNow.Format(time.RFC3339)
	if _, err := db.Exec(`
		INSERT INTO star_transactions (user_id, amount, reason, reference_id, created_at)
		VALUES (?, 20, 'workout', 1, ?), (?, 20, 'workout', 2, ?)
	`, child1ID, txStr, child2ID, txStr); err != nil {
		t.Fatalf("insert transactions: %v", err)
	}

	lb, err := buildLeaderboard(context.Background(), db, parentID, "weekly", since, false)
	if err != nil {
		t.Fatalf("GetWeeklyLeaderboard: %v", err)
	}
	if len(lb.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(lb.Entries))
	}
	// Tied stars: expect alphabetical order — "Alice" before "Zara".
	if lb.Entries[0].Nickname != "Alice" {
		t.Errorf("entries[0].Nickname = %q, want %q (alphabetical tie-break)", lb.Entries[0].Nickname, "Alice")
	}
	if lb.Entries[1].Nickname != "Zara" {
		t.Errorf("entries[1].Nickname = %q, want %q (alphabetical tie-break)", lb.Entries[1].Nickname, "Zara")
	}
	// Both share rank 1.
	if lb.Entries[0].Rank != 1 || lb.Entries[1].Rank != 1 {
		t.Errorf("tied entries should both have Rank=1, got %d and %d", lb.Entries[0].Rank, lb.Entries[1].Rank)
	}
}

func TestLeaderboardHandler_InvalidPeriod(t *testing.T) {
	db := setupTestDB(t)
	parentID := insertUser(t, db, "parenthandler@test.com")
	user := &auth.User{ID: parentID, Email: "parenthandler@test.com", Name: "Parent"}

	handler := LeaderboardHandler(db)
	r := withUser(newRequest(http.MethodGet, "/api/stars/leaderboard?period=invalid"), user)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestLeaderboardHandler_ParentCaller(t *testing.T) {
	db := setupTestDB(t)
	parentID := insertUser(t, db, "parentcaller@test.com")
	user := &auth.User{ID: parentID, Email: "parentcaller@test.com", Name: "Parent"}

	handler := LeaderboardHandler(db)
	r := withUser(newRequest(http.MethodGet, "/api/stars/leaderboard?period=weekly"), user)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var lb Leaderboard
	decode(t, w.Body.Bytes(), &lb)
	if lb.Period != "weekly" {
		t.Errorf("Period = %q, want %q", lb.Period, "weekly")
	}
}

func TestLeaderboardHandler_ChildCaller(t *testing.T) {
	db := setupTestDB(t)
	parentID := insertUser(t, db, "parentchild@test.com")
	childID := insertUser(t, db, "childcaller@test.com")

	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(`
		INSERT INTO family_links (parent_id, child_id, nickname, avatar_emoji, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, parentID, childID, "Kiddo", "⭐", now); err != nil {
		t.Fatalf("link child: %v", err)
	}

	user := &auth.User{ID: childID, Email: "childcaller@test.com", Name: "Child"}

	handler := LeaderboardHandler(db)
	r := withUser(newRequest(http.MethodGet, "/api/stars/leaderboard?period=alltime"), user)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var lb Leaderboard
	decode(t, w.Body.Bytes(), &lb)
	if lb.Period != "alltime" {
		t.Errorf("Period = %q, want %q", lb.Period, "alltime")
	}
	// The child's own entry plus the parent entry should appear (ParentParticipates defaults to true).
	if len(lb.Entries) != 2 {
		t.Errorf("expected 2 entries (child + parent), got %d", len(lb.Entries))
	}
}

func TestGetMonthlyLeaderboard_RankedByStars(t *testing.T) {
	db := setupTestDB(t)
	parentID := insertUser(t, db, "parentmonthly@test.com")
	child1ID := insertUser(t, db, "child1monthly@test.com")
	child2ID := insertUser(t, db, "child2monthly@test.com")

	// Link children to parent with legacy plaintext nicknames; leaderboard uses family.decryptOrPlaintext,
	// which falls back to plaintext when values aren't encrypted.
	linkAt := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339)
	if _, err := db.Exec(`
		INSERT INTO family_links (parent_id, child_id, nickname, avatar_emoji, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, parentID, child1ID, "Mia", "⭐", linkAt); err != nil {
		t.Fatalf("link child1: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO family_links (parent_id, child_id, nickname, avatar_emoji, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, parentID, child2ID, "Leo", "🌟", linkAt); err != nil {
		t.Fatalf("link child2: %v", err)
	}

	// Use a fixed timestamp mid-month to avoid boundary crossings.
	fixedNow := time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC)
	since := monthStart(fixedNow)
	txStr := fixedNow.Format(time.RFC3339)
	// child1 earns 5, child2 earns 25 this month.
	if _, err := db.Exec(`
		INSERT INTO star_transactions (user_id, amount, reason, reference_id, created_at)
		VALUES (?, 5, 'workout', 10, ?), (?, 25, 'workout', 11, ?)
	`, child1ID, txStr, child2ID, txStr); err != nil {
		t.Fatalf("insert transactions: %v", err)
	}

	lb, err := buildLeaderboard(context.Background(), db, parentID, "monthly", since, false)
	if err != nil {
		t.Fatalf("buildLeaderboard monthly: %v", err)
	}
	if lb.Period != "monthly" {
		t.Errorf("Period = %q, want %q", lb.Period, "monthly")
	}
	if len(lb.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(lb.Entries))
	}
	// child2 (25 stars) should rank first.
	if lb.Entries[0].Stars != 25 {
		t.Errorf("entries[0].Stars = %d, want 25", lb.Entries[0].Stars)
	}
	if lb.Entries[0].Rank != 1 {
		t.Errorf("entries[0].Rank = %d, want 1", lb.Entries[0].Rank)
	}
	if lb.Entries[1].Stars != 5 {
		t.Errorf("entries[1].Stars = %d, want 5", lb.Entries[1].Stars)
	}
	if lb.Entries[1].Rank != 2 {
		t.Errorf("entries[1].Rank = %d, want 2", lb.Entries[1].Rank)
	}
}

func TestLeaderboard_WorkoutCountDistinct(t *testing.T) {
	db := setupTestDB(t)
	parentID := insertUser(t, db, "parentwc@test.com")
	childID := insertUser(t, db, "childwc@test.com")

	linkAt := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339)
	if _, err := db.Exec(`
		INSERT INTO family_links (parent_id, child_id, nickname, avatar_emoji, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, parentID, childID, "Sam", "⭐", linkAt); err != nil {
		t.Fatalf("link child: %v", err)
	}

	// Insert three transactions: two for the same workout (reference_id=42) and one
	// for a different workout (reference_id=99). WorkoutCount must be 2, not 3.
	fixedNow := time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC)
	since := weekStart(fixedNow)
	txStr := fixedNow.Format(time.RFC3339)
	if _, err := db.Exec(`
		INSERT INTO star_transactions (user_id, amount, reason, reference_id, created_at)
		VALUES (?, 5, 'workout', 42, ?),
		       (?, 3, 'bonus',   42, ?),
		       (?, 7, 'workout', 99, ?)
	`, childID, txStr, childID, txStr, childID, txStr); err != nil {
		t.Fatalf("insert transactions: %v", err)
	}

	lb, err := buildLeaderboard(context.Background(), db, parentID, "weekly", since, false)
	if err != nil {
		t.Fatalf("buildLeaderboard: %v", err)
	}
	if len(lb.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(lb.Entries))
	}
	if lb.Entries[0].WorkoutCount != 2 {
		t.Errorf("WorkoutCount = %d, want 2 (two distinct reference_ids)", lb.Entries[0].WorkoutCount)
	}
}

func TestLeaderboard_StreakIsReturned(t *testing.T) {
	db := setupTestDB(t)
	parentID := insertUser(t, db, "parentstreak@test.com")
	childID := insertUser(t, db, "childstreak@test.com")

	linkAt := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339)
	if _, err := db.Exec(`
		INSERT INTO family_links (parent_id, child_id, nickname, avatar_emoji, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, parentID, childID, "Pip", "⭐", linkAt); err != nil {
		t.Fatalf("link child: %v", err)
	}

	// Seed a daily_workout streak of 7.
	if _, err := db.Exec(`
		INSERT INTO streaks (user_id, streak_type, current_count, longest_count, last_activity)
		VALUES (?, 'daily_workout', 7, 10, '2026-03-25')
	`, childID); err != nil {
		t.Fatalf("insert streak: %v", err)
	}

	lb, err := buildLeaderboard(context.Background(), db, parentID, "alltime", time.Time{}, false)
	if err != nil {
		t.Fatalf("buildLeaderboard: %v", err)
	}
	if len(lb.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(lb.Entries))
	}
	if lb.Entries[0].Streak != 7 {
		t.Errorf("Streak = %d, want 7", lb.Entries[0].Streak)
	}
}

func TestLeaderboard_StreakNoRowIsZero(t *testing.T) {
	db := setupTestDB(t)
	parentID := insertUser(t, db, "parentnostreak@test.com")
	childID := insertUser(t, db, "childnostreak@test.com")

	linkAt := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339)
	if _, err := db.Exec(`
		INSERT INTO family_links (parent_id, child_id, nickname, avatar_emoji, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, parentID, childID, "Dot", "⭐", linkAt); err != nil {
		t.Fatalf("link child: %v", err)
	}

	// No streaks row inserted — Streak should default to 0 (sql.ErrNoRows handled).
	lb, err := buildLeaderboard(context.Background(), db, parentID, "alltime", time.Time{}, false)
	if err != nil {
		t.Fatalf("buildLeaderboard: %v", err)
	}
	if len(lb.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(lb.Entries))
	}
	if lb.Entries[0].Streak != 0 {
		t.Errorf("Streak = %d, want 0 when no streak row exists", lb.Entries[0].Streak)
	}
}

func TestBuildLeaderboard_ParentParticipates(t *testing.T) {
	db := setupTestDB(t)
	parentID := insertUser(t, db, "parentpp@test.com")
	childID := insertUser(t, db, "childpp@test.com")

	// Insert the parent's name so it can be resolved in the leaderboard.
	if _, err := db.Exec(`UPDATE users SET name = 'TestParent' WHERE id = ?`, parentID); err != nil {
		t.Fatalf("set parent name: %v", err)
	}

	linkAt := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339)
	if _, err := db.Exec(`
		INSERT INTO family_links (parent_id, child_id, nickname, avatar_emoji, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, parentID, childID, "Kiddo", "⭐", linkAt); err != nil {
		t.Fatalf("link child: %v", err)
	}

	// Parent earns 20 stars, child earns 10.
	fixedNow := time.Date(2026, 3, 25, 12, 0, 0, 0, time.UTC)
	since := weekStart(fixedNow)
	txStr := fixedNow.Format(time.RFC3339)
	if _, err := db.Exec(`
		INSERT INTO star_transactions (user_id, amount, reason, reference_id, created_at)
		VALUES (?, 20, 'workout', 1, ?), (?, 10, 'workout', 2, ?)
	`, parentID, txStr, childID, txStr); err != nil {
		t.Fatalf("insert transactions: %v", err)
	}

	lb, err := buildLeaderboard(context.Background(), db, parentID, "weekly", since, true)
	if err != nil {
		t.Fatalf("buildLeaderboard with parentParticipates: %v", err)
	}
	if len(lb.Entries) != 2 {
		t.Fatalf("expected 2 entries (child + parent), got %d", len(lb.Entries))
	}
	// Parent (20 stars) should rank first.
	if lb.Entries[0].Stars != 20 {
		t.Errorf("entries[0].Stars = %d, want 20 (parent)", lb.Entries[0].Stars)
	}
	if lb.Entries[0].UserID != parentID {
		t.Errorf("entries[0].UserID = %d, want parent %d", lb.Entries[0].UserID, parentID)
	}
	if lb.Entries[0].Nickname != "TestParent" {
		t.Errorf("entries[0].Nickname = %q, want %q", lb.Entries[0].Nickname, "TestParent")
	}
	// Child (10 stars) should rank second.
	if lb.Entries[1].Stars != 10 {
		t.Errorf("entries[1].Stars = %d, want 10 (child)", lb.Entries[1].Stars)
	}
}

func TestLeaderboardHandler_LeaderboardNotVisible(t *testing.T) {
	db := setupTestDB(t)
	parentID := insertUser(t, db, "parentnotvisible@test.com")

	// Set leaderboard_visible to false for this parent.
	if err := SetLeaderboardSetting(db, parentID, "kids_stars_leaderboard_visible", "false"); err != nil {
		t.Fatalf("SetLeaderboardSetting: %v", err)
	}

	user := &auth.User{ID: parentID, Email: "parentnotvisible@test.com", Name: "Parent"}
	handler := LeaderboardHandler(db)
	r := withUser(newRequest(http.MethodGet, "/api/stars/leaderboard?period=weekly"), user)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var lb Leaderboard
	decode(t, w.Body.Bytes(), &lb)
	if lb.LeaderboardVisible {
		t.Error("LeaderboardVisible should be false when kids_stars_leaderboard_visible is false")
	}
}

func TestGetLeaderboardSettings_Defaults(t *testing.T) {
	db := setupTestDB(t)
	parentID := insertUser(t, db, "parentlbsettings@test.com")

	s, err := GetLeaderboardSettings(db, parentID)
	if err != nil {
		t.Fatalf("GetLeaderboardSettings: %v", err)
	}
	if !s.LeaderboardVisible {
		t.Error("LeaderboardVisible default should be true")
	}
	if !s.ParentParticipates {
		t.Error("ParentParticipates default should be true")
	}
}

func TestGetLeaderboardSettings_SetFalse(t *testing.T) {
	db := setupTestDB(t)
	parentID := insertUser(t, db, "parentlbfalse@test.com")

	if err := SetLeaderboardSetting(db, parentID, "kids_stars_leaderboard_visible", "false"); err != nil {
		t.Fatalf("SetLeaderboardSetting visible: %v", err)
	}
	if err := SetLeaderboardSetting(db, parentID, "kids_stars_parent_participates", "false"); err != nil {
		t.Fatalf("SetLeaderboardSetting participates: %v", err)
	}

	s, err := GetLeaderboardSettings(db, parentID)
	if err != nil {
		t.Fatalf("GetLeaderboardSettings: %v", err)
	}
	if s.LeaderboardVisible {
		t.Error("LeaderboardVisible should be false after setting to 'false'")
	}
	if s.ParentParticipates {
		t.Error("ParentParticipates should be false after setting to 'false'")
	}
}
