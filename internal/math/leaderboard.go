package math

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/family"
)

// LeaderboardEntry is a single family member's best run for a mode and
// period. Score, SessionID and AchievedAt are pointers so they can serialise
// as JSON null when the member has not yet finished a run in the window.
// Rank is also nullable: members without a score sort to the bottom and are
// displayed as unranked.
type LeaderboardEntry struct {
	UserID      int64   `json:"user_id"`
	Name        string  `json:"name"`
	AvatarEmoji string  `json:"avatar_emoji"`
	IsParent    bool    `json:"is_parent"`
	Score       *int64  `json:"score"`
	SessionID   *int64  `json:"session_id"`
	AchievedAt  *string `json:"achieved_at"`
	Rank        *int    `json:"rank"`
}

// Leaderboard is the full leaderboard response for a family.
type Leaderboard struct {
	Mode    string             `json:"mode"`
	Period  string             `json:"period"`
	Entries []LeaderboardEntry `json:"entries"`
}

// PeriodAll and PeriodWeek are the two supported time windows.
const (
	PeriodAll  = "all"
	PeriodWeek = "week"
)

// ErrInvalidPeriod is returned by the leaderboard service when the caller
// passes a period other than PeriodAll or PeriodWeek.
var ErrInvalidPeriod = errors.New("invalid leaderboard period")

// weekStartUTC returns the Monday 00:00:00 UTC of the ISO week containing t.
// Matches the convention already used by the stars leaderboard.
func weekStartUTC(t time.Time) time.Time {
	t = t.UTC()
	daysSinceMonday := (int(t.Weekday()) + 6) % 7
	return t.AddDate(0, 0, -daysSinceMonday).Truncate(24 * time.Hour)
}

// FamilyMember is a single user considered for inclusion on the leaderboard:
// either the family's parent or one of their linked children. Name is the
// display name — the parent's user.name or the child's family_links.nickname.
type FamilyMember struct {
	UserID      int64
	Name        string
	AvatarEmoji string
	IsParent    bool
}

// resolveFamily returns the set of users in the caller's family: the parent
// plus every linked child. If the caller has no family links they are
// returned alone so the leaderboard still renders a single-row response
// instead of failing.
func resolveFamily(ctx context.Context, db *sql.DB, userID int64) ([]FamilyMember, error) {
	// If the caller is linked as a child, their parent is the family root.
	// Otherwise treat the caller themselves as the root (they may be the
	// parent, or an unlinked solo user).
	var parentID int64
	parentLink, err := family.GetParent(db, userID)
	if err != nil {
		return nil, fmt.Errorf("resolve parent: %w", err)
	}
	if parentLink != nil {
		parentID = parentLink.ParentID
	} else {
		parentID = userID
	}

	var parentName string
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(name, '') FROM users WHERE id = ?`, parentID).Scan(&parentName); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("load parent name: %w", err)
	}

	members := []FamilyMember{{
		UserID:   parentID,
		Name:     parentName,
		IsParent: true,
	}}

	children, err := family.GetChildren(db, parentID)
	if err != nil {
		return nil, fmt.Errorf("load children: %w", err)
	}
	for _, child := range children {
		members = append(members, FamilyMember{
			UserID:      child.ChildID,
			Name:        child.Nickname,
			AvatarEmoji: child.AvatarEmoji,
		})
	}
	return members, nil
}

// BuildLeaderboard fetches the best run for each family member for the given
// mode and period and returns a ranked entry list. Members without a
// qualifying run are included with nil score and no rank.
func (s *Service) BuildLeaderboard(ctx context.Context, userID int64, mode, period string) (*Leaderboard, error) {
	if mode != ModeMarathon && mode != ModeBlitz {
		return nil, ErrInvalidMode
	}
	if period != PeriodAll && period != PeriodWeek {
		return nil, ErrInvalidPeriod
	}

	members, err := resolveFamily(ctx, s.db, userID)
	if err != nil {
		return nil, err
	}

	var sinceStr string
	if period == PeriodWeek {
		sinceStr = weekStartUTC(time.Now()).Format(timeFormat)
	}

	bests, err := s.bestForMembers(ctx, members, mode, sinceStr)
	if err != nil {
		return nil, err
	}

	entries := make([]LeaderboardEntry, 0, len(members))
	for _, m := range members {
		entry := LeaderboardEntry{
			UserID:      m.UserID,
			Name:        m.Name,
			AvatarEmoji: m.AvatarEmoji,
			IsParent:    m.IsParent,
		}
		if best := bests[m.UserID]; best != nil {
			score := best.Score
			sid := best.SessionID
			at := best.AchievedAt
			entry.Score = &score
			entry.SessionID = &sid
			entry.AchievedAt = &at
		}
		entries = append(entries, entry)
	}

	sortLeaderboard(entries, mode)
	assignRanks(entries)
	return &Leaderboard{Mode: mode, Period: period, Entries: entries}, nil
}

// memberBest carries the best-run stats for a single user.
type memberBest struct {
	SessionID  int64
	Score      int64
	AchievedAt string
}

// bestForMembers fetches the best qualifying run for each member in a single
// query, returning a map from user_id to their best. Members with no
// qualifying run are absent from the map.
//
// For Marathon the "best" is the fastest finished run with the canonical
// attempt count; for Blitz it is the highest-scoring finished run with
// duration as the tiebreaker. Both modes use ended_at for the weekly window
// so that a run completed in-week is not excluded because it started just
// before the Monday boundary.
func (s *Service) bestForMembers(ctx context.Context, members []FamilyMember, mode, sinceStr string) (map[int64]*memberBest, error) {
	if len(members) == 0 {
		return nil, nil
	}

	// Build "?,?,?" placeholder for the IN clause.
	placeholders := strings.TrimRight(strings.Repeat("?,", len(members)), ",")

	// Collect member IDs as interface{} for variadic query args.
	idArgs := make([]any, len(members))
	for i, m := range members {
		idArgs[i] = m.UserID
	}

	// Queries use ROW_NUMBER() OVER (PARTITION BY user_id ...) to pick each
	// user's single best session without a separate query per member. Rows end
	// with ORDER BY id ASC as a deterministic tiebreaker so the same session is
	// always returned when every ranking column ties.
	var (
		rows *sql.Rows
		err  error
	)
	switch mode {
	case ModeMarathon:
		if sinceStr == "" {
			args := append(append([]any{}, idArgs...), ModeMarathon, MarathonFactCount)
			rows, err = s.db.QueryContext(ctx, fmt.Sprintf(`
				SELECT user_id, id, duration_ms, ended_at
				FROM (
					SELECT user_id, id, duration_ms, ended_at,
					       ROW_NUMBER() OVER (PARTITION BY user_id ORDER BY duration_ms ASC, total_wrong ASC, id ASC) AS rn
					FROM math_sessions
					WHERE user_id IN (%s)
					  AND mode = ?
					  AND ended_at IS NOT NULL AND ended_at != ''
					  AND (total_correct + total_wrong) = ?
				)
				WHERE rn = 1`, placeholders), args...)
		} else {
			args := append(append([]any{}, idArgs...), ModeMarathon, MarathonFactCount, sinceStr)
			rows, err = s.db.QueryContext(ctx, fmt.Sprintf(`
				SELECT user_id, id, duration_ms, ended_at
				FROM (
					SELECT user_id, id, duration_ms, ended_at,
					       ROW_NUMBER() OVER (PARTITION BY user_id ORDER BY duration_ms ASC, total_wrong ASC, id ASC) AS rn
					FROM math_sessions
					WHERE user_id IN (%s)
					  AND mode = ?
					  AND ended_at IS NOT NULL AND ended_at != ''
					  AND (total_correct + total_wrong) = ?
					  AND ended_at >= ?
				)
				WHERE rn = 1`, placeholders), args...)
		}
	case ModeBlitz:
		if sinceStr == "" {
			args := append(append([]any{}, idArgs...), ModeBlitz)
			rows, err = s.db.QueryContext(ctx, fmt.Sprintf(`
				SELECT user_id, id, score_num, ended_at
				FROM (
					SELECT user_id, id, score_num, ended_at,
					       ROW_NUMBER() OVER (PARTITION BY user_id ORDER BY score_num DESC, duration_ms ASC, id ASC) AS rn
					FROM math_sessions
					WHERE user_id IN (%s)
					  AND mode = ?
					  AND ended_at IS NOT NULL AND ended_at != ''
				)
				WHERE rn = 1`, placeholders), args...)
		} else {
			args := append(append([]any{}, idArgs...), ModeBlitz, sinceStr)
			rows, err = s.db.QueryContext(ctx, fmt.Sprintf(`
				SELECT user_id, id, score_num, ended_at
				FROM (
					SELECT user_id, id, score_num, ended_at,
					       ROW_NUMBER() OVER (PARTITION BY user_id ORDER BY score_num DESC, duration_ms ASC, id ASC) AS rn
					FROM math_sessions
					WHERE user_id IN (%s)
					  AND mode = ?
					  AND ended_at IS NOT NULL AND ended_at != ''
					  AND ended_at >= ?
				)
				WHERE rn = 1`, placeholders), args...)
		}
	default:
		return nil, ErrInvalidMode
	}
	if err != nil {
		return nil, fmt.Errorf("query best for members: %w", err)
	}
	defer rows.Close()

	result := make(map[int64]*memberBest, len(members))
	for rows.Next() {
		var userID int64
		var best memberBest
		if err := rows.Scan(&userID, &best.SessionID, &best.Score, &best.AchievedAt); err != nil {
			return nil, fmt.Errorf("scan best: %w", err)
		}
		result[userID] = &best
	}
	return result, rows.Err()
}

// sortLeaderboard orders entries for display. Members without a score go
// last; among the rest the order is ascending duration for Marathon and
// descending score for Blitz, with name as a deterministic tiebreaker.
func sortLeaderboard(entries []LeaderboardEntry, mode string) {
	sort.SliceStable(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if a.Score == nil && b.Score == nil {
			return a.Name < b.Name
		}
		if a.Score == nil {
			return false
		}
		if b.Score == nil {
			return true
		}
		if *a.Score != *b.Score {
			if mode == ModeMarathon {
				return *a.Score < *b.Score
			}
			return *a.Score > *b.Score
		}
		return a.Name < b.Name
	})
}

// assignRanks fills in Rank for every entry that has a score. Ties on score
// share the same rank; entries with no score are left unranked.
func assignRanks(entries []LeaderboardEntry) {
	rank := 0
	for i := range entries {
		if entries[i].Score == nil {
			entries[i].Rank = nil
			continue
		}
		if i == 0 || entries[i-1].Score == nil || *entries[i-1].Score != *entries[i].Score {
			rank = i + 1
		}
		r := rank
		entries[i].Rank = &r
	}
}
