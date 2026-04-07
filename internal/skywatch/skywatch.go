package skywatch

import (
	"time"

	"github.com/sixdouglas/suncalc"
)

// Default location: home in Bergen, Norway.
const (
	DefaultLat = 60.36091
	DefaultLon = 5.24056
)

// MoonPhaseName returns a human-readable name for the moon phase value (0.0–1.0).
func MoonPhaseName(phase float64) string {
	switch {
	case phase < 0.0625:
		return "New Moon"
	case phase < 0.1875:
		return "Waxing Crescent"
	case phase < 0.3125:
		return "First Quarter"
	case phase < 0.4375:
		return "Waxing Gibbous"
	case phase < 0.5625:
		return "Full Moon"
	case phase < 0.6875:
		return "Waning Gibbous"
	case phase < 0.8125:
		return "Last Quarter"
	case phase < 0.9375:
		return "Waning Crescent"
	default:
		return "New Moon"
	}
}

// MoonInfo holds computed moon data for a specific time and location.
type MoonInfo struct {
	Phase        string  `json:"phase"`
	Illumination float64 `json:"illumination"`
	PhaseValue   float64 `json:"phase_value"`
	Moonrise     *string `json:"moonrise"`
	Moonset      *string `json:"moonset"`
	AlwaysUp     bool    `json:"always_up,omitempty"`
	AlwaysDown   bool    `json:"always_down,omitempty"`
}

// SunTimes holds computed sun data for a specific date and location.
type SunTimes struct {
	Sunrise                string  `json:"sunrise"`
	Sunset                 string  `json:"sunset"`
	SolarNoon              string  `json:"solar_noon"`
	DayLength              float64 `json:"day_length_hours"`
	GoldenHourStart        string  `json:"golden_hour_start"`
	GoldenHourEnd          string  `json:"golden_hour_end"`
	CivilDawn              string  `json:"civil_dawn"`
	CivilDusk              string  `json:"civil_dusk"`
	NauticalDawn           string  `json:"nautical_dawn"`
	NauticalDusk           string  `json:"nautical_dusk"`
	AstronomicalDawn       string  `json:"astronomical_dawn"`
	AstronomicalDusk       string  `json:"astronomical_dusk"`
}

// formatTime formats a time as RFC3339, returning empty string for zero times.
func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

// formatTimePtr formats a time as RFC3339, returning nil for zero times.
func formatTimePtr(t time.Time) *string {
	if t.IsZero() {
		return nil
	}
	s := t.Format(time.RFC3339)
	return &s
}

// GetMoonPhase computes moon phase information for the given time and location.
func GetMoonPhase(t time.Time, lat, lon float64) MoonInfo {
	illum := suncalc.GetMoonIllumination(t)
	moonTimes := suncalc.GetMoonTimes(t, lat, lon, false)

	return MoonInfo{
		Phase:        MoonPhaseName(illum.Phase),
		Illumination: illum.Fraction * 100,
		PhaseValue:   illum.Phase,
		Moonrise:     formatTimePtr(moonTimes.Rise),
		Moonset:      formatTimePtr(moonTimes.Set),
		AlwaysUp:     moonTimes.AlwaysUp,
		AlwaysDown:   moonTimes.AlwaysDown,
	}
}

// GetSunTimes computes sun times for the given date and location.
func GetSunTimes(date time.Time, lat, lon float64) SunTimes {
	times := suncalc.GetTimes(date, lat, lon)

	sunrise := times[suncalc.Sunrise].Value
	sunset := times[suncalc.Sunset].Value

	var dayLength float64
	if !sunrise.IsZero() && !sunset.IsZero() {
		dayLength = sunset.Sub(sunrise).Hours()
	}

	return SunTimes{
		Sunrise:          formatTime(sunrise),
		Sunset:           formatTime(sunset),
		SolarNoon:        formatTime(times[suncalc.SolarNoon].Value),
		DayLength:        dayLength,
		GoldenHourStart:  formatTime(times[suncalc.GoldenHour].Value),
		GoldenHourEnd:    formatTime(times[suncalc.GoldenHourEnd].Value),
		CivilDawn:        formatTime(times[suncalc.Dawn].Value),
		CivilDusk:        formatTime(times[suncalc.Dusk].Value),
		NauticalDawn:     formatTime(times[suncalc.NauticalDawn].Value),
		NauticalDusk:     formatTime(times[suncalc.NauticalDusk].Value),
		AstronomicalDawn: formatTime(times[suncalc.NightEnd].Value),
		AstronomicalDusk: formatTime(times[suncalc.Night].Value),
	}
}
