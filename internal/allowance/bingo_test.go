package allowance

import (
	"database/sql"
	"encoding/json"
	"testing"
)

// setupBingoDB extends the base test DB with the allowance_bingo_cards table.
func setupBingoDB(t *testing.T) *sql.DB {
	t.Helper()
	db := setupTestDB(t)

	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS allowance_bingo_cards (
			id              INTEGER PRIMARY KEY,
			child_id        INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			parent_id       INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			week_start      TEXT NOT NULL,
			cells           TEXT NOT NULL DEFAULT '[]',
			completed_lines INTEGER NOT NULL DEFAULT 0,
			full_card       INTEGER NOT NULL DEFAULT 0,
			bonus_earned    REAL NOT NULL DEFAULT 0,
			created_at      TEXT NOT NULL DEFAULT '',
			updated_at      TEXT NOT NULL DEFAULT '',
			UNIQUE(child_id, week_start)
		)
	`); err != nil {
		t.Fatalf("create bingo table: %v", err)
	}
	return db
}

// overrideBingoCells replaces the cells JSON in the DB for testing.
func overrideBingoCells(t *testing.T, db *sql.DB, cardID int64, cells []AllowanceBingoCell) {
	t.Helper()
	b, err := json.Marshal(cells)
	if err != nil {
		t.Fatalf("marshal cells: %v", err)
	}
	if _, err := db.Exec(`UPDATE allowance_bingo_cards SET cells = ? WHERE id = ?`, string(b), cardID); err != nil {
		t.Fatalf("override cells: %v", err)
	}
}

// insertApprovedCompletion inserts an approved chore completion for child 2.
func insertApprovedCompletion(t *testing.T, db *sql.DB, choreID int64, date string) {
	t.Helper()
	if _, err := db.Exec(`
		INSERT INTO allowance_completions (chore_id, child_id, date, status, created_at)
		VALUES (?, 2, ?, 'approved', '2026-01-01T00:00:00Z')
		ON CONFLICT(chore_id, child_id, date) DO NOTHING
	`, choreID, date); err != nil {
		t.Fatalf("insert completion chore=%d date=%s: %v", choreID, date, err)
	}
}

// seedChore inserts a minimal chore owned by parent 1.
func seedChore(t *testing.T, db *sql.DB, choreID int64) {
	t.Helper()
	if _, err := db.Exec(`
		INSERT OR IGNORE INTO allowance_chores
		  (id, parent_id, name, amount, frequency, requires_approval, active, created_at)
		VALUES (?, 1, 'enc:dummy', 5.0, 'daily', 0, 1, '2026-01-01T00:00:00Z')
	`, choreID); err != nil {
		t.Fatalf("insert chore %d: %v", choreID, err)
	}
}

// ---- GetOrCreateBingoCard tests ----

func TestGetOrCreateBingoCard_NewCard(t *testing.T) {
	db := setupBingoDB(t)
	linkParentChild(t, db)

	card, err := GetOrCreateBingoCard(db, 2, 1, "2026-01-05")
	if err != nil {
		t.Fatalf("GetOrCreateBingoCard: %v", err)
	}

	if card.ChildID != 2 || card.ParentID != 1 {
		t.Errorf("wrong IDs: child=%d parent=%d", card.ChildID, card.ParentID)
	}
	if card.WeekStart != "2026-01-05" {
		t.Errorf("WeekStart = %q, want 2026-01-05", card.WeekStart)
	}
	if len(card.Cells) != 9 {
		t.Errorf("len(Cells) = %d, want 9", len(card.Cells))
	}
	if card.CompletedLines != 0 || card.FullCard || card.BonusEarned != 0 {
		t.Errorf("new card should have zero progress: lines=%d full=%v bonus=%f",
			card.CompletedLines, card.FullCard, card.BonusEarned)
	}
}

func TestGetOrCreateBingoCard_Idempotent(t *testing.T) {
	db := setupBingoDB(t)
	linkParentChild(t, db)

	c1, err := GetOrCreateBingoCard(db, 2, 1, "2026-01-05")
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	c2, err := GetOrCreateBingoCard(db, 2, 1, "2026-01-05")
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if c1.ID != c2.ID {
		t.Errorf("repeated call returned different IDs: %d vs %d", c1.ID, c2.ID)
	}
}

func TestGetOrCreateBingoCard_DifferentWeeks(t *testing.T) {
	db := setupBingoDB(t)
	linkParentChild(t, db)

	c1, _ := GetOrCreateBingoCard(db, 2, 1, "2026-01-05")
	c2, _ := GetOrCreateBingoCard(db, 2, 1, "2026-01-12")
	if c1.ID == c2.ID {
		t.Error("different weeks should produce different cards")
	}
}

func TestGetOrCreateBingoCard_DeterministicCells(t *testing.T) {
	db1 := setupBingoDB(t)
	db2 := setupBingoDB(t)
	linkParentChild(t, db1)
	linkParentChild(t, db2)

	c1, _ := GetOrCreateBingoCard(db1, 2, 1, "2026-01-05")
	c2, _ := GetOrCreateBingoCard(db2, 2, 1, "2026-01-05")

	for i := range 9 {
		if c1.Cells[i].ChallengeKey != c2.Cells[i].ChallengeKey {
			t.Errorf("cell %d differs: %q vs %q", i, c1.Cells[i].ChallengeKey, c2.Cells[i].ChallengeKey)
		}
	}
}

// ---- CheckBingoLines tests ----

func TestCheckBingoLines_Empty(t *testing.T) {
	lines := CheckBingoLines(make([]AllowanceBingoCell, 9))
	if len(lines) != 0 {
		t.Errorf("expected no lines, got %v", lines)
	}
}

func TestCheckBingoLines_FirstRow(t *testing.T) {
	cells := make([]AllowanceBingoCell, 9)
	cells[0].Completed = true
	cells[1].Completed = true
	cells[2].Completed = true
	lines := CheckBingoLines(cells)
	if len(lines) != 1 || lines[0] != 0 {
		t.Errorf("expected [0], got %v", lines)
	}
}

func TestCheckBingoLines_AntiDiagonal(t *testing.T) {
	cells := make([]AllowanceBingoCell, 9)
	cells[2].Completed = true
	cells[4].Completed = true
	cells[6].Completed = true
	lines := CheckBingoLines(cells)
	if len(lines) != 1 || lines[0] != 7 {
		t.Errorf("expected [7] (anti-diagonal), got %v", lines)
	}
}

func TestCheckBingoLines_FullCard(t *testing.T) {
	cells := make([]AllowanceBingoCell, 9)
	for i := range cells {
		cells[i].Completed = true
	}
	if len(CheckBingoLines(cells)) != 8 {
		t.Errorf("full card should have 8 lines")
	}
}

// ---- isChallengeCompleted tests ----

func TestIsChallengeCompleted_Weekdays(t *testing.T) {
	weekStart := "2026-01-05" // Monday
	comps := []Completion{
		{ChoreID: 1, ChildID: 2, Date: "2026-01-06", Status: "approved"}, // Tuesday
	}
	cases := []struct{ key string; want bool }{
		{"chore_monday", false},
		{"chore_tuesday", true},
		{"chore_wednesday", false},
		{"chore_thursday", false},
		{"chore_friday", false},
		{"chore_weekend", false},
	}
	for _, tc := range cases {
		got := isChallengeCompleted(tc.key, comps, weekStart, 0)
		if got != tc.want {
			t.Errorf("key=%q: got %v, want %v", tc.key, got, tc.want)
		}
	}
}

func TestIsChallengeCompleted_Weekend(t *testing.T) {
	weekStart := "2026-01-05"
	comps := []Completion{
		{ChoreID: 1, ChildID: 2, Date: "2026-01-10", Status: "approved"}, // Saturday
	}
	if !isChallengeCompleted("chore_weekend", comps, weekStart, 0) {
		t.Error("expected chore_weekend to be complete for Saturday")
	}
}

func TestIsChallengeCompleted_TwoInOneDay(t *testing.T) {
	weekStart := "2026-01-05"
	two := []Completion{
		{ChoreID: 1, ChildID: 2, Date: "2026-01-07", Status: "approved"},
		{ChoreID: 2, ChildID: 2, Date: "2026-01-07", Status: "approved"},
	}
	if !isChallengeCompleted("two_in_one_day", two, weekStart, 0) {
		t.Error("expected two_in_one_day with 2 chores on same day")
	}
	one := []Completion{
		{ChoreID: 1, ChildID: 2, Date: "2026-01-07", Status: "approved"},
	}
	if isChallengeCompleted("two_in_one_day", one, weekStart, 0) {
		t.Error("two_in_one_day should be false with only 1 chore in a day")
	}
}

func TestIsChallengeCompleted_CountChallenges(t *testing.T) {
	weekStart := "2026-01-05"
	four := []Completion{
		{ChoreID: 1, ChildID: 2, Date: "2026-01-05", Status: "approved"},
		{ChoreID: 2, ChildID: 2, Date: "2026-01-06", Status: "approved"},
		{ChoreID: 3, ChildID: 2, Date: "2026-01-07", Status: "approved"},
		{ChoreID: 4, ChildID: 2, Date: "2026-01-08", Status: "approved"},
	}
	if !isChallengeCompleted("three_in_week", four, weekStart, 0) {
		t.Error("expected three_in_week with 4 completions")
	}
	if isChallengeCompleted("five_in_week", four, weekStart, 0) {
		t.Error("five_in_week should be false with 4 completions")
	}
}

func TestIsChallengeCompleted_Streak3Days(t *testing.T) {
	weekStart := "2026-01-05"
	streak := []Completion{
		{ChoreID: 1, ChildID: 2, Date: "2026-01-07", Status: "approved"}, // Wed
		{ChoreID: 2, ChildID: 2, Date: "2026-01-08", Status: "approved"}, // Thu
		{ChoreID: 3, ChildID: 2, Date: "2026-01-09", Status: "approved"}, // Fri
	}
	if !isChallengeCompleted("streak_3_days", streak, weekStart, 0) {
		t.Error("expected streak_3_days for Wed-Thu-Fri")
	}
	gap := []Completion{
		{ChoreID: 1, ChildID: 2, Date: "2026-01-05", Status: "approved"}, // Mon
		{ChoreID: 2, ChildID: 2, Date: "2026-01-07", Status: "approved"}, // Wed (gap on Tue)
	}
	if isChallengeCompleted("streak_3_days", gap, weekStart, 0) {
		t.Error("streak_3_days should be false with a gap day")
	}
}

func TestIsChallengeCompleted_QualityBonus(t *testing.T) {
	weekStart := "2026-01-05"
	if !isChallengeCompleted("quality_bonus",
		[]Completion{{ChoreID: 1, ChildID: 2, Date: "2026-01-05", Status: "approved", QualityBonus: 5}},
		weekStart, 0) {
		t.Error("expected quality_bonus with QualityBonus > 0")
	}
	if isChallengeCompleted("quality_bonus",
		[]Completion{{ChoreID: 1, ChildID: 2, Date: "2026-01-05", Status: "approved"}},
		weekStart, 0) {
		t.Error("quality_bonus should be false without a bonus")
	}
}

func TestIsChallengeCompleted_ExtraTask(t *testing.T) {
	weekStart := "2026-01-05"
	if !isChallengeCompleted("extra_task", nil, weekStart, 1) {
		t.Error("expected extra_task with count=1")
	}
	if isChallengeCompleted("extra_task", nil, weekStart, 0) {
		t.Error("extra_task should be false with count=0")
	}
}

// ---- UpdateBingoProgress tests ----

func TestUpdateBingoProgress_MarksCell(t *testing.T) {
	db := setupBingoDB(t)
	linkParentChild(t, db)
	weekStart := "2026-01-05"

	seedChore(t, db, 1)
	insertApprovedCompletion(t, db, 1, "2026-01-05") // Monday

	card, err := GetOrCreateBingoCard(db, 2, 1, weekStart)
	if err != nil {
		t.Fatalf("GetOrCreateBingoCard: %v", err)
	}
	card.Cells[0] = AllowanceBingoCell{ChallengeKey: "chore_monday", Label: "Monday"}
	overrideBingoCells(t, db, card.ID, card.Cells)

	updated, err := UpdateBingoProgress(db, 2, 1, weekStart)
	if err != nil {
		t.Fatalf("UpdateBingoProgress: %v", err)
	}
	if !updated.Cells[0].Completed {
		t.Error("expected cell 0 (chore_monday) to be marked completed")
	}
}

func TestUpdateBingoProgress_AwardsLineBonus(t *testing.T) {
	db := setupBingoDB(t)
	linkParentChild(t, db)
	weekStart := "2026-01-05"

	seedChore(t, db, 1)
	insertApprovedCompletion(t, db, 1, "2026-01-05") // Mon
	insertApprovedCompletion(t, db, 1, "2026-01-06") // Tue
	insertApprovedCompletion(t, db, 1, "2026-01-07") // Wed

	card, _ := GetOrCreateBingoCard(db, 2, 1, weekStart)
	// Set all 9 cells explicitly: row 0 has satisfiable challenges, rows 1-2 have
	// challenges that are NOT satisfied by 3 Mon/Tue/Wed completions, ensuring
	// exactly one line completes regardless of the random seed.
	card.Cells[0] = AllowanceBingoCell{ChallengeKey: "chore_monday",    Label: "Monday"}
	card.Cells[1] = AllowanceBingoCell{ChallengeKey: "chore_tuesday",   Label: "Tuesday"}
	card.Cells[2] = AllowanceBingoCell{ChallengeKey: "chore_wednesday", Label: "Wednesday"}
	card.Cells[3] = AllowanceBingoCell{ChallengeKey: "chore_thursday",  Label: "Thursday"}
	card.Cells[4] = AllowanceBingoCell{ChallengeKey: "chore_friday",    Label: "Friday"}
	card.Cells[5] = AllowanceBingoCell{ChallengeKey: "chore_weekend",   Label: "Weekend"}
	card.Cells[6] = AllowanceBingoCell{ChallengeKey: "two_in_one_day",  Label: "Two in one day"}
	card.Cells[7] = AllowanceBingoCell{ChallengeKey: "five_in_week",    Label: "Five in week"}
	card.Cells[8] = AllowanceBingoCell{ChallengeKey: "extra_task",      Label: "Extra task"}
	overrideBingoCells(t, db, card.ID, card.Cells)

	updated, err := UpdateBingoProgress(db, 2, 1, weekStart)
	if err != nil {
		t.Fatalf("UpdateBingoProgress: %v", err)
	}

	if updated.CompletedLines&1 == 0 {
		t.Error("expected bit 0 (row 0) set in CompletedLines bitmask")
	}
	if updated.BonusEarned != BingoLineBonus {
		t.Errorf("BonusEarned = %f, want %f", updated.BonusEarned, BingoLineBonus)
	}
}

func TestUpdateBingoProgress_Idempotent(t *testing.T) {
	db := setupBingoDB(t)
	linkParentChild(t, db)
	weekStart := "2026-01-05"

	seedChore(t, db, 1)
	insertApprovedCompletion(t, db, 1, "2026-01-05")
	insertApprovedCompletion(t, db, 1, "2026-01-06")
	insertApprovedCompletion(t, db, 1, "2026-01-07")

	card, _ := GetOrCreateBingoCard(db, 2, 1, weekStart)
	card.Cells[0] = AllowanceBingoCell{ChallengeKey: "chore_monday",    Label: "Monday"}
	card.Cells[1] = AllowanceBingoCell{ChallengeKey: "chore_tuesday",   Label: "Tuesday"}
	card.Cells[2] = AllowanceBingoCell{ChallengeKey: "chore_wednesday", Label: "Wednesday"}
	overrideBingoCells(t, db, card.ID, card.Cells)

	first, _ := UpdateBingoProgress(db, 2, 1, weekStart)
	second, err := UpdateBingoProgress(db, 2, 1, weekStart)
	if err != nil {
		t.Fatalf("second UpdateBingoProgress: %v", err)
	}
	if first.BonusEarned != second.BonusEarned {
		t.Errorf("bonus changed on idempotent call: %f -> %f", first.BonusEarned, second.BonusEarned)
	}
	if first.CompletedLines != second.CompletedLines {
		t.Errorf("lines changed on idempotent call: %d -> %d", first.CompletedLines, second.CompletedLines)
	}
}

func TestUpdateBingoProgress_JackpotBonus(t *testing.T) {
	db := setupBingoDB(t)
	linkParentChild(t, db)
	weekStart := "2026-01-05"

	// Seed multiple chores so we can insert completions on different dates
	// without hitting the UNIQUE(chore_id, child_id, date) constraint.
	for i := int64(1); i <= 9; i++ {
		seedChore(t, db, i)
	}
	dates := []string{
		"2026-01-05", // Mon
		"2026-01-06", // Tue
		"2026-01-07", // Wed
		"2026-01-08", // Thu
		"2026-01-09", // Fri
	}
	for i, date := range dates {
		insertApprovedCompletion(t, db, int64(i+1), date)
	}

	card, _ := GetOrCreateBingoCard(db, 2, 1, weekStart)
	// Set all 9 cells to challenges satisfied by the 5 completions above.
	allKeys := []string{
		"chore_monday", "chore_tuesday", "chore_wednesday",
		"chore_thursday", "chore_friday", "three_in_week",
		"five_in_week", "streak_3_days", "streak_3_days",
	}
	for i, key := range allKeys {
		card.Cells[i] = AllowanceBingoCell{ChallengeKey: key, Label: key}
	}
	overrideBingoCells(t, db, card.ID, card.Cells)

	updated, err := UpdateBingoProgress(db, 2, 1, weekStart)
	if err != nil {
		t.Fatalf("UpdateBingoProgress: %v", err)
	}
	if !updated.FullCard {
		t.Error("expected FullCard=true")
	}
	// 8 lines * 15 + 50 jackpot = 170
	want := float64(8)*BingoLineBonus + BingoJackpotBonus
	if updated.BonusEarned != want {
		t.Errorf("BonusEarned = %f, want %f", updated.BonusEarned, want)
	}
}

// ---- GetBingoBonusForWeek tests ----

func TestGetBingoBonusForWeek_NoCard(t *testing.T) {
	db := setupBingoDB(t)
	bonus, err := GetBingoBonusForWeek(db, 99, "2026-01-05")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bonus != 0 {
		t.Errorf("expected 0 for missing card, got %f", bonus)
	}
}

func TestGetBingoBonusForWeek_AfterProgress(t *testing.T) {
	db := setupBingoDB(t)
	linkParentChild(t, db)
	weekStart := "2026-01-05"

	seedChore(t, db, 1)
	insertApprovedCompletion(t, db, 1, "2026-01-05")
	insertApprovedCompletion(t, db, 1, "2026-01-06")
	insertApprovedCompletion(t, db, 1, "2026-01-07")

	card, _ := GetOrCreateBingoCard(db, 2, 1, weekStart)
	// Set all 9 cells explicitly so only row 0 completes with 3 Mon/Tue/Wed completions.
	card.Cells[0] = AllowanceBingoCell{ChallengeKey: "chore_monday",    Label: "Monday"}
	card.Cells[1] = AllowanceBingoCell{ChallengeKey: "chore_tuesday",   Label: "Tuesday"}
	card.Cells[2] = AllowanceBingoCell{ChallengeKey: "chore_wednesday", Label: "Wednesday"}
	card.Cells[3] = AllowanceBingoCell{ChallengeKey: "chore_thursday",  Label: "Thursday"}
	card.Cells[4] = AllowanceBingoCell{ChallengeKey: "chore_friday",    Label: "Friday"}
	card.Cells[5] = AllowanceBingoCell{ChallengeKey: "chore_weekend",   Label: "Weekend"}
	card.Cells[6] = AllowanceBingoCell{ChallengeKey: "two_in_one_day",  Label: "Two in one day"}
	card.Cells[7] = AllowanceBingoCell{ChallengeKey: "five_in_week",    Label: "Five in week"}
	card.Cells[8] = AllowanceBingoCell{ChallengeKey: "extra_task",      Label: "Extra task"}
	overrideBingoCells(t, db, card.ID, card.Cells)

	UpdateBingoProgress(db, 2, 1, weekStart) //nolint:errcheck

	bonus, err := GetBingoBonusForWeek(db, 2, weekStart)
	if err != nil {
		t.Fatalf("GetBingoBonusForWeek: %v", err)
	}
	if bonus != BingoLineBonus {
		t.Errorf("bonus = %f, want %f", bonus, BingoLineBonus)
	}
}
