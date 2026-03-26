package stars

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
)

func TestDeposit_Basic(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	userID := insertUser(t, db, "child@test.com")

	// Give the user 20 stars to deposit.
	if _, err := db.Exec(`
		INSERT INTO star_balances (user_id, total_earned) VALUES (?, 20)
	`, userID); err != nil {
		t.Fatalf("seed balance: %v", err)
	}

	acc, err := Deposit(ctx, db, userID, 10)
	if err != nil {
		t.Fatalf("Deposit: %v", err)
	}
	if acc.Balance != 10 {
		t.Errorf("savings balance: got %d, want 10", acc.Balance)
	}

	// Main balance should now be 20 - 10 = 10.
	var bal int
	if err := db.QueryRow(`SELECT current_balance FROM star_balances WHERE user_id = ?`, userID).Scan(&bal); err != nil {
		t.Fatalf("query balance: %v", err)
	}
	if bal != 10 {
		t.Errorf("main balance after deposit: got %d, want 10", bal)
	}

	// A negative transaction should have been recorded.
	var txnAmount int
	if err := db.QueryRow(`SELECT amount FROM star_transactions WHERE user_id = ? AND reason = 'savings_deposit'`, userID).Scan(&txnAmount); err != nil {
		t.Fatalf("query transaction: %v", err)
	}
	if txnAmount != -10 {
		t.Errorf("deposit transaction amount: got %d, want -10", txnAmount)
	}
}

func TestDeposit_InsufficientBalance(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	userID := insertUser(t, db, "poor@test.com")

	// Only 5 stars available.
	if _, err := db.Exec(`INSERT INTO star_balances (user_id, total_earned) VALUES (?, 5)`, userID); err != nil {
		t.Fatalf("seed balance: %v", err)
	}

	_, err := Deposit(ctx, db, userID, 10)
	if err == nil {
		t.Fatal("expected error for insufficient balance, got nil")
	}
}

func TestDeposit_InvalidAmount(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	userID := insertUser(t, db, "invalid@test.com")

	_, err := Deposit(ctx, db, userID, 0)
	if err == nil {
		t.Fatal("expected error for zero amount")
	}

	_, err = Deposit(ctx, db, userID, -5)
	if err == nil {
		t.Fatal("expected error for negative amount")
	}
}

func TestRequestWithdrawal_Basic(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	userID := insertUser(t, db, "saver@test.com")

	// Seed 20 stars and deposit 15 into savings.
	if _, err := db.Exec(`INSERT INTO star_balances (user_id, total_earned) VALUES (?, 20)`, userID); err != nil {
		t.Fatalf("seed balance: %v", err)
	}
	if _, err := Deposit(ctx, db, userID, 15); err != nil {
		t.Fatalf("Deposit: %v", err)
	}

	acc, err := RequestWithdrawal(ctx, db, userID, 10)
	if err != nil {
		t.Fatalf("RequestWithdrawal: %v", err)
	}
	if acc.PendingWithdrawal != 10 {
		t.Errorf("pending_withdrawal: got %d, want 10", acc.PendingWithdrawal)
	}
	if acc.WithdrawalAvailableAt == "" {
		t.Error("withdrawal_available_at should be set")
	}

	// Verify the available time is ~24h from now.
	available, err := time.Parse(time.RFC3339, acc.WithdrawalAvailableAt)
	if err != nil {
		t.Fatalf("parse withdrawal_available_at: %v", err)
	}
	diff := available.Sub(time.Now().UTC())
	if diff < 23*time.Hour || diff > 25*time.Hour {
		t.Errorf("withdrawal_available_at diff: got %v, want ~24h", diff)
	}
}

func TestRequestWithdrawal_AlreadyPending(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	userID := insertUser(t, db, "doublewith@test.com")

	if _, err := db.Exec(`INSERT INTO star_balances (user_id, total_earned) VALUES (?, 50)`, userID); err != nil {
		t.Fatalf("seed balance: %v", err)
	}
	if _, err := Deposit(ctx, db, userID, 30); err != nil {
		t.Fatalf("Deposit: %v", err)
	}

	if _, err := RequestWithdrawal(ctx, db, userID, 10); err != nil {
		t.Fatalf("first RequestWithdrawal: %v", err)
	}

	_, err := RequestWithdrawal(ctx, db, userID, 10)
	if err == nil {
		t.Fatal("expected error for duplicate pending withdrawal")
	}
}

func TestCompleteWithdrawal_Before24h(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	userID := insertUser(t, db, "early@test.com")

	if _, err := db.Exec(`INSERT INTO star_balances (user_id, total_earned) VALUES (?, 30)`, userID); err != nil {
		t.Fatalf("seed balance: %v", err)
	}
	if _, err := Deposit(ctx, db, userID, 20); err != nil {
		t.Fatalf("Deposit: %v", err)
	}
	if _, err := RequestWithdrawal(ctx, db, userID, 15); err != nil {
		t.Fatalf("RequestWithdrawal: %v", err)
	}

	// Try to complete before 24h — should fail.
	_, err := CompleteWithdrawal(ctx, db, userID)
	if err == nil {
		t.Fatal("expected error when completing before 24h")
	}
}

func TestCompleteWithdrawal_After24h(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	userID := insertUser(t, db, "timely@test.com")

	if _, err := db.Exec(`INSERT INTO star_balances (user_id, total_earned) VALUES (?, 30)`, userID); err != nil {
		t.Fatalf("seed balance: %v", err)
	}
	if _, err := Deposit(ctx, db, userID, 20); err != nil {
		t.Fatalf("Deposit: %v", err)
	}
	if _, err := RequestWithdrawal(ctx, db, userID, 15); err != nil {
		t.Fatalf("RequestWithdrawal: %v", err)
	}

	// Backdate withdrawal_available_at to simulate 24h having passed.
	past := time.Now().UTC().Add(-25 * time.Hour).Format(time.RFC3339)
	if _, err := db.Exec(`UPDATE star_savings SET withdrawal_available_at = ? WHERE user_id = ?`, past, userID); err != nil {
		t.Fatalf("backdate withdrawal: %v", err)
	}

	acc, err := CompleteWithdrawal(ctx, db, userID)
	if err != nil {
		t.Fatalf("CompleteWithdrawal: %v", err)
	}
	if acc.PendingWithdrawal != 0 {
		t.Errorf("pending_withdrawal after complete: got %d, want 0", acc.PendingWithdrawal)
	}
	if acc.Balance != 5 {
		t.Errorf("savings balance after withdraw: got %d, want 5", acc.Balance)
	}

	// Main balance should be 10 (original 30 - 20 deposited + 15 withdrawn).
	var bal int
	if err := db.QueryRow(`SELECT current_balance FROM star_balances WHERE user_id = ?`, userID).Scan(&bal); err != nil {
		t.Fatalf("query balance: %v", err)
	}
	if bal != 25 {
		t.Errorf("main balance after withdrawal: got %d, want 25", bal)
	}
}

func TestPayInterest_Basic(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	userID := insertUser(t, db, "investor@test.com")

	// Deposit 100 stars into savings.
	if _, err := db.Exec(`INSERT INTO star_balances (user_id, total_earned) VALUES (?, 100)`, userID); err != nil {
		t.Fatalf("seed balance: %v", err)
	}
	if _, err := Deposit(ctx, db, userID, 100); err != nil {
		t.Fatalf("Deposit: %v", err)
	}

	if err := PayInterest(ctx, db); err != nil {
		t.Fatalf("PayInterest: %v", err)
	}

	// Interest = floor(100 * 0.10) = 10.
	acc, err := GetSavingsAccount(ctx, db, userID)
	if err != nil {
		t.Fatalf("GetSavingsAccount: %v", err)
	}
	if acc.Balance != 110 {
		t.Errorf("savings balance after interest: got %d, want 110", acc.Balance)
	}

	// Main balance should have received 10 interest stars.
	var bal int
	if err := db.QueryRow(`SELECT current_balance FROM star_balances WHERE user_id = ?`, userID).Scan(&bal); err != nil {
		t.Fatalf("query balance: %v", err)
	}
	if bal != 10 {
		t.Errorf("main balance after interest: got %d, want 10", bal)
	}
}

func TestPayInterest_CompoundOverMultipleWeeks(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	userID := insertUser(t, db, "compound@test.com")

	// Start with 100 stars in savings.
	if _, err := db.Exec(`INSERT INTO star_balances (user_id, total_earned) VALUES (?, 100)`, userID); err != nil {
		t.Fatalf("seed balance: %v", err)
	}
	if _, err := Deposit(ctx, db, userID, 100); err != nil {
		t.Fatalf("Deposit: %v", err)
	}

	// Week 1: 100 * 0.10 = 10 interest → savings = 110.
	if err := PayInterest(ctx, db); err != nil {
		t.Fatalf("PayInterest week 1: %v", err)
	}

	acc, _ := GetSavingsAccount(ctx, db, userID)
	if acc.Balance != 110 {
		t.Errorf("week 1 savings: got %d, want 110", acc.Balance)
	}

	// Week 2: 110 * 0.10 = 11 interest → savings = 121.
	if err := PayInterest(ctx, db); err != nil {
		t.Fatalf("PayInterest week 2: %v", err)
	}

	acc, _ = GetSavingsAccount(ctx, db, userID)
	if acc.Balance != 121 {
		t.Errorf("week 2 savings: got %d, want 121", acc.Balance)
	}

	// Week 3: 121 * 0.10 = 12 interest → savings = 133.
	if err := PayInterest(ctx, db); err != nil {
		t.Fatalf("PayInterest week 3: %v", err)
	}

	acc, _ = GetSavingsAccount(ctx, db, userID)
	if acc.Balance != 133 {
		t.Errorf("week 3 savings: got %d, want 133", acc.Balance)
	}
}

func TestGetSavingsAccount_Empty(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	userID := insertUser(t, db, "empty@test.com")

	acc, err := GetSavingsAccount(ctx, db, userID)
	if err != nil {
		t.Fatalf("GetSavingsAccount: %v", err)
	}
	if acc.Balance != 0 {
		t.Errorf("balance: got %d, want 0", acc.Balance)
	}
	if acc.PendingWithdrawal != 0 {
		t.Errorf("pending_withdrawal: got %d, want 0", acc.PendingWithdrawal)
	}
}

// --- HTTP handler tests ---

func TestGetSavingsHandler_Empty(t *testing.T) {
	db := setupTestDB(t)
	userID := insertUser(t, db, "handler-get@test.com")
	user := &auth.User{ID: userID, Email: "handler-get@test.com", Name: "Test"}

	handler := GetSavingsHandler(db)
	r := withUser(httptest.NewRequest(http.MethodGet, "/api/stars/savings", nil), user)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var acc SavingsAccount
	if err := json.Unmarshal(w.Body.Bytes(), &acc); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if acc.Balance != 0 {
		t.Errorf("expected zero balance, got %d", acc.Balance)
	}
}

func TestDepositSavingsHandler_Success(t *testing.T) {
	db := setupTestDB(t)
	userID := insertUser(t, db, "handler-deposit@test.com")
	user := &auth.User{ID: userID, Email: "handler-deposit@test.com", Name: "Test"}

	// Seed 50 stars.
	if _, err := db.Exec(`INSERT INTO star_balances (user_id, total_earned) VALUES (?, 50)`, userID); err != nil {
		t.Fatalf("seed balance: %v", err)
	}

	body := strings.NewReader(`{"amount":20}`)
	r := withUser(httptest.NewRequest(http.MethodPost, "/api/stars/savings/deposit", body), user)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	DepositSavingsHandler(db).ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var acc SavingsAccount
	if err := json.Unmarshal(w.Body.Bytes(), &acc); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if acc.Balance != 20 {
		t.Errorf("expected savings balance 20, got %d", acc.Balance)
	}
}

func TestDepositSavingsHandler_InsufficientBalance(t *testing.T) {
	db := setupTestDB(t)
	userID := insertUser(t, db, "handler-deposit-fail@test.com")
	user := &auth.User{ID: userID, Email: "handler-deposit-fail@test.com", Name: "Test"}

	body := strings.NewReader(`{"amount":100}`)
	r := withUser(httptest.NewRequest(http.MethodPost, "/api/stars/savings/deposit", body), user)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	DepositSavingsHandler(db).ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	var errResp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if errResp["error"] == "" {
		t.Error("expected non-empty error message")
	}
}

func TestWithdrawSavingsHandler_RequestWithdrawal(t *testing.T) {
	db := setupTestDB(t)
	userID := insertUser(t, db, "handler-withdraw@test.com")
	user := &auth.User{ID: userID, Email: "handler-withdraw@test.com", Name: "Test"}

	// Seed and deposit 30 stars.
	if _, err := db.Exec(`INSERT INTO star_balances (user_id, total_earned) VALUES (?, 30)`, userID); err != nil {
		t.Fatalf("seed balance: %v", err)
	}
	if _, err := Deposit(context.Background(), db, userID, 30); err != nil {
		t.Fatalf("Deposit: %v", err)
	}

	body := strings.NewReader(`{"amount":15}`)
	r := withUser(httptest.NewRequest(http.MethodPost, "/api/stars/savings/withdraw", body), user)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	WithdrawSavingsHandler(db).ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var acc SavingsAccount
	if err := json.Unmarshal(w.Body.Bytes(), &acc); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if acc.PendingWithdrawal != 15 {
		t.Errorf("expected pending_withdrawal 15, got %d", acc.PendingWithdrawal)
	}
}

func TestWithdrawSavingsHandler_CompleteWithdrawal(t *testing.T) {
	db := setupTestDB(t)
	userID := insertUser(t, db, "handler-complete@test.com")
	user := &auth.User{ID: userID, Email: "handler-complete@test.com", Name: "Test"}

	// Seed, deposit, and request withdrawal.
	if _, err := db.Exec(`INSERT INTO star_balances (user_id, total_earned) VALUES (?, 40)`, userID); err != nil {
		t.Fatalf("seed balance: %v", err)
	}
	if _, err := Deposit(context.Background(), db, userID, 40); err != nil {
		t.Fatalf("Deposit: %v", err)
	}
	if _, err := RequestWithdrawal(context.Background(), db, userID, 20); err != nil {
		t.Fatalf("RequestWithdrawal: %v", err)
	}
	// Backdate so the 24h delay has passed.
	past := time.Now().UTC().Add(-25 * time.Hour).Format(time.RFC3339)
	if _, err := db.Exec(`UPDATE star_savings SET withdrawal_available_at = ? WHERE user_id = ?`, past, userID); err != nil {
		t.Fatalf("backdate: %v", err)
	}

	r := withUser(httptest.NewRequest(http.MethodPost, "/api/stars/savings/withdraw", strings.NewReader(`{}`)), user)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	WithdrawSavingsHandler(db).ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var acc SavingsAccount
	if err := json.Unmarshal(w.Body.Bytes(), &acc); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if acc.PendingWithdrawal != 0 {
		t.Errorf("expected pending_withdrawal 0 after completion, got %d", acc.PendingWithdrawal)
	}
}
