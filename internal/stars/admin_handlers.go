package stars

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"
)

// adminAwardRequest is the request body for POST /api/admin/stars/award.
type adminAwardRequest struct {
	UserID      int64  `json:"user_id"`
	Amount      int    `json:"amount"`
	Reason      string `json:"reason"`
	Description string `json:"description"`
}

// AdminAwardStarsHandler handles POST /api/admin/stars/award.
// Admin-only. Inserts a star transaction and updates the star balance for the
// specified user. Supports positive awards and negative adjustments.
func AdminAwardStarsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req adminAwardRequest
		// Limit request body size to prevent excessive memory/CPU usage.
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MiB
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}

		if req.UserID <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "user_id is required"})
			return
		}
		if req.Amount == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "amount must be non-zero"})
			return
		}
		req.Reason = strings.TrimSpace(req.Reason)
		req.Description = strings.TrimSpace(req.Description)
		if req.Reason == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "reason is required"})
			return
		}

		// Verify the target user exists before starting a transaction.
		var exists bool
		if err := db.QueryRowContext(r.Context(), `SELECT 1 FROM users WHERE id = ?`, req.UserID).Scan(&exists); err != nil {
			if err == sql.ErrNoRows {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
			} else {
				log.Printf("stars: admin award check user %d: %v", req.UserID, err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to verify user"})
			}
			return
		}

		now := time.Now().UTC().Format(time.RFC3339)

		tx, err := db.BeginTx(r.Context(), nil)
		if err != nil {
			log.Printf("stars: admin award begin tx user %d: %v", req.UserID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to start transaction"})
			return
		}
		defer tx.Rollback() //nolint:errcheck

		// Insert the transaction record.
		_, err = tx.ExecContext(r.Context(), `
			INSERT INTO star_transactions (user_id, amount, reason, description, reference_id, created_at)
			VALUES (?, ?, ?, ?, NULL, ?)
		`, req.UserID, req.Amount, req.Reason, req.Description, now)
		if err != nil {
			log.Printf("stars: admin award insert tx user %d: %v", req.UserID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to insert transaction"})
			return
		}

		// Update the star balance. Positive amounts increase total_earned;
		// negative amounts increase total_spent (stored as absolute value).
		if req.Amount > 0 {
			_, err = tx.ExecContext(r.Context(), `
				INSERT INTO star_balances (user_id, total_earned)
				VALUES (?, ?)
				ON CONFLICT(user_id) DO UPDATE SET total_earned = total_earned + excluded.total_earned
			`, req.UserID, req.Amount)
		} else {
			_, err = tx.ExecContext(r.Context(), `
				INSERT INTO star_balances (user_id, total_spent)
				VALUES (?, ?)
				ON CONFLICT(user_id) DO UPDATE SET total_spent = total_spent + excluded.total_spent
			`, req.UserID, -req.Amount)
		}
		if err != nil {
			log.Printf("stars: admin award update balance user %d: %v", req.UserID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update balance"})
			return
		}

		if err := tx.Commit(); err != nil {
			log.Printf("stars: admin award commit user %d: %v", req.UserID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to commit transaction"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}
