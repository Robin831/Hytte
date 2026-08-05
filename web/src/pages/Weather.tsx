import { useEffect, useState } from 'react'
import type { TFunction } from 'i18next'
import { useTranslation } from 'react-i18next'
import { MapPin, RefreshCw } from 'lucide-react'
import { useAuth } from '../auth'
import { Skeleton } from '../components/ui/skeleton'
import LocationSearch from '../components/LocationSearch'
import UseMyLocationButton from '../components/UseMyLocationButton'
import HourlyChart from '../components/HourlyChart'
import CurrentConditionsCard from '../components/weather/CurrentConditionsCard'
import HourlyStrip from '../components/weather/HourlyStrip'
import DailyForecastList from '../components/weather/DailyForecastList'
import { useWeatherLocation } from '../hooks/useWeatherLocation'
import { useForecast } from '../hooks/useForecast'
import { useSunTimes } from '../hooks/useSunTimes'
import { buildDailyForecasts } from '../lib/weatherForecast'

/** How often the "Updated X min ago" label is recomputed. */
const TIME_AGO_TICK_MS = 30_000

function formatTimeAgo(date: Date, t: TFunction<'weather'>): string {
  const seconds = Math.floor((Date.now() - date.getTime()) / 1000)
  if (seconds < 60) return t('updated.justNow')
  const minutes = Math.floor(seconds / 60)
  if (minutes === 1) return t('updated.minuteAgo')
  return t('updated.minutesAgo', { count: minutes })
}

export default function Weather() {
  const { t } = useTranslation('weather')
  const { user } = useAuth()
  const {
    location,
    displayRecents,
    otherCities,
    locationResolved,
    selectByName,
    selectLocation,
  } = useWeatherLocation()

  const { data: forecast, loading, errorMessage, lastUpdated, refresh } = useForecast(location, {
    persist: true,
    userId: user?.id,
    autoRefresh: true,
    enabled: locationResolved,
  })
  const sun = useSunTimes(location, locationResolved)

  // Tick periodically to keep the "Updated X min ago" text current.
  const [, setTick] = useState(0)
  useEffect(() => {
    const timer = setInterval(() => setTick((v) => v + 1), TIME_AGO_TICK_MS)
    return () => clearInterval(timer)
  }, [])
  const timeAgo = lastUpdated ? formatTimeAgo(lastUpdated, t) : ''

  const timeseries = forecast?.properties?.timeseries ?? []
  const current = timeseries[0]
  const dailyForecasts = timeseries.length > 0 ? buildDailyForecasts(timeseries, t('page.today')) : []
  const currentSymbol =
    current?.data.next_1_hours?.summary.symbol_code ||
    current?.data.next_6_hours?.summary.symbol_code ||
    'cloudy'

  return (
    <main className="max-w-3xl mx-auto px-4 py-8 min-h-screen">
      <div className="flex items-center justify-between mb-8 flex-wrap gap-3">
        <h1 className="text-2xl font-bold">{t('page.title')}</h1>
        <div className="flex items-center gap-2 flex-wrap">
          <MapPin size={16} className="text-gray-400" />
          <LocationSearch onSelect={selectLocation} />
          <UseMyLocationButton onSelect={selectLocation} />
          <select
            value={location?.name ?? ''}
            onChange={(e) => selectByName(e.target.value)}
            className="bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:ring-2 focus:ring-blue-500"
            aria-label={t('page.selectLocation')}
          >
            {displayRecents.length > 0 && (
              <optgroup label={t('page.recentLocations')}>
                {displayRecents.map((loc) => (
                  <option key={`recent-${loc.name}`} value={loc.name}>
                    {loc.name}
                  </option>
                ))}
              </optgroup>
            )}
            {otherCities.length > 0 && (
              <optgroup label={t('page.allCities')}>
                {otherCities.map((loc) => (
                  <option key={`all-${loc.name}`} value={loc.name}>
                    {loc.name}
                  </option>
                ))}
              </optgroup>
            )}
          </select>
          <button
            onClick={refresh}
            disabled={loading}
            className="p-2 rounded-lg bg-gray-700 border border-gray-600 text-gray-300 hover:text-white hover:bg-gray-600 focus:outline-none focus:ring-2 focus:ring-blue-500 disabled:opacity-50 disabled:cursor-not-allowed"
            aria-label={t('page.refreshForecast')}
          >
            <RefreshCw size={16} className={loading ? 'animate-spin' : ''} />
          </button>
        </div>
      </div>

      {loading && !forecast && (
        <div className="space-y-4 py-4" aria-live="polite" aria-busy="true">
          <p className="sr-only">{t('page.loadingForecast')}</p>
          <Skeleton className="h-32 w-full" />
          <Skeleton className="h-48 w-full" />
        </div>
      )}

      {errorMessage && !forecast && (
        <div className="bg-red-900/30 border border-red-800 rounded-xl p-4 mb-6">
          <p className="text-red-400 text-sm">{errorMessage}</p>
        </div>
      )}

      {current && (
        <>
          <CurrentConditionsCard
            current={current}
            symbolCode={currentSymbol}
            locationName={location?.name}
            sun={sun}
            timeAgo={timeAgo}
          />

          {/* Hourly trend chart (next 24 hours) */}
          <HourlyChart timeseries={timeseries.slice(0, 24)} />

          <HourlyStrip timeseries={timeseries} />

          <DailyForecastList days={dailyForecasts} />

          {/* Attribution */}
          <p className="text-xs text-gray-500 mt-4 text-center">
            {t('page.attribution')}{' '}
            <a
              href="https://www.yr.no"
              target="_blank"
              rel="noopener noreferrer"
              className="underline hover:text-gray-400"
            >
              Yr
            </a>{' '}
            (MET Norway / NRK)
          </p>
        </>
      )}
    </main>
  )
}
