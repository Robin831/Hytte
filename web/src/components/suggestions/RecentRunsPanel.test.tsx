// @vitest-environment happy-dom
import { describe, it, expect, vi, afterEach, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react'
import { RecentRunsPanel } from './RecentRunsPanel'
import enCommon from '../../../public/locales/en/common.json'
import enSuggestions from '../../../public/locales/en/suggestions.json'

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

function format(template: string, vars?: Record<string, unknown>): string {
  if (!vars) return template
  return template.replace(/\{\{(\w+)\}\}/g, (_, k) => String(vars[k] ?? ''))
}

function makeT(translations: JsonObject) {
  return function t(key: string, vars?: Record<string, unknown>): string {
    const val = resolveKey(translations, key.split('.'))
    return typeof val === 'string' ? format(val, vars) : key
  }
}

const namespaceMap: Record<string, JsonObject> = {
  common: enCommon as unknown as JsonObject,
  suggestions: enSuggestions as unknown as JsonObject,
}

vi.mock('react-i18next', () => ({
  useTranslation: (ns: string = 'common') => ({
    t: makeT(namespaceMap[ns] ?? (enCommon as unknown as JsonObject)),
    i18n: { language: 'en' },
  }),
  Trans: ({ i18nKey }: { i18nKey: string }) => i18nKey,
  initReactI18next: { type: '3rdParty', init: () => {} },
}))

vi.mock('../../utils/formatDate', () => ({
  formatDate: (date: Date | string, options?: Intl.DateTimeFormatOptions) => {
    const d = typeof date === 'string' ? new Date(date) : date
    return d.toLocaleDateString('en', options)
  },
  formatDateTime: (date: Date | string, options?: Intl.DateTimeFormatOptions) => {
    const d = typeof date === 'string' ? new Date(date) : date
    return d.toLocaleString('en', options)
  },
  formatTime: (date: Date | string, options?: Intl.DateTimeFormatOptions) => {
    const d = typeof date === 'string' ? new Date(date) : date
    return d.toLocaleTimeString('en', options)
  },
  formatNumber: (n: number, options?: Intl.NumberFormatOptions) => n.toLocaleString('en', options),
  toLocalDateString: (date?: Date) => {
    const d = date ?? new Date()
    const y = d.getFullYear()
    const m = String(d.getMonth() + 1).padStart(2, '0')
    const day = String(d.getDate()).padStart(2, '0')
    return `${y}-${m}-${day}`
  },
}))

beforeEach(() => {
  // Pin Date so the 7-day summary computation is deterministic.
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date('2026-05-06T20:00:00Z'))
})

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
  vi.clearAllMocks()
})

describe('RecentRunsPanel', () => {
  it('starts collapsed and only fetches when expanded', async () => {
    const fetchMock = vi.fn(() =>
      Promise.resolve({ ok: true, json: () => Promise.resolve([]) }),
    )
    vi.stubGlobal('fetch', fetchMock)

    render(<RecentRunsPanel />)

    const toggle = screen.getByRole('button', { name: /Recent runs/ })
    expect(toggle).toHaveAttribute('aria-expanded', 'false')
    expect(screen.queryByTestId('recent-runs-content')).not.toBeInTheDocument()
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('expanding fetches runs and renders the empty placeholder when API returns []', async () => {
    const fetchMock = vi.fn((url: string) => {
      if (url === '/api/suggestions/runs?limit=20') {
        return Promise.resolve({ ok: true, json: () => Promise.resolve([]) })
      }
      return Promise.reject(new Error(`Unexpected fetch: ${url}`))
    })
    vi.stubGlobal('fetch', fetchMock)

    render(<RecentRunsPanel />)

    fireEvent.click(screen.getByRole('button', { name: /Recent runs/ }))

    await waitFor(() => {
      expect(screen.getByTestId('recent-runs-empty')).toBeInTheDocument()
    })
    expect(screen.getByTestId('recent-runs-empty')).toHaveTextContent(
      'No runs yet — try Run now.',
    )
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/suggestions/runs?limit=20',
      expect.objectContaining({ credentials: 'include' }),
    )
  })

  it('renders populated rows with trigger badges, page counts, and the 7-day summary', async () => {
    const runs = [
      {
        id: 1,
        user_id: 1,
        started_at: '2026-05-06T18:00:00Z',
        finished_at: '2026-05-06T18:01:00Z',
        trigger: 'manual',
        page_slugs: 'weather,notes,training',
        generated: 3,
        errors: 0,
        cost_usd: 0.0123,
      },
      {
        id: 2,
        user_id: 1,
        started_at: '2026-05-05T03:00:00Z',
        finished_at: '2026-05-05T03:02:00Z',
        trigger: 'scheduled',
        page_slugs: 'budget,__new_page__',
        generated: 2,
        errors: 1,
        cost_usd: 0.5,
      },
      // Older than 7 days — must be excluded from the summary aggregation.
      {
        id: 3,
        user_id: 1,
        started_at: '2026-04-20T03:00:00Z',
        finished_at: '2026-04-20T03:02:00Z',
        trigger: 'scheduled',
        page_slugs: 'links',
        generated: 99,
        errors: 0,
        cost_usd: 1.5,
      },
    ]
    const fetchMock = vi.fn(() =>
      Promise.resolve({ ok: true, json: () => Promise.resolve(runs) }),
    )
    vi.stubGlobal('fetch', fetchMock)

    render(<RecentRunsPanel />)
    fireEvent.click(screen.getByRole('button', { name: /Recent runs/ }))

    await waitFor(() => {
      expect(screen.getByTestId('recent-run-1')).toBeInTheDocument()
    })

    const row1 = screen.getByTestId('recent-run-1')
    expect(within(row1).getByTestId('recent-run-1-trigger')).toHaveTextContent('manual')
    expect(within(row1).getByTestId('recent-run-1-pages')).toHaveTextContent('3')
    expect(within(row1).getByTestId('recent-run-1-cost')).toHaveTextContent('$0.0123')

    const row2 = screen.getByTestId('recent-run-2')
    expect(within(row2).getByTestId('recent-run-2-trigger')).toHaveTextContent('scheduled')
    expect(within(row2).getByTestId('recent-run-2-pages')).toHaveTextContent('2')
    expect(within(row2).getByTestId('recent-run-2-cost')).toHaveTextContent('$0.5000')

    // Older row is rendered too, but the summary should exclude it (only last 7 days).
    expect(screen.getByTestId('recent-run-3')).toBeInTheDocument()

    const summary = screen.getByTestId('recent-runs-summary')
    // 0.0123 + 0.5 = 0.5123 → "$0.51", generated 3 + 2 = 5 (excludes the >7d row's 99).
    expect(summary).toHaveTextContent('Last 7 days: $0.51 total, 5 suggestions.')
  })

  it('cost formatting boundary: cost_usd of 0 shows the placeholder, non-zero shows $X.XXXX', async () => {
    const runs = [
      {
        id: 10,
        user_id: 1,
        started_at: '2026-05-06T10:00:00Z',
        finished_at: '2026-05-06T10:01:00Z',
        trigger: 'manual',
        page_slugs: 'weather',
        generated: 1,
        errors: 0,
        cost_usd: 0,
      },
      {
        id: 11,
        user_id: 1,
        started_at: '2026-05-06T11:00:00Z',
        finished_at: '2026-05-06T11:01:00Z',
        trigger: 'manual',
        page_slugs: 'notes',
        generated: 1,
        errors: 0,
        cost_usd: 0.0123,
      },
    ]
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve({ ok: true, json: () => Promise.resolve(runs) })),
    )

    render(<RecentRunsPanel />)
    fireEvent.click(screen.getByRole('button', { name: /Recent runs/ }))

    await waitFor(() => {
      expect(screen.getByTestId('recent-run-10')).toBeInTheDocument()
    })

    expect(screen.getByTestId('recent-run-10-cost')).toHaveTextContent('—')
    expect(screen.getByTestId('recent-run-11-cost')).toHaveTextContent('$0.0123')
  })

  it('shows an error banner when the runs fetch fails', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve({ ok: false, status: 500, json: () => Promise.resolve({}) })),
    )

    render(<RecentRunsPanel />)
    fireEvent.click(screen.getByRole('button', { name: /Recent runs/ }))

    await waitFor(() => {
      expect(screen.getByTestId('recent-runs-error')).toBeInTheDocument()
    })
    expect(screen.getByTestId('recent-runs-error')).toHaveTextContent(
      'Failed to load recent runs.',
    )
  })

  it('reloadSignal > 0 opens the panel and triggers a fetch', async () => {
    const runs = [
      {
        id: 20,
        user_id: 1,
        started_at: '2026-05-06T19:00:00Z',
        finished_at: '2026-05-06T19:01:00Z',
        trigger: 'manual',
        page_slugs: 'weather',
        generated: 1,
        errors: 0,
        cost_usd: 0.01,
      },
    ]
    let fetchCalls = 0
    const fetchMock = vi.fn((url: string) => {
      if (url === '/api/suggestions/runs?limit=20') {
        fetchCalls += 1
        return Promise.resolve({ ok: true, json: () => Promise.resolve(runs) })
      }
      return Promise.reject(new Error(`Unexpected fetch: ${url}`))
    })
    vi.stubGlobal('fetch', fetchMock)

    const { rerender } = render(<RecentRunsPanel reloadSignal={0} />)

    // Initially collapsed, no fetch.
    expect(screen.getByRole('button', { name: /Recent runs/ })).toHaveAttribute('aria-expanded', 'false')
    expect(fetchMock).not.toHaveBeenCalled()

    // Incrementing reloadSignal should open the panel and trigger a fetch.
    rerender(<RecentRunsPanel reloadSignal={1} />)

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /Recent runs/ })).toHaveAttribute('aria-expanded', 'true')
    })
    await waitFor(() => {
      expect(screen.getByTestId('recent-run-20')).toBeInTheDocument()
    })
    expect(fetchCalls).toBeGreaterThanOrEqual(1)
  })

  it('toggling collapses the panel and hides content', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve({ ok: true, json: () => Promise.resolve([]) })),
    )

    render(<RecentRunsPanel />)
    const toggle = screen.getByRole('button', { name: /Recent runs/ })

    fireEvent.click(toggle)
    await waitFor(() => {
      expect(screen.getByTestId('recent-runs-empty')).toBeInTheDocument()
    })
    expect(toggle).toHaveAttribute('aria-expanded', 'true')

    fireEvent.click(toggle)
    expect(toggle).toHaveAttribute('aria-expanded', 'false')
    expect(screen.queryByTestId('recent-runs-content')).not.toBeInTheDocument()
  })
})
