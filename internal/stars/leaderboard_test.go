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

	lb, err := GetWeeklyLeaderboard(context.Background(), db, parentID)
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

	// Link children to parent with plaintext nicknames (test DB skips encryption for simplicity).
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(`
		INSERT INTO family_links (parent_id, child_id, nickname, avatar_emoji, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, parentID, child1ID, "Alice", "⭐", now); err != nil {
		t.Fatalf("link child1: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO family_links (parent_id, child_id, nickname, avatar_emoji, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, parentID, child2ID, "Bob", "🌟", now); err != nil {
		t.Fatalf("link child2: %v", err)
	}

	// Insert star transactions: child1 earns 10, child2 earns 30 (this week).
	weekStr := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(`
		INSERT INTO star_transactions (user_id, amount, reason, reference_id, created_at)
		VALUES (?, 10, 'workout', 1, ?), (?, 30, 'workout', 2, ?)
	`, child1ID, weekStr, child2ID, weekStr); err != nil {
		t.Fatalf("insert transactions: %v", err)
	}

	lb, err := GetWeeklyLeaderboard(context.Background(), db, parentID)
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

	lb, err := GetAllTimeLeaderboard(context.Background(), db, parentID)
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
	// The child's own entry should appear (the leaderboard shows all siblings).
	if len(lb.Entries) != 1 {
		t.Errorf("expected 1 entry, got %d", len(lb.Entries))
	}
}
