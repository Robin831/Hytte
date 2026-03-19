package training

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestAITagsHandler_NonAdmin(t *testing.T) {
	database := setupTestDB(t)

	req := httptest.NewRequest(http.MethodPost, "/api/training/workouts/1/ai-tags", nil)
	req = withUser(req, 1)
	req = withChiParam(req, "id", "1")
	w := httptest.NewRecorder()

	AITagsHandler(database)(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestAITagsHandler_Disabled(t *testing.T) {
	database := setupTestDB(t)

	req := httptest.NewRequest(http.MethodPost, "/api/training/workouts/1/ai-tags", nil)
	req = withAdminUser(req, 1)
	req = withChiParam(req, "id", "1")
	w := httptest.NewRecorder()

	AITagsHandler(database)(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if !strings.Contains(resp["error"], "not enabled") {
		t.Errorf("expected 'not enabled' error, got: %s", resp["error"])
	}
}

func TestAIFeedbackHandler_NonAdmin(t *testing.T) {
	database := setupTestDB(t)

	req := httptest.NewRequest(http.MethodPost, "/api/training/workouts/1/ai-feedback", nil)
	req = withUser(req, 1)
	req = withChiParam(req, "id", "1")
	w := httptest.NewRecorder()

	AIFeedbackHandler(database)(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestAIFeedbackHandler_Disabled(t *testing.T) {
	database := setupTestDB(t)

	req := httptest.NewRequest(http.MethodPost, "/api/training/workouts/1/ai-feedback", nil)
	req = withAdminUser(req, 1)
	req = withChiParam(req, "id", "1")
	w := httptest.NewRecorder()

	AIFeedbackHandler(database)(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestAICompareInsightsHandler_NonAdmin(t *testing.T) {
	database := setupTestDB(t)

	body := `{"workout_a_id": 1, "workout_b_id": 2}`
	req := httptest.NewRequest(http.MethodPost, "/api/training/compare/ai-insights", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, 1)
	w := httptest.NewRecorder()

	AICompareInsightsHandler(database)(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestAICompareInsightsHandler_MissingIDs(t *testing.T) {
	database := setupTestDB(t)

	body := `{"workout_a_id": 0}`
	req := httptest.NewRequest(http.MethodPost, "/api/training/compare/ai-insights", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withAdminUser(req, 1)
	w := httptest.NewRecorder()

	AICompareInsightsHandler(database)(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestParseTagResponse_JSON(t *testing.T) {
	tags := parseTagResponse(`["tempo run", "intervals", "high intensity"]`)
	if len(tags) != 3 {
		t.Fatalf("expected 3 tags, got %d: %v", len(tags), tags)
	}
	if tags[0] != "tempo run" {
		t.Errorf("expected 'tempo run', got '%s'", tags[0])
	}
}

func TestParseTagResponse_EmbeddedJSON(t *testing.T) {
	tags := parseTagResponse(`Here are the tags: ["long run", "easy pace"] based on your data.`)
	if len(tags) != 2 {
		t.Fatalf("expected 2 tags, got %d: %v", len(tags), tags)
	}
}

func TestParseTagResponse_PlainText(t *testing.T) {
	tags := parseTagResponse("- tempo run\n- hill repeats\n- high cadence")
	if len(tags) != 3 {
		t.Fatalf("expected 3 tags, got %d: %v", len(tags), tags)
	}
	if tags[0] != "tempo run" {
		t.Errorf("expected 'tempo run', got '%s'", tags[0])
	}
}

func TestAITagsHandler_InvalidID(t *testing.T) {
	database := setupTestDB(t)

	req := httptest.NewRequest(http.MethodPost, "/api/training/workouts/abc/ai-tags", nil)
	req = withAdminUser(req, 1)
	req = withChiParam(req, "id", "abc")
	w := httptest.NewRecorder()

	AITagsHandler(database)(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestAICompareInsightsHandler_InvalidJSON(t *testing.T) {
	database := setupTestDB(t)

	req := httptest.NewRequest(http.MethodPost, "/api/training/compare/ai-insights", strings.NewReader("not json"))
	req = withAdminUser(req, 1)
	w := httptest.NewRecorder()

	AICompareInsightsHandler(database)(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

// enableClaude sets Claude preferences so handlers pass the "enabled" check.
func enableClaude(t *testing.T, database *sql.DB, userID int64) {
	t.Helper()
	for _, kv := range [][2]string{
		{"claude_enabled", "true"},
		{"claude_cli_path", "claude"},
	} {
		_, err := database.Exec(
			`INSERT OR REPLACE INTO user_preferences (user_id, key, value) VALUES (?, ?, ?)`,
			userID, kv[0], kv[1],
		)
		if err != nil {
			t.Fatalf("set preference %s: %v", kv[0], err)
		}
	}
}

// createTestWorkout inserts a minimal workout owned by userID and returns its ID.
func createTestWorkout(t *testing.T, database *sql.DB, userID int64) int64 {
	t.Helper()
	pw := &ParsedWorkout{
		Sport:           "running",
		DurationSeconds: 1800,
		DistanceMeters:  5000,
		AvgHeartRate:    145,
		MaxHeartRate:    170,
	}
	workout, err := Create(database, userID, pw, "testhash_"+t.Name())
	if err != nil {
		t.Fatalf("create workout: %v", err)
	}
	return workout.ID
}

func TestAITagsHandler_WorkoutNotFound(t *testing.T) {
	database := setupTestDB(t)
	enableClaude(t, database, 1)

	req := httptest.NewRequest(http.MethodPost, "/api/training/workouts/99999/ai-tags", nil)
	req = withAdminUser(req, 1)
	req = withChiParam(req, "id", "99999")
	w := httptest.NewRecorder()

	AITagsHandler(database)(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestAIFeedbackHandler_WorkoutNotFound(t *testing.T) {
	database := setupTestDB(t)
	enableClaude(t, database, 1)

	req := httptest.NewRequest(http.MethodPost, "/api/training/workouts/99999/ai-feedback", nil)
	req = withAdminUser(req, 1)
	req = withChiParam(req, "id", "99999")
	w := httptest.NewRecorder()

	AIFeedbackHandler(database)(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestAICompareInsightsHandler_WorkoutNotFound(t *testing.T) {
	database := setupTestDB(t)
	enableClaude(t, database, 1)

	body := `{"workout_a_id": 99999, "workout_b_id": 99998}`
	req := httptest.NewRequest(http.MethodPost, "/api/training/compare/ai-insights", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withAdminUser(req, 1)
	w := httptest.NewRecorder()

	AICompareInsightsHandler(database)(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestAITagsHandler_ReachesCLI(t *testing.T) {
	database := setupTestDB(t)
	enableClaude(t, database, 1)
	wID := createTestWorkout(t, database, 1)

	req := httptest.NewRequest(http.MethodPost, "/api/training/workouts/"+strconv.FormatInt(wID, 10)+"/ai-tags", nil)
	req = withAdminUser(req, 1)
	req = withChiParam(req, "id", strconv.FormatInt(wID, 10))
	w := httptest.NewRecorder()

	AITagsHandler(database)(w, req)

	// Without a real Claude CLI binary, the handler returns 500 after passing
	// all validation (admin, enabled, workout exists) and reaching RunPrompt.
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 (CLI unavailable), got %d", w.Code)
	}
	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !strings.Contains(resp["error"], "AI analysis failed") {
		t.Errorf("expected 'AI analysis failed' error, got: %s", resp["error"])
	}
}

func TestAIFeedbackHandler_ReachesCLI(t *testing.T) {
	database := setupTestDB(t)
	enableClaude(t, database, 1)
	wID := createTestWorkout(t, database, 1)

	req := httptest.NewRequest(http.MethodPost, "/api/training/workouts/"+strconv.FormatInt(wID, 10)+"/ai-feedback", nil)
	req = withAdminUser(req, 1)
	req = withChiParam(req, "id", strconv.FormatInt(wID, 10))
	w := httptest.NewRecorder()

	AIFeedbackHandler(database)(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 (CLI unavailable), got %d", w.Code)
	}
	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !strings.Contains(resp["error"], "AI analysis failed") {
		t.Errorf("expected 'AI analysis failed' error, got: %s", resp["error"])
	}
}

// createTestWorkoutWithHash inserts a minimal workout with a custom hash suffix.
func createTestWorkoutWithHash(t *testing.T, database *sql.DB, userID int64, hashSuffix string) int64 {
	t.Helper()
	pw := &ParsedWorkout{
		Sport:           "running",
		DurationSeconds: 1800,
		DistanceMeters:  5000,
		AvgHeartRate:    145,
		MaxHeartRate:    170,
	}
	workout, err := Create(database, userID, pw, "testhash_"+t.Name()+"_"+hashSuffix)
	if err != nil {
		t.Fatalf("create workout: %v", err)
	}
	return workout.ID
}

func TestAICompareInsightsHandler_ReachesCLI(t *testing.T) {
	database := setupTestDB(t)
	enableClaude(t, database, 1)
	wAID := createTestWorkoutWithHash(t, database, 1, "a")
	wBID := createTestWorkoutWithHash(t, database, 1, "b")

	body := fmt.Sprintf(`{"workout_a_id": %d, "workout_b_id": %d}`, wAID, wBID)
	req := httptest.NewRequest(http.MethodPost, "/api/training/compare/ai-insights", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withAdminUser(req, 1)
	w := httptest.NewRecorder()

	AICompareInsightsHandler(database)(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 (CLI unavailable), got %d", w.Code)
	}
	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !strings.Contains(resp["error"], "AI analysis failed") {
		t.Errorf("expected 'AI analysis failed' error, got: %s", resp["error"])
	}
}
