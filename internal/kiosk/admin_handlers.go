package kiosk

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/go-chi/chi/v5"
)

// createTokenRequest is the request body for POST /api/kiosk/tokens.
type createTokenRequest struct {
	Name      string          `json:"name"`
	Config    json.RawMessage `json:"config"`
	ExpiresAt string          `json:"expires_at"` // RFC3339 or empty for no expiry
}

// tokenResponse is the response body for token list entries.
// The raw token is only returned on creation; thereafter only metadata is exposed.
type tokenResponse struct {
	ID         int64   `json:"id"`
	Name       string  `json:"name"`
	Config     any     `json:"config"`
	CreatedBy  string  `json:"created_by"`
	CreatedAt  string  `json:"created_at"`
	ExpiresAt  *string `json:"expires_at"`
	LastUsedAt *string `json:"last_used_at"`
}

// createTokenResponse extends tokenResponse with the plaintext token returned once on creation.
type createTokenResponse struct {
	tokenResponse
	Token string `json:"token"`
}

// CreateTokenHandler handles POST /api/kiosk/tokens.
// Generates a crypto/rand token, stores its SHA-256 hash, and returns the plaintext token once.
func CreateTokenHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if user == nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, 64*1024) // 64 KB max
		var req createTokenRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}

		if req.Name == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
			return
		}

		// Validate config JSON if provided; default to empty object.
		configStr := "{}"
		if len(req.Config) > 0 && string(req.Config) != "null" {
			var check any
			if err := json.Unmarshal(req.Config, &check); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "config must be valid JSON"})
				return
			}
			configStr = string(req.Config)
		}

		// Validate expires_at if provided.
		var expiresAt *string
		if req.ExpiresAt != "" {
			t, err := time.Parse(time.RFC3339, req.ExpiresAt)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "expires_at must be RFC3339"})
				return
			}
			s := t.UTC().Format(time.RFC3339)
			expiresAt = &s
		}

		now := time.Now().UTC().Format(time.RFC3339)

		// Generate a 32-byte (64 hex char) cryptographically random token.
		// Retry up to 3 times in the unlikely event of a hash collision.
		rawBytes := make([]byte, 32)
		var plaintext string
		var id int64
		var insertErr error
		for i := 0; i < 3; i++ {
			if _, err := rand.Read(rawBytes); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to generate token"})
				return
			}
			plaintext = hex.EncodeToString(rawBytes)
			hash := hashToken(plaintext)

			insertErr = db.QueryRowContext(r.Context(),
				`INSERT INTO kiosk_tokens (token_hash, name, config, created_by, created_at, expires_at)
				 VALUES (?, ?, ?, ?, ?, ?)
				 RETURNING id`,
				hash, req.Name, configStr, user.Email, now, expiresAt,
			).Scan(&id)
			if insertErr == nil {
				break
			}
			// Retry only on UNIQUE constraint violations (hash collision).
			if !strings.Contains(insertErr.Error(), "UNIQUE") {
				break
			}
		}
		if insertErr != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create token"})
			return
		}

		var cfg any
		_ = json.Unmarshal([]byte(configStr), &cfg)

		resp := createTokenResponse{
			tokenResponse: tokenResponse{
				ID:        id,
				Name:      req.Name,
				Config:    cfg,
				CreatedBy: user.Email,
				CreatedAt: now,
				ExpiresAt: expiresAt,
			},
			Token: plaintext,
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		writeJSON(w, http.StatusCreated, resp)
	}
}

// ListTokensHandler handles GET /api/kiosk/tokens.
// Returns all tokens with metadata; never returns the token hash or plaintext.
func ListTokensHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := db.QueryContext(r.Context(),
			`SELECT id, name, config, created_by, created_at, expires_at, last_used_at
			 FROM kiosk_tokens
			 ORDER BY created_at DESC, id DESC`,
		)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list tokens"})
			return
		}
		defer rows.Close()

		tokens := []tokenResponse{}
		for rows.Next() {
			var (
				id         int64
				name       string
				configRaw  string
				createdBy  string
				createdAt  string
				expiresAt  sql.NullString
				lastUsedAt sql.NullString
			)
			if err := rows.Scan(&id, &name, &configRaw, &createdBy, &createdAt, &expiresAt, &lastUsedAt); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read token"})
				return
			}

			var cfg any
			_ = json.Unmarshal([]byte(configRaw), &cfg)

			var expiresAtPtr *string
			if expiresAt.Valid && expiresAt.String != "" {
				s := expiresAt.String
				expiresAtPtr = &s
			}
			var lastUsedAtPtr *string
			if lastUsedAt.Valid && lastUsedAt.String != "" {
				s := lastUsedAt.String
				lastUsedAtPtr = &s
			}

			tokens = append(tokens, tokenResponse{
				ID:         id,
				Name:       name,
				Config:     cfg,
				CreatedBy:  createdBy,
				CreatedAt:  createdAt,
				ExpiresAt:  expiresAtPtr,
				LastUsedAt: lastUsedAtPtr,
			})
		}
		if err := rows.Err(); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list tokens"})
			return
		}

		writeJSON(w, http.StatusOK, tokens)
	}
}

// DeleteTokenHandler handles DELETE /api/kiosk/tokens/{id}.
// Permanently removes the token record.
func DeleteTokenHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil || id <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid token id"})
			return
		}

		result, err := db.ExecContext(r.Context(),
			"DELETE FROM kiosk_tokens WHERE id = ?", id,
		)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete token"})
			return
		}

		rows, err := result.RowsAffected()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to confirm deletion"})
			return
		}
		if rows == 0 {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "token not found"})
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

// updateDimRequest is the request body for PUT /api/kiosk/tokens/{id}/dim.
// Dim is a tri-state: null (or absent) clears the override so the screen
// follows the sun, true/false is stored as-is. Empty DimStart/DimEnd clear the
// custom window; otherwise both must be a zero-padded local "HH:MM".
type updateDimRequest struct {
	Dim      *bool  `json:"dim"`
	DimStart string `json:"dim_start"`
	DimEnd   string `json:"dim_end"`
}

// UpdateTokenDimHandler handles PUT /api/kiosk/tokens/{id}/dim.
// Replaces the night-mode override keys ("dim", "dim_start", "dim_end") in the
// token's config while leaving every other config key untouched, and returns
// the updated config.
func UpdateTokenDimHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil || id <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid token id"})
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, 4*1024)
		var req updateDimRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}

		startRaw := strings.TrimSpace(req.DimStart)
		endRaw := strings.TrimSpace(req.DimEnd)
		if (startRaw == "") != (endRaw == "") {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "dim_start and dim_end must be set together"})
			return
		}
		var start, end string
		if startRaw != "" {
			var startOK, endOK bool
			start, startOK = parseHHMM(startRaw)
			end, endOK = parseHHMM(endRaw)
			if !startOK || !endOK {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "dim_start and dim_end must be HH:MM"})
				return
			}
		}

		tx, err := db.BeginTx(r.Context(), nil)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update token"})
			return
		}
		defer tx.Rollback()

		var configRaw string
		err = tx.QueryRowContext(r.Context(),
			"SELECT config FROM kiosk_tokens WHERE id = ?", id,
		).Scan(&configRaw)
		if err == sql.ErrNoRows {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "token not found"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update token"})
			return
		}

		// Refuse to rewrite a config that is not a JSON object: replacing it
		// with a fresh object would silently drop whatever it held.
		cfg := map[string]any{}
		if strings.TrimSpace(configRaw) != "" {
			if err := json.Unmarshal([]byte(configRaw), &cfg); err != nil || cfg == nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "stored token config is not a JSON object"})
				return
			}
		}

		if req.Dim == nil {
			delete(cfg, "dim")
		} else {
			cfg["dim"] = *req.Dim
		}
		if start == "" {
			delete(cfg, "dim_start")
			delete(cfg, "dim_end")
		} else {
			cfg["dim_start"] = start
			cfg["dim_end"] = end
		}

		updated, err := json.Marshal(cfg)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update token"})
			return
		}
		if _, err := tx.ExecContext(r.Context(),
			"UPDATE kiosk_tokens SET config = ? WHERE id = ?", string(updated), id,
		); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update token"})
			return
		}
		if err := tx.Commit(); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update token"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{"config": cfg})
	}
}
