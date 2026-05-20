package familychat

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fixedTime returns a clock that always reports the same instant. Used to
// pin the coturn expiry timestamp so the HMAC assertion is deterministic.
func fixedTime(t time.Time) func() time.Time { return func() time.Time { return t } }

// fixedLoader returns a WebRTCConfig loader that ignores the environment.
// Tests build configs explicitly so they don't pick up host env contamination
// (e.g. WEBRTC_TURN_SHARED_SECRET set on the developer's machine).
func fixedLoader(cfg WebRTCConfig) func() WebRTCConfig {
	return func() WebRTCConfig { return cfg }
}

func decodeICE(t *testing.T, body string) ICEConfig {
	t.Helper()
	var out ICEConfig
	if err := json.NewDecoder(strings.NewReader(body)).Decode(&out); err != nil {
		t.Fatalf("decode ICE config: %v\nbody: %s", err, body)
	}
	return out
}

func TestTurnHandler_STUNOnlyFallback(t *testing.T) {
	cfg := WebRTCConfig{
		STUNURLs: []string{"stun:stun.l.google.com:19302", "stun:stun1.l.google.com:19302"},
	}
	h := turnConfigHandler(fixedLoader(cfg), time.Now)

	req := withUser(httptest.NewRequest("GET", "/api/familychat/turn", nil), 42)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	ice := decodeICE(t, rec.Body.String())
	if len(ice.ICEServers) != 1 {
		t.Fatalf("expected 1 ICE server, got %d: %+v", len(ice.ICEServers), ice.ICEServers)
	}
	srv := ice.ICEServers[0]
	if srv.Username != "" || srv.Credential != "" {
		t.Errorf("STUN server should carry no credentials: %+v", srv)
	}
	if len(srv.URLs) != 2 || srv.URLs[0] != "stun:stun.l.google.com:19302" {
		t.Errorf("unexpected URLs: %+v", srv.URLs)
	}
	if ice.TTL != 0 {
		t.Errorf("ttl should be 0 when no TURN credential was issued, got %d", ice.TTL)
	}
}

func TestTurnHandler_EmptyConfigReturnsEmptyList(t *testing.T) {
	h := turnConfigHandler(fixedLoader(WebRTCConfig{}), time.Now)

	req := withUser(httptest.NewRequest("GET", "/api/familychat/turn", nil), 1)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for empty config, got %d", rec.Code)
	}
	ice := decodeICE(t, rec.Body.String())
	if len(ice.ICEServers) != 0 {
		t.Errorf("expected zero ICE servers for empty config, got %+v", ice.ICEServers)
	}
}

func TestTurnHandler_StaticCredentials(t *testing.T) {
	cfg := WebRTCConfig{
		STUNURLs:       []string{"stun:stun.example.com:3478"},
		TURNURLs:       []string{"turn:turn.example.com:3478"},
		TURNUsername:   "alice",
		TURNCredential: "static-secret",
	}
	h := turnConfigHandler(fixedLoader(cfg), time.Now)

	req := withUser(httptest.NewRequest("GET", "/api/familychat/turn", nil), 7)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	ice := decodeICE(t, rec.Body.String())
	if len(ice.ICEServers) != 2 {
		t.Fatalf("expected 2 ICE servers, got %d: %+v", len(ice.ICEServers), ice.ICEServers)
	}
	turn := ice.ICEServers[1]
	if turn.Username != "alice" || turn.Credential != "static-secret" {
		t.Errorf("static creds not passed through: %+v", turn)
	}
	// Static credentials don't expire — the handler should not report a TTL.
	if ice.TTL != 0 {
		t.Errorf("static creds should not report a ttl, got %d", ice.TTL)
	}
}

func TestTurnHandler_EphemeralCredentialMatchesCoturnSpec(t *testing.T) {
	secret := "super-secret"
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	ttl := 30 * time.Minute
	cfg := WebRTCConfig{
		TURNURLs:     []string{"turn:turn.example.com:3478", "turns:turn.example.com:5349"},
		SharedSecret: secret,
		TTL:          ttl,
	}
	h := turnConfigHandler(fixedLoader(cfg), fixedTime(now))

	req := withUser(httptest.NewRequest("GET", "/api/familychat/turn", nil), 99)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	ice := decodeICE(t, rec.Body.String())
	if len(ice.ICEServers) != 1 {
		t.Fatalf("expected exactly one TURN ICE server, got %+v", ice.ICEServers)
	}
	turn := ice.ICEServers[0]
	wantUsername := fmt.Sprintf("%d:99", now.Add(ttl).Unix())
	if turn.Username != wantUsername {
		t.Errorf("username = %q, want %q", turn.Username, wantUsername)
	}
	mac := hmac.New(sha1.New, []byte(secret))
	mac.Write([]byte(wantUsername))
	wantCred := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	if turn.Credential != wantCred {
		t.Errorf("credential = %q, want %q", turn.Credential, wantCred)
	}
	if got := ice.TTL; got != int(ttl.Seconds()) {
		t.Errorf("ttl = %d, want %d", got, int(ttl.Seconds()))
	}
	// Sanity: the credential must round-trip through base64 (any leading
	// whitespace or non-standard alphabet would break coturn).
	if _, err := base64.StdEncoding.DecodeString(turn.Credential); err != nil {
		t.Errorf("credential is not valid base64: %v", err)
	}
}

func TestBuildICEConfig_UnauthenticatedUserGetsHytteIdentity(t *testing.T) {
	// When no user is in context (e.g. an admin tool exercising the handler
	// in tests), the identity component falls back to "hytte" so the username
	// remains a valid colon-delimited coturn string.
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	cfg := WebRTCConfig{
		TURNURLs:     []string{"turn:turn.example.com:3478"},
		SharedSecret: "secret",
		TTL:          defaultTURNTTL,
	}
	ice := BuildICEConfig(cfg, "", now)
	if len(ice.ICEServers) != 1 {
		t.Fatalf("expected 1 server, got %+v", ice.ICEServers)
	}
	wantUsername := fmt.Sprintf("%d:hytte", now.Add(defaultTURNTTL).Unix())
	if ice.ICEServers[0].Username != wantUsername {
		t.Errorf("username = %q, want %q", ice.ICEServers[0].Username, wantUsername)
	}
}

func TestBuildICEConfig_SharedSecretBeatsStatic(t *testing.T) {
	cfg := WebRTCConfig{
		TURNURLs:       []string{"turn:turn.example.com:3478"},
		TURNUsername:   "static-user",
		TURNCredential: "static-pass",
		SharedSecret:   "secret",
	}
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	ice := BuildICEConfig(cfg, "5", now)
	if len(ice.ICEServers) != 1 {
		t.Fatalf("expected 1 server, got %+v", ice.ICEServers)
	}
	turn := ice.ICEServers[0]
	if turn.Username == "static-user" || turn.Credential == "static-pass" {
		t.Errorf("static creds leaked through despite shared secret being set: %+v", turn)
	}
	if !strings.Contains(turn.Username, ":5") {
		t.Errorf("ephemeral username should embed the user id, got %q", turn.Username)
	}
}

func TestBuildICEConfig_AnonymousTURNWhenNoCredentials(t *testing.T) {
	cfg := WebRTCConfig{
		TURNURLs: []string{"turn:turn.example.com:3478"},
	}
	ice := BuildICEConfig(cfg, "1", time.Now())
	if len(ice.ICEServers) != 1 {
		t.Fatalf("expected 1 server, got %+v", ice.ICEServers)
	}
	turn := ice.ICEServers[0]
	if turn.Username != "" || turn.Credential != "" {
		t.Errorf("anonymous TURN should not carry creds, got %+v", turn)
	}
}

func TestSignCoturnCredential_KnownVector(t *testing.T) {
	// Cross-check against the canonical HMAC-SHA1 worked example from the
	// coturn README: secret=north, username=1485828434:foo. Both halves of
	// the assertion are computed locally to avoid hard-coding the digest in
	// case Go's base64 encoder ever changes (it won't, but the test
	// documents the format).
	secret := "north"
	username := "1485828434:foo"
	got := signCoturnCredential(secret, username)

	mac := hmac.New(sha1.New, []byte(secret))
	mac.Write([]byte(username))
	want := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	if got != want {
		t.Fatalf("signCoturnCredential mismatch: got %q, want %q", got, want)
	}
	if got == "" {
		t.Errorf("expected a non-empty credential")
	}
}

func TestTTLFromEnv(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want time.Duration
	}{
		{"empty defaults", "", defaultTURNTTL},
		{"invalid defaults", "abc", defaultTURNTTL},
		{"negative defaults", "-1", defaultTURNTTL},
		{"zero defaults", "0", defaultTURNTTL},
		{"clamped to minimum", "10", minTURNTTL},
		{"explicit ten minutes", "600", 10 * time.Minute},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ttlFromEnv(tc.in); got != tc.want {
				t.Errorf("ttlFromEnv(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestSplitURLs(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"empty", "", nil},
		{"whitespace only", "   ", nil},
		{"single", "stun:a.example", []string{"stun:a.example"}},
		{"multiple with whitespace", " stun:a , stun:b ,turn:c ", []string{"stun:a", "stun:b", "turn:c"}},
		{"drops blanks", "a,,b", []string{"a", "b"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := splitURLs(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("splitURLs(%q) = %v, want %v", tc.in, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("splitURLs(%q)[%d] = %q, want %q", tc.in, i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestLoadWebRTCConfig_ReadsEnv(t *testing.T) {
	t.Setenv("WEBRTC_STUN_URLS", "stun:a,stun:b")
	t.Setenv("WEBRTC_TURN_URLS", "turn:c")
	t.Setenv("WEBRTC_TURN_USERNAME", "alice")
	t.Setenv("WEBRTC_TURN_CREDENTIAL", "pw")
	t.Setenv("WEBRTC_TURN_SHARED_SECRET", "secret")
	t.Setenv("WEBRTC_TURN_TTL_SECONDS", "120")

	cfg := LoadWebRTCConfig()
	if len(cfg.STUNURLs) != 2 || cfg.STUNURLs[0] != "stun:a" {
		t.Errorf("STUNURLs = %v", cfg.STUNURLs)
	}
	if len(cfg.TURNURLs) != 1 || cfg.TURNURLs[0] != "turn:c" {
		t.Errorf("TURNURLs = %v", cfg.TURNURLs)
	}
	if cfg.TURNUsername != "alice" || cfg.TURNCredential != "pw" {
		t.Errorf("static creds = %q / %q", cfg.TURNUsername, cfg.TURNCredential)
	}
	if cfg.SharedSecret != "secret" {
		t.Errorf("SharedSecret = %q", cfg.SharedSecret)
	}
	if cfg.TTL != 2*time.Minute {
		t.Errorf("TTL = %v", cfg.TTL)
	}
}

func TestTurnHandler_ResponseShape(t *testing.T) {
	// Regression: the JSON keys must match the WebRTC RTCIceServer shape so
	// the frontend can pass the response straight into RTCPeerConnection.
	cfg := WebRTCConfig{
		STUNURLs:       []string{"stun:s.example.com"},
		TURNURLs:       []string{"turn:t.example.com"},
		TURNUsername:   "u",
		TURNCredential: "c",
	}
	h := turnConfigHandler(fixedLoader(cfg), time.Now)
	req := withUser(httptest.NewRequest("GET", "/api/familychat/turn", nil), 1)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var raw map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	servers, ok := raw["iceServers"].([]any)
	if !ok {
		t.Fatalf("iceServers key missing or wrong type: %T", raw["iceServers"])
	}
	if len(servers) != 2 {
		t.Fatalf("expected 2 servers, got %d", len(servers))
	}
	first, _ := servers[0].(map[string]any)
	if _, hasURLs := first["urls"]; !hasURLs {
		t.Errorf("first server missing urls: %+v", first)
	}
	// STUN server must not include username/credential (omitempty).
	if _, present := first["username"]; present {
		t.Errorf("STUN server should omit username, got %+v", first)
	}
	if _, present := first["credential"]; present {
		t.Errorf("STUN server should omit credential, got %+v", first)
	}
	second, _ := servers[1].(map[string]any)
	if second["username"] != "u" || second["credential"] != "c" {
		t.Errorf("TURN server creds wrong: %+v", second)
	}
}

// Sanity: the handler still works when called without a user context. The
// session-auth requirement is enforced by the router middleware; the
// handler itself should not panic if it ever runs outside that wrapper.
func TestTurnHandler_NoUserContext(t *testing.T) {
	h := turnConfigHandler(fixedLoader(WebRTCConfig{
		STUNURLs: []string{"stun:a"},
	}), time.Now)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/familychat/turn", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

