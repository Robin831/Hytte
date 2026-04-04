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
		if err := rows.Err(); err != nil {
			log.Printf("creditcard: groups list rows: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list groups"})
			return
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
			// SQLite reports 0 rows affected on no-op UPDATEs (same value).
			// Check whether the group actually exists to distinguish 404 from no-op.
			var exists int
			if err := db.QueryRow(
				`SELECT COUNT(*) FROM credit_card_groups WHERE id = ? AND user_id = ?`,
				id, user.ID,
			).Scan(&exists); err != nil {
				log.Printf("creditcard: groups update existence check: %v", err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to check group"})
				return
			}
			if exists == 0 {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "group not found"})
				return
			}
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
			result, err := tx.Exec(
				`UPDATE credit_card_groups SET sort_order = ? WHERE id = ? AND user_id = ?`,
				item.SortOrder, item.ID, user.ID,
			)
			if err != nil {
				log.Printf("creditcard: groups reorder update group %d: %v", item.ID, err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to reorder groups"})
				return
			}

			rowsAffected, err := result.RowsAffected()
			if err != nil {
				log.Printf("creditcard: groups reorder rows affected for group %d: %v", item.ID, err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to reorder groups"})
				return
			}
			if rowsAffected != 1 {
				log.Printf("creditcard: groups reorder group %d not found for user %d", item.ID, user.ID)
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "group not found"})
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
		if err := rows.Err(); err != nil {
			log.Printf("creditcard: rules list rows: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list rules"})
			return
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
		).Scan(&count); err != nil {
			log.Printf("creditcard: rules create group lookup: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to verify group"})
			return
		}
		if count == 0 {
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
// Request body: {"transaction_ids": [1,2,3], "group_id": 5} — set group_id to null to unassign.
// The group_id field must always be present: omitting it is an error (prevents accidental unassigns).
func TransactionsBulkAssignHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		var req struct {
			TransactionIDs []int64         `json:"transaction_ids"`
			GroupIDRaw     json.RawMessage `json:"group_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		if len(req.TransactionIDs) == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "transaction_ids is required"})
			return
		}
		if len(req.GroupIDRaw) == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "group_id is required"})
			return
		}

		// Distinguish explicit null (unassign) from a numeric group ID.
		var groupIDVal any // nil sets group_id to SQL NULL
		if string(req.GroupIDRaw) != "null" {
			var gid int64
			if err := json.Unmarshal(req.GroupIDRaw, &gid); err != nil || gid == 0 {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid group_id"})
				return
			}
			groupIDVal = gid

			// Verify the target group belongs to the user.
			var count int
			if err := db.QueryRow(
				`SELECT COUNT(*) FROM credit_card_groups WHERE id = ? AND user_id = ?`,
				gid, user.ID,
			).Scan(&count); err != nil {
				log.Printf("creditcard: bulk assign verify group %d for user %d: %v", gid, user.ID, err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to verify group"})
				return
			}
			if count == 0 {
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

		// Batch updates in chunks of 900 to stay within SQLite's parameter limit.
		const bulkAssignChunkSize = 900
		updated := 0
		for start := 0; start < len(req.TransactionIDs); start += bulkAssignChunkSize {
			end := start + bulkAssignChunkSize
			if end > len(req.TransactionIDs) {
				end = len(req.TransactionIDs)
			}

			chunk := req.TransactionIDs[start:end]
			placeholders := make([]string, len(chunk))
			args := make([]any, 0, 2+len(chunk))
			args = append(args, groupIDVal, user.ID)
			for i, txID := range chunk {
				placeholders[i] = "?"
				args = append(args, txID)
			}

			query := `UPDATE credit_card_transactions SET group_id = ? WHERE user_id = ? AND id IN (` + strings.Join(placeholders, ",") + `)`
			res, err := tx.Exec(query, args...)
			if err != nil {
				log.Printf("creditcard: bulk assign update chunk starting at %d: %v", start, err)
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
		decryptFailures := 0

		for rows.Next() {
			var encDesc, month string
			if err := rows.Scan(&encDesc, &month); err != nil {
				log.Printf("creditcard: recurring merchants scan: %v", err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to scan transaction"})
				return
			}

			desc, err := encryption.DecryptField(encDesc)
			if err != nil {
				// Skip entries that cannot be decrypted to avoid leaking ciphertext
				// in the API response. Count failures for a summary log below.
				decryptFailures++
				continue
			}

			if _, ok := merchantMonths[desc]; !ok {
				merchantMonths[desc] = make(map[string]struct{})
			}
			merchantMonths[desc][month] = struct{}{}
		}
		if err := rows.Err(); err != nil {
			log.Printf("creditcard: recurring merchants rows: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to iterate transactions"})
			return
		}
		if decryptFailures > 0 {
			log.Printf("creditcard: recurring merchants: failed to decrypt %d entries", decryptFailures)
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

// ReapplyRulesHandler applies all merchant_group_rules to existing transactions that are
// currently ungrouped (group_id IS NULL) or assigned to the 'Diverse' group. Only
// transactions for the specified credit_card_id are processed.
// Returns {"updated": N} with the number of transactions that were reassigned.
func ReapplyRulesHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		var req struct {
			CreditCardID string `json:"credit_card_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		if req.CreditCardID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "credit_card_id is required"})
			return
		}

		rules, err := loadMerchantGroupRules(db, user.ID)
		if err != nil {
			log.Printf("creditcard: reapply rules load rules: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load rules"})
			return
		}
		if len(rules) == 0 {
			writeJSON(w, http.StatusOK, map[string]any{"updated": 0})
			return
		}

		// Find the 'Diverse' group ID so we include those transactions.
		var diverseGroupID sql.NullInt64
		if err := db.QueryRow(
			`SELECT id FROM credit_card_groups WHERE user_id = ? AND name = 'Diverse' ORDER BY sort_order, id LIMIT 1`,
			user.ID,
		).Scan(&diverseGroupID); err != nil && err != sql.ErrNoRows {
			log.Printf("creditcard: reapply rules find diverse group: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to query groups"})
			return
		}

		// Fetch candidate transactions: ungrouped or in Diverse.
		var txRows *sql.Rows
		if diverseGroupID.Valid {
			txRows, err = db.Query(
				`SELECT id, beskrivelse FROM credit_card_transactions
				 WHERE user_id = ? AND credit_card_id = ?
				   AND (group_id IS NULL OR group_id = ?)`,
				user.ID, req.CreditCardID, diverseGroupID.Int64,
			)
		} else {
			txRows, err = db.Query(
				`SELECT id, beskrivelse FROM credit_card_transactions
				 WHERE user_id = ? AND credit_card_id = ? AND group_id IS NULL`,
				user.ID, req.CreditCardID,
			)
		}
		if err != nil {
			log.Printf("creditcard: reapply rules query transactions: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to query transactions"})
			return
		}
		defer txRows.Close() //nolint:errcheck

		type assignment struct {
			txID    int64
			groupID int64
		}
		var assignments []assignment

		for txRows.Next() {
			var id int64
			var encDesc string
			if err := txRows.Scan(&id, &encDesc); err != nil {
				log.Printf("creditcard: reapply rules scan: %v", err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to scan transaction"})
				return
			}
			desc, err := encryption.DecryptField(encDesc)
			if err != nil {
				log.Printf("creditcard: reapply rules decrypt tx %d: %v", id, err)
				continue
			}
			if gid := applyMerchantRules(rules, desc); gid > 0 {
				assignments = append(assignments, assignment{txID: id, groupID: gid})
			}
		}
		if err := txRows.Err(); err != nil {
			log.Printf("creditcard: reapply rules rows err: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to iterate transactions"})
			return
		}
		// Close rows explicitly before opening a write transaction — keeping a
		// read cursor open while calling db.Begin() can deadlock on SQLite.
		txRows.Close() //nolint:errcheck

		if len(assignments) == 0 {
			writeJSON(w, http.StatusOK, map[string]any{"updated": 0})
			return
		}

		// Group assignments by target group_id for efficient batched UPDATEs.
		byGroup := make(map[int64][]int64)
		for _, a := range assignments {
			byGroup[a.groupID] = append(byGroup[a.groupID], a.txID)
		}

		tx, err := db.Begin()
		if err != nil {
			log.Printf("creditcard: reapply rules begin tx: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to start transaction"})
			return
		}
		defer tx.Rollback() //nolint:errcheck

		const chunkSize = 900
		updated := 0
		for gid, ids := range byGroup {
			for start := 0; start < len(ids); start += chunkSize {
				end := start + chunkSize
				if end > len(ids) {
					end = len(ids)
				}
				chunk := ids[start:end]
				placeholders := make([]string, len(chunk))
				args := make([]any, 0, 3+len(chunk))
				args = append(args, gid, user.ID)
				for i, id := range chunk {
					placeholders[i] = "?"
					args = append(args, id)
				}
				args = append(args, user.ID)
				query := `UPDATE credit_card_transactions
					SET group_id = ?
					WHERE user_id = ?
					  AND id IN (` + strings.Join(placeholders, ",") + `)
					  AND (
					    group_id IS NULL OR
					    group_id = (
					      SELECT id FROM credit_card_groups
					      WHERE user_id = ? AND name = 'Diverse'
					      ORDER BY sort_order, id LIMIT 1
					    )
					  )`
				res, err := tx.Exec(query, args...)
				if err != nil {
					log.Printf("creditcard: reapply rules update chunk: %v", err)
					writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update transactions"})
					return
				}
				n, _ := res.RowsAffected()
				updated += int(n)
			}
		}

		if err := tx.Commit(); err != nil {
			log.Printf("creditcard: reapply rules commit: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to commit"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{"updated": updated})
	}
}

// EnsureDefaultGroup seeds a 'Diverse' group for the user if they have no groups yet.
// Returns the ID of the newly created group, or 0 if the user already had groups.
// The INSERT…WHERE NOT EXISTS is atomic, preventing duplicate groups under concurrent imports.
func EnsureDefaultGroup(db *sql.DB, userID int64) (int64, error) {
	res, err := db.Exec(
		`INSERT INTO credit_card_groups (user_id, name, sort_order)
		SELECT ?, 'Diverse', 0
		WHERE NOT EXISTS (
			SELECT 1 FROM credit_card_groups WHERE user_id = ?
		)`,
		userID,
		userID,
	)
	if err != nil {
		return 0, err
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	if rowsAffected == 0 {
		return 0, nil
	}
	return res.LastInsertId()
}
