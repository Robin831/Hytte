package recipes

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/grocery"
	"github.com/go-chi/chi/v5"
)

// ownerID and otherID are the two users seeded by setupTestDB. otherID is used
// to prove every route is scoped to the caller.
const (
	ownerID = int64(1)
	otherID = int64(2)
)

// testEnv wires the real router chain — RequireAuth, WithFeatures and the
// "recipes" feature gate from RegisterRoutes — around an in-memory database so
// the tests exercise the middleware that actually runs in production.
type testEnv struct {
	db     *sql.DB
	store  *Store
	router http.Handler
	tokens map[int64]string
}

func setupHandlerTest(t *testing.T) *testEnv {
	t.Helper()
	database := setupTestDB(t)

	env := &testEnv{
		db:     database,
		store:  NewStore(database),
		tokens: map[int64]string{},
	}
	for _, id := range []int64{ownerID, otherID} {
		if err := auth.SetUserFeature(database, id, "recipes", true); err != nil {
			t.Fatalf("enable recipes for user %d: %v", id, err)
		}
		token, _, err := auth.CreateSession(database, id)
		if err != nil {
			t.Fatalf("create session for user %d: %v", id, err)
		}
		env.tokens[id] = token
	}

	r := chi.NewRouter()
	r.Route("/api", func(r chi.Router) {
		r.Use(auth.RequireAuth(database))
		r.Use(auth.WithFeatures(database))
		RegisterRoutes(r, database)
	})
	env.router = r
	return env
}

// do issues a request as the given user and returns the recorded response.
// Pass an empty body for requests without one.
func (e *testEnv) do(t *testing.T, userID int64, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	req.AddCookie(&http.Cookie{Name: "session", Value: e.tokens[userID]})
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	return rec
}

type recipeEnvelope struct {
	Recipe RecipeResponse `json:"recipe"`
}

type recipeListEnvelope struct {
	Recipes []RecipeResponse `json:"recipes"`
}

func decodeRecipe(t *testing.T, rec *httptest.ResponseRecorder) RecipeResponse {
	t.Helper()
	var env recipeEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode recipe response: %v (body: %s)", err, rec.Body.String())
	}
	return env.Recipe
}

func decodeRecipes(t *testing.T, rec *httptest.ResponseRecorder) []RecipeResponse {
	t.Helper()
	var env recipeListEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode recipe list response: %v (body: %s)", err, rec.Body.String())
	}
	return env.Recipes
}

// createBody is a valid POST /api/recipes payload.
const createBody = `{
	"title": "Fiskegrateng",
	"notes": "Grandmother's version.",
	"servings": 4,
	"ingredients": [
		{"text": "400 g torsk", "quantity": 400, "unit": "g", "name": "torsk"},
		{"text": "3 dl melk", "quantity": 3, "unit": "dl", "name": "melk"}
	],
	"steps": [
		{"text": "Kok makaronien.", "duration_seconds": 480},
		{"text": "Stek i ovnen.", "duration_seconds": 1800}
	],
	"tags": ["Dinner", "fish"]
}`

func TestHandleCreateAndList(t *testing.T) {
	env := setupHandlerTest(t)

	rec := env.do(t, ownerID, http.MethodPost, "/api/recipes", createBody)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want %d (body: %s)", rec.Code, http.StatusCreated, rec.Body.String())
	}
	created := decodeRecipe(t, rec)
	if created.ID == 0 {
		t.Fatal("created recipe has no ID")
	}
	if created.Title != "Fiskegrateng" {
		t.Errorf("title = %q, want %q", created.Title, "Fiskegrateng")
	}
	if created.Servings != 4 {
		t.Errorf("servings = %d, want 4", created.Servings)
	}
	if len(created.Ingredients) != 2 || created.Ingredients[0].Name != "torsk" {
		t.Errorf("unexpected ingredients: %+v", created.Ingredients)
	}
	if len(created.Steps) != 2 || created.Steps[1].DurationSeconds != 1800 {
		t.Errorf("unexpected steps: %+v", created.Steps)
	}
	// Tags are normalised (lowercased, sorted) by the store.
	if len(created.Tags) != 2 || created.Tags[0] != "dinner" || created.Tags[1] != "fish" {
		t.Errorf("tags = %v, want [dinner fish]", created.Tags)
	}
	if created.Rating != nil || created.LastCookedAt != nil {
		t.Errorf("new recipe should have no rating or cook history: %+v", created)
	}

	rec = env.do(t, ownerID, http.MethodGet, "/api/recipes", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want %d (body: %s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	list := decodeRecipes(t, rec)
	if len(list) != 1 {
		t.Fatalf("list returned %d recipes, want 1", len(list))
	}
	if list[0].ID != created.ID {
		t.Errorf("list returned recipe %d, want %d", list[0].ID, created.ID)
	}
	if len(list[0].Ingredients) != 2 {
		t.Errorf("list entry should carry its ingredients, got %d", len(list[0].Ingredients))
	}
}

func TestHandleListFiltersByTag(t *testing.T) {
	env := setupHandlerTest(t)

	dinner := mustCreate(t, env.store, ownerID, Recipe{Title: "Dinner one", Tags: []string{"dinner", "fish"}})
	lunch := mustCreate(t, env.store, ownerID, Recipe{Title: "Lunch one", Tags: []string{"lunch"}})

	rec := env.do(t, ownerID, http.MethodGet, "/api/recipes?tag=dinner", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	list := decodeRecipes(t, rec)
	if len(list) != 1 || list[0].ID != dinner.ID {
		t.Fatalf("?tag=dinner returned %+v, want only recipe %d", list, dinner.ID)
	}

	rec = env.do(t, ownerID, http.MethodGet, "/api/recipes?tag_all=dinner&tag_all=fish", "")
	list = decodeRecipes(t, rec)
	if len(list) != 1 || list[0].ID != dinner.ID {
		t.Fatalf("tag_all returned %+v, want only recipe %d", list, dinner.ID)
	}

	rec = env.do(t, ownerID, http.MethodGet, "/api/recipes?tag=lunch", "")
	if list = decodeRecipes(t, rec); len(list) != 1 || list[0].ID != lunch.ID {
		t.Fatalf("?tag=lunch returned %+v, want only recipe %d", list, lunch.ID)
	}

	rec = env.do(t, ownerID, http.MethodGet, "/api/recipes?tag_all=dinner&tag_all=lunch", "")
	if list = decodeRecipes(t, rec); len(list) != 0 {
		t.Fatalf("tag_all with no common recipe returned %+v, want empty", list)
	}
}

func TestHandleCreateValidation(t *testing.T) {
	env := setupHandlerTest(t)

	cases := []struct {
		name string
		body string
	}{
		{"missing title", `{"title": "  ", "servings": 2}`},
		{"malformed JSON", `{"title": `},
		{"negative servings", `{"title": "X", "servings": -1}`},
		{"negative quantity", `{"title": "X", "ingredients": [{"text": "salt", "quantity": -2}]}`},
		{"negative step duration", `{"title": "X", "steps": [{"text": "Bake", "duration_seconds": -5}]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := env.do(t, ownerID, http.MethodPost, "/api/recipes", tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want %d (body: %s)", rec.Code, http.StatusBadRequest, rec.Body.String())
			}
		})
	}
}

// TestHandleCreateDropsBlankRows checks that a trailing empty ingredient or
// step row — routine in form UIs — is dropped rather than rejected or stored.
func TestHandleCreateDropsBlankRows(t *testing.T) {
	env := setupHandlerTest(t)

	body := `{"title": "Simple", "ingredients": [{"text": "salt"}, {"text": "  "}], "steps": [{"text": "Mix"}, {"text": ""}]}`
	rec := env.do(t, ownerID, http.MethodPost, "/api/recipes", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d (body: %s)", rec.Code, http.StatusCreated, rec.Body.String())
	}
	created := decodeRecipe(t, rec)
	if len(created.Ingredients) != 1 || len(created.Steps) != 1 {
		t.Fatalf("blank rows not dropped: %d ingredients, %d steps", len(created.Ingredients), len(created.Steps))
	}
}

func TestHandleGetUpdateDelete(t *testing.T) {
	env := setupHandlerTest(t)

	created := decodeRecipe(t, env.do(t, ownerID, http.MethodPost, "/api/recipes", createBody))
	path := fmt.Sprintf("/api/recipes/%d", created.ID)

	rec := env.do(t, ownerID, http.MethodGet, path, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	if got := decodeRecipe(t, rec); got.Title != "Fiskegrateng" || len(got.Steps) != 2 {
		t.Errorf("unexpected recipe: %+v", got)
	}

	update := `{"title": "Fiskegrateng v2", "notes": "Less dill.", "servings": 6,
		"ingredients": [{"text": "500 g torsk", "quantity": 500, "unit": "g", "name": "torsk"}],
		"steps": [{"text": "Stek.", "duration_seconds": 1200}], "tags": ["Dinner"]}`
	rec = env.do(t, ownerID, http.MethodPut, path, update)
	if rec.Code != http.StatusOK {
		t.Fatalf("update status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	updated := decodeRecipe(t, rec)
	if updated.Title != "Fiskegrateng v2" || updated.Servings != 6 {
		t.Errorf("update did not apply: %+v", updated)
	}
	if len(updated.Ingredients) != 1 || len(updated.Steps) != 1 || len(updated.Tags) != 1 {
		t.Errorf("child lists not replaced: %+v", updated)
	}
	if updated.ID != created.ID {
		t.Errorf("update changed the ID: %d != %d", updated.ID, created.ID)
	}

	rec = env.do(t, ownerID, http.MethodDelete, path, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	if rec = env.do(t, ownerID, http.MethodGet, path, ""); rec.Code != http.StatusNotFound {
		t.Errorf("get after delete status = %d, want 404", rec.Code)
	}
	if rec = env.do(t, ownerID, http.MethodDelete, path, ""); rec.Code != http.StatusNotFound {
		t.Errorf("second delete status = %d, want 404", rec.Code)
	}
}

func TestHandleInvalidAndMissingIDs(t *testing.T) {
	env := setupHandlerTest(t)

	cases := []struct {
		name   string
		method string
		path   string
		body   string
		want   int
	}{
		{"non-numeric get", http.MethodGet, "/api/recipes/abc", "", http.StatusBadRequest},
		{"zero id", http.MethodGet, "/api/recipes/0", "", http.StatusBadRequest},
		{"non-numeric rating", http.MethodPost, "/api/recipes/abc/rating", `{"rating":4}`, http.StatusBadRequest},
		{"missing recipe get", http.MethodGet, "/api/recipes/9999", "", http.StatusNotFound},
		{"missing recipe update", http.MethodPut, "/api/recipes/9999", `{"title":"X"}`, http.StatusNotFound},
		{"missing recipe delete", http.MethodDelete, "/api/recipes/9999", "", http.StatusNotFound},
		{"missing recipe rating", http.MethodPost, "/api/recipes/9999/rating", `{"rating":4}`, http.StatusNotFound},
		{"missing recipe cooked", http.MethodPost, "/api/recipes/9999/cooked", "", http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := env.do(t, ownerID, tc.method, tc.path, tc.body)
			if rec.Code != tc.want {
				t.Errorf("status = %d, want %d (body: %s)", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

func TestHandleRate(t *testing.T) {
	env := setupHandlerTest(t)

	created := decodeRecipe(t, env.do(t, ownerID, http.MethodPost, "/api/recipes", createBody))
	path := fmt.Sprintf("/api/recipes/%d/rating", created.ID)

	rec := env.do(t, ownerID, http.MethodPost, path, `{"rating": 4}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("rate status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	rated := decodeRecipe(t, rec)
	if rated.Rating == nil || *rated.Rating != 4 {
		t.Fatalf("rating = %v, want 4", rated.Rating)
	}
	if rated.RatedAt == nil {
		t.Error("rated_at should be set alongside a rating")
	}

	// 0 clears the rating.
	rec = env.do(t, ownerID, http.MethodPost, path, `{"rating": 0}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("clear rating status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	if cleared := decodeRecipe(t, rec); cleared.Rating != nil {
		t.Errorf("rating = %v, want nil after clearing", *cleared.Rating)
	}

	for _, tc := range []struct {
		name string
		body string
	}{
		{"too high", `{"rating": 6}`},
		{"negative", `{"rating": -1}`},
		{"missing field", `{}`},
		{"malformed", `{"rating":`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := env.do(t, ownerID, http.MethodPost, path, tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400 (body: %s)", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestHandleCooked(t *testing.T) {
	env := setupHandlerTest(t)

	created := decodeRecipe(t, env.do(t, ownerID, http.MethodPost, "/api/recipes", createBody))
	path := fmt.Sprintf("/api/recipes/%d/cooked", created.ID)

	before := time.Now().UTC().Add(-time.Second)
	rec := env.do(t, ownerID, http.MethodPost, path, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("cooked status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	cooked := decodeRecipe(t, rec)
	if cooked.LastCookedAt == nil {
		t.Fatal("last_cooked_at should be set after logging a cook")
	}
	if cooked.LastCookedAt.Before(before) {
		t.Errorf("last_cooked_at = %v, want at/after %v", cooked.LastCookedAt, before)
	}

	// An explicit older timestamp is logged, but last_cooked_at keeps the most
	// recent cook rather than the most recently logged one.
	rec = env.do(t, ownerID, http.MethodPost, path, `{"cooked_at": "2024-01-02T10:00:00Z"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("backfill status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	backfilled := decodeRecipe(t, rec)
	if backfilled.LastCookedAt == nil || !backfilled.LastCookedAt.Equal(*cooked.LastCookedAt) {
		t.Errorf("last_cooked_at = %v, want unchanged %v", backfilled.LastCookedAt, cooked.LastCookedAt)
	}

	cooks, err := env.store.ListCooks(context.Background(), ownerID, created.ID)
	if err != nil {
		t.Fatalf("list cooks: %v", err)
	}
	if len(cooks) != 2 {
		t.Fatalf("cooking log has %d entries, want 2", len(cooks))
	}

	if rec := env.do(t, ownerID, http.MethodPost, path, `{"cooked_at": "not-a-time"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("malformed timestamp status = %d, want 400", rec.Code)
	}
}

func TestHandleCookAgain(t *testing.T) {
	env := setupHandlerTest(t)

	now := time.Now()
	seasonal := ""
	for tag := range seasonTags[seasonOf(now)] {
		seasonal = tag
		break
	}

	// Cooked yesterday but tagged for the current season — should outrank the
	// never-cooked recipe that isn't in season.
	inSeason := mustCreate(t, env.store, ownerID, Recipe{Title: "In season", Tags: []string{seasonal}})
	if err := env.store.RecordCook(context.Background(), ownerID, inSeason.ID, now.Add(-24*time.Hour)); err != nil {
		t.Fatalf("record cook: %v", err)
	}
	neverCooked := mustCreate(t, env.store, ownerID, Recipe{Title: "Never cooked", Tags: []string{"anytime"}})

	rec := env.do(t, ownerID, http.MethodGet, "/api/recipes/cook-again", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("cook-again status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	list := decodeRecipes(t, rec)
	if len(list) != 2 {
		t.Fatalf("cook-again returned %d recipes, want 2", len(list))
	}
	if list[0].ID != inSeason.ID || list[1].ID != neverCooked.ID {
		t.Errorf("cook-again order = [%d %d], want [%d %d]", list[0].ID, list[1].ID, inSeason.ID, neverCooked.ID)
	}

	rec = env.do(t, ownerID, http.MethodGet, "/api/recipes/cook-again?limit=1", "")
	if list = decodeRecipes(t, rec); len(list) != 1 || list[0].ID != inSeason.ID {
		t.Fatalf("limit=1 returned %+v, want only recipe %d", list, inSeason.ID)
	}

	rec = env.do(t, ownerID, http.MethodGet, "/api/recipes/cook-again?limit=0", "")
	if list = decodeRecipes(t, rec); len(list) != 2 {
		t.Fatalf("limit=0 returned %d recipes, want all 2", len(list))
	}

	for _, raw := range []string{"abc", "-1", "1000"} {
		rec := env.do(t, ownerID, http.MethodGet, "/api/recipes/cook-again?limit="+raw, "")
		if rec.Code != http.StatusBadRequest {
			t.Errorf("limit=%s status = %d, want 400", raw, rec.Code)
		}
	}
}

// TestUserScopingIsolation asserts a second user can neither see nor touch the
// owner's recipes, and gets 404 (not 403) so the API never confirms that
// someone else's recipe exists.
func TestUserScopingIsolation(t *testing.T) {
	env := setupHandlerTest(t)

	created := decodeRecipe(t, env.do(t, ownerID, http.MethodPost, "/api/recipes", createBody))
	id := created.ID

	if list := decodeRecipes(t, env.do(t, otherID, http.MethodGet, "/api/recipes", "")); len(list) != 0 {
		t.Errorf("other user's list = %+v, want empty", list)
	}
	if list := decodeRecipes(t, env.do(t, otherID, http.MethodGet, "/api/recipes/cook-again", "")); len(list) != 0 {
		t.Errorf("other user's cook-again = %+v, want empty", list)
	}

	cases := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"get", http.MethodGet, fmt.Sprintf("/api/recipes/%d", id), ""},
		{"update", http.MethodPut, fmt.Sprintf("/api/recipes/%d", id), `{"title":"Stolen"}`},
		{"delete", http.MethodDelete, fmt.Sprintf("/api/recipes/%d", id), ""},
		{"rating", http.MethodPost, fmt.Sprintf("/api/recipes/%d/rating", id), `{"rating":1}`},
		{"cooked", http.MethodPost, fmt.Sprintf("/api/recipes/%d/cooked", id), ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := env.do(t, otherID, tc.method, tc.path, tc.body)
			if rec.Code != http.StatusNotFound {
				t.Errorf("status = %d, want 404 (body: %s)", rec.Code, rec.Body.String())
			}
		})
	}

	// The owner's recipe is untouched by all of the above.
	owned := decodeRecipe(t, env.do(t, ownerID, http.MethodGet, fmt.Sprintf("/api/recipes/%d", id), ""))
	if owned.Title != "Fiskegrateng" || owned.Rating != nil || owned.LastCookedAt != nil {
		t.Errorf("owner's recipe was modified by the other user: %+v", owned)
	}
}

// TestFeatureFlagGating asserts every route is behind the "recipes" flag: a
// non-admin without it gets 403 (the sibling convention from RequireFeature),
// while an admin bypasses the check entirely.
func TestFeatureFlagGating(t *testing.T) {
	env := setupHandlerTest(t)

	created := mustCreate(t, env.store, otherID, Recipe{Title: "Admin visible"})
	if err := auth.SetUserFeature(env.db, otherID, "recipes", false); err != nil {
		t.Fatalf("disable recipes: %v", err)
	}

	routes := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/api/recipes", ""},
		{http.MethodPost, "/api/recipes", createBody},
		{http.MethodGet, "/api/recipes/cook-again", ""},
		{http.MethodGet, fmt.Sprintf("/api/recipes/%d", created.ID), ""},
		{http.MethodPut, fmt.Sprintf("/api/recipes/%d", created.ID), `{"title":"X"}`},
		{http.MethodDelete, fmt.Sprintf("/api/recipes/%d", created.ID), ""},
		{http.MethodPost, fmt.Sprintf("/api/recipes/%d/rating", created.ID), `{"rating":3}`},
		{http.MethodPost, fmt.Sprintf("/api/recipes/%d/cooked", created.ID), ""},
		{http.MethodGet, "/api/recipes/plan", ""},
		{http.MethodPut, "/api/recipes/plan",
			fmt.Sprintf(`{"entries":[{"date":"2024-01-03","slot":"dinner","recipe_id":%d}]}`, created.ID)},
		{http.MethodDelete, "/api/recipes/plan?date=2024-01-03&slot=dinner", ""},
		{http.MethodPost, fmt.Sprintf("/api/recipes/%d/grocery", created.ID), `{"ingredient_ids":[1]}`},
	}
	for _, rt := range routes {
		rec := env.do(t, otherID, rt.method, rt.path, rt.body)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s %s without the flag: status = %d, want 403 (body: %s)",
				rt.method, rt.path, rec.Code, rec.Body.String())
		}
	}

	// Admins bypass the feature check even with the flag off.
	if _, err := env.db.Exec("UPDATE users SET is_admin = 1 WHERE id = ?", otherID); err != nil {
		t.Fatalf("promote user to admin: %v", err)
	}
	rec := env.do(t, otherID, http.MethodGet, "/api/recipes", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("admin list status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	if list := decodeRecipes(t, rec); len(list) != 1 || list[0].ID != created.ID {
		t.Errorf("admin list = %+v, want the admin's own recipe %d", list, created.ID)
	}
}

// TestUnauthenticatedRequestsRejected guards the auth layer in front of the
// group: no session cookie means no access to any recipe route.
func TestUnauthenticatedRequestsRejected(t *testing.T) {
	env := setupHandlerTest(t)

	req := httptest.NewRequest(http.MethodGet, "/api/recipes", nil)
	rec := httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 (body: %s)", rec.Code, rec.Body.String())
	}
}

// --- meal plan ---

// 2024-01-01 is a Monday, so the fixed week below runs 2024-01-01 (Mon) to
// 2024-01-07 (Sun) and the following week starts on 2024-01-08.
const (
	weekMonday    = "2024-01-01"
	weekWednesday = "2024-01-03"
	weekSunday    = "2024-01-07"
	nextMonday    = "2024-01-08"
)

func decodePlanWeek(t *testing.T, rec *httptest.ResponseRecorder) PlanWeekResponse {
	t.Helper()
	var week PlanWeekResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &week); err != nil {
		t.Fatalf("decode plan week response: %v (body: %s)", err, rec.Body.String())
	}
	return week
}

func decodePlanEntries(t *testing.T, rec *httptest.ResponseRecorder) []PlanEntryResponse {
	t.Helper()
	var env struct {
		Entries []PlanEntryResponse `json:"entries"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode plan entries response: %v (body: %s)", err, rec.Body.String())
	}
	return env.Entries
}

// putPlan schedules one recipe and returns the recorded response.
func (e *testEnv) putPlan(t *testing.T, userID int64, date, slot string, recipeID int64) *httptest.ResponseRecorder {
	t.Helper()
	body := fmt.Sprintf(`{"entries":[{"date":%q,"slot":%q,"recipe_id":%d}]}`, date, slot, recipeID)
	return e.do(t, userID, http.MethodPut, "/api/recipes/plan", body)
}

// getPlanWeek fetches the week containing the given day.
func (e *testEnv) getPlanWeek(t *testing.T, userID int64, week string) PlanWeekResponse {
	t.Helper()
	rec := e.do(t, userID, http.MethodGet, "/api/recipes/plan?week="+week, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get plan status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	return decodePlanWeek(t, rec)
}

// TestHandlePlanRoundTrip walks a plan entry through PUT, GET and DELETE and
// checks it lands on — and leaves — the right day.
func TestHandlePlanRoundTrip(t *testing.T) {
	env := setupHandlerTest(t)
	recipe := mustCreate(t, env.store, ownerID, Recipe{Title: "Fiskegrateng"})

	// An untouched week comes back as seven empty days.
	week := env.getPlanWeek(t, ownerID, weekWednesday)
	if week.WeekStart != weekMonday || week.WeekEnd != weekSunday {
		t.Fatalf("week = %s..%s, want %s..%s", week.WeekStart, week.WeekEnd, weekMonday, weekSunday)
	}
	if len(week.Days) != 7 {
		t.Fatalf("week has %d days, want 7: %+v", len(week.Days), week.Days)
	}
	if entries, ok := week.Days[weekWednesday]; !ok || len(entries) != 0 {
		t.Fatalf("empty week day %s = %+v (present: %t), want an empty list", weekWednesday, entries, ok)
	}

	rec := env.putPlan(t, ownerID, weekWednesday, "dinner", recipe.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("put plan status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	stored := decodePlanEntries(t, rec)
	if len(stored) != 1 || stored[0].ID == 0 {
		t.Fatalf("put returned %+v, want one entry with an ID", stored)
	}
	if stored[0].Date != weekWednesday || stored[0].Slot != "dinner" || stored[0].RecipeID != recipe.ID {
		t.Errorf("stored entry = %+v, want %s/dinner/%d", stored[0], weekWednesday, recipe.ID)
	}
	if stored[0].RecipeTitle != "Fiskegrateng" {
		t.Errorf("recipe_title = %q, want %q", stored[0].RecipeTitle, "Fiskegrateng")
	}

	week = env.getPlanWeek(t, ownerID, weekWednesday)
	day := week.Days[weekWednesday]
	if len(day) != 1 || day[0].ID != stored[0].ID {
		t.Fatalf("%s = %+v, want the stored entry %d", weekWednesday, day, stored[0].ID)
	}
	if day[0].RecipeTitle != "Fiskegrateng" {
		t.Errorf("recipe_title = %q, want the joined title", day[0].RecipeTitle)
	}
	if len(week.Days[weekMonday]) != 0 {
		t.Errorf("%s = %+v, want no entries", weekMonday, week.Days[weekMonday])
	}

	rec = env.do(t, ownerID, http.MethodDelete, "/api/recipes/plan?date="+weekWednesday+"&slot=dinner", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("delete plan status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	if week = env.getPlanWeek(t, ownerID, weekWednesday); len(week.Days[weekWednesday]) != 0 {
		t.Errorf("%s after delete = %+v, want empty", weekWednesday, week.Days[weekWednesday])
	}

	// Deleting an already-empty slot is a 404, not a silent success.
	rec = env.do(t, ownerID, http.MethodDelete, "/api/recipes/plan?date="+weekWednesday+"&slot=dinner", "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("second delete status = %d, want 404 (body: %s)", rec.Code, rec.Body.String())
	}
}

// TestHandlePlanDefaultsToCurrentWeek checks that GET without ?week= returns
// the week containing today, keyed by the local calendar day.
func TestHandlePlanDefaultsToCurrentWeek(t *testing.T) {
	env := setupHandlerTest(t)
	recipe := mustCreate(t, env.store, ownerID, Recipe{Title: "Today's dinner"})

	today := time.Now().Format(PlanDateLayout)
	if rec := env.putPlan(t, ownerID, today, "dinner", recipe.ID); rec.Code != http.StatusOK {
		t.Fatalf("put plan status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}

	rec := env.do(t, ownerID, http.MethodGet, "/api/recipes/plan", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get plan status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	week := decodePlanWeek(t, rec)
	if len(week.Days[today]) != 1 {
		t.Fatalf("today (%s) = %+v, want the entry just scheduled", today, week.Days[today])
	}
	if week.WeekStart > today || week.WeekEnd < today {
		t.Errorf("week %s..%s does not contain today (%s)", week.WeekStart, week.WeekEnd, today)
	}
}

// TestHandlePlanWeekBoundary asserts entries are filed by ISO week: the Sunday
// of one week and the Monday of the next never show up together.
func TestHandlePlanWeekBoundary(t *testing.T) {
	env := setupHandlerTest(t)
	sundayRecipe := mustCreate(t, env.store, ownerID, Recipe{Title: "Sunday roast"})
	mondayRecipe := mustCreate(t, env.store, ownerID, Recipe{Title: "Monday soup"})

	if rec := env.putPlan(t, ownerID, weekSunday, "dinner", sundayRecipe.ID); rec.Code != http.StatusOK {
		t.Fatalf("put sunday status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	if rec := env.putPlan(t, ownerID, nextMonday, "dinner", mondayRecipe.ID); rec.Code != http.StatusOK {
		t.Fatalf("put monday status = %d (body: %s)", rec.Code, rec.Body.String())
	}

	first := env.getPlanWeek(t, ownerID, weekWednesday)
	if first.WeekEnd != weekSunday {
		t.Fatalf("first week ends %s, want %s", first.WeekEnd, weekSunday)
	}
	if len(first.Days[weekSunday]) != 1 || first.Days[weekSunday][0].RecipeID != sundayRecipe.ID {
		t.Errorf("%s = %+v, want the Sunday recipe", weekSunday, first.Days[weekSunday])
	}
	if _, leaked := first.Days[nextMonday]; leaked {
		t.Errorf("first week leaked the next Monday: %+v", first.Days)
	}

	second := env.getPlanWeek(t, ownerID, nextMonday)
	if second.WeekStart != nextMonday {
		t.Fatalf("second week starts %s, want %s", second.WeekStart, nextMonday)
	}
	if len(second.Days[nextMonday]) != 1 || second.Days[nextMonday][0].RecipeID != mondayRecipe.ID {
		t.Errorf("%s = %+v, want the Monday recipe", nextMonday, second.Days[nextMonday])
	}
	if _, leaked := second.Days[weekSunday]; leaked {
		t.Errorf("second week leaked the previous Sunday: %+v", second.Days)
	}
}

// TestHandlePlanPutIsIdempotent checks that re-sending the same slot replaces
// it in place instead of stacking up rows, and that a different recipe in the
// same slot takes it over.
func TestHandlePlanPutIsIdempotent(t *testing.T) {
	env := setupHandlerTest(t)
	first := mustCreate(t, env.store, ownerID, Recipe{Title: "First"})
	second := mustCreate(t, env.store, ownerID, Recipe{Title: "Second"})

	rec := env.putPlan(t, ownerID, weekWednesday, "dinner", first.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("first put status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	original := decodePlanEntries(t, rec)[0]

	rec = env.putPlan(t, ownerID, weekWednesday, "dinner", first.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("repeat put status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	if repeated := decodePlanEntries(t, rec)[0]; repeated.ID != original.ID {
		t.Errorf("repeat put created entry %d, want the existing %d", repeated.ID, original.ID)
	}

	week := env.getPlanWeek(t, ownerID, weekWednesday)
	if len(week.Days[weekWednesday]) != 1 {
		t.Fatalf("%s = %+v, want exactly one entry after a repeated PUT", weekWednesday, week.Days[weekWednesday])
	}

	// The same slot with another recipe replaces rather than appends.
	if rec = env.putPlan(t, ownerID, weekWednesday, "dinner", second.ID); rec.Code != http.StatusOK {
		t.Fatalf("replace put status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	week = env.getPlanWeek(t, ownerID, weekWednesday)
	if len(week.Days[weekWednesday]) != 1 || week.Days[weekWednesday][0].RecipeID != second.ID {
		t.Fatalf("%s = %+v, want only the second recipe", weekWednesday, week.Days[weekWednesday])
	}

	// A different slot on the same day is its own entry.
	if rec = env.putPlan(t, ownerID, weekWednesday, "lunch", first.ID); rec.Code != http.StatusOK {
		t.Fatalf("lunch put status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	week = env.getPlanWeek(t, ownerID, weekWednesday)
	day := week.Days[weekWednesday]
	if len(day) != 2 {
		t.Fatalf("%s = %+v, want lunch and dinner", weekWednesday, day)
	}
	// Days come back in the order the slots are eaten in.
	if day[0].Slot != "lunch" || day[1].Slot != "dinner" {
		t.Errorf("slot order = [%s %s], want [lunch dinner]", day[0].Slot, day[1].Slot)
	}
}

// TestHandlePlanPutMultipleEntries checks a whole week can be planned in one
// request.
func TestHandlePlanPutMultipleEntries(t *testing.T) {
	env := setupHandlerTest(t)
	recipe := mustCreate(t, env.store, ownerID, Recipe{Title: "Batch"})

	body := fmt.Sprintf(`{"entries":[
		{"date":%q,"slot":"dinner","recipe_id":%d},
		{"date":%q,"slot":"Breakfast","recipe_id":%d},
		{"date":%q,"slot":"dinner","recipe_id":%d}
	]}`, weekMonday, recipe.ID, weekWednesday, recipe.ID, weekSunday, recipe.ID)

	rec := env.do(t, ownerID, http.MethodPut, "/api/recipes/plan", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("put status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	if stored := decodePlanEntries(t, rec); len(stored) != 3 {
		t.Fatalf("put returned %d entries, want 3", len(stored))
	}

	week := env.getPlanWeek(t, ownerID, weekMonday)
	for _, date := range []string{weekMonday, weekWednesday, weekSunday} {
		if len(week.Days[date]) != 1 {
			t.Errorf("%s = %+v, want one entry", date, week.Days[date])
		}
	}
	// Slot names are normalised on the way in.
	if got := week.Days[weekWednesday][0].Slot; got != "breakfast" {
		t.Errorf("slot = %q, want %q", got, "breakfast")
	}
}

// TestHandlePlanValidation covers the malformed and out-of-range inputs the
// plan routes must reject before touching the database.
func TestHandlePlanValidation(t *testing.T) {
	env := setupHandlerTest(t)
	recipe := mustCreate(t, env.store, ownerID, Recipe{Title: "Valid"})
	foreign := mustCreate(t, env.store, otherID, Recipe{Title: "Someone else's"})

	cases := []struct {
		name   string
		method string
		path   string
		body   string
		want   int
	}{
		{"malformed JSON", http.MethodPut, "/api/recipes/plan", `{"entries":`, http.StatusBadRequest},
		{"no entries", http.MethodPut, "/api/recipes/plan", `{"entries":[]}`, http.StatusBadRequest},
		{"bad date", http.MethodPut, "/api/recipes/plan",
			fmt.Sprintf(`{"entries":[{"date":"03-01-2024","slot":"dinner","recipe_id":%d}]}`, recipe.ID), http.StatusBadRequest},
		{"impossible date", http.MethodPut, "/api/recipes/plan",
			fmt.Sprintf(`{"entries":[{"date":"2024-02-30","slot":"dinner","recipe_id":%d}]}`, recipe.ID), http.StatusBadRequest},
		{"bad slot", http.MethodPut, "/api/recipes/plan",
			fmt.Sprintf(`{"entries":[{"date":%q,"slot":"brunch","recipe_id":%d}]}`, weekWednesday, recipe.ID), http.StatusBadRequest},
		{"missing recipe", http.MethodPut, "/api/recipes/plan",
			fmt.Sprintf(`{"entries":[{"date":%q,"slot":"dinner","recipe_id":9999}]}`, weekWednesday), http.StatusNotFound},
		{"foreign recipe", http.MethodPut, "/api/recipes/plan",
			fmt.Sprintf(`{"entries":[{"date":%q,"slot":"dinner","recipe_id":%d}]}`, weekWednesday, foreign.ID), http.StatusNotFound},
		{"bad week param", http.MethodGet, "/api/recipes/plan?week=2024", "", http.StatusBadRequest},
		{"delete without params", http.MethodDelete, "/api/recipes/plan", "", http.StatusBadRequest},
		{"delete without slot", http.MethodDelete, "/api/recipes/plan?date=" + weekWednesday, "", http.StatusBadRequest},
		{"delete bad date", http.MethodDelete, "/api/recipes/plan?date=nope&slot=dinner", "", http.StatusBadRequest},
		{"delete bad slot", http.MethodDelete, "/api/recipes/plan?date=" + weekWednesday + "&slot=brunch", "", http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := env.do(t, ownerID, tc.method, tc.path, tc.body)
			if rec.Code != tc.want {
				t.Errorf("status = %d, want %d (body: %s)", rec.Code, tc.want, rec.Body.String())
			}
		})
	}

	// A batch containing one bad entry writes none of them.
	body := fmt.Sprintf(`{"entries":[
		{"date":%q,"slot":"dinner","recipe_id":%d},
		{"date":%q,"slot":"brunch","recipe_id":%d}
	]}`, weekMonday, recipe.ID, weekWednesday, recipe.ID)
	if rec := env.do(t, ownerID, http.MethodPut, "/api/recipes/plan", body); rec.Code != http.StatusBadRequest {
		t.Fatalf("mixed batch status = %d, want 400 (body: %s)", rec.Code, rec.Body.String())
	}
	if week := env.getPlanWeek(t, ownerID, weekMonday); len(week.Days[weekMonday]) != 0 {
		t.Errorf("%s = %+v, want nothing written by the rejected batch", weekMonday, week.Days[weekMonday])
	}
}

// TestHandlePlanUserIsolation asserts one user's plan is invisible and
// untouchable from another account.
func TestHandlePlanUserIsolation(t *testing.T) {
	env := setupHandlerTest(t)
	recipe := mustCreate(t, env.store, ownerID, Recipe{Title: "Owner's dinner"})

	if rec := env.putPlan(t, ownerID, weekWednesday, "dinner", recipe.ID); rec.Code != http.StatusOK {
		t.Fatalf("put status = %d (body: %s)", rec.Code, rec.Body.String())
	}

	if week := env.getPlanWeek(t, otherID, weekWednesday); len(week.Days[weekWednesday]) != 0 {
		t.Errorf("other user sees %+v, want an empty plan", week.Days[weekWednesday])
	}

	rec := env.do(t, otherID, http.MethodDelete, "/api/recipes/plan?date="+weekWednesday+"&slot=dinner", "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("other user's delete status = %d, want 404 (body: %s)", rec.Code, rec.Body.String())
	}
	if week := env.getPlanWeek(t, ownerID, weekWednesday); len(week.Days[weekWednesday]) != 1 {
		t.Errorf("owner's entry = %+v, want it untouched", week.Days[weekWednesday])
	}
}

// --- grocery push ---

// TestHandleGroceryPush pushes recipe ingredients onto the grocery list and
// checks the added/skipped split, including de-duplication against items that
// are already on the list.
func TestHandleGroceryPush(t *testing.T) {
	env := setupHandlerTest(t)

	created := decodeRecipe(t, env.do(t, ownerID, http.MethodPost, "/api/recipes", createBody))
	torsk, melk := created.Ingredients[0].ID, created.Ingredients[1].ID

	// "Torsk" is already on the list under a different casing.
	if _, err := grocery.Add(env.db, grocery.GroceryItem{
		HouseholdID:  ownerID,
		Content:      "Torsk",
		OriginalText: "Torsk",
		AddedBy:      ownerID,
	}); err != nil {
		t.Fatalf("seed grocery item: %v", err)
	}

	path := fmt.Sprintf("/api/recipes/%d/grocery", created.ID)
	rec := env.do(t, ownerID, http.MethodPost, path, fmt.Sprintf(`{"ingredient_ids":[%d,%d]}`, torsk, melk))
	if rec.Code != http.StatusOK {
		t.Fatalf("push status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	var push GroceryPushResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &push); err != nil {
		t.Fatalf("decode push response: %v (body: %s)", err, rec.Body.String())
	}
	if push.Added != 1 || push.Skipped != 1 {
		t.Errorf("added/skipped = %d/%d, want 1/1", push.Added, push.Skipped)
	}
	if len(push.Items) != 1 || push.Items[0].Content != "melk" {
		t.Errorf("items = %+v, want only melk", push.Items)
	}

	items, err := grocery.ListByHousehold(env.db, ownerID)
	if err != nil {
		t.Fatalf("list grocery items: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("grocery list has %d items, want 2 (no duplicate torsk): %+v", len(items), items)
	}

	// Pushing the same ingredients again adds nothing.
	rec = env.do(t, ownerID, http.MethodPost, path, fmt.Sprintf(`{"ingredient_ids":[%d,%d]}`, torsk, melk))
	if rec.Code != http.StatusOK {
		t.Fatalf("second push status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	push = GroceryPushResponse{}
	if err := json.Unmarshal(rec.Body.Bytes(), &push); err != nil {
		t.Fatalf("decode second push response: %v", err)
	}
	if push.Added != 0 || push.Skipped != 2 {
		t.Errorf("second push added/skipped = %d/%d, want 0/2", push.Added, push.Skipped)
	}
	if items, err = grocery.ListByHousehold(env.db, ownerID); err != nil || len(items) != 2 {
		t.Errorf("grocery list = %+v (err %v), want the same 2 items", items, err)
	}
}

// TestHandleGroceryPushUsesIngredientLineWhenUnparsed checks the fallback for
// ingredients the client never parsed into a name.
func TestHandleGroceryPushUsesIngredientLineWhenUnparsed(t *testing.T) {
	env := setupHandlerTest(t)

	created := decodeRecipe(t, env.do(t, ownerID, http.MethodPost, "/api/recipes",
		`{"title":"Unparsed","ingredients":[{"text":"En klype salt"}]}`))

	rec := env.do(t, ownerID, http.MethodPost, fmt.Sprintf("/api/recipes/%d/grocery", created.ID),
		fmt.Sprintf(`{"ingredient_ids":[%d]}`, created.Ingredients[0].ID))
	if rec.Code != http.StatusOK {
		t.Fatalf("push status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}

	items, err := grocery.ListByHousehold(env.db, ownerID)
	if err != nil {
		t.Fatalf("list grocery items: %v", err)
	}
	if len(items) != 1 || items[0].Content != "En klype salt" {
		t.Errorf("grocery list = %+v, want the free-form ingredient line", items)
	}
}

// TestHandleGroceryPushValidation checks the ownership and membership rules:
// an ingredient from another recipe is a bad request, someone else's recipe is
// simply not found, and neither writes to the list.
func TestHandleGroceryPushValidation(t *testing.T) {
	env := setupHandlerTest(t)

	created := decodeRecipe(t, env.do(t, ownerID, http.MethodPost, "/api/recipes", createBody))
	otherRecipe := decodeRecipe(t, env.do(t, ownerID, http.MethodPost, "/api/recipes",
		`{"title":"Another","ingredients":[{"text":"1 løk","name":"løk"}]}`))
	foreign := mustCreate(t, env.store, otherID, Recipe{
		Title:       "Someone else's",
		Ingredients: []Ingredient{{Text: "1 dl fløte", Name: "fløte"}},
	})

	path := fmt.Sprintf("/api/recipes/%d/grocery", created.ID)
	cases := []struct {
		name   string
		caller int64
		path   string
		body   string
		want   int
	}{
		{"malformed JSON", ownerID, path, `{"ingredient_ids":`, http.StatusBadRequest},
		{"no ingredients", ownerID, path, `{"ingredient_ids":[]}`, http.StatusBadRequest},
		{"invalid recipe id", ownerID, "/api/recipes/abc/grocery", `{"ingredient_ids":[1]}`, http.StatusBadRequest},
		{"unknown ingredient", ownerID, path, `{"ingredient_ids":[999999]}`, http.StatusBadRequest},
		{"ingredient from another recipe", ownerID, path,
			fmt.Sprintf(`{"ingredient_ids":[%d]}`, otherRecipe.Ingredients[0].ID), http.StatusBadRequest},
		{"missing recipe", ownerID, "/api/recipes/9999/grocery", `{"ingredient_ids":[1]}`, http.StatusNotFound},
		{"another user's recipe", otherID, path,
			fmt.Sprintf(`{"ingredient_ids":[%d]}`, created.Ingredients[0].ID), http.StatusNotFound},
		{"owner reaching into a foreign recipe", ownerID,
			fmt.Sprintf("/api/recipes/%d/grocery", foreign.ID),
			fmt.Sprintf(`{"ingredient_ids":[%d]}`, foreign.Ingredients[0].ID), http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := env.do(t, tc.caller, http.MethodPost, tc.path, tc.body)
			if rec.Code != tc.want {
				t.Errorf("status = %d, want %d (body: %s)", rec.Code, tc.want, rec.Body.String())
			}
		})
	}

	for _, userID := range []int64{ownerID, otherID} {
		items, err := grocery.ListByHousehold(env.db, userID)
		if err != nil {
			t.Fatalf("list grocery items for %d: %v", userID, err)
		}
		if len(items) != 0 {
			t.Errorf("user %d's grocery list = %+v, want nothing written by rejected pushes", userID, items)
		}
	}
}
