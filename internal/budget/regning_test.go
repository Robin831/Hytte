package budget

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// -- regningMonthly --

func TestRegningMonthly(t *testing.T) {
	cases := []struct {
		freq     Frequency
		amount   float64
		expected float64
	}{
		{FrequencyMonthly, 1000, 1000},
		{FrequencyYearly, 12000, 1000},
		{FrequencyWeekly, 1000, math.Round(float64(1000)*52/12*100) / 100}, // rounded to cents
	}
	for _, c := range cases {
		got := regningMonthly(c.amount, c.freq)
		if got != c.expected {
			t.Errorf("regningMonthly(%v, %v) = %v, want %v", c.amount, c.freq, got, c.expected)
		}
	}
}

// -- regningComputeSplit --

func TestRegningComputeSplit_Equal(t *testing.T) {
	your, partner := regningComputeSplit(1000, SplitTypeEqual, nil, 60)
	if your != 500 || partner != 500 {
		t.Errorf("equal split: got %v / %v, want 500 / 500", your, partner)
	}
}

func TestRegningComputeSplit_Percentage_Explicit(t *testing.T) {
	pct := 70.0
	your, partner := regningComputeSplit(1000, SplitTypePercentage, &pct, 60)
	if your != 700 || partner != 300 {
		t.Errorf("percentage 70%%: got %v / %v, want 700 / 300", your, partner)
	}
}

func TestRegningComputeSplit_Percentage_Fallback(t *testing.T) {
	your, partner := regningComputeSplit(1000, SplitTypePercentage, nil, 60)
	if your != 600 || partner != 400 {
		t.Errorf("percentage fallback 60%%: got %v / %v, want 600 / 400", your, partner)
	}
}

func TestRegningComputeSplit_FixedYou(t *testing.T) {
	fixed := 300.0
	your, partner := regningComputeSplit(1000, SplitTypeFixedYou, &fixed, 60)
	if your != 300 || partner != 700 {
		t.Errorf("fixed_you 300: got %v / %v, want 300 / 700", your, partner)
	}
}

func TestRegningComputeSplit_FixedYou_NilFallback(t *testing.T) {
	your, partner := regningComputeSplit(1000, SplitTypeFixedYou, nil, 60)
	if your != 600 || partner != 400 {
		t.Errorf("fixed_you nil fallback: got %v / %v, want 600 / 400", your, partner)
	}
}

func TestRegningComputeSplit_FixedPartner(t *testing.T) {
	fixed := 200.0
	your, partner := regningComputeSplit(1000, SplitTypeFixedPartner, &fixed, 60)
	if your != 800 || partner != 200 {
		t.Errorf("fixed_partner 200: got %v / %v, want 800 / 200", your, partner)
	}
}

func TestRegningComputeSplit_FixedPartner_NilFallback(t *testing.T) {
	your, partner := regningComputeSplit(1000, SplitTypeFixedPartner, nil, 60)
	if your != 600 || partner != 400 {
		t.Errorf("fixed_partner nil fallback: got %v / %v, want 600 / 400", your, partner)
	}
}

// -- RegningHandler --

// -- incomeNextDue --

func TestIncomeNextDue_Weekday(t *testing.T) {
	// income_day=20, today=April 4 2026 → April 20 2026 is a Monday → no adjustment
	now, _ := time.Parse("2006-01-02", "2026-04-04")
	got := incomeNextDue(20, now)
	if got != "2026-04-20" {
		t.Errorf("want 2026-04-20, got %s", got)
	}
}

func TestIncomeNextDue_Weekend(t *testing.T) {
	// income_day=4, today=April 3 2026 → April 4 is Easter Saturday →
	// previousBusinessDay moves to April 1 (Wednesday) which is before today → use May 4 2026
	// May 4 2026 is a Monday → no adjustment needed
	now, _ := time.Parse("2006-01-02", "2026-04-03")
	got := incomeNextDue(4, now)
	if got != "2026-05-04" {
		t.Errorf("want 2026-05-04, got %s", got)
	}
}

func TestIncomeNextDue_AlreadyPassed(t *testing.T) {
	// income_day=1, today=April 4 2026 → April 1 is a Wednesday →
	// adjusted is same day (April 1) which is before today → use May 1 2026
	// May 1 is Norwegian Labour Day (holiday) and a Friday → previousBusinessDay → April 30 (Thursday)
	now, _ := time.Parse("2006-01-02", "2026-04-04")
	got := incomeNextDue(1, now)
	if got != "2026-04-30" {
		t.Errorf("want 2026-04-30 (May 1 Labour Day → Apr 30), got %s", got)
	}
}

func TestIncomeNextDue_DayClampedToLastDayOfMonth(t *testing.T) {
	// income_day=31 in February → clamped to Feb 28 (2026 is not a leap year)
	// today=Feb 1 2026 → Feb 28 2026 is a Saturday → previousBusinessDay → Feb 27 (Friday)
	now, _ := time.Parse("2006-01-02", "2026-02-01")
	got := incomeNextDue(31, now)
	if got != "2026-02-27" {
		t.Errorf("want 2026-02-27 (day 31 clamped to Feb 28, which is Sat → Fri), got %s", got)
	}
}

func mustParseDate(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse("2006-01-02", s)
	if err != nil {
		t.Fatalf("parse date %q: %v", s, err)
	}
	return v
}

func TestRegningHandler_Empty(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("GET", "/api/budget/regning", nil), 1)
	rec := httptest.NewRecorder()
	RegningHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body RegningResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Expenses) != 0 {
		t.Errorf("expected 0 expenses, got %d", len(body.Expenses))
	}
	if body.TotalYourShare != 0 || body.TotalPartnerShare != 0 {
		t.Errorf("expected zero totals, got %v / %v", body.TotalYourShare, body.TotalPartnerShare)
	}
	if body.IncomeSplitPct != 60 {
		t.Errorf("expected default income split 60, got %d", body.IncomeSplitPct)
	}
}

func TestRegningHandler_WithExpenses(t *testing.T) {
	db := setupTestDB(t)

	// Account required for recurring FK.
	_, err := db.Exec(`INSERT INTO budget_accounts (id, user_id, name, type, currency, balance) VALUES (1, 1, 'Main', 'checking', 'NOK', 0)`)
	if err != nil {
		t.Fatalf("insert account: %v", err)
	}
	// Salary config.
	_, err = db.Exec(`INSERT INTO salary_config (user_id, base_salary, effective_from) VALUES (1, 80000, '2024-01-01')`)
	if err != nil {
		t.Fatalf("insert salary config: %v", err)
	}
	// Partner income preference.
	_, err = db.Exec(`INSERT INTO user_preferences (user_id, key, value) VALUES (1, 'partner_income', '60000')`)
	if err != nil {
		t.Fatalf("insert partner_income: %v", err)
	}
	// Income split preference: 60% for user.
	_, err = db.Exec(`INSERT INTO user_preferences (user_id, key, value) VALUES (1, 'income_split_percentage', '60')`)
	if err != nil {
		t.Fatalf("insert income_split_percentage: %v", err)
	}

	startDate := time.Now().Format("2006-01-02")

	// Monthly expense: 2000 NOK, 70% to user.
	pct70 := 70.0
	r1 := &Recurring{
		AccountID:   1,
		Amount:      2000,
		Description: "Strom",
		Frequency:   FrequencyMonthly,
		DayOfMonth:  1,
		StartDate:   mustParseDate(t, startDate),
		Active:      true,
		SplitType:   SplitTypePercentage,
		SplitPct:    &pct70,
	}
	if err := CreateRecurring(db, 1, r1); err != nil {
		t.Fatalf("create recurring 1: %v", err)
	}

	// Yearly expense: 12000 NOK, equal split → 1000/month, 500 each.
	r2 := &Recurring{
		AccountID:   1,
		Amount:      12000,
		Description: "Husforsikring",
		Frequency:   FrequencyYearly,
		DayOfMonth:  1,
		StartDate:   mustParseDate(t, startDate),
		Active:      true,
		SplitType:   SplitTypeEqual,
	}
	if err := CreateRecurring(db, 1, r2); err != nil {
		t.Fatalf("create recurring 2: %v", err)
	}

	// Inactive expense — must be excluded.
	r3 := &Recurring{
		AccountID:   1,
		Amount:      500,
		Description: "Inactive",
		Frequency:   FrequencyMonthly,
		DayOfMonth:  1,
		StartDate:   mustParseDate(t, startDate),
		Active:      false,
		SplitType:   SplitTypePercentage,
	}
	if err := CreateRecurring(db, 1, r3); err != nil {
		t.Fatalf("create recurring 3: %v", err)
	}

	req := withUser(httptest.NewRequest("GET", "/api/budget/regning", nil), 1)
	rec := httptest.NewRecorder()
	RegningHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body RegningResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(body.Expenses) != 2 {
		t.Fatalf("expected 2 expenses (inactive excluded), got %d", len(body.Expenses))
	}

	// r1: 2000 monthly, 70% → 1400 your / 600 partner.
	if body.Expenses[0].YourShare != 1400 {
		t.Errorf("expense 0 your_share: want 1400, got %v", body.Expenses[0].YourShare)
	}
	if body.Expenses[0].PartnerShare != 600 {
		t.Errorf("expense 0 partner_share: want 600, got %v", body.Expenses[0].PartnerShare)
	}
	if body.Expenses[0].Monthly != 2000 {
		t.Errorf("expense 0 monthly: want 2000, got %v", body.Expenses[0].Monthly)
	}

	// r2: 12000 yearly → 1000/month, equal → 500 / 500.
	if body.Expenses[1].Monthly != 1000 {
		t.Errorf("expense 1 monthly: want 1000, got %v", body.Expenses[1].Monthly)
	}
	if body.Expenses[1].YourShare != 500 {
		t.Errorf("expense 1 your_share: want 500, got %v", body.Expenses[1].YourShare)
	}
	if body.Expenses[1].PartnerShare != 500 {
		t.Errorf("expense 1 partner_share: want 500, got %v", body.Expenses[1].PartnerShare)
	}

	// Totals: 1400+500=1900 your, 600+500=1100 partner.
	if body.TotalYourShare != 1900 {
		t.Errorf("total_your_share: want 1900, got %v", body.TotalYourShare)
	}
	if body.TotalPartnerShare != 1100 {
		t.Errorf("total_partner_share: want 1100, got %v", body.TotalPartnerShare)
	}

	if body.YourIncome != 80000 {
		t.Errorf("your_income: want 80000, got %v", body.YourIncome)
	}
	if body.PartnerIncome != 60000 {
		t.Errorf("partner_income: want 60000, got %v", body.PartnerIncome)
	}
	if body.YourRemaining != 80000-1900 {
		t.Errorf("your_remaining: want %v, got %v", 80000-1900, body.YourRemaining)
	}
	if body.PartnerRemaining != 60000-1100 {
		t.Errorf("partner_remaining: want %v, got %v", 60000-1100, body.PartnerRemaining)
	}
}

func TestRegningHandler_DBError(t *testing.T) {
	db := setupTestDB(t)
	// Close the DB to force query errors.
	db.Close()

	req := withUser(httptest.NewRequest("GET", "/api/budget/regning", nil), 1)
	rec := httptest.NewRecorder()
	RegningHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on closed DB, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRegningHandler_NextDue(t *testing.T) {
	db := setupTestDB(t)

	_, err := db.Exec(`INSERT INTO budget_accounts (id, user_id, name, type, currency, balance) VALUES (1, 1, 'Main', 'checking', 'NOK', 0)`)
	if err != nil {
		t.Fatalf("insert account: %v", err)
	}

	// June 15, 2026 is a Monday (not a holiday) — next_due should equal start_date.
	r := &Recurring{
		AccountID:   1,
		Amount:      1000,
		Description: "Test",
		Frequency:   FrequencyMonthly,
		DayOfMonth:  15,
		StartDate:   mustParseDate(t, "2026-06-15"),
		Active:      true,
		SplitType:   SplitTypeEqual,
	}
	if err := CreateRecurring(db, 1, r); err != nil {
		t.Fatalf("create recurring: %v", err)
	}

	req := withUser(httptest.NewRequest("GET", "/api/budget/regning", nil), 1)
	rec := httptest.NewRecorder()
	RegningHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body RegningResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Expenses) != 1 {
		t.Fatalf("expected 1 expense, got %d", len(body.Expenses))
	}
	if body.Expenses[0].NextDue == "" {
		t.Error("expected next_due to be populated")
	}
	// June 15, 2026 is a Monday (not a holiday): no adjustment needed.
	if body.Expenses[0].NextDue != "2026-06-15" {
		t.Errorf("next_due: want 2026-06-15, got %s", body.Expenses[0].NextDue)
	}
}

func TestRegningHandler_NextDue_WeekendAdjustment(t *testing.T) {
	db := setupTestDB(t)

	_, err := db.Exec(`INSERT INTO budget_accounts (id, user_id, name, type, currency, balance) VALUES (1, 1, 'Main', 'checking', 'NOK', 0)`)
	if err != nil {
		t.Fatalf("insert account: %v", err)
	}

	// June 13, 2026 is a Saturday — next_due should advance to Monday June 15.
	r := &Recurring{
		AccountID:   1,
		Amount:      1000,
		Description: "Weekend test",
		Frequency:   FrequencyMonthly,
		DayOfMonth:  13,
		StartDate:   mustParseDate(t, "2026-06-13"),
		Active:      true,
		SplitType:   SplitTypeEqual,
	}
	if err := CreateRecurring(db, 1, r); err != nil {
		t.Fatalf("create recurring: %v", err)
	}

	req := withUser(httptest.NewRequest("GET", "/api/budget/regning", nil), 1)
	rec := httptest.NewRecorder()
	RegningHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body RegningResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Expenses) != 1 {
		t.Fatalf("expected 1 expense, got %d", len(body.Expenses))
	}
	// Saturday June 13 → next business day Monday June 15.
	if body.Expenses[0].NextDue != "2026-06-15" {
		t.Errorf("next_due: want 2026-06-15 (Monday after Saturday), got %s", body.Expenses[0].NextDue)
	}
}

func TestRegningHandler_FallbackIncomeSplit(t *testing.T) {
	db := setupTestDB(t)

	_, err := db.Exec(`INSERT INTO budget_accounts (id, user_id, name, type, currency, balance) VALUES (1, 1, 'Main', 'checking', 'NOK', 0)`)
	if err != nil {
		t.Fatalf("insert account: %v", err)
	}

	// No income_split_percentage preference → defaults to 60.
	startDate := time.Now().Format("2006-01-02")
	r1 := &Recurring{
		AccountID:   1,
		Amount:      1000,
		Description: "Fallback",
		Frequency:   FrequencyMonthly,
		DayOfMonth:  1,
		StartDate:   mustParseDate(t, startDate),
		Active:      true,
		SplitType:   SplitTypePercentage,
		// No SplitPct → should use global default 60%.
	}
	if err := CreateRecurring(db, 1, r1); err != nil {
		t.Fatalf("create recurring: %v", err)
	}

	req := withUser(httptest.NewRequest("GET", "/api/budget/regning", nil), 1)
	rec := httptest.NewRecorder()
	RegningHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body RegningResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Expenses) != 1 {
		t.Fatalf("expected 1 expense, got %d", len(body.Expenses))
	}
	if body.Expenses[0].YourShare != 600 {
		t.Errorf("your_share with fallback 60%%: want 600, got %v", body.Expenses[0].YourShare)
	}
	if body.Expenses[0].PartnerShare != 400 {
		t.Errorf("partner_share with fallback 60%%: want 400, got %v", body.Expenses[0].PartnerShare)
	}
}

func TestRegningHandler_IncomeDueDates(t *testing.T) {
	db := setupTestDB(t)

	// Set income_day=20 for user, partner_income_day=15.
	_, err := db.Exec(`INSERT INTO user_preferences (user_id, key, value) VALUES (1, 'income_day', '20')`)
	if err != nil {
		t.Fatalf("insert income_day: %v", err)
	}
	_, err = db.Exec(`INSERT INTO user_preferences (user_id, key, value) VALUES (1, 'partner_income_day', '15')`)
	if err != nil {
		t.Fatalf("insert partner_income_day: %v", err)
	}

	req := withUser(httptest.NewRequest("GET", "/api/budget/regning", nil), 1)
	rec := httptest.NewRecorder()
	RegningHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body RegningResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if body.YourIncomeDay != 20 {
		t.Errorf("your_income_day: want 20, got %d", body.YourIncomeDay)
	}
	if body.PartnerIncomeDay != 15 {
		t.Errorf("partner_income_day: want 15, got %d", body.PartnerIncomeDay)
	}
	if body.YourIncomeDue == "" {
		t.Error("your_income_due should be populated")
	}
	if body.PartnerIncomeDue == "" {
		t.Error("partner_income_due should be populated")
	}
}
