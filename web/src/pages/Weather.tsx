import { useState, useEffect, useReducer, useCallback, useRef } from 'react'
import { useAuth } from '../auth'
import { NORWEGIAN_CITIES } from '../norwegianCities'
import {
  Cloud,
  CloudDrizzle,
  CloudFog,
  CloudLightning,
  CloudRain,
  CloudSnow,
  CloudSun,
  Droplets,
  Sun,
  Wind,
  MapPin,
  Thermometer,
  RefreshCw,
} from 'lucide-react'


interface TimeseriesEntry {
  time: string
  data: {
    instant: {
      details: {
        air_temperature: number
        wind_speed: number
        relative_humidity: number
        air_pressure_at_sea_level?: number
        wind_from_direction?: number
      }
    }
    next_1_hours?: {
      summary: { symbol_code: string }
      details: { precipitation_amount: number }
    }
    next_6_hours?: {
      summary: { symbol_code: string }
      details: { precipitation_amount: number }
    }
    next_12_hours?: {
      summary: { symbol_code: string }
    }
  }
}

interface ForecastResponse {
  properties: {
    timeseries: TimeseriesEntry[]
  }
}

interface DayForecast {
  date: string
  dayName: string
  symbolCode: string
  tempMin: number
  tempMax: number
  precipitation: number
  windSpeed: number
}

type FetchState = { loading: boolean; error: string | null; forecast: ForecastResponse | null; lastUpdated: Date | null }
type FetchAction =
  | { type: 'start' }
  | { type: 'success'; data: ForecastResponse }
  | { type: 'error'; message: string }

function fetchReducer(state: FetchState, action: FetchAction): FetchState {
  switch (action.type) {
    case 'start': return { ...state, loading: true, error: null }
    case 'success': return { loading: false, error: null, forecast: action.data, lastUpdated: new Date() }
    case 'error': return { loading: false, error: action.message, forecast: state.forecast, lastUpdated: state.lastUpdated }
    default: return state
  }
}

const AUTO_REFRESH_MS = 10 * 60 * 1000 // 10 minutes

function formatTimeAgo(date: Date): string {
  const seconds = Math.floor((Date.now() - date.getTime()) / 1000)
  if (seconds < 60) return 'Updated just now'
  const minutes = Math.floor(seconds / 60)
  if (minutes === 1) return 'Updated 1 min ago'
  return `Updated ${minutes} min ago`
}

function getWeatherIcon(symbolCode: string, size = 24) {
  const code = symbolCode.replace(/_day|_night|_polartwilight/g, '')
  const props = { size, className: 'shrink-0' }

  if (code.includes('thunder')) return <CloudLightning {...props} />
  if (code.includes('snow') || code.includes('sleet')) return <CloudSnow {...props} />
  if (code.includes('drizzle') || code.includes('lightrain')) return <CloudDrizzle {...props} />
  if (code.includes('heavyrain') || code.includes('rain')) return <CloudRain {...props} />
  if (code.includes('fog')) return <CloudFog {...props} />
  if (code === 'clearsky') return <Sun {...props} />
  if (code === 'fair' || code.includes('partlycloudy')) return <CloudSun {...props} />
  return <Cloud {...props} />
}

function getWeatherDescription(symbolCode: string): string {
  const code = symbolCode.replace(/_day|_night|_polartwilight/g, '')
  const descriptions: Record<string, string> = {
    clearsky: 'Clear sky',
    fair: 'Fair',
    partlycloudy: 'Partly cloudy',
    cloudy: 'Cloudy',
    lightrainshowers: 'Light rain showers',
    rainshowers: 'Rain showers',
    heavyrainshowers: 'Heavy rain showers',
    lightrainshowersandthunder: 'Light rain & thunder',
    rainshowersandthunder: 'Rain & thunder',
    heavyrainshowersandthunder: 'Heavy rain & thunder',
    lightsleetshowers: 'Light sleet showers',
    sleetshowers: 'Sleet showers',
    heavysleetshowers: 'Heavy sleet showers',
    lightsnowshowers: 'Light snow showers',
    snowshowers: 'Snow showers',
    heavysnowshowers: 'Heavy snow showers',
    lightrain: 'Light rain',
    rain: 'Rain',
    heavyrain: 'Heavy rain',
    lightrainandthunder: 'Light rain & thunder',
    rainandthunder: 'Rain & thunder',
    heavyrainandthunder: 'Heavy rain & thunder',
    lightsleet: 'Light sleet',
    sleet: 'Sleet',
    heavysleet: 'Heavy sleet',
    lightsnow: 'Light snow',
    snow: 'Snow',
    heavysnow: 'Heavy snow',
    fog: 'Fog',
  }
  return descriptions[code] || code.replace(/_/g, ' ')
}

function buildDailyForecasts(timeseries: TimeseriesEntry[]): DayForecast[] {
  const dayMap = new Map<string, {
    temps: number[]
    winds: number[]
    precip: number
    symbols: string[]
    date: Date
  }>()

  for (const entry of timeseries) {
    const dt = new Date(entry.time)
    const dateKey = `${dt.getFullYear()}-${String(dt.getMonth() + 1).padStart(2, '0')}-${String(dt.getDate()).padStart(2, '0')}`

    if (!dayMap.has(dateKey)) {
      dayMap.set(dateKey, { temps: [], winds: [], precip: 0, symbols: [], date: dt })
    }
    const day = dayMap.get(dateKey)!

    day.temps.push(entry.data.instant.details.air_temperature)
    day.winds.push(entry.data.instant.details.wind_speed)

    const symbol =
      entry.data.next_1_hours?.summary.symbol_code ||
      entry.data.next_6_hours?.summary.symbol_code ||
      entry.data.next_12_hours?.summary.symbol_code

    if (symbol) day.symbols.push(symbol)

    const precip =
      entry.data.next_1_hours?.details.precipitation_amount ??
      entry.data.next_6_hours?.details.precipitation_amount ??
      0
    day.precip += precip
  }

  const days: DayForecast[] = []
  const now = new Date()
  const today = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}-${String(now.getDate()).padStart(2, '0')}`
  let count = 0

  for (const [dateKey, data] of dayMap) {
    if (count >= 7) break
    const dayName =
      dateKey === today
        ? 'Today'
        : data.date.toLocaleDateString(undefined, { weekday: 'short' })
    // Approximate a midday symbol: use the 4th entry if available, otherwise the last, or 'cloudy' if none.
    const symbolCode = data.symbols[Math.min(3, data.symbols.length - 1)] || 'cloudy'

    days.push({
      date: dateKey,
      dayName,
      symbolCode,
      tempMin: Math.round(Math.min(...data.temps)),
      tempMax: Math.round(Math.max(...data.temps)),
      precipitation: Math.round(data.precip * 10) / 10,
      windSpeed: Math.round(data.winds.reduce((a, b) => a + b, 0) / data.winds.length * 10) / 10,
    })
    count++
  }

  return days
}

const STORAGE_KEY = 'weather_location'

function getInitialLocation(): string {
  try {
    const stored = localStorage.getItem(STORAGE_KEY)
    if (stored && NORWEGIAN_CITIES.includes(stored)) {
      return stored
    }
    return 'Oslo'
  } catch {
    return 'Oslo'
  }
}

export default function Weather() {
  const { user, loading: authLoading } = useAuth()
  const [location, setLocation] = useState(getInitialLocation)
  const [prefsFetched, setPrefsFetched] = useState(false)
  const locationResolved = !authLoading && (!user || prefsFetched)
  const [refreshKey, setRefreshKey] = useState(0)
  const [intervalResetKey, setIntervalResetKey] = useState(0)
  // Track whether the user has manually picked a location during this session.
  const userHasSelected = useRef(false)
  // Keep a ref to the latest location so async callbacks can read it without stale closures.
  const locationRef = useRef(location)
  useEffect(() => {
    locationRef.current = location
  }, [location])
  const [{ forecast, loading, error, lastUpdated }, dispatch] = useReducer(fetchReducer, {
    loading: true,
    error: null,
    forecast: null,
    lastUpdated: null,
  })
  const [, setTick] = useState(0)
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null)

  // Load user's preferred location once auth settles.
  // Waits for auth to finish so we don't fetch forecast with the wrong location.
  useEffect(() => {
    if (authLoading) return

    if (!user) return

    let cancelled = false
    fetch('/api/settings/preferences', { credentials: 'include' })
      .then((r) => (r.ok ? r.json() : null))
      .then((data) => {
        if (cancelled) return
        if (userHasSelected.current) {
          // User interacted before prefs loaded; persist their choice server-side now that
          // auth has resolved, but do NOT overwrite what they selected.
          fetch('/api/settings/preferences', {
            method: 'PUT',
            credentials: 'include',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ preferences: { weather_location: locationRef.current } }),
          }).catch(() => {})
          return
        }
        const saved = data?.preferences?.weather_location || data?.preferences?.home_location
        if (saved && NORWEGIAN_CITIES.includes(saved)) {
          setLocation(saved)
        }
      })
      .catch(() => {
        // Intentional: preference load is best-effort; localStorage/Oslo fallback is fine.
      })
      .finally(() => {
        if (!cancelled) setPrefsFetched(true)
      })

    return () => {
      cancelled = true
    }
  }, [user, authLoading])

  const triggerRefresh = useCallback(() => {
    setRefreshKey((k) => k + 1)
    setIntervalResetKey((k) => k + 1)
  }, [])

  // Persist location selection whenever it changes.
  const saveLocation = useCallback(
    (city: string) => {
      // Only use localStorage for unauthenticated users to avoid cross-account leakage.
      if (!user) {
        try {
          localStorage.setItem(STORAGE_KEY, city)
        } catch {
          // localStorage may be unavailable; ignore.
        }
      }
      if (user) {
        fetch('/api/settings/preferences', {
          method: 'PUT',
          credentials: 'include',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ preferences: { weather_location: city } }),
        }).catch(() => {
          // Best-effort save; don't block the UI.
        })
      }
    },
    [user],
  )

  const handleLocationChange = useCallback(
    (city: string) => {
      userHasSelected.current = true
      setLocation(city)
      saveLocation(city)
    },
    [saveLocation],
  )

  // Fetch forecast once we know the correct location.
  useEffect(() => {
    if (!locationResolved) return

    let cancelled = false
    dispatch({ type: 'start' })

    fetch(`/api/weather/forecast?location=${encodeURIComponent(location)}`)
      .then((r) => {
        if (!r.ok) throw new Error('Failed to fetch forecast')
        return r.json()
      })
      .then((data) => {
        if (!cancelled) dispatch({ type: 'success', data })
      })
      .catch((err) => {
        if (!cancelled) dispatch({ type: 'error', message: err.message })
      })

    return () => {
      cancelled = true
    }
  }, [location, locationResolved, refreshKey])

  // Auto-refresh every 10 minutes, pausing when the tab is hidden.
  useEffect(() => {
    function startInterval() {
      stopInterval()
      intervalRef.current = setInterval(triggerRefresh, AUTO_REFRESH_MS)
    }

    function stopInterval() {
      if (intervalRef.current) {
        clearInterval(intervalRef.current)
        intervalRef.current = null
      }
    }

    function handleVisibilityChange() {
      if (document.hidden) {
        stopInterval()
      } else {
        triggerRefresh()
        startInterval()
      }
    }

    // Don't start the interval if the tab is already hidden on mount.
    if (!document.hidden) {
      startInterval()
    }
    document.addEventListener('visibilitychange', handleVisibilityChange)

    return () => {
      stopInterval()
      document.removeEventListener('visibilitychange', handleVisibilityChange)
    }
  }, [triggerRefresh, intervalResetKey, location])

  // Tick every 30 seconds to keep the "Updated X min ago" text current.
  useEffect(() => {
    const timer = setInterval(() => setTick(t => t + 1), 30_000)
    return () => clearInterval(timer)
  }, [])
  const timeAgo = lastUpdated ? formatTimeAgo(lastUpdated) : ''

  const timeseries = forecast?.properties?.timeseries ?? []
  const current = timeseries[0] as TimeseriesEntry | undefined
  const dailyForecasts = timeseries.length > 0 ? buildDailyForecasts(timeseries) : []

  const currentSymbol =
    current?.data.next_1_hours?.summary.symbol_code ||
    current?.data.next_6_hours?.summary.symbol_code ||
    'cloudy'

  return (
    <main className="max-w-3xl mx-auto px-4 py-8 min-h-screen">
      <div className="flex items-center justify-between mb-8">
        <h1 className="text-2xl font-bold">Weather</h1>
        <div className="flex items-center gap-2">
          <MapPin size={16} className="text-gray-400" />
          <select
            value={location}
            onChange={(e) => handleLocationChange(e.target.value)}
            className="bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:ring-2 focus:ring-blue-500"
            aria-label="Select location"
          >
            {NORWEGIAN_CITIES.map((city) => (
              <option key={city} value={city}>
                {city}
              </option>
            ))}
          </select>
          <button
            onClick={triggerRefresh}
            disabled={loading}
            className="p-2 rounded-lg bg-gray-700 border border-gray-600 text-gray-300 hover:text-white hover:bg-gray-600 focus:outline-none focus:ring-2 focus:ring-blue-500 disabled:opacity-50 disabled:cursor-not-allowed"
            aria-label="Refresh forecast"
          >
            <RefreshCw size={16} className={loading ? 'animate-spin' : ''} />
          </button>
        </div>
      </div>

      {loading && !forecast && (
        <div className="flex items-center justify-center py-20">
          <p className="text-gray-400">Loading forecast...</p>
        </div>
      )}

      {error && (
        <div className="bg-red-900/30 border border-red-800 rounded-xl p-4 mb-6">
          <p className="text-red-400 text-sm">{error}</p>
        </div>
      )}

      {current && (
        <>
          {/* Current Conditions */}
          <section className="bg-gray-800 rounded-xl p-6 mb-6">
            <div className="flex items-center justify-between">
              <div>
                <p className="text-sm text-gray-400 mb-1">Right now in {location}</p>
                <div className="flex items-end gap-3">
                  <span className="text-5xl font-bold">
                    {Math.round(current.data.instant.details.air_temperature)}°
                  </span>
                  <span className="text-lg text-gray-300 mb-1">
                    {getWeatherDescription(currentSymbol)}
                  </span>
                </div>
              </div>
              <div className="text-blue-400">
                {getWeatherIcon(currentSymbol, 56)}
              </div>
            </div>

            <div className="grid grid-cols-3 gap-4 mt-6 pt-4 border-t border-gray-700">
              <div className="flex items-center gap-2">
                <Wind size={16} className="text-gray-400" />
                <div>
                  <p className="text-xs text-gray-400">Wind</p>
                  <p className="text-sm font-medium">
                    {current.data.instant.details.wind_speed} m/s
                  </p>
                </div>
              </div>
              <div className="flex items-center gap-2">
                <Droplets size={16} className="text-gray-400" />
                <div>
                  <p className="text-xs text-gray-400">Humidity</p>
                  <p className="text-sm font-medium">
                    {Math.round(current.data.instant.details.relative_humidity)}%
                  </p>
                </div>
              </div>
              <div className="flex items-center gap-2">
                <Thermometer size={16} className="text-gray-400" />
                <div>
                  <p className="text-xs text-gray-400">Pressure</p>
                  <p className="text-sm font-medium">
                    {current.data.instant.details.air_pressure_at_sea_level
                      ? `${Math.round(current.data.instant.details.air_pressure_at_sea_level)} hPa`
                      : '—'}
                  </p>
                </div>
              </div>
            </div>
            {timeAgo && (
              <p className="text-xs text-gray-500 mt-4">{timeAgo}</p>
            )}
          </section>

          {/* Hourly Preview (next 12 hours) */}
          <section className="bg-gray-800 rounded-xl p-6 mb-6">
            <h2 className="text-lg font-semibold mb-4">Next 12 hours</h2>
            <div className="flex gap-4 overflow-x-auto pb-2">
              {timeseries.slice(0, 12).map((entry) => {
                const dt = new Date(entry.time)
                const hour = dt.toLocaleTimeString(undefined, {
                  hour: 'numeric',
                  hour12: false,
                })
                const sym =
                  entry.data.next_1_hours?.summary.symbol_code ||
                  entry.data.next_6_hours?.summary.symbol_code ||
                  'cloudy'
                return (
                  <div
                    key={entry.time}
                    className="flex flex-col items-center gap-1 min-w-[3.5rem]"
                  >
                    <span className="text-xs text-gray-400">{hour}</span>
                    <span className="text-blue-400">{getWeatherIcon(sym, 20)}</span>
                    <span className="text-sm font-medium">
                      {Math.round(entry.data.instant.details.air_temperature)}°
                    </span>
                    {entry.data.next_1_hours?.details.precipitation_amount ? (
                      <span className="text-xs text-blue-400">
                        {entry.data.next_1_hours.details.precipitation_amount} mm
                      </span>
                    ) : null}
                  </div>
                )
              })}
            </div>
          </section>

          {/* 7-Day Forecast */}
          <section className="bg-gray-800 rounded-xl p-6">
            <h2 className="text-lg font-semibold mb-4">7-day forecast</h2>
            <div className="space-y-3">
              {dailyForecasts.map((day) => (
                <div
                  key={day.date}
                  className="flex items-center justify-between bg-gray-700/50 rounded-lg px-4 py-3"
                >
                  <div className="flex items-center gap-3 w-24">
                    <span className="text-sm font-medium">{day.dayName}</span>
                  </div>
                  <div className="flex items-center gap-2 text-blue-400">
                    {getWeatherIcon(day.symbolCode, 20)}
                  </div>
                  <div className="flex items-center gap-1 w-16 justify-end">
                    <Droplets size={12} className="text-blue-400" />
                    <span className="text-xs text-gray-400">{day.precipitation} mm</span>
                  </div>
                  <div className="flex items-center gap-1 w-16 justify-end">
                    <Wind size={12} className="text-gray-400" />
                    <span className="text-xs text-gray-400">{day.windSpeed} m/s</span>
                  </div>
                  <div className="flex items-center gap-2 w-20 justify-end">
                    <span className="text-sm text-gray-400">{day.tempMin}°</span>
                    <span className="text-sm font-medium">{day.tempMax}°</span>
                  </div>
                </div>
              ))}
            </div>
          </section>

          {/* Attribution */}
          <p className="text-xs text-gray-500 mt-4 text-center">
            Weather data from{' '}
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
