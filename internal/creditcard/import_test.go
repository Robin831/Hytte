package creditcard

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/encryption"
	_ "modernc.org/sqlite"
)

// setupTestDB creates an in-memory SQLite database with the credit card schema.
func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()

	// Inject a fixed encryption key so EncryptField/DecryptField work in tests.
	t.Setenv("ENCRYPTION_KEY", "test-key-for-creditcard-tests")
	encryption.ResetEncryptionKey()
	t.Cleanup(func() { encryption.ResetEncryptionKey() })

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	// In-memory SQLite requires a single connection to avoid isolated databases.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	t.Cleanup(func() { db.Close() })

	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		t.Fatalf("enable foreign keys: %v", err)
	}

	schema := `
	CREATE TABLE IF NOT EXISTS users (
		id        INTEGER PRIMARY KEY,
		email     TEXT NOT NULL UNIQUE,
		name      TEXT NOT NULL,
		google_id TEXT NOT NULL DEFAULT '',
		is_admin  INTEGER NOT NULL DEFAULT 0
	);
	CREATE TABLE IF NOT EXISTS credit_card_groups (
		id         INTEGER PRIMARY KEY,
		user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		name       TEXT NOT NULL,
		sort_order INTEGER NOT NULL DEFAULT 0,
		UNIQUE(user_id, id)
	);
	CREATE TABLE IF NOT EXISTS credit_card_transactions (
		id                    INTEGER PRIMARY KEY,
		user_id               INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		credit_card_id        TEXT NOT NULL DEFAULT '',
		transaksjonsdato      TEXT NOT NULL DEFAULT '',
		bokforingsdato        TEXT NOT NULL DEFAULT '',
		beskrivelse           TEXT NOT NULL DEFAULT '',
		mottakers_kontonummer TEXT NOT NULL DEFAULT '',
		kid                   TEXT NOT NULL DEFAULT '',
		belop                 REAL NOT NULL DEFAULT 0,
		belop_i_valuta        REAL NOT NULL DEFAULT 0,
		utsatt                INTEGER NOT NULL DEFAULT 0,
		utsatt_periode        TEXT NOT NULL DEFAULT '',
		utlopsdato            TEXT NOT NULL DEFAULT '',
		is_pending            INTEGER NOT NULL DEFAULT 0,
		is_innbetaling        INTEGER NOT NULL DEFAULT 0,
		deferred_to_next_month INTEGER NOT NULL DEFAULT 0,
		group_id              INTEGER,
		imported_at           TEXT NOT NULL DEFAULT '',
		FOREIGN KEY (user_id, group_id) REFERENCES credit_card_groups(user_id, id) ON DELETE SET NULL
	);
	CREATE TABLE IF NOT EXISTS merchant_group_rules (
		id               INTEGER PRIMARY KEY,
		user_id          INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		merchant_pattern TEXT NOT NULL,
		group_id         INTEGER NOT NULL,
		FOREIGN KEY (user_id, group_id) REFERENCES credit_card_groups(user_id, id) ON DELETE CASCADE
	);
	INSERT INTO users (id, email, name, google_id) VALUES (1, 'test@example.com', 'Test User', 'google-test-1');
	`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("setup schema: %v", err)
	}
	return db
}

// withUser injects a user into the request context (mimics auth middleware).
func withUser(r *http.Request, userID int64) *http.Request {
	u := &auth.User{ID: userID}
	return r.WithContext(auth.ContextWithUser(r.Context(), u))
}

// --- parseDNBCSV tests ---

func TestParseDNBCSV_ValidRow(t *testing.T) {
	csv := "Transaksjonsdato;Bokforingsdato;Beskrivelse;Mottakers kontonummer;KID;Belop;Belop i valuta;Utsatt;Utsatt periode;Utlopsdato\n" +
		"15.03.2026;16.03.2026;Rema 1000;;; −234,50;−234,50;;;;\n"

	rows, errs := parseDNBCSV(strings.NewReader(csv))
	if len(errs) != 0 {
		t.Fatalf("unexpected parse errors: %v", errs)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	row := rows[0]
	if row.Transaksjonsdato != "2026-03-15" {
		t.Errorf("transaksjonsdato: got %q, want 2026-03-15", row.Transaksjonsdato)
	}
	if row.Belop != -234.50 {
		t.Errorf("belop: got %f, want -234.50", row.Belop)
	}
	if row.Error != "" {
		t.Errorf("unexpected row error: %s", row.Error)
	}
}

func TestParseDNBCSV_SkipsHeaderRow(t *testing.T) {
	csv := "Transaksjonsdato;Bokforingsdato;Beskrivelse;Mottakers kontonummer;KID;Belop;Belop i valuta;Utsatt;Utsatt periode;Utlopsdato\n" +
		"15.03.2026;16.03.2026;Butikk;;;−100,00;−100,00;;;;\n" +
		"16.03.2026;17.03.2026;Kafé;;;−50,00;−50,00;;;;\n"

	rows, errs := parseDNBCSV(strings.NewReader(csv))
	if len(errs) != 0 {
		t.Fatalf("unexpected parse errors: %v", errs)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
}

func TestParseDNBCSV_IsPending(t *testing.T) {
	csv := "H;H;H;H;H;H;H;H;H;H\n" +
		"15.03.2026;16.03.2026;Reservert Rema 1000;;;−100,00;−100,00;;;;\n"

	rows, _ := parseDNBCSV(strings.NewReader(csv))
	if len(rows) == 0 {
		t.Fatal("expected 1 row")
	}
	if !rows[0].IsPending {
		t.Error("expected IsPending=true for 'Reservert' in beskrivelse")
	}
}

func TestParseDNBCSV_IsInnbetaling(t *testing.T) {
	csv := "H;H;H;H;H;H;H;H;H;H\n" +
		"15.03.2026;16.03.2026;Innbetaling kredittkort;;;1 000,00;1 000,00;;;;\n"

	rows, _ := parseDNBCSV(strings.NewReader(csv))
	if len(rows) == 0 {
		t.Fatal("expected 1 row")
	}
	if !rows[0].IsInnbetaling {
		t.Error("expected IsInnbetaling=true for 'Innbetaling' in beskrivelse")
	}
}

func TestParseDNBCSV_UtsattJa(t *testing.T) {
	csv := "H;H;H;H;H;H;H;H;H;H\n" +
		"15.03.2026;16.03.2026;Test;;;−50,00;−50,00;Ja;2026-04;;  \n"

	rows, _ := parseDNBCSV(strings.NewReader(csv))
	if len(rows) == 0 {
		t.Fatal("expected 1 row")
	}
	if !rows[0].Utsatt {
		t.Error("expected Utsatt=true when column 7 is 'Ja'")
	}
	if rows[0].UtsattPeriode != "2026-04" {
		t.Errorf("utsatt_periode: got %q, want 2026-04", rows[0].UtsattPeriode)
	}
}

func TestParseDNBCSV_TooFewColumns(t *testing.T) {
	csv := "H;H;H;H;H;H;H;H;H;H\n" +
		"15.03.2026;Butikk\n"

	rows, _ := parseDNBCSV(strings.NewReader(csv))
	if len(rows) == 0 {
		t.Fatal("expected 1 row with error")
	}
	if rows[0].Error == "" {
		t.Error("expected an error for row with too few columns")
	}
}

// --- parseDNBAmount tests ---

func TestParseDNBAmount(t *testing.T) {
	tests := []struct {
		input string
		want  float64
	}{
		{"−1 234,56", -1234.56},  // unicode minus + space thousands + comma decimal
		{"-1234,56", -1234.56},   // ASCII minus
		{"1 234,56", 1234.56},    // space thousands separator
		{"100,00", 100.00},
		{"0,00", 0},
		{"", 0},
		{"1234", 1234},
	}
	for _, tc := range tests {
		got, err := parseDNBAmount(tc.input)
		if err != nil {
			t.Errorf("parseDNBAmount(%q): unexpected error: %v", tc.input, err)
			continue
		}
		if got != tc.want {
			t.Errorf("parseDNBAmount(%q): got %f, want %f", tc.input, got, tc.want)
		}
	}
}

// --- parseDNBDate tests ---

func TestParseDNBDate(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"15.03.2026", "2026-03-15"},
		{"01.01.2025", "2025-01-01"},
		{"2026-03-15", "2026-03-15"}, // already ISO
	}
	for _, tc := range tests {
		got, err := parseDNBDate(tc.input)
		if err != nil {
			t.Errorf("parseDNBDate(%q): unexpected error: %v", tc.input, err)
			continue
		}
		if got != tc.want {
			t.Errorf("parseDNBDate(%q): got %q, want %q", tc.input, got, tc.want)
		}
	}
}

// --- applyMerchantRules tests ---

func TestApplyMerchantRules(t *testing.T) {
	rules := []merchantRule{
		{Pattern: "rema", GroupID: 1},
		{Pattern: "kiwi", GroupID: 2},
	}
	if got := applyMerchantRules(rules, "Rema 1000 Majorstuen"); got != 1 {
		t.Errorf("expected groupID 1, got %d", got)
	}
	if got := applyMerchantRules(rules, "Kiwi Butikk"); got != 2 {
		t.Errorf("expected groupID 2, got %d", got)
	}
	if got := applyMerchantRules(rules, "Spar Russ"); got != 0 {
		t.Errorf("expected groupID 0, got %d", got)
	}
}

// --- HTTP handler integration tests ---

func makePreviewRequest(t *testing.T, csvContent, creditCardID string) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("credit_card_id", creditCardID)
	fw, err := mw.CreateFormFile("file", "test.csv")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := io.WriteString(fw, csvContent); err != nil {
		t.Fatalf("write csv: %v", err)
	}
	mw.Close()

	r := httptest.NewRequest(http.MethodPost, "/api/credit-card/import/preview", &buf)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	return r
}

func TestImportPreviewHandler_ValidCSV(t *testing.T) {
	db := setupTestDB(t)

	csvContent := "H;H;H;H;H;H;H;H;H;H\n" +
		"15.03.2026;16.03.2026;Rema 1000;;;−100,00;−100,00;;;;\n" +
		"16.03.2026;17.03.2026;Kafé;;;−50,00;−50,00;;;;\n"

	r := makePreviewRequest(t, csvContent, "card-1234")
	r = withUser(r, 1)
	w := httptest.NewRecorder()

	ImportPreviewHandler(db)(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp ImportPreviewResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.NewCount != 2 {
		t.Errorf("new_count: got %d, want 2", resp.NewCount)
	}
	if resp.SkippedCount != 0 {
		t.Errorf("skipped_count: got %d, want 0", resp.SkippedCount)
	}
}

func TestImportPreviewHandler_MissingCreditCardID(t *testing.T) {
	db := setupTestDB(t)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "test.csv")
	io.WriteString(fw, "H;H;H;H;H;H;H;H;H;H\n") //nolint:errcheck
	mw.Close()

	r := httptest.NewRequest(http.MethodPost, "/api/credit-card/import/preview", &buf)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	r = withUser(r, 1)
	w := httptest.NewRecorder()

	ImportPreviewHandler(db)(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestImportConfirmHandler_InsertsRows(t *testing.T) {
	db := setupTestDB(t)

	rows := []DNBRow{
		{
			Line:             2,
			Transaksjonsdato: "2026-03-15",
			Bokforingsdato:   "2026-03-16",
			Beskrivelse:      "Rema 1000",
			Belop:            -100.0,
			BelopIValuta:     -100.0,
		},
	}
	reqBody, _ := json.Marshal(ImportConfirmRequest{
		CreditCardID: "card-1234",
		Rows:         rows,
	})

	r := httptest.NewRequest(http.MethodPost, "/api/credit-card/import/confirm",
		bytes.NewReader(reqBody))
	r.Header.Set("Content-Type", "application/json")
	r = withUser(r, 1)
	w := httptest.NewRecorder()

	ImportConfirmHandler(db)(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]int
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["imported"] != 1 {
		t.Errorf("imported: got %d, want 1", resp["imported"])
	}

	// Verify the row is in the DB.
	var count int
	db.QueryRow(`SELECT COUNT(*) FROM credit_card_transactions WHERE user_id = 1`).Scan(&count) //nolint:errcheck
	if count != 1 {
		t.Errorf("expected 1 DB row, got %d", count)
	}
}

func TestImportPreviewHandler_Deduplication(t *testing.T) {
	db := setupTestDB(t)

	// Insert a row that matches one of the CSV rows.
	// We need to encrypt beskrivelse as the handler would.
	// For this test we directly insert as plaintext (simulating legacy data).
	db.Exec( //nolint:errcheck
		`INSERT INTO credit_card_transactions
		 (user_id, credit_card_id, transaksjonsdato, bokforingsdato, beskrivelse,
		  mottakers_kontonummer, kid, belop, belop_i_valuta, utsatt, utsatt_periode,
		  utlopsdato, is_pending, is_innbetaling, group_id, imported_at)
		 VALUES (1, 'card-1234', '2026-03-15', '2026-03-16', 'Rema 1000',
		         '', '', -100.0, -100.0, 0, '', '', 0, 0, NULL, '2026-03-15T00:00:00Z')`,
	)

	csvContent := "H;H;H;H;H;H;H;H;H;H\n" +
		"15.03.2026;16.03.2026;Rema 1000;;;−100,00;−100,00;;;;\n" + // duplicate
		"16.03.2026;17.03.2026;Kafé;;;−50,00;−50,00;;;;\n" // new

	r := makePreviewRequest(t, csvContent, "card-1234")
	r = withUser(r, 1)
	w := httptest.NewRecorder()

	ImportPreviewHandler(db)(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp ImportPreviewResponse
	json.NewDecoder(w.Body).Decode(&resp) //nolint:errcheck
	if resp.NewCount != 1 {
		t.Errorf("new_count: got %d, want 1", resp.NewCount)
	}
	if resp.SkippedCount != 1 {
		t.Errorf("skipped_count: got %d, want 1", resp.SkippedCount)
	}
}

func TestImportConfirmHandler_AppliesMerchantRules(t *testing.T) {
	db := setupTestDB(t)

	// Create a group and a rule.
	db.Exec(`INSERT INTO credit_card_groups (id, user_id, name) VALUES (10, 1, 'Dagligvarer')`) //nolint:errcheck
	db.Exec(`INSERT INTO merchant_group_rules (user_id, merchant_pattern, group_id) VALUES (1, 'rema', 10)`) //nolint:errcheck

	rows := []DNBRow{
		{
			Line:             2,
			Transaksjonsdato: "2026-03-15",
			Bokforingsdato:   "2026-03-16",
			Beskrivelse:      "Rema 1000 Majorstuen",
			Belop:            -150.0,
		},
	}
	reqBody, _ := json.Marshal(ImportConfirmRequest{
		CreditCardID: "card-1234",
		Rows:         rows,
	})

	r := httptest.NewRequest(http.MethodPost, "/api/credit-card/import/confirm",
		bytes.NewReader(reqBody))
	r.Header.Set("Content-Type", "application/json")
	r = withUser(r, 1)
	w := httptest.NewRecorder()

	ImportConfirmHandler(db)(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify group_id was set.
	var groupID sql.NullInt64
	db.QueryRow(`SELECT group_id FROM credit_card_transactions WHERE user_id = 1`).Scan(&groupID) //nolint:errcheck
	if !groupID.Valid || groupID.Int64 != 10 {
		t.Errorf("expected group_id=10, got %v", groupID)
	}
}

// TestImportConfirmHandler_ResolvesPendingTransaction verifies that importing a
// settled transaction resolves its matching pending (Reservert) row rather than
// creating a duplicate.
func TestImportConfirmHandler_ResolvesPendingTransaction(t *testing.T) {
	db := setupTestDB(t)

	// Insert a pending transaction: "Reservert - CLAUDE.AI SUBSCRIPTION".
	pendingDesc, err := encryption.EncryptField("Reservert - CLAUDE.AI SUBSCRIPTION")
	if err != nil {
		t.Fatalf("encrypt pending beskrivelse: %v", err)
	}
	_, err = db.Exec(
		`INSERT INTO credit_card_transactions
		 (user_id, credit_card_id, transaksjonsdato, bokforingsdato, beskrivelse,
		  mottakers_kontonummer, kid, belop, belop_i_valuta, utsatt, utsatt_periode,
		  utlopsdato, is_pending, is_innbetaling, group_id, imported_at)
		 VALUES (1, 'card-1234', '2026-03-20', '', ?,
		         '', '', -199.0, -199.0, 0, '', '', 1, 0, NULL, '2026-03-20T00:00:00Z')`,
		pendingDesc,
	)
	if err != nil {
		t.Fatalf("insert pending transaction: %v", err)
	}

	// Now import the settled version: "CLAUDE.AI SUBSCRIPTION" with same date and amount.
	rows := []DNBRow{
		{
			Line:             2,
			Transaksjonsdato: "2026-03-20",
			Bokforingsdato:   "2026-03-22",
			Beskrivelse:      "CLAUDE.AI SUBSCRIPTION",
			Belop:            -199.0,
		},
	}
	reqBody, _ := json.Marshal(ImportConfirmRequest{
		CreditCardID: "card-1234",
		Rows:         rows,
	})

	r := httptest.NewRequest(http.MethodPost, "/api/credit-card/import/confirm",
		bytes.NewReader(reqBody))
	r.Header.Set("Content-Type", "application/json")
	r = withUser(r, 1)
	w := httptest.NewRecorder()

	ImportConfirmHandler(db)(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Only one transaction row should exist (the pending was resolved, not duplicated).
	var count int
	db.QueryRow(`SELECT COUNT(*) FROM credit_card_transactions WHERE user_id = 1 AND credit_card_id = 'card-1234'`).Scan(&count) //nolint:errcheck
	if count != 1 {
		t.Errorf("expected 1 row, got %d (pending should be resolved, not duplicated)", count)
	}

	// The row should now be settled: is_pending=0, updated beskrivelse and bokforingsdato.
	var isPending int
	var encDesc, bokforingsdato string
	db.QueryRow( //nolint:errcheck
		`SELECT is_pending, beskrivelse, bokforingsdato
		 FROM credit_card_transactions
		 WHERE user_id = 1 AND credit_card_id = 'card-1234'`,
	).Scan(&isPending, &encDesc, &bokforingsdato)

	if isPending != 0 {
		t.Errorf("expected is_pending=0, got %d", isPending)
	}
	desc, err := encryption.DecryptField(encDesc)
	if err != nil {
		t.Fatalf("decrypt settled beskrivelse: %v", err)
	}
	if desc != "CLAUDE.AI SUBSCRIPTION" {
		t.Errorf("expected beskrivelse=%q, got %q", "CLAUDE.AI SUBSCRIPTION", desc)
	}
	if bokforingsdato != "2026-03-22" {
		t.Errorf("expected bokforingsdato=%q, got %q", "2026-03-22", bokforingsdato)
	}
}
