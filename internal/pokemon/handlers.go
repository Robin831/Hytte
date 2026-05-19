package pokemon

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/currency"
	"github.com/Robin831/Hytte/internal/encryption"
	"github.com/go-chi/chi/v5"
)

// Sentinel errors returned by upsertCollection so callers can map them to
// HTTP status codes. Keeping them sentinel keeps the handler decoupled from
// the underlying SQL error surface.
var (
	errVariantNotFound  = errors.New("variant not found")
	errVariantMismatch  = errors.New("variant does not belong to card")
	errInvalidCondition = errors.New("invalid condition")
)

// maxBodySize caps request bodies for collection write endpoints. Notes are
// short free-text fields; 16 KB leaves room for long parent annotations
// without exposing the decoder to unbounded streams.
const maxBodySize = 16 << 10

// defaultListLimit is the default page size for set/search/card listings.
const defaultListLimit = 50

// maxListLimit caps the explicit ?limit= a caller may request. The search and
// list endpoints stay snappy when the upper bound is tight.
const maxListLimit = 50

// defaultTopLimit is the default page size for the "top valued cards" view.
const defaultTopLimit = 50

// maxTopLimit caps ?limit= on the top endpoint. The view is meant for a
// curated highlights list, not a full leaderboard, so the upper bound stays
// modest to keep the query fast and the response tile-friendly.
const maxTopLimit = 100

// allowedConditions enumerates the grades we accept for a collection row.
// An empty string is also permitted (callers may not yet have graded the card).
var allowedConditions = map[string]bool{
	"":                  true,
	"mint":              true,
	"near_mint":         true,
	"lightly_played":    true,
	"moderately_played": true,
	"heavily_played":    true,
	"damaged":           true,
}

// collectorNumberPattern matches a collector-number query like "25" or
// "025/195". The slash form lets callers paste the printed "n/total" notation
// straight from a card.
var collectorNumberPattern = regexp.MustCompile(`^\d+(/\d+)?$`)

// buildSearchPredicate parses a free-form search query into SQL WHERE clauses
// (joined with AND) and their args. It accepts whitespace-separated tokens
// where each token is classified as either a collector-number form (pure
// digits, optionally with /denominator) or a name fragment. The combined
// shape lets users type things like "021 pansear" or "pikachu 25"; pure
// number and pure name queries keep their previous behavior. Returns ok=false
// when q is empty after trimming.
func buildSearchPredicate(q string) (clauses []string, args []any, ok bool) {
	tokens := strings.Fields(q)
	if len(tokens) == 0 {
		return nil, nil, false
	}
	for _, tok := range tokens {
		if collectorNumberPattern.MatchString(tok) {
			numPart := tok
			totalPart := ""
			if idx := strings.Index(tok, "/"); idx >= 0 {
				numPart = tok[:idx]
				totalPart = tok[idx+1:]
			}
			numInt, _ := strconv.Atoi(numPart)
			clauses = append(clauses, "CAST(c.collector_no AS INTEGER) = ?")
			args = append(args, numInt)
			if totalPart != "" {
				totalInt, _ := strconv.Atoi(totalPart)
				// printed_total reflects what's actually on the card face;
				// total_cards is the secret-rare-inclusive count. Match either
				// so both "025/198" (Claude reads the printed denominator) and
				// "025/258" (someone typed the full total) resolve.
				clauses = append(clauses, "(s.printed_total = ? OR s.total_cards = ?)")
				args = append(args, totalInt, totalInt)
			}
		} else {
			clauses = append(clauses, "LOWER(c.name) LIKE ?")
			args = append(args, "%"+strings.ToLower(tok)+"%")
		}
	}
	return clauses, args, true
}

// SetDTO is the JSON shape used by /api/pokemon/sets. OwnedCount is the number
// of distinct cards from the set that the current user owns at least one
// variant of, so the sets browser can render "{owned} / {total}" without
// fetching every card list up front.
type SetDTO struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Series      string `json:"series"`
	ReleaseDate string `json:"release_date"`
	TotalCards  int    `json:"total_cards"`
	SymbolURL   string `json:"symbol_url"`
	LogoURL     string `json:"logo_url"`
	OwnedCount  int    `json:"owned_count"`
}

// VariantDTO is the JSON shape of a card variant, including ownership state
// for the current user. Quantity, Condition, Notes are zero values when the
// user does not own the variant.
type VariantDTO struct {
	ID         int64    `json:"id"`
	Kind       string   `json:"kind"`
	PriceEUR   float64  `json:"price_eur"`
	PriceNOK   *float64 `json:"price_nok"`
	PriceAt    *string  `json:"price_at,omitempty"`
	Owned      bool     `json:"owned"`
	OwnedID    *int64   `json:"owned_id,omitempty"`
	Quantity   int      `json:"quantity"`
	Condition  string   `json:"condition,omitempty"`
	Notes      string   `json:"notes,omitempty"`
	AcquiredAt *string  `json:"acquired_at,omitempty"`
}

// CardDTO is the JSON shape returned for any card listing. TopVariantKind is
// populated only by the Top endpoint; it names the variant with the highest
// price_eur so the UI can label the tile ("Reverse Holo €123" etc.) without
// re-deriving it client-side.
type CardDTO struct {
	ID             string       `json:"id"`
	SetID          string       `json:"set_id"`
	SetName        string       `json:"set_name,omitempty"`
	Name           string       `json:"name"`
	CollectorNo    string       `json:"collector_no"`
	Rarity         string       `json:"rarity"`
	ImageSmallURL  string       `json:"image_small_url"`
	ImageLargeURL  string       `json:"image_large_url"`
	Variants       []VariantDTO `json:"variants"`
	TopVariantKind string       `json:"top_variant_kind,omitempty"`
	OwnedByMe      *bool        `json:"owned_by_me,omitempty"`
}

// CollectionRow is the JSON shape for collection CRUD responses. Timestamps
// are returned as the database-stored string (RFC3339-style) so we don't have
// to translate between driver representations on every read.
type CollectionRow struct {
	ID         int64  `json:"id"`
	UserID     int64  `json:"user_id"`
	CardID     string `json:"card_id"`
	VariantID  int64  `json:"variant_id"`
	Quantity   int    `json:"quantity"`
	Condition  string `json:"condition"`
	Notes      string `json:"notes,omitempty"`
	AcquiredAt string `json:"acquired_at"`
}

// RegisterRoutes mounts all Pokémon Collection routes on r under the "pokemon"
// feature gate. It is called by both the production API router and tests so
// both exercise the same middleware chain — if the gate or admin check changes
// here, the tests automatically reflect that change.
func RegisterRoutes(r chi.Router, db *sql.DB) {
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireFeature(db, "pokemon"))
		r.Get("/pokemon/sets", ListSetsHandler(db))
		r.Get("/pokemon/sets/{id}/cards", ListSetCardsHandler(db))
		r.Get("/pokemon/cards/search", SearchCardsHandler(db))
		// /pokemon/top is intentionally accessible to all pokemon-feature users,
		// not admin-only — it's a fun highlights surface for every collector.
		r.Get("/pokemon/top", TopHandler(db))
		r.Post("/pokemon/collection", UpsertCollectionHandler(db))
		r.Patch("/pokemon/collection/{id}", UpdateCollectionHandler(db))
		r.Delete("/pokemon/collection/{id}", DeleteCollectionHandler(db))
		r.Get("/pokemon/collection/missing", MissingFromSetHandler(db))
		// Async scan queue (Hytte-7fgp). The sync /api/pokemon/scan endpoint
		// is gone; uploads enqueue a job that the background worker picks up.
		// Kids hit /queue from the camera page, then poll /scans for results.
		r.Post("/pokemon/scans/queue", QueueScanHandler(db))
		// Page upload (Hytte-3zej): one multipart request with N cropped card
		// images groups them under a pokemon_scan_pages parent so a binder
		// page is queued as a single user action even though each child
		// flows through the existing single-card worker.
		r.Post("/pokemon/scans/page", PageScanHandler(db))
		// Page-level discard (Hytte-3uq2): drops the parent + soft-discards
		// every child that has not yet been added to the collection so the
		// kid can throw away a whole binder upload from the grid in one
		// click without losing already-added cards.
		r.Delete("/pokemon/scans/pages/{id}", DeleteScanPageHandler(db))
		r.Get("/pokemon/scans", ListScansHandler(db))
		r.Get("/pokemon/scans/counts", ScanCountsHandler(db))
		r.Get("/pokemon/scans/{id}/image", GetScanImageHandler(db))
		r.Post("/pokemon/scans/{id}/resolve", ResolveScanHandler(db))
	})
}

func respondJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("pokemon: encode response: %v", err)
	}
}

func respondError(w http.ResponseWriter, status int, msg string) {
	respondJSON(w, status, map[string]string{"error": msg})
}

// decodeBody decodes r.Body into dst after capping the body at limit bytes.
// Returns false and writes the appropriate error response on failure.
func decodeBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			respondError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return false
		}
		respondError(w, http.StatusBadRequest, "invalid JSON")
		return false
	}
	return true
}

// parseLimit reads ?limit= with a default and cap. Returns the default when
// the parameter is missing or invalid (to keep the API forgiving for
// hand-crafted URLs).
func parseLimit(r *http.Request, def, upper int) int {
	q := strings.TrimSpace(r.URL.Query().Get("limit"))
	if q == "" {
		return def
	}
	n, err := strconv.Atoi(q)
	if err != nil || n <= 0 {
		return def
	}
	if n > upper {
		return upper
	}
	return n
}

// parseOffset reads ?offset= with a 0 default.
func parseOffset(r *http.Request) int {
	q := strings.TrimSpace(r.URL.Query().Get("offset"))
	if q == "" {
		return 0
	}
	n, err := strconv.Atoi(q)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// loadRate fetches the latest EUR/NOK rate. When no rate is available it
// returns ok=false so callers can flag the response with the
// X-Pokemon-Rate-Missing header. Unexpected (non sql.ErrNoRows) errors are
// logged so a misconfigured DB or schema mismatch is visible in operations.
func loadRate(r *http.Request, db *sql.DB) (rate float64, ok bool) {
	rate, _, err := currency.LatestRate(r.Context(), db, currency.PairEURNOK)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			log.Printf("pokemon: load EUR/NOK rate: %v", err)
		}
		return 0, false
	}
	return rate, true
}

// applyNOK fills the price_nok field on every variant when the rate is
// available, and sets the X-Pokemon-Rate-Missing header otherwise. The header
// must be set before the response body is written.
func applyNOK(w http.ResponseWriter, cards []CardDTO, rate float64, rateOK bool) {
	if !rateOK {
		w.Header().Set("X-Pokemon-Rate-Missing", "1")
		return
	}
	for ci := range cards {
		for vi := range cards[ci].Variants {
			nok := cards[ci].Variants[vi].PriceEUR * rate
			cards[ci].Variants[vi].PriceNOK = &nok
		}
	}
}

// ListSetsHandler returns the catalogue of sets, ordered newest-first and
// optionally filtered by series via ?era=<name>. When ?owned=true is set,
// only sets containing at least one card in the authenticated user's
// collection are returned. The OwnedCount column is computed per set for the
// authenticated user via a correlated subquery — the catalogue is a few
// hundred sets at most, so the extra cost is negligible compared to making a
// follow-up request per set.
func ListSetsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if user == nil {
			respondError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		era := strings.TrimSpace(r.URL.Query().Get("era"))
		ownedOnly := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("owned")), "true")
		limit := parseLimit(r, defaultListLimit, maxListLimit)
		offset := parseOffset(r)

		// Build the WHERE clause and argument list dynamically so the era +
		// owned filters can combine freely. The owned_count subquery argument
		// is always first (it appears in SELECT before WHERE).
		var (
			whereParts []string
			args       []any
		)
		args = append(args, user.ID) // owned_count subquery
		if era != "" {
			whereParts = append(whereParts, "s.series = ?")
			args = append(args, era)
		}
		if ownedOnly {
			whereParts = append(whereParts, `EXISTS (
				SELECT 1 FROM pokemon_collections col
				JOIN pokemon_cards c ON c.id = col.card_id
				WHERE col.user_id = ? AND c.set_id = s.id AND col.quantity > 0
			)`)
			args = append(args, user.ID)
		}
		where := ""
		if len(whereParts) > 0 {
			where = "WHERE " + strings.Join(whereParts, " AND ")
		}
		args = append(args, limit, offset)

		query := `
			SELECT s.id, s.name, s.series, s.release_date, s.total_cards, s.symbol_url, s.logo_url,
			       (SELECT COUNT(DISTINCT pc.card_id)
			        FROM pokemon_collections pc
			        JOIN pokemon_cards c ON c.id = pc.card_id
			        WHERE c.set_id = s.id AND pc.user_id = ? AND pc.quantity > 0) AS owned_count
			FROM pokemon_sets s
			` + where + `
			ORDER BY s.release_date DESC, s.id DESC
			LIMIT ? OFFSET ?
		`
		rows, err := db.QueryContext(r.Context(), query, args...)
		if err != nil {
			log.Printf("pokemon: list sets: %v", err)
			respondError(w, http.StatusInternalServerError, "failed to list sets")
			return
		}
		defer rows.Close()

		sets := make([]SetDTO, 0)
		for rows.Next() {
			var s SetDTO
			if err := rows.Scan(&s.ID, &s.Name, &s.Series, &s.ReleaseDate, &s.TotalCards, &s.SymbolURL, &s.LogoURL, &s.OwnedCount); err != nil {
				log.Printf("pokemon: scan set: %v", err)
				respondError(w, http.StatusInternalServerError, "failed to list sets")
				return
			}
			sets = append(sets, s)
		}
		if err := rows.Err(); err != nil {
			log.Printf("pokemon: iterate sets: %v", err)
			respondError(w, http.StatusInternalServerError, "failed to list sets")
			return
		}

		respondJSON(w, http.StatusOK, map[string]any{"sets": sets})
	}
}

// ListSetCardsHandler returns every card in a set with variants and per-user
// ownership flags, plus the set's metadata so the frontend does not need a
// second request to render the page header. Caller must be authenticated; the
// URL must contain {id}.
func ListSetCardsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if user == nil {
			respondError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		setID := strings.TrimSpace(chi.URLParam(r, "id"))
		if setID == "" {
			respondError(w, http.StatusBadRequest, "set id is required")
			return
		}

		set, err := loadSet(r, db, user.ID, setID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			log.Printf("pokemon: load set: %v", err)
			respondError(w, http.StatusInternalServerError, "failed to load set")
			return
		}

		cards, err := loadCardsForSet(r, db, user.ID, setID)
		if err != nil {
			log.Printf("pokemon: list set cards: %v", err)
			respondError(w, http.StatusInternalServerError, "failed to list cards")
			return
		}

		rate, ok := loadRate(r, db)
		applyNOK(w, cards, rate, ok)
		respondJSON(w, http.StatusOK, map[string]any{"set": set, "cards": cards})
	}
}

// loadSet returns the set DTO for setID with the current user's owned_count, or
// sql.ErrNoRows if the set is not in the catalogue. Returning a *SetDTO lets
// the handler embed null in the JSON response when the set is unknown rather
// than fabricating a placeholder.
func loadSet(r *http.Request, db *sql.DB, userID int64, setID string) (*SetDTO, error) {
	var s SetDTO
	err := db.QueryRowContext(r.Context(), `
		SELECT s.id, s.name, s.series, s.release_date, s.total_cards, s.symbol_url, s.logo_url,
		       (SELECT COUNT(DISTINCT pc.card_id)
		        FROM pokemon_collections pc
		        JOIN pokemon_cards c ON c.id = pc.card_id
		        WHERE c.set_id = s.id AND pc.user_id = ? AND pc.quantity > 0) AS owned_count
		FROM pokemon_sets s
		WHERE s.id = ?
	`, userID, setID).Scan(&s.ID, &s.Name, &s.Series, &s.ReleaseDate, &s.TotalCards, &s.SymbolURL, &s.LogoURL, &s.OwnedCount)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// SearchCardsHandler powers autocomplete by collector number or name fragment.
// The query parameter is ?q=<text>. The optional ?limit= caps the response at
// up to 50 cards.
func SearchCardsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if user == nil {
			respondError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		if q == "" {
			respondJSON(w, http.StatusOK, map[string]any{"cards": []CardDTO{}})
			return
		}
		limit := parseLimit(r, defaultListLimit, maxListLimit)

		cards, err := searchCards(r, db, user.ID, q, limit)
		if err != nil {
			log.Printf("pokemon: search cards: %v", err)
			respondError(w, http.StatusInternalServerError, "failed to search cards")
			return
		}

		rate, ok := loadRate(r, db)
		applyNOK(w, cards, rate, ok)
		respondJSON(w, http.StatusOK, map[string]any{"cards": cards})
	}
}

// UpsertCollectionHandler inserts or updates a collection row for the current
// user. The (user_id, card_id, variant_id) tuple is unique, so calling this
// endpoint twice with the same payload updates instead of duplicating.
func UpsertCollectionHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if user == nil {
			respondError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		var body struct {
			CardID    string `json:"card_id"`
			VariantID int64  `json:"variant_id"`
			Quantity  int    `json:"quantity"`
			Condition string `json:"condition"`
			Notes     string `json:"notes"`
		}
		if !decodeBody(w, r, &body) {
			return
		}
		body.CardID = strings.TrimSpace(body.CardID)
		if body.CardID == "" || body.VariantID == 0 {
			respondError(w, http.StatusBadRequest, "card_id and variant_id are required")
			return
		}

		isNew, err := upsertCollection(r.Context(), db, user.ID, body.CardID, body.VariantID, body.Quantity, body.Condition, body.Notes)
		if err != nil {
			switch {
			case errors.Is(err, errVariantNotFound):
				respondError(w, http.StatusNotFound, "variant not found")
			case errors.Is(err, errVariantMismatch):
				respondError(w, http.StatusBadRequest, "variant does not belong to card")
			case errors.Is(err, errInvalidCondition):
				respondError(w, http.StatusBadRequest, "invalid condition")
			default:
				if errors.Is(err, errInvalidQuantity) {
					respondError(w, http.StatusBadRequest, "quantity must be non-negative")
					return
				}
				log.Printf("pokemon: upsert collection: %v", err)
				respondError(w, http.StatusInternalServerError, "failed to upsert collection")
			}
			return
		}

		row, err := loadCollectionRow(r, db, user.ID, body.CardID, body.VariantID)
		if err != nil {
			log.Printf("pokemon: reload collection: %v", err)
			respondError(w, http.StatusInternalServerError, "failed to read collection")
			return
		}
		status := http.StatusOK
		if isNew {
			status = http.StatusCreated
		}
		respondJSON(w, status, map[string]any{"item": row})
	}
}

// errInvalidQuantity signals a negative quantity in the upsert payload. Used
// alongside the other sentinel errors so the handler can map it cleanly to
// HTTP 400 without sprinkling string checks.
var errInvalidQuantity = errors.New("quantity must be non-negative")

// dbExecQuerier is the subset of *sql.DB / *sql.Tx that upsertCollection needs.
// Accepting an interface lets the scan-resolve "add" path execute the upsert
// inside a transaction that also flips the job state atomically, while the
// legacy collection endpoint keeps passing the bare *sql.DB.
type dbExecQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// upsertCollection persists a (user, card, variant) row, returning whether
// the row was created (true) or updated (false). The shared logic lives here
// so both the legacy collection endpoint and the new scan-resolve "add"
// action go through the same validation and encryption path.
func upsertCollection(ctx context.Context, db dbExecQuerier, userID int64, cardID string, variantID int64, quantity int, condition, notes string) (bool, error) {
	condition = strings.TrimSpace(condition)
	if quantity < 0 {
		return false, errInvalidQuantity
	}
	if quantity == 0 {
		quantity = 1
	}
	if !allowedConditions[condition] {
		return false, errInvalidCondition
	}

	var variantCard string
	err := db.QueryRowContext(ctx,
		`SELECT card_id FROM pokemon_card_variants WHERE id = ?`, variantID,
	).Scan(&variantCard)
	if errors.Is(err, sql.ErrNoRows) {
		return false, errVariantNotFound
	}
	if err != nil {
		return false, err
	}
	if variantCard != cardID {
		return false, errVariantMismatch
	}

	notesEnc, err := encryptNotes(notes)
	if err != nil {
		return false, err
	}
	now := time.Now().UTC()
	var rowExists bool
	err = db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM pokemon_collections WHERE user_id = ? AND card_id = ? AND variant_id = ?)`,
		userID, cardID, variantID,
	).Scan(&rowExists)
	if err != nil {
		return false, err
	}
	isNew := !rowExists

	_, err = db.ExecContext(ctx, `
		INSERT INTO pokemon_collections (user_id, card_id, variant_id, quantity, condition, acquired_at, notes_enc)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id, card_id, variant_id) DO UPDATE SET
			quantity   = excluded.quantity,
			condition  = excluded.condition,
			notes_enc  = excluded.notes_enc
	`, userID, cardID, variantID, quantity, condition, now, notesEnc)
	if err != nil {
		return false, err
	}
	return isNew, nil
}

// UpdateCollectionHandler patches an existing collection row. Only fields
// present in the request body are updated. The row must belong to the
// authenticated user.
func UpdateCollectionHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if user == nil {
			respondError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil || id <= 0 {
			respondError(w, http.StatusBadRequest, "invalid collection id")
			return
		}
		var body struct {
			Quantity  *int    `json:"quantity"`
			Condition *string `json:"condition"`
			Notes     *string `json:"notes"`
		}
		if !decodeBody(w, r, &body) {
			return
		}

		// Read-modify-write under the user_id scope to enforce ownership.
		// A row that does not exist or belongs to another user looks the same
		// to the caller: 404.
		var existing CollectionRow
		var encNotes sql.NullString
		err = db.QueryRowContext(r.Context(), `
			SELECT id, user_id, card_id, variant_id, quantity, condition, acquired_at, notes_enc
			FROM pokemon_collections
			WHERE id = ? AND user_id = ?
		`, id, user.ID).Scan(&existing.ID, &existing.UserID, &existing.CardID, &existing.VariantID,
			&existing.Quantity, &existing.Condition, &existing.AcquiredAt, &encNotes)
		if errors.Is(err, sql.ErrNoRows) {
			respondError(w, http.StatusNotFound, "collection item not found")
			return
		}
		if err != nil {
			log.Printf("pokemon: load collection: %v", err)
			respondError(w, http.StatusInternalServerError, "failed to load collection")
			return
		}
		existing.Notes = decryptNotes(encNotes)

		if body.Quantity != nil {
			if *body.Quantity < 0 {
				respondError(w, http.StatusBadRequest, "quantity must be non-negative")
				return
			}
			existing.Quantity = *body.Quantity
		}
		if body.Condition != nil {
			cond := strings.TrimSpace(*body.Condition)
			if !allowedConditions[cond] {
				respondError(w, http.StatusBadRequest, "invalid condition")
				return
			}
			existing.Condition = cond
		}
		if body.Notes != nil {
			existing.Notes = *body.Notes
		}

		notesEnc, err := encryptNotes(existing.Notes)
		if err != nil {
			log.Printf("pokemon: encrypt notes: %v", err)
			respondError(w, http.StatusInternalServerError, "failed to encrypt notes")
			return
		}
		if _, err := db.ExecContext(r.Context(), `
			UPDATE pokemon_collections
			SET quantity = ?, condition = ?, notes_enc = ?
			WHERE id = ? AND user_id = ?
		`, existing.Quantity, existing.Condition, notesEnc, id, user.ID); err != nil {
			log.Printf("pokemon: update collection: %v", err)
			respondError(w, http.StatusInternalServerError, "failed to update collection")
			return
		}

		respondJSON(w, http.StatusOK, map[string]any{"item": existing})
	}
}

// DeleteCollectionHandler removes a collection row owned by the current user.
// Returns 204 on success, 404 when the row does not exist or belongs to a
// different user.
func DeleteCollectionHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if user == nil {
			respondError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil || id <= 0 {
			respondError(w, http.StatusBadRequest, "invalid collection id")
			return
		}
		res, err := db.ExecContext(r.Context(),
			`DELETE FROM pokemon_collections WHERE id = ? AND user_id = ?`,
			id, user.ID,
		)
		if err != nil {
			log.Printf("pokemon: delete collection: %v", err)
			respondError(w, http.StatusInternalServerError, "failed to delete collection")
			return
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			respondError(w, http.StatusNotFound, "collection item not found")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// MissingFromSetHandler returns the cards in a set that the current user does
// not yet own (no variant present in their collection). Useful for "what's
// left to find?" UIs.
func MissingFromSetHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if user == nil {
			respondError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		setID := strings.TrimSpace(r.URL.Query().Get("set_id"))
		if setID == "" {
			respondError(w, http.StatusBadRequest, "set_id is required")
			return
		}
		cards, err := loadCardsForSet(r, db, user.ID, setID)
		if err != nil {
			log.Printf("pokemon: missing-from-set: %v", err)
			respondError(w, http.StatusInternalServerError, "failed to list cards")
			return
		}
		missing := make([]CardDTO, 0, len(cards))
		for _, c := range cards {
			owned := false
			for _, v := range c.Variants {
				if v.Owned {
					owned = true
					break
				}
			}
			if !owned {
				missing = append(missing, c)
			}
		}

		rate, ok := loadRate(r, db)
		applyNOK(w, missing, rate, ok)
		respondJSON(w, http.StatusOK, map[string]any{"cards": missing})
	}
}

// TopHandler returns the highest-valued cards across the entire catalogue,
// ranked by the max price_eur among each card's variants. It powers the
// "Top valued cards" highlights view — a fun "look what could be in here"
// surface for the kids. The optional ?owned=owned|missing|any filter lets the
// UI narrow the list to "what I'm still missing" without a second request.
// ?limit= defaults to 50 and is upper-bounded at 100; missing or invalid values use the default.
func TopHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if user == nil {
			respondError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		limit := parseLimit(r, defaultTopLimit, maxTopLimit)
		owned := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("owned")))
		switch owned {
		case "", "any", "owned", "missing":
		default:
			respondError(w, http.StatusBadRequest, "invalid owned filter")
			return
		}

		cards, err := loadTopCards(r, db, user.ID, owned, limit)
		if err != nil {
			log.Printf("pokemon: top cards: %v", err)
			respondError(w, http.StatusInternalServerError, "failed to list top cards")
			return
		}

		rate, ok := loadRate(r, db)
		applyNOK(w, cards, rate, ok)
		respondJSON(w, http.StatusOK, map[string]any{"cards": cards})
	}
}

// loadTopCards executes the ranked-variant join and returns CardDTOs with
// their variants hydrated and the highest-priced variant flagged via
// TopVariantKind. The owned filter is applied at SQL time so the limit
// matches the visible card count, not the raw catalogue position.
func loadTopCards(r *http.Request, db *sql.DB, userID int64, owned string, limit int) ([]CardDTO, error) {
	// Window function pulls the highest-priced variant per card; the outer
	// join then filters to top-priced rows and joins the catalogue.
	query := `
		WITH ranked AS (
			SELECT card_id, kind, price_eur,
			       ROW_NUMBER() OVER (PARTITION BY card_id ORDER BY price_eur DESC, id) AS rn
			FROM pokemon_card_variants
		)
		SELECT c.id, c.set_id, s.name, c.name, c.collector_no, c.rarity,
		       c.image_small_url, c.image_large_url,
		       r.kind, r.price_eur,
		       CASE WHEN EXISTS (
		         SELECT 1 FROM pokemon_collections col
		         WHERE col.user_id = ? AND col.card_id = c.id AND col.quantity > 0
		       ) THEN 1 ELSE 0 END AS owned
		FROM ranked r
		JOIN pokemon_cards c ON c.id = r.card_id
		JOIN pokemon_sets s ON s.id = c.set_id
		WHERE r.rn = 1 AND r.price_eur > 0
	`
	args := []any{userID}
	switch owned {
	case "owned":
		query += ` AND EXISTS (SELECT 1 FROM pokemon_collections col WHERE col.user_id = ? AND col.card_id = c.id AND col.quantity > 0)`
		args = append(args, userID)
	case "missing":
		query += ` AND NOT EXISTS (SELECT 1 FROM pokemon_collections col WHERE col.user_id = ? AND col.card_id = c.id AND col.quantity > 0)`
		args = append(args, userID)
	}
	query += ` ORDER BY r.price_eur DESC, c.id LIMIT ?`
	args = append(args, limit)

	rows, err := db.QueryContext(r.Context(), query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cards := make([]CardDTO, 0)
	cardIndex := make(map[string]int)
	for rows.Next() {
		var (
			c        CardDTO
			topKind  string
			topPrice float64
			ownedInt int
		)
		if err := rows.Scan(&c.ID, &c.SetID, &c.SetName, &c.Name, &c.CollectorNo, &c.Rarity,
			&c.ImageSmallURL, &c.ImageLargeURL, &topKind, &topPrice, &ownedInt); err != nil {
			return nil, err
		}
		c.TopVariantKind = topKind
		ownedBool := ownedInt == 1
		c.OwnedByMe = &ownedBool
		c.Variants = []VariantDTO{}
		cardIndex[c.ID] = len(cards)
		cards = append(cards, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(cards) == 0 {
		return cards, nil
	}
	return hydrateVariants(r, db, userID, cards, cardIndex)
}

// loadCardsForSet hydrates every card in a set with its variants and owner
// flags for the given user. A set is bounded by its printed total, so no
// pagination is applied here — silently truncating would hide cards from a
// caller building a "what's missing?" view.
func loadCardsForSet(r *http.Request, db *sql.DB, userID int64, setID string) ([]CardDTO, error) {
	cardRows, err := db.QueryContext(r.Context(), `
		SELECT id, set_id, name, collector_no, rarity, image_small_url, image_large_url
		FROM pokemon_cards
		WHERE set_id = ?
		ORDER BY CAST(collector_no AS INTEGER), collector_no
	`, setID)
	if err != nil {
		return nil, err
	}
	defer cardRows.Close()

	cards := make([]CardDTO, 0)
	cardIndex := make(map[string]int)
	for cardRows.Next() {
		var c CardDTO
		if err := cardRows.Scan(&c.ID, &c.SetID, &c.Name, &c.CollectorNo, &c.Rarity, &c.ImageSmallURL, &c.ImageLargeURL); err != nil {
			return nil, err
		}
		c.Variants = []VariantDTO{}
		cardIndex[c.ID] = len(cards)
		cards = append(cards, c)
	}
	if err := cardRows.Err(); err != nil {
		return nil, err
	}
	if len(cards) == 0 {
		return cards, nil
	}
	return hydrateVariants(r, db, userID, cards, cardIndex)
}

// searchCards executes a flexible lookup that accepts:
//   - a pure collector number ("25" / "025")
//   - a collector-number/total pair ("025/198")
//   - a pure name fragment ("pikachu")
//   - a combined "<number> <name>" or "<name> <number>" query ("021 pansear",
//     "pikachu 25") where the tokens are AND-ed
//
// For the N/M form the denominator is matched against `printed_total`
// (what is actually on the card face) and, as a back-compat fallback, the
// inclusive `total_cards`. printed_total wins for the realistic case of a
// user / Claude reading the literal printed number off a card.
func searchCards(r *http.Request, db *sql.DB, userID int64, q string, limit int) ([]CardDTO, error) {
	clauses, args, ok := buildSearchPredicate(q)
	if !ok {
		// Whitespace-only / empty after trimming — no constraint, return empty.
		return []CardDTO{}, nil
	}
	query := `
		SELECT c.id, c.set_id, s.name, c.name, c.collector_no, c.rarity, c.image_small_url, c.image_large_url
		FROM pokemon_cards c
		JOIN pokemon_sets s ON s.id = c.set_id
		WHERE ` + strings.Join(clauses, " AND ") + `
		ORDER BY s.release_date DESC, c.id
		LIMIT ?
	`
	args = append(args, limit)
	rows, err := db.QueryContext(r.Context(), query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cards := make([]CardDTO, 0)
	cardIndex := make(map[string]int)
	for rows.Next() {
		var c CardDTO
		if err := rows.Scan(&c.ID, &c.SetID, &c.SetName, &c.Name, &c.CollectorNo, &c.Rarity, &c.ImageSmallURL, &c.ImageLargeURL); err != nil {
			return nil, err
		}
		c.Variants = []VariantDTO{}
		cardIndex[c.ID] = len(cards)
		cards = append(cards, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(cards) == 0 {
		return cards, nil
	}
	return hydrateVariants(r, db, userID, cards, cardIndex)
}

// hydrateVariants fetches every variant for the cards in cardIndex and merges
// ownership data from pokemon_collections for the given user.
func hydrateVariants(r *http.Request, db *sql.DB, userID int64, cards []CardDTO, cardIndex map[string]int) ([]CardDTO, error) {
	ids := make([]any, 0, len(cardIndex))
	placeholders := make([]string, 0, len(cardIndex))
	for id := range cardIndex {
		ids = append(ids, id)
		placeholders = append(placeholders, "?")
	}
	query := `
		SELECT v.id, v.card_id, v.kind, v.price_eur, v.price_at,
		       c.id, c.quantity, c.condition, c.acquired_at, c.notes_enc
		FROM pokemon_card_variants v
		LEFT JOIN pokemon_collections c
		  ON c.variant_id = v.id AND c.user_id = ?
		WHERE v.card_id IN (` + strings.Join(placeholders, ",") + `)
		ORDER BY v.card_id, v.kind
	`
	args := append([]any{userID}, ids...)
	rows, err := db.QueryContext(r.Context(), query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			vID         int64
			cardID      string
			kind        string
			priceEUR    float64
			priceAt     sql.NullString
			collID      sql.NullInt64
			collQty     sql.NullInt64
			collCond    sql.NullString
			collAcq     sql.NullString
			collNotesEn sql.NullString
		)
		if err := rows.Scan(&vID, &cardID, &kind, &priceEUR, &priceAt,
			&collID, &collQty, &collCond, &collAcq, &collNotesEn); err != nil {
			return nil, err
		}
		idx, ok := cardIndex[cardID]
		if !ok {
			continue
		}
		v := VariantDTO{
			ID:       vID,
			Kind:     kind,
			PriceEUR: priceEUR,
		}
		if priceAt.Valid && priceAt.String != "" {
			ts := priceAt.String
			v.PriceAt = &ts
		}
		if collID.Valid {
			id := collID.Int64
			v.Owned = true
			v.OwnedID = &id
			if collQty.Valid {
				v.Quantity = int(collQty.Int64)
			}
			if collCond.Valid {
				v.Condition = collCond.String
			}
			if collAcq.Valid && collAcq.String != "" {
				ts := collAcq.String
				v.AcquiredAt = &ts
			}
			v.Notes = decryptNotes(collNotesEn)
		}
		cards[idx].Variants = append(cards[idx].Variants, v)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return cards, nil
}

// loadCollectionRow re-reads a single collection row after an upsert so the
// response reflects the persisted state (including the generated id and
// acquired_at timestamp).
func loadCollectionRow(r *http.Request, db *sql.DB, userID int64, cardID string, variantID int64) (*CollectionRow, error) {
	var row CollectionRow
	var encNotes sql.NullString
	err := db.QueryRowContext(r.Context(), `
		SELECT id, user_id, card_id, variant_id, quantity, condition, acquired_at, notes_enc
		FROM pokemon_collections
		WHERE user_id = ? AND card_id = ? AND variant_id = ?
	`, userID, cardID, variantID).Scan(
		&row.ID, &row.UserID, &row.CardID, &row.VariantID,
		&row.Quantity, &row.Condition, &row.AcquiredAt, &encNotes,
	)
	if err != nil {
		return nil, err
	}
	row.Notes = decryptNotes(encNotes)
	return &row, nil
}

// encryptNotes encrypts plaintext notes for storage. Empty notes are stored
// as NULL so the column remains queryable for "has notes" filters.
func encryptNotes(plain string) (any, error) {
	if plain == "" {
		return nil, nil
	}
	return encryption.EncryptField(plain)
}

// decryptNotes returns the plaintext notes for a nullable encrypted column.
// On decrypt failure it returns an empty string and logs a warning: returning
// the raw column value would leak ciphertext (or legacy plaintext from another
// user) into the API response, which the Warden rules explicitly forbid. This
// feature is new and has never stored plaintext, so there is no legacy path
// to preserve.
func decryptNotes(enc sql.NullString) string {
	if !enc.Valid || enc.String == "" {
		return ""
	}
	plain, err := encryption.DecryptField(enc.String)
	if err != nil {
		log.Printf("pokemon: decrypt notes: %v", err)
		return ""
	}
	return plain
}
