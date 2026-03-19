package dashboard

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/db"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	d, err := db.Init(":memory:")
	if err != nil {
		t.Fatalf("failed to init test db: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func createTestUser(t *testing.T, d *sql.DB) *auth.User {
	t.Helper()
	u, err := auth.UpsertUser(d, "test@example.com", "Test User", "https://pic.example.com", "google-123")
	if err != nil {
		t.Fatalf("failed to create test user: %v", err)
	}
	return u
}

func TestActivityHandler_Empty(t *testing.T) {
	d := setupTestDB(t)
	user := createTestUser(t, d)

	handler := ActivityHandler(d)
	req := httptest.NewRequest("GET", "/api/dashboard/activity", nil)
	req = req.WithContext(auth.ContextWithUser(req.Context(), user))
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var resp struct {
		Items []ActivityItem `json:"items"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp.Items) != 0 {
		t.Errorf("expected 0 items, got %d", len(resp.Items))
	}
}

func TestActivityHandler_WithWorkout(t *testing.T) {
	d := setupTestDB(t)
	user := createTestUser(t, d)

	// Insert a workout directly.
	_, err := d.Exec(
		`INSERT INTO workouts (user_id, sport, title, started_at, duration_seconds, distance_meters,
		 avg_heart_rate, max_heart_rate, avg_pace_sec_per_km, avg_cadence, calories,
		 ascent_meters, descent_meters, fit_file_hash, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		user.ID, "running", "Morning Run", "2026-03-19T08:00:00Z", 1800, 5000,
		150, 170, 360, 180, 300, 50, 30, "hash123", "2026-03-19T08:00:00Z",
	)
	if err != nil {
		t.Fatalf("failed to insert workout: %v", err)
	}

	handler := ActivityHandler(d)
	req := httptest.NewRequest("GET", "/api/dashboard/activity", nil)
	req = req.WithContext(auth.ContextWithUser(req.Context(), user))
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var resp struct {
		Items []ActivityItem `json:"items"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(resp.Items))
	}
	if resp.Items[0].Type != "workout" {
		t.Errorf("expected type 'workout', got %q", resp.Items[0].Type)
	}
	if resp.Items[0].Link != "/training" {
		t.Errorf("expected link '/training', got %q", resp.Items[0].Link)
	}
}

func TestActivityHandler_MultipleTypes(t *testing.T) {
	d := setupTestDB(t)
	user := createTestUser(t, d)

	// Insert a workout.
	_, err := d.Exec(
		`INSERT INTO workouts (user_id, sport, title, started_at, duration_seconds, distance_meters,
		 avg_heart_rate, max_heart_rate, avg_pace_sec_per_km, avg_cadence, calories,
		 ascent_meters, descent_meters, fit_file_hash, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		user.ID, "cycling", "", "2026-03-18T10:00:00Z", 3600, 20000,
		140, 160, 0, 90, 500, 100, 80, "hash456", "2026-03-18T10:00:00Z",
	)
	if err != nil {
		t.Fatalf("failed to insert workout: %v", err)
	}

	// Insert a note.
	_, err = d.Exec(
		`INSERT INTO notes (user_id, title, content, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		user.ID, "My Note", "Content here", "2026-03-19T12:00:00Z", "2026-03-19T12:00:00Z",
	)
	if err != nil {
		t.Fatalf("failed to insert note: %v", err)
	}

	handler := ActivityHandler(d)
	req := httptest.NewRequest("GET", "/api/dashboard/activity", nil)
	req = req.WithContext(auth.ContextWithUser(req.Context(), user))
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	var resp struct {
		Items []ActivityItem `json:"items"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(resp.Items))
	}
	// Should be sorted by timestamp descending — note is more recent.
	if resp.Items[0].Type != "note" {
		t.Errorf("expected first item type 'note', got %q", resp.Items[0].Type)
	}
	if resp.Items[1].Type != "workout" {
		t.Errorf("expected second item type 'workout', got %q", resp.Items[1].Type)
	}
}

func TestActivityHandler_WithLactateTest(t *testing.T) {
	d := setupTestDB(t)
	user := createTestUser(t, d)

	_, err := d.Exec(
		`INSERT INTO lactate_tests (user_id, date, comment, created_at) VALUES (?, ?, ?, ?)`,
		user.ID, "2026-03-18", "Threshold test", "2026-03-18T09:00:00Z",
	)
	if err != nil {
		t.Fatalf("failed to insert lactate test: %v", err)
	}

	handler := ActivityHandler(d)
	req := httptest.NewRequest("GET", "/api/dashboard/activity", nil)
	req = req.WithContext(auth.ContextWithUser(req.Context(), user))
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var resp struct {
		Items []ActivityItem `json:"items"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(resp.Items))
	}
	if resp.Items[0].Type != "lactate" {
		t.Errorf("expected type 'lactate', got %q", resp.Items[0].Type)
	}
	if resp.Items[0].Title != "Lactate test: Threshold test" {
		t.Errorf("unexpected title %q", resp.Items[0].Title)
	}
	if resp.Items[0].Link != "/lactate" {
		t.Errorf("expected link '/lactate', got %q", resp.Items[0].Link)
	}
}

func TestActivityHandler_WithShortLink(t *testing.T) {
	d := setupTestDB(t)
	user := createTestUser(t, d)

	_, err := d.Exec(
		`INSERT INTO short_links (user_id, code, target_url, title, created_at) VALUES (?, ?, ?, ?, ?)`,
		user.ID, "abc", "https://example.com", "Example", "2026-03-19T10:00:00Z",
	)
	if err != nil {
		t.Fatalf("failed to insert short link: %v", err)
	}

	handler := ActivityHandler(d)
	req := httptest.NewRequest("GET", "/api/dashboard/activity", nil)
	req = req.WithContext(auth.ContextWithUser(req.Context(), user))
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var resp struct {
		Items []ActivityItem `json:"items"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(resp.Items))
	}
	if resp.Items[0].Type != "link" {
		t.Errorf("expected type 'link', got %q", resp.Items[0].Type)
	}
	if resp.Items[0].Title != "Link created: Example" {
		t.Errorf("unexpected title %q", resp.Items[0].Title)
	}
	if resp.Items[0].Link != "/links" {
		t.Errorf("expected link '/links', got %q", resp.Items[0].Link)
	}
}

func TestActivityHandler_LimitTen(t *testing.T) {
	d := setupTestDB(t)
	user := createTestUser(t, d)

	// Insert 12 notes to exceed the 10-item limit.
	for i := 0; i < 12; i++ {
		ts := fmt.Sprintf("2026-03-%02dT12:00:00Z", i+5)
		_, err := d.Exec(
			`INSERT INTO notes (user_id, title, content, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
			user.ID, fmt.Sprintf("Note %d", i+1), "Content", ts, ts,
		)
		if err != nil {
			t.Fatalf("failed to insert note %d: %v", i+1, err)
		}
	}

	handler := ActivityHandler(d)
	req := httptest.NewRequest("GET", "/api/dashboard/activity", nil)
	req = req.WithContext(auth.ContextWithUser(req.Context(), user))
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	var resp struct {
		Items []ActivityItem `json:"items"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp.Items) != 10 {
		t.Errorf("expected 10 items (limit), got %d", len(resp.Items))
	}
}

func TestSportLabel(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"running", "Running"},
		{"cycling", "Cycling"},
		{"swimming", "Swimming"},
		{"walking", "Walking"},
		{"hiking", "Hiking"},
		{"rowing", "rowing"},
		{"", "Workout"},
	}
	for _, tc := range tests {
		got := sportLabel(tc.input)
		if got != tc.want {
			t.Errorf("sportLabel(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
