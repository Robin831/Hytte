package budget

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// -- Loan DB tests --

func TestLoanCRUD(t *testing.T) {
	db := setupTestDB(t)

	l := &Loan{
		Name:           "Home Mortgage",
		Principal:      3000000,
		CurrentBalance: 2800000,
		AnnualRate:     0.048,
		MonthlyPayment: 15000,
		StartDate:      "2020-01-01",
		TermMonths:     240,
		PropertyValue:  4000000,
		PropertyName:   "My House",
		Notes:          "Primary residence",
	}
	if err := CreateLoan(db, 1, l); err != nil {
		t.Fatalf("CreateLoan: %v", err)
	}
	if l.ID == 0 {
		t.Fatal("expected non-zero loan ID after create")
	}
	if l.UserID != 1 {
		t.Errorf("UserID = %d, want 1", l.UserID)
	}

	// Get
	got, err := GetLoan(db, 1, l.ID)
	if err != nil {
		t.Fatalf("GetLoan: %v", err)
	}
	if got.Name != "Home Mortgage" {
		t.Errorf("Name = %q, want %q", got.Name, "Home Mortgage")
	}
	if got.AnnualRate != 0.048 {
		t.Errorf("AnnualRate = %v, want 0.048", got.AnnualRate)
	}
	if got.Notes != "Primary residence" {
		t.Errorf("Notes = %q, want %q", got.Notes, "Primary residence")
	}

	// List
	loans, err := ListLoans(db, 1)
	if err != nil {
		t.Fatalf("ListLoans: %v", err)
	}
	if len(loans) != 1 {
		t.Fatalf("len(loans) = %d, want 1", len(loans))
	}

	// Update
	got.Name = "Updated Mortgage"
	got.CurrentBalance = 2700000
	got.AnnualRate = 0.05
	if err := UpdateLoan(db, 1, got); err != nil {
		t.Fatalf("UpdateLoan: %v", err)
	}
	after, err := GetLoan(db, 1, got.ID)
	if err != nil {
		t.Fatalf("GetLoan after update: %v", err)
	}
	if after.Name != "Updated Mortgage" {
		t.Errorf("Name = %q, want %q", after.Name, "Updated Mortgage")
	}
	if after.CurrentBalance != 2700000 {
		t.Errorf("CurrentBalance = %v, want 2700000", after.CurrentBalance)
	}

	// Delete
	if err := DeleteLoan(db, 1, got.ID); err != nil {
		t.Fatalf("DeleteLoan: %v", err)
	}
	loans, err = ListLoans(db, 1)
	if err != nil {
		t.Fatalf("ListLoans after delete: %v", err)
	}
	if len(loans) != 0 {
		t.Errorf("expected 0 loans after delete, got %d", len(loans))
	}
}

func TestLoanCRUD_EmptyList(t *testing.T) {
	db := setupTestDB(t)

	loans, err := ListLoans(db, 1)
	if err != nil {
		t.Fatalf("ListLoans: %v", err)
	}
	if loans == nil {
		t.Error("expected non-nil slice, got nil")
	}
	if len(loans) != 0 {
		t.Errorf("expected 0 loans, got %d", len(loans))
	}
}

func TestGetLoan_NotFound(t *testing.T) {
	db := setupTestDB(t)

	_, err := GetLoan(db, 1, 999)
	if err == nil {
		t.Fatal("expected error for missing loan, got nil")
	}
}

func TestUpdateLoan_NotFound(t *testing.T) {
	db := setupTestDB(t)

	l := &Loan{ID: 999, Name: "X", StartDate: "2020-01-01"}
	err := UpdateLoan(db, 1, l)
	if err == nil {
		t.Fatal("expected error for missing loan, got nil")
	}
}

func TestDeleteLoan_NotFound(t *testing.T) {
	db := setupTestDB(t)

	err := DeleteLoan(db, 1, 999)
	if err == nil {
		t.Fatal("expected error for missing loan, got nil")
	}
}

func TestLoanIsolation(t *testing.T) {
	db := setupTestDB(t)

	// User 1 creates a loan.
	l := &Loan{Name: "User1 Loan", StartDate: "2020-01-01", CurrentBalance: 100000, AnnualRate: 0.04}
	if err := CreateLoan(db, 1, l); err != nil {
		t.Fatalf("CreateLoan: %v", err)
	}

	// User 2 should not see it.
	loans, err := ListLoans(db, 2)
	if err != nil {
		t.Fatalf("ListLoans user2: %v", err)
	}
	if len(loans) != 0 {
		t.Errorf("user 2 should not see user 1 loans, got %d", len(loans))
	}

	// User 2 cannot delete user 1's loan.
	if err := DeleteLoan(db, 2, l.ID); err == nil {
		t.Error("expected error deleting another user's loan, got nil")
	}
}

// -- Amortization tests --

func TestBuildAmortization_Basic(t *testing.T) {
	l := &Loan{
		CurrentBalance: 100000,
		AnnualRate:     0.048,
		MonthlyPayment: 1000,
		TermMonths:     120,
		StartDate:      "2020-01-01",
	}
	rows, err := BuildAmortization(l, 12)
	if err != nil {
		t.Fatalf("BuildAmortization: %v", err)
	}
	if len(rows) != 12 {
		t.Errorf("len(rows) = %d, want 12", len(rows))
	}
	// First row: interest = 100000 * 0.048/12 = 400, principal = 600
	if rows[0].Interest != 400 {
		t.Errorf("row[0].Interest = %v, want 400", rows[0].Interest)
	}
	if rows[0].Principal != 600 {
		t.Errorf("row[0].Principal = %v, want 600", rows[0].Principal)
	}
	// Balance should decrease.
	if rows[0].RemainingBalance >= 100000 {
		t.Errorf("balance should decrease after first payment")
	}
}

func TestBuildAmortization_ZeroBalance(t *testing.T) {
	l := &Loan{CurrentBalance: 0, AnnualRate: 0.048, MonthlyPayment: 1000, TermMonths: 120}
	rows, err := BuildAmortization(l, 0)
	if err != nil {
		t.Fatalf("BuildAmortization: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("expected 0 rows for zero balance, got %d", len(rows))
	}
}

func TestBuildAmortization_CalculatesPayment(t *testing.T) {
	// When MonthlyPayment is 0, it should be calculated from balance+rate+term.
	l := &Loan{
		CurrentBalance: 100000,
		AnnualRate:     0.048,
		MonthlyPayment: 0,
		TermMonths:     120,
		StartDate:      "2020-01-01",
	}
	rows, err := BuildAmortization(l, 120)
	if err != nil {
		t.Fatalf("BuildAmortization: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("expected rows when payment is calculated automatically")
	}
	// Final balance should be ~0
	last := rows[len(rows)-1]
	if last.RemainingBalance > 10 {
		t.Errorf("expected near-zero final balance, got %v", last.RemainingBalance)
	}
}

func TestLTV(t *testing.T) {
	l := &Loan{CurrentBalance: 2800000, PropertyValue: 4000000}
	ltv := LTV(l)
	want := 2800000.0 / 4000000.0
	if ltv != want {
		t.Errorf("LTV = %v, want %v", ltv, want)
	}
}

func TestLTV_NoProperty(t *testing.T) {
	l := &Loan{CurrentBalance: 500000, PropertyValue: 0}
	if LTV(l) != 0 {
		t.Error("expected LTV 0 when no property value")
	}
}

// -- Loan handler tests --

func TestLoansListHandler_Empty(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("GET", "/api/budget/loans", nil), 1)
	rec := httptest.NewRecorder()
	LoansListHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Loans []Loan `json:"loans"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Loans == nil {
		t.Error("expected non-nil loans slice")
	}
	if len(body.Loans) != 0 {
		t.Errorf("expected 0 loans, got %d", len(body.Loans))
	}
}

func TestLoansCreateHandler_Success(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"name":"Car Loan","principal":200000,"current_balance":180000,"annual_rate":0.06,"monthly_payment":4000,"start_date":"2023-01-01","term_months":60}`
	req := withUser(httptest.NewRequest("POST", "/api/budget/loans", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	LoansCreateHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Loan Loan `json:"loan"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Loan.Name != "Car Loan" {
		t.Errorf("name = %q, want %q", body.Loan.Name, "Car Loan")
	}
	if body.Loan.ID == 0 {
		t.Error("expected non-zero ID")
	}
}

func TestLoansCreateHandler_MissingName(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"principal":100000,"start_date":"2023-01-01"}`
	req := withUser(httptest.NewRequest("POST", "/api/budget/loans", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	LoansCreateHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestLoansCreateHandler_MissingStartDate(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"name":"Loan","principal":100000}`
	req := withUser(httptest.NewRequest("POST", "/api/budget/loans", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	LoansCreateHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestLoansCreateHandler_InvalidStartDate(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"name":"Loan","start_date":"not-a-date"}`
	req := withUser(httptest.NewRequest("POST", "/api/budget/loans", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	LoansCreateHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestLoansCreateHandler_InvalidBody(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("POST", "/api/budget/loans", strings.NewReader("not json")), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	LoansCreateHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestLoansUpdateHandler_Success(t *testing.T) {
	db := setupTestDB(t)

	l := &Loan{Name: "Original", StartDate: "2020-01-01", CurrentBalance: 500000, AnnualRate: 0.05}
	if err := CreateLoan(db, 1, l); err != nil {
		t.Fatalf("CreateLoan: %v", err)
	}

	payload := fmt.Sprintf(`{"name":"Updated","start_date":"2020-01-01","current_balance":450000,"annual_rate":0.05}`)
	req := withUser(httptest.NewRequest("PUT", fmt.Sprintf("/api/budget/loans/%d", l.ID), strings.NewReader(payload)), 1)
	req = withChiParam(req, "id", fmt.Sprintf("%d", l.ID))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	LoansUpdateHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestLoansUpdateHandler_NotFound(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"name":"X","start_date":"2020-01-01"}`
	req := withUser(httptest.NewRequest("PUT", "/api/budget/loans/999", strings.NewReader(payload)), 1)
	req = withChiParam(req, "id", "999")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	LoansUpdateHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestLoansUpdateHandler_InvalidID(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("PUT", "/api/budget/loans/abc", strings.NewReader(`{}`)), 1)
	req = withChiParam(req, "id", "abc")
	rec := httptest.NewRecorder()
	LoansUpdateHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestLoansDeleteHandler_Success(t *testing.T) {
	db := setupTestDB(t)

	l := &Loan{Name: "To Delete", StartDate: "2020-01-01"}
	if err := CreateLoan(db, 1, l); err != nil {
		t.Fatalf("CreateLoan: %v", err)
	}

	req := withUser(httptest.NewRequest("DELETE", fmt.Sprintf("/api/budget/loans/%d", l.ID), nil), 1)
	req = withChiParam(req, "id", fmt.Sprintf("%d", l.ID))
	rec := httptest.NewRecorder()
	LoansDeleteHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestLoansDeleteHandler_NotFound(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("DELETE", "/api/budget/loans/999", nil), 1)
	req = withChiParam(req, "id", "999")
	rec := httptest.NewRecorder()
	LoansDeleteHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestLoansAmortizationHandler_Success(t *testing.T) {
	db := setupTestDB(t)

	l := &Loan{
		Name:           "Test Loan",
		StartDate:      "2020-01-01",
		CurrentBalance: 100000,
		AnnualRate:     0.048,
		MonthlyPayment: 1000,
		TermMonths:     120,
	}
	if err := CreateLoan(db, 1, l); err != nil {
		t.Fatalf("CreateLoan: %v", err)
	}

	req := withUser(httptest.NewRequest("GET", fmt.Sprintf("/api/budget/loans/%d/amortization?rows=12", l.ID), nil), 1)
	req = withChiParam(req, "id", fmt.Sprintf("%d", l.ID))
	rec := httptest.NewRecorder()
	LoansAmortizationHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Loan         Loan              `json:"loan"`
		Amortization []AmortizationRow `json:"amortization"`
		LTVRatio     float64           `json:"ltv_ratio"`
		LTVMax       float64           `json:"ltv_max"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Amortization) != 12 {
		t.Errorf("expected 12 rows, got %d", len(body.Amortization))
	}
	if body.LTVMax != 0.85 {
		t.Errorf("ltv_max = %v, want 0.85", body.LTVMax)
	}
}

func TestLoansAmortizationHandler_NotFound(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("GET", "/api/budget/loans/999/amortization", nil), 1)
	req = withChiParam(req, "id", "999")
	rec := httptest.NewRecorder()
	LoansAmortizationHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestLoansAmortizationHandler_InvalidRowsParam(t *testing.T) {
	db := setupTestDB(t)

	l := &Loan{Name: "Test", StartDate: "2020-01-01", CurrentBalance: 100000, AnnualRate: 0.048, MonthlyPayment: 1000}
	if err := CreateLoan(db, 1, l); err != nil {
		t.Fatalf("CreateLoan: %v", err)
	}

	req := withUser(httptest.NewRequest("GET", fmt.Sprintf("/api/budget/loans/%d/amortization?rows=bad", l.ID), nil), 1)
	req = withChiParam(req, "id", fmt.Sprintf("%d", l.ID))
	rec := httptest.NewRecorder()
	LoansAmortizationHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

// -- Trends handler tests --

func TestTrendsHandler_Success(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("GET", "/api/budget/trends?months=3", nil), 1)
	rec := httptest.NewRecorder()
	TrendsHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body TrendsResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Months) != 3 {
		t.Errorf("len(months) = %d, want 3", len(body.Months))
	}
	if body.YearOverYear == nil {
		t.Error("expected non-nil year_over_year")
	}
	if len(body.YearOverYear.Monthly) != 12 {
		t.Errorf("yoy monthly len = %d, want 12", len(body.YearOverYear.Monthly))
	}
}

func TestTrendsHandler_InvalidMonths(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("GET", "/api/budget/trends?months=bad", nil), 1)
	rec := httptest.NewRecorder()
	TrendsHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestTrendsHandler_DefaultMonths(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("GET", "/api/budget/trends", nil), 1)
	rec := httptest.NewRecorder()
	TrendsHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body TrendsResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Default is 6 months.
	if len(body.Months) != 6 {
		t.Errorf("len(months) = %d, want 6 (default)", len(body.Months))
	}
}
