/** Default regulatory LTV ceiling – mirrors backend DefaultLTVMax in internal/budget/loans.go. */
export const DEFAULT_LTV_MAX = 0.85

export interface Loan {
  id: number
  name: string
  principal: number
  current_balance: number
  annual_rate: number
  monthly_payment: number
  start_date: string
  first_payment_date: string
  term_months: number
  payment_day: number
  property_value: number
  property_name: string
  notes: string
  ltv_ratio?: number
  ltv_max?: number
}

export interface AmortizationRow {
  payment_num: number
  date: string
  payment: number
  principal: number
  interest: number
  remaining_balance: number
  rate: number
}

export interface LoanRateChange {
  id: number
  loan_id: number
  effective_date: string
  annual_rate: number
}

/** Raw, uncommitted what-if input values (kept as strings for controlled inputs). */
export interface WhatIfParams {
  extraMonthly: string
  lumpSum: string
  lumpSumDate: string
}

export const EMPTY_WHAT_IF: WhatIfParams = { extraMonthly: '', lumpSum: '', lumpSumDate: '' }

/** Mirrors budget.PayoffSummary in internal/budget/model.go. */
export interface PayoffSummary {
  original_payoff_date: string
  new_payoff_date: string
  original_payments: number
  new_payments: number
  months_saved: number
  original_total_interest: number
  new_total_interest: number
  interest_saved: number
}

export interface AmortizationResponse {
  loan: Loan
  amortization: AmortizationRow[]
  rate_changes: LoanRateChange[]
  ltv_ratio: number
  ltv_max: number
  payoff_summary?: PayoffSummary | null
}

/**
 * Builds the amortization query params for a what-if scenario. Returns an empty
 * string when nothing is set, so the baseline request URL is unchanged.
 * A lump sum is only sent when both the amount and the date are filled in —
 * the backend rejects an amount without a date.
 */
export function whatIfQuery(p: WhatIfParams): string {
  const parts: string[] = []
  const extra = Number(p.extraMonthly)
  if (p.extraMonthly.trim() !== '' && !isNaN(extra) && extra !== 0) {
    parts.push(`extra_monthly=${encodeURIComponent(p.extraMonthly.trim())}`)
  }
  const lump = Number(p.lumpSum)
  if (p.lumpSum.trim() !== '' && !isNaN(lump) && lump !== 0 && p.lumpSumDate !== '') {
    parts.push(`lump_sum=${encodeURIComponent(p.lumpSum.trim())}`)
    parts.push(`lump_sum_date=${encodeURIComponent(p.lumpSumDate)}`)
  }
  return parts.join('&')
}
