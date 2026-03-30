package allowance

import (
	"database/sql"
	"testing"

	"github.com/Robin831/Hytte/internal/encryption"
)

// mustEncryptNickname encrypts a nickname string for use in family_links rows.
func mustEncryptNickname(t *testing.T, plain string) string {
	t.Helper()
	enc, err := encryption.EncryptField(plain)
	if err != nil {
		t.Fatalf("encrypt %q: %v", plain, err)
	}
	return enc
}

// insertSoloChore inserts a solo chore owned by parentID and returns its ID.
func insertSoloChore(t *testing.T, db *sql.DB, parentID int64) int64 {
	t.Helper()
	res, err := db.Exec(`
		INSERT INTO allowance_chores
		  (parent_id, child_id, name, description, amount, currency, frequency, icon,
		   requires_approval, active, created_at, completion_mode, min_team_size, team_bonus_pct)
		VALUES (?, NULL, 'Test Chore', '', 10, 'NOK', 'daily', '🧹', 1, 1,
		        '2026-01-01T00:00:00Z', 'solo', 2, 0)
	`, parentID)
	if err != nil {
		t.Fatalf("insertSoloChore: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

// insertPendingCompletion inserts a pending allowance_completion and returns its ID.
func insertPendingCompletion(t *testing.T, db *sql.DB, choreID, childID int64) int64 {
	t.Helper()
	res, err := db.Exec(`
		INSERT INTO allowance_completions
		  (chore_id, child_id, date, status, notes, created_at)
		VALUES (?, ?, '2026-03-30', 'pending', '', '2026-03-30T10:00:00Z')
	`, choreID, childID)
	if err != nil {
		t.Fatalf("insertPendingCompletion: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

// insertTeamMember inserts a row into allowance_team_completions.
func insertTeamMember(t *testing.T, db *sql.DB, completionID, childID int64, joinedAt string) {
	t.Helper()
	if _, err := db.Exec(`
		INSERT INTO allowance_team_completions (completion_id, child_id, joined_at)
		VALUES (?, ?, ?)
	`, completionID, childID, joinedAt); err != nil {
		t.Fatalf("insertTeamMember: %v", err)
	}
}

// setNickname sets (or updates) the encrypted nickname for a family_links row.
func setNickname(t *testing.T, db *sql.DB, parentID, childID int64, nickname string) {
	t.Helper()
	enc := mustEncryptNickname(t, nickname)
	if _, err := db.Exec(
		`UPDATE family_links SET nickname = ? WHERE parent_id = ? AND child_id = ?`,
		enc, parentID, childID,
	); err != nil {
		t.Fatalf("setNickname: %v", err)
	}
}

// TestEnrichWithTeamMemberNames_EmptySlice verifies that an empty slice is a no-op.
func TestEnrichWithTeamMemberNames_EmptySlice(t *testing.T) {
	db := setupTestDB(t)
	if err := enrichWithTeamMemberNames(db, 1, nil); err != nil {
		t.Fatalf("nil slice: %v", err)
	}
	if err := enrichWithTeamMemberNames(db, 1, []CompletionWithDetails{}); err != nil {
		t.Fatalf("empty slice: %v", err)
	}
}

// TestEnrichWithTeamMemberNames_NoTeamRows verifies that a completion with no
// allowance_team_completions rows is left without TeamMemberNames.
func TestEnrichWithTeamMemberNames_NoTeamRows(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	choreID := insertSoloChore(t, db, 1)
	compID := insertPendingCompletion(t, db, choreID, 2)

	completions := []CompletionWithDetails{{ID: compID}}
	if err := enrichWithTeamMemberNames(db, 1, completions); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(completions[0].TeamMemberNames) != 0 {
		t.Errorf("expected no team names, got %v", completions[0].TeamMemberNames)
	}
}

// TestEnrichWithTeamMemberNames_SingleMember verifies that one team member row
// yields one decrypted nickname.
func TestEnrichWithTeamMemberNames_SingleMember(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)
	setNickname(t, db, 1, 2, "Oliver")

	choreID := insertSoloChore(t, db, 1)
	compID := insertPendingCompletion(t, db, choreID, 2)
	insertTeamMember(t, db, compID, 2, "2026-03-30T10:00:00Z")

	completions := []CompletionWithDetails{{ID: compID}}
	if err := enrichWithTeamMemberNames(db, 1, completions); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	names := completions[0].TeamMemberNames
	if len(names) != 1 {
		t.Fatalf("expected 1 name, got %v", names)
	}
	if names[0] != "Oliver" {
		t.Errorf("name: got %q, want %q", names[0], "Oliver")
	}
}

// TestEnrichWithTeamMemberNames_MultipleMembers verifies that multiple team member
// rows are returned in joined_at order with decrypted nicknames.
func TestEnrichWithTeamMemberNames_MultipleMembers(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)
	setNickname(t, db, 1, 2, "Oliver")

	// Add a second child and link them.
	if _, err := db.Exec(`INSERT INTO users (id, email, name, google_id) VALUES (3, 'child2@test.com', 'Child2', 'gc3')`); err != nil {
		t.Fatalf("insert child2: %v", err)
	}
	enc := mustEncryptNickname(t, "Emil")
	if _, err := db.Exec(
		`INSERT INTO family_links (parent_id, child_id, nickname, created_at) VALUES (1, 3, ?, '2026-01-01T00:00:00Z')`,
		enc,
	); err != nil {
		t.Fatalf("link child2: %v", err)
	}

	choreID := insertSoloChore(t, db, 1)
	compID := insertPendingCompletion(t, db, choreID, 2)
	insertTeamMember(t, db, compID, 2, "2026-03-30T10:00:00Z")
	insertTeamMember(t, db, compID, 3, "2026-03-30T10:01:00Z")

	completions := []CompletionWithDetails{{ID: compID}}
	if err := enrichWithTeamMemberNames(db, 1, completions); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	names := completions[0].TeamMemberNames
	if len(names) != 2 {
		t.Fatalf("expected 2 names, got %v", names)
	}
	if names[0] != "Oliver" || names[1] != "Emil" {
		t.Errorf("unexpected names order: %v", names)
	}
}

// TestGetPendingCompletions_TeamNamesEnriched verifies that GetPendingCompletions
// populates TeamMemberNames for team completions end-to-end.
func TestGetPendingCompletions_TeamNamesEnriched(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)
	setNickname(t, db, 1, 2, "Oliver")

	choreID := insertSoloChore(t, db, 1)
	compID := insertPendingCompletion(t, db, choreID, 2)
	insertTeamMember(t, db, compID, 2, "2026-03-30T10:00:00Z")

	results, err := GetPendingCompletions(db, 1)
	if err != nil {
		t.Fatalf("GetPendingCompletions: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if len(results[0].TeamMemberNames) != 1 {
		t.Fatalf("expected 1 team name, got %v", results[0].TeamMemberNames)
	}
	if results[0].TeamMemberNames[0] != "Oliver" {
		t.Errorf("unexpected name: %q", results[0].TeamMemberNames[0])
	}
}
