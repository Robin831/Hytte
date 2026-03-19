package training

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestListComparisonAnalyses_Empty(t *testing.T) {
	db := setupTestDB(t)

	req := httptest.NewRequest(http.MethodGet, "/api/training/compare/analyses", nil)
	req = withAdminUser(req, 1)
	w := httptest.NewRecorder()

	ListComparisonAnalysesHandler(db)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var result []ComparisonAnalysisSummary
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if len(result) != 0 {
		t.Fatalf("expected empty list, got %d items", len(result))
	}
}

func TestListComparisonAnalyses_ReturnsUserAnalyses(t *testing.T) {
	db := setupTestDB(t)

	idA := insertTestWorkout(t, db, 1, "running", 300)
	idB := insertTestWorkout(t, db, 1, "running", 310)
	idC := insertTestWorkout(t, db, 1, "running", 320)

	now := time.Now().UTC()
	analysis := &ComparisonAnalysis{
		Summary:      "A vs B comparison",
		Strengths:    []string{"good"},
		Weaknesses:   []string{},
		Observations: []string{},
	}
	if err := SaveComparisonAnalysis(db, idA, idB, 1, analysis, "claude-sonnet-4-6", "prompt1", now.Add(-time.Hour).Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}

	analysis2 := &ComparisonAnalysis{
		Summary:      "A vs C comparison",
		Strengths:    []string{},
		Weaknesses:   []string{},
		Observations: []string{},
	}
	if err := SaveComparisonAnalysis(db, idA, idC, 1, analysis2, "claude-sonnet-4-6", "prompt2", now.Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/training/compare/analyses", nil)
	req = withAdminUser(req, 1)
	w := httptest.NewRecorder()

	ListComparisonAnalysesHandler(db)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var result []ComparisonAnalysisSummary
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 analyses, got %d", len(result))
	}
	// Most recent first.
	if result[0].Summary != "A vs C comparison" {
		t.Errorf("expected most recent first, got: %s", result[0].Summary)
	}
	if result[1].Summary != "A vs B comparison" {
		t.Errorf("expected older second, got: %s", result[1].Summary)
	}
}

func TestListComparisonAnalyses_UserScoping(t *testing.T) {
	db := setupTestDB(t)

	// Create second user.
	_, err := db.Exec(`INSERT INTO users (id, email, name, google_id) VALUES (2, 'other@example.com', 'Other', 'google-2')`)
	if err != nil {
		t.Fatal(err)
	}

	idA := insertTestWorkout(t, db, 1, "running", 300)
	idB := insertTestWorkout(t, db, 1, "running", 310)

	analysis := &ComparisonAnalysis{
		Summary:      "User 1 analysis",
		Strengths:    []string{},
		Weaknesses:   []string{},
		Observations: []string{},
	}
	if err := SaveComparisonAnalysis(db, idA, idB, 1, analysis, "model", "prompt", time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}

	// User 2 should see empty list.
	req := httptest.NewRequest(http.MethodGet, "/api/training/compare/analyses", nil)
	req = withAdminUser(req, 2)
	w := httptest.NewRecorder()

	ListComparisonAnalysesHandler(db)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var result []ComparisonAnalysisSummary
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if len(result) != 0 {
		t.Fatalf("user 2 should see 0 analyses, got %d", len(result))
	}
}

func TestGetComparisonAnalysisHandler_Success(t *testing.T) {
	db := setupTestDB(t)

	idA := insertTestWorkout(t, db, 1, "running", 300)
	idB := insertTestWorkout(t, db, 1, "running", 310)

	analysis := &ComparisonAnalysis{
		Summary:      "Detailed analysis",
		Strengths:    []string{"pace improvement"},
		Weaknesses:   []string{"HR drift"},
		Observations: []string{"similar route"},
	}
	if err := SaveComparisonAnalysis(db, idA, idB, 1, analysis, "claude-sonnet-4-6", "test prompt", time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}

	// Get the ID of the saved analysis.
	var analysisID int64
	if err := db.QueryRow(`SELECT id FROM comparison_analyses WHERE user_id = 1`).Scan(&analysisID); err != nil {
		t.Fatal(err)
	}

	idStr := fmt.Sprintf("%d", analysisID)
	req := httptest.NewRequest(http.MethodGet, "/api/training/compare/analyses/"+idStr, nil)
	req = withAdminUser(req, 1)
	req = withChiParam(req, "id", idStr)
	w := httptest.NewRecorder()

	GetComparisonAnalysisHandler(db)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var result CachedComparisonAnalysis
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.Summary != "Detailed analysis" {
		t.Errorf("unexpected summary: %s", result.Summary)
	}
	if len(result.Strengths) != 1 {
		t.Errorf("expected 1 strength, got %d", len(result.Strengths))
	}
}

func TestGetComparisonAnalysisHandler_NotFound(t *testing.T) {
	db := setupTestDB(t)

	req := httptest.NewRequest(http.MethodGet, "/api/training/compare/analyses/999", nil)
	req = withAdminUser(req, 1)
	req = withChiParam(req, "id", "999")
	w := httptest.NewRecorder()

	GetComparisonAnalysisHandler(db)(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestGetComparisonAnalysisHandler_InvalidID(t *testing.T) {
	db := setupTestDB(t)

	req := httptest.NewRequest(http.MethodGet, "/api/training/compare/analyses/abc", nil)
	req = withAdminUser(req, 1)
	req = withChiParam(req, "id", "abc")
	w := httptest.NewRecorder()

	GetComparisonAnalysisHandler(db)(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestDeleteComparisonAnalysisHandler_Success(t *testing.T) {
	db := setupTestDB(t)

	idA := insertTestWorkout(t, db, 1, "running", 300)
	idB := insertTestWorkout(t, db, 1, "running", 310)

	analysis := &ComparisonAnalysis{
		Summary:      "To delete",
		Strengths:    []string{},
		Weaknesses:   []string{},
		Observations: []string{},
	}
	if err := SaveComparisonAnalysis(db, idA, idB, 1, analysis, "model", "prompt", time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}

	var analysisID int64
	if err := db.QueryRow(`SELECT id FROM comparison_analyses WHERE user_id = 1`).Scan(&analysisID); err != nil {
		t.Fatal(err)
	}

	idStr := fmt.Sprintf("%d", analysisID)
	req := httptest.NewRequest(http.MethodDelete, "/api/training/compare/analyses/"+idStr, nil)
	req = withAdminUser(req, 1)
	req = withChiParam(req, "id", idStr)
	w := httptest.NewRecorder()

	DeleteComparisonAnalysisHandler(db)(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	// Verify it's gone.
	cached, err := GetComparisonAnalysisByID(db, analysisID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if cached != nil {
		t.Fatal("expected analysis to be deleted")
	}
}

func TestDeleteComparisonAnalysisHandler_NotFound(t *testing.T) {
	db := setupTestDB(t)

	req := httptest.NewRequest(http.MethodDelete, "/api/training/compare/analyses/999", nil)
	req = withAdminUser(req, 1)
	req = withChiParam(req, "id", "999")
	w := httptest.NewRecorder()

	DeleteComparisonAnalysisHandler(db)(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestDeleteComparisonAnalysisHandler_UserScoping(t *testing.T) {
	db := setupTestDB(t)

	// Create second user.
	_, err := db.Exec(`INSERT INTO users (id, email, name, google_id) VALUES (2, 'other@example.com', 'Other', 'google-2')`)
	if err != nil {
		t.Fatal(err)
	}

	idA := insertTestWorkout(t, db, 1, "running", 300)
	idB := insertTestWorkout(t, db, 1, "running", 310)

	analysis := &ComparisonAnalysis{
		Summary:      "User 1 only",
		Strengths:    []string{},
		Weaknesses:   []string{},
		Observations: []string{},
	}
	if err := SaveComparisonAnalysis(db, idA, idB, 1, analysis, "model", "prompt", time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}

	// Query the actual ID of user 1's analysis.
	var analysisID int64
	if err := db.QueryRow(`SELECT id FROM comparison_analyses WHERE user_id = 1`).Scan(&analysisID); err != nil {
		t.Fatal(err)
	}

	// User 2 tries to delete user 1's analysis — should get 404.
	idStr := fmt.Sprintf("%d", analysisID)
	req := httptest.NewRequest(http.MethodDelete, "/api/training/compare/analyses/"+idStr, nil)
	req = withAdminUser(req, 2)
	req = withChiParam(req, "id", idStr)
	w := httptest.NewRecorder()

	DeleteComparisonAnalysisHandler(db)(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for other user's analysis, got %d", w.Code)
	}
}

func TestListComparisonAnalysesStorage(t *testing.T) {
	db := setupTestDB(t)

	idA := insertTestWorkout(t, db, 1, "running", 300)
	idB := insertTestWorkout(t, db, 1, "running", 310)

	analysis := &ComparisonAnalysis{
		Summary:      "Storage test",
		Strengths:    []string{"s1"},
		Weaknesses:   []string{},
		Observations: []string{},
	}
	if err := SaveComparisonAnalysis(db, idA, idB, 1, analysis, "test-model", "prompt", time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}

	results, err := ListComparisonAnalyses(db, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1, got %d", len(results))
	}
	if results[0].Summary != "Storage test" {
		t.Errorf("unexpected summary: %s", results[0].Summary)
	}
	if results[0].Model != "test-model" {
		t.Errorf("unexpected model: %s", results[0].Model)
	}
	if results[0].ID == 0 {
		t.Error("expected non-zero ID")
	}
}

func TestGetComparisonAnalysisByID_NotFound(t *testing.T) {
	db := setupTestDB(t)

	result, err := GetComparisonAnalysisByID(db, 999, 1)
	if err != nil {
		t.Fatal(err)
	}
	if result != nil {
		t.Fatal("expected nil for non-existent analysis")
	}
}

func TestDeleteComparisonAnalysisByID_NotFound(t *testing.T) {
	db := setupTestDB(t)

	err := DeleteComparisonAnalysisByID(db, 999, 1)
	if err != sql.ErrNoRows {
		t.Fatalf("expected sql.ErrNoRows, got %v", err)
	}
}
