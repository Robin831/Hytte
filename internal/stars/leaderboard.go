package stars

import (
	"context"
	"database/sql"
	"time"

	"github.com/Robin831/Hytte/internal/family"
)

// LeaderboardEntry is a single child's stats row in a leaderboard.
type LeaderboardEntry struct {
	UserID       int64  `json:"user_id"`
	Nickname     string `json:"nickname"`
	AvatarEmoji  string `json:"avatar_emoji"`
	Stars        int64  `json:"stars"`
	WorkoutCount int64  `json:"workout_count"`
	Streak       int64  `json:"streak"`
	Rank         int    `json:"rank"`
}

// Leaderboard is the full leaderboard response for a family.
type Leaderboard struct {
	Period             string             `json:"period"`
	GeneratedAt        string             `json:"generated_at"`
	LeaderboardVisible bool               `json:"leaderboard_visible"`
	Entries            []LeaderboardEntry `json:"entries"`
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
// When parentParticipates is true, the parent's own stats are included as an entry.
func GetWeeklyLeaderboard(ctx context.Context, db *sql.DB, parentID int64, parentParticipates bool) (*Leaderboard, error) {
	since := weekStart(time.Now())
	return buildLeaderboard(ctx, db, parentID, "weekly", since, parentParticipates)
}

// GetMonthlyLeaderboard returns the leaderboard for the current calendar month.
// When parentParticipates is true, the parent's own stats are included as an entry.
func GetMonthlyLeaderboard(ctx context.Context, db *sql.DB, parentID int64, parentParticipates bool) (*Leaderboard, error) {
	since := monthStart(time.Now())
	return buildLeaderboard(ctx, db, parentID, "monthly", since, parentParticipates)
}

// GetAllTimeLeaderboard returns the all-time leaderboard using the denormalized
// star_balances table for efficiency.
// When parentParticipates is true, the parent's own stats are included as an entry.
func GetAllTimeLeaderboard(ctx context.Context, db *sql.DB, parentID int64, parentParticipates bool) (*Leaderboard, error) {
	return buildLeaderboard(ctx, db, parentID, "alltime", time.Time{}, parentParticipates)
}

// fetchUserEntry builds a LeaderboardEntry for a single user by querying their
// star, workout, and streak stats for the given period.
func fetchUserEntry(ctx context.Context, db *sql.DB, userID int64, period string, since time.Time, nickname, avatarEmoji string) (LeaderboardEntry, error) {
	entry := LeaderboardEntry{
		UserID:      userID,
		Nickname:    nickname,
		AvatarEmoji: avatarEmoji,
	}

	// Stars for period.
	if period == "alltime" {
		if err := db.QueryRowContext(ctx, `
			SELECT COALESCE(total_earned, 0) FROM star_balances WHERE user_id = ?
		`, userID).Scan(&entry.Stars); err != nil && err != sql.ErrNoRows {
			return entry, err
		}
	} else {
		sinceStr := since.Format(time.RFC3339)
		if err := db.QueryRowContext(ctx, `
			SELECT COALESCE(SUM(amount), 0)
			FROM star_transactions
			WHERE user_id = ? AND amount > 0 AND created_at >= ?
		`, userID, sinceStr).Scan(&entry.Stars); err != nil {
			return entry, err
		}
	}

	// Workout count for period: distinct workouts that earned stars.
	if period == "alltime" {
		if err := db.QueryRowContext(ctx, `
			SELECT COUNT(DISTINCT reference_id)
			FROM star_transactions
			WHERE user_id = ? AND amount > 0 AND reference_id IS NOT NULL
		`, userID).Scan(&entry.WorkoutCount); err != nil {
			return entry, err
		}
	} else {
		sinceStr := since.Format(time.RFC3339)
		if err := db.QueryRowContext(ctx, `
			SELECT COUNT(DISTINCT reference_id)
			FROM star_transactions
			WHERE user_id = ? AND amount > 0 AND reference_id IS NOT NULL AND created_at >= ?
		`, userID, sinceStr).Scan(&entry.WorkoutCount); err != nil {
			return entry, err
		}
	}

	// Current daily workout streak.
	if err := db.QueryRowContext(ctx, `
		SELECT COALESCE(current_count, 0)
		FROM streaks
		WHERE user_id = ? AND streak_type = 'daily_workout'
	`, userID).Scan(&entry.Streak); err != nil && err != sql.ErrNoRows {
		return entry, err
	}

	return entry, nil
}

// buildLeaderboard fetches the children for parentID and aggregates their stats
// for the given period. A zero since time means all-time (no date filter).
// When parentParticipates is true, the parent's own stats are included as an entry.
func buildLeaderboard(ctx context.Context, db *sql.DB, parentID int64, period string, since time.Time, parentParticipates bool) (*Leaderboard, error) {
	children, err := family.GetChildren(db, parentID)
	if err != nil {
		return nil, err
	}

	capacity := len(children)
	if parentParticipates {
		capacity++
	}
	entries := make([]LeaderboardEntry, 0, capacity)
	for _, child := range children {
		entry, err := fetchUserEntry(ctx, db, child.ChildID, period, since, child.Nickname, child.AvatarEmoji)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}

	// Optionally include the parent as a participant.
	if parentParticipates {
		var parentName string
		if err := db.QueryRowContext(ctx, `SELECT COALESCE(name, '') FROM users WHERE id = ?`, parentID).Scan(&parentName); err != nil && err != sql.ErrNoRows {
			return nil, err
		}
		parentEntry, err := fetchUserEntry(ctx, db, parentID, period, since, parentName, "")
		if err != nil {
			return nil, err
		}
		entries = append(entries, parentEntry)
	}

	// Sort entries by stars DESC; break ties alphabetically by nickname for
	// deterministic ordering (simple insertion sort, fine for small families).
	for i := 1; i < len(entries); i++ {
		for j := i; j > 0; j-- {
			a, b := entries[j], entries[j-1]
			if a.Stars > b.Stars || (a.Stars == b.Stars && a.Nickname < b.Nickname) {
				entries[j], entries[j-1] = entries[j-1], entries[j]
			} else {
				break
			}
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
