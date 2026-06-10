package training

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/Robin831/Hytte/internal/db"
	"github.com/Robin831/Hytte/internal/encryption"
	"github.com/Robin831/Hytte/internal/hrzones"
	"github.com/go-chi/chi/v5"
	fitencoder "github.com/muktihari/fit/encoder"
	"github.com/muktihari/fit/profile/filedef"
	"github.com/muktihari/fit/profile/mesgdef"
	"github.com/muktihari/fit/profile/typedef"
	_ "modernc.org/sqlite"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	t.Setenv("ENCRYPTION_KEY", "test-key-for-training-tests")
	encryption.ResetEncryptionKey()
	t.Cleanup(func() { encryption.ResetEncryptionKey() })

	database, err := db.Init(":memory:")
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	// SQLite :memory: databases are per-connection; limit to 1 to avoid "no such table" races.
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	t.Cleanup(func() { database.Close() })

	_, err = database.Exec(`INSERT INTO users (id, email, name, google_id) VALUES (1, 'test@example.com', 'Test', 'google-1')`)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	return database
}

func withUser(r *http.Request, userID int64) *http.Request {
	user := &auth.User{ID: userID, Email: "test@example.com", Name: "Test"}
	ctx := auth.ContextWithUser(r.Context(), user)
	return r.WithContext(ctx)
}

func withAdminUser(r *http.Request, userID int64) *http.Request {
	user := &auth.User{ID: userID, Email: "test@example.com", Name: "Test", IsAdmin: true}
	ctx := auth.ContextWithUser(r.Context(), user)
	return r.WithContext(ctx)
}

func withChiParam(r *http.Request, key, value string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, value)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func TestListHandler_Empty(t *testing.T) {
	database := setupTestDB(t)

	req := withUser(httptest.NewRequest(http.MethodGet, "/api/training/workouts", nil), 1)
	w := httptest.NewRecorder()
	ListHandler(database)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	workouts, ok := resp["workouts"].([]any)
	if !ok {
		t.Fatal("expected workouts array")
	}
	if len(workouts) != 0 {
		t.Fatalf("expected empty list, got %d", len(workouts))
	}
}

// insertWorkoutAt inserts a minimal workout with an explicit started_at and
// returns its id. Used to exercise keyset pagination ordering, including ties on
// started_at that must be broken by id.
func insertWorkoutAt(t *testing.T, db_ *sql.DB, userID int64, startedAt string) int64 {
	t.Helper()
	hash := fmt.Sprintf("pagehash%d", testHashCounter.Add(1))
	res, err := db_.Exec(
		`INSERT INTO workouts (user_id, sport, title, started_at, duration_seconds, distance_meters, fit_file_hash)
		 VALUES (?, 'running', 'run', ?, 1800, 5000, ?)`,
		userID, startedAt, hash,
	)
	if err != nil {
		t.Fatalf("insert workout: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id: %v", err)
	}
	return id
}

// fetchWorkoutPage calls ListHandler with the given limit/cursor query params
// and returns the page's workout ids plus the next_cursor token ("" when null).
func fetchWorkoutPage(t *testing.T, database *sql.DB, userID int64, limit int, cursor string) ([]int64, string) {
	t.Helper()
	url := "/api/training/workouts?limit=" + strconv.Itoa(limit)
	if cursor != "" {
		url += "&cursor=" + cursor
	}
	req := withUser(httptest.NewRequest(http.MethodGet, url, nil), userID)
	rec := httptest.NewRecorder()
	ListHandler(database)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Workouts []struct {
			ID int64 `json:"id"`
		} `json:"workouts"`
		NextCursor *string `json:"next_cursor"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	ids := make([]int64, len(resp.Workouts))
	for i, w := range resp.Workouts {
		ids[i] = w.ID
	}
	next := ""
	if resp.NextCursor != nil {
		next = *resp.NextCursor
	}
	return ids, next
}

func TestListHandler_FirstPageHasCursor(t *testing.T) {
	database := setupTestDB(t)

	for i := 0; i < 5; i++ {
		insertWorkoutAt(t, database, 1, fmt.Sprintf("2024-01-0%dT10:00:00Z", i+1))
	}

	ids, next := fetchWorkoutPage(t, database, 1, 2, "")
	if len(ids) != 2 {
		t.Fatalf("expected 2 workouts on first page, got %d", len(ids))
	}
	if next == "" {
		t.Fatal("expected non-null next_cursor when more pages remain")
	}
}

func TestListHandler_PaginationWalksAllRowsWithTies(t *testing.T) {
	database := setupTestDB(t)

	// Seed including ties on started_at so the id tiebreak is exercised at page
	// boundaries. Insertion order fixes the ids (1..5).
	w1 := insertWorkoutAt(t, database, 1, "2024-01-01T10:00:00Z")
	w2 := insertWorkoutAt(t, database, 1, "2024-01-03T10:00:00Z")
	w3 := insertWorkoutAt(t, database, 1, "2024-01-02T10:00:00Z")
	w4 := insertWorkoutAt(t, database, 1, "2024-01-03T10:00:00Z") // tie with w2
	w5 := insertWorkoutAt(t, database, 1, "2024-01-01T10:00:00Z") // tie with w1

	// Expected order: started_at DESC, id DESC.
	want := []int64{w4, w2, w3, w5, w1}

	var got []int64
	cursor := ""
	seen := map[int64]bool{}
	for page := 0; page < 10; page++ {
		ids, next := fetchWorkoutPage(t, database, 1, 2, cursor)
		for _, id := range ids {
			if seen[id] {
				t.Fatalf("duplicate workout id %d across page boundary", id)
			}
			seen[id] = true
			got = append(got, id)
		}
		if next == "" {
			break
		}
		cursor = next
	}

	if len(got) != len(want) {
		t.Fatalf("expected %d workouts walking all pages, got %d (%v)", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("page walk order mismatch at %d: got %v, want %v", i, got, want)
		}
	}
}

func TestListHandler_FinalPageNullCursor(t *testing.T) {
	database := setupTestDB(t)

	insertWorkoutAt(t, database, 1, "2024-01-01T10:00:00Z")
	insertWorkoutAt(t, database, 1, "2024-01-02T10:00:00Z")

	// limit equals the total — the single page exhausts the list, cursor null.
	ids, next := fetchWorkoutPage(t, database, 1, 2, "")
	if len(ids) != 2 {
		t.Fatalf("expected 2 workouts, got %d", len(ids))
	}
	if next != "" {
		t.Fatalf("expected null next_cursor on final page, got %q", next)
	}
}

func TestListHandler_EmptyHasNullCursor(t *testing.T) {
	database := setupTestDB(t)

	req := withUser(httptest.NewRequest(http.MethodGet, "/api/training/workouts", nil), 1)
	rec := httptest.NewRecorder()
	ListHandler(database)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	workouts, ok := resp["workouts"].([]any)
	if !ok || len(workouts) != 0 {
		t.Fatalf("expected empty workouts array, got %v", resp["workouts"])
	}
	if resp["next_cursor"] != nil {
		t.Fatalf("expected null next_cursor for empty list, got %v", resp["next_cursor"])
	}
}

func TestListHandler_LimitClampedAndDefaulted(t *testing.T) {
	database := setupTestDB(t)

	// Seed more than the max page size so an oversized limit can be observed clamping.
	total := maxWorkoutPageSize + 10
	for i := 0; i < total; i++ {
		insertWorkoutAt(t, database, 1, fmt.Sprintf("2024-06-01T10:%02d:%02dZ", i/60, i%60))
	}

	// Oversized limit is clamped to the max.
	ids, _ := fetchWorkoutPage(t, database, 1, maxWorkoutPageSize+500, "")
	if len(ids) != maxWorkoutPageSize {
		t.Fatalf("expected oversized limit clamped to %d, got %d", maxWorkoutPageSize, len(ids))
	}

	// Negative limit falls back to the default page size.
	negIDs, _ := fetchWorkoutPage(t, database, 1, -5, "")
	if len(negIDs) != defaultWorkoutPageSize {
		t.Fatalf("expected negative limit defaulted to %d, got %d", defaultWorkoutPageSize, len(negIDs))
	}

	// Non-numeric limit falls back to the default page size.
	req := withUser(httptest.NewRequest(http.MethodGet, "/api/training/workouts?limit=abc", nil), 1)
	rec := httptest.NewRecorder()
	ListHandler(database)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp struct {
		Workouts []json.RawMessage `json:"workouts"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Workouts) != defaultWorkoutPageSize {
		t.Fatalf("expected non-numeric limit defaulted to %d, got %d", defaultWorkoutPageSize, len(resp.Workouts))
	}
}

func TestListHandler_InvalidCursorRejected(t *testing.T) {
	database := setupTestDB(t)

	req := withUser(httptest.NewRequest(http.MethodGet, "/api/training/workouts?cursor=not!base64", nil), 1)
	rec := httptest.NewRecorder()
	ListHandler(database)(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for malformed cursor, got %d", rec.Code)
	}
}

func TestLatestHandler_Empty(t *testing.T) {
	database := setupTestDB(t)

	req := withUser(httptest.NewRequest(http.MethodGet, "/api/training/workouts/latest", nil), 1)
	w := httptest.NewRecorder()
	LatestHandler(database)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	latestID, ok := resp["latest_id"].(float64)
	if !ok {
		t.Fatalf("expected latest_id number, got %v", resp["latest_id"])
	}
	if latestID != 0 {
		t.Fatalf("expected latest_id 0 for empty list, got %v", latestID)
	}
}

func TestLatestHandler_Populated(t *testing.T) {
	database := setupTestDB(t)

	// Create a second user to verify per-user isolation.
	if _, err := database.Exec(
		`INSERT INTO users (id, email, name, google_id) VALUES (2, 'other@example.com', 'Other', 'google-2')`,
	); err != nil {
		t.Fatalf("create second user: %v", err)
	}

	mkWorkout := func(hash string) *ParsedWorkout {
		return &ParsedWorkout{
			Sport:           "running",
			DurationSeconds: 1800,
			DistanceMeters:  5000,
			AvgHeartRate:    150,
		}
	}

	w1, err := Create(database, 1, mkWorkout("user1-a"), "user1-a")
	if err != nil {
		t.Fatalf("create user1 workout a: %v", err)
	}
	w2, err := Create(database, 1, mkWorkout("user1-b"), "user1-b")
	if err != nil {
		t.Fatalf("create user1 workout b: %v", err)
	}
	// Workout for user 2 — must not affect user 1's latest_id.
	if _, err := Create(database, 2, mkWorkout("user2-a"), "user2-a"); err != nil {
		t.Fatalf("create user2 workout: %v", err)
	}

	expectedMax := w1.ID
	if w2.ID > expectedMax {
		expectedMax = w2.ID
	}

	req := withUser(httptest.NewRequest(http.MethodGet, "/api/training/workouts/latest", nil), 1)
	rec := httptest.NewRecorder()
	LatestHandler(database)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	latestID, _ := resp["latest_id"].(float64)
	if int64(latestID) != expectedMax {
		t.Fatalf("expected latest_id %d, got %v", expectedMax, latestID)
	}
}

func TestLatestHandler_Unauthenticated(t *testing.T) {
	database := setupTestDB(t)

	req := httptest.NewRequest(http.MethodGet, "/api/training/workouts/latest", nil)
	w := httptest.NewRecorder()
	LatestHandler(database)(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestCreateAndGet(t *testing.T) {
	database := setupTestDB(t)

	pw := &ParsedWorkout{
		Sport:           "running",
		DurationSeconds: 3600,
		DistanceMeters:  10000,
		AvgHeartRate:    150,
		MaxHeartRate:    175,
		Laps: []ParsedLap{
			{DurationSeconds: 600, DistanceMeters: 1000, AvgHeartRate: 145, MaxHeartRate: 160, AvgSpeedMPerS: 1.67},
			{DurationSeconds: 600, DistanceMeters: 1000, AvgHeartRate: 155, MaxHeartRate: 170, AvgSpeedMPerS: 1.67},
		},
		Samples: []Sample{
			{OffsetMs: 0, HeartRate: 120, SpeedMPerS: 2.5},
			{OffsetMs: 1000, HeartRate: 140, SpeedMPerS: 2.8},
			{OffsetMs: 2000, HeartRate: 150, SpeedMPerS: 3.0},
		},
	}

	workout, err := Create(database, 1, pw, "abc123hash")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if workout.ID == 0 {
		t.Fatal("expected non-zero ID")
	}
	if workout.Sport != "running" {
		t.Fatalf("expected running, got %s", workout.Sport)
	}
	if len(workout.Laps) != 2 {
		t.Fatalf("expected 2 laps, got %d", len(workout.Laps))
	}
	if workout.Samples == nil || len(workout.Samples.Points) != 3 {
		t.Fatal("expected 3 sample points")
	}

	// Test duplicate detection.
	exists, err := HashExists(database, 1, "abc123hash")
	if err != nil {
		t.Fatalf("hash exists: %v", err)
	}
	if !exists {
		t.Fatal("expected hash to exist")
	}

	// Test delete.
	if err := Delete(database, workout.ID, 1); err != nil {
		t.Fatalf("delete: %v", err)
	}
	_, err = GetByID(database, workout.ID, 1)
	if err != sql.ErrNoRows {
		t.Fatalf("expected ErrNoRows after delete, got %v", err)
	}
}

func TestUpdateTags(t *testing.T) {
	database := setupTestDB(t)

	pw := &ParsedWorkout{
		Sport:           "running",
		DurationSeconds: 1800,
		DistanceMeters:  5000,
		AvgHeartRate:    140,
	}

	workout, err := Create(database, 1, pw, "taghash")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := UpdateTags(database, workout.ID, 1, []string{"6x6", "intervals"}); err != nil {
		t.Fatalf("update tags: %v", err)
	}

	w, err := GetByID(database, workout.ID, 1)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	// 2 manual tags + auto:treadmill (no GPS in test workout)
	if len(w.Tags) != 3 {
		t.Fatalf("expected 3 tags, got %d: %v", len(w.Tags), w.Tags)
	}
}

func TestCreateAutoTags(t *testing.T) {
	database := setupTestDB(t)

	// Create a workout with a clear alternating interval pattern (work/rest).
	pw := &ParsedWorkout{
		Sport:           "running",
		DurationSeconds: 3600,
		DistanceMeters:  10000,
		AvgHeartRate:    150,
		MaxHeartRate:    175,
		Laps: []ParsedLap{
			{DurationSeconds: 360, DistanceMeters: 1200, AvgSpeedMPerS: 3.33},
			{DurationSeconds: 60, DistanceMeters: 90, AvgSpeedMPerS: 1.5},
			{DurationSeconds: 360, DistanceMeters: 1200, AvgSpeedMPerS: 3.33},
			{DurationSeconds: 60, DistanceMeters: 90, AvgSpeedMPerS: 1.5},
			{DurationSeconds: 360, DistanceMeters: 1200, AvgSpeedMPerS: 3.33},
			{DurationSeconds: 60, DistanceMeters: 90, AvgSpeedMPerS: 1.5},
			{DurationSeconds: 360, DistanceMeters: 1200, AvgSpeedMPerS: 3.33},
		},
	}

	workout, err := Create(database, 1, pw, "autotaghash")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	hasAutoTag := false
	for _, tag := range workout.Tags {
		if strings.HasPrefix(tag, "auto:") {
			hasAutoTag = true
			break
		}
	}
	if !hasAutoTag {
		t.Fatalf("expected auto-tag on workout with interval pattern, got tags: %v", workout.Tags)
	}
}

func TestUpdateTagsPreservesAutoTags(t *testing.T) {
	database := setupTestDB(t)

	pw := &ParsedWorkout{
		Sport:           "running",
		DurationSeconds: 3600,
		DistanceMeters:  10000,
		AvgHeartRate:    150,
		Laps: []ParsedLap{
			{DurationSeconds: 360, DistanceMeters: 1200, AvgSpeedMPerS: 3.33},
			{DurationSeconds: 60, DistanceMeters: 90, AvgSpeedMPerS: 1.5},
			{DurationSeconds: 360, DistanceMeters: 1200, AvgSpeedMPerS: 3.33},
			{DurationSeconds: 60, DistanceMeters: 90, AvgSpeedMPerS: 1.5},
			{DurationSeconds: 360, DistanceMeters: 1200, AvgSpeedMPerS: 3.33},
			{DurationSeconds: 60, DistanceMeters: 90, AvgSpeedMPerS: 1.5},
			{DurationSeconds: 360, DistanceMeters: 1200, AvgSpeedMPerS: 3.33},
		},
	}

	workout, err := Create(database, 1, pw, "preservehash")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	initialAutoTags := 0
	for _, tag := range workout.Tags {
		if strings.HasPrefix(tag, "auto:") {
			initialAutoTags++
		}
	}
	if initialAutoTags == 0 {
		t.Fatal("expected auto-tags on workout with interval pattern")
	}

	// Update manual tags — auto-tags should be preserved.
	if err := UpdateTags(database, workout.ID, 1, []string{"my-tag", "intervals"}); err != nil {
		t.Fatalf("update tags: %v", err)
	}

	w, err := GetByID(database, workout.ID, 1)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	autoCount := 0
	manualCount := 0
	for _, tag := range w.Tags {
		if strings.HasPrefix(tag, "auto:") {
			autoCount++
		} else {
			manualCount++
		}
	}
	if autoCount != initialAutoTags {
		t.Fatalf("expected %d auto-tags preserved, got %d (tags: %v)", initialAutoTags, autoCount, w.Tags)
	}
	if manualCount != 2 {
		t.Fatalf("expected 2 manual tags, got %d (tags: %v)", manualCount, w.Tags)
	}
}

func TestUpdateTagsFiltersAutoPrefix(t *testing.T) {
	database := setupTestDB(t)

	pw := &ParsedWorkout{
		Sport:           "running",
		DurationSeconds: 1800,
		DistanceMeters:  5000,
		AvgHeartRate:    140,
	}

	workout, err := Create(database, 1, pw, "filterhash")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// User tries to submit tags with "auto:" prefix — should be filtered out.
	if err := UpdateTags(database, workout.ID, 1, []string{"auto:fake", "legit-tag"}); err != nil {
		t.Fatalf("update tags: %v", err)
	}

	w, err := GetByID(database, workout.ID, 1)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	for _, tag := range w.Tags {
		if tag == "auto:fake" {
			t.Fatal("auto:fake should have been filtered out from user input")
		}
	}
	found := false
	for _, tag := range w.Tags {
		if tag == "legit-tag" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected 'legit-tag' in tags, got %v", w.Tags)
	}
}

func TestDeleteHandler_NotFound(t *testing.T) {
	database := setupTestDB(t)

	req := withUser(httptest.NewRequest(http.MethodDelete, "/api/training/workouts/999", nil), 1)
	req = withChiParam(req, "id", "999")

	w := httptest.NewRecorder()
	DeleteHandler(database)(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestGetHandler_NotFound(t *testing.T) {
	database := setupTestDB(t)

	req := withUser(httptest.NewRequest(http.MethodGet, "/api/training/workouts/999", nil), 1)
	req = withChiParam(req, "id", "999")

	w := httptest.NewRecorder()
	GetHandler(database)(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestUploadHandler_NoFiles(t *testing.T) {
	database := setupTestDB(t)

	req := withUser(httptest.NewRequest(http.MethodPost, "/api/training/upload", nil), 1)
	w := httptest.NewRecorder()
	UploadHandler(database)(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestCompareHandler_MissingParams(t *testing.T) {
	database := setupTestDB(t)

	req := withUser(httptest.NewRequest(http.MethodGet, "/api/training/compare", nil), 1)
	w := httptest.NewRecorder()
	CompareHandler(database)(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestCompareHandler_NotFound(t *testing.T) {
	database := setupTestDB(t)

	req := withUser(httptest.NewRequest(http.MethodGet, "/api/training/compare?a=999&b=998", nil), 1)
	w := httptest.NewRecorder()
	CompareHandler(database)(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestCompareHandler_Success(t *testing.T) {
	database := setupTestDB(t)

	pw1 := &ParsedWorkout{
		Sport: "running", DurationSeconds: 1800, DistanceMeters: 5000, AvgHeartRate: 150,
		Laps: []ParsedLap{{DurationSeconds: 600, DistanceMeters: 1000, AvgSpeedMPerS: 2.0}},
	}
	pw2 := &ParsedWorkout{
		Sport: "running", DurationSeconds: 1800, DistanceMeters: 5000, AvgHeartRate: 148,
		Laps: []ParsedLap{{DurationSeconds: 600, DistanceMeters: 1000, AvgSpeedMPerS: 2.0}},
	}
	w1, err := Create(database, 1, pw1, "cmph1")
	if err != nil {
		t.Fatalf("create w1: %v", err)
	}
	w2, err := Create(database, 1, pw2, "cmph2")
	if err != nil {
		t.Fatalf("create w2: %v", err)
	}

	url := fmt.Sprintf("/api/training/compare?a=%d&b=%d", w1.ID, w2.ID)
	req := withUser(httptest.NewRequest(http.MethodGet, url, nil), 1)
	w := httptest.NewRecorder()
	CompareHandler(database)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestZonesHandler_NotFound(t *testing.T) {
	database := setupTestDB(t)

	req := withUser(httptest.NewRequest(http.MethodGet, "/api/training/workouts/999/zones", nil), 1)
	req = withChiParam(req, "id", "999")
	w := httptest.NewRecorder()
	ZonesHandler(database)(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestZonesHandler_NoSamples(t *testing.T) {
	database := setupTestDB(t)

	pw := &ParsedWorkout{Sport: "running", DurationSeconds: 1800, DistanceMeters: 5000, AvgHeartRate: 150}
	workout, err := Create(database, 1, pw, "znoshash")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	url := fmt.Sprintf("/api/training/workouts/%d/zones", workout.ID)
	req := withUser(httptest.NewRequest(http.MethodGet, url, nil), 1)
	req = withChiParam(req, "id", strconv.FormatInt(workout.ID, 10))
	w := httptest.NewRecorder()
	ZonesHandler(database)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
}

func TestWeeklySummaries(t *testing.T) {
	database := setupTestDB(t)

	pw := &ParsedWorkout{Sport: "running", DurationSeconds: 3600, DistanceMeters: 10000, AvgHeartRate: 150}
	_, err := Create(database, 1, pw, "wkhash")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	summaries, err := WeeklySummaries(database, 1)
	if err != nil {
		t.Fatalf("summaries: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(summaries))
	}
	if summaries[0].WorkoutCount != 1 {
		t.Fatalf("expected 1 workout count, got %d", summaries[0].WorkoutCount)
	}
	if summaries[0].TotalDuration != 3600 {
		t.Fatalf("expected total duration 3600, got %d", summaries[0].TotalDuration)
	}
}

func TestWeeklySummaries_IncludesNoHRWorkouts(t *testing.T) {
	database := setupTestDB(t)

	// Workout without HR data should still count toward weekly totals.
	pw := &ParsedWorkout{Sport: "running", DurationSeconds: 1800, DistanceMeters: 5000, AvgHeartRate: 0}
	_, err := Create(database, 1, pw, "nohrwk")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	summaries, err := WeeklySummaries(database, 1)
	if err != nil {
		t.Fatalf("summaries: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected 1 summary (no-HR workout counted), got %d", len(summaries))
	}
	if summaries[0].WorkoutCount != 1 {
		t.Fatalf("expected workout_count=1, got %d", summaries[0].WorkoutCount)
	}
}

func TestGetProgression(t *testing.T) {
	database := setupTestDB(t)

	pw := &ParsedWorkout{Sport: "running", DurationSeconds: 3600, DistanceMeters: 10000, AvgHeartRate: 150}
	workout, err := Create(database, 1, pw, "proghash")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := UpdateTags(database, workout.ID, 1, []string{"6x6"}); err != nil {
		t.Fatalf("update tags: %v", err)
	}

	groups, err := GetProgression(database, 1)
	if err != nil {
		t.Fatalf("progression: %v", err)
	}
	// May have 2 groups: auto:treadmill (from no-GPS test workout) + 6x6 (manual tag).
	var found bool
	for _, g := range groups {
		if g.Tag == "6x6" {
			found = true
			if len(g.Workouts) != 1 {
				t.Fatalf("expected 1 workout in '6x6' group, got %d", len(g.Workouts))
			}
			break
		}
	}
	if !found {
		t.Fatalf("expected '6x6' group in progression, got %v", groups)
	}
}

func TestCompareHandler_LapSelection_Valid(t *testing.T) {
	database := setupTestDB(t)

	// Workout A: 3 laps; workout B: 4 laps — incompatible in auto mode.
	idA := insertTestWorkoutWithHR(t, database, 1, "running",
		[]int{150, 160, 155}, []float64{300, 300, 300})
	idB := insertTestWorkoutWithHR(t, database, 1, "running",
		[]int{148, 158, 153, 140}, []float64{300, 300, 300, 300})

	url := fmt.Sprintf("/api/training/compare?a=%d&b=%d&laps_a=0,1&laps_b=0,1", idA, idB)
	req := withUser(httptest.NewRequest(http.MethodGet, url, nil), 1)
	w := httptest.NewRecorder()
	CompareHandler(database)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	cmp, ok := resp["comparison"].(map[string]any)
	if !ok {
		t.Fatal("expected comparison object")
	}
	if cmp["compatible"] != true {
		t.Errorf("expected compatible=true, got %v", cmp["compatible"])
	}
}

func TestCompareHandler_LapSelection_InvalidIntegers(t *testing.T) {
	database := setupTestDB(t)

	req := withUser(httptest.NewRequest(http.MethodGet, "/api/training/compare?a=1&b=2&laps_a=0,x&laps_b=0,1", nil), 1)
	w := httptest.NewRecorder()
	CompareHandler(database)(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestCompareHandler_LapSelection_OnlyOneProvided(t *testing.T) {
	database := setupTestDB(t)

	req := withUser(httptest.NewRequest(http.MethodGet, "/api/training/compare?a=1&b=2&laps_a=0,1", nil), 1)
	w := httptest.NewRecorder()
	CompareHandler(database)(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when only laps_a provided, got %d", w.Code)
	}
}

func TestCompareHandler_LapSelection_EmptyParams(t *testing.T) {
	database := setupTestDB(t)

	// Both params present but empty — should be rejected, not silently fall back to auto mode.
	req := withUser(httptest.NewRequest(http.MethodGet, "/api/training/compare?a=1&b=2&laps_a=&laps_b=", nil), 1)
	w := httptest.NewRecorder()
	CompareHandler(database)(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty laps params, got %d", w.Code)
	}
}

func TestGetZoneDistribution(t *testing.T) {
	database := setupTestDB(t)

	pw := &ParsedWorkout{
		Sport: "running", DurationSeconds: 3600, DistanceMeters: 10000, AvgHeartRate: 150,
		Samples: []Sample{
			{OffsetMs: 0, HeartRate: 130, SpeedMPerS: 3.0},
			{OffsetMs: 1000, HeartRate: 155, SpeedMPerS: 3.0},
			{OffsetMs: 2000, HeartRate: 170, SpeedMPerS: 3.0},
		},
	}
	workout, err := Create(database, 1, pw, "zonehash")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	zones, err := GetZoneDistribution(database, workout.ID, 1, hrzones.GetDefaultZones(220))
	if err != nil {
		t.Fatalf("zones: %v", err)
	}
	if len(zones) != 5 {
		t.Fatalf("expected 5 zones, got %d", len(zones))
	}
	var total float64
	for _, z := range zones {
		total += z.Percentage
	}
	if total < 99 || total > 101 {
		t.Fatalf("expected ~100%% total percentage, got %.1f", total)
	}
}

// TestGetZoneDistribution_BoundaryConditions ensures that HR samples at exactly
// maxHR and above maxHR are captured by the last zone, so totals remain ~100%.
func TestGetZoneDistribution_BoundaryConditions(t *testing.T) {
	database := setupTestDB(t)
	const maxHR = 180

	// Three samples: one at maxHR, one above maxHR, one in a mid zone.
	pw := &ParsedWorkout{
		Sport: "running", DurationSeconds: 600, DistanceMeters: 2000, AvgHeartRate: 170,
		Samples: []Sample{
			{OffsetMs: 0, HeartRate: maxHR, SpeedMPerS: 3.0},     // exactly maxHR
			{OffsetMs: 1000, HeartRate: maxHR + 5, SpeedMPerS: 3.0}, // above maxHR
			{OffsetMs: 2000, HeartRate: 120, SpeedMPerS: 3.0},    // well below maxHR
		},
	}
	workout, err := Create(database, 1, pw, "boundaryhash")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	zones, err := GetZoneDistribution(database, workout.ID, 1, hrzones.GetDefaultZones(maxHR))
	if err != nil {
		t.Fatalf("zones: %v", err)
	}
	if len(zones) != 5 {
		t.Fatalf("expected 5 zones, got %d", len(zones))
	}

	var total float64
	for _, z := range zones {
		total += z.Percentage
	}
	if total < 99 || total > 101 {
		t.Fatalf("expected ~100%% total percentage, got %.1f", total)
	}

	// The last zone (zone 5) must have captured the two high-HR samples (2 of 2 intervals).
	lastZone := zones[4]
	if lastZone.DurationS == 0 {
		t.Fatalf("expected last zone to have non-zero duration for HR >= maxHR samples")
	}
}

// TestScheduleBackgroundAnalysis_AdminEnabled_Fires verifies that
// scheduleBackgroundAnalysis triggers RunClaudeAnalysis for an admin user
// with the claude_ai feature, claude_enabled config set, and a saved
// workout_context row.
func TestScheduleBackgroundAnalysis_AdminEnabled_Fires(t *testing.T) {
	database := setupTestDB(t)

	_, err := database.Exec(`
		INSERT INTO workouts (id, user_id, sport, title, started_at, created_at, fit_file_hash, duration_seconds)
		VALUES (1, 1, 'running', 'Test Run', '2024-01-01T10:00:00Z', '2024-01-01T10:00:00Z', 'hash1', 1800)`)
	if err != nil {
		t.Fatalf("create workout: %v", err)
	}
	if err := saveWorkoutContext(database, &WorkoutContext{
		WorkoutID: 1,
		Surface:   "road",
		RunType:   "easy",
		HRSource:  "wrist",
		FeelNotes: "Felt fine.",
	}); err != nil {
		t.Fatalf("seed workout_context: %v", err)
	}

	if err := auth.SetPreference(database, 1, "claude_enabled", "true"); err != nil {
		t.Fatalf("set pref: %v", err)
	}

	called := make(chan struct{}, 1)
	origFunc := runPromptFunc
	runPromptFunc = func(ctx context.Context, cfg *ClaudeConfig, prompt string) (string, error) {
		select {
		case called <- struct{}{}:
		default:
		}
		return `{"type":"easy_run","tag":"easy","summary":"Easy run","title":"Easy Run"}`, nil
	}
	t.Cleanup(func() { runPromptFunc = origFunc })

	// Admin users automatically have claude_ai feature enabled.
	scheduleBackgroundAnalysis(database, 1, true, []Workout{{ID: 1}})

	select {
	case <-called:
		// success: background analysis fired
	case <-time.After(2 * time.Second):
		t.Fatal("timeout: background analysis did not fire within 2s")
	}
}

// TestScheduleBackgroundAnalysis_NoContext_Skips verifies that the FIT-import
// auto-trigger does NOT call Claude when no workout_context row has been saved.
// The user must capture context first; the orchestrator otherwise sends an
// incomplete prompt that produces low-quality classifications.
func TestScheduleBackgroundAnalysis_NoContext_Skips(t *testing.T) {
	database := setupTestDB(t)

	_, err := database.Exec(`
		INSERT INTO workouts (id, user_id, sport, title, started_at, created_at, fit_file_hash, duration_seconds)
		VALUES (1, 1, 'running', 'Test Run', '2024-01-01T10:00:00Z', '2024-01-01T10:00:00Z', 'hash1', 1800)`)
	if err != nil {
		t.Fatalf("create workout: %v", err)
	}

	if err := auth.SetPreference(database, 1, "claude_enabled", "true"); err != nil {
		t.Fatalf("set pref: %v", err)
	}

	called := false
	origFunc := runPromptFunc
	runPromptFunc = func(ctx context.Context, cfg *ClaudeConfig, prompt string) (string, error) {
		called = true
		return "", nil
	}
	t.Cleanup(func() { runPromptFunc = origFunc })

	scheduleBackgroundAnalysis(database, 1, true, []Workout{{ID: 1}})

	// Allow time for any spurious goroutines.
	time.Sleep(150 * time.Millisecond)
	if called {
		t.Fatal("expected no Claude call when workout_context is missing")
	}

	// analysis_status must remain unset (no "pending" left behind).
	var status string
	if err := database.QueryRow(`SELECT analysis_status FROM workouts WHERE id = 1`).Scan(&status); err != nil {
		t.Fatalf("query status: %v", err)
	}
	if status != "" {
		t.Errorf("analysis_status = %q, want empty when context missing", status)
	}
}

// TestScheduleBackgroundAnalysis_ContextSaved_Fires verifies that once the user
// saves a workout_context row, a subsequent invocation runs Claude exactly once
// (matches the pattern from the FIT-import flow where the user uploads, then
// later saves context, and a re-trigger should now succeed).
func TestScheduleBackgroundAnalysis_ContextSaved_Fires(t *testing.T) {
	database := setupTestDB(t)

	_, err := database.Exec(`
		INSERT INTO workouts (id, user_id, sport, title, started_at, created_at, fit_file_hash, duration_seconds)
		VALUES (1, 1, 'running', 'Test Run', '2024-01-01T10:00:00Z', '2024-01-01T10:00:00Z', 'hash1', 1800)`)
	if err != nil {
		t.Fatalf("create workout: %v", err)
	}

	if err := auth.SetPreference(database, 1, "claude_enabled", "true"); err != nil {
		t.Fatalf("set pref: %v", err)
	}

	// First pass: no context — must not fire.
	calls := 0
	origFunc := runPromptFunc
	runPromptFunc = func(ctx context.Context, cfg *ClaudeConfig, prompt string) (string, error) {
		calls++
		return `{"type":"easy_run","tag":"easy","summary":"Easy run","title":"Easy Run"}`, nil
	}
	t.Cleanup(func() { runPromptFunc = origFunc })

	scheduleBackgroundAnalysis(database, 1, true, []Workout{{ID: 1}})
	time.Sleep(100 * time.Millisecond)
	if calls != 0 {
		t.Fatalf("expected 0 Claude calls before context, got %d", calls)
	}

	// User saves context, then we retry — analysis must run exactly once.
	if err := saveWorkoutContext(database, &WorkoutContext{
		WorkoutID: 1,
		Surface:   "road",
		RunType:   "tempo",
		HRSource:  "chest_strap",
		FeelNotes: "Strong tempo.",
	}); err != nil {
		t.Fatalf("seed workout_context: %v", err)
	}

	scheduleBackgroundAnalysis(database, 1, true, []Workout{{ID: 1}})
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && calls == 0 {
		time.Sleep(20 * time.Millisecond)
	}
	if calls != 1 {
		t.Fatalf("expected exactly 1 Claude call after context saved, got %d", calls)
	}
}

// TestScheduleBackgroundAnalysis_NonAdmin_DoesNotFire verifies that
// scheduleBackgroundAnalysis does NOT trigger for non-admin users.
func TestScheduleBackgroundAnalysis_NonAdmin_DoesNotFire(t *testing.T) {
	database := setupTestDB(t)

	origFunc := runPromptFunc
	called := false
	runPromptFunc = func(ctx context.Context, cfg *ClaudeConfig, prompt string) (string, error) {
		called = true
		return "", nil
	}
	t.Cleanup(func() { runPromptFunc = origFunc })

	scheduleBackgroundAnalysis(database, 1, false, []Workout{{ID: 1}})

	// Allow time for any spurious goroutines.
	time.Sleep(100 * time.Millisecond)

	if called {
		t.Fatal("expected no analysis triggered for non-admin user")
	}
}

// --- ACRTrendHandler ---

func TestACRTrendHandler_DefaultWeeks(t *testing.T) {
	database := setupTestDB(t)

	req := withUser(httptest.NewRequest(http.MethodGet, "/api/training/acr-trend", nil), 1)
	w := httptest.NewRecorder()
	ACRTrendHandler(database)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	trend, ok := resp["acr_trend"].([]any)
	if !ok {
		t.Fatalf("expected acr_trend array, got %T", resp["acr_trend"])
	}
	// Default is 26 weeks.
	if len(trend) != 26 {
		t.Errorf("expected 26 points by default, got %d", len(trend))
	}
}

func TestACRTrendHandler_CustomWeeks(t *testing.T) {
	database := setupTestDB(t)

	req := withUser(httptest.NewRequest(http.MethodGet, "/api/training/acr-trend?weeks=4", nil), 1)
	w := httptest.NewRecorder()
	ACRTrendHandler(database)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	trend, ok := resp["acr_trend"].([]any)
	if !ok {
		t.Fatalf("expected acr_trend array")
	}
	if len(trend) != 4 {
		t.Errorf("expected 4 points for ?weeks=4, got %d", len(trend))
	}
}

func TestACRTrendHandler_FallsBackToDefaultWhenWeeksOverLimit(t *testing.T) {
	database := setupTestDB(t)

	// weeks=200 is above the 104 limit — handler falls back to the default (26).
	req := withUser(httptest.NewRequest(http.MethodGet, "/api/training/acr-trend?weeks=200", nil), 1)
	w := httptest.NewRecorder()
	ACRTrendHandler(database)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	trend, ok := resp["acr_trend"].([]any)
	if !ok {
		t.Fatalf("expected acr_trend array")
	}
	// Invalid value falls back to default (26).
	if len(trend) != 26 {
		t.Errorf("expected 26 points when weeks=200 (over limit), got %d", len(trend))
	}
}

func TestACRTrendHandler_PointsContainDateField(t *testing.T) {
	database := setupTestDB(t)

	req := withUser(httptest.NewRequest(http.MethodGet, "/api/training/acr-trend?weeks=2", nil), 1)
	w := httptest.NewRecorder()
	ACRTrendHandler(database)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	trend, ok := resp["acr_trend"].([]any)
	if !ok || len(trend) == 0 {
		t.Fatal("expected non-empty acr_trend array")
	}

	first, ok := trend[0].(map[string]any)
	if !ok {
		t.Fatalf("expected point to be a map, got %T", trend[0])
	}
	if _, has := first["date"]; !has {
		t.Errorf("expected 'date' field in ACR trend point, got keys: %v", first)
	}
}

// buildMinimalFITBytes encodes a minimal valid Activity FIT file into memory.
func buildMinimalFITBytes(t *testing.T) []byte {
	t.Helper()
	now := time.Date(2024, 6, 1, 8, 0, 0, 0, time.UTC)
	act := &filedef.Activity{
		FileId: mesgdef.FileId{
			Type:        typedef.FileActivity,
			Manufacturer: typedef.ManufacturerGarmin,
			TimeCreated:  now,
		},
		Activity: &mesgdef.Activity{
			Timestamp:   now.Add(30 * time.Minute),
			NumSessions: 1,
			Type:        typedef.ActivityManual,
			Event:       typedef.EventActivity,
			EventType:   typedef.EventTypeStop,
		},
		Sessions: []*mesgdef.Session{
			{
				Timestamp:        now.Add(30 * time.Minute),
				StartTime:        now,
				TotalElapsedTime: 1800000,
				TotalDistance:    10000,
				Sport:            typedef.SportRunning,
				Event:            typedef.EventSession,
				EventType:        typedef.EventTypeStopDisableAll,
				AvgHeartRate:     150,
				MaxHeartRate:     175,
			},
		},
	}
	fit := act.ToFIT(nil)
	var buf bytes.Buffer
	enc := fitencoder.New(&buf)
	if err := enc.Encode(&fit); err != nil {
		t.Fatalf("encode FIT: %v", err)
	}
	return buf.Bytes()
}

// buildUploadRequest constructs a multipart POST request with one or more files
// uploaded under the "files" field. Filenames may be empty to simulate mobile
// browsers that omit the filename.
func buildUploadRequest(t *testing.T, filenames []string, contents [][]byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for i, filename := range filenames {
		fw, err := mw.CreateFormFile("files", filename)
		if err != nil {
			t.Fatalf("create form file %d: %v", i, err)
		}
		if _, err := fw.Write(contents[i]); err != nil {
			t.Fatalf("write file %d: %v", i, err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/training/upload", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req
}

// TestUploadHandler_NonFitFilename_ValidFIT verifies that a valid FIT file
// uploaded with a non-".fit" filename (as mobile browsers send) is accepted.
// This is the primary regression test for the mobile upload fix.
func TestUploadHandler_NonFitFilename_ValidFIT(t *testing.T) {
	database := setupTestDB(t)
	fitData := buildMinimalFITBytes(t)

	req := withUser(buildUploadRequest(t, []string{"activity"}, [][]byte{fitData}), 1)
	w := httptest.NewRecorder()
	UploadHandler(database)(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	imported, ok := resp["imported"].([]any)
	if !ok || len(imported) != 1 {
		t.Fatalf("expected 1 imported workout, got: %v", resp["imported"])
	}
}

// TestUploadHandler_InvalidFIT_ErrorContainsFilename verifies that when a file
// with a non-".fit" filename fails to parse, the error message uses the filename
// as the label.
func TestUploadHandler_InvalidFIT_ErrorContainsFilename(t *testing.T) {
	database := setupTestDB(t)

	req := withUser(buildUploadRequest(t, []string{"workout.xyz"}, [][]byte{[]byte("not a fit file")}), 1)
	w := httptest.NewRecorder()
	UploadHandler(database)(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	errs, ok := resp["errors"].([]any)
	if !ok || len(errs) != 1 {
		t.Fatalf("expected 1 error, got: %v", resp["errors"])
	}
	errMsg, _ := errs[0].(string)
	if !strings.HasPrefix(errMsg, "workout.xyz:") {
		t.Errorf("expected error to start with 'workout.xyz:', got %q", errMsg)
	}
}

// TestUploadHandler_MultiFile_PerFileErrorLabels verifies that when multiple
// files are uploaded and some fail, each error is labelled with its own filename.
func TestUploadHandler_MultiFile_PerFileErrorLabels(t *testing.T) {
	database := setupTestDB(t)
	fitData := buildMinimalFITBytes(t)

	// Two files: one valid FIT ("run.fit"), one invalid content ("data").
	req := withUser(buildUploadRequest(t,
		[]string{"run.fit", "data"},
		[][]byte{fitData, []byte("not a fit file")},
	), 1)
	w := httptest.NewRecorder()
	UploadHandler(database)(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	imported, ok := resp["imported"].([]any)
	if !ok || len(imported) != 1 {
		t.Fatalf("expected 1 imported workout, got: %v", resp["imported"])
	}
	errs, ok := resp["errors"].([]any)
	if !ok || len(errs) != 1 {
		t.Fatalf("expected 1 error, got: %v", resp["errors"])
	}
	errMsg, _ := errs[0].(string)
	if !strings.HasPrefix(errMsg, "data:") {
		t.Errorf("expected error to start with 'data:', got %q", errMsg)
	}
}
