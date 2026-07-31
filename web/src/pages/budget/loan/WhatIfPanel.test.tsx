// @vitest-environment happy-dom
import { describe, it, expect, vi } from 'vitest'
import type { TFunction } from 'i18next'
import { render, screen, fireEvent } from '@testing-library/react'
import { WhatIfPanel } from './WhatIfPanel'
import type { PayoffSummary, WhatIfParams } from './types'
import { EMPTY_WHAT_IF, lumpSumNeedsDate, whatIfQuery } from './types'
import enBudget from '../../../../public/locales/en/budget.json'

// Mock lucide-react to avoid loading the full icon library (~30 MB) in tests.
vi.mock('lucide-react', () => ({
  AlertTriangle: () => null,
  ChevronDown: () => null,
  ChevronUp: () => null,
  RotateCcw: () => null,
}))

type JsonValue = string | number | boolean | null | JsonObject | JsonValue[]
interface JsonObject { [key: string]: JsonValue }

/** Resolves keys against the real en/budget.json so missing keys fail the test. */
function t(key: string, opts?: Record<string, unknown>): string {
  let val: JsonValue | undefined = enBudget as unknown as JsonObject
  for (const part of key.split('.')) {
    if (!val || typeof val !== 'object' || Array.isArray(val)) return key
    val = (val as JsonObject)[part]
  }
  if (typeof val !== 'string') return key
  if (!opts) return val
  return val.replace(/\{\{(\w+)\}\}/g, (_, k) => String(opts[k] ?? `{{${k}}}`))
}
const tFn = t as unknown as TFunction<'budget'>

const SUMMARY: PayoffSummary = {
  original_payoff_date: '2045-06-01',
  new_payoff_date: '2041-02-01',
  original_payments: 300,
  new_payments: 248,
  months_saved: 52,
  original_total_interest: 1500000,
  new_total_interest: 1100000,
  interest_saved: 400000,
}

function renderPanel(overrides: Partial<Parameters<typeof WhatIfPanel>[0]> = {}) {
  const onChange = vi.fn()
  const props = {
    loanId: 7,
    params: EMPTY_WHAT_IF,
    onChange,
    summary: null,
    error: null,
    t: tFn,
    ...overrides,
  }
  const view = render(<WhatIfPanel {...props} />)
  return { onChange, view }
}

/** Opens the collapsed panel by clicking its header. */
function open() {
  fireEvent.click(screen.getByText(t('loan.whatIf.title')))
}

describe('WhatIfPanel', () => {
  it('starts collapsed and toggles open and closed', () => {
    renderPanel()
    expect(screen.queryByLabelText(t('loan.whatIf.extraMonthly'))).toBeNull()

    open()
    expect(screen.getByLabelText(t('loan.whatIf.extraMonthly'))).toBeTruthy()
    expect(screen.getByLabelText(t('loan.whatIf.lumpSum'))).toBeTruthy()
    expect(screen.getByLabelText(t('loan.whatIf.lumpSumDate'))).toBeTruthy()

    open()
    expect(screen.queryByLabelText(t('loan.whatIf.extraMonthly'))).toBeNull()
  })

  it('reports typed values to the parent', () => {
    const { onChange } = renderPanel()
    open()

    fireEvent.change(screen.getByLabelText(t('loan.whatIf.extraMonthly')), {
      target: { value: '2500' },
    })
    expect(onChange).toHaveBeenCalledWith({ ...EMPTY_WHAT_IF, extraMonthly: '2500' })

    fireEvent.change(screen.getByLabelText(t('loan.whatIf.lumpSumDate')), {
      target: { value: '2026-09-01' },
    })
    expect(onChange).toHaveBeenCalledWith({ ...EMPTY_WHAT_IF, lumpSumDate: '2026-09-01' })
  })

  it('renders the summary card with formatted values', () => {
    renderPanel({ summary: SUMMARY })
    open()

    expect(screen.getByText('2045-06-01')).toBeTruthy()
    expect(screen.getByText('2041-02-01')).toBeTruthy()
    expect(screen.getByText('52')).toBeTruthy()
    // Interest saved is rendered as NOK currency, so match on the digits only
    // (Intl inserts non-breaking spaces as group separators).
    expect(screen.getByText(/400.*000/)).toBeTruthy()
    expect(screen.getByText(t('loan.whatIf.interestSaved'))).toBeTruthy()
  })

  it('renders nothing extra when there is no summary', () => {
    renderPanel({ summary: null })
    open()
    expect(screen.queryByText(t('loan.whatIf.interestSaved'))).toBeNull()
    // The inputs are still usable.
    expect(screen.getByLabelText(t('loan.whatIf.extraMonthly'))).toBeTruthy()
  })

  it('shows an inline error and hides the stale summary', () => {
    renderPanel({ summary: SUMMARY, error: 'Amounts cannot be negative.' })
    open()
    expect(screen.getByText('Amounts cannot be negative.')).toBeTruthy()
    expect(screen.queryByText(t('loan.whatIf.interestSaved'))).toBeNull()
  })

  it('resets all params via the reset button', () => {
    const params: WhatIfParams = { extraMonthly: '2000', lumpSum: '50000', lumpSumDate: '2026-01-01' }
    const { onChange } = renderPanel({ params })
    open()

    fireEvent.click(screen.getByText(t('loan.whatIf.reset')))
    expect(onChange).toHaveBeenCalledWith(EMPTY_WHAT_IF)
  })

  it('hides the reset button when nothing is entered', () => {
    renderPanel()
    open()
    expect(screen.queryByText(t('loan.whatIf.reset'))).toBeNull()
  })

  it('warns when a lump sum has no date instead of silently dropping it', () => {
    renderPanel({ params: { ...EMPTY_WHAT_IF, lumpSum: '50000' } })
    open()
    expect(screen.getByText(t('loan.whatIf.errors.lumpSumDateRequired'))).toBeTruthy()
  })

  it('drops the warning once the date is filled in', () => {
    renderPanel({ params: { extraMonthly: '', lumpSum: '50000', lumpSumDate: '2026-01-01' } })
    open()
    expect(screen.queryByText(t('loan.whatIf.errors.lumpSumDateRequired'))).toBeNull()
  })

  it('offers a retry for a failed request', () => {
    const onRetry = vi.fn()
    renderPanel({ error: 'Request failed.', onRetry })
    open()

    fireEvent.click(screen.getByText(t('loan.whatIf.retry')))
    expect(onRetry).toHaveBeenCalled()
  })

  it('shows the recalculating hint only while pending', () => {
    const { view } = renderPanel({ pending: true })
    open()
    expect(screen.getByText(t('loan.whatIf.calculating'))).toBeTruthy()

    view.rerender(
      <WhatIfPanel loanId={7} params={EMPTY_WHAT_IF} onChange={vi.fn()} pending={false} t={tFn} />,
    )
    expect(screen.queryByText(t('loan.whatIf.calculating'))).toBeNull()
  })
})

describe('lumpSumNeedsDate', () => {
  it('is true only for a non-zero lump sum without a date', () => {
    expect(lumpSumNeedsDate(EMPTY_WHAT_IF)).toBe(false)
    expect(lumpSumNeedsDate({ ...EMPTY_WHAT_IF, lumpSum: '0' })).toBe(false)
    expect(lumpSumNeedsDate({ ...EMPTY_WHAT_IF, lumpSum: '50000' })).toBe(true)
    expect(lumpSumNeedsDate({ extraMonthly: '', lumpSum: '50000', lumpSumDate: '2026-01-01' })).toBe(false)
  })
})

describe('whatIfQuery', () => {
  it('is empty for empty or zero input, so the baseline URL is unchanged', () => {
    expect(whatIfQuery(EMPTY_WHAT_IF)).toBe('')
    expect(whatIfQuery({ extraMonthly: '0', lumpSum: '0', lumpSumDate: '2026-01-01' })).toBe('')
  })

  it('sends the extra monthly amount', () => {
    expect(whatIfQuery({ ...EMPTY_WHAT_IF, extraMonthly: '2500' })).toBe('extra_monthly=2500')
  })

  it('only sends a lump sum together with its date', () => {
    expect(whatIfQuery({ ...EMPTY_WHAT_IF, lumpSum: '50000' })).toBe('')
    expect(whatIfQuery({ extraMonthly: '', lumpSum: '50000', lumpSumDate: '2026-01-01' }))
      .toBe('lump_sum=50000&lump_sum_date=2026-01-01')
  })

  it('forwards negative amounts so the backend can reject them', () => {
    expect(whatIfQuery({ ...EMPTY_WHAT_IF, extraMonthly: '-100' })).toBe('extra_monthly=-100')
  })
})
