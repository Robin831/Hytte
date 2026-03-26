// Package stars provides kid-facing APIs for the Stars/Rewards system.
// Parent-facing Challenge CRUD and participant management live in internal/family,
// reflecting the package boundary: family = parent operations, stars = child-facing rewards.
package stars

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/Robin831/Hytte/internal/encryption"
	"github.com/Robin831/Hytte/internal/family"
	"github.com/Robin831/Hytte/internal/push"
)

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
		if err := awardChallengeCompletion(ctx, db, userID, p.participantID, p.challengeID, p.starReward, now); err != nil {
			log.Printf("stars: challenge completion award failed for user %d challenge %d: %v", userID, p.challengeID, err)
			continue
		}
		go sendChallengeCompletionNotifications(db, userID, p.starReward)
	}
	return nil
}

// awardChallengeCompletion atomically marks a challenge participation as completed
// and records the star transaction. If completed_at is already set (race condition),
// the function returns without awarding to prevent double-awarding.
func awardChallengeCompletion(ctx context.Context, db *sql.DB, userID, participantID, challengeID int64, starReward int, now string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Check-and-set: only update if not already completed.
	res, err := tx.ExecContext(ctx, `
		UPDATE challenge_participants SET completed_at = ?
		WHERE id = ? AND completed_at = ''
	`, now, participantID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		// Already completed by a concurrent call; nothing to do.
		return nil
	}

	if starReward <= 0 {
		return tx.Commit()
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO star_transactions (user_id, amount, reason, description, reference_id, created_at)
		VALUES (?, ?, 'challenge_complete', 'Challenge completed!', ?, ?)
	`, userID, starReward, challengeID, now)
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO star_balances (user_id, total_earned)
		VALUES (?, ?)
		ON CONFLICT(user_id) DO UPDATE SET total_earned = total_earned + excluded.total_earned
	`, userID, starReward)
	if err != nil {
		return err
	}

	return tx.Commit()
}

// sendChallengeCompletionNotifications sends push notifications to the child
// and their parent when a challenge is completed. Errors are logged and not propagated.
func sendChallengeCompletionNotifications(db *sql.DB, childID int64, starReward int) {
	childPayload := push.Notification{
		Title: "Challenge Complete!",
		Body:  fmt.Sprintf("You earned %d stars for completing a challenge!", starReward),
		Tag:   "challenge-complete",
	}
	childBytes, err := json.Marshal(childPayload)
	if err != nil {
		log.Printf("stars: marshal child challenge-complete payload: %v", err)
		return
	}
	if _, err := push.SendToUser(db, pushClient, childID, childBytes); err != nil {
		log.Printf("stars: send challenge-complete push to child %d: %v", childID, err)
	}

	link, err := family.GetParent(db, childID)
	if err != nil {
		log.Printf("stars: get parent for child %d: %v", childID, err)
		return
	}
	if link == nil {
		return
	}

	nickname := link.Nickname
	if nickname == "" {
		nickname = "Your child"
	}
	parentPayload := push.Notification{
		Title: fmt.Sprintf("%s completed a challenge!", nickname),
		Body:  fmt.Sprintf("%s earned %d stars!", nickname, starReward),
		Tag:   "challenge-complete",
	}
	parentBytes, err := json.Marshal(parentPayload)
	if err != nil {
		log.Printf("stars: marshal parent challenge-complete payload: %v", err)
		return
	}
	if _, err := push.SendToUser(db, pushClient, link.ParentID, parentBytes); err != nil {
		log.Printf("stars: send challenge-complete push to parent %d: %v", link.ParentID, err)
	}
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
