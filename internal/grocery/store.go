package grocery

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/encryption"
)

// execer is satisfied by both *sql.DB and *sql.Tx so the single-item insert can
// run standalone or as one step of a bulk transaction.
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// ListByHousehold returns all grocery items for the given household, ordered by checked, then sort_order, then created_at.
func ListByHousehold(db *sql.DB, householdID int64) ([]GroceryItem, error) {
	rows, err := db.Query(`
		SELECT id, household_id, content, original_text, source_language, checked, sort_order, added_by, created_at
		FROM grocery_items
		WHERE household_id = ?
		ORDER BY checked ASC, sort_order ASC, created_at ASC
	`, householdID)
	if err != nil {
		return nil, fmt.Errorf("query grocery items: %w", err)
	}
	defer rows.Close()

	var items []GroceryItem
	for rows.Next() {
		var item GroceryItem
		var createdAt string
		var checked int
		if err := rows.Scan(
			&item.ID, &item.HouseholdID, &item.Content, &item.OriginalText,
			&item.SourceLanguage, &checked, &item.SortOrder, &item.AddedBy, &createdAt,
		); err != nil {
			return nil, fmt.Errorf("scan grocery item: %w", err)
		}
		item.Checked = checked != 0

		if item.Content, err = encryption.DecryptField(item.Content); err != nil {
			return nil, fmt.Errorf("decrypt grocery content: %w", err)
		}
		if item.OriginalText, err = encryption.DecryptField(item.OriginalText); err != nil {
			return nil, fmt.Errorf("decrypt grocery original_text: %w", err)
		}

		parsed, err := time.Parse(time.RFC3339, createdAt)
		if err != nil {
			return nil, fmt.Errorf("parse created_at for item %d: %w", item.ID, err)
		}
		item.CreatedAt = parsed
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if items == nil {
		items = []GroceryItem{}
	}
	return items, nil
}

// Add inserts a new grocery item and returns it with its generated ID.
func Add(db *sql.DB, item GroceryItem) (GroceryItem, error) {
	now := time.Now().UTC().Format(time.RFC3339)

	// Default sort_order to the next value for this household.
	var maxOrder sql.NullInt64
	if err := db.QueryRow("SELECT MAX(sort_order) FROM grocery_items WHERE household_id = ?", item.HouseholdID).Scan(&maxOrder); err != nil {
		return GroceryItem{}, fmt.Errorf("select max sort_order: %w", err)
	}
	if maxOrder.Valid {
		item.SortOrder = int(maxOrder.Int64) + 1
	}

	return insertItem(context.Background(), db, item, now)
}

// AddItems bulk-inserts grocery items for a household from a list of names,
// skipping any name that already appears on the list and any repeat within the
// batch. Matching is case- and whitespace-insensitive.
//
// Item content is encrypted at rest, so de-duplication cannot be pushed into
// SQL: the household's current list is loaded and decrypted, and comparison
// happens in Go on the trimmed, lowercased name. Names are stored trimmed, and
// both content and original_text are set to the name — callers pushing already
// normalized text (e.g. recipe ingredients) have nothing to translate back.
//
// The inserts run in a single transaction and the created items are returned in
// input order, so callers can publish one event per added item.
func AddItems(ctx context.Context, db *sql.DB, householdID, addedBy int64, names []string) ([]GroceryItem, error) {
	if len(names) == 0 {
		return []GroceryItem{}, nil
	}

	existing, err := ListByHousehold(db, householdID)
	if err != nil {
		return nil, fmt.Errorf("load existing items: %w", err)
	}
	seen := make(map[string]struct{}, len(existing)+len(names))
	for _, item := range existing {
		seen[dedupeKey(item.Content)] = struct{}{}
	}

	toInsert := make([]string, 0, len(names))
	for _, name := range names {
		trimmed := strings.TrimSpace(name)
		key := dedupeKey(trimmed)
		if key == "" {
			continue
		}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		toInsert = append(toInsert, trimmed)
	}
	if len(toInsert) == 0 {
		return []GroceryItem{}, nil
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin add grocery items: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Sort order continues from the end of the list, matching what repeated
	// Add calls would produce.
	var maxOrder sql.NullInt64
	if err := tx.QueryRowContext(ctx, "SELECT MAX(sort_order) FROM grocery_items WHERE household_id = ?", householdID).Scan(&maxOrder); err != nil {
		return nil, fmt.Errorf("select max sort_order: %w", err)
	}
	sortOrder := 0
	if maxOrder.Valid {
		sortOrder = int(maxOrder.Int64) + 1
	}

	now := time.Now().UTC().Format(time.RFC3339)
	created := make([]GroceryItem, 0, len(toInsert))
	for _, name := range toInsert {
		item, err := insertItem(ctx, tx, GroceryItem{
			HouseholdID:  householdID,
			Content:      name,
			OriginalText: name,
			SortOrder:    sortOrder,
			AddedBy:      addedBy,
		}, now)
		if err != nil {
			return nil, err
		}
		created = append(created, item)
		sortOrder++
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit add grocery items: %w", err)
	}
	return created, nil
}

// dedupeKey normalizes an item name for duplicate comparison.
func dedupeKey(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// insertItem writes one grocery item, encrypting the free-form fields. The
// caller owns sort_order and the created_at timestamp so a bulk insert can
// share one clock reading and number its rows consecutively.
func insertItem(ctx context.Context, ex execer, item GroceryItem, now string) (GroceryItem, error) {
	encContent, err := encryption.EncryptField(item.Content)
	if err != nil {
		return GroceryItem{}, fmt.Errorf("encrypt content: %w", err)
	}
	encOriginalText, err := encryption.EncryptField(item.OriginalText)
	if err != nil {
		return GroceryItem{}, fmt.Errorf("encrypt original_text: %w", err)
	}

	res, err := ex.ExecContext(ctx, `
		INSERT INTO grocery_items (household_id, content, original_text, source_language, checked, sort_order, added_by, created_at)
		VALUES (?, ?, ?, ?, 0, ?, ?, ?)
	`, item.HouseholdID, encContent, encOriginalText, item.SourceLanguage, item.SortOrder, item.AddedBy, now)
	if err != nil {
		return GroceryItem{}, fmt.Errorf("insert grocery item: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return GroceryItem{}, fmt.Errorf("last insert id: %w", err)
	}

	item.ID = id
	item.Checked = false
	item.CreatedAt, _ = time.Parse(time.RFC3339, now)
	return item, nil
}

// UpdateChecked sets the checked flag for an item, scoped to the given household.
func UpdateChecked(db *sql.DB, id int64, householdID int64, checked bool) error {
	val := 0
	if checked {
		val = 1
	}
	res, err := db.Exec("UPDATE grocery_items SET checked = ? WHERE id = ? AND household_id = ?", val, id, householdID)
	if err != nil {
		return fmt.Errorf("update checked: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// UpdateSortOrder sets the sort_order for an item, scoped to the given household.
func UpdateSortOrder(db *sql.DB, id int64, householdID int64, order int) error {
	res, err := db.Exec("UPDATE grocery_items SET sort_order = ? WHERE id = ? AND household_id = ?", order, id, householdID)
	if err != nil {
		return fmt.Errorf("update sort_order: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// CompletedIDs returns the IDs of all checked items for the given household.
// Used to tell SSE subscribers exactly which items a clear-completed removed.
func CompletedIDs(db *sql.DB, householdID int64) ([]int64, error) {
	rows, err := db.Query("SELECT id FROM grocery_items WHERE household_id = ? AND checked = 1", householdID)
	if err != nil {
		return nil, fmt.Errorf("query completed ids: %w", err)
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan completed id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return ids, nil
}

// DeleteCompleted removes all checked items for the given household.
func DeleteCompleted(db *sql.DB, householdID int64) (int64, error) {
	res, err := db.Exec("DELETE FROM grocery_items WHERE household_id = ? AND checked = 1", householdID)
	if err != nil {
		return 0, fmt.Errorf("delete completed: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("rows affected: %w", err)
	}
	return n, nil
}
