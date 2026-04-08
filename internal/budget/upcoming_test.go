package budget

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestUpcomingHandler_Empty(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("GET", "/api/budget/upcoming", nil), 1)
	rec := httptest.NewRecorder()
	UpcomingHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Upcoming []UpcomingTransaction `json:"upcoming"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Upcoming == nil {
		t.Error("expected non-nil upcoming slice, got null")
	}
	if len(body.Upcoming) != 0 {
		t.Errorf("expected 0 upcoming, got %d", len(body.Upcoming))
	}
}

func TestUpcomingHandler_EmptyReturnsArray(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("GET", "/api/budget/upcoming", nil), 1)
	rec := httptest.NewRecorder()
	UpcomingHandler(db).ServeHTTP(rec, req)

	// Verify JSON contains [] not null for the upcoming field.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if string(raw["upcoming"]) == "null" {
		t.Error("upcoming field is null, expected empty array []")
	}
	if string(raw["upcoming"]) != "[]" {
		t.Errorf("upcoming field = %s, expected []", string(raw["upcoming"]))
	}
}

func TestUpcomingHandler_WithRecurring(t *testing.T) {
	db := setupTestDB(t)

	// Create an account for the recurring FK.
	_, err := db.Exec(`INSERT INTO budget_accounts (id, user_id, name, type, currency, balance) VALUES (1, 1, 'Main', 'checking', 'NOK', 0)`)
	if err != nil {
		t.Fatalf("insert account: %v", err)
	}

	// Create a category.
	_, err = db.Exec(`INSERT INTO budget_categories (id, user_id, name, icon, color) VALUES (10, 1, 'Utilities', 'zap', '#ff0000')`)
	if err != nil {
		t.Fatalf("insert category: %v", err)
	}

	// Create a monthly recurring rule due mid-month (should appear in 30-day window).
	now := time.Now()
	dayOfMonth := now.Day() + 5
	if dayOfMonth > 28 {
		dayOfMonth = 1
	}
	catID := int64(10)
	rec := &Recurring{
		AccountID:   1,
		CategoryID:  &catID,
		Amount:      1500,
		Description: "Electricity",
		Frequency:   FrequencyMonthly,
		DayOfMonth:  dayOfMonth,
		StartDate:   mustParseDate(t, now.AddDate(0, -1, 0).Format("2006-01-02")),
		Active:      true,
		SplitType:   SplitTypeEqual,
	}
	if err := CreateRecurring(db, 1, rec); err != nil {
		t.Fatalf("create recurring: %v", err)
	}

	req := withUser(httptest.NewRequest("GET", "/api/budget/upcoming", nil), 1)
	w := httptest.NewRecorder()
	UpcomingHandler(db).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var body struct {
		Upcoming []UpcomingTransaction `json:"upcoming"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Upcoming) == 0 {
		t.Fatal("expected at least 1 upcoming transaction")
	}

	item := body.Upcoming[0]
	if item.Description != "Electricity" {
		t.Errorf("description = %q, want %q", item.Description, "Electricity")
	}
	if item.Amount != 1500 {
		t.Errorf("amount = %v, want 1500", item.Amount)
	}
	if item.YourShare != 750 {
		t.Errorf("your_share = %v, want 750 (equal split)", item.YourShare)
	}
	if item.CategoryName != "Utilities" {
		t.Errorf("category_name = %q, want %q", item.CategoryName, "Utilities")
	}
	if item.CategoryColor != "#ff0000" {
		t.Errorf("category_color = %q, want %q", item.CategoryColor, "#ff0000")
	}
	if item.SplitType != SplitTypeEqual {
		t.Errorf("split_type = %q, want %q", item.SplitType, SplitTypeEqual)
	}
}

func TestUpcomingHandler_ExcludesExpiredRecurring(t *testing.T) {
	db := setupTestDB(t)

	_, err := db.Exec(`INSERT INTO budget_accounts (id, user_id, name, type, currency, balance) VALUES (1, 1, 'Main', 'checking', 'NOK', 0)`)
	if err != nil {
		t.Fatalf("insert account: %v", err)
	}

	// Create a recurring rule that ended in the past.
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	rec := &Recurring{
		AccountID:   1,
		Amount:      500,
		Description: "Expired Sub",
		Frequency:   FrequencyMonthly,
		DayOfMonth:  15,
		StartDate:   mustParseDate(t, "2024-01-01"),
		EndDate:     yesterday,
		Active:      true,
		SplitType:   SplitTypePercentage,
	}
	if err := CreateRecurring(db, 1, rec); err != nil {
		t.Fatalf("create recurring: %v", err)
	}

	req := withUser(httptest.NewRequest("GET", "/api/budget/upcoming", nil), 1)
	w := httptest.NewRecorder()
	UpcomingHandler(db).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var body struct {
		Upcoming []UpcomingTransaction `json:"upcoming"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Upcoming) != 0 {
		t.Errorf("expected 0 upcoming for expired recurring, got %d", len(body.Upcoming))
	}
}

func TestUpcomingHandler_SortedByDate(t *testing.T) {
	db := setupTestDB(t)

	_, err := db.Exec(`INSERT INTO budget_accounts (id, user_id, name, type, currency, balance) VALUES (1, 1, 'Main', 'checking', 'NOK', 0)`)
	if err != nil {
		t.Fatalf("insert account: %v", err)
	}

	now := time.Now()
	// Create two recurring rules with different days.
	dayLater := now.Day() + 10
	dayEarlier := now.Day() + 3
	if dayLater > 28 {
		dayLater = 28
	}
	if dayEarlier > 28 {
		dayEarlier = 1
	}
	// Ensure they're distinct so we can verify ordering.
	if dayEarlier >= dayLater {
		dayEarlier = 1
		dayLater = 15
	}

	startDate := now.AddDate(0, -1, 0).Format("2006-01-02")

	r1 := &Recurring{
		AccountID:   1,
		Amount:      1000,
		Description: "Later Bill",
		Frequency:   FrequencyMonthly,
		DayOfMonth:  dayLater,
		StartDate:   mustParseDate(t, startDate),
		Active:      true,
		SplitType:   SplitTypePercentage,
	}
	r2 := &Recurring{
		AccountID:   1,
		Amount:      500,
		Description: "Earlier Bill",
		Frequency:   FrequencyMonthly,
		DayOfMonth:  dayEarlier,
		StartDate:   mustParseDate(t, startDate),
		Active:      true,
		SplitType:   SplitTypePercentage,
	}
	if err := CreateRecurring(db, 1, r1); err != nil {
		t.Fatalf("create recurring 1: %v", err)
	}
	if err := CreateRecurring(db, 1, r2); err != nil {
		t.Fatalf("create recurring 2: %v", err)
	}

	req := withUser(httptest.NewRequest("GET", "/api/budget/upcoming", nil), 1)
	w := httptest.NewRecorder()
	UpcomingHandler(db).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var body struct {
		Upcoming []UpcomingTransaction `json:"upcoming"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Upcoming) < 2 {
		t.Fatalf("expected at least 2 upcoming, got %d", len(body.Upcoming))
	}
	if body.Upcoming[0].Date > body.Upcoming[1].Date {
		t.Errorf("expected sorted by date ascending: %s > %s", body.Upcoming[0].Date, body.Upcoming[1].Date)
	}
}
