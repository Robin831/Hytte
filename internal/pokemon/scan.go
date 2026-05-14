package pokemon

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/training"
)

// scanMaxImageBytes caps the uploaded image size at 5 MB. Anything larger is
// rejected with 400 — Pokémon card photos from a phone camera fit well within
// this even at the highest quality settings.
const scanMaxImageBytes = 5 << 20

// scanParseFormBytes is the multipart parser limit. It sits slightly above the
// per-image cap so a request that includes only the image field never trips
// before scanMaxImageBytes is enforced on the file itself.
const scanParseFormBytes = 10 << 20

// scanConfidenceThreshold is the floor below which a Claude response is
// treated as "could not read the card" — we then return matched:false rather
// than running a doomed DB lookup.
const scanConfidenceThreshold = 0.4

// scanAllowedMIMETypes whitelists the image types we accept. The list is kept
// tight because Claude's Read tool needs to be able to view the file.
var scanAllowedMIMETypes = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/webp": true,
	"image/heic": true,
	"image/heif": true,
}

// scanPrompt is the exact prompt sent to Claude. It asks for STRICT JSON so
// the response can be parsed without an LLM-grade post-processor.
const scanPrompt = `You will be shown a single Pokémon TCG card photo. Identify two things:
1. The set, by reading the small set symbol on the bottom-right of the card AND any visible set name printed near it.
2. The collector number on the bottom-left, in the format "025/195" (numerator/denominator).

Respond as STRICT JSON, no markdown fence, no prose:
{
  "set_name": "...",         // best guess for the set's English name
  "set_id_hint": "...",      // optional pokemontcg.io set id if recognised (e.g. "sv4"), otherwise empty
  "collector_number": "...", // exact string from the card, e.g. "025/195"
  "confidence": 0.95         // 0.0-1.0; 0 means you can't read the card
}`

// claudeScanResult mirrors the STRICT JSON shape we ask Claude to return.
type claudeScanResult struct {
	SetName         string  `json:"set_name"`
	SetIDHint       string  `json:"set_id_hint"`
	CollectorNumber string  `json:"collector_number"`
	Confidence      float64 `json:"confidence"`
}

// ScanCandidate is a single matched card the scan endpoint returns. Score is
// currently the same as the model's confidence — over time we can refine it
// per-candidate (e.g. lower when only the set_name matched fuzzily).
type ScanCandidate struct {
	Card  CardDTO `json:"card"`
	Set   *SetDTO `json:"set,omitempty"`
	Score float64 `json:"score"`
}

// scanRunPromptFn is the package-level seam used by ScanHandler to invoke
// Claude. Tests override it to stub out the CLI call without touching the
// real claude binary.
var scanRunPromptFn = func(ctx context.Context, cfg *training.ClaudeConfig, prompt, imagePath string) (string, error) {
	return training.RunPromptWithImage(ctx, cfg, prompt, imagePath)
}

// ScanHandler accepts a multipart upload with a single `image` part, runs the
// photo through Claude vision, and returns the matched card (or a low-
// confidence marker). The handler is admin-gated and feature-gated at the
// router level — it does no further auth checks here beyond reading the user
// for Claude config lookup.
func ScanHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if user == nil {
			respondError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		if err := r.ParseMultipartForm(scanParseFormBytes); err != nil {
			respondError(w, http.StatusBadRequest, "failed to parse multipart form")
			return
		}

		file, header, err := r.FormFile("image")
		if err != nil {
			respondError(w, http.StatusBadRequest, "image is required")
			return
		}
		defer file.Close()

		if header.Size > scanMaxImageBytes {
			respondError(w, http.StatusBadRequest, "image too large (max 5 MB)")
			return
		}

		// Sniff the content type from the first 512 bytes. The reported
		// form Content-Type is attacker-controlled; the actual byte pattern
		// is what Claude's Read tool will see.
		sniff := make([]byte, 512)
		n, err := io.ReadFull(file, sniff)
		if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
			respondError(w, http.StatusBadRequest, "failed to read image")
			return
		}
		mime := http.DetectContentType(sniff[:n])
		if !scanAllowedMIMETypes[mime] {
			respondError(w, http.StatusBadRequest, "unsupported image type")
			return
		}

		tmp, err := os.CreateTemp("", "pokemon-scan-*")
		if err != nil {
			log.Printf("pokemon: create temp file: %v", err)
			respondError(w, http.StatusInternalServerError, "failed to create temp file")
			return
		}
		tmpPath := tmp.Name()
		defer os.Remove(tmpPath)

		if _, err := tmp.Write(sniff[:n]); err != nil {
			tmp.Close()
			log.Printf("pokemon: write sniffed bytes: %v", err)
			respondError(w, http.StatusInternalServerError, "failed to buffer image")
			return
		}
		// Enforce the 5 MB cap on the streaming copy too — a client could
		// have lied about Size via the multipart header.
		remaining := int64(scanMaxImageBytes) - int64(n)
		if remaining > 0 {
			written, err := io.Copy(tmp, io.LimitReader(file, remaining+1))
			if err != nil {
				tmp.Close()
				log.Printf("pokemon: copy upload to temp: %v", err)
				respondError(w, http.StatusInternalServerError, "failed to buffer image")
				return
			}
			if written > remaining {
				tmp.Close()
				respondError(w, http.StatusBadRequest, "image too large (max 5 MB)")
				return
			}
		}
		if err := tmp.Close(); err != nil {
			log.Printf("pokemon: close temp file: %v", err)
			respondError(w, http.StatusInternalServerError, "failed to save image")
			return
		}

		cfg, err := training.LoadClaudeConfig(db, user.ID)
		if err != nil {
			log.Printf("pokemon: load claude config: %v", err)
			respondError(w, http.StatusInternalServerError, "failed to load claude config")
			return
		}
		if !cfg.Enabled {
			respondError(w, http.StatusBadRequest, "claude is not enabled for this user")
			return
		}

		raw, err := scanRunPromptFn(r.Context(), cfg, scanPrompt, tmpPath)
		if err != nil {
			log.Printf("pokemon: claude scan: %v", err)
			respondError(w, http.StatusBadGateway, "vision call failed")
			return
		}

		result, parseErr := parseClaudeScanResult(raw)
		if parseErr != nil {
			respondJSON(w, http.StatusOK, map[string]any{
				"matched":    false,
				"confidence": 0.0,
				"reason":     fmt.Sprintf("could not parse vision response: %v", parseErr),
			})
			return
		}
		if result.Confidence < scanConfidenceThreshold {
			respondJSON(w, http.StatusOK, map[string]any{
				"matched":    false,
				"confidence": result.Confidence,
				"reason":     "low confidence",
			})
			return
		}

		candidates, reason, err := findScanCandidates(r.Context(), db, user.ID, result)
		if err != nil {
			log.Printf("pokemon: find scan candidates: %v", err)
			respondError(w, http.StatusInternalServerError, "failed to look up card")
			return
		}

		rate, rateOK := loadRate(r, db)
		if len(candidates) == 0 {
			respondJSON(w, http.StatusOK, map[string]any{
				"matched":    false,
				"confidence": result.Confidence,
				"reason":     reason,
			})
			return
		}

		// Apply EUR→NOK to each candidate card. We reuse the same helper that
		// the listing endpoints use so prices stay consistent across the API.
		if rateOK {
			cards := make([]CardDTO, len(candidates))
			for i := range candidates {
				cards[i] = candidates[i].Card
			}
			applyNOK(w, cards, rate, rateOK)
			for i := range candidates {
				candidates[i].Card = cards[i]
			}
		} else {
			w.Header().Set("X-Pokemon-Rate-Missing", "1")
		}

		respondJSON(w, http.StatusOK, map[string]any{
			"matched":    true,
			"confidence": result.Confidence,
			"candidates": candidates,
		})
	}
}

// parseClaudeScanResult unmarshals the Claude response into claudeScanResult,
// tolerating a stray markdown code fence the model occasionally adds despite
// being asked not to.
func parseClaudeScanResult(raw string) (*claudeScanResult, error) {
	trimmed := strings.TrimSpace(raw)
	// Strip ```json … ``` fences if present.
	if strings.HasPrefix(trimmed, "```") {
		trimmed = strings.TrimPrefix(trimmed, "```json")
		trimmed = strings.TrimPrefix(trimmed, "```")
		trimmed = strings.TrimSuffix(trimmed, "```")
		trimmed = strings.TrimSpace(trimmed)
	}
	var res claudeScanResult
	if err := json.Unmarshal([]byte(trimmed), &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// findScanCandidates resolves the Claude-identified set + collector number to
// one or more catalogue cards. Match priority follows the bead spec: explicit
// pokemontcg.io set id hint first, then case-insensitive set name substring,
// then unfiltered. The returned reason is included in matched:false responses
// so the UI can surface why nothing came back.
func findScanCandidates(ctx context.Context, db *sql.DB, userID int64, result *claudeScanResult) ([]ScanCandidate, string, error) {
	collector := strings.TrimSpace(result.CollectorNumber)
	if collector == "" {
		return nil, "no collector number identified", nil
	}

	var setFilter []string
	var setLabel string

	if hint := strings.TrimSpace(result.SetIDHint); hint != "" {
		var exists string
		err := db.QueryRowContext(ctx,
			`SELECT id FROM pokemon_sets WHERE id = ?`, hint,
		).Scan(&exists)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			// Hint didn't match the catalogue; fall through to name match.
		case err != nil:
			return nil, "", fmt.Errorf("set id lookup: %w", err)
		default:
			setFilter = []string{exists}
			setLabel = exists
		}
	}

	if len(setFilter) == 0 {
		if name := strings.TrimSpace(result.SetName); name != "" {
			rows, err := db.QueryContext(ctx,
				`SELECT id FROM pokemon_sets WHERE LOWER(name) LIKE LOWER(?)`,
				"%"+name+"%",
			)
			if err != nil {
				return nil, "", fmt.Errorf("set name lookup: %w", err)
			}
			for rows.Next() {
				var id string
				if err := rows.Scan(&id); err != nil {
					rows.Close()
					return nil, "", fmt.Errorf("scan set id: %w", err)
				}
				setFilter = append(setFilter, id)
			}
			rows.Close()
			setLabel = name
		}
	}

	cards, err := loadScanCards(ctx, db, userID, collector, setFilter)
	if err != nil {
		return nil, "", err
	}

	if len(cards) == 0 {
		label := setLabel
		if label == "" {
			label = "any set"
		}
		return nil, fmt.Sprintf("no card matches set '%s' collector '%s'", label, collector), nil
	}

	out := make([]ScanCandidate, 0, len(cards))
	for i := range cards {
		card := cards[i]
		set, err := loadSetByID(ctx, db, userID, card.SetID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, "", fmt.Errorf("load set %s: %w", card.SetID, err)
		}
		out = append(out, ScanCandidate{
			Card:  card,
			Set:   set,
			Score: result.Confidence,
		})
	}
	return out, "", nil
}

// loadScanCards fetches cards matching the collector number, optionally
// restricted to one or more set ids. Variants and ownership flags are
// hydrated the same way as the other listing endpoints.
func loadScanCards(ctx context.Context, db *sql.DB, userID int64, collector string, setIDs []string) ([]CardDTO, error) {
	query := `
		SELECT c.id, c.set_id, c.name, c.collector_no, c.rarity, c.image_small_url, c.image_large_url
		FROM pokemon_cards c
		WHERE c.collector_no = ?
	`
	args := []any{collector}
	if len(setIDs) > 0 {
		placeholders := strings.Repeat("?,", len(setIDs))
		placeholders = placeholders[:len(placeholders)-1]
		query += " AND c.set_id IN (" + placeholders + ")"
		for _, id := range setIDs {
			args = append(args, id)
		}
	}
	query += " ORDER BY c.set_id, c.id"

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cards := make([]CardDTO, 0)
	cardIndex := make(map[string]int)
	for rows.Next() {
		var c CardDTO
		if err := rows.Scan(&c.ID, &c.SetID, &c.Name, &c.CollectorNo, &c.Rarity, &c.ImageSmallURL, &c.ImageLargeURL); err != nil {
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
	return hydrateVariantsCtx(ctx, db, userID, cards, cardIndex)
}

// loadSetByID is a context-aware variant of loadSet that does not depend on a
// live *http.Request. The scan handler runs inside an HTTP context, but
// helpers that only need a Context (e.g. background callers added later) can
// reuse the same query without faking a request.
func loadSetByID(ctx context.Context, db *sql.DB, userID int64, setID string) (*SetDTO, error) {
	var s SetDTO
	err := db.QueryRowContext(ctx, `
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

// hydrateVariantsCtx is the context-only equivalent of hydrateVariants. The
// existing helper takes *http.Request because every other caller already has
// one; the scan handler does too but we want a callable seam for future
// background scans.
func hydrateVariantsCtx(ctx context.Context, db *sql.DB, userID int64, cards []CardDTO, cardIndex map[string]int) ([]CardDTO, error) {
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
	rows, err := db.QueryContext(ctx, query, args...)
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
