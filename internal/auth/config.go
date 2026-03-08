package auth

import (
	"log"
	"os"
	"strings"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// Config holds the OAuth2 configuration and session settings.
type Config struct {
	OAuth2       *oauth2.Config
	BaseURL      string
	CookieSecure bool
}

// NewConfig creates an auth configuration from environment variables.
// Returns nil if GOOGLE_CLIENT_ID or GOOGLE_CLIENT_SECRET are not set,
// which disables Google OAuth (the server still runs without auth).
func NewConfig() *Config {
	clientID := os.Getenv("GOOGLE_CLIENT_ID")
	clientSecret := os.Getenv("GOOGLE_CLIENT_SECRET")

	if clientID == "" || clientSecret == "" {
		log.Println("GOOGLE_CLIENT_ID or GOOGLE_CLIENT_SECRET not set — OAuth disabled")
		return nil
	}

	baseURL := os.Getenv("BASE_URL")
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}

	cookieSecure := strings.HasPrefix(baseURL, "https")

	return &Config{
		OAuth2: &oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			RedirectURL:  baseURL + "/api/auth/google/callback",
			Scopes:       []string{"openid", "email", "profile"},
			Endpoint:     google.Endpoint,
		},
		BaseURL:      baseURL,
		CookieSecure: cookieSecure,
	}
}
