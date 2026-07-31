// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import Transit from '../Transit'

// ── Translation mock ──────────────────────────────────────────────────────────
// Keys are returned with interpolation applied so assertions can target stable
// strings without depending on the real locale files.
const TRANSLATIONS: Record<string, string> = {
  'transit:title': 'Departures',
  'transit:loading': 'Loading departures...',
  'transit:error': 'Failed to load departures',
  'transit:noDepartures': 'No upcoming departures',
  'transit:realtime': 'Real-time',
  'transit:scheduled': 'Scheduled',
  'transit:min': 'min',
  'transit:delayed': '+{{minutes}} min late',
  'transit:walkMinutes': 'Walking time (min)',
  'transit:walkMinutesHint': 'Minutes it takes you to walk to this stop.',
  'transit:walkBadge': '{{minutes}} min walk',
  'transit:walkBadgeTitle': '{{minutes}} min walking time subtracted',
  'transit:leaveNow': 'Leave now',
  'transit:missed': 'Missed',
  'transit:showSettings': 'Configure stops',
  'transit:hideSettings': 'Hide settings',
  'common:actions.refresh': 'Refresh',
}

// Module-level (stable) so the `t` in Transit's effect deps never changes identity.
function translate(key: string, opts?: Record<string, unknown>): string {
  const template = TRANSLATIONS[key] ?? key
  if (!opts) return template
  return template.replace(/\{\{(\w+)\}\}/g, (_, name: string) =>
    name in opts ? String(opts[name]) : `{{${name}}}`
  )
}

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: translate,
    i18n: { language: 'en', changeLanguage: () => {} },
  }),
  initReactI18next: { type: '3rdParty', init: () => {} },
}))

// ── Fixtures ──────────────────────────────────────────────────────────────────

/** A departure `minutes` from now, so relative labels are deterministic. */
function departureIn(minutes: number, overrides: Record<string, unknown> = {}) {
  return {
    line: '3',
    destination: 'Sentrum',
    departure_time: new Date(Date.now() + minutes * 60_000).toISOString(),
    is_realtime: true,
    delay_minutes: 0,
    ...overrides,
  }
}

/** Passing `undefined` omits walk_minutes entirely, mimicking a legacy payload. */
function stopPayload(walkMinutes: number | undefined, departures: unknown[]) {
  const stop: Record<string, unknown> = {
    stop_id: 'NSR:StopPlace:1',
    stop_name: 'Bjørndalsbakken',
    departures,
  }
  if (walkMinutes !== undefined) stop.walk_minutes = walkMinutes
  return stop
}

function jsonResponse(body: unknown, ok = true): Response {
  return {
    ok,
    json: async () => body,
    text: async () => JSON.stringify(body),
  } as unknown as Response
}

const fetchMock = vi.fn()

function mockDepartures(stops: unknown[]) {
  fetchMock.mockImplementation((url: string) => {
    if (url.startsWith('/api/transit/departures')) {
      return Promise.resolve(jsonResponse({ stops }))
    }
    if (url.startsWith('/api/transit/settings')) {
      return Promise.resolve(jsonResponse({ stops: [] }))
    }
    return Promise.reject(new Error(`unexpected url: ${url}`))
  })
}

/** The flex container wrapping a single departure. */
function rowFor(text: string): HTMLElement {
  const row = screen.getByText(text).closest('div.flex')
  if (!row) throw new Error(`no departure row found for "${text}"`)
  return row as HTMLElement
}

beforeEach(() => {
  fetchMock.mockReset()
  vi.stubGlobal('fetch', fetchMock)
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('Transit walking offset', () => {
  it('subtracts the walking offset from the departure time', async () => {
    mockDepartures([stopPayload(5, [departureIn(12)])])
    render(<Transit />)

    expect(await screen.findByText('7 min')).toBeInTheDocument()
    expect(screen.queryByText('12 min')).not.toBeInTheDocument()
  })

  it('shows the walk badge when an offset is configured', async () => {
    mockDepartures([stopPayload(5, [departureIn(12)])])
    render(<Transit />)

    expect(await screen.findByText('5 min walk')).toBeInTheDocument()
  })

  it('renders no badge and the raw minutes when the offset is zero', async () => {
    mockDepartures([stopPayload(0, [departureIn(12)])])
    render(<Transit />)

    expect(await screen.findByText('12 min')).toBeInTheDocument()
    expect(screen.queryByText(/min walk/)).not.toBeInTheDocument()
  })

  it('renders identically when the payload omits walk_minutes entirely', async () => {
    mockDepartures([stopPayload(undefined, [departureIn(12)])])
    render(<Transit />)

    expect(await screen.findByText('12 min')).toBeInTheDocument()
    expect(screen.queryByText(/min walk/)).not.toBeInTheDocument()
  })

  it('shows "leave now" and dims the row when the offset exactly consumes the time', async () => {
    mockDepartures([stopPayload(5, [departureIn(5)])])
    render(<Transit />)

    await screen.findByText('Leave now')
    expect(rowFor('Leave now')).toHaveClass('opacity-40')
  })

  it('shows "missed" instead of a negative number once the walk no longer fits', async () => {
    mockDepartures([stopPayload(10, [departureIn(3)])])
    render(<Transit />)

    expect(await screen.findByText('Missed')).toBeInTheDocument()
    expect(screen.queryByText(/-\d+\s*min/)).not.toBeInTheDocument()
    expect(rowFor('Missed')).toHaveClass('opacity-40')
  })

  it('keeps catchable rows undimmed', async () => {
    mockDepartures([stopPayload(5, [departureIn(12)])])
    render(<Transit />)

    await screen.findByText('7 min')
    expect(rowFor('7 min')).not.toHaveClass('opacity-40')
  })

  it('applies the offset on first load without fetching settings', async () => {
    mockDepartures([stopPayload(5, [departureIn(12)])])
    render(<Transit />)

    await screen.findByText('7 min')
    await waitFor(() => {
      expect(
        fetchMock.mock.calls.some(([url]) => String(url).startsWith('/api/transit/settings'))
      ).toBe(false)
    })
  })
})
