package recipes

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/encryption"
)

// ErrInvalidRating is returned by SetRating for a score outside 0-5.
// 0 clears an existing rating; 1-5 set one.
var ErrInvalidRating = errors.New("rating must be between 0 and 5")

// ErrInvalidSlot is returned for a meal slot outside the allowed set.
var ErrInvalidSlot = errors.New("slot must be breakfast, lunch, dinner or snack")

// ErrInvalidPlanDate is returned for a plan date that is not a YYYY-MM-DD
// calendar day.
var ErrInvalidPlanDate = errors.New("plan date must be YYYY-MM-DD")

// ErrIngredientNotInRecipe is returned when an ingredient ID does not belong to
// the recipe it was requested against.
var ErrIngredientNotInRecipe = errors.New("ingredient does not belong to this recipe")

// PlanDateLayout is the calendar-day format meal plan entries are stored and
// exchanged in. Entries are days, not instants — no time zone is attached.
const PlanDateLayout = "2006-01-02"

// slotOrder ranks the meal slots for display: a day's entries come back in the
// order they are eaten rather than in insertion order.
var slotOrder = map[string]int{"breakfast": 0, "lunch": 1, "dinner": 2, "snack": 3}

// NormalizeSlot trims and lowercases a slot name and reports whether it is one
// of the four the schema allows.
func NormalizeSlot(slot string) (string, bool) {
	slot = strings.ToLower(strings.TrimSpace(slot))
	_, ok := slotOrder[slot]
	return slot, ok
}

// ParsePlanDate parses a YYYY-MM-DD calendar day, rejecting anything else
// (including timestamps and out-of-range days such as 2026-02-30).
func ParsePlanDate(s string) (time.Time, error) {
	day, err := time.Parse(PlanDateLayout, strings.TrimSpace(s))
	if err != nil {
		return time.Time{}, ErrInvalidPlanDate
	}
	return day, nil
}

// ISOWeekStart returns the Monday of the ISO week containing day, keeping the
// day's location so a week never shifts across a time-zone boundary.
func ISOWeekStart(day time.Time) time.Time {
	// time.Weekday counts from Sunday; ISO weeks start on Monday.
	offset := (int(day.Weekday()) + 6) % 7
	y, m, d := day.AddDate(0, 0, -offset).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, day.Location())
}

// Store is the persistence layer for the recipes feature. It owns all
// encryption and decryption of recipe text — callers above it only ever see
// plaintext models.
type Store struct {
	db *sql.DB
}

// NewStore wraps an existing database handle.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func nowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

func parseTime(s string) time.Time {
	parsed, _ := time.Parse(time.RFC3339, s)
	return parsed
}

// normalizeTags trims, lowercases and de-duplicates a tag list, dropping empty
// entries. The result is sorted so stored and returned tag sets are stable.
func normalizeTags(tags []string) []string {
	seen := make(map[string]struct{}, len(tags))
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" {
			continue
		}
		if _, dup := seen[t]; dup {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// placeholders builds "?, ?, ?" for an IN clause of n values.
func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

// --- Recipes ---

// CreateRecipe inserts a recipe together with its ingredients, steps and tags
// in a single transaction and returns it with all generated IDs populated.
func (s *Store) CreateRecipe(ctx context.Context, userID int64, r Recipe) (Recipe, error) {
	encTitle, err := encryption.EncryptField(r.Title)
	if err != nil {
		return Recipe{}, fmt.Errorf("encrypt recipe title: %w", err)
	}
	encNotes, err := encryption.EncryptField(r.Notes)
	if err != nil {
		return Recipe{}, fmt.Errorf("encrypt recipe notes: %w", err)
	}

	now := nowRFC3339()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Recipe{}, fmt.Errorf("begin create recipe: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx, `
		INSERT INTO recipes (user_id, title, notes, servings, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, userID, encTitle, encNotes, r.Servings, now, now)
	if err != nil {
		return Recipe{}, fmt.Errorf("insert recipe: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Recipe{}, fmt.Errorf("last insert id: %w", err)
	}

	r.ID = id
	r.UserID = userID
	r.CreatedAt = parseTime(now)
	r.UpdatedAt = r.CreatedAt
	// Rating and cooking history are not settable at creation — SetRating and
	// RecordCook own those columns, so don't echo back values we didn't store.
	r.Rating, r.RatedAt, r.LastCook = nil, nil, nil
	if err := writeChildren(ctx, tx, &r); err != nil {
		return Recipe{}, err
	}

	if err := tx.Commit(); err != nil {
		return Recipe{}, fmt.Errorf("commit create recipe: %w", err)
	}
	return r, nil
}

// writeChildren inserts the recipe's ingredients, steps and tags, rewriting
// Position from slice order so stored positions are always dense and ordered.
// It fills r's child slices with the generated IDs; the caller's slices are
// copied first so a create/update never writes back into the argument it was
// handed.
func writeChildren(ctx context.Context, tx *sql.Tx, r *Recipe) error {
	ings := make([]Ingredient, len(r.Ingredients))
	copy(ings, r.Ingredients)
	r.Ingredients = ings

	steps := make([]Step, len(r.Steps))
	copy(steps, r.Steps)
	r.Steps = steps

	for i := range r.Ingredients {
		ing := &r.Ingredients[i]
		encText, err := encryption.EncryptField(ing.Text)
		if err != nil {
			return fmt.Errorf("encrypt ingredient text: %w", err)
		}
		ing.RecipeID = r.ID
		ing.Position = i
		res, err := tx.ExecContext(ctx, `
			INSERT INTO recipe_ingredients (recipe_id, position, text, quantity, unit, name)
			VALUES (?, ?, ?, ?, ?, ?)
		`, r.ID, i, encText, ing.Quantity, ing.Unit, ing.Name)
		if err != nil {
			return fmt.Errorf("insert recipe ingredient: %w", err)
		}
		if ing.ID, err = res.LastInsertId(); err != nil {
			return fmt.Errorf("last insert id: %w", err)
		}
	}

	for i := range r.Steps {
		step := &r.Steps[i]
		encText, err := encryption.EncryptField(step.Text)
		if err != nil {
			return fmt.Errorf("encrypt step text: %w", err)
		}
		step.RecipeID = r.ID
		step.Position = i
		res, err := tx.ExecContext(ctx, `
			INSERT INTO recipe_steps (recipe_id, position, text, duration_seconds)
			VALUES (?, ?, ?, ?)
		`, r.ID, i, encText, step.DurationSeconds)
		if err != nil {
			return fmt.Errorf("insert recipe step: %w", err)
		}
		if step.ID, err = res.LastInsertId(); err != nil {
			return fmt.Errorf("last insert id: %w", err)
		}
	}

	r.Tags = normalizeTags(r.Tags)
	for _, tag := range r.Tags {
		if _, err := tx.ExecContext(ctx,
			"INSERT OR IGNORE INTO recipe_tags (recipe_id, tag) VALUES (?, ?)", r.ID, tag); err != nil {
			return fmt.Errorf("insert recipe tag: %w", err)
		}
	}
	return nil
}

// GetRecipe loads one recipe with its children, scoped to the owning user.
// Returns sql.ErrNoRows when the recipe does not exist or belongs elsewhere.
func (s *Store) GetRecipe(ctx context.Context, userID, id int64) (Recipe, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, user_id, title, notes, servings, rating, rated_at, last_cooked_at, created_at, updated_at
		FROM recipes
		WHERE id = ? AND user_id = ?
	`, id, userID)

	r, err := scanRecipe(row)
	if err != nil {
		return Recipe{}, err
	}
	if err := s.attachChildren(ctx, []*Recipe{&r}); err != nil {
		return Recipe{}, err
	}
	return r, nil
}

// ListRecipes returns the user's recipes, newest first, optionally narrowed by
// tags. An empty filter returns everything the user owns.
func (s *Store) ListRecipes(ctx context.Context, userID int64, filter TagFilter) ([]Recipe, error) {
	query := `
		SELECT id, user_id, title, notes, servings, rating, rated_at, last_cooked_at, created_at, updated_at
		FROM recipes r
		WHERE r.user_id = ?`
	args := []any{userID}

	if anyTags := normalizeTags(filter.Any); len(anyTags) > 0 {
		query += fmt.Sprintf(`
		AND EXISTS (SELECT 1 FROM recipe_tags t WHERE t.recipe_id = r.id AND t.tag IN (%s))`, placeholders(len(anyTags)))
		for _, tag := range anyTags {
			args = append(args, tag)
		}
	}
	if allTags := normalizeTags(filter.All); len(allTags) > 0 {
		query += fmt.Sprintf(`
		AND (SELECT COUNT(DISTINCT t.tag) FROM recipe_tags t WHERE t.recipe_id = r.id AND t.tag IN (%s)) = ?`,
			placeholders(len(allTags)))
		for _, tag := range allTags {
			args = append(args, tag)
		}
		args = append(args, len(allTags))
	}
	query += `
		ORDER BY r.created_at DESC, r.id DESC`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query recipes: %w", err)
	}
	defer rows.Close()

	recipes := []Recipe{}
	for rows.Next() {
		r, err := scanRecipe(rows)
		if err != nil {
			return nil, err
		}
		recipes = append(recipes, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recipes: %w", err)
	}

	ptrs := make([]*Recipe, len(recipes))
	for i := range recipes {
		ptrs[i] = &recipes[i]
	}
	if err := s.attachChildren(ctx, ptrs); err != nil {
		return nil, err
	}
	return recipes, nil
}

// UpdateRecipe replaces a recipe's editable fields and its child rows, scoped
// to the owning user. Children are deleted and re-inserted wholesale so
// positions stay dense. Rating and cooking history are untouched — SetRating
// and RecordCook own those.
func (s *Store) UpdateRecipe(ctx context.Context, userID int64, r Recipe) (Recipe, error) {
	encTitle, err := encryption.EncryptField(r.Title)
	if err != nil {
		return Recipe{}, fmt.Errorf("encrypt recipe title: %w", err)
	}
	encNotes, err := encryption.EncryptField(r.Notes)
	if err != nil {
		return Recipe{}, fmt.Errorf("encrypt recipe notes: %w", err)
	}

	now := nowRFC3339()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Recipe{}, fmt.Errorf("begin update recipe: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx, `
		UPDATE recipes SET title = ?, notes = ?, servings = ?, updated_at = ?
		WHERE id = ? AND user_id = ?
	`, encTitle, encNotes, r.Servings, now, r.ID, userID)
	if err != nil {
		return Recipe{}, fmt.Errorf("update recipe: %w", err)
	}
	if err := requireRow(res); err != nil {
		return Recipe{}, err
	}

	for _, stmt := range []string{
		"DELETE FROM recipe_ingredients WHERE recipe_id = ?",
		"DELETE FROM recipe_steps WHERE recipe_id = ?",
		"DELETE FROM recipe_tags WHERE recipe_id = ?",
	} {
		if _, err := tx.ExecContext(ctx, stmt, r.ID); err != nil {
			return Recipe{}, fmt.Errorf("clear recipe children: %w", err)
		}
	}

	// Re-read the columns this method does not own so the returned recipe
	// reflects what is stored rather than whatever the caller sent.
	var rating sql.NullInt64
	var ratedAt sql.NullString
	var lastCooked sql.NullString
	var createdAt string
	if err := tx.QueryRowContext(ctx,
		"SELECT rating, rated_at, last_cooked_at, created_at FROM recipes WHERE id = ?", r.ID).
		Scan(&rating, &ratedAt, &lastCooked, &createdAt); err != nil {
		return Recipe{}, fmt.Errorf("reload recipe metadata: %w", err)
	}
	r.Rating, r.RatedAt, r.LastCook = nil, nil, nil
	if rating.Valid {
		v := int(rating.Int64)
		r.Rating = &v
	}
	if ratedAt.Valid && ratedAt.String != "" {
		t := parseTime(ratedAt.String)
		r.RatedAt = &t
	}
	if lastCooked.Valid && lastCooked.String != "" {
		t := parseTime(lastCooked.String)
		r.LastCook = &t
	}
	r.CreatedAt = parseTime(createdAt)

	r.UserID = userID
	r.UpdatedAt = parseTime(now)
	if err := writeChildren(ctx, tx, &r); err != nil {
		return Recipe{}, err
	}

	if err := tx.Commit(); err != nil {
		return Recipe{}, fmt.Errorf("commit update recipe: %w", err)
	}
	return r, nil
}

// DeleteRecipe removes a recipe; ingredients, steps, tags, cooking log entries
// and meal-plan entries cascade.
func (s *Store) DeleteRecipe(ctx context.Context, userID, id int64) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM recipes WHERE id = ? AND user_id = ?", id, userID)
	if err != nil {
		return fmt.Errorf("delete recipe: %w", err)
	}
	return requireRow(res)
}

// SetRating stores a 1-5 score for a recipe. A rating of 0 clears it.
// Returns ErrInvalidRating for anything outside that range and sql.ErrNoRows
// when the recipe is not the user's.
func (s *Store) SetRating(ctx context.Context, userID, recipeID int64, rating int) error {
	if rating < 0 || rating > 5 {
		return ErrInvalidRating
	}
	var value any
	var ratedAt any
	if rating > 0 {
		value = rating
		ratedAt = nowRFC3339()
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE recipes SET rating = ?, rated_at = ?
		WHERE id = ? AND user_id = ?
	`, value, ratedAt, recipeID, userID)
	if err != nil {
		return fmt.Errorf("set recipe rating: %w", err)
	}
	return requireRow(res)
}

// RecordCook appends an entry to the cooking log and advances the recipe's
// last_cooked_at. Backfilling an older cook keeps the newer last_cooked_at:
// the column tracks the most recent cook, not the most recently logged one.
func (s *Store) RecordCook(ctx context.Context, userID, recipeID int64, at time.Time) error {
	stamp := formatTime(at)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin record cook: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Scoping the UPDATE by user_id doubles as the ownership check.
	res, err := tx.ExecContext(ctx, `
		UPDATE recipes SET last_cooked_at = MAX(COALESCE(last_cooked_at, ''), ?)
		WHERE id = ? AND user_id = ?
	`, stamp, recipeID, userID)
	if err != nil {
		return fmt.Errorf("update last cooked: %w", err)
	}
	if err := requireRow(res); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO recipe_cooks (recipe_id, user_id, cooked_at) VALUES (?, ?, ?)
	`, recipeID, userID, stamp); err != nil {
		return fmt.Errorf("insert recipe cook: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit record cook: %w", err)
	}
	return nil
}

// ListCooks returns a recipe's cooking log, most recent first.
func (s *Store) ListCooks(ctx context.Context, userID, recipeID int64) ([]Cook, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT c.id, c.recipe_id, c.user_id, c.cooked_at
		FROM recipe_cooks c
		JOIN recipes r ON r.id = c.recipe_id
		WHERE c.recipe_id = ? AND r.user_id = ?
		ORDER BY c.cooked_at DESC, c.id DESC
	`, recipeID, userID)
	if err != nil {
		return nil, fmt.Errorf("query recipe cooks: %w", err)
	}
	defer rows.Close()

	cooks := []Cook{}
	for rows.Next() {
		var c Cook
		var cookedAt string
		if err := rows.Scan(&c.ID, &c.RecipeID, &c.UserID, &cookedAt); err != nil {
			return nil, fmt.Errorf("scan recipe cook: %w", err)
		}
		c.CookedAt = parseTime(cookedAt)
		cooks = append(cooks, c)
	}
	return cooks, rows.Err()
}

// --- Meal plan ---

// ListPlanEntries returns the user's meal plan entries for the inclusive date
// range [from, to], ordered by day and then by the order the slots are eaten
// in (see slotOrder). Both bounds are YYYY-MM-DD; the lexicographic comparison
// SQLite performs on that format is also the chronological one.
func (s *Store) ListPlanEntries(ctx context.Context, userID int64, from, to string) ([]PlanEntry, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.id, p.user_id, p.recipe_id, p.plan_date, p.slot, p.created_at, r.title
		FROM meal_plan_entries p
		JOIN recipes r ON r.id = p.recipe_id
		WHERE p.user_id = ? AND p.plan_date >= ? AND p.plan_date <= ?
	`, userID, from, to)
	if err != nil {
		return nil, fmt.Errorf("query meal plan entries: %w", err)
	}
	defer rows.Close()

	entries := []PlanEntry{}
	for rows.Next() {
		var e PlanEntry
		var createdAt string
		if err := rows.Scan(&e.ID, &e.UserID, &e.RecipeID, &e.PlanDate, &e.Slot, &createdAt, &e.RecipeTitle); err != nil {
			return nil, fmt.Errorf("scan meal plan entry: %w", err)
		}
		if e.RecipeTitle, err = encryption.DecryptField(e.RecipeTitle); err != nil {
			return nil, fmt.Errorf("decrypt recipe title: %w", err)
		}
		e.CreatedAt = parseTime(createdAt)
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate meal plan entries: %w", err)
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].PlanDate != entries[j].PlanDate {
			return entries[i].PlanDate < entries[j].PlanDate
		}
		return slotOrder[entries[i].Slot] < slotOrder[entries[j].Slot]
	})
	return entries, nil
}

// UpsertPlanEntries writes one or more plan entries for the user and returns
// them as stored. Each entry replaces whatever occupied its (day, slot), so
// sending the same payload twice is a no-op the second time rather than a
// duplicate row. created_at is the moment the slot was first filled and is kept
// across replacements.
//
// Every referenced recipe must belong to the user: a foreign or unknown recipe
// ID fails the whole batch with sql.ErrNoRows and nothing is written. Invalid
// dates and slots fail with ErrInvalidPlanDate / ErrInvalidSlot.
func (s *Store) UpsertPlanEntries(ctx context.Context, userID int64, entries []PlanEntry) ([]PlanEntry, error) {
	if len(entries) == 0 {
		return []PlanEntry{}, nil
	}

	// Validate and normalise up front so a bad entry never opens a transaction.
	normalized := make([]PlanEntry, len(entries))
	recipeIDs := make([]any, 0, len(entries))
	seenRecipe := make(map[int64]struct{}, len(entries))
	for i, e := range entries {
		day, err := ParsePlanDate(e.PlanDate)
		if err != nil {
			return nil, err
		}
		slot, ok := NormalizeSlot(e.Slot)
		if !ok {
			return nil, ErrInvalidSlot
		}
		if e.RecipeID <= 0 {
			return nil, sql.ErrNoRows
		}
		normalized[i] = PlanEntry{
			UserID:   userID,
			RecipeID: e.RecipeID,
			PlanDate: day.Format(PlanDateLayout),
			Slot:     slot,
		}
		if _, dup := seenRecipe[e.RecipeID]; !dup {
			seenRecipe[e.RecipeID] = struct{}{}
			recipeIDs = append(recipeIDs, e.RecipeID)
		}
	}

	titles, err := s.recipeTitles(ctx, userID, recipeIDs)
	if err != nil {
		return nil, err
	}

	now := nowRFC3339()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin upsert plan entries: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	for i := range normalized {
		e := &normalized[i]
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO meal_plan_entries (user_id, recipe_id, plan_date, slot, created_at)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT (user_id, plan_date, slot) DO UPDATE SET recipe_id = excluded.recipe_id
		`, userID, e.RecipeID, e.PlanDate, e.Slot, now); err != nil {
			return nil, fmt.Errorf("upsert meal plan entry: %w", err)
		}

		// Read the row back rather than trusting LastInsertId, which reports the
		// pre-existing row only on insert and not on the conflict path.
		var createdAt string
		if err := tx.QueryRowContext(ctx, `
			SELECT id, created_at FROM meal_plan_entries
			WHERE user_id = ? AND plan_date = ? AND slot = ?
		`, userID, e.PlanDate, e.Slot).Scan(&e.ID, &createdAt); err != nil {
			return nil, fmt.Errorf("reload meal plan entry: %w", err)
		}
		e.CreatedAt = parseTime(createdAt)
		e.RecipeTitle = titles[e.RecipeID]
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit upsert plan entries: %w", err)
	}
	return normalized, nil
}

// DeletePlanEntry clears the entry occupying one (day, slot) for the user.
// Returns sql.ErrNoRows when the slot was already empty.
func (s *Store) DeletePlanEntry(ctx context.Context, userID int64, planDate, slot string) error {
	day, err := ParsePlanDate(planDate)
	if err != nil {
		return err
	}
	normalizedSlot, ok := NormalizeSlot(slot)
	if !ok {
		return ErrInvalidSlot
	}

	res, err := s.db.ExecContext(ctx, `
		DELETE FROM meal_plan_entries WHERE user_id = ? AND plan_date = ? AND slot = ?
	`, userID, day.Format(PlanDateLayout), normalizedSlot)
	if err != nil {
		return fmt.Errorf("delete meal plan entry: %w", err)
	}
	return requireRow(res)
}

// recipeTitles loads the decrypted titles of the given recipe IDs, scoped to
// the user. Any ID that is missing or owned by someone else makes the lookup
// fail with sql.ErrNoRows, so callers can treat foreign recipes as not found
// without leaking that they exist.
func (s *Store) recipeTitles(ctx context.Context, userID int64, ids []any) (map[int64]string, error) {
	titles := make(map[int64]string, len(ids))
	if len(ids) == 0 {
		return titles, nil
	}

	args := append([]any{userID}, ids...)
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT id, title FROM recipes WHERE user_id = ? AND id IN (%s)
	`, placeholders(len(ids))), args...)
	if err != nil {
		return nil, fmt.Errorf("query recipe titles: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var id int64
		var title string
		if err := rows.Scan(&id, &title); err != nil {
			return nil, fmt.Errorf("scan recipe title: %w", err)
		}
		if title, err = encryption.DecryptField(title); err != nil {
			return nil, fmt.Errorf("decrypt recipe title: %w", err)
		}
		titles[id] = title
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recipe titles: %w", err)
	}
	if len(titles) != len(ids) {
		return nil, sql.ErrNoRows
	}
	return titles, nil
}

// IngredientNamesForRecipe resolves ingredient IDs to the names a shopping list
// should carry, in the order they were requested. The parsed Name is preferred;
// ingredients that were never parsed fall back to their free-form line.
//
// Returns sql.ErrNoRows when the recipe is not the user's (or does not exist)
// and ErrIngredientNotInRecipe when an ID is not one of that recipe's
// ingredients, so a caller can tell "wrong recipe" from "wrong ingredient".
func (s *Store) IngredientNamesForRecipe(ctx context.Context, userID, recipeID int64, ingredientIDs []int64) ([]string, error) {
	var exists int
	if err := s.db.QueryRowContext(ctx,
		"SELECT 1 FROM recipes WHERE id = ? AND user_id = ?", recipeID, userID).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		return nil, fmt.Errorf("check recipe ownership: %w", err)
	}
	if len(ingredientIDs) == 0 {
		return []string{}, nil
	}

	args := make([]any, 0, len(ingredientIDs)+1)
	args = append(args, recipeID)
	for _, id := range ingredientIDs {
		args = append(args, id)
	}
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT id, text, name FROM recipe_ingredients WHERE recipe_id = ? AND id IN (%s)
	`, placeholders(len(ingredientIDs))), args...)
	if err != nil {
		return nil, fmt.Errorf("query recipe ingredients: %w", err)
	}
	defer rows.Close()

	byID := make(map[int64]string, len(ingredientIDs))
	for rows.Next() {
		var id int64
		var text, name string
		if err := rows.Scan(&id, &text, &name); err != nil {
			return nil, fmt.Errorf("scan recipe ingredient: %w", err)
		}
		name = strings.TrimSpace(name)
		if name == "" {
			if text, err = encryption.DecryptField(text); err != nil {
				return nil, fmt.Errorf("decrypt ingredient text: %w", err)
			}
			name = strings.TrimSpace(text)
		}
		byID[id] = name
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recipe ingredients: %w", err)
	}

	names := make([]string, 0, len(ingredientIDs))
	for _, id := range ingredientIDs {
		name, ok := byID[id]
		if !ok {
			return nil, ErrIngredientNotInRecipe
		}
		names = append(names, name)
	}
	return names, nil
}

// --- Scaling ---

// ScaleIngredients returns a copy of ings with every Quantity multiplied by
// factor. It is pure: the input slice, its elements and the stored rows are all
// left untouched, so a scaled shopping list never overwrites the base recipe.
// A factor of 0 or less is treated as 1 (no scaling) rather than producing a
// list of zero quantities.
func ScaleIngredients(ings []Ingredient, factor float64) []Ingredient {
	if factor <= 0 {
		factor = 1
	}
	out := make([]Ingredient, len(ings))
	copy(out, ings)
	for i := range out {
		out[i].Quantity = ings[i].Quantity * factor
	}
	return out
}

// --- Cook again ---

// seasonOf returns the meteorological season for t's month.
func seasonOf(t time.Time) string {
	switch t.Month() {
	case time.December, time.January, time.February:
		return "winter"
	case time.March, time.April, time.May:
		return "spring"
	case time.June, time.July, time.August:
		return "summer"
	default:
		return "autumn"
	}
}

// seasonTags maps a season to the (already normalised) tags that mean it.
// Norwegian spellings are accepted alongside English because tags are free
// text and the UI is Norwegian-first.
var seasonTags = map[string]map[string]bool{
	"winter": {"winter": true, "vinter": true},
	"spring": {"spring": true, "vår": true, "var": true},
	"summer": {"summer": true, "sommer": true},
	"autumn": {"autumn": true, "fall": true, "høst": true, "host": true},
}

// isInSeason reports whether any of the recipe's tags names the season that
// contains now.
func isInSeason(tags []string, now time.Time) bool {
	want := seasonTags[seasonOf(now)]
	for _, tag := range tags {
		if want[tag] {
			return true
		}
	}
	return false
}

// CookAgain suggests recipes worth making again. Recipes tagged with the
// current season rank above everything else; within each group the one cooked
// longest ago comes first, with never-cooked recipes treated as maximally
// overdue. Ties break on recipe ID so the order is deterministic. A limit of 0
// or less returns every candidate.
func (s *Store) CookAgain(ctx context.Context, userID int64, now time.Time, limit int) ([]Recipe, error) {
	candidates, err := s.ListRecipes(ctx, userID, TagFilter{})
	if err != nil {
		return nil, err
	}

	type scored struct {
		recipe    Recipe
		inSeason  bool
		daysSince float64
	}
	ranked := make([]scored, len(candidates))
	for i, r := range candidates {
		days := math.MaxFloat64
		if r.LastCook != nil {
			days = now.Sub(*r.LastCook).Hours() / 24
		}
		ranked[i] = scored{recipe: r, inSeason: isInSeason(r.Tags, now), daysSince: days}
	}

	sort.Slice(ranked, func(i, j int) bool {
		a, b := ranked[i], ranked[j]
		if a.inSeason != b.inSeason {
			return a.inSeason
		}
		if a.daysSince != b.daysSince {
			return a.daysSince > b.daysSince
		}
		return a.recipe.ID < b.recipe.ID
	})

	if limit > 0 && limit < len(ranked) {
		ranked = ranked[:limit]
	}
	out := make([]Recipe, len(ranked))
	for i, sc := range ranked {
		out[i] = sc.recipe
	}
	return out, nil
}

// --- scanning helpers ---

// rowScanner is satisfied by both *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanRecipe(sc rowScanner) (Recipe, error) {
	var r Recipe
	var rating sql.NullInt64
	var ratedAt, lastCooked sql.NullString
	var createdAt, updatedAt string
	if err := sc.Scan(&r.ID, &r.UserID, &r.Title, &r.Notes, &r.Servings,
		&rating, &ratedAt, &lastCooked, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Recipe{}, err
		}
		return Recipe{}, fmt.Errorf("scan recipe: %w", err)
	}

	var err error
	if r.Title, err = encryption.DecryptField(r.Title); err != nil {
		return Recipe{}, fmt.Errorf("decrypt recipe title: %w", err)
	}
	if r.Notes, err = encryption.DecryptField(r.Notes); err != nil {
		return Recipe{}, fmt.Errorf("decrypt recipe notes: %w", err)
	}
	if rating.Valid {
		v := int(rating.Int64)
		r.Rating = &v
	}
	if ratedAt.Valid && ratedAt.String != "" {
		t := parseTime(ratedAt.String)
		r.RatedAt = &t
	}
	if lastCooked.Valid && lastCooked.String != "" {
		t := parseTime(lastCooked.String)
		r.LastCook = &t
	}
	r.CreatedAt = parseTime(createdAt)
	r.UpdatedAt = parseTime(updatedAt)
	r.Ingredients = []Ingredient{}
	r.Steps = []Step{}
	r.Tags = []string{}
	return r, nil
}

// attachChildren loads ingredients, steps and tags for the given recipes in
// three queries rather than three per recipe.
func (s *Store) attachChildren(ctx context.Context, recipes []*Recipe) error {
	if len(recipes) == 0 {
		return nil
	}
	byID := make(map[int64]*Recipe, len(recipes))
	ids := make([]any, 0, len(recipes))
	for _, r := range recipes {
		byID[r.ID] = r
		ids = append(ids, r.ID)
	}
	in := placeholders(len(ids))

	ingRows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT id, recipe_id, position, text, quantity, unit, name
		FROM recipe_ingredients
		WHERE recipe_id IN (%s)
		ORDER BY recipe_id, position, id
	`, in), ids...)
	if err != nil {
		return fmt.Errorf("query recipe ingredients: %w", err)
	}
	defer ingRows.Close()
	for ingRows.Next() {
		var ing Ingredient
		if err := ingRows.Scan(&ing.ID, &ing.RecipeID, &ing.Position, &ing.Text,
			&ing.Quantity, &ing.Unit, &ing.Name); err != nil {
			return fmt.Errorf("scan recipe ingredient: %w", err)
		}
		if ing.Text, err = encryption.DecryptField(ing.Text); err != nil {
			return fmt.Errorf("decrypt ingredient text: %w", err)
		}
		if r, ok := byID[ing.RecipeID]; ok {
			r.Ingredients = append(r.Ingredients, ing)
		}
	}
	if err := ingRows.Err(); err != nil {
		return fmt.Errorf("iterate recipe ingredients: %w", err)
	}

	stepRows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT id, recipe_id, position, text, duration_seconds
		FROM recipe_steps
		WHERE recipe_id IN (%s)
		ORDER BY recipe_id, position, id
	`, in), ids...)
	if err != nil {
		return fmt.Errorf("query recipe steps: %w", err)
	}
	defer stepRows.Close()
	for stepRows.Next() {
		var step Step
		if err := stepRows.Scan(&step.ID, &step.RecipeID, &step.Position, &step.Text,
			&step.DurationSeconds); err != nil {
			return fmt.Errorf("scan recipe step: %w", err)
		}
		if step.Text, err = encryption.DecryptField(step.Text); err != nil {
			return fmt.Errorf("decrypt step text: %w", err)
		}
		if r, ok := byID[step.RecipeID]; ok {
			r.Steps = append(r.Steps, step)
		}
	}
	if err := stepRows.Err(); err != nil {
		return fmt.Errorf("iterate recipe steps: %w", err)
	}

	tagRows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT recipe_id, tag FROM recipe_tags
		WHERE recipe_id IN (%s)
		ORDER BY recipe_id, tag
	`, in), ids...)
	if err != nil {
		return fmt.Errorf("query recipe tags: %w", err)
	}
	defer tagRows.Close()
	for tagRows.Next() {
		var t Tag
		if err := tagRows.Scan(&t.RecipeID, &t.Tag); err != nil {
			return fmt.Errorf("scan recipe tag: %w", err)
		}
		if r, ok := byID[t.RecipeID]; ok {
			r.Tags = append(r.Tags, t.Tag)
		}
	}
	return tagRows.Err()
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
