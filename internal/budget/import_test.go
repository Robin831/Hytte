package budget

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Robin831/Hytte/internal/auth"
)

// withTestUser injects a minimal test user into the request context.
func withTestUser(r *http.Request, userID int64) *http.Request {
	user := &auth.User{ID: userID, Email: "test@example.com", Name: "Test"}
	return r.WithContext(auth.ContextWithUser(context.Background(), user))
}

// -- parseAmount tests --

func TestParseAmount(t *testing.T) {
	tests := []struct {
		input string
		want  float64
	}{
		{"1234.56", 1234.56},
		{"1234,56", 1234.56},
		{"1 234,56", 1234.56},
		{"1,234.56", 1234.56},
		{"-500", -500},
		{"(200.50)", -200.50},
		{"0", 0},
		{"", 0},
		{"1.000,00", 1000.00},
		{"1.000", 1000},
	}
	for _, tc := range tests {
		got, err := parseAmount(tc.input)
		if err != nil {
			t.Errorf("parseAmount(%q) error: %v", tc.input, err)
			continue
		}
		if got != tc.want {
			t.Errorf("parseAmount(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

func TestParseAmountErrors(t *testing.T) {
	_, err := parseAmount("not-a-number")
	if err == nil {
		t.Error("expected error for non-numeric input, got nil")
	}
}

// -- parseDateField tests --

func TestParseDateField(t *testing.T) {
	tests := []struct {
		input string
		hint  string
		want  string
	}{
		{"2026-01-15", "", "2026-01-15"},
		{"15.01.2026", "", "2026-01-15"},
		{"01/15/2026", "", "2026-01-15"},
		{"15-01-2026", "", "2026-01-15"},
		{"2026/01/15", "", "2026-01-15"},
		{"2026-01-15", "2006-01-02", "2026-01-15"},
		{"15.01.2026", "02.01.2006", "2026-01-15"},
	}
	for _, tc := range tests {
		got, err := parseDateField(tc.input, tc.hint)
		if err != nil {
			t.Errorf("parseDateField(%q, %q) error: %v", tc.input, tc.hint, err)
			continue
		}
		if got != tc.want {
			t.Errorf("parseDateField(%q, %q) = %q, want %q", tc.input, tc.hint, got, tc.want)
		}
	}
}

func TestParseDateFieldErrors(t *testing.T) {
	if _, err := parseDateField("", ""); err == nil {
		t.Error("expected error for empty date")
	}
	if _, err := parseDateField("not-a-date", ""); err == nil {
		t.Error("expected error for unrecognised date format")
	}
}

// -- parseCSV tests --

func TestParseCSV_Basic(t *testing.T) {
	content := "Date,Description,Amount,Category\n" +
		"2026-01-10,Groceries,-500.00,Mat\n" +
		"2026-01-11,Salary,25000,Lønn\n"

	mapping := ColumnMapping{Date: 0, Description: 1, Amount: 2, Category: 3}
	rows, errs := parseCSV(strings.NewReader(content), mapping, "", true)

	if len(errs) != 0 {
		t.Errorf("unexpected parse errors: %v", errs)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if rows[0].Date != "2026-01-10" {
		t.Errorf("row 0 date = %q, want 2026-01-10", rows[0].Date)
	}
	if rows[0].Amount != -500 {
		t.Errorf("row 0 amount = %v, want -500", rows[0].Amount)
	}
	if rows[0].Category != "Mat" {
		t.Errorf("row 0 category = %q, want Mat", rows[0].Category)
	}
	if rows[1].Amount != 25000 {
		t.Errorf("row 1 amount = %v, want 25000", rows[1].Amount)
	}
}

func TestParseCSV_NorwegianFormat(t *testing.T) {
	content := "15.01.2026;Mat og drikke;-1 234,56;\n"

	mapping := ColumnMapping{Date: 0, Description: 1, Amount: 2, Category: -1}
	rows, errs := parseCSV(strings.NewReader(content), mapping, "", false)

	if len(errs) != 0 {
		t.Errorf("unexpected parse errors: %v", errs)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].Date != "2026-01-15" {
		t.Errorf("date = %q, want 2026-01-15", rows[0].Date)
	}
	if rows[0].Amount != -1234.56 {
		t.Errorf("amount = %v, want -1234.56", rows[0].Amount)
	}
}

func TestParseCSV_InvalidDate(t *testing.T) {
	content := "bad-date,Groceries,-100\n"

	mapping := ColumnMapping{Date: 0, Description: 1, Amount: 2, Category: -1}
	rows, errs := parseCSV(strings.NewReader(content), mapping, "", false)

	if len(errs) != 0 {
		t.Errorf("unexpected structural errors: %v", errs)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].Error == "" {
		t.Error("expected row error for invalid date, got empty string")
	}
}

func TestParseCSV_SkipHeader(t *testing.T) {
	content := "Date,Desc,Amount\n2026-01-01,Test,-50\n"
	mapping := ColumnMapping{Date: 0, Description: 1, Amount: 2, Category: -1}

	rowsSkip, _ := parseCSV(strings.NewReader(content), mapping, "", true)
	rowsNoSkip, _ := parseCSV(strings.NewReader(content), mapping, "", false)

	if len(rowsSkip) != 1 {
		t.Errorf("with skip_header=true: expected 1 row, got %d", len(rowsSkip))
	}
	if len(rowsNoSkip) != 2 {
		t.Errorf("with skip_header=false: expected 2 rows, got %d", len(rowsNoSkip))
	}
}

func TestParseCSV_Empty(t *testing.T) {
	mapping := ColumnMapping{Date: 0, Description: 1, Amount: 2, Category: -1}
	rows, errs := parseCSV(strings.NewReader(""), mapping, "", true)

	if len(rows) != 0 {
		t.Errorf("expected 0 rows for empty input, got %d", len(rows))
	}
	if errs == nil {
		t.Error("expected non-nil errs slice")
	}
}

// -- HTTP handler tests --

func buildCSVRequest(t *testing.T, csvContent, mappingJSON, skipHeader string) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)

	fw, err := mw.CreateFormFile("file", "test.csv")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write([]byte(csvContent)); err != nil {
		t.Fatal(err)
	}
	if err := mw.WriteField("mapping", mappingJSON); err != nil {
		t.Fatal(err)
	}
	if skipHeader != "" {
		if err := mw.WriteField("skip_header", skipHeader); err != nil {
			t.Fatal(err)
		}
	}
	mw.Close() //nolint:errcheck

	req := httptest.NewRequest(http.MethodPost, "/api/budget/import/csv", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req
}

func TestCSVPreviewHandler_Success(t *testing.T) {
	csvContent := "Date,Description,Amount\n2026-01-10,Groceries,-500\n"
	mappingJSON := `{"date":0,"description":1,"amount":2,"category":-1}`

	req := buildCSVRequest(t, csvContent, mappingJSON, "true")
	w := httptest.NewRecorder()

	CSVPreviewHandler(nil)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	rows, ok := resp["rows"].([]any)
	if !ok {
		t.Fatal("expected rows array in response")
	}
	if len(rows) != 1 {
		t.Errorf("expected 1 row, got %d", len(rows))
	}
}

func TestCSVPreviewHandler_MissingFile(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/budget/import/csv", nil)
	req.Header.Set("Content-Type", "multipart/form-data; boundary=test")
	w := httptest.NewRecorder()

	CSVPreviewHandler(nil)(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestCSVPreviewHandler_MissingMapping(t *testing.T) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "test.csv")
	fw.Write([]byte("a,b,c\n")) //nolint:errcheck
	mw.Close()                  //nolint:errcheck

	req := httptest.NewRequest(http.MethodPost, "/api/budget/import/csv", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()

	CSVPreviewHandler(nil)(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestCSVCommitHandler_NoAccount(t *testing.T) {
	db := setupTestDB(t)

	body := `{"account_id": 0, "transactions": [{"line":1,"date":"2026-01-01","description":"Test","amount":-100}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/budget/import/csv/commit", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withTestUser(req, 1)
	w := httptest.NewRecorder()

	CSVCommitHandler(db)(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing account_id, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCSVCommitHandler_AccountNotFound(t *testing.T) {
	db := setupTestDB(t)

	body := `{"account_id": 9999, "transactions": [{"line":1,"date":"2026-01-01","description":"Test","amount":-100}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/budget/import/csv/commit", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withTestUser(req, 1)
	w := httptest.NewRecorder()

	CSVCommitHandler(db)(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for unknown account, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCSVCommitHandler_Success(t *testing.T) {
	db := setupTestDB(t)

	acct := &Account{Name: "Checking", Type: AccountTypeChecking, Currency: "NOK"}
	if err := CreateAccount(db, 1, acct); err != nil {
		t.Fatal(err)
	}

	transactions := []ImportRow{
		{Line: 2, Date: "2026-01-10", Description: "Groceries", Amount: -500},
		{Line: 3, Date: "2026-01-11", Description: "Salary", Amount: 25000},
		{Line: 4, Date: "", Description: "Bad row", Amount: 0, Error: "invalid date"},
	}
	bodyBytes, _ := json.Marshal(ImportCommitRequest{
		AccountID:    acct.ID,
		Transactions: transactions,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/budget/import/csv/commit", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req = withTestUser(req, 1)
	w := httptest.NewRecorder()

	CSVCommitHandler(db)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	imported, ok := resp["imported"].(float64)
	if !ok || imported != 2 {
		t.Errorf("expected imported=2, got %v", resp["imported"])
	}

	// Verify rows are actually in the DB.
	txns, err := ListTransactions(db, 1, TransactionFilter{AccountID: &acct.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(txns) != 2 {
		t.Errorf("expected 2 transactions in DB, got %d", len(txns))
	}
}

func TestCSVCommitHandler_EmptyTransactions(t *testing.T) {
	db := setupTestDB(t)

	body := `{"account_id": 1, "transactions": []}`
	req := httptest.NewRequest(http.MethodPost, "/api/budget/import/csv/commit", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withTestUser(req, 1)
	w := httptest.NewRecorder()

	CSVCommitHandler(db)(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty transactions, got %d", w.Code)
	}
}
