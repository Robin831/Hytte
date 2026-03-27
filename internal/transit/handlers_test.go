package transit

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	_ "modernc.org/sqlite"
)

// setupTestDB creates an in-memory SQLite database with the tables required by transit handlers.
func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	_, err = db.Exec(`
		CREATE TABLE users (
			id         INTEGER PRIMARY KEY,
			email      TEXT UNIQUE NOT NULL,
			name       TEXT NOT NULL,
			picture    TEXT NOT NULL DEFAULT '',
			google_id  TEXT UNIQUE NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			is_admin   INTEGER NOT NULL DEFAULT 0
		);
		CREATE TABLE user_preferences (
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			key     TEXT NOT NULL,
			value   TEXT NOT NULL DEFAULT '',
			PRIMARY KEY (user_id, key)
		);
	`)
	if err != nil {
		t.Fatalf("create schema: %v", err)
	}
	_, err = db.Exec(`INSERT INTO users (email, name, google_id) VALUES ('test@example.com', 'Test', 'google-1')`)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	return db
}

// withTestUser injects a test user into the request context.
func withTestUser(r *http.Request, userID int64) *http.Request {
	user := &auth.User{ID: userID, Email: "test@example.com", Name: "Test"}
	return r.WithContext(auth.ContextWithUser(r.Context(), user))
}

// fakeEnturResponse returns a minimal valid GraphQL departure response.
func fakeEnturResponse() string {
	futureTime := time.Now().Add(10 * time.Minute).UTC().Format(time.RFC3339)
	return `{
		"data": {
			"stopPlace": {
				"name": "Bjørndalsbakken",
				"estimatedCalls": [
					{
						"expectedDepartureTime": "` + futureTime + `",
						"aimedDepartureTime":    "` + futureTime + `",
						"destinationDisplay": {"frontText": "Sentrum"},
						"serviceJourney": {
							"line": {"publicCode": "3"},
							"quay": {"publicCode": "A"}
						},
						"realtime": true
					}
				]
			}
		}
	}`
}

// fakeGeocoderResponse returns a minimal valid geocoder response.
func fakeGeocoderResponse() string {
	return `{
		"features": [
			{
				"properties": {
					"id":    "NSR:StopPlace:42175",
					"label": "Bjørndalsbakken, Bergen",
					"name":  "Bjørndalsbakken"
				}
			}
		]
	}`
}

// --- DeparturesHandler ---

func TestDeparturesHandler_DefaultStops(t *testing.T) {
	enturServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(fakeEnturResponse()))
	}))
	defer enturServer.Close()

	db := setupTestDB(t)
	defer db.Close()

	svc := newTestService(enturServer.URL, "http://unused")
	handler := DeparturesHandler(db, svc)

	req := httptest.NewRequest("GET", "/api/transit/departures", nil)
	req = withTestUser(req, 1)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Stops []StopDepartures `json:"stops"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Stops) != 2 {
		t.Errorf("expected 2 default stops, got %d", len(body.Stops))
	}
}

func TestDeparturesHandler_ExplicitStops(t *testing.T) {
	enturServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(fakeEnturResponse()))
	}))
	defer enturServer.Close()

	db := setupTestDB(t)
	defer db.Close()

	svc := newTestService(enturServer.URL, "http://unused")
	handler := DeparturesHandler(db, svc)

	req := httptest.NewRequest("GET", "/api/transit/departures?stops=NSR:StopPlace:42175", nil)
	req = withTestUser(req, 1)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Stops []StopDepartures `json:"stops"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Stops) != 1 {
		t.Errorf("expected 1 stop, got %d", len(body.Stops))
	}
	if len(body.Stops[0].Departures) != 1 {
		t.Errorf("expected 1 departure, got %d", len(body.Stops[0].Departures))
	}
}

func TestDeparturesHandler_UpstreamError_ReturnsEmptyDepartures(t *testing.T) {
	enturServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer enturServer.Close()

	db := setupTestDB(t)
	defer db.Close()

	svc := newTestService(enturServer.URL, "http://unused")
	handler := DeparturesHandler(db, svc)

	req := httptest.NewRequest("GET", "/api/transit/departures?stops=NSR:StopPlace:42175", nil)
	req = withTestUser(req, 1)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Handler must succeed even when upstream is down — returns stop with empty departures.
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on upstream error, got %d", rec.Code)
	}

	var body struct {
		Stops []StopDepartures `json:"stops"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Stops) != 1 {
		t.Fatalf("expected 1 stop entry, got %d", len(body.Stops))
	}
	if len(body.Stops[0].Departures) != 0 {
		t.Errorf("expected 0 departures on upstream error, got %d", len(body.Stops[0].Departures))
	}
}

func TestDeparturesHandler_StaleCache_ServedOnUpstreamFailure(t *testing.T) {
	callCount := 0
	enturServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(fakeEnturResponse()))
			return
		}
		// Subsequent calls fail.
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer enturServer.Close()

	db := setupTestDB(t)
	defer db.Close()

	svc := newTestService(enturServer.URL, "http://unused")
	stopID := "NSR:StopPlace:42175"

	// Prime the cache.
	_, deps, err := svc.FetchDepartures(context.Background(), stopID)
	if err != nil {
		t.Fatalf("first fetch: %v", err)
	}
	if len(deps) == 0 {
		t.Fatal("expected departures on first fetch")
	}

	// Expire the cache.
	svc.mu.Lock()
	svc.cache[stopID].expires = time.Now().Add(-1 * time.Second)
	svc.mu.Unlock()

	// Second fetch: upstream fails — should return stale data.
	_, deps2, err := svc.FetchDepartures(context.Background(), stopID)
	if err != nil {
		t.Fatalf("expected stale fallback, got error: %v", err)
	}
	if len(deps2) == 0 {
		t.Error("expected stale departures returned on upstream failure")
	}
}

// --- SearchHandler ---

func TestSearchHandler_MissingQuery(t *testing.T) {
	svc := newTestService("http://unused", "http://unused")
	handler := SearchHandler(svc)

	req := httptest.NewRequest("GET", "/api/transit/search", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["error"] == "" {
		t.Error("expected error message")
	}
}

func TestSearchHandler_QueryTooLong(t *testing.T) {
	svc := newTestService("http://unused", "http://unused")
	handler := SearchHandler(svc)

	req := httptest.NewRequest("GET", "/api/transit/search?q="+strings.Repeat("a", 101), nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestSearchHandler_Success(t *testing.T) {
	geocoderServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("text") == "" {
			t.Error("expected text param forwarded to geocoder")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(fakeGeocoderResponse()))
	}))
	defer geocoderServer.Close()

	svc := newTestService("http://unused", geocoderServer.URL)
	handler := SearchHandler(svc)

	req := httptest.NewRequest("GET", "/api/transit/search?q=Bjorn", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Results []GeocoderResult `json:"results"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Results) != 1 {
		t.Errorf("expected 1 result, got %d", len(body.Results))
	}
	if body.Results[0].ID != "NSR:StopPlace:42175" {
		t.Errorf("unexpected stop ID: %s", body.Results[0].ID)
	}
}

func TestSearchHandler_UpstreamError(t *testing.T) {
	geocoderServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer geocoderServer.Close()

	svc := newTestService("http://unused", geocoderServer.URL)
	handler := SearchHandler(svc)

	req := httptest.NewRequest("GET", "/api/transit/search?q=Oslo", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 on upstream error, got %d", rec.Code)
	}
}

// --- SettingsGetHandler / SettingsPutHandler ---

func TestSettingsGetHandler_DefaultsWhenNoPrefs(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	handler := SettingsGetHandler(db)

	req := httptest.NewRequest("GET", "/api/transit/settings", nil)
	req = withTestUser(req, 1)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body struct {
		Stops []FavoriteStop `json:"stops"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Stops) != len(defaultStops) {
		t.Errorf("expected %d default stops, got %d", len(defaultStops), len(body.Stops))
	}
}

func TestSettingsPutHandler_RoundTrip(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	putHandler := SettingsPutHandler(db)
	getHandler := SettingsGetHandler(db)

	stops := []FavoriteStop{
		{ID: "NSR:StopPlace:99999", Name: "Test Stop", Routes: []string{"1", "2"}},
	}
	payload, _ := json.Marshal(map[string]any{"stops": stops})

	putReq := httptest.NewRequest("PUT", "/api/transit/settings", strings.NewReader(string(payload)))
	putReq.Header.Set("Content-Type", "application/json")
	putReq = withTestUser(putReq, 1)
	putRec := httptest.NewRecorder()
	putHandler.ServeHTTP(putRec, putReq)

	if putRec.Code != http.StatusOK {
		t.Fatalf("PUT expected 200, got %d: %s", putRec.Code, putRec.Body.String())
	}

	// Verify round-trip via GET.
	getReq := httptest.NewRequest("GET", "/api/transit/settings", nil)
	getReq = withTestUser(getReq, 1)
	getRec := httptest.NewRecorder()
	getHandler.ServeHTTP(getRec, getReq)

	var body struct {
		Stops []FavoriteStop `json:"stops"`
	}
	if err := json.NewDecoder(getRec.Body).Decode(&body); err != nil {
		t.Fatalf("decode GET: %v", err)
	}
	if len(body.Stops) != 1 {
		t.Fatalf("expected 1 stop after PUT, got %d", len(body.Stops))
	}
	if body.Stops[0].ID != "NSR:StopPlace:99999" {
		t.Errorf("unexpected stop ID: %s", body.Stops[0].ID)
	}
	if len(body.Stops[0].Routes) != 2 {
		t.Errorf("expected 2 routes, got %d", len(body.Stops[0].Routes))
	}
}

func TestSettingsPutHandler_InvalidBody(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	handler := SettingsPutHandler(db)

	req := httptest.NewRequest("PUT", "/api/transit/settings", strings.NewReader("not json"))
	req = withTestUser(req, 1)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}
