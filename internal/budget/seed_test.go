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
