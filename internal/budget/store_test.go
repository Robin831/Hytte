package budget

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/encryption"
	_ "modernc.org/sqlite"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	t.Setenv("ENCRYPTION_KEY", "test-key-for-budget-tests")
	encryption.ResetEncryptionKey()
	t.Cleanup(func() { encryption.ResetEncryptionKey() })

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		t.Fatalf("enable fk: %v", err)
	}
	_, err = db.Exec(`
		CREATE TABLE users (
			id        INTEGER PRIMARY KEY,
			email     TEXT UNIQUE NOT NULL,
			name      TEXT NOT NULL,
			picture   TEXT NOT NULL DEFAULT '',
			google_id TEXT UNIQUE NOT NULL,
			is_admin  INTEGER NOT NULL DEFAULT 0
		);
		CREATE TABLE budget_accounts (
			id       INTEGER PRIMARY KEY,
			user_id  INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			name     TEXT NOT NULL DEFAULT '',
			type     TEXT NOT NULL DEFAULT 'checking',
			currency TEXT NOT NULL DEFAULT 'NOK',
			balance  REAL NOT NULL DEFAULT 0,
			icon     TEXT NOT NULL DEFAULT '',
			UNIQUE(user_id, id)
		);
		CREATE TABLE budget_categories (
			id         INTEGER PRIMARY KEY,
			user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			name       TEXT NOT NULL DEFAULT '',
			group_name TEXT NOT NULL DEFAULT '',
			icon       TEXT NOT NULL DEFAULT '',
			color      TEXT NOT NULL DEFAULT '',
			is_income  INTEGER NOT NULL DEFAULT 0,
			UNIQUE(user_id, id)
		);
		CREATE TABLE budget_transactions (
			id             INTEGER PRIMARY KEY,
			user_id        INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			account_id     INTEGER NOT NULL,
			category_id    INTEGER,
			amount         REAL NOT NULL DEFAULT 0,
			description    TEXT NOT NULL DEFAULT '',
			date           TEXT NOT NULL,
			tags           TEXT NOT NULL DEFAULT '[]',
			is_transfer    INTEGER NOT NULL DEFAULT 0,
			transfer_to_id INTEGER,
			FOREIGN KEY (user_id, account_id)     REFERENCES budget_accounts(user_id, id)   ON DELETE CASCADE,
			FOREIGN KEY (user_id, category_id)    REFERENCES budget_categories(user_id, id) ON DELETE SET NULL,
			FOREIGN KEY (user_id, transfer_to_id) REFERENCES budget_accounts(user_id, id)   ON DELETE SET NULL
		);
		CREATE TABLE budget_recurring (
			id             INTEGER PRIMARY KEY,
			user_id        INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			account_id     INTEGER NOT NULL,
			category_id    INTEGER,
			amount         REAL NOT NULL DEFAULT 0,
			description    TEXT NOT NULL DEFAULT '',
			frequency      TEXT NOT NULL DEFAULT 'monthly',
			day_of_month   INTEGER NOT NULL DEFAULT 1,
			start_date     TEXT NOT NULL,
			end_date       TEXT,
			last_generated TEXT,
			active         INTEGER NOT NULL DEFAULT 1,
			FOREIGN KEY (user_id, account_id)  REFERENCES budget_accounts(user_id, id)   ON DELETE CASCADE,
			FOREIGN KEY (user_id, category_id) REFERENCES budget_categories(user_id, id) ON DELETE SET NULL
		);
		CREATE TABLE budget_limits (
			id             INTEGER PRIMARY KEY,
			user_id        INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			category_id    INTEGER NOT NULL,
			amount         REAL NOT NULL DEFAULT 0,
			period         TEXT NOT NULL DEFAULT 'monthly',
			effective_from TEXT NOT NULL CHECK (effective_from GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-01'),
			UNIQUE(user_id, category_id, effective_from),
			FOREIGN KEY (user_id, category_id) REFERENCES budget_categories(user_id, id) ON DELETE CASCADE
		);
		CREATE TABLE user_preferences (
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			key     TEXT NOT NULL,
			value   TEXT NOT NULL DEFAULT '',
			PRIMARY KEY (user_id, key)
		);
		CREATE TABLE budget_loans (
			id               INTEGER PRIMARY KEY,
			user_id          INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			name             TEXT NOT NULL DEFAULT '',
			principal        REAL NOT NULL DEFAULT 0,
			current_balance  REAL NOT NULL DEFAULT 0,
			annual_rate      REAL NOT NULL DEFAULT 0,
			monthly_payment  REAL NOT NULL DEFAULT 0,
			start_date       TEXT NOT NULL,
			term_months      INTEGER NOT NULL DEFAULT 0,
			property_value   REAL NOT NULL DEFAULT 0,
			property_name    TEXT NOT NULL DEFAULT '',
			notes            TEXT NOT NULL DEFAULT ''
		);
	`)
	if err != nil {
		t.Fatalf("create schema: %v", err)
	}
	_, err = db.Exec("INSERT INTO users (id, email, name, google_id) VALUES (1, 'test@example.com', 'Test', 'g123')")
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	t.Cleanup(func() { db.Close() }) //nolint:errcheck
	return db
}

// -- Account tests --

func TestAccountCRUD(t *testing.T) {
	db := setupTestDB(t)

	a := &Account{
		Name:     "Main Checking",
		Type:     AccountTypeChecking,
		Currency: "NOK",
		Balance:  1000.50,
		Icon:     "bank",
	}
	if err := CreateAccount(db, 1, a); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	if a.ID == 0 {
		t.Fatal("expected non-zero ID after create")
	}

	got, err := GetAccount(db, 1, a.ID)
	if err != nil {
		t.Fatalf("GetAccount: %v", err)
	}
	if got.Name != "Main Checking" {
		t.Errorf("Name = %q, want %q", got.Name, "Main Checking")
	}
	if got.Balance != 1000.50 {
		t.Errorf("Balance = %v, want 1000.50", got.Balance)
	}

	got.Name = "Updated Checking"
	got.Balance = 2000.00
	if err := UpdateAccount(db, 1, got); err != nil {
		t.Fatalf("UpdateAccount: %v", err)
	}

	got2, err := GetAccount(db, 1, a.ID)
	if err != nil {
		t.Fatalf("GetAccount after update: %v", err)
	}
	if got2.Name != "Updated Checking" {
		t.Errorf("Name after update = %q, want %q", got2.Name, "Updated Checking")
	}

	accounts, err := ListAccounts(db, 1)
	if err != nil {
		t.Fatalf("ListAccounts: %v", err)
	}
	if len(accounts) != 1 {
		t.Errorf("len(accounts) = %d, want 1", len(accounts))
	}

	if err := DeleteAccount(db, 1, a.ID); err != nil {
		t.Fatalf("DeleteAccount: %v", err)
	}
	accounts, err = ListAccounts(db, 1)
	if err != nil {
		t.Fatalf("ListAccounts after delete: %v", err)
	}
	if len(accounts) != 0 {
		t.Errorf("len(accounts) after delete = %d, want 0", len(accounts))
	}
}

func TestGetAccount_NotFound(t *testing.T) {
	db := setupTestDB(t)
	_, err := GetAccount(db, 1, 999)
	if err == nil {
		t.Fatal("expected error for missing account")
	}
}

func TestDeleteAccount_NotFound(t *testing.T) {
	db := setupTestDB(t)
	err := DeleteAccount(db, 1, 999)
	if err == nil {
		t.Fatal("expected error for missing account")
	}
}

func TestEncryptionAtRest(t *testing.T) {
	db := setupTestDB(t)

	// Account name must be encrypted in the DB.
	a := &Account{Name: "Secret Account", Type: AccountTypeChecking, Currency: "NOK"}
	if err := CreateAccount(db, 1, a); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	var rawAccountName string
	if err := db.QueryRow("SELECT name FROM budget_accounts WHERE id=?", a.ID).Scan(&rawAccountName); err != nil {
		t.Fatalf("query raw account name: %v", err)
	}
	if rawAccountName == "Secret Account" {
		t.Error("account name stored as plaintext; expected encrypted")
	}
	if !strings.HasPrefix(rawAccountName, "enc:") {
		t.Errorf("account name raw value %q does not have 'enc:' prefix", rawAccountName)
	}

	// Category name and group_name must be encrypted in the DB.
	c := &Category{Name: "Private Category", GroupName: "Hidden Group", Color: "#fff"}
	if err := CreateCategory(db, 1, c); err != nil {
		t.Fatalf("CreateCategory: %v", err)
	}
	var rawCatName, rawGroupName string
	if err := db.QueryRow("SELECT name, group_name FROM budget_categories WHERE id=?", c.ID).Scan(&rawCatName, &rawGroupName); err != nil {
		t.Fatalf("query raw category fields: %v", err)
	}
	if rawCatName == "Private Category" {
		t.Error("category name stored as plaintext; expected encrypted")
	}
	if !strings.HasPrefix(rawCatName, "enc:") {
		t.Errorf("category name raw value %q does not have 'enc:' prefix", rawCatName)
	}
	if rawGroupName == "Hidden Group" {
		t.Error("category group_name stored as plaintext; expected encrypted")
	}
	if !strings.HasPrefix(rawGroupName, "enc:") {
		t.Errorf("category group_name raw value %q does not have 'enc:' prefix", rawGroupName)
	}

	// Transaction description must be encrypted in the DB.
	tx := &Transaction{
		AccountID:   a.ID,
		Amount:      -50,
		Description: "Top Secret Purchase",
		Date:        "2026-01-01",
	}
	if err := CreateTransaction(db, 1, tx); err != nil {
		t.Fatalf("CreateTransaction: %v", err)
	}
	var rawDesc string
	if err := db.QueryRow("SELECT description FROM budget_transactions WHERE id=?", tx.ID).Scan(&rawDesc); err != nil {
		t.Fatalf("query raw transaction description: %v", err)
	}
	if rawDesc == "Top Secret Purchase" {
		t.Error("transaction description stored as plaintext; expected encrypted")
	}
	if !strings.HasPrefix(rawDesc, "enc:") {
		t.Errorf("transaction description raw value %q does not have 'enc:' prefix", rawDesc)
	}
}

// -- Category tests --

func TestCategoryCRUD(t *testing.T) {
	db := setupTestDB(t)

	c := &Category{
		Name:      "Groceries",
		GroupName: "Food",
		Icon:      "shopping-cart",
		Color:     "#00FF00",
		IsIncome:  false,
	}
	if err := CreateCategory(db, 1, c); err != nil {
		t.Fatalf("CreateCategory: %v", err)
	}
	if c.ID == 0 {
		t.Fatal("expected non-zero ID after create")
	}

	got, err := GetCategory(db, 1, c.ID)
	if err != nil {
		t.Fatalf("GetCategory: %v", err)
	}
	if got.Name != "Groceries" {
		t.Errorf("Name = %q, want %q", got.Name, "Groceries")
	}
	if got.GroupName != "Food" {
		t.Errorf("GroupName = %q, want %q", got.GroupName, "Food")
	}
	if got.IsIncome {
		t.Error("IsIncome should be false")
	}

	got.Name = "Supermarket"
	got.IsIncome = true
	if err := UpdateCategory(db, 1, got); err != nil {
		t.Fatalf("UpdateCategory: %v", err)
	}

	got2, err := GetCategory(db, 1, c.ID)
	if err != nil {
		t.Fatalf("GetCategory after update: %v", err)
	}
	if got2.Name != "Supermarket" {
		t.Errorf("Name after update = %q, want %q", got2.Name, "Supermarket")
	}
	if !got2.IsIncome {
		t.Error("IsIncome should be true after update")
	}

	categories, err := ListCategories(db, 1)
	if err != nil {
		t.Fatalf("ListCategories: %v", err)
	}
	if len(categories) != 1 {
		t.Errorf("len(categories) = %d, want 1", len(categories))
	}

	if err := DeleteCategory(db, 1, c.ID); err != nil {
		t.Fatalf("DeleteCategory: %v", err)
	}
	categories, err = ListCategories(db, 1)
	if err != nil {
		t.Fatalf("ListCategories after delete: %v", err)
	}
	if len(categories) != 0 {
		t.Errorf("len(categories) after delete = %d, want 0", len(categories))
	}
}

// -- Transaction tests --

func createTestAccount(t *testing.T, db *sql.DB) int64 {
	t.Helper()
	a := &Account{Name: "Test Account", Type: AccountTypeChecking, Currency: "NOK"}
	if err := CreateAccount(db, 1, a); err != nil {
		t.Fatalf("create test account: %v", err)
	}
	return a.ID
}

func TestTransactionCRUD(t *testing.T) {
	db := setupTestDB(t)
	accID := createTestAccount(t, db)

	tx := &Transaction{
		AccountID:   accID,
		Amount:      -199.00,
		Description: "Cinema tickets",
		Date:        "2026-01-15",
		Tags:        []string{"leisure", "entertainment"},
		IsTransfer:  false,
	}
	if err := CreateTransaction(db, 1, tx); err != nil {
		t.Fatalf("CreateTransaction: %v", err)
	}
	if tx.ID == 0 {
		t.Fatal("expected non-zero ID after create")
	}

	got, err := GetTransaction(db, 1, tx.ID)
	if err != nil {
		t.Fatalf("GetTransaction: %v", err)
	}
	if got.Description != "Cinema tickets" {
		t.Errorf("Description = %q, want %q", got.Description, "Cinema tickets")
	}
	if len(got.Tags) != 2 {
		t.Errorf("len(Tags) = %d, want 2", len(got.Tags))
	}

	got.Description = "Cinema tickets (updated)"
	got.Amount = -220.00
	if err := UpdateTransaction(db, 1, got); err != nil {
		t.Fatalf("UpdateTransaction: %v", err)
	}

	got2, err := GetTransaction(db, 1, tx.ID)
	if err != nil {
		t.Fatalf("GetTransaction after update: %v", err)
	}
	if got2.Description != "Cinema tickets (updated)" {
		t.Errorf("Description after update = %q", got2.Description)
	}

	if err := DeleteTransaction(db, 1, tx.ID); err != nil {
		t.Fatalf("DeleteTransaction: %v", err)
	}
	_, err = GetTransaction(db, 1, tx.ID)
	if err == nil {
		t.Fatal("expected error after delete")
	}
}

func TestListTransactions_Filters(t *testing.T) {
	db := setupTestDB(t)
	acc1 := createTestAccount(t, db)

	a2 := &Account{Name: "Savings", Type: AccountTypeSavings, Currency: "NOK"}
	if err := CreateAccount(db, 1, a2); err != nil {
		t.Fatalf("create second account: %v", err)
	}
	acc2 := a2.ID

	txns := []Transaction{
		{AccountID: acc1, Amount: -100, Description: "Coffee", Date: "2026-01-10"},
		{AccountID: acc1, Amount: -200, Description: "Lunch", Date: "2026-01-20"},
		{AccountID: acc2, Amount: 5000, Description: "Salary", Date: "2026-01-25"},
	}
	for i := range txns {
		if err := CreateTransaction(db, 1, &txns[i]); err != nil {
			t.Fatalf("create tx %d: %v", i, err)
		}
	}

	all, err := ListTransactions(db, 1, TransactionFilter{})
	if err != nil {
		t.Fatalf("ListTransactions all: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("all: got %d, want 3", len(all))
	}

	byAcc, err := ListTransactions(db, 1, TransactionFilter{AccountID: &acc1})
	if err != nil {
		t.Fatalf("ListTransactions by account: %v", err)
	}
	if len(byAcc) != 2 {
		t.Errorf("by account: got %d, want 2", len(byAcc))
	}

	from := "2026-01-15"
	byDate, err := ListTransactions(db, 1, TransactionFilter{FromDate: from})
	if err != nil {
		t.Fatalf("ListTransactions by date: %v", err)
	}
	if len(byDate) != 2 {
		t.Errorf("by from-date: got %d, want 2", len(byDate))
	}

	to := "2026-01-15"
	byRange, err := ListTransactions(db, 1, TransactionFilter{FromDate: "2026-01-10", ToDate: to})
	if err != nil {
		t.Fatalf("ListTransactions by range: %v", err)
	}
	if len(byRange) != 1 {
		t.Errorf("by range: got %d, want 1", len(byRange))
	}
}

// -- Recurring tests --

func TestRecurringCRUD(t *testing.T) {
	db := setupTestDB(t)
	accID := createTestAccount(t, db)

	r := &Recurring{
		AccountID:   accID,
		Amount:      -500,
		Frequency:   FrequencyMonthly,
		DayOfMonth:  1,
		StartDate:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		EndDate:     "",
		LastGenerated: "",
	}
	if err := CreateRecurring(db, 1, r); err != nil {
		t.Fatalf("CreateRecurring: %v", err)
	}
	if r.ID == 0 {
		t.Fatal("expected non-zero ID after create")
	}

	got, err := GetRecurring(db, 1, r.ID)
	if err != nil {
		t.Fatalf("GetRecurring: %v", err)
	}
	if got.Amount != -500 {
		t.Errorf("Amount = %v, want -500", got.Amount)
	}
	if got.Frequency != FrequencyMonthly {
		t.Errorf("Frequency = %v, want monthly", got.Frequency)
	}
	if !got.StartDate.Equal(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("StartDate = %v", got.StartDate)
	}

	got.Amount = -600
	got.EndDate = "2027-12-31"
	if err := UpdateRecurring(db, 1, got); err != nil {
		t.Fatalf("UpdateRecurring: %v", err)
	}

	got2, err := GetRecurring(db, 1, r.ID)
	if err != nil {
		t.Fatalf("GetRecurring after update: %v", err)
	}
	if got2.Amount != -600 {
		t.Errorf("Amount after update = %v, want -600", got2.Amount)
	}
	if got2.EndDate != "2027-12-31" {
		t.Errorf("EndDate after update = %q, want 2027-12-31", got2.EndDate)
	}

	rules, err := ListRecurring(db, 1)
	if err != nil {
		t.Fatalf("ListRecurring: %v", err)
	}
	if len(rules) != 1 {
		t.Errorf("len(rules) = %d, want 1", len(rules))
	}

	if err := DeleteRecurring(db, 1, r.ID); err != nil {
		t.Fatalf("DeleteRecurring: %v", err)
	}
	rules, err = ListRecurring(db, 1)
	if err != nil {
		t.Fatalf("ListRecurring after delete: %v", err)
	}
	if len(rules) != 0 {
		t.Errorf("len(rules) after delete = %d, want 0", len(rules))
	}
}

func TestCreateRecurring_ActiveFalsePreserved(t *testing.T) {
	db := setupTestDB(t)
	accID := createTestAccount(t, db)

	r := &Recurring{
		AccountID:  accID,
		Amount:     -100,
		Frequency:  FrequencyMonthly,
		DayOfMonth: 1,
		StartDate:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Active:     false,
	}
	if err := CreateRecurring(db, 1, r); err != nil {
		t.Fatalf("CreateRecurring: %v", err)
	}
	got, err := GetRecurring(db, 1, r.ID)
	if err != nil {
		t.Fatalf("GetRecurring: %v", err)
	}
	if got.Active {
		t.Error("Active should be false when created with Active=false")
	}
}

func TestIsRecurringDue_EdgeCases(t *testing.T) {
	startDate := time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name          string
		r             Recurring
		today         string
		wantDue       bool
	}{
		{
			name: "monthly DayOfMonth=31 clamps to Feb 28",
			r: Recurring{
				Frequency:     FrequencyMonthly,
				DayOfMonth:    31,
				StartDate:     startDate,
				LastGenerated: "2026-01-31",
			},
			today:   "2026-02-28",
			wantDue: true,
		},
		{
			name: "monthly DayOfMonth=31 not due before clamped date",
			r: Recurring{
				Frequency:     FrequencyMonthly,
				DayOfMonth:    31,
				StartDate:     startDate,
				LastGenerated: "2026-01-31",
			},
			today:   "2026-02-27",
			wantDue: false,
		},
		{
			name: "weekly rule due after 7 days",
			r: Recurring{
				Frequency:     FrequencyWeekly,
				DayOfMonth:    1,
				StartDate:     startDate,
				LastGenerated: "2026-03-26",
			},
			today:   "2026-04-02",
			wantDue: true,
		},
		{
			name: "weekly rule not due before 7 days",
			r: Recurring{
				Frequency:     FrequencyWeekly,
				DayOfMonth:    1,
				StartDate:     startDate,
				LastGenerated: "2026-03-26",
			},
			today:   "2026-04-01",
			wantDue: false,
		},
		{
			name: "monthly DayOfMonth=31 in March has 31 days — no clamping needed",
			r: Recurring{
				Frequency:     FrequencyMonthly,
				DayOfMonth:    31,
				StartDate:     startDate,
				LastGenerated: "2026-02-28",
			},
			today:   "2026-03-31",
			wantDue: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := isRecurringDue(tc.r, tc.today)
			if got != tc.wantDue {
				t.Errorf("isRecurringDue = %v, want %v", got, tc.wantDue)
			}
		})
	}
}

// -- Budget limit tests --

func createTestCategory(t *testing.T, db *sql.DB) int64 {
	t.Helper()
	c := &Category{Name: "Test Category", GroupName: "Group", Color: "#ff0000"}
	if err := CreateCategory(db, 1, c); err != nil {
		t.Fatalf("create test category: %v", err)
	}
	return c.ID
}

func TestSetBudgetLimit_InsertAndUpsert(t *testing.T) {
	db := setupTestDB(t)
	catID := createTestCategory(t, db)

	// Insert a new limit.
	lim := &BudgetLimit{
		CategoryID:    catID,
		Amount:        5000,
		Period:        "monthly",
		EffectiveFrom: "2026-01",
	}
	if err := SetBudgetLimit(db, 1, lim); err != nil {
		t.Fatalf("SetBudgetLimit insert: %v", err)
	}
	if lim.ID == 0 {
		t.Fatal("expected non-zero ID after insert")
	}
	if lim.EffectiveFrom != "2026-01-01" {
		t.Errorf("EffectiveFrom = %q, want %q", lim.EffectiveFrom, "2026-01-01")
	}

	// Upsert the same (user, category, effective_from) — amount should change.
	lim2 := &BudgetLimit{
		CategoryID:    catID,
		Amount:        6000,
		Period:        "monthly",
		EffectiveFrom: "2026-01",
	}
	if err := SetBudgetLimit(db, 1, lim2); err != nil {
		t.Fatalf("SetBudgetLimit upsert: %v", err)
	}

	limits, err := GetBudgetLimits(db, 1, "2026-01")
	if err != nil {
		t.Fatalf("GetBudgetLimits: %v", err)
	}
	got, ok := limits[catID]
	if !ok {
		t.Fatal("expected limit for category not found")
	}
	if got.Amount != 6000 {
		t.Errorf("Amount after upsert = %v, want 6000", got.Amount)
	}
}

func TestGetBudgetLimits_PicksLatestEffective(t *testing.T) {
	db := setupTestDB(t)
	catID := createTestCategory(t, db)

	// Set a limit effective January 2026.
	if err := SetBudgetLimit(db, 1, &BudgetLimit{
		CategoryID: catID, Amount: 3000, Period: "monthly", EffectiveFrom: "2026-01",
	}); err != nil {
		t.Fatalf("SetBudgetLimit jan: %v", err)
	}

	// Set a later limit effective February 2026.
	if err := SetBudgetLimit(db, 1, &BudgetLimit{
		CategoryID: catID, Amount: 4000, Period: "monthly", EffectiveFrom: "2026-02",
	}); err != nil {
		t.Fatalf("SetBudgetLimit feb: %v", err)
	}

	// Querying for January should return the January limit (3000).
	jan, err := GetBudgetLimits(db, 1, "2026-01")
	if err != nil {
		t.Fatalf("GetBudgetLimits jan: %v", err)
	}
	if jan[catID].Amount != 3000 {
		t.Errorf("jan Amount = %v, want 3000", jan[catID].Amount)
	}

	// Querying for February should return the February limit (4000).
	feb, err := GetBudgetLimits(db, 1, "2026-02")
	if err != nil {
		t.Fatalf("GetBudgetLimits feb: %v", err)
	}
	if feb[catID].Amount != 4000 {
		t.Errorf("feb Amount = %v, want 4000", feb[catID].Amount)
	}

	// Querying for March (no March limit) should still return the February limit (latest <= March).
	mar, err := GetBudgetLimits(db, 1, "2026-03")
	if err != nil {
		t.Fatalf("GetBudgetLimits mar: %v", err)
	}
	if mar[catID].Amount != 4000 {
		t.Errorf("mar Amount = %v, want 4000 (latest effective)", mar[catID].Amount)
	}
}

func TestGetBudgetLimits_NoLimits(t *testing.T) {
	db := setupTestDB(t)

	limits, err := GetBudgetLimits(db, 1, "2026-01")
	if err != nil {
		t.Fatalf("GetBudgetLimits: %v", err)
	}
	if len(limits) != 0 {
		t.Errorf("expected 0 limits, got %d", len(limits))
	}
}

func TestGetBudgetLimits_FutureEffectiveExcluded(t *testing.T) {
	db := setupTestDB(t)
	catID := createTestCategory(t, db)

	// Set a limit effective March 2026.
	if err := SetBudgetLimit(db, 1, &BudgetLimit{
		CategoryID: catID, Amount: 7000, Period: "monthly", EffectiveFrom: "2026-03",
	}); err != nil {
		t.Fatalf("SetBudgetLimit: %v", err)
	}

	// Querying for January 2026 — the March limit is in the future, so nothing returned.
	limits, err := GetBudgetLimits(db, 1, "2026-01")
	if err != nil {
		t.Fatalf("GetBudgetLimits: %v", err)
	}
	if len(limits) != 0 {
		t.Errorf("expected 0 limits for month before effective_from, got %d", len(limits))
	}
}

func TestGetRecurringDue(t *testing.T) {
	db := setupTestDB(t)
	accID := createTestAccount(t, db)

	now := time.Date(2026, 4, 2, 0, 0, 0, 0, time.UTC)

	// Rule 1: never generated, start_date in the past — should be due.
	r1 := &Recurring{
		AccountID:  accID,
		Amount:     -100,
		Frequency:  FrequencyMonthly,
		DayOfMonth: 1,
		StartDate:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Active:     true,
	}
	if err := CreateRecurring(db, 1, r1); err != nil {
		t.Fatalf("create r1: %v", err)
	}

	// Rule 2: last_generated one month ago — should be due.
	r2 := &Recurring{
		AccountID:     accID,
		Amount:        -200,
		Frequency:     FrequencyMonthly,
		DayOfMonth:    1,
		StartDate:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		LastGenerated: "2026-03-01",
		Active:        true,
	}
	if err := CreateRecurring(db, 1, r2); err != nil {
		t.Fatalf("create r2: %v", err)
	}

	// Rule 3: last_generated today — NOT yet due (next due is one month away).
	r3 := &Recurring{
		AccountID:     accID,
		Amount:        -300,
		Frequency:     FrequencyMonthly,
		DayOfMonth:    1,
		StartDate:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		LastGenerated: "2026-04-02",
		Active:        true,
	}
	if err := CreateRecurring(db, 1, r3); err != nil {
		t.Fatalf("create r3: %v", err)
	}

	// Rule 4: end_date in the past but never generated — should be included so that
	// ungenerated occurrences before the end_date can be backfilled.
	r4 := &Recurring{
		AccountID:  accID,
		Amount:     -400,
		Frequency:  FrequencyMonthly,
		DayOfMonth: 1,
		StartDate:  time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		EndDate:    "2025-12-31",
		Active:     true,
	}
	if err := CreateRecurring(db, 1, r4); err != nil {
		t.Fatalf("create r4: %v", err)
	}

	due, err := GetRecurringDue(db, 1, now)
	if err != nil {
		t.Fatalf("GetRecurringDue: %v", err)
	}
	if len(due) != 3 {
		t.Errorf("GetRecurringDue: got %d rules, want 3", len(due))
	}
}

func TestGenerateRecurringTransactions(t *testing.T) {
	db := setupTestDB(t)
	accID := createTestAccount(t, db)

	// Fixed reference time: 2026-04-03.
	now := time.Date(2026, 4, 3, 0, 0, 0, 0, time.UTC)

	// Rule 1: never generated, start_date 2026-01-01.
	// Expected occurrences: Jan 1, Feb 1, Mar 1, Apr 1 = 4 transactions.
	r1 := &Recurring{
		AccountID:  accID,
		Amount:     -100,
		Frequency:  FrequencyMonthly,
		DayOfMonth: 1,
		StartDate:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Active:     true,
	}
	if err := CreateRecurring(db, 1, r1); err != nil {
		t.Fatalf("create r1: %v", err)
	}

	// Rule 2: last_generated 2026-03-01, generates for Apr 1 = 1 transaction.
	r2 := &Recurring{
		AccountID:     accID,
		Amount:        -200,
		Frequency:     FrequencyMonthly,
		DayOfMonth:    1,
		StartDate:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		LastGenerated: "2026-03-01",
		Active:        true,
	}
	if err := CreateRecurring(db, 1, r2); err != nil {
		t.Fatalf("create r2: %v", err)
	}

	count, err := GenerateRecurringTransactions(db, 1, now)
	if err != nil {
		t.Fatalf("GenerateRecurringTransactions: %v", err)
	}
	// r1: 4 occurrences (Jan, Feb, Mar, Apr), r2: 1 occurrence (Apr).
	if count != 5 {
		t.Errorf("count = %d, want 5", count)
	}

	// Verify r1.last_generated was advanced to 2026-04-01.
	updated1, err := GetRecurring(db, 1, r1.ID)
	if err != nil {
		t.Fatalf("GetRecurring r1: %v", err)
	}
	if updated1.LastGenerated != "2026-04-01" {
		t.Errorf("r1.LastGenerated = %q, want 2026-04-01", updated1.LastGenerated)
	}

	// Verify r2.last_generated was advanced to 2026-04-01.
	updated2, err := GetRecurring(db, 1, r2.ID)
	if err != nil {
		t.Fatalf("GetRecurring r2: %v", err)
	}
	if updated2.LastGenerated != "2026-04-01" {
		t.Errorf("r2.LastGenerated = %q, want 2026-04-01", updated2.LastGenerated)
	}

	// Verify all 5 transactions were persisted.
	txns, err := ListTransactions(db, 1, TransactionFilter{})
	if err != nil {
		t.Fatalf("ListTransactions: %v", err)
	}
	if len(txns) != 5 {
		t.Errorf("transactions in DB = %d, want 5", len(txns))
	}

	// Second call should generate nothing (rules are up to date).
	count2, err := GenerateRecurringTransactions(db, 1, now)
	if err != nil {
		t.Fatalf("second GenerateRecurringTransactions: %v", err)
	}
	if count2 != 0 {
		t.Errorf("second run count = %d, want 0", count2)
	}
}
