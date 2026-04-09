package grocery

import (
	"database/sql"
	"testing"

	"github.com/Robin831/Hytte/internal/db"
	"github.com/Robin831/Hytte/internal/encryption"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	t.Setenv("ENCRYPTION_KEY", "test-key-for-grocery-tests")
	encryption.ResetEncryptionKey()
	t.Cleanup(func() { encryption.ResetEncryptionKey() })
	database, err := db.Init(":memory:")
	if err != nil {
		t.Fatalf("init test db: %v", err)
	}
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	t.Cleanup(func() { database.Close() })

	// Create a test user.
	_, err = database.Exec(`INSERT INTO users (id, email, name, picture, google_id, created_at) VALUES (1, 'test@example.com', 'Test', '', 'g1', '')`)
	if err != nil {
		t.Fatalf("insert test user: %v", err)
	}
	return database
}

func TestAddAndList(t *testing.T) {
	d := setupTestDB(t)

	item := GroceryItem{
		HouseholdID:    1,
		Content:        "Milk",
		OriginalText:   "Melk",
		SourceLanguage: "nb",
		AddedBy:        1,
	}

	created, err := Add(d, item)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if created.ID == 0 {
		t.Fatal("expected non-zero ID")
	}
	if created.Content != "Milk" {
		t.Errorf("got content %q, want %q", created.Content, "Milk")
	}
	if created.Checked {
		t.Error("new item should not be checked")
	}

	items, err := ListByHousehold(d, 1)
	if err != nil {
		t.Fatalf("ListByHousehold: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	if items[0].Content != "Milk" {
		t.Errorf("got content %q, want %q", items[0].Content, "Milk")
	}
	if items[0].OriginalText != "Melk" {
		t.Errorf("got original_text %q, want %q", items[0].OriginalText, "Melk")
	}
}

func TestUpdateChecked(t *testing.T) {
	d := setupTestDB(t)

	created, err := Add(d, GroceryItem{HouseholdID: 1, Content: "Eggs", OriginalText: "Eggs", AddedBy: 1})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	if err := UpdateChecked(d, created.ID, 1, true); err != nil {
		t.Fatalf("UpdateChecked: %v", err)
	}

	items, err := ListByHousehold(d, 1)
	if err != nil {
		t.Fatalf("ListByHousehold: %v", err)
	}
	if !items[0].Checked {
		t.Error("expected item to be checked")
	}

	// Wrong household should get ErrNoRows.
	if err := UpdateChecked(d, created.ID, 999, false); err != sql.ErrNoRows {
		t.Errorf("expected ErrNoRows for wrong household, got %v", err)
	}
}

func TestUpdateSortOrder(t *testing.T) {
	d := setupTestDB(t)

	created, err := Add(d, GroceryItem{HouseholdID: 1, Content: "Bread", OriginalText: "Bread", AddedBy: 1})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	if err := UpdateSortOrder(d, created.ID, 1, 42); err != nil {
		t.Fatalf("UpdateSortOrder: %v", err)
	}

	items, err := ListByHousehold(d, 1)
	if err != nil {
		t.Fatalf("ListByHousehold: %v", err)
	}
	if items[0].SortOrder != 42 {
		t.Errorf("got sort_order %d, want 42", items[0].SortOrder)
	}
}

func TestDeleteCompleted(t *testing.T) {
	d := setupTestDB(t)

	_, err := Add(d, GroceryItem{HouseholdID: 1, Content: "Milk", OriginalText: "Milk", AddedBy: 1})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	item2, err := Add(d, GroceryItem{HouseholdID: 1, Content: "Eggs", OriginalText: "Eggs", AddedBy: 1})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Check one item.
	if err := UpdateChecked(d, item2.ID, 1, true); err != nil {
		t.Fatalf("UpdateChecked: %v", err)
	}

	deleted, err := DeleteCompleted(d, 1)
	if err != nil {
		t.Fatalf("DeleteCompleted: %v", err)
	}
	if deleted != 1 {
		t.Errorf("deleted %d, want 1", deleted)
	}

	items, err := ListByHousehold(d, 1)
	if err != nil {
		t.Fatalf("ListByHousehold: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	if items[0].Content != "Milk" {
		t.Errorf("remaining item should be Milk, got %q", items[0].Content)
	}
}

func TestHouseholdScoping(t *testing.T) {
	d := setupTestDB(t)

	// Create a second user.
	_, err := d.Exec(`INSERT INTO users (id, email, name, picture, google_id, created_at) VALUES (2, 'other@example.com', 'Other', '', 'g2', '')`)
	if err != nil {
		t.Fatalf("insert user 2: %v", err)
	}

	_, err = Add(d, GroceryItem{HouseholdID: 1, Content: "Milk", OriginalText: "Milk", AddedBy: 1})
	if err != nil {
		t.Fatalf("Add household 1: %v", err)
	}
	_, err = Add(d, GroceryItem{HouseholdID: 2, Content: "Bread", OriginalText: "Bread", AddedBy: 2})
	if err != nil {
		t.Fatalf("Add household 2: %v", err)
	}

	items1, err := ListByHousehold(d, 1)
	if err != nil {
		t.Fatalf("ListByHousehold 1: %v", err)
	}
	if len(items1) != 1 {
		t.Errorf("household 1 has %d items, want 1", len(items1))
	}

	items2, err := ListByHousehold(d, 2)
	if err != nil {
		t.Fatalf("ListByHousehold 2: %v", err)
	}
	if len(items2) != 1 {
		t.Errorf("household 2 has %d items, want 1", len(items2))
	}
}
