package creditcard

import (
	"database/sql"
	"log"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
)

// MonthlyHistoryRow is one group's totals across months.
type MonthlyHistoryRow struct {
	GroupID   *int64             `json:"group_id"`
	GroupName string             `json:"group_name"`
	Totals    map[string]float64 `json:"totals"` // key: YYYY-MM
}

// MonthlyHistoryResponse is returned by MonthlyHistoryHandler.
type MonthlyHistoryResponse struct {
	Months            []string           `json:"months"`             // YYYY-MM, oldest first
	Rows              []MonthlyHistoryRow `json:"rows"`               // one per group, sorted by sort_order
	MonthTotals       map[string]float64 `json:"month_totals"`       // total expenses per month (payments excluded)
	InnbetalingTotals map[string]float64 `json:"innbetaling_totals"` // total payments per month (negative values: payment belop is positive, negated by query)
	NetTotals         map[string]float64 `json:"net_totals"`         // net outstanding = expenses + innbetaling_totals (positive = still owe)
}

// MonthlyHistoryHandler returns group expense totals grouped by month for a
// given credit card. Covers the last N months (default 6, max 24).
//
// Query params:
//   - credit_card_id: required
//   - months:         number of months to cover (default 6, max 24)
func MonthlyHistoryHandler(db *sql.DB) http.HandlerFunc {
	return monthlyHistoryHandler(db, time.Now)
}

func monthlyHistoryHandler(db *sql.DB, nowFn func() time.Time) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		creditCardID := r.URL.Query().Get("credit_card_id")
		if creditCardID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "credit_card_id is required"})
			return
		}

		numMonths := 6
		if raw := r.URL.Query().Get("months"); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil || n < 1 || n > 24 {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "months must be an integer between 1 and 24"})
				return
			}
			numMonths = n
		}

		// Build the list of YYYY-MM months (oldest first).
		now := nowFn()
		months := make([]string, numMonths)
		for i := 0; i < numMonths; i++ {
			offset := numMonths - 1 - i
			t := time.Date(now.Year(), now.Month()-time.Month(offset), 1, 0, 0, 0, 0, time.Local)
			months[i] = t.Format("2006-01")
		}
		startDate := months[0] + "-01"
		endDate := time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, time.Local).Format("2006-01-02")

		// Fetch all groups so we preserve order and include zero-spend groups.
		groupRows, err := db.Query(`
			SELECT id, name, sort_order
			FROM credit_card_groups
			WHERE user_id = ?
			ORDER BY sort_order ASC, id ASC
		`, user.ID)
		if err != nil {
			log.Printf("creditcard: history groups query: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load groups"})
			return
		}
		defer groupRows.Close() //nolint:errcheck

		type groupMeta struct {
			id        int64
			name      string
			sortOrder int
		}
		var allGroups []groupMeta
		for groupRows.Next() {
			var g groupMeta
			if err := groupRows.Scan(&g.id, &g.name, &g.sortOrder); err != nil {
				log.Printf("creditcard: history groups scan: %v", err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to scan group"})
				return
			}
			allGroups = append(allGroups, g)
		}
		if err := groupRows.Err(); err != nil {
			log.Printf("creditcard: history groups rows err: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to iterate groups"})
			return
		}

		// Query totals grouped by month, group_id, and innbetaling flag.
		txRows, err := db.Query(`
			SELECT substr(transaksjonsdato, 1, 7) AS month,
			       group_id,
			       is_innbetaling,
			       COALESCE(-SUM(belop), 0) AS total
			FROM credit_card_transactions
			WHERE user_id = ?
			  AND credit_card_id = ?
			  AND transaksjonsdato >= ?
			  AND transaksjonsdato < ?
			GROUP BY month, group_id, is_innbetaling
		`, user.ID, creditCardID, startDate, endDate)
		if err != nil {
			log.Printf("creditcard: history totals query: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load history"})
			return
		}
		defer txRows.Close() //nolint:errcheck

		// groupTotals[groupID or -1 for null][month] = total
		type groupKey struct{ id int64 } // -1 means unassigned
		totalsMap := map[groupKey]map[string]float64{}
		monthTotals := map[string]float64{}
		innbetalingTotals := map[string]float64{}

		for txRows.Next() {
			var month string
			var gid sql.NullInt64
			var isInnbetaling bool
			var total float64
			if err := txRows.Scan(&month, &gid, &isInnbetaling, &total); err != nil {
				log.Printf("creditcard: history totals scan: %v", err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to scan history"})
				return
			}
			key := groupKey{-1}
			if gid.Valid {
				key = groupKey{gid.Int64}
			}
			if totalsMap[key] == nil {
				totalsMap[key] = map[string]float64{}
			}
			totalsMap[key][month] += total
			if !isInnbetaling {
				monthTotals[month] += total
			} else {
				innbetalingTotals[month] += total
			}
		}
		if err := txRows.Err(); err != nil {
			log.Printf("creditcard: history totals rows err: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to iterate history"})
			return
		}

		// Build group index for quick lookup.
		groupByID := map[int64]groupMeta{}
		for _, g := range allGroups {
			groupByID[g.id] = g
		}

		// Find the Diverse group id (catch-all for unassigned).
		var diverseGroupID int64 = -1
		for _, g := range allGroups {
			if g.name == "Diverse" {
				diverseGroupID = g.id
				break
			}
		}

		// Merge unassigned (null group) into Diverse totals.
		if unassignedTotals, ok := totalsMap[groupKey{-1}]; ok {
			if diverseGroupID >= 0 {
				if totalsMap[groupKey{diverseGroupID}] == nil {
					totalsMap[groupKey{diverseGroupID}] = map[string]float64{}
				}
				for m, v := range unassignedTotals {
					totalsMap[groupKey{diverseGroupID}][m] += v
				}
				delete(totalsMap, groupKey{-1})
			}
		}

		// Build rows: one per named group, then any anonymous group if Diverse not present.
		rows := make([]MonthlyHistoryRow, 0, len(allGroups)+1)
		seenGroups := map[int64]bool{}

		// Named groups in sort_order.
		for _, g := range allGroups {
			id := g.id
			gid := id
			row := MonthlyHistoryRow{
				GroupID:   &gid,
				GroupName: g.name,
				Totals:    totalsMap[groupKey{id}],
			}
			if row.Totals == nil {
				row.Totals = map[string]float64{}
			}
			rows = append(rows, row)
			seenGroups[id] = true
		}

		// Any totals for group IDs not in our groups list (shouldn't happen, but be safe).
		for key, totals := range totalsMap {
			if key.id == -1 {
				continue
			}
			if seenGroups[key.id] {
				continue
			}
			gid := key.id
			name := ""
			if g, ok := groupByID[key.id]; ok {
				name = g.name
			}
			rows = append(rows, MonthlyHistoryRow{
				GroupID:   &gid,
				GroupName: name,
				Totals:    totals,
			})
		}

		// Unassigned row (null group_id) if Diverse doesn't exist.
		if _, ok := totalsMap[groupKey{-1}]; ok {
			rows = append(rows, MonthlyHistoryRow{
				GroupID:   nil,
				GroupName: "",
				Totals:    totalsMap[groupKey{-1}],
			})
		}

		// Sort rows: named groups first by sort_order (then group_id as tie-break),
		// unknown group IDs after known ones, unnamed (nil GroupID) last.
		sort.SliceStable(rows, func(i, j int) bool {
			ri, rj := rows[i], rows[j]
			if ri.GroupID == nil && rj.GroupID != nil {
				return false
			}
			if ri.GroupID != nil && rj.GroupID == nil {
				return true
			}
			if ri.GroupID == nil && rj.GroupID == nil {
				return false
			}
			gi, giOK := groupByID[*ri.GroupID]
			gj, gjOK := groupByID[*rj.GroupID]
			// Unknown group IDs sort after known ones to keep ordering stable.
			if giOK != gjOK {
				return giOK
			}
			if !giOK {
				return *ri.GroupID < *rj.GroupID
			}
			if gi.sortOrder != gj.sortOrder {
				return gi.sortOrder < gj.sortOrder
			}
			return *ri.GroupID < *rj.GroupID
		})

		// Compute net outstanding per month: expenses + innbetaling_totals (payments are negative).
		netTotals := make(map[string]float64, len(months))
		for _, m := range months {
			netTotals[m] = monthTotals[m] + innbetalingTotals[m]
		}

		writeJSON(w, http.StatusOK, MonthlyHistoryResponse{
			Months:            months,
			Rows:              rows,
			MonthTotals:       monthTotals,
			InnbetalingTotals: innbetalingTotals,
			NetTotals:         netTotals,
		})
	}
}
