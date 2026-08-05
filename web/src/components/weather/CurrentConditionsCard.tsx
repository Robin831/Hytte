import { useTranslation } from 'react-i18next'
import { ArrowUp, Droplets, Sunrise, Sunset, Thermometer, Wind } from 'lucide-react'
import type { TimeseriesEntry } from '../../lib/weatherForecast'
import type { SunTimes } from '../../hooks/useSunTimes'
import { getWeatherDescription, getWeatherIcon } from '../../weatherUtils'
import { formatTime } from '../../utils/formatDate'

/** Split a daylight duration in seconds into whole hours and minutes. */
function splitDaylight(seconds: number): { hours: number; minutes: number } {
  const totalMinutes = Math.max(0, Math.round(seconds / 60))
  return { hours: Math.floor(totalMinutes / 60), minutes: totalMinutes % 60 }
}

/**
 * Calculate feels-like temperature.
 * Uses wind chill when temp ≤ 10°C and wind ≥ 1.3 m/s (Environment Canada formula),
 * or heat index when temp ≥ 27°C and humidity ≥ 40% (Rothfusz regression).
 * Returns null when actual temperature already represents perceived comfort.
 */
function calculateFeelsLike(temp: number, windSpeed: number, humidity: number): number | null {
  if (temp <= 10 && windSpeed >= 1.3) {
    const v = windSpeed * 3.6 // m/s to km/h
    const wc = 13.12 + 0.6215 * temp - 11.37 * Math.pow(v, 0.16) + 0.3965 * temp * Math.pow(v, 0.16)
    const rounded = Math.round(wc)
    return rounded !== Math.round(temp) ? rounded : null
  }
  if (temp >= 27 && humidity >= 40) {
    // Rothfusz regression (Fahrenheit), then convert back
    const tf = temp * 9 / 5 + 32
    const hi =
      -42.379 + 2.04901523 * tf + 10.14333127 * humidity
      - 0.22475541 * tf * humidity - 0.00683783 * tf * tf
      - 0.05481717 * humidity * humidity + 0.00122874 * tf * tf * humidity
      + 0.00085282 * tf * humidity * humidity - 0.00000199 * tf * tf * humidity * humidity
    const rounded = Math.round((hi - 32) * 5 / 9)
    return rounded !== Math.round(temp) ? rounded : null
  }
  return null
}

/**
 * Wind direction arrow rotation in degrees (CSS clockwise).
 * wind_from_direction = 180 (from south) → arrow points north (0°).
 */
function windArrowRotation(windFromDirection: number): number {
  return (windFromDirection + 180) % 360
}

interface CurrentConditionsCardProps {
  current: TimeseriesEntry
  /** Symbol code for the current hour, already resolved with a fallback. */
  symbolCode: string
  locationName?: string
  sun: SunTimes | null
  /** Pre-formatted "Updated X min ago" text, or empty when nothing has loaded. */
  timeAgo: string
}

export default function CurrentConditionsCard({
  current,
  symbolCode,
  locationName,
  sun,
  timeAgo,
}: CurrentConditionsCardProps) {
  const { t } = useTranslation('weather')
  const details = current.data.instant.details
  const feelsLike = calculateFeelsLike(
    details.air_temperature,
    details.wind_speed,
    details.relative_humidity,
  )

  return (
    <section className="bg-gray-800 rounded-xl p-6 mb-6">
      <div className="flex items-center justify-between">
        <div>
          <p className="text-sm text-gray-400 mb-1">{t('page.rightNowIn', { location: locationName })}</p>
          <div className="flex items-end gap-3">
            <span className="text-5xl font-bold">{Math.round(details.air_temperature)}°</span>
            {feelsLike !== null && (
              <span className="text-sm text-gray-400 mb-2">
                {t('page.feelsLike', { temp: feelsLike })}
              </span>
            )}
            <span className="text-lg text-gray-300 mb-1">
              {getWeatherDescription(symbolCode, t)}
            </span>
          </div>
          {sun && (
            <div className="mt-3 flex flex-wrap items-center gap-x-4 gap-y-1 text-sm text-gray-300">
              {sun.polarDay ? (
                <span className="flex items-center gap-1.5">
                  <Sunrise size={16} className="text-amber-400 shrink-0" />
                  {t('sun.polarDay')}
                </span>
              ) : sun.polarNight ? (
                <span className="flex items-center gap-1.5">
                  <Sunset size={16} className="text-indigo-400 shrink-0" />
                  {t('sun.polarNight')}
                </span>
              ) : sun.sunrise && sun.sunset ? (
                <>
                  <span
                    className="flex items-center gap-1.5"
                    aria-label={`${t('sun.sunrise')} ${formatTime(sun.sunrise, { hour: '2-digit', minute: '2-digit' })}`}
                  >
                    <Sunrise size={16} className="text-amber-400 shrink-0" />
                    {formatTime(sun.sunrise, { hour: '2-digit', minute: '2-digit' })}
                  </span>
                  <span
                    className="flex items-center gap-1.5"
                    aria-label={`${t('sun.sunset')} ${formatTime(sun.sunset, { hour: '2-digit', minute: '2-digit' })}`}
                  >
                    <Sunset size={16} className="text-orange-400 shrink-0" />
                    {formatTime(sun.sunset, { hour: '2-digit', minute: '2-digit' })}
                  </span>
                  <span className="text-gray-400">
                    {t('sun.daylight', splitDaylight(sun.daylightSeconds))}
                  </span>
                </>
              ) : null}
            </div>
          )}
        </div>
        <div className="text-blue-400">{getWeatherIcon(symbolCode, 56)}</div>
      </div>

      <div className="grid grid-cols-3 gap-4 mt-6 pt-4 border-t border-gray-700">
        <div className="flex items-center gap-2">
          <Wind size={16} className="text-gray-400" />
          <div>
            <p className="text-xs text-gray-400">{t('page.wind')}</p>
            <p className="text-sm font-medium flex items-center gap-1">
              {details.wind_speed} m/s
              {details.wind_from_direction !== undefined && (
                <ArrowUp
                  size={14}
                  className="text-gray-400 shrink-0"
                  style={{ transform: `rotate(${windArrowRotation(details.wind_from_direction)}deg)` }}
                  aria-label={t('page.windFromDirectionAria', { degrees: Math.round(details.wind_from_direction) })}
                />
              )}
            </p>
          </div>
        </div>
        <div className="flex items-center gap-2">
          <Droplets size={16} className="text-gray-400" />
          <div>
            <p className="text-xs text-gray-400">{t('page.humidity')}</p>
            <p className="text-sm font-medium">{Math.round(details.relative_humidity)}%</p>
          </div>
        </div>
        <div className="flex items-center gap-2">
          <Thermometer size={16} className="text-gray-400" />
          <div>
            <p className="text-xs text-gray-400">{t('page.pressure')}</p>
            <p className="text-sm font-medium">
              {details.air_pressure_at_sea_level
                ? `${Math.round(details.air_pressure_at_sea_level)} hPa`
                : '—'}
            </p>
          </div>
        </div>
      </div>
      {timeAgo && <p className="text-xs text-gray-500 mt-4">{timeAgo}</p>}
    </section>
  )
}
