package pokemon

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/encryption"
	"github.com/go-chi/chi/v5"
)

// seedUser inserts a user with the given id+email and enables the pokemon
// feature for them. Returns a *auth.User suitable for injecting into a
// request context.
func seedUser(t *testing.T, db *sql.DB, id int64, email string) *auth.User {
	t.Helper()
	if _, err := db.Exec(`
		INSERT INTO users (id, email, name, google_id)
		VALUES (?, ?, ?, ?)
	`, id, email, email, email); err != nil {
		t.Fatalf("seed user %d: %v", id, err)
	}
	if err := auth.SetUserFeature(db, id, "pokemon", true); err != nil {
		t.Fatalf("enable pokemon feature for %d: %v", id, err)
	}
	return &auth.User{ID: id, Email: email, Name: email}
}

// seedCatalogue inserts two sets, four cards, and their variants. The base
// fixture is shared across most tests so we can exercise filtering, ordering,
// and ownership without rewriting it each time.
func seedCatalogue(t *testing.T, db *sql.DB) {
	t.Helper()
	now := time.Now().UTC()
	sets := []struct {
		id, name, series, releaseDate string
		total                         int
	}{
		{"sv1", "Scarlet & Violet Base", "Scarlet & Violet", "2023/03/31", 198},
		{"swsh1", "Sword & Shield Base", "Sword & Shield", "2020/02/07", 202},
	}
	for _, s := range sets {
		if _, err := db.Exec(`
			INSERT INTO pokemon_sets (id, name, series, release_date, total_cards, symbol_url, logo_url, synced_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		`, s.id, s.name, s.series, s.releaseDate, s.total, "", "", now); err != nil {
			t.Fatalf("seed set %s: %v", s.id, err)
		}
	}

	cards := []struct {
		id, setID, name, collectorNo, rarity string
	}{
		{"sv1-25", "sv1", "Pikachu", "025", "Common"},
		{"sv1-100", "sv1", "Eevee", "100", "Common"},
		{"swsh1-1", "swsh1", "Celebi V", "001", "Rare Holo V"},
		{"swsh1-25", "swsh1", "Pikachu V", "025", "Rare Holo V"},
	}
	for _, c := range cards {
		if _, err := db.Exec(`
			INSERT INTO pokemon_cards (id, set_id, name, collector_no, rarity, image_small_url, image_large_url, synced_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		`, c.id, c.setID, c.name, c.collectorNo, c.rarity, "", "", now); err != nil {
			t.Fatalf("seed card %s: %v", c.id, err)
		}
	}

	variants := []struct {
		cardID, kind string
		eur          float64
	}{
		{"sv1-25", "normal", 10.0},
		{"sv1-25", "reverse_holofoil", 14.5},
		{"sv1-100", "normal", 2.5},
		{"swsh1-1", "normal", 25.0},
		{"swsh1-25", "normal", 8.0},
	}
	for _, v := range variants {
		if _, err := db.Exec(`
			INSERT INTO pokemon_card_variants (card_id, kind, price_eur, price_at)
			VALUES (?, ?, ?, ?)
		`, v.cardID, v.kind, v.eur, now); err != nil {
			t.Fatalf("seed variant %s/%s: %v", v.cardID, v.kind, err)
		}
	}
}

// seedRate inserts a single EUR/NOK rate row dated to today.
func seedRate(t *testing.T, db *sql.DB, rate float64) {
	t.Helper()
	if _, err := db.Exec(`
		INSERT INTO currency_rates (pair, rate, observed, fetched_at)
		VALUES (?, ?, ?, ?)
	`, "EUR/NOK", rate, time.Now().UTC().Format("2006-01-02"), time.Now().UTC()); err != nil {
		t.Fatalf("seed rate: %v", err)
	}
}

// variantID returns the auto-generated row id for a given card/kind pair.
func variantID(t *testing.T, db *sql.DB, cardID, kind string) int64 {
	t.Helper()
	var id int64
	if err := db.QueryRow(
		`SELECT id FROM pokemon_card_variants WHERE card_id = ? AND kind = ?`,
		cardID, kind,
	).Scan(&id); err != nil {
		t.Fatalf("lookup variant %s/%s: %v", cardID, kind, err)
	}
	return id
}

// asUser injects the authenticated user (and feature map for pokemon=true)
// into the request context.
func asUser(r *http.Request, u *auth.User) *http.Request {
	ctx := auth.ContextWithUser(r.Context(), u)
	return r.WithContext(ctx)
}

// asChi attaches chi URL params for handlers that read chi.URLParam.
func asChi(r *http.Request, kv map[string]string) *http.Request {
	rctx := chi.NewRouteContext()
	for k, v := range kv {
		rctx.URLParams.Add(k, v)
	}
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var out T
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v (body=%q)", err, rec.Body.String())
	}
	return out
}

// --- ListSetsHandler ----------------------------------------------------------

func TestListSetsHandler_NewestFirst(t *testing.T) {
	db := setupTestDB(t)
	seedCatalogue(t, db)
	u := seedUser(t, db, 1, "a@example.com")

	req := asUser(httptest.NewRequest(http.MethodGet, "/api/pokemon/sets", nil), u)
	rec := httptest.NewRecorder()
	ListSetsHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := decode[struct {
		Sets []SetDTO `json:"sets"`
	}](t, rec)
	if len(body.Sets) != 2 {
		t.Fatalf("expected 2 sets, got %d", len(body.Sets))
	}
	if body.Sets[0].ID != "sv1" {
		t.Errorf("expected newest set first, got %q", body.Sets[0].ID)
	}
}

func TestListSetsHandler_FilterByEra(t *testing.T) {
	db := setupTestDB(t)
	seedCatalogue(t, db)
	u := seedUser(t, db, 1, "a@example.com")

	req := asUser(httptest.NewRequest(http.MethodGet, "/api/pokemon/sets?era=Sword+%26+Shield", nil), u)
	rec := httptest.NewRecorder()
	ListSetsHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := decode[struct {
		Sets []SetDTO `json:"sets"`
	}](t, rec)
	if len(body.Sets) != 1 || body.Sets[0].ID != "swsh1" {
		t.Errorf("expected only swsh1, got %+v", body.Sets)
	}
}

func TestListSetsHandler_LimitOffset(t *testing.T) {
	db := setupTestDB(t)
	seedCatalogue(t, db)
	u := seedUser(t, db, 1, "a@example.com")

	req := asUser(httptest.NewRequest(http.MethodGet, "/api/pokemon/sets?limit=1&offset=1", nil), u)
	rec := httptest.NewRecorder()
	ListSetsHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := decode[struct {
		Sets []SetDTO `json:"sets"`
	}](t, rec)
	if len(body.Sets) != 1 || body.Sets[0].ID != "swsh1" {
		t.Errorf("expected second set after offset, got %+v", body.Sets)
	}
}

func TestListSetsHandler_OwnedCount(t *testing.T) {
	db := setupTestDB(t)
	seedCatalogue(t, db)
	u := seedUser(t, db, 1, "a@example.com")
	other := seedUser(t, db, 2, "b@example.com")

	// User 1 owns two distinct cards from sv1 (across both variants of one,
	// plus the second card) — owned_count should still be 2.
	pikaNormal := variantID(t, db, "sv1-25", "normal")
	pikaReverse := variantID(t, db, "sv1-25", "reverse_holofoil")
	eeveeNormal := variantID(t, db, "sv1-100", "normal")
	for _, vid := range []struct {
		card    string
		variant int64
	}{{"sv1-25", pikaNormal}, {"sv1-25", pikaReverse}, {"sv1-100", eeveeNormal}} {
		if _, err := db.Exec(`
			INSERT INTO pokemon_collections (user_id, card_id, variant_id, quantity, condition, acquired_at, notes_enc)
			VALUES (?, ?, ?, 1, '', ?, NULL)
		`, u.ID, vid.card, vid.variant, time.Now().UTC()); err != nil {
			t.Fatalf("seed collection: %v", err)
		}
	}
	// Other user's collection must not leak into user 1's owned_count.
	celebiNormal := variantID(t, db, "swsh1-1", "normal")
	if _, err := db.Exec(`
		INSERT INTO pokemon_collections (user_id, card_id, variant_id, quantity, condition, acquired_at, notes_enc)
		VALUES (?, ?, ?, 1, '', ?, NULL)
	`, other.ID, "swsh1-1", celebiNormal, time.Now().UTC()); err != nil {
		t.Fatalf("seed other collection: %v", err)
	}

	req := asUser(httptest.NewRequest(http.MethodGet, "/api/pokemon/sets", nil), u)
	rec := httptest.NewRecorder()
	ListSetsHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := decode[struct {
		Sets []SetDTO `json:"sets"`
	}](t, rec)
	bySet := map[string]int{}
	for _, s := range body.Sets {
		bySet[s.ID] = s.OwnedCount
	}
	if bySet["sv1"] != 2 {
		t.Errorf("expected sv1 owned_count=2, got %d", bySet["sv1"])
	}
	if bySet["swsh1"] != 0 {
		t.Errorf("expected swsh1 owned_count=0 (other user's collection), got %d", bySet["swsh1"])
	}
}

func TestListSetsHandler_Unauthorized(t *testing.T) {
	db := setupTestDB(t)
	seedCatalogue(t, db)

	req := httptest.NewRequest(http.MethodGet, "/api/pokemon/sets", nil)
	rec := httptest.NewRecorder()
	ListSetsHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

// --- ListSetCardsHandler ------------------------------------------------------

func TestListSetCardsHandler_Success_WithNOK(t *testing.T) {
	db := setupTestDB(t)
	seedCatalogue(t, db)
	seedRate(t, db, 11.5)
	u := seedUser(t, db, 1, "a@example.com")

	req := asChi(asUser(httptest.NewRequest(http.MethodGet, "/api/pokemon/sets/sv1/cards", nil), u),
		map[string]string{"id": "sv1"})
	rec := httptest.NewRecorder()
	ListSetCardsHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("X-Pokemon-Rate-Missing") != "" {
		t.Errorf("expected no rate-missing header, got %q", rec.Header().Get("X-Pokemon-Rate-Missing"))
	}
	body := decode[struct {
		Set   *SetDTO   `json:"set"`
		Cards []CardDTO `json:"cards"`
	}](t, rec)
	if len(body.Cards) != 2 {
		t.Fatalf("expected 2 cards in sv1, got %d", len(body.Cards))
	}
	if body.Set == nil || body.Set.ID != "sv1" || body.Set.Name != "Scarlet & Violet Base" {
		t.Errorf("expected embedded set sv1, got %+v", body.Set)
	}
	// Cards are ordered by collector_no — Pikachu (25) before Eevee (100).
	if body.Cards[0].CollectorNo != "025" {
		t.Errorf("expected collector_no 025 first, got %q", body.Cards[0].CollectorNo)
	}
	// Normal variant of Pikachu costs 10 EUR; with rate 11.5 → 115 NOK.
	for _, v := range body.Cards[0].Variants {
		if v.Kind != "normal" {
			continue
		}
		if v.PriceNOK == nil {
			t.Fatalf("expected non-nil price_nok for normal variant")
		}
		if got := *v.PriceNOK; got != 115.0 {
			t.Errorf("expected price_nok=115.0, got %v", got)
		}
	}
}

func TestListSetCardsHandler_MissingRate(t *testing.T) {
	db := setupTestDB(t)
	seedCatalogue(t, db)
	u := seedUser(t, db, 1, "a@example.com")

	req := asChi(asUser(httptest.NewRequest(http.MethodGet, "/api/pokemon/sets/sv1/cards", nil), u),
		map[string]string{"id": "sv1"})
	rec := httptest.NewRecorder()
	ListSetCardsHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if rec.Header().Get("X-Pokemon-Rate-Missing") != "1" {
		t.Errorf("expected X-Pokemon-Rate-Missing=1 header, got %q", rec.Header().Get("X-Pokemon-Rate-Missing"))
	}
	body := decode[struct {
		Cards []CardDTO `json:"cards"`
	}](t, rec)
	for _, c := range body.Cards {
		for _, v := range c.Variants {
			if v.PriceNOK != nil {
				t.Errorf("expected price_nok=nil when rate missing, got %v", *v.PriceNOK)
			}
		}
	}
}

func TestListSetCardsHandler_OwnedFlag(t *testing.T) {
	db := setupTestDB(t)
	seedCatalogue(t, db)
	u := seedUser(t, db, 1, "a@example.com")
	pikachuNormal := variantID(t, db, "sv1-25", "normal")
	if _, err := db.Exec(`
		INSERT INTO pokemon_collections (user_id, card_id, variant_id, quantity, condition, acquired_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, u.ID, "sv1-25", pikachuNormal, 3, "near_mint", time.Now().UTC()); err != nil {
		t.Fatalf("seed collection: %v", err)
	}

	req := asChi(asUser(httptest.NewRequest(http.MethodGet, "/api/pokemon/sets/sv1/cards", nil), u),
		map[string]string{"id": "sv1"})
	rec := httptest.NewRecorder()
	ListSetCardsHandler(db).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := decode[struct {
		Cards []CardDTO `json:"cards"`
	}](t, rec)
	gotOwned := false
	for _, c := range body.Cards {
		for _, v := range c.Variants {
			if v.ID == pikachuNormal {
				if !v.Owned || v.Quantity != 3 || v.Condition != "near_mint" {
					t.Errorf("expected owned variant with quantity=3, got %+v", v)
				}
				gotOwned = true
			} else if v.Owned {
				t.Errorf("unexpected owned flag on variant %d", v.ID)
			}
		}
	}
	if !gotOwned {
		t.Errorf("expected to find owned variant in response")
	}
}

// --- SearchCardsHandler -------------------------------------------------------

func TestSearchCardsHandler_ByCollectorNumber(t *testing.T) {
	db := setupTestDB(t)
	seedCatalogue(t, db)
	u := seedUser(t, db, 1, "a@example.com")

	// "025/198" matches Pikachu in sv1 (total=198) but not Pikachu V in swsh1 (total=202).
	req := asUser(httptest.NewRequest(http.MethodGet, "/api/pokemon/cards/search?q=025/198", nil), u)
	rec := httptest.NewRecorder()
	SearchCardsHandler(db).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := decode[struct {
		Cards []CardDTO `json:"cards"`
	}](t, rec)
	if len(body.Cards) != 1 || body.Cards[0].ID != "sv1-25" {
		t.Errorf("expected single sv1-25 hit, got %+v", body.Cards)
	}
}

func TestSearchCardsHandler_ByCollectorNumberOnly(t *testing.T) {
	db := setupTestDB(t)
	seedCatalogue(t, db)
	u := seedUser(t, db, 1, "a@example.com")

	// "025" without total — should return both Pikachus across sets.
	req := asUser(httptest.NewRequest(http.MethodGet, "/api/pokemon/cards/search?q=025", nil), u)
	rec := httptest.NewRecorder()
	SearchCardsHandler(db).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := decode[struct {
		Cards []CardDTO `json:"cards"`
	}](t, rec)
	if len(body.Cards) != 2 {
		t.Errorf("expected 2 cards with collector_no 25, got %d", len(body.Cards))
	}
}

func TestSearchCardsHandler_ByNameFragment(t *testing.T) {
	db := setupTestDB(t)
	seedCatalogue(t, db)
	u := seedUser(t, db, 1, "a@example.com")

	req := asUser(httptest.NewRequest(http.MethodGet, "/api/pokemon/cards/search?q=pika", nil), u)
	rec := httptest.NewRecorder()
	SearchCardsHandler(db).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := decode[struct {
		Cards []CardDTO `json:"cards"`
	}](t, rec)
	if len(body.Cards) != 2 {
		t.Fatalf("expected 2 Pikachu hits, got %d", len(body.Cards))
	}
	for _, c := range body.Cards {
		if !strings.Contains(strings.ToLower(c.Name), "pikachu") {
			t.Errorf("unexpected hit %q", c.Name)
		}
	}
}

func TestSearchCardsHandler_EmptyQuery(t *testing.T) {
	db := setupTestDB(t)
	seedCatalogue(t, db)
	u := seedUser(t, db, 1, "a@example.com")

	req := asUser(httptest.NewRequest(http.MethodGet, "/api/pokemon/cards/search?q=", nil), u)
	rec := httptest.NewRecorder()
	SearchCardsHandler(db).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := decode[struct {
		Cards []CardDTO `json:"cards"`
	}](t, rec)
	if len(body.Cards) != 0 {
		t.Errorf("expected empty result for empty query, got %d cards", len(body.Cards))
	}
}

// --- TopHandler ---------------------------------------------------------------

// TestTopHandler_SortsByMaxVariantPrice asserts the top endpoint orders cards
// by the highest-priced variant per card, descending. With the seeded
// catalogue: swsh1-1 (25.0) > sv1-25 (14.5 reverse holo) > swsh1-25 (8.0) >
// sv1-100 (2.5), and the response should preserve that order.
func TestTopHandler_SortsByMaxVariantPrice(t *testing.T) {
	db := setupTestDB(t)
	seedCatalogue(t, db)
	u := seedUser(t, db, 1, "a@example.com")

	req := asUser(httptest.NewRequest(http.MethodGet, "/api/pokemon/top", nil), u)
	rec := httptest.NewRecorder()
	TopHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := decode[struct {
		Cards []CardDTO `json:"cards"`
	}](t, rec)
	if len(body.Cards) != 4 {
		t.Fatalf("expected 4 cards, got %d", len(body.Cards))
	}
	wantOrder := []string{"swsh1-1", "sv1-25", "swsh1-25", "sv1-100"}
	for i, id := range wantOrder {
		if body.Cards[i].ID != id {
			t.Errorf("position %d: expected %q, got %q", i, id, body.Cards[i].ID)
		}
	}
	// The top-priced sv1-25 variant is reverse_holofoil at 14.5 EUR.
	if body.Cards[1].TopVariantKind != "reverse_holofoil" {
		t.Errorf("expected top variant for sv1-25 to be reverse_holofoil, got %q", body.Cards[1].TopVariantKind)
	}
	if body.Cards[1].SetName != "Scarlet & Violet Base" {
		t.Errorf("expected set name to be embedded, got %q", body.Cards[1].SetName)
	}
}

func TestTopHandler_LimitClamping(t *testing.T) {
	db := setupTestDB(t)
	seedCatalogue(t, db)
	u := seedUser(t, db, 1, "a@example.com")

	cases := []struct {
		query string
		want  int
	}{
		{"limit=2", 2},
		{"limit=0", 4},
		{"limit=-5", 4},
		{"limit=200", 4},
		{"", 4},
	}
	for _, c := range cases {
		path := "/api/pokemon/top"
		if c.query != "" {
			path += "?" + c.query
		}
		req := asUser(httptest.NewRequest(http.MethodGet, path, nil), u)
		rec := httptest.NewRecorder()
		TopHandler(db).ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: expected 200, got %d: %s", c.query, rec.Code, rec.Body.String())
		}
		body := decode[struct {
			Cards []CardDTO `json:"cards"`
		}](t, rec)
		if len(body.Cards) != c.want {
			t.Errorf("%s: expected %d cards, got %d", c.query, c.want, len(body.Cards))
		}
	}
}

func TestTopHandler_OwnedFilter(t *testing.T) {
	db := setupTestDB(t)
	seedCatalogue(t, db)
	u := seedUser(t, db, 1, "a@example.com")

	// User owns the swsh1-1 normal variant (25 EUR, top-priced card).
	celebiNormal := variantID(t, db, "swsh1-1", "normal")
	if _, err := db.Exec(`
		INSERT INTO pokemon_collections (user_id, card_id, variant_id, quantity, condition, acquired_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, u.ID, "swsh1-1", celebiNormal, 1, "near_mint", time.Now().UTC()); err != nil {
		t.Fatalf("seed collection: %v", err)
	}

	t.Run("owned only", func(t *testing.T) {
		req := asUser(httptest.NewRequest(http.MethodGet, "/api/pokemon/top?owned=owned", nil), u)
		rec := httptest.NewRecorder()
		TopHandler(db).ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		body := decode[struct {
			Cards []CardDTO `json:"cards"`
		}](t, rec)
		if len(body.Cards) != 1 || body.Cards[0].ID != "swsh1-1" {
			t.Fatalf("expected only swsh1-1 in owned filter, got %+v", body.Cards)
		}
		if body.Cards[0].OwnedByMe == nil || !*body.Cards[0].OwnedByMe {
			t.Errorf("expected owned_by_me=true for owned card, got %+v", body.Cards[0].OwnedByMe)
		}
	})

	t.Run("missing only", func(t *testing.T) {
		req := asUser(httptest.NewRequest(http.MethodGet, "/api/pokemon/top?owned=missing", nil), u)
		rec := httptest.NewRecorder()
		TopHandler(db).ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		body := decode[struct {
			Cards []CardDTO `json:"cards"`
		}](t, rec)
		ids := make([]string, 0, len(body.Cards))
		for _, c := range body.Cards {
			ids = append(ids, c.ID)
			if c.OwnedByMe == nil || *c.OwnedByMe {
				t.Errorf("expected owned_by_me=false on missing card %s, got %+v", c.ID, c.OwnedByMe)
			}
		}
		// swsh1-1 owned → filtered out. Remaining 3 cards in price order.
		want := []string{"sv1-25", "swsh1-25", "sv1-100"}
		if len(ids) != len(want) {
			t.Fatalf("expected %d missing cards, got %d (%v)", len(want), len(ids), ids)
		}
		for i, id := range want {
			if ids[i] != id {
				t.Errorf("missing position %d: expected %q, got %q", i, id, ids[i])
			}
		}
	})

	t.Run("any explicit", func(t *testing.T) {
		req := asUser(httptest.NewRequest(http.MethodGet, "/api/pokemon/top?owned=any", nil), u)
		rec := httptest.NewRecorder()
		TopHandler(db).ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		body := decode[struct {
			Cards []CardDTO `json:"cards"`
		}](t, rec)
		if len(body.Cards) != 4 {
			t.Errorf("expected 4 cards under owned=any, got %d", len(body.Cards))
		}
	})

	t.Run("invalid filter", func(t *testing.T) {
		req := asUser(httptest.NewRequest(http.MethodGet, "/api/pokemon/top?owned=garbage", nil), u)
		rec := httptest.NewRecorder()
		TopHandler(db).ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rec.Code)
		}
	})
}

func TestTopHandler_NOKConversion(t *testing.T) {
	db := setupTestDB(t)
	seedCatalogue(t, db)
	seedRate(t, db, 11.5)
	u := seedUser(t, db, 1, "a@example.com")

	req := asUser(httptest.NewRequest(http.MethodGet, "/api/pokemon/top?limit=1", nil), u)
	rec := httptest.NewRecorder()
	TopHandler(db).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if rec.Header().Get("X-Pokemon-Rate-Missing") != "" {
		t.Errorf("expected no rate-missing header, got %q", rec.Header().Get("X-Pokemon-Rate-Missing"))
	}
	body := decode[struct {
		Cards []CardDTO `json:"cards"`
	}](t, rec)
	if len(body.Cards) != 1 || body.Cards[0].ID != "swsh1-1" {
		t.Fatalf("expected swsh1-1 first, got %+v", body.Cards)
	}
	// swsh1-1 normal variant: 25 EUR × 11.5 → 287.5 NOK.
	for _, v := range body.Cards[0].Variants {
		if v.Kind != "normal" {
			continue
		}
		if v.PriceNOK == nil {
			t.Fatalf("expected non-nil price_nok for top card")
		}
		if got := *v.PriceNOK; got != 287.5 {
			t.Errorf("expected price_nok=287.5, got %v", got)
		}
	}
}

func TestTopHandler_MissingRate(t *testing.T) {
	db := setupTestDB(t)
	seedCatalogue(t, db)
	u := seedUser(t, db, 1, "a@example.com")

	req := asUser(httptest.NewRequest(http.MethodGet, "/api/pokemon/top?limit=1", nil), u)
	rec := httptest.NewRecorder()
	TopHandler(db).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if rec.Header().Get("X-Pokemon-Rate-Missing") != "1" {
		t.Errorf("expected X-Pokemon-Rate-Missing=1 header, got %q", rec.Header().Get("X-Pokemon-Rate-Missing"))
	}
}

func TestTopHandler_Unauthorized(t *testing.T) {
	db := setupTestDB(t)
	seedCatalogue(t, db)

	req := httptest.NewRequest(http.MethodGet, "/api/pokemon/top", nil)
	rec := httptest.NewRecorder()
	TopHandler(db).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

// --- Collection CRUD ----------------------------------------------------------

func TestUpsertCollectionHandler_CreatesAndUpdates(t *testing.T) {
	db := setupTestDB(t)
	seedCatalogue(t, db)
	u := seedUser(t, db, 1, "a@example.com")
	vid := variantID(t, db, "sv1-25", "normal")

	// First call creates → 201.
	payload := map[string]any{
		"card_id":    "sv1-25",
		"variant_id": vid,
		"quantity":   2,
		"condition":  "near_mint",
		"notes":      "first booster",
	}
	rec := callJSON(t, http.MethodPost, "/api/pokemon/collection", payload, u, nil, UpsertCollectionHandler(db))
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 on create, got %d: %s", rec.Code, rec.Body.String())
	}
	created := decode[struct {
		Item CollectionRow `json:"item"`
	}](t, rec)
	if created.Item.Quantity != 2 || created.Item.Condition != "near_mint" || created.Item.Notes != "first booster" {
		t.Errorf("unexpected created row: %+v", created.Item)
	}
	if created.Item.ID == 0 {
		t.Errorf("expected non-zero id, got %d", created.Item.ID)
	}

	// Verify notes are encrypted at rest.
	var stored sql.NullString
	if err := db.QueryRow(`SELECT notes_enc FROM pokemon_collections WHERE id = ?`, created.Item.ID).Scan(&stored); err != nil {
		t.Fatalf("read notes_enc: %v", err)
	}
	if !stored.Valid || stored.String == "first booster" {
		t.Errorf("notes_enc should be encrypted, got valid=%v string=%q", stored.Valid, stored.String)
	}
	plain, err := encryption.DecryptField(stored.String)
	if err != nil || plain != "first booster" {
		t.Errorf("decrypt mismatch: plain=%q err=%v", plain, err)
	}

	// Second call updates → 200 (same unique tuple).
	payload["quantity"] = 5
	payload["notes"] = "updated note"
	rec = callJSON(t, http.MethodPost, "/api/pokemon/collection", payload, u, nil, UpsertCollectionHandler(db))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on update, got %d: %s", rec.Code, rec.Body.String())
	}
	updated := decode[struct {
		Item CollectionRow `json:"item"`
	}](t, rec)
	if updated.Item.Quantity != 5 || updated.Item.Notes != "updated note" {
		t.Errorf("unexpected updated row: %+v", updated.Item)
	}
	if updated.Item.ID != created.Item.ID {
		t.Errorf("expected same id after update, got %d vs %d", updated.Item.ID, created.Item.ID)
	}
}

func TestUpsertCollectionHandler_VariantMismatch(t *testing.T) {
	db := setupTestDB(t)
	seedCatalogue(t, db)
	u := seedUser(t, db, 1, "a@example.com")
	otherVID := variantID(t, db, "sv1-100", "normal")

	payload := map[string]any{
		"card_id":    "sv1-25",
		"variant_id": otherVID,
		"quantity":   1,
	}
	rec := callJSON(t, http.MethodPost, "/api/pokemon/collection", payload, u, nil, UpsertCollectionHandler(db))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for mismatched variant, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUpsertCollectionHandler_InvalidCondition(t *testing.T) {
	db := setupTestDB(t)
	seedCatalogue(t, db)
	u := seedUser(t, db, 1, "a@example.com")
	vid := variantID(t, db, "sv1-25", "normal")

	payload := map[string]any{
		"card_id":    "sv1-25",
		"variant_id": vid,
		"quantity":   1,
		"condition":  "perfect",
	}
	rec := callJSON(t, http.MethodPost, "/api/pokemon/collection", payload, u, nil, UpsertCollectionHandler(db))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid condition, got %d", rec.Code)
	}
}

func TestUpdateCollectionHandler_OwnerScopedAndPartial(t *testing.T) {
	db := setupTestDB(t)
	seedCatalogue(t, db)
	uA := seedUser(t, db, 1, "a@example.com")
	uB := seedUser(t, db, 2, "b@example.com")
	vid := variantID(t, db, "sv1-25", "normal")

	// User A creates a row.
	rec := callJSON(t, http.MethodPost, "/api/pokemon/collection", map[string]any{
		"card_id":    "sv1-25",
		"variant_id": vid,
		"quantity":   1,
		"condition":  "mint",
		"notes":      "hers",
	}, uA, nil, UpsertCollectionHandler(db))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create as A: %d %s", rec.Code, rec.Body.String())
	}
	rowA := decode[struct {
		Item CollectionRow `json:"item"`
	}](t, rec).Item

	// User B tries to update A's row → 404.
	rec = callJSON(t, http.MethodPatch, "/api/pokemon/collection/"+strconv.FormatInt(rowA.ID, 10),
		map[string]any{"quantity": 99}, uB, map[string]string{"id": strconv.FormatInt(rowA.ID, 10)},
		UpdateCollectionHandler(db))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 cross-user, got %d", rec.Code)
	}

	// User A partial-updates only condition. Quantity and notes untouched.
	rec = callJSON(t, http.MethodPatch, "/api/pokemon/collection/"+strconv.FormatInt(rowA.ID, 10),
		map[string]any{"condition": "lightly_played"}, uA, map[string]string{"id": strconv.FormatInt(rowA.ID, 10)},
		UpdateCollectionHandler(db))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	patched := decode[struct {
		Item CollectionRow `json:"item"`
	}](t, rec).Item
	if patched.Condition != "lightly_played" || patched.Quantity != 1 || patched.Notes != "hers" {
		t.Errorf("partial update mismatched: %+v", patched)
	}
}

func TestDeleteCollectionHandler_OwnerScoped(t *testing.T) {
	db := setupTestDB(t)
	seedCatalogue(t, db)
	uA := seedUser(t, db, 1, "a@example.com")
	uB := seedUser(t, db, 2, "b@example.com")
	vid := variantID(t, db, "sv1-25", "normal")

	rec := callJSON(t, http.MethodPost, "/api/pokemon/collection", map[string]any{
		"card_id":    "sv1-25",
		"variant_id": vid,
		"quantity":   1,
	}, uA, nil, UpsertCollectionHandler(db))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create as A: %d", rec.Code)
	}
	rowA := decode[struct {
		Item CollectionRow `json:"item"`
	}](t, rec).Item
	idStr := strconv.FormatInt(rowA.ID, 10)

	// B cannot delete A's row.
	rec = callJSON(t, http.MethodDelete, "/api/pokemon/collection/"+idStr, nil, uB,
		map[string]string{"id": idStr}, DeleteCollectionHandler(db))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 cross-user delete, got %d", rec.Code)
	}

	// A can delete → 204.
	rec = callJSON(t, http.MethodDelete, "/api/pokemon/collection/"+idStr, nil, uA,
		map[string]string{"id": idStr}, DeleteCollectionHandler(db))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}

	// Second delete → 404 (row is gone).
	rec = callJSON(t, http.MethodDelete, "/api/pokemon/collection/"+idStr, nil, uA,
		map[string]string{"id": idStr}, DeleteCollectionHandler(db))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 second delete, got %d", rec.Code)
	}
}

// --- MissingFromSetHandler ----------------------------------------------------

func TestMissingFromSetHandler_ExcludesOwned(t *testing.T) {
	db := setupTestDB(t)
	seedCatalogue(t, db)
	seedRate(t, db, 11.5)
	u := seedUser(t, db, 1, "a@example.com")
	vid := variantID(t, db, "sv1-25", "normal")
	if _, err := db.Exec(`
		INSERT INTO pokemon_collections (user_id, card_id, variant_id, quantity, condition, acquired_at)
		VALUES (?, ?, ?, 1, '', ?)
	`, u.ID, "sv1-25", vid, time.Now().UTC()); err != nil {
		t.Fatalf("seed collection: %v", err)
	}

	req := asUser(httptest.NewRequest(http.MethodGet, "/api/pokemon/collection/missing?set_id=sv1", nil), u)
	rec := httptest.NewRecorder()
	MissingFromSetHandler(db).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := decode[struct {
		Cards []CardDTO `json:"cards"`
	}](t, rec)
	if len(body.Cards) != 1 || body.Cards[0].ID != "sv1-100" {
		t.Errorf("expected only sv1-100 missing, got %+v", body.Cards)
	}
}

func TestMissingFromSetHandler_RequiresSetID(t *testing.T) {
	db := setupTestDB(t)
	seedCatalogue(t, db)
	u := seedUser(t, db, 1, "a@example.com")

	req := asUser(httptest.NewRequest(http.MethodGet, "/api/pokemon/collection/missing", nil), u)
	rec := httptest.NewRecorder()
	MissingFromSetHandler(db).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

// --- Feature-flag enforcement -------------------------------------------------

// TestFeatureFlagEnforcement_All wires every endpoint through the real
// auth.RequireAuth + auth.RequireFeature middleware and asserts that a user
// without the pokemon flag is denied across the board. This is the end-to-end
// proof that the feature gate is wired correctly.
func TestFeatureFlagEnforcement_All(t *testing.T) {
	db := setupTestDB(t)
	seedCatalogue(t, db)

	// Create a non-admin user WITHOUT the pokemon feature.
	if _, err := db.Exec(`
		INSERT INTO users (id, email, name, google_id)
		VALUES (1, 'a@example.com', 'A', 'a-google')
	`); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	// Provision a session so RequireAuth resolves the user.
	token, _, err := auth.CreateSession(db, 1)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireAuth(db))
		r.Use(auth.WithFeatures(db))
		r.Group(func(r chi.Router) {
			r.Use(auth.RequireFeature(db, "pokemon"))
			r.Get("/api/pokemon/sets", ListSetsHandler(db))
			r.Get("/api/pokemon/sets/{id}/cards", ListSetCardsHandler(db))
			r.Get("/api/pokemon/cards/search", SearchCardsHandler(db))
			r.Get("/api/pokemon/top", TopHandler(db))
			r.Post("/api/pokemon/collection", UpsertCollectionHandler(db))
			r.Patch("/api/pokemon/collection/{id}", UpdateCollectionHandler(db))
			r.Delete("/api/pokemon/collection/{id}", DeleteCollectionHandler(db))
			r.Get("/api/pokemon/collection/missing", MissingFromSetHandler(db))
			r.Post("/api/pokemon/scans/queue", QueueScanHandler(db))
			r.Get("/api/pokemon/scans", ListScansHandler(db))
			r.Get("/api/pokemon/scans/{id}/image", GetScanImageHandler(db))
			r.Post("/api/pokemon/scans/{id}/resolve", ResolveScanHandler(db))
		})
	})

	calls := []struct {
		method, path string
	}{
		{http.MethodGet, "/api/pokemon/sets"},
		{http.MethodGet, "/api/pokemon/sets/sv1/cards"},
		{http.MethodGet, "/api/pokemon/cards/search?q=pika"},
		{http.MethodGet, "/api/pokemon/top"},
		{http.MethodPost, "/api/pokemon/collection"},
		{http.MethodPatch, "/api/pokemon/collection/1"},
		{http.MethodDelete, "/api/pokemon/collection/1"},
		{http.MethodGet, "/api/pokemon/collection/missing?set_id=sv1"},
		{http.MethodPost, "/api/pokemon/scans/queue"},
		{http.MethodGet, "/api/pokemon/scans"},
		{http.MethodGet, "/api/pokemon/scans/1/image"},
		{http.MethodPost, "/api/pokemon/scans/1/resolve"},
	}
	for _, c := range calls {
		req := httptest.NewRequest(c.method, c.path, strings.NewReader(`{}`))
		req.AddCookie(&http.Cookie{Name: "session", Value: token})
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s %s: expected 403 without pokemon flag, got %d (%s)",
				c.method, c.path, rec.Code, rec.Body.String())
		}
	}
}

// TestFeatureFlagEnforcement_PerKid asserts the pokemon flag is independent
// per user — toggling it for kid A must not change the gate for kid B. Kids
// are normal users, so the existing user_features mechanism gives us this for
// free; this test guards against future regressions that would conflate them.
func TestFeatureFlagEnforcement_PerKid(t *testing.T) {
	db := setupTestDB(t)
	seedCatalogue(t, db)

	if _, err := db.Exec(`
		INSERT INTO users (id, email, name, google_id) VALUES (1, 'kid-a@example.com', 'Kid A', 'g-kid-a');
		INSERT INTO users (id, email, name, google_id) VALUES (2, 'kid-b@example.com', 'Kid B', 'g-kid-b');
	`); err != nil {
		t.Fatalf("seed users: %v", err)
	}

	// Kid A has pokemon enabled; Kid B does not.
	if err := auth.SetUserFeature(db, 1, "pokemon", true); err != nil {
		t.Fatalf("enable pokemon for kid A: %v", err)
	}
	if err := auth.SetUserFeature(db, 2, "pokemon", false); err != nil {
		t.Fatalf("disable pokemon for kid B: %v", err)
	}

	featsA, err := auth.GetUserFeatures(db, 1, false)
	if err != nil {
		t.Fatalf("get features A: %v", err)
	}
	featsB, err := auth.GetUserFeatures(db, 2, false)
	if err != nil {
		t.Fatalf("get features B: %v", err)
	}
	if !featsA["pokemon"] {
		t.Errorf("kid A should have pokemon enabled, got %v", featsA["pokemon"])
	}
	if featsB["pokemon"] {
		t.Errorf("kid B should have pokemon disabled, got %v", featsB["pokemon"])
	}

	tokenA, _, err := auth.CreateSession(db, 1)
	if err != nil {
		t.Fatalf("create session A: %v", err)
	}
	tokenB, _, err := auth.CreateSession(db, 2)
	if err != nil {
		t.Fatalf("create session B: %v", err)
	}

	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireAuth(db))
		r.Use(auth.WithFeatures(db))
		r.Group(func(r chi.Router) {
			r.Use(auth.RequireFeature(db, "pokemon"))
			r.Get("/api/pokemon/sets", ListSetsHandler(db))
		})
	})

	// Kid A: pokemon=true → 200.
	reqA := httptest.NewRequest(http.MethodGet, "/api/pokemon/sets", nil)
	reqA.AddCookie(&http.Cookie{Name: "session", Value: tokenA})
	recA := httptest.NewRecorder()
	r.ServeHTTP(recA, reqA)
	if recA.Code != http.StatusOK {
		t.Errorf("kid A expected 200 with pokemon enabled, got %d (%s)", recA.Code, recA.Body.String())
	}

	// Kid B: pokemon=false → 403.
	reqB := httptest.NewRequest(http.MethodGet, "/api/pokemon/sets", nil)
	reqB.AddCookie(&http.Cookie{Name: "session", Value: tokenB})
	recB := httptest.NewRecorder()
	r.ServeHTTP(recB, reqB)
	if recB.Code != http.StatusForbidden {
		t.Errorf("kid B expected 403 with pokemon disabled, got %d (%s)", recB.Code, recB.Body.String())
	}

	// Flip kid B on — kid A's gate should be unaffected.
	if err := auth.SetUserFeature(db, 2, "pokemon", true); err != nil {
		t.Fatalf("re-enable pokemon for kid B: %v", err)
	}
	reqB2 := httptest.NewRequest(http.MethodGet, "/api/pokemon/sets", nil)
	reqB2.AddCookie(&http.Cookie{Name: "session", Value: tokenB})
	recB2 := httptest.NewRecorder()
	r.ServeHTTP(recB2, reqB2)
	if recB2.Code != http.StatusOK {
		t.Errorf("kid B expected 200 after enabling pokemon, got %d (%s)", recB2.Code, recB2.Body.String())
	}

	// Toggle kid A off — kid B should remain enabled.
	if err := auth.SetUserFeature(db, 1, "pokemon", false); err != nil {
		t.Fatalf("disable pokemon for kid A: %v", err)
	}
	reqA2 := httptest.NewRequest(http.MethodGet, "/api/pokemon/sets", nil)
	reqA2.AddCookie(&http.Cookie{Name: "session", Value: tokenA})
	recA2 := httptest.NewRecorder()
	r.ServeHTTP(recA2, reqA2)
	if recA2.Code != http.StatusForbidden {
		t.Errorf("kid A expected 403 after disabling pokemon, got %d (%s)", recA2.Code, recA2.Body.String())
	}
	reqB3 := httptest.NewRequest(http.MethodGet, "/api/pokemon/sets", nil)
	reqB3.AddCookie(&http.Cookie{Name: "session", Value: tokenB})
	recB3 := httptest.NewRecorder()
	r.ServeHTTP(recB3, reqB3)
	if recB3.Code != http.StatusOK {
		t.Errorf("kid B should still be 200 after toggling kid A, got %d (%s)", recB3.Code, recB3.Body.String())
	}
}

// callJSON encodes payload as JSON, attaches the user + optional chi params,
// and runs the handler against the resulting request/recorder pair.
func callJSON(t *testing.T, method, path string, payload any, user *auth.User, params map[string]string, h http.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	var body *bytes.Buffer
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal payload: %v", err)
		}
		body = bytes.NewBuffer(raw)
	} else {
		body = bytes.NewBuffer(nil)
	}
	req := httptest.NewRequest(method, path, body)
	req.Header.Set("Content-Type", "application/json")
	if user != nil {
		req = asUser(req, user)
	}
	if len(params) > 0 {
		req = asChi(req, params)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}
