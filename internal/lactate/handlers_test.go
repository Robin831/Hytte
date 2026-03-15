package lactate

import (
	"context"
	"encoding/json"
	"math"
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

func TestThresholdsHandler_Success(t *testing.T) {
	db := setupTestDB(t)

	test := &Test{
		Date:              "2026-03-14",
		ProtocolType:      "standard",
		WarmupDurationMin: 10,
		StageDurationMin:  5,
		StartSpeedKmh:     11.5,
		SpeedIncrementKmh: 0.5,
		Stages: []Stage{
			{StageNumber: 0, SpeedKmh: 8.0, LactateMmol: 1.0, HeartRateBpm: 130},
			{StageNumber: 1, SpeedKmh: 9.0, LactateMmol: 1.1, HeartRateBpm: 140},
			{StageNumber: 2, SpeedKmh: 10.0, LactateMmol: 1.3, HeartRateBpm: 150},
			{StageNumber: 3, SpeedKmh: 11.0, LactateMmol: 1.8, HeartRateBpm: 160},
			{StageNumber: 4, SpeedKmh: 12.0, LactateMmol: 2.5, HeartRateBpm: 168},
			{StageNumber: 5, SpeedKmh: 13.0, LactateMmol: 3.5, HeartRateBpm: 175},
			{StageNumber: 6, SpeedKmh: 14.0, LactateMmol: 5.0, HeartRateBpm: 182},
			{StageNumber: 7, SpeedKmh: 15.0, LactateMmol: 8.0, HeartRateBpm: 190},
		},
	}
	created, err := Create(db, 1, test)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	idStr := strconv.FormatInt(created.ID, 10)
	req := withUser(httptest.NewRequest("GET", "/api/lactate/tests/"+idStr+"/thresholds", nil), 1)
	req = withChiParam(req, "id", idStr)
	rec := httptest.NewRecorder()
	ThresholdsHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Thresholds []ThresholdResult `json:"thresholds"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Thresholds) != 5 {
		t.Fatalf("expected 5 thresholds, got %d", len(body.Thresholds))
	}
	// OBLA should be valid with this data set.
	if !body.Thresholds[0].Valid {
		t.Errorf("OBLA should be valid: %s", body.Thresholds[0].Reason)
	}
}

func TestThresholdsHandler_NotFound(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("GET", "/api/lactate/tests/999/thresholds", nil), 1)
	req = withChiParam(req, "id", "999")
	rec := httptest.NewRecorder()
	ThresholdsHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestThresholdsHandler_TooFewStages(t *testing.T) {
	db := setupTestDB(t)

	test := &Test{
		Date:              "2026-03-14",
		ProtocolType:      "standard",
		WarmupDurationMin: 10,
		StageDurationMin:  5,
		StartSpeedKmh:     11.5,
		SpeedIncrementKmh: 0.5,
		Stages: []Stage{
			{StageNumber: 0, SpeedKmh: 10.0, LactateMmol: 1.2, HeartRateBpm: 130},
		},
	}
	created, err := Create(db, 1, test)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	idStr := strconv.FormatInt(created.ID, 10)
	req := withUser(httptest.NewRequest("GET", "/api/lactate/tests/"+idStr+"/thresholds", nil), 1)
	req = withChiParam(req, "id", idStr)
	rec := httptest.NewRecorder()
	ThresholdsHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCalculateHandler_Success(t *testing.T) {
	payload := `{"stages": [
		{"stage_number": 0, "speed_kmh": 8.0, "lactate_mmol": 1.0, "heart_rate_bpm": 130},
		{"stage_number": 1, "speed_kmh": 9.0, "lactate_mmol": 1.1, "heart_rate_bpm": 140},
		{"stage_number": 2, "speed_kmh": 10.0, "lactate_mmol": 1.3, "heart_rate_bpm": 150},
		{"stage_number": 3, "speed_kmh": 11.0, "lactate_mmol": 1.8, "heart_rate_bpm": 160},
		{"stage_number": 4, "speed_kmh": 12.0, "lactate_mmol": 2.5, "heart_rate_bpm": 168},
		{"stage_number": 5, "speed_kmh": 13.0, "lactate_mmol": 3.5, "heart_rate_bpm": 175},
		{"stage_number": 6, "speed_kmh": 14.0, "lactate_mmol": 5.0, "heart_rate_bpm": 182},
		{"stage_number": 7, "speed_kmh": 15.0, "lactate_mmol": 8.0, "heart_rate_bpm": 190}
	]}`
	req := withUser(httptest.NewRequest("POST", "/api/lactate/calculate", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	CalculateHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Thresholds []ThresholdResult `json:"thresholds"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Thresholds) != 5 {
		t.Fatalf("expected 5 thresholds, got %d", len(body.Thresholds))
	}
}

func TestCalculateHandler_TooFewStages(t *testing.T) {
	payload := `{"stages": [{"stage_number": 0, "speed_kmh": 10.0, "lactate_mmol": 1.0}]}`
	req := withUser(httptest.NewRequest("POST", "/api/lactate/calculate", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	CalculateHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCalculateHandler_InvalidJSON(t *testing.T) {
	req := withUser(httptest.NewRequest("POST", "/api/lactate/calculate", strings.NewReader("not json")), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	CalculateHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestCalculateHandler_InvalidSpeed(t *testing.T) {
	payload := `{"stages": [
		{"stage_number": 0, "speed_kmh": -1.0, "lactate_mmol": 1.0},
		{"stage_number": 1, "speed_kmh": 10.0, "lactate_mmol": 2.0}
	]}`
	req := withUser(httptest.NewRequest("POST", "/api/lactate/calculate", strings.NewReader(payload)), 1)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	CalculateHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCalculateHandler_BodyTooLarge(t *testing.T) {
	// Build a payload larger than 1 MB
	large := strings.Repeat(`{"stage_number":0,"speed_kmh":10.0,"lactate_mmol":1.0},`, 25000)
	payload := `{"stages": [` + large[:len(large)-1] + `]}`

	req := httptest.NewRequest("POST", "/api/lactate/calculate", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	CalculateHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for oversized body, got %d", rec.Code)
	}
}

func TestCalculateHandler_NaNSpeed(t *testing.T) {
	// Go's JSON decoder doesn't parse NaN literals, but we test the validation
	// by ensuring the handler rejects non-finite values at the validation layer.
	// Since JSON doesn't support NaN, we test via validateTestInput directly.
	payload := `{"stages": [
		{"stage_number": 0, "speed_kmh": 10.0, "lactate_mmol": 1.0},
		{"stage_number": 1, "speed_kmh": 0, "lactate_mmol": 2.0}
	]}`
	req := httptest.NewRequest("POST", "/api/lactate/calculate", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	CalculateHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for zero speed, got %d", rec.Code)
	}
}

func TestValidateTestInput_NaNFloats(t *testing.T) {
	nan := math.NaN()
	inf := math.Inf(1)

	tests := []struct {
		name  string
		input testInput
		want  string
	}{
		{
			name:  "NaN start_speed",
			input: testInput{Date: "2026-01-01", StartSpeedKmh: &nan},
			want:  "start_speed_kmh must be a finite number",
		},
		{
			name:  "Inf speed_increment",
			input: testInput{Date: "2026-01-01", SpeedIncrementKmh: &inf},
			want:  "speed_increment_kmh must be a finite number",
		},
		{
			name: "NaN stage speed",
			input: testInput{
				Date:   "2026-01-01",
				Stages: []stageInput{{SpeedKmh: nan, LactateMmol: 1.0}},
			},
			want: "stage speed_kmh must be a finite number",
		},
		{
			name: "Inf stage lactate",
			input: testInput{
				Date:   "2026-01-01",
				Stages: []stageInput{{SpeedKmh: 10.0, LactateMmol: inf}},
			},
			want: "stage lactate_mmol must be a finite number",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := validateTestInput(&tt.input)
			if got != tt.want {
				t.Errorf("validateTestInput() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAnalysisHandler_Success(t *testing.T) {
	db := setupTestDB(t)

	test := &Test{
		Date:              "2026-03-14",
		ProtocolType:      "standard",
		WarmupDurationMin: 10,
		StageDurationMin:  5,
		StartSpeedKmh:     11.5,
		SpeedIncrementKmh: 0.5,
		Stages: []Stage{
			{StageNumber: 0, SpeedKmh: 8.0, LactateMmol: 1.0, HeartRateBpm: 130},
			{StageNumber: 1, SpeedKmh: 9.0, LactateMmol: 1.1, HeartRateBpm: 140},
			{StageNumber: 2, SpeedKmh: 10.0, LactateMmol: 1.3, HeartRateBpm: 150},
			{StageNumber: 3, SpeedKmh: 11.0, LactateMmol: 1.8, HeartRateBpm: 160},
			{StageNumber: 4, SpeedKmh: 12.0, LactateMmol: 2.5, HeartRateBpm: 168},
			{StageNumber: 5, SpeedKmh: 13.0, LactateMmol: 3.5, HeartRateBpm: 175},
			{StageNumber: 6, SpeedKmh: 14.0, LactateMmol: 5.0, HeartRateBpm: 182},
			{StageNumber: 7, SpeedKmh: 15.0, LactateMmol: 8.0, HeartRateBpm: 190},
		},
	}
	created, err := Create(db, 1, test)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	idStr := strconv.FormatInt(created.ID, 10)
	req := withUser(httptest.NewRequest("GET", "/api/lactate/tests/"+idStr+"/analysis", nil), 1)
	req = withChiParam(req, "id", idStr)
	rec := httptest.NewRecorder()
	AnalysisHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Thresholds   []ThresholdResult   `json:"thresholds"`
		Zones        []ZonesResult       `json:"zones"`
		Predictions  []RacePrediction    `json:"predictions"`
		TrafficLight []StageTrafficLight `json:"traffic_lights"`
		MethodUsed   string              `json:"method_used"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Thresholds) != 5 {
		t.Errorf("expected 5 thresholds, got %d", len(body.Thresholds))
	}
	if len(body.Zones) != 2 {
		t.Errorf("expected 2 zone systems, got %d", len(body.Zones))
	}
	if len(body.Predictions) != 4 {
		t.Errorf("expected 4 predictions, got %d", len(body.Predictions))
	}
	if len(body.TrafficLight) != 8 {
		t.Errorf("expected 8 traffic lights, got %d", len(body.TrafficLight))
	}
	if body.MethodUsed == "" {
		t.Error("expected method_used to be set")
	}

	// Verify zones have both systems
	systems := map[ZoneSystem]bool{}
	for _, z := range body.Zones {
		systems[z.System] = true
	}
	if !systems[ZoneSystemOlympiatoppen] {
		t.Error("missing olympiatoppen zones")
	}
	if !systems[ZoneSystemNorwegian] {
		t.Error("missing norwegian zones")
	}
}

func TestAnalysisHandler_NotFound(t *testing.T) {
	db := setupTestDB(t)

	req := withUser(httptest.NewRequest("GET", "/api/lactate/tests/999/analysis", nil), 1)
	req = withChiParam(req, "id", "999")
	rec := httptest.NewRecorder()
	AnalysisHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestAnalysisHandler_TooFewStages(t *testing.T) {
	db := setupTestDB(t)

	test := &Test{
		Date:              "2026-03-14",
		ProtocolType:      "standard",
		WarmupDurationMin: 10,
		StageDurationMin:  5,
		StartSpeedKmh:     11.5,
		SpeedIncrementKmh: 0.5,
		Stages: []Stage{
			{StageNumber: 0, SpeedKmh: 10.0, LactateMmol: 1.2, HeartRateBpm: 130},
		},
	}
	created, err := Create(db, 1, test)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	idStr := strconv.FormatInt(created.ID, 10)
	req := withUser(httptest.NewRequest("GET", "/api/lactate/tests/"+idStr+"/analysis", nil), 1)
	req = withChiParam(req, "id", idStr)
	rec := httptest.NewRecorder()
	AnalysisHandler(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
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
