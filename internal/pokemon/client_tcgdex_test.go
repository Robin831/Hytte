package pokemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestShouldUseTCGdex(t *testing.T) {
	cases := []struct {
		setID string
		want  bool
	}{
		{"me1", true},
		{"me2", true},
		{"me2pt5", true},
		{"me3", true},
		{"me4", true},
		{"sv1", false},
		{"sv10", false},
		{"sv3pt5", false},
		{"swsh1", false},
		{"", false},
		{"meowstic", false}, // doesn't actually match — exact set IDs only
	}
	for _, c := range cases {
		if got := shouldUseTCGdex(c.setID); got != c.want {
			t.Errorf("shouldUseTCGdex(%q) = %v, want %v", c.setID, got, c.want)
		}
	}
}

func TestOurSetIDToTCGdex(t *testing.T) {
	cases := map[string]string{
		// Mega Evolution family — primary fallback targets.
		"me1":    "me01",
		"me2":    "me02",
		"me2pt5": "me02.5",
		"me3":    "me03",
		"me4":    "me04",
		// Scarlet & Violet family — same shape, even though we don't
		// actually dispatch them through TCGdex today. Tests fix the
		// translation contract so a future expansion of
		// tcgdexFallbackSets doesn't need to change the translator.
		"sv1":    "sv01",
		"sv10":   "sv10", // already two-digit, unchanged
		"sv3pt5": "sv03.5",
		// Letter-only set ids pass through unchanged.
		"svp": "svp",
		"sve": "sve",
		// Unrecognized shapes also pass through unchanged.
		"":      "",
		"weird": "weird",
	}
	for in, want := range cases {
		if got := ourSetIDToTCGdex(in); got != want {
			t.Errorf("ourSetIDToTCGdex(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestOurCardIDToTCGdex(t *testing.T) {
	cases := map[string]string{
		// me family — set padded, collector padded to 3.
		"me1-1":     "me01-001",
		"me1-21":    "me01-021",
		"me1-188":   "me01-188",
		"me2pt5-21": "me02.5-021",
		"sv7-21":    "sv07-021",
		// Non-numeric collector tails preserved (promo IDs etc.).
		"swsh-SWSH123": "swsh-SWSH123",
		// Malformed (no dash) passes through.
		"weird": "weird",
	}
	for in, want := range cases {
		if got := ourCardIDToTCGdex(in); got != want {
			t.Errorf("ourCardIDToTCGdex(%q) = %q, want %q", in, got, want)
		}
	}
}

// fptr returns a pointer to a float64 literal; helps assemble the test
// fixtures for cardmarket price fields which are *float64 in the API.
func fptr(v float64) *float64 { return &v }

func TestPickTCGdexPrice(t *testing.T) {
	cases := []struct {
		name      string
		variants  []TCGdexVariantDetail
		wantN, wR float64
	}{
		{
			name: "normal + reverse both priced",
			variants: []TCGdexVariantDetail{
				{Type: "normal", Pricing: TCGdexPricing{Cardmarket: &TCGdexCardmarket{Avg: fptr(0.12)}}},
				{Type: "reverse", Pricing: TCGdexPricing{Cardmarket: &TCGdexCardmarket{Avg: fptr(0.40)}}},
			},
			wantN: 0.12, wR: 0.40,
		},
		{
			name: "ex-only card with holo lane (no normal) maps to normal lane",
			variants: []TCGdexVariantDetail{
				{Type: "holo", Pricing: TCGdexPricing{Cardmarket: &TCGdexCardmarket{Avg: fptr(1.07)}}},
			},
			wantN: 1.07, wR: 0,
		},
		{
			name: "normal present preempts holo for normal lane",
			variants: []TCGdexVariantDetail{
				{Type: "normal", Pricing: TCGdexPricing{Cardmarket: &TCGdexCardmarket{Avg: fptr(0.05)}}},
				{Type: "holo", Pricing: TCGdexPricing{Cardmarket: &TCGdexCardmarket{Avg: fptr(2.0)}}},
			},
			wantN: 0.05, wR: 0,
		},
		{
			name: "trend fallback when avg is nil",
			variants: []TCGdexVariantDetail{
				{Type: "normal", Pricing: TCGdexPricing{Cardmarket: &TCGdexCardmarket{Avg: nil, Trend: fptr(0.30)}}},
			},
			wantN: 0.30, wR: 0,
		},
		{
			name: "everything nil -> 0",
			variants: []TCGdexVariantDetail{
				{Type: "normal", Pricing: TCGdexPricing{Cardmarket: &TCGdexCardmarket{Avg: nil, Trend: nil}}},
			},
			wantN: 0, wR: 0,
		},
		{
			name:     "empty variants",
			variants: nil,
			wantN:    0, wR: 0,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			n, r := pickTCGdexPrice(c.variants)
			if n != c.wantN || r != c.wR {
				t.Errorf("pickTCGdexPrice = (%v, %v), want (%v, %v)", n, r, c.wantN, c.wR)
			}
		})
	}
}

// End-to-end overlay test: spin up a fake TCGdex server, seed a me1 card,
// run overlayTCGdexPrices, assert the prices appear on the variant rows.
func TestOverlayTCGdexPrices_EndToEnd(t *testing.T) {
	db := setupTestDB(t)

	// Seed a me1 card so the overlay has something to enrich. We do NOT
	// run a full SyncCards here — the overlay is supposed to run on top of
	// existing card metadata, so this matches the real call site.
	now := time.Now().UTC()
	if _, err := db.Exec(`INSERT INTO pokemon_sets (id, name, series, release_date, total_cards, synced_at)
		VALUES (?, ?, ?, ?, ?, ?)`, "me1", "Mega Evolution", "Mega Evolution", "2026/01/01", 1, now); err != nil {
		t.Fatalf("seed set: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO pokemon_cards (id, set_id, name, collector_no, rarity, image_small_url, image_large_url, synced_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"me1-21", "me1", "Pansear", "21", "Common", "", "", now); err != nil {
		t.Fatalf("seed card: %v", err)
	}

	// Fake TCGdex server returning the standard variants_detailed shape.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/cards/me01-021" {
			http.NotFound(w, r)
			return
		}
		card := TCGdexCard{
			ID:      "me01-021",
			LocalID: "021",
			Name:    "Pansear",
			Rarity:  "Common",
			Variants: TCGdexVariants{
				Normal:  true,
				Reverse: true,
			},
			VariantsDetail: []TCGdexVariantDetail{
				{Type: "normal", Pricing: TCGdexPricing{Cardmarket: &TCGdexCardmarket{Avg: fptr(0.08)}}},
				{Type: "reverse", Pricing: TCGdexPricing{Cardmarket: &TCGdexCardmarket{Avg: fptr(0.32)}}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(card)
	}))
	defer srv.Close()

	prev := tcgdexClientFn
	tcgdexClientFn = func() *TCGdexClient {
		return NewTCGdexClient().WithTCGdexBaseURL(srv.URL).WithHTTPClient(srv.Client())
	}
	t.Cleanup(func() { tcgdexClientFn = prev })

	if err := overlayTCGdexPrices(context.Background(), db, "me1"); err != nil {
		t.Fatalf("overlayTCGdexPrices: %v", err)
	}

	got := map[string]float64{}
	rows, err := db.Query(`SELECT kind, price_eur FROM pokemon_card_variants WHERE card_id = ? ORDER BY kind`, "me1-21")
	if err != nil {
		t.Fatalf("query variants: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		var p float64
		if err := rows.Scan(&k, &p); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got[k] = p
	}
	if got["normal"] != 0.08 {
		t.Errorf("normal variant price: got %v, want 0.08", got["normal"])
	}
	if got["reverse_holofoil"] != 0.32 {
		t.Errorf("reverse_holofoil variant price: got %v, want 0.32", got["reverse_holofoil"])
	}
}

// Overlay should be resilient to per-card 404s — common case when TCGdex
// hasn't ingested a brand-new set yet (e.g. me04 the day it released).
// Failure to fetch one card must not abort the run for the others.
func TestOverlayTCGdexPrices_TolerantOf404(t *testing.T) {
	db := setupTestDB(t)
	now := time.Now().UTC()
	if _, err := db.Exec(`INSERT INTO pokemon_sets (id, name, series, release_date, total_cards, synced_at)
		VALUES (?, ?, ?, ?, ?, ?)`, "me1", "Mega Evolution", "Mega Evolution", "2026/01/01", 2, now); err != nil {
		t.Fatalf("seed set: %v", err)
	}
	for _, id := range []string{"me1-1", "me1-2"} {
		if _, err := db.Exec(`INSERT INTO pokemon_cards (id, set_id, name, collector_no, rarity, image_small_url, image_large_url, synced_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			id, "me1", "Card", id[len("me1-"):], "Common", "", "", now); err != nil {
			t.Fatalf("seed card %s: %v", id, err)
		}
	}

	// Only me01-001 has data; me01-002 returns 404.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/cards/me01-001" {
			http.NotFound(w, r)
			return
		}
		card := TCGdexCard{
			ID: "me01-001",
			VariantsDetail: []TCGdexVariantDetail{
				{Type: "normal", Pricing: TCGdexPricing{Cardmarket: &TCGdexCardmarket{Avg: fptr(0.10)}}},
			},
		}
		_ = json.NewEncoder(w).Encode(card)
	}))
	defer srv.Close()

	prev := tcgdexClientFn
	tcgdexClientFn = func() *TCGdexClient {
		return NewTCGdexClient().WithTCGdexBaseURL(srv.URL).WithHTTPClient(srv.Client())
	}
	t.Cleanup(func() { tcgdexClientFn = prev })

	if err := overlayTCGdexPrices(context.Background(), db, "me1"); err != nil {
		t.Fatalf("overlayTCGdexPrices: %v", err)
	}

	// The overall run should succeed (no error) and me1-1 should get its
	// price from the working response. me1-2's 404 is silently skipped —
	// no variant row will exist for it here because this test exercises
	// the overlay in isolation; in production the canonical pokemontcg.io
	// path runs first and ensures a placeholder for every card.
	var p float64
	if err := db.QueryRow(`SELECT price_eur FROM pokemon_card_variants WHERE card_id='me1-1' AND kind='normal'`).Scan(&p); err != nil {
		t.Fatalf("read me1-1 normal: %v", err)
	}
	if p != 0.10 {
		t.Errorf("me1-1 normal price: got %v, want 0.10", p)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pokemon_card_variants WHERE card_id='me1-2'`).Scan(&n); err != nil {
		t.Fatalf("count me1-2 variants: %v", err)
	}
	if n != 0 {
		t.Errorf("me1-2 should have no variant rows after 404 (run-in-isolation), got %d", n)
	}
}
