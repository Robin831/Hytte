// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { act, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import WorkHoursPage from './WorkHoursPage'
import enWorkhours from '../../public/locales/en/workhours.json'
import type { DaySummary, WorkSession } from './workhours/types'

// ── Translation helper ────────────────────────────────────────────────────────

type JsonValue = string | number | boolean | null | JsonObject | JsonValue[]
interface JsonObject { [key: string]: JsonValue }

function makeT(translations: JsonObject) {
  return function t(key: string, vars?: Record<string, string>): string {
    const dotKey = key.includes(':') ? key.split(':').slice(1).join('.') : key
    let val: JsonValue | undefined = translations
    for (const part of dotKey.split('.')) {
      val = val && typeof val === 'object' && !Array.isArray(val) ? (val as JsonObject)[part] : undefined
    }
    if (typeof val !== 'string') return key
    if (!vars) return val
    return val.replace(/\{\{(\w+)\}\}/g, (_, k) => vars[k] ?? '')
  }
}

const stableT = makeT(enWorkhours as unknown as JsonObject)

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: stableT,
    i18n: { language: 'en' },
  }),
  Trans: ({ i18nKey }: { i18nKey: string }) => i18nKey,
  initReactI18next: { type: '3rdParty', init: () => {} },
}))

vi.mock('../utils/formatDate', () => ({
  formatDate: (value: string) => {
    const [y, m, d] = String(value).split('T')[0].split('-')
    return `${d}.${m}.${y}`
  },
  formatTime: () => '14:00',
  toLocalDateString: () => '2026-04-17',
}))

// ── Fixtures ──────────────────────────────────────────────────────────────────

const TODAY = '2026-04-17'
const FIXED_NOW = new Date('2026-04-17T12:00:00')

function session(id: number, start: string, end: string): WorkSession {
  return { id, day_id: 1, start_time: start, end_time: end, sort_order: id, is_internal: false, crosses_midnight: false }
}

function serverSummary(overrides: Partial<DaySummary>): DaySummary {
  return {
    date: TODAY,
    gross_minutes: 0,
    lunch_minutes: 0,
    deduction_minutes: 0,
    net_minutes: 0,
    reported_minutes: 0,
    reported_hours: 0,
    remainder_minutes: 0,
    standard_minutes: 450,
    balance_minutes: 0,
    ...overrides,
  }
}

const PREFS = { work_hours_standard_day: '450', work_hours_lunch_minutes: '30', work_hours_rounding: '30' }

// `prefs` controls the preferences call: an object answers with it, 'pending'
// never settles and 'error' answers 500.
function buildFetch(opts: {
  sessions: WorkSession[]
  lunch?: boolean
  summary: DaySummary
  prefs: Record<string, string> | 'pending' | 'error'
  punch?: { start_time: string; date: string } | null
}) {
  const defaults: Record<string, unknown> = {
    '/api/workhours/presets': { presets: [] },
    '/api/workhours/punch-session': { session: opts.punch ?? null },
    '/api/workhours/flex': { flex: { total_minutes: 0, to_next_interval: 0 }, reset_date: '2026-01-01', days_in_pool: 0 },
    '/api/workhours/leave': { leave_days: [], balance: { total: 0, used: 0, remaining: 0 } },
    '/api/workhours/day': {
      day: { id: 1, user_id: 1, date: TODAY, lunch: opts.lunch ?? false, notes: '', created_at: '', sessions: opts.sessions, deductions: [] },
      summary: opts.summary,
    },
  }
  const respond = (status: number, body: unknown) => Promise.resolve({
    ok: status >= 200 && status < 300,
    status,
    json: () => Promise.resolve(body),
  } as Response)

  return vi.fn((url: string) => {
    const path = url.toString().split('?')[0]
    if (path === '/api/settings/preferences') {
      if (opts.prefs === 'pending') return new Promise<Response>(() => {})
      if (opts.prefs === 'error') return respond(500, { error: 'boom' })
      return respond(200, { preferences: opts.prefs })
    }
    const body = defaults[path] ?? null
    return respond(body === null ? 404 : 200, body)
  })
}

function renderPage() {
  return render(
    <MemoryRouter>
      <WorkHoursPage />
    </MemoryRouter>,
  )
}

// Value shown next to a label in the Summary card.
async function summaryValue(label: string): Promise<string> {
  const card = (await screen.findByText('Summary')).closest('section')
  expect(card).not.toBeNull()
  const labelEl = within(card as HTMLElement).getByText(label)
  return labelEl.nextElementSibling?.textContent ?? ''
}

// ── Tests ─────────────────────────────────────────────────────────────────────

describe('WorkHoursPage day summary', () => {
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(FIXED_NOW)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.useRealTimers()
  })

  it('credits redeemed flex into the recomputed reported and balance, like the server', async () => {
    // 08:00-16:00 with lunch: gross 8:00, net 7:30, reported 7:30, balance 0,
    // then 60 redeemed minutes credited on top: reported 8:30, balance +1:00.
    // The server's gross is deliberately stale so the test can tell the
    // client recomputation has replaced it.
    vi.stubGlobal('fetch', buildFetch({
      sessions: [session(1, '08:00', '16:00')],
      lunch: true,
      summary: serverSummary({
        gross_minutes: 1,
        lunch_minutes: 30,
        net_minutes: 450,
        reported_minutes: 510,
        reported_hours: 8.5,
        balance_minutes: 60,
        redeemed_minutes: 60,
      }),
      prefs: PREFS,
    }))
    renderPage()

    await waitFor(async () => expect(await summaryValue('Gross')).toBe('8:00'))
    expect(await summaryValue('Net')).toBe('7:30')
    expect(await summaryValue('Reported')).toBe('8:30')
    expect(await summaryValue('Remainder')).toBe('0:00')
    expect(await summaryValue('Balance')).toBe('+1:00')
  })

  it('maps the recomputed totals onto the summary fields', async () => {
    // 08:00-16:10 without lunch, rounding 30: net 8:10, reported 8:00,
    // remainder 0:10, balance 8:00 - 7:30 = +0:30.
    vi.stubGlobal('fetch', buildFetch({
      sessions: [session(1, '08:00', '16:10')],
      summary: serverSummary({ gross_minutes: 1 }),
      prefs: PREFS,
    }))
    renderPage()

    await waitFor(async () => expect(await summaryValue('Gross')).toBe('8:10'))
    expect(await summaryValue('Net')).toBe('8:10')
    expect(await summaryValue('Reported')).toBe('8:00')
    expect(await summaryValue('Remainder')).toBe('0:10')
    expect(await summaryValue('Balance')).toBe('+0:30')
  })

  // A server summary that disagrees with what the client would compute, so
  // the assertions show which one is on screen.
  const distinctServer = serverSummary({
    gross_minutes: 600,
    net_minutes: 590,
    reported_minutes: 570,
    reported_hours: 9.5,
    remainder_minutes: 20,
    balance_minutes: 120,
  })

  it.each(['pending', 'error'] as const)('shows the server summary when preferences are %s', async prefs => {
    vi.stubGlobal('fetch', buildFetch({
      sessions: [session(1, '08:00', '16:00')],
      summary: distinctServer,
      prefs,
    }))
    renderPage()

    await waitFor(async () => expect(await summaryValue('Gross')).toBe('10:00'))
    expect(await summaryValue('Reported')).toBe('9:30')
    expect(await summaryValue('Remainder')).toBe('0:20')
    expect(await summaryValue('Balance')).toBe('+2:00')
  })

  it('falls back to the server summary when a stored session time is malformed', async () => {
    vi.stubGlobal('fetch', buildFetch({
      sessions: [session(1, '08:00', '16:00'), session(2, '17:xx', '18:00')],
      summary: distinctServer,
      prefs: PREFS,
    }))
    renderPage()

    // Let the preferences call settle so the override would have run; without
    // the malformed session the same setup shows the client totals (see above).
    await waitFor(() => expect(fetch).toHaveBeenCalledWith('/api/settings/preferences', expect.anything()))
    await act(async () => {})
    await waitFor(async () => expect(await summaryValue('Gross')).toBe('10:00'))
    expect(await summaryValue('Reported')).toBe('9:30')
    expect(await summaryValue('Balance')).toBe('+2:00')
  })
})

describe('WorkHoursPage live estimate with redeemed flex', () => {
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(FIXED_NOW)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.useRealTimers()
  })

  // Redeemed flex counts toward the standard day, matching the server's
  // balance: 4h worked (08:00-12:00) + 5h redeemed = 9:00 against a 7:30
  // standard, so the estimate reads +1:30 over standard even though the
  // worked time alone is short of it.
  it('counts redeemed flex toward the standard day', async () => {
    vi.stubGlobal('fetch', buildFetch({
      sessions: [],
      summary: serverSummary({ reported_minutes: 300, balance_minutes: -150, redeemed_minutes: 300 }),
      prefs: { ...PREFS, work_hours_lunch_minutes: '0' },
      punch: { start_time: '08:00', date: TODAY },
    }))
    renderPage()

    await waitFor(() => expect(screen.getByText('+1:30 over standard')).toBeInTheDocument())
    const section = screen.getByText('If punched out now').closest('section')
    expect(section?.className).toContain('bg-green-900')
    expect(within(section as HTMLElement).getByText('9:00')).toBeInTheDocument()
  })

  it('stays below standard without a redemption', async () => {
    vi.stubGlobal('fetch', buildFetch({
      sessions: [],
      summary: serverSummary({ balance_minutes: -450 }),
      prefs: { ...PREFS, work_hours_lunch_minutes: '0' },
      punch: { start_time: '08:00', date: TODAY },
    }))
    renderPage()

    await waitFor(() => expect(screen.getByText('If punched out now')).toBeInTheDocument())
    const section = screen.getByText('If punched out now').closest('section')
    expect(section?.className).not.toContain('bg-green-900')
    expect(screen.queryByText(/over standard/)).not.toBeInTheDocument()
  })
})

describe('WorkHoursPage flex redeem', () => {
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(FIXED_NOW)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.useRealTimers()
  })

  // buildFetch with a 45-minute pool and the redeem POST answered by `redeem`.
  function redeemFetch(redeem: { status: number; body: unknown }) {
    const base = buildFetch({ sessions: [], summary: serverSummary({}), prefs: PREFS })
    const flexBody = { flex: { total_minutes: 45, to_next_interval: 15 }, reset_date: '2026-01-01', days_in_pool: 1 }
    return vi.fn((url: string, _init?: RequestInit) => {
      const path = url.toString().split('?')[0]
      if (path === '/api/workhours/flex/redeem') {
        return Promise.resolve({
          ok: redeem.status >= 200 && redeem.status < 300,
          status: redeem.status,
          json: () => Promise.resolve(redeem.body),
        } as Response)
      }
      if (path === '/api/workhours/flex') {
        return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve(flexBody) } as Response)
      }
      return base(url)
    })
  }

  const callsTo = (fetchMock: ReturnType<typeof redeemFetch>, path: string) =>
    fetchMock.mock.calls.filter(([url]) => url.toString().split('?')[0] === path)

  it('redeems onto the viewed day, then reloads the pool and the day', async () => {
    const fetchMock = redeemFetch({ status: 200, body: {} })
    vi.stubGlobal('fetch', fetchMock)
    const alertMock = vi.fn()
    vi.stubGlobal('alert', alertMock)
    renderPage()

    const button = await screen.findByRole('button', { name: 'Redeem 30 min' })
    await waitFor(() => expect(callsTo(fetchMock, '/api/workhours/day')).toHaveLength(1))
    expect(callsTo(fetchMock, '/api/workhours/flex')).toHaveLength(1)

    await act(async () => { button.click() })

    const posts = callsTo(fetchMock, '/api/workhours/flex/redeem')
    expect(posts).toHaveLength(1)
    expect(JSON.parse(posts[0][1]?.body as string)).toEqual({ date: TODAY })
    await waitFor(() => expect(callsTo(fetchMock, '/api/workhours/flex')).toHaveLength(2))
    await waitFor(() => expect(callsTo(fetchMock, '/api/workhours/day')).toHaveLength(2))
    expect(callsTo(fetchMock, '/api/workhours/day')[1][0]).toContain(`date=${TODAY}`)
    expect(alertMock).not.toHaveBeenCalled()
  })

  it('alerts the server error and reloads neither the pool nor the day', async () => {
    const fetchMock = redeemFetch({ status: 400, body: { error: 'not enough flex' } })
    vi.stubGlobal('fetch', fetchMock)
    const alertMock = vi.fn()
    vi.stubGlobal('alert', alertMock)
    renderPage()

    const button = await screen.findByRole('button', { name: 'Redeem 30 min' })
    await waitFor(() => expect(callsTo(fetchMock, '/api/workhours/day')).toHaveLength(1))

    await act(async () => { button.click() })

    await waitFor(() => expect(alertMock).toHaveBeenCalledWith('not enough flex'))
    expect(callsTo(fetchMock, '/api/workhours/flex')).toHaveLength(1)
    expect(callsTo(fetchMock, '/api/workhours/day')).toHaveLength(1)
    expect(button).not.toBeDisabled()
  })
})
