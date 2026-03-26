package stars

import (
	"context"
	"database/sql"
	"time"

	"github.com/Robin831/Hytte/internal/family"
)

// LeaderboardEntry is a single child's stats row in a leaderboard.
type LeaderboardEntry struct {
	UserID      int64  `json:"user_id"`
	Nickname    string `json:"nickname"`
	AvatarEmoji string `json:"avatar_emoji"`
	Stars       int    `json:"stars"`
	WorkoutCount int   `json:"workout_count"`
	Streak      int64  `json:"streak"`
	Rank        int    `json:"rank"`
}

// Leaderboard is the full leaderboard response for a family.
type Leaderboard struct {
	Period      string             `json:"period"`
	GeneratedAt string             `json:"generated_at"`
	Entries     []LeaderboardEntry `json:"entries"`
}

// weekStart returns the Monday 00:00:00 UTC of the ISO week containing t.
func weekStart(t time.Time) time.Time {
	t = t.UTC()
	daysSinceMonday := (int(t.Weekday()) + 6) % 7
	return t.AddDate(0, 0, -daysSinceMonday).Truncate(24 * time.Hour)
}

// monthStart returns the first day 00:00:00 UTC of the month containing t.
func monthStart(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}

// GetWeeklyLeaderboard returns the leaderboard for the current ISO week.
// parentID is the parent whose children form the family group.
func GetWeeklyLeaderboard(ctx context.Context, db *sql.DB, parentID int64) (*Leaderboard, error) {
	since := weekStart(time.Now())
	return buildLeaderboard(ctx, db, parentID, "weekly", since)
}

// GetMonthlyLeaderboard returns the leaderboard for the current calendar month.
func GetMonthlyLeaderboard(ctx context.Context, db *sql.DB, parentID int64) (*Leaderboard, error) {
	since := monthStart(time.Now())
	return buildLeaderboard(ctx, db, parentID, "monthly", since)
}

// GetAllTimeLeaderboard returns the all-time leaderboard using the denormalized
// star_balances table for efficiency.
func GetAllTimeLeaderboard(ctx context.Context, db *sql.DB, parentID int64) (*Leaderboard, error) {
	return buildLeaderboard(ctx, db, parentID, "alltime", time.Time{})
}

// buildLeaderboard fetches the children for parentID and aggregates their stats
// for the given period. A zero since time means all-time (no date filter).
func buildLeaderboard(ctx context.Context, db *sql.DB, parentID int64, period string, since time.Time) (*Leaderboard, error) {
	children, err := family.GetChildren(db, parentID)
	if err != nil {
		return nil, err
	}

	entries := make([]LeaderboardEntry, 0, len(children))
	for _, child := range children {
		entry := LeaderboardEntry{
			UserID:      child.ChildID,
			Nickname:    child.Nickname,
			AvatarEmoji: child.AvatarEmoji,
		}

		// Stars for period.
		if period == "alltime" {
			if err := db.QueryRowContext(ctx, `
				SELECT COALESCE(total_earned, 0) FROM star_balances WHERE user_id = ?
			`, child.ChildID).Scan(&entry.Stars); err != nil && err != sql.ErrNoRows {
				return nil, err
			}
		} else {
			sinceStr := since.Format(time.RFC3339)
			if err := db.QueryRowContext(ctx, `
				SELECT COALESCE(SUM(amount), 0)
				FROM star_transactions
				WHERE user_id = ? AND amount > 0 AND created_at >= ?
			`, child.ChildID, sinceStr).Scan(&entry.Stars); err != nil {
				return nil, err
			}
		}

		// Workout count for period: distinct workouts that earned stars.
		if period == "alltime" {
			if err := db.QueryRowContext(ctx, `
				SELECT COUNT(DISTINCT reference_id)
				FROM star_transactions
				WHERE user_id = ? AND amount > 0 AND reference_id IS NOT NULL
			`, child.ChildID).Scan(&entry.WorkoutCount); err != nil {
				return nil, err
			}
		} else {
			sinceStr := since.Format(time.RFC3339)
			if err := db.QueryRowContext(ctx, `
				SELECT COUNT(DISTINCT reference_id)
				FROM star_transactions
				WHERE user_id = ? AND amount > 0 AND reference_id IS NOT NULL AND created_at >= ?
			`, child.ChildID, sinceStr).Scan(&entry.WorkoutCount); err != nil {
				return nil, err
			}
		}

		// Current daily workout streak.
		if err := db.QueryRowContext(ctx, `
			SELECT COALESCE(current_count, 0)
			FROM streaks
			WHERE user_id = ? AND streak_type = 'daily_workout'
		`, child.ChildID).Scan(&entry.Streak); err != nil && err != sql.ErrNoRows {
			return nil, err
		}

		entries = append(entries, entry)
	}

	// Sort entries by stars DESC (simple insertion sort for small families).
	for i := 1; i < len(entries); i++ {
		for j := i; j > 0 && entries[j].Stars > entries[j-1].Stars; j-- {
			entries[j], entries[j-1] = entries[j-1], entries[j]
		}
	}

	// Assign ranks (tied stars share the same rank).
	for i := range entries {
		if i == 0 {
			entries[i].Rank = 1
		} else if entries[i].Stars == entries[i-1].Stars {
			entries[i].Rank = entries[i-1].Rank
		} else {
			entries[i].Rank = i + 1
		}
	}

	return &Leaderboard{
		Period:      period,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Entries:     entries,
	}, nil
}
