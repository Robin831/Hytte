package lactate

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/go-chi/chi/v5"
	_ "modernc.org/sqlite"
)

func withUser(r *http.Request, userID int64) *http.Request {
	user := &auth.User{ID: userID, Email: "test@example.com", Name: "Test"}
	ctx := auth.ContextWithUser(r.Context(), user)
	return r.WithContext(ctx)
}

func withChiParam(r *http.Request, key, value string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, value)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

const samplePayload = `{
	"date": "2026-03-14",
	"comment": "Morning test",
	"protocol_type": "standard",
	"warmup_duration_min": 10,
	"stage_duration_min": 5,
	"start_speed_kmh": 11.5,
	"speed_increment_kmh": 0.5,
	"stages": [
		{"stage_number": 0, "speed_kmh": 10.0, "lactate_mmol": 1.2, "heart_rate_bpm": 130},
		{"stage_number": 1, "speed_kmh": 11.5, "lactate_mmol": 1.5, "heart_rate_bpm": 145, "rpe": 12},
		{"stage_number": 2, "speed_kmh": 12.0, "lactate_mmol": 2.1, "heart_rate_bpm": 155},
		{"stage_number": 3, "speed_kmh": 12.5, "lactate_mmol": 3.4, "heart_rate_bpm": 165},
		{"stage_number": 4, "speed_kmh": 13.0, "lactate_mmol": 5.2, "heart_rate_bpm": 175}
	]
}`

func TestListHandler_Empty(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("GET", "/api/lactate/tests", nil), 1)
	rec := httptest.NewRecorder()
	ListHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body struct {
		Tests []Test `json:"tests"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Tests) != 0 {
		t.Errorf("expected 0 tests, got %d", len(body.Tests))
	}
}

func TestCreateHandler_Success(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("POST", "/api/lactate/tests", strings.NewReader(samplePayload)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	CreateHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Test Test `json:"test"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Test.Date != "2026-03-14" {
		t.Errorf("date = %q, want %q", body.Test.Date, "2026-03-14")
	}
	if len(body.Test.Stages) != 5 {
		t.Errorf("stages len = %d, want 5", len(body.Test.Stages))
	}
}

func TestCreateHandler_InvalidJSON(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("POST", "/api/lactate/tests", strings.NewReader("not json")), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	CreateHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestCreateHandler_MissingDate(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"stages": [{"stage_number": 0, "speed_kmh": 10.0, "lactate_mmol": 1.2}]}`
	req := withUser(httptest.NewRequest("POST", "/api/lactate/tests", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	CreateHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateHandler_InvalidProtocol(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"date": "2026-03-14", "protocol_type": "invalid"}`
	req := withUser(httptest.NewRequest("POST", "/api/lactate/tests", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	CreateHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateHandler_InvalidRPE(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"date": "2026-03-14", "stages": [{"stage_number": 0, "speed_kmh": 10.0, "lactate_mmol": 1.2, "rpe": 5}]}`
	req := withUser(httptest.NewRequest("POST", "/api/lactate/tests", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	CreateHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGetHandler_Success(t *testing.T) {
	db := setupTestDB(t)

	created, err := Create(db, 1, sampleTest())
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	idStr := strconv.FormatInt(created.ID, 10)
	req := withUser(httptest.NewRequest("GET", "/api/lactate/tests/"+idStr, nil), 1)
	req = withChiParam(req, "id", idStr)
	rec := httptest.NewRecorder()
	GetHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Test Test `json:"test"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Test.Stages) != 5 {
		t.Errorf("stages len = %d, want 5", len(body.Test.Stages))
	}
}

func TestGetHandler_NotFound(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("GET", "/api/lactate/tests/999", nil), 1)
	req = withChiParam(req, "id", "999")
	rec := httptest.NewRecorder()
	GetHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestUpdateHandler_Success(t *testing.T) {
	db := setupTestDB(t)

	created, err := Create(db, 1, sampleTest())
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	idStr := strconv.FormatInt(created.ID, 10)
	updatePayload := `{
		"date": "2026-03-15",
		"comment": "Updated",
		"protocol_type": "custom",
		"stages": [
			{"stage_number": 0, "speed_kmh": 9.0, "lactate_mmol": 1.0, "heart_rate_bpm": 120}
		]
	}`
	req := withUser(httptest.NewRequest("PUT", "/api/lactate/tests/"+idStr, strings.NewReader(updatePayload)), 1)
	req.Header.Set("Content-Type", "application/json")
	req = withChiParam(req, "id", idStr)
	rec := httptest.NewRecorder()
	UpdateHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Test Test `json:"test"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Test.Comment != "Updated" {
		t.Errorf("comment = %q, want %q", body.Test.Comment, "Updated")
	}
	if len(body.Test.Stages) != 1 {
		t.Errorf("stages len = %d, want 1", len(body.Test.Stages))
	}
}

func TestUpdateHandler_NotFound(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"date": "2026-03-15", "stages": []}`
	req := withUser(httptest.NewRequest("PUT", "/api/lactate/tests/999", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	req = withChiParam(req, "id", "999")
	rec := httptest.NewRecorder()
	UpdateHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestDeleteHandler_Success(t *testing.T) {
	db := setupTestDB(t)

	created, err := Create(db, 1, sampleTest())
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	idStr := strconv.FormatInt(created.ID, 10)
	req := withUser(httptest.NewRequest("DELETE", "/api/lactate/tests/"+idStr, nil), 1)
	req = withChiParam(req, "id", idStr)
	rec := httptest.NewRecorder()
	DeleteHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestDeleteHandler_NotFound(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("DELETE", "/api/lactate/tests/999", nil), 1)
	req = withChiParam(req, "id", "999")
	rec := httptest.NewRecorder()
	DeleteHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestCreateHandler_DefaultsApplied(t *testing.T) {
	db := setupTestDB(t)

	payload := `{"date": "2026-03-14", "stages": []}`
	req := withUser(httptest.NewRequest("POST", "/api/lactate/tests", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	CreateHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Test Test `json:"test"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Test.ProtocolType != "standard" {
		t.Errorf("protocol_type = %q, want %q", body.Test.ProtocolType, "standard")
	}
	if body.Test.WarmupDurationMin != 10 {
		t.Errorf("warmup_duration_min = %d, want 10", body.Test.WarmupDurationMin)
	}
	if body.Test.StageDurationMin != 5 {
		t.Errorf("stage_duration_min = %d, want 5", body.Test.StageDurationMin)
	}
	if body.Test.StartSpeedKmh != 11.5 {
		t.Errorf("start_speed_kmh = %f, want 11.5", body.Test.StartSpeedKmh)
	}
	if body.Test.SpeedIncrementKmh != 0.5 {
		t.Errorf("speed_increment_kmh = %f, want 0.5", body.Test.SpeedIncrementKmh)
	}
}
