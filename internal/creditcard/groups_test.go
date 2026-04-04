package creditcard

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Robin831/Hytte/internal/encryption"
	"github.com/go-chi/chi/v5"
)

// withChiParam injects a chi URL parameter into the request context.
func withChiParam(r *http.Request, key, value string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, value)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

// --- EnsureDefaultGroup tests ---

func TestEnsureDefaultGroup_CreatesGroup(t *testing.T) {
	db := setupTestDB(t)

	id, err := EnsureDefaultGroup(db, 1)
	if err != nil {
		t.Fatalf("EnsureDefaultGroup: %v", err)
	}
	if id == 0 {
		t.Fatal("expected a non-zero group id")
	}

	var name string
	if err := db.QueryRow(`SELECT name FROM credit_card_groups WHERE id = ?`, id).Scan(&name); err != nil {
		t.Fatalf("query group: %v", err)
	}
	if name != "Diverse" {
		t.Errorf("group name = %q, want Diverse", name)
	}
}

func TestEnsureDefaultGroup_IdempotentWhenGroupsExist(t *testing.T) {
	db := setupTestDB(t)

	// Create a group first.
	if _, err := db.Exec(`INSERT INTO credit_card_groups (user_id, name, sort_order) VALUES (1, 'My Group', 0)`); err != nil {
		t.Fatalf("insert group: %v", err)
	}

	id, err := EnsureDefaultGroup(db, 1)
	if err != nil {
		t.Fatalf("EnsureDefaultGroup: %v", err)
	}
	if id != 0 {
		t.Errorf("expected id=0 (no new group), got %d", id)
	}

	var count int
	db.QueryRow(`SELECT COUNT(*) FROM credit_card_groups WHERE user_id = 1`).Scan(&count) //nolint:errcheck
	if count != 1 {
		t.Errorf("expected 1 group, got %d", count)
	}
}

// --- GroupsListHandler tests ---

func TestGroupsListHandler_Empty(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("GET", "/credit-card/groups", nil), 1)
	rec := httptest.NewRecorder()
	GroupsListHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var groups []Group
	if err := json.NewDecoder(rec.Body).Decode(&groups); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(groups) != 0 {
		t.Errorf("expected 0 groups, got %d", len(groups))
	}
}

func TestGroupsListHandler_OrderedBySortOrder(t *testing.T) {
	db := setupTestDB(t)

	db.Exec(`INSERT INTO credit_card_groups (user_id, name, sort_order) VALUES (1, 'Beta', 2)`)   //nolint:errcheck
	db.Exec(`INSERT INTO credit_card_groups (user_id, name, sort_order) VALUES (1, 'Alpha', 1)`)  //nolint:errcheck

	req := withUser(httptest.NewRequest("GET", "/credit-card/groups", nil), 1)
	rec := httptest.NewRecorder()
	GroupsListHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var groups []Group
	json.NewDecoder(rec.Body).Decode(&groups) //nolint:errcheck
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}
	if groups[0].Name != "Alpha" {
		t.Errorf("first group = %q, want Alpha", groups[0].Name)
	}
}

// --- GroupsCreateHandler tests ---

func TestGroupsCreateHandler_Success(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"name": "Food", "sort_order": 1}`
	req := withUser(httptest.NewRequest("POST", "/credit-card/groups", strings.NewReader(payload)), 1)
	rec := httptest.NewRecorder()
	GroupsCreateHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var g Group
	if err := json.NewDecoder(rec.Body).Decode(&g); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if g.Name != "Food" {
		t.Errorf("name = %q, want Food", g.Name)
	}
	if g.SortOrder != 1 {
		t.Errorf("sort_order = %d, want 1", g.SortOrder)
	}
	if g.ID == 0 {
		t.Error("expected non-zero id")
	}
}

func TestGroupsCreateHandler_MissingName(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("POST", "/credit-card/groups", strings.NewReader(`{"name": "  "}`)), 1)
	rec := httptest.NewRecorder()
	GroupsCreateHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

// --- GroupsUpdateHandler tests ---

func TestGroupsUpdateHandler_Success(t *testing.T) {
	db := setupTestDB(t)

	db.Exec(`INSERT INTO credit_card_groups (id, user_id, name, sort_order) VALUES (10, 1, 'Old', 0)`) //nolint:errcheck

	payload := `{"name": "New Name"}`
	req := withUser(httptest.NewRequest("PUT", "/credit-card/groups/10", strings.NewReader(payload)), 1)
	req = withChiParam(req, "id", "10")
	rec := httptest.NewRecorder()
	GroupsUpdateHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var name string
	db.QueryRow(`SELECT name FROM credit_card_groups WHERE id = 10`).Scan(&name) //nolint:errcheck
	if name != "New Name" {
		t.Errorf("name = %q, want New Name", name)
	}
}

func TestGroupsUpdateHandler_NotFound(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("PUT", "/credit-card/groups/999", strings.NewReader(`{"name":"X"}`)), 1)
	req = withChiParam(req, "id", "999")
	rec := httptest.NewRecorder()
	GroupsUpdateHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

// --- GroupsReorderHandler tests ---

func TestGroupsReorderHandler_Success(t *testing.T) {
	db := setupTestDB(t)

	db.Exec(`INSERT INTO credit_card_groups (id, user_id, name, sort_order) VALUES (1, 1, 'A', 0)`) //nolint:errcheck
	db.Exec(`INSERT INTO credit_card_groups (id, user_id, name, sort_order) VALUES (2, 1, 'B', 1)`) //nolint:errcheck

	payload := `[{"id":1,"sort_order":5},{"id":2,"sort_order":3}]`
	req := withUser(httptest.NewRequest("PUT", "/credit-card/groups/reorder", strings.NewReader(payload)), 1)
	rec := httptest.NewRecorder()
	GroupsReorderHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var so int
	db.QueryRow(`SELECT sort_order FROM credit_card_groups WHERE id = 1`).Scan(&so) //nolint:errcheck
	if so != 5 {
		t.Errorf("sort_order for id=1 = %d, want 5", so)
	}
}

// --- GroupsDeleteHandler tests ---

func TestGroupsDeleteHandler_Success(t *testing.T) {
	db := setupTestDB(t)

	db.Exec(`INSERT INTO credit_card_groups (id, user_id, name, sort_order) VALUES (20, 1, 'ToDelete', 0)`) //nolint:errcheck

	req := withUser(httptest.NewRequest("DELETE", "/credit-card/groups/20", nil), 1)
	req = withChiParam(req, "id", "20")
	rec := httptest.NewRecorder()
	GroupsDeleteHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var count int
	db.QueryRow(`SELECT COUNT(*) FROM credit_card_groups WHERE id = 20`).Scan(&count) //nolint:errcheck
	if count != 0 {
		t.Errorf("expected group to be deleted, count = %d", count)
	}
}

func TestGroupsDeleteHandler_NotFound(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("DELETE", "/credit-card/groups/999", nil), 1)
	req = withChiParam(req, "id", "999")
	rec := httptest.NewRecorder()
	GroupsDeleteHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

// --- RulesListHandler tests ---

func TestRulesListHandler_Empty(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("GET", "/credit-card/rules", nil), 1)
	rec := httptest.NewRecorder()
	RulesListHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var rules []MerchantRule
	json.NewDecoder(rec.Body).Decode(&rules) //nolint:errcheck
	if len(rules) != 0 {
		t.Errorf("expected 0 rules, got %d", len(rules))
	}
}

// --- RulesCreateHandler tests ---

func TestRulesCreateHandler_Success(t *testing.T) {
	db := setupTestDB(t)

	db.Exec(`INSERT INTO credit_card_groups (id, user_id, name, sort_order) VALUES (5, 1, 'Groceries', 0)`) //nolint:errcheck

	payload := `{"merchant_pattern": "REMA", "group_id": 5}`
	req := withUser(httptest.NewRequest("POST", "/credit-card/rules", strings.NewReader(payload)), 1)
	rec := httptest.NewRecorder()
	RulesCreateHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var mr MerchantRule
	json.NewDecoder(rec.Body).Decode(&mr) //nolint:errcheck
	if mr.MerchantPattern != "REMA" {
		t.Errorf("pattern = %q, want REMA", mr.MerchantPattern)
	}
	if mr.GroupID != 5 {
		t.Errorf("group_id = %d, want 5", mr.GroupID)
	}
}

func TestRulesCreateHandler_GroupNotFound(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"merchant_pattern": "REMA", "group_id": 999}`
	req := withUser(httptest.NewRequest("POST", "/credit-card/rules", strings.NewReader(payload)), 1)
	rec := httptest.NewRecorder()
	RulesCreateHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestRulesCreateHandler_MissingPattern(t *testing.T) {
	db := setupTestDB(t)

	db.Exec(`INSERT INTO credit_card_groups (id, user_id, name, sort_order) VALUES (5, 1, 'G', 0)`) //nolint:errcheck

	payload := `{"merchant_pattern": "", "group_id": 5}`
	req := withUser(httptest.NewRequest("POST", "/credit-card/rules", strings.NewReader(payload)), 1)
	rec := httptest.NewRecorder()
	RulesCreateHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

// --- RulesDeleteHandler tests ---

func TestRulesDeleteHandler_Success(t *testing.T) {
	db := setupTestDB(t)

	db.Exec(`INSERT INTO credit_card_groups (id, user_id, name, sort_order) VALUES (5, 1, 'G', 0)`)             //nolint:errcheck
	db.Exec(`INSERT INTO merchant_group_rules (id, user_id, merchant_pattern, group_id) VALUES (7, 1, 'X', 5)`) //nolint:errcheck

	req := withUser(httptest.NewRequest("DELETE", "/credit-card/rules/7", nil), 1)
	req = withChiParam(req, "id", "7")
	rec := httptest.NewRecorder()
	RulesDeleteHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var count int
	db.QueryRow(`SELECT COUNT(*) FROM merchant_group_rules WHERE id = 7`).Scan(&count) //nolint:errcheck
	if count != 0 {
		t.Errorf("expected rule to be deleted, count = %d", count)
	}
}

func TestRulesDeleteHandler_NotFound(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("DELETE", "/credit-card/rules/999", nil), 1)
	req = withChiParam(req, "id", "999")
	rec := httptest.NewRecorder()
	RulesDeleteHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

// --- TransactionsBulkAssignHandler tests ---

func TestBulkAssignHandler_Assign(t *testing.T) {
	db := setupTestDB(t)
	t.Setenv("ENCRYPTION_KEY", "test-key-for-creditcard-tests")
	encryption.ResetEncryptionKey()

	db.Exec(`INSERT INTO credit_card_groups (id, user_id, name, sort_order) VALUES (1, 1, 'G', 0)`) //nolint:errcheck

	encDesc, _ := encryption.EncryptField("Coffee Shop")
	db.Exec(`INSERT INTO credit_card_transactions
		(id, user_id, credit_card_id, transaksjonsdato, beskrivelse, belop, imported_at)
		VALUES (10, 1, 'card1', '2026-01-15', ?, -45.0, '2026-01-20')`, encDesc) //nolint:errcheck
	db.Exec(`INSERT INTO credit_card_transactions
		(id, user_id, credit_card_id, transaksjonsdato, beskrivelse, belop, imported_at)
		VALUES (11, 1, 'card1', '2026-01-20', ?, -60.0, '2026-01-25')`, encDesc) //nolint:errcheck

	payload := `{"transaction_ids": [10, 11], "group_id": 1}`
	req := withUser(httptest.NewRequest("POST", "/credit-card/transactions/bulk-assign", strings.NewReader(payload)), 1)
	rec := httptest.NewRecorder()
	TransactionsBulkAssignHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]int
	json.NewDecoder(rec.Body).Decode(&resp) //nolint:errcheck
	if resp["updated"] != 2 {
		t.Errorf("updated = %d, want 2", resp["updated"])
	}

	var gid int64
	db.QueryRow(`SELECT group_id FROM credit_card_transactions WHERE id = 10`).Scan(&gid) //nolint:errcheck
	if gid != 1 {
		t.Errorf("group_id = %d, want 1", gid)
	}
}

func TestBulkAssignHandler_Unassign(t *testing.T) {
	db := setupTestDB(t)
	t.Setenv("ENCRYPTION_KEY", "test-key-for-creditcard-tests")
	encryption.ResetEncryptionKey()

	db.Exec(`INSERT INTO credit_card_groups (id, user_id, name, sort_order) VALUES (1, 1, 'G', 0)`) //nolint:errcheck

	encDesc, _ := encryption.EncryptField("Shop")
	db.Exec(`INSERT INTO credit_card_transactions
		(id, user_id, credit_card_id, transaksjonsdato, beskrivelse, belop, group_id, imported_at)
		VALUES (10, 1, 'card1', '2026-01-15', ?, -45.0, 1, '2026-01-20')`, encDesc) //nolint:errcheck

	// Unassign by sending group_id: null
	payload := `{"transaction_ids": [10], "group_id": null}`
	req := withUser(httptest.NewRequest("POST", "/credit-card/transactions/bulk-assign", strings.NewReader(payload)), 1)
	rec := httptest.NewRecorder()
	TransactionsBulkAssignHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var gid *int64
	db.QueryRow(`SELECT group_id FROM credit_card_transactions WHERE id = 10`).Scan(&gid) //nolint:errcheck
	if gid != nil {
		t.Errorf("expected group_id to be NULL, got %v", *gid)
	}
}

func TestBulkAssignHandler_EmptyIDs(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"transaction_ids": []}`
	req := withUser(httptest.NewRequest("POST", "/credit-card/transactions/bulk-assign", strings.NewReader(payload)), 1)
	rec := httptest.NewRecorder()
	TransactionsBulkAssignHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

// --- RecurringMerchantsHandler tests ---

func TestRecurringMerchantsHandler_ReturnsSuggestions(t *testing.T) {
	db := setupTestDB(t)
	t.Setenv("ENCRYPTION_KEY", "test-key-for-creditcard-tests")
	encryption.ResetEncryptionKey()

	// Insert transactions: "Rema 1000" in 3 months, "Kiosk" in 1 month.
	insertTx := func(id int, desc, date string) {
		enc, _ := encryption.EncryptField(desc)
		db.Exec(`INSERT INTO credit_card_transactions
			(id, user_id, credit_card_id, transaksjonsdato, beskrivelse, belop, imported_at)
			VALUES (?, 1, 'card1', ?, ?, -100.0, '2026-01-01')`, id, date, enc) //nolint:errcheck
	}

	insertTx(1, "Rema 1000", "2026-01-10")
	insertTx(2, "Rema 1000", "2026-02-10")
	insertTx(3, "Rema 1000", "2026-03-10")
	insertTx(4, "Kiosk", "2026-01-05")

	req := withUser(httptest.NewRequest("GET", "/credit-card/recurring-merchants", nil), 1)
	rec := httptest.NewRecorder()
	RecurringMerchantsHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	type Suggestion struct {
		Beskrivelse string   `json:"beskrivelse"`
		MonthCount  int      `json:"month_count"`
		Months      []string `json:"months"`
	}
	var suggestions []Suggestion
	if err := json.NewDecoder(rec.Body).Decode(&suggestions); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(suggestions) != 1 {
		t.Fatalf("expected 1 suggestion, got %d", len(suggestions))
	}
	if suggestions[0].Beskrivelse != "Rema 1000" {
		t.Errorf("beskrivelse = %q, want Rema 1000", suggestions[0].Beskrivelse)
	}
	if suggestions[0].MonthCount != 3 {
		t.Errorf("month_count = %d, want 3", suggestions[0].MonthCount)
	}
}

func TestRecurringMerchantsHandler_ExcludesPayments(t *testing.T) {
	db := setupTestDB(t)
	t.Setenv("ENCRYPTION_KEY", "test-key-for-creditcard-tests")
	encryption.ResetEncryptionKey()

	// Insert "Innbetaling" transactions (payments) across multiple months — should be excluded.
	insertPayment := func(id int, date string) {
		enc, _ := encryption.EncryptField("Innbetaling")
		db.Exec(`INSERT INTO credit_card_transactions
			(id, user_id, credit_card_id, transaksjonsdato, beskrivelse, belop, is_innbetaling, imported_at)
			VALUES (?, 1, 'card1', ?, ?, 500.0, 1, '2026-01-01')`, id, date, enc) //nolint:errcheck
	}

	insertPayment(1, "2026-01-01")
	insertPayment(2, "2026-02-01")
	insertPayment(3, "2026-03-01")

	req := withUser(httptest.NewRequest("GET", "/credit-card/recurring-merchants", nil), 1)
	rec := httptest.NewRecorder()
	RecurringMerchantsHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var suggestions []struct{ Beskrivelse string }
	json.NewDecoder(rec.Body).Decode(&suggestions) //nolint:errcheck
	if len(suggestions) != 0 {
		t.Errorf("expected 0 suggestions (payments excluded), got %d", len(suggestions))
	}
}

// --- ReapplyRulesHandler tests ---

func TestReapplyRulesHandler_MissingCardID(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("POST", "/credit-card/transactions/reapply-rules", strings.NewReader(`{}`)), 1)
	rec := httptest.NewRecorder()
	ReapplyRulesHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestReapplyRulesHandler_NoRules(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"credit_card_id": "card1"}`
	req := withUser(httptest.NewRequest("POST", "/credit-card/transactions/reapply-rules", strings.NewReader(payload)), 1)
	rec := httptest.NewRecorder()
	ReapplyRulesHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]int
	json.NewDecoder(rec.Body).Decode(&resp) //nolint:errcheck
	if resp["updated"] != 0 {
		t.Errorf("updated = %d, want 0", resp["updated"])
	}
}

func TestReapplyRulesHandler_AssignsUngrouped(t *testing.T) {
	db := setupTestDB(t)
	t.Setenv("ENCRYPTION_KEY", "test-key-for-creditcard-tests")
	encryption.ResetEncryptionKey()

	// Create a group and a rule matching "Rema".
	db.Exec(`INSERT INTO credit_card_groups (id, user_id, name, sort_order) VALUES (1, 1, 'Groceries', 0)`) //nolint:errcheck
	db.Exec(`INSERT INTO merchant_group_rules (user_id, merchant_pattern, group_id) VALUES (1, 'Rema', 1)`) //nolint:errcheck

	// Insert one ungrouped transaction that matches the rule, one that doesn't.
	encRema, _ := encryption.EncryptField("Rema 1000")
	encOther, _ := encryption.EncryptField("Other Shop")
	db.Exec(`INSERT INTO credit_card_transactions (id, user_id, credit_card_id, transaksjonsdato, beskrivelse, belop, imported_at) VALUES (10, 1, 'card1', '2026-01-10', ?, -100.0, '2026-01-20')`, encRema)   //nolint:errcheck
	db.Exec(`INSERT INTO credit_card_transactions (id, user_id, credit_card_id, transaksjonsdato, beskrivelse, belop, imported_at) VALUES (11, 1, 'card1', '2026-01-11', ?, -50.0, '2026-01-20')`, encOther) //nolint:errcheck

	payload := `{"credit_card_id": "card1"}`
	req := withUser(httptest.NewRequest("POST", "/credit-card/transactions/reapply-rules", strings.NewReader(payload)), 1)
	rec := httptest.NewRecorder()
	ReapplyRulesHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]int
	json.NewDecoder(rec.Body).Decode(&resp) //nolint:errcheck
	if resp["updated"] != 1 {
		t.Errorf("updated = %d, want 1", resp["updated"])
	}

	var gid sql.NullInt64
	db.QueryRow(`SELECT group_id FROM credit_card_transactions WHERE id = 10`).Scan(&gid) //nolint:errcheck
	if !gid.Valid || gid.Int64 != 1 {
		t.Errorf("tx 10 group_id = %v, want 1", gid)
	}

	var gid2 sql.NullInt64
	db.QueryRow(`SELECT group_id FROM credit_card_transactions WHERE id = 11`).Scan(&gid2) //nolint:errcheck
	if gid2.Valid {
		t.Errorf("tx 11 should remain ungrouped, got group_id = %d", gid2.Int64)
	}
}

func TestReapplyRulesHandler_IncludesDiverseGroup(t *testing.T) {
	db := setupTestDB(t)
	t.Setenv("ENCRYPTION_KEY", "test-key-for-creditcard-tests")
	encryption.ResetEncryptionKey()

	// Create a 'Diverse' group and a target group with a matching rule.
	db.Exec(`INSERT INTO credit_card_groups (id, user_id, name, sort_order) VALUES (1, 1, 'Diverse', 0)`)   //nolint:errcheck
	db.Exec(`INSERT INTO credit_card_groups (id, user_id, name, sort_order) VALUES (2, 1, 'Groceries', 1)`) //nolint:errcheck
	db.Exec(`INSERT INTO merchant_group_rules (user_id, merchant_pattern, group_id) VALUES (1, 'Rema', 2)`) //nolint:errcheck

	// Insert a transaction already in Diverse — it should be re-assigned.
	encRema, _ := encryption.EncryptField("Rema 1000")
	db.Exec(`INSERT INTO credit_card_transactions (id, user_id, credit_card_id, transaksjonsdato, beskrivelse, belop, group_id, imported_at) VALUES (20, 1, 'card1', '2026-01-10', ?, -100.0, 1, '2026-01-20')`, encRema) //nolint:errcheck

	payload := `{"credit_card_id": "card1"}`
	req := withUser(httptest.NewRequest("POST", "/credit-card/transactions/reapply-rules", strings.NewReader(payload)), 1)
	rec := httptest.NewRecorder()
	ReapplyRulesHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]int
	json.NewDecoder(rec.Body).Decode(&resp) //nolint:errcheck
	if resp["updated"] != 1 {
		t.Errorf("updated = %d, want 1", resp["updated"])
	}

	var gid int64
	db.QueryRow(`SELECT group_id FROM credit_card_transactions WHERE id = 20`).Scan(&gid) //nolint:errcheck
	if gid != 2 {
		t.Errorf("tx 20 group_id = %d, want 2 (Groceries)", gid)
	}
}

func TestReapplyRulesHandler_NoMatches(t *testing.T) {
	db := setupTestDB(t)
	t.Setenv("ENCRYPTION_KEY", "test-key-for-creditcard-tests")
	encryption.ResetEncryptionKey()

	db.Exec(`INSERT INTO credit_card_groups (id, user_id, name, sort_order) VALUES (1, 1, 'Groceries', 0)`)  //nolint:errcheck
	db.Exec(`INSERT INTO merchant_group_rules (user_id, merchant_pattern, group_id) VALUES (1, 'Rema', 1)`)  //nolint:errcheck

	encOther, _ := encryption.EncryptField("Unrelated Merchant")
	db.Exec(`INSERT INTO credit_card_transactions (id, user_id, credit_card_id, transaksjonsdato, beskrivelse, belop, imported_at) VALUES (30, 1, 'card1', '2026-01-10', ?, -75.0, '2026-01-20')`, encOther) //nolint:errcheck

	payload := `{"credit_card_id": "card1"}`
	req := withUser(httptest.NewRequest("POST", "/credit-card/transactions/reapply-rules", strings.NewReader(payload)), 1)
	rec := httptest.NewRecorder()
	ReapplyRulesHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]int
	json.NewDecoder(rec.Body).Decode(&resp) //nolint:errcheck
	if resp["updated"] != 0 {
		t.Errorf("updated = %d, want 0", resp["updated"])
	}
}

// TestImportConfirmSeeds_DefaultGroup verifies that ImportConfirmHandler seeds
// the 'Diverse' group on a user's first import.
func TestImportConfirmSeeds_DefaultGroup(t *testing.T) {
	db := setupTestDB(t)
	t.Setenv("ENCRYPTION_KEY", "test-key-for-creditcard-tests")
	encryption.ResetEncryptionKey()

	csvLine := "Transaksjonsdato;Bokforingsdato;Beskrivelse;Mottakers kontonummer;KID;Belop;Belop i valuta;Utsatt;Utsatt periode;Utlopsdato\n" +
		"15.03.2026;16.03.2026;Rema 1000;;; −100,00;−100,00;;;;\n"

	// Parse the CSV to get a valid DNBRow.
	rows, _ := parseDNBCSV(strings.NewReader(csvLine))
	if len(rows) == 0 {
		t.Fatal("no rows parsed")
	}

	payload, _ := json.Marshal(map[string]any{
		"credit_card_id": "card-test",
		"rows":           rows,
	})

	req := withUser(httptest.NewRequest("POST", "/credit-card/import/confirm", bytes.NewReader(payload)), 1)
	rec := httptest.NewRecorder()
	ImportConfirmHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify the 'Diverse' default group was seeded.
	var count int
	db.QueryRow(`SELECT COUNT(*) FROM credit_card_groups WHERE user_id = 1 AND name = 'Diverse'`).Scan(&count) //nolint:errcheck
	if count != 1 {
		t.Errorf("expected 1 Diverse group, got %d", count)
	}
}
