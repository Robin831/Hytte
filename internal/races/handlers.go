package races

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"

	"github.com/Robin831/Hytte/internal/auth"
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
		writeJSON(w, http.StatusOK, map[string]any{"events": events, "watches": watches})
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
		writeJSON(w, http.StatusOK, map[string]any{"event": event, "changes": changes, "watch": watch})
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
