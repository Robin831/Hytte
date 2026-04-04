package creditcard

import (
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/encryption"
)

const maxCSVSize = 10 << 20      // 10 MB
const maxConfirmBodySize = 5 << 20 // 5 MB

// DNBRow represents a single parsed row from a DNB credit card CSV export.
type DNBRow struct {
	Line                 int     `json:"line"`
	Transaksjonsdato     string  `json:"transaksjonsdato"`       // YYYY-MM-DD
	Bokforingsdato       string  `json:"bokforingsdato"`         // YYYY-MM-DD
	Beskrivelse          string  `json:"beskrivelse"`
	MottakersKontonummer string  `json:"mottakers_kontonummer"`
	KID                  string  `json:"kid"`
	Belop                float64 `json:"belop"`
	BelopIValuta         float64 `json:"belop_i_valuta"`
	Utsatt               bool    `json:"utsatt"`
	UtsattPeriode        string  `json:"utsatt_periode"`
	Utlopsdato           string  `json:"utlopsdato"`
	IsPending            bool    `json:"is_pending"`
	IsInnbetaling        bool    `json:"is_innbetaling"`
	Error                string  `json:"error,omitempty"`
}

// ImportPreviewResponse is returned by the preview endpoint.
type ImportPreviewResponse struct {
	NewCount     int      `json:"new_count"`
	SkippedCount int      `json:"skipped_count"`
	Rows         []DNBRow `json:"rows"`
}

// ImportConfirmRequest is the body for the confirm endpoint.
type ImportConfirmRequest struct {
	CreditCardID string   `json:"credit_card_id"`
	Rows         []DNBRow `json:"rows"`
}

// merchantRule is a single rule loaded from merchant_group_rules.
type merchantRule struct {
	Pattern string
	GroupID int64
}

// ImportPreviewHandler parses an uploaded DNB credit card CSV file and returns
// a deduped preview of rows that would be inserted.
//
// Request: multipart/form-data
//   - file:           the CSV file (required)
//   - credit_card_id: identifier for the card, used to scope duplicate checks (required)
func ImportPreviewHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		r.Body = http.MaxBytesReader(w, r.Body, maxCSVSize)
		if err := r.ParseMultipartForm(maxCSVSize); err != nil {
			var maxBytesErr *http.MaxBytesError
			if errors.As(err, &maxBytesErr) {
				writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "uploaded file exceeds 10 MB limit"})
				return
			}
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "failed to parse form: " + err.Error()})
			return
		}
		defer func() {
			if r.MultipartForm != nil {
				_ = r.MultipartForm.RemoveAll()
			}
		}()

		creditCardID := r.FormValue("credit_card_id")
		if creditCardID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "credit_card_id is required"})
			return
		}

		f, _, err := r.FormFile("file")
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing file field"})
			return
		}
		defer f.Close() //nolint:errcheck

		rows, parseErrs := parseDNBCSV(f)
		if len(parseErrs) > 0 {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error":        "CSV parse errors",
				"parse_errors": parseErrs,
			})
			return
		}

		newRows, skippedCount, err := deduplicateRows(db, user.ID, creditCardID, rows)
		if err != nil {
			log.Printf("creditcard: preview dedup: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to check for duplicates"})
			return
		}

		writeJSON(w, http.StatusOK, ImportPreviewResponse{
			NewCount:     len(newRows),
			SkippedCount: skippedCount,
			Rows:         newRows,
		})
	}
}

// ImportConfirmHandler accepts a confirmed list of rows from a prior preview
// and inserts them into the database. After insertion, merchant_group_rules are
// applied to auto-assign group_id.
//
// Request body (JSON):
//   - credit_card_id: identifier for the card
//   - rows:           array of DNBRow values (as returned by the preview endpoint)
func ImportConfirmHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		r.Body = http.MaxBytesReader(w, r.Body, maxConfirmBodySize)
		var req ImportConfirmRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "request body too large"})
				return
			}
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		if req.CreditCardID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "credit_card_id is required"})
			return
		}
		if len(req.Rows) == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no rows to import"})
			return
		}
		if len(req.Rows) > 10000 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "too many rows (max 10000)"})
			return
		}

		for i, row := range req.Rows {
			rowRef := fmt.Sprintf("row %d", i+1)
			if row.Line > 0 {
				rowRef = fmt.Sprintf("CSV line %d (row %d)", row.Line, i+1)
			}
			if row.Error != "" {
				writeJSON(w, http.StatusBadRequest, map[string]string{
					"error": fmt.Sprintf("%s has a parse error and must be fixed before import", rowRef),
				})
				return
			}
			if row.Transaksjonsdato == "" {
				writeJSON(w, http.StatusBadRequest, map[string]string{
					"error": fmt.Sprintf("%s is missing transaksjonsdato", rowRef),
				})
				return
			}
			// Re-validate date formats server-side (comment 3).
			if _, err := parseDNBDate(row.Transaksjonsdato); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{
					"error": fmt.Sprintf("%s has invalid transaksjonsdato: %v", rowRef, err),
				})
				return
			}
			if row.Bokforingsdato != "" {
				if _, err := parseDNBDate(row.Bokforingsdato); err != nil {
					writeJSON(w, http.StatusBadRequest, map[string]string{
						"error": fmt.Sprintf("%s has invalid bokforingsdato: %v", rowRef, err),
					})
					return
				}
			}
			if math.IsNaN(row.Belop) || math.IsInf(row.Belop, 0) {
				writeJSON(w, http.StatusBadRequest, map[string]string{
					"error": fmt.Sprintf("%s has invalid belop", rowRef),
				})
				return
			}
			if math.IsNaN(row.BelopIValuta) || math.IsInf(row.BelopIValuta, 0) {
				writeJSON(w, http.StatusBadRequest, map[string]string{
					"error": fmt.Sprintf("%s has invalid belop_i_valuta", rowRef),
				})
				return
			}
			// Recompute IsPending/IsInnbetaling from Beskrivelse server-side to
			// prevent clients from supplying incorrect classification flags.
			req.Rows[i].IsPending = strings.Contains(row.Beskrivelse, "Reservert")
			req.Rows[i].IsInnbetaling = strings.Contains(row.Beskrivelse, "Innbetaling")
		}

		// Seed a default 'Diverse' group on the user's first import.
		if _, err := EnsureDefaultGroup(db, user.ID); err != nil {
			log.Printf("creditcard: ensure default group: %v", err)
			// Non-fatal — continue without a default group.
		}

		// Re-run deduplication to prevent double-imports (e.g. confirm called twice).
		dedupedRows, skipped, err := deduplicateRows(db, user.ID, req.CreditCardID, req.Rows)
		if err != nil {
			log.Printf("creditcard: confirm dedup: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to check for duplicates"})
			return
		}
		if skipped > 0 {
			log.Printf("creditcard: confirm: skipped %d duplicate rows for user %d card %s", skipped, user.ID, req.CreditCardID)
		}
		if len(dedupedRows) == 0 {
			writeJSON(w, http.StatusOK, map[string]any{"imported": 0, "skipped": skipped})
			return
		}

		rules, err := loadMerchantGroupRules(db, user.ID)
		if err != nil {
			log.Printf("creditcard: confirm load rules: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load merchant rules"})
			return
		}

		tx, err := db.Begin()
		if err != nil {
			log.Printf("creditcard: confirm begin tx: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to start transaction"})
			return
		}
		defer tx.Rollback() //nolint:errcheck

		imported := 0
		importedAt := time.Now().Format(time.RFC3339)

		for _, row := range dedupedRows {
			groupID := applyMerchantRules(rules, row.Beskrivelse)

			encDesc, err := encryption.EncryptField(row.Beskrivelse)
			if err != nil {
				log.Printf("creditcard: encrypt beskrivelse: %v", err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to encrypt transaction data"})
				return
			}

			utsattInt := boolToInt(row.Utsatt)
			isPendingInt := boolToInt(row.IsPending)
			isInnbetalingInt := boolToInt(row.IsInnbetaling)

			var groupIDVal interface{}
			if groupID > 0 {
				groupIDVal = groupID
			}

			_, err = tx.Exec(
				`INSERT INTO credit_card_transactions
				 (user_id, credit_card_id, transaksjonsdato, bokforingsdato, beskrivelse,
				  mottakers_kontonummer, kid, belop, belop_i_valuta, utsatt, utsatt_periode,
				  utlopsdato, is_pending, is_innbetaling, group_id, imported_at)
				 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				user.ID, req.CreditCardID,
				row.Transaksjonsdato, row.Bokforingsdato, encDesc,
				row.MottakersKontonummer, row.KID,
				row.Belop, row.BelopIValuta,
				utsattInt, row.UtsattPeriode, row.Utlopsdato,
				isPendingInt, isInnbetalingInt,
				groupIDVal, importedAt,
			)
			if err != nil {
				log.Printf("creditcard: confirm insert row (line %d): %v", row.Line, err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to insert transaction"})
				return
			}
			imported++
		}

		if err := tx.Commit(); err != nil {
			log.Printf("creditcard: confirm commit: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to commit import"})
			return
		}

		// Sync the linked variable bill for each affected billing period.
		// Errors here are non-fatal — the import already succeeded.
		for period := range collectPeriods(dedupedRows) {
			if err := SyncCreditCardExpense(db, user.ID, req.CreditCardID, period); err != nil {
				log.Printf("creditcard: sync variable expense for card %s period %s: %v", req.CreditCardID, period, err)
			}
		}

		writeJSON(w, http.StatusOK, map[string]any{"imported": imported, "skipped": skipped})
	}
}

// collectPeriods returns the unique YYYY-MM billing periods represented by the
// given rows (derived from each row's transaksjonsdato).
func collectPeriods(rows []DNBRow) map[string]struct{} {
	periods := make(map[string]struct{})
	for _, row := range rows {
		if len(row.Transaksjonsdato) >= 7 {
			periods[row.Transaksjonsdato[:7]] = struct{}{}
		}
	}
	return periods
}

// parseDNBCSV reads a DNB credit card CSV export (semicolon-delimited, header
// row, 10 columns) and returns a slice of DNBRow values.
//
// The DNB export format columns (0-based):
//   0: Transaksjonsdato (dd.mm.yyyy)
//   1: Bokføringsdato   (dd.mm.yyyy)
//   2: Beskrivelse
//   3: Mottakers kontonummer
//   4: KID
//   5: Beløp            (Norwegian number format, may use unicode minus U+2212)
//   6: Beløp i valuta
//   7: Utsatt           ("Ja" or empty)
//   8: Utsatt periode
//   9: Utløpsdato       (dd.mm.yyyy or empty)
func parseDNBCSV(r io.Reader) ([]DNBRow, []string) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return []DNBRow{}, []string{"failed to read CSV: " + err.Error()}
	}

	cr := csv.NewReader(strings.NewReader(string(raw)))
	cr.Comma = ';'
	cr.FieldsPerRecord = -1 // allow variable column counts
	cr.LazyQuotes = true

	var rows []DNBRow
	var errs []string
	lineNum := 0

	for {
		record, err := cr.Read()
		if err == io.EOF {
			break
		}
		lineNum++
		if err != nil {
			errs = append(errs, fmt.Sprintf("line %d: CSV parse error: %v", lineNum, err))
			continue
		}
		// Skip header row.
		if lineNum == 1 {
			continue
		}
		// Skip empty lines.
		if len(record) == 0 || (len(record) == 1 && strings.TrimSpace(record[0]) == "") {
			continue
		}

		row := DNBRow{Line: lineNum}

		if len(record) < 10 {
			row.Error = fmt.Sprintf("expected 10 columns, got %d", len(record))
			rows = append(rows, row)
			continue
		}

		// Column 0: Transaksjonsdato
		tdate, err := parseDNBDate(strings.TrimSpace(record[0]))
		if err != nil {
			row.Error = fmt.Sprintf("invalid transaksjonsdato %q: %v", record[0], err)
		} else {
			row.Transaksjonsdato = tdate
		}

		// Column 1: Bokføringsdato
		bdate, err := parseDNBDate(strings.TrimSpace(record[1]))
		if err != nil && row.Error == "" {
			// Bokforingsdato is optional — log but don't fail the row.
			log.Printf("creditcard: line %d: invalid bokforingsdato %q: %v", lineNum, record[1], err)
		} else {
			row.Bokforingsdato = bdate
		}

		// Column 2: Beskrivelse
		row.Beskrivelse = strings.TrimSpace(record[2])

		// Column 3: Mottakers kontonummer
		row.MottakersKontonummer = strings.TrimSpace(record[3])

		// Column 4: KID
		row.KID = strings.TrimSpace(record[4])

		// Column 5: Beløp
		belop, err := parseDNBAmount(strings.TrimSpace(record[5]))
		if err != nil && row.Error == "" {
			row.Error = fmt.Sprintf("invalid belop %q: %v", record[5], err)
		} else {
			row.Belop = belop
		}

		// Column 6: Beløp i valuta
		belopValuta, err := parseDNBAmount(strings.TrimSpace(record[6]))
		if err == nil {
			row.BelopIValuta = belopValuta
		}

		// Column 7: Utsatt — "Ja" means deferred.
		row.Utsatt = strings.EqualFold(strings.TrimSpace(record[7]), "ja")

		// Column 8: Utsatt periode
		row.UtsattPeriode = strings.TrimSpace(record[8])

		// Column 9: Utløpsdato (may be empty)
		if v := strings.TrimSpace(record[9]); v != "" {
			utlopsdato, err := parseDNBDate(v)
			if err == nil {
				row.Utlopsdato = utlopsdato
			}
		}

		// Classify row based on Beskrivelse.
		row.IsPending = strings.Contains(row.Beskrivelse, "Reservert")
		row.IsInnbetaling = strings.Contains(row.Beskrivelse, "Innbetaling")

		rows = append(rows, row)
	}

	if rows == nil {
		rows = []DNBRow{}
	}
	if errs == nil {
		errs = []string{}
	}
	return rows, errs
}

// belopToOere converts a float64 amount to integer øre (hundredths) for stable
// deduplication comparisons, avoiding float64 equality pitfalls.
func belopToOere(f float64) int64 {
	return int64(math.Round(f * 100))
}

// deduplicateRows loads existing transactions for the user+card from the DB,
// decrypts their beskrivelse, and returns only rows that are not already present.
// Duplicate detection key: (transaksjonsdato, beskrivelse, belop in øre).
func deduplicateRows(db *sql.DB, userID int64, creditCardID string, rows []DNBRow) ([]DNBRow, int, error) {
	type dedupKey struct {
		transaksjonsdato string
		beskrivelse      string
		belopOere        int64 // stored as øre to avoid float64 equality issues
	}

	existing := make(map[dedupKey]struct{})

	dbRows, err := db.Query(
		`SELECT transaksjonsdato, beskrivelse, belop
		 FROM credit_card_transactions
		 WHERE user_id = ? AND credit_card_id = ?`,
		userID, creditCardID,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("query existing transactions: %w", err)
	}
	defer dbRows.Close()

	for dbRows.Next() {
		var tdate, encDesc string
		var belop float64
		if err := dbRows.Scan(&tdate, &encDesc, &belop); err != nil {
			return nil, 0, fmt.Errorf("scan existing row: %w", err)
		}
		desc, err := encryption.DecryptField(encDesc)
		if err != nil {
			// Decryption failed for an existing row; do not use raw stored value
			// for dedup matching because imported rows use plaintext descriptions.
			log.Printf("creditcard: dedup decrypt beskrivelse failed for existing row dated %s amount %.2f: %v (skipping row for dedup matching)", tdate, belop, err)
			continue
		}
		existing[dedupKey{tdate, desc, belopToOere(belop)}] = struct{}{}
	}
	if err := dbRows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate existing rows: %w", err)
	}

	var newRows []DNBRow
	skipped := 0
	for _, row := range rows {
		if row.Error != "" {
			newRows = append(newRows, row)
			continue
		}
		k := dedupKey{row.Transaksjonsdato, row.Beskrivelse, belopToOere(row.Belop)}
		if _, dup := existing[k]; dup {
			skipped++
		} else {
			newRows = append(newRows, row)
			// Add to existing set so we deduplicate within the CSV itself.
			existing[k] = struct{}{}
		}
	}

	if newRows == nil {
		newRows = []DNBRow{}
	}
	return newRows, skipped, nil
}

// loadMerchantGroupRules fetches all merchant_group_rules for the user.
func loadMerchantGroupRules(db *sql.DB, userID int64) ([]merchantRule, error) {
	rows, err := db.Query(
		`SELECT merchant_pattern, group_id FROM merchant_group_rules WHERE user_id = ?`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rules []merchantRule
	for rows.Next() {
		var r merchantRule
		if err := rows.Scan(&r.Pattern, &r.GroupID); err != nil {
			return nil, err
		}
		rules = append(rules, r)
	}
	return rules, rows.Err()
}

// applyMerchantRules returns the group_id from the first rule whose pattern is
// found (case-insensitive substring match) in the given beskrivelse. Returns 0
// if no rule matches.
func applyMerchantRules(rules []merchantRule, beskrivelse string) int64 {
	lower := strings.ToLower(beskrivelse)
	for _, r := range rules {
		if strings.Contains(lower, strings.ToLower(r.Pattern)) {
			return r.GroupID
		}
	}
	return 0
}

// parseDNBDate parses a date in "dd.mm.yyyy" format and returns "yyyy-mm-dd".
func parseDNBDate(s string) (string, error) {
	if s == "" {
		return "", fmt.Errorf("empty date")
	}
	// Try dd.mm.yyyy first (standard DNB format).
	if t, err := time.Parse("02.01.2006", s); err == nil {
		return t.Format("2006-01-02"), nil
	}
	// Fallback: already in yyyy-mm-dd.
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t.Format("2006-01-02"), nil
	}
	return "", fmt.Errorf("unrecognised date format %q", s)
}

// parseDNBAmount parses a Norwegian-formatted amount string. It handles:
//   - Unicode minus (U+2212 −) in addition to ASCII hyphen
//   - Space or non-breaking space as thousands separator
//   - Comma as decimal separator
//   - Empty string → 0
func parseDNBAmount(s string) (float64, error) {
	if s == "" {
		return 0, nil
	}

	// Normalise unicode minus (U+2212) to ASCII hyphen.
	s = strings.ReplaceAll(s, "\u2212", "-")

	neg := false
	if strings.HasPrefix(s, "-") {
		neg = true
		s = s[1:]
	}

	// Strip spaces and non-breaking spaces (thousands separators).
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, "\u00a0", "")

	// Norwegian format uses comma as decimal separator.
	// Replace comma with dot.
	s = strings.ReplaceAll(s, ",", ".")

	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, err
	}
	if neg {
		v = -v
	}
	return v, nil
}

// boolToInt converts a bool to SQLite integer (0 or 1).
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("creditcard: writeJSON encode error: %v", err)
	}
}
