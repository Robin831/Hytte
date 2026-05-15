package pokemon

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/training"
	"github.com/go-chi/chi/v5"
)

// pngMagic is the 8-byte signature that http.DetectContentType uses to
// recognise image/png. Embedding it directly keeps the test fixtures byte-
// exact and avoids needing a real PNG file on disk.
var pngMagic = []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}

// jpegMagic is the leading marker bytes for image/jpeg.
var jpegMagic = []byte{0xFF, 0xD8, 0xFF, 0xE0}

// enableClaudeForUser flips the per-user claude flag so the scan handler does
// not bail with "claude is not enabled". The CLI path stays "claude" so the
// real binary is never invoked — the test seam intercepts the call first.
func enableClaudeForUser(t *testing.T, db *sql.DB, userID int64) {
	t.Helper()
	if err := auth.SetPreference(db, userID, "claude_enabled", "true"); err != nil {
		t.Fatalf("enable claude: %v", err)
	}
}

// stubScanPrompt replaces scanRunPromptFn with a fixed-response stub and
// restores the original on cleanup. The returned counter tracks how many
// times the stub was invoked, which lets tests assert that the DB lookup is
// skipped when confidence is below the threshold.
func stubScanPrompt(t *testing.T, response string, stubErr error) *atomic.Int32 {
	t.Helper()
	orig := scanRunPromptFn
	calls := new(atomic.Int32)
	scanRunPromptFn = func(_ context.Context, _ *training.ClaudeConfig, _, _ string) (string, error) {
		calls.Add(1)
		return response, stubErr
	}
	t.Cleanup(func() { scanRunPromptFn = orig })
	return calls
}

// buildScanRequest creates a POST /api/pokemon/scan multipart/form-data
// request with the supplied bytes attached as the `image` field. filename is
// arbitrary; the handler sniffs the bytes for content-type detection.
func buildScanRequest(t *testing.T, payload []byte, filename string) *http.Request {
	t.Helper()
	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	part, err := w.CreateFormFile("image", filename)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write(payload); err != nil {
		t.Fatalf("write form payload: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close multipart: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/pokemon/scan", body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	return req
}

func TestScanHandler_MatchedCandidate(t *testing.T) {
	db := setupTestDB(t)
	seedCatalogue(t, db)
	u := seedUser(t, db, 1, "scan@example.com")
	enableClaudeForUser(t, db, u.ID)

	stubScanPrompt(t, `{"set_name":"Scarlet & Violet Base","set_id_hint":"sv1","collector_number":"025","confidence":0.95}`, nil)

	req := buildScanRequest(t, append(pngMagic, []byte("padding")...), "card.png")
	req = asUser(req, u)
	rec := httptest.NewRecorder()

	ScanHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := decode[struct {
		Matched    bool            `json:"matched"`
		Confidence float64         `json:"confidence"`
		Candidates []ScanCandidate `json:"candidates"`
	}](t, rec)
	if !body.Matched {
		t.Fatalf("expected matched=true, got %+v", body)
	}
	if body.Confidence != 0.95 {
		t.Errorf("expected confidence=0.95, got %v", body.Confidence)
	}
	if len(body.Candidates) != 1 || body.Candidates[0].Card.ID != "sv1-25" {
		t.Errorf("expected single sv1-25 candidate, got %+v", body.Candidates)
	}
	if body.Candidates[0].Set == nil || body.Candidates[0].Set.ID != "sv1" {
		t.Errorf("expected embedded set sv1, got %+v", body.Candidates[0].Set)
	}
}

func TestScanHandler_LowConfidenceSkipsLookup(t *testing.T) {
	db := setupTestDB(t)
	// Intentionally do NOT seed cards — a DB lookup would error out if reached.
	u := seedUser(t, db, 1, "scan@example.com")
	enableClaudeForUser(t, db, u.ID)

	calls := stubScanPrompt(t, `{"set_name":"Scarlet & Violet Base","set_id_hint":"","collector_number":"025","confidence":0.30}`, nil)

	req := buildScanRequest(t, append(pngMagic, []byte("padding")...), "card.png")
	req = asUser(req, u)
	rec := httptest.NewRecorder()
	ScanHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 even on low confidence, got %d: %s", rec.Code, rec.Body.String())
	}
	body := decode[struct {
		Matched         bool    `json:"matched"`
		Confidence      float64 `json:"confidence"`
		Reason          string  `json:"reason"`
		SetName         string  `json:"set_name"`
		CollectorNumber string  `json:"collector_number"`
	}](t, rec)
	if body.Matched {
		t.Errorf("expected matched=false on low confidence, got true")
	}
	if body.Confidence != 0.30 {
		t.Errorf("expected confidence=0.30, got %v", body.Confidence)
	}
	if body.Reason == "" {
		t.Errorf("expected a reason string, got empty")
	}
	if body.SetName != "Scarlet & Violet Base" {
		t.Errorf("expected set_name to pass through, got %q", body.SetName)
	}
	if body.CollectorNumber != "025" {
		t.Errorf("expected collector_number to pass through, got %q", body.CollectorNumber)
	}
	if calls.Load() != 1 {
		t.Errorf("expected exactly one Claude call, got %d", calls.Load())
	}
}

func TestScanHandler_MalformedJSON(t *testing.T) {
	db := setupTestDB(t)
	seedCatalogue(t, db)
	u := seedUser(t, db, 1, "scan@example.com")
	enableClaudeForUser(t, db, u.ID)

	stubScanPrompt(t, "I am not JSON", nil)

	req := buildScanRequest(t, append(pngMagic, []byte("p")...), "card.png")
	req = asUser(req, u)
	rec := httptest.NewRecorder()
	ScanHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 even on malformed JSON, got %d: %s", rec.Code, rec.Body.String())
	}
	body := decode[struct {
		Matched bool   `json:"matched"`
		Reason  string `json:"reason"`
	}](t, rec)
	if body.Matched {
		t.Errorf("expected matched=false on malformed JSON, got true")
	}
	if !strings.Contains(body.Reason, "parse") {
		t.Errorf("expected reason to mention parse failure, got %q", body.Reason)
	}
}

func TestScanHandler_MultipleCandidates(t *testing.T) {
	db := setupTestDB(t)
	seedCatalogue(t, db)
	u := seedUser(t, db, 1, "scan@example.com")
	enableClaudeForUser(t, db, u.ID)

	// Both sv1-25 (Pikachu, 025) and swsh1-25 (Pikachu V, 025) share the same
	// collector number. With no set hint and no set name match, both should
	// come back as candidates.
	stubScanPrompt(t, `{"set_name":"unknown set","set_id_hint":"","collector_number":"025","confidence":0.80}`, nil)

	req := buildScanRequest(t, append(pngMagic, []byte("p")...), "card.png")
	req = asUser(req, u)
	rec := httptest.NewRecorder()
	ScanHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := decode[struct {
		Matched    bool            `json:"matched"`
		Candidates []ScanCandidate `json:"candidates"`
	}](t, rec)
	if !body.Matched {
		t.Fatalf("expected matched=true, got %+v", body)
	}
	if len(body.Candidates) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(body.Candidates))
	}
	ids := map[string]bool{}
	for _, c := range body.Candidates {
		ids[c.Card.ID] = true
	}
	if !ids["sv1-25"] || !ids["swsh1-25"] {
		t.Errorf("expected both Pikachu cards in candidates, got %v", ids)
	}
}

func TestScanHandler_UnsupportedMIME(t *testing.T) {
	db := setupTestDB(t)
	u := seedUser(t, db, 1, "scan@example.com")
	enableClaudeForUser(t, db, u.ID)

	// Plain text gets sniffed as text/plain.
	req := buildScanRequest(t, []byte("not an image"), "fake.png")
	req = asUser(req, u)
	rec := httptest.NewRecorder()
	ScanHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestScanHandler_TooLarge(t *testing.T) {
	db := setupTestDB(t)
	u := seedUser(t, db, 1, "scan@example.com")
	enableClaudeForUser(t, db, u.ID)

	// 5 MB + 1 byte of JPEG-flavoured padding. Multipart parser still accepts
	// the request (parser limit is 10 MB); the handler's own size check fires.
	payload := append([]byte{}, jpegMagic...)
	for len(payload) <= scanMaxImageBytes {
		payload = append(payload, 0x00)
	}

	req := buildScanRequest(t, payload, "huge.jpg")
	req = asUser(req, u)
	rec := httptest.NewRecorder()
	ScanHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestScanHandler_MissingFile(t *testing.T) {
	db := setupTestDB(t)
	u := seedUser(t, db, 1, "scan@example.com")
	enableClaudeForUser(t, db, u.ID)

	// Multipart body with no image part.
	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	if err := w.WriteField("other", "value"); err != nil {
		t.Fatalf("write field: %v", err)
	}
	w.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/pokemon/scan", body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req = asUser(req, u)
	rec := httptest.NewRecorder()
	ScanHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestScanHandler_StripsCodeFence(t *testing.T) {
	db := setupTestDB(t)
	seedCatalogue(t, db)
	u := seedUser(t, db, 1, "scan@example.com")
	enableClaudeForUser(t, db, u.ID)

	// Claude occasionally wraps the answer in a markdown fence despite the
	// "no markdown fence" instruction. The handler must still parse this.
	stubScanPrompt(t, "```json\n{\"set_name\":\"Scarlet & Violet Base\",\"set_id_hint\":\"sv1\",\"collector_number\":\"025\",\"confidence\":0.9}\n```", nil)

	req := buildScanRequest(t, append(pngMagic, []byte("p")...), "card.png")
	req = asUser(req, u)
	rec := httptest.NewRecorder()
	ScanHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := decode[struct {
		Matched    bool            `json:"matched"`
		Candidates []ScanCandidate `json:"candidates"`
	}](t, rec)
	if !body.Matched || len(body.Candidates) != 1 || body.Candidates[0].Card.ID != "sv1-25" {
		t.Errorf("expected sv1-25 match through code fence, got %+v", body)
	}
}

func TestScanHandler_NoCardMatch(t *testing.T) {
	db := setupTestDB(t)
	seedCatalogue(t, db)
	u := seedUser(t, db, 1, "scan@example.com")
	enableClaudeForUser(t, db, u.ID)

	stubScanPrompt(t, `{"set_name":"Scarlet & Violet Base","set_id_hint":"sv1","collector_number":"999","confidence":0.92}`, nil)

	req := buildScanRequest(t, append(pngMagic, []byte("p")...), "card.png")
	req = asUser(req, u)
	rec := httptest.NewRecorder()
	ScanHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := decode[struct {
		Matched         bool    `json:"matched"`
		Confidence      float64 `json:"confidence"`
		Reason          string  `json:"reason"`
		SetName         string  `json:"set_name"`
		CollectorNumber string  `json:"collector_number"`
	}](t, rec)
	if body.Matched {
		t.Errorf("expected matched=false when no card matches, got true")
	}
	if body.Confidence != 0.92 {
		t.Errorf("expected confidence to pass through, got %v", body.Confidence)
	}
	if !strings.Contains(body.Reason, "collector '999'") {
		t.Errorf("expected reason to include collector number, got %q", body.Reason)
	}
	if body.SetName != "Scarlet & Violet Base" {
		t.Errorf("expected set_name to pass through, got %q", body.SetName)
	}
	if body.CollectorNumber != "999" {
		t.Errorf("expected collector_number to pass through, got %q", body.CollectorNumber)
	}
}

// TestScanHandler_FeatureGate asserts the route is wired up behind
// RequireFeature(db, "pokemon"). A non-admin user with the feature enabled
// must be allowed through (kids are the primary scanner users), and an admin
// without any explicit feature flag also passes (admins bypass feature checks
// by design).
//
// The router is built using RegisterRoutes — the same helper that the
// production API router calls — so this test would catch any accidental
// removal of the feature gate from the shared registration code.
func TestScanHandler_FeatureGate(t *testing.T) {
	db := setupTestDB(t)
	seedCatalogue(t, db)

	// Non-admin kid with the pokemon feature ON but no admin flag.
	if _, err := db.Exec(`
		INSERT INTO users (id, email, name, google_id, is_admin)
		VALUES (1, 'kid@example.com', 'Kid', 'g-kid', 0)
	`); err != nil {
		t.Fatalf("seed kid: %v", err)
	}
	if err := auth.SetUserFeature(db, 1, "pokemon", true); err != nil {
		t.Fatalf("enable pokemon for kid: %v", err)
	}
	enableClaudeForUser(t, db, 1)

	// Admin with no explicit pokemon flag — admins bypass feature checks.
	if _, err := db.Exec(`
		INSERT INTO users (id, email, name, google_id, is_admin)
		VALUES (2, 'admin@example.com', 'Admin', 'g-admin', 1)
	`); err != nil {
		t.Fatalf("seed admin: %v", err)
	}
	enableClaudeForUser(t, db, 2)

	kidToken, _, err := auth.CreateSession(db, 1)
	if err != nil {
		t.Fatalf("create kid session: %v", err)
	}
	adminToken, _, err := auth.CreateSession(db, 2)
	if err != nil {
		t.Fatalf("create admin session: %v", err)
	}

	stubScanPrompt(t, `{"set_name":"Scarlet & Violet Base","set_id_hint":"sv1","collector_number":"025","confidence":0.95}`, nil)

	// Build the router using RegisterRoutes — the same function the production
	// API router calls — so changes to the gate or admin check are caught here.
	r := chi.NewRouter()
	r.Route("/api", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(auth.RequireAuth(db))
			r.Use(auth.WithFeatures(db))
			RegisterRoutes(r, db)
		})
	})

	makeReq := func(token string) *http.Request {
		req := buildScanRequest(t, append(pngMagic, []byte("p")...), "card.png")
		req.AddCookie(&http.Cookie{Name: "session", Value: token})
		return req
	}

	// Kid (non-admin) with pokemon=true → 200 because the feature gate is the
	// only requirement; kids are the primary scanner users.
	recKid := httptest.NewRecorder()
	r.ServeHTTP(recKid, makeReq(kidToken))
	if recKid.Code != http.StatusOK {
		t.Errorf("kid expected 200 on scan, got %d (%s)", recKid.Code, recKid.Body.String())
	}

	// Admin → 200.
	recAdmin := httptest.NewRecorder()
	r.ServeHTTP(recAdmin, makeReq(adminToken))
	if recAdmin.Code != http.StatusOK {
		t.Errorf("admin expected 200, got %d (%s)", recAdmin.Code, recAdmin.Body.String())
	}

	// User with no pokemon flag AND no admin flag → 403 from the feature gate.
	if _, err := db.Exec(`
		INSERT INTO users (id, email, name, google_id, is_admin)
		VALUES (3, 'no-feat@example.com', 'NoFeat', 'g-nofeat', 0)
	`); err != nil {
		t.Fatalf("seed no-feat user: %v", err)
	}
	noFeatToken, _, err := auth.CreateSession(db, 3)
	if err != nil {
		t.Fatalf("create no-feat session: %v", err)
	}
	recNoFeat := httptest.NewRecorder()
	r.ServeHTTP(recNoFeat, makeReq(noFeatToken))
	if recNoFeat.Code != http.StatusForbidden {
		t.Errorf("no-feat user expected 403, got %d (%s)", recNoFeat.Code, recNoFeat.Body.String())
	}
}

// TestParseClaudeScanResult exercises the JSON parser directly against the
// payload variants we expect to see in production: a strict object, an
// accidental markdown fence, and a malformed string.
func TestParseClaudeScanResult(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		wantErr bool
		conf    float64
	}{
		{
			name:    "strict json",
			raw:     `{"set_name":"x","set_id_hint":"sv1","collector_number":"025","confidence":0.9}`,
			wantErr: false,
			conf:    0.9,
		},
		{
			name:    "code fence",
			raw:     "```json\n{\"set_name\":\"x\",\"set_id_hint\":\"sv1\",\"collector_number\":\"025\",\"confidence\":0.5}\n```",
			wantErr: false,
			conf:    0.5,
		},
		{
			name:    "garbage",
			raw:     "i give up",
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, err := parseClaudeScanResult(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %+v", r)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if r.Confidence != tc.conf {
				t.Errorf("expected confidence %v, got %v", tc.conf, r.Confidence)
			}
		})
	}
}

// TestScanCandidate_JSON verifies ScanCandidate round-trips through JSON
// symmetrically. A change to any exported field name or type would break this
// check and signal an API shape regression.
func TestScanCandidate_JSON(t *testing.T) {
	c := ScanCandidate{
		Card:  CardDTO{ID: "sv1-25", SetID: "sv1", Name: "Pikachu", Variants: []VariantDTO{}},
		Set:   &SetDTO{ID: "sv1", Name: "SV Base"},
		Score: 0.91,
	}
	out, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got ScanCandidate
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Card.ID != c.Card.ID || got.Score != c.Score || got.Set == nil || got.Set.ID != c.Set.ID {
		t.Errorf("round-trip mismatch: got %+v, want %+v", got, c)
	}
}
