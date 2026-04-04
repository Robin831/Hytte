package creditcard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Robin831/Hytte/internal/encryption"
)

func TestTransactionsListHandler_MissingCardID(t *testing.T) {
	db := setupTestDB(t)
	handler := TransactionsListHandler(db)

	req := httptest.NewRequest(http.MethodGet, "/api/credit-card/transactions?month=2026-03", nil)
	req = withUser(req, 1)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestTransactionsListHandler_InvalidMonth(t *testing.T) {
	db := setupTestDB(t)
	handler := TransactionsListHandler(db)

	req := httptest.NewRequest(http.MethodGet, "/api/credit-card/transactions?credit_card_id=1&month=bad", nil)
	req = withUser(req, 1)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestTransactionsListHandler_EmptyResult(t *testing.T) {
	db := setupTestDB(t)
	handler := TransactionsListHandler(db)

	req := httptest.NewRequest(http.MethodGet, "/api/credit-card/transactions?credit_card_id=1&month=2026-03", nil)
	req = withUser(req, 1)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}

	var resp TransactionsListResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Transactions) != 0 {
		t.Errorf("expected 0 transactions, got %d", len(resp.Transactions))
	}
}

func TestTransactionsListHandler_ReturnsDecryptedTransactions(t *testing.T) {
	db := setupTestDB(t)

	// Encrypt a merchant name as the handler expects encrypted storage.
	encDesc, err := encryption.EncryptField("Rema 1000")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	if _, err := db.Exec(`
		INSERT INTO credit_card_transactions
			(user_id, credit_card_id, transaksjonsdato, beskrivelse, belop, belop_i_valuta, is_pending, is_innbetaling)
		VALUES (1, '42', '2026-03-15', ?, -234.50, -234.50, 0, 0)
	`, encDesc); err != nil {
		t.Fatalf("insert transaction: %v", err)
	}

	handler := TransactionsListHandler(db)
	req := httptest.NewRequest(http.MethodGet, "/api/credit-card/transactions?credit_card_id=42&month=2026-03", nil)
	req = withUser(req, 1)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}

	var resp TransactionsListResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Transactions) != 1 {
		t.Fatalf("expected 1 transaction, got %d", len(resp.Transactions))
	}
	if resp.Transactions[0].Beskrivelse != "Rema 1000" {
		t.Errorf("beskrivelse = %q, want %q", resp.Transactions[0].Beskrivelse, "Rema 1000")
	}
	if resp.Transactions[0].Belop != -234.50 {
		t.Errorf("belop = %f, want -234.50", resp.Transactions[0].Belop)
	}
}

func TestTransactionsListHandler_FiltersByMonth(t *testing.T) {
	db := setupTestDB(t)

	encDesc, err := encryption.EncryptField("March txn")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	encDesc2, err := encryption.EncryptField("April txn")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	if _, err := db.Exec(`
		INSERT INTO credit_card_transactions
			(user_id, credit_card_id, transaksjonsdato, beskrivelse, belop, belop_i_valuta, is_pending, is_innbetaling)
		VALUES
			(1, '1', '2026-03-15', ?, -100, -100, 0, 0),
			(1, '1', '2026-04-05', ?, -200, -200, 0, 0)
	`, encDesc, encDesc2); err != nil {
		t.Fatalf("insert transactions: %v", err)
	}

	handler := TransactionsListHandler(db)
	req := httptest.NewRequest(http.MethodGet, "/api/credit-card/transactions?credit_card_id=1&month=2026-03", nil)
	req = withUser(req, 1)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", rr.Code, rr.Body.String())
	}

	var resp TransactionsListResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Transactions) != 1 {
		t.Fatalf("expected 1 transaction for March, got %d", len(resp.Transactions))
	}
	if resp.Transactions[0].Beskrivelse != "March txn" {
		t.Errorf("beskrivelse = %q, want %q", resp.Transactions[0].Beskrivelse, "March txn")
	}
}

func TestTransactionsListHandler_VariableBillDecrypted(t *testing.T) {
	db := setupTestDB(t)
	if _, err := db.Exec(budgetSchema); err != nil {
		t.Fatalf("create budget tables: %v", err)
	}

	encName, err := encryption.EncryptField("My Card Bill")
	if err != nil {
		t.Fatalf("encrypt bill name: %v", err)
	}

	// Insert a variable bill linked to card "99".
	if _, err := db.Exec(`
		INSERT INTO budget_variable_bills (id, user_id, name, credit_card_id) VALUES (1, 1, ?, '99')
	`, encName); err != nil {
		t.Fatalf("insert variable bill: %v", err)
	}

	// Insert two entries for 2026-04 (payment month for March transactions).
	if _, err := db.Exec(`
		INSERT INTO budget_variable_entries (variable_id, month, sub_name, amount)
		VALUES (1, '2026-04', 'entry1', 150.0), (1, '2026-04', 'entry2', 75.0)
	`); err != nil {
		t.Fatalf("insert variable entries: %v", err)
	}

	handler := TransactionsListHandler(db)
	req := httptest.NewRequest(http.MethodGet, "/api/credit-card/transactions?credit_card_id=99&month=2026-03", nil)
	req = withUser(req, 1)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", rr.Code, rr.Body.String())
	}

	var resp TransactionsListResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.VariableBillName != "My Card Bill" {
		t.Errorf("variable_bill_name = %q, want %q", resp.VariableBillName, "My Card Bill")
	}
	if resp.VariableBillAmount != 225.0 {
		t.Errorf("variable_bill_amount = %f, want 225.0", resp.VariableBillAmount)
	}
}

func TestTransactionsListHandler_GroupInfoIncluded(t *testing.T) {
	db := setupTestDB(t)

	if _, err := db.Exec(`
		INSERT INTO credit_card_groups (id, user_id, name, sort_order) VALUES (10, 1, 'Groceries', 0)
	`); err != nil {
		t.Fatalf("insert group: %v", err)
	}

	encDesc, err := encryption.EncryptField("Rema 1000")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	if _, err := db.Exec(`
		INSERT INTO credit_card_transactions
			(user_id, credit_card_id, transaksjonsdato, beskrivelse, belop, belop_i_valuta, is_pending, is_innbetaling, group_id)
		VALUES (1, '1', '2026-03-10', ?, -150, -150, 0, 0, 10)
	`, encDesc); err != nil {
		t.Fatalf("insert transaction: %v", err)
	}

	handler := TransactionsListHandler(db)
	req := httptest.NewRequest(http.MethodGet, "/api/credit-card/transactions?credit_card_id=1&month=2026-03", nil)
	req = withUser(req, 1)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", rr.Code, rr.Body.String())
	}

	var resp TransactionsListResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Transactions) != 1 {
		t.Fatalf("expected 1 transaction, got %d", len(resp.Transactions))
	}
	tx := resp.Transactions[0]
	if tx.GroupName != "Groceries" {
		t.Errorf("group_name = %q, want %q", tx.GroupName, "Groceries")
	}
	if tx.GroupID == nil || *tx.GroupID != 10 {
		t.Errorf("group_id = %v, want 10", tx.GroupID)
	}
}
