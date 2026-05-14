// Package currency syncs daily exchange rates from Norges Bank and exposes
// helpers for downstream readers (Pokémon Collection NOK conversion etc.).
package currency

import (
	"context"
	"database/sql"
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// PairEURNOK is the canonical pair key stored in currency_rates for the
// EUR→NOK reference rate.
const PairEURNOK = "EUR/NOK"

// norgesBankEURNOKURL is the public Norges Bank API endpoint that returns the
// most recent EUR/NOK observation as semicolon-separated CSV with Norwegian
// decimal commas.
const norgesBankEURNOKURL = "https://data.norges-bank.no/api/data/EXR/B.EUR.NOK.SP?lastNObservations=1&format=csv-no-utf8"

// httpClient is the HTTP client used for upstream requests. Tests can replace
// it (typically together with overrideURL) to point at a httptest server.
var httpClient = &http.Client{Timeout: 30 * time.Second}

// overrideURL, when non-empty, replaces norgesBankEURNOKURL. Used by tests.
var overrideURL string

// SyncEURNOK fetches the latest EUR/NOK observation from Norges Bank and
// upserts a row into currency_rates keyed by (pair, observed). Calling it
// multiple times on the same observation date is idempotent — the row is
// replaced with a fresh rate and fetched_at value.
func SyncEURNOK(ctx context.Context, db *sql.DB) error {
	url := norgesBankEURNOKURL
	if overrideURL != "" {
		url = overrideURL
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build norges bank request: %w", err)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("fetch norges bank: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("norges bank: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	observed, rate, err := parseEURNOKCSV(resp.Body)
	if err != nil {
		return fmt.Errorf("parse norges bank csv: %w", err)
	}

	_, err = db.ExecContext(ctx, `
		INSERT INTO currency_rates (pair, rate, observed, fetched_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(pair, observed) DO UPDATE SET
			rate       = excluded.rate,
			fetched_at = excluded.fetched_at
	`, PairEURNOK, rate, observed, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("upsert currency_rates: %w", err)
	}
	return nil
}

// LatestRate returns the most recent rate stored for pair, along with the
// observation date it was recorded for. Returns sql.ErrNoRows wrapped in a
// descriptive error if no rate exists yet.
func LatestRate(ctx context.Context, db *sql.DB, pair string) (rate float64, observed time.Time, err error) {
	var observedStr string
	err = db.QueryRowContext(ctx, `
		SELECT rate, observed
		FROM currency_rates
		WHERE pair = ?
		ORDER BY observed DESC
		LIMIT 1
	`, pair).Scan(&rate, &observedStr)
	if err != nil {
		return 0, time.Time{}, fmt.Errorf("query latest rate for %s: %w", pair, err)
	}
	observed, err = time.Parse("2006-01-02", observedStr)
	if err != nil {
		return 0, time.Time{}, fmt.Errorf("parse observed date %q: %w", observedStr, err)
	}
	return rate, observed, nil
}

// parseEURNOKCSV extracts the observation date and rate from a Norges Bank
// csv-no-utf8 response. The format is semicolon-separated with a header row;
// the relevant columns are TIME_PERIOD (observation date, ISO 8601) and
// OBS_VALUE (decimal with a Norwegian comma separator).
func parseEURNOKCSV(r io.Reader) (observed string, rate float64, err error) {
	reader := csv.NewReader(r)
	reader.Comma = ';'
	reader.FieldsPerRecord = -1
	reader.LazyQuotes = true

	header, err := reader.Read()
	if err != nil {
		return "", 0, fmt.Errorf("read csv header: %w", err)
	}

	timeIdx, valueIdx := -1, -1
	for i, col := range header {
		switch strings.TrimSpace(strings.ToUpper(col)) {
		case "TIME_PERIOD":
			timeIdx = i
		case "OBS_VALUE":
			valueIdx = i
		}
	}
	if timeIdx < 0 || valueIdx < 0 {
		return "", 0, fmt.Errorf("missing TIME_PERIOD/OBS_VALUE columns in header %v", header)
	}

	row, err := reader.Read()
	if err != nil {
		return "", 0, fmt.Errorf("read csv row: %w", err)
	}
	if timeIdx >= len(row) || valueIdx >= len(row) {
		return "", 0, fmt.Errorf("short csv row: have %d columns, need %d", len(row), max(timeIdx, valueIdx)+1)
	}

	observed = strings.TrimSpace(row[timeIdx])
	if _, perr := time.Parse("2006-01-02", observed); perr != nil {
		return "", 0, fmt.Errorf("parse observed date %q: %w", observed, perr)
	}

	// Norges Bank csv-no-utf8 uses Norwegian decimal commas (e.g. "11,4567").
	// Strip thousand separators (space) before swapping the comma for a dot.
	raw := strings.TrimSpace(row[valueIdx])
	raw = strings.ReplaceAll(raw, " ", "")
	raw = strings.Replace(raw, ",", ".", 1)
	rate, err = strconv.ParseFloat(raw, 64)
	if err != nil {
		return "", 0, fmt.Errorf("parse rate %q: %w", row[valueIdx], err)
	}
	return observed, rate, nil
}
