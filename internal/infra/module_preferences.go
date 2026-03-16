package infra

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/go-chi/chi/v5"
)

// ModulePreference represents a single key-value preference for a module.
type ModulePreference struct {
	Module    string `json:"module"`
	Key       string `json:"key"`
	Value     string `json:"value"`
	UpdatedAt string `json:"updated_at"`
}

// GetModulePreferences returns all preferences for a given module and user.
func GetModulePreferences(db *sql.DB, userID int64, module string) ([]ModulePreference, error) {
	rows, err := db.Query(
		`SELECT module, key, value, updated_at FROM infra_module_preferences WHERE user_id = ? AND module = ? ORDER BY key`,
		userID, module,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	prefs := make([]ModulePreference, 0)
	for rows.Next() {
		var p ModulePreference
		if err := rows.Scan(&p.Module, &p.Key, &p.Value, &p.UpdatedAt); err != nil {
			return nil, err
		}
		prefs = append(prefs, p)
	}
	return prefs, rows.Err()
}

// GetAllModulePreferences returns all module preferences for a user across all modules.
func GetAllModulePreferences(db *sql.DB, userID int64) ([]ModulePreference, error) {
	rows, err := db.Query(
		`SELECT module, key, value, updated_at FROM infra_module_preferences WHERE user_id = ? ORDER BY module, key`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	prefs := make([]ModulePreference, 0)
	for rows.Next() {
		var p ModulePreference
		if err := rows.Scan(&p.Module, &p.Key, &p.Value, &p.UpdatedAt); err != nil {
			return nil, err
		}
		prefs = append(prefs, p)
	}
	return prefs, rows.Err()
}

// SetModulePreference sets a key-value preference for a module.
func SetModulePreference(db *sql.DB, userID int64, module, key, value string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(
		`INSERT INTO infra_module_preferences (user_id, module, key, value, updated_at)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(user_id, module, key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		userID, module, key, value, now,
	)
	return err
}

// DeleteModulePreference removes a preference key for a module.
func DeleteModulePreference(db *sql.DB, userID int64, module, key string) error {
	res, err := db.Exec(
		`DELETE FROM infra_module_preferences WHERE user_id = ? AND module = ? AND key = ?`,
		userID, module, key,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// --- HTTP handlers ---

// ModulePreferencesGetHandler returns preferences for a specific module.
func ModulePreferencesGetHandler(db *sql.DB, registry *Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		moduleName := chi.URLParam(r, "name")

		if registry.Get(moduleName) == nil {
			writeError(w, http.StatusNotFound, "unknown module")
			return
		}

		prefs, err := GetModulePreferences(db, user.ID, moduleName)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load preferences")
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{"preferences": prefs})
	}
}

// ModulePreferencesPutHandler sets a preference for a module.
func ModulePreferencesPutHandler(db *sql.DB, registry *Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		moduleName := chi.URLParam(r, "name")

		if registry.Get(moduleName) == nil {
			writeError(w, http.StatusNotFound, "unknown module")
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		var body struct {
			Key   string `json:"key"`
			Value string `json:"value"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		body.Key = strings.TrimSpace(body.Key)
		body.Value = strings.TrimSpace(body.Value)

		if body.Key == "" {
			writeError(w, http.StatusBadRequest, "key is required")
			return
		}

		if len(body.Key) > 64 {
			writeError(w, http.StatusBadRequest, "key too long (max 64 characters)")
			return
		}

		if len(body.Value) > 1024 {
			writeError(w, http.StatusBadRequest, "value too long (max 1024 characters)")
			return
		}

		if err := SetModulePreference(db, user.ID, moduleName, body.Key, body.Value); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to save preference")
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

// ModulePreferencesDeleteHandler removes a preference for a module.
func ModulePreferencesDeleteHandler(db *sql.DB, registry *Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		moduleName := chi.URLParam(r, "name")

		if registry.Get(moduleName) == nil {
			writeError(w, http.StatusNotFound, "unknown module")
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		var body struct {
			Key string `json:"key"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		body.Key = strings.TrimSpace(body.Key)

		if body.Key == "" {
			writeError(w, http.StatusBadRequest, "key is required")
			return
		}

		if err := DeleteModulePreference(db, user.ID, moduleName, body.Key); err != nil {
			if err == sql.ErrNoRows {
				writeError(w, http.StatusNotFound, "preference not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "failed to delete preference")
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

// AllModulePreferencesHandler returns all module preferences for the user.
func AllModulePreferencesHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		prefs, err := GetAllModulePreferences(db, user.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load preferences")
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{"preferences": prefs})
	}
}
