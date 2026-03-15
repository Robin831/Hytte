package infra

import (
	"database/sql"
	"time"
)

// ModuleConfig represents a user's configuration for an infra module.
type ModuleConfig struct {
	UserID    int64  `json:"user_id"`
	Module    string `json:"module"`
	Enabled   bool   `json:"enabled"`
	UpdatedAt string `json:"updated_at"`
}

// GetModuleConfigs returns all module configs for a user.
func GetModuleConfigs(db *sql.DB, userID int64) ([]ModuleConfig, error) {
	rows, err := db.Query(
		`SELECT user_id, module, enabled, updated_at FROM infra_module_config WHERE user_id = ? ORDER BY module`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var configs []ModuleConfig
	for rows.Next() {
		var c ModuleConfig
		if err := rows.Scan(&c.UserID, &c.Module, &c.Enabled, &c.UpdatedAt); err != nil {
			return nil, err
		}
		configs = append(configs, c)
	}
	return configs, rows.Err()
}

// IsModuleEnabled checks if a module is enabled for a user.
// Returns true by default if no config exists (modules are opt-out).
func IsModuleEnabled(db *sql.DB, userID int64, module string) (bool, error) {
	var enabled bool
	err := db.QueryRow(
		`SELECT enabled FROM infra_module_config WHERE user_id = ? AND module = ?`,
		userID, module,
	).Scan(&enabled)
	if err == sql.ErrNoRows {
		return true, nil // enabled by default
	}
	return enabled, err
}

// SetModuleEnabled enables or disables a module for a user.
func SetModuleEnabled(db *sql.DB, userID int64, module string, enabled bool) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(
		`INSERT INTO infra_module_config (user_id, module, enabled, updated_at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(user_id, module) DO UPDATE SET enabled = excluded.enabled, updated_at = excluded.updated_at`,
		userID, module, enabled, now,
	)
	return err
}
