// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, fireEvent, cleanup, act } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import { ResearchLogPanel, ResearchStatus } from './Research'
import type { ResearchRun } from './racesApi'

function mockT(key: string, opts?: Record<string, unknown>): string {
  if (opts && 'count' in opts) return `${key}:${opts.count}`
  if (opts && 'spent' in opts) return `${key}:${opts.spent}/${opts.budget}`
  return key
}
vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: mockT, i18n: { language: 'en' } }),
  initReactI18next: { type: '3rdParty', init: () => {} },
}))

function run(over: Partial<ResearchRun>): ResearchRun {
  return {
    id: 1, kind: 'race', event_id: 7, event_name: 'Berlin Marathon', trigger: 'manual', status: 'done',
    started_at: '2026-10-10T03:30:00Z', finished_at: '2026-10-10T03:32:00Z', cost_usd: 0.4, changes: 0,
    summary: '', sources: [], error: '', ...over,
  }
}

type Responder = (url: string, init?: RequestInit) => { status: number; body: unknown }
function installFetch(responder: Responder) {
  const calls: { url: string; method: string }[] = []
  vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
    calls.push({ url, method: init?.method ?? 'GET' })
    const { status, body } = responder(url, init)
    return { ok: status < 300, status, json: async () => body } as Response
  }))
  return calls
}

describe('ResearchStatus', () => {
  beforeEach(() => vi.useFakeTimers({ shouldAdvanceTime: true }))
  afterEach(() => {
    cleanup()
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  it('starts a check, polls until it finishes and reloads the race', async () => {
    let finished = false
    const calls = installFetch((_url, init) => {
      if (init?.method === 'POST') return { status: 202, body: { run: run({ id: 2, status: 'running', finished_at: '' }) } }
      return {
        status: 200,
        body: { research: finished ? run({ id: 2, status: 'done', changes: 3, summary: 'Lottery opened.' }) : run({ id: 2, status: 'running', finished_at: '' }) },
      }
    })
    const onFinished = vi.fn()
    render(<ResearchStatus eventId={7} run={null} onFinished={onFinished} />)
    expect(screen.getByText('research.never')).toBeInTheDocument()

    await act(async () => { fireEvent.click(screen.getByRole('button', { name: /research.check/ })) })
    expect(calls.some(c => c.method === 'POST' && c.url === '/api/races/7/research')).toBe(true)
    expect(screen.getByText('research.running')).toBeInTheDocument()

    finished = true
    await act(async () => { await vi.advanceTimersByTimeAsync(4100) })
    expect(onFinished).toHaveBeenCalled()
    expect(screen.getByText('research.updated:3')).toBeInTheDocument()
    expect(screen.getByText('Lottery opened.')).toBeInTheDocument()
  })

  it('explains a cooldown refusal', async () => {
    installFetch(() => ({ status: 429, body: { error: 'this race was checked recently' } }))
    render(<ResearchStatus eventId={7} run={run({ changes: 0 })} onFinished={() => {}} />)
    expect(screen.getByText('research.noChanges')).toBeInTheDocument()
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: /research.check/ })) })
    expect(screen.getByRole('alert')).toHaveTextContent('research.cooldown')
  })
})

// recharts measures its container; give it a size under happy-dom.
vi.mock('recharts', async () => {
  const actual = await vi.importActual<typeof import('recharts')>('recharts')
  return {
    ...actual,
    ResponsiveContainer: ({ children }: { children: React.ReactElement }) => <div style={{ width: 600, height: 200 }}>{children}</div>,
  }
})

const SETTINGS = {
  enabled: true, discovery_enabled: true, model: 'claude-sonnet-5-5', daily_budget_usd: 5, monthly_budget_usd: 60, nightly_max_races: 12,
  home_city: 'Bergen', home_airport: 'BGO',
}
const STATS = {
  today_usd: 1.2, last_7_days_usd: 3.4, month_to_date_usd: 9.5, last_30_days_usd: 12, all_time_usd: 20,
  race_checks_30d: 40, discoveries_30d: 4, avg_race_check_usd: 0.25, changes_30d: 31,
  daily: Array.from({ length: 30 }, (_, i) => ({ date: `2026-09-${String(i + 1).padStart(2, '0')}`, cost_usd: i === 29 ? 1.2 : 0, runs: i === 29 ? 3 : 0 })),
}

describe('ResearchLogPanel', () => {
  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
  })

  it('shows spend against the caps, the runs, saves limits and starts a discovery pass', async () => {
    let saved: unknown = null
    const calls = installFetch((url, init) => {
      if (url === '/api/races/research/settings' && init?.method === 'PUT') {
        saved = JSON.parse(String(init.body))
        return { status: 200, body: { settings: saved } }
      }
      if (init?.method === 'POST') return { status: 202, body: { run: run({ id: 9, kind: 'discover', event_id: null, status: 'running' }) } }
      return {
        status: 200,
        body: {
          stats: STATS, settings: SETTINGS, models: ['claude-haiku-4-5-20251001', 'claude-sonnet-5-5', 'claude-opus-5-5'],
          runs: [run({ status: 'done', changes: 2, summary: 'Price went up.' }), run({ id: 3, status: 'failed', error: 'timeout' })],
        },
      }
    })
    render(<MemoryRouter><ResearchLogPanel /></MemoryRouter>)
    expect(await screen.findByText('research.today')).toBeInTheDocument()
    expect(screen.getAllByText('$1.20').length).toBeGreaterThan(0) // today (tile and table)
    expect(screen.getAllByText('/ $5.00').length).toBe(1) // daily cap
    expect(screen.getByText('$9.50')).toBeInTheDocument() // month
    expect(screen.getByRole('img', { name: 'research.dailyChart' })).toBeInTheDocument()
    expect(screen.getAllByRole('link', { name: 'Berlin Marathon' })).toHaveLength(2)
    expect(screen.getByText(/Price went up\./)).toBeInTheDocument()
    expect(screen.getByText('timeout')).toBeInTheDocument()

    fireEvent.change(screen.getByLabelText('research.dailyBudget'), { target: { value: '2.5' } })
    fireEvent.change(screen.getByLabelText('research.model'), { target: { value: 'claude-haiku-4-5-20251001' } })
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'admin.save' })) })
    expect(saved).toEqual({ ...SETTINGS, daily_budget_usd: 2.5, model: 'claude-haiku-4-5-20251001' })
    expect(screen.getByRole('status')).toHaveTextContent('watch.saved')

    await act(async () => { fireEvent.click(screen.getByRole('button', { name: /research.discover/ })) })
    expect(calls.some(c => c.method === 'POST' && c.url === '/api/races/research/discover')).toBe(true)
  })
})
