package budget

import (
	"database/sql"
	"testing"

	"github.com/Robin831/Hytte/internal/encryption"
	_ "modernc.org/sqlite"
)

// setupSeedTestDB creates an in-memory SQLite DB with the budget category
// schema and user_preferences table needed for seed and income-split tests.
func setupSeedTestDB(t *testing.T) *sql.DB {
	t.Helper()
	t.Setenv("ENCRYPTION_KEY", "test-key-for-seed-tests")
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
		CREATE TABLE user_preferences (
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			key     TEXT NOT NULL,
			value   TEXT NOT NULL DEFAULT '',
			PRIMARY KEY (user_id, key)
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

func TestSeedDefaultCategories(t *testing.T) {
	db := setupSeedTestDB(t)

	if err := SeedDefaultCategories(db, 1); err != nil {
		t.Fatalf("SeedDefaultCategories: %v", err)
	}

	cats, err := ListCategories(db, 1)
	if err != nil {
		t.Fatalf("ListCategories: %v", err)
	}

	if len(cats) != len(defaultCategories) {
		t.Errorf("got %d categories, want %d", len(cats), len(defaultCategories))
	}

	// Verify Lønn is marked as income.
	var found bool
	for _, c := range cats {
		if c.Name == "Lønn" {
			found = true
			if !c.IsIncome {
				t.Error("Lønn should have IsIncome=true")
			}
		}
	}
	if !found {
		t.Error("Lønn category not found after seed")
	}
}

func TestSeedDefaultCategoriesIdempotent(t *testing.T) {
	db := setupSeedTestDB(t)

	if err := SeedDefaultCategories(db, 1); err != nil {
		t.Fatalf("first SeedDefaultCategories: %v", err)
	}
	if err := SeedDefaultCategories(db, 1); err != nil {
		t.Fatalf("second SeedDefaultCategories: %v", err)
	}

	cats, err := ListCategories(db, 1)
	if err != nil {
		t.Fatalf("ListCategories: %v", err)
	}

	if len(cats) != len(defaultCategories) {
		t.Errorf("after double seed: got %d categories, want %d (idempotency violated)", len(cats), len(defaultCategories))
	}
}

func TestGetIncomeSplit_Default(t *testing.T) {
	db := setupSeedTestDB(t)

	pct, err := GetIncomeSplit(db, 1)
	if err != nil {
		t.Fatalf("GetIncomeSplit: %v", err)
	}
	if pct != defaultIncomeSplit {
		t.Errorf("got %d, want %d (default)", pct, defaultIncomeSplit)
	}
}

func TestSetGetIncomeSplit(t *testing.T) {
	db := setupSeedTestDB(t)

	if err := SetIncomeSplit(db, 1, 75); err != nil {
		t.Fatalf("SetIncomeSplit: %v", err)
	}

	pct, err := GetIncomeSplit(db, 1)
	if err != nil {
		t.Fatalf("GetIncomeSplit: %v", err)
	}
	if pct != 75 {
		t.Errorf("got %d, want 75", pct)
	}

	// Update to a different value to verify upsert works.
	if err := SetIncomeSplit(db, 1, 40); err != nil {
		t.Fatalf("SetIncomeSplit update: %v", err)
	}
	pct, err = GetIncomeSplit(db, 1)
	if err != nil {
		t.Fatalf("GetIncomeSplit after update: %v", err)
	}
	if pct != 40 {
		t.Errorf("got %d, want 40", pct)
	}
}

func TestSetIncomeSplit_OutOfRange(t *testing.T) {
	db := setupSeedTestDB(t)

	if err := SetIncomeSplit(db, 1, -1); err == nil {
		t.Error("expected error for pct=-1")
	}
	if err := SetIncomeSplit(db, 1, 101); err == nil {
		t.Error("expected error for pct=101")
	}
}

func TestGetIncomeSplit_OutOfRange(t *testing.T) {
	db := setupSeedTestDB(t)

	// Manually write out-of-range values directly to simulate corrupted data.
	for _, raw := range []string{"999", "-5", "101"} {
		if _, err := db.Exec(
			`INSERT INTO user_preferences (user_id, key, value) VALUES (1, ?, ?)
			 ON CONFLICT(user_id, key) DO UPDATE SET value = excluded.value`,
			incomeSplitKey, raw,
		); err != nil {
			t.Fatalf("insert preference %s: %v", raw, err)
		}
		pct, err := GetIncomeSplit(db, 1)
		if err != nil {
			t.Fatalf("GetIncomeSplit with value %s: %v", raw, err)
		}
		if pct != defaultIncomeSplit {
			t.Errorf("value %s: got %d, want default %d", raw, pct, defaultIncomeSplit)
		}
	}
}

func TestSetIncomeSplit_Boundaries(t *testing.T) {
	db := setupSeedTestDB(t)

	for _, pct := range []int{0, 100} {
		if err := SetIncomeSplit(db, 1, pct); err != nil {
			t.Errorf("SetIncomeSplit(%d): unexpected error: %v", pct, err)
		}
		got, err := GetIncomeSplit(db, 1)
		if err != nil {
			t.Fatalf("GetIncomeSplit: %v", err)
		}
		if got != pct {
			t.Errorf("got %d, want %d", got, pct)
		}
	}
}

func TestGetPartnerIncome_Default(t *testing.T) {
	db := setupSeedTestDB(t)

	amount, err := GetPartnerIncome(db, 1)
	if err != nil {
		t.Fatalf("GetPartnerIncome: %v", err)
	}
	if amount != defaultPartnerIncome {
		t.Errorf("got %d, want %d (default)", amount, defaultPartnerIncome)
	}
}

func TestSetGetPartnerIncome(t *testing.T) {
	db := setupSeedTestDB(t)

	if err := SetPartnerIncome(db, 1, 50000); err != nil {
		t.Fatalf("SetPartnerIncome: %v", err)
	}

	amount, err := GetPartnerIncome(db, 1)
	if err != nil {
		t.Fatalf("GetPartnerIncome: %v", err)
	}
	if amount != 50000 {
		t.Errorf("got %d, want 50000", amount)
	}

	// Update to a different value to verify upsert works.
	if err := SetPartnerIncome(db, 1, 75000); err != nil {
		t.Fatalf("SetPartnerIncome update: %v", err)
	}
	amount, err = GetPartnerIncome(db, 1)
	if err != nil {
		t.Fatalf("GetPartnerIncome after update: %v", err)
	}
	if amount != 75000 {
		t.Errorf("got %d, want 75000", amount)
	}
}

func TestSetPartnerIncome_Negative(t *testing.T) {
	db := setupSeedTestDB(t)

	if err := SetPartnerIncome(db, 1, -1); err == nil {
		t.Error("expected error for amount=-1")
	}
}

func TestSetPartnerIncome_Zero(t *testing.T) {
	db := setupSeedTestDB(t)

	if err := SetPartnerIncome(db, 1, 0); err != nil {
		t.Errorf("SetPartnerIncome(0): unexpected error: %v", err)
	}
	got, err := GetPartnerIncome(db, 1)
	if err != nil {
		t.Fatalf("GetPartnerIncome: %v", err)
	}
	if got != 0 {
		t.Errorf("got %d, want 0", got)
	}
}

func TestGetPartnerIncome_CorruptedData(t *testing.T) {
	db := setupSeedTestDB(t)

	// Manually write invalid/negative raw values to simulate corrupted data.
	for _, raw := range []string{"not-a-number", "-1", "-999"} {
		if _, err := db.Exec(
			`INSERT INTO user_preferences (user_id, key, value) VALUES (1, ?, ?)
			 ON CONFLICT(user_id, key) DO UPDATE SET value = excluded.value`,
			partnerIncomeKey, raw,
		); err != nil {
			t.Fatalf("insert preference %s: %v", raw, err)
		}
		amount, err := GetPartnerIncome(db, 1)
		if err != nil {
			t.Fatalf("GetPartnerIncome with value %s: %v", raw, err)
		}
		if amount != defaultPartnerIncome {
			t.Errorf("value %s: got %d, want default %d", raw, amount, defaultPartnerIncome)
		}
	}
}

func TestGetIncomeDay_Default(t *testing.T) {
	db := setupSeedTestDB(t)

	day, err := GetIncomeDay(db, 1)
	if err != nil {
		t.Fatalf("GetIncomeDay: %v", err)
	}
	if day != defaultIncomeDay {
		t.Errorf("got %d, want %d (default)", day, defaultIncomeDay)
	}
}

func TestSetGetIncomeDay(t *testing.T) {
	db := setupSeedTestDB(t)

	if err := SetIncomeDay(db, 1, 15); err != nil {
		t.Fatalf("SetIncomeDay: %v", err)
	}
	day, err := GetIncomeDay(db, 1)
	if err != nil {
		t.Fatalf("GetIncomeDay: %v", err)
	}
	if day != 15 {
		t.Errorf("got %d, want 15", day)
	}

	// Update and verify upsert works.
	if err := SetIncomeDay(db, 1, 25); err != nil {
		t.Fatalf("SetIncomeDay update: %v", err)
	}
	day, err = GetIncomeDay(db, 1)
	if err != nil {
		t.Fatalf("GetIncomeDay after update: %v", err)
	}
	if day != 25 {
		t.Errorf("got %d, want 25", day)
	}
}

func TestSetIncomeDay_OutOfRange(t *testing.T) {
	db := setupSeedTestDB(t)

	if err := SetIncomeDay(db, 1, 0); err == nil {
		t.Error("expected error for day=0")
	}
	if err := SetIncomeDay(db, 1, 32); err == nil {
		t.Error("expected error for day=32")
	}
}

func TestSetGetPartnerIncomeDay(t *testing.T) {
	db := setupSeedTestDB(t)

	if err := SetPartnerIncomeDay(db, 1, 10); err != nil {
		t.Fatalf("SetPartnerIncomeDay: %v", err)
	}
	day, err := GetPartnerIncomeDay(db, 1)
	if err != nil {
		t.Fatalf("GetPartnerIncomeDay: %v", err)
	}
	if day != 10 {
		t.Errorf("got %d, want 10", day)
	}
}

func TestGetIncomeDay_Corrupted(t *testing.T) {
	db := setupSeedTestDB(t)

	for _, raw := range []string{"not-a-number", "0", "32", "-1"} {
		if _, err := db.Exec(
			`INSERT INTO user_preferences (user_id, key, value) VALUES (1, ?, ?)
			 ON CONFLICT(user_id, key) DO UPDATE SET value = excluded.value`,
			incomeDayKey, raw,
		); err != nil {
			t.Fatalf("insert preference %s: %v", raw, err)
		}
		day, err := GetIncomeDay(db, 1)
		if err != nil {
			t.Fatalf("GetIncomeDay with value %s: %v", raw, err)
		}
		if day != defaultIncomeDay {
			t.Errorf("value %s: got %d, want default %d", raw, day, defaultIncomeDay)
		}
	}
}
