package stars

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
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
