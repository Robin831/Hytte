package races

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/currency"
	"github.com/go-chi/chi/v5"
)

// changeHistoryLimit caps the change log returned on the detail page.
const changeHistoryLimit = 100

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("races: writeJSON encode error: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// writeStoreError maps store errors to responses: validation → 400 with the
// message, missing → 404, anything else → 500 (logged, generic message).
func writeStoreError(w http.ResponseWriter, err error, what string) {
	switch {
	case errors.Is(err, ErrValidation):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrNotFound):
		writeError(w, http.StatusNotFound, what+" not found")
	default:
		log.Printf("races: %s: %v", what, err)
		writeError(w, http.StatusInternalServerError, "failed to save "+what)
	}
}

// nokRates returns NOK per unit for every synced currency, so the page can
// show entry fees in kroner. Rates are a nicety: on failure the page just
// shows prices as published.
func nokRates(r *http.Request, db *sql.DB) map[string]float64 {
	rates, err := currency.LatestNOKRates(r.Context(), db)
	if err != nil {
		log.Printf("races: load NOK rates: %v", err)
		return map[string]float64{"NOK": 1}
	}
	return rates
}

func idParam(r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, name), 10, 64)
	return id, err == nil && id > 0
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return false
	}
	return true
}

// HandleList returns the whole catalog plus the user's watches. Filtering
// and grouping happen client-side.
func HandleList(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		events, err := ListEvents(r.Context(), db)
		if err != nil {
			log.Printf("races: list events: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to list races")
			return
		}
		watches, err := ListWatches(r.Context(), db, user.ID)
		if err != nil {
			log.Printf("races: list watches: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to list races")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"events": events, "watches": watches, "rates": nokRates(r, db)})
	}
}

// HandleGet returns one race with its change history and the user's watch.
func HandleGet(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		id, ok := idParam(r, "id")
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid race ID")
			return
		}
		event, err := GetEvent(r.Context(), db, id)
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "race not found")
			return
		}
		if err != nil {
			log.Printf("races: get event: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to load race")
			return
		}
		changes, err := ListChanges(r.Context(), db, id, changeHistoryLimit)
		if err != nil {
			log.Printf("races: list changes: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to load race")
			return
		}
		var watch *Watch
		if wt, err := GetWatch(r.Context(), db, user.ID, id); err == nil {
			watch = wt
		} else if !errors.Is(err, ErrNotFound) {
			log.Printf("races: get watch: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to load race")
			return
		}
		research, err := LatestRunForEvent(r.Context(), db, id)
		if err != nil {
			log.Printf("races: latest research run: %v", err)
		}
		writeJSON(w, http.StatusOK, map[string]any{"event": event, "changes": changes, "watch": watch,
			"rates": nokRates(r, db), "research": research})
	}
}

// HandleSetWatch creates or updates the user's watch on a race.
func HandleSetWatch(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		id, ok := idParam(r, "id")
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid race ID")
			return
		}
		var in WatchInput
		if !decode(w, r, &in) {
			return
		}
		watch, err := SetWatch(r.Context(), db, user.ID, id, in)
		if err != nil {
			writeStoreError(w, err, "race")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"watch": watch})
	}
}

// HandleDeleteWatch stops tracking a race.
func HandleDeleteWatch(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		id, ok := idParam(r, "id")
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid race ID")
			return
		}
		if err := DeleteWatch(r.Context(), db, user.ID, id); err != nil {
			writeStoreError(w, err, "watch")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// HandleCreate adds a race to the catalog. Admin-only.
func HandleCreate(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		var in EventInput
		if !decode(w, r, &in) {
			return
		}
		event, err := CreateEvent(r.Context(), db, in, "", "manual", user.ID)
		if err != nil {
			writeStoreError(w, err, "race")
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"event": event})
	}
}

// HandleUpdate replaces a race's editable fields. Admin-only.
func HandleUpdate(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		id, ok := idParam(r, "id")
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid race ID")
			return
		}
		var in EventInput
		if !decode(w, r, &in) {
			return
		}
		event, _, err := UpdateEvent(r.Context(), db, id, in, "manual", user.ID)
		if err != nil {
			writeStoreError(w, err, "race")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"event": event})
	}
}

// HandleDelete removes a race from the catalog. Admin-only.
func HandleDelete(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := idParam(r, "id")
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid race ID")
			return
		}
		if err := DeleteEvent(r.Context(), db, id); err != nil {
			writeStoreError(w, err, "race")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// HandleCreateDeadline adds a deadline to a race. Admin-only.
func HandleCreateDeadline(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		id, ok := idParam(r, "id")
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid race ID")
			return
		}
		var in DeadlineInput
		if !decode(w, r, &in) {
			return
		}
		d, err := CreateDeadline(r.Context(), db, id, in, "manual", user.ID)
		if err != nil {
			writeStoreError(w, err, "race")
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"deadline": d})
	}
}

// HandleUpdateDeadline replaces a deadline. Admin-only.
func HandleUpdateDeadline(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		id, ok := idParam(r, "id")
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid deadline ID")
			return
		}
		var in DeadlineInput
		if !decode(w, r, &in) {
			return
		}
		d, err := UpdateDeadline(r.Context(), db, id, in, "manual", user.ID)
		if err != nil {
			writeStoreError(w, err, "deadline")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"deadline": d})
	}
}

// HandleDeleteDeadline removes a deadline. Admin-only.
func HandleDeleteDeadline(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		id, ok := idParam(r, "id")
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid deadline ID")
			return
		}
		if err := DeleteDeadline(r.Context(), db, id, "manual", user.ID); err != nil {
			writeStoreError(w, err, "deadline")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// writeResearchError maps research start errors to responses.
func writeResearchError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		writeError(w, http.StatusNotFound, "race not found")
	case errors.Is(err, ErrResearchBusy):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, ErrResearchCooldown), errors.Is(err, ErrResearchBudget):
		writeError(w, http.StatusTooManyRequests, err.Error())
	case errors.Is(err, ErrResearchNoClaude):
		writeError(w, http.StatusServiceUnavailable, err.Error())
	default:
		log.Printf("races: start research: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to start research")
	}
}

// HandleStartResearch queues an automatic check of one race. Anyone with
// the feature may ask; non-admins are limited per race (see manualCooldown).
func HandleStartResearch(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		id, ok := idParam(r, "id")
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid race ID")
			return
		}
		run, err := DefaultResearcher(db).StartEventResearch(r.Context(), id, user.ID, user.IsAdmin)
		if err != nil {
			writeResearchError(w, err)
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"run": run})
	}
}

// HandleStartDiscovery queues a search for missing races. Admin-only.
func HandleStartDiscovery(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		run, err := DefaultResearcher(db).StartDiscovery(r.Context(), user.ID)
		if err != nil {
			writeResearchError(w, err)
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"run": run})
	}
}

// HandleResearchLog returns recent research runs and today's spend against
// the budget. Admin-only.
func HandleResearchLog(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		runs, err := ListRuns(r.Context(), db, 50)
		if err != nil {
			log.Printf("races: research log: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to load research log")
			return
		}
		res := DefaultResearcher(db)
		spent, err := res.SpentToday(r.Context())
		if err != nil {
			log.Printf("races: research spend: %v", err)
		}
		resp := map[string]any{"runs": runs, "spent_today_usd": spent}
		if cfg, budget, err := res.Config(r.Context(), db); err == nil {
			resp["budget_usd"] = budget
			resp["model"] = cfg.Model
		} else {
			resp["config_error"] = err.Error()
		}
		writeJSON(w, http.StatusOK, resp)
	}
}
