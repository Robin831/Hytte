package grocery

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/Robin831/Hytte/internal/encryption"
)

// ListByUser returns all grocery items for the given user, ordered by sort_order then created_at.
func ListByUser(db *sql.DB, userID int64) ([]GroceryItem, error) {
	rows, err := db.Query(`
		SELECT id, user_id, content, original_text, source_language, checked, sort_order, added_by, created_at
		FROM grocery_items
		WHERE user_id = ?
		ORDER BY checked ASC, sort_order ASC, created_at ASC
	`, userID)
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
			&item.ID, &item.UserID, &item.Content, &item.OriginalText,
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

		item.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
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
	encContent, err := encryption.EncryptField(item.Content)
	if err != nil {
		return GroceryItem{}, fmt.Errorf("encrypt content: %w", err)
	}
	encOriginalText, err := encryption.EncryptField(item.OriginalText)
	if err != nil {
		return GroceryItem{}, fmt.Errorf("encrypt original_text: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)

	// Default sort_order to the next value for this user.
	var maxOrder sql.NullInt64
	_ = db.QueryRow("SELECT MAX(sort_order) FROM grocery_items WHERE user_id = ?", item.UserID).Scan(&maxOrder)
	if maxOrder.Valid {
		item.SortOrder = int(maxOrder.Int64) + 1
	}

	res, err := db.Exec(`
		INSERT INTO grocery_items (user_id, content, original_text, source_language, checked, sort_order, added_by, created_at)
		VALUES (?, ?, ?, ?, 0, ?, ?, ?)
	`, item.UserID, encContent, encOriginalText, item.SourceLanguage, item.SortOrder, item.AddedBy, now)
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

// UpdateChecked sets the checked flag for an item, scoped to the given user.
func UpdateChecked(db *sql.DB, id int64, userID int64, checked bool) error {
	val := 0
	if checked {
		val = 1
	}
	res, err := db.Exec("UPDATE grocery_items SET checked = ? WHERE id = ? AND user_id = ?", val, id, userID)
	if err != nil {
		return fmt.Errorf("update checked: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// UpdateSortOrder sets the sort_order for an item, scoped to the given user.
func UpdateSortOrder(db *sql.DB, id int64, userID int64, order int) error {
	res, err := db.Exec("UPDATE grocery_items SET sort_order = ? WHERE id = ? AND user_id = ?", order, id, userID)
	if err != nil {
		return fmt.Errorf("update sort_order: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// DeleteCompleted removes all checked items for the given user.
func DeleteCompleted(db *sql.DB, userID int64) (int64, error) {
	res, err := db.Exec("DELETE FROM grocery_items WHERE user_id = ? AND checked = 1", userID)
	if err != nil {
		return 0, fmt.Errorf("delete completed: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}
