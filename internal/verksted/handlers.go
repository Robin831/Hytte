package verksted

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

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("verksted: encode response: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// writeErr maps store errors to HTTP statuses.
func writeErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		writeError(w, http.StatusNotFound, "not found")
	case errors.Is(err, ErrInvalid):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		log.Printf("verksted: %v", err)
		writeError(w, http.StatusInternalServerError, "something went wrong")
	}
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return false
	}
	return true
}

func idParam(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "bad id")
		return 0, false
	}
	return id, true
}

func userID(r *http.Request) int64 { return auth.UserFromContext(r.Context()).ID }

// RegisterRoutes mounts the Verksted API (gate it with the "verksted"
// feature at the call site).
func RegisterRoutes(r chi.Router, db *sql.DB) {
	r.Get("/verksted", handleOverview(db))
	r.Post("/verksted/people", handleAddPerson(db))
	r.Patch("/verksted/people/{id}", handleUpdatePerson(db))
	r.Delete("/verksted/people/{id}", handleDeletePerson(db))
	r.Put("/verksted/people/{id}/budget", handlePersonBudget(db))
	r.Put("/verksted/budget", handleTotalBudget(db))
	r.Post("/verksted/wishes", handleAddWish(db))
	r.Patch("/verksted/wishes/{id}", handleUpdateWish(db))
	r.Delete("/verksted/wishes/{id}", handleDeleteWish(db))
	r.Post("/verksted/wishes/{id}/reserve", handleReserveWish(db))
	r.Post("/verksted/gifts", handleAddGift(db))
	r.Patch("/verksted/gifts/{id}", handleUpdateGift(db))
	r.Delete("/verksted/gifts/{id}", handleDeleteGift(db))
}

func handleOverview(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		season := CurrentSeason(time.Now())
		if s := r.URL.Query().Get("season"); s != "" {
			v, err := strconv.Atoi(s)
			if err != nil || checkSeason(v) != nil {
				writeError(w, http.StatusBadRequest, "season must be a year")
				return
			}
			season = v
		}
		o, err := LoadOverview(r.Context(), db, userID(r), season)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, o)
	}
}

func handleAddPerson(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in PersonInput
		if !decode(w, r, &in) {
			return
		}
		id, err := AddPerson(r.Context(), db, in)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]int64{"id": id})
	}
}

func handleUpdatePerson(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := idParam(w, r)
		if !ok {
			return
		}
		var in PersonInput
		if !decode(w, r, &in) {
			return
		}
		if err := UpdatePerson(r.Context(), db, id, in); err != nil {
			writeErr(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleDeletePerson(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := idParam(w, r)
		if !ok {
			return
		}
		if err := DeletePerson(r.Context(), db, id); err != nil {
			writeErr(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

type budgetBody struct {
	Season int     `json:"season"`
	NOK    float64 `json:"budget_nok"`
}

func handlePersonBudget(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := idParam(w, r)
		if !ok {
			return
		}
		var in budgetBody
		if !decode(w, r, &in) {
			return
		}
		if err := SetPersonBudget(r.Context(), db, id, in.Season, in.NOK); err != nil {
			writeErr(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleTotalBudget(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in budgetBody
		if !decode(w, r, &in) {
			return
		}
		if err := SetTotalBudget(r.Context(), db, in.Season, in.NOK); err != nil {
			writeErr(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleAddWish(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in WishInput
		if !decode(w, r, &in) {
			return
		}
		id, err := AddWish(r.Context(), db, userID(r), in)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]int64{"id": id})
	}
}

func handleUpdateWish(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := idParam(w, r)
		if !ok {
			return
		}
		var in WishInput
		if !decode(w, r, &in) {
			return
		}
		if err := UpdateWish(r.Context(), db, id, in); err != nil {
			writeErr(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleDeleteWish(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := idParam(w, r)
		if !ok {
			return
		}
		if err := DeleteWish(r.Context(), db, id); err != nil {
			writeErr(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleReserveWish(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := idParam(w, r)
		if !ok {
			return
		}
		var in struct {
			Reserved bool `json:"reserved"`
		}
		if !decode(w, r, &in) {
			return
		}
		if err := ReserveWish(r.Context(), db, userID(r), id, in.Reserved); err != nil {
			writeErr(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleAddGift(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in GiftInput
		if !decode(w, r, &in) {
			return
		}
		id, err := AddGift(r.Context(), db, userID(r), in)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]int64{"id": id})
	}
}

func handleUpdateGift(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := idParam(w, r)
		if !ok {
			return
		}
		var in GiftInput
		if !decode(w, r, &in) {
			return
		}
		if err := UpdateGift(r.Context(), db, userID(r), id, in); err != nil {
			writeErr(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleDeleteGift(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := idParam(w, r)
		if !ok {
			return
		}
		if err := DeleteGift(r.Context(), db, userID(r), id); err != nil {
			writeErr(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
