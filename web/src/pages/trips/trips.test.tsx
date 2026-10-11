// @vitest-environment happy-dom
import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, fireEvent, cleanup, act, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router'
import { dateIn, editI18n, tripState, txt, zonedInstant, type Trip, type TripSummary } from './tripsApi'
import TripsPage from './TripsPage'
import TripPage from './TripPage'
import { FamilyPicker } from './Family'
import { TripChat } from './TripChat'
import { TripCosts, TripPrices } from './TripPrices'
import type { Candidate, Person, Traveller } from './tripsApi'

function mockT(key: string, opts?: Record<string, unknown>): string {
  if (!opts) return key
  return `${key}:${Object.entries(opts).map(([k, v]) => `${k}=${v}`).join(',')}`
}
vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: mockT, i18n: { language: 'en' } }),
  initReactI18next: { type: '3rdParty', init: () => {} },
}))

afterEach(() => {
  cleanup()
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe('trip helpers', () => {
  it('converts wall-clock times in a zone to instants, across DST', () => {
    // 15:10 in Amsterdam on 5 Oct 2026 (CEST, UTC+2) = 13:10 UTC.
    expect(zonedInstant('2026-10-05T15:10', 'Europe/Amsterdam').toISOString()).toBe('2026-10-05T13:10:00.000Z')
    // 15:10 in Amsterdam on 5 Nov 2026 (CET, UTC+1) = 14:10 UTC.
    expect(zonedInstant('2026-11-05T15:10', 'Europe/Amsterdam').toISOString()).toBe('2026-11-05T14:10:00.000Z')
    expect(zonedInstant('2026-10-07T06:00', 'Asia/Bangkok').toISOString()).toBe('2026-10-06T23:00:00.000Z')
    expect(dateIn('Asia/Bangkok', new Date('2026-10-06T20:00:00Z'))).toBe('2026-10-07')
  })

  it('knows before, during and after in the destination zone', () => {
    const t = { start_date: '2026-10-06', end_date: '2026-10-16', dest_tz: 'Asia/Bangkok' }
    expect(tripState(t, new Date('2026-10-05T12:00:00Z'))).toBe('before')
    expect(tripState(t, new Date('2026-10-05T18:00:00Z'))).toBe('during') // already the 6th in Bangkok
    expect(tripState(t, new Date('2026-10-17T00:00:00Z'))).toBe('after')
  })

  it('falls back across languages and clears stale translations on edit', () => {
    expect(txt({ nb: 'Hei' }, 'th')).toBe('Hei')
    expect(txt({ en: 'Hi', nb: 'Hei' }, 'th')).toBe('Hi')
    expect(editI18n({ nb: 'Pass', en: 'Passport', th: 'หนังสือเดินทาง' }, 'nb', 'Pass')).toEqual({ nb: 'Pass', en: 'Passport', th: 'หนังสือเดินทาง' })
    expect(editI18n({ nb: 'Pass', en: 'Passport' }, 'en', 'Passports')).toEqual({ en: 'Passports' })
  })
})

const SURIN: Trip = {
  id: 7, owner_id: 1, kind: 'family', start_date: '2026-10-06', end_date: '2026-10-16', home_tz: 'Europe/Oslo', dest_tz: 'Asia/Bangkok',
  share_family: false, race_event_id: null, result_id: null, can_edit: true, cost_summary: { total_nok: 0, per_person_nok: 0, by_category: {}, booked_nok: 0 }, created_at: '', updated_at: '2026-10-10T08:00:00Z',
  doc: {
    title: { en: 'Khatiya and Olivia to Surin' }, summary: {}, route: ['BGO', 'AMS', 'BKK', 'Surin'],
    travellers: [{ name: 'Khatiya', child: false }, { name: 'Olivia', child: true }], race: null,
    phases: [{ key: 'surin', title: { en: 'Surin' }, start: '2026-10-09', end: '2026-10-15' }],
    flights: [{ phase: 'return', airline: 'KLM', flight_no: 'KL844', from: 'BKK', from_name: 'Bangkok', to: 'AMS', to_name: 'Amsterdam',
      dep_local: '2026-10-16T12:35', dep_tz: 'Asia/Bangkok', arr_local: '2026-10-16T19:25', arr_tz: 'Europe/Amsterdam', booking_ref: '', seat: '', note: {} }],
    stays: [], transport: [], days: [], documents: [], followups: [],
    contacts: [{ label: { en: 'Tourist police' }, value: '1155', kind: 'phone', urgent: true, note: {} }],
    notes: [{ phase: 'surin', title: { en: 'Surin' }, body: { en: 'Hot and humid.' } }],
  },
  checklists: [{ id: 3, phase: 'surin', title: { en: 'While in Surin' }, position: 1, items: [
    { id: 30, title: { en: 'Call home' }, detail: {}, urgent: false, done: false, done_by_name: '', done_at: '', position: 1 },
    { id: 31, title: { en: 'Buy SIM' }, detail: {}, urgent: false, done: true, done_by_name: 'Robin', done_at: '', position: 2 },
  ] }],
}

describe('TripsPage', () => {
  it('groups trips into ongoing, upcoming and past', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    vi.setSystemTime(new Date('2026-10-10T08:00:00Z'))
    const trips: TripSummary[] = [
      { id: 1, kind: 'race', title: { en: 'Cardiff Race Weekend' }, start_date: '2026-10-02', end_date: '2026-10-05', dest_tz: 'Europe/London', route: [], travellers: ['Robin'], tentative: false, open: 0, total: 10 },
      { id: 7, kind: 'family', title: { en: 'Surin' }, start_date: '2026-10-06', end_date: '2026-10-16', dest_tz: 'Asia/Bangkok', route: ['BGO', 'BKK'], travellers: null, tentative: false, open: 30, total: 34 },
      { id: 9, kind: 'race', title: { en: 'Valencia' }, start_date: '2027-12-04', end_date: '2027-12-06', dest_tz: 'Europe/Madrid', route: [], travellers: [], tentative: false, open: 0, total: 0 },
    ]
    vi.stubGlobal('fetch', vi.fn(async () => ({ ok: true, status: 200, json: async () => ({ trips }) }) as Response))
    render(<MemoryRouter><TripsPage /></MemoryRouter>)
    const ongoing = await screen.findByRole('region', { name: 'group.ongoing' })
    expect(within(ongoing).getByText('Surin')).toBeInTheDocument()
    expect(within(screen.getByRole('region', { name: 'group.upcoming' })).getByText('Valencia')).toBeInTheDocument()
    expect(within(screen.getByRole('region', { name: 'group.past' })).getByText('Cardiff Race Weekend')).toBeInTheDocument()
  })
})

describe('TripPage', () => {
  it('shows the now box, checklists with who ticked, and contacts; ticking posts', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    vi.setSystemTime(new Date('2026-10-10T08:00:00Z')) // 15:00 in Bangkok, Surin phase
    const calls: { url: string; method: string; body?: unknown }[] = []
    vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
      calls.push({ url, method: init?.method ?? 'GET', body: init?.body ? JSON.parse(String(init.body)) : undefined })
      return { ok: true, status: 200, json: async () => ({ trip: SURIN, status: 'ok' }) } as Response
    }))
    render(<MemoryRouter initialEntries={['/trips/7']}><Routes><Route path="/trips/:id" element={<TripPage />} /></Routes></MemoryRouter>)
    expect(await screen.findByRole('heading', { level: 1, name: 'Khatiya and Olivia to Surin' })).toBeInTheDocument()
    expect(screen.getByText('now · Surin')).toBeInTheDocument()
    expect(screen.getByText(/Olivia \(child\)/)).toBeInTheDocument()
    expect(screen.getByText('doneBy:name=Robin')).toBeInTheDocument()
    expect(screen.getByText('progress:done=1,total=2')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '1155' })).toHaveAttribute('href', 'tel:1155')

    await act(async () => { fireEvent.click(screen.getByRole('checkbox', { name: 'Call home' })) })
    expect(calls.find(c => c.url === '/api/trips/items/30/done')?.body).toEqual({ done: true })

    expect(screen.queryByPlaceholderText('addItem')).toBeNull()
    expect(screen.queryByRole('button', { name: /edit/ })).toBeNull()
  })
})

const PARIS: Trip = {
  ...SURIN, id: 9, kind: 'holiday', start_date: '2026-11-01', end_date: '2026-11-30', dest_tz: 'Europe/Paris', checklists: [],
  doc: { ...SURIN.doc, title: { en: 'Paris weekend' }, phases: [], flights: [], contacts: [], notes: [],
    flex: { from: '2026-11-01', to: '2026-11-30', nights: 2, depart_days: [5], chosen: false } },
}
const CANDIDATES: Candidate[] = [
  { start: '2026-11-06', end: '2026-11-08', clashes: [] },
  { start: '2026-11-13', end: '2026-11-15', clashes: [{ kind: 'calendar', title: 'Cabin weekend', start: '2026-11-14', end: '2026-11-14', who: 'Robin' }] },
  { start: '2026-11-20', end: '2026-11-22', clashes: [{ kind: 'race', title: 'Lisbon Half', start: '2026-11-22', end: '2026-11-22', id: 7 }] },
]

describe('tentative trips', () => {
  it('lists candidate dates with clashes and chooses one', async () => {
    const calls: { url: string; method: string; body?: unknown }[] = []
    vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
      calls.push({ url, method: init?.method ?? 'GET', body: init?.body ? JSON.parse(String(init.body)) : undefined })
      const ok = (body: unknown) => ({ ok: true, status: 200, json: async () => body }) as Response
      if (url === '/api/trips/9/candidates') return ok({ candidates: CANDIDATES })
      if (url === '/api/family') return ok({ people: [] })
      return ok({ trip: PARIS, status: 'ok' })
    }))
    render(<MemoryRouter initialEntries={['/trips/9']}><Routes><Route path="/trips/:id" element={<TripPage />} /></Routes></MemoryRouter>)
    const section = await screen.findByRole('region', { name: 'dates.choose' })
    expect(await within(section).findByText('Cabin weekend')).toBeInTheDocument()
    expect(within(section).getByRole('link', { name: 'Lisbon Half' })).toHaveAttribute('href', '/races/7')
    expect(within(section).getAllByText('dates.free')).toHaveLength(1)
    expect(screen.getByText('dates.notSet:nights=2')).toBeInTheDocument()

    fireEvent.click(within(section).getByRole('checkbox'))
    expect(within(section).queryByText('Cabin weekend')).toBeNull()

    expect(within(section).queryByRole('button', { name: 'dates.pick' })).toBeNull()
  })

  it('groups tentative trips under planning', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    vi.setSystemTime(new Date('2026-10-10T08:00:00Z'))
    const trips: TripSummary[] = [
      { id: 9, kind: 'holiday', title: { en: 'Paris weekend' }, start_date: '2026-11-01', end_date: '2026-11-30', dest_tz: 'Europe/Paris', route: [], travellers: [], tentative: true, open: 0, total: 0 },
    ]
    vi.stubGlobal('fetch', vi.fn(async () => ({ ok: true, status: 200, json: async () => ({ trips, people: [] }) }) as Response))
    render(<MemoryRouter><TripsPage /></MemoryRouter>)
    const planning = await screen.findByRole('region', { name: 'group.planning' })
    expect(within(planning).getByText('Paris weekend')).toBeInTheDocument()
    expect(within(planning).getByText('dates.tentative')).toBeInTheDocument()
  })
})

describe('FamilyPicker', () => {
  it('picks travellers from the roster with the child flag from the birth year', () => {
    const people: Person[] = [
      { id: 1, user_id: 1, name: 'Robin', birth_year: 1983 },
      { id: 4, user_id: 4, name: 'William', birth_year: 2017 },
      { id: 6, user_id: null, name: 'Olivia', birth_year: 2024 },
    ]
    let value: Traveller[] = []
    const onChange = vi.fn((v: Traveller[]) => { value = v })
    const { rerender } = render(<FamilyPicker people={people} value={value} onChange={onChange} date="2026-11-06" />)
    expect(screen.getByText('ageYears:age=9')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('checkbox', { name: /William/ }))
    expect(value).toEqual([{ name: 'William', person_id: 4, user_id: 4, child: true }])
    rerender(<FamilyPicker people={people} value={value} onChange={onChange} date="2026-11-06" />)
    fireEvent.change(screen.getByRole('textbox', { name: 'field.others' }), { target: { value: 'Granny, ' } })
    expect(value).toEqual([{ name: 'William', person_id: 4, user_id: 4, child: true }, { name: 'Granny', child: false }])
  })
})

describe('TripChat', () => {
  it('asks Claude, polls while pending and reloads the trip when it changed', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    const onChanged = vi.fn()
    let polls = 0
    const calls: { url: string; method: string; body?: unknown }[] = []
    vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
      const method = init?.method ?? 'GET'
      calls.push({ url, method, body: init?.body ? JSON.parse(String(init.body)) : undefined })
      const ok = (body: unknown) => ({ ok: true, status: 200, json: async () => body }) as Response
      const q = { id: 1, role: 'user', user_name: 'Robin', content: 'Tick the SIM card', status: 'done', changed: false, created_at: '' }
      if (method === 'POST') return ok({ messages: [q, { id: 2, role: 'assistant', content: '', status: 'pending', changed: false, created_at: '' }] })
      polls++
      if (polls === 1) return ok({ messages: [] })
      if (polls === 2) return ok({ messages: [q, { id: 2, role: 'assistant', content: '', status: 'pending', changed: false, created_at: '' }] })
      return ok({ messages: [q, { id: 2, role: 'assistant', content: 'Done – ticked.', status: 'done', changed: true, created_at: '' }] })
    }))
    render(<MemoryRouter><TripChat trip={SURIN} onTripChanged={onChanged} /></MemoryRouter>)
    fireEvent.click(screen.getByRole('button', { name: /chat.open/ }))
    expect(await screen.findByText('chat.introEdit')).toBeInTheDocument()
    fireEvent.change(screen.getByRole('textbox', { name: 'chat.placeholder' }), { target: { value: 'Tick the SIM card' } })
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'chat.send' })) })
    expect(calls.find(c => c.method === 'POST')?.body).toEqual({ message: 'Tick the SIM card', lang: 'en', images: [] })
    expect(screen.getByText('chat.thinking')).toBeInTheDocument()
    await act(async () => { await vi.advanceTimersByTimeAsync(3000) })
    expect(await screen.findByText('Done – ticked.')).toBeInTheDocument()
    expect(screen.getByText('chat.changed')).toBeInTheDocument()
    expect(onChanged).toHaveBeenCalledTimes(1)
  })
})

describe('TripChat pictures and memory', () => {
  const PNG = 'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=='

  it('attaches pictures, sends them without text and shows them in the conversation', async () => {
    const calls: { url: string; method: string; body?: { message: string; images: { data: string }[] } }[] = []
    vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
      const method = init?.method ?? 'GET'
      calls.push({ url, method, body: init?.body ? JSON.parse(String(init.body)) : undefined })
      const ok = (body: unknown) => ({ ok: true, status: 200, json: async () => body }) as Response
      if (method === 'POST') {
        return ok({ messages: [
          { id: 1, role: 'user', user_name: 'Robin', content: '📷', status: 'done', changed: false, images: [41], created_at: '' },
          { id: 2, role: 'assistant', content: 'That is GO Hotel.', status: 'done', changed: false, images: [], created_at: '' },
        ] })
      }
      if (url === '/api/trips/assistant/notes') return ok({ notes: [{ id: 3, text: 'Khatiya is also called Tan', created_at: '' }] })
      return ok({ messages: [], status: 'ok' })
    }))
    render(<MemoryRouter><TripChat trip={SURIN} onTripChanged={() => {}} /></MemoryRouter>)
    fireEvent.click(screen.getByRole('button', { name: /chat.open/ }))
    await screen.findByText('chat.introEdit')

    // Nothing to send yet.
    expect(screen.getByRole('button', { name: 'chat.send' })).toBeDisabled()
    const file = new File([Uint8Array.from(atob(PNG.split(',')[1]), c => c.charCodeAt(0))], 'hotel.png', { type: 'image/png' })
    await act(async () => { fireEvent.change(screen.getByTestId('chat-pictures'), { target: { files: [file] } }) })
    const attached = await screen.findByRole('list', { name: 'chat.attached' })
    expect(within(attached).getAllByRole('img')).toHaveLength(1)

    // A picture alone can be sent.
    expect(screen.getByRole('button', { name: 'chat.send' })).toBeEnabled()
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'chat.send' })) })
    const post = calls.find(c => c.method === 'POST')!
    expect(post.body!.message).toBe('')
    expect(post.body!.images).toHaveLength(1)
    expect(post.body!.images[0].data.startsWith('data:image/png;base64,')).toBe(true)
    expect(screen.queryByRole('list', { name: 'chat.attached' })).toBeNull()
    const shown = await screen.findByRole('img', { name: 'chat.picture' })
    expect(shown).toHaveAttribute('src', '/api/trips/7/chat/images/41')

    // What Claude remembers, with a way to forget.
    fireEvent.click(screen.getByRole('button', { name: 'chat.memory' }))
    expect(await screen.findByText('Khatiya is also called Tan')).toBeInTheDocument()
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: /chat.forget/ })) })
    expect(calls.find(c => c.method === 'DELETE')?.url).toBe('/api/trips/assistant/notes/3')
    expect(screen.queryByText('Khatiya is also called Tan')).toBeNull()
  })
})

describe('TripPrices and TripCosts', () => {
  const quote = (kind: 'flights' | 'hotels', start: string, end: string, cheapest: number, extra: object = {}) => ({
    id: Math.random(), kind, start, end, status: 'done', searched_at: '2026-10-11T08:00:00Z', cheapest, currency: 'NOK', options: [], ...extra,
  })

  it('compares the dates, marks the cheapest and searches on request', async () => {
    const trip: Trip = { ...PARIS, doc: { ...PARIS.doc, destination: { city: 'Paris', airports: ['CDG', 'ORY'] } } }
    const calls: { url: string; method: string }[] = []
    vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
      calls.push({ url, method: init?.method ?? 'GET' })
      const ok = (body: unknown) => ({ ok: true, status: 200, json: async () => body }) as Response
      if (init?.method === 'POST') return ok({ queued: 8 })
      return ok({
        home: 'BGO', quota: { used: 8, limit: 200, ready: true }, passengers: { adults: 1, child_ages: [9], infants: 0 },
        quotes: [
          quote('flights', '2026-11-06', '2026-11-08', 4822, { price_level: 'low', typical_low: 4300, typical_high: 6400,
            options: [{ price: 4822, minutes: 180, stops: 0, airlines: ['Norwegian'], legs: [{ airline: 'Norwegian', flight_no: 'DY 1', from: 'BGO', to: 'ORY', dep: '2026-11-06 06:00', arr: '2026-11-06 09:00' }] }],
            link: 'https://www.google.com/travel/flights?x' }),
          quote('hotels', '2026-11-06', '2026-11-08', 3000, { options: [{ name: 'Hôtel Crayon Rouge', rating: 4.6, total: 3000, per_night: 1500 }] }),
          quote('flights', '2026-11-13', '2026-11-15', 6100),
          quote('hotels', '2026-11-13', '2026-11-15', 3500),
        ],
      })
    }))
    render(<MemoryRouter><TripPrices trip={trip} /></MemoryRouter>)
    const cheapest = await screen.findByText('prices.cheapest')
    const row = cheapest.closest('li')!
    expect(within(row).getByText(/7[,.\s\u00a0]?822/)).toBeInTheDocument()
    expect(within(row).getByText('prices.level.low')).toBeInTheDocument()
    expect(screen.getByText('prices.quota:used=8,limit=200')).toBeInTheDocument()
    fireEvent.click(within(row).getByRole('button'))
    expect(within(row).getByText('Hôtel Crayon Rouge')).toBeInTheDocument()
    expect(within(row).getByRole('link', { name: /prices.openFlights/ })).toHaveAttribute('href', 'https://www.google.com/travel/flights?x')
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: /prices.check/ })) })
    expect(calls.find(c => c.method === 'POST')?.url).toBe('/api/trips/9/prices/search')
  })

  it('asks for a destination first', () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({ ok: true, status: 200, json: async () => ({ quotes: [], quota: { used: 0, limit: 200, ready: true } }) }) as Response))
    render(<MemoryRouter><TripPrices trip={PARIS} /></MemoryRouter>)
    expect(screen.getByText('prices.noDestination')).toBeInTheDocument()
  })

  it('totals the costs', () => {
    const trip: Trip = { ...SURIN, cost_summary: { total_nok: 7474, per_person_nok: 3737, by_category: { flights: 5174, stay: 2300 }, booked_nok: 5174 },
      doc: { ...SURIN.doc, costs: [{ category: 'flights', label: { en: 'KLM' }, amount: 5174, currency: 'NOK', status: 'booked' },
        { category: 'stay', label: { en: 'Hotel' }, amount: 200, currency: 'EUR', status: 'estimate' }] } }
    render(<MemoryRouter><TripCosts trip={trip} /></MemoryRouter>)
    expect(screen.getByText(/7[,.\s\u00a0]?474/)).toBeInTheDocument()
    expect(screen.getByText(/3[,.\s\u00a0]?737/)).toBeInTheDocument()
    expect(screen.getByText('KLM')).toBeInTheDocument()
  })
})
