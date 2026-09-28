package livestream

import (
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/db"
	"github.com/Robin831/Hytte/internal/encryption"
	"github.com/Robin831/Hytte/internal/familychat"
	"github.com/go-chi/chi/v5"
)

const (
	ownerID  = int64(1)
	viewerID = int64(2)
	noFeatID = int64(3)
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	t.Setenv("ENCRYPTION_KEY", "test-key-for-livestream-tests")
	encryption.ResetEncryptionKey()
	t.Cleanup(func() { encryption.ResetEncryptionKey() })

	database, err := db.Init(":memory:")
	if err != nil {
		t.Fatalf("init test db: %v", err)
	}
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	t.Cleanup(func() { database.Close() })

	_, err = database.Exec(`INSERT INTO users (id, email, name, picture, google_id, created_at) VALUES
		(1, 'owner@example.com', 'Owner', '', 'g1', ''),
		(2, 'viewer@example.com', 'Viewer', '', 'g2', ''),
		(3, 'nofeat@example.com', 'NoFeature', '', 'g3', '')`)
	if err != nil {
		t.Fatalf("insert test users: %v", err)
	}
	return database
}

// fakeMediaMTX records what Hytte sends to MediaMTX and answers like it.
type fakeMediaMTX struct {
	mu       sync.Mutex
	requests []recordedRequest
	ready    bool
	kicked   []string
}

type recordedRequest struct {
	Method, Path, Query, User, Pass, Authorization, ContentType, Cookie, Body string
}

func (f *fakeMediaMTX) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, _ := r.BasicAuth()
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.requests = append(f.requests, recordedRequest{
			Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery, User: user, Pass: pass, Authorization: r.Header.Get("Authorization"),
			ContentType: r.Header.Get("Content-Type"), Cookie: r.Header.Get("Cookie"), Body: string(body),
		})
		ready := f.ready
		f.mu.Unlock()

		switch {
		case r.Method == http.MethodPost && (strings.HasSuffix(r.URL.Path, "/whip") || strings.HasSuffix(r.URL.Path, "/whep")):
			w.Header().Set("Content-Type", "application/sdp")
			w.Header().Set("ETag", "*")
			w.Header().Set("Accept-Patch", "application/trickle-ice-sdpfrag")
			w.Header().Set("Location", r.URL.Path+"/0b7c9a36-5a3e-4b9e-9d3c-1c2d3e4f5a6b")
			w.WriteHeader(http.StatusCreated)
			io.WriteString(w, "v=0\r\nanswer\r\n")
		case r.Method == http.MethodPatch || r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		case strings.HasPrefix(r.URL.Path, "/v3/paths/get/"):
			if !ready {
				http.Error(w, `{"error":"path not found"}`, http.StatusNotFound)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{
				"ready":  true,
				"source": map[string]string{"type": "webRTCSession", "id": "pub-1"},
				"readers": []map[string]string{
					{"type": "webRTCSession", "id": "read-1"},
					{"type": "hlsMuxer", "id": ""},
				},
			})
		case strings.Contains(r.URL.Path, "/kick/"):
			f.mu.Lock()
			f.kicked = append(f.kicked, r.URL.Path)
			f.mu.Unlock()
			w.WriteHeader(http.StatusOK)
		case strings.HasSuffix(r.URL.Path, ".m3u8"):
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			w.Header().Set("Cache-Control", "max-age=30")
			io.WriteString(w, "#EXTM3U\n")
		default:
			t.Logf("fake mediamtx: unexpected %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	})
}

func (f *fakeMediaMTX) last() recordedRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests[len(f.requests)-1]
}

type testEnv struct {
	db     *sql.DB
	h      *Handlers
	media  *fakeMediaMTX
	router http.Handler
	tokens map[int64]string
	now    time.Time
}

func setupEnv(t *testing.T) *testEnv {
	t.Helper()
	database := setupTestDB(t)
	fake := &fakeMediaMTX{}
	srv := httptest.NewServer(fake.handler(t))
	t.Cleanup(srv.Close)

	env := &testEnv{db: database, media: fake, tokens: map[int64]string{}, now: time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)}
	cfg := Config{
		WebRTCURL: srv.URL, HLSURL: srv.URL, APIURL: srv.URL, HLSSecret: "cdn-secret",
		PublishUser: "pub", PublishPass: "pubpass",
		ReadUser: "reader", ReadPass: "readpass",
		APIUser: "api", APIPass: "apipass",
	}
	ice := familychat.WebRTCConfig{
		STUNURLs:     []string{"stun:turn.example.com:3478"},
		TURNURLs:     []string{"turn:turn.example.com:3478"},
		SharedSecret: "s3cret",
		TTL:          time.Hour,
	}
	env.h = NewHandlers(database, cfg, ice)
	env.h.now = func() time.Time { return env.now }

	for _, id := range []int64{ownerID, viewerID, noFeatID} {
		if id != noFeatID {
			if err := auth.SetUserFeature(database, id, "livestream", true); err != nil {
				t.Fatalf("enable feature: %v", err)
			}
		}
		token, _, err := auth.CreateSession(database, id)
		if err != nil {
			t.Fatalf("create session: %v", err)
		}
		env.tokens[id] = token
	}

	r := chi.NewRouter()
	r.Route("/api", func(r chi.Router) {
		r.Use(auth.RequireAuth(database))
		r.Use(auth.WithFeatures(database))
		env.h.Mount(r)
	})
	env.router = r
	return env
}

func (e *testEnv) do(t *testing.T, userID int64, method, path, contentType, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if userID != 0 {
		req.AddCookie(&http.Cookie{Name: "session", Value: e.tokens[userID]})
	}
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	return rec
}

func (e *testEnv) goLive(t *testing.T, userID int64, title string) sessionResponse {
	t.Helper()
	rec := e.do(t, userID, http.MethodPost, "/api/live/sessions", "application/json", `{"title":"`+title+`"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create session: %d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Session sessionResponse `json:"session"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out.Session
}

func sessionPath(id int64, suffix string) string {
	return "/api/live/sessions/" + strconv.FormatInt(id, 10) + suffix
}

func TestCreateSessionEncryptsTitleAndHidesKey(t *testing.T) {
	env := setupEnv(t)
	s := env.goLive(t, ownerID, "Morning run")
	if s.Title != "Morning run" || s.Status != StatusLive || !s.IsOwner || s.OwnerName != "Owner" {
		t.Fatalf("unexpected session: %+v", s)
	}

	var rawTitle, key string
	if err := env.db.QueryRow(`SELECT title, stream_key FROM live_sessions WHERE id = ?`, s.ID).Scan(&rawTitle, &key); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(rawTitle, "enc:") {
		t.Errorf("title not encrypted at rest: %q", rawTitle)
	}
	if len(key) != 32 {
		t.Errorf("stream key should be 32 hex chars, got %q", key)
	}

	rec := env.do(t, viewerID, http.MethodGet, "/api/live/sessions", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), key) {
		t.Error("stream key leaked in list response")
	}
}

func TestCreateSessionEndsPreviousLiveSession(t *testing.T) {
	env := setupEnv(t)
	first := env.goLive(t, ownerID, "one")
	second := env.goLive(t, ownerID, "two")

	old, err := GetSession(env.db, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if old.Status != StatusEnded {
		t.Errorf("first session should be ended, got %q", old.Status)
	}
	live, err := ListLive(env.db)
	if err != nil {
		t.Fatal(err)
	}
	if len(live) != 1 || live[0].ID != second.ID {
		t.Fatalf("expected only the second session live, got %+v", live)
	}
}

func TestCreateSessionRejectsLongTitle(t *testing.T) {
	env := setupEnv(t)
	rec := env.do(t, ownerID, http.MethodPost, "/api/live/sessions", "application/json", `{"title":"`+strings.Repeat("x", maxTitleLen+1)+`"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestFeatureGateAndAuth(t *testing.T) {
	env := setupEnv(t)
	s := env.goLive(t, ownerID, "")

	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/live/sessions"},
		{http.MethodPost, "/api/live/sessions"},
		{http.MethodGet, "/api/live/ice"},
		{http.MethodPost, sessionPath(s.ID, "/whep")},
		{http.MethodGet, sessionPath(s.ID, "/hls/index.m3u8")},
	} {
		if rec := env.do(t, noFeatID, tc.method, tc.path, "", ""); rec.Code != http.StatusForbidden {
			t.Errorf("%s %s without feature: want 403, got %d", tc.method, tc.path, rec.Code)
		}
		if rec := env.do(t, 0, tc.method, tc.path, "", ""); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s unauthenticated: want 401, got %d", tc.method, tc.path, rec.Code)
		}
	}
}

func TestWHIPProxyOwnerOnlyWithInjectedCredentials(t *testing.T) {
	env := setupEnv(t)
	s := env.goLive(t, ownerID, "")

	// A viewer cannot publish into someone else's session.
	if rec := env.do(t, viewerID, http.MethodPost, sessionPath(s.ID, "/whip"), "application/sdp", "v=0\r\noffer\r\n"); rec.Code != http.StatusNotFound {
		t.Fatalf("non-owner WHIP: want 404, got %d", rec.Code)
	}

	rec := env.do(t, ownerID, http.MethodPost, sessionPath(s.ID, "/whip"), "application/sdp", "v=0\r\noffer\r\n")
	if rec.Code != http.StatusCreated {
		t.Fatalf("owner WHIP: %d %s", rec.Code, rec.Body.String())
	}
	up := env.media.last()
	var key string
	env.db.QueryRow(`SELECT stream_key FROM live_sessions WHERE id = ?`, s.ID).Scan(&key)
	if up.Path != "/live/"+key+"/whip" || up.User != "pub" || up.Pass != "pubpass" {
		t.Errorf("unexpected upstream request: %+v", up)
	}
	if up.Cookie != "" {
		t.Errorf("session cookie must not be forwarded to MediaMTX, got %q", up.Cookie)
	}
	if up.ContentType != "application/sdp" || !strings.Contains(up.Body, "offer") {
		t.Errorf("offer not forwarded: %+v", up)
	}
	wantLoc := sessionPath(s.ID, "/whip/0b7c9a36-5a3e-4b9e-9d3c-1c2d3e4f5a6b")
	if got := rec.Header().Get("Location"); got != wantLoc {
		t.Errorf("Location = %q, want %q", got, wantLoc)
	}
	if strings.Contains(rec.Header().Get("Location"), key) {
		t.Error("stream key leaked in Location")
	}
	if rec.Header().Get("Accept-Patch") == "" || rec.Body.String() != "v=0\r\nanswer\r\n" {
		t.Errorf("answer not relayed: headers=%v body=%q", rec.Header(), rec.Body.String())
	}

	// Trickle ICE and hang-up go to the MediaMTX resource.
	if rec := env.do(t, ownerID, http.MethodPatch, wantLoc, "application/trickle-ice-sdpfrag", "a=candidate"); rec.Code != http.StatusNoContent {
		t.Fatalf("PATCH: %d", rec.Code)
	}
	if up := env.media.last(); up.Method != http.MethodPatch || up.Path != "/live/"+key+"/whip/0b7c9a36-5a3e-4b9e-9d3c-1c2d3e4f5a6b" {
		t.Errorf("unexpected PATCH upstream: %+v", up)
	}
	if rec := env.do(t, ownerID, http.MethodPatch, sessionPath(s.ID, "/whip/..%2F..%2Fv3"), "application/trickle-ice-sdpfrag", ""); rec.Code != http.StatusBadRequest && rec.Code != http.StatusNotFound {
		t.Errorf("path traversal in resource id: want 400/404, got %d", rec.Code)
	}
}

func TestWHEPAnyFeatureUserUsesReaderCredentials(t *testing.T) {
	env := setupEnv(t)
	s := env.goLive(t, ownerID, "")
	rec := env.do(t, viewerID, http.MethodPost, sessionPath(s.ID, "/whep"), "application/sdp", "v=0\r\noffer\r\n")
	if rec.Code != http.StatusCreated {
		t.Fatalf("WHEP: %d %s", rec.Code, rec.Body.String())
	}
	if up := env.media.last(); up.User != "reader" || up.Pass != "readpass" || !strings.HasSuffix(up.Path, "/whep") {
		t.Errorf("unexpected upstream: %+v", up)
	}
}

func TestEndedSessionRejectsNewConnections(t *testing.T) {
	env := setupEnv(t)
	s := env.goLive(t, ownerID, "")
	env.media.ready = true

	if rec := env.do(t, viewerID, http.MethodPost, sessionPath(s.ID, "/end"), "", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("non-owner end: want 404, got %d", rec.Code)
	}
	if rec := env.do(t, ownerID, http.MethodPost, sessionPath(s.ID, "/end"), "", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("end: %d", rec.Code)
	}
	if len(env.media.kicked) != 2 {
		t.Errorf("expected publisher + WebRTC reader kicked, got %v", env.media.kicked)
	}

	for _, p := range []string{"/whip", "/whep"} {
		if rec := env.do(t, ownerID, http.MethodPost, sessionPath(s.ID, p), "application/sdp", "v=0"); rec.Code != http.StatusGone {
			t.Errorf("POST %s after end: want 410, got %d", p, rec.Code)
		}
	}
	if rec := env.do(t, viewerID, http.MethodGet, sessionPath(s.ID, "/hls/index.m3u8"), "", ""); rec.Code != http.StatusGone {
		t.Errorf("HLS after end: want 410, got %d", rec.Code)
	}
	// Hang-up is still allowed so clients can clean up.
	if rec := env.do(t, ownerID, http.MethodDelete, sessionPath(s.ID, "/whip/0b7c9a36-5a3e-4b9e-9d3c-1c2d3e4f5a6b"), "", ""); rec.Code != http.StatusNoContent {
		t.Errorf("DELETE after end: want 204, got %d", rec.Code)
	}
	if rec := env.do(t, ownerID, http.MethodPost, sessionPath(s.ID, "/heartbeat"), "", ""); rec.Code != http.StatusConflict {
		t.Errorf("heartbeat after end: want 409, got %d", rec.Code)
	}
	rec := env.do(t, viewerID, http.MethodGet, sessionPath(s.ID, ""), "", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"status":"ended"`) {
		t.Errorf("GET ended session: %d %s", rec.Code, rec.Body.String())
	}
}

func TestListReportsOnAirAndViewers(t *testing.T) {
	env := setupEnv(t)
	env.goLive(t, ownerID, "")
	env.media.ready = true
	rec := env.do(t, viewerID, http.MethodGet, "/api/live/sessions", "", "")
	var out struct {
		Sessions   []sessionResponse `json:"sessions"`
		Configured bool              `json:"configured"`
	}
	json.Unmarshal(rec.Body.Bytes(), &out)
	if !out.Configured || len(out.Sessions) != 1 {
		t.Fatalf("unexpected list: %s", rec.Body.String())
	}
	if s := out.Sessions[0]; !s.OnAir || s.Viewers != 1 || s.IsOwner {
		t.Errorf("unexpected session state: %+v", s)
	}
}

func TestHLSProxy(t *testing.T) {
	env := setupEnv(t)
	s := env.goLive(t, ownerID, "")
	rec := env.do(t, viewerID, http.MethodGet, sessionPath(s.ID, "/hls/index.m3u8")+"?_HLS_msn=5&_HLS_part=1", "", "")
	if rec.Code != http.StatusOK || rec.Body.String() != "#EXTM3U\n" {
		t.Fatalf("HLS: %d %q", rec.Code, rec.Body.String())
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "private, no-store" {
		t.Errorf("Cache-Control = %q, must not be cacheable", cc)
	}
	up := env.media.last()
	if up.Authorization != "Bearer cdn-secret" || up.Query != "_HLS_msn=5&_HLS_part=1" || !strings.HasSuffix(up.Path, "/index.m3u8") {
		t.Errorf("unexpected upstream: %+v", up)
	}
	if rec := env.do(t, viewerID, http.MethodGet, sessionPath(s.ID, "/hls/secret.txt"), "", ""); rec.Code != http.StatusNotFound {
		t.Errorf("non-HLS file: want 404, got %d", rec.Code)
	}
}

func TestHeartbeatAndReaper(t *testing.T) {
	env := setupEnv(t)
	s := env.goLive(t, ownerID, "")

	env.now = env.now.Add(90 * time.Second)
	if rec := env.do(t, ownerID, http.MethodPost, sessionPath(s.ID, "/heartbeat"), "", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("heartbeat: %d", rec.Code)
	}
	if rec := env.do(t, viewerID, http.MethodPost, sessionPath(s.ID, "/heartbeat"), "", ""); rec.Code != http.StatusNotFound {
		t.Errorf("non-owner heartbeat: want 404, got %d", rec.Code)
	}

	// 90s after the heartbeat: still fresh.
	env.now = env.now.Add(90 * time.Second)
	env.h.reapOnce()
	if got, _ := GetSession(env.db, s.ID); got.Status != StatusLive {
		t.Fatalf("session reaped too early")
	}

	// Past StaleAfter since the last heartbeat: ended.
	env.now = env.now.Add(StaleAfter)
	env.h.reapOnce()
	if got, _ := GetSession(env.db, s.ID); got.Status != StatusEnded {
		t.Fatalf("stale session not reaped, status %q", got.Status)
	}
}

func TestICEConfigHasEphemeralTURN(t *testing.T) {
	env := setupEnv(t)
	rec := env.do(t, viewerID, http.MethodGet, "/api/live/ice", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("ice: %d", rec.Code)
	}
	var cfg familychat.ICEConfig
	json.Unmarshal(rec.Body.Bytes(), &cfg)
	if len(cfg.ICEServers) != 2 || cfg.ICEServers[1].Credential == "" || !strings.HasSuffix(cfg.ICEServers[1].Username, ":live-2") || cfg.TTL != 3600 {
		t.Errorf("unexpected ICE config: %+v", cfg)
	}
}

func TestNotConfigured(t *testing.T) {
	env := setupEnv(t)
	env.h.cfg = Config{}
	if rec := env.do(t, ownerID, http.MethodPost, "/api/live/sessions", "", ""); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("create without media server: want 503, got %d", rec.Code)
	}
}
