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
