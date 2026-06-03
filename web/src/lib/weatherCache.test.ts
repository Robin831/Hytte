// @vitest-environment happy-dom
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { keyFor, readForecastCache, writeForecastCache } from './weatherCache'

const ANON_KEY = 'weather:forecastCache:anon'

// Minimal forecast-like payload — the cache treats the response as opaque JSON.
function makeResponse(temp: number) {
  return { properties: { timeseries: [{ time: '2026-06-03T12:00:00Z', temp }] } }
}

describe('weatherCache', () => {
  beforeEach(() => {
    localStorage.clear()
  })

  afterEach(() => {
    vi.useRealTimers()
    localStorage.clear()
  })

  it('keyFor builds a `lat,lon` key', () => {
    expect(keyFor(59.91, 10.75)).toBe('59.91,10.75')
  })

  it('round-trips a written response and timestamp', () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-06-03T10:00:00Z'))
    const response = makeResponse(21)

    writeForecastCache(59.91, 10.75, response)
    const entry = readForecastCache<ReturnType<typeof makeResponse>>(59.91, 10.75)

    expect(entry).not.toBeNull()
    expect(entry!.key).toBe('59.91,10.75')
    expect(entry!.response).toEqual(response)
    expect(entry!.lastUpdated).toBe(Date.now())
  })

  it('returns null for a never-written location', () => {
    writeForecastCache(59.91, 10.75, makeResponse(21))
    expect(readForecastCache(60.39, 5.32)).toBeNull()
  })

  it('returns null when the store key is missing', () => {
    expect(readForecastCache(59.91, 10.75)).toBeNull()
  })

  it('caps the store at 5 entries, evicting the oldest', () => {
    // Write 6 distinct locations; the first (oldest) should be evicted.
    for (let i = 0; i < 6; i++) {
      writeForecastCache(i, i, makeResponse(i))
    }

    const stored = JSON.parse(localStorage.getItem(ANON_KEY)!) as Array<{ key: string }>
    expect(stored).toHaveLength(5)
    // Oldest (0,0) evicted; (1,1)..(5,5) retained.
    expect(readForecastCache(0, 0)).toBeNull()
    expect(readForecastCache(1, 1)).not.toBeNull()
    expect(readForecastCache(5, 5)).not.toBeNull()
    expect(stored.map((e) => e.key)).toEqual(['1,1', '2,2', '3,3', '4,4', '5,5'])
  })

  it('re-writing an existing key refreshes it without growing the array', () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-06-03T10:00:00Z'))

    // Fill with 5 locations.
    for (let i = 0; i < 5; i++) {
      writeForecastCache(i, i, makeResponse(i))
    }

    // Re-write the oldest entry (0,0) — it should move to most-recently-used and
    // refresh its timestamp, and adding a new 6th location must then evict (1,1).
    vi.setSystemTime(new Date('2026-06-03T11:00:00Z'))
    writeForecastCache(0, 0, makeResponse(99))

    const refreshed = readForecastCache<ReturnType<typeof makeResponse>>(0, 0)
    expect(refreshed!.response).toEqual(makeResponse(99))
    expect(refreshed!.lastUpdated).toBe(new Date('2026-06-03T11:00:00Z').getTime())

    const stored = JSON.parse(localStorage.getItem(ANON_KEY)!) as Array<{ key: string }>
    expect(stored).toHaveLength(5)
    // (0,0) is now most-recently-used (last), (1,1) is now oldest.
    expect(stored.map((e) => e.key)).toEqual(['1,1', '2,2', '3,3', '4,4', '0,0'])

    // Adding a 6th distinct location evicts the new oldest (1,1), not (0,0).
    writeForecastCache(9, 9, makeResponse(9))
    expect(readForecastCache(1, 1)).toBeNull()
    expect(readForecastCache(0, 0)).not.toBeNull()
  })

  it('reading an entry promotes it to most-recently-used', () => {
    // Fill with 5 locations: 0,0 through 4,4.
    for (let i = 0; i < 5; i++) {
      writeForecastCache(i, i, makeResponse(i))
    }

    // Read the oldest entry (0,0) — it should be promoted to the tail.
    readForecastCache(0, 0)

    const stored = JSON.parse(localStorage.getItem(ANON_KEY)!) as Array<{ key: string }>
    expect(stored.map((e) => e.key)).toEqual(['1,1', '2,2', '3,3', '4,4', '0,0'])

    // Adding a 6th location should now evict (1,1) — the new oldest — not (0,0).
    writeForecastCache(9, 9, makeResponse(9))
    expect(readForecastCache(1, 1)).toBeNull()
    expect(readForecastCache(0, 0)).not.toBeNull()
  })

  it('falls back to null on corrupt (non-JSON) localStorage data', () => {
    localStorage.setItem(ANON_KEY, 'not-json{')
    expect(() => readForecastCache(59.91, 10.75)).not.toThrow()
    expect(readForecastCache(59.91, 10.75)).toBeNull()
  })

  it('falls back to null when stored data is not an array', () => {
    localStorage.setItem(ANON_KEY, JSON.stringify({ key: '1,1' }))
    expect(readForecastCache(1, 1)).toBeNull()
  })

  it('ignores malformed entries within the array', () => {
    localStorage.setItem(
      ANON_KEY,
      JSON.stringify([
        { key: '1,1' }, // missing response + lastUpdated
        { key: '2,2', response: makeResponse(2), lastUpdated: 123 },
      ]),
    )
    expect(readForecastCache(1, 1)).toBeNull()
    expect(readForecastCache(2, 2)).not.toBeNull()
  })

  it('recovers from corrupt data on write by starting fresh', () => {
    localStorage.setItem(ANON_KEY, 'garbage')
    expect(() => writeForecastCache(59.91, 10.75, makeResponse(21))).not.toThrow()

    const entry = readForecastCache<ReturnType<typeof makeResponse>>(59.91, 10.75)
    expect(entry).not.toBeNull()
    expect(entry!.response).toEqual(makeResponse(21))
    const stored = JSON.parse(localStorage.getItem(ANON_KEY)!) as unknown[]
    expect(stored).toHaveLength(1)
  })

  it('does not throw when localStorage.setItem fails (quota)', () => {
    const spy = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('QuotaExceededError')
    })
    expect(() => writeForecastCache(59.91, 10.75, makeResponse(21))).not.toThrow()
    spy.mockRestore()
  })

  describe('user-scoped isolation', () => {
    it('scopes cache entries by user ID', () => {
      writeForecastCache(59.91, 10.75, makeResponse(21), 1)
      writeForecastCache(59.91, 10.75, makeResponse(42), 2)

      const user1 = readForecastCache<ReturnType<typeof makeResponse>>(59.91, 10.75, 1)
      const user2 = readForecastCache<ReturnType<typeof makeResponse>>(59.91, 10.75, 2)
      const anon = readForecastCache<ReturnType<typeof makeResponse>>(59.91, 10.75)

      expect(user1!.response).toEqual(makeResponse(21))
      expect(user2!.response).toEqual(makeResponse(42))
      expect(anon).toBeNull()
    })

    it('uses separate storage keys per user', () => {
      writeForecastCache(59.91, 10.75, makeResponse(10), 1)
      writeForecastCache(59.91, 10.75, makeResponse(20))

      expect(localStorage.getItem('weather:forecastCache:1')).not.toBeNull()
      expect(localStorage.getItem(ANON_KEY)).not.toBeNull()
      expect(JSON.parse(localStorage.getItem('weather:forecastCache:1')!)).toHaveLength(1)
      expect(JSON.parse(localStorage.getItem(ANON_KEY)!)).toHaveLength(1)
    })
  })
})
