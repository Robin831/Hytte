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

export interface AmortizationResponse {
  loan: Loan
  amortization: AmortizationRow[]
  rate_changes: LoanRateChange[]
  ltv_ratio: number
  ltv_max: number
}
