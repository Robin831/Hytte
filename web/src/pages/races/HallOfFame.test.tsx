// @vitest-environment happy-dom
import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, fireEvent, cleanup, act, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import { HallOfFame } from './HallOfFame'
import type { RaceResult } from './racesApi'

function mockT(key: string, opts?: Record<string, unknown>): string {
  if (!opts) return key
  return `${key}:${Object.entries(opts).map(([k, v]) => `${k}=${v}`).join(',')}`
}
vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: mockT, i18n: { language: 'en' } }),
  initReactI18next: { type: '3rdParty', init: () => {} },
}))
vi.mock('recharts', async () => {
  const actual = await vi.importActual<typeof import('recharts')>('recharts')
  return { ...actual, ResponsiveContainer: ({ children }: { children: React.ReactElement }) => <div style={{ width: 600, height: 160 }}>{children}</div> }
})

function result(over: Partial<RaceResult>): RaceResult {
  return {
    id: 1, person_name: '', event_id: null, race_name: 'Race', race_date: '2020-01-01', date_exact: true, distance_m: 21097,
    city: 'Bergen', country: 'NO', finish_seconds: null, time_source: '', time_url: '', bib: '', status: 'confirmed',
    confidence: 'high', source: 'gmail', evidence: [], notes: '', series_key: '', created_at: '', updated_at: '', ...over,
  }
}

const RESULTS: RaceResult[] = [
  result({ id: 1, race_name: 'Valencia Marathon', race_date: '2016-11-20', distance_m: 42195, city: 'Valencia', country: 'ES', finish_seconds: 14368, time_source: 'strava' }),
  result({ id: 2, race_name: 'Berlin Marathon', race_date: '2017-09-24', distance_m: 42195, city: 'Berlin', country: 'DE', finish_seconds: 12601, series_key: 'berlin' }),
  result({ id: 3, race_name: 'Bergen City Marathon', race_date: '2017-04-29' }),
  result({ id: 4, race_name: 'Cardiff Half', race_date: '2023-10-01', status: 'pending', confidence: 'low', evidence: [{ what: 'registration only' }] }),
  result({ id: 5, race_name: 'Semi de Paris', race_date: '2022-03-06', status: 'pending', confidence: 'high', finish_seconds: 7203 }),
  result({ id: 6, person_name: 'Khatiya', race_name: 'Bergen3000', race_date: '2025-06-10', distance_m: 3000 }),
]

function installFetch() {
  const calls: { url: string; method: string; body?: unknown }[] = []
  vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
    const method = init?.method ?? 'GET'
    calls.push({ url, method, body: init?.body ? JSON.parse(String(init.body)) : undefined })
    const ok = (body: unknown, status = 200) => ({ ok: true, status, json: async () => body }) as Response
    if (url === '/api/races/results' && method === 'GET') {
      return ok({
        results: RESULTS,
        summary: {
          races: 3, pending: 2, first_year: 2016, countries: ['DE', 'ES', 'NO'], per_year: { '2016': 1, '2017': 2 },
          pbs: [{ distance: 'marathon', result_id: 2, finish_seconds: 12601, race_name: 'Berlin Marathon', race_date: '2017-09-24', count: 2 },
            { distance: 'half', result_id: 0, finish_seconds: 0, race_name: '', race_date: '', count: 1 }],
        },
      })
    }
    if (url.endsWith('/lookup')) return ok({ run: { id: 9 } }, 202)
    return ok({ confirmed: 1, status: 'ok', result: {} })
  }))
  return calls
}

describe('HallOfFame', () => {
  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
  })

  it('shows the review queue, PBs, progression and races by year', async () => {
    const calls = installFetch()
    render(<MemoryRouter><HallOfFame /></MemoryRouter>)
    const review = await screen.findByRole('region', { name: /hall.reviewTitle:count=2/ })
    expect(within(review).getByText('Cardiff Half')).toBeInTheDocument()
    expect(within(review).getByText(/registration only/)).toBeInTheDocument()

    expect(screen.getByText('hall.stats:races=3,since=2016,countries=3')).toBeInTheDocument()
    expect(screen.getAllByText('3:30:01')).toHaveLength(2) // PB tile + the race row
    expect(screen.getByRole('img', { name: /hall.progressionOf:distance=hall.distance.marathon/ })).toBeInTheDocument()
    expect(screen.getByText('hall.pb')).toBeInTheDocument()
    // Guests are behind their own filter chip.
    expect(screen.queryByText('Bergen3000')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: 'Khatiya' }))
    expect(screen.getByText('Bergen3000')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'hall.me' }))

    await act(async () => { fireEvent.click(screen.getByRole('button', { name: /hall.confirmHigh/ })) })
    expect(calls.find(c => c.url === '/api/races/results/confirm')?.body).toEqual({ all_high: true })
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'hall.reject:race=Cardiff Half' })) })
    expect(calls.some(c => c.method === 'DELETE' && c.url === '/api/races/results/4')).toBe(true)

    await act(async () => { fireEvent.click(screen.getByRole('button', { name: /hall.findTime/ })) })
    expect(calls.some(c => c.method === 'POST' && c.url === '/api/races/results/3/lookup')).toBe(true)
  })

  it('adds a race by hand with a parsed time', async () => {
    const calls = installFetch()
    render(<MemoryRouter><HallOfFame /></MemoryRouter>)
    await act(async () => { fireEvent.click(await screen.findByRole('button', { name: /hall.add/ })) })
    fireEvent.change(screen.getByLabelText('hall.race'), { target: { value: 'Bergen Fjellmaraton' } })
    fireEvent.change(screen.getByLabelText('admin.date'), { target: { value: '2017-06-01' } })
    fireEvent.change(screen.getByLabelText('hall.time'), { target: { value: '1:58:40' } })
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'admin.save' })) })
    const post = calls.find(c => c.method === 'POST' && c.url === '/api/races/results')
    expect(post?.body).toMatchObject({ race_name: 'Bergen Fjellmaraton', race_date: '2017-06-01', distance_m: 21097, finish_seconds: 7120, time_source: 'manual', status: 'confirmed' })
  })
})
