// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor, act } from '@testing-library/react'
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
  'transit:walkBadge': '{{minutes}} min',
  'transit:walkBadgeTitle': '{{minutes}} min walking time subtracted',
  'transit:leaveNow': 'Leave now',
  'transit:departed_one': '{{count}} departed',
  'transit:departed_other': '{{count}} departed',
  'transit:showSettings': 'Configure stops',
  'transit:hideSettings': 'Hide settings',
  'common:actions.refresh': 'Refresh',
}

// Module-level (stable) so the `t` in Transit's effect deps never changes identity.
function translate(key: string, opts?: Record<string, unknown>): string {
  // Mirror i18next's plural suffix resolution for keys passed a `count`.
  const lookup =
    opts?.count !== undefined && !(key in TRANSLATIONS)
      ? key + (Number(opts.count) === 1 ? '_one' : '_other')
      : key
  const template = TRANSLATIONS[lookup] ?? key
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

function departureFetchCount(): number {
  return fetchMock.mock.calls.filter(([url]) =>
    String(url).startsWith('/api/transit/departures')
  ).length
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

    const badge = await screen.findByLabelText('5 min walking time subtracted')
    expect(badge).toHaveTextContent('5 min')
  })

  it('renders no badge and the raw minutes when the offset is zero', async () => {
    mockDepartures([stopPayload(0, [departureIn(12)])])
    render(<Transit />)

    expect(await screen.findByText('12 min')).toBeInTheDocument()
    expect(screen.queryByLabelText(/walking time subtracted/)).not.toBeInTheDocument()
  })

  it('renders identically when the payload omits walk_minutes entirely', async () => {
    mockDepartures([stopPayload(undefined, [departureIn(12)])])
    render(<Transit />)

    expect(await screen.findByText('12 min')).toBeInTheDocument()
    expect(screen.queryByLabelText(/walking time subtracted/)).not.toBeInTheDocument()
  })

  it('keeps the delay indicator visible at every breakpoint alongside a walk badge', async () => {
    mockDepartures([stopPayload(5, [departureIn(12, { delay_minutes: 2 })])])
    render(<Transit />)

    const delay = await screen.findByText('+2 min late')
    // No responsive hiding — the delay is as relevant on mobile as on desktop.
    expect(delay.className).not.toMatch(/hidden/)
  })

  it('shows "leave now" while the offset has all but consumed the time', async () => {
    // 5.4 min out with a 5 min walk leaves ~24s, which rounds down to zero.
    mockDepartures([stopPayload(5, [departureIn(5.4)])])
    render(<Transit />)

    expect(await screen.findByText('Leave now')).toBeInTheDocument()
  })

  it('drops a departure the walk no longer fits instead of showing a negative number', async () => {
    mockDepartures([stopPayload(10, [departureIn(3)])])
    render(<Transit />)

    expect(await screen.findByText('No upcoming departures')).toBeInTheDocument()
    expect(screen.queryByText(/-\d+\s*min/)).not.toBeInTheDocument()
    expect(screen.queryByText('Sentrum')).not.toBeInTheDocument()
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

// ── Self-pruning list ─────────────────────────────────────────────────────────

async function flushMicrotasks() {
  for (let i = 0; i < 5; i++) {
    await Promise.resolve()
  }
}

/** Let the initial fetch resolve and render under fake timers. */
async function renderAndSettle() {
  render(<Transit />)
  await act(async () => {
    await flushMicrotasks()
  })
}

async function advance(ms: number) {
  await act(async () => {
    vi.advanceTimersByTime(ms)
    await flushMicrotasks()
  })
}

describe('Transit departed-row pruning', () => {
  beforeEach(() => {
    vi.useFakeTimers({
      toFake: ['setInterval', 'clearInterval', 'setTimeout', 'clearTimeout', 'Date'],
    })
    vi.setSystemTime(new Date('2026-08-05T12:00:00Z'))
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('removes a departure on the next tick once its leave time passes', async () => {
    // 10m01s out with a 10 min walk: one second of slack left.
    mockDepartures([
      stopPayload(10, [
        departureIn(10 + 1 / 60, { line: '3', destination: 'Sentrum' }),
        departureIn(25, { line: '4', destination: 'Nord' }),
      ]),
    ])
    await renderAndSettle()

    expect(screen.getByText('Sentrum')).toBeInTheDocument()

    // Two seconds later the leave time has passed — no poll involved.
    await advance(2_000)

    expect(screen.queryByText('Sentrum')).not.toBeInTheDocument()
    expect(screen.getByText('Nord')).toBeInTheDocument()
  })

  it('keeps showing "0 min" without an offset until the departure itself passes', async () => {
    mockDepartures([stopPayload(0, [departureIn(20 / 60)])])
    await renderAndSettle()

    expect(screen.getByText('0 min')).toBeInTheDocument()

    // Still in the final minute — the row must stay put.
    await advance(10_000)
    expect(screen.getByText('0 min')).toBeInTheDocument()

    // Past the departure timestamp — now it goes.
    await advance(15_000)
    expect(screen.queryByText('0 min')).not.toBeInTheDocument()
  })

  it('shows a departed count for the rows it pruned', async () => {
    mockDepartures([
      stopPayload(10, [
        departureIn(3, { line: '1', destination: 'Gone A' }),
        departureIn(4, { line: '2', destination: 'Gone B' }),
        departureIn(25, { line: '4', destination: 'Nord' }),
      ]),
    ])
    await renderAndSettle()

    expect(screen.getByText('2 departed')).toBeInTheDocument()
    expect(screen.getByText('Nord')).toBeInTheDocument()
  })

  it('falls back to the empty state when every departure is pruned', async () => {
    mockDepartures([
      stopPayload(10, [
        departureIn(3, { line: '1', destination: 'Gone A' }),
        departureIn(4, { line: '2', destination: 'Gone B' }),
      ]),
    ])
    await renderAndSettle()

    expect(screen.getByText('No upcoming departures')).toBeInTheDocument()
    expect(screen.getByText('2 departed')).toBeInTheDocument()
  })

  it('fires at most one off-cycle refresh across a burst of ticks', async () => {
    mockDepartures([
      stopPayload(10, [
        departureIn(3, { line: '1', destination: 'Gone A' }),
        departureIn(25, { line: '4', destination: 'Nord' }),
      ]),
    ])
    await renderAndSettle()

    // Initial poll plus exactly one off-cycle refresh for the short list.
    expect(departureFetchCount()).toBe(2)

    // Five more ticks, all inside the 10s rate-limit window.
    for (let i = 0; i < 5; i++) await advance(1_000)

    expect(departureFetchCount()).toBe(2)
  })
})
