package netatmo

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/encryption"
)

const (
	authURL  = "https://api.netatmo.com/oauth2/authorize"
	tokenURL = "https://api.netatmo.com/oauth2/token"

	// DefaultRedirectURL is the default OAuth2 callback URL.
	DefaultRedirectURL = "http://localhost:8080/api/netatmo/callback"
)

// NetatmoToken holds the OAuth2 tokens for a user.
type NetatmoToken struct {
	AccessToken  string
	RefreshToken string
	Expiry       time.Time
}

// IsExpired reports whether the access token needs refreshing.
// Tokens are considered expired 60 seconds before their actual expiry to
// avoid races.
func (t *NetatmoToken) IsExpired() bool {
	if t.Expiry.IsZero() {
		return false
	}
	return time.Now().After(t.Expiry.Add(-60 * time.Second))
}

// OAuthClient handles the Netatmo OAuth2 flow.
type OAuthClient struct {
	clientID     string
	clientSecret string
	redirectURL  string
	httpClient   *http.Client
}

// NewOAuthClient creates a new OAuthClient from explicit credentials.
func NewOAuthClient(clientID, clientSecret, redirectURL string) *OAuthClient {
	if redirectURL == "" {
		redirectURL = DefaultRedirectURL
	}
	return &OAuthClient{
		clientID:     clientID,
		clientSecret: clientSecret,
		redirectURL:  redirectURL,
		httpClient:   &http.Client{Timeout: 15 * time.Second},
	}
}

// ClientFromEnv creates an OAuthClient from environment variables:
// NETATMO_CLIENT_ID, NETATMO_CLIENT_SECRET, NETATMO_REDIRECT_URL.
func ClientFromEnv() *OAuthClient {
	redirectURL := os.Getenv("NETATMO_REDIRECT_URL")
	if redirectURL == "" {
		redirectURL = DefaultRedirectURL
	}
	return NewOAuthClient(
		os.Getenv("NETATMO_CLIENT_ID"),
		os.Getenv("NETATMO_CLIENT_SECRET"),
		redirectURL,
	)
}

// IsConfigured reports whether the client has the required credentials.
func (c *OAuthClient) IsConfigured() bool {
	return c.clientID != "" && c.clientSecret != ""
}

// AuthorizationURL returns the Netatmo OAuth2 authorization URL with the
// read_station scope and the provided state parameter.
func (c *OAuthClient) AuthorizationURL(state string) string {
	params := url.Values{}
	params.Set("client_id", c.clientID)
	params.Set("redirect_uri", c.redirectURL)
	params.Set("response_type", "code")
	params.Set("scope", "read_station")
	params.Set("state", state)
	return authURL + "?" + params.Encode()
}

// ExchangeCode exchanges an authorization code for tokens.
func (c *OAuthClient) ExchangeCode(ctx context.Context, code string) (*NetatmoToken, error) {
	data := url.Values{}
	data.Set("grant_type", "authorization_code")
	data.Set("client_id", c.clientID)
	data.Set("client_secret", c.clientSecret)
	data.Set("redirect_uri", c.redirectURL)
	data.Set("code", code)

	return c.doTokenRequest(ctx, data)
}

// RefreshToken exchanges a refresh token for a new access token.
func (c *OAuthClient) RefreshToken(ctx context.Context, refreshToken string) (*NetatmoToken, error) {
	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("client_id", c.clientID)
	data.Set("client_secret", c.clientSecret)
	data.Set("refresh_token", refreshToken)

	return c.doTokenRequest(ctx, data)
}

// doTokenRequest performs the actual token endpoint request.
func (c *OAuthClient) doTokenRequest(ctx context.Context, data url.Values) (*NetatmoToken, error) {
	if c.httpClient == nil {
		c.httpClient = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token request: %w", err)
	}
	defer resp.Body.Close()

	const maxBody int64 = 64 * 1024
	lr := &io.LimitedReader{R: resp.Body, N: maxBody + 1}
	body, err := io.ReadAll(lr)
	if err != nil {
		return nil, fmt.Errorf("read token response: %w", err)
	}
	if int64(len(body)) > maxBody {
		return nil, fmt.Errorf("token response too large")
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token endpoint HTTP %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		Error        string `json:"error"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("decode token response: %w", err)
	}
	if tokenResp.Error != "" {
		return nil, fmt.Errorf("netatmo token error: %s", tokenResp.Error)
	}
	if tokenResp.AccessToken == "" {
		return nil, fmt.Errorf("netatmo returned empty access token")
	}

	expiry := time.Time{}
	if tokenResp.ExpiresIn > 0 {
		expiry = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	}

	return &NetatmoToken{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		Expiry:       expiry,
	}, nil
}

// GetAccessToken returns a valid access token for userID, transparently
// refreshing if the stored token is expired. Returns ("", nil) if no token
// is stored for the user.
func (c *OAuthClient) GetAccessToken(ctx context.Context, db *sql.DB, userID int64) (string, error) {
	token, err := LoadToken(db, userID)
	if err != nil {
		return "", fmt.Errorf("load netatmo token: %w", err)
	}
	if token == nil {
		return "", nil
	}

	if !token.IsExpired() {
		return token.AccessToken, nil
	}

	if token.RefreshToken == "" {
		return "", fmt.Errorf("netatmo access token expired and no refresh token available")
	}

	refreshed, err := c.RefreshToken(ctx, token.RefreshToken)
	if err != nil {
		return "", fmt.Errorf("refresh netatmo token: %w", err)
	}

	// Preserve the refresh token from the stored token if the new one is empty.
	// Netatmo does not always return a new refresh token on refresh.
	if refreshed.RefreshToken == "" {
		refreshed.RefreshToken = token.RefreshToken
	}

	if err := SaveToken(db, userID, refreshed); err != nil {
		log.Printf("netatmo: failed to save refreshed token for user %d: %v", userID, err)
		return "", fmt.Errorf("save refreshed netatmo token: %w", err)
	}

	return refreshed.AccessToken, nil
}

// GenerateState creates a cryptographically random state token for CSRF protection.
func GenerateState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate state: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// --- Database operations ---

// SaveToken encrypts and upserts the Netatmo token for userID.
func SaveToken(db *sql.DB, userID int64, token *NetatmoToken) error {
	if token == nil {
		return fmt.Errorf("SaveToken: token must not be nil")
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
		INSERT INTO netatmo_oauth_tokens (user_id, access_token, refresh_token, expiry, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET
			access_token  = excluded.access_token,
			refresh_token = excluded.refresh_token,
			expiry        = excluded.expiry,
			updated_at    = excluded.updated_at`,
		userID, encAccess, encRefresh, expiry, now,
	)
	return err
}

// LoadToken retrieves and decrypts the Netatmo token for userID.
// Returns (nil, nil) if no token is stored.
func LoadToken(db *sql.DB, userID int64) (*NetatmoToken, error) {
	var encAccess, encRefresh, expiryStr string
	err := db.QueryRow(
		`SELECT access_token, refresh_token, expiry FROM netatmo_oauth_tokens WHERE user_id = ?`,
		userID,
	).Scan(&encAccess, &encRefresh, &expiryStr)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	accessToken, err := encryption.DecryptField(encAccess)
	if err != nil {
		return nil, fmt.Errorf("decrypt access token for user %d: %w", userID, err)
	}

	refreshToken, err := encryption.DecryptField(encRefresh)
	if err != nil {
		return nil, fmt.Errorf("decrypt refresh token for user %d: %w", userID, err)
	}

	var expiry time.Time
	if expiryStr != "" {
		if t, parseErr := time.Parse(time.RFC3339, expiryStr); parseErr == nil {
			expiry = t
		}
	}

	return &NetatmoToken{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		Expiry:       expiry,
	}, nil
}

// HasToken reports whether a token is stored for userID.
func HasToken(db *sql.DB, userID int64) (bool, error) {
	var count int
	err := db.QueryRow(
		`SELECT COUNT(*) FROM netatmo_oauth_tokens WHERE user_id = ?`,
		userID,
	).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// DeleteToken removes the stored token for userID.
func DeleteToken(db *sql.DB, userID int64) error {
	_, err := db.Exec(`DELETE FROM netatmo_oauth_tokens WHERE user_id = ?`, userID)
	return err
}
