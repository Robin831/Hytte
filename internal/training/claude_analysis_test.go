package training

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	json.Unmarshal(w.Body.Bytes(), &resp)
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
