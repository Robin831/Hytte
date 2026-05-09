package training

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func createWorkoutForUser(t *testing.T, database *sql.DB, userID int64, hash string) int64 {
	t.Helper()
	pw := &ParsedWorkout{
		Sport:           "running",
		DurationSeconds: 1800,
		DistanceMeters:  5000,
		AvgHeartRate:    140,
	}
	workout, err := Create(database, userID, pw, hash)
	if err != nil {
		t.Fatalf("create workout: %v", err)
	}
	return workout.ID
}

func putContext(t *testing.T, database *sql.DB, userID, workoutID int64, body WorkoutContext) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPut, "/api/training/workouts/"+strconv.FormatInt(workoutID, 10)+"/context", bytes.NewReader(raw))
	req = withUser(req, userID)
	req = withChiParam(req, "id", strconv.FormatInt(workoutID, 10))
	w := httptest.NewRecorder()
	PutWorkoutContext(database)(w, req)
	return w
}

func getContext(t *testing.T, database *sql.DB, userID, workoutID int64) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/training/workouts/"+strconv.FormatInt(workoutID, 10)+"/context", nil)
	req = withUser(req, userID)
	req = withChiParam(req, "id", strconv.FormatInt(workoutID, 10))
	w := httptest.NewRecorder()
	GetWorkoutContext(database)(w, req)
	return w
}

func decodeContext(t *testing.T, body []byte) WorkoutContext {
	t.Helper()
	var wrapper struct {
		Context WorkoutContext `json:"context"`
	}
	if err := json.Unmarshal(body, &wrapper); err != nil {
		t.Fatalf("unmarshal context: %v (body=%s)", err, string(body))
	}
	return wrapper.Context
}

func TestPutWorkoutContext_CreatesAndDecryptsRoundTrip(t *testing.T) {
	database := setupTestDB(t)
	workoutID := createWorkoutForUser(t, database, 1, "ctx-create-hash")

	body := WorkoutContext{
		Surface:   "trail",
		RunType:   "tempo",
		HRSource:  "chest_strap",
		FeelNotes: "Felt strong on the climbs, legs heavy late.",
		SpeedPlan: []SpeedSegment{
			{Kind: "warmup", SpeedKmph: 9.0, DurationSec: 600, Repeats: 1},
			{Kind: "work", SpeedKmph: 14.5, DurationSec: 180, Repeats: 6},
			{Kind: "recovery", SpeedKmph: 8.0, DurationSec: 90, Repeats: 6, SameAsPrevious: true},
		},
	}

	w := putContext(t, database, 1, workoutID, body)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT: expected 200, got %d (body=%s)", w.Code, w.Body.String())
	}
	saved := decodeContext(t, w.Body.Bytes())
	if saved.WorkoutID != workoutID {
		t.Fatalf("expected workout_id=%d, got %d", workoutID, saved.WorkoutID)
	}
	if saved.FeelNotes != body.FeelNotes {
		t.Fatalf("feel_notes round-trip failed: %q vs %q", saved.FeelNotes, body.FeelNotes)
	}
	if len(saved.SpeedPlan) != len(body.SpeedPlan) {
		t.Fatalf("speed_plan length mismatch: got %d, want %d", len(saved.SpeedPlan), len(body.SpeedPlan))
	}
	if saved.SpeedPlan[1].Kind != "work" || saved.SpeedPlan[1].SpeedKmph != 14.5 {
		t.Fatalf("speed_plan[1] mismatch: %+v", saved.SpeedPlan[1])
	}
	if !saved.SpeedPlan[2].SameAsPrevious {
		t.Fatalf("expected same_as_previous=true on segment 2, got %+v", saved.SpeedPlan[2])
	}

	// GET should return the same data.
	gw := getContext(t, database, 1, workoutID)
	if gw.Code != http.StatusOK {
		t.Fatalf("GET: expected 200, got %d", gw.Code)
	}
	got := decodeContext(t, gw.Body.Bytes())
	if got.FeelNotes != body.FeelNotes {
		t.Fatalf("GET feel_notes mismatch: %q vs %q", got.FeelNotes, body.FeelNotes)
	}

	// Verify ciphertext at rest — the raw DB value must not contain the plaintext.
	var feelEnc, planEnc string
	err := database.QueryRow(`SELECT feel_notes, speed_plan FROM workout_context WHERE workout_id = ?`, workoutID).Scan(&feelEnc, &planEnc)
	if err != nil {
		t.Fatalf("query encrypted columns: %v", err)
	}
	if strings.Contains(feelEnc, body.FeelNotes) {
		t.Fatalf("feel_notes stored in plaintext: %s", feelEnc)
	}
	if !strings.HasPrefix(feelEnc, "enc:") {
		t.Fatalf("expected feel_notes to be encrypted (enc: prefix), got %q", feelEnc)
	}
	if !strings.HasPrefix(planEnc, "enc:") {
		t.Fatalf("expected speed_plan to be encrypted (enc: prefix), got %q", planEnc)
	}
	if strings.Contains(planEnc, "warmup") || strings.Contains(planEnc, "tempo") {
		t.Fatalf("speed_plan stored in plaintext: %s", planEnc)
	}
}

func TestPutWorkoutContext_UpdatesExistingRow(t *testing.T) {
	database := setupTestDB(t)
	workoutID := createWorkoutForUser(t, database, 1, "ctx-update-hash")

	first := WorkoutContext{
		Surface:   "road",
		RunType:   "easy",
		HRSource:  "wrist",
		FeelNotes: "First note",
		SpeedPlan: []SpeedSegment{{Kind: "steady", SpeedKmph: 10.0, DurationSec: 1800, Repeats: 1}},
	}
	if w := putContext(t, database, 1, workoutID, first); w.Code != http.StatusOK {
		t.Fatalf("first PUT: expected 200, got %d", w.Code)
	}

	second := WorkoutContext{
		Surface:   "trail",
		RunType:   "long",
		HRSource:  "chest_strap",
		FeelNotes: "Updated note",
		SpeedPlan: []SpeedSegment{{Kind: "steady", SpeedKmph: 9.5, DurationSec: 3600, Repeats: 1}},
	}
	w := putContext(t, database, 1, workoutID, second)
	if w.Code != http.StatusOK {
		t.Fatalf("second PUT: expected 200, got %d (body=%s)", w.Code, w.Body.String())
	}

	gw := getContext(t, database, 1, workoutID)
	if gw.Code != http.StatusOK {
		t.Fatalf("GET: expected 200, got %d", gw.Code)
	}
	got := decodeContext(t, gw.Body.Bytes())
	if got.Surface != "trail" || got.RunType != "long" || got.HRSource != "chest_strap" {
		t.Fatalf("plain fields not updated: %+v", got)
	}
	if got.FeelNotes != "Updated note" {
		t.Fatalf("feel_notes not updated: %q", got.FeelNotes)
	}
	if len(got.SpeedPlan) != 1 || got.SpeedPlan[0].SpeedKmph != 9.5 || got.SpeedPlan[0].DurationSec != 3600 {
		t.Fatalf("speed_plan not updated: %+v", got.SpeedPlan)
	}

	// Confirm only one row exists.
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM workout_context WHERE workout_id = ?`, workoutID).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 context row after update, got %d", count)
	}
}

func TestGetWorkoutContext_MissingContextReturns404(t *testing.T) {
	database := setupTestDB(t)
	workoutID := createWorkoutForUser(t, database, 1, "ctx-missing-hash")

	w := getContext(t, database, 1, workoutID)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing context, got %d (body=%s)", w.Code, w.Body.String())
	}
}

func TestGetWorkoutContext_DifferentUserGets404(t *testing.T) {
	database := setupTestDB(t)
	if _, err := database.Exec(`INSERT INTO users (id, email, name, google_id) VALUES (2, 'other@example.com', 'Other', 'google-2')`); err != nil {
		t.Fatalf("create second user: %v", err)
	}
	workoutID := createWorkoutForUser(t, database, 1, "ctx-cross-user-hash")

	// User 1 saves context.
	body := WorkoutContext{Surface: "road", RunType: "easy", HRSource: "wrist", FeelNotes: "private"}
	if w := putContext(t, database, 1, workoutID, body); w.Code != http.StatusOK {
		t.Fatalf("PUT (user 1): expected 200, got %d", w.Code)
	}

	// User 2 must not be able to read or write that workout.
	if w := getContext(t, database, 2, workoutID); w.Code != http.StatusNotFound {
		t.Fatalf("GET (user 2): expected 404, got %d", w.Code)
	}
	if w := putContext(t, database, 2, workoutID, body); w.Code != http.StatusNotFound {
		t.Fatalf("PUT (user 2): expected 404, got %d", w.Code)
	}
}

func TestPutWorkoutContext_InvalidJSON(t *testing.T) {
	database := setupTestDB(t)
	workoutID := createWorkoutForUser(t, database, 1, "ctx-invalid-json-hash")

	req := httptest.NewRequest(http.MethodPut, "/api/training/workouts/"+strconv.FormatInt(workoutID, 10)+"/context", strings.NewReader("{not json"))
	req = withUser(req, 1)
	req = withChiParam(req, "id", strconv.FormatInt(workoutID, 10))
	w := httptest.NewRecorder()
	PutWorkoutContext(database)(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}
