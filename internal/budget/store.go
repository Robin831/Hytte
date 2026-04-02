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
		`INSERT INTO budget_accounts (user_id, name, type, currency, balance, icon)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		userID, encName, string(a.Type), a.Currency, a.Balance, a.Icon,
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
		`SELECT id, user_id, name, type, currency, balance, icon
		 FROM budget_accounts WHERE id = ? AND user_id = ?`,
		id, userID,
	)
	return scanAccount(row)
}

// ListAccounts returns all accounts for a user ordered by id.
func ListAccounts(db *sql.DB, userID int64) ([]Account, error) {
	rows, err := db.Query(
		`SELECT id, user_id, name, type, currency, balance, icon
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
		`UPDATE budget_accounts SET name=?, type=?, currency=?, balance=?, icon=?
		 WHERE id=? AND user_id=?`,
		encName, string(a.Type), a.Currency, a.Balance, a.Icon, a.ID, userID,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("account not found")
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
		return fmt.Errorf("account not found")
	}
	return nil
}

func scanAccount(s scanner) (*Account, error) {
	var a Account
	if err := s.Scan(&a.ID, &a.UserID, &a.Name, &a.Type, &a.Currency, &a.Balance, &a.Icon); err != nil {
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
		return fmt.Errorf("category not found")
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
		return fmt.Errorf("category not found")
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
		return fmt.Errorf("transaction not found")
	}
	return nil
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
		return fmt.Errorf("transaction not found")
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
		t.Tags = []string{}
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
	var endDate, lastGenerated any
	if r.EndDate != "" {
		endDate = r.EndDate
	}
	if r.LastGenerated != "" {
		lastGenerated = r.LastGenerated
	}
	res, err := db.Exec(
		`INSERT INTO budget_recurring
		 (user_id, account_id, category_id, amount, frequency, day_of_month,
		  start_date, end_date, last_generated)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		userID, r.AccountID, int64PtrToNull(r.CategoryID),
		r.Amount, string(r.Frequency), r.DayOfMonth,
		r.StartDate.Format("2006-01-02"), endDate, lastGenerated,
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
		`SELECT id, user_id, account_id, category_id, amount, frequency, day_of_month,
		        start_date, end_date, last_generated
		 FROM budget_recurring WHERE id = ? AND user_id = ?`,
		id, userID,
	)
	return scanRecurring(row)
}

// ListRecurring returns all recurring rules for a user ordered by id.
func ListRecurring(db *sql.DB, userID int64) ([]Recurring, error) {
	rows, err := db.Query(
		`SELECT id, user_id, account_id, category_id, amount, frequency, day_of_month,
		        start_date, end_date, last_generated
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

// UpdateRecurring replaces the mutable fields of an existing recurring rule.
func UpdateRecurring(db *sql.DB, userID int64, r *Recurring) error {
	var endDate, lastGenerated any
	if r.EndDate != "" {
		endDate = r.EndDate
	}
	if r.LastGenerated != "" {
		lastGenerated = r.LastGenerated
	}
	res, err := db.Exec(
		`UPDATE budget_recurring
		 SET account_id=?, category_id=?, amount=?, frequency=?, day_of_month=?,
		     start_date=?, end_date=?, last_generated=?
		 WHERE id=? AND user_id=?`,
		r.AccountID, int64PtrToNull(r.CategoryID),
		r.Amount, string(r.Frequency), r.DayOfMonth,
		r.StartDate.Format("2006-01-02"), endDate, lastGenerated,
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
		return fmt.Errorf("recurring rule not found")
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
		return fmt.Errorf("recurring rule not found")
	}
	return nil
}

// GetRecurringDue returns recurring rules for the user whose next occurrence is on or before now.
// A rule is due when:
//   - last_generated is unset and start_date <= now, or
//   - last_generated is set and last_generated + frequency interval <= now
//
// Rules whose end_date has passed are excluded.
func GetRecurringDue(db *sql.DB, userID int64, now time.Time) ([]Recurring, error) {
	today := now.Format("2006-01-02")
	rows, err := db.Query(
		`SELECT id, user_id, account_id, category_id, amount, frequency, day_of_month,
		        start_date, end_date, last_generated
		 FROM budget_recurring
		 WHERE user_id = ?
		   AND (end_date IS NULL OR end_date = '' OR end_date >= ?)
		 ORDER BY id`,
		userID, today,
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

// isRecurringDue reports whether the recurring rule r has a next occurrence on or before today.
// today must be a YYYY-MM-DD string. Lexicographic comparison is correct for this format.
func isRecurringDue(r Recurring, today string) bool {
	var nextDue string
	if r.LastGenerated == "" {
		// Never generated; first occurrence is the start date.
		nextDue = r.StartDate.Format("2006-01-02")
	} else {
		t, err := time.Parse("2006-01-02", r.LastGenerated)
		if err != nil {
			return false
		}
		switch r.Frequency {
		case FrequencyWeekly:
			t = t.AddDate(0, 0, 7)
		case FrequencyMonthly:
			t = t.AddDate(0, 1, 0)
		case FrequencyYearly:
			t = t.AddDate(1, 0, 0)
		default:
			t = t.AddDate(0, 1, 0)
		}
		nextDue = t.Format("2006-01-02")
	}
	return nextDue <= today
}

func scanRecurring(s scanner) (*Recurring, error) {
	var r Recurring
	var categoryID sql.NullInt64
	var startDateStr string
	var endDate, lastGenerated sql.NullString
	if err := s.Scan(
		&r.ID, &r.UserID, &r.AccountID, &categoryID,
		&r.Amount, &r.Frequency, &r.DayOfMonth,
		&startDateStr, &endDate, &lastGenerated,
	); err != nil {
		return nil, err
	}
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
	return &r, nil
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
