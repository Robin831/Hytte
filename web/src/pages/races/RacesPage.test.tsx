// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor, fireEvent, within, cleanup } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router'
import RacesPage from './RacesPage'
import RaceDetailPage from './RaceDetailPage'
import type { RaceEvent, Watch } from './racesApi'

// Stable t: returns the key, with interpolation values appended so tests can
// assert on them ("when.inDays:10").
function mockT(key: string, opts?: Record<string, unknown>): string {
  if (opts && 'count' in opts) return `${key}:${opts.count}`
  if (opts && 'date' in opts) return `${key}:${opts.date}`
  return key
}
vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: mockT, i18n: { language: 'en' } }),
  initReactI18next: { type: '3rdParty', init: () => {} },
}))

const authState: { user: object | null; hasFeature: (f: string) => boolean } = {
  user: { id: 1, is_admin: false },
  hasFeature: () => false,
}
vi.mock('../../auth', () => ({ useAuth: () => authState }))

function race(over: Partial<RaceEvent>): RaceEvent {
  return {
    id: 1, slug: 'x', name: 'Race', edition_year: 2027, race_date: '2027-05-09', date_precision: 'day',
    country: 'NO', distance_m: 21097, status: 'open', entry_type: 'fcfs', travel: 'direct', url: '',
    series: [], texts: { en: { place: 'Bergen, Norway', participants: '', course: '', travel: '', how: 'Open.', price: '' } },
    scope: 'away', distances: [], place: '', lat: null, lng: null, source: '', source_id: '',
    checked_at: '2026-10-09T12:00:00Z', created_at: '', updated_at: '', deadlines: [], ...over,
  }
}

const EVENTS: RaceEvent[] = [
  race({ id: 1, name: 'Bergen Half', status: 'open' }),
  race({
    id: 2, name: 'Berlin Marathon', status: 'open', distance_m: 42195, series: ['majors'], country: 'DE', race_date: '2027-09-26',
    deadlines: [
      { id: 10, event_id: 2, kind: 'lottery_closes', due_date: '2099-11-06', date_precision: 'approx', due_time: '', tz: '', expected: true, texts: { en: { what: 'Ballot closes.' } } },
      { id: 11, event_id: 2, kind: 'entry_opens', due_date: '2020-01-01', date_precision: 'day', due_time: '', tz: '', expected: false, texts: { en: { what: 'Long gone.' } } },
    ],
  }),
  race({ id: 3, name: 'London Marathon', status: 'closed', distance_m: 42195, series: ['majors', 'emc'], travel: 'direct', race_date: '2027-04-25' }),
  race({
    id: 4, name: 'Chicago Marathon', status: 'later', distance_m: 42195, travel: 'none', race_date: '2027-10-10',
    deadlines: [
      { id: 20, event_id: 4, kind: 'lottery_results', due_date: '2099-12-08', date_precision: 'day', due_time: '', tz: '', expected: false, texts: { en: { what: 'Results.' } } },
    ],
  }),
]

let watches: Watch[] = []
const calls: { method: string; url: string; body?: unknown }[] = []

function installFetch() {
  vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
    const method = init?.method ?? 'GET'
    const body = init?.body ? JSON.parse(String(init.body)) : undefined
    calls.push({ method, url, body })
    const ok = (data: unknown) => ({ ok: true, status: 200, json: async () => data }) as Response
    if (url === '/api/races' && method === 'GET') return ok({ events: EVENTS, watches })
    const m = url.match(/^\/api\/races\/(\d+)(\/watch)?$/)
    if (m && m[2] && method === 'PUT') {
      const w: Watch = { event_id: Number(m[1]), state: body.state, notes: body.notes, stride_race_id: null, created_at: '', updated_at: '' }
      return ok({ watch: w })
    }
    if (m && m[2] && method === 'DELETE') return ok({ status: 'ok' })
    if (m && method === 'GET') {
      const event = EVENTS.find(e => e.id === Number(m[1]))
      return ok({
        event,
        changes: [{ id: 1, event_id: event!.id, field: 'status', old_value: 'later', new_value: 'open', source: 'manual', created_at: '2026-10-09T12:00:00Z' }],
        watch: watches.find(w => w.event_id === event!.id) ?? null,
      })
    }
    throw new Error(`unexpected fetch ${method} ${url}`)
  }))
}

function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="/races" element={<RacesPage />} />
        <Route path="/races/:id" element={<RaceDetailPage />} />
      </Routes>
    </MemoryRouter>,
  )
}

describe('RacesPage', () => {
  beforeEach(() => {
    watches = []
    calls.length = 0
    authState.user = { id: 1, is_admin: false }
    installFetch()
  })
  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
    EVENTS.splice(4)
  })

  it('separates local races from trips, with distance chips and kids and distance filters', async () => {
    EVENTS.push(race({
      id: 5, name: 'Øygardskarusellen', scope: 'local', source: 'kondis', url: 'https://terminlista.kondis.no/events/x', distance_m: 5000, travel: '', race_date: '2027-03-01',
      distances: [{ m: 600, label: 'Barneløp', kids: true }, { m: 3000, label: '3 km', kids: false }, { m: 5000, label: '5 km', kids: false }],
    }))
    renderAt('/races')
    await screen.findByText('Øygardskarusellen')
    const scope = screen.getByRole('group', { name: 'filters.scope' })

    fireEvent.click(within(scope).getByRole('button', { name: 'scope.local' }))
    expect(screen.getByText('Øygardskarusellen')).toBeInTheDocument()
    expect(screen.queryByText('Berlin Marathon')).toBeNull()
    expect(screen.queryByRole('button', { name: 'filters.directOnly' })).toBeNull()
    const chips = screen.getByRole('list', { name: 'facts.distances' })
    expect(within(chips).getAllByRole('listitem')).toHaveLength(3)
    expect(within(chips).getByText(/Barneløp/)).toHaveTextContent('kidsBadge')
    expect(screen.getByRole('link', { name: /facts.kondis/ })).toBeInTheDocument()

    fireEvent.click(within(scope).getByRole('button', { name: 'scope.away' }))
    expect(screen.queryByText('Øygardskarusellen')).toBeNull()
    expect(screen.getByText('Berlin Marathon')).toBeInTheDocument()

    fireEvent.click(within(scope).getByRole('button', { name: 'filters.all' }))
    fireEvent.click(screen.getByRole('button', { name: 'filters.kids' }))
    expect(screen.getByText('Øygardskarusellen')).toBeInTheDocument()
    expect(screen.queryByText('Bergen Half')).toBeNull()

    fireEvent.click(screen.getByRole('button', { name: 'filters.kids' }))
    fireEvent.click(screen.getByRole('button', { name: 'distance.3k' }))
    expect(screen.getByText('Øygardskarusellen')).toBeInTheDocument()
    expect(screen.queryByText('Bergen Half')).toBeNull()
  })

  it('opens on Discover when nothing is tracked, with live status counts and filters', async () => {
    renderAt('/races')
    await screen.findByText('Bergen Half')
    expect(screen.getByRole('tab', { name: /tabs.discover/ })).toHaveAttribute('aria-selected', 'true')

    const statusGroup = screen.getByRole('group', { name: 'filters.status' })
    expect(within(statusGroup).getByRole('button', { name: /filters.all/ })).toHaveTextContent('4')
    expect(within(statusGroup).getByRole('button', { name: /status.open/ })).toHaveTextContent('2')

    fireEvent.click(within(statusGroup).getByRole('button', { name: /status.closed/ }))
    expect(screen.getByText('London Marathon')).toBeInTheDocument()
    expect(screen.queryByText('Bergen Half')).toBeNull()

    // Marathon + Majors + direct flight narrows to London.
    fireEvent.click(within(statusGroup).getByRole('button', { name: /filters.all/ }))
    fireEvent.click(screen.getByRole('button', { name: 'distance.marathon' }))
    fireEvent.click(screen.getByRole('button', { name: 'series.majors' }))
    fireEvent.click(screen.getByRole('button', { name: 'filters.directOnly' }))
    expect(screen.getByText('London Marathon')).toBeInTheDocument()
    expect(screen.getByText('Berlin Marathon')).toBeInTheDocument()
    expect(screen.queryByText('Chicago Marathon')).toBeNull()
    expect(screen.queryByText('Bergen Half')).toBeNull()
  })

  it('tracks and untracks a race from the bookmark button', async () => {
    renderAt('/races?tab=discover')
    const button = await screen.findByRole('button', { name: 'watch.track: Berlin Marathon' })
    fireEvent.click(button)
    await waitFor(() => expect(calls.some(c => c.method === 'PUT' && c.url === '/api/races/2/watch')).toBe(true))
    expect(calls.find(c => c.method === 'PUT')?.body).toEqual({ state: 'watching', notes: '' })

    const untrack = await screen.findByRole('button', { name: 'watch.untrack: Berlin Marathon' })
    fireEvent.click(untrack)
    await waitFor(() => expect(calls.some(c => c.method === 'DELETE' && c.url === '/api/races/2/watch')).toBe(true))
  })

  it('groups my races by state with the next upcoming deadline', async () => {
    watches = [
      { event_id: 2, state: 'lottery_entered', notes: '', stride_race_id: null, created_at: '', updated_at: '' },
      { event_id: 1, state: 'registered', notes: '', stride_race_id: null, created_at: '', updated_at: '' },
    ]
    renderAt('/races')
    const lottery = await screen.findByRole('region', { name: /watchState.lottery_entered/ })
    expect(within(lottery).getByText('Berlin Marathon')).toBeInTheDocument()
    // The passed entry_opens deadline is skipped; the upcoming lottery close is shown.
    expect(within(lottery).getByText('kind.lottery_closes')).toBeInTheDocument()
    const registered = screen.getByRole('region', { name: /watchState.registered/ })
    expect(within(registered).getByText('mine.noDeadline')).toBeInTheDocument()
    // Pipeline order: registered comes before in-the-lottery.
    const headings = screen.getAllByRole('heading', { level: 2 }).map(h => h.textContent)
    expect(headings.findIndex(h => h?.includes('registered'))).toBeLessThan(headings.findIndex(h => h?.includes('lottery_entered')))
  })

  it('lists upcoming deadlines only, soonest first, scoped to my races or all', async () => {
    watches = [{ event_id: 4, state: 'watching', notes: '', stride_race_id: null, created_at: '', updated_at: '' }]
    renderAt('/races?tab=deadlines')
    await screen.findByText('Results.')
    // Only my races by default: Berlin's deadline is hidden.
    expect(screen.queryByText('Ballot closes.')).toBeNull()

    fireEvent.click(screen.getByRole('button', { name: 'deadlines.all' }))
    const texts = screen.getAllByText(/Ballot closes\.|Results\./).map(n => n.textContent)
    expect(texts).toEqual(['Ballot closes.', 'Results.'])
    expect(screen.queryByText('Long gone.')).toBeNull()
    expect(screen.getAllByText('expected').length).toBe(1)
  })

  it('shows the admin "new race" button only to admins', async () => {
    renderAt('/races')
    await screen.findByText('Bergen Half')
    expect(screen.queryByRole('button', { name: /admin.newRace/ })).toBeNull()
    expect(screen.queryByRole('tab', { name: /tabs.research/ })).toBeNull()
    cleanup()
    authState.user = { id: 1, is_admin: true }
    renderAt('/races')
    expect(await screen.findByRole('button', { name: /admin.newRace/ })).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: /tabs.research/ })).toBeInTheDocument()
  })
})

describe('RaceDetailPage', () => {
  beforeEach(() => {
    watches = [{ event_id: 2, state: 'watching', notes: 'Apply with Kari', stride_race_id: null, created_at: '', updated_at: '' }]
    calls.length = 0
    authState.user = { id: 1, is_admin: false }
    installFetch()
  })
  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
  })

  it('shows status, notes, deadlines and history, and saves a state change', async () => {
    renderAt('/races/2')
    expect(await screen.findByRole('heading', { level: 1, name: 'Berlin Marathon' })).toBeInTheDocument()
    expect(screen.getByLabelText('watch.notes')).toHaveValue('Apply with Kari')
    expect(screen.getByText('Ballot closes.')).toBeInTheDocument()
    // History renders the status change with translated values.
    expect(screen.getByText('field.status')).toBeInTheDocument()
    expect(screen.getByText('status.later')).toBeInTheDocument()

    fireEvent.change(screen.getByLabelText('watch.state'), { target: { value: 'lottery_entered' } })
    await waitFor(() => expect(calls.find(c => c.method === 'PUT')?.body).toEqual({ state: 'lottery_entered', notes: 'Apply with Kari' }))
    expect(screen.queryByRole('button', { name: /admin.edit/ })).toBeNull()
  })

  it('reports an invalid race id without fetching', async () => {
    renderAt('/races/abc')
    expect(await screen.findByRole('alert')).toHaveTextContent('errors.notFound')
    expect(calls).toHaveLength(0)
  })
})
