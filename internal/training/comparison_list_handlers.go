package training

import (
	"database/sql"
	"net/http"
	"strconv"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/go-chi/chi/v5"
)

// ListComparisonAnalysesHandler handles GET /api/training/compare/analyses.
// Returns all cached comparison analyses for the authenticated user. Restricted to admin users.
func ListComparisonAnalysesHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if !user.IsAdmin {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "AI features are restricted to admin users"})
			return
		}

		analyses, err := ListComparisonAnalyses(db, user.ID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list analyses"})
			return
		}

		writeJSON(w, http.StatusOK, analyses)
	}
}

// GetComparisonAnalysisHandler handles GET /api/training/compare/analyses/{id}.
// Returns a single cached comparison analysis by ID for the authenticated user. Restricted to admin users.
func GetComparisonAnalysisHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if !user.IsAdmin {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "AI features are restricted to admin users"})
			return
		}

		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil || id <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid analysis ID"})
			return
		}

		analysis, err := GetComparisonAnalysisByID(db, id, user.ID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to retrieve analysis"})
			return
		}
		if analysis == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "analysis not found"})
			return
		}

		writeJSON(w, http.StatusOK, analysis)
	}
}

// DeleteComparisonAnalysisHandler handles DELETE /api/training/compare/analyses/{id}.
// Deletes a cached comparison analysis by ID. Restricted to admin users.
func DeleteComparisonAnalysisHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if !user.IsAdmin {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "AI features are restricted to admin users"})
			return
		}

		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil || id <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid analysis ID"})
			return
		}

		err = DeleteComparisonAnalysisByID(db, id, user.ID)
		if err == sql.ErrNoRows {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "analysis not found"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete analysis"})
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}
