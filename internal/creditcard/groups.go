package creditcard

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/encryption"
	"github.com/go-chi/chi/v5"
)

// Group represents a credit card transaction group.
type Group struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	SortOrder int    `json:"sort_order"`
}

// MerchantRule represents a rule mapping a merchant pattern to a group.
type MerchantRule struct {
	ID              int64  `json:"id"`
	MerchantPattern string `json:"merchant_pattern"`
	GroupID         int64  `json:"group_id"`
}

// GroupsListHandler returns all groups for the authenticated user ordered by sort_order.
func GroupsListHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		rows, err := db.Query(
			`SELECT id, name, sort_order FROM credit_card_groups WHERE user_id = ? ORDER BY sort_order, id`,
			user.ID,
		)
		if err != nil {
			log.Printf("creditcard: groups list: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list groups"})
			return
		}
		defer rows.Close() //nolint:errcheck

		groups := []Group{}
		for rows.Next() {
			var g Group
			if err := rows.Scan(&g.ID, &g.Name, &g.SortOrder); err != nil {
				log.Printf("creditcard: groups list scan: %v", err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to scan group"})
				return
			}
			groups = append(groups, g)
		}
		writeJSON(w, http.StatusOK, groups)
	}
}

// GroupsCreateHandler creates a new group for the authenticated user.
func GroupsCreateHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		var req struct {
			Name      string `json:"name"`
			SortOrder int    `json:"sort_order"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		req.Name = strings.TrimSpace(req.Name)
		if req.Name == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
			return
		}

		res, err := db.Exec(
			`INSERT INTO credit_card_groups (user_id, name, sort_order) VALUES (?, ?, ?)`,
			user.ID, req.Name, req.SortOrder,
		)
		if err != nil {
			log.Printf("creditcard: groups create: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create group"})
			return
		}
		id, _ := res.LastInsertId()
		writeJSON(w, http.StatusCreated, Group{ID: id, Name: req.Name, SortOrder: req.SortOrder})
	}
}

// GroupsUpdateHandler renames a group belonging to the authenticated user.
func GroupsUpdateHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid group id"})
			return
		}

		var req struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		req.Name = strings.TrimSpace(req.Name)
		if req.Name == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
			return
		}

		res, err := db.Exec(
			`UPDATE credit_card_groups SET name = ? WHERE id = ? AND user_id = ?`,
			req.Name, id, user.ID,
		)
		if err != nil {
			log.Printf("creditcard: groups update: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update group"})
			return
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "group not found"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

// GroupsReorderHandler updates sort_order for multiple groups at once.
// Request body: array of {id, sort_order} objects.
func GroupsReorderHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		var req []struct {
			ID        int64 `json:"id"`
			SortOrder int   `json:"sort_order"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}

		tx, err := db.Begin()
		if err != nil {
			log.Printf("creditcard: groups reorder begin tx: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to start transaction"})
			return
		}
		defer tx.Rollback() //nolint:errcheck

		for _, item := range req {
			if _, err := tx.Exec(
				`UPDATE credit_card_groups SET sort_order = ? WHERE id = ? AND user_id = ?`,
				item.SortOrder, item.ID, user.ID,
			); err != nil {
				log.Printf("creditcard: groups reorder update group %d: %v", item.ID, err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to reorder groups"})
				return
			}
		}

		if err := tx.Commit(); err != nil {
			log.Printf("creditcard: groups reorder commit: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to commit reorder"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

// GroupsDeleteHandler deletes a group belonging to the authenticated user.
// Transactions in this group will have their group_id set to NULL (ON DELETE SET NULL).
func GroupsDeleteHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid group id"})
			return
		}

		res, err := db.Exec(
			`DELETE FROM credit_card_groups WHERE id = ? AND user_id = ?`,
			id, user.ID,
		)
		if err != nil {
			log.Printf("creditcard: groups delete: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete group"})
			return
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "group not found"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

// RulesListHandler returns all merchant group rules for the authenticated user.
func RulesListHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		rows, err := db.Query(
			`SELECT id, merchant_pattern, group_id FROM merchant_group_rules WHERE user_id = ? ORDER BY id`,
			user.ID,
		)
		if err != nil {
			log.Printf("creditcard: rules list: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list rules"})
			return
		}
		defer rows.Close() //nolint:errcheck

		rules := []MerchantRule{}
		for rows.Next() {
			var mr MerchantRule
			if err := rows.Scan(&mr.ID, &mr.MerchantPattern, &mr.GroupID); err != nil {
				log.Printf("creditcard: rules list scan: %v", err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to scan rule"})
				return
			}
			rules = append(rules, mr)
		}
		writeJSON(w, http.StatusOK, rules)
	}
}

// RulesCreateHandler creates a new merchant group rule for the authenticated user.
func RulesCreateHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		var req struct {
			MerchantPattern string `json:"merchant_pattern"`
			GroupID         int64  `json:"group_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		req.MerchantPattern = strings.TrimSpace(req.MerchantPattern)
		if req.MerchantPattern == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "merchant_pattern is required"})
			return
		}
		if req.GroupID == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "group_id is required"})
			return
		}

		// Verify the group belongs to the user before creating the rule.
		var count int
		if err := db.QueryRow(
			`SELECT COUNT(*) FROM credit_card_groups WHERE id = ? AND user_id = ?`,
			req.GroupID, user.ID,
		).Scan(&count); err != nil || count == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "group not found"})
			return
		}

		res, err := db.Exec(
			`INSERT INTO merchant_group_rules (user_id, merchant_pattern, group_id) VALUES (?, ?, ?)`,
			user.ID, req.MerchantPattern, req.GroupID,
		)
		if err != nil {
			log.Printf("creditcard: rules create: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create rule"})
			return
		}
		id, _ := res.LastInsertId()
		writeJSON(w, http.StatusCreated, MerchantRule{
			ID:              id,
			MerchantPattern: req.MerchantPattern,
			GroupID:         req.GroupID,
		})
	}
}

// RulesDeleteHandler deletes a merchant group rule belonging to the authenticated user.
func RulesDeleteHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid rule id"})
			return
		}

		res, err := db.Exec(
			`DELETE FROM merchant_group_rules WHERE id = ? AND user_id = ?`,
			id, user.ID,
		)
		if err != nil {
			log.Printf("creditcard: rules delete: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete rule"})
			return
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "rule not found"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

// TransactionsBulkAssignHandler assigns a list of transactions to a group (or unassigns them).
// Request body: {"transaction_ids": [1,2,3], "group_id": 5} — set group_id to null/0 to unassign.
func TransactionsBulkAssignHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		var req struct {
			TransactionIDs []int64 `json:"transaction_ids"`
			GroupID        *int64  `json:"group_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		if len(req.TransactionIDs) == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "transaction_ids is required"})
			return
		}

		// Verify the target group belongs to the user (when assigning, not unassigning).
		if req.GroupID != nil && *req.GroupID != 0 {
			var count int
			if err := db.QueryRow(
				`SELECT COUNT(*) FROM credit_card_groups WHERE id = ? AND user_id = ?`,
				*req.GroupID, user.ID,
			).Scan(&count); err != nil || count == 0 {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "group not found"})
				return
			}
		}

		tx, err := db.Begin()
		if err != nil {
			log.Printf("creditcard: bulk assign begin tx: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to start transaction"})
			return
		}
		defer tx.Rollback() //nolint:errcheck

		var groupIDVal any
		if req.GroupID != nil && *req.GroupID != 0 {
			groupIDVal = *req.GroupID
		}

		updated := 0
		for _, txID := range req.TransactionIDs {
			res, err := tx.Exec(
				`UPDATE credit_card_transactions SET group_id = ? WHERE id = ? AND user_id = ?`,
				groupIDVal, txID, user.ID,
			)
			if err != nil {
				log.Printf("creditcard: bulk assign update tx %d: %v", txID, err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update transaction"})
				return
			}
			n, _ := res.RowsAffected()
			updated += int(n)
		}

		if err := tx.Commit(); err != nil {
			log.Printf("creditcard: bulk assign commit: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to commit bulk assign"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"updated": updated})
	}
}

// RecurringMerchantsHandler returns merchant descriptions (beskrivelse) that appear in
// transactions across 2 or more distinct calendar months, suggesting them as grouping candidates.
func RecurringMerchantsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		// Fetch all non-payment transactions: encrypted beskrivelse + month prefix.
		rows, err := db.Query(
			`SELECT beskrivelse, substr(transaksjonsdato, 1, 7) AS month
			 FROM credit_card_transactions
			 WHERE user_id = ? AND is_innbetaling = 0 AND is_pending = 0`,
			user.ID,
		)
		if err != nil {
			log.Printf("creditcard: recurring merchants query: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to query transactions"})
			return
		}
		defer rows.Close() //nolint:errcheck

		// Group by decrypted description, tracking which distinct months it appears in.
		merchantMonths := make(map[string]map[string]struct{})

		for rows.Next() {
			var encDesc, month string
			if err := rows.Scan(&encDesc, &month); err != nil {
				log.Printf("creditcard: recurring merchants scan: %v", err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to scan transaction"})
				return
			}

			desc, err := encryption.DecryptField(encDesc)
			if err != nil {
				// Legacy plaintext — use as-is with a warning.
				log.Printf("creditcard: recurring merchants decrypt: %v", err)
				desc = encDesc
			}

			if _, ok := merchantMonths[desc]; !ok {
				merchantMonths[desc] = make(map[string]struct{})
			}
			merchantMonths[desc][month] = struct{}{}
		}

		type Suggestion struct {
			Beskrivelse string   `json:"beskrivelse"`
			MonthCount  int      `json:"month_count"`
			Months      []string `json:"months"`
		}

		suggestions := []Suggestion{}
		for desc, months := range merchantMonths {
			if len(months) < 2 {
				continue
			}
			ms := make([]string, 0, len(months))
			for m := range months {
				ms = append(ms, m)
			}
			sort.Strings(ms)
			suggestions = append(suggestions, Suggestion{
				Beskrivelse: desc,
				MonthCount:  len(months),
				Months:      ms,
			})
		}

		// Sort by month count descending, then alphabetically by description.
		sort.Slice(suggestions, func(i, j int) bool {
			if suggestions[i].MonthCount != suggestions[j].MonthCount {
				return suggestions[i].MonthCount > suggestions[j].MonthCount
			}
			return suggestions[i].Beskrivelse < suggestions[j].Beskrivelse
		})

		writeJSON(w, http.StatusOK, suggestions)
	}
}

// EnsureDefaultGroup seeds a 'Diverse' group for the user if they have no groups yet.
// Returns the ID of the newly created group, or 0 if the user already had groups.
func EnsureDefaultGroup(db *sql.DB, userID int64) (int64, error) {
	var count int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM credit_card_groups WHERE user_id = ?`,
		userID,
	).Scan(&count); err != nil {
		return 0, err
	}
	if count > 0 {
		return 0, nil
	}

	res, err := db.Exec(
		`INSERT INTO credit_card_groups (user_id, name, sort_order) VALUES (?, 'Diverse', 0)`,
		userID,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}
