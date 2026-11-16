package pokemon

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
)

// TCGdex sometimes has a set without a logo or symbol (new sets, promos,
// energies). Scrydex — pokemontcg.io's image host — usually has them, under
// our id or the pokemontcg-style id (me06 → me6, me02.5 → me2pt5). For an
// unknown id it answers 200 with a generic "Pokémon TCG" picture, so a hit
// only counts when it differs from what a made-up id returns.

func scrydexArt(id, kind string) string {
	return "https://images.scrydex.com/pokemon/" + id + "-" + kind + "/" + kind
}

var tcgdexSetIDPattern = regexp.MustCompile(`^([a-z]+)0*(\d+)(\.5)?$`)

// scrydexSetIDs lists the ids Scrydex may file a set under.
func scrydexSetIDs(ourID, tcgdexID string) []string {
	ids := []string{ourID}
	if m := tcgdexSetIDPattern.FindStringSubmatch(tcgdexID); m != nil {
		alt := m[1] + m[2]
		if m[3] != "" {
			alt += "pt5"
		}
		if alt != ourID {
			ids = append(ids, alt)
		}
	}
	return ids
}

// imageDigestHTTP fetches an image and returns its SHA-256, or "" when it
// doesn't load.
func (c *TCGdexClient) imageDigestHTTP(ctx context.Context, u string) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return ""
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	h := sha256.New()
	if _, err := io.Copy(h, io.LimitReader(resp.Body, 5<<20)); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

// FillMissingSetArt gives sets without a logo or symbol one from Scrydex
// where it has a real one. TCGdex's own art wins on the next sync once it
// appears, since the sync only overwrites with a non-empty value.
func FillMissingSetArt(ctx context.Context, db *sql.DB, client *TCGdexClient) error {
	if client == nil {
		client = NewTCGdexClient()
	}
	type set struct{ id, tcgdexID, logo, symbol string }
	rows, err := db.QueryContext(ctx, `
		SELECT id, tcgdex_id, logo_url, symbol_url FROM pokemon_sets
		WHERE logo_url = '' OR symbol_url = ''`)
	if err != nil {
		return err
	}
	var sets []set
	for rows.Next() {
		var s set
		if err := rows.Scan(&s.id, &s.tcgdexID, &s.logo, &s.symbol); err != nil {
			rows.Close()
			return err
		}
		sets = append(sets, s)
	}
	rows.Close()
	if err := rows.Err(); err != nil || len(sets) == 0 {
		return err
	}

	placeholder := map[string]string{}
	for _, kind := range []string{"logo", "symbol"} {
		placeholder[kind] = client.imageDigest(ctx, scrydexArt("hytte-no-such-set", kind))
	}
	find := func(s set, kind string) string {
		for _, id := range scrydexSetIDs(s.id, s.tcgdexID) {
			u := scrydexArt(id, kind)
			if d := client.imageDigest(ctx, u); d != "" && d != placeholder[kind] {
				return u
			}
		}
		return ""
	}

	filled := 0
	for _, s := range sets {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		logo, symbol := s.logo, s.symbol
		if logo == "" {
			logo = find(s, "logo")
		}
		if symbol == "" {
			symbol = find(s, "symbol")
		}
		if logo == s.logo && symbol == s.symbol {
			continue
		}
		if _, err := db.ExecContext(ctx, `UPDATE pokemon_sets SET logo_url = ?, symbol_url = ? WHERE id = ?`, logo, symbol, s.id); err != nil {
			return fmt.Errorf("set art for %s: %w", s.id, err)
		}
		filled++
	}
	if filled > 0 {
		log.Printf("pokemon: filled missing logo/symbol for %d sets from Scrydex", filled)
	}
	return nil
}
