import type { WorkSession, WorkDeduction } from '../workHoursUtils'

// Shared interfaces for the Work Hours feature.
// Re-export the session/deduction/settings types so callers can import everything
// work-hours related from a single module.
export type { WorkSession, WorkDeduction, WorkSettings, LiveEstimate } from '../workHoursUtils'

export interface WorkDay {
  id: number
  user_id: number
  date: string
  lunch: boolean
  notes: string
  created_at: string
  sessions: WorkSession[]
  deductions: WorkDeduction[]
}

export interface DaySummary {
  date: string
  gross_minutes: number
  lunch_minutes: number
  deduction_minutes: number
  net_minutes: number
  reported_minutes: number
  reported_hours: number
  remainder_minutes: number
  standard_minutes: number
  balance_minutes: number
}

export interface WorkDeductionPreset {
  id: number
  user_id: number
  name: string
  default_minutes: number
  icon: string
  sort_order: number
  active: boolean
}

export interface FlexPoolResult {
  total_minutes: number
  to_next_interval: number
}

export type LeaveType = 'vacation' | 'sick' | 'personal' | 'public_holiday'

export interface LeaveDay {
  id: number
  user_id: number
  date: string
  leave_type: LeaveType
  note: string
  created_at: string
}

export interface LeaveBalance {
  year: number
  vacation_allowance: number
  vacation_used: number
  sick_used: number
  personal_used: number
  public_holiday_used: number
}

export interface WeekSummaryResponse {
  week_start: string
  week_end: string
  days: WorkDay[]
  summaries: DaySummary[]
  flex: FlexPoolResult
  leave_days: LeaveDay[]
}

export interface MonthSummaryResponse {
  month: string
  days: WorkDay[]
  summaries: DaySummary[]
  flex: FlexPoolResult
  leave_days: LeaveDay[]
}

export type ViewMode = 'day' | 'week' | 'month' | 'settings'

// Day-detail payload returned by GET/PUT /api/workhours/day.
export interface DayDetail {
  day: WorkDay | null
  summary: DaySummary | null
}

// Flex pool payload returned by GET /api/workhours/flex.
export interface FlexState {
  flex: FlexPoolResult
  reset_date: string
  days_in_pool: number
  rounding_minutes?: number
}

// In-progress punch session returned by GET /api/workhours/punch-session.
export interface PunchSession {
  start_time: string
  date?: string
}
