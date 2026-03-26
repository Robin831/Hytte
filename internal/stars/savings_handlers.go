package stars

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/Robin831/Hytte/internal/auth"
)

// savingsErrMsg returns a safe client-facing message for savings errors.
// Errors wrapping an internal cause (SQL, I/O) get a generic message;
// domain errors (insufficient balance, pending withdrawal, etc.) are passed through.
func savingsErrMsg(err error) string {
	if errors.Unwrap(err) != nil {
		return "internal error"
	}
	return err.Error()
}

// GetSavingsHandler handles GET /api/stars/savings.
// Returns the authenticated user's savings account state.
func GetSavingsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		acc, err := GetSavingsAccount(r.Context(), db, user.ID)
		if err != nil {
			log.Printf("stars: savings get user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load savings"})
			return
		}
		writeJSON(w, http.StatusOK, acc)
	}
}

// DepositSavingsHandler handles POST /api/stars/savings/deposit.
// Moves the requested amount of stars from the user's main balance into savings.
func DepositSavingsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		var req struct {
			Amount int `json:"amount"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
			return
		}
		if req.Amount <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "amount must be positive"})
			return
		}

		acc, err := Deposit(r.Context(), db, user.ID, req.Amount)
		if err != nil {
			log.Printf("stars: savings deposit user %d amount %d: %v", user.ID, req.Amount, err)
			status := http.StatusBadRequest
			if errors.Unwrap(err) != nil {
				status = http.StatusInternalServerError
			}
			writeJSON(w, status, map[string]string{"error": savingsErrMsg(err)})
			return
		}
		writeJSON(w, http.StatusOK, acc)
	}
}

// WithdrawSavingsHandler handles POST /api/stars/savings/withdraw.
// If no withdrawal is pending, it accepts {"amount": N} and starts a 24h countdown.
// If a withdrawal is already pending and the 24h delay has passed, it completes it
// (no body needed).
func WithdrawSavingsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		acc, err := GetSavingsAccount(r.Context(), db, user.ID)
		if err != nil {
			log.Printf("stars: savings withdraw get user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load savings"})
			return
		}

		if acc.PendingWithdrawal > 0 {
			// Complete the pending withdrawal.
			updated, err := CompleteWithdrawal(r.Context(), db, user.ID)
			if err != nil {
				log.Printf("stars: savings complete withdrawal user %d: %v", user.ID, err)
				status := http.StatusBadRequest
				if errors.Unwrap(err) != nil {
					status = http.StatusInternalServerError
				}
				writeJSON(w, status, map[string]string{"error": savingsErrMsg(err)})
				return
			}
			writeJSON(w, http.StatusOK, updated)
			return
		}

		// Request a new withdrawal.
		var req struct {
			Amount int `json:"amount"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
			return
		}
		if req.Amount <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "amount must be positive"})
			return
		}

		updated, err := RequestWithdrawal(r.Context(), db, user.ID, req.Amount)
		if err != nil {
			log.Printf("stars: savings request withdrawal user %d amount %d: %v", user.ID, req.Amount, err)
			status := http.StatusBadRequest
			if errors.Unwrap(err) != nil {
				status = http.StatusInternalServerError
			}
			writeJSON(w, status, map[string]string{"error": savingsErrMsg(err)})
			return
		}
		writeJSON(w, http.StatusOK, updated)
	}
}
