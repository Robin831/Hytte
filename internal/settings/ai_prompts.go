package settings

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

// DefaultPromptBodies contains the hardcoded fallback prompt bodies used when a key
// is absent from the ai_prompts table (e.g. after a DELETE before the next server start).
var DefaultPromptBodies = map[string]string{
	"analysis":      "Classify this {sport} workout. Respond with ONLY a JSON object, no markdown formatting.",
	"comparison":    "Compare these two workouts and provide coaching insights. Respond with JSON only, no markdown.",
	"training_load": "Analyze this training period and provide structured coaching feedback. Respond with JSON only, no markdown.",
	"insights":      "Analyze this workout and provide coaching insights. Respond with JSON only, no markdown.",
}

// LoadPrompt loads the prompt body for the given key from the DB.
// If no row is found or a DB error occurs, defaultBody is returned.
func LoadPrompt(db *sql.DB, key, defaultBody string) string {
	var body string
	err := db.QueryRow(`SELECT prompt_body FROM ai_prompts WHERE prompt_key = ?`, key).Scan(&body)
	if err == sql.ErrNoRows {
		return defaultBody
	}
	if err != nil {
		log.Printf("LoadPrompt: query error for key %q: %v", key, err)
		return defaultBody
	}
	return body
}

// AIPrompt is the response shape for a single prompt entry.
type AIPrompt struct {
	Key       string `json:"key"`
	Body      string `json:"body"`
	IsDefault bool   `json:"is_default"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

// GetAIPromptsHandler handles GET /api/settings/ai-prompts.
// Returns all known prompt keys with their current body and a flag indicating
// whether the body matches the compiled-in default. Requires admin access.
func GetAIPromptsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := db.Query(`SELECT prompt_key, prompt_body, updated_at FROM ai_prompts ORDER BY prompt_key`)
		if err != nil {
			log.Printf("GetAIPromptsHandler: query: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load prompts"})
			return
		}
		defer rows.Close()

		dbPrompts := make(map[string]AIPrompt)
		for rows.Next() {
			var key, body, updatedAt string
			if err := rows.Scan(&key, &body, &updatedAt); err != nil {
				log.Printf("GetAIPromptsHandler: scan: %v", err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read prompts"})
				return
			}
			defaultBody, hasDefault := DefaultPromptBodies[key]
			isDefault := hasDefault && body == defaultBody
			dbPrompts[key] = AIPrompt{
				Key:       key,
				Body:      body,
				IsDefault: isDefault,
				UpdatedAt: updatedAt,
			}
		}
		if err := rows.Err(); err != nil {
			log.Printf("GetAIPromptsHandler: rows: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read prompts"})
			return
		}

		// Build result with stable ordering: collect all keys, sort, emit.
		allKeys := make([]string, 0, len(DefaultPromptBodies)+len(dbPrompts))
		seen := make(map[string]struct{}, len(DefaultPromptBodies))
		for key := range DefaultPromptBodies {
			allKeys = append(allKeys, key)
			seen[key] = struct{}{}
		}
		for key := range dbPrompts {
			if _, known := seen[key]; !known {
				allKeys = append(allKeys, key)
			}
		}
		sort.Strings(allKeys)

		result := make([]AIPrompt, 0, len(allKeys))
		for _, key := range allKeys {
			if p, ok := dbPrompts[key]; ok {
				result = append(result, p)
			} else {
				defaultBody := DefaultPromptBodies[key]
				result = append(result, AIPrompt{Key: key, Body: defaultBody, IsDefault: true})
			}
		}

		writeJSON(w, http.StatusOK, map[string]any{"prompts": result})
	}
}

// PutAIPromptHandler handles PUT /api/settings/ai-prompts/{key}.
// Upserts the prompt body for the given key. Requires admin access.
func PutAIPromptHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := chi.URLParam(r, "key")
		if key == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "key is required"})
			return
		}
		if _, ok := DefaultPromptBodies[key]; !ok {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown prompt key"})
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, 64*1024) // 64 KB limit
		var req struct {
			Body string `json:"body"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
			return
		}
		if strings.TrimSpace(req.Body) == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "body must not be empty"})
			return
		}

		now := time.Now().UTC().Format(time.RFC3339)
		_, err := db.Exec(
			`INSERT INTO ai_prompts (prompt_key, prompt_body, created_at, updated_at)
			 VALUES (?, ?, ?, ?)
			 ON CONFLICT(prompt_key) DO UPDATE SET prompt_body = excluded.prompt_body, updated_at = excluded.updated_at`,
			key, req.Body, now, now,
		)
		if err != nil {
			log.Printf("PutAIPromptHandler: upsert key %q: %v", key, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save prompt"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{"key": key, "body": req.Body})
	}
}

// DeleteAIPromptHandler handles DELETE /api/settings/ai-prompts/{key}.
// Removes the DB row for the given key, reverting prompt loading to the hardcoded
// default until the next server start re-seeds the row. Requires admin access.
func DeleteAIPromptHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := chi.URLParam(r, "key")
		if key == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "key is required"})
			return
		}

		// Only allow deletion of known built-in prompt keys.
		if _, ok := DefaultPromptBodies[key]; !ok {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown prompt key"})
			return
		}

		result, err := db.Exec(`DELETE FROM ai_prompts WHERE prompt_key = ?`, key)
		if err != nil {
			log.Printf("DeleteAIPromptHandler: delete key %q: %v", key, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete prompt"})
			return
		}
		n, _ := result.RowsAffected()
		if n == 0 {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "prompt key not found"})
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}
