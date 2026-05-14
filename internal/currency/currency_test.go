package currency

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`
		CREATE TABLE currency_rates (
			pair       TEXT NOT NULL,
			rate       REAL NOT NULL,
			observed   TEXT NOT NULL,
			fetched_at TIMESTAMP NOT NULL,
			PRIMARY KEY(pair, observed)
		);
	`); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	return db
}

// fixtureCSV is a minimal Norges Bank csv-no-utf8 response with the columns
// we rely on (TIME_PERIOD, OBS_VALUE) plus a handful of the surrounding
// metadata columns the real upstream emits.
const fixtureCSV = "FREQ;Frequency;BASE_CUR;Base Currency;QUOTE_CUR;Quote Currency;TENOR;Tenor;DECIMALS;CALCULATED;UNIT_MULT;COLLECTION;TIME_PERIOD;OBS_VALUE\n" +
	"B;Forretningsdag;EUR;Euro;NOK;Norske kroner;SP;Spot;4;false;0;A;2026-05-14;11,4567\n"

func startFixtureServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func withOverrideURL(t *testing.T, url string) {
	t.Helper()
	prev := overrideURL
	overrideURL = url
	t.Cleanup(func() { overrideURL = prev })
}

func TestSyncEURNOK_InsertsRow(t *testing.T) {
	db := setupTestDB(t)
	srv := startFixtureServer(t, fixtureCSV)
	withOverrideURL(t, srv.URL)

	ctx := context.Background()
	if err := SyncEURNOK(ctx, db); err != nil {
		t.Fatalf("SyncEURNOK: %v", err)
	}

	var pair, observed string
	var rate float64
	var fetchedAt time.Time
	if err := db.QueryRowContext(ctx,
		`SELECT pair, rate, observed, fetched_at FROM currency_rates`).
		Scan(&pair, &rate, &observed, &fetchedAt); err != nil {
		t.Fatalf("query inserted row: %v", err)
	}
	if pair != PairEURNOK {
		t.Errorf("pair = %q, want %q", pair, PairEURNOK)
	}
	if observed != "2026-05-14" {
		t.Errorf("observed = %q, want 2026-05-14", observed)
	}
	if rate != 11.4567 {
		t.Errorf("rate = %v, want 11.4567", rate)
	}
	if fetchedAt.IsZero() {
		t.Errorf("fetched_at should not be zero")
	}
}

func TestSyncEURNOK_IdempotentSameDay(t *testing.T) {
	db := setupTestDB(t)
	srv := startFixtureServer(t, fixtureCSV)
	withOverrideURL(t, srv.URL)

	ctx := context.Background()
	if err := SyncEURNOK(ctx, db); err != nil {
		t.Fatalf("first SyncEURNOK: %v", err)
	}
	if err := SyncEURNOK(ctx, db); err != nil {
		t.Fatalf("second SyncEURNOK: %v", err)
	}

	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM currency_rates`).Scan(&n); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if n != 1 {
		t.Fatalf("row count = %d, want 1 (upsert should not duplicate the same observed date)", n)
	}
}

func TestSyncEURNOK_UpstreamError(t *testing.T) {
	db := setupTestDB(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	withOverrideURL(t, srv.URL)

	if err := SyncEURNOK(context.Background(), db); err == nil {
		t.Fatal("expected error on HTTP 500, got nil")
	}
}

func TestLatestRate_PicksMostRecent(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	now := time.Now().UTC()
	insertRate(t, db, "EUR/NOK", 11.0000, "2026-05-12", now)
	insertRate(t, db, "EUR/NOK", 11.5000, "2026-05-14", now)
	insertRate(t, db, "EUR/NOK", 11.2500, "2026-05-13", now)

	rate, observed, err := LatestRate(ctx, db, "EUR/NOK")
	if err != nil {
		t.Fatalf("LatestRate: %v", err)
	}
	if rate != 11.5000 {
		t.Errorf("rate = %v, want 11.5000", rate)
	}
	want := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC)
	if !observed.Equal(want) {
		t.Errorf("observed = %v, want %v", observed, want)
	}
}

func TestLatestRate_Missing(t *testing.T) {
	db := setupTestDB(t)
	_, _, err := LatestRate(context.Background(), db, "EUR/NOK")
	if err == nil {
		t.Fatal("expected error when no rates exist, got nil")
	}
}

func TestParseEURNOKCSV_RejectsBadDate(t *testing.T) {
	bad := "TIME_PERIOD;OBS_VALUE\n" +
		"not-a-date;11,4567\n"
	_, _, err := parseEURNOKCSV(strings.NewReader(bad))
	if err == nil {
		t.Fatal("expected error for invalid date, got nil")
	}
}

func TestNextDailyRun(t *testing.T) {
	oslo, err := time.LoadLocation("Europe/Oslo")
	if err != nil {
		t.Fatalf("load Oslo: %v", err)
	}
	cases := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{
			name: "before 06:00 returns today",
			now:  time.Date(2026, 5, 14, 5, 59, 59, 0, oslo),
			want: time.Date(2026, 5, 14, 6, 0, 0, 0, oslo),
		},
		{
			name: "after 06:00 returns tomorrow",
			now:  time.Date(2026, 5, 14, 6, 0, 1, 0, oslo),
			want: time.Date(2026, 5, 15, 6, 0, 0, 0, oslo),
		},
		{
			name: "midnight returns today",
			now:  time.Date(2026, 5, 14, 0, 0, 0, 0, oslo),
			want: time.Date(2026, 5, 14, 6, 0, 0, 0, oslo),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := NextDailyRun(tc.now, oslo)
			if !got.Equal(tc.want) {
				t.Errorf("NextDailyRun(%v) = %v, want %v", tc.now, got, tc.want)
			}
		})
	}
}

func insertRate(t *testing.T, db *sql.DB, pair string, rate float64, observed string, fetched time.Time) {
	t.Helper()
	if _, err := db.Exec(
		`INSERT INTO currency_rates (pair, rate, observed, fetched_at) VALUES (?, ?, ?, ?)`,
		pair, rate, observed, fetched,
	); err != nil {
		t.Fatalf("insert rate: %v", err)
	}
}
