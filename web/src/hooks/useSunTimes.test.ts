// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { renderHook, waitFor, act } from '@testing-library/react'
import { useSunTimes, type SunTimes } from './useSunTimes'

const OSLO = { name: 'Oslo', lat: 59.9139, lon: 10.7522 }
const BERGEN = { name: 'Bergen', lat: 60.3913, lon: 5.3221 }

const osloSun: SunTimes = {
  sunrise: '2026-06-02T03:55:00Z',
  sunset: '2026-06-02T20:45:00Z',
  daylightSeconds: 60_600,
  polarDay: false,
  polarNight: false,
}

const bergenSun: SunTimes = { ...osloSun, daylightSeconds: 61_200 }

function json(data: unknown): Response {
  return { ok: true, json: () => Promise.resolve(data) } as unknown as Response
}

beforeEach(() => {
  vi.spyOn(console, 'warn').mockImplementation(() => {})
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('useSunTimes', () => {
  it('fetches sun data for the given location', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(json(osloSun))

    const { result } = renderHook(() => useSunTimes(OSLO))

    await waitFor(() => expect(result.current).toEqual(osloSun))
    expect(fetchSpy).toHaveBeenCalledWith('/api/weather/sun?lat=59.9139&lon=10.7522')
  })

  it('does not fetch without a location', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(json(osloSun))

    const { result } = renderHook(() => useSunTimes(null))

    await act(async () => {
      await new Promise((r) => setTimeout(r, 0))
    })
    expect(fetchSpy).not.toHaveBeenCalled()
    expect(result.current).toBeNull()
  })

  it('does not fetch until enabled', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(json(osloSun))

    const { result, rerender } = renderHook(
      ({ enabled }) => useSunTimes(OSLO, enabled),
      { initialProps: { enabled: false } },
    )
    expect(fetchSpy).not.toHaveBeenCalled()

    rerender({ enabled: true })
    await waitFor(() => expect(result.current).toEqual(osloSun))
  })

  it('refetches when the location changes and never shows the previous times', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockImplementation((input) =>
      Promise.resolve(json(String(input).includes('5.3221') ? bergenSun : osloSun)),
    )

    const { result, rerender } = renderHook(({ loc }) => useSunTimes(loc), {
      initialProps: { loc: OSLO },
    })
    await waitFor(() => expect(result.current).toEqual(osloSun))

    rerender({ loc: BERGEN })
    // The stale Oslo entry is tagged with Oslo's coordinates, so it is discarded
    // rather than rendered under Bergen while the new request is in flight.
    expect(result.current).toBeNull()

    await waitFor(() => expect(result.current).toEqual(bergenSun))
    expect(fetchSpy).toHaveBeenCalledTimes(2)
  })

  it('returns null when the request fails', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: false,
      status: 500,
      json: () => Promise.resolve(null),
    } as Response)

    const { result } = renderHook(() => useSunTimes(OSLO))

    await act(async () => {
      await new Promise((r) => setTimeout(r, 0))
    })
    expect(result.current).toBeNull()
  })

  it('returns null when the network fails', async () => {
    vi.spyOn(globalThis, 'fetch').mockRejectedValue(new Error('offline'))

    const { result } = renderHook(() => useSunTimes(OSLO))

    await act(async () => {
      await new Promise((r) => setTimeout(r, 0))
    })
    expect(result.current).toBeNull()
  })
})
