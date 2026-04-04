package budget

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/go-chi/chi/v5"
)

// currentMonth returns the current month as YYYY-MM.
func currentMonth() string {
	return time.Now().Format("2006-01")
}

// -- Variable Bills handlers --

// VariableBillsListHandler returns all variable bills for the authenticated user
// with entries for the given month (defaults to current month).
// Query param: ?month=YYYY-MM
func VariableBillsListHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		month := r.URL.Query().Get("month")
		if month == "" {
			month = currentMonth()
		}
		if err := ValidateMonth(month); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		bills, err := ListVariableBills(db, user.ID, month)
		if err != nil {
			log.Printf("budget: list variable bills for user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list variable bills"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"variable_bills": bills})
	}
}

// VariableBillsCreateHandler creates a new variable bill for the authenticated user.
// Body: {"name": "...", "recurring_id": null|<int>}
func VariableBillsCreateHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		var b VariableBill
		if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		if b.Name == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
			return
		}
		if err := CreateVariableBill(db, user.ID, &b); err != nil {
			if errors.Is(err, ErrCreditCardAlreadyLinked) {
				writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
				return
			}
			log.Printf("budget: create variable bill for user %d: %v", user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create variable bill"})
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"variable_bill": b})
	}
}

// VariableBillsUpdateHandler updates name and/or recurring_id for a variable bill.
// Body: {"name": "...", "recurring_id": null|<int>}
func VariableBillsUpdateHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
			return
		}
		var b VariableBill
		if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		if b.Name == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
			return
		}
		if err := UpdateVariableBill(db, user.ID, id, &b); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "variable bill not found"})
				return
			}
			if errors.Is(err, ErrCreditCardAlreadyLinked) {
				writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
				return
			}
			log.Printf("budget: update variable bill %d for user %d: %v", id, user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update variable bill"})
			return
		}
		b.ID = id
		b.UserID = user.ID
		b.Entries = []VariableEntry{}
		writeJSON(w, http.StatusOK, map[string]any{"variable_bill": b})
	}
}

// VariableBillsDeleteHandler removes a variable bill (and cascades entries).
func VariableBillsDeleteHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
			return
		}
		if err := DeleteVariableBill(db, user.ID, id); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "variable bill not found"})
				return
			}
			log.Printf("budget: delete variable bill %d for user %d: %v", id, user.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete variable bill"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	}
}

// VariableBillsSetEntriesHandler replaces all entries for a month.
// URL: PUT /budget/variables/{id}/entries?month=YYYY-MM
// Body: [{"sub_name": "...", "amount": 123.45}, ...]
func VariableBillsSetEntriesHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
			return
		}
		month := r.URL.Query().Get("month")
		if month == "" {
			month = currentMonth()
		}
		if err := ValidateMonth(month); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		var entries []VariableEntry
		if err := json.NewDecoder(r.Body).Decode(&entries); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		if err := SetMonthEntries(db, user.ID, id, month, entries); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "variable bill not found"})
				return
			}
			log.Printf("budget: set entries for variable bill %d user %d month %s: %v", id, user.ID, month, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to set entries"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// VariableBillsCopyEntriesHandler copies entries from one month to another.
// URL: POST /budget/variables/{id}/copy?from=YYYY-MM&to=YYYY-MM
func VariableBillsCopyEntriesHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
			return
		}
		from := r.URL.Query().Get("from")
		to := r.URL.Query().Get("to")
		if from == "" || to == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "from and to query parameters are required"})
			return
		}
		if err := ValidateMonth(from); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid from: " + err.Error()})
			return
		}
		if err := ValidateMonth(to); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid to: " + err.Error()})
			return
		}
		entries, err := CopyMonthEntries(db, user.ID, id, from, to)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "variable bill not found"})
				return
			}
			log.Printf("budget: copy entries for variable bill %d user %d from %s to %s: %v", id, user.ID, from, to, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to copy entries"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"entries": entries})
	}
}
