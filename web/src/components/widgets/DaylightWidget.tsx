import { useState, useEffect } from 'react'
import { Sunrise, Sunset } from 'lucide-react'
import Widget from '../Widget'
import { usePreferredLocation } from '../../usePreferredLocation'

type SunTimes =
  | { kind: 'normal'; sunrise: Date; sunset: Date }
  | { kind: 'polarDay' }
  | { kind: 'polarNight' }

/**
 * Compute approximate sunrise and sunset times for a given lat/lon and date.
 * Uses the NOAA simplified algorithm. Accurate to within ~1 minute for most locations.
 * Returns an explicit kind discriminant so callers can distinguish polar day from polar night
 * without recomputing solar angles.
 */
function getSunTimes(lat: number, lon: number, date: Date): SunTimes {
  // Days since J2000
  const startOfDay = new Date(date)
  startOfDay.setUTCHours(0, 0, 0, 0)
  const n = startOfDay.getTime() / 86400000 - 10957.5 // days since J2000.0

  // Mean longitude and anomaly (degrees)
  const L = ((280.46 + 0.9856474 * n) % 360 + 360) % 360
  const g = ((357.528 + 0.9856003 * n) % 360 + 360) % 360
  const gRad = (g * Math.PI) / 180

  // Ecliptic longitude (degrees)
  const lambda = L + 1.915 * Math.sin(gRad) + 0.02 * Math.sin(2 * gRad)
  const lambdaRad = (lambda * Math.PI) / 180

  // Obliquity of the ecliptic (degrees)
  const epsilon = 23.439 - 0.0000004 * n
  const epsilonRad = (epsilon * Math.PI) / 180

  // Sun's declination
  const sinDec = Math.sin(epsilonRad) * Math.sin(lambdaRad)
  const dec = Math.asin(sinDec)

  // Hour angle for sunrise/sunset (sun altitude = -0.8333° to account for refraction)
  const latRad = (lat * Math.PI) / 180
  const cosH =
    (Math.sin((-0.8333 * Math.PI) / 180) - Math.sin(latRad) * sinDec) /
    (Math.cos(latRad) * Math.cos(dec))

  // cosH > 1: sun never rises (polar night); cosH < -1: sun never sets (polar day)
  if (cosH > 1) return { kind: 'polarNight' }
  if (cosH < -1) return { kind: 'polarDay' }

  const H = (Math.acos(cosH) * 180) / Math.PI

  // Equation of time (minutes)
  const B = ((360 / 365) * (n - 81) * Math.PI) / 180
  const EoT = 9.87 * Math.sin(2 * B) - 7.53 * Math.cos(B) - 1.5 * Math.sin(B)

  // Solar noon in minutes from midnight UTC
  const solarNoonUTC = 720 - 4 * lon - EoT

  const sunriseMin = solarNoonUTC - H * 4
  const sunsetMin = solarNoonUTC + H * 4

  return {
    kind: 'normal',
    sunrise: new Date(startOfDay.getTime() + sunriseMin * 60000),
    sunset: new Date(startOfDay.getTime() + sunsetMin * 60000),
  }
}

function formatDuration(ms: number): string {
  const totalMinutes = Math.round(ms / 60000)
  const hours = Math.floor(totalMinutes / 60)
  const minutes = totalMinutes % 60
  return `${hours}h ${minutes}m`
}

function formatTime(date: Date): string {
  // Use Europe/Oslo explicitly so sunrise/sunset times always reflect the
  // location's local time regardless of the viewer's browser timezone.
  // All supported locations are in Norway (Europe/Oslo).
  return date.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', timeZone: 'Europe/Oslo' })
}

export default function DaylightWidget() {
  const location = usePreferredLocation()
  const [now, setNow] = useState(new Date())

  // Update current time every minute; pause when tab is hidden, resume (and sync) when visible
  useEffect(() => {
    const tick = () => setNow(new Date())
    let timer: ReturnType<typeof setInterval> | null = null

    const startTimer = () => {
      if (timer === null) timer = setInterval(tick, 60_000)
    }
    const stopTimer = () => {
      if (timer !== null) { clearInterval(timer); timer = null }
    }
    const onVisibility = () => {
      if (document.visibilityState === 'visible') { tick(); startTimer() }
      else stopTimer()
    }

    document.addEventListener('visibilitychange', onVisibility)
    if (document.visibilityState === 'visible') startTimer()

    return () => {
      stopTimer()
      document.removeEventListener('visibilitychange', onVisibility)
    }
  }, [])

  const sunTimes = getSunTimes(location.lat, location.lon, now)

  const daylightMs =
    sunTimes.kind === 'normal'
      ? sunTimes.sunset.getTime() - sunTimes.sunrise.getTime()
      : null
  const daylightStr = daylightMs != null ? formatDuration(daylightMs) : null

  // Progress through the day (0–1), clamped to daylight window
  let progress: number | null = null
  if (sunTimes.kind === 'normal') {
    const total = sunTimes.sunset.getTime() - sunTimes.sunrise.getTime()
    const elapsed = now.getTime() - sunTimes.sunrise.getTime()
    progress = Math.max(0, Math.min(1, elapsed / total))
  }

  return (
    <Widget title="Daylight">
      <div className="space-y-4">
        {sunTimes.kind === 'polarDay' ? (
          <p className="text-yellow-300 text-sm font-medium">Midnight sun — no darkness tonight!</p>
        ) : sunTimes.kind === 'polarNight' ? (
          <p className="text-blue-300 text-sm font-medium">Polar night — sun stays below the horizon</p>
        ) : (
          <>
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-2">
                <Sunrise size={16} className="text-orange-400" />
                <div>
                  <p className="text-xs text-gray-400">Sunrise</p>
                  <p className="text-sm font-medium">{formatTime(sunTimes.sunrise)}</p>
                </div>
              </div>
              <div className="flex items-center gap-2">
                <Sunset size={16} className="text-orange-500" />
                <div className="text-right">
                  <p className="text-xs text-gray-400">Sunset</p>
                  <p className="text-sm font-medium">{formatTime(sunTimes.sunset)}</p>
                </div>
              </div>
            </div>

            {progress !== null && (
              <div>
                <div className="w-full h-2 bg-gray-700 rounded-full overflow-hidden">
                  <div
                    className="h-full rounded-full transition-all duration-1000"
                    style={{
                      width: `${progress * 100}%`,
                      background: 'linear-gradient(to right, #f97316, #fbbf24)',
                    }}
                  />
                </div>
              </div>
            )}

            {daylightStr && (
              <p className="text-xs text-gray-400">
                {daylightStr} of daylight
                {progress !== null && progress > 0 && progress < 1 ? (
                  <span className="text-green-400"> · daylight now</span>
                ) : progress !== null && progress >= 1 ? (
                  <span className="text-blue-400"> · after sunset</span>
                ) : (
                  <span className="text-indigo-400"> · before sunrise</span>
                )}
              </p>
            )}

            <p className="text-xs text-gray-500">{location.name}</p>
          </>
        )}
      </div>
    </Widget>
  )
}
