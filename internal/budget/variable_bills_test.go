package budget

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// -- Store tests --

func TestVariableBillCRUD(t *testing.T) {
	db := setupTestDB(t)

	// Create
	b := &VariableBill{Name: "Electricity"}
	if err := CreateVariableBill(db, 1, b); err != nil {
		t.Fatalf("CreateVariableBill: %v", err)
	}
	if b.ID == 0 {
		t.Fatal("expected non-zero ID after create")
	}
	if b.UserID != 1 {
		t.Errorf("UserID = %d, want 1", b.UserID)
	}

	// List
	bills, err := ListVariableBills(db, 1, "2024-03")
	if err != nil {
		t.Fatalf("ListVariableBills: %v", err)
	}
	if len(bills) != 1 {
		t.Fatalf("expected 1 bill, got %d", len(bills))
	}
	if bills[0].Name != "Electricity" {
		t.Errorf("Name = %q, want %q", bills[0].Name, "Electricity")
	}

	// Update
	bills[0].Name = "Power"
	if err := UpdateVariableBill(db, 1, bills[0].ID, &bills[0]); err != nil {
		t.Fatalf("UpdateVariableBill: %v", err)
	}
	updated, err := ListVariableBills(db, 1, "2024-03")
	if err != nil {
		t.Fatalf("ListVariableBills after update: %v", err)
	}
	if updated[0].Name != "Power" {
		t.Errorf("Name after update = %q, want %q", updated[0].Name, "Power")
	}

	// Delete
	if err := DeleteVariableBill(db, 1, b.ID); err != nil {
		t.Fatalf("DeleteVariableBill: %v", err)
	}
	empty, err := ListVariableBills(db, 1, "2024-03")
	if err != nil {
		t.Fatalf("ListVariableBills after delete: %v", err)
	}
	if len(empty) != 0 {
		t.Errorf("expected 0 bills after delete, got %d", len(empty))
	}
}

func TestVariableBillUpdateNotFound(t *testing.T) {
	db := setupTestDB(t)
	b := &VariableBill{Name: "X"}
	err := UpdateVariableBill(db, 1, 999, b)
	if err != sql.ErrNoRows {
		t.Errorf("expected ErrNoRows, got %v", err)
	}
}

func TestVariableBillDeleteNotFound(t *testing.T) {
	db := setupTestDB(t)
	err := DeleteVariableBill(db, 1, 999)
	if err != sql.ErrNoRows {
		t.Errorf("expected ErrNoRows, got %v", err)
	}
}

func TestVariableBillUserIsolation(t *testing.T) {
	db := setupTestDB(t)
	// Insert a second user.
	if _, err := db.Exec("INSERT INTO users (id, email, name, google_id) VALUES (2, 'other@example.com', 'Other', 'g456')"); err != nil {
		t.Fatalf("insert user 2: %v", err)
	}

	b := &VariableBill{Name: "Mine"}
	if err := CreateVariableBill(db, 1, b); err != nil {
		t.Fatalf("CreateVariableBill: %v", err)
	}

	// User 2 should not see user 1's bill.
	bills, err := ListVariableBills(db, 2, "2024-03")
	if err != nil {
		t.Fatalf("ListVariableBills user 2: %v", err)
	}
	if len(bills) != 0 {
		t.Errorf("user 2 should see 0 bills, got %d", len(bills))
	}

	// User 2 should not be able to update user 1's bill.
	b.Name = "Hacked"
	if err := UpdateVariableBill(db, 2, b.ID, b); err != sql.ErrNoRows {
		t.Errorf("user 2 update: expected ErrNoRows, got %v", err)
	}

	// User 2 should not be able to delete user 1's bill.
	if err := DeleteVariableBill(db, 2, b.ID); err != sql.ErrNoRows {
		t.Errorf("user 2 delete: expected ErrNoRows, got %v", err)
	}
}

func TestSetMonthEntriesReplaces(t *testing.T) {
	db := setupTestDB(t)

	b := &VariableBill{Name: "Electricity"}
	if err := CreateVariableBill(db, 1, b); err != nil {
		t.Fatalf("CreateVariableBill: %v", err)
	}

	// Set initial entries.
	initial := []VariableEntry{
		{SubName: "Tibber", Amount: 400},
		{SubName: "BKK", Amount: 200},
	}
	if err := SetMonthEntries(db, 1, b.ID, "2024-03", initial); err != nil {
		t.Fatalf("SetMonthEntries initial: %v", err)
	}

	bills, err := ListVariableBills(db, 1, "2024-03")
	if err != nil {
		t.Fatalf("ListVariableBills: %v", err)
	}
	if len(bills[0].Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(bills[0].Entries))
	}

	// Replace with a single entry — verify old ones are removed.
	if err := SetMonthEntries(db, 1, b.ID, "2024-03", []VariableEntry{{SubName: "Tibber", Amount: 500}}); err != nil {
		t.Fatalf("SetMonthEntries replace: %v", err)
	}
	bills, err = ListVariableBills(db, 1, "2024-03")
	if err != nil {
		t.Fatalf("ListVariableBills after replace: %v", err)
	}
	if len(bills[0].Entries) != 1 {
		t.Fatalf("expected 1 entry after replace, got %d", len(bills[0].Entries))
	}
	if bills[0].Entries[0].SubName != "Tibber" {
		t.Errorf("SubName = %q, want Tibber", bills[0].Entries[0].SubName)
	}
	if bills[0].Entries[0].Amount != 500 {
		t.Errorf("Amount = %f, want 500", bills[0].Entries[0].Amount)
	}
}

func TestSetMonthEntriesNotFound(t *testing.T) {
	db := setupTestDB(t)
	err := SetMonthEntries(db, 1, 999, "2024-03", []VariableEntry{{SubName: "X", Amount: 1}})
	if err != sql.ErrNoRows {
		t.Errorf("expected ErrNoRows, got %v", err)
	}
}

func TestCopyMonthEntries(t *testing.T) {
	db := setupTestDB(t)

	b := &VariableBill{Name: "Electricity"}
	if err := CreateVariableBill(db, 1, b); err != nil {
		t.Fatalf("CreateVariableBill: %v", err)
	}

	src := []VariableEntry{
		{SubName: "Tibber", Amount: 400},
		{SubName: "BKK", Amount: 200},
	}
	if err := SetMonthEntries(db, 1, b.ID, "2024-03", src); err != nil {
		t.Fatalf("SetMonthEntries source: %v", err)
	}

	copied, err := CopyMonthEntries(db, 1, b.ID, "2024-03", "2024-04")
	if err != nil {
		t.Fatalf("CopyMonthEntries: %v", err)
	}
	if len(copied) != 2 {
		t.Fatalf("expected 2 copied entries, got %d", len(copied))
	}

	// Verify amounts match source.
	names := map[string]float64{}
	for _, e := range copied {
		names[e.SubName] = e.Amount
	}
	if names["Tibber"] != 400 {
		t.Errorf("Tibber amount = %f, want 400", names["Tibber"])
	}
	if names["BKK"] != 200 {
		t.Errorf("BKK amount = %f, want 200", names["BKK"])
	}

	// Source month must remain intact.
	srcBills, err := ListVariableBills(db, 1, "2024-03")
	if err != nil {
		t.Fatalf("ListVariableBills source: %v", err)
	}
	if len(srcBills[0].Entries) != 2 {
		t.Errorf("source entries should still be 2, got %d", len(srcBills[0].Entries))
	}

	// Target month entries should be populated.
	tgtBills, err := ListVariableBills(db, 1, "2024-04")
	if err != nil {
		t.Fatalf("ListVariableBills target: %v", err)
	}
	if len(tgtBills[0].Entries) != 2 {
		t.Errorf("target entries should be 2, got %d", len(tgtBills[0].Entries))
	}
}

func TestCopyMonthEntriesReplacesTarget(t *testing.T) {
	db := setupTestDB(t)

	b := &VariableBill{Name: "Water"}
	if err := CreateVariableBill(db, 1, b); err != nil {
		t.Fatalf("CreateVariableBill: %v", err)
	}

	if err := SetMonthEntries(db, 1, b.ID, "2024-03", []VariableEntry{{SubName: "A", Amount: 100}}); err != nil {
		t.Fatalf("SetMonthEntries src: %v", err)
	}
	// Pre-populate target with different entry.
	if err := SetMonthEntries(db, 1, b.ID, "2024-04", []VariableEntry{{SubName: "B", Amount: 999}, {SubName: "C", Amount: 111}}); err != nil {
		t.Fatalf("SetMonthEntries target: %v", err)
	}

	copied, err := CopyMonthEntries(db, 1, b.ID, "2024-03", "2024-04")
	if err != nil {
		t.Fatalf("CopyMonthEntries: %v", err)
	}
	// Should have only the copied entry, not the old two.
	if len(copied) != 1 {
		t.Fatalf("expected 1 copied entry, got %d", len(copied))
	}
	if copied[0].SubName != "A" {
		t.Errorf("SubName = %q, want A", copied[0].SubName)
	}
}

func TestCopyMonthEntriesNotFound(t *testing.T) {
	db := setupTestDB(t)
	_, err := CopyMonthEntries(db, 1, 999, "2024-03", "2024-04")
	if err != sql.ErrNoRows {
		t.Errorf("expected ErrNoRows, got %v", err)
	}
}

// -- Handler tests --

func TestVariableBillsListHandler(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("GET", "/api/budget/variables", nil), 1)
	rec := httptest.NewRecorder()
	VariableBillsListHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		VariableBills []VariableBill `json:"variable_bills"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.VariableBills) != 0 {
		t.Errorf("expected 0 bills, got %d", len(body.VariableBills))
	}
}

func TestVariableBillsCreateHandler_Success(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"name":"Electricity"}`
	req := withUser(httptest.NewRequest("POST", "/api/budget/variables", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	VariableBillsCreateHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		VariableBill VariableBill `json:"variable_bill"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.VariableBill.Name != "Electricity" {
		t.Errorf("Name = %q, want Electricity", body.VariableBill.Name)
	}
	if body.VariableBill.ID == 0 {
		t.Error("expected non-zero ID")
	}
}

func TestVariableBillsCreateHandler_MissingName(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"recurring_id":null}`
	req := withUser(httptest.NewRequest("POST", "/api/budget/variables", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	VariableBillsCreateHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestVariableBillsUpdateHandler(t *testing.T) {
	db := setupTestDB(t)

	b := &VariableBill{Name: "Old Name"}
	if err := CreateVariableBill(db, 1, b); err != nil {
		t.Fatalf("CreateVariableBill: %v", err)
	}

	payload := `{"name":"New Name"}`
	req := withUser(httptest.NewRequest("PUT", "/api/budget/variables/1", strings.NewReader(payload)), 1)
	req = withChiParam(req, "id", "1")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	VariableBillsUpdateHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestVariableBillsUpdateHandler_NotFound(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"name":"X"}`
	req := withUser(httptest.NewRequest("PUT", "/api/budget/variables/999", strings.NewReader(payload)), 1)
	req = withChiParam(req, "id", "999")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	VariableBillsUpdateHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestVariableBillsDeleteHandler(t *testing.T) {
	db := setupTestDB(t)

	b := &VariableBill{Name: "To Delete"}
	if err := CreateVariableBill(db, 1, b); err != nil {
		t.Fatalf("CreateVariableBill: %v", err)
	}

	req := withUser(httptest.NewRequest("DELETE", "/api/budget/variables/1", nil), 1)
	req = withChiParam(req, "id", "1")
	rec := httptest.NewRecorder()
	VariableBillsDeleteHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestVariableBillsDeleteHandler_NotFound(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("DELETE", "/api/budget/variables/999", nil), 1)
	req = withChiParam(req, "id", "999")
	rec := httptest.NewRecorder()
	VariableBillsDeleteHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestVariableBillsSetEntriesHandler(t *testing.T) {
	db := setupTestDB(t)

	b := &VariableBill{Name: "Electricity"}
	if err := CreateVariableBill(db, 1, b); err != nil {
		t.Fatalf("CreateVariableBill: %v", err)
	}

	payload := `[{"sub_name":"Tibber","amount":400},{"sub_name":"BKK","amount":200}]`
	req := withUser(httptest.NewRequest("PUT", "/api/budget/variables/1/entries?month=2024-03", strings.NewReader(payload)), 1)
	req = withChiParam(req, "id", "1")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	VariableBillsSetEntriesHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	bills, err := ListVariableBills(db, 1, "2024-03")
	if err != nil {
		t.Fatalf("ListVariableBills: %v", err)
	}
	if len(bills[0].Entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(bills[0].Entries))
	}
}

func TestVariableBillsSetEntriesHandler_InvalidMonth(t *testing.T) {
	db := setupTestDB(t)

	b := &VariableBill{Name: "E"}
	if err := CreateVariableBill(db, 1, b); err != nil {
		t.Fatalf("CreateVariableBill: %v", err)
	}

	payload := `[]`
	req := withUser(httptest.NewRequest("PUT", "/api/budget/variables/1/entries?month=2024-3", strings.NewReader(payload)), 1)
	req = withChiParam(req, "id", "1")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	VariableBillsSetEntriesHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestVariableBillsCopyEntriesHandler(t *testing.T) {
	db := setupTestDB(t)

	b := &VariableBill{Name: "Electricity"}
	if err := CreateVariableBill(db, 1, b); err != nil {
		t.Fatalf("CreateVariableBill: %v", err)
	}
	if err := SetMonthEntries(db, 1, b.ID, "2024-03", []VariableEntry{{SubName: "Tibber", Amount: 400}}); err != nil {
		t.Fatalf("SetMonthEntries: %v", err)
	}

	req := withUser(httptest.NewRequest("POST", "/api/budget/variables/1/copy?from=2024-03&to=2024-04", nil), 1)
	req = withChiParam(req, "id", "1")
	rec := httptest.NewRecorder()
	VariableBillsCopyEntriesHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Entries []VariableEntry `json:"entries"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(body.Entries))
	}
	if body.Entries[0].SubName != "Tibber" {
		t.Errorf("SubName = %q, want Tibber", body.Entries[0].SubName)
	}
	if body.Entries[0].Amount != 400 {
		t.Errorf("Amount = %f, want 400", body.Entries[0].Amount)
	}
}

func TestVariableBillsCopyEntriesHandler_MissingParams(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("POST", "/api/budget/variables/1/copy?from=2024-03", nil), 1)
	req = withChiParam(req, "id", "1")
	rec := httptest.NewRecorder()
	VariableBillsCopyEntriesHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}
