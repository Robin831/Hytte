// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import SkyWatchPage from './SkyWatchPage'

// ── Translation mock ──────────────────────────────────────────────────────────
// mockT must be a stable reference — the data-fetching effect has `t` as a
// dependency, so a new function per render would refetch endlessly.

const TRANSLATIONS: Record<string, string> = {
  'skywatch:title': 'Sky Watch',
  'skywatch:loading': 'Loading sky data...',
  'skywatch:error': 'Failed to load sky data',
  'common:actions.refresh': 'Refresh',
  'location.label': 'Location',
  'location.select': 'Select location',
  'location.choose': 'Choose a location',
  'location.myLocation': 'My location',
  'location.useMyLocation': 'Use my location',
  'location.useMyLocationAria': 'Use current position for sky data',
  'location.locating': 'Finding your location',
  'location.denied': 'Location access was denied',
  'location.unavailable': 'Your location could not be determined',
  'location.timeout': 'Finding your location took too long',
  'location.unsupported': 'This browser does not support location services',
}

function mockT(key: string): string {
  return TRANSLATIONS[key] ?? key
}

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: mockT,
    i18n: { language: 'en' },
  }),
  initReactI18next: { type: '3rdParty', init: () => {} },
}))

// ── Auth mock ─────────────────────────────────────────────────────────────────

const authState: { user: object | null; loading: boolean } = { user: null, loading: false }

vi.mock('../auth', () => ({
  useAuth: () => authState,
}))

// ── Fixtures ──────────────────────────────────────────────────────────────────

const BERGEN = { name: 'Bergen', lat: 60.3913, lon: 5.3221 }
const OSLO = { name: 'Oslo', lat: 59.9139, lon: 10.7522 }

const nowResponse = {
  timestamp: '2026-07-31T20:00:00Z',
  location: { lat: 59.9139, lon: 10.7522 },
  moon: {
    phase: 'Full Moon',
    illumination: 99,
    phase_value: 0.5,
    moonrise: '2026-07-31T21:10:00Z',
    moonset: '2026-08-01T03:20:00Z',
  },
  sun: {
    sunrise: '2026-07-31T02:30:00Z',
    sunset: '2026-07-31T19:40:00Z',
    day_length_hours: 17.1,
    golden_hour_start: '2026-07-31T18:40:00Z',
    golden_hour_end: '2026-07-31T19:40:00Z',
  },
  planets: [],
  highlights: [],
}

const calendarResponse = {
  location: { lat: 59.9139, lon: 10.7522 },
  days: 30,
  calendar: [
    { date: '2026-07-31', phase: 'Full Moon', illumination: 99, phase_value: 0.5 },
    { date: '2026-08-01', phase: 'Waning Gibbous', illumination: 95, phase_value: 0.55 },
  ],
}

const auroraResponse = {
  current_kp: 3,
  max_kp_tonight: 4,
  probability: 'possible',
  best_time: '23:00',
  best_direction: 'N',
  entries: [],
  location: { lat: 59.9139, lon: 10.7522, geomagnetic_lat: 55, min_kp_for_aurora: 5 },
}

/** Records every request URL and answers with the matching fixture. */
function makeFetchMock(preferences: Record<string, string> = {}) {
  const calls: { url: string; method: string; body?: string }[] = []
  const fetchMock = vi.fn((input: string, init?: RequestInit) => {
    const url = String(input)
    calls.push({ url, method: init?.method ?? 'GET', body: init?.body as string | undefined })

    const json = (data: unknown) =>
      Promise.resolve({ ok: true, json: () => Promise.resolve(data) } as Response)

    if (url.startsWith('/api/weather/locations')) return json({ locations: [OSLO, BERGEN] })
    if (url.startsWith('/api/settings/preferences')) return json({ preferences })
    if (url.startsWith('/api/skywatch/moon')) return json(calendarResponse)
    if (url.startsWith('/api/skywatch/aurora')) return json(auroraResponse)
    if (url.startsWith('/api/skywatch/now')) return json(nowResponse)
    return Promise.reject(new Error(`unexpected fetch: ${url}`))
  })
  return { fetchMock, calls }
}

function urlsFor(calls: { url: string }[], path: string): string[] {
  return calls.map((c) => c.url).filter((u) => u.startsWith(path))
}

async function renderPage() {
  const result = render(<SkyWatchPage />)
  // The picker is populated once /api/weather/locations resolves.
  await waitFor(() => expect(screen.getByRole('option', { name: 'Bergen' })).toBeInTheDocument())
  return result
}

describe('SkyWatchPage location picker', () => {
  beforeEach(() => {
    authState.user = null
    authState.loading = false
    try {
      localStorage.clear()
    } catch {
      /* ignore */
    }
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.clearAllMocks()
    // Remove the per-test geolocation stub (an own property on navigator).
    delete (navigator as { geolocation?: unknown }).geolocation
  })

  it('populates the dropdown from /api/weather/locations and fetches without coordinates', async () => {
    const { fetchMock, calls } = makeFetchMock()
    vi.stubGlobal('fetch', fetchMock)

    await renderPage()
    await waitFor(() => expect(urlsFor(calls, '/api/skywatch/aurora')).toHaveLength(1))

    expect(screen.getByRole('option', { name: 'Oslo' })).toBeInTheDocument()
    // No selection yet — the backend default applies, so no lat/lon is sent.
    expect(urlsFor(calls, '/api/skywatch/now')[0]).toBe('/api/skywatch/now')
    expect(urlsFor(calls, '/api/skywatch/aurora')[0]).toBe('/api/skywatch/aurora')
  })

  it('refetches now, moon and aurora with the selected coordinates', async () => {
    const { fetchMock, calls } = makeFetchMock()
    vi.stubGlobal('fetch', fetchMock)

    await renderPage()
    await waitFor(() => expect(urlsFor(calls, '/api/skywatch/aurora')).toHaveLength(1))

    fireEvent.change(screen.getByLabelText('Select location'), {
      target: { value: `${BERGEN.lat},${BERGEN.lon}` },
    })

    await waitFor(() => expect(urlsFor(calls, '/api/skywatch/aurora')).toHaveLength(2))

    const coords = `lat=${BERGEN.lat}&lon=${BERGEN.lon}`
    expect(urlsFor(calls, '/api/skywatch/now').at(-1)).toContain(coords)
    expect(urlsFor(calls, '/api/skywatch/moon').at(-1)).toContain(coords)
    expect(urlsFor(calls, '/api/skywatch/aurora').at(-1)).toContain(coords)
  })

  it('stores the selection in localStorage when signed out and restores it on reload', async () => {
    const first = makeFetchMock()
    vi.stubGlobal('fetch', first.fetchMock)

    const { unmount } = await renderPage()
    await waitFor(() => expect(urlsFor(first.calls, '/api/skywatch/aurora')).toHaveLength(1))

    fireEvent.change(screen.getByLabelText('Select location'), {
      target: { value: `${BERGEN.lat},${BERGEN.lon}` },
    })

    await waitFor(() =>
      expect(JSON.parse(localStorage.getItem('skywatch_location') ?? 'null')).toEqual(BERGEN),
    )
    // Signed-out users must not hit the preferences endpoint.
    expect(urlsFor(first.calls, '/api/settings/preferences')).toHaveLength(0)

    // Re-mount: the stored location is applied to the very first data fetch.
    unmount()
    const second = makeFetchMock()
    vi.stubGlobal('fetch', second.fetchMock)
    await renderPage()
    await waitFor(() => expect(urlsFor(second.calls, '/api/skywatch/now')).not.toHaveLength(0))
    expect(urlsFor(second.calls, '/api/skywatch/now')[0]).toContain(`lat=${BERGEN.lat}`)
  })

  it('saves the selection to preferences when signed in', async () => {
    authState.user = { id: 1 }
    const { fetchMock, calls } = makeFetchMock()
    vi.stubGlobal('fetch', fetchMock)

    await renderPage()
    await waitFor(() => expect(urlsFor(calls, '/api/skywatch/aurora')).toHaveLength(1))

    fireEvent.change(screen.getByLabelText('Select location'), {
      target: { value: `${BERGEN.lat},${BERGEN.lon}` },
    })

    await waitFor(() => {
      const put = calls.find((c) => c.method === 'PUT' && c.url.startsWith('/api/settings/preferences'))
      expect(put).toBeDefined()
      expect(JSON.parse(put!.body as string)).toEqual({
        preferences: { skywatch_location: JSON.stringify(BERGEN) },
      })
    })
    // Server-side storage only — no cross-account leakage through localStorage.
    expect(localStorage.getItem('skywatch_location')).toBeNull()
  })

  it('restores a signed-in user’s saved preference before the first fetch', async () => {
    authState.user = { id: 1 }
    const { fetchMock, calls } = makeFetchMock({ skywatch_location: JSON.stringify(BERGEN) })
    vi.stubGlobal('fetch', fetchMock)

    await renderPage()
    await waitFor(() => expect(urlsFor(calls, '/api/skywatch/now')).not.toHaveLength(0))

    const nowUrls = urlsFor(calls, '/api/skywatch/now')
    expect(nowUrls[0]).toContain(`lat=${BERGEN.lat}&lon=${BERGEN.lon}`)
    // Exactly one round of requests — no default-location fetch first.
    expect(nowUrls.filter((u) => u === '/api/skywatch/now')).toHaveLength(0)
  })

  it('uses the coordinates returned by browser geolocation', async () => {
    const { fetchMock, calls } = makeFetchMock()
    vi.stubGlobal('fetch', fetchMock)
    Object.defineProperty(navigator, 'geolocation', {
      configurable: true,
      value: {
        getCurrentPosition: (success: PositionCallback) =>
          success({ coords: { latitude: 63.43, longitude: 10.39 } } as GeolocationPosition),
      },
    })

    await renderPage()
    await waitFor(() => expect(urlsFor(calls, '/api/skywatch/aurora')).toHaveLength(1))

    fireEvent.click(screen.getByRole('button', { name: 'Use current position for sky data' }))

    await waitFor(() => expect(urlsFor(calls, '/api/skywatch/aurora')).toHaveLength(2))
    expect(urlsFor(calls, '/api/skywatch/aurora').at(-1)).toContain('lat=63.43&lon=10.39')
    // Geolocation results are labelled generically — no reverse geocoding.
    expect(screen.getByRole('option', { name: 'My location' })).toBeInTheDocument()
  })

  it('shows a message and keeps the selection when geolocation is denied', async () => {
    const { fetchMock, calls } = makeFetchMock()
    vi.stubGlobal('fetch', fetchMock)
    Object.defineProperty(navigator, 'geolocation', {
      configurable: true,
      value: {
        getCurrentPosition: (_success: PositionCallback, failure: PositionErrorCallback) =>
          failure({ code: 1, PERMISSION_DENIED: 1, POSITION_UNAVAILABLE: 2, TIMEOUT: 3, message: '' } as GeolocationPositionError),
      },
    })

    await renderPage()
    await waitFor(() => expect(urlsFor(calls, '/api/skywatch/aurora')).toHaveLength(1))

    fireEvent.change(screen.getByLabelText('Select location'), {
      target: { value: `${BERGEN.lat},${BERGEN.lon}` },
    })
    await waitFor(() => expect(urlsFor(calls, '/api/skywatch/aurora')).toHaveLength(2))

    fireEvent.click(screen.getByRole('button', { name: 'Use current position for sky data' }))

    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('Location access was denied'))
    // The failed lookup must not trigger a refetch.
    expect(urlsFor(calls, '/api/skywatch/aurora')).toHaveLength(2)
    expect((screen.getByLabelText('Select location') as HTMLSelectElement).value).toBe(
      `${BERGEN.lat},${BERGEN.lon}`,
    )
  })
})
