import { useState, useEffect, useMemo, Component } from 'react'
import type { ReactNode, ErrorInfo } from 'react'
import { useSearchParams } from 'react-router'
import { useTranslation } from 'react-i18next'
import KioskClock from '../components/kiosk/KioskClock'
import KioskBusDepartures from '../components/kiosk/KioskBusDepartures'
import KioskWeather from '../components/kiosk/KioskWeather'
import type { ForecastData } from '../components/kiosk/KioskWeather'
import KioskSunrise from '../components/kiosk/KioskSunrise'
import KioskStaleBadge from '../components/kiosk/KioskStaleBadge'
import KioskStatusScreen from '../components/kiosk/KioskStatusScreen'
import mockData from '../mocks/kioskData.json'
import { useWakeLock } from '../hooks/useWakeLock'
import { isNightMode, pixelShift } from '../components/kiosk/nightMode'
import type { SunTimes, DimConfig } from '../components/kiosk/nightMode'

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

interface KioskData {
  transit: StopDepartures[]
  outdoor?: OutdoorReadings | null
  indoor?: IndoorReadings | null
  wind?: WindReadings | null
  forecast?: ForecastData | null
  sun?: SunTimes | null
  dim?: DimConfig | null
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

// Outcome of the most recent fetch attempt. 'loading' covers the window before
// the first attempt resolves; 'rejected' means the kiosk token was refused
// (401/403); 'unreachable' covers network errors and every other non-2xx.
type FetchState = 'loading' | 'ok' | 'rejected' | 'unreachable'

function KioskPageInner() {
  const [searchParams] = useSearchParams()
  // useSuspense is off and every string carries an English default so an
  // unattended kiosk still renders these panels when the locale JSON cannot be
  // fetched — which is exactly the situation the 'unreachable' panel reports.
  const { t } = useTranslation('kiosk', { useSuspense: false })

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

  const [now, setNow] = useState<number>(() => Date.now())
  // Both the payload and the outcome are stored together with the token that
  // produced them, so a rescan (token change) implicitly resets them during
  // render. Keying matters for correctness, not just tidiness: kiosk tokens are
  // individually scoped (stop IDs, location, Netatmo user), so re-showing the
  // previous token's departures, forecast and indoor readings under a new token
  // would leak another screen's — and another household's — data. It also stops
  // a kiosk sitting on the rejected panel from telling the user to rescan the QR
  // code while the freshly scanned token's first request is still in flight.
  const [apiResult, setApiResult] = useState<
    { token: string | null; data: KioskData; at: number } | null
  >(null)
  const [fetchStatus, setFetchStatus] = useState<{ token: string | null; state: FetchState }>(
    () => ({ token, state: 'loading' })
  )
  const fetchState: FetchState = fetchStatus.token === token ? fetchStatus.state : 'loading'
  const activeResult = apiResult && apiResult.token === token ? apiResult : null
  // Anchored to the current token too, so the stale badge never reports the
  // previous token's success time.
  const lastSuccessAt = activeResult ? activeResult.at : null

  // Mock data is exclusive to the token-less demo path. A real kiosk must never
  // display fabricated departures or weather, so it stays null until the first
  // successful fetch under *this* token and renders a loading/error screen
  // instead.
  const data = useMemo<KioskData | null>(() => {
    if (token) return activeResult ? activeResult.data : null
    return relativizeMockData(mockData)
  }, [token, activeResult])

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
        // Send the token as a Bearer header rather than a query parameter so
        // it never lands in reverse-proxy access logs. KioskAuth reads
        // Authorization first and only falls back to ?token= (internal/kiosk/
        // auth.go), and /api/kiosk/data is the sole endpoint it guards, so no
        // other request depends on the query form. Bookmarked /kiosk?token=...
        // URLs keep working: that query parameter is consumed client-side by
        // this page (and persisted to localStorage), never sent to the API.
        const res = await fetch('/api/kiosk/data', {
          credentials: 'include',
          headers: { Authorization: 'Bearer ' + token! },
          signal: myController?.signal,
        })
        // Bail if unmounted or superseded by a newer fetch.
        if (cancelled || myRequestId !== requestId) return
        if (!res.ok) {
          setFetchStatus({
            token,
            state: res.status === 401 || res.status === 403 ? 'rejected' : 'unreachable',
          })
          failureCount += 1
          scheduleNext(backoffDelay())
          return
        }
        const json: KioskData = await res.json()
        if (cancelled || myRequestId !== requestId) return
        setApiResult({ token, data: json, at: Date.now() })
        setFetchStatus({ token, state: 'ok' })
        failureCount = 0
        scheduleNext(POLL_INTERVAL_MS)
      } catch {
        // Network failure or abort. If the abort came from unmount/supersede,
        // skip; otherwise treat as a failure and back off.
        if (cancelled || myRequestId !== requestId) return
        setFetchStatus({ token, state: 'unreachable' })
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

  // The kiosk's single clock tick. It drives two things: the "Updated X ago"
  // badge, and the minute-derived night mode / pixel-shift offset. Both read
  // `now`, so one interval is enough — neither feature adds a timer of its own.
  //
  // The effect stays gated on a token and a first successful fetch: a healthy
  // kiosk that has never fetched successfully produces zero ticks. Nothing is
  // lost by that — without data the page renders a full-screen status panel,
  // which uses neither the badge nor the dimmed palette, and the token-less
  // demo path is a preview rather than a wall display.
  //
  // The tick fires every 5 s but deliberately skips the state update (and
  // therefore the re-render) when nothing observable has changed: low-power
  // wall-mounted devices should not repaint once per tick all day. A re-render
  // happens when either
  //   * the local minute rolls over — night mode and the pixel-shift offset
  //     are both pure functions of minutes-of-day, so they are observed up to
  //     one tick (5 s) after the boundary they belong to, which is invisible
  //     for a palette swap and a 3px nudge, or
  //   * we are within one tick of the stale threshold or already past it, so
  //     the badge age stays accurate.
  //
  // The effect re-runs on each successful fetch (lastSuccessAt changes), so
  // the staleness half is always anchored to the most recent success and a
  // recovery fetch automatically resets it.
  useEffect(() => {
    if (!token || lastSuccessAt === null) return
    const id = setInterval(() => {
      const t = Date.now()
      setNow((prev) => {
        if (Math.floor(t / 60_000) !== Math.floor(prev / 60_000)) return t
        if (t - lastSuccessAt >= STALE_THRESHOLD_MS - STALE_TICK_INTERVAL_MS) return t
        return prev
      })
    }, STALE_TICK_INTERVAL_MS)
    return () => clearInterval(id)
  }, [token, lastSuccessAt])

  const isStale =
    !!token &&
    lastSuccessAt !== null &&
    now - lastSuccessAt > STALE_THRESHOLD_MS

  // Night mode and the burn-in pixel shift both ride the tick above: `now`
  // only changes when the minute rolls over (or the badge needs refreshing),
  // so these memos recompute at most once a minute.
  // Depend on the sun/dim slices rather than `data`: every successful poll
  // hands back a fresh object identity, so keying on `data` would recompute on
  // each fetch even though night mode only reads these two fields.
  const sun = data?.sun
  const dim = data?.dim
  const dimmed = useMemo(() => isNightMode(new Date(now), sun, dim), [now, sun, dim])
  const shift = useMemo(() => pixelShift(new Date(now)), [now])

  // A rejected token means the screen is deauthorised, so it takes precedence
  // over any data we may already hold: a wall-mounted kiosk whose token is
  // revoked would otherwise keep rendering its last payload forever behind
  // nothing but the small stale badge, and never tell anyone to rescan.
  // (The token is deliberately left in localStorage — a transient 401 should
  // not demote the kiosk to the token-less demo layout on the next reload.)
  if (token && fetchState === 'rejected') {
    return (
      <KioskStatusScreen
        tone="rejected"
        title={t('state.rejected.title', { defaultValue: 'Kiosk token rejected' })}
        body={t('state.rejected.body', {
          defaultValue:
            'This screen is no longer authorised. Rescan the QR code from Settings to set it up again.',
        })}
      />
    )
  }

  // Beyond that, `data` is only null on the tokened path before the first
  // successful fetch, so these screens never replace the demo layout. Polling
  // continues underneath, so a later success swaps the panel for live data; and
  // once data has arrived a non-auth failure falls through to the normal
  // layout, where the stale badge takes over.
  if (!data) {
    if (fetchState === 'unreachable') {
      return (
        <KioskStatusScreen
          tone="unreachable"
          title={t('state.unreachable.title', { defaultValue: 'Cannot reach the server' })}
          body={t('state.unreachable.body', {
            defaultValue:
              'The kiosk keeps retrying automatically. Check the network connection.',
          })}
        />
      )
    }
    return (
      <KioskStatusScreen
        tone="loading"
        title={t('state.loading.title', { defaultValue: 'Connecting…' })}
        body={t('state.loading.body', {
          defaultValue: 'Waiting for the first update from the server.',
        })}
      />
    )
  }

  const dividerClass = `h-px mx-4 ${dimmed ? 'bg-gray-900' : 'bg-gray-800'}`

  return (
    // Outer frame: clips the few pixels the shifted layout pokes outside the
    // viewport, so the burn-in offset can never add a scrollbar. It grows with
    // the inner element, so content taller than the screen still scrolls the
    // page exactly as it did before the shift was introduced.
    <div
      data-testid="kiosk-root"
      data-dimmed={dimmed ? 'true' : 'false'}
      className={`min-h-screen w-full overflow-hidden ${dimmed ? 'bg-black' : 'bg-gray-950'}`}
    >
      <div
        data-testid="kiosk-shift"
        // A transform, not padding: it does not reflow the layout, so the
        // panels keep their exact sizes. The offset is positive on both axes,
        // so the outer overflow-hidden clips up to PIXEL_SHIFT_MAX_PX (3px)
        // at the right edge and eats into the pb-16 gutter at the bottom —
        // both are empty margin, and in exchange the shift can never add a
        // scrollbar.
        style={{ transform: `translate(${shift.x}px, ${shift.y}px)` }}
        className={`min-h-screen flex flex-col overflow-hidden pb-16 ${
          dimmed ? 'text-gray-500' : 'text-white'
        }`}
      >
        {/* Clock & Date */}
        <KioskClock dimmed={dimmed} />

        {/* Divider */}
        <div className={dividerClass} />

        {/* Bus Departures — scrollable but not greedy */}
        <div className="overflow-y-auto py-2" style={{ maxHeight: '45vh' }}>
          <KioskBusDepartures stops={data.transit} dimmed={dimmed} />
        </div>

        {/* Divider */}
        <div className={dividerClass} />

        {/* Weather strip */}
        <KioskWeather
          outdoor={data.outdoor ?? null}
          indoor={data.indoor ?? null}
          wind={data.wind ?? null}
          forecast={data.forecast ?? null}
          dimmed={dimmed}
        />

        {/* Divider */}
        <div className={dividerClass} />

        {/* Sunrise / Sunset */}
        <KioskSunrise sun={data.sun ?? null} dimmed={dimmed} />
      </div>

      {/* Outside the shifted wrapper on purpose: a non-none transform makes an
          element the containing block for its position:fixed descendants, so
          the badge would be anchored to (and clipped with) the shifted layout
          instead of staying pinned to the viewport. */}
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
