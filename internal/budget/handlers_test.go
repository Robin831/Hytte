package budget

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/go-chi/chi/v5"
	_ "modernc.org/sqlite"
)

// withUser injects an authenticated user into the request context.
func withUser(r *http.Request, userID int64) *http.Request {
	user := &auth.User{ID: userID, Email: "test@example.com", Name: "Test"}
	ctx := auth.ContextWithUser(r.Context(), user)
	return r.WithContext(ctx)
}

// withChiParam injects a chi URL parameter into the request context.
func withChiParam(r *http.Request, key, value string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, value)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

// -- Accounts handler tests --

func TestAccountsListHandler_Empty(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("GET", "/api/budget/accounts", nil), 1)
	rec := httptest.NewRecorder()
	AccountsListHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Accounts []Account `json:"accounts"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Accounts) != 0 {
		t.Errorf("expected 0 accounts, got %d", len(body.Accounts))
	}
}

func TestAccountsCreateHandler_Success(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"name":"Main Checking","type":"checking","currency":"NOK","balance":0}`
	req := withUser(httptest.NewRequest("POST", "/api/budget/accounts", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	AccountsCreateHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Account Account `json:"account"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Account.Name != "Main Checking" {
		t.Errorf("name = %q, want %q", body.Account.Name, "Main Checking")
	}
	if body.Account.ID == 0 {
		t.Error("expected non-zero ID")
	}
}

func TestAccountsCreateHandler_MissingName(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"type":"checking"}`
	req := withUser(httptest.NewRequest("POST", "/api/budget/accounts", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	AccountsCreateHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestAccountsCreateHandler_InvalidBody(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("POST", "/api/budget/accounts", strings.NewReader("not json")), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	AccountsCreateHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestAccountsDeleteHandler_Success(t *testing.T) {
	db := setupTestDB(t)

	a := &Account{Name: "To Delete", Type: AccountTypeChecking, Currency: "NOK"}
	if err := CreateAccount(db, 1, a); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	req := withUser(httptest.NewRequest("DELETE", fmt.Sprintf("/api/budget/accounts/%d", a.ID), nil), 1)
	req = withChiParam(req, "id", fmt.Sprintf("%d", a.ID))
	rec := httptest.NewRecorder()
	AccountsDeleteHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAccountsDeleteHandler_NotFound(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("DELETE", "/api/budget/accounts/999", nil), 1)
	req = withChiParam(req, "id", "999")
	rec := httptest.NewRecorder()
	AccountsDeleteHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

// -- Transaction handler tests --

func TestTransactionsListHandler_Empty(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("GET", "/api/budget/transactions?month=2026-01", nil), 1)
	rec := httptest.NewRecorder()
	TransactionsListHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Transactions []Transaction `json:"transactions"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Transactions) != 0 {
		t.Errorf("expected 0 transactions, got %d", len(body.Transactions))
	}
}

func TestTransactionsListHandler_InvalidMonth(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("GET", "/api/budget/transactions?month=bad", nil), 1)
	rec := httptest.NewRecorder()
	TransactionsListHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestTransactionsCreateHandler_Success(t *testing.T) {
	db := setupTestDB(t)
	accID := createTestAccount(t, db)

	payload, _ := json.Marshal(map[string]any{
		"account_id":  accID,
		"amount":      -150.0,
		"description": "Lunch",
		"date":        "2026-01-10",
	})
	req := withUser(httptest.NewRequest("POST", "/api/budget/transactions", strings.NewReader(string(payload))), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	TransactionsCreateHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Transaction Transaction `json:"transaction"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Transaction.Description != "Lunch" {
		t.Errorf("description = %q, want %q", body.Transaction.Description, "Lunch")
	}
}

func TestTransactionsCreateHandler_MissingAccountID(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"amount":-100,"description":"Coffee","date":"2026-01-10"}`
	req := withUser(httptest.NewRequest("POST", "/api/budget/transactions", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	TransactionsCreateHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestTransactionsDeleteHandler_NotFound(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("DELETE", "/api/budget/transactions/999", nil), 1)
	req = withChiParam(req, "id", "999")
	rec := httptest.NewRecorder()
	TransactionsDeleteHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

// -- Summary handler tests --

func TestSummaryHandler_MissingMonth(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("GET", "/api/budget/summary", nil), 1)
	rec := httptest.NewRecorder()
	SummaryHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestSummaryHandler_Success(t *testing.T) {
	db := setupTestDB(t)
	accID := createTestAccount(t, db)

	tx := &Transaction{AccountID: accID, Amount: -200, Description: "Groceries", Date: "2026-01-15"}
	if err := CreateTransaction(db, 1, tx); err != nil {
		t.Fatalf("CreateTransaction: %v", err)
	}

	req := withUser(httptest.NewRequest("GET", "/api/budget/summary?month=2026-01", nil), 1)
	rec := httptest.NewRecorder()
	SummaryHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body MonthlySummary
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Month != "2026-01" {
		t.Errorf("month = %q, want %q", body.Month, "2026-01")
	}
	if body.ExpenseTotal != 200 {
		t.Errorf("expense_total = %v, want 200", body.ExpenseTotal)
	}
	if body.IncomeTotal != 0 {
		t.Errorf("income_total = %v, want 0", body.IncomeTotal)
	}
}

func TestSummaryHandler_InvalidMonth(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("GET", "/api/budget/summary?month=2026-13", nil), 1)
	rec := httptest.NewRecorder()
	SummaryHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}
