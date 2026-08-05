// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, within, fireEvent } from '@testing-library/react'
import type { TFunction } from 'i18next'
import {
  buildDailyForecasts,
  formatTimeAgo,
  selectCurrentIndex,
  selectUpcoming,
  type TimeseriesEntry,
} from '../lib/weatherForecast'
import { formatDate } from '../utils/formatDate'
import weatherEn from '../../public/locales/en/weather.json'
import weatherNb from '../../public/locales/nb/weather.json'
import weatherTh from '../../public/locales/th/weather.json'
import Weather from './Weather'

// ── Translation ───────────────────────────────────────────────────────────────
// Translate against the real locale bundles rather than a hand-written map, so a
// missing or renamed key fails the test instead of silently falling through.

type Bundle = Record<string, unknown>

function lookup(bundle: Bundle, key: string): unknown {
  return key
    .split('.')
    .reduce<unknown>((acc, part) => (acc as Bundle | undefined)?.[part], bundle)
}

/** Minimal stand-in for i18next: `_one`/`_other` plurals plus `{{var}}` interpolation. */
function makeT(bundle: Bundle) {
  return (key: string, opts?: Record<string, unknown>): string => {
    const count = opts?.count
    const plural =
      typeof count === 'number' ? lookup(bundle, `${key}_${count === 1 ? 'one' : 'other'}`) : undefined
    const value = plural ?? lookup(bundle, key)
    if (typeof value !== 'string') return (opts?.defaultValue as string | undefined) ?? key

    return Object.entries(opts ?? {}).reduce<string>(
      (acc, [name, replacement]) => acc.replaceAll(`{{${name}}}`, String(replacement)),
      value,
    )
  }
}

const tEn = makeT(weatherEn as Bundle)

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: tEn, i18n: { language: 'en' } }),
  initReactI18next: { type: '3rdParty', init: () => {} },
}))

// ── Page dependencies ─────────────────────────────────────────────────────────

vi.mock('../auth', () => ({ useAuth: () => ({ user: { id: 1 }, loading: false }) }))

vi.mock('../components/LocationSearch', () => ({ default: () => null }))
vi.mock('../components/UseMyLocationButton', () => ({ default: () => null }))

const OSLO = { name: 'Oslo', lat: 59.9139, lon: 10.7522 }

vi.mock('../hooks/useWeatherLocation', () => ({
  useWeatherLocation: () => ({
    location: OSLO,
    recents: [OSLO],
    knownLocations: [OSLO],
    displayRecents: [OSLO],
    otherCities: [],
    locationResolved: true,
    userHasSelected: false,
    selectByName: () => {},
    selectLocation: () => {},
  }),
}))

vi.mock('../hooks/useSunTimes', () => ({ useSunTimes: () => null }))

interface ForecastMock {
  data: { properties: { timeseries: TimeseriesEntry[] } } | null
  loading: boolean
  error: boolean
  errorMessage: string | null
  lastUpdated: Date | null
}

const forecastMock: ForecastMock = {
  data: null,
  loading: false,
  error: false,
  errorMessage: null,
  lastUpdated: null,
}

vi.mock('../hooks/useForecast', () => ({
  useForecast: () => ({ ...forecastMock, refresh: () => {} }),
}))

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

  it('parses production RFC3339 Z timestamps and groups by local calendar date', () => {
    // yr.no returns UTC timestamps with trailing Z. Verify that parsing
    // and local-calendar grouping work with the production format.
    const series = [
      entry('2026-07-15T12:00:00Z', 'rain'),
      entry('2026-07-13T12:00:00Z', 'cloudy'),
      entry('2026-07-13T09:00:00Z', 'clearsky_day'),
      entry('2026-07-14T11:00:00Z', 'fair_day'),
    ]

    const days = buildDailyForecasts(series, 'Today')

    // Three UTC days may become 3 or 4 local days depending on timezone offset
    expect(days.length).toBeGreaterThanOrEqual(3)
    expect(days.length).toBeLessThanOrEqual(4)
    // Must be chronologically sorted regardless of input order
    for (let i = 1; i < days.length; i++) {
      expect(days[i].date > days[i - 1].date).toBe(true)
    }
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

describe('selectCurrentIndex', () => {
  const series = [
    entry('2026-06-10T12:00:00'),
    entry('2026-06-10T13:00:00'),
    entry('2026-06-10T14:00:00'),
    entry('2026-06-10T15:00:00'),
  ]

  it('returns -1 for an empty series', () => {
    expect(selectCurrentIndex([], new Date('2026-06-10T14:20:00').getTime())).toBe(-1)
  })

  it('picks the entry 20 minutes in the past over the nearer-in-index next hour', () => {
    const now = new Date('2026-06-10T14:20:00').getTime()
    expect(selectCurrentIndex(series, now)).toBe(2)
  })

  it('rolls over to the next hour past the half-hour mark', () => {
    const now = new Date('2026-06-10T14:40:00').getTime()
    expect(selectCurrentIndex(series, now)).toBe(3)
  })

  it('never picks index 0 just because the cache is old', () => {
    // Every entry is hours in the past: the last one is still the closest.
    const now = new Date('2026-06-11T09:00:00').getTime()
    expect(selectCurrentIndex(series, now)).toBe(series.length - 1)
  })

  it('picks the first entry when the cache is somehow ahead of the clock', () => {
    const now = new Date('2026-06-10T06:00:00').getTime()
    expect(selectCurrentIndex(series, now)).toBe(0)
  })
})

describe('selectUpcoming', () => {
  const series = [
    entry('2026-06-10T12:00:00'),
    entry('2026-06-10T13:00:00'),
    entry('2026-06-10T14:00:00'),
    entry('2026-06-10T15:00:00'),
  ]

  it('keeps the current hour and drops the ones that fully elapsed', () => {
    const now = new Date('2026-06-10T14:20:00').getTime()
    expect(selectUpcoming(series, now).map((e) => e.time)).toEqual([
      '2026-06-10T14:00:00',
      '2026-06-10T15:00:00',
    ])
  })

  it('returns everything when the forecast starts in the future', () => {
    const now = new Date('2026-06-10T06:00:00').getTime()
    expect(selectUpcoming(series, now)).toHaveLength(4)
  })

  it('returns nothing when the whole series is in the past', () => {
    const now = new Date('2026-06-11T09:00:00').getTime()
    expect(selectUpcoming(series, now)).toEqual([])
  })

  it('handles an empty series', () => {
    expect(selectUpcoming([], Date.now())).toEqual([])
  })
})

describe('formatTimeAgo', () => {
  const now = new Date('2026-06-10T14:00:00').getTime()
  const ago = (ms: number) => new Date(now - ms)

  const MINUTE = 60_000
  const HOUR = 60 * MINUTE
  const DAY = 24 * HOUR

  it('reads "just now" under a minute', () => {
    expect(formatTimeAgo(ago(30_000), tEn as unknown as TFunction<'weather'>, now)).toBe(
      'Updated just now',
    )
  })

  it('reads minutes under an hour', () => {
    const t = tEn as unknown as TFunction<'weather'>
    expect(formatTimeAgo(ago(MINUTE), t, now)).toBe('Updated 1 min ago')
    expect(formatTimeAgo(ago(45 * MINUTE), t, now)).toBe('Updated 45 min ago')
    expect(formatTimeAgo(ago(59 * MINUTE), t, now)).toBe('Updated 59 min ago')
  })

  it('reads hours under a day, singular and plural', () => {
    const t = tEn as unknown as TFunction<'weather'>
    expect(formatTimeAgo(ago(HOUR), t, now)).toBe('Updated 1 hour ago')
    expect(formatTimeAgo(ago(5 * HOUR), t, now)).toBe('Updated 5 hours ago')
    expect(formatTimeAgo(ago(23 * HOUR), t, now)).toBe('Updated 23 hours ago')
  })

  it('reads days beyond 24 hours instead of 1440 minutes', () => {
    const t = tEn as unknown as TFunction<'weather'>
    expect(formatTimeAgo(ago(DAY), t, now)).toBe('Updated 1 day ago')
    expect(formatTimeAgo(ago(3 * DAY), t, now)).toBe('Updated 3 days ago')
  })

  it('resolves every tier in all three locales', () => {
    for (const bundle of [weatherEn, weatherNb, weatherTh] as Bundle[]) {
      const t = makeT(bundle) as unknown as TFunction<'weather'>
      for (const elapsed of [30_000, MINUTE, 45 * MINUTE, HOUR, 5 * HOUR, DAY, 3 * DAY]) {
        const text = formatTimeAgo(ago(elapsed), t, now)
        // A missing key would fall through to the raw key path.
        expect(text.startsWith('updated.')).toBe(false)
        expect(text).not.toContain('{{')
      }
    }
  })
})

// ── Page rendering ────────────────────────────────────────────────────────────

function setForecast(timeseries: TimeseriesEntry[], overrides: Partial<ForecastMock> = {}) {
  Object.assign(forecastMock, {
    data: { properties: { timeseries } },
    loading: false,
    error: false,
    errorMessage: null,
    lastUpdated: null,
    ...overrides,
  })
}

function sectionFor(headingText: string, label: string): HTMLElement {
  const section = screen.getByText(headingText).closest('section')
  if (!section) throw new Error(`${label} not found`)
  return section
}

function currentCard(): HTMLElement {
  return sectionFor(tEn('page.rightNowIn', { location: 'Oslo' }), 'current conditions card')
}

/**
 * The hour-by-hour strip. The 7-day cards below it summarise the *whole* cached
 * series by design, so past temperatures legitimately survive there — staleness
 * assertions have to be scoped to the strip.
 */
function hourlyStrip(): HTMLElement {
  return sectionFor(tEn('page.nextHours', { count: 12 }), 'hourly strip')
}

/** Same formatting HourlyStrip uses for its midnight separator. */
function dateSeparatorLabel(iso: string): string {
  return formatDate(new Date(iso), { weekday: 'short', month: 'short', day: 'numeric' })
}

describe('Weather page', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    Object.assign(forecastMock, {
      data: null,
      loading: false,
      error: false,
      errorMessage: null,
      lastUpdated: null,
    })
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('shows the entry nearest to now, not the first cached one', () => {
    vi.setSystemTime(new Date('2026-06-10T14:20:00'))
    setForecast([
      entry('2026-06-10T12:00:00', 'rain', { temp: -8 }),
      entry('2026-06-10T13:00:00', 'rain', { temp: -7 }),
      entry('2026-06-10T14:00:00', 'cloudy', { temp: 14 }),
      entry('2026-06-10T15:00:00', 'cloudy', { temp: 15 }),
    ])

    render(<Weather />)

    expect(within(currentCard()).getByText('14°')).toBeTruthy()
    // The elapsed hours are gone from the strip.
    const strip = within(hourlyStrip())
    expect(strip.queryByText('-8°')).toBeNull()
    expect(strip.queryByText('-7°')).toBeNull()
    expect(strip.getByText('14°')).toBeTruthy()
    expect(strip.getByText('15°')).toBeTruthy()
  })

  it('starts the hourly strip at the current hour and keeps date separators correct', () => {
    // 00:00 is the first upcoming entry, so it must not draw a "new day" separator.
    vi.setSystemTime(new Date('2026-06-11T00:30:00'))
    setForecast([
      entry('2026-06-10T22:00:00', 'cloudy', { temp: -21 }),
      entry('2026-06-10T23:00:00', 'cloudy', { temp: -22 }),
      entry('2026-06-11T00:00:00', 'cloudy', { temp: 1 }),
      entry('2026-06-11T01:00:00', 'cloudy', { temp: 2 }),
    ])

    render(<Weather />)

    const strip = within(hourlyStrip())
    expect(strip.queryByText('-21°')).toBeNull()
    expect(strip.queryByText('-22°')).toBeNull()
    expect(strip.getByText('1°')).toBeTruthy()
    expect(strip.getByText('2°')).toBeTruthy()
    expect(strip.queryByText(dateSeparatorLabel('2026-06-11T00:00:00'))).toBeNull()
  })

  it('still draws the separator when the retained hours cross midnight', () => {
    vi.setSystemTime(new Date('2026-06-10T22:20:00'))
    setForecast([
      entry('2026-06-10T20:00:00', 'cloudy', { temp: -21 }),
      entry('2026-06-10T22:00:00', 'cloudy', { temp: 22 }),
      entry('2026-06-10T23:00:00', 'cloudy', { temp: 23 }),
      entry('2026-06-11T00:00:00', 'cloudy', { temp: 1 }),
    ])

    render(<Weather />)

    const strip = within(hourlyStrip())
    expect(strip.queryByText('-21°')).toBeNull()
    expect(strip.getAllByText(dateSeparatorLabel('2026-06-11T00:00:00'))).toHaveLength(1)
  })

  it('falls back to the last entry and reports no upcoming hours for a fully elapsed cache', () => {
    vi.setSystemTime(new Date('2026-06-13T09:00:00'))
    setForecast(
      [
        entry('2026-06-10T12:00:00', 'cloudy', { temp: 12 }),
        entry('2026-06-10T13:00:00', 'cloudy', { temp: 13 }),
      ],
      { error: true, errorMessage: 'Failed to fetch forecast', lastUpdated: new Date('2026-06-10T13:00:00') },
    )

    render(<Weather />)

    expect(within(currentCard()).getByText('13°')).toBeTruthy()
    expect(screen.getByText(tEn('page.noUpcomingHours'))).toBeTruthy()
    expect(screen.getByText(tEn('stale.chip'))).toBeTruthy()
    expect(within(currentCard()).getByText('Updated 2 days ago')).toBeTruthy()
  })

  it('shows a dismissible cached-data chip while a forecast is on screen', () => {
    vi.setSystemTime(new Date('2026-06-10T14:20:00'))
    setForecast([entry('2026-06-10T14:00:00', 'cloudy', { temp: 14 })], {
      error: true,
      errorMessage: 'Failed to fetch forecast',
    })

    const { rerender } = render(<Weather />)

    expect(screen.getByText(tEn('stale.chip'))).toBeTruthy()
    // The red banner is for the no-data case only.
    expect(screen.queryByText('Failed to fetch forecast')).toBeNull()

    fireEvent.click(screen.getByLabelText(tEn('stale.dismiss')))
    expect(screen.queryByText(tEn('stale.chip'))).toBeNull()

    // A recovered refresh followed by a new failure brings the chip back.
    Object.assign(forecastMock, { error: false, errorMessage: null })
    rerender(<Weather />)
    expect(screen.queryByText(tEn('stale.chip'))).toBeNull()

    Object.assign(forecastMock, { error: true, errorMessage: 'Failed to fetch forecast' })
    rerender(<Weather />)
    expect(screen.getByText(tEn('stale.chip'))).toBeTruthy()
  })

  it('keeps the red banner and hides the chip when there is nothing cached to show', () => {
    vi.setSystemTime(new Date('2026-06-10T14:20:00'))
    Object.assign(forecastMock, {
      data: null,
      loading: false,
      error: true,
      errorMessage: 'Failed to fetch forecast',
      lastUpdated: null,
    })

    render(<Weather />)

    expect(screen.getByText('Failed to fetch forecast')).toBeTruthy()
    expect(screen.queryByText(tEn('stale.chip'))).toBeNull()
  })

  it('shows no chip when a fresh forecast loaded successfully', () => {
    vi.setSystemTime(new Date('2026-06-10T14:20:00'))
    setForecast([entry('2026-06-10T14:00:00', 'cloudy', { temp: 14 })], {
      lastUpdated: new Date('2026-06-10T14:19:00'),
    })

    render(<Weather />)

    expect(screen.queryByText(tEn('stale.chip'))).toBeNull()
    expect(within(currentCard()).getByText('Updated 1 min ago')).toBeTruthy()
  })
})
