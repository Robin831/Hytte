// @vitest-environment happy-dom
import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, fireEvent, cleanup, act } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import { FamilyChips, FamilyList, LedgerSection, SeriesSection } from './Phase5'

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
  vi.unstubAllGlobals()
})

describe('FamilyChips / FamilyList', () => {
  const family = [
    { user_id: 3, name: 'Kari', picture: '', state: 'registered' as const },
    { user_id: 4, name: 'Nok', picture: '', state: 'watching' as const },
  ]
  it('shows initials with names and statuses in the label', () => {
    render(<FamilyChips family={family} />)
    expect(screen.getByLabelText(/Kari: watchState.registered, Nok: watchState.watching/)).toBeInTheDocument()
    expect(screen.getByText('K')).toBeInTheDocument()
  })
  it('lists family members on the race page and renders nothing when alone', () => {
    const { container, rerender } = render(<FamilyList family={family} />)
    expect(screen.getByText('Kari')).toBeInTheDocument()
    rerender(<FamilyList family={[]} />)
    expect(container).toBeEmptyDOMElement()
  })
})

describe('SeriesSection', () => {
  it('shows progress and adds an earlier finish', async () => {
    const posts: unknown[] = []
    vi.stubGlobal('fetch', vi.fn(async (_url: string, init?: RequestInit) => {
      if (init?.method === 'POST') {
        posts.push(JSON.parse(String(init.body)))
        return { ok: true, status: 200, json: async () => ({ status: 'ok' }) } as Response
      }
      return {
        ok: true, status: 200, json: async () => ({
          series: [
            { key: 'majors', required: 7, done: 1, races: [
              { key: 'berlin', name: 'Berlin', finishes: [{ race_key: 'berlin', year: 2019, finish_seconds: 12000, source: 'manual' }], next_event_id: 5 },
              { key: 'boston', name: 'Boston', finishes: [], next_event_id: null },
            ] },
            { key: 'emc', required: 5, done: 0, races: [] },
          ],
        }),
      } as Response
    }))
    render(<MemoryRouter><SeriesSection /></MemoryRouter>)
    expect(await screen.findByText('series.progress:done=1,required=7')).toBeInTheDocument()
    expect(screen.getByRole('progressbar', { name: 'series.majors' })).toHaveAttribute('aria-valuenow', '1')
    expect(screen.getByText('2019 · 3:20:00')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'series.nextEdition' })).toHaveAttribute('href', '/races/5')

    const add = screen.getAllByRole('button', { name: /series.addFinish/ })[1] // Boston
    fireEvent.click(add)
    fireEvent.change(screen.getByLabelText('series.year'), { target: { value: '2023' } })
    fireEvent.change(screen.getByLabelText('series.time'), { target: { value: '3:05:10' } })
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'admin.save' })) })
    expect(posts).toEqual([{ race_key: 'boston', year: 2023, finish_seconds: 3 * 3600 + 5 * 60 + 10 }])
  })
})

describe('LedgerSection', () => {
  it('summarizes entries, win rate and losing streaks', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({
      ok: true, status: 200, json: async () => ({
        entered: 3, won: 1, lost: 2, pending: 0,
        streaks: [{ race: 'Berlin Marathon', losses: 2 }],
        entries: [
          { event_id: 1, name: 'London Marathon', edition_year: 2027, race_date: '2027-04-25', entered_at: '2026-05-01T10:00:00Z', outcome: 'won', decided_at: '' },
          { event_id: 2, name: 'Berlin Marathon', edition_year: 2027, race_date: '2027-09-26', entered_at: '2026-10-02T10:00:00Z', outcome: 'lost', decided_at: '' },
        ],
      }),
    }) as Response))
    render(<MemoryRouter><LedgerSection /></MemoryRouter>)
    expect(await screen.findByText(/ledger.summary:entered=3,won=1,lost=2,pending=0/)).toBeInTheDocument()
    expect(screen.getByText(/ledger.rate:rate=33/)).toBeInTheDocument()
    expect(screen.getByText('ledger.streak:race=Berlin Marathon,count=2')).toBeInTheDocument()
    expect(screen.getByText('ledger.outcome.won')).toBeInTheDocument()
  })
})
