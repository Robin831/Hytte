package creditcard

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
)

type openingBalanceResponse struct {
	Balance float64 `json:"balance"`
}

// OpeningBalanceGetHandler returns the opening balance for a given credit card
// and billing month. Returns {"balance": 0} if no opening balance has been set.
//
// Query params:
//   - credit_card_id: the card identifier (required)
//   - month:          billing month in YYYY-MM format (required)
func OpeningBalanceGetHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		creditCardID := r.URL.Query().Get("credit_card_id")
		month := r.URL.Query().Get("month")
		if creditCardID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "credit_card_id and month (YYYY-MM) are required"})
			return
		}
		if _, err := time.Parse("2006-01", month); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "credit_card_id and month (YYYY-MM) are required"})
			return
		}

		var balance float64
		err := db.QueryRow(
			`SELECT balance FROM credit_card_opening_balances WHERE user_id = ? AND credit_card_id = ? AND month = ?`,
			user.ID, creditCardID, month,
		).Scan(&balance)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			log.Printf("creditcard: opening balance get: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load opening balance"})
			return
		}

		writeJSON(w, http.StatusOK, openingBalanceResponse{Balance: balance})
	}
}

// OpeningBalancePutHandler sets or updates the opening balance for a given
// credit card and billing month. After saving, it attempts a best-effort resync
// of the linked variable bill; sync failures are logged but do not affect the
// 200 OK response, since the balance itself was saved successfully.
//
// Query params:
//   - credit_card_id: the card identifier (required)
//   - month:          billing month in YYYY-MM format (required)
//
// Body (JSON): {"balance": 1234.56}
func OpeningBalancePutHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		creditCardID := r.URL.Query().Get("credit_card_id")
		month := r.URL.Query().Get("month")
		if creditCardID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "credit_card_id and month (YYYY-MM) are required"})
			return
		}
		if _, err := time.Parse("2006-01", month); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "credit_card_id and month (YYYY-MM) are required"})
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, 1024)
		var body struct {
			Balance float64 `json:"balance"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
			return
		}

		_, err := db.Exec(
			`INSERT INTO credit_card_opening_balances (user_id, credit_card_id, month, balance)
			 VALUES (?, ?, ?, ?)
			 ON CONFLICT(user_id, credit_card_id, month) DO UPDATE SET balance = excluded.balance`,
			user.ID, creditCardID, month, body.Balance,
		)
		if err != nil {
			log.Printf("creditcard: opening balance put: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save opening balance"})
			return
		}

		// Resync the variable bill so it immediately reflects the new opening balance.
		if err := SyncCreditCardExpense(db, user.ID, creditCardID, month); err != nil {
			log.Printf("creditcard: opening balance put sync: %v", err)
			// Non-fatal — balance was saved, sync can be retried manually.
		}

		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}
