package training

import (
	"database/sql"
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
	// 45 min easy run at 10 km/h (600 m/min), avg HR 140, max HR 185, resting HR 55.
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
}

// TestGetVO2maxHandler_WithHistory tests the handler returning history and trend.
func TestGetVO2maxHandler_WithHistory(t *testing.T) {
	db := setupTestDB(t)
	insertWorkoutsForVO2max(t, db, 5)

	req := httptest.NewRequest(http.MethodGet, "/api/training/vo2max", nil)
	req = withUser(req, 1)
	rr := httptest.NewRecorder()

	GetVO2maxHandler(db).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
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
		{"improving", []float64{45, 47, 49, 51, 53}, "improving"},
		{"declining", []float64{55, 53, 51, 49, 47}, "declining"},
		{"stable", []float64{50, 50.1, 49.9, 50.2, 50}, "stable"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var history []VO2maxEstimate
			for _, v := range tc.values {
				history = append(history, VO2maxEstimate{VO2max: v})
			}
			got := computeVO2maxTrend(history)
			if got != tc.expected {
				t.Errorf("expected %q, got %q", tc.expected, got)
			}
		})
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
			INSERT OR IGNORE INTO workouts (id, user_id, sport, started_at, duration_seconds, created_at)
			VALUES (?, 1, 'running', ?, 3600, ?)`,
			i,
			"2026-01-"+formatDay(i)+"T10:00:00Z",
			"2026-01-"+formatDay(i)+"T10:00:00Z",
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
