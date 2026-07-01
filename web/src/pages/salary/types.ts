// Shared types and pure formatting helpers for the Salary page and its subcomponents.

export interface SalaryConfig {
  id: number
  user_id: number
  base_salary: number
  hourly_rate: number
  internal_hourly_rate: number
  standard_hours: number
  currency: string
  taxable_benefits: number
  effective_from: string
}

export interface CommissionTier {
  id: number
  config_id: number
  floor: number
  ceiling: number // 0 = unbounded
  rate: number
}

export interface SalaryRecord {
  id: number
  user_id: number
  month: string
  working_days: number
  hours_worked: number
  billable_hours: number
  internal_hours: number
  base_amount: number
  commission: number
  gross: number
  tax: number
  net: number
  vacation_days: number
  sick_days: number
  is_estimate: boolean
}

export interface EstimateResponse {
  month: string
  config: SalaryConfig
  commission_tiers: CommissionTier[]
  adjusted_commission_tiers: CommissionTier[]
  estimate: SalaryRecord
  working_days: number
  working_days_done: number
  working_days_remaining: number
  hours_worked: number
  internal_hours_worked: number
  standard_hours_total: number
  billable_revenue: number
  internal_revenue: number
  absence_cost_per_day: number
  sick_day_cost: number
  vacation_day_cost: number
  extra_hour_net: number
}

export interface MonthProjection {
  month: string
  working_days: number
  hours_worked: number
  standard_hours_total: number
  billable_revenue: number
  utilization_pct: number
  base_amount: number
  commission: number
  gross: number
  tax: number
  net: number
  is_estimate: boolean
  is_current: boolean
  is_future: boolean
  record_id?: number
}

export interface YearTotals {
  hours_worked: number
  billable_revenue: number
  base_amount: number
  commission: number
  gross: number
  tax: number
  net: number
}

export interface YearEstimateResponse {
  year: number
  months: MonthProjection[]
  totals: YearTotals
}

export interface TrinnskattTier {
  income_from: number
  rate: number
}

export interface TrekktabellAssignment {
  user_id: number
  effective_from: string // YYYY-MM
  table_number: string
}

export interface TrekktabellParams {
  id: number
  user_id: number
  year: number
  minstefradrag_rate: number
  minstefradrag_min: number
  minstefradrag_max: number
  personfradrag: number
  alminnelig_skatt_rate: number
  trygdeavgift: number
  trinnskatt_tiers: TrinnskattTier[]
}

export interface VacationResponse {
  year: number
  days_allowance: number
  days_used: number
  days_remaining: number
  gross_ytd: number
  feriepenger_pct: number
  feriepenger_accrued: number
}

export type Tab = 'month' | 'year'

export function formatMonthLabel(month: string, locale: string): string {
  const [year, mon] = month.split('-').map(Number)
  const date = new Date(year, mon - 1, 1)
  return date.toLocaleDateString(locale, { month: 'long', year: 'numeric' })
}

export function shortMonthLabel(month: string, locale: string): string {
  const [year, mon] = month.split('-').map(Number)
  return new Date(year, mon - 1, 1).toLocaleDateString(locale, { month: 'short' })
}

export function addMonth(month: string, delta: number): string {
  const [y, m] = month.split('-').map(Number)
  const d = new Date(y, m - 1 + delta, 1)
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}`
}

export const formatHours = (h: number) =>
  new Intl.NumberFormat(undefined, { maximumFractionDigits: 1 }).format(h)

export const formatCompact = (n: number) =>
  new Intl.NumberFormat(undefined, { maximumFractionDigits: 0 }).format(n / 1000) + 'k'
