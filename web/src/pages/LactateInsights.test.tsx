// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import LactateInsights from './LactateInsights'
import type { LactateTest, Analysis } from '../types/lactate'

vi.mock('react-i18next', async () => {
  const { default: enLactate } = await import('../../public/locales/en/lactate.json')

  function resolveKey(obj: Record<string, unknown>, parts: string[]): unknown {
    const [head, ...rest] = parts
    const val = obj[head]
    if (rest.length === 0) return val
    if (val && typeof val === 'object' && !Array.isArray(val)) {
      return resolveKey(val as Record<string, unknown>, rest)
    }
    return undefined
  }

  // `t` must be stable — the tests load effect depends on it.
  const t = (key: string, opts?: Record<string, unknown>): string => {
    const localKey = key.includes(':') ? key.slice(key.indexOf(':') + 1) : key
    const val = resolveKey(enLactate as Record<string, unknown>, localKey.split('.'))
    if (typeof val !== 'string') return key
    if (opts) return val.replace(/\{\{(\w+)\}\}/g, (_, k) => String(opts[k] ?? `{{${k}}}`))
    return val
  }
  const translation = { t, i18n: { language: 'en' } }

  return {
    useTranslation: () => translation,
    initReactI18next: { type: '3rdParty', init: () => {} },
  }
})

// Keep `user` referentially stable, as the real AuthContext does.
const authState = { user: { id: 1, email: 'a@b.c' } as object | null, loading: false }
vi.mock('../auth', () => ({
  useAuth: () => authState,
}))

vi.mock('../utils/formatDate', () => ({
  formatDate: () => 'Jan 1',
}))

vi.mock('lucide-react', async () => (await import('../test/lucideStub')).lucideStub)

// Expose the trend data the page computes instead of rendering recharts.
vi.mock('../components/charts/ThresholdTrendsChart', () => ({
  default: ({ data }: { data: { date: string; speed: number }[] }) => (
    <div data-testid="trend-chart">
      {data.map((d) => (
        <span key={d.date} data-testid="trend-point">{`${d.date}:${d.speed}`}</span>
      ))}
    </div>
  ),
}))
vi.mock('../components/charts/FixedSpeedChart', () => ({ default: () => null }))
vi.mock('../components/charts/ComparisonChart', () => ({ default: () => null }))

function makeTest(id: number, date: string): LactateTest {
  return {
    id,
    date,
    comment: '',
    protocol_type: 'standard',
    warmup_duration_min: 10,
    stage_duration_min: 5,
    start_speed_kmh: 11.5,
    speed_increment_kmh: 0.5,
    stages: [],
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  }
}

function makeAnalysis(speed: number): Analysis {
  return {
    thresholds: [{ method: 'OBLA', speed_kmh: speed, lactate_mmol: 4, heart_rate_bpm: 170, valid: true }],
    zones: [],
    predictions: [],
    traffic_lights: [],
    method_used: 'OBLA',
  }
}

type AnalysesResponse =
  | { ok: true; body: Record<string, Analysis> }
  | { ok: false }
  | { reject: Error }

function stubFetch(tests: LactateTest[], analyses: AnalysesResponse) {
  const fetchMock = vi.fn((url: string) => {
    if (url === '/api/lactate/tests') {
      return Promise.resolve({ ok: true, json: () => Promise.resolve({ tests }) })
    }
    if (url === '/api/lactate/analyses') {
      if ('reject' in analyses) return Promise.reject(analyses.reject)
      if (!analyses.ok) return Promise.resolve({ ok: false, status: 500, json: () => Promise.resolve({}) })
      return Promise.resolve({ ok: true, json: () => Promise.resolve(analyses.body) })
    }
    return Promise.reject(new Error(`unexpected fetch ${url}`))
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

function renderPage() {
  return render(
    <MemoryRouter>
      <LactateInsights />
    </MemoryRouter>,
  )
}

describe('LactateInsights analyses loading', () => {
  beforeEach(() => {
    authState.user = { id: 1, email: 'a@b.c' }
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('attaches analyses keyed by test ID to the matching tests', async () => {
    const fetchMock = stubFetch(
      [makeTest(1, '2026-01-01'), makeTest(2, '2026-02-01')],
      { ok: true, body: { '1': makeAnalysis(13), '2': makeAnalysis(14) } },
    )
    renderPage()

    await waitFor(() => expect(screen.getAllByTestId('trend-point')).toHaveLength(2))
    expect(screen.getAllByTestId('trend-point').map((el) => el.textContent)).toEqual([
      '2026-01-01:13',
      '2026-02-01:14',
    ])
    expect(screen.queryByText('Analyzing tests...')).toBeNull()
    expect(fetchMock.mock.calls.filter(([url]) => url === '/api/lactate/analyses')).toHaveLength(1)
  })

  it('leaves tests missing from the response out of the trend', async () => {
    stubFetch(
      [makeTest(1, '2026-01-01'), makeTest(2, '2026-02-01'), makeTest(3, '2026-03-01')],
      { ok: true, body: { '1': makeAnalysis(13), '3': makeAnalysis(15) } },
    )
    renderPage()

    await waitFor(() => expect(screen.getAllByTestId('trend-point')).toHaveLength(2))
    expect(screen.getAllByTestId('trend-point').map((el) => el.textContent)).toEqual([
      '2026-01-01:13',
      '2026-03-01:15',
    ])
  })

  it('clears the spinner when the analyses request returns 500', async () => {
    stubFetch([makeTest(1, '2026-01-01'), makeTest(2, '2026-02-01')], { ok: false })
    renderPage()

    await waitFor(() => expect(screen.getByTestId('trend-chart')).toBeTruthy())
    expect(screen.queryByText('Analyzing tests...')).toBeNull()
    expect(screen.queryAllByTestId('trend-point')).toHaveLength(0)
  })

  it('clears the spinner when the analyses request rejects', async () => {
    stubFetch([makeTest(1, '2026-01-01'), makeTest(2, '2026-02-01')], { reject: new TypeError('network down') })
    renderPage()

    await waitFor(() => expect(screen.getByTestId('trend-chart')).toBeTruthy())
    expect(screen.queryByText('Analyzing tests...')).toBeNull()
    expect(screen.queryAllByTestId('trend-point')).toHaveLength(0)
  })

  it('does not request analyses or leave a spinner when there are no tests', async () => {
    const fetchMock = stubFetch([], { ok: true, body: {} })
    renderPage()

    await waitFor(() => expect(screen.getByText('At least 2 tests are needed for insights.')).toBeTruthy())
    expect(screen.queryByText('Analyzing tests...')).toBeNull()
    expect(fetchMock.mock.calls.some(([url]) => url === '/api/lactate/analyses')).toBe(false)
  })
})
