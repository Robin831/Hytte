package math

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// linkChild creates a family_links row linking child to parent with the
// given display name. Tests call this directly (bypassing family.CreateLink)
// so they can set a deterministic created_at and avoid encryption noise —
// family.decryptOrPlaintext falls back to plaintext when values are not
// "enc:"-prefixed, which matches legacy data in production.
func linkChild(t *testing.T, db *sql.DB, parentID, childID int64, nickname, avatar string) {
	t.Helper()
	linkAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339)
	if _, err := db.Exec(`
		INSERT INTO family_links (parent_id, child_id, nickname, avatar_emoji, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, parentID, childID, nickname, avatar, linkAt); err != nil {
		t.Fatalf("link child %d→%d: %v", parentID, childID, err)
	}
}

// finishedMarathon inserts a completed Marathon session for userID at a
// specific start time so tests can pin sessions to "last week" vs "this week"
// without time.Now skew.
func finishedMarathon(t *testing.T, db *sql.DB, userID int64, durationMs int64, totalWrong int, startedAt time.Time) int64 {
	t.Helper()
	startStr := startedAt.UTC().Format(time.RFC3339)
	endStr := startedAt.Add(time.Duration(durationMs) * time.Millisecond).UTC().Format(time.RFC3339)
	res, err := db.Exec(`
		INSERT INTO math_sessions
			(user_id, mode, started_at, ended_at, duration_ms, total_correct, total_wrong, score_num)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, userID, ModeMarathon, startStr, endStr, durationMs, MarathonFactCount-totalWrong, totalWrong, MarathonFactCount-totalWrong)
	if err != nil {
		t.Fatalf("insert marathon session: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

// finishedBlitz inserts a completed Blitz session.
func finishedBlitz(t *testing.T, db *sql.DB, userID int64, score int64, startedAt time.Time) int64 {
	t.Helper()
	startStr := startedAt.UTC().Format(time.RFC3339)
	endStr := startedAt.Add(60 * time.Second).UTC().Format(time.RFC3339)
	res, err := db.Exec(`
		INSERT INTO math_sessions
			(user_id, mode, started_at, ended_at, duration_ms, total_correct, total_wrong, score_num)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, userID, ModeBlitz, startStr, endStr, 60000, int(score), 0, score)
	if err != nil {
		t.Fatalf("insert blitz session: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

func TestWeekStartUTC(t *testing.T) {
	// 2026-04-22 is a Wednesday. Monday of that ISO week is 2026-04-20.
	wed := time.Date(2026, 4, 22, 15, 0, 0, 0, time.UTC)
	got := weekStartUTC(wed)
	want := time.Date(2026, 4, 20, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("weekStartUTC(%v) = %v, want %v", wed, got, want)
	}
	// Sunday rolls back to the previous Monday.
	sun := time.Date(2026, 4, 26, 23, 59, 0, 0, time.UTC)
	got = weekStartUTC(sun)
	if !got.Equal(want) {
		t.Errorf("weekStartUTC(sunday %v) = %v, want %v", sun, got, want)
	}
}

func TestBuildLeaderboardInvalidMode(t *testing.T) {
	d := setupTestDB(t)
	svc := NewService(d)
	if _, err := svc.BuildLeaderboard(context.Background(), 1, "mixed", PeriodAll); !errors.Is(err, ErrInvalidMode) {
		t.Errorf("expected ErrInvalidMode, got %v", err)
	}
}

func TestBuildLeaderboardInvalidPeriod(t *testing.T) {
	d := setupTestDB(t)
	svc := NewService(d)
	if _, err := svc.BuildLeaderboard(context.Background(), 1, ModeMarathon, "month"); !errors.Is(err, ErrInvalidPeriod) {
		t.Errorf("expected ErrInvalidPeriod, got %v", err)
	}
}

func TestBuildLeaderboardMarathonAllTime(t *testing.T) {
	d := setupTestDB(t)
	// User 1 is parent, user 2 is child; link them.
	linkChild(t, d, 1, 2, "Alice", "🐼")

	// Parent posts a slower run; child posts a faster run.
	parentStart := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	childStart := time.Date(2026, 3, 2, 10, 0, 0, 0, time.UTC)
	parentSess := finishedMarathon(t, d, 1, 300000, 2, parentStart)
	childSess := finishedMarathon(t, d, 2, 250000, 0, childStart)

	svc := NewService(d)
	lb, err := svc.BuildLeaderboard(context.Background(), 1, ModeMarathon, PeriodAll)
	if err != nil {
		t.Fatalf("BuildLeaderboard: %v", err)
	}
	if len(lb.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(lb.Entries))
	}
	// Child is fastest → rank 1.
	first, second := lb.Entries[0], lb.Entries[1]
	if first.UserID != 2 || first.Score == nil || *first.Score != 250000 {
		t.Errorf("entry[0] = %+v (score=%v), want child with 250000", first, first.Score)
	}
	if first.SessionID == nil || *first.SessionID != childSess {
		t.Errorf("entry[0].SessionID = %v, want %d", first.SessionID, childSess)
	}
	if first.Rank == nil || *first.Rank != 1 {
		t.Errorf("entry[0].Rank = %v, want 1", first.Rank)
	}
	if second.UserID != 1 || second.Score == nil || *second.Score != 300000 {
		t.Errorf("entry[1] = %+v, want parent with 300000", second)
	}
	if second.SessionID == nil || *second.SessionID != parentSess {
		t.Errorf("entry[1].SessionID = %v, want %d", second.SessionID, parentSess)
	}
	if !second.IsParent {
		t.Error("parent entry should have IsParent=true")
	}
}

func TestBuildLeaderboardIncludesMembersWithoutSessions(t *testing.T) {
	d := setupTestDB(t)
	linkChild(t, d, 1, 2, "Alice", "🐼")

	svc := NewService(d)
	lb, err := svc.BuildLeaderboard(context.Background(), 2, ModeBlitz, PeriodAll)
	if err != nil {
		t.Fatalf("BuildLeaderboard: %v", err)
	}
	if len(lb.Entries) != 2 {
		t.Fatalf("expected 2 entries even without sessions, got %d", len(lb.Entries))
	}
	for _, e := range lb.Entries {
		if e.Score != nil {
			t.Errorf("entry %+v should have nil score", e)
		}
		if e.Rank != nil {
			t.Errorf("entry %+v should be unranked", e)
		}
	}
}

func TestBuildLeaderboardBlitzSortsByHighestScore(t *testing.T) {
	d := setupTestDB(t)
	linkChild(t, d, 1, 2, "Alice", "🐼")
	start := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	finishedBlitz(t, d, 1, 42, start)
	// Child has two runs; the best is what should appear.
	finishedBlitz(t, d, 2, 20, start)
	finishedBlitz(t, d, 2, 55, start.Add(time.Hour))

	svc := NewService(d)
	lb, err := svc.BuildLeaderboard(context.Background(), 1, ModeBlitz, PeriodAll)
	if err != nil {
		t.Fatalf("BuildLeaderboard: %v", err)
	}
	if len(lb.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(lb.Entries))
	}
	// Child (55) should rank 1 ahead of parent (42).
	if lb.Entries[0].UserID != 2 || lb.Entries[0].Score == nil || *lb.Entries[0].Score != 55 {
		t.Errorf("entry[0]=%+v, want child with 55", lb.Entries[0])
	}
	if lb.Entries[1].UserID != 1 || lb.Entries[1].Score == nil || *lb.Entries[1].Score != 42 {
		t.Errorf("entry[1]=%+v, want parent with 42", lb.Entries[1])
	}
}

func TestBuildLeaderboardWeeklyExcludesOlderRuns(t *testing.T) {
	d := setupTestDB(t)
	linkChild(t, d, 1, 2, "Alice", "🐼")

	// Use times relative to time.Now() so the test doesn't rely on the
	// current wall-clock falling within a particular ISO week. Eight days
	// ago is guaranteed to be before the Monday that starts the current
	// week (at worst today is Monday, in which case the cutoff is today
	// 00:00 UTC and a run 8 days ago is still excluded).
	now := time.Now().UTC()
	lastWeek := now.Add(-8 * 24 * time.Hour)
	thisWeek := now.Add(-1 * time.Hour)
	finishedMarathon(t, d, 2, 200000, 0, lastWeek) // excluded in weekly view
	thisID := finishedMarathon(t, d, 2, 260000, 1, thisWeek)

	svc := NewService(d)
	// All-time: child entry reflects the faster, older run.
	lbAll, err := svc.BuildLeaderboard(context.Background(), 1, ModeMarathon, PeriodAll)
	if err != nil {
		t.Fatalf("BuildLeaderboard(all): %v", err)
	}
	childAll := findEntry(t, lbAll.Entries, 2)
	if childAll.Score == nil || *childAll.Score != 200000 {
		t.Errorf("all-time child score = %v, want 200000", childAll.Score)
	}

	// Weekly: old run is filtered out; the this-week run shows instead.
	lbWeek, err := svc.BuildLeaderboard(context.Background(), 1, ModeMarathon, PeriodWeek)
	if err != nil {
		t.Fatalf("BuildLeaderboard(week): %v", err)
	}
	childWeek := findEntry(t, lbWeek.Entries, 2)
	if childWeek.Score == nil || *childWeek.Score != 260000 {
		t.Errorf("weekly child score = %v, want 260000", childWeek.Score)
	}
	if childWeek.SessionID == nil || *childWeek.SessionID != thisID {
		t.Errorf("weekly child session = %v, want %d", childWeek.SessionID, thisID)
	}
}

func TestBuildLeaderboardExcludesPartialMarathon(t *testing.T) {
	d := setupTestDB(t)
	linkChild(t, d, 1, 2, "Alice", "🐼")
	start := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	startStr := start.Format(time.RFC3339)
	endStr := start.Add(5 * time.Minute).Format(time.RFC3339)
	// A partial Marathon for the child (only 100 attempts recorded).
	if _, err := d.Exec(`INSERT INTO math_sessions
		(user_id, mode, started_at, ended_at, duration_ms, total_correct, total_wrong, score_num)
		VALUES (2, ?, ?, ?, 100000, 100, 0, 100)`,
		ModeMarathon, startStr, endStr); err != nil {
		t.Fatalf("insert partial: %v", err)
	}

	svc := NewService(d)
	lb, err := svc.BuildLeaderboard(context.Background(), 1, ModeMarathon, PeriodAll)
	if err != nil {
		t.Fatalf("BuildLeaderboard: %v", err)
	}
	child := findEntry(t, lb.Entries, 2)
	if child.Score != nil {
		t.Errorf("partial marathon should not count: score=%v", child.Score)
	}
}

func TestBuildLeaderboardScopedToCallerFamily(t *testing.T) {
	d := setupTestDB(t)
	// Insert a third user unrelated to the family and give them a fast run.
	if _, err := d.Exec(`INSERT INTO users (id, email, name, picture, google_id, created_at) VALUES (3, 'outsider@example.com', 'Outsider', '', 'g3', '')`); err != nil {
		t.Fatalf("insert outsider: %v", err)
	}
	start := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	finishedMarathon(t, d, 3, 100000, 0, start)
	// Parent 1 has a slower run; no link exists to user 3.
	finishedMarathon(t, d, 1, 280000, 0, start)

	svc := NewService(d)
	lb, err := svc.BuildLeaderboard(context.Background(), 1, ModeMarathon, PeriodAll)
	if err != nil {
		t.Fatalf("BuildLeaderboard: %v", err)
	}
	for _, e := range lb.Entries {
		if e.UserID == 3 {
			t.Fatal("outsider leaked into family leaderboard")
		}
	}
	if len(lb.Entries) != 1 || lb.Entries[0].UserID != 1 {
		t.Errorf("expected single parent entry, got %+v", lb.Entries)
	}
}

func TestBuildLeaderboardChildCallerSeesWholeFamily(t *testing.T) {
	d := setupTestDB(t)
	// Parent=1, Child=2. Child is the caller.
	linkChild(t, d, 1, 2, "Alice", "🐼")

	svc := NewService(d)
	lb, err := svc.BuildLeaderboard(context.Background(), 2, ModeBlitz, PeriodAll)
	if err != nil {
		t.Fatalf("BuildLeaderboard: %v", err)
	}
	if len(lb.Entries) != 2 {
		t.Fatalf("expected child to see 2 entries (self + parent), got %d", len(lb.Entries))
	}
	ids := map[int64]bool{lb.Entries[0].UserID: true, lb.Entries[1].UserID: true}
	if !ids[1] || !ids[2] {
		t.Errorf("expected entries for both parent and child, got %v", ids)
	}
}

func TestLeaderboardHandlerUnknownMode(t *testing.T) {
	d := setupTestDB(t)
	h := LeaderboardHandler(d)
	r := withUser(httptest.NewRequest(http.MethodGet, "/api/math/leaderboard?mode=mixed&period=all", nil), testUser)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestLeaderboardHandlerReturnsEntries(t *testing.T) {
	d := setupTestDB(t)
	linkChild(t, d, 1, 2, "Alice", "🐼")
	start := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	finishedBlitz(t, d, 2, 77, start)

	h := LeaderboardHandler(d)
	r := withUser(httptest.NewRequest(http.MethodGet, "/api/math/leaderboard?mode=blitz&period=all", nil), testUser)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var lb Leaderboard
	if err := json.Unmarshal(w.Body.Bytes(), &lb); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if lb.Mode != ModeBlitz || lb.Period != PeriodAll {
		t.Errorf("mode/period mismatch: %+v", lb)
	}
	if len(lb.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(lb.Entries))
	}
	top := lb.Entries[0]
	if top.UserID != 2 || top.Score == nil || *top.Score != 77 {
		t.Errorf("top entry %+v, want child with 77", top)
	}
}

func TestLeaderboardHandlerDefaultsPeriodToAll(t *testing.T) {
	d := setupTestDB(t)
	h := LeaderboardHandler(d)
	r := withUser(httptest.NewRequest(http.MethodGet, "/api/math/leaderboard?mode=marathon", nil), testUser)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var lb Leaderboard
	if err := json.Unmarshal(w.Body.Bytes(), &lb); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if lb.Period != PeriodAll {
		t.Errorf("default period = %q, want %q", lb.Period, PeriodAll)
	}
}

func TestLeaderboardHandlerUnauthorized(t *testing.T) {
	d := setupTestDB(t)
	h := LeaderboardHandler(d)
	// No auth context attached.
	r := httptest.NewRequest(http.MethodGet, "/api/math/leaderboard?mode=marathon&period=all", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

// findEntry returns the leaderboard entry for the given user, or fails the
// test if the user is absent.
func findEntry(t *testing.T, entries []LeaderboardEntry, userID int64) LeaderboardEntry {
	t.Helper()
	for _, e := range entries {
		if e.UserID == userID {
			return e
		}
	}
	t.Fatalf("entry for user %d not found in %+v", userID, entries)
	return LeaderboardEntry{}
}

