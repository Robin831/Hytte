package budget

import (
	"database/sql"
	"log"
	"net/http"
	"sort"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
)

// UpcomingTransaction represents a future transaction from a recurring rule.
type UpcomingTransaction struct {
	Date           string    `json:"date"`
	Description    string    `json:"description"`
	Amount         float64   `json:"amount"`
	YourShare      float64   `json:"your_share"`
	SplitType      SplitType `json:"split_type"`
	CategoryID     *int64    `json:"category_id"`
	CategoryName   string    `json:"category_name"`
	CategoryColor  string    `json:"category_color"`
	CategoryIcon   string    `json:"category_icon"`
	Frequency      Frequency `json:"frequency"`
	RecurringID    int64     `json:"recurring_id"`
	VariableName   string    `json:"variable_name,omitempty"`
}

// UpcomingHandler returns upcoming transactions for the next 30 days.
// GET /api/budget/upcoming
func UpcomingHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		osloLoc, err := time.LoadLocation("Europe/Oslo")
		if err != nil {
			osloLoc = time.UTC
		}
		now := time.Now().In(osloLoc)
		today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, osloLoc)
		horizon := today.AddDate(0, 0, 30)

		recurrings, err := listActiveRecurring(db, user.ID)
		if err != nil {
			log.Printf("budget: upcoming: list recurring for user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list recurring transactions"})
			return
		}

		globalSplitPct, err := GetIncomeSplit(db, user.ID)
		if err != nil {
			log.Printf("budget: upcoming: get income split for user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to get income split"})
			return
		}

		// Build category lookup.
		cats, err := ListCategories(db, user.ID)
		if err != nil {
			log.Printf("budget: upcoming: list categories for user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list categories"})
			return
		}
		catByID := make(map[int64]Category, len(cats))
		for _, c := range cats {
			catByID[c.ID] = c
		}

		currentMonth := now.Format("2006-01")

		var upcoming []UpcomingTransaction
		for _, rec := range recurrings {
			nextDue, err := nextRecurringDueDate(rec)
			if err != nil {
				continue
			}
			adjusted := nextBusinessDay(nextDue)

			// Skip if before today or beyond horizon.
			if adjusted.Before(today) || adjusted.After(horizon) {
				continue
			}

			// Respect end_date.
			if rec.EndDate != "" {
				endDate, err := time.Parse("2006-01-02", rec.EndDate)
				if err == nil && nextDue.After(endDate) {
					continue
				}
			}

			amount := rec.Amount
			var variableName string
			if rec.VariableID != nil {
				month := nextDue.Format("2006-01")
				if month == currentMonth {
					varName, varTotal, _, varErr := variableBillMonthInfo(db, *rec.VariableID, month)
					if varErr == nil {
						amount = varTotal
						variableName = varName
					}
				}
			}

			yourShare, _ := regningComputeSplit(amount, rec.SplitType, rec.SplitPct, globalSplitPct)

			var catName, catColor, catIcon string
			if rec.CategoryID != nil {
				if cat, ok := catByID[*rec.CategoryID]; ok {
					catName = cat.Name
					catColor = cat.Color
					catIcon = cat.Icon
				}
			}

			upcoming = append(upcoming, UpcomingTransaction{
				Date:          adjusted.Format("2006-01-02"),
				Description:   rec.Description,
				Amount:        amount,
				YourShare:     yourShare,
				SplitType:     rec.SplitType,
				CategoryID:    rec.CategoryID,
				CategoryName:  catName,
				CategoryColor: catColor,
				CategoryIcon:  catIcon,
				Frequency:     rec.Frequency,
				RecurringID:   rec.ID,
				VariableName:  variableName,
			})
		}

		sort.Slice(upcoming, func(i, j int) bool {
			return upcoming[i].Date < upcoming[j].Date
		})

		writeJSON(w, http.StatusOK, map[string]any{"upcoming": upcoming})
	}
}
