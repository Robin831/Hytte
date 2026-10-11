package auth

import (
	"os"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// Config returns the Google OAuth2 configuration from environment variables.
// Required env vars: GOOGLE_CLIENT_ID, GOOGLE_CLIENT_SECRET, GOOGLE_REDIRECT_URL.
//
// Scopes include calendar.readonly and calendar.events for Google Calendar
// integration. Existing users will need to re-consent after this change.
// CalendarScopes are requested only when a user connects their Google
// Calendar (login?calendar=1): Google treats them as sensitive and warns
// that the app is unverified, which a plain sign-in should not trigger.
var CalendarScopes = []string{
	"https://www.googleapis.com/auth/calendar.readonly",
	"https://www.googleapis.com/auth/calendar.events",
}

func Config() *oauth2.Config {
	redirectURL := os.Getenv("GOOGLE_REDIRECT_URL")
	if redirectURL == "" {
		redirectURL = "http://localhost:8080/api/auth/google/callback"
	}

	return &oauth2.Config{
		ClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
		ClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
		RedirectURL:  redirectURL,
		// Signing in asks only for who you are. These are not "sensitive"
		// scopes, so Google shows no unverified-app warning for them.
		Scopes: []string{
			"openid",
			"email",
			"profile",
		},
		Endpoint: google.Endpoint,
	}
}
