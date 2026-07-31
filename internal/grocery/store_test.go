package grocery

import (
	"context"
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

// contents returns the content of every item on a household's list, in list order.
func contents(t *testing.T, d *sql.DB, householdID int64) []string {
	t.Helper()
	items, err := ListByHousehold(d, householdID)
	if err != nil {
		t.Fatalf("ListByHousehold: %v", err)
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.Content)
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestAddItemsAllNew(t *testing.T) {
	d := setupTestDB(t)

	created, err := AddItems(context.Background(), d, 1, 1, []string{"Milk", "Eggs", "Bread"})
	if err != nil {
		t.Fatalf("AddItems: %v", err)
	}
	if len(created) != 3 {
		t.Fatalf("created %d items, want 3", len(created))
	}
	for i, item := range created {
		if item.ID == 0 {
			t.Errorf("item %d has zero ID", i)
		}
		if item.SortOrder != i {
			t.Errorf("item %d has sort_order %d, want %d", i, item.SortOrder, i)
		}
		if item.CreatedAt.IsZero() {
			t.Errorf("item %d has zero created_at", i)
		}
		if item.OriginalText != item.Content {
			t.Errorf("item %d original_text %q, want %q", i, item.OriginalText, item.Content)
		}
	}

	want := []string{"Milk", "Eggs", "Bread"}
	if got := contents(t, d, 1); !equalStrings(got, want) {
		t.Errorf("list is %v, want %v", got, want)
	}
}

func TestAddItemsMixedNewAndDuplicate(t *testing.T) {
	d := setupTestDB(t)

	seeded, err := Add(d, GroceryItem{HouseholdID: 1, Content: "Milk", OriginalText: "Melk", SourceLanguage: "nb", AddedBy: 1})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	created, err := AddItems(context.Background(), d, 1, 1, []string{"Milk", "Eggs", "Bread"})
	if err != nil {
		t.Fatalf("AddItems: %v", err)
	}
	if len(created) != 2 {
		t.Fatalf("created %d items, want 2", len(created))
	}

	want := []string{"Milk", "Eggs", "Bread"}
	if got := contents(t, d, 1); !equalStrings(got, want) {
		t.Errorf("list is %v, want %v", got, want)
	}

	// The pre-existing item must be left exactly as it was.
	items, err := ListByHousehold(d, 1)
	if err != nil {
		t.Fatalf("ListByHousehold: %v", err)
	}
	if items[0].ID != seeded.ID {
		t.Errorf("existing item ID changed: got %d, want %d", items[0].ID, seeded.ID)
	}
	if items[0].OriginalText != "Melk" {
		t.Errorf("existing original_text is %q, want %q", items[0].OriginalText, "Melk")
	}
	if !items[0].CreatedAt.Equal(seeded.CreatedAt) {
		t.Errorf("existing created_at changed: got %v, want %v", items[0].CreatedAt, seeded.CreatedAt)
	}
}

func TestAddItemsCaseAndWhitespaceInsensitive(t *testing.T) {
	d := setupTestDB(t)

	if _, err := Add(d, GroceryItem{HouseholdID: 1, Content: "Milk", OriginalText: "Milk", AddedBy: 1}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	created, err := AddItems(context.Background(), d, 1, 1, []string{"  milk ", "MILK", "\tMiLk"})
	if err != nil {
		t.Fatalf("AddItems: %v", err)
	}
	if len(created) != 0 {
		t.Fatalf("created %d items, want 0", len(created))
	}
	if got := contents(t, d, 1); !equalStrings(got, []string{"Milk"}) {
		t.Errorf("list is %v, want [Milk]", got)
	}
}

func TestAddItemsDuplicatesWithinBatch(t *testing.T) {
	d := setupTestDB(t)

	created, err := AddItems(context.Background(), d, 1, 1, []string{"Eggs", "eggs", " EGGS ", "  ", ""})
	if err != nil {
		t.Fatalf("AddItems: %v", err)
	}
	if len(created) != 1 {
		t.Fatalf("created %d items, want 1", len(created))
	}
	if created[0].Content != "Eggs" {
		t.Errorf("kept %q, want the first spelling %q", created[0].Content, "Eggs")
	}
	if got := contents(t, d, 1); !equalStrings(got, []string{"Eggs"}) {
		t.Errorf("list is %v, want [Eggs]", got)
	}
}

func TestAddItemsEmptySlice(t *testing.T) {
	d := setupTestDB(t)

	if _, err := Add(d, GroceryItem{HouseholdID: 1, Content: "Milk", OriginalText: "Milk", AddedBy: 1}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	for _, names := range [][]string{nil, {}} {
		created, err := AddItems(context.Background(), d, 1, 1, names)
		if err != nil {
			t.Fatalf("AddItems(%v): %v", names, err)
		}
		if len(created) != 0 {
			t.Errorf("AddItems(%v) created %d items, want 0", names, len(created))
		}
	}

	if got := contents(t, d, 1); !equalStrings(got, []string{"Milk"}) {
		t.Errorf("list is %v, want [Milk]", got)
	}
}

func TestAddItemsNamesAreTrimmedOnInsert(t *testing.T) {
	d := setupTestDB(t)

	created, err := AddItems(context.Background(), d, 1, 1, []string{"  Sour cream  "})
	if err != nil {
		t.Fatalf("AddItems: %v", err)
	}
	if len(created) != 1 || created[0].Content != "Sour cream" {
		t.Fatalf("created %v, want one item with content %q", created, "Sour cream")
	}
	if got := contents(t, d, 1); !equalStrings(got, []string{"Sour cream"}) {
		t.Errorf("list is %v, want [Sour cream]", got)
	}
}

func TestAddItemsHouseholdIsolation(t *testing.T) {
	d := setupTestDB(t)

	if _, err := d.Exec(`INSERT INTO users (id, email, name, picture, google_id, created_at) VALUES (2, 'other@example.com', 'Other', '', 'g2', '')`); err != nil {
		t.Fatalf("insert user 2: %v", err)
	}
	if _, err := Add(d, GroceryItem{HouseholdID: 2, Content: "Milk", OriginalText: "Milk", AddedBy: 2}); err != nil {
		t.Fatalf("Add household 2: %v", err)
	}

	// Another household's identically named item must not suppress the insert.
	created, err := AddItems(context.Background(), d, 1, 1, []string{"Milk"})
	if err != nil {
		t.Fatalf("AddItems: %v", err)
	}
	if len(created) != 1 {
		t.Fatalf("created %d items, want 1", len(created))
	}
	if got := contents(t, d, 1); !equalStrings(got, []string{"Milk"}) {
		t.Errorf("household 1 list is %v, want [Milk]", got)
	}
	if got := contents(t, d, 2); !equalStrings(got, []string{"Milk"}) {
		t.Errorf("household 2 list is %v, want [Milk]", got)
	}
}

func TestAddItemsContinuesSortOrderFromAdd(t *testing.T) {
	d := setupTestDB(t)

	first, err := Add(d, GroceryItem{HouseholdID: 1, Content: "Milk", OriginalText: "Milk", AddedBy: 1})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	created, err := AddItems(context.Background(), d, 1, 1, []string{"Eggs", "Bread"})
	if err != nil {
		t.Fatalf("AddItems: %v", err)
	}
	if len(created) != 2 {
		t.Fatalf("created %d items, want 2", len(created))
	}
	if created[0].SortOrder != first.SortOrder+1 || created[1].SortOrder != first.SortOrder+2 {
		t.Errorf("sort orders %d, %d after %d — want consecutive", created[0].SortOrder, created[1].SortOrder, first.SortOrder)
	}

	// Content must survive the encryption round-trip just like Add's does.
	items, err := ListByHousehold(d, 1)
	if err != nil {
		t.Fatalf("ListByHousehold: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("got %d items, want 3", len(items))
	}
	for _, item := range items {
		if item.Checked {
			t.Errorf("item %q should not be checked", item.Content)
		}
		if item.CreatedAt.IsZero() {
			t.Errorf("item %q has zero created_at", item.Content)
		}
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
