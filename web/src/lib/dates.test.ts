import { describe, it, expect, vi, beforeAll, afterAll, afterEach } from 'vitest'
import { localDateString, parseLocalDate } from './dates'

// Run under a non-UTC zone so the UTC-based implementations these helpers
// replace (`toISOString().slice(0, 10)`, `new Date('YYYY-MM-DD')`) would fail.
function withTimeZone(tz: string) {
  beforeAll(() => { vi.stubEnv('TZ', tz) })
  afterAll(() => { vi.unstubAllEnvs() })
}

describe('localDateString (Europe/Oslo)', () => {
  withTimeZone('Europe/Oslo')
  afterEach(() => { vi.useRealTimers() })

  it('returns the local date just after local midnight', () => {
    vi.useFakeTimers({ toFake: ['Date'] })
    // 2026-07-01 00:30 in Oslo (UTC+2) is still 2026-06-30 in UTC.
    vi.setSystemTime(new Date('2026-06-30T22:30:00Z'))
    expect(localDateString()).toBe('2026-07-01')
  })

  it('formats a given date from its local components', () => {
    expect(localDateString(new Date(2026, 6, 1, 0, 30))).toBe('2026-07-01')
  })

  it('zero-pads single-digit months and days', () => {
    expect(localDateString(new Date(2026, 0, 5))).toBe('2026-01-05')
  })
})

describe('parseLocalDate (America/New_York)', () => {
  withTimeZone('America/New_York')

  it('parses YYYY-MM-DD as local midnight on that day', () => {
    const d = parseLocalDate('2026-07-01')
    expect(d.getFullYear()).toBe(2026)
    expect(d.getMonth()).toBe(6)
    expect(d.getDate()).toBe(1)
    expect(d.getHours()).toBe(0)
    expect(d.getMinutes()).toBe(0)
  })

  it('round-trips through localDateString', () => {
    expect(localDateString(parseLocalDate('2026-12-31'))).toBe('2026-12-31')
  })

  it('falls back to new Date for full timestamps', () => {
    const s = '2026-07-01T12:34:56Z'
    expect(parseLocalDate(s).getTime()).toBe(new Date(s).getTime())
  })
})
