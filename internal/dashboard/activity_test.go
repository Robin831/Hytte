package dashboard

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/db"
	"github.com/Robin831/Hytte/internal/encryption"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	// Pin the encryption key so EncryptField/DecryptField produce stable,
	// hermetic output. Without this the tests share the developer's
	// auto-generated key file or fail in CI where the config dir may not be
	// writable. See internal/suggestions/store_test.go for the established pattern.
	t.Setenv("ENCRYPTION_KEY", "test-encryption-key-for-dashboard-tests")
	encryption.ResetEncryptionKey()
	t.Cleanup(func() { encryption.ResetEncryptionKey() })

	d, err := db.Init(":memory:")
	if err != nil {
		t.Fatalf("failed to init test db: %v", err)
	}
	d.SetMaxOpenConns(1)
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

// englishLabelFragments is the set of human-readable phrases that the
// previous implementation embedded in API responses. The new contract
// returns structured fields only, so these substrings must never appear
// in any string field of the response.
var englishLabelFragments = []string{
	"recorded",
	"Note:",
	"Note created",
	"Lactate test",
	"Short link",
	"Link created",
	"workout",
	"Workout",
	"Running",
	"Cycling",
}

func assertNoEnglishLabels(t *testing.T, item ActivityItem) {
	t.Helper()
	// Only the user-supplied fields (title, comment) may legitimately contain
	// English words; the structural fields below must stay free of label text.
	// The `type` field is a stable enum discriminator ("workout", "lactate",
	// "note", "link") — those values are machine-readable API keys, not labels,
	// so they are not subject to this check.
	// `code` is excluded because short-link codes are user-controlled and may
	// legitimately contain substrings like "workout" or "running".
	for _, field := range []struct{ name, value string }{
		{"sport", item.Sport},
		{"link", item.Link},
	} {
		for _, frag := range englishLabelFragments {
			if strings.Contains(field.value, frag) {
				t.Errorf("item.%s = %q contains forbidden English label fragment %q", field.name, field.value, frag)
			}
		}
	}
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
	now := time.Now().UTC()
	ts := now.Add(-time.Hour).Format(time.RFC3339)

	// Workout title is stored encrypted at rest; the handler must decrypt it.
	encTitle, err := encryption.EncryptField("Morning Run")
	if err != nil {
		t.Fatalf("failed to encrypt workout title: %v", err)
	}
	_, err = d.Exec(
		`INSERT INTO workouts (user_id, sport, title, started_at, duration_seconds, distance_meters,
		 avg_heart_rate, max_heart_rate, avg_pace_sec_per_km, avg_cadence, calories,
		 ascent_meters, descent_meters, fit_file_hash, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		user.ID, "running", encTitle, ts, 1800, 5000,
		150, 170, 360, 180, 300, 50, 30, "hash123", ts,
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
	got := resp.Items[0]
	if got.Type != "workout" {
		t.Errorf("expected type 'workout', got %q", got.Type)
	}
	if got.Sport != "running" {
		t.Errorf("expected sport 'running', got %q", got.Sport)
	}
	if got.Title != "Morning Run" {
		t.Errorf("expected title 'Morning Run', got %q", got.Title)
	}
	if got.Link != "/training" {
		t.Errorf("expected link '/training', got %q", got.Link)
	}
	if got.Timestamp == "" {
		t.Errorf("expected timestamp to be populated")
	}
	assertNoEnglishLabels(t, got)
}

func TestActivityHandler_WorkoutWithoutTitle(t *testing.T) {
	d := setupTestDB(t)
	user := createTestUser(t, d)
	now := time.Now().UTC()
	ts := now.Add(-time.Hour).Format(time.RFC3339)

	_, err := d.Exec(
		`INSERT INTO workouts (user_id, sport, title, started_at, duration_seconds, distance_meters,
		 avg_heart_rate, max_heart_rate, avg_pace_sec_per_km, avg_cadence, calories,
		 ascent_meters, descent_meters, fit_file_hash, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		user.ID, "cycling", "", ts, 3600, 20000,
		140, 160, 0, 90, 500, 100, 80, "hash-empty", ts,
	)
	if err != nil {
		t.Fatalf("failed to insert workout: %v", err)
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
	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(resp.Items))
	}
	got := resp.Items[0]
	if got.Sport != "cycling" {
		t.Errorf("expected sport 'cycling', got %q", got.Sport)
	}
	if got.Title != "" {
		t.Errorf("expected empty title for blank workout, got %q", got.Title)
	}
	assertNoEnglishLabels(t, got)
}

func TestActivityHandler_MultipleTypes(t *testing.T) {
	d := setupTestDB(t)
	user := createTestUser(t, d)
	now := time.Now().UTC()
	workoutTs := now.Add(-24 * time.Hour).Format(time.RFC3339)
	noteTs := now.Add(-time.Hour).Format(time.RFC3339)

	// Insert a workout.
	_, err := d.Exec(
		`INSERT INTO workouts (user_id, sport, title, started_at, duration_seconds, distance_meters,
		 avg_heart_rate, max_heart_rate, avg_pace_sec_per_km, avg_cadence, calories,
		 ascent_meters, descent_meters, fit_file_hash, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		user.ID, "cycling", "", workoutTs, 3600, 20000,
		140, 160, 0, 90, 500, 100, 80, "hash456", workoutTs,
	)
	if err != nil {
		t.Fatalf("failed to insert workout: %v", err)
	}

	// Insert a note (title is encrypted at rest).
	encTitle, err := encryption.EncryptField("My Note")
	if err != nil {
		t.Fatalf("failed to encrypt note title: %v", err)
	}
	_, err = d.Exec(
		`INSERT INTO notes (user_id, title, content, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		user.ID, encTitle, "Content here", noteTs, noteTs,
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
	if resp.Items[0].Title != "My Note" {
		t.Errorf("expected note title to be decrypted to 'My Note', got %q", resp.Items[0].Title)
	}
	if resp.Items[1].Type != "workout" {
		t.Errorf("expected second item type 'workout', got %q", resp.Items[1].Type)
	}
	if resp.Items[1].Sport != "cycling" {
		t.Errorf("expected sport 'cycling', got %q", resp.Items[1].Sport)
	}
	for _, it := range resp.Items {
		assertNoEnglishLabels(t, it)
	}
}

func TestActivityHandler_WithLactateTest(t *testing.T) {
	d := setupTestDB(t)
	user := createTestUser(t, d)
	now := time.Now().UTC()

	encComment, err := encryption.EncryptField("Threshold test")
	if err != nil {
		t.Fatalf("failed to encrypt lactate comment: %v", err)
	}
	_, err = d.Exec(
		`INSERT INTO lactate_tests (user_id, date, comment, created_at) VALUES (?, ?, ?, ?)`,
		user.ID, now.Add(-24*time.Hour).Format("2006-01-02"), encComment, now.Add(-24*time.Hour).Format(time.RFC3339),
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
	got := resp.Items[0]
	if got.Type != "lactate" {
		t.Errorf("expected type 'lactate', got %q", got.Type)
	}
	if got.Comment != "Threshold test" {
		t.Errorf("expected comment 'Threshold test' (decrypted), got %q", got.Comment)
	}
	if got.Link != "/lactate" {
		t.Errorf("expected link '/lactate', got %q", got.Link)
	}
	assertNoEnglishLabels(t, got)
}

func TestActivityHandler_WithLactateTestEmptyComment(t *testing.T) {
	d := setupTestDB(t)
	user := createTestUser(t, d)
	now := time.Now().UTC()

	_, err := d.Exec(
		`INSERT INTO lactate_tests (user_id, date, comment, created_at) VALUES (?, ?, ?, ?)`,
		user.ID, now.Add(-24*time.Hour).Format("2006-01-02"), "", now.Add(-24*time.Hour).Format(time.RFC3339),
	)
	if err != nil {
		t.Fatalf("failed to insert lactate test: %v", err)
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
	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(resp.Items))
	}
	got := resp.Items[0]
	if got.Comment != "" {
		t.Errorf("expected empty comment to round-trip as empty, got %q", got.Comment)
	}
}

func TestActivityHandler_WithShortLink(t *testing.T) {
	d := setupTestDB(t)
	user := createTestUser(t, d)
	now := time.Now().UTC()

	_, err := d.Exec(
		`INSERT INTO short_links (user_id, code, target_url, title, created_at) VALUES (?, ?, ?, ?, ?)`,
		user.ID, "abc", "https://example.com", "Example", now.Add(-time.Hour).Format(time.RFC3339),
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
	got := resp.Items[0]
	if got.Type != "link" {
		t.Errorf("expected type 'link', got %q", got.Type)
	}
	if got.Code != "abc" {
		t.Errorf("expected code 'abc', got %q", got.Code)
	}
	if got.Title != "Example" {
		t.Errorf("expected title 'Example', got %q", got.Title)
	}
	if got.Link != "/links" {
		t.Errorf("expected link '/links', got %q", got.Link)
	}
	assertNoEnglishLabels(t, got)
}

func TestActivityHandler_ShortLinkWithoutTitle(t *testing.T) {
	d := setupTestDB(t)
	user := createTestUser(t, d)
	now := time.Now().UTC()

	_, err := d.Exec(
		`INSERT INTO short_links (user_id, code, target_url, title, created_at) VALUES (?, ?, ?, ?, ?)`,
		user.ID, "xyz", "https://example.com/page", "", now.Add(-time.Hour).Format(time.RFC3339),
	)
	if err != nil {
		t.Fatalf("failed to insert short link: %v", err)
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
	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(resp.Items))
	}
	got := resp.Items[0]
	if got.Code != "xyz" {
		t.Errorf("expected code 'xyz', got %q", got.Code)
	}
	if got.Title != "" {
		t.Errorf("expected empty title to round-trip as empty, got %q", got.Title)
	}
}

func TestActivityHandler_NoEnglishLabelsAcrossAllTypes(t *testing.T) {
	d := setupTestDB(t)
	user := createTestUser(t, d)
	now := time.Now().UTC()
	workoutTs := now.Add(-4 * time.Hour).Format(time.RFC3339)
	noteTs := now.Add(-3 * time.Hour).Format(time.RFC3339)
	linkTs := now.Add(-2 * time.Hour).Format(time.RFC3339)
	lactateDate := now.Add(-time.Hour).Format("2006-01-02")
	lactateTs := now.Add(-time.Hour).Format(time.RFC3339)

	if _, err := d.Exec(
		`INSERT INTO workouts (user_id, sport, title, started_at, duration_seconds, distance_meters,
		 avg_heart_rate, max_heart_rate, avg_pace_sec_per_km, avg_cadence, calories,
		 ascent_meters, descent_meters, fit_file_hash, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		user.ID, "running", "", workoutTs, 1800, 5000,
		150, 170, 360, 180, 300, 50, 30, "h-empty", workoutTs,
	); err != nil {
		t.Fatalf("insert workout: %v", err)
	}

	if _, err := d.Exec(
		`INSERT INTO notes (user_id, title, content, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		user.ID, "", "body", noteTs, noteTs,
	); err != nil {
		t.Fatalf("insert note: %v", err)
	}

	if _, err := d.Exec(
		`INSERT INTO short_links (user_id, code, target_url, title, created_at) VALUES (?, ?, ?, ?, ?)`,
		user.ID, "go1", "https://example.com", "", linkTs,
	); err != nil {
		t.Fatalf("insert short link: %v", err)
	}

	if _, err := d.Exec(
		`INSERT INTO lactate_tests (user_id, date, comment, created_at) VALUES (?, ?, ?, ?)`,
		user.ID, lactateDate, "", lactateTs,
	); err != nil {
		t.Fatalf("insert lactate: %v", err)
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
	if len(resp.Items) != 4 {
		t.Fatalf("expected 4 items, got %d", len(resp.Items))
	}

	gotTypes := map[string]bool{}
	for _, it := range resp.Items {
		gotTypes[it.Type] = true
		if it.Timestamp == "" {
			t.Errorf("item %+v missing timestamp", it)
		}
		if it.Link == "" {
			t.Errorf("item %+v missing link", it)
		}
		assertNoEnglishLabels(t, it)
		// Empty user-supplied fields should round-trip as empty strings.
		if it.Title != "" {
			t.Errorf("expected empty title for %s, got %q", it.Type, it.Title)
		}
		if it.Type == "lactate" && it.Comment != "" {
			t.Errorf("expected empty comment for lactate, got %q", it.Comment)
		}
	}
	for _, want := range []string{"workout", "note", "link", "lactate"} {
		if !gotTypes[want] {
			t.Errorf("missing item type %q in response", want)
		}
	}
}

func TestActivityHandler_LimitTen(t *testing.T) {
	d := setupTestDB(t)
	user := createTestUser(t, d)
	now := time.Now().UTC()

	// Insert 12 notes to exceed the 10-item limit.
	for i := 0; i < 12; i++ {
		ts := now.Add(-time.Duration(12-i) * time.Hour).Format(time.RFC3339)
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
