package auth

import (
	"database/sql"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/encryption"
)

// GoogleToken holds the OAuth2 tokens from Google for a user.
type GoogleToken struct {
	AccessToken  string
	RefreshToken string
	TokenType    string
	Expiry       time.Time
	Scopes       string
}

// IsExpired reports whether the access token needs refreshing.
// Tokens are considered expired 60 seconds before their actual expiry to
// avoid races with in-flight requests.
func (t *GoogleToken) IsExpired() bool {
	if t.Expiry.IsZero() {
		return false
	}
	return time.Now().After(t.Expiry.Add(-60 * time.Second))
}

// SaveGoogleToken encrypts and upserts the Google OAuth token for userID.
func SaveGoogleToken(db *sql.DB, userID int64, token *GoogleToken) error {
	if token == nil {
		return fmt.Errorf("SaveGoogleToken: token must not be nil")
	}
	encAccess, err := encryption.EncryptField(token.AccessToken)
	if err != nil {
		return fmt.Errorf("encrypt access token: %w", err)
	}
	encRefresh, err := encryption.EncryptField(token.RefreshToken)
	if err != nil {
		return fmt.Errorf("encrypt refresh token: %w", err)
	}

	expiry := ""
	if !token.Expiry.IsZero() {
		expiry = token.Expiry.UTC().Format(time.RFC3339)
	}
	now := time.Now().UTC().Format(time.RFC3339)

	_, err = db.Exec(`
		INSERT INTO google_tokens (user_id, access_token, refresh_token, token_type, expiry, scopes, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET
			access_token  = excluded.access_token,
			refresh_token = excluded.refresh_token,
			token_type    = excluded.token_type,
			expiry        = excluded.expiry,
			scopes        = excluded.scopes,
			updated_at    = excluded.updated_at`,
		userID, encAccess, encRefresh, token.TokenType, expiry, token.Scopes, now,
	)
	return err
}

// LoadGoogleToken retrieves and decrypts the Google token for userID.
// Returns (nil, nil) if no token is stored.
func LoadGoogleToken(db *sql.DB, userID int64) (*GoogleToken, error) {
	var encAccess, encRefresh, tokenType, expiryStr, scopes string
	err := db.QueryRow(
		`SELECT access_token, refresh_token, token_type, expiry, scopes FROM google_tokens WHERE user_id = ?`,
		userID,
	).Scan(&encAccess, &encRefresh, &tokenType, &expiryStr, &scopes)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	accessToken, err := encryption.DecryptField(encAccess)
	if err != nil {
		if strings.HasPrefix(encAccess, "enc:") {
			return nil, fmt.Errorf("decrypt access token for user %d: %w", userID, err)
		}
		log.Printf("google_token: decrypt access token warning (legacy plaintext) for user %d: %v", userID, err)
		accessToken = encAccess
	}

	refreshToken, err := encryption.DecryptField(encRefresh)
	if err != nil {
		if strings.HasPrefix(encRefresh, "enc:") {
			return nil, fmt.Errorf("decrypt refresh token for user %d: %w", userID, err)
		}
		log.Printf("google_token: decrypt refresh token warning (legacy plaintext) for user %d: %v", userID, err)
		refreshToken = encRefresh
	}

	var expiry time.Time
	if expiryStr != "" {
		if t, parseErr := time.Parse(time.RFC3339, expiryStr); parseErr == nil {
			expiry = t
		}
	}

	return &GoogleToken{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		TokenType:    tokenType,
		Expiry:       expiry,
		Scopes:       scopes,
	}, nil
}

// HasGoogleToken reports whether a Google token is stored for userID.
func HasGoogleToken(db *sql.DB, userID int64) (bool, error) {
	var count int
	err := db.QueryRow(
		`SELECT COUNT(*) FROM google_tokens WHERE user_id = ?`,
		userID,
	).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// DeleteGoogleToken removes the stored Google token for userID.
func DeleteGoogleToken(db *sql.DB, userID int64) error {
	_, err := db.Exec(`DELETE FROM google_tokens WHERE user_id = ?`, userID)
	return err
}
