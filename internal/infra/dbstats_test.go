package infra

import (
	"testing"
)

func TestDBStatsModule_Name(t *testing.T) {
	db := setupTestDB(t)
	mod := NewDBStatsModule(db)

	if mod.Name() != "db_stats" {
		t.Errorf("expected name db_stats, got %s", mod.Name())
	}
	if mod.DisplayName() != "Database Stats" {
		t.Errorf("unexpected display name: %s", mod.DisplayName())
	}
}

func TestDBStatsModule_Check(t *testing.T) {
	db := setupTestDB(t)
	mod := NewDBStatsModule(db)

	// Insert a DNS monitor so user 1 has data in a user-scoped table.
	_, err := db.Exec(
		`INSERT INTO infra_dns_monitors (user_id, name, hostname, record_type, created_at) VALUES (1, 'Test', 'example.com', 'A', '2026-03-16T00:00:00Z')`,
	)
	if err != nil {
		t.Fatalf("insert test data: %v", err)
	}

	result := mod.Check(1)
	if result.Status != StatusOK {
		t.Errorf("expected ok, got %s: %s", result.Status, result.Message)
	}
	if result.Name != "db_stats" {
		t.Errorf("expected name db_stats, got %s", result.Name)
	}

	details, ok := result.Details.(map[string]any)
	if !ok {
		t.Fatalf("expected map details, got %T", result.Details)
	}
	overview, ok := details["overview"].(*DBOverview)
	if !ok {
		t.Fatalf("expected *DBOverview, got %T", details["overview"])
	}

	if overview.PageSize <= 0 {
		t.Errorf("expected positive page size, got %d", overview.PageSize)
	}
	if overview.PageCount <= 0 {
		t.Errorf("expected positive page count, got %d", overview.PageCount)
	}

	// Only user-scoped tables (with user_id column) should appear.
	if len(overview.Tables) == 0 {
		t.Error("expected at least one user-scoped table")
	}

	// The users table should NOT appear (no user_id column).
	for _, tbl := range overview.Tables {
		if tbl.Name == "users" {
			t.Error("users table should not appear in user-scoped stats")
		}
	}

	// The infra_dns_monitors table should appear with 1 row for user 1.
	found := false
	for _, tbl := range overview.Tables {
		if tbl.Name == "infra_dns_monitors" {
			found = true
			if tbl.RowCount != 1 {
				t.Errorf("expected 1 row in infra_dns_monitors for user 1, got %d", tbl.RowCount)
			}
		}
	}
	if !found {
		t.Error("expected to find infra_dns_monitors in user-scoped stats")
	}
}

func TestDBStatsModule_Check_ScopedToUser(t *testing.T) {
	db := setupTestDB(t)

	// Insert a second user.
	_, err := db.Exec(`INSERT INTO users (id, email, name, google_id) VALUES (2, 'other@example.com', 'Other', 'g2')`)
	if err != nil {
		t.Fatalf("insert user 2: %v", err)
	}

	// Insert data for user 2 only.
	_, err = db.Exec(
		`INSERT INTO infra_dns_monitors (user_id, name, hostname, record_type, created_at) VALUES (2, 'Other', 'other.com', 'A', '2026-03-16T00:00:00Z')`,
	)
	if err != nil {
		t.Fatalf("insert test data: %v", err)
	}

	mod := NewDBStatsModule(db)

	// User 1 should see 0 rows in dns_monitors.
	result := mod.Check(1)
	details := result.Details.(map[string]any)
	overview := details["overview"].(*DBOverview)

	for _, tbl := range overview.Tables {
		if tbl.Name == "infra_dns_monitors" && tbl.RowCount != 0 {
			t.Errorf("user 1 should see 0 rows in infra_dns_monitors, got %d", tbl.RowCount)
		}
	}
}
