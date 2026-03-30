package allowance

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"time"
)

// bingoUpdateMu serialises UpdateBingoProgress calls so that a DEFERRED SQLite
// transaction cannot race against another call that reads the same card before
// either write is committed, which would cause bonus_earned to be double-awarded.
var bingoUpdateMu sync.Mutex

// AllowanceBingoChallenge describes one possible cell in a weekly bingo card.
type AllowanceBingoChallenge struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

// AllowanceBingoCell is one square on a bingo card.
type AllowanceBingoCell struct {
	ChallengeKey string `json:"challenge_key"`
	Label        string `json:"label"`
	Completed    bool   `json:"completed"`
}

// AllowanceBingoCard is the full state of a child's weekly bingo card.
type AllowanceBingoCard struct {
	ID             int64                `json:"id"`
	ChildID        int64                `json:"child_id"`
	ParentID       int64                `json:"parent_id"`
	WeekStart      string               `json:"week_start"` // YYYY-MM-DD (Monday)
	Cells          []AllowanceBingoCell `json:"cells"`
	CompletedLines int                  `json:"completed_lines"` // bitmask, bits 0-7 for 8 lines
	FullCard       bool                 `json:"full_card"`
	BonusEarned    float64              `json:"bonus_earned"` // total NOK awarded
	CreatedAt      string               `json:"created_at"`
	UpdatedAt      string               `json:"updated_at"`
}

// AllowanceChallengePool is the complete set of possible bingo challenges.
// Must contain at least 9 entries for a 3×3 grid.
var AllowanceChallengePool = []AllowanceBingoChallenge{
	{Key: "chore_monday",    Label: "Do a chore on Monday"},
	{Key: "chore_tuesday",   Label: "Do a chore on Tuesday"},
	{Key: "chore_wednesday", Label: "Do a chore on Wednesday"},
	{Key: "chore_thursday",  Label: "Do a chore on Thursday"},
	{Key: "chore_friday",    Label: "Do a chore on Friday"},
	{Key: "chore_weekend",   Label: "Do a chore on the weekend"},
	{Key: "two_in_one_day",  Label: "Complete 2 chores in one day"},
	{Key: "three_in_week",   Label: "Complete 3 chores this week"},
	{Key: "five_in_week",    Label: "Complete 5 chores this week"},
	{Key: "streak_3_days",   Label: "Do chores 3 days in a row"},
	{Key: "quality_bonus",   Label: "Earn a quality bonus"},
	{Key: "extra_task",      Label: "Complete an extra task"},
}

// allowanceBingoLines defines the 8 winning lines on a 3×3 grid (cell indices 0–8,
// row-major: row r, col c → r*3+c).
var allowanceBingoLines = [8][3]int{
	{0, 1, 2}, // row 0
	{3, 4, 5}, // row 1
	{6, 7, 8}, // row 2
	{0, 3, 6}, // col 0
	{1, 4, 7}, // col 1
	{2, 5, 8}, // col 2
	{0, 4, 8}, // main diagonal
	{2, 4, 6}, // anti-diagonal
}

const (
	BingoLineBonus    = 15.0 // NOK awarded per new completed line
	BingoJackpotBonus = 50.0 // NOK awarded for completing the full card
)

// generateBingoCells selects 9 unique challenges from AllowanceChallengePool using rng.
func generateBingoCells(rng *rand.Rand) []AllowanceBingoCell {
	perm := rng.Perm(len(AllowanceChallengePool))
	cells := make([]AllowanceBingoCell, 9)
	for i := range cells {
		c := AllowanceChallengePool[perm[i]]
		cells[i] = AllowanceBingoCell{ChallengeKey: c.Key, Label: c.Label}
	}
	return cells
}

// CheckBingoLines returns the indices (0–7) of lines that are fully completed.
func CheckBingoLines(cells []AllowanceBingoCell) []int {
	var lines []int
	for i, line := range allowanceBingoLines {
		if cells[line[0]].Completed && cells[line[1]].Completed && cells[line[2]].Completed {
			lines = append(lines, i)
		}
	}
	return lines
}

// isChallengeCompleted returns true if the approved completions (and extra task
// count) satisfy the given challenge key for the week starting at weekStart.
func isChallengeCompleted(key string, completions []Completion, weekStart string, approvedExtrasCount int) bool {
	start, err := time.Parse("2006-01-02", weekStart)
	if err != nil {
		return false
	}

	dayDate := func(offset int) string {
		return start.AddDate(0, 0, offset).Format("2006-01-02")
	}

	// Build per-date completion count and quality bonus flag.
	dateCount := make(map[string]int)
	hasQualityBonus := false
	for _, c := range completions {
		dateCount[c.Date]++
		if c.QualityBonus > 0 {
			hasQualityBonus = true
		}
	}

	switch key {
	case "chore_monday":
		return dateCount[dayDate(0)] > 0
	case "chore_tuesday":
		return dateCount[dayDate(1)] > 0
	case "chore_wednesday":
		return dateCount[dayDate(2)] > 0
	case "chore_thursday":
		return dateCount[dayDate(3)] > 0
	case "chore_friday":
		return dateCount[dayDate(4)] > 0
	case "chore_weekend":
		return dateCount[dayDate(5)] > 0 || dateCount[dayDate(6)] > 0
	case "two_in_one_day":
		for _, cnt := range dateCount {
			if cnt >= 2 {
				return true
			}
		}
		return false
	case "three_in_week":
		return len(completions) >= 3
	case "five_in_week":
		return len(completions) >= 5
	case "streak_3_days":
		// Check for any 3 consecutive days (offsets 0–6) with at least one completion.
		for i := 0; i <= 4; i++ {
			if dateCount[dayDate(i)] > 0 && dateCount[dayDate(i+1)] > 0 && dateCount[dayDate(i+2)] > 0 {
				return true
			}
		}
		return false
	case "quality_bonus":
		return hasQualityBonus
	case "extra_task":
		return approvedExtrasCount > 0
	}
	return false
}

// GetOrCreateBingoCard loads the child's bingo card for the given week,
// creating a new randomly generated card if one does not yet exist.
// The card layout is deterministic for the same child+weekStart combination.
func GetOrCreateBingoCard(db *sql.DB, childID, parentID int64, weekStart string) (*AllowanceBingoCard, error) {
	var card AllowanceBingoCard
	var cellsJSON string
	var fullCardInt int

	err := db.QueryRow(`
		SELECT id, child_id, parent_id, week_start, cells,
		       completed_lines, full_card, bonus_earned, created_at, updated_at
		FROM allowance_bingo_cards
		WHERE child_id = ? AND week_start = ?
	`, childID, weekStart).Scan(
		&card.ID, &card.ChildID, &card.ParentID, &card.WeekStart,
		&cellsJSON, &card.CompletedLines, &fullCardInt,
		&card.BonusEarned, &card.CreatedAt, &card.UpdatedAt,
	)
	if err == nil {
		card.FullCard = fullCardInt != 0
		if err2 := json.Unmarshal([]byte(cellsJSON), &card.Cells); err2 != nil {
			return nil, fmt.Errorf("allowance bingo: unmarshal cells: %w", err2)
		}
		return &card, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("allowance bingo: load card: %w", err)
	}

	// No card for this week — generate one. The seed is deterministic so the
	// same child+week always produces the same initial layout.
	start, parseErr := time.Parse("2006-01-02", weekStart)
	if parseErr != nil {
		return nil, fmt.Errorf("allowance bingo: parse week_start: %w", parseErr)
	}
	isoYear, isoWeek := start.ISOWeek()
	seed := childID*1_000_000 + int64(isoYear)*100 + int64(isoWeek)
	rng := rand.New(rand.NewSource(seed)) //nolint:gosec
	cells := generateBingoCells(rng)

	cellsData, marshalErr := json.Marshal(cells)
	if marshalErr != nil {
		return nil, fmt.Errorf("allowance bingo: marshal cells: %w", marshalErr)
	}

	now := nowRFC3339()
	res, insertErr := db.Exec(`
		INSERT INTO allowance_bingo_cards
		  (child_id, parent_id, week_start, cells, completed_lines, full_card, bonus_earned, created_at, updated_at)
		VALUES (?, ?, ?, ?, 0, 0, 0, ?, ?)
		ON CONFLICT(child_id, week_start) DO NOTHING
	`, childID, parentID, weekStart, string(cellsData), now, now)
	if insertErr != nil {
		return nil, fmt.Errorf("allowance bingo: insert card: %w", insertErr)
	}

	// Another goroutine may have inserted the row first (ON CONFLICT DO NOTHING returns 0 rows).
	if n, _ := res.RowsAffected(); n == 0 {
		return GetOrCreateBingoCard(db, childID, parentID, weekStart)
	}

	id, idErr := res.LastInsertId()
	if idErr != nil {
		return nil, fmt.Errorf("allowance bingo: last insert id: %w", idErr)
	}
	return &AllowanceBingoCard{
		ID:             id,
		ChildID:        childID,
		ParentID:       parentID,
		WeekStart:      weekStart,
		Cells:          cells,
		CompletedLines: 0,
		FullCard:       false,
		BonusEarned:    0,
		CreatedAt:      now,
		UpdatedAt:      now,
	}, nil
}

// UpdateBingoProgress checks the child's approved chore completions for the
// given week against their bingo card and marks cells done. It awards +15 NOK
// for each newly completed line and +50 NOK jackpot for completing the full card.
//
// Idempotency is guaranteed via the completed_lines bitmask: lines already
// recorded in the bitmask will not be awarded again on subsequent calls.
// The mutex prevents concurrent calls from reading stale state before the first
// write commits (a DEFERRED SQLite transaction does not prevent that race).
func UpdateBingoProgress(db *sql.DB, childID, parentID int64, weekStart string) (*AllowanceBingoCard, error) {
	bingoUpdateMu.Lock()
	defer bingoUpdateMu.Unlock()

	// Ensure a card exists before entering the transaction.
	if _, err := GetOrCreateBingoCard(db, childID, parentID, weekStart); err != nil {
		return nil, err
	}

	// Fetch completions and extras outside the transaction (read-only queries).
	completions, err := GetChildCompletionsForWeek(db, childID, weekStart)
	if err != nil {
		return nil, fmt.Errorf("allowance bingo: get completions: %w", err)
	}
	approved := make([]Completion, 0, len(completions))
	for _, c := range completions {
		if c.Status == "approved" {
			approved = append(approved, c)
		}
	}

	approvedExtrasCount, err := countApprovedExtrasForWeek(db, childID, parentID, weekStart)
	if err != nil {
		return nil, fmt.Errorf("allowance bingo: count extras: %w", err)
	}

	tx, err := db.Begin()
	if err != nil {
		return nil, fmt.Errorf("allowance bingo: begin tx: %w", err)
	}

	var card AllowanceBingoCard
	var cellsJSON string
	var fullCardInt int
	err = tx.QueryRow(`
		SELECT id, child_id, parent_id, week_start, cells,
		       completed_lines, full_card, bonus_earned, created_at, updated_at
		FROM allowance_bingo_cards
		WHERE child_id = ? AND week_start = ?
	`, childID, weekStart).Scan(
		&card.ID, &card.ChildID, &card.ParentID, &card.WeekStart,
		&cellsJSON, &card.CompletedLines, &fullCardInt,
		&card.BonusEarned, &card.CreatedAt, &card.UpdatedAt,
	)
	if err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("allowance bingo: reload card in tx: %w", err)
	}
	card.FullCard = fullCardInt != 0

	if err2 := json.Unmarshal([]byte(cellsJSON), &card.Cells); err2 != nil {
		tx.Rollback()
		return nil, fmt.Errorf("allowance bingo: unmarshal cells in tx: %w", err2)
	}

	// Quick exit: full card already complete, nothing left to award.
	if card.FullCard {
		tx.Rollback()
		return &card, nil
	}

	// Apply completions to cells.
	anyNew := false
	for i := range card.Cells {
		if !card.Cells[i].Completed && isChallengeCompleted(card.Cells[i].ChallengeKey, approved, weekStart, approvedExtrasCount) {
			card.Cells[i].Completed = true
			anyNew = true
		}
	}
	if !anyNew {
		tx.Rollback()
		return &card, nil
	}

	// Compute which lines are newly complete (not already in the bitmask).
	completedLineIndices := CheckBingoLines(card.Cells)
	var newLinesMask int
	for _, lineIdx := range completedLineIndices {
		newLinesMask |= 1 << lineIdx
	}
	onlyNew := newLinesMask &^ card.CompletedLines // bits not already recorded
	updatedLinesMask := card.CompletedLines | newLinesMask

	// Count newly completed lines for the bonus.
	newLineCount := 0
	for i := range 8 {
		if onlyNew&(1<<i) != 0 {
			newLineCount++
		}
	}

	// Detect full card (all 9 cells done) and jackpot eligibility.
	fullCard := true
	for _, cell := range card.Cells {
		if !cell.Completed {
			fullCard = false
			break
		}
	}
	awardJackpot := fullCard && !card.FullCard

	addedBonus := float64(newLineCount) * BingoLineBonus
	if awardJackpot {
		addedBonus += BingoJackpotBonus
	}

	cellsData, marshalErr := json.Marshal(card.Cells)
	if marshalErr != nil {
		tx.Rollback()
		return nil, fmt.Errorf("allowance bingo: marshal updated cells: %w", marshalErr)
	}

	now := nowRFC3339()
	fullCardVal := 0
	if fullCard {
		fullCardVal = 1
	}
	newTotalBonus := card.BonusEarned + addedBonus

	_, err = tx.Exec(`
		UPDATE allowance_bingo_cards
		SET cells = ?, completed_lines = ?, full_card = ?, bonus_earned = ?, updated_at = ?
		WHERE id = ?
	`, string(cellsData), updatedLinesMask, fullCardVal, newTotalBonus, now, card.ID)
	if err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("allowance bingo: update card: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("allowance bingo: commit tx: %w", err)
	}

	card.CompletedLines = updatedLinesMask
	card.FullCard = fullCard
	card.BonusEarned = newTotalBonus
	card.UpdatedAt = now
	return &card, nil
}

// GetBingoBonusForWeek returns the total NOK bonus earned by a child from bingo
// for the given week. Returns 0 if no card exists yet.
func GetBingoBonusForWeek(db *sql.DB, childID int64, weekStart string) (float64, error) {
	var bonus float64
	err := db.QueryRow(`
		SELECT bonus_earned FROM allowance_bingo_cards
		WHERE child_id = ? AND week_start = ?
	`, childID, weekStart).Scan(&bonus)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("allowance bingo: get bonus: %w", err)
	}
	return bonus, nil
}

// countApprovedExtrasForWeek returns the number of extra tasks approved for
// the child in the 7-day period starting at weekStart (YYYY-MM-DD).
func countApprovedExtrasForWeek(db *sql.DB, childID, parentID int64, weekStart string) (int, error) {
	start, err := time.Parse("2006-01-02", weekStart)
	if err != nil {
		return 0, fmt.Errorf("allowance bingo: parse week_start for extras: %w", err)
	}
	weekStartRFC := start.UTC().Format(time.RFC3339)
	weekEndRFC := start.AddDate(0, 0, 7).UTC().Format(time.RFC3339)

	var count int
	err = db.QueryRow(`
		SELECT COUNT(*) FROM allowance_extras
		WHERE (child_id = ? OR claimed_by = ?)
		  AND parent_id = ?
		  AND status = 'approved'
		  AND approved_at >= ? AND approved_at < ?
	`, childID, childID, parentID, weekStartRFC, weekEndRFC).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("allowance bingo: count approved extras: %w", err)
	}
	return count, nil
}
