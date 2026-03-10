// Sunrise/sunset calculator using the NOAA solar position algorithm.
// Accurate to within ~1 minute for typical latitudes.

const DEG = Math.PI / 180
const RAD = 180 / Math.PI

export interface SunTimes {
  sunrise: Date | null // null during polar night
  sunset: Date | null // null during polar day
  daylightMinutes: number // 0 for polar night, 1440 for polar day
}

/**
 * Calculate sunrise and sunset for a given date and location.
 * Returns null for sunrise/sunset during polar day/night.
 */
export function getSunTimes(date: Date, lat: number, lon: number): SunTimes {
  // Julian date at noon UTC for the given calendar date
  const y = date.getFullYear()
  const m = date.getMonth() + 1
  const d = date.getDate()

  // Julian Day Number (noon UTC)
  const jdn =
    367 * y -
    Math.floor((7 * (y + Math.floor((m + 9) / 12))) / 4) +
    Math.floor((275 * m) / 9) +
    d +
    1721013.5
  const n = Math.ceil(jdn - 2451545.0 + 0.0008) // integer day count since J2000.0

  // Mean solar noon (approximate)
  const jStar = n - lon / 360

  // Solar mean anomaly (degrees)
  const M = (357.5291 + 0.98560028 * jStar) % 360

  // Equation of center (degrees)
  const C =
    1.9148 * Math.sin(M * DEG) +
    0.02 * Math.sin(2 * M * DEG) +
    0.0003 * Math.sin(3 * M * DEG)

  // Ecliptic longitude (degrees)
  const lambda = (M + C + 180 + 102.9372) % 360

  // Solar transit (Julian date)
  const jTransit = 2451545.0 + jStar + 0.0053 * Math.sin(M * DEG) - 0.0069 * Math.sin(2 * lambda * DEG)

  // Sun declination (radians)
  const sinDec = Math.sin(lambda * DEG) * Math.sin(23.4397 * DEG)
  const dec = Math.asin(sinDec)

  // Hour angle (cos)
  const cosOmega =
    (Math.sin(-0.833 * DEG) - Math.sin(lat * DEG) * Math.sin(dec)) /
    (Math.cos(lat * DEG) * Math.cos(dec))

  // Polar day or polar night
  if (cosOmega < -1) {
    // Sun never sets (midnight sun / polar day)
    return { sunrise: null, sunset: null, daylightMinutes: 1440 }
  }
  if (cosOmega > 1) {
    // Sun never rises (polar night)
    return { sunrise: null, sunset: null, daylightMinutes: 0 }
  }

  const omega = Math.acos(cosOmega) * RAD // hour angle in degrees

  // Sunrise and sunset as Julian dates
  const jRise = jTransit - omega / 360
  const jSet = jTransit + omega / 360

  return {
    sunrise: julianToDate(jRise),
    sunset: julianToDate(jSet),
    daylightMinutes: Math.round((omega * 2 / 360) * 1440),
  }
}

function julianToDate(jd: number): Date {
  // Convert Julian date to JavaScript Date
  const millis = (jd - 2440587.5) * 86400000
  return new Date(millis)
}

/**
 * Format daylight duration as "Xh Ym".
 */
export function formatDaylight(minutes: number): string {
  if (minutes >= 1440) return '24h 0m'
  if (minutes <= 0) return '0h 0m'
  const h = Math.floor(minutes / 60)
  const m = minutes % 60
  return `${h}h ${m}m`
}
