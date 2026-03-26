package stars

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"
)

// addBingoSchema extends the shared test DB with the bingo_cards table.
func addBingoSchema(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS bingo_cards (
			id              INTEGER PRIMARY KEY,
			user_id         INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			week_key        TEXT NOT NULL,
			cells           TEXT NOT NULL DEFAULT '[]',
			completed_lines TEXT NOT NULL DEFAULT '[]',
			jackpot_awarded INTEGER NOT NULL DEFAULT 0,
			created_at      TEXT NOT NULL DEFAULT '',
			updated_at      TEXT NOT NULL DEFAULT '',
			UNIQUE(user_id, week_key)
		);
		CREATE INDEX IF NOT EXISTS idx_bingo_user_week ON bingo_cards(user_id, week_key);
	`)
	if err != nil {
		t.Fatalf("add bingo schema: %v", err)
	}
}

func TestGetOrCreateBingoCard_CreatesCard(t *testing.T) {
	db := setupTestDB(t)
	addBingoSchema(t, db)

	parentID := insertUser(t, db, "parent@example.com")
	childID := insertUser(t, db, "child@example.com")
	linkChild(t, db, parentID, childID)

	card, err := GetOrCreateBingoCard(context.Background(), db, childID)
	if err != nil {
		t.Fatalf("GetOrCreateBingoCard: %v", err)
	}

	if len(card.Cells) != 9 {
		t.Errorf("expected 9 cells, got %d", len(card.Cells))
	}
	if card.WeekKey == "" {
		t.Error("expected non-empty week_key")
	}
	if card.JackpotAwarded {
		t.Error("expected jackpot_awarded=false on new card")
	}
	if len(card.CompletedLines) != 0 {
		t.Errorf("expected 0 completed lines, got %d", len(card.CompletedLines))
	}
}

func TestGetOrCreateBingoCard_IdempotentWithinWeek(t *testing.T) {
	db := setupTestDB(t)
	addBingoSchema(t, db)

	parentID := insertUser(t, db, "parent@example.com")
	childID := insertUser(t, db, "child@example.com")
	linkChild(t, db, parentID, childID)

	card1, err := GetOrCreateBingoCard(context.Background(), db, childID)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	card2, err := GetOrCreateBingoCard(context.Background(), db, childID)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}

	if card1.ID != card2.ID {
		t.Errorf("expected same card ID, got %d vs %d", card1.ID, card2.ID)
	}
	for i := range card1.Cells {
		if card1.Cells[i].ChallengeKey != card2.Cells[i].ChallengeKey {
			t.Errorf("cell %d challenge key changed: %q vs %q",
				i, card1.Cells[i].ChallengeKey, card2.Cells[i].ChallengeKey)
		}
	}
}

func TestGetOrCreateBingoCard_RandomnessAcrossUsers(t *testing.T) {
	db := setupTestDB(t)
	addBingoSchema(t, db)

	parentID := insertUser(t, db, "parent@example.com")
	child1 := insertUser(t, db, "child1@example.com")
	child2 := insertUser(t, db, "child2@example.com")
	linkChild(t, db, parentID, child1)
	linkChild(t, db, parentID, child2)

	card1, _ := GetOrCreateBingoCard(context.Background(), db, child1)
	card2, _ := GetOrCreateBingoCard(context.Background(), db, child2)

	// Layouts may differ between users (not guaranteed but very likely with 15 pool).
	same := true
	for i := range card1.Cells {
		if card1.Cells[i].ChallengeKey != card2.Cells[i].ChallengeKey {
			same = false
			break
		}
	}
	if same {
		t.Log("note: two users got the same bingo card layout (rare but possible)")
	}
}

func TestGetOrCreateBingoCard_NoDuplicateChallenges(t *testing.T) {
	db := setupTestDB(t)
	addBingoSchema(t, db)

	parentID := insertUser(t, db, "parent@example.com")
	childID := insertUser(t, db, "child@example.com")
	linkChild(t, db, parentID, childID)

	card, _ := GetOrCreateBingoCard(context.Background(), db, childID)

	seen := map[string]bool{}
	for _, cell := range card.Cells {
		if seen[cell.ChallengeKey] {
			t.Errorf("duplicate challenge key on card: %q", cell.ChallengeKey)
		}
		seen[cell.ChallengeKey] = true
	}
}

func TestCheckBingoLines_Rows(t *testing.T) {
	makeCompleted := func(indices ...int) []BingoCell {
		cells := make([]BingoCell, 9)
		for _, i := range indices {
			cells[i].Completed = true
		}
		return cells
	}

	tests := []struct {
		name      string
		cells     []BingoCell
		wantLines []int
	}{
		{"row0", makeCompleted(0, 1, 2), []int{0}},
		{"row1", makeCompleted(3, 4, 5), []int{1}},
		{"row2", makeCompleted(6, 7, 8), []int{2}},
		{"col0", makeCompleted(0, 3, 6), []int{3}},
		{"col1", makeCompleted(1, 4, 7), []int{4}},
		{"col2", makeCompleted(2, 5, 8), []int{5}},
		{"diag_main", makeCompleted(0, 4, 8), []int{6}},
		{"diag_anti", makeCompleted(2, 4, 6), []int{7}},
		{"no_line", makeCompleted(0, 1), []int(nil)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := checkBingoLines(tt.cells)
			if len(got) != len(tt.wantLines) {
				t.Errorf("got lines %v, want %v", got, tt.wantLines)
				return
			}
			for i, l := range tt.wantLines {
				if got[i] != l {
					t.Errorf("line[%d]: got %d, want %d", i, got[i], l)
				}
			}
		})
	}
}

func TestCheckBingoLines_FullCard(t *testing.T) {
	cells := make([]BingoCell, 9)
	for i := range cells {
		cells[i].Completed = true
	}
	lines := checkBingoLines(cells)
	if len(lines) != 8 {
		t.Errorf("full card: expected 8 lines, got %d", len(lines))
	}
}

func TestUpdateBingoProgress_MarksCell(t *testing.T) {
	db := setupTestDB(t)
	addBingoSchema(t, db)

	parentID := insertUser(t, db, "parent@example.com")
	childID := insertUser(t, db, "child@example.com")
	linkChild(t, db, parentID, childID)

	// Pre-create the card so we can inspect and manipulate it.
	card, err := GetOrCreateBingoCard(context.Background(), db, childID)
	if err != nil {
		t.Fatalf("GetOrCreateBingoCard: %v", err)
	}

	// Force a known challenge into cell 0 so we can trigger it reliably.
	forceBingoCell(t, db, card.ID, 0, "workout_30min")

	workoutID := insertWorkout(t, db, childID, 35*60, 3000, 200, 0, 0)

	w := WorkoutInput{
		ID:              workoutID,
		DurationSeconds: 35 * 60,
		DistanceMeters:  3000,
		Calories:        200,
	}
	if err := UpdateBingoProgress(context.Background(), db, childID, w); err != nil {
		t.Fatalf("UpdateBingoProgress: %v", err)
	}

	updated, _ := GetOrCreateBingoCard(context.Background(), db, childID)
	if !updated.Cells[0].Completed {
		t.Error("expected cell 0 to be completed after 35-minute workout")
	}
}

func TestUpdateBingoProgress_NonChildNoOp(t *testing.T) {
	db := setupTestDB(t)
	addBingoSchema(t, db)

	// Non-child user (no family_links row).
	userID := insertUser(t, db, "adult@example.com")
	workoutID := insertWorkout(t, db, userID, 35*60, 5000, 300, 0, 0)

	w := WorkoutInput{ID: workoutID, DurationSeconds: 35 * 60, DistanceMeters: 5000}
	if err := UpdateBingoProgress(context.Background(), db, userID, w); err != nil {
		t.Fatalf("unexpected error for non-child: %v", err)
	}

	// No bingo card should have been created.
	var count int
	db.QueryRow(`SELECT COUNT(*) FROM bingo_cards WHERE user_id = ?`, userID).Scan(&count)
	if count != 0 {
		t.Errorf("expected no bingo cards for non-child, got %d", count)
	}
}

func TestUpdateBingoProgress_LineAwardsStars(t *testing.T) {
	db := setupTestDB(t)
	addBingoSchema(t, db)

	parentID := insertUser(t, db, "parent@example.com")
	childID := insertUser(t, db, "child@example.com")
	linkChild(t, db, parentID, childID)

	card, _ := GetOrCreateBingoCard(context.Background(), db, childID)

	// Fill row 0 cells with "workout_30min" challenges so one workout completes a line.
	for col := 0; col < 3; col++ {
		forceBingoCell(t, db, card.ID, col, "workout_30min")
	}
	// Pre-complete cells 1 and 2 so the workout only needs to complete cell 0.
	preCompleteCell(t, db, card.ID, 1)
	preCompleteCell(t, db, card.ID, 2)

	workoutID := insertWorkout(t, db, childID, 35*60, 3000, 200, 0, 0)
	w := WorkoutInput{ID: workoutID, DurationSeconds: 35 * 60, DistanceMeters: 3000, Calories: 200}

	if err := UpdateBingoProgress(context.Background(), db, childID, w); err != nil {
		t.Fatalf("UpdateBingoProgress: %v", err)
	}

	// Verify a bingo_line star transaction was recorded.
	var reason string
	err := db.QueryRow(`SELECT reason FROM star_transactions WHERE user_id = ? AND reason = 'bingo_line'`, childID).Scan(&reason)
	if err != nil {
		t.Errorf("expected bingo_line transaction: %v", err)
	}
}

func TestUpdateBingoProgress_JackpotAwardsStars(t *testing.T) {
	db := setupTestDB(t)
	addBingoSchema(t, db)

	parentID := insertUser(t, db, "parent@example.com")
	childID := insertUser(t, db, "child@example.com")
	linkChild(t, db, parentID, childID)

	card, _ := GetOrCreateBingoCard(context.Background(), db, childID)

	// Set all cells to "workout_30min" and pre-complete 8 of them.
	for i := 0; i < 9; i++ {
		forceBingoCell(t, db, card.ID, i, "workout_30min")
	}
	for i := 1; i < 9; i++ {
		preCompleteCell(t, db, card.ID, i)
	}

	workoutID := insertWorkout(t, db, childID, 35*60, 3000, 200, 0, 0)
	w := WorkoutInput{ID: workoutID, DurationSeconds: 35 * 60, DistanceMeters: 3000, Calories: 200}

	if err := UpdateBingoProgress(context.Background(), db, childID, w); err != nil {
		t.Fatalf("UpdateBingoProgress: %v", err)
	}

	// Verify jackpot transaction.
	var reason string
	err := db.QueryRow(`SELECT reason FROM star_transactions WHERE user_id = ? AND reason = 'bingo_jackpot'`, childID).Scan(&reason)
	if err != nil {
		t.Errorf("expected bingo_jackpot transaction: %v", err)
	}

	// Verify jackpot_awarded flag set.
	updated, _ := GetOrCreateBingoCard(context.Background(), db, childID)
	if !updated.JackpotAwarded {
		t.Error("expected jackpot_awarded=true after full card completion")
	}
}

func TestUpdateBingoProgress_JackpotNotAwardedTwice(t *testing.T) {
	db := setupTestDB(t)
	addBingoSchema(t, db)

	parentID := insertUser(t, db, "parent@example.com")
	childID := insertUser(t, db, "child@example.com")
	linkChild(t, db, parentID, childID)

	card, _ := GetOrCreateBingoCard(context.Background(), db, childID)

	// All cells the same challenge; 8 pre-completed.
	for i := 0; i < 9; i++ {
		forceBingoCell(t, db, card.ID, i, "workout_30min")
	}
	for i := 1; i < 9; i++ {
		preCompleteCell(t, db, card.ID, i)
	}

	workoutID := insertWorkout(t, db, childID, 35*60, 0, 0, 0, 0)
	w := WorkoutInput{ID: workoutID, DurationSeconds: 35 * 60}
	_ = UpdateBingoProgress(context.Background(), db, childID, w)

	// Second workout should not re-award jackpot.
	workoutID2 := insertWorkout(t, db, childID, 40*60, 0, 0, 0, 0)
	w2 := WorkoutInput{ID: workoutID2, DurationSeconds: 40 * 60}
	_ = UpdateBingoProgress(context.Background(), db, childID, w2)

	var count int
	db.QueryRow(`SELECT COUNT(*) FROM star_transactions WHERE user_id = ? AND reason = 'bingo_jackpot'`, childID).Scan(&count)
	if count != 1 {
		t.Errorf("expected exactly 1 jackpot transaction, got %d", count)
	}
}

func TestIsChallengeCompleted(t *testing.T) {
	now := time.Now()
	saturday := time.Date(2026, 3, 28, 10, 0, 0, 0, time.UTC) // Saturday
	earlyMorning := time.Date(2026, 3, 26, 6, 0, 0, 0, time.UTC)
	nightTime := time.Date(2026, 3, 26, 21, 0, 0, 0, time.UTC)
	_ = now

	tests := []struct {
		key       string
		w         WorkoutInput
		startedAt time.Time
		maxHR     int
		want      bool
	}{
		{"workout_30min", WorkoutInput{DurationSeconds: 30 * 60}, time.Time{}, 190, true},
		{"workout_30min", WorkoutInput{DurationSeconds: 29 * 60}, time.Time{}, 190, false},
		{"workout_45min", WorkoutInput{DurationSeconds: 45 * 60}, time.Time{}, 190, true},
		{"workout_60min", WorkoutInput{DurationSeconds: 60 * 60}, time.Time{}, 190, true},
		{"workout_90min", WorkoutInput{DurationSeconds: 90 * 60}, time.Time{}, 190, true},
		{"workout_90min", WorkoutInput{DurationSeconds: 89 * 60}, time.Time{}, 190, false},
		{"run_5k", WorkoutInput{DistanceMeters: 5000}, time.Time{}, 190, true},
		{"run_5k", WorkoutInput{DistanceMeters: 4999}, time.Time{}, 190, false},
		{"run_10k", WorkoutInput{DistanceMeters: 10000}, time.Time{}, 190, true},
		{"zone4_effort", WorkoutInput{AvgHeartRate: 160}, time.Time{}, 190, true},  // 84% → zone 4
		{"zone4_effort", WorkoutInput{AvgHeartRate: 130}, time.Time{}, 190, false}, // 68% → zone 2
		{"early_bird", WorkoutInput{}, earlyMorning, 190, true},
		{"early_bird", WorkoutInput{}, nightTime, 190, false},
		{"night_owl", WorkoutInput{}, nightTime, 190, true},
		{"night_owl", WorkoutInput{}, earlyMorning, 190, false},
		{"weekend_workout", WorkoutInput{}, saturday, 190, true},
		{"weekend_workout", WorkoutInput{}, time.Date(2026, 3, 27, 10, 0, 0, 0, time.UTC), 190, true},  // Sunday
		{"weekend_workout", WorkoutInput{}, time.Date(2026, 3, 26, 10, 0, 0, 0, time.UTC), 190, false}, // Thursday
		{"calories_300", WorkoutInput{Calories: 300}, time.Time{}, 190, true},
		{"calories_300", WorkoutInput{Calories: 299}, time.Time{}, 190, false},
		{"calories_500", WorkoutInput{Calories: 500}, time.Time{}, 190, true},
		{"climb_100m", WorkoutInput{AscentMeters: 100}, time.Time{}, 190, true},
		{"climb_100m", WorkoutInput{AscentMeters: 99}, time.Time{}, 190, false},
		{"fast_pace", WorkoutInput{AvgPaceSecPerKm: 280}, time.Time{}, 190, true},  // 4:40/km
		{"fast_pace", WorkoutInput{AvgPaceSecPerKm: 320}, time.Time{}, 190, false}, // 5:20/km
		{"fast_pace", WorkoutInput{AvgPaceSecPerKm: 0}, time.Time{}, 190, false},   // no pace data
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			got := isChallengeCompleted(tt.key, tt.w, tt.startedAt, tt.maxHR)
			if got != tt.want {
				t.Errorf("isChallengeCompleted(%q) = %v, want %v", tt.key, got, tt.want)
			}
		})
	}
}

// forceBingoCell overwrites a cell's challenge key in the DB by rebuilding the JSON.
func forceBingoCell(t *testing.T, db *sql.DB, cardID int64, idx int, challengeKey string) {
	t.Helper()
	var cellsJSON string
	if err := db.QueryRow(`SELECT cells FROM bingo_cards WHERE id = ?`, cardID).Scan(&cellsJSON); err != nil {
		t.Fatalf("load cells for force: %v", err)
	}
	var cells []BingoCell
	if err := unmarshalJSON(cellsJSON, &cells); err != nil {
		t.Fatalf("unmarshal for force: %v", err)
	}
	if idx < 0 || idx >= len(cells) {
		t.Fatalf("forceBingoCell: index %d out of range", idx)
	}
	// Find a label for this key.
	label := challengeKey
	for _, c := range challengePool {
		if c.Key == challengeKey {
			label = c.Label
			break
		}
	}
	cells[idx] = BingoCell{ChallengeKey: challengeKey, Label: label}
	data, _ := marshalJSON(cells)
	_, err := db.Exec(`UPDATE bingo_cards SET cells = ? WHERE id = ?`, string(data), cardID)
	if err != nil {
		t.Fatalf("force cell update: %v", err)
	}
}

// preCompleteCell marks a cell as already completed directly in the DB.
func preCompleteCell(t *testing.T, db *sql.DB, cardID int64, idx int) {
	t.Helper()
	var cellsJSON string
	if err := db.QueryRow(`SELECT cells FROM bingo_cards WHERE id = ?`, cardID).Scan(&cellsJSON); err != nil {
		t.Fatalf("load cells for pre-complete: %v", err)
	}
	var cells []BingoCell
	if err := unmarshalJSON(cellsJSON, &cells); err != nil {
		t.Fatalf("unmarshal for pre-complete: %v", err)
	}
	if idx < 0 || idx >= len(cells) {
		t.Fatalf("preCompleteCell: index %d out of range", idx)
	}
	cells[idx].Completed = true
	cells[idx].CompletedAt = time.Now().UTC().Format(time.RFC3339)
	data, _ := marshalJSON(cells)
	_, err := db.Exec(`UPDATE bingo_cards SET cells = ? WHERE id = ?`, string(data), cardID)
	if err != nil {
		t.Fatalf("pre-complete update: %v", err)
	}
}

func unmarshalJSON(s string, v any) error { return json.Unmarshal([]byte(s), v) }
func marshalJSON(v any) ([]byte, error)   { return json.Marshal(v) }
