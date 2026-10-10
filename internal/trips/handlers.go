package trips

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

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("trips: writeJSON: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func writeStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrValidation):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrNotFound):
		writeError(w, http.StatusNotFound, "trip not found")
	case errors.Is(err, ErrForbidden):
		writeError(w, http.StatusForbidden, "only the trip's travellers can change it")
	default:
		log.Printf("trips: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to save trip")
	}
}

func idParam(r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, name), 10, 64)
	return id, err == nil && id > 0
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return false
	}
	return true
}

// HandleList lists the trips the user can see.
func HandleList(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		list, err := ListTrips(r.Context(), db, user.ID)
		if err != nil {
			log.Printf("trips: list: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to list trips")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"trips": list})
	}
}

// HandleGet returns one trip.
func HandleGet(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		id, ok := idParam(r, "id")
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid trip ID")
			return
		}
		t, err := GetTrip(r.Context(), db, id, user.ID)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"trip": t})
	}
}

// HandleCreate creates a trip owned by the user.
func HandleCreate(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		var in TripInput
		if !decode(w, r, &in) {
			return
		}
		id, err := CreateTrip(r.Context(), db, user.ID, in)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		TranslateSoon(db, id, user.ID)
		writeJSON(w, http.StatusCreated, map[string]any{"id": id})
	}
}

// HandleUpdate replaces a trip.
func HandleUpdate(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		id, ok := idParam(r, "id")
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid trip ID")
			return
		}
		var in TripInput
		if !decode(w, r, &in) {
			return
		}
		if err := UpdateTrip(r.Context(), db, id, user.ID, in); err != nil {
			writeStoreError(w, err)
			return
		}
		TranslateSoon(db, id, user.ID)
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// HandleDelete deletes a trip (owner only).
func HandleDelete(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		id, ok := idParam(r, "id")
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid trip ID")
			return
		}
		if err := DeleteTrip(r.Context(), db, id, user.ID); err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// HandleAddChecklist adds a checklist group to a trip.
func HandleAddChecklist(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		id, ok := idParam(r, "id")
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid trip ID")
			return
		}
		var in ChecklistInput
		if !decode(w, r, &in) {
			return
		}
		gid, err := AddChecklist(r.Context(), db, id, user.ID, in)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		TranslateSoon(db, id, user.ID)
		writeJSON(w, http.StatusCreated, map[string]any{"id": gid})
	}
}

// HandleDeleteChecklist removes a checklist group.
func HandleDeleteChecklist(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		id, ok := idParam(r, "id")
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid checklist ID")
			return
		}
		if err := DeleteChecklist(r.Context(), db, id, user.ID); err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// HandleAddItem adds an item to a checklist.
func HandleAddItem(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		gid, ok := idParam(r, "id")
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid checklist ID")
			return
		}
		var in ItemInput
		if !decode(w, r, &in) {
			return
		}
		iid, err := AddItem(r.Context(), db, gid, user.ID, in)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		if tripID, err := groupTrip(r.Context(), db, gid); err == nil {
			TranslateSoon(db, tripID, user.ID)
		}
		writeJSON(w, http.StatusCreated, map[string]any{"id": iid})
	}
}

// HandleUpdateItem edits a checklist item.
func HandleUpdateItem(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		id, ok := idParam(r, "id")
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid item ID")
			return
		}
		var in ItemInput
		if !decode(w, r, &in) {
			return
		}
		if err := UpdateItem(r.Context(), db, id, user.ID, in); err != nil {
			writeStoreError(w, err)
			return
		}
		if tripID, err := itemTrip(r.Context(), db, id); err == nil {
			TranslateSoon(db, tripID, user.ID)
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// HandleToggleItem ticks or unticks an item: {"done": true}.
func HandleToggleItem(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		id, ok := idParam(r, "id")
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid item ID")
			return
		}
		var in struct {
			Done bool `json:"done"`
		}
		if !decode(w, r, &in) {
			return
		}
		if err := SetDone(r.Context(), db, id, user.ID, in.Done); err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// HandleDeleteItem removes an item.
func HandleDeleteItem(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		id, ok := idParam(r, "id")
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid item ID")
			return
		}
		if err := DeleteItem(r.Context(), db, id, user.ID); err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// HandleCandidates lists a tentative trip's possible dates with clashes.
func HandleCandidates(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		id, ok := idParam(r, "id")
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid trip ID")
			return
		}
		cands, err := Candidates(r.Context(), db, id, user.ID)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"candidates": cands})
	}
}

// HandleListPeople returns the family roster.
func HandleListPeople(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		people, err := ListPeople(r.Context(), db)
		if err != nil {
			log.Printf("trips: roster: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to load the family")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"people": people})
	}
}

// HandleAddPerson adds someone without a Hytte account (admin).
func HandleAddPerson(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in PersonInput
		if !decode(w, r, &in) {
			return
		}
		id, err := AddPerson(r.Context(), db, in)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"id": id})
	}
}

// HandleUpdatePerson edits a roster entry (admin).
func HandleUpdatePerson(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := idParam(r, "id")
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid person ID")
			return
		}
		var in PersonInput
		if !decode(w, r, &in) {
			return
		}
		if err := UpdatePerson(r.Context(), db, id, in); err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// HandleDeletePerson removes someone without an account (admin).
func HandleDeletePerson(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := idParam(r, "id")
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid person ID")
			return
		}
		if err := DeletePerson(r.Context(), db, id); err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}
