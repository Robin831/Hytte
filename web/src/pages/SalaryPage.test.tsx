// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor, fireEvent, act } from '@testing-library/react'
import { parseDecimal } from '../utils/parseDecimal'
import SalaryPage from './SalaryPage'
import { addMonth, formatMonthLabel } from './salary/types'
import type { EstimateResponse, TrekktabellParams, VacationResponse } from './salary/types'

describe('parseDecimal', () => {
  it('parses Norwegian comma decimals', () => {
    expect(parseDecimal('7,5')).toBe(7.5)
    expect(parseDecimal('1234,56')).toBe(1234.56)
  })

  it('parses dot decimals unchanged', () => {
    expect(parseDecimal('7.5')).toBe(7.5)
  })

  it('parses integers', () => {
    expect(parseDecimal('42')).toBe(42)
  })

  it('returns NaN for empty input', () => {
    expect(Number.isNaN(parseDecimal(''))).toBe(true)
    expect(Number.isNaN(parseDecimal('  '))).toBe(true)
  })

  it('returns NaN for unparseable input', () => {
    expect(Number.isNaN(parseDecimal('abc'))).toBe(true)
  })

  it('preserves explicit zero', () => {
    expect(parseDecimal('0')).toBe(0)
    expect(parseDecimal('0,0')).toBe(0)
  })
})

// ── Translation mock ──────────────────────────────────────────────────────────
// mockT is a stable reference so effects that depend on `t` do not re-run.

const TRANSLATIONS: Record<string, string> = {
  'title': 'Salary Estimator',
  'config.edit': 'Edit Config',
  'year.tabs.month': 'This Month',
  'year.tabs.year': 'Year View',
  'month.prev': 'Previous month',
  'month.next': 'Next month',
  'hero.estimate': 'Estimate',
  'hero.actual': 'Actual',
  'hero.gross': 'Gross',
  'hero.tax': 'Tax',
  'hero.net': 'Net',
  'hours.title': 'Hours',
  'hours.worked': '{{worked}} / {{total}} h',
  'workingDays.title': 'Working Days',
  'vacation.title': 'Vacation',
  'vacation.used': '{{used}} of {{allowance}} days used',
  'trekktabell.title': 'Trekktabell',
  'trekktabell.year': 'Year {{year}}',
  'trekktabell.edit': 'Edit',
  'trekktabellAssignments.title': 'Tabelltrekk assignments',
  'actions.retry': 'Retry',
  'a11y.refreshing': 'Updating month estimate…',
  'errors.failedToLoad': 'Failed to load salary data',
  'errors.failedToLoadVacation': 'Failed to load vacation data',
  'errors.failedToLoadTrekktabell': 'Failed to load trekktabell parameters',
}

function mockT(key: string, opts?: Record<string, string | number>): string {
  const template = TRANSLATIONS[key]
  if (template === undefined) return key
  if (!opts) return template
  return template.replace(/\{\{(\w+)\}\}/g, (_, name: string) => String(opts[name] ?? ''))
}

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: mockT,
    i18n: { language: 'en' },
  }),
  initReactI18next: { type: '3rdParty', init: () => {} },
}))

// ── Auth mock ─────────────────────────────────────────────────────────────────

vi.mock('../auth', () => ({
  useAuth: () => ({ user: { id: 1, is_admin: false } }),
}))

// ── Fixtures & fetch routing ──────────────────────────────────────────────────

function makeEstimate(month: string, hoursWorked = 120): EstimateResponse {
  return {
    month,
    config: {
      id: 1, user_id: 1, base_salary: 50000, hourly_rate: 1200,
      internal_hourly_rate: 600, standard_hours: 7.5, currency: 'NOK',
      taxable_benefits: 0, effective_from: '2026-01-01',
    },
    commission_tiers: [],
    adjusted_commission_tiers: [],
    estimate: {
      id: 1, user_id: 1, month, working_days: 20, hours_worked: hoursWorked,
      billable_hours: hoursWorked, internal_hours: 0, base_amount: 50000,
      commission: 0, gross: 50000, tax: 15000, net: 35000,
      vacation_days: 0, sick_days: 0, is_estimate: true,
    },
    working_days: 20,
    working_days_done: 10,
    working_days_remaining: 10,
    hours_worked: hoursWorked,
    internal_hours_worked: 0,
    standard_hours_total: 150,
    billable_revenue: 144000,
    internal_revenue: 0,
    absence_cost_per_day: 0,
    sick_day_cost: 0,
    vacation_day_cost: 0,
    extra_hour_net: 0,
  }
}

const VACATION: VacationResponse = {
  year: 2026, days_allowance: 25, days_used: 5, days_remaining: 20,
  gross_ytd: 400000, feriepenger_pct: 12, feriepenger_accrued: 48000,
}

const TREKKTABELL: TrekktabellParams = {
  id: 1, user_id: 1, year: 2026,
  minstefradrag_rate: 0.46, minstefradrag_min: 4000, minstefradrag_max: 92000,
  personfradrag: 88250, alminnelig_skatt_rate: 0.22, trygdeavgift: 0.078,
  trinnskatt_tiers: [],
}

type FakeResponse = { ok: boolean; status: number; json: () => Promise<unknown>; text: () => Promise<string> }

function jsonOk(data: unknown): Promise<FakeResponse> {
  return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve(data), text: () => Promise.resolve('') })
}

function httpError(status: number, body: string): Promise<FakeResponse> {
  return Promise.resolve({
    ok: false,
    status,
    json: () => Promise.resolve({ error: body }),
    text: () => Promise.resolve(body),
  })
}

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((res, rej) => { resolve = res; reject = rej })
  return { promise, resolve, reject }
}

// Mutable routing table — individual tests swap a single route to fail or hang.
const routes = {
  estimate: (month: string): Promise<FakeResponse> => jsonOk(makeEstimate(month)),
  vacation: (): Promise<FakeResponse> => jsonOk(VACATION),
  trekktabell: (): Promise<FakeResponse> => jsonOk(TREKKTABELL),
  assignments: (): Promise<FakeResponse> => jsonOk({ assignments: [] }),
}

function installFetch() {
  const fetchMock = vi.fn((input: string) => {
    const url = String(input)
    if (url.startsWith('/api/salary/estimate/month')) {
      const month = new URLSearchParams(url.split('?')[1] ?? '').get('month') ?? ''
      return routes.estimate(month)
    }
    if (url.startsWith('/api/salary/trekktabell-assignments')) return routes.assignments()
    if (url.startsWith('/api/salary/trekktabell')) return routes.trekktabell()
    if (url.startsWith('/api/salary/vacation')) return routes.vacation()
    return jsonOk({})
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

const currentMonthStr = (() => {
  const d = new Date()
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}`
})()
const nextMonthStr = addMonth(currentMonthStr, 1)
const currentMonthLabel = formatMonthLabel(currentMonthStr, 'en')
const nextMonthLabel = formatMonthLabel(nextMonthStr, 'en')

async function renderLoaded() {
  render(<SalaryPage />)
  await waitFor(() => expect(screen.getByText(currentMonthLabel)).toBeInTheDocument())
}

// The tab switcher and config toggle only exist in the full page render — their
// presence proves the page shell was never torn down.
function expectShellVisible() {
  expect(screen.getByRole('heading', { name: 'Salary Estimator' })).toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'This Month' })).toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'Year View' })).toBeInTheDocument()
  expect(screen.getByRole('button', { name: /Edit Config/ })).toBeInTheDocument()
}

describe('SalaryPage – month navigation continuity', () => {
  beforeEach(() => {
    routes.estimate = (month: string) => jsonOk(makeEstimate(month))
    routes.vacation = () => jsonOk(VACATION)
    routes.trekktabell = () => jsonOk(TREKKTABELL)
    routes.assignments = () => jsonOk({ assignments: [] })
    installFetch()
  })
  afterEach(() => { vi.unstubAllGlobals(); vi.clearAllMocks() })

  it('keeps the shell and the previous month on screen while the next month loads', async () => {
    await renderLoaded()

    const pending = deferred<FakeResponse>()
    routes.estimate = () => pending.promise
    fireEvent.click(screen.getByRole('button', { name: 'Next month' }))

    // Still the old month's card, no full-page loader.
    expectShellVisible()
    expect(screen.getByText(currentMonthLabel)).toBeInTheDocument()
    expect(screen.queryByText(nextMonthLabel)).not.toBeInTheDocument()

    await act(async () => {
      pending.resolve(await jsonOk(makeEstimate(nextMonthStr)))
    })

    await waitFor(() => expect(screen.getByText(nextMonthLabel)).toBeInTheDocument())
    expectShellVisible()
  })

  it('dims the hero card and announces the refresh while a month refetch is in flight', async () => {
    await renderLoaded()
    const heroCard = screen.getByText(currentMonthLabel).closest('div.bg-gray-800') as HTMLElement
    expect(heroCard).toHaveAttribute('aria-busy', 'false')

    const pending = deferred<FakeResponse>()
    routes.estimate = () => pending.promise
    fireEvent.click(screen.getByRole('button', { name: 'Next month' }))

    const busyCard = screen.getByText(currentMonthLabel).closest('div.bg-gray-800') as HTMLElement
    expect(busyCard).toHaveAttribute('aria-busy', 'true')
    expect(busyCard.className).toContain('opacity-60')
    expect(screen.getByText('Updating month estimate…')).toBeInTheDocument()

    await act(async () => {
      pending.resolve(await jsonOk(makeEstimate(nextMonthStr)))
    })

    await waitFor(() => expect(screen.queryByText('Updating month estimate…')).not.toBeInTheDocument())
    const settledCard = screen.getByText(nextMonthLabel).closest('div.bg-gray-800') as HTMLElement
    expect(settledCard).toHaveAttribute('aria-busy', 'false')
  })

  it('keeps the last good estimate and reports inline when a refetch fails', async () => {
    await renderLoaded()

    routes.estimate = () => httpError(500, 'boom')
    fireEvent.click(screen.getByRole('button', { name: 'Next month' }))

    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent('Failed to load salary data')
    })
    // The previous month's card and the page shell survived the failure.
    expect(screen.getByText(currentMonthLabel)).toBeInTheDocument()
    expectShellVisible()
  })

  it('shows the full-page error when the very first load fails', async () => {
    routes.estimate = () => httpError(500, 'boom')
    render(<SalaryPage />)

    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent('Failed to load salary data')
    })
    expect(screen.queryByRole('button', { name: 'This Month' })).not.toBeInTheDocument()
  })
})

describe('SalaryPage – sub-fetch errors', () => {
  beforeEach(() => {
    routes.estimate = (month: string) => jsonOk(makeEstimate(month))
    routes.vacation = () => jsonOk(VACATION)
    routes.trekktabell = () => jsonOk(TREKKTABELL)
    routes.assignments = () => jsonOk({ assignments: [] })
    installFetch()
  })
  afterEach(() => { vi.unstubAllGlobals(); vi.clearAllMocks() })

  it('offers a retry when the vacation fetch fails and renders the card on success', async () => {
    routes.vacation = () => httpError(500, 'vacation down')
    await renderLoaded()

    await waitFor(() => {
      expect(screen.getByText('Failed to load vacation data')).toBeInTheDocument()
    })
    expect(screen.queryByText('Vacation')).not.toBeInTheDocument()

    routes.vacation = () => jsonOk(VACATION)
    fireEvent.click(screen.getByRole('button', { name: /Retry/ }))

    await waitFor(() => expect(screen.getByText('Vacation')).toBeInTheDocument())
    expect(screen.queryByText('Failed to load vacation data')).not.toBeInTheDocument()
  })

  it('offers a retry when the trekktabell fetch fails instead of rendering nothing', async () => {
    routes.trekktabell = () => httpError(500, 'trekktabell down')
    await renderLoaded()

    await waitFor(() => {
      expect(screen.getByText('Failed to load trekktabell parameters')).toBeInTheDocument()
    })

    routes.trekktabell = () => jsonOk(TREKKTABELL)
    fireEvent.click(screen.getByRole('button', { name: /Retry/ }))

    await waitFor(() => {
      expect(screen.getByRole('heading', { name: /Trekktabell/ })).toBeInTheDocument()
    })
    expect(screen.queryByText('Failed to load trekktabell parameters')).not.toBeInTheDocument()
  })
})
