package training

import (
	"bytes"
	"context"
	"database/sql"
	"log"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
)

// ClaudeTestHandler verifies the Claude CLI is available and working.
// POST /api/settings/claude-test
func ClaudeTestHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		if !user.IsAdmin {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "Claude AI features are restricted to admin users"})
			return
		}

		cfg, err := LoadClaudeConfig(db, user.ID)
		if err != nil {
			log.Printf("Failed to load claude config: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{
				"ok":    false,
				"error": "failed to load claude configuration",
			})
			return
		}

		if !cfg.Enabled {
			writeJSON(w, http.StatusOK, map[string]any{
				"ok":    false,
				"error": "Claude is not enabled — enable it first in settings",
			})
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()

		cmd := exec.CommandContext(ctx, cfg.CLIPath, "--version")
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		if err := cmd.Run(); err != nil {
			errMsg := strings.TrimSpace(stderr.String())
			if errMsg == "" {
				errMsg = err.Error()
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"ok":    false,
				"error": "Claude CLI not reachable: " + errMsg,
			})
			return
		}

		version := strings.TrimSpace(stdout.String())
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":      true,
			"version": version,
		})
	}
}

