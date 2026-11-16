package pokemon

import (
	"context"
	"reflect"
	"testing"
	"time"
)

func TestScrydexSetIDs(t *testing.T) {
	cases := map[[2]string][]string{
		{"me06", "me06"}:     {"me06", "me6"},
		{"me2pt5", "me02.5"}: {"me2pt5"},
		{"mep", "mep"}:       {"mep"},
		{"me55", "30th"}:     {"me55"},
	}
	for in, want := range cases {
		if got := scrydexSetIDs(in[0], in[1]); !reflect.DeepEqual(got, want) {
			t.Errorf("scrydexSetIDs(%q, %q) = %v, want %v", in[0], in[1], got, want)
		}
	}
}

func TestFillMissingSetArt(t *testing.T) {
	d := setupTestDB(t)
	ctx := context.Background()
	now := time.Now().UTC()
	for _, s := range [][4]string{
		{"me06", "me06", "", ""},            // Delta Reign: under me6 on Scrydex
		{"mep", "mep", "", "https://x/sym"}, // promos: logo missing only
		{"zz1", "zz1", "", ""},              // unknown to Scrydex: placeholder only
		{"me5", "me05", "https://tcgdex/logo.png", "https://tcgdex/sym.png"},
	} {
		if _, err := d.Exec(`INSERT INTO pokemon_sets (id, tcgdex_id, name, series, release_date, logo_url, symbol_url, synced_at) VALUES (?, ?, ?, 'Mega Evolution', '2026/01/01', ?, ?, ?)`,
			s[0], s[1], s[0], s[2], s[3], now); err != nil {
			t.Fatal(err)
		}
	}
	f := newFakeTCGdex()
	f.artDigests = map[string]string{
		// Scrydex's generic picture for any unknown id.
		scrydexArt("hytte-no-such-set", "logo"): "placeholder",
		scrydexArt("me06", "logo"):              "placeholder",
		scrydexArt("zz1", "logo"):               "placeholder",
		scrydexArt("me6", "logo"):               "delta-reign",
		scrydexArt("me6", "symbol"):             "dlr",
		scrydexArt("mep", "logo"):               "promo-star",
	}
	c := newFakeClient(t, f)

	if err := FillMissingSetArt(ctx, d, c); err != nil {
		t.Fatal(err)
	}
	want := map[string][2]string{
		"me06": {scrydexArt("me6", "logo"), scrydexArt("me6", "symbol")},
		"mep":  {scrydexArt("mep", "logo"), "https://x/sym"},
		"zz1":  {"", ""},
		"me5":  {"https://tcgdex/logo.png", "https://tcgdex/sym.png"},
	}
	for id, w := range want {
		var logo, symbol string
		if err := d.QueryRow(`SELECT logo_url, symbol_url FROM pokemon_sets WHERE id = ?`, id).Scan(&logo, &symbol); err != nil {
			t.Fatal(err)
		}
		if logo != w[0] || symbol != w[1] {
			t.Errorf("%s: logo %q symbol %q, want %q %q", id, logo, symbol, w[0], w[1])
		}
	}
}
