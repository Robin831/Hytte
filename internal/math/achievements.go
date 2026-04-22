package math

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Achievement tier constants. Tiers group the registry both server-side
// (in the API response) and on the frontend (where they drive the tabbed
// display).
const (
	TierMarathon = "marathon"
	TierBlitz    = "blitz"
	TierRivalry  = "rivalry"
)

// Achievement codes. Stored as math_achievements.code and used by the
// frontend to look up translated titles. Codes are part of the public API:
// once shipped they must not be renamed — adding a new milestone means
// adding a new code, not editing an existing one.
const (
	AchMarathonSub10   = "marathon_sub_10"
	AchMarathonSub7    = "marathon_sub_7"
	AchMarathonSub5    = "marathon_sub_5"
	AchMarathonSub4    = "marathon_sub_4"
	AchMarathonSub3    = "marathon_sub_3"
	AchMarathonPerfect = "marathon_perfect_100"
	AchStreak25        = "streak_25"
	AchStreak50        = "streak_50"
	AchStreak100       = "streak_100"
	AchFirstBlood      = "first_blood"
)

// CheckContext bundles the inputs every Achievement.Check evaluates. The
// Session pointer is the run that just finished — all unlocks fire from
// the Finish handler — so it is non-nil during evaluation. UserStats holds
// the user's aggregate state (best Marathon time, best Blitz streak,
// leaderboard standing) computed once per evaluation so each Check stays
// cheap.
type CheckContext struct {
	Session   *Summary
	UserStats UserAchievementStats
}

// Achievement defines a single milestone the user can unlock. Title and
// Description are English fallbacks for the API; the frontend prefers
// per-code i18n strings.
type Achievement struct {
	Code        string
	Title       string
	Description string
	Tier        string
	Check       func(ctx CheckContext) bool
}

// UserAchievementStats is the aggregated state every Check function reads.
// It is also returned by the achievements endpoint so the frontend can
// render progress hints on locked rows ("Best: 5:42 — 42s to Sub-5") in
// the user's locale.
type UserAchievementStats struct {
	HasMarathon         bool  `json:"has_marathon"`
	BestMarathonMs      int64 `json:"best_marathon_ms"`
	BestMarathonWrong   int   `json:"best_marathon_wrong"`
	FewestMarathonWrong int   `json:"fewest_marathon_wrong"`
	HasBlitz            bool  `json:"has_blitz"`
	BestBlitzStreak     int   `json:"best_blitz_streak"`
	OnTopAnyBoard       bool  `json:"on_top_any_board"`
}

func marathonSubCheck(thresholdMs int64) func(CheckContext) bool {
	return func(c CheckContext) bool {
		if c.Session == nil {
			return false
		}
		if c.Session.Mode != ModeMarathon {
			return false
		}
		if c.Session.TotalCorrect+c.Session.TotalWrong != MarathonFactCount {
			return false
		}
		return c.Session.DurationMs > 0 && c.Session.DurationMs < thresholdMs
	}
}

func marathonPerfectCheck(c CheckContext) bool {
	if c.Session == nil {
		return false
	}
	if c.Session.Mode != ModeMarathon {
		return false
	}
	if c.Session.TotalCorrect+c.Session.TotalWrong != MarathonFactCount {
		return false
	}
	return c.Session.TotalWrong == 0
}

func streakCheck(threshold int) func(CheckContext) bool {
	return func(c CheckContext) bool {
		if c.Session == nil {
			return false
		}
		if c.Session.Mode != ModeBlitz {
			return false
		}
		return c.Session.BestStreak >= threshold
	}
}

func firstBloodCheck(c CheckContext) bool {
	if c.Session == nil {
		return false
	}
	return c.UserStats.OnTopAnyBoard
}

// Registry returns the canonical achievement list, in the order the
// frontend should render them (each tier ascending in difficulty).
// Returning a fresh slice keeps callers from accidentally mutating the
// shared list.
func Registry() []Achievement {
	return []Achievement{
		{
			Code:        AchMarathonSub10,
			Title:       "Marathon Sub-10",
			Description: "Finish a Marathon in under 10:00.",
			Tier:        TierMarathon,
			Check:       marathonSubCheck(10 * 60 * 1000),
		},
		{
			Code:        AchMarathonSub7,
			Title:       "Marathon Sub-7",
			Description: "Finish a Marathon in under 7:00.",
			Tier:        TierMarathon,
			Check:       marathonSubCheck(7 * 60 * 1000),
		},
		{
			Code:        AchMarathonSub5,
			Title:       "Marathon Sub-5",
			Description: "Finish a Marathon in under 5:00.",
			Tier:        TierMarathon,
			Check:       marathonSubCheck(5 * 60 * 1000),
		},
		{
			Code:        AchMarathonSub4,
			Title:       "Marathon Sub-4",
			Description: "Finish a Marathon in under 4:00.",
			Tier:        TierMarathon,
			Check:       marathonSubCheck(4 * 60 * 1000),
		},
		{
			Code:        AchMarathonSub3,
			Title:       "Marathon Sub-3",
			Description: "Finish a Marathon in under 3:00.",
			Tier:        TierMarathon,
			Check:       marathonSubCheck(3 * 60 * 1000),
		},
		{
			Code:        AchMarathonPerfect,
			Title:       "Flawless Marathon",
			Description: "Finish a Marathon with zero wrong answers.",
			Tier:        TierMarathon,
			Check:       marathonPerfectCheck,
		},
		{
			Code:        AchStreak25,
			Title:       "Streak 25",
			Description: "Reach a 25-answer streak in a single Blitz run.",
			Tier:        TierBlitz,
			Check:       streakCheck(25),
		},
		{
			Code:        AchStreak50,
			Title:       "Streak 50",
			Description: "Reach a 50-answer streak in a single Blitz run.",
			Tier:        TierBlitz,
			Check:       streakCheck(50),
		},
		{
			Code:        AchStreak100,
			Title:       "Streak 100",
			Description: "Reach a 100-answer streak in a single Blitz run.",
			Tier:        TierBlitz,
			Check:       streakCheck(100),
		},
		{
			Code:        AchFirstBlood,
			Title:       "First Blood",
			Description: "Land at #1 on any leaderboard for the first time.",
			Tier:        TierRivalry,
			Check:       firstBloodCheck,
		},
	}
}

// EarnedAchievementRow is one row from math_achievements joined with the
// registry metadata.
type EarnedAchievementRow struct {
	Code        string `json:"code"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Tier        string `json:"tier"`
	UnlockedAt  string `json:"unlocked_at"`
	SessionID   *int64 `json:"session_id,omitempty"`
}

// LockedAchievementRow describes an achievement the user has not yet
// earned. The frontend formats per-tier progress hints from UserStats —
// the row itself only carries identification metadata.
type LockedAchievementRow struct {
	Code        string `json:"code"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Tier        string `json:"tier"`
}

// AchievementsResponse is the wire format for GET /api/math/achievements.
type AchievementsResponse struct {
	UserStats UserAchievementStats   `json:"user_stats"`
	Earned    []EarnedAchievementRow `json:"earned"`
	Locked    []LockedAchievementRow `json:"locked"`
}

// LoadAchievementStats computes the user's aggregate state used both by
// EvaluateAchievements (during finish) and by ListAchievements (for the
// achievements page). Errors loading any single component are wrapped so
// the caller can surface them.
func (s *Service) LoadAchievementStats(ctx context.Context, userID int64) (UserAchievementStats, error) {
	stats := UserAchievementStats{}

	bestM, err := s.BestMarathon(ctx, userID)
	if err != nil {
		return stats, fmt.Errorf("best marathon: %w", err)
	}
	if bestM != nil {
		stats.HasMarathon = true
		stats.BestMarathonMs = bestM.DurationMs
		stats.BestMarathonWrong = bestM.TotalWrong

		// FewestMarathonWrong is the minimum wrong-answer count across all
		// completed marathons — distinct from BestMarathonWrong (which is the
		// wrong count on the fastest run). The Flawless Marathon progress hint
		// needs the true minimum, not the PB run's wrong count.
		var fewest int
		if err := s.db.QueryRowContext(ctx, `
			SELECT MIN(total_wrong) FROM math_sessions
			WHERE user_id = ? AND mode = ?
			  AND ended_at IS NOT NULL AND ended_at != ''
			  AND (total_correct + total_wrong) = ?`,
			userID, ModeMarathon, MarathonFactCount,
		).Scan(&fewest); err != nil {
			return stats, fmt.Errorf("fewest marathon wrong: %w", err)
		}
		stats.FewestMarathonWrong = fewest
	}

	// BestBlitzStreak is the maximum streak across all finished Blitz sessions,
	// not just the highest-scoring one. Query MAX(best_streak) directly so the
	// progress hint reflects the user's true personal best.
	var blitzCount int
	var maxStreak sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*), MAX(best_streak) FROM math_sessions
		WHERE user_id = ? AND mode = ?
		  AND ended_at IS NOT NULL AND ended_at != ''`,
		userID, ModeBlitz,
	).Scan(&blitzCount, &maxStreak); err != nil {
		return stats, fmt.Errorf("blitz stats: %w", err)
	}
	if blitzCount > 0 {
		stats.HasBlitz = true
		if maxStreak.Valid {
			stats.BestBlitzStreak = int(maxStreak.Int64)
		}
	}

	top, err := s.userOnTopOfAnyBoard(ctx, userID)
	if err != nil {
		return stats, fmt.Errorf("leaderboard rank: %w", err)
	}
	stats.OnTopAnyBoard = top

	return stats, nil
}

// userOnTopOfAnyBoard reports whether the user is currently ranked #1 on
// any (mode, period) family leaderboard. A solo user with no family ties
// is always #1 of a one-row leaderboard, which would make first_blood
// trivial — so a board with a single ranked entry does not count.
func (s *Service) userOnTopOfAnyBoard(ctx context.Context, userID int64) (bool, error) {
	combos := []struct {
		mode, period string
	}{
		{ModeMarathon, PeriodAll},
		{ModeMarathon, PeriodWeek},
		{ModeBlitz, PeriodAll},
		{ModeBlitz, PeriodWeek},
	}
	for _, c := range combos {
		lb, err := s.BuildLeaderboard(ctx, userID, c.mode, c.period)
		if err != nil {
			return false, fmt.Errorf("leaderboard %s/%s: %w", c.mode, c.period, err)
		}
		// Count how many entries actually have a score — a solo family
		// or a single-finisher family doesn't count as "beat everyone".
		ranked := 0
		for _, e := range lb.Entries {
			if e.Score != nil {
				ranked++
			}
		}
		if ranked < 2 {
			continue
		}
		for _, e := range lb.Entries {
			if e.UserID == userID && e.Rank != nil && *e.Rank == 1 {
				return true, nil
			}
		}
	}
	return false, nil
}

// hasLockedAchievements reports whether any registered achievement has not
// yet been earned by the user. Used to short-circuit the expensive stats
// load when all achievements are already unlocked.
func hasLockedAchievements(earned map[string]bool) bool {
	for _, ach := range Registry() {
		if !earned[ach.Code] {
			return true
		}
	}
	return false
}

// EvaluateAchievements is called from the finish handler after the session
// has been committed. It loads already-earned codes first and skips the
// expensive stats queries when all achievements are already unlocked. For
// users with locked achievements it loads aggregate stats, runs every
// registered Check, and inserts any newly-earned rows. The returned slice
// contains the codes that were actually inserted by this call (i.e. that
// the user did not already have) — the UNIQUE(user_id, code) constraint
// guarantees that subsequent calls are no-ops.
func (s *Service) EvaluateAchievements(ctx context.Context, userID int64, summary Summary) ([]EarnedAchievementRow, error) {
	earned, err := s.listEarnedCodes(ctx, userID)
	if err != nil {
		return nil, err
	}
	if !hasLockedAchievements(earned) {
		return nil, nil
	}

	stats, err := s.LoadAchievementStats(ctx, userID)
	if err != nil {
		return nil, err
	}

	check := CheckContext{Session: &summary, UserStats: stats}
	now := time.Now().UTC().Format(timeFormat)
	var unlocked []EarnedAchievementRow
	for _, ach := range Registry() {
		if earned[ach.Code] {
			continue
		}
		if !ach.Check(check) {
			continue
		}
		// INSERT OR IGNORE protects against a concurrent finisher racing us
		// (two requests for the same session would each try to insert).
		res, err := s.db.ExecContext(ctx,
			`INSERT OR IGNORE INTO math_achievements (user_id, code, unlocked_at, session_id) VALUES (?, ?, ?, ?)`,
			userID, ach.Code, now, summary.SessionID,
		)
		if err != nil {
			return nil, fmt.Errorf("insert achievement %s: %w", ach.Code, err)
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return nil, fmt.Errorf("rows affected for %s: %w", ach.Code, err)
		}
		if affected == 0 {
			continue
		}
		sid := summary.SessionID
		unlocked = append(unlocked, EarnedAchievementRow{
			Code:        ach.Code,
			Title:       ach.Title,
			Description: ach.Description,
			Tier:        ach.Tier,
			UnlockedAt:  now,
			SessionID:   &sid,
		})
	}
	return unlocked, nil
}

// listEarnedCodes returns a set of achievement codes the user has already
// unlocked, used to skip Check calls that would only no-op.
func (s *Service) listEarnedCodes(ctx context.Context, userID int64) (map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT code FROM math_achievements WHERE user_id = ?`, userID,
	)
	if err != nil {
		return nil, fmt.Errorf("select earned codes: %w", err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return nil, fmt.Errorf("scan earned code: %w", err)
		}
		out[code] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate earned codes: %w", err)
	}
	return out, nil
}

// ListAchievements returns the user's earned + locked achievement set
// alongside the aggregate stats the frontend needs to render progress
// hints. Earned rows are joined with the registry so callers receive
// titles, descriptions and tiers in one round trip.
func (s *Service) ListAchievements(ctx context.Context, userID int64) (AchievementsResponse, error) {
	resp := AchievementsResponse{
		Earned: []EarnedAchievementRow{},
		Locked: []LockedAchievementRow{},
	}

	stats, err := s.LoadAchievementStats(ctx, userID)
	if err != nil {
		return resp, err
	}
	resp.UserStats = stats

	rows, err := s.db.QueryContext(ctx,
		`SELECT code, unlocked_at, session_id FROM math_achievements WHERE user_id = ? ORDER BY unlocked_at ASC, id ASC`,
		userID,
	)
	if err != nil {
		return resp, fmt.Errorf("select achievements: %w", err)
	}
	defer rows.Close()
	earnedCodes := map[string]EarnedAchievementRow{}
	for rows.Next() {
		var (
			code       string
			unlockedAt string
			sid        sql.NullInt64
		)
		if err := rows.Scan(&code, &unlockedAt, &sid); err != nil {
			return resp, fmt.Errorf("scan achievement row: %w", err)
		}
		row := EarnedAchievementRow{Code: code, UnlockedAt: unlockedAt}
		if sid.Valid {
			v := sid.Int64
			row.SessionID = &v
		}
		earnedCodes[code] = row
	}
	if err := rows.Err(); err != nil {
		return resp, fmt.Errorf("iterate achievement rows: %w", err)
	}

	for _, ach := range Registry() {
		if e, ok := earnedCodes[ach.Code]; ok {
			e.Title = ach.Title
			e.Description = ach.Description
			e.Tier = ach.Tier
			resp.Earned = append(resp.Earned, e)
			continue
		}
		resp.Locked = append(resp.Locked, LockedAchievementRow{
			Code:        ach.Code,
			Title:       ach.Title,
			Description: ach.Description,
			Tier:        ach.Tier,
		})
	}
	return resp, nil
}

// achievementCodeSet is the set of valid codes; populated once at init.
var achievementCodeSet = func() map[string]bool {
	m := map[string]bool{}
	for _, a := range Registry() {
		m[a.Code] = true
	}
	return m
}()

// IsValidAchievementCode reports whether code is one of the registered
// milestones. Useful for handlers that accept codes from external input.
func IsValidAchievementCode(code string) bool {
	return achievementCodeSet[code]
}
