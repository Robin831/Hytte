import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { CloudOff, MapPin, RefreshCw, X } from 'lucide-react'
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
import { useCurrentTime } from '../hooks/useCurrentTime'
import {
  buildDailyForecasts,
  formatTimeAgo,
  selectCurrentIndex,
  selectUpcoming,
} from '../lib/weatherForecast'

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

  const { data: forecast, loading, error, errorMessage, lastUpdated, refresh } = useForecast(location, {
    persist: true,
    userId: user?.id,
    autoRefresh: true,
    enabled: locationResolved,
  })
  const sun = useSunTimes(location, locationResolved)

  // A live clock keeps the "Updated X ago" text and the now-relative entry
  // selection below current, so the page rolls over to the next hour on its own
  // instead of waiting for a fetch to re-render it.
  const now = useCurrentTime().getTime()

  // Re-armed on every new failure so the chip returns after the user dismissed an
  // earlier one, but stays hidden for the rest of the current failure. Compared
  // against the previous render rather than reset from an effect, which would
  // paint the dismissed chip once before removing it.
  const [staleDismissed, setStaleDismissed] = useState(false)
  const [dismissedForError, setDismissedForError] = useState(error)
  if (dismissedForError !== error) {
    setDismissedForError(error)
    setStaleDismissed(false)
  }

  const timeAgo = lastUpdated ? formatTimeAgo(lastUpdated, t, now) : ''

  const timeseries = forecast?.properties?.timeseries ?? []
  // Index 0 is only "right now" for a freshly fetched forecast. A cached one can
  // start hours or days back, so pick the entry nearest the clock instead. When
  // the whole series has elapsed this still resolves to the last entry, keeping
  // the card populated while the stale chip explains what it is showing.
  const currentIndex = selectCurrentIndex(timeseries, now)
  const current = currentIndex >= 0 ? timeseries[currentIndex] : undefined
  const upcoming = selectUpcoming(timeseries, now)
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

      {/*
        Only surface the error when there is nothing to show. A failed background
        refresh (interval tick, tab re-show, manual refresh) leaves the previous
        forecast on screen untouched rather than replacing it with a banner.
      */}
      {errorMessage && !forecast && (
        <div className="bg-red-900/30 border border-red-800 rounded-xl p-4 mb-6">
          <p className="text-red-400 text-sm">{errorMessage}</p>
        </div>
      )}

      {/*
        A failed refresh with a forecast still on screen is exactly the case the
        banner above skips. Say plainly that the numbers come from the cache
        rather than letting an hours-old reading pass for the current conditions.
      */}
      {error && forecast && !staleDismissed && (
        <div
          role="status"
          className="flex items-center gap-2 bg-amber-900/30 border border-amber-800 rounded-xl px-4 py-2 mb-4 text-sm text-amber-300"
        >
          <CloudOff size={16} className="shrink-0" />
          <span className="flex-1">{t('stale.chip')}</span>
          <button
            type="button"
            onClick={() => setStaleDismissed(true)}
            aria-label={t('stale.dismiss')}
            className="p-1 -mr-1 rounded text-amber-400 hover:text-amber-200 focus:outline-none focus-visible:ring-2 focus-visible:ring-amber-500"
          >
            <X size={16} />
          </button>
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
          <HourlyChart timeseries={upcoming.slice(0, 24)} />

          <HourlyStrip timeseries={upcoming} />

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
