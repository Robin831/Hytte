// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { renderHook, waitFor, act } from '@testing-library/react'
import { useForecast, clearForecastCache, AUTO_REFRESH_MS } from './useForecast'

const mockLocation = { lat: 59.91, lon: 10.75, name: 'Oslo' }
const BERGEN = { name: 'Bergen', lat: 60.39, lon: 5.32 }

vi.mock('../usePreferredLocation', () => ({
  usePreferredLocation: () => mockLocation,
}))

const fakeForecast = {
  properties: {
    timeseries: [
      {
        time: '2026-06-02T12:00:00Z',
        data: {
          instant: { details: { air_temperature: 18 } },
          next_1_hours: { summary: { symbol_code: 'cloudy' } },
        },
      },
    ],
  },
}

beforeEach(() => {
  clearForecastCache()
  localStorage.clear()
  vi.restoreAllMocks()
})

afterEach(() => {
  vi.useRealTimers()
  Reflect.deleteProperty(document, 'hidden')
})

/** Override `document.hidden` for visibility-driven behaviour. */
function setHidden(hidden: boolean) {
  Object.defineProperty(document, 'hidden', { configurable: true, get: () => hidden })
}

describe('useForecast', () => {
  it('fetches forecast and returns data on success', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: () => Promise.resolve(fakeForecast),
    } as Response)

    const { result } = renderHook(() => useForecast())

    expect(result.current.loading).toBe(true)
    expect(result.current.error).toBe(false)

    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.data).toEqual(fakeForecast)
    expect(result.current.error).toBe(false)

    expect(globalThis.fetch).toHaveBeenCalledWith(
      '/api/weather/forecast?lat=59.91&lon=10.75&location=Oslo',
      expect.objectContaining({ credentials: 'include' }),
    )
  })

  it('sets error state on failed fetch', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: false,
      status: 500,
      json: () => Promise.resolve({}),
    } as Response)

    const { result } = renderHook(() => useForecast())

    await waitFor(() => expect(result.current.error).toBe(true))
    expect(result.current.loading).toBe(false)
    expect(result.current.data).toBeNull()
  })

  it('sets error state on network failure', async () => {
    vi.spyOn(globalThis, 'fetch').mockRejectedValue(new Error('network'))

    const { result } = renderHook(() => useForecast())

    await waitFor(() => expect(result.current.error).toBe(true))
    expect(result.current.loading).toBe(false)
  })

  it('returns cached data without re-fetching', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: () => Promise.resolve(fakeForecast),
    } as Response)

    const { result: first } = renderHook(() => useForecast())
    await waitFor(() => expect(first.current.loading).toBe(false))
    expect(fetchSpy).toHaveBeenCalledTimes(1)

    const { result: second } = renderHook(() => useForecast())
    expect(second.current.loading).toBe(false)
    expect(second.current.data).toEqual(fakeForecast)
    expect(fetchSpy).toHaveBeenCalledTimes(1)
  })

  it('deduplicates concurrent requests for the same location', async () => {
    let resolveFetch!: (v: Response) => void
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockReturnValue(
      new Promise((r) => { resolveFetch = r }),
    )

    const { result: a } = renderHook(() => useForecast())
    const { result: b } = renderHook(() => useForecast())

    expect(a.current.loading).toBe(true)
    expect(b.current.loading).toBe(true)
    expect(fetchSpy).toHaveBeenCalledTimes(1)

    resolveFetch({
      ok: true,
      json: () => Promise.resolve(fakeForecast),
    } as Response)

    await waitFor(() => expect(a.current.loading).toBe(false))
    await waitFor(() => expect(b.current.loading).toBe(false))
    expect(a.current.data).toEqual(fakeForecast)
    expect(b.current.data).toEqual(fakeForecast)
  })
})

describe('useForecast with an explicit location', () => {
  it('fetches the given location instead of the preferred one', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: () => Promise.resolve(fakeForecast),
    } as Response)

    const { result } = renderHook(() => useForecast(BERGEN))

    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(fetchSpy).toHaveBeenCalledWith(
      '/api/weather/forecast?lat=60.39&lon=5.32&location=Bergen',
      expect.objectContaining({ credentials: 'include' }),
    )
    expect(result.current.lastUpdated).toBeInstanceOf(Date)
  })

  it('does not fetch while the location is unresolved', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: () => Promise.resolve(fakeForecast),
    } as Response)

    const { result } = renderHook(() => useForecast(null))

    await act(async () => {
      await new Promise((r) => setTimeout(r, 0))
    })
    expect(fetchSpy).not.toHaveBeenCalled()
    expect(result.current.loading).toBe(true)
  })

  it('does not fetch until enabled', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: () => Promise.resolve(fakeForecast),
    } as Response)

    const { result, rerender } = renderHook(
      ({ enabled }) => useForecast(BERGEN, { enabled }),
      { initialProps: { enabled: false } },
    )
    expect(fetchSpy).not.toHaveBeenCalled()

    rerender({ enabled: true })
    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(fetchSpy).toHaveBeenCalledTimes(1)
  })

  it('exposes an error message alongside the error flag', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: false,
      status: 500,
      json: () => Promise.resolve({}),
    } as Response)

    const { result } = renderHook(() => useForecast(BERGEN))

    await waitFor(() => expect(result.current.error).toBe(true))
    expect(result.current.errorMessage).toBe('Failed to fetch forecast')
  })

  it('refetches on refresh(), bypassing the cache', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: () => Promise.resolve(fakeForecast),
    } as Response)

    const { result } = renderHook(() => useForecast(BERGEN))
    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(fetchSpy).toHaveBeenCalledTimes(1)

    act(() => result.current.refresh())
    await waitFor(() => expect(fetchSpy).toHaveBeenCalledTimes(2))
  })

  it('deduplicates a persisted and an in-memory caller on the same location', async () => {
    let resolveFetch!: (v: Response) => void
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockReturnValue(
      new Promise((r) => { resolveFetch = r }),
    )

    const { result: page } = renderHook(() => useForecast(BERGEN, { persist: true }))
    const { result: widget } = renderHook(() => useForecast(BERGEN))
    expect(fetchSpy).toHaveBeenCalledTimes(1)

    resolveFetch({ ok: true, json: () => Promise.resolve(fakeForecast) } as Response)

    await waitFor(() => expect(page.current.loading).toBe(false))
    await waitFor(() => expect(widget.current.loading).toBe(false))
    expect(page.current.data).toEqual(fakeForecast)
    expect(widget.current.data).toEqual(fakeForecast)
  })

  it('unmounting one caller does not fail a shared request', async () => {
    let resolveFetch!: (v: Response) => void
    vi.spyOn(globalThis, 'fetch').mockReturnValue(
      new Promise((r) => { resolveFetch = r }),
    )

    const first = renderHook(() => useForecast(BERGEN, { persist: true }))
    const { result: second } = renderHook(() => useForecast(BERGEN))
    first.unmount()

    resolveFetch({ ok: true, json: () => Promise.resolve(fakeForecast) } as Response)

    await waitFor(() => expect(second.current.loading).toBe(false))
    expect(second.current.data).toEqual(fakeForecast)
    expect(second.current.error).toBe(false)
  })

  it('refetches over the network on refresh() when persisting', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: () => Promise.resolve(fakeForecast),
    } as Response)

    const { result } = renderHook(() => useForecast(BERGEN, { persist: true }))
    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(fetchSpy).toHaveBeenCalledTimes(1)

    // The persisted cache written by the first fetch must not satisfy the refresh.
    act(() => result.current.refresh())
    await waitFor(() => expect(fetchSpy).toHaveBeenCalledTimes(2))
    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.data).toEqual(fakeForecast)
  })

  it('keeps the displayed forecast when a background refresh fails', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: () => Promise.resolve(fakeForecast),
    } as Response)

    const { result } = renderHook(() => useForecast(BERGEN, { persist: true }))
    await waitFor(() => expect(result.current.loading).toBe(false))
    const firstLoad = result.current.lastUpdated

    fetchSpy.mockRejectedValue(new Error('network'))
    act(() => result.current.refresh())

    await waitFor(() => expect(result.current.error).toBe(true))
    // Non-blocking: the previous forecast and its timestamp stay on screen.
    expect(result.current.data).toEqual(fakeForecast)
    expect(result.current.lastUpdated).toEqual(firstLoad)
    expect(result.current.loading).toBe(false)
  })

  it('seeds from the persisted cache and revalidates', async () => {
    localStorage.setItem(
      'weather:forecastCache:anon',
      JSON.stringify([{ key: '60.39,5.32', response: fakeForecast, lastUpdated: 1_700_000_000_000 }]),
    )
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: () => Promise.resolve(fakeForecast),
    } as Response)

    const { result } = renderHook(() => useForecast(BERGEN, { persist: true }))

    // Cached numbers render immediately; a fresh request is still in flight.
    expect(result.current.data).toEqual(fakeForecast)
    expect(result.current.loading).toBe(true)

    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(fetchSpy).toHaveBeenCalledTimes(1)
  })
})

describe('useForecast auto-refresh', () => {
  it('refreshes on an interval, pauses while hidden, and resumes on re-show', async () => {
    setHidden(false)
    vi.useFakeTimers()
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: () => Promise.resolve(fakeForecast),
    } as Response)

    renderHook(() => useForecast(BERGEN, { autoRefresh: true }))

    await act(async () => {
      await vi.advanceTimersByTimeAsync(0)
    })
    expect(fetchSpy).toHaveBeenCalledTimes(1)

    // The 10-minute interval fires.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(AUTO_REFRESH_MS)
    })
    expect(fetchSpy).toHaveBeenCalledTimes(2)

    // Hiding the tab stops the interval entirely.
    setHidden(true)
    await act(async () => {
      document.dispatchEvent(new Event('visibilitychange'))
      await vi.advanceTimersByTimeAsync(AUTO_REFRESH_MS * 3)
    })
    expect(fetchSpy).toHaveBeenCalledTimes(2)

    // Re-showing refreshes immediately and restarts the interval.
    setHidden(false)
    await act(async () => {
      document.dispatchEvent(new Event('visibilitychange'))
      await vi.advanceTimersByTimeAsync(0)
    })
    expect(fetchSpy).toHaveBeenCalledTimes(3)

    await act(async () => {
      await vi.advanceTimersByTimeAsync(AUTO_REFRESH_MS)
    })
    expect(fetchSpy).toHaveBeenCalledTimes(4)
  })

  it('does not start the interval when the tab is hidden on mount', async () => {
    setHidden(true)
    vi.useFakeTimers()
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: () => Promise.resolve(fakeForecast),
    } as Response)

    renderHook(() => useForecast(BERGEN, { autoRefresh: true }))

    await act(async () => {
      await vi.advanceTimersByTimeAsync(AUTO_REFRESH_MS * 2)
    })
    // Only the initial load — no interval ticks while hidden.
    expect(fetchSpy).toHaveBeenCalledTimes(1)
  })
})
