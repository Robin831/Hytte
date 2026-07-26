package wardrobe

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/encryption"
)

func nowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339)
}

// --- Kids ---

// ListKids returns all kid profiles owned by the parent, oldest first.
func ListKids(db *sql.DB, parentID int64) ([]Kid, error) {
	rows, err := db.Query(`
		SELECT id, parent_id, user_id, name, birthdate, avatar_emoji, created_at
		FROM wardrobe_kids
		WHERE parent_id = ?
		ORDER BY created_at ASC, id ASC
	`, parentID)
	if err != nil {
		return nil, fmt.Errorf("query wardrobe kids: %w", err)
	}
	defer rows.Close()

	kids := []Kid{}
	for rows.Next() {
		var k Kid
		var createdAt string
		var userID sql.NullInt64
		if err := rows.Scan(&k.ID, &k.ParentID, &userID, &k.Name, &k.Birthdate, &k.AvatarEmoji, &createdAt); err != nil {
			return nil, fmt.Errorf("scan wardrobe kid: %w", err)
		}
		if userID.Valid {
			k.UserID = &userID.Int64
		}
		if k.Name, err = encryption.DecryptField(k.Name); err != nil {
			return nil, fmt.Errorf("decrypt kid name: %w", err)
		}
		k.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		kids = append(kids, k)
	}
	return kids, rows.Err()
}

// AddKid inserts a kid profile and returns it with its generated ID.
func AddKid(db *sql.DB, kid Kid) (Kid, error) {
	encName, err := encryption.EncryptField(kid.Name)
	if err != nil {
		return Kid{}, fmt.Errorf("encrypt kid name: %w", err)
	}
	now := nowRFC3339()
	res, err := db.Exec(`
		INSERT INTO wardrobe_kids (parent_id, user_id, name, birthdate, avatar_emoji, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, kid.ParentID, kid.UserID, encName, kid.Birthdate, kid.AvatarEmoji, now)
	if err != nil {
		return Kid{}, fmt.Errorf("insert wardrobe kid: %w", err)
	}
	kid.ID, err = res.LastInsertId()
	if err != nil {
		return Kid{}, fmt.Errorf("last insert id: %w", err)
	}
	kid.CreatedAt, _ = time.Parse(time.RFC3339, now)
	return kid, nil
}

// UpdateKid updates a kid's editable fields, scoped to the owning parent.
func UpdateKid(db *sql.DB, id, parentID int64, req KidRequest) error {
	encName, err := encryption.EncryptField(req.Name)
	if err != nil {
		return fmt.Errorf("encrypt kid name: %w", err)
	}
	res, err := db.Exec(`
		UPDATE wardrobe_kids SET name = ?, birthdate = ?, avatar_emoji = ?
		WHERE id = ? AND parent_id = ?
	`, encName, req.Birthdate, req.AvatarEmoji, id, parentID)
	if err != nil {
		return fmt.Errorf("update wardrobe kid: %w", err)
	}
	return requireRow(res)
}

// DeleteKid removes a kid profile (measurements and items cascade).
func DeleteKid(db *sql.DB, id, parentID int64) error {
	res, err := db.Exec("DELETE FROM wardrobe_kids WHERE id = ? AND parent_id = ?", id, parentID)
	if err != nil {
		return fmt.Errorf("delete wardrobe kid: %w", err)
	}
	return requireRow(res)
}

// kidOwnedBy reports whether the kid exists and belongs to the parent.
func kidOwnedBy(db *sql.DB, kidID, parentID int64) (bool, error) {
	var n int
	err := db.QueryRow("SELECT COUNT(*) FROM wardrobe_kids WHERE id = ? AND parent_id = ?", kidID, parentID).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("check kid ownership: %w", err)
	}
	return n > 0, nil
}

// --- Measurements ---

// ListMeasurements returns a kid's measurements, oldest first (the order
// growthRate expects). The caller must have verified kid ownership.
func ListMeasurements(db *sql.DB, kidID int64) ([]Measurement, error) {
	rows, err := db.Query(`
		SELECT id, kid_id, measured_at, height_cm, foot_length_mm, weight_kg, note, created_at
		FROM wardrobe_measurements
		WHERE kid_id = ?
		ORDER BY measured_at ASC, id ASC
	`, kidID)
	if err != nil {
		return nil, fmt.Errorf("query measurements: %w", err)
	}
	defer rows.Close()

	ms := []Measurement{}
	for rows.Next() {
		var m Measurement
		var createdAt string
		if err := rows.Scan(&m.ID, &m.KidID, &m.MeasuredAt, &m.HeightCM, &m.FootLengthMM, &m.WeightKG, &m.Note, &createdAt); err != nil {
			return nil, fmt.Errorf("scan measurement: %w", err)
		}
		if m.Note, err = encryption.DecryptField(m.Note); err != nil {
			return nil, fmt.Errorf("decrypt measurement note: %w", err)
		}
		m.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		ms = append(ms, m)
	}
	return ms, rows.Err()
}

// AddMeasurement inserts a measurement. The caller must have verified kid ownership.
func AddMeasurement(db *sql.DB, m Measurement) (Measurement, error) {
	encNote, err := encryption.EncryptField(m.Note)
	if err != nil {
		return Measurement{}, fmt.Errorf("encrypt measurement note: %w", err)
	}
	now := nowRFC3339()
	res, err := db.Exec(`
		INSERT INTO wardrobe_measurements (kid_id, measured_at, height_cm, foot_length_mm, weight_kg, note, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, m.KidID, m.MeasuredAt, m.HeightCM, m.FootLengthMM, m.WeightKG, encNote, now)
	if err != nil {
		return Measurement{}, fmt.Errorf("insert measurement: %w", err)
	}
	m.ID, err = res.LastInsertId()
	if err != nil {
		return Measurement{}, fmt.Errorf("last insert id: %w", err)
	}
	m.CreatedAt, _ = time.Parse(time.RFC3339, now)
	return m, nil
}

// DeleteMeasurement removes a measurement, scoped to the owning parent via the kid.
func DeleteMeasurement(db *sql.DB, id, parentID int64) error {
	res, err := db.Exec(`
		DELETE FROM wardrobe_measurements
		WHERE id = ? AND kid_id IN (SELECT id FROM wardrobe_kids WHERE parent_id = ?)
	`, id, parentID)
	if err != nil {
		return fmt.Errorf("delete measurement: %w", err)
	}
	return requireRow(res)
}

// --- Categories ---

// ListCategories returns the parent's categories in display order.
func ListCategories(db *sql.DB, parentID int64) ([]Category, error) {
	rows, err := db.Query(`
		SELECT id, parent_id, name, icon, size_system, target_qty, sort_order, created_at
		FROM wardrobe_categories
		WHERE parent_id = ?
		ORDER BY sort_order ASC, id ASC
	`, parentID)
	if err != nil {
		return nil, fmt.Errorf("query wardrobe categories: %w", err)
	}
	defer rows.Close()

	cats := []Category{}
	for rows.Next() {
		var c Category
		var createdAt string
		if err := rows.Scan(&c.ID, &c.ParentID, &c.Name, &c.Icon, &c.SizeSystem, &c.TargetQty, &c.SortOrder, &createdAt); err != nil {
			return nil, fmt.Errorf("scan wardrobe category: %w", err)
		}
		c.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		cats = append(cats, c)
	}
	return cats, rows.Err()
}

// AddCategory inserts a category, appending it after the parent's existing ones.
func AddCategory(db *sql.DB, c Category) (Category, error) {
	var maxOrder sql.NullInt64
	if err := db.QueryRow("SELECT MAX(sort_order) FROM wardrobe_categories WHERE parent_id = ?", c.ParentID).Scan(&maxOrder); err != nil {
		return Category{}, fmt.Errorf("select max sort_order: %w", err)
	}
	if maxOrder.Valid {
		c.SortOrder = int(maxOrder.Int64) + 1
	}
	now := nowRFC3339()
	res, err := db.Exec(`
		INSERT INTO wardrobe_categories (parent_id, name, icon, size_system, target_qty, sort_order, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, c.ParentID, c.Name, c.Icon, c.SizeSystem, c.TargetQty, c.SortOrder, now)
	if err != nil {
		return Category{}, fmt.Errorf("insert wardrobe category: %w", err)
	}
	c.ID, err = res.LastInsertId()
	if err != nil {
		return Category{}, fmt.Errorf("last insert id: %w", err)
	}
	c.CreatedAt, _ = time.Parse(time.RFC3339, now)
	return c, nil
}

// UpdateCategory updates a category's editable fields, scoped to the owner.
func UpdateCategory(db *sql.DB, id, parentID int64, req CategoryRequest) error {
	res, err := db.Exec(`
		UPDATE wardrobe_categories SET name = ?, icon = ?, size_system = ?, target_qty = ?
		WHERE id = ? AND parent_id = ?
	`, req.Name, req.Icon, req.SizeSystem, req.TargetQty, id, parentID)
	if err != nil {
		return fmt.Errorf("update wardrobe category: %w", err)
	}
	return requireRow(res)
}

// ErrCategoryInUse is returned when deleting a category that still has items.
var ErrCategoryInUse = fmt.Errorf("category has items")

// DeleteCategory removes a category, refusing while items still reference it.
func DeleteCategory(db *sql.DB, id, parentID int64) error {
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM wardrobe_items WHERE category_id = ?", id).Scan(&n); err != nil {
		return fmt.Errorf("count category items: %w", err)
	}
	if n > 0 {
		return ErrCategoryInUse
	}
	res, err := db.Exec("DELETE FROM wardrobe_categories WHERE id = ? AND parent_id = ?", id, parentID)
	if err != nil {
		return fmt.Errorf("delete wardrobe category: %w", err)
	}
	return requireRow(res)
}

// --- Items ---

// ListItems returns all of a parent's items, optionally restricted to one kid.
func ListItems(db *sql.DB, parentID int64, kidID int64) ([]Item, error) {
	query := `
		SELECT id, parent_id, kid_id, category_id, name, size_label, quantity,
		       condition, status, location, season, notes, created_at, updated_at
		FROM wardrobe_items
		WHERE parent_id = ?`
	args := []any{parentID}
	if kidID > 0 {
		query += " AND kid_id = ?"
		args = append(args, kidID)
	}
	query += " ORDER BY category_id ASC, created_at ASC, id ASC"

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query wardrobe items: %w", err)
	}
	defer rows.Close()

	items := []Item{}
	for rows.Next() {
		var it Item
		var createdAt, updatedAt string
		if err := rows.Scan(&it.ID, &it.ParentID, &it.KidID, &it.CategoryID, &it.Name, &it.SizeLabel,
			&it.Quantity, &it.Condition, &it.Status, &it.Location, &it.Season, &it.Notes, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("scan wardrobe item: %w", err)
		}
		if it.Name, err = encryption.DecryptField(it.Name); err != nil {
			return nil, fmt.Errorf("decrypt item name: %w", err)
		}
		if it.Notes, err = encryption.DecryptField(it.Notes); err != nil {
			return nil, fmt.Errorf("decrypt item notes: %w", err)
		}
		it.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		it.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		items = append(items, it)
	}
	return items, rows.Err()
}

// AddItem inserts an item and returns it with its generated ID. The caller
// must have verified kid and category ownership.
func AddItem(db *sql.DB, it Item) (Item, error) {
	encName, err := encryption.EncryptField(it.Name)
	if err != nil {
		return Item{}, fmt.Errorf("encrypt item name: %w", err)
	}
	encNotes, err := encryption.EncryptField(it.Notes)
	if err != nil {
		return Item{}, fmt.Errorf("encrypt item notes: %w", err)
	}
	now := nowRFC3339()
	res, err := db.Exec(`
		INSERT INTO wardrobe_items (parent_id, kid_id, category_id, name, size_label, quantity,
			condition, status, location, season, notes, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, it.ParentID, it.KidID, it.CategoryID, encName, it.SizeLabel, it.Quantity,
		it.Condition, it.Status, it.Location, it.Season, encNotes, now, now)
	if err != nil {
		return Item{}, fmt.Errorf("insert wardrobe item: %w", err)
	}
	it.ID, err = res.LastInsertId()
	if err != nil {
		return Item{}, fmt.Errorf("last insert id: %w", err)
	}
	it.CreatedAt, _ = time.Parse(time.RFC3339, now)
	it.UpdatedAt = it.CreatedAt
	return it, nil
}

// UpdateItem updates an item's editable fields, scoped to the owner. The
// caller must have verified ownership of any new kid/category references.
func UpdateItem(db *sql.DB, id, parentID int64, req ItemRequest) error {
	encName, err := encryption.EncryptField(req.Name)
	if err != nil {
		return fmt.Errorf("encrypt item name: %w", err)
	}
	encNotes, err := encryption.EncryptField(req.Notes)
	if err != nil {
		return fmt.Errorf("encrypt item notes: %w", err)
	}
	res, err := db.Exec(`
		UPDATE wardrobe_items SET kid_id = ?, category_id = ?, name = ?, size_label = ?, quantity = ?,
			condition = ?, status = ?, location = ?, season = ?, notes = ?, updated_at = ?
		WHERE id = ? AND parent_id = ?
	`, req.KidID, req.CategoryID, encName, req.SizeLabel, req.Quantity,
		req.Condition, req.Status, req.Location, req.Season, encNotes, nowRFC3339(), id, parentID)
	if err != nil {
		return fmt.Errorf("update wardrobe item: %w", err)
	}
	return requireRow(res)
}

// DeleteItem removes an item, scoped to the owner.
func DeleteItem(db *sql.DB, id, parentID int64) error {
	res, err := db.Exec("DELETE FROM wardrobe_items WHERE id = ? AND parent_id = ?", id, parentID)
	if err != nil {
		return fmt.Errorf("delete wardrobe item: %w", err)
	}
	return requireRow(res)
}

// --- Derived views ---

// KidStats assembles a kid's derived size guidance from measurement history.
func KidStats(db *sql.DB, kid Kid) (KidWithStats, error) {
	stats := KidWithStats{Kid: kid}
	history, err := ListMeasurements(db, kid.ID)
	if err != nil {
		return stats, err
	}
	if len(history) == 0 {
		return stats, nil
	}
	latest := history[len(history)-1]
	stats.LatestMeasurement = &latest
	stats.Clothing = RecommendClothing(latest.HeightCM)
	stats.Shoe = RecommendShoe(latest.FootLengthMM)
	stats.HeightRateCMPerMonth = growthRate(history, func(m Measurement) float64 { return m.HeightCM })
	stats.FootRateMMPerMonth = growthRate(history, func(m Measurement) float64 { return m.FootLengthMM })
	return stats, nil
}

// Needs computes a kid's wardrobe gaps: categories with a target quantity not
// covered by active items, plus items flagged too_small, each with the size to
// buy derived from the latest measurement.
func Needs(db *sql.DB, parentID, kidID int64) ([]NeedEntry, []TooSmallEntry, error) {
	cats, err := ListCategories(db, parentID)
	if err != nil {
		return nil, nil, err
	}
	items, err := ListItems(db, parentID, kidID)
	if err != nil {
		return nil, nil, err
	}
	history, err := ListMeasurements(db, kidID)
	if err != nil {
		return nil, nil, err
	}
	var latest *Measurement
	if len(history) > 0 {
		latest = &history[len(history)-1]
	}

	activeQty := map[int64]int{}
	tooSmall := []TooSmallEntry{}
	catByID := map[int64]Category{}
	for _, c := range cats {
		catByID[c.ID] = c
	}
	for _, it := range items {
		switch it.Status {
		case "active":
			activeQty[it.CategoryID] += it.Quantity
		case "too_small":
			tooSmall = append(tooSmall, TooSmallEntry{
				Item:            it,
				RecommendedSize: recommendedSizeLabel(catByID[it.CategoryID].SizeSystem, latest),
			})
		}
	}

	needs := []NeedEntry{}
	for _, c := range cats {
		if c.TargetQty <= 0 {
			continue
		}
		have := activeQty[c.ID]
		if have >= c.TargetQty {
			continue
		}
		needs = append(needs, NeedEntry{
			CategoryID:      c.ID,
			CategoryName:    c.Name,
			CategoryIcon:    c.Icon,
			Have:            have,
			Target:          c.TargetQty,
			RecommendedSize: recommendedSizeLabel(c.SizeSystem, latest),
		})
	}
	return needs, tooSmall, nil
}

func requireRow(res sql.Result) error {
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// validation helpers shared by handlers

func validOneOf(v string, allowed ...string) bool {
	for _, a := range allowed {
		if v == a {
			return true
		}
	}
	return false
}

// normalizeDate accepts YYYY-MM-DD and returns it trimmed, or "" if invalid/empty.
func normalizeDate(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if _, err := time.Parse("2006-01-02", s); err != nil {
		return ""
	}
	return s
}
