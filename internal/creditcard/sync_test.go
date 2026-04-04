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

	// Two purchases (negative belop) and one innbetaling (positive, excluded).
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

	// The entry for March should be 500 + 300 = 800.
	var amount float64
	var count int
	if err := db.QueryRow(
		`SELECT COALESCE(SUM(amount), 0), COUNT(*) FROM budget_variable_entries WHERE variable_id = ? AND month = ?`,
		variableID, "2026-03",
	).Scan(&amount, &count); err != nil {
		t.Fatalf("query entry: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 entry, got %d", count)
	}
	if amount != 800.0 {
		t.Errorf("expected amount 800, got %f", amount)
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

	// Pre-existing entry that should be replaced.
	encOldSubName, err := encryption.EncryptField("old entry")
	if err != nil {
		t.Fatalf("encrypt old sub_name: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO budget_variable_entries (variable_id, month, sub_name, amount) VALUES (?, '2026-02', ?, 9999.0)`,
		variableID, encOldSubName,
	); err != nil {
		t.Fatalf("insert old entry: %v", err)
	}

	// One transaction for February 2026.
	if _, err := db.Exec(`
		INSERT INTO credit_card_transactions
			(user_id, credit_card_id, transaksjonsdato, beskrivelse, belop, is_innbetaling, imported_at)
		VALUES
			(1, 'card-002', '2026-02-20', '', -150.0, 0, '2026-03-01T00:00:00Z')
	`); err != nil {
		t.Fatalf("insert transaction: %v", err)
	}

	if err := SyncCreditCardExpense(db, 1, "card-002", "2026-02"); err != nil {
		t.Fatalf("SyncCreditCardExpense: %v", err)
	}

	var amount float64
	var count int
	if err := db.QueryRow(
		`SELECT COALESCE(SUM(amount), 0), COUNT(*) FROM budget_variable_entries WHERE variable_id = ? AND month = ?`,
		variableID, "2026-02",
	).Scan(&amount, &count); err != nil {
		t.Fatalf("query entry: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 entry, got %d", count)
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
