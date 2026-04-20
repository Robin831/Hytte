package creditcard

import (
	"encoding/json"
	"fmt"
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

func TestTransactionsListHandler_DeferredCarryoverShownInTargetMonth(t *testing.T) {
	db := setupTestDB(t)

	// March transaction deferred forward to April; plus a regular April transaction.
	encDeferred, err := encryption.EncryptField("Deferred March")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	encApril, err := encryption.EncryptField("Plain April")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	if _, err := db.Exec(`
		INSERT INTO credit_card_transactions
			(user_id, credit_card_id, transaksjonsdato, beskrivelse, belop, belop_i_valuta, is_pending, is_innbetaling, deferred_to_next_month)
		VALUES
			(1, '1', '2026-03-20', ?, -300, -300, 0, 0, 1),
			(1, '1', '2026-04-05', ?, -100, -100, 0, 0, 0)
	`, encDeferred, encApril); err != nil {
		t.Fatalf("insert transactions: %v", err)
	}

	handler := TransactionsListHandler(db)

	// April list must include both rows: the regular April txn and the deferred
	// March txn carried forward, with the carryover flagged appropriately.
	req := httptest.NewRequest(http.MethodGet, "/api/credit-card/transactions?credit_card_id=1&month=2026-04", nil)
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
	if len(resp.Transactions) != 2 {
		t.Fatalf("expected 2 transactions for April, got %d", len(resp.Transactions))
	}

	var carryover, plain *TransactionRow
	for i := range resp.Transactions {
		switch resp.Transactions[i].Beskrivelse {
		case "Deferred March":
			carryover = &resp.Transactions[i]
		case "Plain April":
			plain = &resp.Transactions[i]
		}
	}
	if carryover == nil {
		t.Fatal("deferred March transaction missing from April list")
	}
	if !carryover.DeferredToNextMonth {
		t.Errorf("carryover deferred_to_next_month = false, want true")
	}
	if !carryover.DeferredFromPreviousMonth {
		t.Errorf("carryover deferred_from_previous_month = false, want true")
	}
	if plain == nil {
		t.Fatal("plain April transaction missing from April list")
	}
	if plain.DeferredFromPreviousMonth {
		t.Errorf("plain April transaction deferred_from_previous_month = true, want false")
	}

	// Source month (March) still shows the deferred row, but flagged as deferred-away,
	// not as a carryover.
	reqMar := httptest.NewRequest(http.MethodGet, "/api/credit-card/transactions?credit_card_id=1&month=2026-03", nil)
	reqMar = withUser(reqMar, 1)
	rrMar := httptest.NewRecorder()
	handler.ServeHTTP(rrMar, reqMar)

	if rrMar.Code != http.StatusOK {
		t.Fatalf("march status = %d; body: %s", rrMar.Code, rrMar.Body.String())
	}
	var respMar TransactionsListResponse
	if err := json.NewDecoder(rrMar.Body).Decode(&respMar); err != nil {
		t.Fatalf("march decode: %v", err)
	}
	if len(respMar.Transactions) != 1 {
		t.Fatalf("expected 1 transaction for March, got %d", len(respMar.Transactions))
	}
	if !respMar.Transactions[0].DeferredToNextMonth {
		t.Errorf("march row deferred_to_next_month = false, want true")
	}
	if respMar.Transactions[0].DeferredFromPreviousMonth {
		t.Errorf("march row deferred_from_previous_month = true, want false")
	}
}

func TestTransactionsListHandler_PendingPreviousMonthNotCarriedOver(t *testing.T) {
	db := setupTestDB(t)

	// A pending row from the previous month should never appear as a carryover,
	// matching the rule that only settled transactions can be deferred.
	encDesc, err := encryption.EncryptField("Pending March")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO credit_card_transactions
			(user_id, credit_card_id, transaksjonsdato, beskrivelse, belop, belop_i_valuta, is_pending, is_innbetaling, deferred_to_next_month)
		VALUES (1, '1', '2026-03-25', ?, -50, -50, 1, 0, 1)
	`, encDesc); err != nil {
		t.Fatalf("insert transaction: %v", err)
	}

	handler := TransactionsListHandler(db)
	req := httptest.NewRequest(http.MethodGet, "/api/credit-card/transactions?credit_card_id=1&month=2026-04", nil)
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
	if len(resp.Transactions) != 0 {
		t.Errorf("expected 0 transactions for April, got %d", len(resp.Transactions))
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

// --- TransactionDeferHandler tests ---

func TestTransactionDeferHandler_InvalidID(t *testing.T) {
	db := setupTestDB(t)
	handler := TransactionDeferHandler(db)

	req := httptest.NewRequest(http.MethodPatch, "/api/credit-card/transactions/abc/defer", nil)
	req = withUser(req, 1)
	req = withChiParam(req, "id", "abc")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestTransactionDeferHandler_NotFound(t *testing.T) {
	db := setupTestDB(t)
	handler := TransactionDeferHandler(db)

	req := httptest.NewRequest(http.MethodPatch, "/api/credit-card/transactions/999/defer", nil)
	req = withUser(req, 1)
	req = withChiParam(req, "id", "999")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusNotFound)
	}
}

func TestTransactionDeferHandler_TogglesDeferred(t *testing.T) {
	db := setupTestDB(t)

	encDesc, err := encryption.EncryptField("Test merchant")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	res, err := db.Exec(`
		INSERT INTO credit_card_transactions
			(user_id, credit_card_id, transaksjonsdato, beskrivelse, belop, belop_i_valuta, is_pending, is_innbetaling, deferred_to_next_month)
		VALUES (1, 'card1', '2026-03-28', ?, -100.0, -100.0, 0, 0, 0)
	`, encDesc)
	if err != nil {
		t.Fatalf("insert transaction: %v", err)
	}
	txID, _ := res.LastInsertId()

	handler := TransactionDeferHandler(db)

	// First call: defer (0 → 1)
	req := httptest.NewRequest(http.MethodPatch, "/api/credit-card/transactions/1/defer", nil)
	req = withUser(req, 1)
	req = withChiParam(req, "id", fmt.Sprintf("%d", txID))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("first defer: status = %d; body: %s", rr.Code, rr.Body.String())
	}
	var resp1 map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp1); err != nil {
		t.Fatalf("decode resp1: %v", err)
	}
	if resp1["deferred_to_next_month"] != true {
		t.Errorf("expected deferred_to_next_month=true, got %v", resp1["deferred_to_next_month"])
	}

	var stored int
	if err := db.QueryRow(`SELECT deferred_to_next_month FROM credit_card_transactions WHERE id = ?`, txID).Scan(&stored); err != nil {
		t.Fatalf("query stored: %v", err)
	}
	if stored != 1 {
		t.Errorf("DB deferred_to_next_month = %d, want 1", stored)
	}

	// Second call: un-defer (1 → 0)
	req2 := httptest.NewRequest(http.MethodPatch, "/api/credit-card/transactions/1/defer", nil)
	req2 = withUser(req2, 1)
	req2 = withChiParam(req2, "id", fmt.Sprintf("%d", txID))
	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, req2)

	if rr2.Code != http.StatusOK {
		t.Fatalf("second defer: status = %d; body: %s", rr2.Code, rr2.Body.String())
	}
	var resp2 map[string]any
	if err := json.NewDecoder(rr2.Body).Decode(&resp2); err != nil {
		t.Fatalf("decode resp2: %v", err)
	}
	if resp2["deferred_to_next_month"] != false {
		t.Errorf("expected deferred_to_next_month=false, got %v", resp2["deferred_to_next_month"])
	}
}

func TestTransactionDeferHandler_PendingTransactionRejected(t *testing.T) {
	db := setupTestDB(t)

	encDesc, err := encryption.EncryptField("Pending merchant")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	res, err := db.Exec(`
		INSERT INTO credit_card_transactions
			(user_id, credit_card_id, transaksjonsdato, beskrivelse, belop, belop_i_valuta, is_pending, is_innbetaling)
		VALUES (1, 'card1', '2026-03-28', ?, -50.0, -50.0, 1, 0)
	`, encDesc)
	if err != nil {
		t.Fatalf("insert transaction: %v", err)
	}
	txID, _ := res.LastInsertId()

	handler := TransactionDeferHandler(db)
	req := httptest.NewRequest(http.MethodPatch, "/api/credit-card/transactions/1/defer", nil)
	req = withUser(req, 1)
	req = withChiParam(req, "id", fmt.Sprintf("%d", txID))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d (pending transaction should be rejected)", rr.Code, http.StatusBadRequest)
	}
}
