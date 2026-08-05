// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { renderHook, waitFor, act } from '@testing-library/react'
import { useWeatherLocation } from './useWeatherLocation'

let mockAuth: { user: { id: number } | null; loading: boolean } = { user: null, loading: false }

vi.mock('../auth', () => ({
  useAuth: () => mockAuth,
}))

const OSLO = { name: 'Oslo', lat: 59.9139, lon: 10.7522 }
const BERGEN = { name: 'Bergen', lat: 60.3913, lon: 5.3221 }
const TRONDHEIM = { name: 'Trondheim', lat: 63.4305, lon: 10.3951 }
// Returned unsorted so the reducer's name sort is exercised.
const LOCATIONS = [TRONDHEIM, OSLO, BERGEN]

function json(data: unknown): Response {
  return { ok: true, json: () => Promise.resolve(data) } as unknown as Response
}

function deferred() {
  let resolve!: () => void
  const promise = new Promise<void>((r) => {
    resolve = () => r()
  })
  return { promise, resolve }
}

interface Routes {
  /** Resolved value of GET /api/weather/locations, gated behind `locationsGate`. */
  locations?: unknown
  locationsGate?: Promise<void>
  /** Resolved value of GET /api/settings/preferences, gated behind `prefsGate`. */
  prefs?: unknown
  prefsGate?: Promise<void>
}

/** Route fetch by URL, so tests only describe the responses they care about. */
function routeFetch(routes: Routes) {
  return vi.spyOn(globalThis, 'fetch').mockImplementation((input, init) => {
    const url = String(input)
    if (url.startsWith('/api/weather/locations')) {
      const body = json({ locations: routes.locations ?? [] })
      return routes.locationsGate ? routes.locationsGate.then(() => body) : Promise.resolve(body)
    }
    if (url.startsWith('/api/settings/preferences')) {
      if (init?.method === 'PUT') return Promise.resolve(json({}))
      const body = json(routes.prefs ?? { preferences: {} })
      return routes.prefsGate ? routes.prefsGate.then(() => body) : Promise.resolve(body)
    }
    return Promise.resolve({ ok: false, status: 404, json: () => Promise.resolve(null) } as Response)
  })
}

/** Let every queued promise callback and effect flush. */
async function flush() {
  await act(async () => {
    await new Promise((r) => setTimeout(r, 0))
  })
}

beforeEach(() => {
  localStorage.clear()
  mockAuth = { user: null, loading: false }
  vi.spyOn(console, 'warn').mockImplementation(() => {})
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('useWeatherLocation', () => {
  it('retries a preference that arrived before the locations list', async () => {
    mockAuth = { user: { id: 1 }, loading: false }
    const gate = deferred()
    routeFetch({
      locations: LOCATIONS,
      locationsGate: gate.promise,
      prefs: { preferences: { weather_location: 'Bergen' } },
    })

    const { result } = renderHook(() => useWeatherLocation())

    // Preferences settle first. Bergen cannot be resolved against an empty
    // locations list, so it is parked rather than dropped.
    await flush()
    expect(result.current.location).toBeNull()
    expect(result.current.locationResolved).toBe(false)

    gate.resolve()
    await flush()

    expect(result.current.location).toEqual(BERGEN)
    expect(result.current.locationResolved).toBe(true)
  })

  it('falls back to the localStorage location when logged out', async () => {
    localStorage.setItem('recent_locations', JSON.stringify([OSLO, BERGEN]))
    localStorage.setItem('weather_location', JSON.stringify(BERGEN))
    const fetchSpy = routeFetch({ locations: LOCATIONS })

    const { result } = renderHook(() => useWeatherLocation())

    // Resolved from localStorage on the very first render — no waiting on the API.
    expect(result.current.location).toEqual(BERGEN)
    expect(result.current.locationResolved).toBe(true)

    await flush()
    expect(result.current.location).toEqual(BERGEN)
    // A logged-out visitor must never hit the preferences endpoint.
    expect(fetchSpy.mock.calls.every(([url]) => !String(url).startsWith('/api/settings/preferences')))
      .toBe(true)
  })

  it('persists a logged-out selection to localStorage', async () => {
    localStorage.setItem('recent_locations', JSON.stringify([OSLO]))
    routeFetch({ locations: LOCATIONS })

    const { result } = renderHook(() => useWeatherLocation())
    await flush()

    act(() => result.current.selectByName('Bergen'))
    await flush()

    expect(result.current.location).toEqual(BERGEN)
    expect(JSON.parse(localStorage.getItem('weather_location')!)).toEqual(BERGEN)
    expect(JSON.parse(localStorage.getItem('recent_locations')!)[0]).toEqual(BERGEN)
  })

  it('persists a logged-in selection to preferences and not localStorage', async () => {
    mockAuth = { user: { id: 7 }, loading: false }
    const fetchSpy = routeFetch({ locations: LOCATIONS, prefs: { preferences: {} } })

    const { result } = renderHook(() => useWeatherLocation())
    await waitFor(() => expect(result.current.locationResolved).toBe(true))

    act(() => result.current.selectByName('Bergen'))
    await flush()

    expect(result.current.location).toEqual(BERGEN)

    const put = fetchSpy.mock.calls.find(([, init]) => init?.method === 'PUT')
    expect(put).toBeDefined()
    const body = JSON.parse(String(put![1]!.body)) as {
      preferences: { weather_location: string; recent_locations: string }
    }
    expect(body.preferences.weather_location).toBe('Bergen')
    expect(JSON.parse(body.preferences.recent_locations)[0]).toEqual(BERGEN)

    // Authenticated users store recents server-side only — nothing may leak to
    // localStorage, where a different account on the same browser would read it.
    expect(localStorage.getItem('weather_location')).toBeNull()
    expect(localStorage.getItem('recent_locations')).toBeNull()
  })

  it('keeps an explicit selection when a stored preference arrives later', async () => {
    mockAuth = { user: { id: 7 }, loading: false }
    const gate = deferred()
    const fetchSpy = routeFetch({
      locations: LOCATIONS,
      prefs: {
        preferences: {
          weather_location: 'Bergen',
          // Server recents that predate the selection — they must not replace the
          // local list, which is what gets pushed back.
          recent_locations: JSON.stringify([BERGEN, OSLO]),
        },
      },
      prefsGate: gate.promise,
    })

    const { result } = renderHook(() => useWeatherLocation())
    await waitFor(() => expect(result.current.knownLocations).toHaveLength(3))

    act(() => result.current.selectByName('Trondheim'))
    expect(result.current.location).toEqual(TRONDHEIM)

    gate.resolve()
    await flush()

    expect(result.current.location).toEqual(TRONDHEIM)
    expect(result.current.userHasSelected).toBe(true)
    // The late-arriving sync pushes the user's choice back to the server.
    const put = fetchSpy.mock.calls.find(([, init]) => init?.method === 'PUT')
    expect(put).toBeDefined()
    const body = JSON.parse(String(put![1]!.body)) as {
      preferences: { weather_location: string; recent_locations: string }
    }
    expect(body.preferences.weather_location).toBe('Trondheim')
    // Display and the PUT body must agree, and both must contain the selection.
    const pushedRecents = JSON.parse(body.preferences.recent_locations) as typeof LOCATIONS
    expect(pushedRecents).toEqual(result.current.recents)
    expect(pushedRecents[0]).toEqual(TRONDHEIM)
    expect(result.current.recents).toContainEqual(TRONDHEIM)
  })

  it('adopts the server recents when no selection races the preferences load', async () => {
    mockAuth = { user: { id: 7 }, loading: false }
    routeFetch({
      locations: LOCATIONS,
      prefs: {
        preferences: {
          weather_location: 'Bergen',
          recent_locations: JSON.stringify([BERGEN, TRONDHEIM]),
        },
      },
    })

    const { result } = renderHook(() => useWeatherLocation())
    await waitFor(() => expect(result.current.locationResolved).toBe(true))

    expect(result.current.location).toEqual(BERGEN)
    expect(result.current.recents).toEqual([BERGEN, TRONDHEIM])
  })

  it('falls back safely when the stored location is invalid', async () => {
    // lat/lon out of range — rejected by isValidRecentLocation.
    localStorage.setItem('recent_locations', JSON.stringify([OSLO, BERGEN]))
    localStorage.setItem('weather_location', JSON.stringify({ name: 'Nowhere', lat: 999, lon: 999 }))
    routeFetch({ locations: LOCATIONS })

    const { result } = renderHook(() => useWeatherLocation())
    await flush()

    expect(result.current.location).toEqual(OSLO)
  })

  it('falls back to the first recent when the stored name is unknown', async () => {
    localStorage.setItem('recent_locations', JSON.stringify([BERGEN, OSLO]))
    localStorage.setItem('weather_location', 'Atlantis')
    routeFetch({ locations: LOCATIONS })

    const { result } = renderHook(() => useWeatherLocation())
    await flush()

    expect(result.current.location).toEqual(BERGEN)
  })

  it('builds defaults from the API on a first visit with no stored data', async () => {
    routeFetch({ locations: LOCATIONS })

    const { result } = renderHook(() => useWeatherLocation())
    expect(result.current.location).toBeNull()

    await flush()

    expect(result.current.location).toEqual(OSLO)
    expect(result.current.recents.map((l) => l.name)).toEqual(['Oslo', 'Bergen', 'Trondheim'])
    expect(result.current.knownLocations.map((l) => l.name)).toEqual(['Bergen', 'Oslo', 'Trondheim'])
  })

  it('keeps the active location in the dropdown even when it is not a recent', async () => {
    mockAuth = { user: { id: 7 }, loading: false }
    localStorage.setItem('recent_locations', JSON.stringify([OSLO]))
    routeFetch({ locations: LOCATIONS, prefs: { preferences: { weather_location: 'Bergen' } } })

    const { result } = renderHook(() => useWeatherLocation())
    await flush()

    // Bergen came from the preference and is not in the recents list, but must
    // still appear as the selected dropdown option rather than an empty select.
    expect(result.current.location).toEqual(BERGEN)
    expect(result.current.recents.map((l) => l.name)).toEqual(['Oslo'])
    expect(result.current.displayRecents.map((l) => l.name)).toEqual(['Bergen', 'Oslo'])
    expect(result.current.otherCities.map((l) => l.name)).toEqual(['Trondheim'])
  })

  it('adds a searched location to the front of recents', async () => {
    routeFetch({ locations: LOCATIONS })

    const { result } = renderHook(() => useWeatherLocation())
    await flush()

    act(() => result.current.selectLocation({ name: 'Reykjavík', lat: 64.14, lon: -21.94 }))
    await flush()

    expect(result.current.location).toEqual({ name: 'Reykjavík', lat: 64.14, lon: -21.94 })
    expect(result.current.recents[0].name).toBe('Reykjavík')
  })

  it('still resolves a location when the locations request fails', async () => {
    localStorage.setItem('recent_locations', JSON.stringify([BERGEN]))
    vi.spyOn(globalThis, 'fetch').mockRejectedValue(new Error('offline'))

    const { result } = renderHook(() => useWeatherLocation())
    await flush()

    expect(result.current.location).toEqual(BERGEN)
    expect(result.current.locationResolved).toBe(true)
  })

  it('ignores a selection for a name that resolves to nothing', async () => {
    localStorage.setItem('recent_locations', JSON.stringify([OSLO]))
    routeFetch({ locations: [] })

    const { result } = renderHook(() => useWeatherLocation())
    await flush()

    act(() => result.current.selectByName('Atlantis'))
    await flush()

    expect(result.current.location).toEqual(OSLO)
  })
})
