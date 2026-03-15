package lactate

import "math"

// ZoneSystem identifies a training zone system.
type ZoneSystem string

const (
	ZoneSystemOlympiatoppen ZoneSystem = "olympiatoppen"
	ZoneSystemNorwegian     ZoneSystem = "norwegian"
)

// TrainingZone represents a single training zone with boundaries.
type TrainingZone struct {
	Zone        int     `json:"zone"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	MinSpeedKmh float64 `json:"min_speed_kmh"`
	MaxSpeedKmh float64 `json:"max_speed_kmh"`
	MinHR       int     `json:"min_hr"`
	MaxHR       int     `json:"max_hr"`
	LactateFrom float64 `json:"lactate_from"`
	LactateTo   float64 `json:"lactate_to"`
}

// ZonesResult holds the complete zone calculation output.
type ZonesResult struct {
	System         ZoneSystem     `json:"system"`
	ThresholdSpeed float64        `json:"threshold_speed_kmh"`
	ThresholdHR    int            `json:"threshold_hr"`
	Zones          []TrainingZone `json:"zones"`
}

// TrafficLight represents the classification of a lactate reading.
type TrafficLight string

const (
	TrafficGreen  TrafficLight = "green"
	TrafficYellow TrafficLight = "yellow"
	TrafficRed    TrafficLight = "red"
)

// StageTrafficLight classifies a single stage's lactate value.
type StageTrafficLight struct {
	StageNumber int          `json:"stage_number"`
	SpeedKmh    float64      `json:"speed_kmh"`
	LactateMmol float64      `json:"lactate_mmol"`
	Light       TrafficLight `json:"light"`
	Label       string       `json:"label"`
}

// ClassifyLactate returns a traffic light classification for a lactate value
// relative to the given threshold.
func ClassifyLactate(lactateMmol, thresholdLactate float64) (TrafficLight, string) {
	// Green: below 50% of threshold — solidly aerobic
	// Yellow: 50-100% of threshold — approaching threshold
	// Red: above threshold — anaerobic/high lactate
	if thresholdLactate <= 0 {
		thresholdLactate = DefaultOBLAThreshold
	}

	ratio := lactateMmol / thresholdLactate
	switch {
	case ratio < 0.5:
		return TrafficGreen, "Easy aerobic"
	case ratio < 1.0:
		return TrafficYellow, "Approaching threshold"
	default:
		return TrafficRed, "Above threshold"
	}
}

// ClassifyStages applies traffic light classification to all stages.
func ClassifyStages(stages []Stage, thresholdLactate float64) []StageTrafficLight {
	results := make([]StageTrafficLight, len(stages))
	for i, s := range stages {
		light, label := ClassifyLactate(s.LactateMmol, thresholdLactate)
		results[i] = StageTrafficLight{
			StageNumber: s.StageNumber,
			SpeedKmh:    s.SpeedKmh,
			LactateMmol: s.LactateMmol,
			Light:       light,
			Label:       label,
		}
	}
	return results
}

// olympiatoppenZoneDefs defines the Olympiatoppen 5-zone model.
// Percentages are relative to threshold speed and HR at OBLA (4 mmol/L).
// Lactate ranges are fixed reference values from the model.
var olympiatoppenZoneDefs = []struct {
	zone        int
	name        string
	description string
	speedPctMin float64
	speedPctMax float64
	hrPctMin    float64
	hrPctMax    float64
	lactateMin  float64
	lactateMax  float64
}{
	{1, "I1 - Recovery", "Low-intensity recovery and warm-up", 0.0, 0.72, 0.0, 0.72, 0.0, 1.5},
	{2, "I2 - Endurance", "Aerobic base building", 0.72, 0.82, 0.72, 0.82, 1.5, 2.5},
	{3, "I3 - Tempo", "Moderate aerobic intensity", 0.82, 0.92, 0.82, 0.87, 2.5, 4.0},
	{4, "I4 - Threshold", "Lactate threshold training", 0.92, 1.02, 0.87, 0.92, 4.0, 6.0},
	{5, "I5 - VO2max", "High-intensity intervals", 1.02, 1.20, 0.92, 1.00, 6.0, 20.0},
}

// norwegianZoneDefs defines the Norwegian 5-zone model used by the
// Norwegian Olympic Federation. Very similar to Olympiatoppen but with
// slightly different HR boundaries and a split zone 5.
var norwegianZoneDefs = []struct {
	zone        int
	name        string
	description string
	speedPctMin float64
	speedPctMax float64
	hrPctMin    float64
	hrPctMax    float64
	lactateMin  float64
	lactateMax  float64
}{
	{1, "Sone 1 - Rolig", "Easy/recovery pace", 0.0, 0.72, 0.0, 0.72, 0.0, 1.5},
	{2, "Sone 2 - Moderat", "Moderate endurance", 0.72, 0.82, 0.72, 0.82, 1.5, 2.5},
	{3, "Sone 3 - Hardt", "Hard continuous effort", 0.82, 0.92, 0.82, 0.87, 2.5, 4.0},
	{4, "Sone 4 - Terskel", "Threshold intervals", 0.92, 1.00, 0.87, 0.92, 4.0, 6.0},
	{5, "Sone 5 - Maks", "Maximal/VO2max intervals", 1.00, 1.20, 0.92, 1.00, 6.0, 20.0},
}

// CalculateZones computes training zones for a given system based on
// threshold speed and heart rate.
func CalculateZones(system ZoneSystem, thresholdSpeed float64, thresholdHR int) *ZonesResult {
	var defs []struct {
		zone        int
		name        string
		description string
		speedPctMin float64
		speedPctMax float64
		hrPctMin    float64
		hrPctMax    float64
		lactateMin  float64
		lactateMax  float64
	}

	switch system {
	case ZoneSystemNorwegian:
		defs = norwegianZoneDefs
	default:
		defs = olympiatoppenZoneDefs
	}

	zones := make([]TrainingZone, len(defs))
	hrFloat := float64(thresholdHR)

	for i, d := range defs {
		zones[i] = TrainingZone{
			Zone:        d.zone,
			Name:        d.name,
			Description: d.description,
			MinSpeedKmh: round2(thresholdSpeed * d.speedPctMin),
			MaxSpeedKmh: round2(thresholdSpeed * d.speedPctMax),
			MinHR:       int(math.Round(hrFloat * d.hrPctMin)),
			MaxHR:       int(math.Round(hrFloat * d.hrPctMax)),
			LactateFrom: d.lactateMin,
			LactateTo:   d.lactateMax,
		}
	}

	return &ZonesResult{
		System:         system,
		ThresholdSpeed: thresholdSpeed,
		ThresholdHR:    thresholdHR,
		Zones:          zones,
	}
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}
