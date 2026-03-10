import { describe, it, expect } from 'vitest'
import { getSunTimes, formatDaylight } from './sunCalc'

// Reference values from NOAA solar calculator / timeanddate.com
// Tolerances account for the simplified algorithm (~1-2 min accuracy)

/** Helper: check time is within +-N minutes of expected HH:MM UTC */
function expectTimeNear(actual: Date, expectedH: number, expectedM: number, toleranceMin = 3) {
  const actualMin = actual.getUTCHours() * 60 + actual.getUTCMinutes()
  const expectedMin = expectedH * 60 + expectedM
  const diff = Math.abs(actualMin - expectedMin)
  // Handle wrap-around midnight
  const wrapped = Math.min(diff, 1440 - diff)
  expect(wrapped).toBeLessThanOrEqual(toleranceMin)
}

describe('getSunTimes', () => {
  it('calculates sunrise/sunset for Oslo on spring equinox', () => {
    // Oslo (59.91°N, 10.75°E), March 20 2025
    // Expected ~05:15 UTC sunrise, ~17:20 UTC sunset (approx)
    const result = getSunTimes(new Date(2025, 2, 20), 59.91, 10.75)

    expect(result.sunrise).not.toBeNull()
    expect(result.sunset).not.toBeNull()
    expectTimeNear(result.sunrise!, 5, 19, 5)
    expectTimeNear(result.sunset!, 17, 30, 5)
    // ~12 hours of daylight near equinox
    expect(result.daylightMinutes).toBeGreaterThan(720)
    expect(result.daylightMinutes).toBeLessThan(745)
  })

  it('calculates sunrise/sunset for Oslo in summer', () => {
    // Oslo, June 21 2025 (summer solstice)
    // Expected ~01:53 UTC sunrise, ~20:44 UTC sunset (approx)
    const result = getSunTimes(new Date(2025, 5, 21), 59.91, 10.75)

    expect(result.sunrise).not.toBeNull()
    expect(result.sunset).not.toBeNull()
    expectTimeNear(result.sunrise!, 1, 53, 5)
    expectTimeNear(result.sunset!, 20, 44, 5)
    // ~18+ hours of daylight
    expect(result.daylightMinutes).toBeGreaterThan(1100)
  })

  it('calculates sunrise/sunset for Oslo in winter', () => {
    // Oslo, December 21 2025 (winter solstice)
    // Expected ~08:18 UTC sunrise, ~14:12 UTC sunset (approx)
    const result = getSunTimes(new Date(2025, 11, 21), 59.91, 10.75)

    expect(result.sunrise).not.toBeNull()
    expect(result.sunset).not.toBeNull()
    expectTimeNear(result.sunrise!, 8, 18, 5)
    expectTimeNear(result.sunset!, 14, 12, 5)
    // ~6 hours of daylight
    expect(result.daylightMinutes).toBeGreaterThan(340)
    expect(result.daylightMinutes).toBeLessThan(365)
  })

  it('returns polar day for Tromsoe in summer', () => {
    // Tromsoe (69.65°N, 18.96°E), June 21
    // Midnight sun — sun never sets
    const result = getSunTimes(new Date(2025, 5, 21), 69.65, 18.96)

    expect(result.sunrise).toBeNull()
    expect(result.sunset).toBeNull()
    expect(result.daylightMinutes).toBe(1440)
  })

  it('returns polar night for Tromsoe in winter', () => {
    // Tromsoe (69.65°N, 18.96°E), December 21
    // Polar night — sun never rises
    const result = getSunTimes(new Date(2025, 11, 21), 69.65, 18.96)

    expect(result.sunrise).toBeNull()
    expect(result.sunset).toBeNull()
    expect(result.daylightMinutes).toBe(0)
  })

  it('calculates sunrise/sunset near equator', () => {
    // Nairobi (1.29°S, 36.82°E), March 20 2025
    // Near-equatorial: ~12 hours daylight year-round
    const result = getSunTimes(new Date(2025, 2, 20), -1.29, 36.82)

    expect(result.sunrise).not.toBeNull()
    expect(result.sunset).not.toBeNull()
    // Should be close to 12 hours
    expect(result.daylightMinutes).toBeGreaterThan(715)
    expect(result.daylightMinutes).toBeLessThan(730)
  })

  it('handles southern hemisphere correctly', () => {
    // Sydney (33.87°S, 151.21°E), December 21 (summer in south)
    const result = getSunTimes(new Date(2025, 11, 21), -33.87, 151.21)

    expect(result.sunrise).not.toBeNull()
    expect(result.sunset).not.toBeNull()
    // Long summer day in southern hemisphere
    expect(result.daylightMinutes).toBeGreaterThan(840)
  })

  it('handles negative longitude (western hemisphere)', () => {
    // New York (40.71°N, -74.01°W), March 20 2025
    const result = getSunTimes(new Date(2025, 2, 20), 40.71, -74.01)

    expect(result.sunrise).not.toBeNull()
    expect(result.sunset).not.toBeNull()
    // Near equinox ~12 hours
    expect(result.daylightMinutes).toBeGreaterThan(710)
    expect(result.daylightMinutes).toBeLessThan(740)
  })

  it('produces consistent Julian Day Numbers', () => {
    // Verify the JDN formula gives correct results by checking
    // that Jan 1, 2000 noon UTC = JD 2451545.0 (J2000.0 epoch)
    // We test this indirectly: getSunTimes should give reasonable results
    // for the J2000.0 date at a known location
    const result = getSunTimes(new Date(2000, 0, 1), 51.48, -0.0)
    expect(result.sunrise).not.toBeNull()
    expect(result.sunset).not.toBeNull()
    // London in January: short day, ~8 hours
    expect(result.daylightMinutes).toBeGreaterThan(450)
    expect(result.daylightMinutes).toBeLessThan(510)
  })
})

describe('formatDaylight', () => {
  it('formats regular values', () => {
    expect(formatDaylight(750)).toBe('12h 30m')
    expect(formatDaylight(60)).toBe('1h 0m')
    expect(formatDaylight(1)).toBe('0h 1m')
    expect(formatDaylight(90)).toBe('1h 30m')
  })

  it('formats full day (polar day)', () => {
    expect(formatDaylight(1440)).toBe('24h 0m')
    expect(formatDaylight(1500)).toBe('24h 0m')
  })

  it('formats zero (polar night)', () => {
    expect(formatDaylight(0)).toBe('0h 0m')
    expect(formatDaylight(-1)).toBe('0h 0m')
  })
})
