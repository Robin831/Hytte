package allowance

import (
	"database/sql"
	"errors"
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

// TestGetChildChoresWithStatus_TeamParticipantSeesCompletion verifies that a child
// who joined a team session via allowance_team_completions (but is NOT the
// allowance_completions.child_id initiator) gets a non-nil completion_status,
// so the chore is not shown as available/clickable.
func TestGetChildChoresWithStatus_TeamParticipantSeesCompletion(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db) // links parent(1) → child1(2)

	// Add a second child (ID=3) linked to the same parent.
	if _, err := db.Exec(`INSERT INTO users (id, email, name, google_id) VALUES (3, 'child2@test.com', 'Child2', 'gc3')`); err != nil {
		t.Fatalf("insert child2: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO family_links (parent_id, child_id, created_at) VALUES (1, 3, '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("link parent-child2: %v", err)
	}

	choreID := insertSoloChore(t, db, 1)

	// child1 (ID=2) initiates the completion; child2 (ID=3) joins as participant.
	compID := insertPendingCompletion(t, db, choreID, 2)
	insertTeamMember(t, db, compID, 3, "2026-03-30T10:01:00Z")

	// Ask for child2's chore list — the chore should appear as already completed.
	chores, err := GetChildChoresWithStatus(db, 1, 3, "2026-03-30")
	if err != nil {
		t.Fatalf("GetChildChoresWithStatus: %v", err)
	}
	if len(chores) != 1 {
		t.Fatalf("expected 1 chore, got %d", len(chores))
	}
	if chores[0].CompletionStatus == nil {
		t.Error("expected non-nil CompletionStatus for team participant, got nil (chore appears available)")
	}
}

// ---- RecordSoloCompletionByParent tests ----

func TestRecordSoloCompletionByParent_Approved(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)
	choreID := insertSoloChore(t, db, 1)

	comp, err := RecordSoloCompletionByParent(db, choreID, 2, 1, "2026-04-15", "great job", "approved")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if comp.Status != "approved" {
		t.Errorf("status: got %q, want %q", comp.Status, "approved")
	}
	if comp.ApprovedBy == nil || *comp.ApprovedBy != 1 {
		t.Errorf("approved_by: got %v, want 1", comp.ApprovedBy)
	}
	if comp.ApprovedAt == nil || *comp.ApprovedAt == "" {
		t.Error("approved_at should be set")
	}
	if comp.Notes != "great job" {
		t.Errorf("notes: got %q, want %q", comp.Notes, "great job")
	}

	// Verify the notes are encrypted in the DB.
	var encNotes string
	if err := db.QueryRow(`SELECT notes FROM allowance_completions WHERE id = ?`, comp.ID).Scan(&encNotes); err != nil {
		t.Fatalf("query notes: %v", err)
	}
	if encNotes == "great job" {
		t.Error("notes should be encrypted in the database")
	}
}

func TestRecordSoloCompletionByParent_Pending(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)
	choreID := insertSoloChore(t, db, 1)

	comp, err := RecordSoloCompletionByParent(db, choreID, 2, 1, "2026-04-15", "", "pending")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if comp.Status != "pending" {
		t.Errorf("status: got %q, want %q", comp.Status, "pending")
	}
	if comp.ApprovedBy != nil {
		t.Errorf("approved_by should be nil for pending, got %v", *comp.ApprovedBy)
	}
	if comp.ApprovedAt != nil {
		t.Errorf("approved_at should be nil for pending, got %v", *comp.ApprovedAt)
	}
}

func TestRecordSoloCompletionByParent_DuplicateReturnsExists(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)
	choreID := insertSoloChore(t, db, 1)

	if _, err := RecordSoloCompletionByParent(db, choreID, 2, 1, "2026-04-15", "", "approved"); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	_, err := RecordSoloCompletionByParent(db, choreID, 2, 1, "2026-04-15", "", "approved")
	if err != ErrCompletionExists {
		t.Fatalf("expected ErrCompletionExists, got %v", err)
	}
}

// ---- RecordTeamCompletionByParent tests ----

func TestRecordTeamCompletionByParent_Success(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	// Add a second child.
	if _, err := db.Exec(`INSERT INTO users (id, email, name, google_id) VALUES (3, 'child2@test.com', 'Child2', 'gc3')`); err != nil {
		t.Fatalf("insert child2: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO family_links (parent_id, child_id, created_at) VALUES (1, 3, '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("link child2: %v", err)
	}

	childID := int64(2)
	choreID := insertTeamChore(t, db, 1, &childID, 20, 2, 10)

	comp, err := RecordTeamCompletionByParent(db, choreID, 1, []int64{2, 3}, "2026-04-15", "team effort", "approved")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if comp.ChildID != 2 {
		t.Errorf("child_id: got %d, want 2", comp.ChildID)
	}
	if comp.Status != "approved" {
		t.Errorf("status: got %q, want %q", comp.Status, "approved")
	}

	// Verify team_completions rows.
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM allowance_team_completions WHERE completion_id = ?`, comp.ID).Scan(&count); err != nil {
		t.Fatalf("count team completions: %v", err)
	}
	if count != 2 {
		t.Errorf("team_completions count: got %d, want 2", count)
	}
}

func TestRecordTeamCompletionByParent_BelowMinTeamSize(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	childID := int64(2)
	choreID := insertTeamChore(t, db, 1, &childID, 20, 3, 10) // min_team_size=3

	_, err := RecordTeamCompletionByParent(db, choreID, 1, []int64{2}, "2026-04-15", "", "approved")
	if err != ErrTeamTooSmall {
		t.Fatalf("expected ErrTeamTooSmall, got %v", err)
	}
}

func TestRecordTeamCompletionByParent_EmptyChildIDsReturnsErrTeamTooSmall(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	childID := int64(2)
	choreID := insertTeamChore(t, db, 1, &childID, 20, 2, 10) // min_team_size=2

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("RecordTeamCompletionByParent panicked for empty childIDs: %v", r)
		}
	}()

	_, err := RecordTeamCompletionByParent(db, choreID, 1, []int64{}, "2026-04-15", "", "approved")
	if err != ErrTeamTooSmall {
		t.Fatalf("expected ErrTeamTooSmall for empty childIDs, got %v", err)
	}
}

func TestRecordTeamCompletionByParent_ExistingActiveSessionConflicts(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	// Add a second child.
	if _, err := db.Exec(`INSERT INTO users (id, email, name, google_id) VALUES (3, 'child2@test.com', 'Child2', 'gc3')`); err != nil {
		t.Fatalf("insert child2: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO family_links (parent_id, child_id, created_at) VALUES (1, 3, '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("link child2: %v", err)
	}

	childID := int64(2)
	choreID := insertTeamChore(t, db, 1, &childID, 20, 2, 10)

	// First: create an active session via StartTeamCompletion.
	if _, err := StartTeamCompletion(db, 1, choreID, 2, "2026-04-15"); err != nil {
		t.Fatalf("start team session: %v", err)
	}

	// Second: attempt parent-recorded team completion for the same chore+date.
	_, err := RecordTeamCompletionByParent(db, choreID, 1, []int64{2, 3}, "2026-04-15", "", "approved")
	if err != ErrTeamSessionConflict {
		t.Fatalf("expected ErrTeamSessionConflict, got %v", err)
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

// ---- UnapproveCompletion tests ----

// unapproveTestWeek is the Monday of the week containing 2026-03-01, the date
// seedPendingCompletions gives the first completion.
const unapproveTestWeek = "2026-02-23"

// seedApprovedCompletion creates one completion for child 2 under parent 1,
// approves it and adds a quality bonus.
func seedApprovedCompletion(t *testing.T, db *sql.DB) int64 {
	t.Helper()
	id := seedPendingCompletions(t, db, 1)[0]
	if _, err := ApproveCompletion(db, id, 1); err != nil {
		t.Fatalf("ApproveCompletion: %v", err)
	}
	if _, err := AddQualityBonus(db, id, 1, 5); err != nil {
		t.Fatalf("AddQualityBonus: %v", err)
	}
	return id
}

func TestUnapproveCompletion_ResetsToPending(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)
	id := seedApprovedCompletion(t, db)

	comp, err := UnapproveCompletion(db, id, 1)
	if err != nil {
		t.Fatalf("UnapproveCompletion: %v", err)
	}
	if comp.Status != "pending" {
		t.Errorf("expected status pending, got %q", comp.Status)
	}
	if comp.QualityBonus != 0 || comp.ApprovedBy != nil || comp.ApprovedAt != nil {
		t.Errorf("expected cleared approval fields, got bonus=%v by=%v at=%v", comp.QualityBonus, comp.ApprovedBy, comp.ApprovedAt)
	}

	var status string
	var bonus float64
	var approvedBy sql.NullInt64
	var approvedAt, unapprovedAt sql.NullString
	if err := db.QueryRow(`
		SELECT status, quality_bonus, approved_by, approved_at, unapproved_at
		FROM allowance_completions WHERE id = ?
	`, id).Scan(&status, &bonus, &approvedBy, &approvedAt, &unapprovedAt); err != nil {
		t.Fatalf("read completion: %v", err)
	}
	if status != "pending" || bonus != 0 || approvedBy.Valid || approvedAt.Valid {
		t.Errorf("row not reset: status=%q bonus=%v approved_by=%v approved_at=%v", status, bonus, approvedBy, approvedAt)
	}
	if !unapprovedAt.Valid {
		t.Error("expected unapproved_at to be set")
	}

	pending, err := GetPendingCompletions(db, 1)
	if err != nil {
		t.Fatalf("GetPendingCompletions: %v", err)
	}
	found := false
	for _, p := range pending {
		if p.ID == id {
			found = true
		}
	}
	if !found {
		t.Errorf("expected completion %d in pending list", id)
	}
}

func TestUnapproveCompletion_NotImmediatelyAutoApproved(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)
	id := seedApprovedCompletion(t, db)

	// Backdate created_at well past the auto-approve window.
	if _, err := db.Exec(`UPDATE allowance_completions SET created_at = '2026-01-01T00:00:00Z' WHERE id = ?`, id); err != nil {
		t.Fatalf("backdate: %v", err)
	}
	if _, err := UnapproveCompletion(db, id, 1); err != nil {
		t.Fatalf("UnapproveCompletion: %v", err)
	}
	n, err := AutoApproveStaleCompletions(db, 1, 0, 24)
	if err != nil {
		t.Fatalf("AutoApproveStaleCompletions: %v", err)
	}
	if n != 0 {
		t.Errorf("expected no auto-approvals, got %d", n)
	}
	if got := completionStatus(t, db, id); got != "pending" {
		t.Errorf("expected pending, got %q", got)
	}
}

func TestUnapproveCompletion_AutoApprovedAfterWindow(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)
	id := seedApprovedCompletion(t, db)

	if _, err := UnapproveCompletion(db, id, 1); err != nil {
		t.Fatalf("UnapproveCompletion: %v", err)
	}
	// Once the un-approval itself is older than the window, the completion
	// becomes eligible for auto-approval again.
	if _, err := db.Exec(`UPDATE allowance_completions SET created_at = '2026-01-01T00:00:00Z', unapproved_at = '2026-01-02T00:00:00Z' WHERE id = ?`, id); err != nil {
		t.Fatalf("backdate: %v", err)
	}
	n, err := AutoApproveStaleCompletions(db, 1, 0, 24)
	if err != nil {
		t.Fatalf("AutoApproveStaleCompletions: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 auto-approval, got %d", n)
	}
	if got := completionStatus(t, db, id); got != "approved" {
		t.Errorf("expected approved, got %q", got)
	}
}

func TestUpsertPayout_LeavesPaidRowUntouched(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)

	payout, err := UpsertPayout(db, 1, 2, unapproveTestWeek, 20, 5, 25)
	if err != nil {
		t.Fatalf("UpsertPayout: %v", err)
	}
	if _, err := MarkPayoutPaid(db, payout.ID, 1); err != nil {
		t.Fatalf("MarkPayoutPaid: %v", err)
	}

	got, err := UpsertPayout(db, 1, 2, unapproveTestWeek, 10, 0, 10)
	if err != nil {
		t.Fatalf("UpsertPayout after paid: %v", err)
	}
	if !got.PaidOut {
		t.Error("expected payout to remain paid")
	}
	if got.BaseAmount != 20 || got.BonusAmount != 5 || got.TotalAmount != 25 {
		t.Errorf("paid amounts changed: base %v bonus %v total %v", got.BaseAmount, got.BonusAmount, got.TotalAmount)
	}
}

func TestUnapproveCompletion_EarningsAndPayoutDrop(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)
	id := seedApprovedCompletion(t, db)

	before, err := CalculateWeeklyEarnings(db, 1, 2, unapproveTestWeek)
	if err != nil {
		t.Fatalf("CalculateWeeklyEarnings before: %v", err)
	}
	if before.TotalAmount <= 0 {
		t.Fatalf("expected positive earnings before unapprove, got %v", before.TotalAmount)
	}
	if _, err := UpsertPayout(db, 1, 2, unapproveTestWeek, before.BaseAllowance, before.BonusAmount, before.TotalAmount); err != nil {
		t.Fatalf("UpsertPayout: %v", err)
	}

	if _, err := UnapproveCompletion(db, id, 1); err != nil {
		t.Fatalf("UnapproveCompletion: %v", err)
	}

	after, err := CalculateWeeklyEarnings(db, 1, 2, unapproveTestWeek)
	if err != nil {
		t.Fatalf("CalculateWeeklyEarnings after: %v", err)
	}
	if after.TotalAmount >= before.TotalAmount {
		t.Errorf("expected earnings to drop, before=%v after=%v", before.TotalAmount, after.TotalAmount)
	}

	var stored float64
	if err := db.QueryRow(`
		SELECT total_amount FROM allowance_payouts WHERE parent_id = 1 AND child_id = 2 AND week_start = ?
	`, unapproveTestWeek).Scan(&stored); err != nil {
		t.Fatalf("read payout: %v", err)
	}
	if stored != after.TotalAmount {
		t.Errorf("expected stored payout refreshed to %v, got %v", after.TotalAmount, stored)
	}
}

func TestUnapproveCompletion_PaidWeekRefused(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)
	id := seedApprovedCompletion(t, db)

	payout, err := UpsertPayout(db, 1, 2, unapproveTestWeek, 0, 0, 25)
	if err != nil {
		t.Fatalf("UpsertPayout: %v", err)
	}
	if _, err := MarkPayoutPaid(db, payout.ID, 1); err != nil {
		t.Fatalf("MarkPayoutPaid: %v", err)
	}

	if _, err := UnapproveCompletion(db, id, 1); !errors.Is(err, ErrWeekAlreadyPaid) {
		t.Fatalf("expected ErrWeekAlreadyPaid, got %v", err)
	}
	if got := completionStatus(t, db, id); got != "approved" {
		t.Errorf("expected status to stay approved, got %q", got)
	}
}

func TestUnapproveCompletion_TeamParticipantPaidWeekRefused(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)
	if _, err := db.Exec(`INSERT INTO users (id, email, name, google_id) VALUES (3, 'sib@test.com', 'Sibling', 'gs3')`); err != nil {
		t.Fatalf("seed sibling: %v", err)
	}
	id := seedApprovedCompletion(t, db)
	insertTeamMember(t, db, id, 2, "2026-03-01T10:00:00Z")
	insertTeamMember(t, db, id, 3, "2026-03-01T10:05:00Z")

	payout, err := UpsertPayout(db, 1, 3, unapproveTestWeek, 0, 0, 10)
	if err != nil {
		t.Fatalf("UpsertPayout: %v", err)
	}
	if _, err := MarkPayoutPaid(db, payout.ID, 1); err != nil {
		t.Fatalf("MarkPayoutPaid: %v", err)
	}

	if _, err := UnapproveCompletion(db, id, 1); !errors.Is(err, ErrWeekAlreadyPaid) {
		t.Fatalf("expected ErrWeekAlreadyPaid, got %v", err)
	}
	if got := completionStatus(t, db, id); got != "approved" {
		t.Errorf("expected status to stay approved, got %q", got)
	}
}

func TestUnapproveCompletion_UnpaidPayoutAllowed(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)
	id := seedApprovedCompletion(t, db)

	if _, err := UpsertPayout(db, 1, 2, unapproveTestWeek, 0, 0, 25); err != nil {
		t.Fatalf("UpsertPayout: %v", err)
	}
	if _, err := UnapproveCompletion(db, id, 1); err != nil {
		t.Fatalf("expected unpaid week to allow unapprove, got %v", err)
	}
}

func TestUnapproveCompletion_NotFound(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)
	id := seedApprovedCompletion(t, db)

	if _, err := UnapproveCompletion(db, 99999, 1); !errors.Is(err, ErrCompletionNotFound) {
		t.Errorf("unknown id: expected ErrCompletionNotFound, got %v", err)
	}
	if _, err := UnapproveCompletion(db, id, 42); !errors.Is(err, ErrCompletionNotFound) {
		t.Errorf("other parent: expected ErrCompletionNotFound, got %v", err)
	}
	if got := completionStatus(t, db, id); got != "approved" {
		t.Errorf("expected status to stay approved, got %q", got)
	}
}

func TestUnapproveCompletion_NotApproved(t *testing.T) {
	db := setupTestDB(t)
	linkParentChild(t, db)
	ids := seedPendingCompletions(t, db, 2)
	if _, err := RejectCompletion(db, ids[1], 1, ""); err != nil {
		t.Fatalf("RejectCompletion: %v", err)
	}

	if _, err := UnapproveCompletion(db, ids[0], 1); !errors.Is(err, ErrCompletionNotApproved) {
		t.Errorf("pending: expected ErrCompletionNotApproved, got %v", err)
	}
	if _, err := UnapproveCompletion(db, ids[1], 1); !errors.Is(err, ErrCompletionNotApproved) {
		t.Errorf("rejected: expected ErrCompletionNotApproved, got %v", err)
	}
	if got := completionStatus(t, db, ids[1]); got != "rejected" {
		t.Errorf("expected rejected to stay rejected, got %q", got)
	}
}
