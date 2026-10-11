package auth

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func loginRedirect(t *testing.T, target string) url.Values {
	t.Helper()
	t.Setenv("GOOGLE_CLIENT_ID", "client-id")
	rec := httptest.NewRecorder()
	GoogleLoginHandler()(rec, httptest.NewRequest(http.MethodGet, target, nil))
	if rec.Code != http.StatusTemporaryRedirect {
		t.Fatalf("status = %d", rec.Code)
	}
	u, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	return u.Query()
}

func TestSignInAsksOnlyForIdentity(t *testing.T) {
	q := loginRedirect(t, "/api/auth/google/login")
	if strings.Contains(q.Get("scope"), "calendar") {
		t.Fatalf("a plain sign-in must not ask for calendar access (Google warns on sensitive scopes): %q", q.Get("scope"))
	}
	if q.Get("scope") != "openid email profile" || q.Get("include_granted_scopes") != "true" || q.Get("prompt") != "" {
		t.Fatalf("plain sign-in = %v", q)
	}
}

func TestConnectingCalendarAsksForIt(t *testing.T) {
	for _, target := range []string{"/api/auth/google/login?calendar=1", "/api/auth/google/login?prompt=consent"} {
		q := loginRedirect(t, target)
		if !strings.Contains(q.Get("scope"), "auth/calendar.readonly") || !strings.Contains(q.Get("scope"), "auth/calendar.events") ||
			q.Get("prompt") != "consent" || q.Get("access_type") != "offline" {
			t.Fatalf("%s = %v", target, q)
		}
	}
}
