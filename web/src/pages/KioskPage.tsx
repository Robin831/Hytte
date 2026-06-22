import { useState, useEffect, useMemo, Component } from 'react'
import type { ReactNode, ErrorInfo } from 'react'
import { useSearchParams } from 'react-router-dom'
import KioskClock from '../components/kiosk/KioskClock'
import KioskBusDepartures from '../components/kiosk/KioskBusDepartures'
import KioskWeather from '../components/kiosk/KioskWeather'
import type { ForecastData } from '../components/kiosk/KioskWeather'
import KioskSunrise from '../components/kiosk/KioskSunrise'
import KioskStaleBadge from '../components/kiosk/KioskStaleBadge'
import mockData from '../mocks/kioskData.json'
import { useWakeLock } from '../hooks/useWakeLock'

// Error boundary so that JS errors show a visible message instead of a blank
// white page. This is especially important on older browsers (Android 5 /
// Firefox ESR) where a single unhandled exception would otherwise leave the
// screen completely empty.
interface ErrorBoundaryState {
  error: Error | null
}
class KioskErrorBoundary extends Component<{ children: ReactNode }, ErrorBoundaryState> {
  constructor(props: { children: ReactNode }) {
    super(props)
    this.state = { error: null }
  }

  static getDerivedStateFromError(error: Error): ErrorBoundaryState {
    return { error }
  }

  componentDidCatch(error: Error, errorInfo: ErrorInfo) {
    // Log the error and component stack to aid diagnosing kiosk-only failures
    console.error('KioskErrorBoundary caught an error:', error, errorInfo.componentStack)
  }

  render() {
    if (this.state.error) {
      return (
        <div
          role="alert"
          aria-live="assertive"
          style={{
            background: '#000',
            color: '#f87171',
            minHeight: '100vh',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            fontFamily: 'monospace',
            padding: '2rem',
            textAlign: 'center',
          }}
        >
          <div>
            <div style={{ fontSize: '1.5rem', marginBottom: '1rem' }}>Kiosk failed to load</div>
            <div style={{ fontSize: '1rem', opacity: 0.7 }}>
              {this.state.error.message || 'An unexpected error occurred.'}
            </div>
          </div>
        </div>
      )
    }
    return this.props.children
  }
}

interface Departure {
  line: string
  destination: string
  departure_time: string
  is_realtime: boolean
  delay_minutes: number
  platform?: string
}

interface StopDepartures {
  stop_id: string
  stop_name: string
  departures: Departure[]
}

interface OutdoorReadings {
  Temperature: number
  Humidity: number
}

interface IndoorReadings {
  Temperature: number
  Humidity: number
  CO2: number
  Noise: number
  Pressure: number
}

interface WindReadings {
  Speed: number
  Gust: number
  Direction: number
}

interface SunTimes {
  kind: string
  sunrise?: string
  sunset?: string
}

interface KioskData {
  transit: StopDepartures[]
  outdoor?: OutdoorReadings | null
  indoor?: IndoorReadings | null
  wind?: WindReadings | null
  forecast?: ForecastData | null
  sun?: SunTimes | null
  fetched_at: string
}

const POLL_INTERVAL_MS = 30_000
const STALE_THRESHOLD_MS = 2 * POLL_INTERVAL_MS
const STALE_TICK_INTERVAL_MS = 5_000

// Backoff schedule applied to consecutive fetch failures. Index 0 is the delay
// after the first failure, index 1 after the second, etc. The final entry is
// used for any further consecutive failures (cap). A successful fetch resets
// the failure count, so the next poll fires at POLL_INTERVAL_MS again.
const BACKOFF_SCHEDULE_MS = [30_000, 60_000, 120_000, 300_000]

// Offset mock departure times so they appear relative to the current time,
// preventing all departures from showing as "now/0 min" once the static
// fixture timestamps are in the past.
function relativizeMockData(mock: typeof mockData): KioskData {
  const offset = Date.now() - new Date(mock.fetched_at).getTime()
  return {
    ...mock,
    transit: mock.transit.map((stop) => ({
      ...stop,
      departures: stop.departures.map((dep) => ({
        ...dep,
        departure_time: new Date(
          new Date(dep.departure_time).getTime() + offset
        ).toISOString(),
      })),
    })),
  } as KioskData
}

const KIOSK_TOKEN_KEY = 'hytte_kiosk_token'

function KioskPageInner() {
  const [searchParams] = useSearchParams()

  // Keep the screen awake while the kiosk is displayed (re-acquires on
  // visibility change; no-ops on browsers without the Wake Lock API).
  useWakeLock()

  // Override the PWA manifest so "Add to Home Screen" uses /kiosk as start_url
  useEffect(() => {
    const link = document.querySelector('link[rel="manifest"]')
    if (link) link.setAttribute('href', '/kiosk-manifest.json')
    return () => { if (link) link.setAttribute('href', '/manifest.json') }
  }, [])

  const urlToken = searchParams.get('token')

  // Persist URL token to localStorage in an effect so the kiosk works after
  // "Add to Home Screen" (which strips query params). Doing this in a render
  // body would write to storage on every re-render; an effect only runs when
  // the URL token actually changes.
  useEffect(() => {
    if (urlToken) {
      try { localStorage.setItem(KIOSK_TOKEN_KEY, urlToken) } catch { /* ignore */ }
    }
  }, [urlToken])

  // Read the stored token once at mount via the state initializer. URL param
  // takes precedence so a fresh ?token=... URL always wins over the stored one.
  const [storedToken] = useState<string | null>(() => {
    try { return localStorage.getItem(KIOSK_TOKEN_KEY) } catch { return null }
  })
  const token = urlToken ?? storedToken

  const [apiData, setApiData] = useState<KioskData | null>(null)
  const [lastSuccessAt, setLastSuccessAt] = useState<number | null>(null)
  const [now, setNow] = useState<number>(() => Date.now())

  // When no token is present, display relativized mock data; otherwise show API data (or mock while loading)
  const data = useMemo<KioskData>(() => {
    if (token && apiData) return apiData
    return relativizeMockData(mockData)
  }, [token, apiData])

  useEffect(() => {
    if (!token) {
      return
    }

    // Visibility-aware polling with exponential backoff on failure. We avoid
    // setInterval so each scheduling decision can react to the current state
    // (visibility, failure count) instead of firing on a fixed cadence.
    let cancelled = false
    let failureCount = 0
    let timerId: ReturnType<typeof setTimeout> | null = null
    let activeController: AbortController | null = null
    // Monotonically increasing request ID. Guards against a stale response
    // winning a race and calling setApiData after a newer fetch has started,
    // independently of whether AbortController is available (when it is not,
    // both myController and activeController would both be null and the
    // controller-identity check would never fire).
    let requestId = 0

    // Older browsers (Android 5 / Firefox ESR) may not implement the Page
    // Visibility API. Feature-detect and skip the listener if absent — polling
    // then runs unconditionally, as before.
    const supportsVisibility =
      typeof document !== 'undefined' && typeof document.visibilityState !== 'undefined'

    function clearTimer() {
      if (timerId !== null) {
        clearTimeout(timerId)
        timerId = null
      }
    }

    function scheduleNext(delay: number) {
      if (cancelled) return
      // Don't schedule new fetches while the tab is hidden; the visibility
      // listener will trigger an immediate fetch when the tab returns.
      if (supportsVisibility && document.visibilityState === 'hidden') return
      clearTimer()
      timerId = setTimeout(() => {
        timerId = null
        fetchData()
      }, delay)
    }

    function backoffDelay() {
      const idx = Math.min(failureCount - 1, BACKOFF_SCHEDULE_MS.length - 1)
      return BACKOFF_SCHEDULE_MS[idx]
    }

    async function fetchData() {
      if (cancelled) return

      // Abort any prior in-flight fetch (e.g. when visibility returns mid-poll
      // we don't want two requests racing).
      if (activeController) {
        try { activeController.abort() } catch { /* ignore */ }
      }
      const myController =
        typeof AbortController !== 'undefined' ? new AbortController() : null
      activeController = myController

      // Claim a unique ID for this invocation. This is the primary guard
      // against a stale response winning a race — it works even when
      // AbortController is unavailable (where both myController and
      // activeController would be null, making the identity check useless).
      requestId += 1
      const myRequestId = requestId

      try {
        const res = await fetch('/api/kiosk/data?token=' + encodeURIComponent(token!), {
          credentials: 'include',
          signal: myController?.signal,
        })
        // Bail if unmounted or superseded by a newer fetch.
        if (cancelled || myRequestId !== requestId) return
        if (!res.ok) {
          failureCount += 1
          scheduleNext(backoffDelay())
          return
        }
        const json: KioskData = await res.json()
        if (cancelled || myRequestId !== requestId) return
        setApiData(json)
        setLastSuccessAt(Date.now())
        failureCount = 0
        scheduleNext(POLL_INTERVAL_MS)
      } catch {
        // Network failure or abort. If the abort came from unmount/supersede,
        // skip; otherwise treat as a failure and back off.
        if (cancelled || myRequestId !== requestId) return
        failureCount += 1
        scheduleNext(backoffDelay())
      }
    }

    function handleVisibilityChange() {
      if (cancelled) return
      if (document.visibilityState === 'hidden') {
        clearTimer()
      } else if (document.visibilityState === 'visible') {
        clearTimer()
        fetchData()
      }
    }

    if (supportsVisibility) {
      document.addEventListener('visibilitychange', handleVisibilityChange)
    }

    fetchData()

    return () => {
      cancelled = true
      clearTimer()
      if (activeController) {
        try { activeController.abort() } catch { /* ignore */ }
      }
      if (supportsVisibility) {
        document.removeEventListener('visibilitychange', handleVisibilityChange)
      }
    }
  }, [token])

  // Drive the "Updated X ago" clock.
  //
  // The effect is gated on lastSuccessAt being non-null so the interval
  // never starts before we have received a first data payload — a healthy
  // kiosk that has never fetched successfully produces zero ticks.
  //
  // While data *is* fresh the interval fires every 5 s but skips the
  // setNow call (and therefore skips the re-render) when we are still well
  // below the stale threshold.  Only as we approach or cross the threshold
  // does the state update fire, keeping the badge age display accurate
  // without wasting renders on low-power wall-mounted devices during normal
  // operation.
  //
  // The effect re-runs on each successful fetch (lastSuccessAt changes), so
  // the timer is always anchored to the most recent success and a recovery
  // fetch automatically resets the clock.
  useEffect(() => {
    if (!token || lastSuccessAt === null) return
    const id = setInterval(() => {
      const t = Date.now()
      // Only trigger a re-render when we are within one tick of the stale
      // threshold or already past it; during the clearly-fresh window the
      // DOM does not need updating.
      if (t - lastSuccessAt >= STALE_THRESHOLD_MS - STALE_TICK_INTERVAL_MS) {
        setNow(t)
      }
    }, STALE_TICK_INTERVAL_MS)
    return () => clearInterval(id)
  }, [token, lastSuccessAt])

  const isStale =
    !!token &&
    lastSuccessAt !== null &&
    now - lastSuccessAt > STALE_THRESHOLD_MS

  return (
    <div className="min-h-screen bg-gray-950 text-white flex flex-col overflow-hidden pb-16">
      {/* Clock & Date */}
      <KioskClock />

      {/* Divider */}
      <div className="h-px bg-gray-800 mx-4" />

      {/* Bus Departures — scrollable but not greedy */}
      <div className="overflow-y-auto py-2" style={{ maxHeight: '45vh' }}>
        <KioskBusDepartures stops={data.transit} />
      </div>

      {/* Divider */}
      <div className="h-px bg-gray-800 mx-4" />

      {/* Weather strip */}
      <KioskWeather
        outdoor={data.outdoor ?? null}
        indoor={data.indoor ?? null}
        wind={data.wind ?? null}
        forecast={data.forecast ?? null}
      />

      {/* Divider */}
      <div className="h-px bg-gray-800 mx-4" />

      {/* Sunrise / Sunset */}
      <KioskSunrise sun={data.sun ?? null} />

      <KioskStaleBadge isStale={isStale} lastSuccessAt={lastSuccessAt} now={now} />
    </div>
  )
}

export default function KioskPage() {
  return (
    <KioskErrorBoundary>
      <KioskPageInner />
    </KioskErrorBoundary>
  )
}
