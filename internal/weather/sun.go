package weather

import (
	"math"
	"time"
)

// SunData holds solar-day information for a location on a given date.
//
// Sunrise and Sunset are absolute instants in UTC. When the sun never sets
// (polar day) or never rises (polar night), Sunrise and Sunset are nil and the
// corresponding PolarDay / PolarNight flag is set instead.
type SunData struct {
	Sunrise         *time.Time
	Sunset          *time.Time
	DaylightSeconds int
	PolarDay        bool
	PolarNight      bool
}

// ComputeSunData computes sunrise, sunset, and daylight length for the given
// latitude/longitude on the calendar date of the supplied time (interpreted in
// UTC). It uses the NOAA-derived sunrise equation, which is accurate to roughly
// a minute — more than enough for display purposes — and avoids an upstream
// dependency, making polar edge cases deterministic.
//
// The returned Sunrise/Sunset are UTC instants; callers format them in the
// viewer's locale/timezone.
func ComputeSunData(lat, lon float64, date time.Time) SunData {
	const rad = math.Pi / 180

	y, mo, d := date.UTC().Date()
	jd := julianDate(y, int(mo), d)

	// n: integer number of days since J2000.0 (2000-01-01 12:00 UTC) for the
	// solar noon belonging to this calendar date. The 0.0008 term is a small
	// leap-second correction from the reference algorithm.
	n := math.Round(jd - 2451545.0 + 0.0008)

	// Mean solar noon. lw is the longitude measured positive to the west, so an
	// east-positive longitude becomes -lon.
	lw := -lon
	jStar := n - lw/360.0

	// Solar mean anomaly.
	m := math.Mod(357.5291+0.98560028*jStar, 360)

	// Equation of the center.
	c := 1.9148*math.Sin(m*rad) + 0.0200*math.Sin(2*m*rad) + 0.0003*math.Sin(3*m*rad)

	// Ecliptic longitude.
	lambda := math.Mod(m+c+180+102.9372, 360)

	// Solar transit (Julian date of solar noon).
	jTransit := 2451545.0 + jStar + 0.0053*math.Sin(m*rad) - 0.0069*math.Sin(2*lambda*rad)

	// Solar declination.
	sinDecl := math.Sin(lambda*rad) * math.Sin(23.4397*rad)
	decl := math.Asin(sinDecl)

	// Hour angle of sunrise/sunset. -0.833° accounts for atmospheric refraction
	// and the sun's apparent radius (the standard official sunrise altitude).
	cosOmega := (math.Sin(-0.833*rad) - math.Sin(lat*rad)*math.Sin(decl)) /
		(math.Cos(lat*rad) * math.Cos(decl))

	if math.IsNaN(cosOmega) {
		// Degenerate case (e.g. lat=±90): determine polar condition from
		// whether the latitude and solar declination share the same sign.
		if lat*sinDecl >= 0 {
			return SunData{PolarDay: true, DaylightSeconds: 24 * 3600}
		}
		return SunData{PolarNight: true, DaylightSeconds: 0}
	}
	if cosOmega < -1 {
		return SunData{PolarDay: true, DaylightSeconds: 24 * 3600}
	}
	if cosOmega > 1 {
		return SunData{PolarNight: true, DaylightSeconds: 0}
	}

	omega := math.Acos(cosOmega) / rad // degrees

	sunrise := julianToTime(jTransit - omega/360.0)
	sunset := julianToTime(jTransit + omega/360.0)

	daylight := int(math.Round(sunset.Sub(sunrise).Seconds()))
	if daylight < 0 {
		daylight = 0
	}

	return SunData{
		Sunrise:         &sunrise,
		Sunset:          &sunset,
		DaylightSeconds: daylight,
	}
}

// julianDate returns the Julian Date at 00:00 UTC for the given Gregorian
// calendar date.
func julianDate(year, month, day int) float64 {
	if month <= 2 {
		year--
		month += 12
	}
	a := year / 100
	b := 2 - a + a/4
	return math.Floor(365.25*float64(year+4716)) +
		math.Floor(30.6001*float64(month+1)) +
		float64(day) + float64(b) - 1524.5
}

// julianToTime converts a Julian Date (UTC) to a time.Time in UTC.
func julianToTime(jd float64) time.Time {
	// 2440587.5 is the Julian Date of the Unix epoch (1970-01-01 00:00 UTC).
	unixSeconds := (jd - 2440587.5) * 86400.0
	return time.Unix(int64(math.Round(unixSeconds)), 0).UTC()
}
