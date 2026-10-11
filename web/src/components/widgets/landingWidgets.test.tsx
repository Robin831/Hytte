// @vitest-environment happy-dom
import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, fireEvent, cleanup, waitFor, act } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import TripsWidget from './TripsWidget'
import RacesWidget from './RacesWidget'
import StrideWidget from './StrideWidget'
import WordfeudWidget from './WordfeudWidget'
import FamilyChatWidget from './FamilyChatWidget'
import StarsWidget from './StarsWidget'
import ChoresWidget from './ChoresWidget'
import WorkHoursWidget from './WorkHoursWidget'

function mockT(key: string, opts?: Record<string, unknown>): string {
  if (!opts) return key
  return `${key}:${Object.entries(opts).map(([k, v]) => `${k}=${v}`).join(',')}`
}
vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: mockT, i18n: { language: 'en' } }),
  initReactI18next: { type: '3rdParty', init: () => {} },
}))

const authState = {
  user: { id: 1, name: 'Robin' },
  familyStatus: null as { is_parent: boolean; is_child: boolean } | null,
}
vi.mock('../../auth', () => ({ useAuth: () => authState }))

const pad = (n: number) => String(n).padStart(2, '0')
/** A local calendar date `offset` days from today, as YYYY-MM-DD. */
function localDay(offset: number): string {
  const d = new Date()
  d.setDate(d.getDate() + offset)
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
}
/** The same, on the UTC calendar (trips are dated in their destination's zone). */
function utcDay(offset: number): string {
  return new Date(Date.now() + offset * 86_400_000).toISOString().slice(0, 10)
}

interface Call { url: string; method: string; body?: unknown }

/** Routes fetches by URL prefix; an unrouted URL answers 404. */
function installFetch(routes: Record<string, unknown | (() => unknown)>, failing: string[] = []) {
  const calls: Call[] = []
  vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
    const method = init?.method ?? 'GET'
    calls.push({ url, method, body: init?.body ? JSON.parse(String(init.body)) : undefined })
    const reply = (status: number, body: unknown) => ({ ok: status < 400, status, json: async () => body }) as Response
    if (failing.some((f) => url.startsWith(f))) return reply(500, { error: 'boom' })
    const key = Object.keys(routes).find((k) => url.startsWith(k))
    if (!key) return reply(404, { error: 'not found' })
    const value = routes[key]
    return reply(200, typeof value === 'function' ? (value as () => unknown)() : value)
  }))
  return calls
}

const show = (ui: React.ReactElement) => render(<MemoryRouter>{ui}</MemoryRouter>)

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  authState.familyStatus = null
})

describe('TripsWidget', () => {
  const trip = (over: Record<string, unknown>) => ({
    id: 1, kind: 'holiday', title: { en: 'Surin' }, start_date: utcDay(-2), end_date: utcDay(3), dest_tz: 'UTC',
    route: ['Bergen', 'Bangkok'], travellers: null, tentative: false, open: 0, total: 0, ...over,
  })

  it('shows the ongoing trip first with which day it is, then what is coming up', async () => {
    installFetch({
      '/api/trips': {
        trips: [
          trip({ id: 3, title: { en: 'Someday' }, start_date: utcDay(60), end_date: utcDay(70), tentative: true }),
          trip({ id: 2, title: { en: 'Cardiff' }, start_date: utcDay(10), end_date: utcDay(13) }),
          trip({ id: 1, open: 4, total: 9 }),
          trip({ id: 9, title: { en: 'Long ago' }, start_date: utcDay(-40), end_date: utcDay(-30) }),
        ],
      },
    })
    show(<TripsWidget />)
    expect(await screen.findByText('Surin')).toBeInTheDocument()
    expect(screen.getByText('widgets.trips.dayOf:day=3,total=6')).toBeInTheDocument()
    expect(screen.getByText('widgets.trips.checklistOpen:count=4')).toBeInTheDocument()
    expect(screen.getByText('widgets.trips.inDays:count=10')).toBeInTheDocument()
    expect(screen.getByText('widgets.trips.tentative')).toBeInTheDocument()
    expect(screen.queryByText('Long ago')).toBeNull()
    expect(screen.getByRole('link', { name: /Surin/ })).toHaveAttribute('href', '/trips/1')
  })

  it('stays out of the way when no trip is ahead', async () => {
    const calls = installFetch({ '/api/trips': { trips: [trip({ start_date: utcDay(-40), end_date: utcDay(-30) })] } })
    const { container } = show(<TripsWidget />)
    await waitFor(() => expect(calls).toHaveLength(1))
    await act(async () => {})
    expect(container).toBeEmptyDOMElement()
  })
})

describe('RacesWidget', () => {
  const event = (over: Record<string, unknown>) => ({
    id: 1, name: 'Race', race_date: localDay(30), date_precision: 'day', deadlines: [], ...over,
  })

  it('leads with the race you are registered for and lists the nearest deadlines', async () => {
    installFetch({
      '/api/races': {
        events: [
          event({ id: 1, name: 'Watched 10k', race_date: localDay(5) }),
          event({ id: 2, name: 'Valencia Half', race_date: localDay(12), deadlines: [
            { id: 7, event_id: 2, kind: 'payment_due', due_date: localDay(3), date_precision: 'day' },
            { id: 8, event_id: 2, kind: 'entry_closes', due_date: localDay(-4), date_precision: 'day' },
          ] }),
          event({ id: 3, name: 'Already run', race_date: localDay(-20) }),
          event({ id: 4, name: 'Not on my list', race_date: localDay(2) }),
        ],
        watches: [
          { event_id: 1, state: 'watching' }, { event_id: 2, state: 'registered' }, { event_id: 3, state: 'registered' },
        ],
      },
    })
    show(<RacesWidget />)
    const lead = await screen.findByRole('link', { name: /Valencia Half.*when.inDays:count=12/ })
    expect(lead).toHaveAttribute('href', '/races/2')
    expect(screen.getByText(/watchState.registered/)).toBeInTheDocument()
    expect(screen.getByText('kind.payment_due · Valencia Half')).toBeInTheDocument()
    expect(screen.getByText('when.inDays:count=3')).toBeInTheDocument()
    expect(screen.queryByText(/kind.entry_closes/)).toBeNull()
    expect(screen.queryByText(/Already run|Not on my list/)).toBeNull()
  })

  it('says so when nothing is on the list', async () => {
    installFetch({ '/api/races': { events: [event({})], watches: [] } })
    show(<RacesWidget />)
    expect(await screen.findByText('widgets.races.none')).toBeInTheDocument()
  })
})

describe('StrideWidget', () => {
  it("shows today's session from this week's plan", async () => {
    installFetch({
      '/api/stride/plans/current': {
        plan: {
          id: 1, week_start: localDay(-1), week_end: localDay(5), phase: 'base',
          plan: [
            { date: localDay(-1), rest_day: true },
            { date: localDay(0), rest_day: false, session: { description: '8 km easy', target_hr_cap: 145 } },
            { date: localDay(1), rest_day: false, session: { description: 'Intervals', target_hr_cap: 0 } },
          ],
        },
      },
    })
    show(<StrideWidget />)
    expect(await screen.findByText('8 km easy')).toBeInTheDocument()
    expect(screen.getByText(/plan.bpm:value=145/)).toBeInTheDocument()
    expect(screen.getByText(/widgets.stride.sessions:count=2/)).toBeInTheDocument()
  })

  it('is left out in a week without a plan', async () => {
    const calls = installFetch({})
    const { container } = show(<StrideWidget />)
    await waitFor(() => expect(calls).toHaveLength(1))
    await act(async () => {})
    expect(container).toBeEmptyDOMElement()
  })
})

describe('WordfeudWidget', () => {
  it('counts the games waiting on you', async () => {
    installFetch({
      '/api/wordfeud/games': {
        games: [
          { id: 1, opponent: 'kari', scores: [120, 98], is_my_turn: true },
          { id: 2, opponent: 'ola', scores: [40, 61], is_my_turn: false },
          { id: 3, opponent: 'per', scores: [10, 12], is_my_turn: true },
        ],
      },
    })
    show(<WordfeudWidget />)
    expect(await screen.findByText('widgets.wordfeud.yourTurn:count=2')).toBeInTheDocument()
    expect(screen.getByText('kari')).toBeInTheDocument()
    expect(screen.getByText('120–98')).toBeInTheDocument()
    expect(screen.queryByText('ola')).toBeNull()
  })

  it('disappears when Wordfeud cannot be reached', async () => {
    const calls = installFetch({}, ['/api/wordfeud/games'])
    const { container } = show(<WordfeudWidget />)
    await waitFor(() => expect(calls).toHaveLength(1))
    await act(async () => {})
    expect(container).toBeEmptyDOMElement()
  })
})

describe('FamilyChatWidget', () => {
  it('totals unread messages and lists unread conversations first', async () => {
    installFetch({
      '/api/familychat/conversations': {
        conversations: [
          { id: 1, name: 'Parents', unread_count: 0, last_message_preview: 'ok', last_message_at: '' },
          { id: 2, name: '', unread_count: 3, last_message_preview: 'Dinner?', last_message_at: '' },
        ],
      },
    })
    show(<FamilyChatWidget />)
    expect(await screen.findByText('widgets.familyChat.unread:count=3')).toBeInTheDocument()
    const rows = screen.getAllByRole('listitem')
    expect(rows[0]).toHaveTextContent('widgets.familyChat.unnamed')
    expect(rows[0]).toHaveTextContent('Dinner?')
    expect(rows[1]).toHaveTextContent('Parents')
  })
})

describe('StarsWidget', () => {
  it('shows the balance, level title and streak', async () => {
    installFetch({
      '/api/stars/balance': { current_balance: 42, level: 3, title: 'Explorer' },
      '/api/stars/streaks': { daily_workout: { current_count: 5, longest_count: 9 } },
    })
    show(<StarsWidget />)
    expect(await screen.findByText('42')).toBeInTheDocument()
    expect(screen.getByText('Explorer')).toBeInTheDocument()
    expect(screen.getByText('widgets.stars.streak:count=5')).toBeInTheDocument()
  })

  it('still shows the balance when the streak cannot be loaded', async () => {
    installFetch({ '/api/stars/balance': { current_balance: 7, level: 1, title: '' } }, ['/api/stars/streaks'])
    show(<StarsWidget />)
    expect(await screen.findByText('7')).toBeInTheDocument()
    expect(screen.queryByText(/widgets.stars.streak/)).toBeNull()
  })
})

describe('ChoresWidget', () => {
  it("lists a child's chores and counts the ones still to do", async () => {
    authState.familyStatus = { is_parent: false, is_child: true }
    installFetch({
      '/api/allowance/my/chores': {
        chores: [
          { id: 1, name: 'Dishes' },
          { id: 2, name: 'Tidy room', completion_status: 'pending' },
          { id: 3, name: 'Bins', completion_status: 'approved' },
          { id: 4, name: 'Laundry', completion_status: 'rejected' },
        ],
      },
    })
    show(<ChoresWidget />)
    expect(await screen.findByText('widgets.chores.left:count=2')).toBeInTheDocument()
    expect(screen.getAllByRole('listitem')).toHaveLength(4)
    expect(screen.getByRole('link', { name: 'widgets.chores.open' })).toHaveAttribute('href', '/chores')
  })

  it('tells a parent how many chores wait for approval', async () => {
    authState.familyStatus = { is_parent: true, is_child: false }
    installFetch({ '/api/allowance/pending': { pending: [{ id: 1 }, { id: 2 }] } })
    show(<ChoresWidget />)
    expect(await screen.findByText('widgets.chores.toApprove:count=2')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'widgets.chores.review' })).toHaveAttribute('href', '/allowance')
  })

  it('shows a parent nothing when nothing waits', async () => {
    authState.familyStatus = { is_parent: true, is_child: false }
    const calls = installFetch({ '/api/allowance/pending': { pending: [] } })
    const { container } = show(<ChoresWidget />)
    await waitFor(() => expect(calls).toHaveLength(1))
    await act(async () => {})
    expect(container).toBeEmptyDOMElement()
  })
})

describe('WorkHoursWidget', () => {
  it('punches in from the landing page', async () => {
    let session: { date: string; start_time: string } | null = null
    const calls = installFetch({
      '/api/workhours/punch-session': () => ({ session }),
      '/api/workhours/day': { summary: { net_minutes: 0, standard_minutes: 450 } },
      '/api/workhours/punch-in': () => {
        session = { date: localDay(0), start_time: '00:00' }
        return { session }
      },
    })
    show(<WorkHoursWidget />)
    expect(await screen.findByText('widgets.workHours.notPunchedIn')).toBeInTheDocument()
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'widgets.workHours.punchIn' })) })

    const post = calls.find((c) => c.method === 'POST')
    expect(post?.url).toBe('/api/workhours/punch-in')
    expect(post?.body).toMatchObject({ date: localDay(0), start_time: expect.stringMatching(/^\d\d:\d\d$/) })
    expect(await screen.findByRole('button', { name: 'widgets.workHours.punchOut' })).toBeInTheDocument()
    expect(screen.getByText('widgets.workHours.punchedIn:time=00:00')).toBeInTheDocument()
  })

  it('punches out an open session', async () => {
    const calls = installFetch({
      '/api/workhours/punch-session': { session: { date: localDay(0), start_time: '00:00' } },
      '/api/workhours/day': { summary: { net_minutes: 120, standard_minutes: 450 } },
      '/api/workhours/punch-out': { status: 'ok' },
    })
    show(<WorkHoursWidget />)
    const out = await screen.findByRole('button', { name: 'widgets.workHours.punchOut' })
    await act(async () => { fireEvent.click(out) })
    const post = calls.find((c) => c.method === 'POST')
    expect(post?.url).toBe('/api/workhours/punch-out')
    expect(post?.body).toMatchObject({ end_time: expect.stringMatching(/^\d\d:\d\d$/), crosses_midnight: false })
    expect(screen.getByRole('progressbar', { name: 'widgets.workHours.progress' })).toBeInTheDocument()
  })

  it('says so when a punch does not go through', async () => {
    installFetch({
      '/api/workhours/punch-session': { session: null },
      '/api/workhours/day': { summary: null },
    }, ['/api/workhours/punch-in'])
    show(<WorkHoursWidget />)
    const punchIn = await screen.findByRole('button', { name: 'widgets.workHours.punchIn' })
    await act(async () => { fireEvent.click(punchIn) })
    expect(await screen.findByRole('alert')).toHaveTextContent('widgets.workHours.error')
  })
})
