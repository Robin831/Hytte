package stars

import (
	"context"
	"database/sql"
	"fmt"
	"log"
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

	// Atomically debit the main balance: the UPDATE only succeeds when the
	// user has enough stars. This prevents a read-then-write race where two
	// concurrent deposits both observe sufficient funds and both commit,
	// driving the balance negative.
	res, err := tx.ExecContext(ctx, `
		UPDATE star_balances
		SET total_spent = total_spent + ?
		WHERE user_id = ? AND (total_earned - total_spent) >= ?
	`, amount, userID, amount)
	if err != nil {
		return nil, fmt.Errorf("update balance: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("update balance: %w", err)
	}
	if affected == 0 {
		// Diagnose: no row means zero balance; existing row means insufficient funds.
		var currentBalance int
		qErr := tx.QueryRowContext(ctx, `
			SELECT COALESCE(total_earned - total_spent, 0) FROM star_balances WHERE user_id = ?
		`, userID).Scan(&currentBalance)
		if qErr != nil && qErr != sql.ErrNoRows {
			return nil, fmt.Errorf("load balance: %w", qErr)
		}
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

	now := time.Now().UTC()
	availableAt := now.Add(24 * time.Hour).Format(time.RFC3339)

	// Atomically set a pending withdrawal only if there is no existing pending
	// withdrawal and the balance is sufficient. This prevents write-skew where
	// two concurrent requests both read pending_withdrawal=0 and both commit.
	res, err := tx.ExecContext(ctx, `
		UPDATE star_savings
		SET pending_withdrawal = ?, withdrawal_available_at = ?, updated_at = ?
		WHERE user_id = ? AND pending_withdrawal = 0 AND balance >= ?
	`, amount, availableAt, now.Format(time.RFC3339), userID, amount)
	if err != nil {
		return nil, fmt.Errorf("set pending withdrawal: %w", err)
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("set pending withdrawal: %w", err)
	}

	if rowsAffected == 0 {
		// Diagnose why the update failed to provide a useful error message,
		// without affecting the atomicity of the state change.
		var savingsBalance, pendingWithdrawal int
		err = tx.QueryRowContext(ctx, `
			SELECT balance, pending_withdrawal FROM star_savings WHERE user_id = ?
		`, userID).Scan(&savingsBalance, &pendingWithdrawal)
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("insufficient savings: have %d, need %d", 0, amount)
		} else if err != nil {
			return nil, fmt.Errorf("load savings: %w", err)
		}

		if pendingWithdrawal > 0 {
			return nil, fmt.Errorf("withdrawal already pending")
		}

		return nil, fmt.Errorf("insufficient savings: have %d, need %d", savingsBalance, amount)
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

	// Reverse earlier savings deposit by reducing total_spent (not increasing total_earned,
	// which would inflate lifetime leaderboard stats for what is a balance transfer back).
	_, err = tx.ExecContext(ctx, `
		INSERT INTO star_balances (user_id, total_spent)
		VALUES (?, ?)
		ON CONFLICT(user_id) DO UPDATE SET total_spent = total_spent - excluded.total_spent
	`, userID, pendingWithdrawal)
	if err != nil {
		return nil, fmt.Errorf("update balance: %w", err)
	}

	// Debit savings balance and clear the pending withdrawal. The WHERE guard on
	// pending_withdrawal and withdrawal_available_at ensures only one concurrent
	// completion can succeed — a second racing transaction will find 0 rows affected.
	res, err := tx.ExecContext(ctx, `
		UPDATE star_savings
		SET balance = balance - ?,
		    pending_withdrawal = 0,
		    withdrawal_available_at = '',
		    updated_at = ?
		WHERE user_id = ?
		  AND pending_withdrawal = ?
		  AND withdrawal_available_at = ?
	`, pendingWithdrawal, now, userID, pendingWithdrawal, withdrawalAvailableAt)
	if err != nil {
		return nil, fmt.Errorf("clear pending withdrawal: %w", err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("clear pending withdrawal: %w", err)
	}
	if rows == 0 {
		// Another transaction may have already completed this withdrawal.
		return nil, fmt.Errorf("no pending withdrawal")
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return GetSavingsAccount(ctx, db, userID)
}

// PayInterest awards balance/10 stars to every user with a positive savings balance.
// Interest lives only in savings: total_earned and total_spent are both incremented
// so current_balance does not change (interest is not spendable until withdrawn).
// The savings balance grows for compounding in future weeks.
// Intended to run once per week (Sundays). The now parameter determines the ISO week
// key used for dedup — repeated calls with the same week are no-ops.
func PayInterest(ctx context.Context, db *sql.DB, now time.Time) error {
	key := weekKey(now)

	// Claim this week's interest run atomically. If another server instance (or a
	// restart mid-run) already started this week's payment, the INSERT will fail the
	// UNIQUE constraint and we skip entirely, preventing double-pay.
	res, err := db.ExecContext(ctx, `
		INSERT INTO savings_interest_payments (week_key, paid_at)
		VALUES (?, ?)
		ON CONFLICT(week_key) DO NOTHING
	`, key, now.UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("savings interest dedup insert: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("savings interest dedup rows: %w", err)
	}
	if affected == 0 {
		log.Printf("savings: interest already paid for week %s, skipping", key)
		return nil
	}

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

	nowStr := now.UTC().Format(time.RFC3339)
	for _, e := range entries {
		interest := e.balance / 10 // 10% rate, integer division floors
		if interest <= 0 {
			continue
		}
		if err := payInterestForUser(ctx, db, e.userID, interest, nowStr); err != nil {
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

	// Credit total_earned for accounting, but immediately offset with total_spent so
	// the interest is not available in current_balance until the user withdraws it.
	// This avoids double-counting (available balance + still in savings).
	_, err = tx.ExecContext(ctx, `
		INSERT INTO star_balances (user_id, total_earned, total_spent)
		VALUES (?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET
			total_earned = total_earned + excluded.total_earned,
			total_spent  = total_spent  + excluded.total_spent
	`, userID, interest, interest)
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
