package creditcard

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Robin831/Hytte/internal/encryption"
)

// SyncCreditCardExpense calculates the statement closing balance for creditCardID
// in the given billing period (YYYY-MM) and updates the linked variable bill entry
// so the budget reflects what is actually owed on the card.
//
// Closing balance = opening balance + expenses − innbetalinger (payments)
//
// The opening balance is the carried-over amount from the previous statement,
// stored in credit_card_opening_balances for (user_id, credit_card_id, period).
// If no opening balance has been set, it defaults to 0.
//
// The variable bill is identified by the credit_card_id column on
// budget_variable_bills. If no variable bill is linked to this card for the
// user, the function is a no-op and returns nil.
//
// DNB purchases carry a negative belop and payments carry a positive belop.
// Negating the sum of all settled transactions gives expenses minus payments,
// which is then added to the opening balance to get the closing balance.
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
	periodStartStr := periodStart.Format("2006-01-02")                 // e.g. "2026-03-01"
	periodEndStr := periodStart.AddDate(0, 1, 0).Format("2006-01-02") // e.g. "2026-04-01"

	// Look up the opening balance for this period (defaults to 0 if not set).
	var openingBalance float64
	if err := db.QueryRow(
		`SELECT balance FROM credit_card_opening_balances WHERE user_id = ? AND credit_card_id = ? AND month = ?`,
		userID, creditCardID, period,
	).Scan(&openingBalance); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("look up opening balance for card %q period %s: %w", creditCardID, period, err)
	}

	// Sum all settled transactions: purchases (negative belop) and payments
	// (positive belop / innbetalinger). Negating the sum gives expenses minus
	// payments, which represents the net change in what is owed this period.
	var settledNet float64
	if err := db.QueryRow(
		`SELECT COALESCE(-SUM(belop), 0)
		 FROM credit_card_transactions
		 WHERE user_id = ? AND credit_card_id = ?
		   AND is_pending = 0
		   AND transaksjonsdato >= ? AND transaksjonsdato < ?`,
		userID, creditCardID, periodStartStr, periodEndStr,
	).Scan(&settledNet); err != nil {
		return fmt.Errorf("sum transactions for card %q period %s: %w", creditCardID, period, err)
	}

	// Closing balance = opening + (expenses − payments) = opening + settledNet.
	total := openingBalance + settledNet

	// The variable bill entry goes into the NEXT month, because credit card
	// expenses from e.g. March are paid in April.
	paymentMonth := periodStart.AddDate(0, 1, 0).Format("2006-01")

	// Replace the variable bill entry for the payment month with a single entry
	// representing the total card closing balance from the billing period.
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.Exec(
		`DELETE FROM budget_variable_entries WHERE variable_id = ? AND month = ?`,
		variableID, paymentMonth,
	); err != nil {
		return fmt.Errorf("clear entries for variable bill %d month %s: %w", variableID, paymentMonth, err)
	}

	encSubName, err := encryption.EncryptField("Card statement")
	if err != nil {
		return fmt.Errorf("encrypt sub_name: %w", err)
	}

	if _, err := tx.Exec(
		`INSERT INTO budget_variable_entries (variable_id, month, sub_name, amount) VALUES (?, ?, ?, ?)`,
		variableID, paymentMonth, encSubName, total,
	); err != nil {
		return fmt.Errorf("insert variable entry for variable bill %d month %s: %w", variableID, paymentMonth, err)
	}

	return tx.Commit()
}
