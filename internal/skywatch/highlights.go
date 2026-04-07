package skywatch

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/sixdouglas/suncalc"
)

// Highlight represents a notable sky event for tonight.
type Highlight struct {
	Type   string            `json:"type"`
	Key    string            `json:"key"`
	Params map[string]string `json:"params"`
}

// celestialPos holds position data for an object (planet or moon) at a given time.
type celestialPos struct {
	name       string
	altDeg     float64
	azDeg      float64
	isPlanet   bool
	magnitude  float64
	elongation float64
	direction  string
}

// angularSepAltAz computes the angular separation in degrees between two objects
// given their altitude and azimuth in degrees.
func angularSepAltAz(alt1, az1, alt2, az2 float64) float64 {
	a1 := alt1 * deg2rad
	a2 := alt2 * deg2rad
	daz := (az1 - az2) * deg2rad
	cosD := math.Sin(a1)*math.Sin(a2) + math.Cos(a1)*math.Cos(a2)*math.Cos(daz)
	cosD = math.Max(-1, math.Min(1, cosD))
	return math.Acos(cosD) * rad2deg
}

// formatDeg formats a float as a string with one decimal place.
func formatDeg(deg float64) string {
	return fmt.Sprintf("%.1f", math.Round(deg*10)/10)
}

// planetNameKey returns the lowercase i18n key for a planet name.
func planetNameKey(name string) string {
	return strings.ToLower(name)
}

// conjunctionThreshold is the maximum angular separation (degrees) for a conjunction.
const conjunctionThreshold = 5.0

// oppositionThreshold is the minimum elongation (degrees) for opposition detection.
const oppositionThreshold = 170.0

// brightMagnitudeThreshold is the magnitude below which a planet is considered very bright.
const brightMagnitudeThreshold = -2.0

// GetTonightHighlights computes notable sky events for the evening of the given date.
// It detects conjunctions (objects within 5°), planets at opposition, and exceptionally bright planets.
func GetTonightHighlights(date time.Time, lat, lon float64) []Highlight {
	// Determine evening observation time: sunset + 1 hour.
	sunTimes := suncalc.GetTimes(date, lat, lon)
	sunset := sunTimes[suncalc.Sunset].Value

	var eveningTime time.Time
	if !sunset.IsZero() {
		eveningTime = sunset.Add(time.Hour)
	} else {
		// Polar day/night — use 21:00 local.
		eveningTime = time.Date(date.Year(), date.Month(), date.Day(), 21, 0, 0, 0, date.Location())
	}

	// Planet positions at evening time.
	planets := GetPlanetPositions(eveningTime, lat, lon)

	// Moon position at evening time.
	moonPos := suncalc.GetMoonPosition(eveningTime, lat, lon)
	moonAltDeg := moonPos.Altitude * rad2deg
	// suncalc azimuth: from south, positive west → convert to from north, clockwise.
	moonAzDeg := normalizeAngle(moonPos.Azimuth*rad2deg + 180)

	// Build list of celestial objects.
	objects := make([]celestialPos, 0, len(planets)+1)

	objects = append(objects, celestialPos{
		name:      "Moon",
		altDeg:    moonAltDeg,
		azDeg:     moonAzDeg,
		isPlanet:  false,
		direction: compassDirection(moonAzDeg),
	})

	for _, p := range planets {
		objects = append(objects, celestialPos{
			name:       string(p.Name),
			altDeg:     p.Altitude,
			azDeg:      p.Azimuth,
			isPlanet:   true,
			magnitude:  p.Magnitude,
			elongation: p.Elongation,
			direction:  p.Direction,
		})
	}

	var highlights []Highlight

	// 1. Detect conjunctions (two objects within 5°).
	for i := 0; i < len(objects); i++ {
		for j := i + 1; j < len(objects); j++ {
			a, b := objects[i], objects[j]
			// At least one must be above the horizon.
			if a.altDeg <= 0 && b.altDeg <= 0 {
				continue
			}
			sep := angularSepAltAz(a.altDeg, a.azDeg, b.altDeg, b.azDeg)
			if sep > conjunctionThreshold {
				continue
			}
			if a.name == "Moon" || b.name == "Moon" {
				planet := a.name
				if a.name == "Moon" {
					planet = b.name
				}
				highlights = append(highlights, Highlight{
					Type: "moon_conjunction",
					Key:  "highlights.moonNearPlanet",
					Params: map[string]string{
						"planetKey": planetNameKey(planet),
						"degrees":   formatDeg(sep),
					},
				})
			} else {
				highlights = append(highlights, Highlight{
					Type: "planet_conjunction",
					Key:  "highlights.planetConjunction",
					Params: map[string]string{
						"planet1Key": planetNameKey(a.name),
						"planet2Key": planetNameKey(b.name),
						"degrees":    formatDeg(sep),
					},
				})
			}
		}
	}

	// 2. Detect opposition (outer planets with elongation > 170°).
	for _, p := range planets {
		if p.Name != Mars && p.Name != Jupiter && p.Name != Saturn {
			continue
		}
		if p.Elongation > oppositionThreshold {
			highlights = append(highlights, Highlight{
				Type: "opposition",
				Key:  "highlights.opposition",
				Params: map[string]string{
					"planetKey": planetNameKey(string(p.Name)),
				},
			})
		}
	}

	// 3. Bright planet highlights (magnitude < -2 and visible).
	for _, p := range planets {
		if !p.Visible {
			continue
		}
		if p.Magnitude < brightMagnitudeThreshold {
			highlights = append(highlights, Highlight{
				Type: "bright_planet",
				Key:  "highlights.brightPlanet",
				Params: map[string]string{
					"planetKey": planetNameKey(string(p.Name)),
					"direction": p.Direction,
				},
			})
		}
	}

	return highlights
}
