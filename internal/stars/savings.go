package stars

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"math"
	"time"
)

// SavingsAccount holds the current state of a user's star savings account.
type SavingsAccount struct {
	Balance               int    `json:"balance"`
	PendingWithdrawal     int    `json:"pending_withdrawal"`
	WithdrawalAvailableAt string `json:"withdrawal_available_at,omitempty"`
}

// GetSavingsAccount returns the current savings state for a user.
// Returns an empty account (all zeros) if no savings row exists yet.
func GetSavingsAccount(ctx context.Context, db *sql.DB, userID int64) (*SavingsAccount, error) {
	var acc SavingsAccount
	err := db.QueryRowContext(ctx, `
		SELECT balance, pending_withdrawal, COALESCE(withdrawal_available_at, '')
		FROM star_savings WHERE user_id = ?
	`, userID).Scan(&acc.Balance, &acc.PendingWithdrawal, &acc.WithdrawalAvailableAt)
	if err == sql.ErrNoRows {
		return &SavingsAccount{}, nil
	}
	if err != nil {
		return nil, err
	}
	return &acc, nil
}

// Deposit moves amount stars from the user's main balance into savings.
// Records a negative star_transaction (reason='savings_deposit') and
// credits star_savings.balance atomically.
func Deposit(ctx context.Context, db *sql.DB, userID int64, amount int) (*SavingsAccount, error) {
	if amount <= 0 {
		return nil, fmt.Errorf("deposit amount must be positive")
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// Check available balance.
	var currentBalance int
	err = tx.QueryRowContext(ctx, `
		SELECT COALESCE(current_balance, 0) FROM star_balances WHERE user_id = ?
	`, userID).Scan(&currentBalance)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("load balance: %w", err)
	}

	if currentBalance < amount {
		return nil, fmt.Errorf("insufficient balance: have %d, need %d", currentBalance, amount)
	}

	now := time.Now().UTC().Format(time.RFC3339)

	// Record a negative transaction in the main ledger.
	_, err = tx.ExecContext(ctx, `
		INSERT INTO star_transactions (user_id, amount, reason, description, created_at)
		VALUES (?, ?, 'savings_deposit', 'Deposited to Star Bank', ?)
	`, userID, -amount, now)
	if err != nil {
		return nil, fmt.Errorf("insert transaction: %w", err)
	}

	// Debit star_balances (increase total_spent reduces current_balance).
	_, err = tx.ExecContext(ctx, `
		INSERT INTO star_balances (user_id, total_spent)
		VALUES (?, ?)
		ON CONFLICT(user_id) DO UPDATE SET total_spent = total_spent + excluded.total_spent
	`, userID, amount)
	if err != nil {
		return nil, fmt.Errorf("update balance: %w", err)
	}

	// Credit savings balance.
	_, err = tx.ExecContext(ctx, `
		INSERT INTO star_savings (user_id, balance, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET
			balance = balance + excluded.balance,
			updated_at = excluded.updated_at
	`, userID, amount, now)
	if err != nil {
		return nil, fmt.Errorf("update savings: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return GetSavingsAccount(ctx, db, userID)
}

// RequestWithdrawal sets a pending withdrawal of amount stars from savings.
// The withdrawal becomes available after 24 hours.
// Returns an error if a withdrawal is already pending or savings are insufficient.
func RequestWithdrawal(ctx context.Context, db *sql.DB, userID int64, amount int) (*SavingsAccount, error) {
	if amount <= 0 {
		return nil, fmt.Errorf("withdrawal amount must be positive")
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var savingsBalance, pendingWithdrawal int
	err = tx.QueryRowContext(ctx, `
		SELECT balance, pending_withdrawal FROM star_savings WHERE user_id = ?
	`, userID).Scan(&savingsBalance, &pendingWithdrawal)
	if err == sql.ErrNoRows {
		savingsBalance = 0
		pendingWithdrawal = 0
	} else if err != nil {
		return nil, fmt.Errorf("load savings: %w", err)
	}

	if pendingWithdrawal > 0 {
		return nil, fmt.Errorf("withdrawal already pending")
	}

	if savingsBalance < amount {
		return nil, fmt.Errorf("insufficient savings: have %d, need %d", savingsBalance, amount)
	}

	now := time.Now().UTC()
	availableAt := now.Add(24 * time.Hour).Format(time.RFC3339)

	_, err = tx.ExecContext(ctx, `
		INSERT INTO star_savings (user_id, balance, pending_withdrawal, withdrawal_available_at, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET
			pending_withdrawal = excluded.pending_withdrawal,
			withdrawal_available_at = excluded.withdrawal_available_at,
			updated_at = excluded.updated_at
	`, userID, savingsBalance, amount, availableAt, now.Format(time.RFC3339))
	if err != nil {
		return nil, fmt.Errorf("set pending withdrawal: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return GetSavingsAccount(ctx, db, userID)
}

// CompleteWithdrawal completes a pending withdrawal if the 24h delay has passed.
// Stars are moved back to the main balance via a positive star_transaction
// (reason='savings_withdrawal').
func CompleteWithdrawal(ctx context.Context, db *sql.DB, userID int64) (*SavingsAccount, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var savingsBalance, pendingWithdrawal int
	var withdrawalAvailableAt string
	err = tx.QueryRowContext(ctx, `
		SELECT balance, pending_withdrawal, COALESCE(withdrawal_available_at, '')
		FROM star_savings WHERE user_id = ?
	`, userID).Scan(&savingsBalance, &pendingWithdrawal, &withdrawalAvailableAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("no savings account")
	} else if err != nil {
		return nil, fmt.Errorf("load savings: %w", err)
	}

	if pendingWithdrawal <= 0 {
		return nil, fmt.Errorf("no pending withdrawal")
	}

	available, err := time.Parse(time.RFC3339, withdrawalAvailableAt)
	if err != nil {
		return nil, fmt.Errorf("parse withdrawal timestamp: %w", err)
	}

	if time.Now().UTC().Before(available) {
		return nil, fmt.Errorf("withdrawal not yet available")
	}

	now := time.Now().UTC().Format(time.RFC3339)

	// Record positive transaction in main ledger.
	_, err = tx.ExecContext(ctx, `
		INSERT INTO star_transactions (user_id, amount, reason, description, created_at)
		VALUES (?, ?, 'savings_withdrawal', 'Withdrawn from Star Bank', ?)
	`, userID, pendingWithdrawal, now)
	if err != nil {
		return nil, fmt.Errorf("insert transaction: %w", err)
	}

	// Credit star_balances.total_earned.
	_, err = tx.ExecContext(ctx, `
		INSERT INTO star_balances (user_id, total_earned)
		VALUES (?, ?)
		ON CONFLICT(user_id) DO UPDATE SET total_earned = total_earned + excluded.total_earned
	`, userID, pendingWithdrawal)
	if err != nil {
		return nil, fmt.Errorf("update balance: %w", err)
	}

	// Debit savings balance and clear the pending withdrawal.
	_, err = tx.ExecContext(ctx, `
		UPDATE star_savings
		SET balance = balance - ?,
		    pending_withdrawal = 0,
		    withdrawal_available_at = '',
		    updated_at = ?
		WHERE user_id = ?
	`, pendingWithdrawal, now, userID)
	if err != nil {
		return nil, fmt.Errorf("clear pending withdrawal: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return GetSavingsAccount(ctx, db, userID)
}

// PayInterest awards floor(balance * 0.10) stars to every user with a positive
// savings balance. Interest is compounded: the same amount is added to both
// the main star ledger (reason='savings_interest') and star_savings.balance so
// future interest compounds on the larger balance.
// Intended to run once per week (Sundays). Callers are responsible for dedup.
func PayInterest(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, `
		SELECT user_id, balance FROM star_savings WHERE balance > 0
	`)
	if err != nil {
		return fmt.Errorf("query savings: %w", err)
	}
	defer rows.Close()

	type entry struct {
		userID  int64
		balance int
	}
	var entries []entry
	for rows.Next() {
		var e entry
		if err := rows.Scan(&e.userID, &e.balance); err != nil {
			return fmt.Errorf("scan savings: %w", err)
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	for _, e := range entries {
		interest := int(math.Floor(float64(e.balance) * 0.10))
		if interest <= 0 {
			continue
		}
		if err := payInterestForUser(ctx, db, e.userID, interest, now); err != nil {
			// Log and continue so remaining users still receive interest.
			log.Printf("savings: interest payment failed for user %d: %v", e.userID, err)
		}
	}
	return nil
}

func payInterestForUser(ctx context.Context, db *sql.DB, userID int64, interest int, now string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Record positive transaction in main ledger.
	_, err = tx.ExecContext(ctx, `
		INSERT INTO star_transactions (user_id, amount, reason, description, created_at)
		VALUES (?, ?, 'savings_interest', 'Weekly savings interest', ?)
	`, userID, interest, now)
	if err != nil {
		return fmt.Errorf("insert interest transaction: %w", err)
	}

	// Credit star_balances.total_earned.
	_, err = tx.ExecContext(ctx, `
		INSERT INTO star_balances (user_id, total_earned)
		VALUES (?, ?)
		ON CONFLICT(user_id) DO UPDATE SET total_earned = total_earned + excluded.total_earned
	`, userID, interest)
	if err != nil {
		return fmt.Errorf("update balance: %w", err)
	}

	// Compound: also add interest to savings balance for next week's calculation.
	_, err = tx.ExecContext(ctx, `
		UPDATE star_savings SET balance = balance + ?, updated_at = ? WHERE user_id = ?
	`, interest, now, userID)
	if err != nil {
		return fmt.Errorf("compound savings balance: %w", err)
	}

	return tx.Commit()
}
