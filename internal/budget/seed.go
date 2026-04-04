package budget

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strconv"
)

// seedCategory describes a default budget category to be inserted on first use.
type seedCategory struct {
	Name      string
	GroupName string
	IsIncome  bool
}

// defaultCategories is the canonical list of categories seeded for every user.
// Groups: Bolig (housing), Barn (children), Fast (fixed), Variabel (variable), Inntekt (income).
var defaultCategories = []seedCategory{
	// Bolig — housing fixed costs
	{Name: "Husforsikring", GroupName: "Bolig"},
	{Name: "Strøm", GroupName: "Bolig"},
	// Barn — child-related expenses
	{Name: "Barnehage", GroupName: "Barn"},
	// Fast — other fixed monthly expenses
	{Name: "Forsikring", GroupName: "Fast"},
	{Name: "Lån", GroupName: "Fast"},
	{Name: "Mobil", GroupName: "Fast"},
	// Variabel — variable spending
	{Name: "Mat", GroupName: "Variabel"},
	{Name: "Transport", GroupName: "Variabel"},
	{Name: "Underholdning", GroupName: "Variabel"},
	// Inntekt — income; is_income=true
	{Name: "Lønn", GroupName: "Inntekt", IsIncome: true},
}

// SeedDefaultCategories inserts the prescribed default categories for the given
// user. It is idempotent: categories that already exist (same name and group)
// are skipped. Existing categories created by the user are not affected.
// The existence check and inserts run inside a serializable transaction
// (BEGIN IMMEDIATE in SQLite) to prevent duplicate seeds under concurrent
// first-time access.
func SeedDefaultCategories(db *sql.DB, userID int64) error {
	tx, err := db.BeginTx(context.Background(), &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return fmt.Errorf("seed: begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			tx.Rollback() //nolint:errcheck
		}
	}()

	existing, err := listCategoriesTx(tx, userID)
	if err != nil {
		return fmt.Errorf("list categories for seed: %w", err)
	}

	type key struct{ name, group string }
	present := make(map[key]bool, len(existing))
	for _, c := range existing {
		present[key{c.Name, c.GroupName}] = true
	}

	for _, dc := range defaultCategories {
		if present[key{dc.Name, dc.GroupName}] {
			continue
		}
		c := &Category{
			Name:      dc.Name,
			GroupName: dc.GroupName,
			IsIncome:  dc.IsIncome,
		}
		if err = createCategoryTx(tx, userID, c); err != nil {
			log.Printf("budget: seed: failed to insert category %q (%s): %v", dc.Name, dc.GroupName, err)
			return fmt.Errorf("seed category %q: %w", dc.Name, err)
		}
	}

	err = tx.Commit()
	return err
}

const (
	incomeSplitKey     = "income_split_percentage"
	defaultIncomeSplit = 60

	partnerIncomeKey     = "partner_income"
	defaultPartnerIncome = 0
	maxPartnerIncome     = 10_000_000 // monthly salary cap in NOK; must match settings_handlers.go intRangeKeys

	incomeDayKey        = "income_day"
	partnerIncomeDayKey = "partner_income_day"
	defaultIncomeDay    = 20 // day of month when salary is paid; must match settings_handlers.go intRangeKeys
)

// GetIncomeSplit returns the user's income split percentage (0–100).
// Defaults to 60 if not set.
func GetIncomeSplit(db *sql.DB, userID int64) (int, error) {
	var value string
	err := db.QueryRow(
		"SELECT value FROM user_preferences WHERE user_id = ? AND key = ?",
		userID, incomeSplitKey,
	).Scan(&value)
	if err == sql.ErrNoRows {
		return defaultIncomeSplit, nil
	}
	if err != nil {
		return 0, fmt.Errorf("get income split: %w", err)
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		return defaultIncomeSplit, nil
	}
	if n < 0 || n > 100 {
		return defaultIncomeSplit, nil
	}
	return n, nil
}

// SetIncomeSplit stores the user's income split percentage (0–100).
func SetIncomeSplit(db *sql.DB, userID int64, pct int) error {
	if pct < 0 || pct > 100 {
		return fmt.Errorf("income split percentage must be between 0 and 100, got %d", pct)
	}
	_, err := db.Exec(
		`INSERT INTO user_preferences (user_id, key, value) VALUES (?, ?, ?)
		 ON CONFLICT(user_id, key) DO UPDATE SET value = excluded.value`,
		userID, incomeSplitKey, strconv.Itoa(pct),
	)
	if err != nil {
		return fmt.Errorf("set income split: %w", err)
	}
	return nil
}

// GetPartnerIncome returns the partner's monthly salary. Defaults to 0 if not set.
func GetPartnerIncome(db *sql.DB, userID int64) (int, error) {
	var value string
	err := db.QueryRow(
		"SELECT value FROM user_preferences WHERE user_id = ? AND key = ?",
		userID, partnerIncomeKey,
	).Scan(&value)
	if err == sql.ErrNoRows {
		return defaultPartnerIncome, nil
	}
	if err != nil {
		return 0, fmt.Errorf("get partner income: %w", err)
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		return defaultPartnerIncome, nil
	}
	if n < 0 || n > maxPartnerIncome {
		return defaultPartnerIncome, nil
	}
	return n, nil
}

// GetIncomeDay returns the day of month (1–31) when the user's salary is paid.
// Defaults to 20 if not set.
func GetIncomeDay(db *sql.DB, userID int64) (int, error) {
	return getIncomeDay(db, userID, incomeDayKey)
}

// SetIncomeDay stores the day of month when the user's salary is paid.
func SetIncomeDay(db *sql.DB, userID int64, day int) error {
	return setIncomeDay(db, userID, incomeDayKey, day)
}

// GetPartnerIncomeDay returns the day of month (1–31) when the partner's salary is paid.
// Defaults to 20 if not set.
func GetPartnerIncomeDay(db *sql.DB, userID int64) (int, error) {
	return getIncomeDay(db, userID, partnerIncomeDayKey)
}

// SetPartnerIncomeDay stores the day of month when the partner's salary is paid.
func SetPartnerIncomeDay(db *sql.DB, userID int64, day int) error {
	return setIncomeDay(db, userID, partnerIncomeDayKey, day)
}

func getIncomeDay(db *sql.DB, userID int64, key string) (int, error) {
	var value string
	err := db.QueryRow(
		"SELECT value FROM user_preferences WHERE user_id = ? AND key = ?",
		userID, key,
	).Scan(&value)
	if err == sql.ErrNoRows {
		return defaultIncomeDay, nil
	}
	if err != nil {
		return 0, fmt.Errorf("get income day (%s): %w", key, err)
	}
	n, err := strconv.Atoi(value)
	if err != nil || n < 1 || n > 31 {
		return defaultIncomeDay, nil
	}
	return n, nil
}

func setIncomeDay(db *sql.DB, userID int64, key string, day int) error {
	if day < 1 || day > 31 {
		return fmt.Errorf("income day must be between 1 and 31, got %d", day)
	}
	_, err := db.Exec(
		`INSERT INTO user_preferences (user_id, key, value) VALUES (?, ?, ?)
		 ON CONFLICT(user_id, key) DO UPDATE SET value = excluded.value`,
		userID, key, strconv.Itoa(day),
	)
	if err != nil {
		return fmt.Errorf("set income day (%s): %w", key, err)
	}
	return nil
}

// SetPartnerIncome stores the partner's monthly salary.
func SetPartnerIncome(db *sql.DB, userID int64, amount int) error {
	if amount < 0 || amount > maxPartnerIncome {
		return fmt.Errorf("partner income must be between 0 and %d, got %d", maxPartnerIncome, amount)
	}
	_, err := db.Exec(
		`INSERT INTO user_preferences (user_id, key, value) VALUES (?, ?, ?)
		 ON CONFLICT(user_id, key) DO UPDATE SET value = excluded.value`,
		userID, partnerIncomeKey, strconv.Itoa(amount),
	)
	if err != nil {
		return fmt.Errorf("set partner income: %w", err)
	}
	return nil
}
