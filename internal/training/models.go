package training

import "time"

// Workout is the summary record for a single imported workout.
type Workout struct {
	ID              int64   `json:"id"`
	UserID          int64   `json:"user_id"`
	Sport           string  `json:"sport"`
	Title           string  `json:"title"`
	StartedAt       string  `json:"started_at"`
	DurationSeconds int     `json:"duration_seconds"`
	DistanceMeters  float64 `json:"distance_meters"`
	AvgHeartRate    int     `json:"avg_heart_rate"`
	MaxHeartRate    int     `json:"max_heart_rate"`
	AvgPaceSecPerKm float64 `json:"avg_pace_sec_per_km"`
	AvgCadence      int     `json:"avg_cadence"`
	Calories        int     `json:"calories"`
	AscentMeters    float64 `json:"ascent_meters"`
	DescentMeters   float64 `json:"descent_meters"`
	SubSport        string  `json:"sub_sport"`
	IsIndoor        bool    `json:"is_indoor"`
	FitFileHash     string  `json:"fit_file_hash"`
	AnalysisStatus  string   `json:"analysis_status"`
	TitleSource     string   `json:"title_source"`
	CreatedAt       string   `json:"created_at"`
	TrainingLoad    *float64 `json:"training_load,omitempty"`
	HRDriftPct      *float64 `json:"hr_drift_pct,omitempty"`
	PaceCVPct       *float64 `json:"pace_cv_pct,omitempty"`

	// Populated on detail requests.
	Laps    []Lap    `json:"laps,omitempty"`
	Tags    []string `json:"tags,omitempty"`
	Samples *Samples `json:"samples,omitempty"`
}

// Lap represents a single lap/interval within a workout.
type Lap struct {
	ID              int64   `json:"id"`
	WorkoutID       int64   `json:"workout_id"`
	LapNumber       int     `json:"lap_number"`
	StartOffsetMs   int64   `json:"start_offset_ms"`
	DurationSeconds float64 `json:"duration_seconds"`
	DistanceMeters  float64 `json:"distance_meters"`
	AvgHeartRate    int     `json:"avg_heart_rate"`
	MaxHeartRate    int     `json:"max_heart_rate"`
	AvgPaceSecPerKm float64 `json:"avg_pace_sec_per_km"`
	AvgCadence      int     `json:"avg_cadence"`
}

// Sample is a single time-series data point within a workout.
type Sample struct {
	OffsetMs      int64   `json:"t"`
	HeartRate     int     `json:"hr,omitempty"`
	SpeedMPerS    float64 `json:"spd,omitempty"`
	Cadence       int     `json:"cad,omitempty"`
	AltitudeM     float64 `json:"alt,omitempty"`
	DistanceM     float64 `json:"dist,omitempty"`
}

// Samples wraps the time-series data for a workout.
type Samples struct {
	Points []Sample `json:"points"`
}

// ParsedWorkout holds the result of parsing a single .fit file.
type ParsedWorkout struct {
	Title           string // Workout name from FIT metadata, empty if none found.
	Sport           string
	SubSport        string // e.g. "treadmill", "indoor_running", "trail", or "" for generic
	HasGPS          bool   // true if any record has valid lat/lng
	StartedAt       time.Time
	DurationSeconds int
	DistanceMeters  float64
	AvgHeartRate    int
	MaxHeartRate    int
	AvgCadence      int
	Calories        int
	AscentMeters    float64
	DescentMeters   float64
	Laps            []ParsedLap
	Samples         []Sample
}

// ParsedLap holds lap data extracted from a .fit file.
type ParsedLap struct {
	StartOffsetMs   int64
	DurationSeconds float64
	DistanceMeters  float64
	AvgHeartRate    int
	MaxHeartRate    int
	AvgSpeedMPerS   float64
	AvgCadence      int
}

// ComparisonResult holds the result of comparing two workouts.
type ComparisonResult struct {
	WorkoutA   WorkoutSummary       `json:"workout_a"`
	WorkoutB   WorkoutSummary       `json:"workout_b"`
	Compatible bool                 `json:"compatible"`
	Reason     string               `json:"reason,omitempty"`
	LapDeltas  []LapDelta           `json:"lap_deltas,omitempty"`
	Summary    *ComparisonSummary   `json:"summary,omitempty"`
}

// WorkoutSummary is a minimal workout reference for comparison results.
type WorkoutSummary struct {
	ID        int64  `json:"id"`
	Title     string `json:"title"`
	StartedAt string `json:"started_at"`
	Sport     string `json:"sport"`
}

// LapDelta shows the difference between matched laps.
type LapDelta struct {
	LapNumber    int     `json:"lap_number"`
	LapNumberA   int     `json:"lap_number_a"`
	LapNumberB   int     `json:"lap_number_b"`
	DurationDiff float64 `json:"duration_diff_seconds"`
	AvgHRA       int     `json:"avg_hr_a"`
	AvgHRB       int     `json:"avg_hr_b"`
	HRDelta      int     `json:"hr_delta"`
	PaceA        float64 `json:"pace_a_sec_per_km"`
	PaceB        float64 `json:"pace_b_sec_per_km"`
	PaceDelta    float64 `json:"pace_delta_sec_per_km"`
}

// ComparisonSummary gives an overall comparison summary.
type ComparisonSummary struct {
	AvgHRDelta  float64 `json:"avg_hr_delta"`
	AvgPaceDelta float64 `json:"avg_pace_delta"`
	Verdict     string  `json:"verdict"`
}

// ProgressionPoint is a single data point in a progression trend.
type ProgressionPoint struct {
	WorkoutID  int64   `json:"workout_id"`
	Date       string  `json:"date"`
	AvgHR      float64 `json:"avg_hr"`
	AvgPace    float64 `json:"avg_pace_sec_per_km"`
	RecoveryHR float64 `json:"recovery_hr,omitempty"`
}

// ProgressionGroup groups workouts with similar structure.
type ProgressionGroup struct {
	Tag        string             `json:"tag"`
	Sport      string             `json:"sport"`
	LapCount   int                `json:"lap_count"`
	Workouts   []ProgressionPoint `json:"workouts"`
}

// WeeklySummary aggregates training volume per week.
type WeeklySummary struct {
	WeekStart      string  `json:"week_start"`
	TotalDuration  int     `json:"total_duration_seconds"`
	TotalDistance   float64 `json:"total_distance_meters"`
	WorkoutCount   int     `json:"workout_count"`
	AvgHeartRate   float64 `json:"avg_heart_rate"`
}

// TrendAnalysis holds AI-generated trend context relative to recent training history.
type TrendAnalysis struct {
	FitnessDirection   string   `json:"fitness_direction"`
	ComparisonToRecent string   `json:"comparison_to_recent"`
	NotableChanges     []string `json:"notable_changes"`
}

// TrainingInsights holds AI-generated coaching feedback for a workout.
type TrainingInsights struct {
	EffortSummary    string         `json:"effort_summary"`
	PacingAnalysis   string         `json:"pacing_analysis"`
	HRZones          string         `json:"hr_zones"`
	ThresholdContext string         `json:"threshold_context,omitempty"`
	Observations     []string       `json:"observations"`
	Suggestions      []string       `json:"suggestions"`
	TrendAnalysis    *TrendAnalysis `json:"trend_analysis,omitempty"`
}

// normalize ensures slice fields are non-nil so they serialize as [] instead of null.
func (t *TrainingInsights) normalize() {
	if t.Observations == nil {
		t.Observations = []string{}
	}
	if t.Suggestions == nil {
		t.Suggestions = []string{}
	}
	if t.TrendAnalysis != nil && t.TrendAnalysis.NotableChanges == nil {
		t.TrendAnalysis.NotableChanges = []string{}
	}
}

// CachedInsights is a TrainingInsights with metadata about when/how it was generated.
type CachedInsights struct {
	TrainingInsights
	Model     string `json:"model"`
	CreatedAt string `json:"created_at"`
	Cached    bool   `json:"cached"`
}

// ComparisonAnalysis holds AI-generated natural language comparison analysis.
type ComparisonAnalysis struct {
	Summary      string   `json:"summary"`
	Strengths    []string `json:"strengths"`
	Weaknesses   []string `json:"weaknesses"`
	Observations []string `json:"observations"`
}

// normalize ensures slice fields are non-nil so they serialize as [] instead of null.
func (c *ComparisonAnalysis) normalize() {
	if c.Strengths == nil {
		c.Strengths = []string{}
	}
	if c.Weaknesses == nil {
		c.Weaknesses = []string{}
	}
	if c.Observations == nil {
		c.Observations = []string{}
	}
}

// CachedComparisonAnalysis is a ComparisonAnalysis with metadata about when/how it was generated.
type CachedComparisonAnalysis struct {
	ComparisonAnalysis
	WorkoutIDA int64  `json:"workout_id_a"`
	WorkoutIDB int64  `json:"workout_id_b"`
	Model      string `json:"model"`
	CreatedAt  string `json:"created_at"`
	Cached     bool   `json:"cached"`
}

// ZoneDistribution shows time spent in each HR zone for a workout.
type ZoneDistribution struct {
	Zone       int     `json:"zone"`
	Name       string  `json:"name"`
	MinHR      int     `json:"min_hr"`
	MaxHR      int     `json:"max_hr"`
	DurationS  float64 `json:"duration_seconds"`
	Percentage float64 `json:"percentage"`
}

// ACRTrendPoint represents the Acute:Chronic Workload Ratio computed as of a specific date.
type ACRTrendPoint struct {
	Date    string   `json:"date"`
	ACR     *float64 `json:"acr"`
	Acute   float64  `json:"acute"`
	Chronic float64  `json:"chronic"`
}

// WeeklyLoad aggregates training load for a single calendar week per user,
// split into easy (< 80% max HR) and hard (>= 80% max HR) effort categories.
type WeeklyLoad struct {
	UserID       int64   `json:"user_id"`
	WeekStart    string  `json:"week_start"`
	EasyLoad     float64 `json:"easy_load"`
	HardLoad     float64 `json:"hard_load"`
	TotalLoad    float64 `json:"total_load"`
	WorkoutCount int     `json:"workout_count"`
	UpdatedAt    string  `json:"updated_at"`
}

// TrainingSummary caches the computed training status for a given week.
type TrainingSummary struct {
	UserID      int64          `json:"user_id"`
	WeekStart   string         `json:"week_start"`
	Status      TrainingStatus `json:"status"`
	ACR         *float64       `json:"acr,omitempty"`
	AcuteLoad   float64        `json:"acute_load"`
	ChronicLoad float64        `json:"chronic_load"`
	UpdatedAt   string         `json:"updated_at"`
}
