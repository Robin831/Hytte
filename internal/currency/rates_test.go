package currency

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const ratesFixture = `FREQ;Frequency;BASE_CUR;Base Currency;QUOTE_CUR;Quote Currency;TENOR;Tenor;DECIMALS;CALCULATED;UNIT_MULT;Unit Multiplier;COLLECTION;Collection Indicator;TIME_PERIOD;OBS_VALUE
B;Business;SEK;Swedish krona;NOK;Norwegian krone;SP;Spot;2;false;2;Hundreds;C;ECB concertation time 14:15 CET;2026-10-09;95.95
B;Business;EUR;Euro;NOK;Norwegian krone;SP;Spot;4;false;0;Units;C;ECB concertation time 14:15 CET;2026-10-09;10.7155
B;Business;JPY;Japanese yen;NOK;Norwegian krone;SP;Spot;4;false;2;Hundreds;C;ECB concertation time 14:15 CET;2026-10-09;6.0423
`

func TestSyncRates_NormalizesUnitMultiplierAndKeepsEURPair(t *testing.T) {
	db := setupTestDB(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(ratesFixture))
	}))
	t.Cleanup(srv.Close)
	prev := overrideRatesURL
	overrideRatesURL = srv.URL
	t.Cleanup(func() { overrideRatesURL = prev })

	if err := SyncRates(context.Background(), db); err != nil {
		t.Fatalf("SyncRates: %v", err)
	}
	rates, err := LatestNOKRates(context.Background(), db)
	if err != nil {
		t.Fatalf("LatestNOKRates: %v", err)
	}
	near := func(got, want float64) bool { return got > want-1e-9 && got < want+1e-9 }
	if !near(rates["SEK"], 0.9595) || !near(rates["JPY"], 0.060423) || !near(rates["EUR"], 10.7155) || rates["NOK"] != 1 {
		t.Fatalf("rates = %v", rates)
	}
	// The EUR row lands under the same pair key the Pokémon prices read.
	if rate, _, err := LatestRate(context.Background(), db, PairEURNOK); err != nil || !near(rate, 10.7155) {
		t.Fatalf("EUR/NOK = %v, %v", rate, err)
	}
}

func TestParseRatesCSV_RejectsMissingColumns(t *testing.T) {
	if _, err := parseRatesCSV(strings.NewReader("TIME_PERIOD;OBS_VALUE\n2026-10-09;1.0\n")); err == nil {
		t.Fatal("expected an error for a header without BASE_CUR/UNIT_MULT")
	}
}
