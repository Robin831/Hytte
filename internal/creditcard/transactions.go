package creditcard

import (
	"database/sql"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/encryption"
)

// TransactionRow is a single credit card transaction returned by the list endpoint.
type TransactionRow struct {
	ID               int64   `json:"id"`
	Transaksjonsdato string  `json:"transaksjonsdato"`
	Beskrivelse      string  `json:"beskrivelse"`
	Belop            float64 `json:"belop"`
	BelopIValuta     float64 `json:"belop_i_valuta"`
	IsPending        bool    `json:"is_pending"`
	IsInnbetaling    bool    `json:"is_innbetaling"`
	GroupID          *int64  `json:"group_id"`
	GroupName        string  `json:"group_name"`
}

// TransactionsListResponse is returned by TransactionsListHandler.
type TransactionsListResponse struct {
	Transactions       []TransactionRow `json:"transactions"`
	VariableBillName   string           `json:"variable_bill_name"`
	VariableBillAmount float64          `json:"variable_bill_amount"`
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
			       t.is_pending, t.is_innbetaling, t.group_id, COALESCE(g.name, '') AS group_name
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
			var isPending, isInnbetaling int
			var groupID sql.NullInt64
			var groupName string
			if err := rows.Scan(&t.ID, &t.Transaksjonsdato, &encDesc, &t.Belop, &t.BelopIValuta,
				&isPending, &isInnbetaling, &groupID, &groupName); err != nil {
				log.Printf("creditcard: transactions list scan: %v", err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to scan transaction"})
				return
			}

			desc, err := encryption.DecryptField(encDesc)
			if err != nil {
				log.Printf("creditcard: transactions list decrypt beskrivelse: %v (using raw value)", err)
				desc = encDesc
			}

			t.Beskrivelse = desc
			t.IsPending = isPending == 1
			t.IsInnbetaling = isInnbetaling == 1
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

		// Variable bill sync status — non-fatal if not linked.
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
		`, month, user.ID, creditCardID).Scan(&billName, &billAmount)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			log.Printf("creditcard: transactions list variable bill lookup: %v", err)
		}

		writeJSON(w, http.StatusOK, TransactionsListResponse{
			Transactions:       txns,
			VariableBillName:   billName,
			VariableBillAmount: billAmount,
		})
	}
}
