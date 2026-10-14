// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor, fireEvent, act } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import WorkHoursPage from './WorkHoursPage'

// Keys stand in for translations; the punch buttons are found by aria-label.
const stableT = (key: string) => key

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: stableT, i18n: { language: 'en' } }),
  Trans: ({ i18nKey }: { i18nKey: string }) => i18nKey,
  initReactI18next: { type: '3rdParty', init: () => {} },
}))

// The real module pulls in the i18n setup, whose HTTP backend would try to
// load locale files over the network.
vi.mock('../utils/formatDate', () => ({
  formatDate: (value: string) => String(value).split('T')[0],
  formatTime: () => '--:--',
  toLocalDateString: () => '2026-04-17',
}))

// ── Fetch mock ────────────────────────────────────────────────────────────────

const TODAY = '2026-04-17'
const YESTERDAY = '2026-04-16'

type Handler = (url: string, init?: RequestInit) => Promise<Response>

function jsonResponse(body: unknown, ok = true): Response {
  return { ok, json: () => Promise.resolve(body) } as Response
}

const json = (body: unknown): Handler => () => Promise.resolve(jsonResponse(body))

const emptyDay = json({ day: null, summary: null })

function dayWithSession(date: string, start: string, end: string) {
  return {
    date,
    day: {
      id: 1, user_id: 1, date, lunch: false, notes: '', created_at: '',
      sessions: [{ id: 7, day_id: 1, start_time: start, end_time: end, sort_order: 0, is_internal: false, crosses_midnight: false }],
      deductions: [],
    },
    summary: {
      date, gross_minutes: 60, lunch_minutes: 0, deduction_minutes: 0, net_minutes: 60,
      reported_minutes: 60, reported_hours: 1, remainder_minutes: 0, standard_minutes: 450, balance_minutes: -390,
    },
  }
}

// Routes fetch by "METHOD path"; unrouted requests fall back to sensible empties.
function stubFetch(routes: Record<string, Handler>) {
  const defaults: Record<string, Handler> = {
    'GET /api/workhours/presets': json({ presets: [] }),
    'GET /api/workhours/punch-session': json({ session: null }),
    'GET /api/settings/preferences': json({ preferences: {} }),
    'GET /api/workhours/flex': json({ flex: { total_minutes: 0, to_next_interval: 0 }, reset_date: '2026-01-01', days_in_pool: 0 }),
    'GET /api/workhours/day': emptyDay,
    'GET /api/workhours/leave': json({ leave_days: [], balance: { total: 0, used: 0, remaining: 0 } }),
    'POST /api/workhours/punch-in': json({}),
  }
  const fetchMock = vi.fn((url: string, init?: RequestInit) => {
    const path = url.toString().split('?')[0]
    const method = (init?.method ?? 'GET').toUpperCase()
    const key = `${method} ${path}`
    const handler = routes[key] ?? defaults[key]
    return handler ? handler(url.toString(), init) : Promise.resolve(jsonResponse(null, false))
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

function calls(fetchMock: ReturnType<typeof stubFetch>, method: string, path: string) {
  return fetchMock.mock.calls.filter(([url, init]) =>
    url.toString().split('?')[0] === path && (init?.method ?? 'GET').toUpperCase() === method,
  )
}

function renderPage() {
  return render(
    <MemoryRouter>
      <WorkHoursPage />
    </MemoryRouter>,
  )
}

async function pressP() {
  await act(async () => {
    fireEvent.keyDown(window, { key: 'p' })
  })
}

// ── Tests ─────────────────────────────────────────────────────────────────────

describe('WorkHoursPage punch clock', () => {
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(new Date(`${TODAY}T12:00:00`))
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.useRealTimers()
  })

  it('P punches in, then P again punches out', async () => {
    const fetchMock = stubFetch({
      'POST /api/workhours/punch-out': json(dayWithSession(TODAY, '12:00', '13:00')),
    })
    renderPage()
    await waitFor(() => expect(screen.getByLabelText('workhours:punchIn')).toBeInTheDocument())
    await waitFor(() => expect(calls(fetchMock, 'GET', '/api/workhours/punch-session')).toHaveLength(1))

    await pressP()
    await waitFor(() => expect(screen.getByLabelText('workhours:punchOut')).toBeInTheDocument())
    expect(calls(fetchMock, 'POST', '/api/workhours/punch-in')).toHaveLength(1)

    vi.setSystemTime(new Date(`${TODAY}T13:00:00`))
    await pressP()
    await waitFor(() => expect(screen.getByLabelText('workhours:punchIn')).toBeInTheDocument())
    expect(calls(fetchMock, 'POST', '/api/workhours/punch-in')).toHaveLength(1)
    expect(calls(fetchMock, 'POST', '/api/workhours/punch-out')).toHaveLength(1)
  })

  it('P from the week tab switches to the day view and punches in', async () => {
    const fetchMock = stubFetch({})
    renderPage()
    await waitFor(() => expect(calls(fetchMock, 'GET', '/api/workhours/punch-session')).toHaveLength(1))

    await act(async () => {
      fireEvent.keyDown(window, { key: '2' })
    })
    await waitFor(() => expect(screen.getByText('workhours:viewWeek').className).toContain('bg-gray-700'))
    expect(screen.queryByLabelText('workhours:punchIn')).not.toBeInTheDocument()

    await pressP()
    await waitFor(() => expect(screen.getByText('workhours:viewDay').className).toContain('bg-gray-700'))
    await waitFor(() => expect(screen.getByLabelText('workhours:punchOut')).toBeInTheDocument())
    expect(calls(fetchMock, 'POST', '/api/workhours/punch-in')).toHaveLength(1)
  })

  it('P waits while a day edit is saving', async () => {
    let resolveSaveDay: (r: Response) => void = () => {}
    const fetchMock = stubFetch({
      'PUT /api/workhours/day': () => new Promise<Response>(resolve => { resolveSaveDay = resolve }),
    })
    renderPage()
    await waitFor(() => expect(screen.getByLabelText('workhours:punchIn')).toBeInTheDocument())
    await waitFor(() => expect(calls(fetchMock, 'GET', '/api/workhours/punch-session')).toHaveLength(1))

    await act(async () => {
      fireEvent.click(screen.getByRole('checkbox', { name: 'workhours:lunch' }))
    })
    await waitFor(() => expect(screen.getByLabelText('workhours:punchIn')).toBeDisabled())

    await pressP()
    expect(calls(fetchMock, 'POST', '/api/workhours/punch-in')).toHaveLength(0)

    await act(async () => {
      resolveSaveDay(jsonResponse({ day: null, summary: null }))
    })
    await waitFor(() => expect(screen.getByLabelText('workhours:punchIn')).not.toBeDisabled())

    await pressP()
    await waitFor(() => expect(calls(fetchMock, 'POST', '/api/workhours/punch-in')).toHaveLength(1))
  })

  it('punch-out on the viewed day shows the saved session and re-fetches flex', async () => {
    const fetchMock = stubFetch({
      'GET /api/workhours/punch-session': json({ session: { start_time: '08:00', date: TODAY } }),
      'POST /api/workhours/punch-out': json(dayWithSession(TODAY, '08:00', '12:00')),
    })
    renderPage()
    await waitFor(() => expect(screen.getByLabelText('workhours:punchOut')).toBeInTheDocument())
    await waitFor(() => expect(calls(fetchMock, 'GET', '/api/workhours/flex')).toHaveLength(1))
    expect(screen.queryByText('08:00')).not.toBeInTheDocument()

    await act(async () => {
      fireEvent.click(screen.getByLabelText('workhours:punchOut'))
    })

    await waitFor(() => expect(screen.getByLabelText('workhours:punchIn')).toBeInTheDocument())
    await waitFor(() => expect(screen.getByText('08:00')).toBeInTheDocument())
    expect(screen.getByText('12:00')).toBeInTheDocument()
    expect(calls(fetchMock, 'GET', '/api/workhours/flex')).toHaveLength(2)
  })

  it('punch-out saved on the previous day moves the view there and shows its data', async () => {
    // A restored punch without a date leaves the view on today; the server
    // then files the session under yesterday.
    const fetchMock = stubFetch({
      'GET /api/workhours/punch-session': json({ session: { start_time: '08:00' } }),
      'GET /api/workhours/day': url =>
        Promise.resolve(jsonResponse(url.includes(YESTERDAY) ? dayWithSession(YESTERDAY, '08:00', '12:00') : { day: null, summary: null })),
      'POST /api/workhours/punch-out': json(dayWithSession(YESTERDAY, '08:00', '12:00')),
    })
    renderPage()
    await waitFor(() => expect(screen.getByLabelText('workhours:punchOut')).toBeInTheDocument())
    await waitFor(() => expect(calls(fetchMock, 'GET', '/api/workhours/flex')).toHaveLength(1))
    const dayGets = () => calls(fetchMock, 'GET', '/api/workhours/day').map(([url]) => url.toString())
    expect(dayGets().every(u => u.includes(TODAY))).toBe(true)

    await act(async () => {
      fireEvent.click(screen.getByLabelText('workhours:punchOut'))
    })

    await waitFor(() => expect(screen.getByLabelText('workhours:punchIn')).toBeInTheDocument())
    await waitFor(() => expect(screen.getByText('08:00')).toBeInTheDocument())
    expect(screen.getByText('12:00')).toBeInTheDocument()
    expect(dayGets().some(u => u.includes(YESTERDAY))).toBe(true)
    expect(calls(fetchMock, 'GET', '/api/workhours/flex')).toHaveLength(2)
  })
})
