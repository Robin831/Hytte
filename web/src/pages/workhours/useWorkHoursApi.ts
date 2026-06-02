import { useMemo } from 'react'
import type {
  DayDetail,
  DaySummary,
  FlexState,
  LeaveBalance,
  LeaveDay,
  LeaveType,
  MonthSummaryResponse,
  PunchSession,
  WeekSummaryResponse,
  WorkDay,
  WorkDeductionPreset,
} from './types'

const JSON_HEADERS = { 'Content-Type': 'application/json' }

interface SessionInput {
  day_id: number
  start_time: string
  end_time: string
  sort_order: number
  is_internal: boolean
}

interface SessionUpdate {
  start_time: string
  end_time: string
  sort_order: number
  is_internal: boolean
}

interface DeductionInput {
  day_id: number
  name: string
  minutes: number
  preset_id?: number
}

interface PresetInput {
  name: string
  default_minutes: number
  icon: string
}

interface PresetUpdate extends PresetInput {
  active: boolean
}

interface PunchOutResult {
  day: WorkDay | null
  summary: DaySummary | null
  date: string
}

interface RedeemResult {
  ok: boolean
  error?: string
}

export interface WorkHoursApi {
  // ── Day ──
  getDay(date: string, signal?: AbortSignal): Promise<DayDetail | null>
  saveDay(body: { date: string; lunch: boolean }): Promise<DayDetail | null>
  // ── Sessions ──
  addSession(body: SessionInput): Promise<boolean>
  updateSession(id: number, body: SessionUpdate): Promise<boolean>
  deleteSession(id: number): Promise<boolean>
  // ── Deductions ──
  addDeduction(body: DeductionInput): Promise<boolean>
  deleteDeduction(id: number): Promise<boolean>
  // ── Punch clock ──
  getPunchSession(signal?: AbortSignal): Promise<PunchSession | null>
  punchIn(body: { date: string; start_time: string }): Promise<boolean>
  punchOut(body: { end_time: string }): Promise<PunchOutResult | null>
  cancelPunch(): Promise<boolean>
  editPunchStart(start_time: string, signal?: AbortSignal): Promise<boolean>
  // ── Flex ──
  getFlex(signal?: AbortSignal): Promise<FlexState | null>
  redeemFlex(date: string): Promise<RedeemResult>
  resetFlex(): Promise<boolean>
  // ── Leave ──
  getLeaveForYear(year: string, signal?: AbortSignal): Promise<{ leave_days: LeaveDay[]; balance: LeaveBalance } | null>
  getLeaveBalance(year: string, signal?: AbortSignal): Promise<LeaveBalance | null>
  setLeave(date: string, leaveType: LeaveType, note: string): Promise<LeaveDay | null>
  removeLeave(date: string): Promise<boolean>
  // ── Summaries ──
  getWeek(date: string, signal?: AbortSignal): Promise<WeekSummaryResponse | null>
  getMonth(month: string, signal?: AbortSignal): Promise<MonthSummaryResponse | null>
  // ── Presets ──
  getPresets(): Promise<WorkDeductionPreset[]>
  addPreset(body: PresetInput): Promise<boolean>
  savePreset(id: number, body: PresetUpdate): Promise<WorkDeductionPreset | null>
  deletePreset(id: number): Promise<boolean>
  // ── Preferences ──
  getPreferences(signal?: AbortSignal): Promise<Record<string, string> | null>
  setPreferences(preferences: Record<string, string>): Promise<boolean>
}

// Centralized, typed wrappers around every /api/workhours/* call (and the
// workhours-related /api/settings/preferences calls). The returned object is
// stable across renders so it can safely be listed in effect/callback deps.
export function useWorkHoursApi(): WorkHoursApi {
  return useMemo<WorkHoursApi>(() => ({
    async getDay(date, signal) {
      const r = await fetch(`/api/workhours/day?date=${encodeURIComponent(date)}`, { credentials: 'include', signal })
      if (!r.ok) return null
      return r.json() as Promise<DayDetail>
    },
    async saveDay(body) {
      const r = await fetch('/api/workhours/day', {
        method: 'PUT',
        credentials: 'include',
        headers: JSON_HEADERS,
        body: JSON.stringify(body),
      })
      if (!r.ok) return null
      return r.json() as Promise<DayDetail>
    },

    async addSession(body) {
      const r = await fetch('/api/workhours/day/session', {
        method: 'POST',
        credentials: 'include',
        headers: JSON_HEADERS,
        body: JSON.stringify(body),
      })
      return r.ok
    },
    async updateSession(id, body) {
      const r = await fetch(`/api/workhours/day/session/${id}`, {
        method: 'PUT',
        credentials: 'include',
        headers: JSON_HEADERS,
        body: JSON.stringify(body),
      })
      return r.ok
    },
    async deleteSession(id) {
      const r = await fetch(`/api/workhours/day/session/${id}`, {
        method: 'DELETE',
        credentials: 'include',
      })
      return r.ok
    },

    async addDeduction(body) {
      const r = await fetch('/api/workhours/day/deduction', {
        method: 'POST',
        credentials: 'include',
        headers: JSON_HEADERS,
        body: JSON.stringify(body),
      })
      return r.ok
    },
    async deleteDeduction(id) {
      const r = await fetch(`/api/workhours/day/deduction/${id}`, {
        method: 'DELETE',
        credentials: 'include',
      })
      return r.ok
    },

    async getPunchSession(signal) {
      const r = await fetch('/api/workhours/punch-session', { credentials: 'include', signal })
      if (!r.ok) return null
      const data = (await r.json()) as { session: PunchSession | null } | null
      return data?.session ?? null
    },
    async punchIn(body) {
      const r = await fetch('/api/workhours/punch-in', {
        method: 'POST',
        credentials: 'include',
        headers: JSON_HEADERS,
        body: JSON.stringify(body),
      })
      return r.ok
    },
    async punchOut(body) {
      const r = await fetch('/api/workhours/punch-out', {
        method: 'POST',
        credentials: 'include',
        headers: JSON_HEADERS,
        body: JSON.stringify(body),
      })
      if (!r.ok) return null
      return r.json() as Promise<PunchOutResult>
    },
    async cancelPunch() {
      const r = await fetch('/api/workhours/punch-session', {
        method: 'DELETE',
        credentials: 'include',
      })
      // 204/404 are both treated as success (already gone or just removed).
      return r.status === 204 || r.status === 404 || r.ok
    },
    async editPunchStart(start_time, signal) {
      const r = await fetch('/api/workhours/punch/edit', {
        method: 'PUT',
        credentials: 'include',
        headers: JSON_HEADERS,
        body: JSON.stringify({ start_time }),
        signal,
      })
      return r.ok
    },

    async getFlex(signal) {
      const r = await fetch('/api/workhours/flex', { credentials: 'include', signal })
      if (!r.ok) return null
      return r.json() as Promise<FlexState>
    },
    async redeemFlex(date) {
      const r = await fetch('/api/workhours/flex/redeem', {
        method: 'POST',
        credentials: 'include',
        headers: JSON_HEADERS,
        body: JSON.stringify({ date }),
      })
      if (r.ok) return { ok: true }
      const data = (await r.json().catch(() => null)) as { error?: string } | null
      return { ok: false, error: data?.error }
    },
    async resetFlex() {
      const r = await fetch('/api/workhours/flex/reset', { method: 'POST', credentials: 'include' })
      return r.ok
    },

    async getLeaveForYear(year, signal) {
      const r = await fetch(`/api/workhours/leave?year=${encodeURIComponent(year)}`, { credentials: 'include', signal })
      if (!r.ok) return null
      return r.json() as Promise<{ leave_days: LeaveDay[]; balance: LeaveBalance }>
    },
    async getLeaveBalance(year, signal) {
      const r = await fetch(`/api/workhours/leave/balance?year=${encodeURIComponent(year)}`, { credentials: 'include', signal })
      if (!r.ok) return null
      return r.json() as Promise<LeaveBalance>
    },
    async setLeave(date, leaveType, note) {
      const r = await fetch('/api/workhours/leave', {
        method: 'PUT',
        credentials: 'include',
        headers: JSON_HEADERS,
        body: JSON.stringify({ date, leave_type: leaveType, note }),
      })
      if (!r.ok) return null
      return r.json() as Promise<LeaveDay>
    },
    async removeLeave(date) {
      const r = await fetch(`/api/workhours/leave?date=${encodeURIComponent(date)}`, {
        method: 'DELETE',
        credentials: 'include',
      })
      return r.ok || r.status === 404
    },

    async getWeek(date, signal) {
      const r = await fetch(`/api/workhours/summary/week?date=${encodeURIComponent(date)}`, { credentials: 'include', signal })
      if (!r.ok) return null
      return r.json() as Promise<WeekSummaryResponse>
    },
    async getMonth(month, signal) {
      const r = await fetch(`/api/workhours/summary/month?month=${encodeURIComponent(month)}`, { credentials: 'include', signal })
      if (!r.ok) return null
      return r.json() as Promise<MonthSummaryResponse>
    },

    async getPresets() {
      const r = await fetch('/api/workhours/presets', { credentials: 'include' })
      if (!r.ok) return []
      const data = (await r.json()) as { presets?: WorkDeductionPreset[] }
      return data.presets ?? []
    },
    async addPreset(body) {
      const r = await fetch('/api/workhours/presets', {
        method: 'POST',
        credentials: 'include',
        headers: JSON_HEADERS,
        body: JSON.stringify(body),
      })
      return r.ok
    },
    async savePreset(id, body) {
      const r = await fetch(`/api/workhours/presets/${id}`, {
        method: 'PUT',
        credentials: 'include',
        headers: JSON_HEADERS,
        body: JSON.stringify(body),
      })
      if (!r.ok) return null
      return r.json() as Promise<WorkDeductionPreset>
    },
    async deletePreset(id) {
      const r = await fetch(`/api/workhours/presets/${id}`, { method: 'DELETE', credentials: 'include' })
      return r.ok
    },

    async getPreferences(signal) {
      const r = await fetch('/api/settings/preferences', { credentials: 'include', signal })
      if (!r.ok) return null
      const data = (await r.json()) as { preferences?: Record<string, string> } | null
      return data?.preferences ?? null
    },
    async setPreferences(preferences) {
      const r = await fetch('/api/settings/preferences', {
        method: 'PUT',
        credentials: 'include',
        headers: JSON_HEADERS,
        body: JSON.stringify({ preferences }),
      })
      return r.ok
    },
  }), [])
}
