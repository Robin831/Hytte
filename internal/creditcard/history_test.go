package creditcard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestMonthlyHistoryHandler_MissingCardID(t *testing.T) {
	db := setupTestDB(t)
	handler := MonthlyHistoryHandler(db)

	req := httptest.NewRequest(http.MethodGet, "/api/credit-card/monthly-history", nil)
	req = withUser(req, 1)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestMonthlyHistoryHandler_EmptyResult(t *testing.T) {
	db := setupTestDB(t)
	handler := MonthlyHistoryHandler(db)

	req := httptest.NewRequest(http.MethodGet, "/api/credit-card/monthly-history?credit_card_id=42", nil)
	req = withUser(req, 1)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}

	var resp MonthlyHistoryResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Months) != 6 {
		t.Errorf("months count = %d, want 6 (default)", len(resp.Months))
	}
	if len(resp.Rows) != 0 {
		t.Errorf("rows count = %d, want 0 (no groups)", len(resp.Rows))
	}
}

func TestMonthlyHistoryHandler_CustomMonthCount(t *testing.T) {
	db := setupTestDB(t)
	handler := MonthlyHistoryHandler(db)

	req := httptest.NewRequest(http.MethodGet, "/api/credit-card/monthly-history?credit_card_id=1&months=3", nil)
	req = withUser(req, 1)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}

	var resp MonthlyHistoryResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Months) != 3 {
		t.Errorf("months = %d, want 3", len(resp.Months))
	}
}

func TestMonthlyHistoryHandler_GroupTotals(t *testing.T) {
	db := setupTestDB(t)

	if _, err := db.Exec(`
		INSERT INTO credit_card_groups (id, user_id, name, sort_order) VALUES (1, 1, 'Mat', 0), (2, 1, 'Diverse', 1)
	`); err != nil {
		t.Fatalf("insert groups: %v", err)
	}

	// Use dates in the current month so they fall within the handler's rolling window.
	now := time.Now()
	currentMonth := now.Format("2006-01")
	date1 := currentMonth + "-10"
	date2 := currentMonth + "-20"

	if _, err := db.Exec(`
		INSERT INTO credit_card_transactions
			(user_id, credit_card_id, transaksjonsdato, beskrivelse, belop, belop_i_valuta, is_pending, is_innbetaling, group_id)
		VALUES
			(1, 'card1', ?, 'Rema', -300, -300, 0, 0, 1),
			(1, 'card1', ?, 'Kiwi', -200, -200, 0, 0, 1)
	`, date1, date2); err != nil {
		t.Fatalf("insert transactions: %v", err)
	}

	handler := MonthlyHistoryHandler(db)
	req := httptest.NewRequest(http.MethodGet, "/api/credit-card/monthly-history?credit_card_id=card1&months=3", nil)
	req = withUser(req, 1)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", rr.Code, rr.Body.String())
	}

	var resp MonthlyHistoryResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Find "Mat" row.
	var matRow *MonthlyHistoryRow
	for i := range resp.Rows {
		if resp.Rows[i].GroupName == "Mat" {
			matRow = &resp.Rows[i]
			break
		}
	}
	if matRow == nil {
		t.Fatal("Mat group row not found in response")
	}
	if matRow.Totals[currentMonth] != 500 {
		t.Errorf("Mat total for %s = %f, want 500", currentMonth, matRow.Totals[currentMonth])
	}

	// Month total should also reflect 500.
	if resp.MonthTotals[currentMonth] != 500 {
		t.Errorf("month total for %s = %f, want 500", currentMonth, resp.MonthTotals[currentMonth])
	}
}

func TestMonthlyHistoryHandler_UnassignedMergedIntoDiverse(t *testing.T) {
	db := setupTestDB(t)

	if _, err := db.Exec(`
		INSERT INTO credit_card_groups (id, user_id, name, sort_order) VALUES (1, 1, 'Diverse', 0)
	`); err != nil {
		t.Fatalf("insert group: %v", err)
	}

	// Transaction with no group_id (unassigned), dated in the current month.
	now := time.Now()
	currentMonth := now.Format("2006-01")
	date := currentMonth + "-05"

	if _, err := db.Exec(`
		INSERT INTO credit_card_transactions
			(user_id, credit_card_id, transaksjonsdato, beskrivelse, belop, belop_i_valuta, is_pending, is_innbetaling, group_id)
		VALUES (1, 'card1', ?, 'Unknown Shop', -150, -150, 0, 0, NULL)
	`, date); err != nil {
		t.Fatalf("insert transaction: %v", err)
	}

	handler := MonthlyHistoryHandler(db)
	req := httptest.NewRequest(http.MethodGet, "/api/credit-card/monthly-history?credit_card_id=card1&months=3", nil)
	req = withUser(req, 1)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", rr.Code, rr.Body.String())
	}

	var resp MonthlyHistoryResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	var diverseRow *MonthlyHistoryRow
	for i := range resp.Rows {
		if resp.Rows[i].GroupName == "Diverse" {
			diverseRow = &resp.Rows[i]
			break
		}
	}
	if diverseRow == nil {
		t.Fatal("Diverse group row not found")
	}
	if diverseRow.Totals[currentMonth] != 150 {
		t.Errorf("Diverse total for %s = %f, want 150", currentMonth, diverseRow.Totals[currentMonth])
	}
}

func TestMonthlyHistoryHandler_ExcludesInnbetaling(t *testing.T) {
	db := setupTestDB(t)

	if _, err := db.Exec(`
		INSERT INTO credit_card_groups (id, user_id, name, sort_order) VALUES (1, 1, 'Mat', 0)
	`); err != nil {
		t.Fatalf("insert group: %v", err)
	}

	// One expense and one innbetaling (payment) — payment must be excluded.
	// Use dates in the current month so they fall within the handler's rolling window.
	now := time.Now()
	currentMonth := now.Format("2006-01")
	date1 := currentMonth + "-10"
	date2 := currentMonth + "-15"

	if _, err := db.Exec(`
		INSERT INTO credit_card_transactions
			(user_id, credit_card_id, transaksjonsdato, beskrivelse, belop, belop_i_valuta, is_pending, is_innbetaling, group_id)
		VALUES
			(1, 'card1', ?, 'Rema', -100, -100, 0, 0, 1),
			(1, 'card1', ?, 'Innbetaling', 500, 500, 0, 1, NULL)
	`, date1, date2); err != nil {
		t.Fatalf("insert transactions: %v", err)
	}

	handler := MonthlyHistoryHandler(db)
	req := httptest.NewRequest(http.MethodGet, "/api/credit-card/monthly-history?credit_card_id=card1&months=3", nil)
	req = withUser(req, 1)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", rr.Code, rr.Body.String())
	}

	var resp MonthlyHistoryResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Month total should only count the expense (100), not the payment.
	if resp.MonthTotals[currentMonth] != 100 {
		t.Errorf("month total for %s = %f, want 100 (payments excluded)", currentMonth, resp.MonthTotals[currentMonth])
	}
}

func TestMonthlyHistoryHandler_RowsOrderedBySortOrder(t *testing.T) {
	db := setupTestDB(t)

	if _, err := db.Exec(`
		INSERT INTO credit_card_groups (id, user_id, name, sort_order)
		VALUES (1, 1, 'Subscriptions', 2), (2, 1, 'Mat', 0), (3, 1, 'Diverse', 1)
	`); err != nil {
		t.Fatalf("insert groups: %v", err)
	}

	handler := MonthlyHistoryHandler(db)
	req := httptest.NewRequest(http.MethodGet, "/api/credit-card/monthly-history?credit_card_id=card1&months=1", nil)
	req = withUser(req, 1)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", rr.Code, rr.Body.String())
	}

	var resp MonthlyHistoryResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Rows) != 3 {
		t.Fatalf("rows = %d, want 3", len(resp.Rows))
	}
	want := []string{"Mat", "Diverse", "Subscriptions"}
	for i, name := range want {
		if resp.Rows[i].GroupName != name {
			t.Errorf("rows[%d].GroupName = %q, want %q", i, resp.Rows[i].GroupName, name)
		}
	}
}
