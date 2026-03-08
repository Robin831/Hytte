package auth

import (
	"os"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// Config returns the Google OAuth2 configuration from environment variables.
// Required env vars: GOOGLE_CLIENT_ID, GOOGLE_CLIENT_SECRET, GOOGLE_REDIRECT_URL.
func Config() *oauth2.Config {
	redirectURL := os.Getenv("GOOGLE_REDIRECT_URL")
	if redirectURL == "" {
		redirectURL = "http://localhost:8080/api/auth/google/callback"
	}

	return &oauth2.Config{
		ClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
		ClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
		RedirectURL:  redirectURL,
		Scopes:       []string{"openid", "email", "profile"},
		Endpoint:     google.Endpoint,
	}
}
