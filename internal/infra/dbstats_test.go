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
	if len(overview.Tables) == 0 {
		t.Error("expected at least one table")
	}

	// The test DB has a users table with one inserted row.
	found := false
	for _, tbl := range overview.Tables {
		if tbl.Name == "users" {
			found = true
			if tbl.RowCount != 1 {
				t.Errorf("expected 1 row in users table, got %d", tbl.RowCount)
			}
		}
	}
	if !found {
		t.Error("expected to find users table in stats")
	}
}
