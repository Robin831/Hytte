// @vitest-environment happy-dom
import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, fireEvent, cleanup, act, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router'
import { dateIn, editI18n, tripState, txt, zonedInstant, type Trip, type TripSummary } from './tripsApi'
import TripsPage from './TripsPage'
import TripPage from './TripPage'
import { FamilyPicker } from './Family'
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
  share_family: false, race_event_id: null, result_id: null, can_edit: true, created_at: '', updated_at: '2026-10-10T08:00:00Z',
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

    await act(async () => { fireEvent.change(screen.getByPlaceholderText('addItem'), { target: { value: 'Pay hotel' } }) })
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'addItem' })) })
    expect(calls.find(c => c.url === '/api/trips/checklists/3/items')?.body).toMatchObject({ title: { en: 'Pay hotel' } })
  })

  it('edits a section and saves the whole trip', async () => {
    const calls: { url: string; method: string; body?: unknown }[] = []
    vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
      calls.push({ url, method: init?.method ?? 'GET', body: init?.body ? JSON.parse(String(init.body)) : undefined })
      return { ok: true, status: 200, json: async () => ({ trip: SURIN, status: 'ok' }) } as Response
    }))
    render(<MemoryRouter initialEntries={['/trips/7']}><Routes><Route path="/trips/:id" element={<TripPage />} /></Routes></MemoryRouter>)
    await screen.findByRole('heading', { level: 1 })
    const notes = screen.getByRole('region', { name: 'section.notes' })
    fireEvent.click(within(notes).getByRole('button', { name: /edit/ }))
    const form = screen.getByRole('form', { name: /editSection/ })
    fireEvent.change(within(form).getByDisplayValue('Hot and humid.'), { target: { value: 'Very hot.' } })
    await act(async () => { fireEvent.click(within(form).getByRole('button', { name: 'save' })) })
    const put = calls.find(c => c.method === 'PUT' && c.url === '/api/trips/7') as { body: { doc: Trip['doc'] } }
    expect(put.body.doc.notes[0].body).toEqual({ en: 'Very hot.' })
    expect(put.body.doc.flights[0].flight_no).toBe('KL844')
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

    await act(async () => { fireEvent.click(within(section).getByRole('button', { name: 'dates.pick' })) })
    const put = calls.find(c => c.method === 'PUT' && c.url === '/api/trips/9') as { body: { start_date: string; end_date: string; doc: Trip['doc'] } }
    expect(put.body.start_date).toBe('2026-11-06')
    expect(put.body.end_date).toBe('2026-11-08')
    expect(put.body.doc.flex).toMatchObject({ from: '2026-11-01', chosen: true })
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
