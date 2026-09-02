package training

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// makeWorkout is a helper that builds a minimal Workout for estimation tests.
func makeWorkout(durationSeconds, distanceMeters int, avgHR, maxHR int, tags []string) *Workout {
	return &Workout{
		ID:              1,
		UserID:          1,
		Sport:           "running",
		DurationSeconds: durationSeconds,
		DistanceMeters:  float64(distanceMeters),
		AvgHeartRate:    avgHR,
		MaxHeartRate:    maxHR,
		Tags:            tags,
	}
}

// TestEstimateVO2max_SteadyStateWithRestingHR tests the Daniels formula when
// resting HR is provided (HRR method).
func TestEstimateVO2max_SteadyStateWithRestingHR(t *testing.T) {
	// 45 min easy run at 10 km/h (~167 m/min), avg HR 140, max HR 185, resting HR 55.
	w := makeWorkout(45*60, 7500, 140, 185, nil)
	restingHR := 55

	est, err := EstimateVO2max(w, &restingHR)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if est == nil {
		t.Fatal("expected an estimate, got nil")
	}
	if est.Method != "daniels" {
		t.Errorf("expected method 'daniels', got %q", est.Method)
	}
	if est.VO2max < 30 || est.VO2max > 70 {
		t.Errorf("VO2max %v outside plausible range [30, 70]", est.VO2max)
	}
	if est.WorkoutID != w.ID {
		t.Errorf("expected workout_id %d, got %d", w.ID, est.WorkoutID)
	}
	if est.EstimatedAt == "" {
		t.Error("estimated_at should not be empty")
	}
}

// TestEstimateVO2max_SteadyStateWithoutRestingHR tests the Daniels formula
// when no resting HR is provided (%HRmax approximation).
func TestEstimateVO2max_SteadyStateWithoutRestingHR(t *testing.T) {
	// 30 min steady run, avg HR 155 (84% of 185).
	w := makeWorkout(30*60, 5000, 155, 185, nil)

	est, err := EstimateVO2max(w, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if est == nil {
		t.Fatal("expected an estimate, got nil")
	}
	if est.Method != "daniels" {
		t.Errorf("expected method 'daniels', got %q", est.Method)
	}
	if est.VO2max < 25 || est.VO2max > 75 {
		t.Errorf("VO2max %v outside plausible range [25, 75]", est.VO2max)
	}
}

// TestEstimateVO2max_SkipIntervals verifies that structured interval workouts
// are excluded from VO2max estimation.
func TestEstimateVO2max_SkipIntervals(t *testing.T) {
	w := makeWorkout(40*60, 8000, 165, 185, []string{"auto:6x6m (r1m)"})
	restingHR := 55

	est, err := EstimateVO2max(w, &restingHR)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if est != nil {
		t.Errorf("expected nil for interval workout, got estimate with VO2max=%.1f", est.VO2max)
	}
}

// TestEstimateVO2max_SkipShortWorkout verifies that workouts under 15 minutes
// are skipped.
func TestEstimateVO2max_SkipShortWorkout(t *testing.T) {
	w := makeWorkout(10*60, 2000, 150, 185, nil)

	est, err := EstimateVO2max(w, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if est != nil {
		t.Errorf("expected nil for short workout, got estimate with VO2max=%.1f", est.VO2max)
	}
}

// TestEstimateVO2max_HRRatioFallback tests the Uth formula when no distance
// data is available but resting HR is provided.
func TestEstimateVO2max_HRRatioFallback(t *testing.T) {
	// Workout without distance (e.g. pure HR-only recording).
	w := makeWorkout(30*60, 0, 145, 185, nil)
	restingHR := 55

	est, err := EstimateVO2max(w, &restingHR)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if est == nil {
		t.Fatal("expected hr_ratio fallback estimate, got nil")
	}
	if est.Method != "hr_ratio" {
		t.Errorf("expected method 'hr_ratio', got %q", est.Method)
	}
	// 15.3 × (185/55) ≈ 51.5
	if est.VO2max < 40 || est.VO2max > 65 {
		t.Errorf("VO2max %v outside expected range for hr_ratio", est.VO2max)
	}
}

// TestEstimateVO2max_SkipHillRepeats verifies that repeats tagged as intervals
// (e.g. hill repeats producing an auto:Nx tag) are skipped.
func TestEstimateVO2max_SkipHillRepeats(t *testing.T) {
	w := makeWorkout(35*60, 5000, 168, 185, []string{"auto:10x2m (r90s)"})

	est, err := EstimateVO2max(w, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if est != nil {
		t.Errorf("expected nil for hill repeat workout, got estimate with VO2max=%.1f", est.VO2max)
	}
}

// TestSaveAndGetVO2maxHistory tests the full storage round-trip.
func TestSaveAndGetVO2maxHistory(t *testing.T) {
	db := setupTestDB(t)

	// Insert a workout to satisfy FK constraint.
	insertWorkout(t, db)

	est := &VO2maxEstimate{
		UserID:      1,
		WorkoutID:   1,
		VO2max:      52.3,
		Method:      "daniels",
		EstimatedAt: "2026-01-01T10:00:00Z",
	}
	if err := SaveVO2maxEstimate(db, est); err != nil {
		t.Fatalf("SaveVO2maxEstimate: %v", err)
	}
	if est.ID == 0 {
		t.Error("expected ID to be set after save")
	}

	history, err := GetVO2maxHistory(db, 1, 10)
	if err != nil {
		t.Fatalf("GetVO2maxHistory: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("expected 1 history entry, got %d", len(history))
	}
	if history[0].VO2max != 52.3 {
		t.Errorf("expected VO2max 52.3, got %v", history[0].VO2max)
	}

	latest, err := GetLatestVO2max(db, 1)
	if err != nil {
		t.Fatalf("GetLatestVO2max: %v", err)
	}
	if latest.VO2max != 52.3 {
		t.Errorf("expected latest VO2max 52.3, got %v", latest.VO2max)
	}
}

// vo2maxResponse mirrors the JSON shape returned by GetVO2maxHandler.
type vo2maxResponse struct {
	History []VO2maxEstimate `json:"history"`
	Latest  *VO2maxEstimate  `json:"latest"`
	Trend   string           `json:"trend"`
	Summary *vo2maxSummary   `json:"summary"`
}

// TestGetVO2maxHandler_Empty tests the handler when no estimates exist.
func TestGetVO2maxHandler_Empty(t *testing.T) {
	db := setupTestDB(t)

	req := httptest.NewRequest(http.MethodGet, "/api/training/vo2max", nil)
	req = withUser(req, 1)
	rr := httptest.NewRecorder()

	GetVO2maxHandler(db).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", ct)
	}

	var resp vo2maxResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.History == nil {
		t.Error("history should be an empty array, not null")
	}
	if len(resp.History) != 0 {
		t.Errorf("expected empty history, got %d entries", len(resp.History))
	}
	if resp.Latest != nil {
		t.Errorf("expected latest to be null, got %+v", resp.Latest)
	}
	if resp.Trend != "stable" {
		t.Errorf("expected trend 'stable' for empty history, got %q", resp.Trend)
	}
	if resp.Summary != nil {
		t.Errorf("expected summary to be null for empty history, got %+v", resp.Summary)
	}
}

// TestGetVO2maxHandler_WithHistory tests the handler returning history, the
// median/range summary and a trend suppressed by the estimates' spread.
func TestGetVO2maxHandler_WithHistory(t *testing.T) {
	db := setupTestDB(t)
	// Insert 5 workouts with VO2max 46..50 — a 4-unit spread, which is estimator
	// scatter rather than a readable slope, so the trend must be "noisy".
	insertWorkoutsForVO2max(t, db, 5)

	req := httptest.NewRequest(http.MethodGet, "/api/training/vo2max", nil)
	req = withUser(req, 1)
	rr := httptest.NewRecorder()

	GetVO2maxHandler(db).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}

	var resp vo2maxResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.History) != 5 {
		t.Errorf("expected 5 history entries, got %d", len(resp.History))
	}
	if resp.Latest == nil {
		t.Fatal("expected latest to be non-null")
	}
	// insertWorkoutsForVO2max stores VO2max = 45 + i, so latest (i=5) should be 50.
	if resp.Latest.VO2max != 50 {
		t.Errorf("expected latest VO2max 50, got %v", resp.Latest.VO2max)
	}
	if resp.Trend != "noisy" {
		t.Errorf("expected trend 'noisy' for a 4-unit spread, got %q", resp.Trend)
	}
	if resp.Summary == nil {
		t.Fatal("expected a summary alongside the history")
	}
	if resp.Summary.Count != 5 {
		t.Errorf("expected summary count 5, got %d", resp.Summary.Count)
	}
	if resp.Summary.Median != 48 {
		t.Errorf("expected summary median 48, got %v", resp.Summary.Median)
	}
	if resp.Summary.Low != 46 || resp.Summary.High != 50 {
		t.Errorf("expected summary range 46-50, got %v-%v", resp.Summary.Low, resp.Summary.High)
	}
	if resp.Summary.Spread != 4 {
		t.Errorf("expected summary spread 4, got %v", resp.Summary.Spread)
	}
	if !resp.Summary.Noisy {
		t.Error("expected summary to be flagged noisy for a 4-unit spread")
	}
}

// TestComputeVO2maxTrend tests trend calculation across various scenarios.
func TestComputeVO2maxTrend(t *testing.T) {
	cases := []struct {
		name     string
		values   []float64
		expected string
	}{
		{"empty", nil, "stable"},
		{"single", []float64{50}, "stable"},
		{"improving", []float64{48.0, 48.5, 49.0, 49.5, 50.0}, "improving"},
		{"declining", []float64{50.0, 49.5, 49.0, 48.5, 48.0}, "declining"},
		{"stable", []float64{50, 50.1, 49.9, 50.2, 50}, "stable"},
		// A wide spread is estimator scatter, not fitness change: no trend to read,
		// however cleanly the values happen to line up.
		{"noisy ascending", []float64{45, 47, 49, 51, 53}, "noisy"},
		{"noisy scattered", []float64{37, 56, 44, 51, 41}, "noisy"},
		// Only the trend window is considered, so an old outlier cannot poison a
		// tight run of recent estimates.
		{"old outlier outside window", []float64{37, 48.0, 48.5, 49.0, 49.5, 50.0}, "improving"},
		// Zero values are placeholders, not estimates, and must not widen the range.
		{"zeros ignored", []float64{0, 50, 50.1, 49.9, 50.2, 50}, "stable"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var history []VO2maxEstimate
			for _, v := range tc.values {
				history = append(history, VO2maxEstimate{VO2max: v})
			}
			got, _ := computeVO2maxTrend(history)
			if got != tc.expected {
				t.Errorf("expected %q, got %q", tc.expected, got)
			}
		})
	}
}

// TestComputeVO2maxSummary checks the median/range reported alongside the trend.
func TestComputeVO2maxSummary(t *testing.T) {
	history := []VO2maxEstimate{
		{VO2max: 0}, // placeholder, ignored
		{VO2max: 37},
		{VO2max: 56},
		{VO2max: 44},
		{VO2max: 51},
		{VO2max: 41},
	}

	trend, summary := computeVO2maxTrend(history)
	if trend != "noisy" {
		t.Errorf("expected trend 'noisy', got %q", trend)
	}
	if summary == nil {
		t.Fatal("expected a summary")
	}
	if summary.Count != 5 {
		t.Errorf("expected count 5 (zero excluded), got %d", summary.Count)
	}
	if summary.Median != 44 {
		t.Errorf("expected median 44, got %v", summary.Median)
	}
	if summary.Low != 37 || summary.High != 56 {
		t.Errorf("expected range 37-56, got %v-%v", summary.Low, summary.High)
	}
	if summary.Spread != 19 {
		t.Errorf("expected spread 19, got %v", summary.Spread)
	}
	if !summary.Noisy {
		t.Error("expected the 19-unit spread to be flagged noisy")
	}

	// An even-sized window averages the two middle values.
	_, evenSummary := computeVO2maxTrend([]VO2maxEstimate{{VO2max: 48}, {VO2max: 49}})
	if evenSummary == nil || evenSummary.Median != 48.5 {
		t.Errorf("expected median 48.5 for an even window, got %+v", evenSummary)
	}

	// A single estimate still reports a summary, with no spread to speak of.
	singleTrend, singleSummary := computeVO2maxTrend([]VO2maxEstimate{{VO2max: 44.2}})
	if singleTrend != "stable" {
		t.Errorf("expected trend 'stable' for a single estimate, got %q", singleTrend)
	}
	if singleSummary == nil || singleSummary.Count != 1 || singleSummary.Median != 44.2 || singleSummary.Noisy {
		t.Errorf("unexpected summary for a single estimate: %+v", singleSummary)
	}
}

// insertWorkout inserts a minimal workout with ID=1 for FK purposes.
func insertWorkout(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.Exec(`
		INSERT OR IGNORE INTO workouts (id, user_id, sport, started_at, duration_seconds, created_at)
		VALUES (1, 1, 'running', '2026-01-01T10:00:00Z', 3600, '2026-01-01T10:00:00Z')`)
	if err != nil {
		t.Fatalf("insert workout: %v", err)
	}
}

// insertWorkoutsForVO2max inserts n workouts and corresponding VO2max estimates.
func insertWorkoutsForVO2max(t *testing.T, db *sql.DB, n int) {
	t.Helper()
	for i := 1; i <= n; i++ {
		_, err := db.Exec(`
			INSERT OR IGNORE INTO workouts (id, user_id, sport, started_at, duration_seconds, created_at, fit_file_hash)
			VALUES (?, 1, 'running', ?, 3600, ?, ?)`,
			i,
			"2026-01-"+formatDay(i)+"T10:00:00Z",
			"2026-01-"+formatDay(i)+"T10:00:00Z",
			fmt.Sprintf("hash-%d", i),
		)
		if err != nil {
			t.Fatalf("insert workout %d: %v", i, err)
		}
		est := &VO2maxEstimate{
			UserID:      1,
			WorkoutID:   int64(i),
			VO2max:      45 + float64(i),
			Method:      "daniels",
			EstimatedAt: "2026-01-" + formatDay(i) + "T11:00:00Z",
		}
		if err := SaveVO2maxEstimate(db, est); err != nil {
			t.Fatalf("SaveVO2maxEstimate %d: %v", i, err)
		}
	}
}

// formatDay zero-pads a day number to two digits.
func formatDay(d int) string {
	return fmt.Sprintf("%02d", d)
}
