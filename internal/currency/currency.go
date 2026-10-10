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
// Norges Bank's SDMX endpoint deprecated `format=csv-no-utf8` in May 2026 — it
// returns HTTP 500 "Could not resolve delimiter 'utf8'". Use plain `format=csv`,
// which still returns semicolon-delimited UTF-8 with the same column headers
// (TIME_PERIOD, OBS_VALUE, etc.) and a dot-decimal OBS_VALUE.
const norgesBankEURNOKURL = "https://data.norges-bank.no/api/data/EXR/B.EUR.NOK.SP?lastNObservations=1&format=csv"

// httpClient is the HTTP client used for upstream requests. Tests can replace
// it (typically together with overrideURL) to point at a httptest server.
var httpClient = &http.Client{Timeout: 30 * time.Second}

// overrideURL, when non-empty, replaces norgesBankEURNOKURL. Used by tests.
var overrideURL string

// maxResponseBytes caps the response body fed to the CSV parser. The real
// Norges Bank response is well under 1 KB; 1 MiB leaves comfortable headroom
// while bounding memory if the upstream misbehaves.
const maxResponseBytes int64 = 1 << 20

// maxErrorBodyBytes caps how much of an HTTP error body we include in the
// returned error message. Truncation is intentional here (it's only for
// diagnostics), so the parser uses io.LimitedReader with limit+1 to detect
// and signal overflow rather than silently swallow it.
const maxErrorBodyBytes int64 = 1024

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
		limited := &io.LimitedReader{R: resp.Body, N: maxErrorBodyBytes + 1}
		body, _ := io.ReadAll(limited)
		truncated := ""
		if int64(len(body)) > maxErrorBodyBytes {
			body = body[:maxErrorBodyBytes]
			truncated = " (truncated)"
		}
		return fmt.Errorf("norges bank: HTTP %d: %s%s", resp.StatusCode, strings.TrimSpace(string(body)), truncated)
	}

	observed, rate, err := parseEURNOKCSV(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return fmt.Errorf("parse norges bank csv: %w", err)
	}
	if rate <= 0 {
		return fmt.Errorf("norges bank: invalid rate %v for %s", rate, PairEURNOK)
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

// RateCurrencies are the currencies SyncRates mirrors against NOK: every
// currency race entry fees are published in, plus EUR (which keeps
// PairEURNOK fresh for the Pokémon prices).
var RateCurrencies = []string{"EUR", "GBP", "USD", "SEK", "DKK", "CHF", "AUD", "JPY", "CZK", "PLN", "HUF", "THB"}

// norgesBankRatesURL fetches the latest observation for every currency in
// one request. The %s is a "+"-joined list of currency codes.
const norgesBankRatesURL = "https://data.norges-bank.no/api/data/EXR/B.%s.NOK.SP?lastNObservations=1&format=csv"

// overrideRatesURL, when non-empty, replaces norgesBankRatesURL. Used by tests.
var overrideRatesURL string

// SyncRates fetches the latest NOK rate for every RateCurrencies entry and
// upserts one currency_rates row per currency ("GBP/NOK", …), normalized to
// NOK per ONE unit — Norges Bank quotes some currencies (SEK, JPY, …) per
// 100, signalled by UNIT_MULT.
func SyncRates(ctx context.Context, db *sql.DB) error {
	url := fmt.Sprintf(norgesBankRatesURL, strings.Join(RateCurrencies, "+"))
	if overrideRatesURL != "" {
		url = overrideRatesURL
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
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
		return fmt.Errorf("norges bank: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	rates, err := parseRatesCSV(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return fmt.Errorf("parse norges bank csv: %w", err)
	}
	if len(rates) == 0 {
		return fmt.Errorf("norges bank: no rates in response")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	fetched := time.Now().UTC()
	for _, r := range rates {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO currency_rates (pair, rate, observed, fetched_at)
			VALUES (?, ?, ?, ?)
			ON CONFLICT(pair, observed) DO UPDATE SET
				rate       = excluded.rate,
				fetched_at = excluded.fetched_at
		`, r.base+"/NOK", r.rate, r.observed, fetched); err != nil {
			return fmt.Errorf("upsert currency_rates: %w", err)
		}
	}
	return tx.Commit()
}

type parsedRate struct {
	base     string
	observed string
	rate     float64
}

// parseRatesCSV reads a multi-currency Norges Bank response: one row per
// currency with BASE_CUR, UNIT_MULT (power of ten the quote is per),
// TIME_PERIOD and OBS_VALUE.
func parseRatesCSV(r io.Reader) ([]parsedRate, error) {
	reader := csv.NewReader(r)
	reader.Comma = ';'
	reader.FieldsPerRecord = -1
	reader.LazyQuotes = true

	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("read csv header: %w", err)
	}
	idx := map[string]int{}
	for i, col := range header {
		idx[strings.TrimSpace(strings.ToUpper(col))] = i
	}
	for _, col := range []string{"BASE_CUR", "UNIT_MULT", "TIME_PERIOD", "OBS_VALUE"} {
		if _, ok := idx[col]; !ok {
			return nil, fmt.Errorf("missing %s column in header %v", col, header)
		}
	}

	var out []parsedRate
	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read csv row: %w", err)
		}
		get := func(col string) string {
			if i := idx[col]; i < len(row) {
				return strings.TrimSpace(row[i])
			}
			return ""
		}
		base := strings.ToUpper(get("BASE_CUR"))
		observed := get("TIME_PERIOD")
		if len(base) != 3 {
			continue
		}
		if _, err := time.Parse("2006-01-02", observed); err != nil {
			return nil, fmt.Errorf("parse observed date %q for %s: %w", observed, base, err)
		}
		value, err := strconv.ParseFloat(strings.Replace(strings.ReplaceAll(get("OBS_VALUE"), " ", ""), ",", ".", 1), 64)
		if err != nil || value <= 0 {
			return nil, fmt.Errorf("parse rate %q for %s", get("OBS_VALUE"), base)
		}
		mult, err := strconv.Atoi(get("UNIT_MULT"))
		if err != nil || mult < 0 || mult > 6 {
			return nil, fmt.Errorf("parse unit multiplier %q for %s", get("UNIT_MULT"), base)
		}
		for i := 0; i < mult; i++ {
			value /= 10
		}
		out = append(out, parsedRate{base: base, observed: observed, rate: value})
	}
	return out, nil
}

// LatestNOKRates returns NOK per one unit for every currency with a stored
// rate, keyed by currency code ("EUR": 10.71), using each pair's newest
// observation. NOK itself is included as 1.
func LatestNOKRates(ctx context.Context, db *sql.DB) (map[string]float64, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT r.pair, r.rate FROM currency_rates r
		WHERE r.pair LIKE '%/NOK'
		  AND r.observed = (SELECT MAX(observed) FROM currency_rates WHERE pair = r.pair)`)
	if err != nil {
		return nil, fmt.Errorf("query latest NOK rates: %w", err)
	}
	defer rows.Close()
	rates := map[string]float64{"NOK": 1}
	for rows.Next() {
		var pair string
		var rate float64
		if err := rows.Scan(&pair, &rate); err != nil {
			return nil, err
		}
		rates[strings.TrimSuffix(pair, "/NOK")] = rate
	}
	return rates, rows.Err()
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
