package recipes

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/db"
	"github.com/Robin831/Hytte/internal/encryption"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	t.Setenv("ENCRYPTION_KEY", "test-key-for-recipes-tests")
	encryption.ResetEncryptionKey()
	t.Cleanup(func() { encryption.ResetEncryptionKey() })

	database, err := db.Init(":memory:")
	if err != nil {
		t.Fatalf("init test db: %v", err)
	}
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	t.Cleanup(func() { database.Close() })

	_, err = database.Exec(`INSERT INTO users (id, email, name, picture, google_id, created_at) VALUES
		(1, 'owner@example.com', 'Owner', '', 'g1', ''),
		(2, 'other@example.com', 'Other', '', 'g2', '')`)
	if err != nil {
		t.Fatalf("insert test users: %v", err)
	}
	return database
}

// sampleRecipe is a fully populated recipe used across the tests.
func sampleRecipe() Recipe {
	return Recipe{
		Title:    "Fiskegrateng",
		Notes:    "Grandmother's version — plenty of dill.",
		Servings: 4,
		Ingredients: []Ingredient{
			{Text: "400 g torsk, i terninger", Quantity: 400, Unit: "g", Name: "torsk"},
			{Text: "3 dl melk", Quantity: 3, Unit: "dl", Name: "melk"},
			{Text: "1 ts salt", Quantity: 1, Unit: "ts", Name: "salt"},
		},
		Steps: []Step{
			{Text: "Kok makaronien.", DurationSeconds: 480},
			{Text: "Rør sammen hvit saus.", DurationSeconds: 300},
			{Text: "Stek i ovnen.", DurationSeconds: 1800},
		},
		Tags: []string{"Dinner", "fish", "winter"},
	}
}

func mustCreate(t *testing.T, s *Store, userID int64, r Recipe) Recipe {
	t.Helper()
	created, err := s.CreateRecipe(context.Background(), userID, r)
	if err != nil {
		t.Fatalf("create recipe: %v", err)
	}
	return created
}

func TestCreateAndGetRecipe_EncryptionRoundTrip(t *testing.T) {
	database := setupTestDB(t)
	s := NewStore(database)
	ctx := context.Background()

	in := sampleRecipe()
	created := mustCreate(t, s, 1, in)
	if created.ID == 0 {
		t.Fatal("expected a generated recipe ID")
	}
	for i, ing := range created.Ingredients {
		if ing.ID == 0 || ing.RecipeID != created.ID || ing.Position != i {
			t.Fatalf("ingredient %d not populated: %+v", i, ing)
		}
	}
	for i, st := range created.Steps {
		if st.ID == 0 || st.RecipeID != created.ID || st.Position != i {
			t.Fatalf("step %d not populated: %+v", i, st)
		}
	}

	got, err := s.GetRecipe(ctx, 1, created.ID)
	if err != nil {
		t.Fatalf("get recipe: %v", err)
	}
	if got.Title != in.Title {
		t.Errorf("title round-trip: got %q, want %q", got.Title, in.Title)
	}
	if got.Notes != in.Notes {
		t.Errorf("notes round-trip: got %q, want %q", got.Notes, in.Notes)
	}
	if got.Servings != in.Servings {
		t.Errorf("servings: got %d, want %d", got.Servings, in.Servings)
	}
	if len(got.Ingredients) != len(in.Ingredients) {
		t.Fatalf("ingredients: got %d, want %d", len(got.Ingredients), len(in.Ingredients))
	}
	for i, ing := range got.Ingredients {
		want := in.Ingredients[i]
		if ing.Text != want.Text || ing.Quantity != want.Quantity || ing.Unit != want.Unit || ing.Name != want.Name {
			t.Errorf("ingredient %d round-trip: got %+v, want %+v", i, ing, want)
		}
	}
	if len(got.Steps) != len(in.Steps) {
		t.Fatalf("steps: got %d, want %d", len(got.Steps), len(in.Steps))
	}
	for i, st := range got.Steps {
		want := in.Steps[i]
		if st.Text != want.Text || st.DurationSeconds != want.DurationSeconds {
			t.Errorf("step %d round-trip: got %+v, want %+v", i, st, want)
		}
	}
	// Tags are normalised: trimmed, lowercased and sorted.
	wantTags := []string{"dinner", "fish", "winter"}
	if strings.Join(got.Tags, ",") != strings.Join(wantTags, ",") {
		t.Errorf("tags: got %v, want %v", got.Tags, wantTags)
	}

	// The raw columns must not hold the plaintext.
	var rawTitle, rawNotes string
	if err := database.QueryRow("SELECT title, notes FROM recipes WHERE id = ?", created.ID).
		Scan(&rawTitle, &rawNotes); err != nil {
		t.Fatalf("read raw recipe row: %v", err)
	}
	if rawTitle == in.Title || strings.Contains(rawTitle, in.Title) {
		t.Errorf("recipe title stored in plaintext: %q", rawTitle)
	}
	if rawNotes == in.Notes || strings.Contains(rawNotes, in.Notes) {
		t.Errorf("recipe notes stored in plaintext: %q", rawNotes)
	}

	var rawIngText, rawUnit, rawName string
	var rawQty float64
	if err := database.QueryRow(
		"SELECT text, quantity, unit, name FROM recipe_ingredients WHERE recipe_id = ? AND position = 0",
		created.ID).Scan(&rawIngText, &rawQty, &rawUnit, &rawName); err != nil {
		t.Fatalf("read raw ingredient row: %v", err)
	}
	if strings.Contains(rawIngText, in.Ingredients[0].Text) {
		t.Errorf("ingredient text stored in plaintext: %q", rawIngText)
	}
	// Quantity/unit/name stay plaintext — the store scales and filters on them.
	if rawQty != 400 || rawUnit != "g" || rawName != "torsk" {
		t.Errorf("ingredient scalars should be plaintext: got %v %q %q", rawQty, rawUnit, rawName)
	}

	var rawStepText string
	var rawDuration int
	if err := database.QueryRow(
		"SELECT text, duration_seconds FROM recipe_steps WHERE recipe_id = ? AND position = 0",
		created.ID).Scan(&rawStepText, &rawDuration); err != nil {
		t.Fatalf("read raw step row: %v", err)
	}
	if strings.Contains(rawStepText, in.Steps[0].Text) {
		t.Errorf("step text stored in plaintext: %q", rawStepText)
	}
	if rawDuration != 480 {
		t.Errorf("step duration should be plaintext: got %d", rawDuration)
	}

	// Tags are deliberately plaintext so they can be filtered in SQL.
	var rawTag string
	if err := database.QueryRow(
		"SELECT tag FROM recipe_tags WHERE recipe_id = ? ORDER BY tag LIMIT 1", created.ID).Scan(&rawTag); err != nil {
		t.Fatalf("read raw tag row: %v", err)
	}
	if rawTag != "dinner" {
		t.Errorf("tag should be stored plaintext and normalised: got %q", rawTag)
	}
}

func TestGetRecipe_OtherUserIsNotFound(t *testing.T) {
	s := NewStore(setupTestDB(t))
	created := mustCreate(t, s, 1, sampleRecipe())

	if _, err := s.GetRecipe(context.Background(), 2, created.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected sql.ErrNoRows for another user, got %v", err)
	}
}

func TestUpdateRecipe_ReplacesChildren(t *testing.T) {
	s := NewStore(setupTestDB(t))
	ctx := context.Background()
	created := mustCreate(t, s, 1, sampleRecipe())

	created.Title = "Fiskegrateng v2"
	created.Notes = ""
	created.Servings = 6
	created.Ingredients = []Ingredient{
		{Text: "500 g sei", Quantity: 500, Unit: "g", Name: "sei"},
	}
	created.Steps = []Step{{Text: "Bare stek det.", DurationSeconds: 900}}
	created.Tags = []string{"dinner", "quick"}

	if _, err := s.UpdateRecipe(ctx, 1, created); err != nil {
		t.Fatalf("update recipe: %v", err)
	}

	got, err := s.GetRecipe(ctx, 1, created.ID)
	if err != nil {
		t.Fatalf("get recipe: %v", err)
	}
	if got.Title != "Fiskegrateng v2" || got.Notes != "" || got.Servings != 6 {
		t.Errorf("scalar fields not updated: %+v", got)
	}
	if len(got.Ingredients) != 1 || got.Ingredients[0].Name != "sei" || got.Ingredients[0].Position != 0 {
		t.Errorf("ingredients not replaced: %+v", got.Ingredients)
	}
	if len(got.Steps) != 1 || got.Steps[0].Text != "Bare stek det." {
		t.Errorf("steps not replaced: %+v", got.Steps)
	}
	if strings.Join(got.Tags, ",") != "dinner,quick" {
		t.Errorf("tags not replaced: %v", got.Tags)
	}

	// The old child rows are gone, not merely orphaned.
	var count int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM recipe_ingredients WHERE recipe_id = ?", created.ID).Scan(&count); err != nil {
		t.Fatalf("count ingredients: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 ingredient row after update, got %d", count)
	}
}

func TestUpdateRecipe_PreservesRatingAndCookingHistory(t *testing.T) {
	s := NewStore(setupTestDB(t))
	ctx := context.Background()
	created := mustCreate(t, s, 1, sampleRecipe())

	cookedAt := time.Date(2026, 5, 2, 17, 30, 0, 0, time.UTC)
	if err := s.SetRating(ctx, 1, created.ID, 5); err != nil {
		t.Fatalf("set rating: %v", err)
	}
	if err := s.RecordCook(ctx, 1, created.ID, cookedAt); err != nil {
		t.Fatalf("record cook: %v", err)
	}

	// A caller editing the recipe sends a struct with no rating/cook metadata.
	edit := created
	edit.Title = "Fiskegrateng, justert"
	edit.Rating = nil
	edit.LastCook = nil

	updated, err := s.UpdateRecipe(ctx, 1, edit)
	if err != nil {
		t.Fatalf("update recipe: %v", err)
	}
	if updated.Rating == nil || *updated.Rating != 5 {
		t.Errorf("update should return the stored rating, got %v", updated.Rating)
	}
	if updated.LastCook == nil || !updated.LastCook.Equal(cookedAt) {
		t.Errorf("update should return the stored last cook, got %v", updated.LastCook)
	}

	got, err := s.GetRecipe(ctx, 1, created.ID)
	if err != nil {
		t.Fatalf("get recipe: %v", err)
	}
	if got.Rating == nil || *got.Rating != 5 {
		t.Errorf("rating should survive an update, got %v", got.Rating)
	}
	if got.LastCook == nil || !got.LastCook.Equal(cookedAt) {
		t.Errorf("last cook should survive an update, got %v", got.LastCook)
	}
}

func TestUpdateRecipe_OtherUserIsNotFound(t *testing.T) {
	s := NewStore(setupTestDB(t))
	created := mustCreate(t, s, 1, sampleRecipe())

	created.Title = "Hijacked"
	if _, err := s.UpdateRecipe(context.Background(), 2, created); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected sql.ErrNoRows updating another user's recipe, got %v", err)
	}
}

func TestDeleteRecipe_CascadesChildren(t *testing.T) {
	s := NewStore(setupTestDB(t))
	ctx := context.Background()
	created := mustCreate(t, s, 1, sampleRecipe())
	if err := s.RecordCook(ctx, 1, created.ID, time.Now()); err != nil {
		t.Fatalf("record cook: %v", err)
	}

	if err := s.DeleteRecipe(ctx, 2, created.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected sql.ErrNoRows deleting another user's recipe, got %v", err)
	}
	if err := s.DeleteRecipe(ctx, 1, created.ID); err != nil {
		t.Fatalf("delete recipe: %v", err)
	}

	for _, table := range []string{"recipe_ingredients", "recipe_steps", "recipe_tags", "recipe_cooks"} {
		var count int
		if err := s.db.QueryRow("SELECT COUNT(*) FROM "+table+" WHERE recipe_id = ?", created.ID).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != 0 {
			t.Errorf("%s rows not cascaded: %d remain", table, count)
		}
	}
}

func TestSetRating(t *testing.T) {
	s := NewStore(setupTestDB(t))
	ctx := context.Background()
	created := mustCreate(t, s, 1, sampleRecipe())

	if created.Rating != nil {
		t.Fatalf("new recipe should be unrated, got %v", *created.Rating)
	}

	if err := s.SetRating(ctx, 1, created.ID, 4); err != nil {
		t.Fatalf("set rating: %v", err)
	}
	got, err := s.GetRecipe(ctx, 1, created.ID)
	if err != nil {
		t.Fatalf("get recipe: %v", err)
	}
	if got.Rating == nil || *got.Rating != 4 {
		t.Fatalf("expected rating 4, got %v", got.Rating)
	}
	if got.RatedAt == nil || got.RatedAt.IsZero() {
		t.Error("expected rated_at to be set alongside the rating")
	}

	// 0 clears the rating.
	if err := s.SetRating(ctx, 1, created.ID, 0); err != nil {
		t.Fatalf("clear rating: %v", err)
	}
	got, err = s.GetRecipe(ctx, 1, created.ID)
	if err != nil {
		t.Fatalf("get recipe: %v", err)
	}
	if got.Rating != nil {
		t.Errorf("expected rating cleared, got %v", *got.Rating)
	}

	for _, bad := range []int{-1, 6, 100} {
		if err := s.SetRating(ctx, 1, created.ID, bad); !errors.Is(err, ErrInvalidRating) {
			t.Errorf("rating %d: expected ErrInvalidRating, got %v", bad, err)
		}
	}
	if err := s.SetRating(ctx, 2, created.ID, 3); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected sql.ErrNoRows rating another user's recipe, got %v", err)
	}
}

func TestRecordCook_KeepsMostRecentTimestamp(t *testing.T) {
	s := NewStore(setupTestDB(t))
	ctx := context.Background()
	created := mustCreate(t, s, 1, sampleRecipe())

	recent := time.Date(2026, 6, 1, 18, 0, 0, 0, time.UTC)
	older := time.Date(2025, 12, 24, 16, 0, 0, 0, time.UTC)

	if err := s.RecordCook(ctx, 1, created.ID, recent); err != nil {
		t.Fatalf("record recent cook: %v", err)
	}
	if err := s.RecordCook(ctx, 1, created.ID, older); err != nil {
		t.Fatalf("record older cook: %v", err)
	}

	got, err := s.GetRecipe(ctx, 1, created.ID)
	if err != nil {
		t.Fatalf("get recipe: %v", err)
	}
	if got.LastCook == nil || !got.LastCook.Equal(recent) {
		t.Errorf("last_cooked_at should stay at the most recent cook: got %v, want %v", got.LastCook, recent)
	}

	cooks, err := s.ListCooks(ctx, 1, created.ID)
	if err != nil {
		t.Fatalf("list cooks: %v", err)
	}
	if len(cooks) != 2 {
		t.Fatalf("expected 2 log entries, got %d", len(cooks))
	}
	if !cooks[0].CookedAt.Equal(recent) || !cooks[1].CookedAt.Equal(older) {
		t.Errorf("cooking log should be newest first: %+v", cooks)
	}

	if err := s.RecordCook(ctx, 2, created.ID, recent); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected sql.ErrNoRows recording a cook for another user's recipe, got %v", err)
	}
}

func TestScaleIngredients_PureAndLeavesRowsUntouched(t *testing.T) {
	s := NewStore(setupTestDB(t))
	ctx := context.Background()
	created := mustCreate(t, s, 1, sampleRecipe())

	base, err := s.GetRecipe(ctx, 1, created.ID)
	if err != nil {
		t.Fatalf("get recipe: %v", err)
	}

	scaled := ScaleIngredients(base.Ingredients, 1.5)
	want := []float64{600, 4.5, 1.5}
	for i, ing := range scaled {
		if ing.Quantity != want[i] {
			t.Errorf("scaled ingredient %d: got %v, want %v", i, ing.Quantity, want[i])
		}
		// Everything except the quantity is copied verbatim.
		if ing.ID != base.Ingredients[i].ID || ing.Text != base.Ingredients[i].Text ||
			ing.Unit != base.Ingredients[i].Unit || ing.Name != base.Ingredients[i].Name {
			t.Errorf("scaled ingredient %d changed a non-quantity field: %+v", i, ing)
		}
	}

	// The input slice is untouched.
	original := []float64{400, 3, 1}
	for i, ing := range base.Ingredients {
		if ing.Quantity != original[i] {
			t.Errorf("input ingredient %d mutated: got %v, want %v", i, ing.Quantity, original[i])
		}
	}

	// And so are the stored rows.
	reread, err := s.GetRecipe(ctx, 1, created.ID)
	if err != nil {
		t.Fatalf("re-read recipe: %v", err)
	}
	for i, ing := range reread.Ingredients {
		if ing.Quantity != original[i] {
			t.Errorf("stored ingredient %d changed after scaling: got %v, want %v", i, ing.Quantity, original[i])
		}
	}

	// A non-positive factor is a no-op rather than zeroing the list.
	for _, factor := range []float64{0, -2} {
		noop := ScaleIngredients(base.Ingredients, factor)
		for i, ing := range noop {
			if ing.Quantity != original[i] {
				t.Errorf("factor %v should be a no-op, ingredient %d got %v", factor, i, ing.Quantity)
			}
		}
	}

	if got := ScaleIngredients(nil, 2); len(got) != 0 {
		t.Errorf("scaling nil should return an empty slice, got %v", got)
	}
}

func TestListRecipes_TagFiltering(t *testing.T) {
	s := NewStore(setupTestDB(t))
	ctx := context.Background()

	stew := mustCreate(t, s, 1, Recipe{Title: "Lapskaus", Tags: []string{"dinner", "winter", "beef"}})
	salad := mustCreate(t, s, 1, Recipe{Title: "Sommersalat", Tags: []string{"dinner", "summer", "vegetarian"}})
	bread := mustCreate(t, s, 1, Recipe{Title: "Grovbrød", Tags: []string{"baking"}})
	// Another user's recipe carrying the same tags must never leak.
	mustCreate(t, s, 2, Recipe{Title: "Andres lapskaus", Tags: []string{"dinner", "winter", "beef"}})

	ids := func(rs []Recipe) map[int64]bool {
		out := make(map[int64]bool, len(rs))
		for _, r := range rs {
			out[r.ID] = true
		}
		return out
	}

	cases := []struct {
		name   string
		filter TagFilter
		want   []int64
	}{
		{"no filter returns all of the user's recipes", TagFilter{}, []int64{stew.ID, salad.ID, bread.ID}},
		{"any matches either tag", TagFilter{Any: []string{"winter", "baking"}}, []int64{stew.ID, bread.ID}},
		{"any is case-insensitive", TagFilter{Any: []string{"  WINTER "}}, []int64{stew.ID}},
		{"all requires every tag", TagFilter{All: []string{"dinner", "winter"}}, []int64{stew.ID}},
		{"all with an absent tag matches nothing", TagFilter{All: []string{"dinner", "dessert"}}, nil},
		{"any and all combine", TagFilter{Any: []string{"summer", "winter"}, All: []string{"dinner", "vegetarian"}}, []int64{salad.ID}},
		{"unknown tag matches nothing", TagFilter{Any: []string{"nonexistent"}}, nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := s.ListRecipes(ctx, 1, tc.filter)
			if err != nil {
				t.Fatalf("list recipes: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %d recipes, want %d (%+v)", len(got), len(tc.want), got)
			}
			gotIDs := ids(got)
			for _, id := range tc.want {
				if !gotIDs[id] {
					t.Errorf("recipe %d missing from result", id)
				}
			}
		})
	}

	// The other user sees only their own recipe.
	others, err := s.ListRecipes(ctx, 2, TagFilter{Any: []string{"winter"}})
	if err != nil {
		t.Fatalf("list recipes for user 2: %v", err)
	}
	if len(others) != 1 || others[0].Title != "Andres lapskaus" {
		t.Fatalf("user 2 should see exactly their own recipe, got %+v", others)
	}
}

func TestListRecipes_LoadsChildrenPerRecipe(t *testing.T) {
	s := NewStore(setupTestDB(t))
	ctx := context.Background()

	a := mustCreate(t, s, 1, sampleRecipe())
	b := mustCreate(t, s, 1, Recipe{
		Title:       "Havregrøt",
		Ingredients: []Ingredient{{Text: "1 dl havregryn", Quantity: 1, Unit: "dl", Name: "havregryn"}},
		Steps:       []Step{{Text: "Kok i 5 minutter.", DurationSeconds: 300}},
		Tags:        []string{"breakfast"},
	})

	list, err := s.ListRecipes(ctx, 1, TagFilter{})
	if err != nil {
		t.Fatalf("list recipes: %v", err)
	}
	byID := map[int64]Recipe{}
	for _, r := range list {
		byID[r.ID] = r
	}
	if got := byID[a.ID]; len(got.Ingredients) != 3 || len(got.Steps) != 3 || len(got.Tags) != 3 {
		t.Errorf("recipe A children mis-assigned: %d ingredients, %d steps, %d tags",
			len(got.Ingredients), len(got.Steps), len(got.Tags))
	}
	if got := byID[b.ID]; len(got.Ingredients) != 1 || len(got.Steps) != 1 || len(got.Tags) != 1 {
		t.Errorf("recipe B children mis-assigned: %d ingredients, %d steps, %d tags",
			len(got.Ingredients), len(got.Steps), len(got.Tags))
	}
	if got := byID[b.ID]; got.Ingredients[0].Name != "havregryn" || got.Steps[0].Text != "Kok i 5 minutter." {
		t.Errorf("recipe B children decrypted incorrectly: %+v", got)
	}
}

func TestSeasonOf(t *testing.T) {
	cases := map[time.Month]string{
		time.January: "winter", time.February: "winter", time.December: "winter",
		time.March: "spring", time.April: "spring", time.May: "spring",
		time.June: "summer", time.July: "summer", time.August: "summer",
		time.September: "autumn", time.October: "autumn", time.November: "autumn",
	}
	for month, want := range cases {
		got := seasonOf(time.Date(2026, month, 15, 12, 0, 0, 0, time.UTC))
		if got != want {
			t.Errorf("%v: got season %q, want %q", month, got, want)
		}
	}
}

func TestCookAgain_Ranking(t *testing.T) {
	s := NewStore(setupTestDB(t))
	ctx := context.Background()
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC) // summer

	inSeasonRecent := mustCreate(t, s, 1, Recipe{Title: "Grillet laks", Tags: []string{"summer"}})
	outOfSeasonOld := mustCreate(t, s, 1, Recipe{Title: "Lapskaus", Tags: []string{"winter"}})
	inSeasonNever := mustCreate(t, s, 1, Recipe{Title: "Rekesalat", Tags: []string{"sommer"}})
	untaggedMiddle := mustCreate(t, s, 1, Recipe{Title: "Pasta", Tags: []string{"quick"}})
	// Another user's in-season recipe must not appear.
	mustCreate(t, s, 2, Recipe{Title: "Andres grillmat", Tags: []string{"summer"}})

	if err := s.RecordCook(ctx, 1, inSeasonRecent.ID, now.AddDate(0, 0, -5)); err != nil {
		t.Fatalf("record cook: %v", err)
	}
	if err := s.RecordCook(ctx, 1, outOfSeasonOld.ID, now.AddDate(0, 0, -100)); err != nil {
		t.Fatalf("record cook: %v", err)
	}
	if err := s.RecordCook(ctx, 1, untaggedMiddle.ID, now.AddDate(0, 0, -50)); err != nil {
		t.Fatalf("record cook: %v", err)
	}

	got, err := s.CookAgain(ctx, 1, now, 0)
	if err != nil {
		t.Fatalf("cook again: %v", err)
	}
	want := []int64{inSeasonNever.ID, inSeasonRecent.ID, outOfSeasonOld.ID, untaggedMiddle.ID}
	if len(got) != len(want) {
		t.Fatalf("got %d suggestions, want %d", len(got), len(want))
	}
	for i, id := range want {
		if got[i].ID != id {
			t.Fatalf("position %d: got recipe %d (%q), want %d", i, got[i].ID, got[i].Title, id)
		}
	}

	limited, err := s.CookAgain(ctx, 1, now, 2)
	if err != nil {
		t.Fatalf("cook again with limit: %v", err)
	}
	if len(limited) != 2 || limited[0].ID != inSeasonNever.ID || limited[1].ID != inSeasonRecent.ID {
		t.Fatalf("limit not respected: %+v", limited)
	}
}

func TestCookAgain_SeasonFollowsTheClock(t *testing.T) {
	s := NewStore(setupTestDB(t))
	ctx := context.Background()

	summerDish := mustCreate(t, s, 1, Recipe{Title: "Grillet laks", Tags: []string{"summer"}})
	winterDish := mustCreate(t, s, 1, Recipe{Title: "Lapskaus", Tags: []string{"vinter"}})

	inJuly, err := s.CookAgain(ctx, 1, time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC), 1)
	if err != nil {
		t.Fatalf("cook again in July: %v", err)
	}
	if len(inJuly) != 1 || inJuly[0].ID != summerDish.ID {
		t.Errorf("July should surface the summer dish, got %+v", inJuly)
	}

	inJanuary, err := s.CookAgain(ctx, 1, time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC), 1)
	if err != nil {
		t.Fatalf("cook again in January: %v", err)
	}
	if len(inJanuary) != 1 || inJanuary[0].ID != winterDish.ID {
		t.Errorf("January should surface the winter dish, got %+v", inJanuary)
	}
}

func TestNormalizeTagsAndFilterIsEmpty(t *testing.T) {
	got := normalizeTags([]string{" Dinner ", "dinner", "", "  ", "Winter"})
	if strings.Join(got, ",") != "dinner,winter" {
		t.Errorf("normalizeTags: got %v", got)
	}
	if !(TagFilter{}).IsEmpty() {
		t.Error("zero TagFilter should be empty")
	}
	if !(TagFilter{Any: []string{"  "}}).IsEmpty() {
		t.Error("whitespace-only tags should count as empty")
	}
	if (TagFilter{All: []string{"dinner"}}).IsEmpty() {
		t.Error("filter with a real tag should not be empty")
	}
}
