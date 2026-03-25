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

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("stars: writeJSON encode error: %v", err)
	}
}

// BalanceResponse is the API response for GET /api/stars/balance.
type BalanceResponse struct {
	CurrentBalance int    `json:"current_balance"`
	TotalEarned    int    `json:"total_earned"`
	TotalSpent     int    `json:"total_spent"`
	Level          int    `json:"level"`
	XP             int    `json:"xp"`
	Title          string `json:"title"`
}

// BalanceHandler handles GET /api/stars/balance.
func BalanceHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		var resp BalanceResponse
		err := db.QueryRow(`
			SELECT COALESCE(total_earned, 0), COALESCE(total_spent, 0), COALESCE(current_balance, 0)
			FROM star_balances
			WHERE user_id = ?
		`, user.ID).Scan(&resp.TotalEarned, &resp.TotalSpent, &resp.CurrentBalance)
		if err == sql.ErrNoRows {
			resp = BalanceResponse{Level: 1, Title: "Rookie Runner"}
			writeJSON(w, http.StatusOK, resp)
			return
		}
		if err != nil {
			log.Printf("stars: balance query user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load balance"})
			return
		}

		// Level data (optional row — defaults if missing).
		resp.Level = 1
		resp.Title = "Rookie Runner"
		err = db.QueryRow(`
			SELECT xp, level, title FROM user_levels WHERE user_id = ?
		`, user.ID).Scan(&resp.XP, &resp.Level, &resp.Title)
		if err != nil && err != sql.ErrNoRows {
			log.Printf("stars: level query user %d: %v", user.ID, err)
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

		rows, err := db.Query(`
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
				continue
			}
			txns = append(txns, tx)
		}
		if err := rows.Err(); err != nil {
			log.Printf("stars: transactions rows error user %d: %v", user.ID, err)
		}

		if txns == nil {
			txns = []Transaction{}
		}

		// Weekly stats: stars earned and workout count in the last 7 days.
		weekAgo := time.Now().UTC().Add(-7 * 24 * time.Hour).Format(time.RFC3339)
		var weeklyStars int
		var weeklyWorkouts int
		if err := db.QueryRow(`
			SELECT COALESCE(SUM(amount), 0), COUNT(DISTINCT reference_id)
			FROM star_transactions
			WHERE user_id = ? AND created_at >= ? AND amount > 0
		`, user.ID, weekAgo).Scan(&weeklyStars, &weeklyWorkouts); err != nil {
			log.Printf("stars: weekly stats user %d: %v", user.ID, err)
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"transactions":    txns,
			"weekly_stars":    weeklyStars,
			"weekly_workouts": weeklyWorkouts,
		})
	}
}
