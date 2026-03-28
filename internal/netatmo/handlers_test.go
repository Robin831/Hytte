package netatmo

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	_ "modernc.org/sqlite"
)

// setupHandlerTestDB opens an in-memory SQLite database with the tables needed
// to test the Netatmo HTTP handlers.
func setupHandlerTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	_, err = db.Exec(`
		CREATE TABLE users (
			id      INTEGER PRIMARY KEY,
			email   TEXT NOT NULL,
			name    TEXT NOT NULL DEFAULT ''
		);
		CREATE TABLE netatmo_readings (
			id          INTEGER PRIMARY KEY,
			user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			timestamp   TEXT NOT NULL,
			module_type TEXT NOT NULL,
			metric      TEXT NOT NULL,
			value       REAL NOT NULL
		);
		CREATE INDEX idx_netatmo_readings_user_ts ON netatmo_readings(user_id, timestamp);
		INSERT INTO users (id, email) VALUES (1, 'test@example.com');
	`)
	if err != nil {
		t.Fatalf("create schema: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	t.Cleanup(func() { db.Close() })
	return db
}

// injectCache pre-populates the client cache so GetStationsData returns cached
// data without making a real HTTP request.
func injectCache(c *Client, userID int64, readings *ModuleReadings) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache[userID] = cacheEntry{readings: readings, fetchedAt: time.Now()}
}

func requestWithUser(userID int64) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	u := &auth.User{ID: userID}
	return r.WithContext(auth.ContextWithUser(r.Context(), u))
}

func newStubClient(db *sql.DB) *Client {
	return &Client{
		oauth:      &OAuthClient{},
		db:         db,
		cache:      make(map[int64]cacheEntry),
		httpClient: nil, // not used when cache is populated
	}
}

func TestCurrentHandler_Success(t *testing.T) {
	sqlDB := setupHandlerTestDB(t)

	readings := &ModuleReadings{
		Indoor:    &IndoorReadings{Temperature: 21.5, Humidity: 50, CO2: 800, Noise: 35, Pressure: 1013.0},
		FetchedAt: time.Now(),
	}
	client := newStubClient(sqlDB)
	injectCache(client, 1, readings)

	h := CurrentHandler(client, sqlDB)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, requestWithUser(1))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var got ModuleReadings
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Indoor == nil {
		t.Fatal("expected indoor readings in response")
	}
	if got.Indoor.Temperature != 21.5 {
		t.Errorf("temperature: got %v, want 21.5", got.Indoor.Temperature)
	}
}

func TestCurrentHandler_APIError(t *testing.T) {
	sqlDB := setupHandlerTestDB(t)

	// Client with no cache entry and no configured OAuth/token storage in the
	// test DB — GetStationsData will fail when trying to access Netatmo data.
	client := newStubClient(sqlDB)

	h := CurrentHandler(client, sqlDB)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, requestWithUser(99))

	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHistoryHandler_Success(t *testing.T) {
	sqlDB := setupHandlerTestDB(t)

	// Pre-populate history with a stored reading.
	seed := ModuleReadings{
		Indoor:    &IndoorReadings{Temperature: 20.0, Humidity: 45},
		FetchedAt: time.Now().Add(-time.Hour),
	}
	if err := StoreReadings(sqlDB, 1, seed); err != nil {
		t.Fatalf("store seed: %v", err)
	}

	readings := &ModuleReadings{
		Indoor:    &IndoorReadings{Temperature: 21.0, Humidity: 48},
		FetchedAt: time.Now(),
	}
	client := newStubClient(sqlDB)
	injectCache(client, 1, readings)

	h := HistoryHandler(client, sqlDB)
	w := httptest.NewRecorder()
	req := requestWithUser(1)
	req.URL.RawQuery = "hours=24"
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var got struct {
		Readings []Reading `json:"readings"`
	}
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(got.Readings) == 0 {
		t.Fatal("expected readings in response, got none")
	}
}

func TestHistoryHandler_InvalidHoursDefaultsTo24(t *testing.T) {
	sqlDB := setupHandlerTestDB(t)

	readings := &ModuleReadings{
		Indoor:    &IndoorReadings{Temperature: 22.0},
		FetchedAt: time.Now(),
	}
	client := newStubClient(sqlDB)
	injectCache(client, 1, readings)

	h := HistoryHandler(client, sqlDB)
	w := httptest.NewRecorder()
	req := requestWithUser(1)
	req.URL.RawQuery = "hours=notanumber"
	h.ServeHTTP(w, req)

	// Should default to 24 hours and succeed.
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}
