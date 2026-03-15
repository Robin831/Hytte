package infra

import (
	"database/sql"
	"encoding/json"
	"net/http"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/go-chi/chi/v5"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// ModulesListHandler returns the list of available modules and their enabled
// state for the authenticated user.
func ModulesListHandler(db *sql.DB, registry *Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		configs, err := GetModuleConfigs(db, user.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load module config")
			return
		}

		enabledMap := make(map[string]bool)
		for _, c := range configs {
			enabledMap[c.Module] = c.Enabled
		}

		type moduleInfo struct {
			Name        string `json:"name"`
			DisplayName string `json:"display_name"`
			Description string `json:"description"`
			Enabled     bool   `json:"enabled"`
		}

		modules := make([]moduleInfo, 0)
		for _, m := range registry.All() {
			enabled := true // default
			if v, ok := enabledMap[m.Name()]; ok {
				enabled = v
			}
			modules = append(modules, moduleInfo{
				Name:        m.Name(),
				DisplayName: m.DisplayName(),
				Description: m.Description(),
				Enabled:     enabled,
			})
		}

		writeJSON(w, http.StatusOK, map[string]any{"modules": modules})
	}
}

// ModuleToggleHandler enables or disables a module for the authenticated user.
func ModuleToggleHandler(db *sql.DB, registry *Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		moduleName := chi.URLParam(r, "name")

		if registry.Get(moduleName) == nil {
			writeError(w, http.StatusNotFound, "unknown module")
			return
		}

		var body struct {
			Enabled bool `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		if err := SetModuleEnabled(db, user.ID, moduleName, body.Enabled); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update module config")
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

// StatusHandler returns aggregated status from all enabled modules.
func StatusHandler(db *sql.DB, registry *Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		configs, err := GetModuleConfigs(db, user.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load module config")
			return
		}

		enabledMap := make(map[string]bool)
		for _, c := range configs {
			enabledMap[c.Module] = c.Enabled
		}

		var results []ModuleResult
		for _, m := range registry.All() {
			enabled := true
			if v, ok := enabledMap[m.Name()]; ok {
				enabled = v
			}
			if !enabled {
				continue
			}
			results = append(results, m.Check())
		}

		// Compute overall status.
		overall := StatusOK
		if len(results) == 0 {
			overall = StatusUnknown
		}
		for _, res := range results {
			switch res.Status {
			case StatusDown:
				overall = StatusDown
			case StatusDegraded:
				if overall != StatusDown {
					overall = StatusDegraded
				}
			case StatusUnknown:
				if overall == StatusOK {
					overall = StatusUnknown
				}
			}
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"overall": overall,
			"modules": results,
		})
	}
}

// ModuleDetailHandler returns detailed data for a specific module.
// It verifies the module is enabled for the authenticated user before checking.
func ModuleDetailHandler(db *sql.DB, registry *Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		moduleName := chi.URLParam(r, "name")

		m := registry.Get(moduleName)
		if m == nil {
			writeError(w, http.StatusNotFound, "unknown module")
			return
		}

		enabled, err := IsModuleEnabled(db, user.ID, moduleName)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to check module config")
			return
		}
		if !enabled {
			writeError(w, http.StatusForbidden, "module is disabled")
			return
		}

		result := m.Check()
		writeJSON(w, http.StatusOK, result)
	}
}
