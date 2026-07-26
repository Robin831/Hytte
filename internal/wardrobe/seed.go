package wardrobe

import (
	"context"
	"database/sql"
	"fmt"
)

// seedCategory describes a default wardrobe category inserted on first use.
type seedCategory struct {
	Name       string
	Icon       string
	SizeSystem string
	TargetQty  int
}

// defaultCategories is the canonical category list seeded for every user.
// Names are Norwegian like other seeded backend content (see budget). Targets
// reflect a typical barnehage/school packing list — e.g. two full sets of
// spare clothes and rain gear that must always be present — and are editable
// per user afterwards.
var defaultCategories = []seedCategory{
	{Name: "Yttertøy", Icon: "🧥", SizeSystem: "clothing", TargetQty: 1},
	{Name: "Regntøy", Icon: "🌧️", SizeSystem: "clothing", TargetQty: 1},
	{Name: "Ullundertøy", Icon: "🐑", SizeSystem: "clothing", TargetQty: 2},
	{Name: "Fleece/mellomlag", Icon: "🧶", SizeSystem: "clothing", TargetQty: 1},
	{Name: "Overdeler", Icon: "👕", SizeSystem: "clothing", TargetQty: 5},
	{Name: "Bukser", Icon: "👖", SizeSystem: "clothing", TargetQty: 4},
	{Name: "Undertøy og sokker", Icon: "🧦", SizeSystem: "clothing", TargetQty: 6},
	{Name: "Skiftetøy", Icon: "🎒", SizeSystem: "clothing", TargetQty: 2},
	{Name: "Sko", Icon: "👟", SizeSystem: "shoe", TargetQty: 1},
	{Name: "Regnstøvler", Icon: "🥾", SizeSystem: "shoe", TargetQty: 1},
	{Name: "Vintersko", Icon: "❄️", SizeSystem: "shoe", TargetQty: 1},
	{Name: "Innesko", Icon: "🩴", SizeSystem: "shoe", TargetQty: 1},
	{Name: "Lue, votter og hals", Icon: "🧤", SizeSystem: "none", TargetQty: 2},
	{Name: "Badetøy", Icon: "🩱", SizeSystem: "none", TargetQty: 0},
	{Name: "Pysjamas", Icon: "🌙", SizeSystem: "clothing", TargetQty: 0},
	{Name: "Annet", Icon: "📦", SizeSystem: "none", TargetQty: 0},
}

// SeedDefaultCategories inserts the default categories for the given user.
// It is idempotent: categories whose name already exists are skipped, and the
// check + inserts run in a serializable transaction (BEGIN IMMEDIATE in
// SQLite) to prevent duplicate seeds under concurrent first-time access.
func SeedDefaultCategories(db *sql.DB, parentID int64) error {
	tx, err := db.BeginTx(context.Background(), &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return fmt.Errorf("wardrobe seed: begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			tx.Rollback() //nolint:errcheck
		}
	}()

	present := map[string]bool{}
	rows, err := tx.Query("SELECT name FROM wardrobe_categories WHERE parent_id = ?", parentID)
	if err != nil {
		return fmt.Errorf("wardrobe seed: list categories: %w", err)
	}
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			rows.Close()
			return fmt.Errorf("wardrobe seed: scan category: %w", err)
		}
		present[name] = true
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}

	now := nowRFC3339()
	for i, dc := range defaultCategories {
		if present[dc.Name] {
			continue
		}
		if _, err = tx.Exec(`
			INSERT INTO wardrobe_categories (parent_id, name, icon, size_system, target_qty, sort_order, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, parentID, dc.Name, dc.Icon, dc.SizeSystem, dc.TargetQty, i, now); err != nil {
			return fmt.Errorf("wardrobe seed: insert category %q: %w", dc.Name, err)
		}
	}

	err = tx.Commit()
	return err
}
