package pokemon

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/go-chi/chi/v5"
)

// shippingZip is the postcode new shops' shipping is estimated for
// (central Bergen).
const shippingZip = "5003"

// refreshInBackground starts an offers refresh that outlives the request.
func refreshInBackground(db *sql.DB, setIDs ...string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
		defer cancel()
		if _, err := RefreshOffers(ctx, db, setIDs...); err != nil {
			log.Printf("pokemon: refresh shop offers: %v", err)
		}
	}()
}

// SinglesPageHandler: GET /api/pokemon/singles.
func SinglesPageHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if err := seedShops(r.Context(), db); err != nil {
			log.Printf("pokemon: seed shops: %v", err)
		}
		ensureSetCodes(r.Context(), db, user.ID)
		rate, ok := loadRate(r, db)
		if !ok {
			w.Header().Set("X-Pokemon-Rate-Missing", "1")
		}
		page, err := loadSinglesPage(r.Context(), db, user.ID, rate, ok)
		if err != nil {
			log.Printf("pokemon: singles page: %v", err)
			respondError(w, http.StatusInternalServerError, "failed to load singles")
			return
		}
		respondJSON(w, http.StatusOK, page)
	}
}

// AddWantedSetHandler: POST /api/pokemon/singles/sets {set_id}. Starts a
// shop check for the set.
func AddWantedSetHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		var body struct {
			SetID string `json:"set_id"`
		}
		if !decodeBody(w, r, &body) {
			return
		}
		var exists int
		if err := db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM pokemon_sets WHERE id = ?`, body.SetID).Scan(&exists); err != nil || exists == 0 {
			respondError(w, http.StatusBadRequest, "unknown set")
			return
		}
		if _, err := db.ExecContext(r.Context(), `INSERT OR IGNORE INTO pokemon_wanted_sets (user_id, set_id, created_at) VALUES (?, ?, ?)`,
			user.ID, body.SetID, time.Now().UTC()); err != nil {
			log.Printf("pokemon: add wanted set: %v", err)
			respondError(w, http.StatusInternalServerError, "failed to add set")
			return
		}
		refreshInBackground(db, body.SetID)
		respondJSON(w, http.StatusCreated, map[string]string{"status": "added"})
	}
}

// RemoveWantedSetHandler: DELETE /api/pokemon/singles/sets/{id}.
func RemoveWantedSetHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if _, err := db.ExecContext(r.Context(), `DELETE FROM pokemon_wanted_sets WHERE user_id = ? AND set_id = ?`,
			user.ID, chi.URLParam(r, "id")); err != nil {
			respondError(w, http.StatusInternalServerError, "failed to remove set")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// RefreshOffersHandler: POST /api/pokemon/singles/refresh — check the shops
// now instead of waiting for the nightly run.
func RefreshOffersHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if offersRefreshing.Load() {
			respondJSON(w, http.StatusConflict, map[string]string{"error": "already checking the shops"})
			return
		}
		refreshInBackground(db)
		respondJSON(w, http.StatusAccepted, map[string]string{"status": "started"})
	}
}

// BasketHandler: POST /api/pokemon/singles/basket {card_ids} → the
// cheapest split of those cards across shops.
func BasketHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		var body struct {
			CardIDs []string `json:"card_ids"`
		}
		if !decodeBody(w, r, &body) {
			return
		}
		page, err := loadSinglesPage(r.Context(), db, user.ID, 0, false)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "failed to load cards")
			return
		}
		want := map[string]bool{}
		for _, id := range body.CardIDs {
			want[id] = true
		}
		var cards []basketCard
		for _, c := range page.Cards {
			if want[c.ID] {
				cards = append(cards, basketCard{id: c.ID, name: c.Name + " #" + c.CollectorNo, offers: c.Offers})
			}
		}
		var enabled []Shop
		for _, s := range page.Shops {
			if s.Enabled {
				enabled = append(enabled, s)
			}
		}
		respondJSON(w, http.StatusOK, planBasket(cards, enabled))
	}
}

// MarkOrderedHandler: POST /api/pokemon/singles/ordered {card_ids, source}
// marks cards as ordered (replacing an earlier mark).
func MarkOrderedHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		var body struct {
			CardIDs []string `json:"card_ids"`
			Source  string   `json:"source"`
		}
		if !decodeBody(w, r, &body) {
			return
		}
		source := strings.TrimSpace(body.Source)
		if len(source) > 200 {
			respondError(w, http.StatusBadRequest, "source is too long")
			return
		}
		if len(body.CardIDs) == 0 || len(body.CardIDs) > 1000 {
			respondError(w, http.StatusBadRequest, "card_ids must list 1–1000 cards")
			return
		}
		tx, err := db.BeginTx(r.Context(), nil)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "failed to save")
			return
		}
		defer tx.Rollback() //nolint:errcheck
		now := time.Now().UTC()
		for _, id := range body.CardIDs {
			var exists int
			if err := tx.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM pokemon_cards WHERE id = ?`, id).Scan(&exists); err != nil || exists == 0 {
				respondError(w, http.StatusBadRequest, "unknown card "+id)
				return
			}
			if _, err := tx.ExecContext(r.Context(), `INSERT INTO pokemon_singles_ordered (user_id, card_id, source, ordered_at) VALUES (?, ?, ?, ?)
				ON CONFLICT(user_id, card_id) DO UPDATE SET source = excluded.source, ordered_at = excluded.ordered_at`,
				user.ID, id, source, now); err != nil {
				respondError(w, http.StatusInternalServerError, "failed to save")
				return
			}
		}
		if err := tx.Commit(); err != nil {
			respondError(w, http.StatusInternalServerError, "failed to save")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// UnmarkOrderedHandler: DELETE /api/pokemon/singles/ordered/{id}.
func UnmarkOrderedHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if _, err := db.ExecContext(r.Context(), `DELETE FROM pokemon_singles_ordered WHERE user_id = ? AND card_id = ?`,
			user.ID, chi.URLParam(r, "id")); err != nil {
			respondError(w, http.StatusInternalServerError, "failed to remove")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// wantsLine is a Cardmarket wants-list line for a card. Cardmarket needs a
// Pokémon's name plus its abilities/attacks ("2x Umbreon EX Moon Mirage
// Onyx"); trainers and energies go by name. The text import can't name an
// expansion or version, so the line matches any print of the card.
func wantsLine(name string, card *TCGdexCard) string {
	parts := []string{"1x", name}
	if card != nil && strings.EqualFold(card.Category, "pokemon") {
		for _, a := range card.Abilities {
			if a.Name != "" {
				parts = append(parts, a.Name)
			}
		}
		for _, a := range card.Attacks {
			if a.Name != "" {
				parts = append(parts, a.Name)
			}
		}
	}
	return strings.Join(parts, " ")
}

// tcgdexClientForExport is a seam so tests don't call TCGdex.
var tcgdexClientForExport = NewTCGdexClient

// WantsExportHandler: POST /api/pokemon/singles/export {card_ids} →
// {lines} in the order given, one Cardmarket wants-list line per card.
func WantsExportHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			CardIDs []string `json:"card_ids"`
		}
		if !decodeBody(w, r, &body) {
			return
		}
		if len(body.CardIDs) > 1000 {
			respondError(w, http.StatusBadRequest, "too many cards")
			return
		}
		type row struct{ name, tcgdexID string }
		rows := make([]row, len(body.CardIDs))
		for i, id := range body.CardIDs {
			if err := db.QueryRowContext(r.Context(), `SELECT name, tcgdex_id FROM pokemon_cards WHERE id = ?`, id).Scan(&rows[i].name, &rows[i].tcgdexID); err != nil {
				respondError(w, http.StatusBadRequest, "unknown card "+id)
				return
			}
		}
		client := tcgdexClientForExport()
		details := make([]*TCGdexCard, len(rows))
		var wg sync.WaitGroup
		sem := make(chan struct{}, tcgdexParallelism)
		for i, rw := range rows {
			if rw.tcgdexID == "" {
				continue
			}
			i, rw := i, rw
			wg.Add(1)
			sem <- struct{}{}
			go func() {
				defer wg.Done()
				defer func() { <-sem }()
				if c, err := client.GetCard(r.Context(), rw.tcgdexID); err == nil {
					details[i] = c
				}
			}()
		}
		wg.Wait()
		lines := make([]string, len(rows))
		for i, rw := range rows {
			lines[i] = wantsLine(rw.name, details[i])
		}
		respondJSON(w, http.StatusOK, map[string]any{"lines": lines})
	}
}

func requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	user := auth.UserFromContext(r.Context())
	if user == nil || !user.IsAdmin {
		respondError(w, http.StatusForbidden, "admin only")
		return false
	}
	return true
}

// AddShopHandler: POST /api/pokemon/singles/shops {url} (admin). Checks the
// address is a Shopify shop and estimates its shipping to Bergen.
func AddShopHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireAdmin(w, r) {
			return
		}
		var body struct {
			URL string `json:"url"`
		}
		if !decodeBody(w, r, &body) {
			return
		}
		base, err := normalizeShopURL(body.URL)
		if err != nil {
			respondError(w, http.StatusBadRequest, err.Error())
			return
		}
		client := newShopifyClient(base, shopHTTPClient)
		client.minGap = 0
		var probe struct {
			Products []shopifyProduct `json:"products"`
		}
		if err := client.get(r.Context(), "/products.json?limit=50", &probe); err != nil {
			respondError(w, http.StatusBadRequest, "can't read the shop's catalogue — only Shopify shops are supported for now")
			return
		}
		shipping := 0.0
		for _, p := range probe.Products {
			for _, v := range p.Variants {
				if v.Available {
					if est, err := shippingEstimate(r.Context(), base, v.ID, shippingZip); err == nil {
						shipping = est
					}
					break
				}
			}
			if shipping > 0 {
				break
			}
		}
		u, _ := url.Parse(base)
		name := strings.TrimPrefix(u.Host, "www.")
		if i := strings.Index(name, "."); i > 0 {
			name = strings.ToUpper(name[:1]) + name[1:i]
		}
		res, err := db.ExecContext(r.Context(), `INSERT INTO pokemon_shops (name, base_url, shipping_nok, free_over_nok, enabled, created_at)
			VALUES (?, ?, ?, 0, 1, ?)`, name, base, shipping, time.Now().UTC())
		if err != nil {
			if strings.Contains(err.Error(), "UNIQUE") {
				respondError(w, http.StatusConflict, "shop already added")
				return
			}
			respondError(w, http.StatusInternalServerError, "failed to add shop")
			return
		}
		id, _ := res.LastInsertId()
		refreshInBackground(db)
		respondJSON(w, http.StatusCreated, Shop{ID: id, Name: name, BaseURL: base, ShippingNOK: shipping, Enabled: true})
	}
}

// UpdateShopHandler: PATCH /api/pokemon/singles/shops/{id} (admin).
func UpdateShopHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireAdmin(w, r) {
			return
		}
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			respondError(w, http.StatusBadRequest, "bad shop id")
			return
		}
		var body struct {
			Name        *string  `json:"name"`
			ShippingNOK *float64 `json:"shipping_nok"`
			FreeOverNOK *float64 `json:"free_over_nok"`
			Enabled     *bool    `json:"enabled"`
		}
		if !decodeBody(w, r, &body) {
			return
		}
		if body.Name != nil && strings.TrimSpace(*body.Name) == "" {
			respondError(w, http.StatusBadRequest, "name can't be empty")
			return
		}
		for _, v := range []*float64{body.ShippingNOK, body.FreeOverNOK} {
			if v != nil && (*v < 0 || *v > 100000) {
				respondError(w, http.StatusBadRequest, "amounts must be between 0 and 100000")
				return
			}
		}
		res, err := db.ExecContext(r.Context(), `UPDATE pokemon_shops SET
			name = COALESCE(?, name), shipping_nok = COALESCE(?, shipping_nok),
			free_over_nok = COALESCE(?, free_over_nok), enabled = COALESCE(?, enabled)
			WHERE id = ?`, trimPtr(body.Name), body.ShippingNOK, body.FreeOverNOK, body.Enabled, id)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "failed to update shop")
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			respondError(w, http.StatusNotFound, "shop not found")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func trimPtr(s *string) any {
	if s == nil {
		return nil
	}
	return strings.TrimSpace(*s)
}

// DeleteShopHandler: DELETE /api/pokemon/singles/shops/{id} (admin).
func DeleteShopHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireAdmin(w, r) {
			return
		}
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			respondError(w, http.StatusBadRequest, "bad shop id")
			return
		}
		res, err := db.ExecContext(r.Context(), `DELETE FROM pokemon_shops WHERE id = ?`, id)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "failed to delete shop")
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			respondError(w, http.StatusNotFound, "shop not found")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
