// Package recipes stores personal recipes with ingredients, steps, tags,
// ratings and a cooking log, plus the meal-plan entries that schedule them.
//
// Free-form user text (recipe title and notes, ingredient lines, step text) is
// encrypted at rest via internal/encryption. Everything the store needs to
// filter, sort or compute with — tags, ratings, timestamps, ingredient
// quantities and step durations — stays plaintext, following the same split the
// training and wardrobe packages use.
package recipes

import "time"

// Recipe is one recipe owned by a user, together with its ordered ingredients
// and steps and its tag set. Title and Notes hold plaintext in memory;
// encryption happens only at the store boundary.
type Recipe struct {
	ID     int64  `json:"id"`
	UserID int64  `json:"user_id"`
	Title  string `json:"title"` // encrypted at rest
	Notes  string `json:"notes"` // encrypted at rest
	// Servings is the yield the stored ingredient quantities describe. It is
	// the denominator callers divide by to build a scaling factor; 0 means the
	// recipe does not declare a yield.
	Servings int `json:"servings"`
	// Rating is a 1-5 score, nil when the recipe has not been rated.
	Rating   *int       `json:"rating,omitempty"`
	RatedAt  *time.Time `json:"rated_at,omitempty"`
	LastCook *time.Time `json:"last_cooked_at,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	Ingredients []Ingredient `json:"ingredients"`
	Steps       []Step       `json:"steps"`
	// Tags are normalised (trimmed, lowercased) and sorted for stable output.
	Tags []string `json:"tags"`
}

// Ingredient is one line of a recipe's ingredient list.
//
// Text is the full free-form line as the user typed it ("2 dl fløte, romtemperert")
// and is encrypted. Quantity, Unit and Name are the parsed triple and stay
// plaintext: Quantity is what ScaleIngredients multiplies, and Unit/Name let a
// scaled list be re-rendered (and later matched against the grocery list)
// without decrypting every row.
type Ingredient struct {
	ID       int64   `json:"id"`
	RecipeID int64   `json:"recipe_id"`
	Position int     `json:"position"`
	Text     string  `json:"text"` // encrypted at rest
	Quantity float64 `json:"quantity"`
	Unit     string  `json:"unit"`
	Name     string  `json:"name"`
}

// Step is one instruction in a recipe's method. Text is encrypted;
// DurationSeconds stays plaintext so timers and total-time sums can be computed
// in SQL. 0 means the step has no timed duration.
type Step struct {
	ID              int64  `json:"id"`
	RecipeID        int64  `json:"recipe_id"`
	Position        int    `json:"position"`
	Text            string `json:"text"` // encrypted at rest
	DurationSeconds int    `json:"duration_seconds"`
}

// Tag is a plaintext label attached to a recipe ("dinner", "vegetarian",
// "summer"). Tags drive filtering and the in-season component of CookAgain, so
// they are deliberately not encrypted.
type Tag struct {
	RecipeID int64  `json:"recipe_id"`
	Tag      string `json:"tag"`
}

// Rating is a 1-5 star score for a recipe. It lives on the recipes row rather
// than in its own table — a recipe has exactly one rating from its owner.
type Rating struct {
	RecipeID int64     `json:"recipe_id"`
	Value    int       `json:"value"`
	RatedAt  time.Time `json:"rated_at"`
}

// Cook is one entry in the cooking log: the user made this recipe at this time.
// The log is append-only so "cook again" can rank by time since last cook.
type Cook struct {
	ID       int64     `json:"id"`
	RecipeID int64     `json:"recipe_id"`
	UserID   int64     `json:"user_id"`
	CookedAt time.Time `json:"cooked_at"`
}

// PlanEntry schedules a recipe into a meal slot on a given day. At most one
// entry exists per (user, day, slot) — the store upserts on that triple.
type PlanEntry struct {
	ID        int64     `json:"id"`
	UserID    int64     `json:"user_id"`
	RecipeID  int64     `json:"recipe_id"`
	PlanDate  string    `json:"plan_date"` // YYYY-MM-DD, local calendar day
	Slot      string    `json:"slot"`      // breakfast | lunch | dinner | snack
	CreatedAt time.Time `json:"created_at"`
	// RecipeTitle is the scheduled recipe's (decrypted) title, joined in on
	// read so a plan week can be rendered without fetching every recipe. It is
	// derived, never stored on the entry row.
	RecipeTitle string `json:"recipe_title"`
}

// TagFilter narrows a recipe listing by tags. Any matches recipes carrying at
// least one of the listed tags; All matches recipes carrying every listed tag.
// Both empty means "no tag filter". Tags are compared after normalisation, so
// callers may pass user input verbatim.
type TagFilter struct {
	Any []string `json:"any"`
	All []string `json:"all"`
}

// IsEmpty reports whether the filter would exclude nothing.
func (f TagFilter) IsEmpty() bool {
	return len(normalizeTags(f.Any)) == 0 && len(normalizeTags(f.All)) == 0
}
