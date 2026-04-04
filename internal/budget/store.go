package budget

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Robin831/Hytte/internal/encryption"
)

// TransactionFilter holds optional filters for ListTransactions.
type TransactionFilter struct {
	AccountID  *int64
	CategoryID *int64
	FromDate   string // YYYY-MM-DD, inclusive; empty means no lower bound
	ToDate     string // YYYY-MM-DD, inclusive; empty means no upper bound
}

// scanner is satisfied by both *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

// -- Account CRUD --

// CreateAccount inserts a new account for the given user and sets a.ID.
func CreateAccount(db *sql.DB, userID int64, a *Account) error {
	encName, err := encryption.EncryptField(a.Name)
	if err != nil {
		return fmt.Errorf("encrypt account name: %w", err)
	}
	res, err := db.Exec(
		`INSERT INTO budget_accounts (user_id, name, type, currency, balance, icon, credit_limit)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		userID, encName, string(a.Type), a.Currency, a.Balance, a.Icon, a.CreditLimit,
	)
	if err != nil {
		return err
	}
	a.ID, err = res.LastInsertId()
	a.UserID = userID
	return err
}

// GetAccount returns a single account scoped to the given user.
func GetAccount(db *sql.DB, userID, id int64) (*Account, error) {
	row := db.QueryRow(
		`SELECT id, user_id, name, type, currency, balance, icon, credit_limit
		 FROM budget_accounts WHERE id = ? AND user_id = ?`,
		id, userID,
	)
	return scanAccount(row)
}

// ListAccounts returns all accounts for a user ordered by id.
func ListAccounts(db *sql.DB, userID int64) ([]Account, error) {
	rows, err := db.Query(
		`SELECT id, user_id, name, type, currency, balance, icon, credit_limit
		 FROM budget_accounts WHERE user_id = ? ORDER BY id`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck

	var accounts []Account
	for rows.Next() {
		a, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		accounts = append(accounts, *a)
	}
	if accounts == nil {
		accounts = []Account{}
	}
	return accounts, rows.Err()
}

// UpdateAccount replaces the mutable fields of an existing account.
func UpdateAccount(db *sql.DB, userID int64, a *Account) error {
	encName, err := encryption.EncryptField(a.Name)
	if err != nil {
		return fmt.Errorf("encrypt account name: %w", err)
	}
	res, err := db.Exec(
		`UPDATE budget_accounts SET name=?, type=?, currency=?, balance=?, icon=?, credit_limit=?
		 WHERE id=? AND user_id=?`,
		encName, string(a.Type), a.Currency, a.Balance, a.Icon, a.CreditLimit, a.ID, userID,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		var exists int
		if err := db.QueryRow(`SELECT COUNT(*) FROM budget_accounts WHERE id=? AND user_id=?`, a.ID, userID).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			return sql.ErrNoRows
		}
	}
	return nil
}

// DeleteAccount removes an account. Child transactions are cascade-deleted by the DB.
func DeleteAccount(db *sql.DB, userID, id int64) error {
	res, err := db.Exec(
		`DELETE FROM budget_accounts WHERE id=? AND user_id=?`, id, userID,
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

func scanAccount(s scanner) (*Account, error) {
	var a Account
	if err := s.Scan(&a.ID, &a.UserID, &a.Name, &a.Type, &a.Currency, &a.Balance, &a.Icon, &a.CreditLimit); err != nil {
		return nil, err
	}
	name, err := encryption.DecryptField(a.Name)
	if err != nil {
		return nil, fmt.Errorf("decrypt account name: %w", err)
	}
	a.Name = name
	return &a, nil
}

// -- Category CRUD --

// CreateCategory inserts a new category for the given user and sets c.ID.
func CreateCategory(db *sql.DB, userID int64, c *Category) error {
	encName, err := encryption.EncryptField(c.Name)
	if err != nil {
		return fmt.Errorf("encrypt category name: %w", err)
	}
	encGroup, err := encryption.EncryptField(c.GroupName)
	if err != nil {
		return fmt.Errorf("encrypt category group_name: %w", err)
	}
	res, err := db.Exec(
		`INSERT INTO budget_categories (user_id, name, group_name, icon, color, is_income)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		userID, encName, encGroup, c.Icon, c.Color, boolToInt(c.IsIncome),
	)
	if err != nil {
		return err
	}
	c.ID, err = res.LastInsertId()
	c.UserID = userID
	return err
}

// GetCategory returns a single category scoped to the given user.
func GetCategory(db *sql.DB, userID, id int64) (*Category, error) {
	row := db.QueryRow(
		`SELECT id, user_id, name, group_name, icon, color, is_income
		 FROM budget_categories WHERE id = ? AND user_id = ?`,
		id, userID,
	)
	return scanCategory(row)
}

// ListCategories returns all categories for a user ordered by id.
func ListCategories(db *sql.DB, userID int64) ([]Category, error) {
	rows, err := db.Query(
		`SELECT id, user_id, name, group_name, icon, color, is_income
		 FROM budget_categories WHERE user_id = ? ORDER BY id`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck

	var categories []Category
	for rows.Next() {
		c, err := scanCategory(rows)
		if err != nil {
			return nil, err
		}
		categories = append(categories, *c)
	}
	if categories == nil {
		categories = []Category{}
	}
	return categories, rows.Err()
}

// UpdateCategory replaces the mutable fields of an existing category.
func UpdateCategory(db *sql.DB, userID int64, c *Category) error {
	encName, err := encryption.EncryptField(c.Name)
	if err != nil {
		return fmt.Errorf("encrypt category name: %w", err)
	}
	encGroup, err := encryption.EncryptField(c.GroupName)
	if err != nil {
		return fmt.Errorf("encrypt category group_name: %w", err)
	}
	res, err := db.Exec(
		`UPDATE budget_categories SET name=?, group_name=?, icon=?, color=?, is_income=?
		 WHERE id=? AND user_id=?`,
		encName, encGroup, c.Icon, c.Color, boolToInt(c.IsIncome), c.ID, userID,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		var exists int
		if err := db.QueryRow(`SELECT COUNT(*) FROM budget_categories WHERE id=? AND user_id=?`, c.ID, userID).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			return sql.ErrNoRows
		}
	}
	return nil
}

// DeleteCategory removes a category. Transactions referencing it have category_id set to NULL.
func DeleteCategory(db *sql.DB, userID, id int64) error {
	res, err := db.Exec(
		`DELETE FROM budget_categories WHERE id=? AND user_id=?`, id, userID,
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

func scanCategory(s scanner) (*Category, error) {
	var c Category
	var isIncome int
	if err := s.Scan(&c.ID, &c.UserID, &c.Name, &c.GroupName, &c.Icon, &c.Color, &isIncome); err != nil {
		return nil, err
	}
	name, err := encryption.DecryptField(c.Name)
	if err != nil {
		return nil, fmt.Errorf("decrypt category name: %w", err)
	}
	c.Name = name
	group, err := encryption.DecryptField(c.GroupName)
	if err != nil {
		return nil, fmt.Errorf("decrypt category group_name: %w", err)
	}
	c.GroupName = group
	c.IsIncome = isIncome != 0
	return &c, nil
}

// -- Transaction CRUD --

// CreateTransaction inserts a new transaction for the given user and sets t.ID.
func CreateTransaction(db *sql.DB, userID int64, t *Transaction) error {
	encDesc, err := encryption.EncryptField(t.Description)
	if err != nil {
		return fmt.Errorf("encrypt transaction description: %w", err)
	}
	tags := t.Tags
	if tags == nil {
		tags = []string{}
	}
	tagsJSON, err := json.Marshal(tags)
	if err != nil {
		return fmt.Errorf("marshal tags: %w", err)
	}
	res, err := db.Exec(
		`INSERT INTO budget_transactions
		 (user_id, account_id, category_id, amount, description, date, tags, is_transfer, transfer_to_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		userID, t.AccountID, int64PtrToNull(t.CategoryID), t.Amount,
		encDesc, t.Date, string(tagsJSON),
		boolToInt(t.IsTransfer), int64PtrToNull(t.TransferToID),
	)
	if err != nil {
		return err
	}
	t.ID, err = res.LastInsertId()
	t.UserID = userID
	return err
}

// GetTransaction returns a single transaction scoped to the given user.
func GetTransaction(db *sql.DB, userID, id int64) (*Transaction, error) {
	row := db.QueryRow(
		`SELECT id, user_id, account_id, category_id, amount, description, date,
		        tags, is_transfer, transfer_to_id
		 FROM budget_transactions WHERE id = ? AND user_id = ?`,
		id, userID,
	)
	return scanTransaction(row)
}

// ListTransactions returns transactions for a user with optional filtering.
func ListTransactions(db *sql.DB, userID int64, f TransactionFilter) ([]Transaction, error) {
	query := `SELECT id, user_id, account_id, category_id, amount, description, date,
	                 tags, is_transfer, transfer_to_id
	          FROM budget_transactions WHERE user_id = ?`
	args := []any{userID}

	if f.AccountID != nil {
		query += ` AND account_id = ?`
		args = append(args, *f.AccountID)
	}
	if f.CategoryID != nil {
		query += ` AND category_id = ?`
		args = append(args, *f.CategoryID)
	}
	if f.FromDate != "" {
		query += ` AND date >= ?`
		args = append(args, f.FromDate)
	}
	if f.ToDate != "" {
		query += ` AND date <= ?`
		args = append(args, f.ToDate)
	}
	query += ` ORDER BY date DESC, id DESC`

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck

	var txns []Transaction
	for rows.Next() {
		tx, err := scanTransaction(rows)
		if err != nil {
			return nil, err
		}
		txns = append(txns, *tx)
	}
	if txns == nil {
		txns = []Transaction{}
	}
	return txns, rows.Err()
}

// UpdateTransaction replaces the mutable fields of an existing transaction.
func UpdateTransaction(db *sql.DB, userID int64, t *Transaction) error {
	encDesc, err := encryption.EncryptField(t.Description)
	if err != nil {
		return fmt.Errorf("encrypt transaction description: %w", err)
	}
	tags := t.Tags
	if tags == nil {
		tags = []string{}
	}
	tagsJSON, err := json.Marshal(tags)
	if err != nil {
		return fmt.Errorf("marshal tags: %w", err)
	}
	res, err := db.Exec(
		`UPDATE budget_transactions
		 SET account_id=?, category_id=?, amount=?, description=?, date=?,
		     tags=?, is_transfer=?, transfer_to_id=?
		 WHERE id=? AND user_id=?`,
		t.AccountID, int64PtrToNull(t.CategoryID), t.Amount,
		encDesc, t.Date, string(tagsJSON),
		boolToInt(t.IsTransfer), int64PtrToNull(t.TransferToID),
		t.ID, userID,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		var exists int
		if err := db.QueryRow(`SELECT COUNT(*) FROM budget_transactions WHERE id=? AND user_id=?`, t.ID, userID).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			return sql.ErrNoRows
		}
	}
	return nil
}

// CreateTransfer atomically inserts a debit and a credit transaction and links
// them via transfer_to_id. It returns both created transactions.
func CreateTransfer(db *sql.DB, userID, fromAccountID, toAccountID int64, amount float64, description, date string) (*Transaction, *Transaction, error) {
	encDesc, err := encryption.EncryptField(description)
	if err != nil {
		return nil, nil, fmt.Errorf("encrypt transfer description: %w", err)
	}
	emptyTags := `[]`

	tx, err := db.Begin()
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback() //nolint:errcheck

	// Insert debit (negative amount on source account)
	resOut, err := tx.Exec(
		`INSERT INTO budget_transactions
		 (user_id, account_id, amount, description, date, tags, is_transfer, transfer_to_id)
		 VALUES (?, ?, ?, ?, ?, ?, 1, NULL)`,
		userID, fromAccountID, -amount, encDesc, date, emptyTags,
	)
	if err != nil {
		return nil, nil, err
	}
	outID, err := resOut.LastInsertId()
	if err != nil {
		return nil, nil, err
	}

	// Insert credit (positive amount on destination account), linking back to debit
	resIn, err := tx.Exec(
		`INSERT INTO budget_transactions
		 (user_id, account_id, amount, description, date, tags, is_transfer, transfer_to_id)
		 VALUES (?, ?, ?, ?, ?, ?, 1, ?)`,
		userID, toAccountID, amount, encDesc, date, emptyTags, outID,
	)
	if err != nil {
		return nil, nil, err
	}
	inID, err := resIn.LastInsertId()
	if err != nil {
		return nil, nil, err
	}

	// Update debit to link to credit
	if _, err = tx.Exec(`UPDATE budget_transactions SET transfer_to_id=? WHERE id=?`, inID, outID); err != nil {
		return nil, nil, err
	}

	if err = tx.Commit(); err != nil {
		return nil, nil, err
	}

	debit := &Transaction{
		ID: outID, UserID: userID, AccountID: fromAccountID,
		Amount: -amount, Description: description, Date: date,
		Tags: []string{}, IsTransfer: true, TransferToID: &inID,
	}
	credit := &Transaction{
		ID: inID, UserID: userID, AccountID: toAccountID,
		Amount: amount, Description: description, Date: date,
		Tags: []string{}, IsTransfer: true, TransferToID: &outID,
	}
	return debit, credit, nil
}

// DeleteTransaction removes a transaction scoped to the given user.
func DeleteTransaction(db *sql.DB, userID, id int64) error {
	res, err := db.Exec(
		`DELETE FROM budget_transactions WHERE id=? AND user_id=?`, id, userID,
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

func scanTransaction(s scanner) (*Transaction, error) {
	var t Transaction
	var categoryID sql.NullInt64
	var transferToID sql.NullInt64
	var isTransfer int
	var tagsJSON string
	if err := s.Scan(
		&t.ID, &t.UserID, &t.AccountID, &categoryID,
		&t.Amount, &t.Description, &t.Date,
		&tagsJSON, &isTransfer, &transferToID,
	); err != nil {
		return nil, err
	}
	desc, err := encryption.DecryptField(t.Description)
	if err != nil {
		return nil, fmt.Errorf("decrypt transaction description: %w", err)
	}
	t.Description = desc
	if err := json.Unmarshal([]byte(tagsJSON), &t.Tags); err != nil {
		return nil, fmt.Errorf("unmarshal transaction tags %q: %w", tagsJSON, err)
	}
	if t.Tags == nil {
		t.Tags = []string{}
	}
	t.IsTransfer = isTransfer != 0
	if categoryID.Valid {
		t.CategoryID = &categoryID.Int64
	}
	if transferToID.Valid {
		t.TransferToID = &transferToID.Int64
	}
	return &t, nil
}

// -- Recurring CRUD --

// CreateRecurring inserts a new recurring rule for the given user and sets r.ID.
func CreateRecurring(db *sql.DB, userID int64, r *Recurring) error {
	encDesc, err := encryption.EncryptField(r.Description)
	if err != nil {
		return fmt.Errorf("encrypt recurring description: %w", err)
	}
	var endDate, lastGenerated any
	if r.EndDate != "" {
		endDate = r.EndDate
	}
	if r.LastGenerated != "" {
		lastGenerated = r.LastGenerated
	}
	splitType := string(r.SplitType)
	if splitType == "" {
		splitType = string(SplitTypePercentage)
	}
	var splitPct any
	if r.SplitPct != nil {
		splitPct = *r.SplitPct
	}
	res, err := db.Exec(
		`INSERT INTO budget_recurring
		 (user_id, account_id, category_id, amount, description, frequency, day_of_month,
		  start_date, end_date, last_generated, active, split_type, split_pct)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		userID, r.AccountID, int64PtrToNull(r.CategoryID),
		r.Amount, encDesc, string(r.Frequency), r.DayOfMonth,
		r.StartDate.Format("2006-01-02"), endDate, lastGenerated, boolToInt(r.Active),
		splitType, splitPct,
	)
	if err != nil {
		return err
	}
	r.ID, err = res.LastInsertId()
	r.UserID = userID
	return err
}

// GetRecurring returns a single recurring rule scoped to the given user.
func GetRecurring(db *sql.DB, userID, id int64) (*Recurring, error) {
	row := db.QueryRow(
		`SELECT id, user_id, account_id, category_id, amount, description, frequency, day_of_month,
		        start_date, end_date, last_generated, active, split_type, split_pct
		 FROM budget_recurring WHERE id = ? AND user_id = ?`,
		id, userID,
	)
	return scanRecurring(row)
}

// ListRecurring returns all recurring rules for a user ordered by id.
func ListRecurring(db *sql.DB, userID int64) ([]Recurring, error) {
	rows, err := db.Query(
		`SELECT id, user_id, account_id, category_id, amount, description, frequency, day_of_month,
		        start_date, end_date, last_generated, active, split_type, split_pct
		 FROM budget_recurring WHERE user_id = ? ORDER BY id`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck

	var rules []Recurring
	for rows.Next() {
		r, err := scanRecurring(rows)
		if err != nil {
			return nil, err
		}
		rules = append(rules, *r)
	}
	if rules == nil {
		rules = []Recurring{}
	}
	return rules, rows.Err()
}

// listActiveRecurring returns all active recurring rules for a user.
// The regning calculator operates on every active rule; use split_type/split_pct
// on individual rules to control how each expense is divided between partners.
func listActiveRecurring(db *sql.DB, userID int64) ([]Recurring, error) {
	rows, err := db.Query(
		`SELECT id, user_id, account_id, category_id, amount, description, frequency, day_of_month,
		        start_date, end_date, last_generated, active, split_type, split_pct
		 FROM budget_recurring WHERE user_id = ? AND active = 1 ORDER BY id`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck

	var rules []Recurring
	for rows.Next() {
		r, err := scanRecurring(rows)
		if err != nil {
			return nil, err
		}
		rules = append(rules, *r)
	}
	if rules == nil {
		rules = []Recurring{}
	}
	return rules, rows.Err()
}

// UpdateRecurring replaces the mutable fields of an existing recurring rule.
func UpdateRecurring(db *sql.DB, userID int64, r *Recurring) error {
	encDesc, err := encryption.EncryptField(r.Description)
	if err != nil {
		return fmt.Errorf("encrypt recurring description: %w", err)
	}
	var endDate, lastGenerated any
	if r.EndDate != "" {
		endDate = r.EndDate
	}
	if r.LastGenerated != "" {
		lastGenerated = r.LastGenerated
	}
	splitType := string(r.SplitType)
	if splitType == "" {
		splitType = string(SplitTypePercentage)
	}
	var splitPct any
	if r.SplitPct != nil {
		splitPct = *r.SplitPct
	}
	res, err := db.Exec(
		`UPDATE budget_recurring
		 SET account_id=?, category_id=?, amount=?, description=?, frequency=?, day_of_month=?,
		     start_date=?, end_date=?, last_generated=?, active=?, split_type=?, split_pct=?
		 WHERE id=? AND user_id=?`,
		r.AccountID, int64PtrToNull(r.CategoryID),
		r.Amount, encDesc, string(r.Frequency), r.DayOfMonth,
		r.StartDate.Format("2006-01-02"), endDate, lastGenerated, boolToInt(r.Active),
		splitType, splitPct,
		r.ID, userID,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		var exists int
		if err := db.QueryRow(`SELECT COUNT(*) FROM budget_recurring WHERE id=? AND user_id=?`, r.ID, userID).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			return sql.ErrNoRows
		}
	}
	return nil
}

// DeleteRecurring removes a recurring rule scoped to the given user.
func DeleteRecurring(db *sql.DB, userID, id int64) error {
	res, err := db.Exec(
		`DELETE FROM budget_recurring WHERE id=? AND user_id=?`, id, userID,
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

// GetRecurringDue returns active recurring rules for the user whose next occurrence is on or before now.
// A rule is due when:
//   - last_generated is unset and start_date <= now, or
//   - last_generated is set and last_generated + frequency interval <= now (capped at end_date)
//
// Inactive rules are excluded. Rules whose end_date has passed are included so that any
// ungenerated occurrences before the end_date can be backfilled.
func GetRecurringDue(db *sql.DB, userID int64, now time.Time) ([]Recurring, error) {
	today := now.Format("2006-01-02")
	rows, err := db.Query(
		`SELECT id, user_id, account_id, category_id, amount, description, frequency, day_of_month,
		        start_date, end_date, last_generated, active, split_type, split_pct
		 FROM budget_recurring
		 WHERE user_id = ?
		   AND active = 1
		 ORDER BY id`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck

	var due []Recurring
	for rows.Next() {
		r, err := scanRecurring(rows)
		if err != nil {
			return nil, err
		}
		if isRecurringDue(*r, today) {
			due = append(due, *r)
		}
	}
	if due == nil {
		due = []Recurring{}
	}
	return due, rows.Err()
}

// isRecurringDue reports whether the recurring rule r has a next occurrence on or before today
// (capped at end_date when set, so ended rules are only due if ungenerated occurrences remain).
// today must be a YYYY-MM-DD string.
func isRecurringDue(r Recurring, today string) bool {
	todayTime, err := time.Parse("2006-01-02", today)
	if err != nil {
		return false
	}
	nextDue, err := nextRecurringDueDate(r)
	if err != nil {
		return false
	}
	cutoff := todayTime
	if r.EndDate != "" {
		endDate, err := time.Parse("2006-01-02", r.EndDate)
		if err == nil && endDate.Before(cutoff) {
			cutoff = endDate
		}
	}
	return !nextDue.After(cutoff)
}

// nextRecurringDueDate computes the next due date for a recurring rule, respecting DayOfMonth
// and clamping to the last day of the target month to avoid month overflow (e.g. Jan 31 + 1 month).
func nextRecurringDueDate(r Recurring) (time.Time, error) {
	if r.LastGenerated == "" {
		// Never generated; first occurrence is the start date.
		return r.StartDate, nil
	}
	anchor, err := time.Parse("2006-01-02", r.LastGenerated)
	if err != nil {
		return time.Time{}, err
	}
	switch r.Frequency {
	case FrequencyWeekly:
		return anchor.AddDate(0, 0, 7), nil
	case FrequencyMonthly:
		year, month, _ := anchor.Date()
		targetMonth := month + 1
		day := recurringDayOfMonth(r.DayOfMonth, anchor.Day())
		day = clampDayOfMonth(year, targetMonth, day)
		return time.Date(year, targetMonth, day, 0, 0, 0, 0, anchor.Location()), nil
	case FrequencyQuarterly:
		year, month, _ := anchor.Date()
		targetMonth := month + 3
		day := recurringDayOfMonth(r.DayOfMonth, anchor.Day())
		day = clampDayOfMonth(year, targetMonth, day)
		return time.Date(year, targetMonth, day, 0, 0, 0, 0, anchor.Location()), nil
	case FrequencyYearly:
		targetYear := anchor.Year() + 1
		targetMonth := r.StartDate.Month()
		day := recurringDayOfMonth(r.DayOfMonth, r.StartDate.Day())
		day = clampDayOfMonth(targetYear, targetMonth, day)
		return time.Date(targetYear, targetMonth, day, 0, 0, 0, 0, anchor.Location()), nil
	default:
		year, month, _ := anchor.Date()
		targetMonth := month + 1
		day := recurringDayOfMonth(r.DayOfMonth, anchor.Day())
		day = clampDayOfMonth(year, targetMonth, day)
		return time.Date(year, targetMonth, day, 0, 0, 0, 0, anchor.Location()), nil
	}
}

func recurringDayOfMonth(configuredDay int, fallbackDay int) int {
	if configuredDay >= 1 && configuredDay <= 31 {
		return configuredDay
	}
	if fallbackDay < 1 {
		return 1
	}
	if fallbackDay > 31 {
		return 31
	}
	return fallbackDay
}

func clampDayOfMonth(year int, month time.Month, day int) int {
	lastDay := daysInMonth(year, month)
	if day < 1 {
		return 1
	}
	if day > lastDay {
		return lastDay
	}
	return day
}

func daysInMonth(year int, month time.Month) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

func scanRecurring(s scanner) (*Recurring, error) {
	var r Recurring
	var categoryID sql.NullInt64
	var startDateStr string
	var endDate, lastGenerated sql.NullString
	var isActive int
	var splitType string
	var splitPct sql.NullFloat64
	if err := s.Scan(
		&r.ID, &r.UserID, &r.AccountID, &categoryID,
		&r.Amount, &r.Description, &r.Frequency, &r.DayOfMonth,
		&startDateStr, &endDate, &lastGenerated, &isActive,
		&splitType, &splitPct,
	); err != nil {
		return nil, err
	}
	desc, err := encryption.DecryptField(r.Description)
	if err != nil {
		return nil, fmt.Errorf("decrypt recurring description: %w", err)
	}
	r.Description = desc
	t, err := time.Parse("2006-01-02", startDateStr)
	if err != nil {
		return nil, fmt.Errorf("parse start_date %q: %w", startDateStr, err)
	}
	r.StartDate = t
	if categoryID.Valid {
		r.CategoryID = &categoryID.Int64
	}
	if endDate.Valid {
		r.EndDate = endDate.String
	}
	if lastGenerated.Valid {
		r.LastGenerated = lastGenerated.String
	}
	r.Active = isActive != 0
	if splitType == "" {
		splitType = "percentage"
	}
	r.SplitType = SplitType(splitType)
	if splitPct.Valid {
		r.SplitPct = &splitPct.Float64
	}
	return &r, nil
}

// GenerateRecurringTransactions creates transactions for all due recurring rules
// for the given user and updates last_generated. Returns the number of transactions
// created. Each rule may generate multiple transactions if it has not been processed
// for more than one period.
//
// Each create-transaction + advance-last_generated pair is wrapped in its own DB
// transaction so that a crash between the two cannot produce duplicates on the next run.
func GenerateRecurringTransactions(db *sql.DB, userID int64, now time.Time) (int, error) {
	rules, err := GetRecurringDue(db, userID, now)
	if err != nil {
		return 0, err
	}
	count := 0
	for i := range rules {
		rule := rules[i]
		// Compute the generation cutoff: min(now, end_date) so we don't overshoot.
		cutoff := now
		if rule.EndDate != "" {
			endDate, err := time.Parse("2006-01-02", rule.EndDate)
			if err == nil && endDate.Before(cutoff) {
				cutoff = endDate
			}
		}
		// Generate one transaction per due occurrence until caught up.
		for {
			nextDue, err := nextRecurringDueDate(rule)
			if err != nil {
				return count, fmt.Errorf("compute next due date for rule %d: %w", rule.ID, err)
			}
			adjustedDue := nextBusinessDay(nextDue)
			if adjustedDue.After(cutoff) {
				break
			}
			// scheduledDateStr tracks the rule's cadence (stored in last_generated).
			// transactionDateStr is adjusted to the next business day for the actual record.
			scheduledDateStr := nextDue.Format("2006-01-02")
			transactionDateStr := adjustedDue.Format("2006-01-02")
			if err := generateOneOccurrence(db, userID, &rule, scheduledDateStr, transactionDateStr); err != nil {
				return count, fmt.Errorf("generate occurrence for rule %d on %s: %w", rule.ID, scheduledDateStr, err)
			}
			rule.LastGenerated = scheduledDateStr
			count++
		}
	}
	return count, nil
}

// generateOneOccurrence atomically inserts one budget transaction and advances
// last_generated on the recurring rule within a single DB transaction.
// scheduledDateStr is the raw computed due date (stored in last_generated to keep
// the recurrence schedule stable). transactionDateStr is the business-day-adjusted
// date used for the actual transaction record.
func generateOneOccurrence(db *sql.DB, userID int64, rule *Recurring, scheduledDateStr, transactionDateStr string) error {
	encDesc, err := encryption.EncryptField(rule.Description)
	if err != nil {
		return fmt.Errorf("encrypt description: %w", err)
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.Exec(
		`INSERT INTO budget_transactions
		 (user_id, account_id, category_id, amount, description, date, tags, is_transfer, transfer_to_id)
		 VALUES (?, ?, ?, ?, ?, ?, '[]', 0, NULL)`,
		userID, rule.AccountID, int64PtrToNull(rule.CategoryID), rule.Amount, encDesc, transactionDateStr,
	); err != nil {
		return fmt.Errorf("insert transaction: %w", err)
	}

	// Assert the previous last_generated value to guard against concurrent generation runs
	// producing duplicate transactions (optimistic concurrency). COALESCE maps NULL→'' so
	// we can compare against the in-memory value which uses '' for unset.
	// We store the raw scheduled date in last_generated so the recurrence cadence is
	// not affected by business-day adjustments.
	res, err := tx.Exec(
		`UPDATE budget_recurring SET last_generated = ? WHERE id = ? AND user_id = ? AND COALESCE(last_generated, '') = ?`,
		scheduledDateStr, rule.ID, userID, rule.LastGenerated,
	)
	if err != nil {
		return fmt.Errorf("update last_generated: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("recurring rule %d not found", rule.ID)
	}
	return tx.Commit()
}

// -- tx-aware helpers used by SeedDefaultCategories --

// listCategoriesTx queries categories within an existing transaction.
func listCategoriesTx(tx *sql.Tx, userID int64) ([]Category, error) {
	rows, err := tx.Query(
		`SELECT id, user_id, name, group_name, icon, color, is_income
		 FROM budget_categories WHERE user_id = ? ORDER BY id`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck

	var categories []Category
	for rows.Next() {
		c, err := scanCategory(rows)
		if err != nil {
			return nil, err
		}
		categories = append(categories, *c)
	}
	if categories == nil {
		categories = []Category{}
	}
	return categories, rows.Err()
}

// createCategoryTx inserts a category within an existing transaction.
func createCategoryTx(tx *sql.Tx, userID int64, c *Category) error {
	encName, err := encryption.EncryptField(c.Name)
	if err != nil {
		return fmt.Errorf("encrypt category name: %w", err)
	}
	encGroup, err := encryption.EncryptField(c.GroupName)
	if err != nil {
		return fmt.Errorf("encrypt category group_name: %w", err)
	}
	res, err := tx.Exec(
		`INSERT INTO budget_categories (user_id, name, group_name, icon, color, is_income)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		userID, encName, encGroup, c.Icon, c.Color, boolToInt(c.IsIncome),
	)
	if err != nil {
		return err
	}
	c.ID, err = res.LastInsertId()
	c.UserID = userID
	return err
}

// -- Budget limits --

// normalizeEffectiveFrom accepts YYYY-MM or YYYY-MM-01 and returns YYYY-MM-01,
// or an error for any other format.
func normalizeEffectiveFrom(s string) (string, error) {
	if len(s) == 7 {
		s = s + "-01"
	}
	if _, err := time.Parse("2006-01-02", s); err != nil || s[8:] != "01" {
		return "", fmt.Errorf("effective_from must be in YYYY-MM or YYYY-MM-01 format, got %q", s)
	}
	return s, nil
}

// SetBudgetLimit upserts a budget limit for a category effective from the first
// day of the given month (month must be in YYYY-MM format). If a limit already
// exists for that (user, category, effective_from) triple it is replaced.
func SetBudgetLimit(db *sql.DB, userID int64, limit *BudgetLimit) error {
	effectiveFrom, err := normalizeEffectiveFrom(limit.EffectiveFrom)
	if err != nil {
		return err
	}
	period := limit.Period
	if period == "" {
		period = "monthly"
	}
	res, err := db.Exec(
		`INSERT INTO budget_limits (user_id, category_id, amount, period, effective_from)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(user_id, category_id, effective_from)
		 DO UPDATE SET amount=excluded.amount, period=excluded.period`,
		userID, limit.CategoryID, limit.Amount, period, effectiveFrom,
	)
	if err != nil {
		return err
	}
	// LastInsertId is 0/undefined for the UPDATE path of ON CONFLICT DO UPDATE,
	// so we always query the actual row id after upsert.
	_ = res
	err = db.QueryRow(
		`SELECT id FROM budget_limits WHERE user_id=? AND category_id=? AND effective_from=?`,
		userID, limit.CategoryID, effectiveFrom,
	).Scan(&limit.ID)
	limit.UserID = userID
	limit.EffectiveFrom = effectiveFrom
	return err
}

// SetBudgetLimitTx is the transaction-aware variant of SetBudgetLimit.
func SetBudgetLimitTx(tx *sql.Tx, userID int64, limit *BudgetLimit) error {
	effectiveFrom, err := normalizeEffectiveFrom(limit.EffectiveFrom)
	if err != nil {
		return err
	}
	period := limit.Period
	if period == "" {
		period = "monthly"
	}
	_, err = tx.Exec(
		`INSERT INTO budget_limits (user_id, category_id, amount, period, effective_from)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(user_id, category_id, effective_from)
		 DO UPDATE SET amount=excluded.amount, period=excluded.period`,
		userID, limit.CategoryID, limit.Amount, period, effectiveFrom,
	)
	if err != nil {
		return err
	}
	err = tx.QueryRow(
		`SELECT id FROM budget_limits WHERE user_id=? AND category_id=? AND effective_from=?`,
		userID, limit.CategoryID, effectiveFrom,
	).Scan(&limit.ID)
	limit.UserID = userID
	limit.EffectiveFrom = effectiveFrom
	return err
}

// GetBudgetLimits returns a map of category_id → BudgetLimit for the given
// month (YYYY-MM). For each category the limit with the latest effective_from
// that is <= the first day of the given month is returned.
func GetBudgetLimits(db *sql.DB, userID int64, month string) (map[int64]BudgetLimit, error) {
	// Convert YYYY-MM to YYYY-MM-01 for comparison.
	upTo := month
	if len(month) == 7 {
		upTo = month + "-01"
	}
	rows, err := db.Query(
		`SELECT bl.id, bl.user_id, bl.category_id, bl.amount, bl.period, bl.effective_from
		 FROM budget_limits bl
		 INNER JOIN (
		     SELECT category_id, MAX(effective_from) AS max_eff
		     FROM budget_limits
		     WHERE user_id = ? AND effective_from <= ?
		     GROUP BY category_id
		 ) latest ON bl.category_id = latest.category_id
		             AND bl.effective_from = latest.max_eff
		 WHERE bl.user_id = ?`,
		userID, upTo, userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck

	result := make(map[int64]BudgetLimit)
	for rows.Next() {
		var l BudgetLimit
		if err := rows.Scan(&l.ID, &l.UserID, &l.CategoryID, &l.Amount, &l.Period, &l.EffectiveFrom); err != nil {
			return nil, err
		}
		result[l.CategoryID] = l
	}
	return result, rows.Err()
}

// -- helpers --

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func int64PtrToNull(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}
