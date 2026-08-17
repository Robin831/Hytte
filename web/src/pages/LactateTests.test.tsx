// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import LactateTests from './LactateTests'
import type { LactateTest, PrimaryThreshold } from '../types/lactate'

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

  // The page and DeltaBadge both call useTranslation, so `t` must be stable —
  // the load effect depends on it.
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

// The real AuthContext keeps `user` in state, so it is referentially stable
// across renders. Keep the stub stable too, or the load effect refires.
const authState = { user: { id: 1, email: 'a@b.c' } as object | null, loading: false }
vi.mock('../auth', () => ({
  useAuth: () => authState,
}))

// The real helpers pull in ../i18n; stub them so the tests stay locale-stable.
vi.mock('../utils/formatDate', () => ({
  formatDate: () => 'January 1, 2026',
  formatNumber: (n: number, opts?: Intl.NumberFormatOptions) => n.toFixed(opts?.minimumFractionDigits ?? 0),
}))

// Every icon renders as `<span data-testid="icon-kebab-name" />`; the assertions
// below query those testids. See src/test/lucideStub.tsx.
vi.mock('lucide-react', async () => (await import('../test/lucideStub')).lucideStub)

function makeTest(id: number, threshold?: PrimaryThreshold | null): LactateTest {
  return {
    id,
    date: '2026-01-01',
    comment: `Test ${id}`,
    protocol_type: 'incremental',
    warmup_duration_min: 10,
    stage_duration_min: 4,
    start_speed_kmh: 8,
    speed_increment_kmh: 1,
    stages: [],
    ...(threshold ? { primary_threshold: threshold } : {}),
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  }
}

function threshold(speed: number, hr: number, valid = true): PrimaryThreshold {
  return { method: 'Dmax', speed_kmh: speed, heart_rate_bpm: hr, valid }
}

function stubTests(tests: LactateTest[]) {
  vi.stubGlobal('fetch', vi.fn(() => Promise.resolve({
    ok: true,
    json: () => Promise.resolve({ tests }),
  })))
}

function renderPage() {
  return render(
    <MemoryRouter>
      <LactateTests />
    </MemoryRouter>,
  )
}

// The badge text node sits inside the coloured wrapper span.
function badgeWrapper(text: string): HTMLElement {
  const el = screen.getByText(text)
  return el.parentElement as HTMLElement
}

describe('LactateTests summary card', () => {
  beforeEach(() => {
    authState.user = { id: 1, email: 'a@b.c' }
  })
  afterEach(() => {
    vi.unstubAllGlobals()
    vi.clearAllMocks()
  })

  it('renders no summary card when the user has no tests', async () => {
    stubTests([])
    renderPage()

    expect(await screen.findByText('No lactate tests yet. Record your first test to get started.')).toBeInTheDocument()
    expect(screen.queryByText('Latest threshold')).not.toBeInTheDocument()
  })

  it('shows the latest-threshold card for a user with exactly one test', async () => {
    stubTests([makeTest(1, threshold(12.5, 165))])
    renderPage()

    expect(await screen.findByText('Latest threshold')).toBeInTheDocument()
    expect(screen.getByText('12.5 km/h · 165 bpm')).toBeInTheDocument()
    // The method is shown next to the value, and again in the row chip.
    expect(screen.getAllByText('Dmax').length).toBeGreaterThan(0)
  })

  it('shows the no-previous-comparison note and no deltas for a single test', async () => {
    stubTests([makeTest(1, threshold(12.5, 165))])
    renderPage()

    expect(await screen.findByText('No previous test to compare.')).toBeInTheDocument()
    expect(screen.queryByText('vs previous test')).not.toBeInTheDocument()
    expect(screen.queryByTestId('icon-arrow-up')).not.toBeInTheDocument()
    expect(screen.queryByTestId('icon-arrow-down')).not.toBeInTheDocument()
  })

  it('does not show the Insights link with a single test, but does with two', async () => {
    stubTests([makeTest(1, threshold(12.5, 165))])
    const { unmount } = renderPage()

    expect(await screen.findByText('Latest threshold')).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: /Insights/ })).not.toBeInTheDocument()
    unmount()

    stubTests([makeTest(2, threshold(12.5, 165)), makeTest(1, threshold(12.0, 162))])
    renderPage()

    expect(await screen.findByRole('link', { name: /Insights/ })).toHaveAttribute('href', '/lactate/insights')
  })

  it('falls back to the no-result copy when the single test has no primary threshold', async () => {
    stubTests([makeTest(1)])
    renderPage()

    expect(await screen.findByText('Latest threshold')).toBeInTheDocument()
    expect(screen.getByText('No result yet')).toBeInTheDocument()
    expect(screen.queryByText('No previous test to compare.')).not.toBeInTheDocument()
    expect(document.body.textContent).not.toMatch(/NaN/)
  })

  it('falls back to the no-result copy when the single test threshold is invalid', async () => {
    stubTests([makeTest(1, threshold(12.5, 165, false))])
    renderPage()

    expect(await screen.findByText('Latest threshold')).toBeInTheDocument()
    expect(screen.getByText('No result yet')).toBeInTheDocument()
    expect(document.body.textContent).not.toMatch(/NaN/)
  })

  it('keeps green-up / neutral-HR colouring when the speed improved', async () => {
    stubTests([makeTest(2, threshold(12.5, 168)), makeTest(1, threshold(12.0, 165))])
    renderPage()

    expect(await screen.findByText('vs previous test')).toBeInTheDocument()

    const speed = badgeWrapper('+0.5 km/h')
    expect(speed.className).toContain('text-green-400')
    expect(speed.querySelector('[data-testid="icon-arrow-up"]')).not.toBeNull()

    const hr = badgeWrapper('+3 bpm')
    expect(hr.className).toContain('text-gray-400')
    expect(hr.className).not.toContain('text-green-400')
    expect(hr.querySelector('[data-testid="icon-arrow-up"]')).not.toBeNull()
  })

  it('keeps red-down for speed while the HR badge stays neutral when both fall', async () => {
    stubTests([makeTest(2, threshold(11.5, 160)), makeTest(1, threshold(12.0, 165))])
    renderPage()

    await waitFor(() => {
      expect(screen.getByText('vs previous test')).toBeInTheDocument()
    })

    const speed = badgeWrapper('−0.5 km/h')
    expect(speed.className).toContain('text-red-400')
    expect(speed.querySelector('[data-testid="icon-arrow-down"]')).not.toBeNull()

    const hr = badgeWrapper('−5 bpm')
    expect(hr.className).toContain('text-gray-400')
    expect(hr.className).not.toContain('text-red-400')
    expect(hr.querySelector('[data-testid="icon-arrow-down"]')).not.toBeNull()
  })
})
