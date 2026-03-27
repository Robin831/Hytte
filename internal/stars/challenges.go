// Package stars provides kid-facing APIs for the Stars/Rewards system.
// Parent-facing Challenge CRUD and participant management live in internal/family,
// reflecting the package boundary: family = parent operations, stars = child-facing rewards.
package stars

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"math"
	"time"

	"github.com/Robin831/Hytte/internal/encryption"
)

// SystemCreatorID is the user ID reserved for system-generated weekly challenges.
// A users row with this ID is seeded at startup (see internal/db).
const SystemCreatorID int64 = 0

// ChallengeWithProgress wraps a Challenge with the authenticated child's current
// progress toward the challenge target. Returned by GET /api/stars/challenges.
type ChallengeWithProgress struct {
	ID            int64   `json:"id"`
	CreatorID     int64   `json:"creator_id"`
	Title         string  `json:"title"`
	Description   string  `json:"description"`
	ChallengeType string  `json:"challenge_type"`
	TargetValue   float64 `json:"target_value"`
	StarReward    int     `json:"star_reward"`
	StartDate     string  `json:"start_date"`
	EndDate       string  `json:"end_date"`
	IsActive      bool    `json:"is_active"`
	CreatedAt     string  `json:"created_at"`
	UpdatedAt     string  `json:"updated_at"`
	CurrentValue  float64 `json:"current_value"`
	Completed     bool    `json:"completed"`
}

// GetActiveChallenges returns all active challenges in which childID is a
// participant, enriched with that child's current progress.
// Progress for all challenges is computed using at most two queries (one for
// workout aggregation, one for streak), rather than one query per challenge.
func GetActiveChallenges(db *sql.DB, childID int64) ([]ChallengeWithProgress, error) {
	rows, err := db.Query(`
		SELECT fc.id, fc.creator_id, fc.title, fc.description, fc.challenge_type,
		       fc.target_value, fc.star_reward, fc.start_date, fc.end_date,
		       fc.is_active, fc.created_at, fc.updated_at
		FROM family_challenges fc
		JOIN challenge_participants cp ON cp.challenge_id = fc.id
		WHERE cp.child_id = ? AND fc.is_active = 1
		ORDER BY fc.start_date DESC
	`, childID)
	if err != nil {
		return nil, err
	}

	// Collect all rows before closing, so batchChallengeProgress can issue its
	// own queries without holding an open rows cursor (avoids SQLite locking).
	var results []ChallengeWithProgress
	for rows.Next() {
		var c ChallengeWithProgress
		var encTitle, encDesc string
		var isActiveInt int

		if err := rows.Scan(
			&c.ID, &c.CreatorID, &encTitle, &encDesc,
			&c.ChallengeType, &c.TargetValue, &c.StarReward,
			&c.StartDate, &c.EndDate, &isActiveInt,
			&c.CreatedAt, &c.UpdatedAt,
		); err != nil {
			rows.Close()
			return nil, err
		}
		c.Title = encryption.DecryptLenient(encTitle)
		c.Description = encryption.DecryptLenient(encDesc)
		c.IsActive = isActiveInt != 0
		results = append(results, c)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Now that rows is closed, compute progress for all challenges in bulk.
	if err := batchChallengeProgress(db, childID, results); err != nil {
		return nil, err
	}

	return results, nil
}

// UpdateChallengeProgress evaluates all active, uncompleted challenges the user
// is enrolled in and awards stars for any that have just reached their target.
// Called after each workout save. Accumulation units match the challenge type:
//   - distance: total km run (from workouts table, within the challenge date range)
//   - duration: total minutes run (from workouts table, within the challenge date range)
//   - workout_count: number of workouts (from workouts table, within the challenge date range)
//   - streak: current daily_workout streak count
//
// Double-awarding is prevented by updating completed_at only when it is still
// empty ('' → now) inside a transaction; concurrent calls that race are ignored.
func UpdateChallengeProgress(ctx context.Context, db *sql.DB, userID int64, _ WorkoutInput) error {
	// Fetch all active, uncompleted challenge participations for this user.
	rows, err := db.QueryContext(ctx, `
		SELECT fc.id, fc.challenge_type, fc.target_value, fc.star_reward, cp.id
		FROM family_challenges fc
		JOIN challenge_participants cp ON cp.challenge_id = fc.id
		WHERE cp.child_id = ? AND fc.is_active = 1 AND cp.completed_at = ''
		ORDER BY fc.id ASC
	`, userID)
	if err != nil {
		return err
	}

	type pendingChallenge struct {
		challengeID   int64
		challengeType string
		targetValue   float64
		starReward    int
		participantID int64
	}
	var pending []pendingChallenge
	for rows.Next() {
		var p pendingChallenge
		if err := rows.Scan(&p.challengeID, &p.challengeType, &p.targetValue, &p.starReward, &p.participantID); err != nil {
			rows.Close()
			return err
		}
		pending = append(pending, p)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(pending) == 0 {
		return nil
	}

	// Build index from challenge ID to slice position.
	idxByID := make(map[int64]int, len(pending))
	for i, p := range pending {
		idxByID[p.challengeID] = i
	}

	// Fetch current daily streak once; shared by all streak-type challenges.
	var currentStreak float64
	if err := db.QueryRowContext(ctx, `
		SELECT COALESCE(current_count, 0)
		FROM streaks
		WHERE user_id = ? AND streak_type = 'daily_workout'
	`, userID).Scan(&currentStreak); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	// Aggregate workout metrics per challenge in a single query (same approach
	// as batchChallengeProgress), restricted to uncompleted participations.
	progressRows, err := db.QueryContext(ctx, `
		SELECT
			fc.id,
			fc.challenge_type,
			COALESCE(SUM(w.distance_meters) / 1000.0, 0),
			COALESCE(SUM(w.duration_seconds) / 60.0, 0),
			COUNT(w.id)
		FROM family_challenges fc
		JOIN challenge_participants cp ON cp.challenge_id = fc.id AND cp.child_id = ?
		LEFT JOIN workouts w ON w.user_id = ?
			AND (fc.start_date = '' OR w.started_at >= fc.start_date)
			AND (fc.end_date = '' OR w.started_at < date(fc.end_date, '+1 day'))
		WHERE fc.is_active = 1 AND cp.completed_at = ''
		GROUP BY fc.id, fc.challenge_type
	`, userID, userID)
	if err != nil {
		return err
	}

	type completedChallenge struct {
		challengeID int64
	}
	var completed []completedChallenge

	for progressRows.Next() {
		var id int64
		var challengeType string
		var distanceKm, durationMin float64
		var workoutCount int64
		if err := progressRows.Scan(&id, &challengeType, &distanceKm, &durationMin, &workoutCount); err != nil {
			progressRows.Close()
			return err
		}
		idx, ok := idxByID[id]
		if !ok {
			continue
		}
		var currentValue float64
		switch challengeType {
		case "distance":
			currentValue = distanceKm
		case "duration":
			currentValue = durationMin
		case "workout_count":
			currentValue = float64(workoutCount)
		case "streak":
			currentValue = currentStreak
		default:
			// Custom or unknown types: skip automatic completion.
			continue
		}
		p := pending[idx]
		if p.targetValue > 0 && currentValue >= p.targetValue {
			completed = append(completed, completedChallenge{challengeID: id})
		}
	}
	if err := progressRows.Close(); err != nil {
		return err
	}
	if err := progressRows.Err(); err != nil {
		return err
	}

	// Award stars for each newly completed challenge.
	now := time.Now().UTC().Format(time.RFC3339)
	for _, comp := range completed {
		idx := idxByID[comp.challengeID]
		p := pending[idx]
		didAward, err := awardChallengeCompletion(ctx, db, userID, p.participantID, p.starReward, now)
		if err != nil {
			log.Printf("stars: challenge completion award failed for user %d challenge %d: %v", userID, p.challengeID, err)
			continue
		}
		if didAward {
			go SendChallengeCompletedNotification(db, userID, p.starReward)
		}
	}
	return nil
}

// awardChallengeCompletion atomically marks a challenge participation as completed
// and records the star transaction. Returns (true, nil) when the award was made,
// (false, nil) when already completed by a concurrent call, or (false, err) on failure.
func awardChallengeCompletion(ctx context.Context, db *sql.DB, userID, participantID int64, starReward int, now string) (bool, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	// Check-and-set: only update if not already completed.
	res, err := tx.ExecContext(ctx, `
		UPDATE challenge_participants SET completed_at = ?
		WHERE id = ? AND completed_at = ''
	`, now, participantID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if n == 0 {
		// Already completed by a concurrent call; nothing to do.
		return false, nil
	}

	if starReward <= 0 {
		return true, tx.Commit()
	}

	// reference_id is intentionally NULL here: it is used elsewhere to identify
	// workout IDs (e.g. weekly starred-workout counts), so inserting a challenge
	// ID would inflate those metrics.
	_, err = tx.ExecContext(ctx, `
		INSERT INTO star_transactions (user_id, amount, reason, description, reference_id, created_at)
		VALUES (?, ?, 'challenge_complete', 'Challenge completed!', NULL, ?)
	`, userID, starReward, now)
	if err != nil {
		return false, err
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO star_balances (user_id, total_earned)
		VALUES (?, ?)
		ON CONFLICT(user_id) DO UPDATE SET total_earned = total_earned + excluded.total_earned
	`, userID, starReward)
	if err != nil {
		return false, err
	}

	return true, tx.Commit()
}

// batchChallengeProgress computes progress for all challenges using at most two
// database queries: one aggregation JOIN across all workout-based challenges, and
// one streak lookup shared by all streak-type challenges. This avoids the N+1
// pattern of issuing one query per challenge.
//
// Units: distance in km, duration in minutes, workout_count as count, streak as
// current daily streak count. Custom challenges keep CurrentValue = 0.
func batchChallengeProgress(db *sql.DB, childID int64, challenges []ChallengeWithProgress) error {
	if len(challenges) == 0 {
		return nil
	}

	// Fetch current streak once; shared by all streak-type challenges.
	var currentStreak float64
	err := db.QueryRow(`
		SELECT COALESCE(current_count, 0)
		FROM streaks
		WHERE user_id = ? AND streak_type = 'daily_workout'
	`, childID).Scan(&currentStreak)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	// Build a lookup from challenge ID to slice index.
	idxByID := make(map[int64]int, len(challenges))
	for i, c := range challenges {
		idxByID[c.ID] = i
	}

	// Single query: aggregate workout metrics per challenge using a LEFT JOIN.
	// The per-row date-range filters mean each challenge gets its own slice of
	// workouts without a separate round-trip.
	// Empty start_date/end_date are treated as open-ended via the OR guards;
	// SQLite evaluates the OR short-circuit before date() can return NULL.
	progressRows, err := db.Query(`
		SELECT
			fc.id,
			fc.challenge_type,
			COALESCE(SUM(w.distance_meters) / 1000.0, 0),
			COALESCE(SUM(w.duration_seconds) / 60.0, 0),
			COUNT(w.id)
		FROM family_challenges fc
		JOIN challenge_participants cp ON cp.challenge_id = fc.id AND cp.child_id = ?
		LEFT JOIN workouts w ON w.user_id = ?
			AND (fc.start_date = '' OR w.started_at >= fc.start_date)
			AND (fc.end_date = '' OR w.started_at < date(fc.end_date, '+1 day'))
		WHERE fc.is_active = 1
		GROUP BY fc.id, fc.challenge_type
	`, childID, childID)
	if err != nil {
		return err
	}
	defer progressRows.Close()

	for progressRows.Next() {
		var id int64
		var challengeType string
		var distanceKm, durationMin float64
		var workoutCount int64
		if err := progressRows.Scan(&id, &challengeType, &distanceKm, &durationMin, &workoutCount); err != nil {
			return err
		}
		idx, ok := idxByID[id]
		if !ok {
			continue
		}
		switch challengeType {
		case "distance":
			challenges[idx].CurrentValue = distanceKm
		case "duration":
			challenges[idx].CurrentValue = durationMin
		case "workout_count":
			challenges[idx].CurrentValue = float64(workoutCount)
		case "streak":
			challenges[idx].CurrentValue = currentStreak
		// "custom" and future types: CurrentValue stays 0.
		}
		c := &challenges[idx]
		c.Completed = c.TargetValue > 0 && c.CurrentValue >= c.TargetValue
	}
	return progressRows.Err()
}

// GenerateWeeklyChallenges creates four fixed weekly challenges for every active
// child linked in family_links. The function is idempotent: if system challenges
// already exist for the current ISO week they are not re-created.
//
// Challenges created each Monday:
//   - Beat Last Week       – distance, target = previous-week km + 10%, reward 15 ⭐
//   - Try a New Sport      – custom type, reward 10 ⭐
//   - Consistency King     – workout_count = 4, reward 10 ⭐
//   - Heart Zone Challenge – custom type, reward 10 ⭐
//
// "Beat Last Week" is created per-child because its target depends on each child's
// individual last-week distance. The other three are shared challenges that all
// children are enrolled in as participants.
func GenerateWeeklyChallenges(ctx context.Context, db *sql.DB) error {
	now := time.Now().UTC()
	year, week := now.ISOWeek()
	mon := firstDayOfISOWeek(year, week)
	sun := mon.AddDate(0, 0, 6)
	startDate := mon.Format("2006-01-02")
	endDate := sun.Format("2006-01-02")

	// Idempotency: skip only when the week was fully committed. Because all
	// inserts run inside a transaction below, a partial/failed run rolls back
	// entirely, so this guard will be false on the next attempt and generation
	// will retry from scratch.
	var existing int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM family_challenges WHERE is_system = 1 AND start_date = ?
	`, startDate).Scan(&existing); err != nil {
		return fmt.Errorf("weekly challenges idempotency check: %w", err)
	}
	if existing > 0 {
		return nil
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("weekly challenges begin tx: %w", err)
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	// Collect all distinct children across all family links.
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT child_id FROM family_links`)
	if err != nil {
		return fmt.Errorf("weekly challenges get children: %w", err)
	}
	var childIDs []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return fmt.Errorf("weekly challenges scan child: %w", err)
		}
		childIDs = append(childIDs, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("weekly challenges children rows: %w", err)
	}
	if len(childIDs) == 0 {
		return nil
	}

	nowStr := now.Format(time.RFC3339)
	prevMon := mon.AddDate(0, 0, -7)
	prevWeekStart := prevMon.Format(time.RFC3339)
	prevWeekEnd := mon.Format(time.RFC3339)

	// Create the three shared challenges (same target for all children).
	type sharedDef struct {
		title, description, challengeType string
		targetValue                       float64
		starReward                        int
	}
	shared := []sharedDef{
		{
			title:         "Try a New Sport",
			description:   "Try a sport you haven't logged this month. Mix it up!",
			challengeType: "custom",
			targetValue:   1,
			starReward:    10,
		},
		{
			title:         "Consistency King",
			description:   "Log 4 or more workouts this week. Stay consistent!",
			challengeType: "workout_count",
			targetValue:   4,
			starReward:    10,
		},
		{
			title:         "Heart Zone Challenge",
			description:   "Spend time in your target heart rate zone this week. Feel the burn!",
			challengeType: "custom",
			targetValue:   1,
			starReward:    10,
		},
	}

	var sharedIDs []int64
	for _, def := range shared {
		id, err := insertSystemChallenge(ctx, tx, def.title, def.description, def.challengeType, def.targetValue, def.starReward, startDate, endDate, nowStr)
		if err != nil {
			return fmt.Errorf("insert shared challenge %q: %w", def.title, err)
		}
		sharedIDs = append(sharedIDs, id)
	}

	// Enroll all children in the shared challenges.
	for _, childID := range childIDs {
		for _, challengeID := range sharedIDs {
			if _, err := tx.ExecContext(ctx, `
				INSERT OR IGNORE INTO challenge_participants (challenge_id, child_id, added_at)
				VALUES (?, ?, ?)
			`, challengeID, childID, nowStr); err != nil {
				return fmt.Errorf("enroll child %d in challenge %d: %w", childID, challengeID, err)
			}
		}
	}

	// Create a per-child "Beat Last Week" challenge whose target is 10% above
	// each child's previous-week total distance. Minimum target is 1 km.
	for _, childID := range childIDs {
		var prevDistanceM float64
		if err := tx.QueryRowContext(ctx, `
			SELECT COALESCE(SUM(distance_meters), 0) FROM workouts
			WHERE user_id = ? AND started_at >= ? AND started_at < ?
		`, childID, prevWeekStart, prevWeekEnd).Scan(&prevDistanceM); err != nil {
			log.Printf("stars: weekly challenge prev distance child %d: %v", childID, err)
		}

		targetKm := prevDistanceM / 1000.0 * 1.1
		if targetKm < 1.0 {
			targetKm = 1.0
		}
		targetKm = math.Round(targetKm*10) / 10 // round to 1 decimal place

		desc := fmt.Sprintf("Run %.1f km this week — 10%% more than last week!", targetKm)
		id, err := insertSystemChallenge(ctx, tx, "Beat Last Week", desc, "distance", targetKm, 15, startDate, endDate, nowStr)
		if err != nil {
			return fmt.Errorf("insert Beat Last Week challenge for child %d: %w", childID, err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT OR IGNORE INTO challenge_participants (challenge_id, child_id, added_at)
			VALUES (?, ?, ?)
		`, id, childID, nowStr); err != nil {
			return fmt.Errorf("enroll child %d in Beat Last Week challenge: %w", childID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("weekly challenges commit: %w", err)
	}
	tx = nil // prevent deferred rollback
	return nil
}

// insertSystemChallenge inserts a system-generated challenge with is_system=1 and
// creator_id=SystemCreatorID, encrypting title and description. Returns the new row ID.
func insertSystemChallenge(ctx context.Context, tx *sql.Tx, title, description, challengeType string, targetValue float64, starReward int, startDate, endDate, now string) (int64, error) {
	encTitle, err := encryption.EncryptField(title)
	if err != nil {
		return 0, fmt.Errorf("encrypt title: %w", err)
	}
	encDesc, err := encryption.EncryptField(description)
	if err != nil {
		return 0, fmt.Errorf("encrypt description: %w", err)
	}

	res, err := tx.ExecContext(ctx, `
		INSERT INTO family_challenges
		  (creator_id, title, description, challenge_type, target_value, star_reward,
		   start_date, end_date, is_active, is_system, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1, 1, ?, ?)
	`, SystemCreatorID, encTitle, encDesc, challengeType, targetValue, starReward,
		startDate, endDate, now, now)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}
