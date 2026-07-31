package budget

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
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

func TestBuildAmortization_PaymentDayNoDuplicateMonths(t *testing.T) {
	// Regression: when start_date day > payment_day, months could duplicate.
	l := &Loan{
		Principal:      4000000,
		CurrentBalance: 4000000,
		AnnualRate:     0.048,
		MonthlyPayment: 22837,
		TermMonths:     240,
		PaymentDay:     23,
		StartDate:      "2025-09-23",
	}
	rows, err := BuildAmortization(l, 12, nil)
	if err != nil {
		t.Fatalf("BuildAmortization: %v", err)
	}
	// Every consecutive row must be in a different month.
	for i := 1; i < len(rows); i++ {
		if rows[i].Date[:7] == rows[i-1].Date[:7] {
			t.Errorf("duplicate month at rows %d and %d: %s vs %s",
				rows[i-1].PaymentNum, rows[i].PaymentNum, rows[i-1].Date, rows[i].Date)
		}
	}
	// All rows should have day 23-26 (business day adjusted from 23; up to +3 when
	// both the nominal day and a subsequent Monday fall on a public holiday).
	for _, r := range rows {
		day := r.Date[8:]
		if day != "23" && day != "24" && day != "25" && day != "26" {
			t.Errorf("expected day 23-26 (business day adjusted), got %s in %s", day, r.Date)
		}
	}
}

func TestBuildAmortization_Basic(t *testing.T) {
	l := &Loan{
		Principal:      100000,
		CurrentBalance: 100000,
		AnnualRate:     0.048,
		MonthlyPayment: 1000,
		TermMonths:     120,
		StartDate:      "2020-01-01",
	}
	rows, err := BuildAmortization(l, 12, nil)
	if err != nil {
		t.Fatalf("BuildAmortization: %v", err)
	}
	if len(rows) != 12 {
		t.Errorf("len(rows) = %d, want 12", len(rows))
	}
	// First row: interest depends on actual days in first period (actual/365).
	// StartDate=Jan 1, PayDay=0→1, so first payment is Feb 1 (31 days).
	// 100000 * 0.048 * 31/365 ≈ 407.67
	if rows[0].Interest < 380 || rows[0].Interest > 440 {
		t.Errorf("row[0].Interest = %v, expected ~400 (actual/365)", rows[0].Interest)
	}
	if rows[0].Principal < 560 || rows[0].Principal > 620 {
		t.Errorf("row[0].Principal = %v, expected ~600 (actual/365)", rows[0].Principal)
	}
	// Balance should decrease.
	if rows[0].RemainingBalance >= 100000 {
		t.Errorf("balance should decrease after first payment")
	}
}

func TestBuildAmortization_ZeroBalance(t *testing.T) {
	l := &Loan{CurrentBalance: 0, AnnualRate: 0.048, MonthlyPayment: 1000, TermMonths: 120}
	rows, err := BuildAmortization(l, 0, nil)
	if err != nil {
		t.Fatalf("BuildAmortization: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("expected 0 rows for zero balance, got %d", len(rows))
	}
}

func TestBuildAmortization_CalculatesPayment(t *testing.T) {
	// When MonthlyPayment is 0, it should be calculated from principal+rate+term.
	l := &Loan{
		Principal:      100000,
		CurrentBalance: 100000,
		AnnualRate:     0.048,
		MonthlyPayment: 0,
		TermMonths:     120,
		StartDate:      "2020-01-01",
	}
	rows, err := BuildAmortization(l, 120, nil)
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

// -- Overpayment (what-if) tests --

// whatIfLoan is the shared fixture for the overpayment tests: 90 000 kr over
// three months at 5 %, paid on the 1st, disbursed 2020-01-01.
func whatIfLoan() *Loan {
	return &Loan{
		Principal:      90000,
		CurrentBalance: 90000,
		AnnualRate:     0.05,
		MonthlyPayment: 30000,
		TermMonths:     3,
		PaymentDay:     1,
		StartDate:      "2020-01-01",
	}
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }

func TestBuildAmortizationWithOverpayments_ZeroOptionsMatchesBaseline(t *testing.T) {
	l := whatIfLoan()
	rateChanges := []LoanRateChange{{EffectiveDate: "2020-02-15", AnnualRate: 0.10}}

	base, err := BuildAmortization(l, 0, rateChanges)
	if err != nil {
		t.Fatalf("BuildAmortization: %v", err)
	}
	got, err := BuildAmortizationWithOverpayments(l, 0, rateChanges, OverpaymentOptions{})
	if err != nil {
		t.Fatalf("BuildAmortizationWithOverpayments: %v", err)
	}
	if !reflect.DeepEqual(base, got) {
		t.Errorf("zero options changed the schedule:\nbase = %+v\ngot  = %+v", base, got)
	}
}

func TestBuildPayoffSummary_NilWithoutOverpayments(t *testing.T) {
	summary, err := BuildPayoffSummary(whatIfLoan(), nil, OverpaymentOptions{})
	if err != nil {
		t.Fatalf("BuildPayoffSummary: %v", err)
	}
	if summary != nil {
		t.Errorf("expected nil summary without overpayments, got %+v", summary)
	}
}

func TestBuildAmortizationWithOverpayments_ExtraMonthlyShortensTerm(t *testing.T) {
	l := &Loan{
		Principal:      1000000,
		CurrentBalance: 1000000,
		AnnualRate:     0.05,
		MonthlyPayment: 0, // auto-calculated
		TermMonths:     120,
		PaymentDay:     1,
		StartDate:      "2020-01-01",
	}
	base, err := BuildAmortization(l, 0, nil)
	if err != nil {
		t.Fatalf("BuildAmortization: %v", err)
	}
	got, err := BuildAmortizationWithOverpayments(l, 0, nil, OverpaymentOptions{ExtraMonthly: 3000})
	if err != nil {
		t.Fatalf("BuildAmortizationWithOverpayments: %v", err)
	}
	if len(got) >= len(base) {
		t.Fatalf("expected a shorter schedule, got %d rows vs baseline %d", len(got), len(base))
	}
	last := got[len(got)-1]
	if last.RemainingBalance != 0 {
		t.Errorf("final remaining balance = %v, want 0", last.RemainingBalance)
	}
	// The final payment is reduced so the balance never goes negative.
	regular := got[0].Payment
	if last.Payment > regular {
		t.Errorf("final payment %v exceeds a regular payment %v", last.Payment, regular)
	}
	for _, r := range got {
		if r.RemainingBalance < 0 {
			t.Fatalf("negative balance at payment %d: %v", r.PaymentNum, r.RemainingBalance)
		}
	}
	// Overpaying must not increase total interest.
	baseInterest, gotInterest := 0.0, 0.0
	for _, r := range base {
		baseInterest += r.Interest
	}
	for _, r := range got {
		gotInterest += r.Interest
	}
	if gotInterest >= baseInterest {
		t.Errorf("interest with overpayments = %v, want less than baseline %v", gotInterest, baseInterest)
	}
}

func TestBuildAmortizationWithOverpayments_LumpSumTiming(t *testing.T) {
	l := &Loan{
		Principal:      1000000,
		CurrentBalance: 1000000,
		AnnualRate:     0.05,
		TermMonths:     120,
		PaymentDay:     1,
		StartDate:      "2020-01-01",
	}
	base, err := BuildAmortization(l, 0, nil)
	if err != nil {
		t.Fatalf("BuildAmortization: %v", err)
	}

	// The lump sum lands on the first scheduled payment on or after its date.
	got, err := BuildAmortizationWithOverpayments(l, 0, nil, OverpaymentOptions{
		LumpSum: 100000, LumpSumDate: "2020-04-15",
	})
	if err != nil {
		t.Fatalf("BuildAmortizationWithOverpayments: %v", err)
	}
	// Payments 1-3 fall in Feb-Apr, so nothing changes before the May payment.
	for i := 0; i < 3; i++ {
		if got[i].RemainingBalance != base[i].RemainingBalance {
			t.Errorf("row %d changed before the lump sum date: %v vs %v",
				i+1, got[i].RemainingBalance, base[i].RemainingBalance)
		}
	}
	if got[3].Date < "2020-04-15" {
		t.Fatalf("row 4 date = %s, expected the first payment on or after 2020-04-15", got[3].Date)
	}
	drop := base[3].RemainingBalance - got[3].RemainingBalance
	if math.Abs(drop-100000) > 1 {
		t.Errorf("balance drop on the lump sum payment = %v, want ~100000", drop)
	}
	// It is applied exactly once: the next payment is a regular one again, so the
	// gap only widens by the interest saved on the lump sum (a few hundred kroner),
	// not by another 100 000.
	nextDrop := (base[4].RemainingBalance - got[4].RemainingBalance) - drop
	if nextDrop < 0 || nextDrop > 1000 {
		t.Errorf("lump sum looks applied more than once (extra drop %v)", nextDrop)
	}

	// A lump sum dated after payoff is a no-op.
	after, err := BuildAmortizationWithOverpayments(l, 0, nil, OverpaymentOptions{
		LumpSum: 100000, LumpSumDate: "2099-01-01",
	})
	if err != nil {
		t.Fatalf("BuildAmortizationWithOverpayments: %v", err)
	}
	if !reflect.DeepEqual(base, after) {
		t.Errorf("lump sum after payoff changed the schedule")
	}
}

// TestBuildAmortizationWithOverpayments_RateChangeFixture replays a loan with a
// mid-period rate change against a hand-computed schedule.
//
// Fixture: 90 000 kr, 5 % nominal, 30 000 kr/month, term 3 months, payment day 1,
// disbursed 2020-01-01, rate raised to 10 % on 2020-02-15 (mid-period), with a
// 20 000 kr recurring extra payment.
func TestBuildAmortizationWithOverpayments_RateChangeFixture(t *testing.T) {
	l := whatIfLoan()
	rateChanges := []LoanRateChange{{EffectiveDate: "2020-02-15", AnnualRate: 0.10}}

	// -- Hand-computed baseline (actual/365, interest excludes the disbursement day) --
	// Period 1: 2020-01-02 → 2020-02-01 = 30 days at 5 %.
	b0 := 90000.0
	i1 := b0 * 0.05 * 30 / 365
	b1 := b0 - (30000 - i1)
	// Period 2: 2020-02-01 → 2020-02-15 = 14 days at 5 %, then 15 days at 10 %.
	i2 := b1*0.05*14/365 + b1*0.10*15/365
	// The rate change recalculates the annuity over the 2 remaining months at 10 %.
	r := 0.10 / 12
	pay2 := b1 * r / (1 - math.Pow(1+r, -2))
	b2 := b1 - (pay2 - i2)
	// Period 3: 2020-03-01 → 2020-04-01 = 31 days at 10 %; the last payment clears
	// the balance.
	i3 := b2 * 0.10 * 31 / 365
	baseInterest := i1 + i2 + i3

	base, err := BuildAmortization(l, 0, rateChanges)
	if err != nil {
		t.Fatalf("BuildAmortization: %v", err)
	}
	if len(base) != 3 {
		t.Fatalf("baseline rows = %d, want 3", len(base))
	}
	for i, want := range []float64{round2(i1), round2(i2), round2(i3)} {
		if math.Abs(base[i].Interest-want) > 0.02 {
			t.Errorf("baseline row %d interest = %v, want %v", i+1, base[i].Interest, want)
		}
	}

	// -- Hand-computed what-if with 20 000 kr extra per month --
	// Period 1: same interest, but 20 000 kr more principal.
	e := 20000.0
	w1 := b0 - (30000 - i1 + e) // remaining balance after payment 1
	// Period 2: same 14/15 day split, now on the smaller balance.
	wi2 := w1*0.05*14/365 + w1*0.10*15/365
	// The contractual payment stays pay2 (overpayments shorten the term, they do
	// not lower the payment), so pay2 - wi2 + 20 000 > w1: the loan is cleared.
	whatIfInterest := i1 + wi2

	got, err := BuildAmortizationWithOverpayments(l, 0, rateChanges, OverpaymentOptions{ExtraMonthly: e})
	if err != nil {
		t.Fatalf("BuildAmortizationWithOverpayments: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("what-if rows = %d, want 2", len(got))
	}
	if math.Abs(got[0].Interest-round2(i1)) > 0.02 {
		t.Errorf("what-if row 1 interest = %v, want %v", got[0].Interest, round2(i1))
	}
	if math.Abs(got[0].RemainingBalance-round2(w1)) > 0.02 {
		t.Errorf("what-if row 1 balance = %v, want %v", got[0].RemainingBalance, round2(w1))
	}
	if math.Abs(got[1].Interest-round2(wi2)) > 0.02 {
		t.Errorf("what-if row 2 interest = %v, want %v", got[1].Interest, round2(wi2))
	}
	if got[1].RemainingBalance != 0 {
		t.Errorf("what-if final balance = %v, want 0", got[1].RemainingBalance)
	}
	if math.Abs(got[1].Payment-round2(wi2+w1)) > 0.02 {
		t.Errorf("what-if final payment = %v, want %v", got[1].Payment, round2(wi2+w1))
	}
	if got[1].Rate != 0.10 {
		t.Errorf("what-if row 2 rate = %v, want 0.10", got[1].Rate)
	}

	// -- Summary --
	summary, err := BuildPayoffSummary(l, rateChanges, OverpaymentOptions{ExtraMonthly: e})
	if err != nil {
		t.Fatalf("BuildPayoffSummary: %v", err)
	}
	if summary == nil {
		t.Fatal("expected a payoff summary")
	}
	if summary.MonthsSaved != 1 {
		t.Errorf("MonthsSaved = %d, want 1", summary.MonthsSaved)
	}
	if summary.OriginalPayoffDate != base[2].Date {
		t.Errorf("OriginalPayoffDate = %s, want %s", summary.OriginalPayoffDate, base[2].Date)
	}
	if summary.NewPayoffDate != got[1].Date {
		t.Errorf("NewPayoffDate = %s, want %s", summary.NewPayoffDate, got[1].Date)
	}
	wantSaved := round2(baseInterest - whatIfInterest)
	if math.Abs(summary.InterestSaved-wantSaved) > 0.05 {
		t.Errorf("InterestSaved = %v, want %v", summary.InterestSaved, wantSaved)
	}
}

func TestLoansAmortizationHandler_WhatIf(t *testing.T) {
	db := setupTestDB(t)

	l := &Loan{
		Name: "Test Loan", StartDate: "2020-01-01", Principal: 1000000,
		CurrentBalance: 1000000, AnnualRate: 0.05, TermMonths: 120, PaymentDay: 1,
	}
	if err := CreateLoan(db, 1, l); err != nil {
		t.Fatalf("CreateLoan: %v", err)
	}

	url := fmt.Sprintf("/api/budget/loans/%d/amortization?rows=12&extra_monthly=3000", l.ID)
	req := withUser(httptest.NewRequest("GET", url, nil), 1)
	req = withChiParam(req, "id", fmt.Sprintf("%d", l.ID))
	rec := httptest.NewRecorder()
	LoansAmortizationHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Amortization  []AmortizationRow `json:"amortization"`
		PayoffSummary *PayoffSummary    `json:"payoff_summary"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.PayoffSummary == nil {
		t.Fatal("expected a payoff_summary when extra_monthly is set")
	}
	if body.PayoffSummary.MonthsSaved <= 0 {
		t.Errorf("MonthsSaved = %d, want > 0", body.PayoffSummary.MonthsSaved)
	}
	if body.PayoffSummary.InterestSaved <= 0 {
		t.Errorf("InterestSaved = %v, want > 0", body.PayoffSummary.InterestSaved)
	}
	if body.PayoffSummary.NewPayoffDate >= body.PayoffSummary.OriginalPayoffDate {
		t.Errorf("NewPayoffDate %s should precede OriginalPayoffDate %s",
			body.PayoffSummary.NewPayoffDate, body.PayoffSummary.OriginalPayoffDate)
	}
}

func TestLoansAmortizationHandler_NoWhatIfSummary(t *testing.T) {
	db := setupTestDB(t)

	l := &Loan{Name: "Test", StartDate: "2020-01-01", Principal: 100000, CurrentBalance: 100000, AnnualRate: 0.048, MonthlyPayment: 1000}
	if err := CreateLoan(db, 1, l); err != nil {
		t.Fatalf("CreateLoan: %v", err)
	}

	req := withUser(httptest.NewRequest("GET", fmt.Sprintf("/api/budget/loans/%d/amortization?rows=6", l.ID), nil), 1)
	req = withChiParam(req, "id", fmt.Sprintf("%d", l.ID))
	rec := httptest.NewRecorder()
	LoansAmortizationHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		PayoffSummary *PayoffSummary `json:"payoff_summary"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.PayoffSummary != nil {
		t.Errorf("expected null payoff_summary without overpayments, got %+v", body.PayoffSummary)
	}
}

func TestLoansAmortizationHandler_InvalidWhatIfParams(t *testing.T) {
	db := setupTestDB(t)

	l := &Loan{Name: "Test", StartDate: "2020-01-01", Principal: 100000, CurrentBalance: 100000, AnnualRate: 0.048, MonthlyPayment: 1000}
	if err := CreateLoan(db, 1, l); err != nil {
		t.Fatalf("CreateLoan: %v", err)
	}

	cases := []struct{ name, query string }{
		{"negative extra_monthly", "extra_monthly=-100"},
		{"non-numeric extra_monthly", "extra_monthly=abc"},
		{"negative lump_sum", "lump_sum=-1&lump_sum_date=2025-01-01"},
		{"malformed lump_sum_date", "lump_sum=1000&lump_sum_date=01-01-2025"},
		{"lump_sum without date", "lump_sum=1000"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			url := fmt.Sprintf("/api/budget/loans/%d/amortization?%s", l.ID, tc.query)
			req := withUser(httptest.NewRequest("GET", url, nil), 1)
			req = withChiParam(req, "id", fmt.Sprintf("%d", l.ID))
			rec := httptest.NewRecorder()
			LoansAmortizationHandler(db).ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
			}
			var body map[string]string
			if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if body["error"] == "" {
				t.Error("expected a non-empty error message")
			}
		})
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

func TestLoansListHandler_ExposesLTVMax(t *testing.T) {
	db := setupTestDB(t)

	l := &Loan{
		Name:           "Mortgage",
		StartDate:      "2020-01-01",
		Principal:      3000000,
		CurrentBalance: 2800000,
		AnnualRate:     0.048,
		MonthlyPayment: 15000,
		TermMonths:     240,
		PropertyValue:  4000000,
	}
	if err := CreateLoan(db, 1, l); err != nil {
		t.Fatalf("CreateLoan: %v", err)
	}

	req := withUser(httptest.NewRequest("GET", "/api/budget/loans", nil), 1)
	rec := httptest.NewRecorder()
	LoansListHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Loans []struct {
			Loan
			LTVRatio float64 `json:"ltv_ratio"`
			LTVMax   float64 `json:"ltv_max"`
		} `json:"loans"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Loans) != 1 {
		t.Fatalf("expected 1 loan, got %d", len(body.Loans))
	}
	if body.Loans[0].LTVMax != DefaultLTVMax {
		t.Errorf("ltv_max = %v, want %v", body.Loans[0].LTVMax, DefaultLTVMax)
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
		Principal:      100000,
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
	if body.LTVMax != DefaultLTVMax {
		t.Errorf("ltv_max = %v, want %v", body.LTVMax, DefaultLTVMax)
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

	l := &Loan{Name: "Test", StartDate: "2020-01-01", Principal: 100000, CurrentBalance: 100000, AnnualRate: 0.048, MonthlyPayment: 1000}
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
