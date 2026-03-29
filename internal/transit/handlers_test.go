package transit

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	_ "modernc.org/sqlite"
)

// setupTestDB creates an in-memory SQLite database with the tables required by transit handlers.
// It constrains the connection pool to 1 connection so all operations share the same in-memory DB,
// and registers a t.Cleanup to close the DB when the test finishes.
func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	t.Cleanup(func() {
		_ = db.Close()
	})
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
						"quay": {"publicCode": "A"},
						"serviceJourney": {
							"line": {"publicCode": "3"}
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
	if len(body.Stops) != 0 {
		t.Errorf("expected 0 default stops (no defaults configured), got %d", len(body.Stops))
	}
}

func TestDeparturesHandler_ExplicitStops(t *testing.T) {
	enturServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(fakeEnturResponse()))
	}))
	defer enturServer.Close()

	db := setupTestDB(t)

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

	dep := body.Stops[0].Departures[0]
	if dep.Platform != "A" {
		t.Errorf("expected platform %q, got %q", "A", dep.Platform)
	}
	if dep.Line != "3" {
		t.Errorf("expected line %q, got %q", "3", dep.Line)
	}
	if dep.Destination != "Sentrum" {
		t.Errorf("expected destination %q, got %q", "Sentrum", dep.Destination)
	}
}

func TestDeparturesHandler_UpstreamError_ReturnsEmptyDepartures(t *testing.T) {
	enturServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer enturServer.Close()

	db := setupTestDB(t)

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
	// Stop name must fall back to the stop ID when name is unavailable.
	if body.Stops[0].StopName != "NSR:StopPlace:42175" {
		t.Errorf("expected stop name to fall back to stop ID, got %q", body.Stops[0].StopName)
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

	svc := newTestService(enturServer.URL, "http://unused")
	stopID := "NSR:StopPlace:42175"

	// Prime the cache.
	_, deps, err := svc.FetchDepartures(context.Background(), stopID, numberOfDepartures)
	if err != nil {
		t.Fatalf("first fetch: %v", err)
	}
	if len(deps) == 0 {
		t.Fatal("expected departures on first fetch")
	}

	// Expire the cache.
	cacheKey := fmt.Sprintf("%s:%d", stopID, numberOfDepartures)
	svc.mu.Lock()
	svc.cache[cacheKey].expires = time.Now().Add(-1 * time.Second)
	svc.mu.Unlock()

	// Second fetch: upstream fails — should return stale data.
	_, deps2, err := svc.FetchDepartures(context.Background(), stopID, numberOfDepartures)
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

	handler := SettingsPutHandler(db)

	req := httptest.NewRequest("PUT", "/api/transit/settings", strings.NewReader("not json"))
	req = withTestUser(req, 1)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestDeparturesHandler_RouteFilter_UsesFilteredDepartureCount(t *testing.T) {
	var capturedCount int
	enturServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Decode the GraphQL POST body to extract the count variable.
		var reqBody struct {
			Variables struct {
				Count int `json:"count"`
			} `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Errorf("failed to decode request body: %v", err)
		}
		capturedCount = reqBody.Variables.Count
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(fakeEnturResponse()))
	}))
	defer enturServer.Close()

	db := setupTestDB(t)

	// Save a stop with route filters into user preferences.
	stops := []FavoriteStop{
		{ID: "NSR:StopPlace:42175", Name: "Bjørndalsbakken", Routes: []string{"3", "4"}},
	}
	stopsJSON, _ := json.Marshal(stops)
	_, err := db.Exec(
		`INSERT INTO user_preferences (user_id, key, value) VALUES (1, ?, ?)`,
		transitStopsPreferenceKey, string(stopsJSON),
	)
	if err != nil {
		t.Fatalf("insert preference: %v", err)
	}

	svc := newTestService(enturServer.URL, "http://unused")
	handler := DeparturesHandler(db, svc)

	req := httptest.NewRequest("GET", "/api/transit/departures", nil)
	req = withTestUser(req, 1)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if capturedCount != filteredDepartureCount {
		t.Errorf("expected Entur request count=%d for filtered stop, got %d", filteredDepartureCount, capturedCount)
	}
}

func TestSettingsPutHandler_TooManyStops(t *testing.T) {
	db := setupTestDB(t)

	handler := SettingsPutHandler(db)

	stops := make([]FavoriteStop, maxTransitStops+1)
	for i := range stops {
		stops[i] = FavoriteStop{ID: "NSR:StopPlace:1", Name: "Stop"}
	}
	payload, _ := json.Marshal(map[string]any{"stops": stops})

	req := httptest.NewRequest("PUT", "/api/transit/settings", strings.NewReader(string(payload)))
	req.Header.Set("Content-Type", "application/json")
	req = withTestUser(req, 1)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for too many stops, got %d", rec.Code)
	}
}
