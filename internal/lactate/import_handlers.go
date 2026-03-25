package lactate

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/Robin831/Hytte/internal/auth"
)

// importRequest is the request body for workout-based lactate import endpoints.
type importRequest struct {
	WorkoutID         int64  `json:"workout_id"`
	LactateData       string `json:"lactate_data"`
	WarmupDurationMin int    `json:"warmup_duration_min"`
	StageDurationMin  int    `json:"stage_duration_min"`
}

// samplePoint mirrors the JSON serialization format of training.Sample to
// avoid a circular import (training already imports lactate).
type samplePoint struct {
	OffsetMs  int64 `json:"t"`
	HeartRate int   `json:"hr"`
}

// resolveImportRequest decodes the request body, parses lactate data, and
// extracts proposed stages for the linked workout.  On any error it writes the
// appropriate HTTP response and returns false; on success it returns the decoded
// request and the extraction result.
func resolveImportRequest(w http.ResponseWriter, r *http.Request, db *sql.DB, userID int64, logPrefix string) (*importRequest, *ImportResult, bool) {
	req, err := decodeImportRequest(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return nil, nil, false
	}

	pairs, parseErr := ParseLactateInput(req.LactateData)
	if parseErr != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": fmt.Sprintf("invalid lactate data: %v", parseErr),
			"hint":  `expected one pair per line, e.g. "10.5 2.3" (speed km/h, lactate mmol/L)`,
		})
		return nil, nil, false
	}

	result, err := extractStagesForWorkout(db, userID, req, pairs)
	if err == sql.ErrNoRows {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "workout not found"})
		return nil, nil, false
	}
	if err != nil {
		log.Printf("%s: %v", logPrefix, err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load workout data"})
		return nil, nil, false
	}

	return req, result, true
}

// PreviewFromWorkoutHandler returns a proposed lactate test derived from a
// workout's laps and heart rate samples without persisting anything.
//
// POST /api/lactate/tests/preview-from-workout
// Request: {workout_id, lactate_data, warmup_duration_min, stage_duration_min}
func PreviewFromWorkoutHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)

		_, result, ok := resolveImportRequest(w, r, db, user.ID, "preview-from-workout")
		if !ok {
			return
		}

		warnings := result.Warnings
		if warnings == nil {
			warnings = []string{}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"stages":   result.Stages,
			"warnings": warnings,
			"method":   result.Method,
		})
	}
}

// ImportFromWorkoutHandler creates and persists a lactate test from a workout.
//
// POST /api/lactate/tests/from-workout
// Request: {workout_id, lactate_data, warmup_duration_min, stage_duration_min}
func ImportFromWorkoutHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)

		req, result, ok := resolveImportRequest(w, r, db, user.ID, "import-from-workout")
		if !ok {
			return
		}

		stages := make([]Stage, len(result.Stages))
		for i, s := range result.Stages {
			stages[i] = Stage{
				StageNumber:  s.StageNumber,
				SpeedKmh:     s.SpeedKmh,
				LactateMmol:  s.LactateMmol,
				HeartRateBpm: s.HeartRateBpm,
			}
		}

		startSpeed := 0.0
		if len(stages) > 0 {
			startSpeed = stages[0].SpeedKmh
		}
		speedIncrement := 0.5
		if len(stages) >= 2 {
			speedIncrement = stages[1].SpeedKmh - stages[0].SpeedKmh
		}

		// Use the workout's start date if available, otherwise today.
		date := time.Now().UTC().Format("2006-01-02")
		var workoutStartedAt string
		if scanErr := db.QueryRow(
			`SELECT started_at FROM workouts WHERE id = ? AND user_id = ?`,
			req.WorkoutID, user.ID,
		).Scan(&workoutStartedAt); scanErr == nil && len(workoutStartedAt) >= 10 {
			date = workoutStartedAt[:10]
		}

		workoutID := req.WorkoutID
		t := &Test{
			Date:              date,
			ProtocolType:      "standard",
			WarmupDurationMin: req.WarmupDurationMin,
			StageDurationMin:  req.StageDurationMin,
			StartSpeedKmh:     startSpeed,
			SpeedIncrementKmh: speedIncrement,
			WorkoutID:         &workoutID,
			Stages:            stages,
		}

		created, err := Create(db, user.ID, t)
		if err != nil {
			log.Printf("import-from-workout create: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create test"})
			return
		}

		writeJSON(w, http.StatusCreated, map[string]any{"test": created})
	}
}

// decodeImportRequest decodes and validates the import request body.
// Zero values for WarmupDurationMin and StageDurationMin are replaced with
// sensible defaults (10 min warmup, 5 min stage) so the normalized values
// are used consistently for both HR extraction and test persistence.
func decodeImportRequest(r *http.Request) (*importRequest, error) {
	var req importRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, fmt.Errorf("invalid JSON")
	}
	if req.WorkoutID <= 0 {
		return nil, fmt.Errorf("workout_id is required")
	}
	if strings.TrimSpace(req.LactateData) == "" {
		return nil, fmt.Errorf("lactate_data is required")
	}

	// Apply defaults for omitted duration fields before range validation.
	if req.WarmupDurationMin == 0 {
		req.WarmupDurationMin = 10
	}
	if req.StageDurationMin == 0 {
		req.StageDurationMin = 5
	}

	if req.WarmupDurationMin < 0 {
		return nil, fmt.Errorf("warmup_duration_min must be >= 0")
	}
	if req.StageDurationMin < 1 || req.StageDurationMin > 60 {
		return nil, fmt.Errorf("stage_duration_min must be between 1 and 60 minutes")
	}

	return &req, nil
}

// extractStagesForWorkout loads workout laps and samples and matches them to
// the provided lactate pairs.  If the workout has no laps or samples, stages
// are returned without HR data and with a warning instead of an error.
func extractStagesForWorkout(db *sql.DB, userID int64, req *importRequest, pairs []SpeedLactatePair) (*ImportResult, error) {
	laps, err := loadImportLaps(db, req.WorkoutID, userID)
	if err != nil {
		return nil, err
	}

	if len(laps) == 0 {
		return buildStagesWithoutHR(pairs, "workout has no laps; heart rate data not available"), nil
	}

	samples, err := loadImportSamples(db, req.WorkoutID)
	if err != nil {
		return nil, err
	}

	if len(samples) == 0 {
		return buildStagesWithoutHR(pairs, "workout has no time-series data; heart rate data not available"), nil
	}

	opts := ImportOptions{
		WarmupDurationMin: req.WarmupDurationMin,
		StageDurationMin:  req.StageDurationMin,
	}
	res, err := ExtractStageHR(laps, samples, pairs, opts)
	if err != nil {
		return nil, err
	}

	// Ensure we always return one stage per input lactate pair. If some stages
	// could not be matched to workout data, fill in missing HR/lap info with 0
	// and add a warning instead of dropping those pairs.
	if len(res.Stages) < len(pairs) {
		for i := len(res.Stages); i < len(pairs); i++ {
			p := pairs[i]
			res.Stages = append(res.Stages, ProposedStage{
				StageNumber:  i + 1,
				SpeedKmh:     p.SpeedKmh,
				LactateMmol:  p.LactateMmol,
				HeartRateBpm: 0,
				LapNumber:    0,
			})
		}
		res.Warnings = append(res.Warnings, "some stages could not be matched to workout data; missing heart rate and lap information has been set to 0")
	}

	return res, nil
}

// buildStagesWithoutHR creates proposed stages from parsed pairs without HR data.
func buildStagesWithoutHR(pairs []SpeedLactatePair, warning string) *ImportResult {
	stages := make([]ProposedStage, len(pairs))
	for i, p := range pairs {
		stages[i] = ProposedStage{
			StageNumber:  i + 1,
			SpeedKmh:     p.SpeedKmh,
			LactateMmol:  p.LactateMmol,
			HeartRateBpm: 0,
			LapNumber:    0,
		}
	}
	return &ImportResult{
		Stages:   stages,
		Warnings: []string{warning},
		Method:   "none",
	}
}

// loadImportLaps queries workout laps for the given workout, verifying that the
// workout belongs to the specified user.  Returns sql.ErrNoRows if the workout
// does not exist or is not owned by the user.
func loadImportLaps(db *sql.DB, workoutID, userID int64) ([]ImportLap, error) {
	var exists int
	err := db.QueryRow(
		`SELECT 1 FROM workouts WHERE id = ? AND user_id = ?`, workoutID, userID,
	).Scan(&exists)
	if err == sql.ErrNoRows {
		return nil, sql.ErrNoRows
	}
	if err != nil {
		return nil, fmt.Errorf("check workout ownership: %w", err)
	}

	rows, err := db.Query(`
		SELECT lap_number, start_offset_ms, duration_seconds, avg_pace_sec_per_km
		FROM workout_laps
		WHERE workout_id = ?
		ORDER BY lap_number`, workoutID)
	if err != nil {
		return nil, fmt.Errorf("query laps: %w", err)
	}
	defer rows.Close()

	var laps []ImportLap
	for rows.Next() {
		var l ImportLap
		if err := rows.Scan(&l.LapNumber, &l.StartOffsetMs, &l.DurationSeconds, &l.AvgPaceSecPerKm); err != nil {
			return nil, fmt.Errorf("scan lap: %w", err)
		}
		laps = append(laps, l)
	}
	return laps, rows.Err()
}

// loadImportSamples queries the time-series HR samples for a workout.
// Returns nil (no error) when the workout has no samples.
func loadImportSamples(db *sql.DB, workoutID int64) ([]ImportSample, error) {
	var data string
	err := db.QueryRow(
		`SELECT data FROM workout_samples WHERE workout_id = ?`, workoutID,
	).Scan(&data)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query samples: %w", err)
	}

	var points []samplePoint
	if err := json.Unmarshal([]byte(data), &points); err != nil {
		return nil, fmt.Errorf("unmarshal samples: %w", err)
	}

	samples := make([]ImportSample, len(points))
	for i, p := range points {
		samples[i] = ImportSample{OffsetMs: p.OffsetMs, HeartRate: p.HeartRate}
	}
	return samples, nil
}
