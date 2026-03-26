package stars

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
)

// challengePool is the full set of possible bingo challenges (must have at least 9).
var challengePool = []bingoChallenge{
	{Key: "workout_30min", Label: "Work out 30+ min"},
	{Key: "workout_45min", Label: "Work out 45+ min"},
	{Key: "workout_60min", Label: "Work out 60+ min"},
	{Key: "workout_90min", Label: "Work out 90+ min"},
	{Key: "run_5k", Label: "Run 5 km or more"},
	{Key: "run_10k", Label: "Run 10 km or more"},
	{Key: "zone4_effort", Label: "Reach HR Zone 4"},
	{Key: "all_zones", Label: "Hit all 5 HR zones"},
	{Key: "early_bird", Label: "Work out before 8 AM"},
	{Key: "night_owl", Label: "Work out after 8 PM"},
	{Key: "weekend_workout", Label: "Work out on a weekend"},
	{Key: "calories_300", Label: "Burn 300+ calories"},
	{Key: "calories_500", Label: "Burn 500+ calories"},
	{Key: "climb_100m", Label: "Climb 100+ meters"},
	{Key: "fast_pace", Label: "Run under 5 min/km"},
}

// bingoLines defines the 8 winning lines on a 3×3 grid (by cell index 0–8,
// row-major). Indices map as: row r, col c → r*3+c.
var bingoLines = [8][3]int{
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
	bingoLineStars    = 15 // stars awarded per new completed line
	bingoJackpotStars = 50 // stars awarded for completing the full card
)

type bingoChallenge struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

// BingoCell is one square on the bingo card.
type BingoCell struct {
	ChallengeKey string `json:"challenge_key"`
	Label        string `json:"label"`
	Completed    bool   `json:"completed"`
	CompletedAt  string `json:"completed_at,omitempty"`
}

// BingoCard is the full state of a user's weekly bingo card.
type BingoCard struct {
	ID             int64       `json:"id"`
	UserID         int64       `json:"user_id"`
	WeekKey        string      `json:"week_key"`
	Cells          []BingoCell `json:"cells"`
	CompletedLines []int       `json:"completed_lines"`
	JackpotAwarded bool        `json:"jackpot_awarded"`
	CreatedAt      string      `json:"created_at"`
	UpdatedAt      string      `json:"updated_at"`
}

// isoWeekKey returns an ISO year+week string like "2026-W13".
func isoWeekKey(t time.Time) string {
	year, week := t.ISOWeek()
	return fmt.Sprintf("%04d-W%02d", year, week)
}

// generateCells picks 9 unique challenges from the pool using the given rng.
func generateCells(rng *rand.Rand) []BingoCell {
	perm := rng.Perm(len(challengePool))
	cells := make([]BingoCell, 9)
	for i := range cells {
		c := challengePool[perm[i]]
		cells[i] = BingoCell{
			ChallengeKey: c.Key,
			Label:        c.Label,
		}
	}
	return cells
}

// GetOrCreateBingoCard loads the current-week bingo card for the user,
// creating a fresh random card if one does not yet exist.
func GetOrCreateBingoCard(ctx context.Context, db *sql.DB, userID int64) (*BingoCard, error) {
	weekKey := isoWeekKey(time.Now())

	var card BingoCard
	var cellsJSON, linesJSON string
	var jackpotInt int

	err := db.QueryRowContext(ctx, `
		SELECT id, user_id, week_key, cells, completed_lines, jackpot_awarded, created_at, updated_at
		FROM bingo_cards
		WHERE user_id = ? AND week_key = ?
	`, userID, weekKey).Scan(
		&card.ID, &card.UserID, &card.WeekKey,
		&cellsJSON, &linesJSON, &jackpotInt,
		&card.CreatedAt, &card.UpdatedAt,
	)
	if err == nil {
		card.JackpotAwarded = jackpotInt == 1
		if err2 := json.Unmarshal([]byte(cellsJSON), &card.Cells); err2 != nil {
			return nil, fmt.Errorf("bingo: unmarshal cells: %w", err2)
		}
		if err2 := json.Unmarshal([]byte(linesJSON), &card.CompletedLines); err2 != nil {
			return nil, fmt.Errorf("bingo: unmarshal lines: %w", err2)
		}
		return &card, nil
	}
	if err != sql.ErrNoRows {
		return nil, fmt.Errorf("bingo: load card: %w", err)
	}

	// No card for this week — generate one using a seeded rng so the same
	// user+week always produce the same initial layout (deterministic but
	// still random-looking across users and weeks).
	seed := int64(userID)*1000000 + int64(time.Now().Year())*100 + int64(func() int { _, w := time.Now().ISOWeek(); return w }())
	rng := rand.New(rand.NewSource(seed)) //nolint:gosec
	cells := generateCells(rng)

	cellsData, err := json.Marshal(cells)
	if err != nil {
		return nil, fmt.Errorf("bingo: marshal cells: %w", err)
	}
	linesData := []byte("[]")
	now := time.Now().UTC().Format(time.RFC3339)

	res, err := db.ExecContext(ctx, `
		INSERT INTO bingo_cards (user_id, week_key, cells, completed_lines, jackpot_awarded, created_at, updated_at)
		VALUES (?, ?, ?, ?, 0, ?, ?)
		ON CONFLICT(user_id, week_key) DO NOTHING
	`, userID, weekKey, string(cellsData), string(linesData), now, now)
	if err != nil {
		return nil, fmt.Errorf("bingo: insert card: %w", err)
	}

	// Another goroutine may have inserted first (ON CONFLICT DO NOTHING returns 0 rows).
	if n, _ := res.RowsAffected(); n == 0 {
		// Re-load the card created by the other goroutine.
		return GetOrCreateBingoCard(ctx, db, userID)
	}

	id, _ := res.LastInsertId()
	return &BingoCard{
		ID:             id,
		UserID:         userID,
		WeekKey:        weekKey,
		Cells:          cells,
		CompletedLines: []int{},
		JackpotAwarded: false,
		CreatedAt:      now,
		UpdatedAt:      now,
	}, nil
}

// isChallengeCompleted returns true if the workout satisfies the given bingo challenge.
// startedAt may be zero if the workout start time could not be loaded.
func isChallengeCompleted(key string, w WorkoutInput, startedAt time.Time, maxHR int) bool {
	switch key {
	case "workout_30min":
		return w.DurationSeconds >= 30*60
	case "workout_45min":
		return w.DurationSeconds >= 45*60
	case "workout_60min":
		return w.DurationSeconds >= 60*60
	case "workout_90min":
		return w.DurationSeconds >= 90*60
	case "run_5k":
		return w.DistanceMeters >= 5000
	case "run_10k":
		return w.DistanceMeters >= 10000
	case "zone4_effort":
		if maxHR <= 0 || w.AvgHeartRate <= 0 {
			return false
		}
		return hrZone(w.AvgHeartRate, maxHR) >= 4
	case "all_zones":
		if len(w.Samples) < 2 || maxHR <= 0 {
			return false
		}
		zones := computeTimeInZones(w.Samples, maxHR)
		for z := 1; z <= 5; z++ {
			if zones[z] <= 0 {
				return false
			}
		}
		return true
	case "early_bird":
		return !startedAt.IsZero() && startedAt.Hour() < 8
	case "night_owl":
		return !startedAt.IsZero() && startedAt.Hour() >= 20
	case "weekend_workout":
		if startedAt.IsZero() {
			return false
		}
		wd := startedAt.Weekday()
		return wd == time.Saturday || wd == time.Sunday
	case "calories_300":
		return w.Calories >= 300
	case "calories_500":
		return w.Calories >= 500
	case "climb_100m":
		return w.AscentMeters >= 100
	case "fast_pace":
		// AvgPaceSecPerKm of 0 means no pace data; reject it.
		return w.AvgPaceSecPerKm > 0 && w.AvgPaceSecPerKm < 300
	}
	return false
}

// checkBingoLines returns the indices (0–7) of lines that are now fully completed.
func checkBingoLines(cells []BingoCell) []int {
	var lines []int
	for i, line := range bingoLines {
		if cells[line[0]].Completed && cells[line[1]].Completed && cells[line[2]].Completed {
			lines = append(lines, i)
		}
	}
	return lines
}

// UpdateBingoProgress checks a newly completed workout against the user's
// current-week bingo card and awards stars for any newly completed lines.
// It is safe to call for non-child users — it returns nil immediately.
func UpdateBingoProgress(ctx context.Context, db *sql.DB, userID int64, w WorkoutInput) error {
	isChild, err := isChildUser(db, userID)
	if err != nil {
		return err
	}
	if !isChild {
		return nil
	}

	card, err := GetOrCreateBingoCard(ctx, db, userID)
	if err != nil {
		return fmt.Errorf("bingo: get card: %w", err)
	}

	// Load the workout start time for time-of-day and weekend checks.
	var startedAt time.Time
	var startedAtStr string
	if dbErr := db.QueryRowContext(ctx, `SELECT started_at FROM workouts WHERE id = ?`, w.ID).Scan(&startedAtStr); dbErr == nil {
		if t, parseErr := parseWorkoutTime(startedAtStr); parseErr == nil {
			startedAt = t
		}
	}

	// Resolve max HR (mirrors EvaluateWorkout logic).
	maxHR := w.MaxHeartRate
	if prefs, prefErr := auth.GetPreferences(db, userID); prefErr == nil {
		if v, ok := prefs["max_hr"]; ok {
			if parsed, parseErr := strconv.Atoi(v); parseErr == nil && parsed > 0 {
				maxHR = parsed
			}
		}
	}
	if maxHR <= 0 {
		maxHR = 190
	}

	now := time.Now().UTC().Format(time.RFC3339)
	changed := false

	for i := range card.Cells {
		if card.Cells[i].Completed {
			continue
		}
		if isChallengeCompleted(card.Cells[i].ChallengeKey, w, startedAt, maxHR) {
			card.Cells[i].Completed = true
			card.Cells[i].CompletedAt = now
			changed = true
		}
	}

	if !changed {
		return nil
	}

	// Check for new bingo lines.
	allCompleted := checkBingoLines(card.Cells)
	var newLines []int
	for _, lineIdx := range allCompleted {
		if !slices.Contains(card.CompletedLines, lineIdx) {
			newLines = append(newLines, lineIdx)
		}
	}

	// Build updated completed_lines.
	updatedLines := append(card.CompletedLines, newLines...)

	// Check for jackpot (all 9 cells completed).
	fullCard := true
	for _, cell := range card.Cells {
		if !cell.Completed {
			fullCard = false
			break
		}
	}
	awardJackpot := fullCard && !card.JackpotAwarded

	// Persist updated card.
	cellsData, err := json.Marshal(card.Cells)
	if err != nil {
		return fmt.Errorf("bingo: marshal updated cells: %w", err)
	}
	linesData, err := json.Marshal(updatedLines)
	if err != nil {
		return fmt.Errorf("bingo: marshal updated lines: %w", err)
	}
	jackpotVal := 0
	if awardJackpot || card.JackpotAwarded {
		jackpotVal = 1
	}

	_, err = db.ExecContext(ctx, `
		UPDATE bingo_cards
		SET cells = ?, completed_lines = ?, jackpot_awarded = ?, updated_at = ?
		WHERE id = ?
	`, string(cellsData), string(linesData), jackpotVal, now, card.ID)
	if err != nil {
		return fmt.Errorf("bingo: update card: %w", err)
	}

	// Award stars for new lines and jackpot outside the card update so a star
	// recording failure doesn't leave the card in a dirty state.
	var awards []StarAward
	for range newLines {
		awards = append(awards, StarAward{
			Amount:      bingoLineStars,
			Reason:      "bingo_line",
			Description: "Bingo line complete!",
		})
	}
	if awardJackpot {
		awards = append(awards, StarAward{
			Amount:      bingoJackpotStars,
			Reason:      "bingo_jackpot",
			Description: "Full bingo card complete!",
		})
	}
	if len(awards) > 0 {
		if err := recordAwards(db, userID, w.ID, awards); err != nil {
			log.Printf("bingo: record awards user %d workout %d: %v", userID, w.ID, err)
		}
	}

	return nil
}

// BingoHandler handles GET /api/stars/bingo.
// Returns the authenticated user's bingo card for the current ISO week,
// creating one automatically if it does not yet exist.
func BingoHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		card, err := GetOrCreateBingoCard(r.Context(), db, user.ID)
		if err != nil {
			log.Printf("bingo: get card user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load bingo card"})
			return
		}
		writeJSON(w, http.StatusOK, card)
	}
}
