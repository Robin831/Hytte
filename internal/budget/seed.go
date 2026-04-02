package budget

import (
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
func SeedDefaultCategories(db *sql.DB, userID int64) error {
	existing, err := ListCategories(db, userID)
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
		if err := CreateCategory(db, userID, c); err != nil {
			log.Printf("budget: seed: failed to insert category %q (%s): %v", dc.Name, dc.GroupName, err)
			return fmt.Errorf("seed category %q: %w", dc.Name, err)
		}
	}
	return nil
}

const (
	incomeSplitKey     = "income_split_percentage"
	defaultIncomeSplit = 60
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
