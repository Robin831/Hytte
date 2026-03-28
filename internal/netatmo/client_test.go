package netatmo

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/encryption"
)

// approxEqual reports whether a and b are within a small epsilon of each other.
func approxEqual(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}

// stationsDataHandler returns a handler that serves a canned getstationsdata response.
func stationsDataHandler(t *testing.T) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			http.Error(w, "missing auth", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"status": "ok",
			"body": map[string]any{
				"devices": []any{
					map[string]any{
						"type": "NAMain",
						"dashboard_data": map[string]any{
							"Temperature": 21.3,
							"Humidity":    48,
							"CO2":         615,
							"Noise":       38,
							"Pressure":    1012.5,
						},
						"modules": []any{
							map[string]any{
								"type": "NAModule1",
								"dashboard_data": map[string]any{
									"Temperature": 6.7,
									"Humidity":    74,
								},
							},
							map[string]any{
								"type": "NAModule2",
								"dashboard_data": map[string]any{
									"WindStrength": 4.0,
									"WindGust":     8.5,
									"WindAngle":    225,
								},
							},
						},
					},
				},
			},
		})
	})
}

func TestGetStationsData(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", "test-key-netatmo-client-stations")
	encryption.ResetEncryptionKey()
	defer encryption.ResetEncryptionKey()

	srv := httptest.NewServer(stationsDataHandler(t))
	defer srv.Close()

	db := setupTestDB(t)
	defer db.Close()
	userID := insertTestUser(t, db)

	// Store a non-expired access token.
	tok := &NetatmoToken{
		AccessToken:  "test-access-token",
		RefreshToken: "test-refresh-token",
		Expiry:       time.Now().Add(1 * time.Hour),
	}
	if err := SaveToken(db, userID, tok); err != nil {
		t.Fatalf("SaveToken: %v", err)
	}

	oauth := &OAuthClient{
		clientID:     "id",
		clientSecret: "secret",
		redirectURL:  "http://localhost/cb",
		httpClient: &http.Client{
			Transport: &redirectTransport{base: srv.URL},
		},
	}

	client := &Client{
		oauth:      oauth,
		db:         db,
		httpClient: &http.Client{Transport: &redirectTransport{base: srv.URL}},
		cache:      make(map[int64]cacheEntry),
	}

	readings, err := client.GetStationsData(context.Background(), userID)
	if err != nil {
		t.Fatalf("GetStationsData: %v", err)
	}

	if readings.Indoor == nil {
		t.Fatal("Indoor readings should not be nil")
	}
	if !approxEqual(readings.Indoor.Temperature, 21.3) {
		t.Errorf("Indoor.Temperature: got %v, want 21.3", readings.Indoor.Temperature)
	}
	if readings.Indoor.Humidity != 48 {
		t.Errorf("Indoor.Humidity: got %v, want 48", readings.Indoor.Humidity)
	}
	if readings.Indoor.CO2 != 615 {
		t.Errorf("Indoor.CO2: got %v, want 615", readings.Indoor.CO2)
	}
	if readings.Indoor.Noise != 38 {
		t.Errorf("Indoor.Noise: got %v, want 38", readings.Indoor.Noise)
	}
	if !approxEqual(readings.Indoor.Pressure, 1012.5) {
		t.Errorf("Indoor.Pressure: got %v, want 1012.5", readings.Indoor.Pressure)
	}

	if readings.Outdoor == nil {
		t.Fatal("Outdoor readings should not be nil")
	}
	if !approxEqual(readings.Outdoor.Temperature, 6.7) {
		t.Errorf("Outdoor.Temperature: got %v, want 6.7", readings.Outdoor.Temperature)
	}
	if readings.Outdoor.Humidity != 74 {
		t.Errorf("Outdoor.Humidity: got %v, want 74", readings.Outdoor.Humidity)
	}

	if readings.Wind == nil {
		t.Fatal("Wind readings should not be nil")
	}
	if !approxEqual(readings.Wind.Speed, 4.0) {
		t.Errorf("Wind.Speed: got %v, want 4.0", readings.Wind.Speed)
	}
	if !approxEqual(readings.Wind.Gust, 8.5) {
		t.Errorf("Wind.Gust: got %v, want 8.5", readings.Wind.Gust)
	}
	if readings.Wind.Direction != 225 {
		t.Errorf("Wind.Direction: got %v, want 225", readings.Wind.Direction)
	}

	if readings.FetchedAt.IsZero() {
		t.Error("FetchedAt should not be zero")
	}
}

func TestGetStationsData_Cache(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", "test-key-netatmo-client-cache")
	encryption.ResetEncryptionKey()
	defer encryption.ResetEncryptionKey()

	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"status": "ok",
			"body": map[string]any{
				"devices": []any{
					map[string]any{
						"type":           "NAMain",
						"dashboard_data": map[string]any{"Temperature": 20.0},
						"modules":        []any{},
					},
				},
			},
		})
	}))
	defer srv.Close()

	db := setupTestDB(t)
	defer db.Close()
	userID := insertTestUser(t, db)

	if err := SaveToken(db, userID, &NetatmoToken{
		AccessToken:  "tok",
		RefreshToken: "ref",
		Expiry:       time.Now().Add(1 * time.Hour),
	}); err != nil {
		t.Fatalf("SaveToken: %v", err)
	}

	oauth := &OAuthClient{
		clientID:   "id",
		clientSecret: "secret",
		httpClient: &http.Client{Transport: &redirectTransport{base: srv.URL}},
	}
	client := &Client{
		oauth:      oauth,
		db:         db,
		httpClient: &http.Client{Transport: &redirectTransport{base: srv.URL}},
		cache:      make(map[int64]cacheEntry),
	}

	if _, err := client.GetStationsData(context.Background(), userID); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if _, err := client.GetStationsData(context.Background(), userID); err != nil {
		t.Fatalf("second call: %v", err)
	}

	if callCount != 1 {
		t.Errorf("expected 1 HTTP call (cache hit on second), got %d", callCount)
	}
}

func TestGetStationsData_CacheExpiry(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", "test-key-netatmo-client-cache-expiry")
	encryption.ResetEncryptionKey()
	defer encryption.ResetEncryptionKey()

	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"status": "ok",
			"body": map[string]any{
				"devices": []any{
					map[string]any{
						"type":           "NAMain",
						"dashboard_data": map[string]any{"Temperature": 20.0},
						"modules":        []any{},
					},
				},
			},
		})
	}))
	defer srv.Close()

	db := setupTestDB(t)
	defer db.Close()
	userID := insertTestUser(t, db)

	if err := SaveToken(db, userID, &NetatmoToken{
		AccessToken:  "tok",
		RefreshToken: "ref",
		Expiry:       time.Now().Add(1 * time.Hour),
	}); err != nil {
		t.Fatalf("SaveToken: %v", err)
	}

	oauth := &OAuthClient{
		clientID:     "id",
		clientSecret: "secret",
		httpClient:   &http.Client{Transport: &redirectTransport{base: srv.URL}},
	}
	client := &Client{
		oauth:      oauth,
		db:         db,
		httpClient: &http.Client{Transport: &redirectTransport{base: srv.URL}},
		cache:      make(map[int64]cacheEntry),
	}

	// Seed cache with an already-expired entry.
	client.mu.Lock()
	client.cache[userID] = cacheEntry{
		readings:  &ModuleReadings{Indoor: &IndoorReadings{Temperature: 15.0}},
		fetchedAt: time.Now().Add(-10 * time.Minute),
	}
	client.mu.Unlock()

	if _, err := client.GetStationsData(context.Background(), userID); err != nil {
		t.Fatalf("GetStationsData: %v", err)
	}

	if callCount != 1 {
		t.Errorf("expected 1 HTTP call after cache expiry, got %d", callCount)
	}
}

func TestGetStationsData_NoToken(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", "test-key-netatmo-client-no-token")
	encryption.ResetEncryptionKey()
	defer encryption.ResetEncryptionKey()

	db := setupTestDB(t)
	defer db.Close()
	userID := insertTestUser(t, db)

	oauth := NewOAuthClient("id", "secret", "http://localhost/cb")
	client := NewClient(oauth, db)

	_, err := client.GetStationsData(context.Background(), userID)
	if err == nil {
		t.Fatal("expected error for user with no token, got nil")
	}
}

func TestParseStationsData_MissingModules(t *testing.T) {
	body, _ := json.Marshal(map[string]any{
		"status": "ok",
		"body": map[string]any{
			"devices": []any{
				map[string]any{
					"type": "NAMain",
					"dashboard_data": map[string]any{
						"Temperature": 19.5,
						"Humidity":    52,
						"CO2":         580,
						"Noise":       35,
						"Pressure":    1010.0,
					},
					"modules": []any{},
				},
			},
		},
	})

	readings, err := parseStationsData(body)
	if err != nil {
		t.Fatalf("parseStationsData: %v", err)
	}
	if readings.Indoor == nil {
		t.Fatal("Indoor should not be nil")
	}
	if !approxEqual(readings.Indoor.Temperature, 19.5) {
		t.Errorf("Indoor.Temperature: got %v, want 19.5", readings.Indoor.Temperature)
	}
	if readings.Outdoor != nil {
		t.Error("Outdoor should be nil when no outdoor module present")
	}
	if readings.Wind != nil {
		t.Error("Wind should be nil when no wind module present")
	}
}

func TestParseStationsData_EmptyDevices(t *testing.T) {
	body, _ := json.Marshal(map[string]any{
		"status": "ok",
		"body": map[string]any{
			"devices": []any{},
		},
	})

	readings, err := parseStationsData(body)
	if err != nil {
		t.Fatalf("parseStationsData: %v", err)
	}
	if readings.Indoor != nil || readings.Outdoor != nil || readings.Wind != nil {
		t.Error("all module readings should be nil for empty devices")
	}
}

func TestParseStationsData_APIError(t *testing.T) {
	body, _ := json.Marshal(map[string]any{
		"error": map[string]any{
			"code":    3,
			"message": "Access token expired",
		},
	})

	_, err := parseStationsData(body)
	if err == nil {
		t.Fatal("expected error for API error response, got nil")
	}
}
