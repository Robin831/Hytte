// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { renderHook, waitFor } from '@testing-library/react'
import { useForecast, clearForecastCache } from './useForecast'

const mockLocation = { lat: 59.91, lon: 10.75, name: 'Oslo' }

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
  vi.restoreAllMocks()
})

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
