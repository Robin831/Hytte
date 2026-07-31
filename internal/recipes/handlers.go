package recipes

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/grocery"
	"github.com/Robin831/Hytte/internal/training"
	"github.com/go-chi/chi/v5"
)

// defaultCookAgainLimit is how many suggestions GET /api/recipes/cook-again
// returns when the caller does not ask for a specific number.
const defaultCookAgainLimit = 5

// maxCookAgainLimit caps the ?limit= query parameter so a stray large value
// cannot ask the server to rank and serialise an unbounded list.
const maxCookAgainLimit = 100

// --- DTOs ---

// RecipeResponse is the JSON shape the frontend consumes for a single recipe.
// It deliberately omits user_id: every route is already scoped to the caller,
// so the owner is implicit. Nullable fields are emitted as null rather than
// dropped so the client can rely on the keys being present.
type RecipeResponse struct {
	ID           int64                `json:"id"`
	Title        string               `json:"title"`
	Notes        string               `json:"notes"`
	Servings     int                  `json:"servings"`
	Rating       *int                 `json:"rating"`
	RatedAt      *time.Time           `json:"rated_at"`
	LastCookedAt *time.Time           `json:"last_cooked_at"`
	CreatedAt    time.Time            `json:"created_at"`
	UpdatedAt    time.Time            `json:"updated_at"`
	Ingredients  []IngredientResponse `json:"ingredients"`
	Steps        []StepResponse       `json:"steps"`
	Tags         []string             `json:"tags"`
}

// IngredientResponse is one ingredient line of a RecipeResponse.
type IngredientResponse struct {
	ID       int64   `json:"id"`
	Position int     `json:"position"`
	Text     string  `json:"text"`
	Quantity float64 `json:"quantity"`
	Unit     string  `json:"unit"`
	Name     string  `json:"name"`
}

// StepResponse is one method step of a RecipeResponse.
type StepResponse struct {
	ID              int64  `json:"id"`
	Position        int    `json:"position"`
	Text            string `json:"text"`
	DurationSeconds int    `json:"duration_seconds"`
}

// CreateRecipeRequest is the body of POST /api/recipes. Ingredient and step
// order is taken from the array order — the store rewrites Position from it.
type CreateRecipeRequest struct {
	Title       string              `json:"title"`
	Notes       string              `json:"notes"`
	Servings    int                 `json:"servings"`
	Ingredients []IngredientRequest `json:"ingredients"`
	Steps       []StepRequest       `json:"steps"`
	Tags        []string            `json:"tags"`
}

// UpdateRecipeRequest is the body of PUT /api/recipes/{id}. PUT replaces the
// whole recipe — including its ingredient, step and tag lists — so the shape is
// identical to create.
type UpdateRecipeRequest = CreateRecipeRequest

// IngredientRequest is one submitted ingredient line. Text is the free-form
// line the user typed; Quantity/Unit/Name are the parsed triple the client
// sends alongside it so scaling and grocery matching stay possible without
// re-parsing server-side.
type IngredientRequest struct {
	Text     string  `json:"text"`
	Quantity float64 `json:"quantity"`
	Unit     string  `json:"unit"`
	Name     string  `json:"name"`
}

// StepRequest is one submitted method step.
type StepRequest struct {
	Text            string `json:"text"`
	DurationSeconds int    `json:"duration_seconds"`
}

// RateRecipeRequest is the body of POST /api/recipes/{id}/rating. Rating is a
// pointer so an omitted field is rejected rather than silently read as 0,
// which would clear an existing rating.
type RateRecipeRequest struct {
	Rating *int `json:"rating"`
}

// CookedRequest is the body of POST /api/recipes/{id}/cooked. CookedAt is
// optional — omit it (or send an empty body) to log the cook as happening now.
type CookedRequest struct {
	CookedAt *time.Time `json:"cooked_at"`
}

// PlanEntryResponse is one scheduled meal. The day it falls on is the key it
// is filed under in PlanWeekResponse.Days and is repeated here so an entry
// stays self-describing once the client pulls it out of the map.
type PlanEntryResponse struct {
	ID          int64  `json:"id"`
	Date        string `json:"date"`
	Slot        string `json:"slot"`
	RecipeID    int64  `json:"recipe_id"`
	RecipeTitle string `json:"recipe_title"`
}

// PlanWeekResponse is the body of GET /api/recipes/plan: one ISO week, keyed
// by calendar day. Days always holds all seven keys — an empty day is an empty
// array rather than a missing key — so the client can render the grid straight
// from the response.
type PlanWeekResponse struct {
	WeekStart string                         `json:"week_start"`
	WeekEnd   string                         `json:"week_end"`
	Days      map[string][]PlanEntryResponse `json:"days"`
}

// PlanEntryRequest is one entry of a PUT /api/recipes/plan body.
type PlanEntryRequest struct {
	Date     string `json:"date"` // YYYY-MM-DD
	Slot     string `json:"slot"` // breakfast | lunch | dinner | snack
	RecipeID int64  `json:"recipe_id"`
}

// UpdatePlanRequest is the body of PUT /api/recipes/plan. Several entries may
// be sent at once so a whole week can be planned in one round trip; each one
// replaces whatever occupied its day and slot.
type UpdatePlanRequest struct {
	Entries []PlanEntryRequest `json:"entries"`
}

// GroceryPushRequest is the body of POST /api/recipes/{id}/grocery: the subset
// of the recipe's ingredients to put on the grocery list.
type GroceryPushRequest struct {
	IngredientIDs []int64 `json:"ingredient_ids"`
}

// GroceryPushResponse reports what the push did. Added counts the items that
// reached the list; Skipped counts the requested ingredients that were already
// on it (or repeated within the request), which the grocery store de-duplicates
// case-insensitively.
type GroceryPushResponse struct {
	Added   int                   `json:"added"`
	Skipped int                   `json:"skipped"`
	Items   []grocery.GroceryItem `json:"items"`
}

// ImportRequest is the body of POST /api/recipes/import: the page to read a
// recipe from.
type ImportRequest struct {
	URL string `json:"url"`
}

// --- Handlers ---

// Handlers serves the recipes REST API on top of the recipe Store. It keeps
// the database handle as well: pushing ingredients onto the grocery list goes
// through the grocery package, which owns that table.
type Handlers struct {
	store *Store
	db    *sql.DB
}

// NewHandlers builds the recipe handlers around a database handle.
func NewHandlers(db *sql.DB) *Handlers {
	return &Handlers{store: NewStore(db), db: db}
}

// RegisterRoutes mounts every /api/recipes route on r behind the "recipes"
// feature gate. Production and tests both call this, so the middleware chain
// under test is the one that actually runs.
func RegisterRoutes(r chi.Router, db *sql.DB) {
	h := NewHandlers(db)
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireFeature(db, "recipes"))
		r.Get("/recipes", h.HandleList)
		r.Post("/recipes", h.HandleCreate)
		// Registered before /recipes/{id} so the literal segment wins even if
		// chi's static-over-wildcard preference ever changes.
		r.Get("/recipes/cook-again", h.HandleCookAgain)
		r.Post("/recipes/import", h.HandleImport)
		r.Get("/recipes/plan", h.HandlePlanGet)
		r.Put("/recipes/plan", h.HandlePlanPut)
		r.Delete("/recipes/plan", h.HandlePlanDelete)
		r.Get("/recipes/{id}", h.HandleGet)
		r.Put("/recipes/{id}", h.HandleUpdate)
		r.Delete("/recipes/{id}", h.HandleDelete)
		r.Post("/recipes/{id}/rating", h.HandleRate)
		r.Post("/recipes/{id}/cooked", h.HandleCooked)
		r.Post("/recipes/{id}/grocery", h.HandleGroceryPush)
	})
}

// HandleList returns the caller's recipes, newest first. Optional repeated
// query parameters narrow the list by tag: ?tag=dinner&tag=fish matches
// recipes carrying at least one of them, ?tag_all=dinner&tag_all=fish matches
// only recipes carrying all of them.
func (h *Handlers) HandleList(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())

	query := r.URL.Query()
	filter := TagFilter{Any: query["tag"], All: query["tag_all"]}

	recipes, err := h.store.ListRecipes(r.Context(), user.ID, filter)
	if err != nil {
		log.Printf("recipes: list: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to list recipes")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"recipes": toRecipeResponses(recipes)})
}

// HandleCreate stores a new recipe with its ingredients, steps and tags.
func (h *Handlers) HandleCreate(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())

	var body CreateRecipeRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	recipe, errMsg := recipeFromRequest(body)
	if errMsg != "" {
		writeError(w, http.StatusBadRequest, errMsg)
		return
	}

	created, err := h.store.CreateRecipe(r.Context(), user.ID, recipe)
	if err != nil {
		log.Printf("recipes: create: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to create recipe")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"recipe": toRecipeResponse(created)})
}

// HandleGet returns one recipe. A recipe owned by another user is reported as
// missing rather than forbidden so the API does not confirm it exists.
func (h *Handlers) HandleGet(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())

	id, ok := urlID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid recipe ID")
		return
	}

	recipe, err := h.store.GetRecipe(r.Context(), user.ID, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "recipe not found")
			return
		}
		log.Printf("recipes: get: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to load recipe")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"recipe": toRecipeResponse(recipe)})
}

// HandleUpdate replaces a recipe's editable fields and its child lists. Rating
// and cooking history are owned by their own endpoints and are left alone.
func (h *Handlers) HandleUpdate(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())

	id, ok := urlID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid recipe ID")
		return
	}

	var body UpdateRecipeRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	recipe, errMsg := recipeFromRequest(body)
	if errMsg != "" {
		writeError(w, http.StatusBadRequest, errMsg)
		return
	}
	recipe.ID = id

	updated, err := h.store.UpdateRecipe(r.Context(), user.ID, recipe)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "recipe not found")
			return
		}
		log.Printf("recipes: update: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to update recipe")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"recipe": toRecipeResponse(updated)})
}

// HandleDelete removes a recipe; its ingredients, steps, tags, cooking log and
// meal-plan entries cascade.
func (h *Handlers) HandleDelete(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())

	id, ok := urlID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid recipe ID")
		return
	}

	if err := h.store.DeleteRecipe(r.Context(), user.ID, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "recipe not found")
			return
		}
		log.Printf("recipes: delete: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to delete recipe")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// HandleRate sets a recipe's 1-5 star rating. A rating of 0 clears it.
func (h *Handlers) HandleRate(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())

	id, ok := urlID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid recipe ID")
		return
	}

	var body RateRecipeRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if body.Rating == nil {
		writeError(w, http.StatusBadRequest, "rating is required")
		return
	}

	if err := h.store.SetRating(r.Context(), user.ID, id, *body.Rating); err != nil {
		switch {
		case errors.Is(err, ErrInvalidRating):
			writeError(w, http.StatusBadRequest, "rating must be between 0 and 5")
		case errors.Is(err, sql.ErrNoRows):
			writeError(w, http.StatusNotFound, "recipe not found")
		default:
			log.Printf("recipes: set rating: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to set rating")
		}
		return
	}

	h.respondWithRecipe(w, r, user.ID, id, "failed to load recipe")
}

// HandleCooked appends an entry to a recipe's cooking log. The client may send
// the moment it was cooked (to backfill); an omitted or empty body logs now.
func (h *Handlers) HandleCooked(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())

	id, ok := urlID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid recipe ID")
		return
	}

	var body CookedRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	cookedAt := time.Now().UTC()
	if body.CookedAt != nil {
		cookedAt = body.CookedAt.UTC()
	}

	if err := h.store.RecordCook(r.Context(), user.ID, id, cookedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "recipe not found")
			return
		}
		log.Printf("recipes: record cook: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to record cook")
		return
	}

	h.respondWithRecipe(w, r, user.ID, id, "failed to load recipe")
}

// HandleCookAgain suggests recipes worth making again: in-season tags first,
// then whatever was cooked longest ago. ?limit=0 returns every candidate.
func (h *Handlers) HandleCookAgain(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())

	limit := defaultCookAgainLimit
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 || parsed > maxCookAgainLimit {
			writeError(w, http.StatusBadRequest, "invalid limit")
			return
		}
		limit = parsed
	}

	recipes, err := h.store.CookAgain(r.Context(), user.ID, time.Now(), limit)
	if err != nil {
		log.Printf("recipes: cook again: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to load suggestions")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"recipes": toRecipeResponses(recipes)})
}

// HandlePlanGet returns one ISO week of the caller's meal plan, keyed by
// calendar day. ?week=YYYY-MM-DD selects the week containing that date — any
// day of the week works, it is normalised to the Monday — and defaults to the
// week containing today.
func (h *Handlers) HandlePlanGet(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())

	day := time.Now()
	if raw := r.URL.Query().Get("week"); raw != "" {
		parsed, err := ParsePlanDate(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "week must be a YYYY-MM-DD date")
			return
		}
		day = parsed
	}
	start := ISOWeekStart(day)
	end := start.AddDate(0, 0, 6)

	entries, err := h.store.ListPlanEntries(r.Context(), user.ID,
		start.Format(PlanDateLayout), end.Format(PlanDateLayout))
	if err != nil {
		log.Printf("recipes: list plan entries: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to load meal plan")
		return
	}

	// Seed all seven days so the client always gets a full week back, then file
	// each entry under its own date.
	days := make(map[string][]PlanEntryResponse, 7)
	for i := 0; i < 7; i++ {
		days[start.AddDate(0, 0, i).Format(PlanDateLayout)] = []PlanEntryResponse{}
	}
	for _, e := range entries {
		days[e.PlanDate] = append(days[e.PlanDate], toPlanEntryResponse(e))
	}

	writeJSON(w, http.StatusOK, PlanWeekResponse{
		WeekStart: start.Format(PlanDateLayout),
		WeekEnd:   end.Format(PlanDateLayout),
		Days:      days,
	})
}

// HandlePlanPut schedules one or more recipes into meal slots. Each entry
// replaces whatever occupied its (date, slot), so re-sending the same payload
// is idempotent. The whole batch is rejected if any entry is malformed or names
// a recipe the caller does not own.
func (h *Handlers) HandlePlanPut(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())

	var body UpdatePlanRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if len(body.Entries) == 0 {
		writeError(w, http.StatusBadRequest, "at least one entry is required")
		return
	}

	entries := make([]PlanEntry, 0, len(body.Entries))
	for _, in := range body.Entries {
		entries = append(entries, PlanEntry{
			RecipeID: in.RecipeID,
			PlanDate: in.Date,
			Slot:     in.Slot,
		})
	}

	stored, err := h.store.UpsertPlanEntries(r.Context(), user.ID, entries)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidPlanDate):
			writeError(w, http.StatusBadRequest, "date must be YYYY-MM-DD")
		case errors.Is(err, ErrInvalidSlot):
			writeError(w, http.StatusBadRequest, "slot must be breakfast, lunch, dinner or snack")
		case errors.Is(err, sql.ErrNoRows):
			// A recipe belonging to someone else is reported as missing rather
			// than forbidden, matching the single-recipe routes.
			writeError(w, http.StatusNotFound, "recipe not found")
		default:
			log.Printf("recipes: upsert plan entries: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to save meal plan")
		}
		return
	}

	out := make([]PlanEntryResponse, 0, len(stored))
	for _, e := range stored {
		out = append(out, toPlanEntryResponse(e))
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": out})
}

// HandlePlanDelete clears one meal slot, identified by ?date= and ?slot=.
// An already-empty slot is a 404 so the client can tell a no-op from a delete.
func (h *Handlers) HandlePlanDelete(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())

	query := r.URL.Query()
	date, slot := query.Get("date"), query.Get("slot")
	if date == "" || slot == "" {
		writeError(w, http.StatusBadRequest, "date and slot are required")
		return
	}

	if err := h.store.DeletePlanEntry(r.Context(), user.ID, date, slot); err != nil {
		switch {
		case errors.Is(err, ErrInvalidPlanDate):
			writeError(w, http.StatusBadRequest, "date must be YYYY-MM-DD")
		case errors.Is(err, ErrInvalidSlot):
			writeError(w, http.StatusBadRequest, "slot must be breakfast, lunch, dinner or snack")
		case errors.Is(err, sql.ErrNoRows):
			writeError(w, http.StatusNotFound, "meal plan entry not found")
		default:
			log.Printf("recipes: delete plan entry: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to clear meal plan entry")
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// HandleGroceryPush puts the named ingredients of one of the caller's recipes
// onto their grocery list. Ingredients already on the list are skipped rather
// than duplicated, and the response says how many of each there were.
//
// The route sits behind the "recipes" gate only: it is a recipe action, and the
// list it writes to is the caller's own.
func (h *Handlers) HandleGroceryPush(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())

	id, ok := urlID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid recipe ID")
		return
	}

	var body GroceryPushRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if len(body.IngredientIDs) == 0 {
		writeError(w, http.StatusBadRequest, "ingredient_ids is required")
		return
	}

	names, err := h.store.IngredientNamesForRecipe(r.Context(), user.ID, id, body.IngredientIDs)
	if err != nil {
		switch {
		case errors.Is(err, ErrIngredientNotInRecipe):
			writeError(w, http.StatusBadRequest, "ingredient does not belong to this recipe")
		case errors.Is(err, sql.ErrNoRows):
			writeError(w, http.StatusNotFound, "recipe not found")
		default:
			log.Printf("recipes: resolve ingredient names: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to load ingredients")
		}
		return
	}

	// The grocery list is keyed by household, which is the user's own ID
	// throughout the grocery package.
	created, err := grocery.AddItems(r.Context(), h.db, user.ID, user.ID, names)
	if err != nil {
		log.Printf("recipes: push ingredients to grocery list: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to add items to the grocery list")
		return
	}
	for _, item := range created {
		grocery.DefaultBroker.Publish(user.ID, grocery.GroceryEvent{Type: grocery.EventItemAdded, Payload: item})
	}

	writeJSON(w, http.StatusOK, GroceryPushResponse{
		Added:   len(created),
		Skipped: len(names) - len(created),
		Items:   created,
	})
}

// HandleImport reads a recipe off a web page and returns the parsed structure
// for the user to review and edit.
//
// The endpoint is read-only by design: it performs no database writes at all.
// A failed fetch or a response Claude could not parse therefore cannot leave a
// partial recipe behind — the recipe is only persisted when the user submits
// the (possibly edited) result through POST /api/recipes.
func (h *Handlers) HandleImport(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())

	var body ImportRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, maxImportBodyBytes)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	target, err := NormalizeImportURL(body.URL)
	if err != nil {
		writeError(w, http.StatusBadRequest, "url must be an absolute http or https address")
		return
	}

	cfg, err := training.LoadClaudeConfig(h.db, user.ID)
	if err != nil {
		log.Printf("recipes: import: load claude config: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to load import config")
		return
	}
	if !cfg.Enabled {
		writeError(w, http.StatusBadRequest, "claude is not enabled")
		return
	}

	parsed, err := ImportFromURL(r.Context(), cfg, target)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidURL):
			writeError(w, http.StatusBadRequest, "url must be an absolute http or https address")
		case errors.Is(err, ErrFetch):
			log.Printf("recipes: import: fetch %s: %v", target, err)
			writeError(w, http.StatusBadGateway, "could not fetch the page")
		case errors.Is(err, ErrParse):
			log.Printf("recipes: import: parse %s: %v", target, err)
			writeError(w, http.StatusUnprocessableEntity, "could not find a recipe on that page")
		default:
			log.Printf("recipes: import: %s: %v", target, err)
			writeError(w, http.StatusBadGateway, "recipe import failed")
		}
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"recipe": parsed})
}

// respondWithRecipe re-reads a recipe and writes it as the response body. The
// mutating endpoints use it so the client always gets the stored state back
// rather than having to re-fetch.
func (h *Handlers) respondWithRecipe(w http.ResponseWriter, r *http.Request, userID, id int64, failMsg string) {
	recipe, err := h.store.GetRecipe(r.Context(), userID, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "recipe not found")
			return
		}
		log.Printf("recipes: reload recipe: %v", err)
		writeError(w, http.StatusInternalServerError, failMsg)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"recipe": toRecipeResponse(recipe)})
}

// --- mapping and validation ---

// recipeFromRequest validates a create/update body and converts it into a
// store model. It returns a non-empty message when the body is unusable; the
// caller turns that into a 400. Blank ingredient and step rows are dropped
// rather than rejected — form UIs routinely submit a trailing empty row.
func recipeFromRequest(body CreateRecipeRequest) (Recipe, string) {
	title := strings.TrimSpace(body.Title)
	if title == "" {
		return Recipe{}, "title is required"
	}
	if body.Servings < 0 {
		return Recipe{}, "servings cannot be negative"
	}

	recipe := Recipe{
		Title:       title,
		Notes:       strings.TrimSpace(body.Notes),
		Servings:    body.Servings,
		Ingredients: make([]Ingredient, 0, len(body.Ingredients)),
		Steps:       make([]Step, 0, len(body.Steps)),
		Tags:        body.Tags,
	}

	for _, in := range body.Ingredients {
		text := strings.TrimSpace(in.Text)
		name := strings.TrimSpace(in.Name)
		if text == "" && name == "" {
			continue
		}
		if in.Quantity < 0 {
			return Recipe{}, "ingredient quantity cannot be negative"
		}
		recipe.Ingredients = append(recipe.Ingredients, Ingredient{
			Text:     text,
			Quantity: in.Quantity,
			Unit:     strings.TrimSpace(in.Unit),
			Name:     name,
		})
	}

	for _, in := range body.Steps {
		text := strings.TrimSpace(in.Text)
		if text == "" {
			continue
		}
		if in.DurationSeconds < 0 {
			return Recipe{}, "step duration cannot be negative"
		}
		recipe.Steps = append(recipe.Steps, Step{Text: text, DurationSeconds: in.DurationSeconds})
	}

	return recipe, ""
}

func toRecipeResponse(r Recipe) RecipeResponse {
	resp := RecipeResponse{
		ID:           r.ID,
		Title:        r.Title,
		Notes:        r.Notes,
		Servings:     r.Servings,
		Rating:       r.Rating,
		RatedAt:      r.RatedAt,
		LastCookedAt: r.LastCook,
		CreatedAt:    r.CreatedAt,
		UpdatedAt:    r.UpdatedAt,
		Ingredients:  make([]IngredientResponse, 0, len(r.Ingredients)),
		Steps:        make([]StepResponse, 0, len(r.Steps)),
		Tags:         r.Tags,
	}
	if resp.Tags == nil {
		resp.Tags = []string{}
	}
	for _, ing := range r.Ingredients {
		resp.Ingredients = append(resp.Ingredients, IngredientResponse{
			ID:       ing.ID,
			Position: ing.Position,
			Text:     ing.Text,
			Quantity: ing.Quantity,
			Unit:     ing.Unit,
			Name:     ing.Name,
		})
	}
	for _, step := range r.Steps {
		resp.Steps = append(resp.Steps, StepResponse{
			ID:              step.ID,
			Position:        step.Position,
			Text:            step.Text,
			DurationSeconds: step.DurationSeconds,
		})
	}
	return resp
}

func toPlanEntryResponse(e PlanEntry) PlanEntryResponse {
	return PlanEntryResponse{
		ID:          e.ID,
		Date:        e.PlanDate,
		Slot:        e.Slot,
		RecipeID:    e.RecipeID,
		RecipeTitle: e.RecipeTitle,
	}
}

func toRecipeResponses(recipes []Recipe) []RecipeResponse {
	out := make([]RecipeResponse, 0, len(recipes))
	for _, r := range recipes {
		out = append(out, toRecipeResponse(r))
	}
	return out
}

// --- HTTP helpers ---

// urlID parses the {id} path parameter.
func urlID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("recipes: encode response: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
