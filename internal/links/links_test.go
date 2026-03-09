package links

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	_, err = db.Exec("PRAGMA foreign_keys=ON")
	if err != nil {
		t.Fatalf("enable fk: %v", err)
	}
	_, err = db.Exec(`
		CREATE TABLE users (
			id INTEGER PRIMARY KEY,
			email TEXT UNIQUE NOT NULL,
			name TEXT NOT NULL,
			picture TEXT NOT NULL DEFAULT '',
			google_id TEXT UNIQUE NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE short_links (
			id         INTEGER PRIMARY KEY,
			user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			code       TEXT UNIQUE NOT NULL,
			target_url TEXT NOT NULL,
			title      TEXT NOT NULL DEFAULT '',
			clicks     INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`)
	if err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	// Insert a test user.
	_, err = db.Exec("INSERT INTO users (id, email, name, google_id) VALUES (1, 'test@example.com', 'Test', 'g123')")
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	return db
}

func TestCreateAndGetByCode(t *testing.T) {
	db := setupTestDB(t)

	link, err := Create(db, 1, "abc", "https://example.com", "Example")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if link.Code != "abc" {
		t.Errorf("code = %q, want %q", link.Code, "abc")
	}
	if link.TargetURL != "https://example.com" {
		t.Errorf("target_url = %q, want %q", link.TargetURL, "https://example.com")
	}

	got, err := GetByCode(db, "abc")
	if err != nil {
		t.Fatalf("get by code: %v", err)
	}
	if got.ID != link.ID {
		t.Errorf("id = %d, want %d", got.ID, link.ID)
	}
}

func TestCreateAutoCode(t *testing.T) {
	db := setupTestDB(t)

	link, err := Create(db, 1, "", "https://example.com", "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(link.Code) != 6 {
		t.Errorf("auto code length = %d, want 6", len(link.Code))
	}
}

func TestCreateDuplicateCode(t *testing.T) {
	db := setupTestDB(t)

	_, err := Create(db, 1, "dup", "https://a.com", "")
	if err != nil {
		t.Fatalf("first create: %v", err)
	}
	_, err = Create(db, 1, "dup", "https://b.com", "")
	if err == nil {
		t.Fatal("expected error for duplicate code")
	}
}

func TestListByUser(t *testing.T) {
	db := setupTestDB(t)

	if _, err := Create(db, 1, "a1", "https://a.com", "A"); err != nil {
		t.Fatalf("create a1: %v", err)
	}
	if _, err := Create(db, 1, "b2", "https://b.com", "B"); err != nil {
		t.Fatalf("create b2: %v", err)
	}

	links, err := ListByUser(db, 1)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(links) != 2 {
		t.Fatalf("got %d links, want 2", len(links))
	}
}

func TestIncrementClicks(t *testing.T) {
	db := setupTestDB(t)

	link, _ := Create(db, 1, "clk", "https://click.com", "")
	IncrementClicks(db, link.ID)
	IncrementClicks(db, link.ID)

	got, _ := GetByCode(db, "clk")
	if got.Clicks != 2 {
		t.Errorf("clicks = %d, want 2", got.Clicks)
	}
}

func TestDelete(t *testing.T) {
	db := setupTestDB(t)

	link, _ := Create(db, 1, "del", "https://del.com", "")

	err := Delete(db, link.ID, 1)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}

	_, err = GetByCode(db, "del")
	if err != sql.ErrNoRows {
		t.Errorf("expected ErrNoRows after delete, got %v", err)
	}
}

func TestDeleteWrongUser(t *testing.T) {
	db := setupTestDB(t)

	link, _ := Create(db, 1, "own", "https://own.com", "")

	err := Delete(db, link.ID, 999)
	if err != sql.ErrNoRows {
		t.Errorf("expected ErrNoRows for wrong user, got %v", err)
	}
}

func TestUpdate(t *testing.T) {
	db := setupTestDB(t)

	link, _ := Create(db, 1, "old", "https://old.com", "Old")

	updated, err := Update(db, link.ID, 1, "new", "https://new.com", "New")
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Code != "new" {
		t.Errorf("code = %q, want %q", updated.Code, "new")
	}
	if updated.TargetURL != "https://new.com" {
		t.Errorf("target_url = %q, want %q", updated.TargetURL, "https://new.com")
	}
	if updated.Title != "New" {
		t.Errorf("title = %q, want %q", updated.Title, "New")
	}
}

func TestUpdateWrongUser(t *testing.T) {
	db := setupTestDB(t)

	link, _ := Create(db, 1, "own", "https://own.com", "Own")

	_, err := Update(db, link.ID, 999, "new", "https://new.com", "New")
	if err != sql.ErrNoRows {
		t.Errorf("expected ErrNoRows for wrong user, got %v", err)
	}
}

func TestCascadeDeleteUser(t *testing.T) {
	db := setupTestDB(t)

	Create(db, 1, "cas", "https://cascade.com", "")

	_, err := db.Exec("DELETE FROM users WHERE id = 1")
	if err != nil {
		t.Fatalf("delete user: %v", err)
	}

	links, err := ListByUser(db, 1)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(links) != 0 {
		t.Errorf("expected 0 links after user delete, got %d", len(links))
	}
}
