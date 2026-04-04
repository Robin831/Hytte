package creditcard

import (
	"database/sql"
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/encryption"
	"github.com/go-chi/chi/v5"
)

// TransactionRow is a single credit card transaction returned by the list endpoint.
type TransactionRow struct {
	ID                   int64   `json:"id"`
	Transaksjonsdato     string  `json:"transaksjonsdato"`
	Beskrivelse          string  `json:"beskrivelse"`
	Belop                float64 `json:"belop"`
	BelopIValuta         float64 `json:"belop_i_valuta"`
	IsPending            bool    `json:"is_pending"`
	IsInnbetaling        bool    `json:"is_innbetaling"`
	DeferredToNextMonth  bool    `json:"deferred_to_next_month"`
	GroupID              *int64  `json:"group_id"`
	GroupName            string  `json:"group_name"`
}

// TransactionsListResponse is returned by TransactionsListHandler.
type TransactionsListResponse struct {
	Transactions       []TransactionRow `json:"transactions"`
	VariableBillName   string           `json:"variable_bill_name"`
	VariableBillAmount float64          `json:"variable_bill_amount"`
	OpeningBalance     float64          `json:"opening_balance"`
}

// TransactionsListHandler returns all credit card transactions for a given card
// and billing month, with group info and variable bill sync status.
//
// Query params:
//   - credit_card_id: the card identifier used during import (required)
//   - month:          billing month in YYYY-MM format (required)
func TransactionsListHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		creditCardID := r.URL.Query().Get("credit_card_id")
		month := r.URL.Query().Get("month")

		if creditCardID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "credit_card_id is required"})
			return
		}
		if len(month) != 7 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "month must be YYYY-MM"})
			return
		}

		periodStart, err := time.Parse("2006-01", month)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid month format"})
			return
		}
		startStr := periodStart.Format("2006-01-02")
		endStr := periodStart.AddDate(0, 1, 0).Format("2006-01-02")

		rows, err := db.Query(`
			SELECT t.id, t.transaksjonsdato, t.beskrivelse, t.belop, t.belop_i_valuta,
			       t.is_pending, t.is_innbetaling, t.deferred_to_next_month,
			       t.group_id, COALESCE(g.name, '') AS group_name
			FROM credit_card_transactions t
			LEFT JOIN credit_card_groups g ON g.id = t.group_id AND g.user_id = t.user_id
			WHERE t.user_id = ? AND t.credit_card_id = ?
			  AND t.transaksjonsdato >= ? AND t.transaksjonsdato < ?
			ORDER BY t.transaksjonsdato DESC, t.id DESC
		`, user.ID, creditCardID, startStr, endStr)
		if err != nil {
			log.Printf("creditcard: transactions list query: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list transactions"})
			return
		}
		defer rows.Close() //nolint:errcheck

		txns := []TransactionRow{}
		for rows.Next() {
			var t TransactionRow
			var encDesc string
			var isPending, isInnbetaling, deferredToNextMonth int
			var groupID sql.NullInt64
			var groupName string
			if err := rows.Scan(&t.ID, &t.Transaksjonsdato, &encDesc, &t.Belop, &t.BelopIValuta,
				&isPending, &isInnbetaling, &deferredToNextMonth, &groupID, &groupName); err != nil {
				log.Printf("creditcard: transactions list scan: %v", err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to scan transaction"})
				return
			}

			desc, err := encryption.DecryptField(encDesc)
			if err != nil {
				log.Printf("creditcard: transactions list decrypt beskrivelse: %v", err)
				desc = ""
			}

			t.Beskrivelse = desc
			t.IsPending = isPending == 1
			t.IsInnbetaling = isInnbetaling == 1
			t.DeferredToNextMonth = deferredToNextMonth == 1
			if groupID.Valid {
				gid := groupID.Int64
				t.GroupID = &gid
				t.GroupName = groupName
			}
			txns = append(txns, t)
		}
		if err := rows.Err(); err != nil {
			log.Printf("creditcard: transactions list rows err: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to iterate transactions"})
			return
		}

		// Variable bill sync status — the entry is stored in the next month
		// (March expenses → April payment), so query month+1.
		paymentMonth := periodStart.AddDate(0, 1, 0).Format("2006-01")
		var billName string
		var billAmount float64
		err = db.QueryRow(`
			SELECT vb.name,
			       COALESCE((
			           SELECT SUM(ve.amount)
			           FROM budget_variable_entries ve
			           WHERE ve.variable_id = vb.id AND ve.month = ?
			       ), 0)
			FROM budget_variable_bills vb
			WHERE vb.user_id = ? AND vb.credit_card_id = ?
			LIMIT 1
		`, paymentMonth, user.ID, creditCardID).Scan(&billName, &billAmount)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			log.Printf("creditcard: transactions list variable bill lookup: %v", err)
		}

		if billName != "" {
			decrypted, decErr := encryption.DecryptField(billName)
			if decErr != nil {
				log.Printf("creditcard: transactions list decrypt bill name: %v", decErr)
				billName = ""
			} else {
				billName = decrypted
			}
		}

		// Opening balance for this billing period.
		var openingBalance float64
		if err := db.QueryRow(
			`SELECT balance FROM credit_card_opening_balances WHERE user_id = ? AND credit_card_id = ? AND month = ?`,
			user.ID, creditCardID, month,
		).Scan(&openingBalance); err != nil && !errors.Is(err, sql.ErrNoRows) {
			log.Printf("creditcard: transactions list opening balance lookup: %v", err)
		}

		writeJSON(w, http.StatusOK, TransactionsListResponse{
			Transactions:       txns,
			VariableBillName:   billName,
			VariableBillAmount: billAmount,
			OpeningBalance:     openingBalance,
		})
	}
}

// SyncVariableBillHandler triggers a resync of the linked variable bill
// for a given credit card and billing month.
func SyncVariableBillHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		creditCardID := r.URL.Query().Get("credit_card_id")
		month := r.URL.Query().Get("month")
		if creditCardID == "" || len(month) != 7 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "credit_card_id and month (YYYY-MM) are required"})
			return
		}

		if err := SyncCreditCardExpense(db, user.ID, creditCardID, month); err != nil {
			log.Printf("creditcard: manual sync variable bill: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "sync failed"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

// TransactionDeleteHandler deletes a single credit card transaction by ID.
func TransactionDeleteHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid transaction id"})
			return
		}

		res, err := db.Exec(
			`DELETE FROM credit_card_transactions WHERE id = ? AND user_id = ?`,
			id, user.ID,
		)
		if err != nil {
			log.Printf("creditcard: transaction delete: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete transaction"})
			return
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "transaction not found"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

// TransactionDeferHandler toggles the deferred_to_next_month flag on a single
// credit card transaction. When deferred, the transaction is excluded from the
// billing period it was dated in and carried over into the following period.
//
// After toggling, both the transaction's own billing period and the next period
// are resynced so that variable bill entries reflect the change immediately.
func TransactionDeferHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid transaction id"})
			return
		}

		// Fetch the transaction to verify ownership and get the date + card ID
		// needed for the resync.
		var creditCardID, transaksjonsdato string
		var currentDeferred int
		err = db.QueryRow(
			`SELECT credit_card_id, transaksjonsdato, deferred_to_next_month FROM credit_card_transactions WHERE id = ? AND user_id = ?`,
			id, user.ID,
		).Scan(&creditCardID, &transaksjonsdato, &currentDeferred)
		if errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "transaction not found"})
			return
		}
		if err != nil {
			log.Printf("creditcard: defer fetch transaction: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to fetch transaction"})
			return
		}

		newDeferred := 1
		if currentDeferred == 1 {
			newDeferred = 0
		}

		if _, err := db.Exec(
			`UPDATE credit_card_transactions SET deferred_to_next_month = ? WHERE id = ? AND user_id = ?`,
			newDeferred, id, user.ID,
		); err != nil {
			log.Printf("creditcard: defer update: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update transaction"})
			return
		}

		// Determine the billing period from the transaction date (YYYY-MM-DD → YYYY-MM).
		period := ""
		if len(transaksjonsdato) >= 7 {
			period = transaksjonsdato[:7]
		}

		// Resync this period and the next period:
		// - This period no longer includes (or now includes) the toggled transaction.
		// - The next period may now carry over (or drop) the deferred transaction.
		if period != "" {
			if err := SyncCreditCardExpense(db, user.ID, creditCardID, period); err != nil {
				log.Printf("creditcard: defer resync current period %s: %v", period, err)
			}
			// Compute next period (YYYY-MM + 1 month).
			if t, parseErr := time.Parse("2006-01", period); parseErr == nil {
				nextPeriod := t.AddDate(0, 1, 0).Format("2006-01")
				if err := SyncCreditCardExpense(db, user.ID, creditCardID, nextPeriod); err != nil {
					log.Printf("creditcard: defer resync next period %s: %v", nextPeriod, err)
				}
			}
		}

		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "deferred_to_next_month": newDeferred == 1})
	}
}
