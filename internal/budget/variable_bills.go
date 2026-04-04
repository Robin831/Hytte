package budget

import (
	"database/sql"
	"fmt"
	"log"
	"regexp"

	"github.com/Robin831/Hytte/internal/encryption"
)

var reMonth = regexp.MustCompile(`^\d{4}-\d{2}$`)

// ValidateMonth returns an error if month is not in YYYY-MM format.
func ValidateMonth(month string) error {
	if !reMonth.MatchString(month) {
		return fmt.Errorf("month must be in YYYY-MM format")
	}
	return nil
}

// -- Variable Bills store --

// ListVariableBills returns all variable bills for userID, each populated with
// entries for the given month (may be empty if none recorded yet).
func ListVariableBills(db *sql.DB, userID int64, month string) ([]VariableBill, error) {
	rows, err := db.Query(
		`SELECT id, user_id, name, recurring_id FROM budget_variable_bills
		 WHERE user_id = ? ORDER BY id`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck

	var bills []VariableBill
	for rows.Next() {
		var b VariableBill
		var encName string
		if err := rows.Scan(&b.ID, &b.UserID, &encName, &b.RecurringID); err != nil {
			return nil, err
		}
		name, err := encryption.DecryptField(encName)
		if err != nil {
			log.Printf("budget: decrypt variable bill name (id=%d): %v — using plaintext fallback", b.ID, err)
			name = encName // legacy plaintext fallback
		}
		b.Name = name
		b.Entries = []VariableEntry{}
		bills = append(bills, b)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if bills == nil {
		return []VariableBill{}, nil
	}

	// Load entries for the requested month and associate with bills.
	entryRows, err := db.Query(
		`SELECT id, variable_id, month, sub_name, amount
		 FROM budget_variable_entries
		 WHERE variable_id IN (
		   SELECT id FROM budget_variable_bills WHERE user_id = ?
		 ) AND month = ?
		 ORDER BY id`,
		userID, month,
	)
	if err != nil {
		return nil, err
	}
	defer entryRows.Close() //nolint:errcheck

	// Build index from variable_id to position in bills slice.
	idx := make(map[int64]int, len(bills))
	for i, b := range bills {
		idx[b.ID] = i
	}

	for entryRows.Next() {
		var e VariableEntry
		var encSubName string
		if err := entryRows.Scan(&e.ID, &e.VariableID, &e.Month, &encSubName, &e.Amount); err != nil {
			return nil, err
		}
		subName, err := encryption.DecryptField(encSubName)
		if err != nil {
			log.Printf("budget: decrypt variable entry sub_name (id=%d): %v — using plaintext fallback", e.ID, err)
			subName = encSubName // legacy plaintext fallback
		}
		e.SubName = subName
		if i, ok := idx[e.VariableID]; ok {
			bills[i].Entries = append(bills[i].Entries, e)
		}
	}
	return bills, entryRows.Err()
}

// CreateVariableBill inserts a new variable bill and sets b.ID and b.UserID.
func CreateVariableBill(db *sql.DB, userID int64, b *VariableBill) error {
	encName, err := encryption.EncryptField(b.Name)
	if err != nil {
		return fmt.Errorf("encrypt bill name: %w", err)
	}
	res, err := db.Exec(
		`INSERT INTO budget_variable_bills (user_id, name, recurring_id) VALUES (?, ?, ?)`,
		userID, encName, b.RecurringID,
	)
	if err != nil {
		return err
	}
	b.ID, err = res.LastInsertId()
	b.UserID = userID
	if b.Entries == nil {
		b.Entries = []VariableEntry{}
	}
	return err
}

// UpdateVariableBill updates name and recurring_id for the bill identified by id
// and owned by userID. Returns sql.ErrNoRows if not found.
func UpdateVariableBill(db *sql.DB, userID, id int64, b *VariableBill) error {
	encName, err := encryption.EncryptField(b.Name)
	if err != nil {
		return fmt.Errorf("encrypt bill name: %w", err)
	}
	res, err := db.Exec(
		`UPDATE budget_variable_bills SET name = ?, recurring_id = ?
		 WHERE id = ? AND user_id = ?`,
		encName, b.RecurringID, id, userID,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// DeleteVariableBill removes a variable bill and cascades to its entries.
// Returns sql.ErrNoRows if not found.
func DeleteVariableBill(db *sql.DB, userID, id int64) error {
	res, err := db.Exec(
		`DELETE FROM budget_variable_bills WHERE id = ? AND user_id = ?`,
		id, userID,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// SetMonthEntries replaces all entries for variableID+month with the given slice.
// Ownership is verified via userID: the bill must belong to userID.
func SetMonthEntries(db *sql.DB, userID, variableID int64, month string, entries []VariableEntry) error {
	// Verify ownership.
	var dummy int
	if err := db.QueryRow(
		`SELECT 1 FROM budget_variable_bills WHERE id = ? AND user_id = ? LIMIT 1`,
		variableID, userID,
	).Scan(&dummy); err != nil {
		return err // sql.ErrNoRows propagates directly
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.Exec(
		`DELETE FROM budget_variable_entries WHERE variable_id = ? AND month = ?`,
		variableID, month,
	); err != nil {
		return err
	}

	for _, e := range entries {
		encSubName, err := encryption.EncryptField(e.SubName)
		if err != nil {
			return fmt.Errorf("encrypt sub_name: %w", err)
		}
		if _, err := tx.Exec(
			`INSERT INTO budget_variable_entries (variable_id, month, sub_name, amount) VALUES (?, ?, ?, ?)`,
			variableID, month, encSubName, e.Amount,
		); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// CopyMonthEntries copies all entries from fromMonth to toMonth for variableID.
// Ownership is verified via userID.  Existing entries for toMonth are replaced.
func CopyMonthEntries(db *sql.DB, userID, variableID int64, fromMonth, toMonth string) ([]VariableEntry, error) {
	// Verify ownership.
	var dummy int
	if err := db.QueryRow(
		`SELECT 1 FROM budget_variable_bills WHERE id = ? AND user_id = ? LIMIT 1`,
		variableID, userID,
	).Scan(&dummy); err != nil {
		return nil, err // sql.ErrNoRows propagates directly
	}

	// Load source entries (already encrypted in DB, so fetch raw values).
	rows, err := db.Query(
		`SELECT sub_name, amount FROM budget_variable_entries
		 WHERE variable_id = ? AND month = ? ORDER BY id`,
		variableID, fromMonth,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck

	type rawEntry struct {
		encSubName string
		amount     float64
	}
	var raws []rawEntry
	for rows.Next() {
		var re rawEntry
		if err := rows.Scan(&re.encSubName, &re.amount); err != nil {
			return nil, err
		}
		raws = append(raws, re)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck

	// Delete existing entries for toMonth.
	if _, err := tx.Exec(
		`DELETE FROM budget_variable_entries WHERE variable_id = ? AND month = ?`,
		variableID, toMonth,
	); err != nil {
		return nil, err
	}

	var result []VariableEntry
	for _, re := range raws {
		res, err := tx.Exec(
			`INSERT INTO budget_variable_entries (variable_id, month, sub_name, amount) VALUES (?, ?, ?, ?)`,
			variableID, toMonth, re.encSubName, re.amount,
		)
		if err != nil {
			return nil, err
		}
		newID, err := res.LastInsertId()
		if err != nil {
			return nil, err
		}
		subName, err := encryption.DecryptField(re.encSubName)
		if err != nil {
			log.Printf("budget: decrypt copied entry sub_name: %v — using plaintext fallback", err)
			subName = re.encSubName
		}
		result = append(result, VariableEntry{
			ID:         newID,
			VariableID: variableID,
			Month:      toMonth,
			SubName:    subName,
			Amount:     re.amount,
		})
	}
	if result == nil {
		result = []VariableEntry{}
	}

	return result, tx.Commit()
}
