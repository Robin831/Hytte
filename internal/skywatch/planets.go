package skywatch

import (
	"math"
	"time"
)

// PlanetName identifies a planet for calculations and display.
type PlanetName string

const (
	Mercury PlanetName = "Mercury"
	Venus   PlanetName = "Venus"
	Mars    PlanetName = "Mars"
	Jupiter PlanetName = "Jupiter"
	Saturn  PlanetName = "Saturn"
)

// allPlanets is the ordered list of planets we compute visibility for.
var allPlanets = []PlanetName{Mercury, Venus, Mars, Jupiter, Saturn}

// AllPlanets returns the ordered list of planets we compute visibility for.
// The returned slice is a copy so callers cannot modify package state.
func AllPlanets() []PlanetName {
	return append([]PlanetName(nil), allPlanets...)
}

// PlanetInfo holds computed position and visibility data for a planet.
type PlanetInfo struct {
	Name       PlanetName `json:"name"`
	Altitude   float64    `json:"altitude"`    // degrees above horizon (negative = below)
	Azimuth    float64    `json:"azimuth"`     // degrees from north, clockwise
	Direction  string     `json:"direction"`   // compass direction (N/NE/E/SE/S/SW/W/NW)
	Visible    bool       `json:"visible"`     // true if above horizon and far enough from sun
	Status     string     `json:"status"`      // "visible_now", "rises_at", "not_visible"
	RiseTime   *string    `json:"rise_time"`   // next rise time (RFC3339), nil if always up/down
	SetTime    *string    `json:"set_time"`    // next set time (RFC3339), nil if always up/down
	Magnitude  float64    `json:"magnitude"`   // approximate visual magnitude
	Elongation float64    `json:"elongation"`  // angular distance from sun in degrees
}

// orbitalElements holds the mean orbital elements for a planet at J2000.0 epoch,
// plus their rates of change per century.
type orbitalElements struct {
	a0, aRate     float64 // semi-major axis (AU) and rate
	e0, eRate     float64 // eccentricity and rate
	i0, iRate     float64 // inclination (deg) and rate
	L0, LRate     float64 // mean longitude (deg) and rate
	w0, wRate     float64 // longitude of perihelion (deg) and rate
	node0, nRate  float64 // longitude of ascending node (deg) and rate
}

// planetElements contains Keplerian elements for J2000.0 epoch from JPL.
// Reference: Standish (1992), "Keplerian Elements for Approximate Positions of the Major Planets"
var planetElements = map[PlanetName]orbitalElements{
	Mercury: {
		a0: 0.38709927, aRate: 0.00000037,
		e0: 0.20563593, eRate: 0.00001906,
		i0: 7.00497902, iRate: -0.00594749,
		L0: 252.25032350, LRate: 149472.67411175,
		w0: 77.45779628, wRate: 0.16047689,
		node0: 48.33076593, nRate: -0.12534081,
	},
	Venus: {
		a0: 0.72333566, aRate: 0.00000390,
		e0: 0.00677672, eRate: -0.00004107,
		i0: 3.39467605, iRate: -0.00078890,
		L0: 181.97909950, LRate: 58517.81538729,
		w0: 131.60246718, wRate: 0.00268329,
		node0: 76.67984255, nRate: -0.27769418,
	},
	Mars: {
		a0: 1.52371034, aRate: 0.00001847,
		e0: 0.09339410, eRate: 0.00007882,
		i0: 1.84969142, iRate: -0.00813131,
		L0: -4.55343205, LRate: 19140.30268499,
		w0: -23.94362959, wRate: 0.44441088,
		node0: 49.55953891, nRate: -0.29257343,
	},
	Jupiter: {
		a0: 5.20288700, aRate: -0.00011607,
		e0: 0.04838624, eRate: -0.00013253,
		i0: 1.30439695, iRate: -0.00183714,
		L0: 34.39644051, LRate: 3034.74612775,
		w0: 14.72847983, wRate: 0.21252668,
		node0: 100.47390909, nRate: 0.20469106,
	},
	Saturn: {
		a0: 9.53667594, aRate: -0.00125060,
		e0: 0.05386179, eRate: -0.00050991,
		i0: 2.48599187, iRate: 0.00193609,
		L0: 49.95424423, LRate: 1222.49362201,
		w0: 92.59887831, wRate: -0.41897216,
		node0: 113.66242448, nRate: -0.28867794,
	},
}

// earthElements for computing Earth's position (needed for geocentric conversion).
var earthElements = orbitalElements{
	a0: 1.00000261, aRate: 0.00000562,
	e0: 0.01671123, eRate: -0.00004392,
	i0: -0.00001531, iRate: -0.01294668,
	L0: 100.46457166, LRate: 35999.37244981,
	w0: 102.93768193, wRate: 0.32327364,
	node0: 0.0, nRate: 0.0,
}

// sunElements for computing the Sun's position (simplified).
var sunElements = orbitalElements{
	L0: 280.46646, LRate: 36000.76983,
	w0: 282.93735, wRate: 0.32,
}

const deg2rad = math.Pi / 180.0
const rad2deg = 180.0 / math.Pi

// julianCenturies returns centuries since J2000.0 for the given time.
func julianCenturies(t time.Time) float64 {
	// J2000.0 = January 1.5, 2000 = JD 2451545.0
	j2000 := time.Date(2000, 1, 1, 12, 0, 0, 0, time.UTC)
	days := t.Sub(j2000).Hours() / 24.0
	return days / 36525.0
}

// normalizeAngle normalizes an angle to [0, 360).
func normalizeAngle(deg float64) float64 {
	deg = math.Mod(deg, 360)
	if deg < 0 {
		deg += 360
	}
	return deg
}

// solveKepler solves Kepler's equation M = E - e*sin(E) iteratively.
func solveKepler(M, e float64) float64 {
	M = M * deg2rad
	E := M
	for range 20 {
		dE := (M - (E - e*math.Sin(E))) / (1 - e*math.Cos(E))
		E += dE
		if math.Abs(dE) < 1e-12 {
			break
		}
	}
	return E
}

// heliocentricPosition computes the heliocentric ecliptic coordinates (x, y, z) in AU.
func heliocentricPosition(elem orbitalElements, T float64) (float64, float64, float64) {
	a := elem.a0 + elem.aRate*T
	e := elem.e0 + elem.eRate*T
	i := (elem.i0 + elem.iRate*T) * deg2rad
	L := normalizeAngle(elem.L0 + elem.LRate*T)
	w := normalizeAngle(elem.w0 + elem.wRate*T)
	node := normalizeAngle(elem.node0 + elem.nRate*T)

	// Mean anomaly
	M := normalizeAngle(L - w)

	// Solve Kepler's equation
	E := solveKepler(M, e)

	// True anomaly
	xv := a * (math.Cos(E) - e)
	yv := a * math.Sqrt(1-e*e) * math.Sin(E)

	// Distance and true anomaly
	v := math.Atan2(yv, xv)

	r := math.Sqrt(xv*xv + yv*yv)

	// Argument of perihelion (omega - node)
	argPeri := (w - node) * deg2rad
	nodeRad := node * deg2rad

	// Heliocentric ecliptic coordinates
	cosNode := math.Cos(nodeRad)
	sinNode := math.Sin(nodeRad)
	cosI := math.Cos(i)
	sinI := math.Sin(i)
	cosVW := math.Cos(v + argPeri)
	sinVW := math.Sin(v + argPeri)

	x := r * (cosNode*cosVW - sinNode*sinVW*cosI)
	y := r * (sinNode*cosVW + cosNode*sinVW*cosI)
	z := r * sinVW * sinI

	return x, y, z
}

// geocentricEcliptic converts heliocentric planet coords to geocentric ecliptic (lon, lat, dist).
func geocentricEcliptic(px, py, pz, ex, ey, ez float64) (float64, float64, float64) {
	dx := px - ex
	dy := py - ey
	dz := pz - ez

	dist := math.Sqrt(dx*dx + dy*dy + dz*dz)
	lon := math.Atan2(dy, dx) * rad2deg
	lat := math.Asin(dz/dist) * rad2deg

	return normalizeAngle(lon), lat, dist
}

// eclipticToEquatorial converts ecliptic coordinates to equatorial (RA, Dec).
// Obliquity of the ecliptic for J2000.0 ≈ 23.4393°.
func eclipticToEquatorial(lon, lat float64) (float64, float64) {
	eps := 23.4393 * deg2rad
	lonRad := lon * deg2rad
	latRad := lat * deg2rad

	sinDec := math.Sin(latRad)*math.Cos(eps) + math.Cos(latRad)*math.Sin(eps)*math.Sin(lonRad)
	dec := math.Asin(sinDec) * rad2deg

	y := math.Sin(lonRad)*math.Cos(eps) - math.Tan(latRad)*math.Sin(eps)
	x := math.Cos(lonRad)
	ra := normalizeAngle(math.Atan2(y, x) * rad2deg)

	return ra, dec
}

// equatorialToHorizontal converts RA/Dec to altitude/azimuth for a given location and time.
func equatorialToHorizontal(ra, dec, lat, lon float64, t time.Time) (float64, float64) {
	// Greenwich Mean Sidereal Time
	T := julianCenturies(t)
	gmst := normalizeAngle(280.46061837 + 360.98564736629*(t.Sub(time.Date(2000, 1, 1, 12, 0, 0, 0, time.UTC)).Hours()/24.0) + 0.000387933*T*T)

	// Local sidereal time
	lst := normalizeAngle(gmst + lon)

	// Hour angle
	ha := normalizeAngle(lst-ra) * deg2rad

	decRad := dec * deg2rad
	latRad := lat * deg2rad

	// Altitude
	sinAlt := math.Sin(decRad)*math.Sin(latRad) + math.Cos(decRad)*math.Cos(latRad)*math.Cos(ha)
	sinAlt = math.Max(-1, math.Min(1, sinAlt))
	alt := math.Asin(sinAlt) * rad2deg

	// Azimuth
	// Use atan2-based azimuth to avoid 0/0 at the poles or when the object is at zenith/nadir.
	y := -math.Sin(ha)
	x := math.Tan(decRad)*math.Cos(latRad) - math.Sin(latRad)*math.Cos(ha)
	az := normalizeAngle(math.Atan2(y, x) * rad2deg)

	return alt, az
}

// compassDirection returns the 8-point compass direction for an azimuth angle.
func compassDirection(az float64) string {
	az = normalizeAngle(az)
	directions := []string{"N", "NE", "E", "SE", "S", "SW", "W", "NW"}
	idx := int(math.Round(az/45.0)) % 8
	return directions[idx]
}

// sunPosition computes the Sun's ecliptic longitude (simplified).
func sunEclipticLongitude(T float64) float64 {
	L := normalizeAngle(sunElements.L0 + sunElements.LRate*T)
	g := normalizeAngle(L - (sunElements.w0 + sunElements.wRate*T))
	gRad := g * deg2rad
	// Equation of center (first two terms)
	lon := L + 1.9146*math.Sin(gRad) + 0.0200*math.Sin(2*gRad)
	return normalizeAngle(lon)
}

// angularSeparation computes the angular distance between two ecliptic longitudes.
func angularSeparation(lon1, lon2 float64) float64 {
	diff := math.Abs(lon1 - lon2)
	if diff > 180 {
		diff = 360 - diff
	}
	return diff
}

// approximateMagnitude returns a rough visual magnitude for a planet given its
// distance from sun (r AU), distance from earth (d AU), and phase angle.
func approximateMagnitude(planet PlanetName, r, d, phaseAngle float64) float64 {
	// Base absolute magnitudes (V(1,0) values)
	// These are standard photometric values from the Astronomical Almanac
	i := phaseAngle * deg2rad

	switch planet {
	case Mercury:
		return -0.36 + 5*math.Log10(r*d) + 3.80*(i/math.Pi) - 2.73*math.Pow(i/math.Pi, 2) + 2.00*math.Pow(i/math.Pi, 3)
	case Venus:
		return -4.34 + 5*math.Log10(r*d) + 0.013*(i*rad2deg) + 4.2e-7*math.Pow(i*rad2deg, 3)
	case Mars:
		return -1.51 + 5*math.Log10(r*d) + 0.016*(i*rad2deg)
	case Jupiter:
		return -9.40 + 5*math.Log10(r*d) + 0.005*(i*rad2deg)
	case Saturn:
		// Simplified — doesn't account for ring tilt
		return -8.88 + 5*math.Log10(r*d) + 0.044*(i*rad2deg)
	default:
		return 99
	}
}

// findRiseSet searches for the next rise or set time of a planet from the given start time.
// It steps forward in 10-minute increments looking for altitude to cross 0°.
// direction: +1 for rise (crossing from below to above), -1 for set (above to below).
func findRiseSet(planet PlanetName, lat, lon float64, start time.Time, direction int) *time.Time {
	T := julianCenturies(start)
	px, py, pz := heliocentricPosition(planetElements[planet], T)
	ex, ey, ez := heliocentricPosition(earthElements, T)
	ecLon, ecLat, _ := geocentricEcliptic(px, py, pz, ex, ey, ez)
	ra, dec := eclipticToEquatorial(ecLon, ecLat)
	prevAlt, _ := equatorialToHorizontal(ra, dec, lat, lon, start)

	step := 10 * time.Minute
	// Search up to 24 hours ahead
	for i := 1; i <= 144; i++ {
		t := start.Add(time.Duration(i) * step)
		T = julianCenturies(t)
		px, py, pz = heliocentricPosition(planetElements[planet], T)
		ex, ey, ez = heliocentricPosition(earthElements, T)
		ecLon, ecLat, _ = geocentricEcliptic(px, py, pz, ex, ey, ez)
		ra, dec = eclipticToEquatorial(ecLon, ecLat)
		alt, _ := equatorialToHorizontal(ra, dec, lat, lon, t)

		if direction > 0 && prevAlt < 0 && alt >= 0 {
			// Refine with bisection
			return refineTransit(planet, lat, lon, t.Add(-step), t, true)
		}
		if direction < 0 && prevAlt >= 0 && alt < 0 {
			return refineTransit(planet, lat, lon, t.Add(-step), t, false)
		}
		prevAlt = alt
	}
	return nil
}

// refineTransit uses bisection to find the precise moment altitude crosses 0°.
func refineTransit(planet PlanetName, lat, lon float64, t1, t2 time.Time, rising bool) *time.Time {
	for range 20 {
		mid := t1.Add(t2.Sub(t1) / 2)
		T := julianCenturies(mid)
		px, py, pz := heliocentricPosition(planetElements[planet], T)
		ex, ey, ez := heliocentricPosition(earthElements, T)
		ecLon, ecLat, _ := geocentricEcliptic(px, py, pz, ex, ey, ez)
		ra, dec := eclipticToEquatorial(ecLon, ecLat)
		alt, _ := equatorialToHorizontal(ra, dec, lat, lon, mid)

		if rising {
			if alt < 0 {
				t1 = mid
			} else {
				t2 = mid
			}
		} else {
			if alt >= 0 {
				t1 = mid
			} else {
				t2 = mid
			}
		}

		if t2.Sub(t1) < time.Second {
			break
		}
	}
	result := t1.Add(t2.Sub(t1) / 2)
	return &result
}

// GetPlanetPositions computes positions and visibility for all naked-eye planets.
func GetPlanetPositions(t time.Time, lat, lon float64) []PlanetInfo {
	T := julianCenturies(t)

	// Earth's heliocentric position
	ex, ey, ez := heliocentricPosition(earthElements, T)

	// Sun's ecliptic longitude (for elongation calculation)
	sunLon := sunEclipticLongitude(T)

	// Earth's distance from sun
	earthDist := math.Sqrt(ex*ex + ey*ey + ez*ez)

	planets := make([]PlanetInfo, len(allPlanets))

	for idx, name := range allPlanets {
		elem := planetElements[name]

		// Planet's heliocentric position
		px, py, pz := heliocentricPosition(elem, T)

		// Geocentric ecliptic coordinates
		ecLon, ecLat, geoDist := geocentricEcliptic(px, py, pz, ex, ey, ez)

		// Equatorial coordinates
		ra, dec := eclipticToEquatorial(ecLon, ecLat)

		// Horizontal coordinates
		alt, az := equatorialToHorizontal(ra, dec, lat, lon, t)

		// Elongation from sun
		elongation := angularSeparation(ecLon, sunLon)

		// Heliocentric distance of planet
		helioDist := math.Sqrt(px*px + py*py + pz*pz)

		// Phase angle (Sun-Planet-Earth angle) via law of cosines
		cosPhase := (helioDist*helioDist + geoDist*geoDist - earthDist*earthDist) / (2 * helioDist * geoDist)
		cosPhase = math.Max(-1, math.Min(1, cosPhase))
		phaseAngle := math.Acos(cosPhase) * rad2deg

		mag := approximateMagnitude(name, helioDist, geoDist, phaseAngle)

		// Visibility: above horizon AND far enough from sun.
		// Inner planets (Mercury, Venus) need larger elongation thresholds because
		// they are always close to the sun and only observable near the horizon in twilight.
		minElongation := 15.0
		if name == Mercury {
			minElongation = 18.0
		} else if name == Venus {
			minElongation = 18.0
		}
		visible := alt > 0 && elongation > minElongation

		// Determine status
		status := "not_visible"
		var riseTime, setTime *string
		if visible {
			status = "visible_now"
			// Find set time
			if st := findRiseSet(name, lat, lon, t, -1); st != nil {
				s := st.Format(time.RFC3339)
				setTime = &s
			}
		} else if alt <= 0 {
			// Planet is below horizon — find next rise
			if rt := findRiseSet(name, lat, lon, t, +1); rt != nil && elongation > minElongation {
				status = "rises_at"
				s := rt.Format(time.RFC3339)
				riseTime = &s
			}
		}

		planets[idx] = PlanetInfo{
			Name:       name,
			Altitude:   math.Round(alt*10) / 10,
			Azimuth:    math.Round(az*10) / 10,
			Direction:  compassDirection(az),
			Visible:    visible,
			Status:     status,
			RiseTime:   riseTime,
			SetTime:    setTime,
			Magnitude:  math.Round(mag*10) / 10,
			Elongation: math.Round(elongation*10) / 10,
		}
	}

	return planets
}
