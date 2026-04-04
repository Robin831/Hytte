package budget

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

func TestAccountsCreateHandler_CreditLimit(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"name":"My Visa","type":"credit","currency":"NOK","balance":0,"credit_limit":15000}`
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
	if body.Account.CreditLimit != 15000 {
		t.Errorf("credit_limit = %v, want 15000", body.Account.CreditLimit)
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
	if body.IncomeSplit != defaultIncomeSplit {
		t.Errorf("income_split = %v, want %v (default)", body.IncomeSplit, defaultIncomeSplit)
	}
}

func TestSummaryHandler_WithCustomSplit(t *testing.T) {
	db := setupTestDB(t)
	accID := createTestAccount(t, db)

	// Add an income transaction.
	tx := &Transaction{AccountID: accID, Amount: 50000, Description: "Salary", Date: "2026-02-15"}
	if err := CreateTransaction(db, 1, tx); err != nil {
		t.Fatalf("CreateTransaction: %v", err)
	}

	// Set a custom income split of 70%.
	if err := SetIncomeSplit(db, 1, 70); err != nil {
		t.Fatalf("SetIncomeSplit: %v", err)
	}

	req := withUser(httptest.NewRequest("GET", "/api/budget/summary?month=2026-02", nil), 1)
	rec := httptest.NewRecorder()
	SummaryHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body MonthlySummary
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.IncomeSplit != 70 {
		t.Errorf("income_split = %v, want 70", body.IncomeSplit)
	}
	if body.IncomeTotal != 50000 {
		t.Errorf("income_total = %v, want 50000", body.IncomeTotal)
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

// -- Accounts update handler tests --

func TestAccountsUpdateHandler_Success(t *testing.T) {
	db := setupTestDB(t)
	a := &Account{Name: "Old Name", Type: AccountTypeChecking, Currency: "NOK"}
	if err := CreateAccount(db, 1, a); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	payload := `{"name":"New Name","type":"checking","currency":"NOK"}`
	req := withUser(httptest.NewRequest("PUT", fmt.Sprintf("/api/budget/accounts/%d", a.ID), strings.NewReader(payload)), 1)
	req = withChiParam(req, "id", fmt.Sprintf("%d", a.ID))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	AccountsUpdateHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Account Account `json:"account"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Account.Name != "New Name" {
		t.Errorf("name = %q, want %q", body.Account.Name, "New Name")
	}
}

func TestAccountsUpdateHandler_CreditLimit(t *testing.T) {
	db := setupTestDB(t)
	a := &Account{Name: "My Visa", Type: AccountTypeCredit, Currency: "NOK", CreditLimit: 10000}
	if err := CreateAccount(db, 1, a); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	payload := `{"name":"My Visa","type":"credit","currency":"NOK","credit_limit":25000}`
	req := withUser(httptest.NewRequest("PUT", fmt.Sprintf("/api/budget/accounts/%d", a.ID), strings.NewReader(payload)), 1)
	req = withChiParam(req, "id", fmt.Sprintf("%d", a.ID))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	AccountsUpdateHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Account Account `json:"account"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Account.CreditLimit != 25000 {
		t.Errorf("credit_limit = %v, want 25000", body.Account.CreditLimit)
	}
}

func TestAccountsUpdateHandler_MissingName(t *testing.T) {
	db := setupTestDB(t)
	a := &Account{Name: "Existing", Type: AccountTypeChecking, Currency: "NOK"}
	if err := CreateAccount(db, 1, a); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	payload := `{"type":"checking","currency":"NOK"}`
	req := withUser(httptest.NewRequest("PUT", fmt.Sprintf("/api/budget/accounts/%d", a.ID), strings.NewReader(payload)), 1)
	req = withChiParam(req, "id", fmt.Sprintf("%d", a.ID))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	AccountsUpdateHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestAccountsUpdateHandler_NotFound(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"name":"X","type":"checking","currency":"NOK"}`
	req := withUser(httptest.NewRequest("PUT", "/api/budget/accounts/999", strings.NewReader(payload)), 1)
	req = withChiParam(req, "id", "999")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	AccountsUpdateHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

// -- Categories handler tests --

func TestCategoriesCreateHandler_Success(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"name":"Food","group_name":"Living","icon":"🍔","color":"#ff0000","is_income":false}`
	req := withUser(httptest.NewRequest("POST", "/api/budget/categories", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	CategoriesCreateHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Category Category `json:"category"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Category.Name != "Food" {
		t.Errorf("name = %q, want %q", body.Category.Name, "Food")
	}
}

func TestCategoriesCreateHandler_MissingName(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"group_name":"Living"}`
	req := withUser(httptest.NewRequest("POST", "/api/budget/categories", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	CategoriesCreateHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestCategoriesUpdateHandler_Success(t *testing.T) {
	db := setupTestDB(t)
	c := &Category{Name: "Old Cat", GroupName: "Group", Icon: "", Color: "#aabbcc"}
	if err := CreateCategory(db, 1, c); err != nil {
		t.Fatalf("CreateCategory: %v", err)
	}

	payload := `{"name":"New Cat","group_name":"Group","icon":"","color":"#aabbcc","is_income":false}`
	req := withUser(httptest.NewRequest("PUT", fmt.Sprintf("/api/budget/categories/%d", c.ID), strings.NewReader(payload)), 1)
	req = withChiParam(req, "id", fmt.Sprintf("%d", c.ID))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	CategoriesUpdateHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Category Category `json:"category"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Category.Name != "New Cat" {
		t.Errorf("name = %q, want %q", body.Category.Name, "New Cat")
	}
}

func TestCategoriesUpdateHandler_MissingName(t *testing.T) {
	db := setupTestDB(t)
	c := &Category{Name: "Existing", GroupName: "Group"}
	if err := CreateCategory(db, 1, c); err != nil {
		t.Fatalf("CreateCategory: %v", err)
	}

	payload := `{"group_name":"Group"}`
	req := withUser(httptest.NewRequest("PUT", fmt.Sprintf("/api/budget/categories/%d", c.ID), strings.NewReader(payload)), 1)
	req = withChiParam(req, "id", fmt.Sprintf("%d", c.ID))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	CategoriesUpdateHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestCategoriesUpdateHandler_NotFound(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"name":"X","group_name":"G"}`
	req := withUser(httptest.NewRequest("PUT", "/api/budget/categories/999", strings.NewReader(payload)), 1)
	req = withChiParam(req, "id", "999")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	CategoriesUpdateHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestCategoriesDeleteHandler_Success(t *testing.T) {
	db := setupTestDB(t)
	c := &Category{Name: "To Delete", GroupName: "Group"}
	if err := CreateCategory(db, 1, c); err != nil {
		t.Fatalf("CreateCategory: %v", err)
	}

	req := withUser(httptest.NewRequest("DELETE", fmt.Sprintf("/api/budget/categories/%d", c.ID), nil), 1)
	req = withChiParam(req, "id", fmt.Sprintf("%d", c.ID))
	rec := httptest.NewRecorder()
	CategoriesDeleteHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCategoriesDeleteHandler_NotFound(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("DELETE", "/api/budget/categories/999", nil), 1)
	req = withChiParam(req, "id", "999")
	rec := httptest.NewRecorder()
	CategoriesDeleteHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

// -- Transactions update/delete handler tests --

func TestTransactionsUpdateHandler_Success(t *testing.T) {
	db := setupTestDB(t)
	accID := createTestAccount(t, db)

	tx := &Transaction{AccountID: accID, Amount: -100, Description: "Original", Date: "2026-01-05"}
	if err := CreateTransaction(db, 1, tx); err != nil {
		t.Fatalf("CreateTransaction: %v", err)
	}

	payload, _ := json.Marshal(map[string]any{
		"account_id":  accID,
		"amount":      -200.0,
		"description": "Updated",
		"date":        "2026-01-06",
	})
	req := withUser(httptest.NewRequest("PUT", fmt.Sprintf("/api/budget/transactions/%d", tx.ID), strings.NewReader(string(payload))), 1)
	req = withChiParam(req, "id", fmt.Sprintf("%d", tx.ID))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	TransactionsUpdateHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Transaction Transaction `json:"transaction"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Transaction.Description != "Updated" {
		t.Errorf("description = %q, want %q", body.Transaction.Description, "Updated")
	}
}

func TestTransactionsUpdateHandler_MissingAccountID(t *testing.T) {
	db := setupTestDB(t)
	accID := createTestAccount(t, db)

	tx := &Transaction{AccountID: accID, Amount: -100, Description: "Orig", Date: "2026-01-05"}
	if err := CreateTransaction(db, 1, tx); err != nil {
		t.Fatalf("CreateTransaction: %v", err)
	}

	payload := `{"amount":-200,"description":"No account","date":"2026-01-06"}`
	req := withUser(httptest.NewRequest("PUT", fmt.Sprintf("/api/budget/transactions/%d", tx.ID), strings.NewReader(payload)), 1)
	req = withChiParam(req, "id", fmt.Sprintf("%d", tx.ID))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	TransactionsUpdateHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestTransactionsUpdateHandler_InvalidDate(t *testing.T) {
	db := setupTestDB(t)
	accID := createTestAccount(t, db)

	tx := &Transaction{AccountID: accID, Amount: -100, Description: "Orig", Date: "2026-01-05"}
	if err := CreateTransaction(db, 1, tx); err != nil {
		t.Fatalf("CreateTransaction: %v", err)
	}

	payload, _ := json.Marshal(map[string]any{
		"account_id":  accID,
		"amount":      -200.0,
		"description": "Bad date",
		"date":        "not-a-date",
	})
	req := withUser(httptest.NewRequest("PUT", fmt.Sprintf("/api/budget/transactions/%d", tx.ID), strings.NewReader(string(payload))), 1)
	req = withChiParam(req, "id", fmt.Sprintf("%d", tx.ID))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	TransactionsUpdateHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestTransactionsDeleteHandler_Success(t *testing.T) {
	db := setupTestDB(t)
	accID := createTestAccount(t, db)

	tx := &Transaction{AccountID: accID, Amount: -50, Description: "To delete", Date: "2026-01-10"}
	if err := CreateTransaction(db, 1, tx); err != nil {
		t.Fatalf("CreateTransaction: %v", err)
	}

	req := withUser(httptest.NewRequest("DELETE", fmt.Sprintf("/api/budget/transactions/%d", tx.ID), nil), 1)
	req = withChiParam(req, "id", fmt.Sprintf("%d", tx.ID))
	rec := httptest.NewRecorder()
	TransactionsDeleteHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
}

// -- Budget limits handler tests --

func createTestCategoryForHandler(t *testing.T, db *sql.DB) int64 {
	t.Helper()
	c := &Category{Name: "Test Cat", GroupName: "Group", Color: "#abcdef"}
	if err := CreateCategory(db, 1, c); err != nil {
		t.Fatalf("create test category: %v", err)
	}
	return c.ID
}

func TestLimitsGetHandler_MissingMonth(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("GET", "/api/budget/limits", nil), 1)
	rec := httptest.NewRecorder()
	LimitsGetHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestLimitsGetHandler_InvalidMonth(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("GET", "/api/budget/limits?month=bad", nil), 1)
	rec := httptest.NewRecorder()
	LimitsGetHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestLimitsGetHandler_Success(t *testing.T) {
	db := setupTestDB(t)
	catID := createTestCategoryForHandler(t, db)

	if err := SetBudgetLimit(db, 1, &BudgetLimit{
		CategoryID: catID, Amount: 3000, Period: "monthly", EffectiveFrom: "2026-01",
	}); err != nil {
		t.Fatalf("SetBudgetLimit: %v", err)
	}

	req := withUser(httptest.NewRequest("GET", "/api/budget/limits?month=2026-01", nil), 1)
	rec := httptest.NewRecorder()
	LimitsGetHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Month  string                 `json:"month"`
		Limits map[string]interface{} `json:"limits"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Month != "2026-01" {
		t.Errorf("month = %q, want %q", body.Month, "2026-01")
	}
	if len(body.Limits) == 0 {
		t.Error("expected at least one limit in response")
	}
}

func TestLimitsPutHandler_Success(t *testing.T) {
	db := setupTestDB(t)
	catID := createTestCategoryForHandler(t, db)

	payload, _ := json.Marshal(map[string]any{
		"month":  "2026-03",
		"limits": []map[string]any{{"category_id": catID, "amount": 2500.0}},
	})
	req := withUser(httptest.NewRequest("PUT", "/api/budget/limits", strings.NewReader(string(payload))), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	LimitsPutHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify the limit was actually saved.
	limits, err := GetBudgetLimits(db, 1, "2026-03")
	if err != nil {
		t.Fatalf("GetBudgetLimits: %v", err)
	}
	if limits[catID].Amount != 2500 {
		t.Errorf("Amount = %v, want 2500", limits[catID].Amount)
	}
}

// -- Credit card summary handler tests --

func TestCreditCardSummaryHandler_MissingAccountID(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("GET", "/api/budget/credit/summary?month=2026-01", nil), 1)
	rec := httptest.NewRecorder()
	CreditCardSummaryHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreditCardSummaryHandler_MissingMonth(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("GET", "/api/budget/credit/summary?account_id=1", nil), 1)
	rec := httptest.NewRecorder()
	CreditCardSummaryHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreditCardSummaryHandler_InvalidMonth(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("GET", "/api/budget/credit/summary?account_id=1&month=bad", nil), 1)
	rec := httptest.NewRecorder()
	CreditCardSummaryHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreditCardSummaryHandler_AccountNotFound(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("GET", "/api/budget/credit/summary?account_id=999&month=2026-01", nil), 1)
	rec := httptest.NewRecorder()
	CreditCardSummaryHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreditCardSummaryHandler_Success(t *testing.T) {
	db := setupTestDB(t)

	// Create a credit account with a non-zero balance and credit limit.
	acct := &Account{Name: "My Visa", Type: AccountTypeCredit, Currency: "NOK", Balance: -3000, CreditLimit: 20000}
	if err := CreateAccount(db, 1, acct); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	// Add an expense transaction in 2026-01.
	tx := &Transaction{AccountID: acct.ID, Amount: -500, Description: "Restaurant", Date: "2026-01-15"}
	if err := CreateTransaction(db, 1, tx); err != nil {
		t.Fatalf("CreateTransaction: %v", err)
	}

	req := withUser(httptest.NewRequest("GET", fmt.Sprintf("/api/budget/credit/summary?account_id=%d&month=2026-01", acct.ID), nil), 1)
	rec := httptest.NewRecorder()
	CreditCardSummaryHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body CreditCardSummary
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Month != "2026-01" {
		t.Errorf("month = %q, want %q", body.Month, "2026-01")
	}
	if body.CreditLimit != 20000 {
		t.Errorf("credit_limit = %v, want 20000", body.CreditLimit)
	}
	if body.UsedAmount != 3000 {
		t.Errorf("used_amount = %v, want 3000 (abs(balance))", body.UsedAmount)
	}
	if body.ExpenseTotal != 500 {
		t.Errorf("expense_total = %v, want 500", body.ExpenseTotal)
	}
	if len(body.ByCategory) == 0 {
		t.Error("expected at least one category entry in by_category")
	}
}

func TestLimitsPutHandler_MissingMonth(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"limits":[{"category_id":1,"amount":1000}]}`
	req := withUser(httptest.NewRequest("PUT", "/api/budget/limits", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	LimitsPutHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestLimitsPutHandler_InvalidBody(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("PUT", "/api/budget/limits", strings.NewReader("not json")), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	LimitsPutHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestLimitsPutHandler_InvalidCategoryID(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"month":"2026-03","limits":[{"category_id":0,"amount":1000}]}`
	req := withUser(httptest.NewRequest("PUT", "/api/budget/limits", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	LimitsPutHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestTransactionsCreateHandler_InvalidDateFormat(t *testing.T) {
	db := setupTestDB(t)
	accID := createTestAccount(t, db)

	payload, _ := json.Marshal(map[string]any{
		"account_id":  accID,
		"amount":      -100.0,
		"description": "Bad date",
		"date":        "10/01/2026",
	})
	req := withUser(httptest.NewRequest("POST", "/api/budget/transactions", strings.NewReader(string(payload))), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	TransactionsCreateHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

// -- Recurring handler tests --

func TestRecurringListHandler_Empty(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("GET", "/api/budget/recurring", nil), 1)
	rec := httptest.NewRecorder()
	RecurringListHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Recurring []recurringResponse `json:"recurring"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Recurring) != 0 {
		t.Errorf("expected empty list, got %d items", len(body.Recurring))
	}
}

func TestRecurringCreateHandler_Success(t *testing.T) {
	db := setupTestDB(t)
	accID := createTestAccount(t, db)

	payload, _ := json.Marshal(map[string]any{
		"account_id":   accID,
		"amount":       -1200.0,
		"description":  "Rent",
		"frequency":    "monthly",
		"day_of_month": 1,
		"start_date":   "2026-01-01",
		"active":       true,
		"split_type":   "equal",
	})
	req := withUser(httptest.NewRequest("POST", "/api/budget/recurring", strings.NewReader(string(payload))), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	RecurringCreateHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Recurring recurringResponse `json:"recurring"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Recurring.ID == 0 {
		t.Fatal("expected non-zero ID")
	}
	if body.Recurring.Amount != -1200.0 {
		t.Errorf("Amount = %v, want -1200", body.Recurring.Amount)
	}
	if !body.Recurring.Active {
		t.Error("Active should be true")
	}
}

func TestRecurringCreateHandler_InactiveRule(t *testing.T) {
	db := setupTestDB(t)
	accID := createTestAccount(t, db)

	payload, _ := json.Marshal(map[string]any{
		"account_id":   accID,
		"amount":       -500.0,
		"description":  "Inactive rule",
		"frequency":    "monthly",
		"day_of_month": 15,
		"start_date":   "2026-01-01",
		"active":       false,
		"split_type":   "equal",
	})
	req := withUser(httptest.NewRequest("POST", "/api/budget/recurring", strings.NewReader(string(payload))), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	RecurringCreateHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Recurring recurringResponse `json:"recurring"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Recurring.Active {
		t.Error("Active should be false when created with active=false")
	}
}

func TestRecurringCreateHandler_NextDue_BusinessDay(t *testing.T) {
	db := setupTestDB(t)
	accID := createTestAccount(t, db)

	// June 15, 2026 is a Monday (not a holiday) — next_due should equal start_date.
	payload, _ := json.Marshal(map[string]any{
		"account_id":   accID,
		"amount":       -1000.0,
		"description":  "Next due test",
		"frequency":    "monthly",
		"day_of_month": 15,
		"start_date":   "2026-06-15",
		"active":       true,
		"split_type":   "equal",
	})
	req := withUser(httptest.NewRequest("POST", "/api/budget/recurring", strings.NewReader(string(payload))), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	RecurringCreateHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Recurring recurringResponse `json:"recurring"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Recurring.NextDue == "" {
		t.Error("expected next_due to be non-empty")
	}
	if body.Recurring.NextDue != "2026-06-15" {
		t.Errorf("next_due: want 2026-06-15, got %s", body.Recurring.NextDue)
	}
}

func TestRecurringCreateHandler_NextDue_WeekendAdjustment(t *testing.T) {
	db := setupTestDB(t)
	accID := createTestAccount(t, db)

	// June 13, 2026 is a Saturday — next_due should advance to Monday June 15.
	payload, _ := json.Marshal(map[string]any{
		"account_id":   accID,
		"amount":       -1000.0,
		"description":  "Weekend adjustment test",
		"frequency":    "monthly",
		"day_of_month": 13,
		"start_date":   "2026-06-13",
		"active":       true,
		"split_type":   "equal",
	})
	req := withUser(httptest.NewRequest("POST", "/api/budget/recurring", strings.NewReader(string(payload))), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	RecurringCreateHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Recurring recurringResponse `json:"recurring"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Saturday June 13 → next business day Monday June 15.
	if body.Recurring.NextDue != "2026-06-15" {
		t.Errorf("next_due: want 2026-06-15 (Monday after Saturday), got %s", body.Recurring.NextDue)
	}
}

func TestRecurringCreateHandler_MissingAccountID(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"amount":-100,"start_date":"2026-01-01","frequency":"monthly","day_of_month":1}`
	req := withUser(httptest.NewRequest("POST", "/api/budget/recurring", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	RecurringCreateHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestRecurringCreateHandler_InvalidBody(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("POST", "/api/budget/recurring", strings.NewReader("not json")), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	RecurringCreateHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestRecurringUpdateHandler_Success(t *testing.T) {
	db := setupTestDB(t)
	accID := createTestAccount(t, db)

	// Create a rule first.
	createPayload, _ := json.Marshal(map[string]any{
		"account_id":   accID,
		"amount":       -500.0,
		"description":  "Old desc",
		"frequency":    "monthly",
		"day_of_month": 1,
		"start_date":   "2026-01-01",
		"active":       true,
		"split_type":   "equal",
	})
	createReq := withUser(httptest.NewRequest("POST", "/api/budget/recurring", strings.NewReader(string(createPayload))), 1)
	createReq.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	RecurringCreateHandler(db).ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d", createRec.Code)
	}
	var createBody struct {
		Recurring recurringResponse `json:"recurring"`
	}
	json.NewDecoder(createRec.Body).Decode(&createBody) //nolint:errcheck
	id := fmt.Sprintf("%d", createBody.Recurring.ID)

	// Update it.
	updatePayload, _ := json.Marshal(map[string]any{
		"account_id":   accID,
		"amount":       -800.0,
		"description":  "Updated desc",
		"frequency":    "monthly",
		"day_of_month": 15,
		"start_date":   "2026-01-01",
		"active":       false,
		"split_type":   "equal",
	})
	req := withUser(httptest.NewRequest("PUT", "/api/budget/recurring/"+id, strings.NewReader(string(updatePayload))), 1)
	req = withChiParam(req, "id", id)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	RecurringUpdateHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Recurring recurringResponse `json:"recurring"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Recurring.Amount != -800.0 {
		t.Errorf("Amount = %v, want -800", body.Recurring.Amount)
	}
	if body.Recurring.Active {
		t.Error("Active should be false after update")
	}
}

func TestRecurringUpdateHandler_NotFound(t *testing.T) {
	db := setupTestDB(t)
	accID := createTestAccount(t, db)

	payload, _ := json.Marshal(map[string]any{
		"account_id":   accID,
		"amount":       -100.0,
		"description":  "x",
		"frequency":    "monthly",
		"day_of_month": 1,
		"start_date":   "2026-01-01",
		"active":       true,
	})
	req := withUser(httptest.NewRequest("PUT", "/api/budget/recurring/9999", strings.NewReader(string(payload))), 1)
	req = withChiParam(req, "id", "9999")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	RecurringUpdateHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestRecurringDeleteHandler_Success(t *testing.T) {
	db := setupTestDB(t)
	accID := createTestAccount(t, db)

	// Create a rule.
	createPayload, _ := json.Marshal(map[string]any{
		"account_id":   accID,
		"amount":       -300.0,
		"description":  "To delete",
		"frequency":    "monthly",
		"day_of_month": 1,
		"start_date":   "2026-01-01",
		"active":       true,
		"split_type":   "equal",
	})
	createReq := withUser(httptest.NewRequest("POST", "/api/budget/recurring", strings.NewReader(string(createPayload))), 1)
	createReq.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	RecurringCreateHandler(db).ServeHTTP(createRec, createReq)
	var createBody struct {
		Recurring recurringResponse `json:"recurring"`
	}
	json.NewDecoder(createRec.Body).Decode(&createBody) //nolint:errcheck
	id := fmt.Sprintf("%d", createBody.Recurring.ID)

	// Delete it.
	req := withUser(httptest.NewRequest("DELETE", "/api/budget/recurring/"+id, nil), 1)
	req = withChiParam(req, "id", id)
	rec := httptest.NewRecorder()
	RecurringDeleteHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}
}

func TestRecurringDeleteHandler_NotFound(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("DELETE", "/api/budget/recurring/9999", nil), 1)
	req = withChiParam(req, "id", "9999")
	rec := httptest.NewRecorder()
	RecurringDeleteHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestRecurringCreateHandler_WithSplitFields(t *testing.T) {
	db := setupTestDB(t)
	accID := createTestAccount(t, db)

	pct := 40.0
	payload, _ := json.Marshal(map[string]any{
		"account_id":   accID,
		"amount":       -600.0,
		"description":  "Shared expense",
		"frequency":    "monthly",
		"day_of_month": 1,
		"start_date":   "2026-01-01",
		"active":       true,
		"split_type":   "percentage",
		"split_pct":    pct,
	})
	req := withUser(httptest.NewRequest("POST", "/api/budget/recurring", strings.NewReader(string(payload))), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	RecurringCreateHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Recurring recurringResponse `json:"recurring"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Recurring.SplitType != "percentage" {
		t.Errorf("SplitType = %q, want %q", body.Recurring.SplitType, "percentage")
	}
	if body.Recurring.SplitPct == nil || *body.Recurring.SplitPct != pct {
		t.Errorf("SplitPct = %v, want %v", body.Recurring.SplitPct, pct)
	}
}

func TestRecurringCreateHandler_InvalidSplitType(t *testing.T) {
	db := setupTestDB(t)
	accID := createTestAccount(t, db)

	payload, _ := json.Marshal(map[string]any{
		"account_id":   accID,
		"amount":       -100.0,
		"description":  "bad split",
		"frequency":    "monthly",
		"day_of_month": 1,
		"start_date":   "2026-01-01",
		"active":       true,
		"split_type":   "invalid_value",
	})
	req := withUser(httptest.NewRequest("POST", "/api/budget/recurring", strings.NewReader(string(payload))), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	RecurringCreateHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRecurringCreateHandler_SplitPctOutOfRange(t *testing.T) {
	db := setupTestDB(t)
	accID := createTestAccount(t, db)

	payload, _ := json.Marshal(map[string]any{
		"account_id":   accID,
		"amount":       -100.0,
		"description":  "bad pct",
		"frequency":    "monthly",
		"day_of_month": 1,
		"start_date":   "2026-01-01",
		"active":       true,
		"split_type":   "percentage",
		"split_pct":    150.0,
	})
	req := withUser(httptest.NewRequest("POST", "/api/budget/recurring", strings.NewReader(string(payload))), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	RecurringCreateHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRecurringUpdateHandler_UpdateSplitFields(t *testing.T) {
	db := setupTestDB(t)
	accID := createTestAccount(t, db)

	// Create a rule first.
	createPayload, _ := json.Marshal(map[string]any{
		"account_id":   accID,
		"amount":       -500.0,
		"description":  "Shared",
		"frequency":    "monthly",
		"day_of_month": 1,
		"start_date":   "2026-01-01",
		"active":       true,
		"split_type":   "equal",
	})
	createReq := withUser(httptest.NewRequest("POST", "/api/budget/recurring", strings.NewReader(string(createPayload))), 1)
	createReq.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	RecurringCreateHandler(db).ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d", createRec.Code)
	}
	var createBody struct {
		Recurring recurringResponse `json:"recurring"`
	}
	json.NewDecoder(createRec.Body).Decode(&createBody) //nolint:errcheck
	id := fmt.Sprintf("%d", createBody.Recurring.ID)

	// Update split fields.
	newPct := 60.0
	updatePayload, _ := json.Marshal(map[string]any{
		"account_id":   accID,
		"amount":       -500.0,
		"description":  "Shared",
		"frequency":    "monthly",
		"day_of_month": 1,
		"start_date":   "2026-01-01",
		"active":       true,
		"split_type":   "percentage",
		"split_pct":    newPct,
	})
	req := withUser(httptest.NewRequest("PUT", "/api/budget/recurring/"+id, strings.NewReader(string(updatePayload))), 1)
	req = withChiParam(req, "id", id)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	RecurringUpdateHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Recurring recurringResponse `json:"recurring"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Recurring.SplitType != "percentage" {
		t.Errorf("SplitType = %q, want %q", body.Recurring.SplitType, "percentage")
	}
	if body.Recurring.SplitPct == nil || *body.Recurring.SplitPct != newPct {
		t.Errorf("SplitPct = %v, want %v", body.Recurring.SplitPct, newPct)
	}
}

// TestRecurringUpdateHandler_OmitSplitPct verifies that omitting split_pct in an update
// preserves the existing DB value.
func TestRecurringUpdateHandler_OmitSplitPct(t *testing.T) {
	db := setupTestDB(t)
	accID := createTestAccount(t, db)

	// Create a rule with percentage split and a specific pct.
	initialPct := 40.0
	createPayload, _ := json.Marshal(map[string]any{
		"account_id":   accID,
		"amount":       -500.0,
		"description":  "Shared",
		"frequency":    "monthly",
		"day_of_month": 1,
		"start_date":   "2026-01-01",
		"active":       true,
		"split_type":   "percentage",
		"split_pct":    initialPct,
	})
	createReq := withUser(httptest.NewRequest("POST", "/api/budget/recurring", strings.NewReader(string(createPayload))), 1)
	createReq.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	RecurringCreateHandler(db).ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d", createRec.Code)
	}
	var createBody struct {
		Recurring recurringResponse `json:"recurring"`
	}
	json.NewDecoder(createRec.Body).Decode(&createBody) //nolint:errcheck
	id := fmt.Sprintf("%d", createBody.Recurring.ID)

	// Update without providing split_pct — existing value should be preserved.
	updatePayload, _ := json.Marshal(map[string]any{
		"account_id":   accID,
		"amount":       -600.0,
		"description":  "Shared updated",
		"frequency":    "monthly",
		"day_of_month": 1,
		"start_date":   "2026-01-01",
		"active":       true,
		"split_type":   "percentage",
		// split_pct intentionally omitted
	})
	req := withUser(httptest.NewRequest("PUT", "/api/budget/recurring/"+id, strings.NewReader(string(updatePayload))), 1)
	req = withChiParam(req, "id", id)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	RecurringUpdateHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Recurring recurringResponse `json:"recurring"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Recurring.SplitPct == nil || *body.Recurring.SplitPct != initialPct {
		t.Errorf("SplitPct = %v, want %v (existing value preserved)", body.Recurring.SplitPct, initialPct)
	}
}

// TestRecurringUpdateHandler_ClearSplitPct verifies that changing split_type to a non-percentage
// type clears split_pct to NULL regardless of the existing DB value.
func TestRecurringUpdateHandler_ClearSplitPct(t *testing.T) {
	db := setupTestDB(t)
	accID := createTestAccount(t, db)

	// Create a rule with percentage split.
	createPayload, _ := json.Marshal(map[string]any{
		"account_id":   accID,
		"amount":       -500.0,
		"description":  "Shared",
		"frequency":    "monthly",
		"day_of_month": 1,
		"start_date":   "2026-01-01",
		"active":       true,
		"split_type":   "percentage",
		"split_pct":    55.0,
	})
	createReq := withUser(httptest.NewRequest("POST", "/api/budget/recurring", strings.NewReader(string(createPayload))), 1)
	createReq.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	RecurringCreateHandler(db).ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d", createRec.Code)
	}
	var createBody struct {
		Recurring recurringResponse `json:"recurring"`
	}
	json.NewDecoder(createRec.Body).Decode(&createBody) //nolint:errcheck
	id := fmt.Sprintf("%d", createBody.Recurring.ID)

	// Update to equal split — split_pct should be cleared to NULL automatically.
	updatePayload, _ := json.Marshal(map[string]any{
		"account_id":   accID,
		"amount":       -500.0,
		"description":  "Shared",
		"frequency":    "monthly",
		"day_of_month": 1,
		"start_date":   "2026-01-01",
		"active":       true,
		"split_type":   "equal",
	})
	req := withUser(httptest.NewRequest("PUT", "/api/budget/recurring/"+id, strings.NewReader(string(updatePayload))), 1)
	req = withChiParam(req, "id", id)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	RecurringUpdateHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Recurring recurringResponse `json:"recurring"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Recurring.SplitType != "equal" {
		t.Errorf("SplitType = %q, want %q", body.Recurring.SplitType, "equal")
	}
	if body.Recurring.SplitPct != nil {
		t.Errorf("SplitPct = %v, want nil (cleared to NULL)", body.Recurring.SplitPct)
	}
}

func TestRecurringGenerateHandler_Empty(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("POST", "/api/budget/recurring/generate", nil), 1)
	rec := httptest.NewRecorder()
	RecurringGenerateHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Generated int `json:"generated"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Generated != 0 {
		t.Errorf("expected 0 generated, got %d", body.Generated)
	}
}

func TestRecurringGenerateHandler_WithDueRule(t *testing.T) {
	db := setupTestDB(t)
	accID := createTestAccount(t, db)

	// Create a rule that was last generated two months ago so it is definitely
	// due at least once regardless of the current date.
	twoMonthsAgo := time.Now().AddDate(0, -2, 0).Format("2006-01-02")
	rule := &Recurring{
		AccountID:     accID,
		Amount:        -150,
		Description:   "monthly expense",
		Frequency:     FrequencyMonthly,
		DayOfMonth:    1,
		StartDate:     time.Now().AddDate(-1, 0, 0), // 1 year ago
		LastGenerated: twoMonthsAgo,
		Active:        true,
	}
	if err := CreateRecurring(db, 1, rule); err != nil {
		t.Fatalf("CreateRecurring: %v", err)
	}

	req := withUser(httptest.NewRequest("POST", "/api/budget/recurring/generate", nil), 1)
	rec := httptest.NewRecorder()
	RecurringGenerateHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Generated int `json:"generated"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Generated < 1 {
		t.Errorf("expected at least 1 generated, got %d", body.Generated)
	}

	// Verify transactions were actually persisted.
	txns, err := ListTransactions(db, 1, TransactionFilter{})
	if err != nil {
		t.Fatalf("ListTransactions: %v", err)
	}
	if len(txns) != body.Generated {
		t.Errorf("persisted transactions = %d, want %d", len(txns), body.Generated)
	}

	// Verify last_generated was advanced on the rule.
	updated, err := GetRecurring(db, 1, rule.ID)
	if err != nil {
		t.Fatalf("GetRecurring: %v", err)
	}
	if updated.LastGenerated == twoMonthsAgo {
		t.Error("last_generated was not advanced after generation")
	}
}

func TestRecurringCreateHandler_InvalidVariableID(t *testing.T) {
	db := setupTestDB(t)
	accID := createTestAccount(t, db)

	// Insert a second user.
	if _, err := db.Exec("INSERT INTO users (id, email, name, google_id) VALUES (2, 'other@example.com', 'Other', 'g456')"); err != nil {
		t.Fatalf("insert user 2: %v", err)
	}
	// Create a variable bill owned by user 2.
	otherBill := &VariableBill{Name: "OtherBill"}
	if err := CreateVariableBill(db, 2, otherBill); err != nil {
		t.Fatalf("CreateVariableBill user2: %v", err)
	}

	// User 1 attempts to create a recurring rule pointing at user 2's variable bill.
	payload, _ := json.Marshal(map[string]any{
		"account_id":   accID,
		"amount":       -100.0,
		"description":  "Test",
		"frequency":    "monthly",
		"day_of_month": 1,
		"start_date":   "2026-01-01",
		"variable_id":  otherBill.ID,
	})
	req := withUser(httptest.NewRequest("POST", "/api/budget/recurring", strings.NewReader(string(payload))), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	RecurringCreateHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for cross-user variable_id, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRecurringUpdateHandler_InvalidVariableID(t *testing.T) {
	db := setupTestDB(t)
	accID := createTestAccount(t, db)

	// Insert a second user.
	if _, err := db.Exec("INSERT INTO users (id, email, name, google_id) VALUES (2, 'other@example.com', 'Other', 'g456')"); err != nil {
		t.Fatalf("insert user 2: %v", err)
	}
	// Create a variable bill owned by user 2.
	otherBill := &VariableBill{Name: "OtherBill"}
	if err := CreateVariableBill(db, 2, otherBill); err != nil {
		t.Fatalf("CreateVariableBill user2: %v", err)
	}

	// Create a valid recurring rule for user 1.
	rule := &Recurring{
		AccountID:   accID,
		Amount:      -100,
		Description: "Rent",
		Frequency:   FrequencyMonthly,
		DayOfMonth:  1,
		StartDate:   time.Now(),
		Active:      true,
	}
	if err := CreateRecurring(db, 1, rule); err != nil {
		t.Fatalf("CreateRecurring: %v", err)
	}

	// User 1 attempts to update rule pointing at user 2's variable bill.
	payload, _ := json.Marshal(map[string]any{
		"account_id":   accID,
		"amount":       -100.0,
		"description":  "Rent",
		"frequency":    "monthly",
		"day_of_month": 1,
		"start_date":   "2026-01-01",
		"variable_id":  otherBill.ID,
	})
	req := withUser(httptest.NewRequest("PUT", "/api/budget/recurring/1", strings.NewReader(string(payload))), 1)
	req = withChiParam(req, "id", fmt.Sprintf("%d", rule.ID))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	RecurringUpdateHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for cross-user variable_id, got %d: %s", rec.Code, rec.Body.String())
	}
}
