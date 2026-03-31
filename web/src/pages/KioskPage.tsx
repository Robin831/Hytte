import { useState, useEffect, useMemo, Component } from 'react'
import type { ReactNode, ErrorInfo } from 'react'
import { useSearchParams } from 'react-router-dom'
import KioskClock from '../components/kiosk/KioskClock'
import KioskBusDepartures from '../components/kiosk/KioskBusDepartures'
import KioskWeather from '../components/kiosk/KioskWeather'
import type { ForecastData } from '../components/kiosk/KioskWeather'
import KioskSunrise from '../components/kiosk/KioskSunrise'
import mockData from '../mocks/kioskData.json'

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

interface SunTimes {
  kind: string
  sunrise?: string
  sunset?: string
}

interface KioskData {
  transit: StopDepartures[]
  outdoor?: OutdoorReadings | null
  forecast?: ForecastData | null
  sun?: SunTimes | null
  fetched_at: string
}

const POLL_INTERVAL_MS = 30_000

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

function KioskPageInner() {
  const [searchParams] = useSearchParams()
  const token = searchParams.get('token')

  const [apiData, setApiData] = useState<KioskData | null>(null)

  // When no token is present, display relativized mock data; otherwise show API data (or mock while loading)
  const data = useMemo<KioskData>(() => {
    if (token && apiData) return apiData
    return relativizeMockData(mockData)
  }, [token, apiData])

  useEffect(() => {
    if (!token) {
      return
    }

    // AbortController may be absent on very old browsers (Android 5); guard
    // so the kiosk doesn't throw before the fetch try/catch can catch it.
    const controller = typeof AbortController !== 'undefined' ? new AbortController() : null

    async function fetchData() {
      try {
        const res = await fetch('/api/kiosk/data?token=' + encodeURIComponent(token!), {
          credentials: 'include',
          signal: controller?.signal,
        })
        if (!res.ok) return
        const json: KioskData = await res.json()
        setApiData(json)
      } catch {
        // Keep displaying last known data on error
      }
    }

    fetchData()
    const intervalId = setInterval(fetchData, POLL_INTERVAL_MS)

    return () => {
      controller?.abort()
      clearInterval(intervalId)
    }
  }, [token])

  return (
    <div className="min-h-screen bg-gray-950 text-white flex flex-col overflow-hidden">
      {/* Clock & Date */}
      <KioskClock />

      {/* Divider */}
      <div className="h-px bg-gray-800 mx-4" />

      {/* Bus Departures */}
      <div className="flex-1 overflow-y-auto py-2">
        <KioskBusDepartures stops={data.transit} />
      </div>

      {/* Divider */}
      <div className="h-px bg-gray-800 mx-4" />

      {/* Weather strip */}
      <KioskWeather
        outdoor={data.outdoor ?? null}
        forecast={data.forecast ?? null}
      />

      {/* Divider */}
      <div className="h-px bg-gray-800 mx-4" />

      {/* Sunrise / Sunset */}
      <KioskSunrise sun={data.sun ?? null} />
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
