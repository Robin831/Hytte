// @vitest-environment happy-dom
import { describe, it, expect } from 'vitest'
import { buildDailyForecasts, type TimeseriesEntry } from '../lib/weatherForecast'

/**
 * Build a minimal timeseries entry. `time` is parsed with `new Date(time)`; using
 * naive local-time strings (no trailing `Z`/offset) keeps the local wall-clock hour
 * and calendar date deterministic regardless of the machine's timezone.
 */
function entry(
  time: string,
  symbol?: string,
  { temp = 10, wind = 2, precip = 0 }: { temp?: number; wind?: number; precip?: number } = {},
): TimeseriesEntry {
  return {
    time,
    data: {
      instant: {
        details: { air_temperature: temp, wind_speed: wind, relative_humidity: 50 },
      },
      ...(symbol
        ? { next_1_hours: { summary: { symbol_code: symbol }, details: { precipitation_amount: precip } } }
        : {}),
    },
  }
}

describe('buildDailyForecasts', () => {
  it('renders days in ascending date order regardless of entry order', () => {
    const series = [
      entry('2026-06-12T12:00:00', 'rain'),
      entry('2026-06-10T12:00:00', 'cloudy'),
      entry('2026-06-11T12:00:00', 'fair_day'),
    ]

    const days = buildDailyForecasts(series, 'Today')

    expect(days.map((d) => d.date)).toEqual(['2026-06-10', '2026-06-11', '2026-06-12'])
  })

  it('picks the symbol closest to 12:00 local time', () => {
    const series = [
      entry('2026-06-10T06:00:00', 'clearsky_day'),
      entry('2026-06-10T11:00:00', 'cloudy'),
      entry('2026-06-10T12:30:00', 'rain'), // closest to noon (750 vs 720)
      entry('2026-06-10T18:00:00', 'clearsky_night'),
    ]

    const days = buildDailyForecasts(series, 'Today')

    expect(days).toHaveLength(1)
    expect(days[0].symbolCode).toBe('rain')
  })

  it('breaks midday ties deterministically toward the earliest timestamp', () => {
    // 13:00 and 11:00 are equidistant from noon (60 min). Earliest (11:00) wins,
    // even though the later entry appears first in the input.
    const series = [
      entry('2026-06-10T13:00:00', 'later'),
      entry('2026-06-10T11:00:00', 'earlier'),
    ]

    const days = buildDailyForecasts(series, 'Today')

    expect(days[0].symbolCode).toBe('earlier')
  })

  it('uses a near/after-noon remaining hour when only afternoon data is left', () => {
    // Mimics the current day late in the afternoon: no morning/midnight entries remain.
    const series = [
      entry('2026-06-10T15:00:00', 'rain'), // closest remaining to noon
      entry('2026-06-10T18:00:00', 'cloudy'),
      entry('2026-06-10T21:00:00', 'clearsky_night'),
    ]

    const days = buildDailyForecasts(series, 'Today')

    expect(days[0].symbolCode).toBe('rain')
    expect(days[0].symbolCode).not.toBe('clearsky_night')
  })

  it('groups DST-straddling hours under the correct local calendar date and orders them', () => {
    // 2026-03-29 is the European spring-forward date (02:00 -> 03:00). All hours on
    // that wall-clock day must group under 2026-03-29 and stay ordered after the prior day.
    const series = [
      entry('2026-03-29T13:00:00', 'fair_day'),
      entry('2026-03-28T13:00:00', 'cloudy'),
      entry('2026-03-29T01:30:00', 'partlycloudy_night'),
      entry('2026-03-29T03:30:00', 'rain'),
    ]

    const days = buildDailyForecasts(series, 'Today')

    expect(days.map((d) => d.date)).toEqual(['2026-03-28', '2026-03-29'])
    // 13:00 is closest to noon on the 29th.
    expect(days[1].symbolCode).toBe('fair_day')
  })

  it('falls back to "cloudy" when a day has no usable symbol', () => {
    const series = [entry('2026-06-10T12:00:00', undefined)]

    const days = buildDailyForecasts(series, 'Today')

    expect(days[0].symbolCode).toBe('cloudy')
  })

  it('caps the result at the first 7 chronological days', () => {
    const series = Array.from({ length: 10 }, (_, i) =>
      entry(`2026-06-${String(20 - i).padStart(2, '0')}T12:00:00`, 'cloudy'),
    )

    const days = buildDailyForecasts(series, 'Today')

    expect(days).toHaveLength(7)
    expect(days.map((d) => d.date)).toEqual([
      '2026-06-11',
      '2026-06-12',
      '2026-06-13',
      '2026-06-14',
      '2026-06-15',
      '2026-06-16',
      '2026-06-17',
    ])
  })
})
