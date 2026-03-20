package training

import (
	"database/sql"
	"encoding/json"
	"testing"
	"time"
)

func TestComparisonAnalysisCacheRoundTrip(t *testing.T) {
	db := setupTestDB(t)

	// Create two workouts.
	idA := insertTestWorkout(t, db, 1, "running", 300, 300)
	idB := insertTestWorkout(t, db, 1, "running", 310, 290)

	// Initially no cache.
	cached, err := GetCachedComparisonAnalysis(db, idA, idB, 1)
	if err != nil {
		t.Fatal(err)
	}
	if cached != nil {
		t.Fatal("expected nil for uncached comparison")
	}

	// Save analysis.
	analysis := &ComparisonAnalysis{
		Summary:      "Workout B shows improvement over A",
		Strengths:    []string{"Lower HR at similar pace"},
		Weaknesses:   []string{"Slightly less consistent pacing"},
		Observations: []string{"Both workouts have similar structure"},
	}
	if err := SaveComparisonAnalysis(db, idA, idB, 1, analysis, "claude-sonnet-4-6", "test prompt", time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}

	// Retrieve cached.
	cached, err = GetCachedComparisonAnalysis(db, idA, idB, 1)
	if err != nil {
		t.Fatal(err)
	}
	if cached == nil {
		t.Fatal("expected cached comparison analysis")
	}
	if cached.Summary != "Workout B shows improvement over A" {
		t.Errorf("unexpected summary: %s", cached.Summary)
	}
	if cached.Model != "claude-sonnet-4-6" {
		t.Errorf("unexpected model: %s", cached.Model)
	}
	if !cached.Cached {
		t.Error("expected cached=true")
	}
	if len(cached.Strengths) != 1 || cached.Strengths[0] != "Lower HR at similar pace" {
		t.Errorf("unexpected strengths: %v", cached.Strengths)
	}
}

func TestComparisonAnalysisCacheNormalizesOrder(t *testing.T) {
	db := setupTestDB(t)

	idA := insertTestWorkout(t, db, 1, "running", 300)
	idB := insertTestWorkout(t, db, 1, "running", 300)

	analysis := &ComparisonAnalysis{
		Summary:      "Test order normalization",
		Strengths:    []string{},
		Weaknesses:   []string{},
		Observations: []string{},
	}
	// Save with (A, B).
	if err := SaveComparisonAnalysis(db, idA, idB, 1, analysis, "test-model", "", time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}

	// Retrieve with (B, A) — should still hit cache.
	cached, err := GetCachedComparisonAnalysis(db, idB, idA, 1)
	if err != nil {
		t.Fatal(err)
	}
	if cached == nil {
		t.Fatal("expected cache hit with reversed workout IDs")
	}
	if cached.Summary != "Test order normalization" {
		t.Errorf("unexpected summary: %s", cached.Summary)
	}
}

func TestComparisonAnalysisCacheUserScoping(t *testing.T) {
	db := setupTestDB(t)

	// Create a second user.
	_, err := db.Exec(`INSERT INTO users (id, email, name, google_id) VALUES (2, 'other@example.com', 'Other', 'google-2')`)
	if err != nil {
		t.Fatal(err)
	}

	idA := insertTestWorkout(t, db, 1, "running", 300)
	idB := insertTestWorkout(t, db, 1, "running", 300)

	analysis := &ComparisonAnalysis{
		Summary:      "User 1 comparison",
		Strengths:    []string{},
		Weaknesses:   []string{},
		Observations: []string{},
	}
	if err := SaveComparisonAnalysis(db, idA, idB, 1, analysis, "test-model", "", time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}

	// User 1 can see their own.
	cached, err := GetCachedComparisonAnalysis(db, idA, idB, 1)
	if err != nil {
		t.Fatal(err)
	}
	if cached == nil {
		t.Fatal("user 1 should see own cached analysis")
	}

	// User 2 cannot see user 1's analysis.
	cached, err = GetCachedComparisonAnalysis(db, idA, idB, 2)
	if err != nil {
		t.Fatal(err)
	}
	if cached != nil {
		t.Fatal("user 2 should not see user 1's cached analysis")
	}
}

func TestDeleteComparisonAnalysis(t *testing.T) {
	db := setupTestDB(t)

	idA := insertTestWorkout(t, db, 1, "running", 300)
	idB := insertTestWorkout(t, db, 1, "running", 300)

	analysis := &ComparisonAnalysis{
		Summary:      "To be deleted",
		Strengths:    []string{},
		Weaknesses:   []string{},
		Observations: []string{},
	}
	if err := SaveComparisonAnalysis(db, idA, idB, 1, analysis, "test-model", "", time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}

	// Delete it.
	if err := DeleteComparisonAnalysis(db, idA, idB, 1); err != nil {
		t.Fatal(err)
	}

	// Should be gone.
	cached, err := GetCachedComparisonAnalysis(db, idA, idB, 1)
	if err != nil {
		t.Fatal(err)
	}
	if cached != nil {
		t.Fatal("expected nil after deletion")
	}

	// Deleting again should return ErrNoRows.
	if err := DeleteComparisonAnalysis(db, idA, idB, 1); err != sql.ErrNoRows {
		t.Fatalf("expected sql.ErrNoRows when deleting non-existent analysis, got %v", err)
	}
}

func TestDeleteComparisonAnalysis_ReversedOrder(t *testing.T) {
	db := setupTestDB(t)

	idA := insertTestWorkout(t, db, 1, "running", 300)
	idB := insertTestWorkout(t, db, 1, "running", 300)

	analysis := &ComparisonAnalysis{
		Summary:      "Delete with reversed IDs",
		Strengths:    []string{},
		Weaknesses:   []string{},
		Observations: []string{},
	}
	// Save with (A, B).
	if err := SaveComparisonAnalysis(db, idA, idB, 1, analysis, "test-model", "", time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}

	// Delete with (B, A) — should still work due to normalization.
	if err := DeleteComparisonAnalysis(db, idB, idA, 1); err != nil {
		t.Fatal(err)
	}

	cached, err := GetCachedComparisonAnalysis(db, idA, idB, 1)
	if err != nil {
		t.Fatal(err)
	}
	if cached != nil {
		t.Fatal("expected nil after reversed-order deletion")
	}
}

func TestDeleteComparisonAnalysesForWorkout(t *testing.T) {
	db := setupTestDB(t)

	idA := insertTestWorkout(t, db, 1, "running", 300)
	idB := insertTestWorkout(t, db, 1, "running", 300)
	idC := insertTestWorkout(t, db, 1, "running", 300)

	analysis := &ComparisonAnalysis{
		Summary:      "test",
		Strengths:    []string{},
		Weaknesses:   []string{},
		Observations: []string{},
	}
	// Save A vs B and A vs C.
	if err := SaveComparisonAnalysis(db, idA, idB, 1, analysis, "m", "", time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	if err := SaveComparisonAnalysis(db, idA, idC, 1, analysis, "m", "", time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	// Also save B vs C to verify it's NOT deleted.
	if err := SaveComparisonAnalysis(db, idB, idC, 1, analysis, "m", "", time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}

	// Delete all analyses involving workout A.
	if err := DeleteComparisonAnalysesForWorkout(db, idA, 1); err != nil {
		t.Fatal(err)
	}

	// A vs B should be gone.
	cached, err := GetCachedComparisonAnalysis(db, idA, idB, 1)
	if err != nil {
		t.Fatal(err)
	}
	if cached != nil {
		t.Error("expected A vs B to be deleted")
	}
	// A vs C should be gone.
	cached, err = GetCachedComparisonAnalysis(db, idA, idC, 1)
	if err != nil {
		t.Fatal(err)
	}
	if cached != nil {
		t.Error("expected A vs C to be deleted")
	}
	// B vs C should still exist.
	cached, err = GetCachedComparisonAnalysis(db, idB, idC, 1)
	if err != nil {
		t.Fatal(err)
	}
	if cached == nil {
		t.Error("expected B vs C to still exist")
	}
}

func TestComparisonAnalysisNilSlices(t *testing.T) {
	db := setupTestDB(t)

	idA := insertTestWorkout(t, db, 1, "running", 300)
	idB := insertTestWorkout(t, db, 1, "running", 300)

	// Save with nil slices — they should normalize to [] on retrieval.
	analysis := &ComparisonAnalysis{
		Summary: "Nil slice test",
	}
	if err := SaveComparisonAnalysis(db, idA, idB, 1, analysis, "test-model", "", time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}

	cached, err := GetCachedComparisonAnalysis(db, idA, idB, 1)
	if err != nil {
		t.Fatal(err)
	}
	if cached == nil {
		t.Fatal("expected cached analysis")
	}
	if cached.Strengths == nil {
		t.Error("strengths should be [] not nil after cache round-trip")
	}
	if cached.Weaknesses == nil {
		t.Error("weaknesses should be [] not nil after cache round-trip")
	}
	if cached.Observations == nil {
		t.Error("observations should be [] not nil after cache round-trip")
	}

	// Verify JSON serialization produces [] not null.
	data, err := json.Marshal(cached)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !contains(s, `"strengths":[]`) {
		t.Errorf("expected strengths:[], got %s", s)
	}
	if !contains(s, `"weaknesses":[]`) {
		t.Errorf("expected weaknesses:[], got %s", s)
	}
	if !contains(s, `"observations":[]`) {
		t.Errorf("expected observations:[], got %s", s)
	}
}

func TestSaveComparisonAnalysisUpserts(t *testing.T) {
	db := setupTestDB(t)

	idA := insertTestWorkout(t, db, 1, "running", 300)
	idB := insertTestWorkout(t, db, 1, "running", 300)

	analysis1 := &ComparisonAnalysis{
		Summary:      "First version",
		Strengths:    []string{},
		Weaknesses:   []string{},
		Observations: []string{},
	}
	if err := SaveComparisonAnalysis(db, idA, idB, 1, analysis1, "model-1", "", time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}

	// Save again with updated content — should upsert.
	analysis2 := &ComparisonAnalysis{
		Summary:      "Updated version",
		Strengths:    []string{"better"},
		Weaknesses:   []string{},
		Observations: []string{},
	}
	if err := SaveComparisonAnalysis(db, idA, idB, 1, analysis2, "model-2", "", time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}

	cached, err := GetCachedComparisonAnalysis(db, idA, idB, 1)
	if err != nil {
		t.Fatal(err)
	}
	if cached == nil {
		t.Fatal("expected cached analysis after upsert")
	}
	if cached.Summary != "Updated version" {
		t.Errorf("expected updated summary, got: %s", cached.Summary)
	}
	if cached.Model != "model-2" {
		t.Errorf("expected model-2, got: %s", cached.Model)
	}
}

func TestComparisonAnalysisEncryptedAtRest(t *testing.T) {
	db := setupTestDB(t)

	idA := insertTestWorkout(t, db, 1, "running", 300)
	idB := insertTestWorkout(t, db, 1, "running", 300)

	analysis := &ComparisonAnalysis{
		Summary:      "Encrypted at rest test",
		Strengths:    []string{"good pace"},
		Weaknesses:   []string{},
		Observations: []string{},
	}
	prompt := "Compare these two workouts"
	if err := SaveComparisonAnalysis(db, idA, idB, 1, analysis, "test-model", prompt, time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}

	// Read raw values from the database — they should be encrypted ciphertext.
	var rawPrompt, rawResponse string
	err := db.QueryRow(
		"SELECT prompt, response_json FROM comparison_analyses WHERE user_id = 1",
	).Scan(&rawPrompt, &rawResponse)
	if err != nil {
		t.Fatalf("query raw: %v", err)
	}

	if rawPrompt == prompt {
		t.Error("prompt stored in database matches plaintext — expected encrypted ciphertext")
	}
	if rawPrompt == "" {
		t.Error("prompt stored in database is empty")
	}

	// The raw response_json should not contain the plaintext summary.
	if contains(rawResponse, "Encrypted at rest test") {
		t.Error("response_json stored in database contains plaintext summary — expected encrypted ciphertext")
	}
	if rawResponse == "" {
		t.Error("response_json stored in database is empty")
	}

	// Verify round-trip still works (data decrypts correctly).
	cached, err := GetCachedComparisonAnalysis(db, idA, idB, 1)
	if err != nil {
		t.Fatal(err)
	}
	if cached == nil {
		t.Fatal("expected cached analysis")
	}
	if cached.Summary != "Encrypted at rest test" {
		t.Errorf("unexpected summary after decrypt: %s", cached.Summary)
	}
}
