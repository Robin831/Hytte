import { describe, it, expect, vi } from 'vitest'
import type { TFunction } from 'i18next'

vi.mock('./formatDate', () => ({
  formatNumber: (n: number, opts?: Intl.NumberFormatOptions) =>
    n.toLocaleString('en', opts),
}))

const { formatDistance, formatDuration, formatPace } = await import('./training')

// Minimal stub for the i18n `t` function: returns the unit suffixes the real
// `training` locale provides and interpolates the duration helper keys, so the
// tests assert on predictable, locale-independent output.
const t = ((key: string, vars?: Record<string, unknown>): string => {
  switch (key) {
    case 'units.m':
      return 'm'
    case 'units.km':
      return 'km'
    case 'units.h':
      return 'h'
    case 'units.pace':
      return '/km'
    case 'units.hours_minutes':
      return `${vars?.h}h ${vars?.m}m`
    case 'units.minutes':
      return `${vars?.m}m`
    default:
      return key
  }
}) as unknown as TFunction<'training'>

describe('formatDistance', () => {
  it('renders sub-1km distances in whole meters', () => {
    expect(formatDistance(0, t)).toBe('0 m')
    expect(formatDistance(450, t)).toBe('450 m')
    expect(formatDistance(999.4, t)).toBe('999 m')
  })

  it('renders multi-km distances with two fraction digits by default', () => {
    expect(formatDistance(1000, t)).toBe('1.00 km')
    expect(formatDistance(5234, t)).toBe('5.23 km')
    expect(formatDistance(42195, t)).toBe('42.20 km')
  })

  it('honors an explicit fractionDigits override', () => {
    expect(formatDistance(5234, t, { fractionDigits: 1 })).toBe('5.2 km')
    expect(formatDistance(5234, t, { fractionDigits: 0 })).toBe('5 km')
  })
})

describe('formatPace', () => {
  it('returns --:-- for non-positive paces', () => {
    expect(formatPace(0, t)).toBe('--:--')
    expect(formatPace(-30, t)).toBe('--:--')
  })

  it('zero-pads seconds and appends the localized unit', () => {
    expect(formatPace(305, t)).toBe('5:05 /km')
    expect(formatPace(330, t)).toBe('5:30 /km')
  })

  it('rolls a rounded 60-second remainder over to the next minute', () => {
    // 359.6s rounds to 60 seconds -> should become 6:00, not 5:60.
    expect(formatPace(359.6, t)).toBe('6:00 /km')
  })

  it('omits the unit suffix when withUnit is false', () => {
    expect(formatPace(305, t, { withUnit: false })).toBe('5:05')
  })
})

describe('formatDuration', () => {
  it('formats clock style as M:SS without hours', () => {
    expect(formatDuration(0, t)).toBe('0:00')
    expect(formatDuration(65, t)).toBe('1:05')
    expect(formatDuration(599, t)).toBe('9:59')
  })

  it('formats clock style as H:MM:SS once an hour is reached', () => {
    expect(formatDuration(3600, t)).toBe('1:00:00')
    expect(formatDuration(3661, t)).toBe('1:01:01')
    expect(formatDuration(36000, t)).toBe('10:00:00')
  })

  it('rounds fractional seconds in clock style', () => {
    expect(formatDuration(59.6, t)).toBe('1:00')
  })

  it('formats human style with localized hours and minutes', () => {
    expect(formatDuration(0, t, { style: 'human' })).toBe('0m')
    expect(formatDuration(1800, t, { style: 'human' })).toBe('30m')
    expect(formatDuration(5400, t, { style: 'human' })).toBe('1h 30m')
  })

  it('formats decimal style with one fraction digit', () => {
    expect(formatDuration(0, t, { style: 'decimal' })).toBe('0.0h')
    expect(formatDuration(5400, t, { style: 'decimal' })).toBe('1.5h')
    expect(formatDuration(360000, t, { style: 'decimal' })).toBe('100.0h')
  })
})
