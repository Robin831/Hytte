// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import TrainingTrends from './TrainingTrends'
import enTraining from '../../public/locales/en/training.json'

// ── Translation mock ──────────────────────────────────────────────────────────
// Resolves against the real English training bundle so the assertions below pin
// the rendered copy, not key names.

type JsonValue = string | number | boolean | null | JsonObject | JsonValue[]
interface JsonObject { [key: string]: JsonValue }

function resolveKey(obj: JsonObject, parts: string[]): JsonValue | undefined {
  const [head, ...rest] = parts
  const val = obj[head]
  if (rest.length === 0) return val
  if (val && typeof val === 'object' && !Array.isArray(val)) {
    return resolveKey(val as JsonObject, rest)
  }
  return undefined
}

function makeT(translations: JsonObject) {
  return function t(key: string, opts?: Record<string, unknown>): string {
    const localKey = key.includes(':') ? key.slice(key.indexOf(':') + 1) : key
    const val = resolveKey(translations, localKey.split('.'))
    if (typeof val === 'string') {
      return val.replace(/\{\{(\w+)\}\}/g, (_, k) => String(opts?.[k] ?? `{{${k}}}`))
    }
    if (typeof opts?.defaultValue === 'string') return opts.defaultValue
    return localKey
  }
}

// t is cached so it stays referentially stable across renders — the page has it
// in a useEffect dependency array.
const stableT = makeT(enTraining as unknown as JsonObject)

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: stableT, i18n: { language: 'en' } }),
  Trans: ({ i18nKey }: { i18nKey: string }) => i18nKey,
  initReactI18next: { type: '3rdParty', init: () => {} },
}))

vi.mock('../utils/formatDate', () => ({
  formatDate: () => 'Jun 1',
  formatTime: () => '08:00',
  formatNumber: (n: number, options?: Intl.NumberFormatOptions) =>
    new Intl.NumberFormat('en', options).format(n),
}))

// ── Auth mock ─────────────────────────────────────────────────────────────────

const authState: { user: object | null } = { user: { id: 1, email: 'a@b.c', name: 'Tester' } }

vi.mock('../auth', () => ({
  useAuth: () => authState,
}))

// ── Sub-components / chart library ────────────────────────────────────────────

vi.mock('recharts', () => ({
  ResponsiveContainer: ({ children }: { children: unknown }) => children,
  LineChart: ({ children }: { children: unknown }) => <div data-testid="line-chart">{children as never}</div>,
  BarChart: ({ children }: { children: unknown }) => <div>{children as never}</div>,
  ComposedChart: ({ children }: { children: unknown }) => <div>{children as never}</div>,
  Line: () => null,
  Bar: () => null,
  XAxis: () => null,
  YAxis: () => null,
  CartesianGrid: () => null,
  Tooltip: () => null,
  Legend: () => null,
}))

vi.mock('../components/WeeklyAiSummary', () => ({ WeeklyAiSummary: () => null }))
vi.mock('../components/training/RacePredictionsCard', () => ({ default: () => null }))
vi.mock('../components/AcrGauge', () => ({ AcrGauge: () => null }))
vi.mock('../components/TrainingStatusBadge', () => ({ default: () => null }))

vi.mock('lucide-react', async () => (await import('../test/lucideStub')).lucideStub)

// ── Fixtures ──────────────────────────────────────────────────────────────────

function estimate(vo2max: number, day: number) {
  return {
    id: day,
    user_id: 1,
    workout_id: day,
    vo2max,
    method: 'daniels',
    estimated_at: `2026-06-${String(day).padStart(2, '0')}T10:00:00Z`,
  }
}

const NOISY_RESPONSE = {
  history: [estimate(37, 1), estimate(56, 2), estimate(44, 3), estimate(51, 4), estimate(41, 5)],
  latest: estimate(41, 5),
  trend: 'noisy',
  summary: { count: 5, median: 44, low: 37, high: 56, spread: 19, noisy: true },
}

const TIGHT_RESPONSE = {
  history: [estimate(48, 1), estimate(48.5, 2), estimate(49, 3), estimate(49.5, 4), estimate(50, 5)],
  latest: estimate(50, 5),
  trend: 'improving',
  summary: { count: 5, median: 49, low: 48, high: 50, spread: 2, noisy: false },
}

const NO_SUMMARY_RESPONSE = {
  history: [],
  latest: null,
  trend: 'stable',
  summary: null,
}

// The page loads five endpoints in parallel; only the VO2max one varies per
// test, the rest return empty payloads of the shape the page expects.
function makeFetchMock(vo2maxRes: object) {
  return vi.fn((url: string) => {
    let body: object = {}
    if (url.includes('/training/vo2max')) body = vo2maxRes
    else if (url.includes('/training/load')) body = { weeks: [], acr: null, status: 'optimal' }
    else if (url.includes('/training/summary')) body = { summaries: [] }
    else if (url.includes('/training/progression')) body = { groups: [] }
    return Promise.resolve({ ok: true, json: () => Promise.resolve(body) })
  })
}

function renderPage(vo2maxRes: object) {
  vi.stubGlobal('fetch', makeFetchMock(vo2maxRes))
  render(
    <MemoryRouter>
      <TrainingTrends />
    </MemoryRouter>
  )
}

describe('TrainingTrends VO2max card', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('reports the median and range instead of the latest estimate', async () => {
    renderPage(NOISY_RESPONSE)

    expect(await screen.findByText('Median: 44 mL/kg/min')).toBeTruthy()
    expect(screen.getByText('Range: 37–56 (n=5)')).toBeTruthy()
    // The old header printed the single newest value; it must be gone.
    expect(screen.queryByText(/Latest/)).toBeNull()
  })

  it('replaces the trend line with the noise caveat when the spread is noise', async () => {
    renderPage(NOISY_RESPONSE)

    expect(await screen.findByText('Spread 19 – too noisy to read a trend')).toBeTruthy()
    expect(screen.queryByText(/^Trend:/)).toBeNull()
    // trend 'noisy' has no locale entry, and must never be rendered raw: the
    // caveat is the only way the noisy state reaches the screen.
    expect(screen.queryByText(/noisy$/)).toBeNull()
  })

  it('shows the trend direction when the estimates cluster tightly', async () => {
    renderPage(TIGHT_RESPONSE)

    expect(await screen.findByText('Median: 49 mL/kg/min')).toBeTruthy()
    expect(screen.getByText('Range: 48–50 (n=5)')).toBeTruthy()
    expect(screen.getByText('Trend: Improving')).toBeTruthy()
    expect(screen.queryByText(/too noisy/)).toBeNull()
  })

  it('renders the card without a header summary when the backend reports none', async () => {
    renderPage(NO_SUMMARY_RESPONSE)

    // The card itself still renders (with its empty state); only the
    // median/range/trend header is withheld.
    expect(await screen.findByText('VO2max History')).toBeTruthy()
    expect(screen.getByText('No VO2max data yet')).toBeTruthy()
    expect(screen.queryByText(/Median:/)).toBeNull()
    expect(screen.queryByText(/^Trend:/)).toBeNull()
  })

  it('renders the chart when history is present', async () => {
    renderPage(TIGHT_RESPONSE)

    await waitFor(() => expect(screen.getByTestId('line-chart')).toBeTruthy())
  })
})
