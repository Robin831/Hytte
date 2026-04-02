package budget

import (
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
)

const maxCSVSize = 10 << 20 // 10 MB

// ColumnMapping specifies which CSV column index (0-based) maps to each field.
// Use -1 to indicate a field is not present in the CSV.
type ColumnMapping struct {
	Date        int `json:"date"`
	Description int `json:"description"`
	Amount      int `json:"amount"`
	Category    int `json:"category"`
}

// ImportRow represents a single parsed row from a CSV preview.
type ImportRow struct {
	Line        int     `json:"line"`
	Date        string  `json:"date"`
	Description string  `json:"description"`
	Amount      float64 `json:"amount"`
	Category    string  `json:"category"`
	// Error is set when the row could not be parsed cleanly.
	Error string `json:"error,omitempty"`
}

// ImportCommitRequest is the request body for the commit endpoint.
type ImportCommitRequest struct {
	AccountID    int64       `json:"account_id"`
	Transactions []ImportRow `json:"transactions"`
}

// CSVPreviewHandler parses an uploaded CSV file and returns the rows for
// user review. The file is processed entirely in memory and never persisted.
//
// Request: multipart/form-data with fields:
//   - file        — the CSV file
//   - mapping     — JSON-encoded ColumnMapping
//   - date_format — optional Go time format string (tries common formats if omitted)
//   - skip_header — "true" (default) to skip the first row
func CSVPreviewHandler(_ *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(maxCSVSize); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "failed to parse form: " + err.Error()})
			return
		}

		f, _, err := r.FormFile("file")
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing file field"})
			return
		}
		defer f.Close() //nolint:errcheck

		mappingRaw := r.FormValue("mapping")
		if mappingRaw == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing mapping field"})
			return
		}
		var mapping ColumnMapping
		if err := json.Unmarshal([]byte(mappingRaw), &mapping); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid mapping JSON: " + err.Error()})
			return
		}

		dateFormat := r.FormValue("date_format")
		skipHeader := r.FormValue("skip_header") != "false"

		rows, parseErrors := parseCSV(f, mapping, dateFormat, skipHeader)
		writeJSON(w, http.StatusOK, map[string]any{
			"rows":   rows,
			"errors": parseErrors,
		})
	}
}

// CSVCommitHandler accepts a reviewed list of transactions and inserts them
// into the database under the specified account.
func CSVCommitHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		var req ImportCommitRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		if req.AccountID == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "account_id is required"})
			return
		}
		if len(req.Transactions) == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no transactions to import"})
			return
		}
		if len(req.Transactions) > 10000 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "too many transactions (max 10000)"})
			return
		}

		// Verify the account belongs to the authenticated user.
		_, err := GetAccount(db, user.ID, req.AccountID)
		if err != nil {
			if err == sql.ErrNoRows {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "account not found"})
			} else {
				log.Printf("budget: import commit get account %d: %v", req.AccountID, err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to verify account"})
			}
			return
		}

		imported := 0
		for _, row := range req.Transactions {
			if row.Error != "" {
				// Skip rows that had parse errors — user should have removed them.
				continue
			}
			t := &Transaction{
				AccountID:   req.AccountID,
				Amount:      row.Amount,
				Description: row.Description,
				Date:        row.Date,
				Tags:        []string{},
			}
			if err := CreateTransaction(db, user.ID, t); err != nil {
				log.Printf("budget: import commit create tx (line %d): %v", row.Line, err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{
					"error": fmt.Sprintf("failed to import row %d: %v", row.Line, err),
				})
				return
			}
			imported++
		}

		writeJSON(w, http.StatusOK, map[string]any{"imported": imported})
	}
}

// parseCSV reads all rows from the CSV and maps them to ImportRow values.
func parseCSV(r io.Reader, m ColumnMapping, dateFormat string, skipHeader bool) ([]ImportRow, []string) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1 // allow variable column counts

	var rows []ImportRow
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
		if skipHeader && lineNum == 1 {
			continue
		}
		if len(record) == 0 {
			continue
		}

		row := ImportRow{Line: lineNum}

		// Date column.
		if m.Date >= 0 && m.Date < len(record) {
			parsed, err := parseDateField(strings.TrimSpace(record[m.Date]), dateFormat)
			if err != nil {
				row.Error = fmt.Sprintf("invalid date %q: %v", record[m.Date], err)
			} else {
				row.Date = parsed
			}
		} else {
			row.Error = "date column out of range"
		}

		// Description column.
		if m.Description >= 0 && m.Description < len(record) {
			row.Description = strings.TrimSpace(record[m.Description])
		}

		// Amount column.
		if m.Amount >= 0 && m.Amount < len(record) {
			amount, err := parseAmount(strings.TrimSpace(record[m.Amount]))
			if err != nil && row.Error == "" {
				row.Error = fmt.Sprintf("invalid amount %q: %v", record[m.Amount], err)
			} else if err == nil {
				row.Amount = amount
			}
		} else if row.Error == "" {
			row.Error = "amount column out of range"
		}

		// Category column (optional — use -1 to omit).
		if m.Category >= 0 && m.Category < len(record) {
			row.Category = strings.TrimSpace(record[m.Category])
		}

		rows = append(rows, row)
	}

	if rows == nil {
		rows = []ImportRow{}
	}
	if errs == nil {
		errs = []string{}
	}
	return rows, errs
}

// knownDateFormats are tried in order when no explicit format hint is given.
var knownDateFormats = []string{
	"2006-01-02",
	"02.01.2006",
	"2.1.2006",
	"01/02/2006",
	"1/2/2006",
	"02-01-2006",
	"2006/01/02",
}

func parseDateField(s, hint string) (string, error) {
	if s == "" {
		return "", fmt.Errorf("empty date")
	}
	if hint != "" {
		t, err := time.Parse(hint, s)
		if err != nil {
			return "", err
		}
		return t.Format("2006-01-02"), nil
	}
	for _, layout := range knownDateFormats {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Format("2006-01-02"), nil
		}
	}
	return "", fmt.Errorf("unrecognised date format")
}

// parseAmount handles common numeric formats found in spreadsheet CSV exports:
//   - "1 234,56"  — Norwegian: space thousands separator, comma decimal
//   - "1,234.56"  — US: comma thousands separator, dot decimal
//   - "-1234.56"  — leading minus
//   - "(1234.56)" — parentheses for negative
//   - "1234"      — integer
func parseAmount(s string) (float64, error) {
	if s == "" {
		return 0, nil
	}

	neg := false
	if strings.HasPrefix(s, "(") && strings.HasSuffix(s, ")") {
		neg = true
		s = s[1 : len(s)-1]
	}
	if strings.HasPrefix(s, "-") {
		neg = true
		s = s[1:]
	}

	// Strip spaces and non-breaking spaces (thousands separators).
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, "\u00a0", "")

	// Determine the decimal separator: the rightmost of . or , is the decimal.
	lastDot := strings.LastIndex(s, ".")
	lastComma := strings.LastIndex(s, ",")

	switch {
	case lastDot > lastComma:
		// Dot is decimal; remove commas used as thousands separators.
		s = strings.ReplaceAll(s, ",", "")
	case lastComma > lastDot:
		// Comma is decimal; remove dots used as thousands separators, then normalise.
		s = strings.ReplaceAll(s, ".", "")
		s = strings.ReplaceAll(s, ",", ".")
	}

	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, err
	}
	if neg {
		v = -v
	}
	return v, nil
}
