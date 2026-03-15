package lactate

import (
	"database/sql"
	"encoding/json"
	"log"
	"math"
	"net/http"
	"strconv"

	"github.com/Robin831/Hytte/internal/auth"
	"github.com/go-chi/chi/v5"
)

// maxBodySize is the maximum allowed request body size (1 MB).
const maxBodySize = 1 << 20

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("writeJSON encode error: %v", err)
	}
}

// ListHandler returns all lactate tests for the authenticated user.
func ListHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		tests, err := List(db, user.ID)
		if err != nil {
			log.Printf("Failed to list lactate tests: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list tests"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"tests": tests})
	}
}

// CreateHandler creates a new lactate test with stages.
func CreateHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)

		var body testInput
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}

		if msg := validateTestInput(&body); msg != "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": msg})
			return
		}

		t := inputToTest(&body)
		created, err := Create(db, user.ID, t)
		if err != nil {
			log.Printf("Failed to create lactate test: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create test"})
			return
		}

		writeJSON(w, http.StatusCreated, map[string]any{"test": created})
	}
}

// GetHandler returns a single lactate test by ID with all stages.
func GetHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid test ID"})
			return
		}

		test, err := GetByID(db, id, user.ID)
		if err != nil {
			if err == sql.ErrNoRows {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "test not found"})
				return
			}
			log.Printf("Failed to get lactate test %d: %v", id, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to get test"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{"test": test})
	}
}

// UpdateHandler updates an existing lactate test and its stages.
func UpdateHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid test ID"})
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)

		var body testInput
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}

		if msg := validateTestInput(&body); msg != "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": msg})
			return
		}

		t := inputToTest(&body)
		updated, err := Update(db, id, user.ID, t)
		if err != nil {
			if err == sql.ErrNoRows {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "test not found"})
				return
			}
			log.Printf("Failed to update lactate test %d: %v", id, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update test"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{"test": updated})
	}
}

// DeleteHandler deletes a lactate test owned by the authenticated user.
func DeleteHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid test ID"})
			return
		}

		if err := Delete(db, id, user.ID); err != nil {
			if err == sql.ErrNoRows {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "test not found"})
				return
			}
			log.Printf("Failed to delete lactate test %d: %v", id, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete test"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// ThresholdsHandler calculates lactate thresholds for a saved test.
func ThresholdsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid test ID"})
			return
		}

		test, err := GetByID(db, id, user.ID)
		if err != nil {
			if err == sql.ErrNoRows {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "test not found"})
				return
			}
			log.Printf("Failed to get lactate test %d for thresholds: %v", id, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to get test"})
			return
		}

		if len(test.Stages) < 2 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "test must have at least 2 stages"})
			return
		}

		results := CalculateThresholds(test.Stages)
		writeJSON(w, http.StatusOK, map[string]any{"thresholds": results})
	}
}

// AnalysisHandler returns a full analysis of a saved test: thresholds, zones,
// predictions, and traffic light classification.
func AnalysisHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())

		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid test ID"})
			return
		}

		test, err := GetByID(db, id, user.ID)
		if err != nil {
			if err == sql.ErrNoRows {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "test not found"})
				return
			}
			log.Printf("Failed to get lactate test %d for analysis: %v", id, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to get test"})
			return
		}

		if len(test.Stages) < 2 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "test must have at least 2 stages"})
			return
		}

		thresholds := CalculateThresholds(test.Stages)

		// Use the first valid threshold for zones and predictions (prefer OBLA).
		var bestThreshold *ThresholdResult
		for i := range thresholds {
			if thresholds[i].Valid {
				bestThreshold = &thresholds[i]
				break
			}
		}

		// Allow ?method= query param to select a specific threshold method.
		if methodParam := r.URL.Query().Get("method"); methodParam != "" {
			for i := range thresholds {
				if string(thresholds[i].Method) == methodParam && thresholds[i].Valid {
					bestThreshold = &thresholds[i]
					break
				}
			}
		}

		// Read max HR from user preferences for zone calculations.
		var maxHR int
		prefs, prefsErr := auth.GetPreferences(db, user.ID)
		if prefsErr == nil {
			if v, ok := prefs["max_hr"]; ok {
				if parsed, parseErr := strconv.Atoi(v); parseErr == nil && parsed > 0 {
					maxHR = parsed
				}
			}
		}

		zones := []ZonesResult{}
		predictions := []RacePrediction{}
		var trafficLights []StageTrafficLight
		thresholdLactate := DefaultOBLAThreshold

		if bestThreshold != nil {
			olympiatoppen := CalculateZones(ZoneSystemOlympiatoppen, bestThreshold.SpeedKmh, bestThreshold.HeartRateBpm, maxHR)
			norwegian := CalculateZones(ZoneSystemNorwegian, bestThreshold.SpeedKmh, bestThreshold.HeartRateBpm, maxHR)
			zones = []ZonesResult{*olympiatoppen, *norwegian}
			predictions = PredictRaceTimes(bestThreshold.SpeedKmh)
			thresholdLactate = bestThreshold.LactateMmol
		}

		trafficLights = ClassifyStages(test.Stages, thresholdLactate)

		writeJSON(w, http.StatusOK, map[string]any{
			"thresholds":     thresholds,
			"zones":          zones,
			"predictions":    predictions,
			"traffic_lights": trafficLights,
			"method_used":    methodUsedName(bestThreshold),
		})
	}
}

func methodUsedName(t *ThresholdResult) string {
	if t == nil {
		return ""
	}
	return string(t.Method)
}

// CalculateHandler computes thresholds from provided stage data without requiring a saved test.
func CalculateHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)

		var body struct {
			Stages []stageInput `json:"stages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}

		if len(body.Stages) < 2 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "need at least 2 stages"})
			return
		}

		stages := make([]Stage, len(body.Stages))
		prevSpeed := -1.0
		for i, s := range body.Stages {
			if math.IsNaN(s.SpeedKmh) || math.IsInf(s.SpeedKmh, 0) {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "stage speed_kmh must be a finite number"})
				return
			}
			if math.IsNaN(s.LactateMmol) || math.IsInf(s.LactateMmol, 0) {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "stage lactate_mmol must be a finite number"})
				return
			}
			if s.SpeedKmh <= 0 {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "stage speed_kmh must be positive"})
				return
			}
			if prevSpeed >= 0 && s.SpeedKmh <= prevSpeed {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "stage speed_kmh must be strictly increasing across stages"})
				return
			}
			prevSpeed = s.SpeedKmh
			if s.LactateMmol < 0 {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "stage lactate_mmol must be non-negative"})
				return
			}
			if s.HeartRateBpm < 0 {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "stage heart_rate_bpm must be non-negative"})
				return
			}
			stages[i] = Stage{
				StageNumber:  s.StageNumber,
				SpeedKmh:     s.SpeedKmh,
				LactateMmol:  s.LactateMmol,
				HeartRateBpm: s.HeartRateBpm,
			}
		}

		results := CalculateThresholds(stages)
		writeJSON(w, http.StatusOK, map[string]any{"thresholds": results})
	}
}

// testInput is the JSON body for create/update requests.
type testInput struct {
	Date              string       `json:"date"`
	Comment           string       `json:"comment"`
	ProtocolType      string       `json:"protocol_type"`
	WarmupDurationMin *int         `json:"warmup_duration_min"`
	StageDurationMin  *int         `json:"stage_duration_min"`
	StartSpeedKmh     *float64     `json:"start_speed_kmh"`
	SpeedIncrementKmh *float64     `json:"speed_increment_kmh"`
	Stages            []stageInput `json:"stages"`
}

type stageInput struct {
	StageNumber  int     `json:"stage_number"`
	SpeedKmh     float64 `json:"speed_kmh"`
	LactateMmol  float64 `json:"lactate_mmol"`
	HeartRateBpm int     `json:"heart_rate_bpm"`
	RPE          *int    `json:"rpe"`
	Notes        string  `json:"notes"`
}

func validateTestInput(b *testInput) string {
	if b.Date == "" {
		return "date is required"
	}

	if b.ProtocolType != "" && b.ProtocolType != "standard" && b.ProtocolType != "custom" {
		return "protocol_type must be 'standard' or 'custom'"
	}

	if b.WarmupDurationMin != nil && *b.WarmupDurationMin < 0 {
		return "warmup_duration_min must be non-negative"
	}

	if b.StageDurationMin != nil && (*b.StageDurationMin < 1 || *b.StageDurationMin > 30) {
		return "stage_duration_min must be between 1 and 30"
	}

	if b.StartSpeedKmh != nil && (math.IsNaN(*b.StartSpeedKmh) || math.IsInf(*b.StartSpeedKmh, 0)) {
		return "start_speed_kmh must be a finite number"
	}

	if b.StartSpeedKmh != nil && *b.StartSpeedKmh <= 0 {
		return "start_speed_kmh must be positive"
	}

	if b.SpeedIncrementKmh != nil && (math.IsNaN(*b.SpeedIncrementKmh) || math.IsInf(*b.SpeedIncrementKmh, 0)) {
		return "speed_increment_kmh must be a finite number"
	}

	if b.SpeedIncrementKmh != nil && *b.SpeedIncrementKmh <= 0 {
		return "speed_increment_kmh must be positive"
	}

	seen := make(map[int]bool, len(b.Stages))
	prevSpeed := -1.0
	for _, s := range b.Stages {
		if s.StageNumber < 0 {
			return "stage_number must be non-negative"
		}
		if seen[s.StageNumber] {
			return "stage_number must be unique within a test"
		}
		seen[s.StageNumber] = true
		if math.IsNaN(s.SpeedKmh) || math.IsInf(s.SpeedKmh, 0) {
			return "stage speed_kmh must be a finite number"
		}
		if s.SpeedKmh <= 0 {
			return "stage speed_kmh must be positive"
		}
		if prevSpeed >= 0 && s.SpeedKmh <= prevSpeed {
			return "stage speed_kmh must be strictly increasing across stages"
		}
		prevSpeed = s.SpeedKmh
		if math.IsNaN(s.LactateMmol) || math.IsInf(s.LactateMmol, 0) {
			return "stage lactate_mmol must be a finite number"
		}
		if s.LactateMmol < 0 {
			return "stage lactate_mmol must be non-negative"
		}
		if s.HeartRateBpm < 0 {
			return "stage heart_rate_bpm must be non-negative"
		}
		if s.RPE != nil && (*s.RPE < 6 || *s.RPE > 20) {
			return "stage rpe must be between 6 and 20 (Borg scale)"
		}
	}

	return ""
}

func inputToTest(b *testInput) *Test {
	t := &Test{
		Date:              b.Date,
		Comment:           b.Comment,
		ProtocolType:      b.ProtocolType,
		WarmupDurationMin: 10,
		StageDurationMin:  5,
		StartSpeedKmh:     11.5,
		SpeedIncrementKmh: 0.5,
	}

	if t.ProtocolType == "" {
		t.ProtocolType = "standard"
	}
	if b.WarmupDurationMin != nil {
		t.WarmupDurationMin = *b.WarmupDurationMin
	}
	if b.StageDurationMin != nil {
		t.StageDurationMin = *b.StageDurationMin
	}
	if b.StartSpeedKmh != nil {
		t.StartSpeedKmh = *b.StartSpeedKmh
	}
	if b.SpeedIncrementKmh != nil {
		t.SpeedIncrementKmh = *b.SpeedIncrementKmh
	}

	for _, s := range b.Stages {
		t.Stages = append(t.Stages, Stage{
			StageNumber:  s.StageNumber,
			SpeedKmh:     s.SpeedKmh,
			LactateMmol:  s.LactateMmol,
			HeartRateBpm: s.HeartRateBpm,
			RPE:          s.RPE,
			Notes:        s.Notes,
		})
	}
	if t.Stages == nil {
		t.Stages = []Stage{}
	}

	return t
}
