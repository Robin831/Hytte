package infra

import (
	"database/sql"
	"encoding/json"
	"net/http"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/go-chi/chi/v5"
)

// ModulesListHandler returns the list of available modules and their enabled
// state for the authenticated user.
func ModulesListHandler(db *sql.DB, registry *Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		configs, err := GetModuleConfigs(db, user.ID)
		if err != nil {
			http.Error(w, `{"error":"failed to load module config"}`, http.StatusInternalServerError)
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

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"modules": modules})
	}
}

// ModuleToggleHandler enables or disables a module for the authenticated user.
func ModuleToggleHandler(db *sql.DB, registry *Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		moduleName := chi.URLParam(r, "name")

		if registry.Get(moduleName) == nil {
			http.Error(w, `{"error":"unknown module"}`, http.StatusNotFound)
			return
		}

		var body struct {
			Enabled bool `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
			return
		}

		if err := SetModuleEnabled(db, user.ID, moduleName, body.Enabled); err != nil {
			http.Error(w, `{"error":"failed to update module config"}`, http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}
}

// StatusHandler returns aggregated status from all enabled modules.
func StatusHandler(db *sql.DB, registry *Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		configs, err := GetModuleConfigs(db, user.ID)
		if err != nil {
			http.Error(w, `{"error":"failed to load module config"}`, http.StatusInternalServerError)
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

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"overall": overall,
			"modules": results,
		})
	}
}

// ModuleDetailHandler returns detailed data for a specific module.
func ModuleDetailHandler(db *sql.DB, registry *Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		moduleName := chi.URLParam(r, "name")

		m := registry.Get(moduleName)
		if m == nil {
			http.Error(w, `{"error":"unknown module"}`, http.StatusNotFound)
			return
		}

		result := m.Check()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}
}
