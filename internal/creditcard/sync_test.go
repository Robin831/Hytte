package creditcard

import (
	"testing"

	"github.com/Robin831/Hytte/internal/encryption"
)

// budgetSchema adds the budget_variable_bills and budget_variable_entries tables
// needed for sync tests. It is called within individual tests since the base
// setupTestDB only creates credit card tables.
const budgetSchema = `
CREATE TABLE IF NOT EXISTS budget_variable_bills (
	id             INTEGER PRIMARY KEY,
	user_id        INTEGER NOT NULL,
	name           TEXT NOT NULL DEFAULT '',
	recurring_id   INTEGER,
	credit_card_id TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS budget_variable_entries (
	id          INTEGER PRIMARY KEY,
	variable_id INTEGER NOT NULL REFERENCES budget_variable_bills(id) ON DELETE CASCADE,
	month       TEXT NOT NULL,
	sub_name    TEXT NOT NULL DEFAULT '',
	amount      REAL NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS credit_card_opening_balances (
	id             INTEGER PRIMARY KEY,
	user_id        INTEGER NOT NULL,
	credit_card_id TEXT NOT NULL,
	month          TEXT NOT NULL,
	balance        REAL NOT NULL DEFAULT 0,
	UNIQUE(user_id, credit_card_id, month)
);
`

func TestSyncCreditCardExpense_NoLinkedBill(t *testing.T) {
	db := setupTestDB(t)
	if _, err := db.Exec(budgetSchema); err != nil {
		t.Fatalf("create budget tables: %v", err)
	}

	// No variable bill linked — should be a no-op.
	if err := SyncCreditCardExpense(db, 1, "card-001", "2026-03"); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestSyncCreditCardExpense_UpdatesLinkedBill(t *testing.T) {
	db := setupTestDB(t)
	if _, err := db.Exec(budgetSchema); err != nil {
		t.Fatalf("create budget tables: %v", err)
	}

	// Create a variable bill linked to card-001.
	encName, err := encryption.EncryptField("Credit Card Bill")
	if err != nil {
		t.Fatalf("encrypt name: %v", err)
	}
	res, err := db.Exec(
		`INSERT INTO budget_variable_bills (user_id, name, credit_card_id) VALUES (1, ?, 'card-001')`,
		encName,
	)
	if err != nil {
		t.Fatalf("insert variable bill: %v", err)
	}
	variableID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id: %v", err)
	}

	// Two purchases (negative belop) and one innbetaling (positive).
	// closing = opening(0) + expenses(800) - payments(2000) = -1200
	if _, err := db.Exec(`
		INSERT INTO credit_card_transactions
			(user_id, credit_card_id, transaksjonsdato, beskrivelse, belop, is_innbetaling, imported_at)
		VALUES
			(1, 'card-001', '2026-03-10', '', -500.0, 0, '2026-04-01T00:00:00Z'),
			(1, 'card-001', '2026-03-15', '', -300.0, 0, '2026-04-01T00:00:00Z'),
			(1, 'card-001', '2026-03-01', '', 2000.0, 1, '2026-04-01T00:00:00Z')
	`); err != nil {
		t.Fatalf("insert transactions: %v", err)
	}

	if err := SyncCreditCardExpense(db, 1, "card-001", "2026-03"); err != nil {
		t.Fatalf("SyncCreditCardExpense: %v", err)
	}

	// closing = 0 (no opening balance) + 800 expenses - 2000 payments = -1200.
	// Entry goes into April (next month) because March expenses are paid in April.
	var amount float64
	var count int
	if err := db.QueryRow(
		`SELECT COALESCE(SUM(amount), 0), COUNT(*) FROM budget_variable_entries WHERE variable_id = ? AND month = ?`,
		variableID, "2026-04",
	).Scan(&amount, &count); err != nil {
		t.Fatalf("query entry: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 entry, got %d", count)
	}
	if amount != -1200.0 {
		t.Errorf("expected amount -1200 (expenses 800 - payments 2000), got %f", amount)
	}
}

func TestSyncCreditCardExpense_WithOpeningBalance(t *testing.T) {
	db := setupTestDB(t)
	if _, err := db.Exec(budgetSchema); err != nil {
		t.Fatalf("create budget tables: %v", err)
	}

	encName, err := encryption.EncryptField("Credit Card Bill")
	if err != nil {
		t.Fatalf("encrypt name: %v", err)
	}
	res, err := db.Exec(
		`INSERT INTO budget_variable_bills (user_id, name, credit_card_id) VALUES (1, ?, 'card-003')`,
		encName,
	)
	if err != nil {
		t.Fatalf("insert variable bill: %v", err)
	}
	variableID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id: %v", err)
	}

	// Set an opening balance of 3000 for March.
	if _, err := db.Exec(
		`INSERT INTO credit_card_opening_balances (user_id, credit_card_id, month, balance) VALUES (1, 'card-003', '2026-03', 3000.0)`,
	); err != nil {
		t.Fatalf("insert opening balance: %v", err)
	}

	// Purchases: 500 + 300 = 800. Payments: 2000.
	if _, err := db.Exec(`
		INSERT INTO credit_card_transactions
			(user_id, credit_card_id, transaksjonsdato, beskrivelse, belop, is_innbetaling, imported_at)
		VALUES
			(1, 'card-003', '2026-03-10', '', -500.0, 0, '2026-04-01T00:00:00Z'),
			(1, 'card-003', '2026-03-15', '', -300.0, 0, '2026-04-01T00:00:00Z'),
			(1, 'card-003', '2026-03-01', '', 2000.0, 1, '2026-04-01T00:00:00Z')
	`); err != nil {
		t.Fatalf("insert transactions: %v", err)
	}

	if err := SyncCreditCardExpense(db, 1, "card-003", "2026-03"); err != nil {
		t.Fatalf("SyncCreditCardExpense: %v", err)
	}

	// closing = 3000 (opening) + 800 (expenses) - 2000 (payments) = 1800.
	var amount float64
	if err := db.QueryRow(
		`SELECT COALESCE(SUM(amount), 0) FROM budget_variable_entries WHERE variable_id = ? AND month = ?`,
		variableID, "2026-04",
	).Scan(&amount); err != nil {
		t.Fatalf("query entry: %v", err)
	}
	if amount != 1800.0 {
		t.Errorf("expected amount 1800 (3000 + 800 - 2000), got %f", amount)
	}
}

func TestSyncCreditCardExpense_ReplacesExistingEntry(t *testing.T) {
	db := setupTestDB(t)
	if _, err := db.Exec(budgetSchema); err != nil {
		t.Fatalf("create budget tables: %v", err)
	}

	encName, err := encryption.EncryptField("Credit Card Bill")
	if err != nil {
		t.Fatalf("encrypt name: %v", err)
	}
	res, err := db.Exec(
		`INSERT INTO budget_variable_bills (user_id, name, credit_card_id) VALUES (1, ?, 'card-002')`,
		encName,
	)
	if err != nil {
		t.Fatalf("insert variable bill: %v", err)
	}
	variableID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id: %v", err)
	}

	// Pre-existing entry for March (the payment month) that should be replaced.
	encOldSubName, err := encryption.EncryptField("old entry")
	if err != nil {
		t.Fatalf("encrypt old sub_name: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO budget_variable_entries (variable_id, month, sub_name, amount) VALUES (?, '2026-03', ?, 9999.0)`,
		variableID, encOldSubName,
	); err != nil {
		t.Fatalf("insert old entry: %v", err)
	}

	// One transaction for February 2026 (no opening balance, no payments).
	if _, err := db.Exec(`
		INSERT INTO credit_card_transactions
			(user_id, credit_card_id, transaksjonsdato, beskrivelse, belop, is_innbetaling, imported_at)
		VALUES
			(1, 'card-002', '2026-02-20', '', -150.0, 0, '2026-03-01T00:00:00Z')
	`); err != nil {
		t.Fatalf("insert transaction: %v", err)
	}

	// Sync Feb transactions — entry should land in March (payment month).
	if err := SyncCreditCardExpense(db, 1, "card-002", "2026-02"); err != nil {
		t.Fatalf("SyncCreditCardExpense: %v", err)
	}

	var amount float64
	var count int
	if err := db.QueryRow(
		`SELECT COALESCE(SUM(amount), 0), COUNT(*) FROM budget_variable_entries WHERE variable_id = ? AND month = ?`,
		variableID, "2026-03",
	).Scan(&amount, &count); err != nil {
		t.Fatalf("query entry: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 entry (old replaced), got %d", count)
	}
	if amount != 150.0 {
		t.Errorf("expected amount 150, got %f", amount)
	}
}

func TestSyncCreditCardExpense_UserIsolation(t *testing.T) {
	db := setupTestDB(t)
	if _, err := db.Exec(budgetSchema); err != nil {
		t.Fatalf("create budget tables: %v", err)
	}

	// User 2 has a variable bill for card-shared.
	encName, err := encryption.EncryptField("Other user bill")
	if err != nil {
		t.Fatalf("encrypt name: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO budget_variable_bills (user_id, name, credit_card_id) VALUES (2, ?, 'card-shared')`,
		encName,
	); err != nil {
		t.Fatalf("insert variable bill for user 2: %v", err)
	}

	// User 1 syncing the same credit_card_id should be a no-op (no bill for user 1).
	if err := SyncCreditCardExpense(db, 1, "card-shared", "2026-03"); err != nil {
		t.Fatalf("expected no-op for user 1, got: %v", err)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM budget_variable_entries`).Scan(&count); err != nil {
		t.Fatalf("count entries: %v", err)
	}
	if count != 0 {
		t.Errorf("expected no entries inserted for wrong user, got %d", count)
	}
}

func TestSyncCreditCardExpense_PendingExcluded(t *testing.T) {
	db := setupTestDB(t)
	if _, err := db.Exec(budgetSchema); err != nil {
		t.Fatalf("create budget tables: %v", err)
	}

	encName, err := encryption.EncryptField("Credit Card Bill")
	if err != nil {
		t.Fatalf("encrypt name: %v", err)
	}
	res, err := db.Exec(
		`INSERT INTO budget_variable_bills (user_id, name, credit_card_id) VALUES (1, ?, 'card-004')`,
		encName,
	)
	if err != nil {
		t.Fatalf("insert variable bill: %v", err)
	}
	variableID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id: %v", err)
	}

	// One settled purchase and one pending purchase.
	if _, err := db.Exec(`
		INSERT INTO credit_card_transactions
			(user_id, credit_card_id, transaksjonsdato, beskrivelse, belop, is_innbetaling, is_pending, imported_at)
		VALUES
			(1, 'card-004', '2026-03-10', '', -400.0, 0, 0, '2026-04-01T00:00:00Z'),
			(1, 'card-004', '2026-03-20', '', -600.0, 0, 1, '2026-04-01T00:00:00Z')
	`); err != nil {
		t.Fatalf("insert transactions: %v", err)
	}

	if err := SyncCreditCardExpense(db, 1, "card-004", "2026-03"); err != nil {
		t.Fatalf("SyncCreditCardExpense: %v", err)
	}

	// Only settled 400 counted; pending 600 excluded.
	var amount float64
	if err := db.QueryRow(
		`SELECT COALESCE(SUM(amount), 0) FROM budget_variable_entries WHERE variable_id = ? AND month = ?`,
		variableID, "2026-04",
	).Scan(&amount); err != nil {
		t.Fatalf("query entry: %v", err)
	}
	if amount != 400.0 {
		t.Errorf("expected 400 (pending excluded), got %f", amount)
	}
}

func TestSyncCreditCardExpense_DeferredExcludedFromOwnPeriod(t *testing.T) {
	db := setupTestDB(t)
	if _, err := db.Exec(budgetSchema); err != nil {
		t.Fatalf("create budget tables: %v", err)
	}

	encName, err := encryption.EncryptField("Credit Card Bill")
	if err != nil {
		t.Fatalf("encrypt name: %v", err)
	}
	res, err := db.Exec(
		`INSERT INTO budget_variable_bills (user_id, name, credit_card_id) VALUES (1, ?, 'card-defer')`,
		encName,
	)
	if err != nil {
		t.Fatalf("insert variable bill: %v", err)
	}
	variableID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id: %v", err)
	}

	// Two March purchases: one normal (300) and one deferred to next month (200).
	// Only the non-deferred 300 should count toward March's closing balance.
	if _, err := db.Exec(`
		INSERT INTO credit_card_transactions
			(user_id, credit_card_id, transaksjonsdato, beskrivelse, belop, is_innbetaling, imported_at, deferred_to_next_month)
		VALUES
			(1, 'card-defer', '2026-03-10', '', -300.0, 0, '2026-04-01T00:00:00Z', 0),
			(1, 'card-defer', '2026-03-28', '', -200.0, 0, '2026-04-01T00:00:00Z', 1)
	`); err != nil {
		t.Fatalf("insert transactions: %v", err)
	}

	if err := SyncCreditCardExpense(db, 1, "card-defer", "2026-03"); err != nil {
		t.Fatalf("SyncCreditCardExpense: %v", err)
	}

	// March closing = 0 (opening) + 300 (non-deferred only) = 300.
	// Entry lands in April (payment month).
	var amount float64
	if err := db.QueryRow(
		`SELECT COALESCE(SUM(amount), 0) FROM budget_variable_entries WHERE variable_id = ? AND month = ?`,
		variableID, "2026-04",
	).Scan(&amount); err != nil {
		t.Fatalf("query entry: %v", err)
	}
	if amount != 300.0 {
		t.Errorf("expected 300 (deferred 200 excluded), got %f", amount)
	}
}

func TestSyncCreditCardExpense_DeferredCarriedToNextPeriod(t *testing.T) {
	db := setupTestDB(t)
	if _, err := db.Exec(budgetSchema); err != nil {
		t.Fatalf("create budget tables: %v", err)
	}

	encName, err := encryption.EncryptField("Credit Card Bill")
	if err != nil {
		t.Fatalf("encrypt name: %v", err)
	}
	res, err := db.Exec(
		`INSERT INTO budget_variable_bills (user_id, name, credit_card_id) VALUES (1, ?, 'card-carry')`,
		encName,
	)
	if err != nil {
		t.Fatalf("insert variable bill: %v", err)
	}
	variableID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id: %v", err)
	}

	// March: deferred purchase of 200 (excluded from March, carried to April).
	// April: normal purchase of 150.
	// Syncing April should include both the April purchase (150) AND the March deferred (200).
	if _, err := db.Exec(`
		INSERT INTO credit_card_transactions
			(user_id, credit_card_id, transaksjonsdato, beskrivelse, belop, is_innbetaling, imported_at, deferred_to_next_month)
		VALUES
			(1, 'card-carry', '2026-03-28', '', -200.0, 0, '2026-04-01T00:00:00Z', 1),
			(1, 'card-carry', '2026-04-05', '', -150.0, 0, '2026-05-01T00:00:00Z', 0)
	`); err != nil {
		t.Fatalf("insert transactions: %v", err)
	}

	if err := SyncCreditCardExpense(db, 1, "card-carry", "2026-04"); err != nil {
		t.Fatalf("SyncCreditCardExpense: %v", err)
	}

	// April closing = 0 (opening) + 150 (April normal) + 200 (March deferred carry-over) = 350.
	// Entry lands in May (payment month).
	var amount float64
	if err := db.QueryRow(
		`SELECT COALESCE(SUM(amount), 0) FROM budget_variable_entries WHERE variable_id = ? AND month = ?`,
		variableID, "2026-05",
	).Scan(&amount); err != nil {
		t.Fatalf("query entry: %v", err)
	}
	if amount != 350.0 {
		t.Errorf("expected 350 (150 April + 200 March deferred), got %f", amount)
	}
}

func TestCollectPeriods(t *testing.T) {
	rows := []DNBRow{
		{Transaksjonsdato: "2026-03-10"},
		{Transaksjonsdato: "2026-03-25"},
		{Transaksjonsdato: "2026-04-01"},
		{Transaksjonsdato: ""},
	}
	periods := collectPeriods(rows)
	if len(periods) != 2 {
		t.Errorf("expected 2 periods, got %d: %v", len(periods), periods)
	}
	if _, ok := periods["2026-03"]; !ok {
		t.Error("expected period 2026-03")
	}
	if _, ok := periods["2026-04"]; !ok {
		t.Error("expected period 2026-04")
	}
}
