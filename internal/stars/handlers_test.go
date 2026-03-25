package stars

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
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
	if _, err := db.Exec(`
		INSERT INTO star_balances (user_id, total_earned) VALUES (?, 100)
	`, userID); err != nil {
		t.Fatalf("seed balance: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO user_levels (user_id, xp, level, title) VALUES (?, 200, 3, 'Rising Star')
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
	if resp.Title != "Rising Star" {
		t.Errorf("title = %q, want 'Rising Star'", resp.Title)
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
