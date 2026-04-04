package creditcard

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Robin831/Hytte/internal/encryption"
)

// SyncCreditCardExpense calculates the monthly total for all non-payment
// transactions on creditCardID in the given billing period (YYYY-MM), then
// updates the linked variable bill entry so the budget reflects the actual
// card spend.
//
// The variable bill is identified by the credit_card_id column on
// budget_variable_bills. If no variable bill is linked to this card for the
// user, the function is a no-op and returns nil.
//
// DNB purchases carry a negative belop; the function negates the sum so the
// stored variable entry amount is positive (matching the convention used by
// other variable bill entries).
func SyncCreditCardExpense(db *sql.DB, userID int64, creditCardID, period string) error {
	// Find the variable bill linked to this credit card.
	var variableID int64
	// The UNIQUE index on (user_id, credit_card_id) WHERE credit_card_id <> ''
	// guarantees at most one row, but LIMIT 1 makes the intent explicit.
	err := db.QueryRow(
		`SELECT id FROM budget_variable_bills WHERE user_id = ? AND credit_card_id = ? LIMIT 1`,
		userID, creditCardID,
	).Scan(&variableID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil // no linked variable bill — nothing to do
	}
	if err != nil {
		return fmt.Errorf("find variable bill for card %q: %w", creditCardID, err)
	}

	// Compute date range for the billing period so the query can use the composite
	// index on (user_id, credit_card_id, transaksjonsdato) for an efficient range scan.
	periodStart, err := time.Parse("2006-01", period)
	if err != nil {
		return fmt.Errorf("invalid period %q: %w", period, err)
	}
	periodStartStr := periodStart.Format("2006-01-02")                     // e.g. "2026-03-01"
	periodEndStr := periodStart.AddDate(0, 1, 0).Format("2006-01-02")     // e.g. "2026-04-01"

	// Sum belop for non-innbetaling transactions in the billing period.
	// Purchases have negative belop; negate the sum to get a positive expense total.
	var total float64
	if err := db.QueryRow(
		`SELECT COALESCE(-SUM(belop), 0)
		 FROM credit_card_transactions
		 WHERE user_id = ? AND credit_card_id = ? AND is_innbetaling = 0
		   AND transaksjonsdato >= ? AND transaksjonsdato < ?`,
		userID, creditCardID, periodStartStr, periodEndStr,
	).Scan(&total); err != nil {
		return fmt.Errorf("sum transactions for card %q period %s: %w", creditCardID, period, err)
	}

	// Replace the variable bill entry for this period with a single entry
	// representing the total card spend.
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.Exec(
		`DELETE FROM budget_variable_entries WHERE variable_id = ? AND month = ?`,
		variableID, period,
	); err != nil {
		return fmt.Errorf("clear entries for variable bill %d period %s: %w", variableID, period, err)
	}

	encSubName, err := encryption.EncryptField("Card statement")
	if err != nil {
		return fmt.Errorf("encrypt sub_name: %w", err)
	}

	if _, err := tx.Exec(
		`INSERT INTO budget_variable_entries (variable_id, month, sub_name, amount) VALUES (?, ?, ?, ?)`,
		variableID, period, encSubName, total,
	); err != nil {
		return fmt.Errorf("insert variable entry for variable bill %d period %s: %w", variableID, period, err)
	}

	return tx.Commit()
}
