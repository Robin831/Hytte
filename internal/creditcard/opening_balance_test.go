package creditcard

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ── GET /credit-card/opening-balance ─────────────────────────────────────────

func TestOpeningBalanceGetHandler_MissingParams(t *testing.T) {
	db := setupTestDB(t)
	if _, err := db.Exec(budgetSchema); err != nil {
		t.Fatalf("create budget tables: %v", err)
	}
	handler := OpeningBalanceGetHandler(db)

	cases := []struct {
		name  string
		query string
	}{
		{"missing credit_card_id", "?month=2026-03"},
		{"missing month", "?credit_card_id=card-1"},
		{"month too short", "?credit_card_id=card-1&month=2026-3"},
		{"month too long", "?credit_card_id=card-1&month=2026-030"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/credit-card/opening-balance"+tc.query, nil)
			req = withUser(req, 1)
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)
			if rr.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want %d", rr.Code, http.StatusBadRequest)
			}
		})
	}
}

func TestOpeningBalanceGetHandler_DefaultsToZero(t *testing.T) {
	db := setupTestDB(t)
	if _, err := db.Exec(budgetSchema); err != nil {
		t.Fatalf("create budget tables: %v", err)
	}
	handler := OpeningBalanceGetHandler(db)

	req := httptest.NewRequest(http.MethodGet, "/api/credit-card/opening-balance?credit_card_id=card-1&month=2026-03", nil)
	req = withUser(req, 1)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	var resp openingBalanceResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Balance != 0 {
		t.Errorf("balance = %f, want 0", resp.Balance)
	}
}

func TestOpeningBalanceGetHandler_ReturnsStoredBalance(t *testing.T) {
	db := setupTestDB(t)
	if _, err := db.Exec(budgetSchema); err != nil {
		t.Fatalf("create budget tables: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO credit_card_opening_balances (user_id, credit_card_id, month, balance) VALUES (1, 'card-1', '2026-03', 1500.0)`,
	); err != nil {
		t.Fatalf("insert opening balance: %v", err)
	}
	handler := OpeningBalanceGetHandler(db)

	req := httptest.NewRequest(http.MethodGet, "/api/credit-card/opening-balance?credit_card_id=card-1&month=2026-03", nil)
	req = withUser(req, 1)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	var resp openingBalanceResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Balance != 1500.0 {
		t.Errorf("balance = %f, want 1500", resp.Balance)
	}
}

func TestOpeningBalanceGetHandler_UserIsolation(t *testing.T) {
	db := setupTestDB(t)
	if _, err := db.Exec(budgetSchema); err != nil {
		t.Fatalf("create budget tables: %v", err)
	}
	// Insert balance for user 2 — user 1 must not see it.
	if _, err := db.Exec(
		`INSERT INTO credit_card_opening_balances (user_id, credit_card_id, month, balance) VALUES (2, 'card-1', '2026-03', 9999.0)`,
	); err != nil {
		t.Fatalf("insert opening balance: %v", err)
	}
	handler := OpeningBalanceGetHandler(db)

	req := httptest.NewRequest(http.MethodGet, "/api/credit-card/opening-balance?credit_card_id=card-1&month=2026-03", nil)
	req = withUser(req, 1)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	var resp openingBalanceResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Balance != 0 {
		t.Errorf("balance = %f, want 0 (user isolation)", resp.Balance)
	}
}

// ── PUT /credit-card/opening-balance ─────────────────────────────────────────

func TestOpeningBalancePutHandler_MissingParams(t *testing.T) {
	db := setupTestDB(t)
	if _, err := db.Exec(budgetSchema); err != nil {
		t.Fatalf("create budget tables: %v", err)
	}
	handler := OpeningBalancePutHandler(db)

	req := httptest.NewRequest(http.MethodPut, "/api/credit-card/opening-balance?month=2026-03",
		strings.NewReader(`{"balance":100}`))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, 1)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestOpeningBalancePutHandler_InvalidJSON(t *testing.T) {
	db := setupTestDB(t)
	if _, err := db.Exec(budgetSchema); err != nil {
		t.Fatalf("create budget tables: %v", err)
	}
	handler := OpeningBalancePutHandler(db)

	req := httptest.NewRequest(http.MethodPut, "/api/credit-card/opening-balance?credit_card_id=card-1&month=2026-03",
		strings.NewReader(`not json`))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, 1)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestOpeningBalancePutHandler_BodyTooLarge(t *testing.T) {
	db := setupTestDB(t)
	if _, err := db.Exec(budgetSchema); err != nil {
		t.Fatalf("create budget tables: %v", err)
	}
	handler := OpeningBalancePutHandler(db)

	body := bytes.Repeat([]byte("x"), 2048)
	req := httptest.NewRequest(http.MethodPut, "/api/credit-card/opening-balance?credit_card_id=card-1&month=2026-03",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, 1)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d (oversized body should be rejected)", rr.Code, http.StatusBadRequest)
	}
}

func TestOpeningBalancePutHandler_InsertsBalance(t *testing.T) {
	db := setupTestDB(t)
	if _, err := db.Exec(budgetSchema); err != nil {
		t.Fatalf("create budget tables: %v", err)
	}
	handler := OpeningBalancePutHandler(db)

	body, _ := json.Marshal(map[string]any{"balance": 2500.0})
	req := httptest.NewRequest(http.MethodPut, "/api/credit-card/opening-balance?credit_card_id=card-1&month=2026-03",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, 1)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}

	var saved float64
	if err := db.QueryRow(
		`SELECT balance FROM credit_card_opening_balances WHERE user_id = 1 AND credit_card_id = 'card-1' AND month = '2026-03'`,
	).Scan(&saved); err != nil {
		t.Fatalf("query saved balance: %v", err)
	}
	if saved != 2500.0 {
		t.Errorf("saved balance = %f, want 2500", saved)
	}
}

func TestOpeningBalancePutHandler_UpdatesExistingBalance(t *testing.T) {
	db := setupTestDB(t)
	if _, err := db.Exec(budgetSchema); err != nil {
		t.Fatalf("create budget tables: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO credit_card_opening_balances (user_id, credit_card_id, month, balance) VALUES (1, 'card-1', '2026-03', 1000.0)`,
	); err != nil {
		t.Fatalf("insert existing balance: %v", err)
	}
	handler := OpeningBalancePutHandler(db)

	body, _ := json.Marshal(map[string]any{"balance": 3000.0})
	req := httptest.NewRequest(http.MethodPut, "/api/credit-card/opening-balance?credit_card_id=card-1&month=2026-03",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, 1)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}

	var saved float64
	if err := db.QueryRow(
		`SELECT balance FROM credit_card_opening_balances WHERE user_id = 1 AND credit_card_id = 'card-1' AND month = '2026-03'`,
	).Scan(&saved); err != nil {
		t.Fatalf("query saved balance: %v", err)
	}
	if saved != 3000.0 {
		t.Errorf("saved balance = %f, want 3000 (upsert)", saved)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM credit_card_opening_balances`).Scan(&count); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if count != 1 {
		t.Errorf("row count = %d, want 1 (upsert must not insert duplicate)", count)
	}
}
