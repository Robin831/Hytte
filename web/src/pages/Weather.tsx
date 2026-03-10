import { useState, useEffect, useReducer, useCallback, useRef } from 'react'
import { useAuth } from '../auth'
import {
  type RecentLocation,
  loadRecentLocations,
  saveRecentLocations,
  addRecentLocation,
  parseRecentLocationsPreference,
  buildDefaultLocations,
} from '../recentLocations'
import {
  ArrowUp,
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

/** Resolve a location name, checking recents first then the fetched known locations. */
function resolveLocation(
  name: string,
  recents: RecentLocation[],
  knownLocations?: RecentLocation[],
): RecentLocation | undefined {
  const fromRecents = recents.find((l) => l.name === name)
  if (fromRecents) return fromRecents
  return knownLocations?.find((l) => l.name === name)
}

function getInitialState(): { location: RecentLocation | null; recents: RecentLocation[] } {
  const recents = loadRecentLocations()
  if (!recents) {
    // First visit — no localStorage data. Coordinates will come from the API.
    return { location: null, recents: [] }
  }
  // Try to restore the last selected location name from localStorage.
  try {
    const storedName = localStorage.getItem('weather_location')
    if (storedName) {
      const loc = resolveLocation(storedName, recents)
      if (loc) return { location: loc, recents }
    }
  } catch {
    // localStorage may be unavailable.
  }
  return { location: recents[0] ?? null, recents }
}

/** Build the forecast API URL for a location, always using lat/lon. */
function forecastUrl(loc: RecentLocation): string {
  return `/api/weather/forecast?lat=${loc.lat}&lon=${loc.lon}&location=${encodeURIComponent(loc.name)}`
}


type HourlyRange = 12 | 24 | 48

export default function Weather() {
  const { user, loading: authLoading } = useAuth()
  const [initialState] = useState(getInitialState)
  const [selectedLocation, setSelectedLocation] = useState<RecentLocation | null>(initialState.location)
  const [hourlyRange, setHourlyRange] = useState<HourlyRange>(12)
  const [recentLocations, setRecentLocations] = useState<RecentLocation[]>(initialState.recents)
  const [knownLocations, setKnownLocations] = useState<RecentLocation[]>([])
  const [locationsLoaded, setLocationsLoaded] = useState(false)
  const [prefsFetched, setPrefsFetched] = useState(false)
  const locationResolved =
    selectedLocation !== null && !authLoading && (!user || prefsFetched) && (recentLocations.length > 0 || locationsLoaded)
  const [refreshKey, setRefreshKey] = useState(0)
  const [intervalResetKey, setIntervalResetKey] = useState(0)
  const userHasSelected = useRef(false)
  const selectedLocationRef = useRef(selectedLocation)
  const recentLocationsRef = useRef(recentLocations)
  const knownLocationsRef = useRef(knownLocations)
  useEffect(() => {
    selectedLocationRef.current = selectedLocation
  }, [selectedLocation])
  useEffect(() => {
    recentLocationsRef.current = recentLocations
  }, [recentLocations])
  useEffect(() => {
    knownLocationsRef.current = knownLocations
  }, [knownLocations])

  // Persist recent locations to localStorage — only for unauthenticated users after auth settles.
  // Authenticated users store recents server-side only to prevent cross-account leakage.
  useEffect(() => {
    if (authLoading || user) return
    saveRecentLocations(recentLocations)
  }, [recentLocations, user, authLoading])

  // Fetch available locations from the backend (single source of truth for coordinates).
  useEffect(() => {
    let cancelled = false
    fetch('/api/weather/locations')
      .then((r) => {
        if (!r.ok) throw new Error('Failed to fetch locations')
        return r.json()
      })
      .then((data) => {
        if (cancelled) return
        const locs = (data.locations ?? []) as RecentLocation[]
        locs.sort((a, b) => a.name.localeCompare(b.name))
        setKnownLocations(locs)
        // Reconcile recent locations with canonical coordinates from API.
        const locMap = new Map(locs.map((l) => [l.name, l]))
        setRecentLocations((prev) => {
          if (prev.length === 0) {
            // First visit — build defaults from API data (no hardcoded coordinates).
            return buildDefaultLocations(locs)
          }
          // Reconcile stored locations with canonical coordinates from the API.
          return prev.map((r) => locMap.get(r.name) ?? r)
        })
        // Set initial selected location if not yet set (first visit).
        setSelectedLocation((prev) => {
          if (prev !== null) return prev
          const defaults = buildDefaultLocations(locs)
          return defaults[0] ?? locs[0] ?? null
        })
      })
      .catch((err) => {
        // Best-effort: dropdown will still show recent locations from localStorage.
        console.warn('Failed to fetch locations:', err)
      })
      .finally(() => {
        if (!cancelled) setLocationsLoaded(true)
      })
    return () => {
      cancelled = true
    }
  }, [])
  const [{ forecast, loading, error, lastUpdated }, dispatch] = useReducer(fetchReducer, {
    loading: true,
    error: null,
    forecast: null,
    lastUpdated: null,
  })
  const [, setTick] = useState(0)
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null)

  // Load user's preferred location and recents once auth settles.
  useEffect(() => {
    if (authLoading) return

    if (!user) return

    let cancelled = false
    fetch('/api/settings/preferences', { credentials: 'include' })
      .then((r) => (r.ok ? r.json() : null))
      .then((data) => {
        if (cancelled) return

        // Sync recent locations from backend.
        const serverRecentsRaw = data?.preferences?.recent_locations
        if (serverRecentsRaw) {
          const serverRecents = parseRecentLocationsPreference(serverRecentsRaw)
          if (serverRecents && serverRecents.length > 0) {
            setRecentLocations(serverRecents)
            // Do NOT write to localStorage here — authenticated users store recents server-side only.
          }
        }

        if (userHasSelected.current) {
          // User interacted before prefs loaded; persist their choice server-side.
          const currentRecents = recentLocationsRef.current
          fetch('/api/settings/preferences', {
            method: 'PUT',
            credentials: 'include',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
              preferences: {
                weather_location: selectedLocationRef.current?.name ?? '',
                recent_locations: JSON.stringify(currentRecents),
              },
            }),
          }).catch((err: unknown) => {
            console.warn('Failed to push user selection to server:', err)
          })
          return
        }

        const savedName = data?.preferences?.weather_location || data?.preferences?.home_location
        if (savedName) {
          // Resolve from server recents first, then known cities from API.
          const serverRecents = serverRecentsRaw
            ? parseRecentLocationsPreference(serverRecentsRaw)
            : null
          const loc = resolveLocation(
            savedName,
            serverRecents ?? recentLocationsRef.current,
            knownLocationsRef.current,
          )
          if (loc) {
            setSelectedLocation(loc)
          }
        }
      })
      .catch((err: unknown) => {
        // Preference load is best-effort; localStorage values are used as fallback.
        console.warn('Failed to fetch preferences:', err)
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

  // Persist location and recents whenever they change.
  const saveState = useCallback(
    (loc: RecentLocation, updatedRecents: RecentLocation[]) => {
      if (!user) {
        try {
          localStorage.setItem('weather_location', loc.name)
        } catch {
          // localStorage may be unavailable.
        }
        saveRecentLocations(updatedRecents)
      }
      if (user) {
        fetch('/api/settings/preferences', {
          method: 'PUT',
          credentials: 'include',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            preferences: {
              weather_location: loc.name,
              recent_locations: JSON.stringify(updatedRecents),
            },
          }),
        }).catch((err: unknown) => {
          // Best-effort save; localStorage is used as local fallback.
          console.warn('Failed to save preferences:', err)
        })
      }
    },
    [user],
  )

  // Handles selection from the quick-access dropdown (named cities only).
  const handleLocationChange = useCallback(
    (cityName: string) => {
      userHasSelected.current = true
      const loc = resolveLocation(cityName, recentLocationsRef.current, knownLocationsRef.current)
      if (!loc) return
      setSelectedLocation(loc)
      const updatedRecents = addRecentLocation(recentLocationsRef.current, loc)
      setRecentLocations(updatedRecents)
      saveState(loc, updatedRecents)
    },
    [saveState],
  )

  // Fetch forecast once we know the correct location.
  useEffect(() => {
    if (!locationResolved || !selectedLocation) return

    let cancelled = false
    dispatch({ type: 'start' })

    fetch(forecastUrl(selectedLocation))
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
  }, [selectedLocation, locationResolved, refreshKey])

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
  }, [triggerRefresh, intervalResetKey, selectedLocation])

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

  // Build dropdown options: recent locations, then remaining known cities not in recents.
  // Always include selectedLocation to prevent empty/mismatched dropdown during loading.
  const displayRecents =
    selectedLocation && !recentLocations.some((l) => l.name === selectedLocation.name)
      ? [selectedLocation, ...recentLocations]
      : recentLocations
  const recentNames = new Set(displayRecents.map((l) => l.name))
  const otherCities = knownLocations.filter((l) => !recentNames.has(l.name))

  return (
    <main className="max-w-3xl mx-auto px-4 py-8 min-h-screen">
      <div className="flex items-center justify-between mb-8 flex-wrap gap-3">
        <h1 className="text-2xl font-bold">Weather</h1>
        <div className="flex items-center gap-2 flex-wrap">
          <MapPin size={16} className="text-gray-400" />
          <select
            value={selectedLocation?.name ?? ''}
            onChange={(e) => handleLocationChange(e.target.value)}
            className="bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:ring-2 focus:ring-blue-500"
            aria-label="Select location"
          >
            {displayRecents.length > 0 && (
              <optgroup label="Recent">
                {displayRecents.map((loc) => (
                  <option key={`recent-${loc.name}`} value={loc.name}>
                    {loc.name}
                  </option>
                ))}
              </optgroup>
            )}
            {otherCities.length > 0 && (
              <optgroup label="All cities">
                {otherCities.map((loc) => (
                  <option key={`all-${loc.name}`} value={loc.name}>
                    {loc.name}
                  </option>
                ))}
              </optgroup>
            )}
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
                <p className="text-sm text-gray-400 mb-1">Right now in {selectedLocation?.name}</p>
                <div className="flex items-end gap-3">
                  <span className="text-5xl font-bold">
                    {Math.round(current.data.instant.details.air_temperature)}°
                  </span>
                  {(() => {
                    const feelsLike = calculateFeelsLike(
                      current.data.instant.details.air_temperature,
                      current.data.instant.details.wind_speed,
                      current.data.instant.details.relative_humidity,
                    )
                    return feelsLike !== null ? (
                      <span className="text-sm text-gray-400 mb-2">
                        Feels like {feelsLike}°
                      </span>
                    ) : null
                  })()}
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
                  <p className="text-sm font-medium flex items-center gap-1">
                    {current.data.instant.details.wind_speed} m/s
                    {current.data.instant.details.wind_from_direction !== undefined && (
                      <ArrowUp
                        size={14}
                        className="text-gray-400 shrink-0"
                        style={{ transform: `rotate(${windArrowRotation(current.data.instant.details.wind_from_direction)}deg)` }}
                        aria-label={`Wind from ${Math.round(current.data.instant.details.wind_from_direction)}°`}
                      />
                    )}
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

          {/* Hourly Preview */}
          <section className="bg-gray-800 rounded-xl p-6 mb-6">
            <div className="flex items-center justify-between mb-4">
              <h2 className="text-lg font-semibold">Next {hourlyRange} hours</h2>
              <div className="flex rounded-lg overflow-hidden border border-gray-600 text-xs">
                {([12, 24, 48] as HourlyRange[]).map((range) => (
                  <button
                    key={range}
                    onClick={() => setHourlyRange(range)}
                    className={`px-3 py-1 transition-colors ${
                      hourlyRange === range
                        ? 'bg-blue-600 text-white'
                        : 'bg-gray-700 text-gray-300 hover:bg-gray-600'
                    }`}
                    aria-label={`Show next ${range} hours`}
                    aria-pressed={hourlyRange === range}
                  >
                    {range}h
                  </button>
                ))}
              </div>
            </div>
            <div className="flex gap-4 overflow-x-auto pb-2">
              {timeseries.slice(0, hourlyRange).map((entry, index) => {
                const dt = new Date(entry.time)
                const hour = dt.toLocaleTimeString(undefined, {
                  hour: 'numeric',
                  hour12: false,
                })
                const sym =
                  entry.data.next_1_hours?.summary.symbol_code ||
                  entry.data.next_6_hours?.summary.symbol_code ||
                  'cloudy'
                // Show date separator when crossing midnight (hour 0) after the first entry
                const showDateSep = index > 0 && dt.getHours() === 0
                const dateLabel = dt.toLocaleDateString(undefined, { weekday: 'short', month: 'short', day: 'numeric' })
                return (
                  <div key={entry.time} className="flex items-start gap-4">
                    {showDateSep && (
                      <div className="flex flex-col items-center self-stretch">
                        <div className="w-px bg-gray-600 flex-1" />
                        <span className="text-xs text-gray-400 whitespace-nowrap rotate-0 py-1 px-1 bg-gray-700 rounded text-center leading-tight">
                          {dateLabel}
                        </span>
                        <div className="w-px bg-gray-600 flex-1" />
                      </div>
                    )}
                    <div className="flex flex-col items-center gap-1 min-w-[3.5rem]">
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
