import { useCallback, useEffect, useLayoutEffect, useReducer, useRef, useState } from 'react'
import { usePreferredLocation } from '../usePreferredLocation'
import type { RecentLocation } from '../recentLocations'
import type { TimeseriesEntry } from '../lib/weatherForecast'
import { readForecastCache, writeForecastCache } from '../lib/weatherCache'

export type { TimeseriesEntry }

export interface ForecastResponse {
  properties: { timeseries: TimeseriesEntry[] }
}

/** Auto-refresh cadence for long-lived weather surfaces. */
export const AUTO_REFRESH_MS = 10 * 60 * 1000 // 10 minutes

export interface ForecastState {
  loading: boolean
  /** True when the most recent request failed. */
  error: boolean
  /** Message from the most recent failure, for surfaces that render the reason. */
  errorMessage: string | null
  data: ForecastResponse | null
  /** When `data` was fetched (or cached), or null when nothing has loaded. */
  lastUpdated: Date | null
  /** Force a fresh fetch, bypassing any cached response. */
  refresh: () => void
}

type State = Omit<ForecastState, 'refresh'>
type Action =
  | { type: 'start' }
  | { type: 'done'; data: ForecastResponse; at: Date }
  | { type: 'error'; message: string }
  // Show a cached response while a fresh one loads (stale-while-revalidate).
  | { type: 'seed'; data: ForecastResponse; at: Date }
  // Clear displayed data so skeletons show (location with no cached forecast).
  | { type: 'reset' }

function reducer(state: State, action: Action): State {
  switch (action.type) {
    case 'start':
      return { ...state, loading: true, error: false, errorMessage: null }
    case 'done':
      return { loading: false, error: false, errorMessage: null, data: action.data, lastUpdated: action.at }
    case 'error':
      return { ...state, loading: false, error: true, errorMessage: action.message }
    case 'seed':
      return { loading: true, error: false, errorMessage: null, data: action.data, lastUpdated: action.at }
    case 'reset':
      return { loading: true, error: false, errorMessage: null, data: null, lastUpdated: null }
  }
}

interface CacheEntry {
  data: ForecastResponse
  at: Date
}

const cache = new Map<string, CacheEntry>()
const inflight = new Map<string, Promise<ForecastResponse>>()

export function cacheKey(lat: number, lon: number, name: string) {
  return `${lat},${lon},${name}`
}

export function clearForecastCache() {
  cache.clear()
  inflight.clear()
}

export interface UseForecastOptions {
  /**
   * Seed from and write to the per-user localStorage forecast cache instead of the
   * in-memory one, and always revalidate on mount. Suits a full page that must show
   * fresh data; the default in-memory cache suits short-lived widgets.
   */
  persist?: boolean
  /** User id scoping the persisted cache. Undefined uses the anonymous namespace. */
  userId?: number
  /** Refresh every {@link AUTO_REFRESH_MS}, pausing while the tab is hidden. */
  autoRefresh?: boolean
  /** Hold off fetching until the caller knows the location is final. */
  enabled?: boolean
}

function initState(
  { location, persist, userId }: { location: RecentLocation | null; persist: boolean; userId?: number },
): State {
  const empty: State = { loading: true, error: false, errorMessage: null, data: null, lastUpdated: null }
  if (!location) return empty

  if (persist) {
    // Seed from the per-location cache so a revisit renders real numbers on first
    // paint (no skeleton flash) while a fresh forecast loads in the background.
    const cached = readForecastCache<ForecastResponse>(location.lat, location.lon, userId)
    if (!cached) return empty
    return { ...empty, data: cached.response, lastUpdated: new Date(cached.lastUpdated) }
  }

  const cached = cache.get(cacheKey(location.lat, location.lon, location.name))
  if (!cached) return empty
  return { loading: false, error: false, errorMessage: null, data: cached.data, lastUpdated: cached.at }
}

function forecastUrl(location: RecentLocation): string {
  return `/api/weather/forecast?lat=${location.lat}&lon=${location.lon}&location=${encodeURIComponent(location.name)}`
}

/**
 * Request a forecast, reusing any request already in flight for the same location.
 *
 * The guard is shared by every caller regardless of `persist`, so a page and a
 * widget mounted on the same location issue one request between them. The request
 * is deliberately not tied to any single caller's lifetime — cancelling a consumer
 * must not abort a request other consumers are still awaiting.
 */
function fetchForecast(location: RecentLocation, key: string): Promise<ForecastResponse> {
  const shared = inflight.get(key)
  if (shared) return shared

  const promise = fetch(forecastUrl(location), { credentials: 'include' }).then((r) => {
    if (!r.ok) throw new Error('Failed to fetch forecast')
    return r.json() as Promise<ForecastResponse>
  })
  inflight.set(key, promise)

  // Clear only our own entry: refresh() may already have replaced it with a newer
  // request, and deleting that one would defeat the guard for its consumers.
  const clear = () => {
    if (inflight.get(key) === promise) inflight.delete(key)
  }
  promise.then(clear, clear)

  return promise
}

/**
 * Fetch the forecast for a location.
 *
 * Called with no arguments it resolves the location from {@link usePreferredLocation}
 * and reads through a shared in-memory cache, which is what the compact Today
 * widgets want. Pass an explicit location (or `null` while one is still being
 * resolved) plus options to opt into localStorage-backed stale-while-revalidate
 * and periodic auto-refresh.
 */
export function useForecast(
  location?: RecentLocation | null,
  options: UseForecastOptions = {},
): ForecastState {
  const { persist = false, userId, autoRefresh = false, enabled = true } = options
  // Whether the caller supplies its own location is fixed per call site, so gating
  // the preferred-location lookup on it never changes hook order.
  const usePreferred = location === undefined
  const preferred = usePreferredLocation(usePreferred)
  const active = usePreferred ? preferred : location

  const [refreshKey, setRefreshKey] = useState(0)
  const [state, dispatch] = useReducer(reducer, { location: active, persist, userId }, initState)
  const timerRef = useRef<ReturnType<typeof setInterval> | null>(null)

  const lat = active?.lat
  const lon = active?.lon
  const name = active?.name
  const key = active ? cacheKey(active.lat, active.lon, active.name) : null

  const stopInterval = useCallback(() => {
    if (timerRef.current !== null) {
      clearInterval(timerRef.current)
      timerRef.current = null
    }
  }, [])

  // Held in a ref so the auto-refresh effect never has to restart just because
  // `refresh` was re-created (which happens whenever the location changes).
  const refreshRef = useRef<() => void>(() => {})

  const startInterval = useCallback(() => {
    stopInterval()
    timerRef.current = setInterval(() => refreshRef.current(), AUTO_REFRESH_MS)
  }, [stopInterval])

  const refresh = useCallback(() => {
    if (key) {
      // Drop the in-memory entry and any shared in-flight request so the fetch
      // effect issues a genuinely fresh request instead of replaying the previous
      // one. Both maps are cleared whether or not this caller persists, since a
      // persisted caller can be sharing an in-flight request with a widget.
      // The localStorage copy is intentionally kept: it only seeds first paint
      // and is overwritten as soon as the new response lands.
      cache.delete(key)
      inflight.delete(key)
    }
    // Bumping the key re-runs the fetch effect below for every caller, persisted
    // or not — the effect never short-circuits on the persisted cache.
    setRefreshKey((k) => k + 1)
    // Restart the countdown so a manual refresh gets a full interval rather than
    // whatever remained of the previous one.
    if (timerRef.current !== null) startInterval()
  }, [key, startInterval])

  useEffect(() => {
    refreshRef.current = refresh
  }, [refresh])

  // Re-seed displayed data when the active location changes: show that location's
  // cached forecast instantly, or clear to skeletons when it was never viewed.
  // useLayoutEffect runs before paint, preventing a brief flash of the previous
  // location's forecast under the newly selected location name.
  useLayoutEffect(() => {
    if (!persist || lat === undefined || lon === undefined) return
    const cached = readForecastCache<ForecastResponse>(lat, lon, userId)
    if (cached) {
      dispatch({ type: 'seed', data: cached.response, at: new Date(cached.lastUpdated) })
    } else {
      dispatch({ type: 'reset' })
    }
  }, [persist, lat, lon, userId])

  useEffect(() => {
    if (!enabled || key === null || lat === undefined || lon === undefined || name === undefined) return

    // Only the in-memory cache can satisfy a request outright. The persisted cache
    // is a first-paint seed, never a substitute for revalidating, so a persisted
    // caller always reaches the network here — including on refresh().
    if (!persist) {
      const cached = cache.get(key)
      if (cached) {
        dispatch({ type: 'done', data: cached.data, at: cached.at })
        return
      }
    }

    let cancelled = false
    dispatch({ type: 'start' })

    fetchForecast({ lat, lon, name }, key)
      .then((data) => {
        const at = new Date()
        // Cache regardless of who is still listening — the response is already paid for.
        if (persist) {
          writeForecastCache(lat, lon, data, userId)
        } else {
          cache.set(key, { data, at })
        }
        if (!cancelled) {
          dispatch({ type: 'done', data, at })
        }
      })
      .catch((err: unknown) => {
        // Keeps whatever data is already on screen: a failed background refresh
        // must not blank out a forecast the user is looking at.
        if (!cancelled) {
          dispatch({
            type: 'error',
            message: err instanceof Error ? err.message : 'Failed to fetch forecast',
          })
        }
      })

    return () => {
      cancelled = true
    }
  }, [enabled, persist, userId, key, lat, lon, name, refreshKey])

  // Auto-refresh on a fixed cadence, pausing while the tab is hidden and
  // refreshing immediately when it becomes visible again.
  useEffect(() => {
    if (!autoRefresh) return

    function handleVisibilityChange() {
      if (document.hidden) {
        stopInterval()
      } else {
        refreshRef.current()
        startInterval()
      }
    }

    // Don't start the interval if the tab is already hidden on mount.
    if (!document.hidden) startInterval()
    document.addEventListener('visibilitychange', handleVisibilityChange)

    return () => {
      stopInterval()
      document.removeEventListener('visibilitychange', handleVisibilityChange)
    }
  }, [autoRefresh, startInterval, stopInterval])

  return { ...state, refresh }
}
