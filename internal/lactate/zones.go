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
	MaxHR          int            `json:"max_hr,omitempty"`
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
// Norwegian naming and slightly different speed/HR boundaries for zone 4-5.
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
// threshold speed and heart rate. If maxHR is provided (> 0), it is used
// as the ceiling for zone 5 and HR ranges are scaled between threshold HR
// and max HR for zones above threshold, producing physiologically correct
// zones. Without max HR, threshold HR is used as ceiling (legacy behavior).
func CalculateZones(system ZoneSystem, thresholdSpeed float64, thresholdHR int, maxHR int) *ZonesResult {
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

	// When max HR is available, scale HR zones so that:
	// - Zone 4/5 boundary sits near threshold HR
	// - Zone 5 tops out at max HR
	// - Zones below threshold scale proportionally from 0 to threshold HR
	//
	// The zone defs use percentages of threshold (0.0-1.0 maps to 0-thresholdHR).
	// For zones above threshold (pct > ~0.92), we interpolate between threshold HR and max HR.
	useMaxHR := maxHR > 0 && maxHR > thresholdHR

	for i, d := range defs {
		minHR := scaleHR(d.hrPctMin, thresholdHR, maxHR, useMaxHR)
		maxHRVal := scaleHR(d.hrPctMax, thresholdHR, maxHR, useMaxHR)

		zones[i] = TrainingZone{
			Zone:        d.zone,
			Name:        d.name,
			Description: d.description,
			MinSpeedKmh: round2(thresholdSpeed * d.speedPctMin),
			MaxSpeedKmh: round2(thresholdSpeed * d.speedPctMax),
			MinHR:       minHR,
			MaxHR:       maxHRVal,
			LactateFrom: d.lactateMin,
			LactateTo:   d.lactateMax,
		}
	}

	result := &ZonesResult{
		System:         system,
		ThresholdSpeed: thresholdSpeed,
		ThresholdHR:    thresholdHR,
		Zones:          zones,
	}
	if useMaxHR {
		result.MaxHR = maxHR
	}
	return result
}

// scaleHR converts a zone percentage to an HR value. The zone defs define
// percentages where 0.92 ≈ threshold HR and 1.0 = ceiling. When max HR is
// available, percentages at or below the threshold boundary (~0.92) map
// linearly from 0 to threshold HR, and percentages above 0.92 interpolate
// between threshold HR and max HR. This places the zone 4/5 boundary near
// threshold HR and zone 5 ceiling at max HR.
func scaleHR(pct float64, thresholdHR, maxHR int, useMaxHR bool) int {
	if !useMaxHR {
		return int(math.Round(float64(thresholdHR) * pct))
	}

	// The threshold boundary in the zone definitions is ~0.92 (zone 4 max HR pct).
	// Below this, scale linearly from 0 to threshold HR.
	// Above this, interpolate from threshold HR to max HR.
	const thresholdPct = 0.92

	if pct <= thresholdPct {
		// Scale so that thresholdPct maps to thresholdHR
		return int(math.Round(float64(thresholdHR) * pct / thresholdPct))
	}

	// Interpolate from threshold HR to max HR
	// pct=0.92 → thresholdHR, pct=1.0 → maxHR
	fraction := (pct - thresholdPct) / (1.0 - thresholdPct)
	hr := float64(thresholdHR) + fraction*float64(maxHR-thresholdHR)
	return int(math.Round(hr))
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}
