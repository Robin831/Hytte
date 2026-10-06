// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor, fireEvent, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import WorkHoursPage from './WorkHoursPage'
import enWorkhours from '../../public/locales/en/workhours.json'
import type { WorkSession } from './workHoursUtils'

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

// ── Fetch mock ────────────────────────────────────────────────────────────────

const TODAY = '2026-04-17'
const YESTERDAY = '2026-04-16'

function session(id: number, start: string, end: string): WorkSession {
  return { id, day_id: 1, start_time: start, end_time: end, sort_order: id, is_internal: false, crosses_midnight: false }
}

function dayBody(date: string, sessions: WorkSession[]) {
  return {
    day: { id: date === TODAY ? 1 : 2, user_id: 1, date, lunch: false, notes: '', created_at: '', sessions, deductions: [] },
    summary: null,
  }
}

interface Reply { status: number; body: unknown }

// Routes by method + path. `days` maps a date to the sessions getDay returns
// for it (mutable so a test can change what the reload sees). `sessionPost` and
// `sessionPut` answer the add/update session calls in order; the last reply repeats.
function buildFetch(opts: {
  days: Record<string, WorkSession[]>
  sessionPost?: Reply[]
  sessionPut?: Reply[]
}) {
  const defaults: Record<string, unknown> = {
    '/api/workhours/presets': { presets: [] },
    '/api/workhours/punch-session': { session: null },
    '/api/settings/preferences': { preferences: {} },
    '/api/workhours/flex': { flex: { total_minutes: 0, to_next_interval: 0 }, reset_date: '2026-01-01', days_in_pool: 0 },
    '/api/workhours/leave': { leave_days: [], balance: { total: 0, used: 0, remaining: 0 } },
  }
  const posts = [...(opts.sessionPost ?? [])]
  const puts = [...(opts.sessionPut ?? [])]
  const next = (queue: Reply[]): Reply => (queue.length > 1 ? queue.shift()! : queue[0] ?? { status: 200, body: {} })
  const respond = ({ status, body }: Reply) => Promise.resolve({
    ok: status >= 200 && status < 300,
    status,
    json: () => Promise.resolve(body),
  } as Response)

  return vi.fn((url: string, init?: RequestInit) => {
    const [path, query = ''] = url.toString().split('?')
    const method = init?.method ?? 'GET'
    if (path === '/api/workhours/day/session' && method === 'POST') return respond(next(posts))
    if (path.startsWith('/api/workhours/day/session/') && method === 'PUT') return respond(next(puts))
    if (path.startsWith('/api/workhours/day/session/') && method === 'DELETE') return respond({ status: 204, body: null })
    if (path === '/api/workhours/day') {
      const date = new URLSearchParams(query).get('date') ?? TODAY
      return respond({ status: 200, body: dayBody(date, opts.days[date] ?? []) })
    }
    const body = defaults[path] ?? null
    return respond({ status: body === null ? 404 : 200, body })
  })
}

function conflictReply(conflict: WorkSession): Reply {
  return { status: 409, body: { error: 'session overlaps an existing session', conflict } }
}

function renderPage() {
  return render(
    <MemoryRouter>
      <WorkHoursPage />
    </MemoryRouter>,
  )
}

// The add-session row is the last pair of start/end pickers on the page.
function addRowPickers() {
  const starts = screen.getAllByRole('combobox', { name: 'Start time' })
  const ends = screen.getAllByRole('combobox', { name: 'End time' })
  return { start: starts[starts.length - 1], end: ends[ends.length - 1] }
}

function typeTime(input: HTMLElement, value: string) {
  fireEvent.focus(input)
  fireEvent.change(input, { target: { value } })
  fireEvent.blur(input)
}

async function submitAdd(start: string, end: string) {
  const pickers = addRowPickers()
  typeTime(pickers.start, start)
  typeTime(pickers.end, end)
  fireEvent.click(screen.getByRole('button', { name: 'Add' }))
}

const msg = (key: 'sessionConflict' | 'sessionConflictToggle' | 'sessionConflictCopy', start: string, end: string) =>
  stableT(`workhours:${key}`, { start, end })

// ── Tests ─────────────────────────────────────────────────────────────────────

describe('DayView session conflict messages', () => {
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(new Date('2026-04-17T12:00:00'))
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.useRealTimers()
  })

  it('shows the conflicting times under the add row on a 409 and keeps them on a no-op blur', async () => {
    const existing = session(1, '10:00', '12:00')
    vi.stubGlobal('fetch', buildFetch({ days: { [TODAY]: [existing] }, sessionPost: [conflictReply(existing)] }))
    renderPage()
    await screen.findByText('10:00')

    await submitAdd('09:00', '11:00')
    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent(msg('sessionConflict', '10:00', '12:00'))

    // Focusing a picker and leaving without editing commits the same value.
    const { start } = addRowPickers()
    fireEvent.focus(start)
    fireEvent.blur(start)
    expect(screen.getByRole('alert')).toBeInTheDocument()

    // A real edit clears it.
    typeTime(start, '08:00')
    await waitFor(() => expect(screen.queryByRole('alert')).not.toBeInTheDocument())
  })

  it('shows the conflict on the edited row and clears it on cancel', async () => {
    const a = session(1, '08:00', '09:00')
    const b = session(2, '10:00', '12:00')
    vi.stubGlobal('fetch', buildFetch({ days: { [TODAY]: [a, b] }, sessionPut: [conflictReply(b)] }))
    renderPage()
    await screen.findByText('08:00')

    fireEvent.click(screen.getAllByRole('button', { name: 'Edit session' })[0])
    const ends = screen.getAllByRole('combobox', { name: 'End time' })
    typeTime(ends[0], '11:00')
    fireEvent.click(screen.getByRole('button', { name: enWorkhours.saveSession }))

    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent(msg('sessionConflict', '10:00', '12:00'))
    // It belongs to the edited row, not the add row.
    expect(within(alert.parentElement!).getAllByRole('combobox', { name: 'End time' })).toHaveLength(1)

    fireEvent.click(screen.getByRole('button', { name: enWorkhours.cancelEditSession }))
    await waitFor(() => expect(screen.queryByRole('alert')).not.toBeInTheDocument())
  })

  it('uses the toggle wording when marking a stored overlap internal is rejected', async () => {
    const a = session(1, '09:00', '11:00')
    const b = session(2, '10:00', '12:00')
    vi.stubGlobal('fetch', buildFetch({ days: { [TODAY]: [a, b] }, sessionPut: [conflictReply(b)] }))
    renderPage()
    await screen.findByText('09:00')

    fireEvent.click(screen.getAllByRole('button', { name: enWorkhours.markInternal })[0])
    expect(await screen.findByRole('alert')).toHaveTextContent(msg('sessionConflictToggle', '10:00', '12:00'))
  })

  it('clears the conflict when the session it names is deleted', async () => {
    const existing = session(1, '10:00', '12:00')
    const days: Record<string, WorkSession[]> = { [TODAY]: [existing] }
    vi.stubGlobal('fetch', buildFetch({ days, sessionPost: [conflictReply(existing)] }))
    renderPage()
    await screen.findByText('10:00')

    await submitAdd('09:00', '11:00')
    await screen.findByRole('alert')

    days[TODAY] = []
    fireEvent.click(screen.getByRole('button', { name: enWorkhours.removeSession }))
    await waitFor(() => expect(screen.queryByRole('alert')).not.toBeInTheDocument())
  })

  it('reloads the day and re-enables the form after copy-yesterday stops on a conflict', async () => {
    const first = session(11, '08:00', '10:00')
    const second = session(12, '09:00', '11:00')
    const days: Record<string, WorkSession[]> = { [TODAY]: [], [YESTERDAY]: [first, second] }
    const fetchMock = buildFetch({ days, sessionPost: [{ status: 201, body: {} }, conflictReply(first)] })
    vi.stubGlobal('fetch', fetchMock)
    renderPage()
    const copy = await screen.findByRole('button', { name: enWorkhours.copyYesterday })
    await waitFor(() => expect(copy).not.toBeDisabled())

    days[TODAY] = [{ ...first, id: 21 }]
    fireEvent.click(copy)

    expect(await screen.findByRole('alert')).toHaveTextContent(msg('sessionConflictCopy', '08:00', '10:00'))
    // The first session was copied and shows after the reload.
    await screen.findByText('08:00')
    const posts = fetchMock.mock.calls.filter(([u, init]) => u === '/api/workhours/day/session' && init?.method === 'POST')
    expect(posts).toHaveLength(2)
    await waitFor(() => expect(screen.getByRole('button', { name: enWorkhours.removeSession })).not.toBeDisabled())
  })
})
