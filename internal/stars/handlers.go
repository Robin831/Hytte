package stars

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/family"
	"github.com/Robin831/Hytte/internal/push"
	"github.com/go-chi/chi/v5"
)

// StreakInfo holds the streak counts and last activity date for a single streak type.
type StreakInfo struct {
	CurrentCount int64  `json:"current_count"`
	LongestCount int64  `json:"longest_count"`
	LastActivity string `json:"last_activity"`
}

// StreaksResponse is the API response for GET /api/stars/streaks.
type StreaksResponse struct {
	DailyWorkout  StreakInfo `json:"daily_workout"`
	WeeklyWorkout StreakInfo `json:"weekly_workout"`
}

// StreaksHandler handles GET /api/stars/streaks.
// Returns daily and weekly workout streak data for the authenticated user.
func StreaksHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		rows, err := db.QueryContext(r.Context(), `
			SELECT streak_type, current_count, longest_count, last_activity
			FROM streaks
			WHERE user_id = ? AND streak_type IN ('daily_workout', 'weekly_workout')
		`, user.ID)
		if err != nil {
			log.Printf("stars: streaks query user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load streaks"})
			return
		}
		defer rows.Close()

		resp := StreaksResponse{}
		for rows.Next() {
			var streakType string
			var info StreakInfo
			if err := rows.Scan(&streakType, &info.CurrentCount, &info.LongestCount, &info.LastActivity); err != nil {
				log.Printf("stars: streaks scan user %d: %v", user.ID, err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to scan streaks"})
				return
			}
			switch streakType {
			case "daily_workout":
				resp.DailyWorkout = info
			case "weekly_workout":
				resp.WeeklyWorkout = info
			}
		}
		if err := rows.Err(); err != nil {
			log.Printf("stars: streaks rows error user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read streaks"})
			return
		}

		writeJSON(w, http.StatusOK, resp)
	}
}

// normalizeRFC3339 parses a timestamp string from the DB and re-formats it as
// RFC3339 UTC. If parsing fails the original string is returned unchanged.
func normalizeRFC3339(s string) string {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC().Format(time.RFC3339)
	}
	if t, err := time.Parse("2006-01-02T15:04:05", s); err == nil {
		return t.UTC().Format(time.RFC3339)
	}
	return s
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("stars: writeJSON encode error: %v", err)
	}
}

// BalanceResponse is the API response for GET /api/stars/balance.
type BalanceResponse struct {
	CurrentBalance  int     `json:"current_balance"`
	TotalEarned     int     `json:"total_earned"`
	TotalSpent      int     `json:"total_spent"`
	Level           int     `json:"level"`
	XP              int     `json:"xp"`
	Title           string  `json:"title"`
	Emoji           string  `json:"emoji"`
	XPForNextLevel  int     `json:"xp_for_next_level"`
	ProgressPercent float64 `json:"progress_percent"`
}

// BalanceHandler handles GET /api/stars/balance.
func BalanceHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		var resp BalanceResponse
		err := db.QueryRowContext(r.Context(), `
			SELECT COALESCE(total_earned, 0), COALESCE(total_spent, 0), COALESCE(current_balance, 0)
			FROM star_balances
			WHERE user_id = ?
		`, user.ID).Scan(&resp.TotalEarned, &resp.TotalSpent, &resp.CurrentBalance)
		if err == sql.ErrNoRows {
			lvl1 := LevelDefinitions[0]
			var xpForNext int
			if len(LevelDefinitions) > 1 {
				xpForNext = LevelDefinitions[1].XP
			}
			resp = BalanceResponse{
				Level:          lvl1.Level,
				Title:          lvl1.Title,
				Emoji:          lvl1.Emoji,
				XPForNextLevel: xpForNext,
				ProgressPercent: 0,
			}
			writeJSON(w, http.StatusOK, resp)
			return
		}
		if err != nil {
			log.Printf("stars: balance query user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load balance"})
			return
		}

		// Level data via GetLevelInfo for full progress information.
		info, err := GetLevelInfo(r.Context(), db, user.ID)
		if err != nil {
			log.Printf("stars: level info user %d: %v", user.ID, err)
			resp.Level = 1
			resp.Title = "Rookie Runner"
			resp.Emoji = "🐣"
			resp.XP = 0
			resp.XPForNextLevel = 0
			resp.ProgressPercent = 0
		} else {
			resp.Level = info.Level
			resp.XP = info.CurrentXP
			resp.Title = info.Title
			resp.Emoji = info.Emoji
			resp.XPForNextLevel = info.XPForNextLevel
			resp.ProgressPercent = info.ProgressPercent
		}

		writeJSON(w, http.StatusOK, resp)
	}
}

// BadgeResponse is the API shape for a single earned badge.
type BadgeResponse struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	IconEmoji   string `json:"icon_emoji"`
	Category    string `json:"category"`
	Tier        string `json:"tier"`
	XPReward    int    `json:"xp_reward"`
	AwardedAt   string `json:"awarded_at"`
}

// AvailableBadgeResponse is the API shape for a badge in the full catalogue,
// which includes whether the authenticated user has earned it.
type AvailableBadgeResponse struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	IconEmoji   string `json:"icon_emoji"`
	Category    string `json:"category"`
	Tier        string `json:"tier"`
	XPReward    int    `json:"xp_reward"`
	Earned      bool   `json:"earned"`
	AwardedAt   string `json:"awarded_at,omitempty"`
}

// BadgesHandler handles GET /api/stars/badges.
// Returns all badges that the authenticated user has earned.
func BadgesHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		rows, err := db.QueryContext(r.Context(), `
			SELECT bd.key, bd.name, bd.description, bd.icon, bd.category, bd.tier, bd.xp_reward,
			       ub.earned_at
			FROM user_badges ub
			JOIN badge_definitions bd ON bd.key = ub.badge_key
			WHERE ub.user_id = ?
			ORDER BY ub.earned_at DESC
		`, user.ID)
		if err != nil {
			log.Printf("stars: badges query user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load badges"})
			return
		}
		defer rows.Close()

		badges := []BadgeResponse{}
		for rows.Next() {
			var b BadgeResponse
			var rawAt string
			if err := rows.Scan(&b.Key, &b.Name, &b.Description, &b.IconEmoji, &b.Category, &b.Tier, &b.XPReward, &rawAt); err != nil {
				log.Printf("stars: badges scan user %d: %v", user.ID, err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to scan badges"})
				return
			}
			b.AwardedAt = normalizeRFC3339(rawAt)
			badges = append(badges, b)
		}
		if err := rows.Err(); err != nil {
			log.Printf("stars: badges rows error user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read badges"})
			return
		}

		writeJSON(w, http.StatusOK, badges)
	}
}

// AvailableBadgesHandler handles GET /api/stars/badges/available.
// Returns all badge definitions with an earned flag. Unearned secret badges
// are filtered out server-side so their existence remains hidden until earned.
func AvailableBadgesHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		// Load the user's earned badge keys and their earned_at timestamps in one query.
		earnedRows, err := db.QueryContext(r.Context(),
			`SELECT badge_key, earned_at FROM user_badges WHERE user_id = ?`, user.ID)
		if err != nil {
			log.Printf("stars: available badges earned query user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load earned badges"})
			return
		}

		earned := make(map[string]string) // badge_key → RFC3339 earned_at
		for earnedRows.Next() {
			var key, rawAt string
			if err := earnedRows.Scan(&key, &rawAt); err != nil {
				earnedRows.Close()
				log.Printf("stars: available badges earned scan user %d: %v", user.ID, err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to scan earned badges"})
				return
			}
			earned[key] = normalizeRFC3339(rawAt)
		}
		if err := earnedRows.Err(); err != nil {
			earnedRows.Close()
			log.Printf("stars: available badges earned rows error user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read earned badges"})
			return
		}
		earnedRows.Close()

		// Load all badge definitions.
		defRows, err := db.QueryContext(r.Context(),
			`SELECT key, name, description, icon, category, tier, xp_reward FROM badge_definitions ORDER BY category, name`)
		if err != nil {
			log.Printf("stars: available badges defs query user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load badge definitions"})
			return
		}
		defer defRows.Close()

		available := []AvailableBadgeResponse{}
		for defRows.Next() {
			var b AvailableBadgeResponse
			if err := defRows.Scan(&b.Key, &b.Name, &b.Description, &b.IconEmoji, &b.Category, &b.Tier, &b.XPReward); err != nil {
				log.Printf("stars: available badges defs scan user %d: %v", user.ID, err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to scan badge definitions"})
				return
			}
			awardedAt, isEarned := earned[b.Key]
			// Hide unearned secret badges so their existence is not revealed.
			if b.Category == "secret" && !isEarned {
				continue
			}
			b.Earned = isEarned
			b.AwardedAt = awardedAt
			available = append(available, b)
		}
		if err := defRows.Err(); err != nil {
			log.Printf("stars: available badges defs rows error user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read badge definitions"})
			return
		}

		writeJSON(w, http.StatusOK, available)
	}
}

// Transaction is a single star transaction record for the API.
type Transaction struct {
	ID          int64  `json:"id"`
	Amount      int    `json:"amount"`
	Reason      string `json:"reason"`
	Description string `json:"description"`
	CreatedAt   string `json:"created_at"`
}

// TransactionsHandler handles GET /api/stars/transactions?limit=50&offset=0.
func TransactionsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		limit := 50
		offset := 0
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
				limit = n
			}
		}
		if v := r.URL.Query().Get("offset"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n >= 0 {
				offset = n
			}
		}

		rows, err := db.QueryContext(r.Context(), `
			SELECT id, amount, reason, description, created_at
			FROM star_transactions
			WHERE user_id = ?
			ORDER BY created_at DESC
			LIMIT ? OFFSET ?
		`, user.ID, limit, offset)
		if err != nil {
			log.Printf("stars: transactions query user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load transactions"})
			return
		}
		defer rows.Close()

		var txns []Transaction
		for rows.Next() {
			var tx Transaction
			if err := rows.Scan(&tx.ID, &tx.Amount, &tx.Reason, &tx.Description, &tx.CreatedAt); err != nil {
				log.Printf("stars: transaction scan user %d: %v", user.ID, err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to scan transactions"})
				return
			}
			txns = append(txns, tx)
		}
		if err := rows.Err(); err != nil {
			log.Printf("stars: transactions rows error user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read transactions"})
			return
		}

		if txns == nil {
			txns = []Transaction{}
		}

		// Weekly stats: stars earned and count of workouts that earned stars this week
		// (current calendar week starting Monday, UTC).
		now := time.Now().UTC()
		// Go's Weekday: Sunday=0, Monday=1, ..., Saturday=6. We want Monday as week start.
		daysSinceMonday := (int(now.Weekday()) + 6) % 7
		weekStart := now.AddDate(0, 0, -daysSinceMonday).Truncate(24 * time.Hour)
		weekStartStr := weekStart.Format(time.RFC3339)
		var weeklyStars int
		var weeklyStarredWorkouts int
		if err := db.QueryRowContext(r.Context(), `
			SELECT COALESCE(SUM(amount), 0), COUNT(DISTINCT reference_id)
			FROM star_transactions
			WHERE user_id = ? AND created_at >= ? AND amount > 0
		`, user.ID, weekStartStr).Scan(&weeklyStars, &weeklyStarredWorkouts); err != nil {
			log.Printf("stars: weekly stats user %d: %v", user.ID, err)
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"transactions":            txns,
			"weekly_stars":            weeklyStars,
			"weekly_starred_workouts": weeklyStarredWorkouts,
		})
	}
}

// sendRewardClaimedPush notifies a child's parent when a reward is claimed.
// Errors are logged and not propagated.
func sendRewardClaimedPush(db *sql.DB, childID int64, rewardTitle string) {
	link, err := family.GetParent(db, childID)
	if err != nil || link == nil {
		return
	}
	nickname := link.Nickname
	if nickname == "" {
		nickname = "Your child"
	}
	payload := push.Notification{
		Title: "Reward Claimed",
		Body:  fmt.Sprintf("%s wants to redeem: %s", nickname, rewardTitle),
		Tag:   "reward-claim",
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		log.Printf("stars: marshal reward claim push: %v", err)
		return
	}
	if _, err := push.SendToUser(db, pushClient, link.ParentID, payloadBytes); err != nil {
		log.Printf("stars: send reward claim push to parent %d: %v", link.ParentID, err)
	}
}

// kidRewardView is the API shape for a reward shown to a child.
type kidRewardView struct {
	family.Reward
	CanAfford    bool `json:"can_afford"`
	TimesClaimed int  `json:"times_claimed"`
}

// KidRewardsHandler returns active rewards available for the authenticated child to claim.
// Includes can_afford (based on balance) and times_claimed (non-denied claims by this child).
// GET /api/stars/rewards
func KidRewardsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		// Resolve the child's parent.
		link, err := family.GetParent(db, user.ID)
		if err != nil {
			log.Printf("stars: get parent for rewards user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load family link"})
			return
		}
		if link == nil {
			writeJSON(w, http.StatusOK, map[string]any{"rewards": []any{}})
			return
		}

		// Fetch active rewards from parent.
		rewards, err := family.GetActiveRewards(db, link.ParentID)
		if err != nil {
			log.Printf("stars: get active rewards user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load rewards"})
			return
		}

		// Load the child's current balance.
		var balance int
		if err := db.QueryRowContext(r.Context(), `
			SELECT COALESCE(current_balance, 0) FROM star_balances WHERE user_id = ?
		`, user.ID).Scan(&balance); err != nil && err != sql.ErrNoRows {
			log.Printf("stars: balance for rewards user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load balance"})
			return
		}

		// Load per-reward claim counts for this child (pending + approved only).
		claimRows, err := db.QueryContext(r.Context(), `
			SELECT reward_id, COUNT(*) FROM reward_claims
			WHERE child_id = ? AND status != 'denied'
			GROUP BY reward_id
		`, user.ID)
		if err != nil {
			log.Printf("stars: claim counts user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load claim counts"})
			return
		}
		claimCounts := make(map[int64]int)
		for claimRows.Next() {
			var rid int64
			var cnt int
			if err := claimRows.Scan(&rid, &cnt); err != nil {
				claimRows.Close()
				log.Printf("stars: claim count scan user %d: %v", user.ID, err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to scan claim counts"})
				return
			}
			claimCounts[rid] = cnt
		}
		claimRows.Close()
		if err := claimRows.Err(); err != nil {
			log.Printf("stars: claim count rows user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read claim counts"})
			return
		}

		views := make([]kidRewardView, 0, len(rewards))
		for _, rw := range rewards {
			views = append(views, kidRewardView{
				Reward:       rw,
				CanAfford:    balance >= rw.StarCost,
				TimesClaimed: claimCounts[rw.ID],
			})
		}

		writeJSON(w, http.StatusOK, map[string]any{"rewards": views})
	}
}

// ClaimRewardHandler creates a pending claim for a reward, deducting stars immediately.
// POST /api/stars/rewards/{id}/claim
func ClaimRewardHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		rewardID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid reward ID"})
			return
		}

		claim, err := family.ClaimReward(db, user.ID, rewardID)
		if err != nil {
			switch {
			case errors.Is(err, family.ErrRewardNotFound):
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "reward not found"})
			case errors.Is(err, family.ErrRewardNotActive):
				writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "reward is not available"})
			case errors.Is(err, family.ErrInsufficientStars):
				writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "insufficient stars"})
			case errors.Is(err, family.ErrMaxClaimsReached):
				writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "reward claim limit reached"})
			default:
				log.Printf("stars: claim reward %d user %d: %v", rewardID, user.ID, err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to claim reward"})
			}
			return
		}

		// Notify parent asynchronously.
		go func() {
			title, titleErr := family.GetRewardTitleByID(db, rewardID)
			if titleErr != nil {
				log.Printf("stars: get reward title for push notification reward %d: %v", rewardID, titleErr)
				return
			}
			sendRewardClaimedPush(db, user.ID, title)
		}()

		writeJSON(w, http.StatusCreated, map[string]any{"claim": claim})
	}
}

// KidClaimsHandler returns all reward claims for the authenticated child.
// GET /api/stars/claims
func KidClaimsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		claims, err := family.GetClaimsByUser(db, user.ID)
		if err != nil {
			log.Printf("stars: kid claims user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load claims"})
			return
		}
		if claims == nil {
			claims = []family.KidClaimView{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"claims": claims})
	}
}
