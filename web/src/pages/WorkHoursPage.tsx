import { useState, useEffect, useCallback, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { Building2, Calendar, ChevronLeft, ChevronRight, Clock, Copy, Plus, Settings, Trash2 } from 'lucide-react'
import { formatDate } from '../utils/formatDate'
import { Skeleton } from '../components/ui/skeleton'
import { ConfirmDialog } from '../components/ui/dialog'
import { Select, type SelectOption } from '../components/ui/select'
import { TimePicker } from '../components/ui/time-picker'

// ── Interfaces ──────────────────────────────────────────────────────────────

interface WorkSession {
  id: number
  day_id: number
  start_time: string
  end_time: string
  sort_order: number
  is_internal: boolean
}

interface WorkDeduction {
  id: number
  day_id: number
  name: string
  minutes: number
  preset_id?: number | null
}

interface WorkDay {
  id: number
  user_id: number
  date: string
  lunch: boolean
  notes: string
  created_at: string
  sessions: WorkSession[]
  deductions: WorkDeduction[]
}

interface DaySummary {
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

interface WorkDeductionPreset {
  id: number
  user_id: number
  name: string
  default_minutes: number
  icon: string
  sort_order: number
  active: boolean
}

interface FlexPoolResult {
  total_minutes: number
  to_next_interval: number
}

type LeaveType = 'vacation' | 'sick' | 'personal' | 'public_holiday'

interface LeaveDay {
  id: number
  user_id: number
  date: string
  leave_type: LeaveType
  note: string
  created_at: string
}

interface LeaveBalance {
  year: number
  vacation_allowance: number
  vacation_used: number
  sick_used: number
  personal_used: number
  public_holiday_used: number
}

interface WeekSummaryResponse {
  week_start: string
  week_end: string
  days: WorkDay[]
  summaries: DaySummary[]
  flex: FlexPoolResult
  leave_days: LeaveDay[]
}

interface MonthSummaryResponse {
  month: string
  days: WorkDay[]
  summaries: DaySummary[]
  flex: FlexPoolResult
  leave_days: LeaveDay[]
}

type ViewMode = 'day' | 'week' | 'month' | 'settings'

// ── Utility functions ──────────────────────────────────────────────────────

function formatMins(mins: number): string {
  const abs = Math.abs(mins)
  const h = Math.floor(abs / 60)
  const m = abs % 60
  const prefix = mins < 0 ? '-' : ''
  return `${prefix}${h}:${m.toString().padStart(2, '0')}`
}

function formatHours(minutes: number): string {
  return (minutes / 60).toFixed(1) + 'h'
}

function localDateStr(d: Date): string {
  const y = d.getFullYear()
  const m = String(d.getMonth() + 1).padStart(2, '0')
  const day = String(d.getDate()).padStart(2, '0')
  return `${y}-${m}-${day}`
}

function getInitialDate(): string {
  const d = new Date()
  while (d.getDay() === 0 || d.getDay() === 6) {
    d.setDate(d.getDate() - 1)
  }
  return localDateStr(d)
}

function prevWeekday(date: string): string {
  const d = new Date(date + 'T12:00:00')
  d.setDate(d.getDate() - 1)
  while (d.getDay() === 0 || d.getDay() === 6) {
    d.setDate(d.getDate() - 1)
  }
  return localDateStr(d)
}

function nextWeekday(date: string): string {
  const d = new Date(date + 'T12:00:00')
  d.setDate(d.getDate() + 1)
  while (d.getDay() === 0 || d.getDay() === 6) {
    d.setDate(d.getDate() + 1)
  }
  return localDateStr(d)
}

function addWeeks(date: string, weeks: number): string {
  const d = new Date(date + 'T12:00:00')
  d.setDate(d.getDate() + weeks * 7)
  return localDateStr(d)
}

function addMonths(monthStr: string, n: number): string {
  const [y, m] = monthStr.split('-').map(Number)
  const d = new Date(y, m - 1 + n, 1)
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}`
}

function dateToMonthStr(date: string): string {
  return date.substring(0, 7)
}

// Returns session range (earliest start, latest end) for a day
function sessionRange(day: WorkDay | undefined): { start: string; end: string } | null {
  if (!day || !day.sessions || day.sessions.length === 0) return null
  const sorted = [...day.sessions].sort((a, b) => a.start_time.localeCompare(b.start_time))
  const start = sorted[0].start_time
  const end = sorted.reduce((mx, s) => (s.end_time > mx ? s.end_time : mx), sorted[0].end_time)
  return { start, end }
}

// Returns the 5 Mon-Fri dates for the week starting at weekStart
function weekDays(weekStart: string): string[] {
  const result: string[] = []
  const d = new Date(weekStart + 'T12:00:00')
  for (let i = 0; i < 5; i++) {
    result.push(localDateStr(d))
    d.setDate(d.getDate() + 1)
  }
  return result
}

// Builds a month grid of weeks (Mon–Sun), with null for out-of-month cells
function buildMonthGrid(monthStr: string): (string | null)[][] {
  const year = parseInt(monthStr.split('-')[0])
  const month = parseInt(monthStr.split('-')[1]) - 1
  const numDays = new Date(year, month + 1, 0).getDate()

  // Offset for Monday-based week (Mon=0 ... Sun=6)
  let startDow = new Date(year, month, 1).getDay() - 1
  if (startDow < 0) startDow = 6

  const cells: (string | null)[] = Array(startDow).fill(null)
  for (let d = 1; d <= numDays; d++) {
    cells.push(`${monthStr}-${String(d).padStart(2, '0')}`)
  }
  while (cells.length % 7 !== 0) cells.push(null)

  const weeks: (string | null)[][] = []
  for (let i = 0; i < cells.length; i += 7) {
    weeks.push(cells.slice(i, i + 7))
  }
  return weeks
}

// Count Mon-Fri days in month up to today (or end of month if past), excluding holidays
function countWorkdaysUpToNow(monthStr: string, holidays?: Set<string>): number {
  const year = parseInt(monthStr.split('-')[0])
  const month = parseInt(monthStr.split('-')[1]) - 1
  const numDays = new Date(year, month + 1, 0).getDate()
  const today = new Date()
  let count = 0
  for (let d = 1; d <= numDays; d++) {
    const date = new Date(year, month, d)
    if (date > today) break
    const dow = date.getDay()
    if (dow === 0 || dow === 6) continue
    if (holidays?.has(localDateStr(date))) continue
    count++
  }
  return count
}

// Count all Mon-Fri days in a full month (used for past/future months), excluding holidays
function countWorkdaysInMonth(mStr: string, holidays?: Set<string>): number {
  if (!mStr || mStr.length < 7) return 0
  const parts = mStr.split('-')
  if (parts.length < 2) return 0
  const year = Number(parts[0])
  const month = Number(parts[1])
  if (!Number.isFinite(year) || !Number.isFinite(month) || month < 1 || month > 12) return 0

  const start = new Date(year, month - 1, 1)
  const end = new Date(year, month, 1)
  let workdays = 0

  for (let d = new Date(start); d < end; d.setDate(d.getDate() + 1)) {
    const dow = d.getDay()
    if (dow === 0 || dow === 6) continue
    if (holidays?.has(localDateStr(d))) continue
    workdays++
  }

  return workdays
}

// Returns Tailwind classes for a calendar cell based on reported hours
function dayCellClass(summary: DaySummary | undefined, isWeekend: boolean, isHoliday?: boolean, leaveType?: LeaveType): string {
  if (isWeekend || isHoliday) return 'bg-gray-900/30 text-gray-600'
  if (leaveType === 'vacation') return 'bg-purple-900/60 text-purple-200'
  if (leaveType === 'sick') return 'bg-orange-900/60 text-orange-200'
  if (leaveType === 'personal') return 'bg-teal-900/60 text-teal-200'
  if (leaveType === 'public_holiday') return 'bg-gray-900/30 text-gray-600'
  if (!summary || summary.reported_minutes === 0) return 'bg-gray-800/40 text-gray-400'
  if (summary.reported_minutes > summary.standard_minutes) return 'bg-blue-900/60 text-blue-200'
  if (summary.reported_minutes === summary.standard_minutes) return 'bg-green-900/60 text-green-200'
  return 'bg-amber-900/60 text-amber-200'
}

// ── Norwegian public holidays ──────────────────────────────────────────────

function getEasterSunday(year: number): Date {
  const a = year % 19
  const b = Math.floor(year / 100)
  const c = year % 100
  const d = Math.floor(b / 4)
  const e = b % 4
  const f = Math.floor((b + 8) / 25)
  const g = Math.floor((b - f + 1) / 3)
  const h = (19 * a + b - d - g + 15) % 30
  const i = Math.floor(c / 4)
  const k = c % 4
  const l = (32 + 2 * e + 2 * i - h - k) % 7
  const m = Math.floor((a + 11 * h + 22 * l) / 451)
  const month = Math.floor((h + l - 7 * m + 114) / 31)
  const day = ((h + l - 7 * m + 114) % 31) + 1
  return new Date(year, month - 1, day)
}

function addDays(date: Date, n: number): Date {
  const r = new Date(date)
  r.setDate(r.getDate() + n)
  return r
}

// Returns a Map of YYYY-MM-DD → Norwegian holiday name for the given year
function getNorwegianHolidays(year: number): Map<string, string> {
  const m = new Map<string, string>()
  const fmt = (d: Date) => localDateStr(d)
  m.set(`${year}-01-01`, 'Nyttårsdag')
  m.set(`${year}-05-01`, 'Arbeidernes dag')
  m.set(`${year}-05-17`, 'Grunnlovsdagen')
  m.set(`${year}-12-25`, '1. juledag')
  m.set(`${year}-12-26`, '2. juledag')
  const easter = getEasterSunday(year)
  m.set(fmt(addDays(easter, -3)), 'Skjærtorsdag')
  m.set(fmt(addDays(easter, -2)), 'Langfredag')
  m.set(fmt(easter), '1. påskedag')
  m.set(fmt(addDays(easter, 1)), '2. påskedag')
  m.set(fmt(addDays(easter, 39)), 'Kristi himmelfartsdag')
  m.set(fmt(addDays(easter, 49)), '1. pinsedag')
  m.set(fmt(addDays(easter, 50)), '2. pinsedag')
  return m
}

function currentTimeHHMM(): string {
  const now = new Date()
  return `${String(now.getHours()).padStart(2, '0')}:${String(now.getMinutes()).padStart(2, '0')}`
}

// ── Main page ──────────────────────────────────────────────────────────────

export default function WorkHoursPage() {
  const { t } = useTranslation(['workhours', 'common'])
  const [activeTab, setActiveTab] = useState<ViewMode>('day')
  const [currentDate, setCurrentDate] = useState(getInitialDate)

  function handleSelectDay(date: string) {
    setCurrentDate(date)
    setActiveTab('day')
  }

  return (
    <div className="max-w-3xl mx-auto p-4 space-y-4">
      <h1 className="text-xl font-semibold text-white">{t('workhours:title')}</h1>

      {/* Tab bar */}
      <div className="flex gap-1 bg-gray-800 rounded-lg p-1">
        <button
          type="button"
          onClick={() => setActiveTab('day')}
          className={`flex-1 py-1.5 text-sm font-medium rounded transition-colors cursor-pointer ${
            activeTab === 'day' ? 'bg-gray-700 text-white' : 'text-gray-400 hover:text-gray-200'
          }`}
        >
          {t('workhours:viewDay')}
        </button>
        <button
          type="button"
          onClick={() => setActiveTab('week')}
          className={`flex-1 py-1.5 text-sm font-medium rounded transition-colors cursor-pointer ${
            activeTab === 'week' ? 'bg-gray-700 text-white' : 'text-gray-400 hover:text-gray-200'
          }`}
        >
          {t('workhours:viewWeek')}
        </button>
        <button
          type="button"
          onClick={() => setActiveTab('month')}
          className={`flex-1 py-1.5 text-sm font-medium rounded transition-colors cursor-pointer ${
            activeTab === 'month' ? 'bg-gray-700 text-white' : 'text-gray-400 hover:text-gray-200'
          }`}
        >
          {t('workhours:viewMonth')}
        </button>
        <button
          type="button"
          onClick={() => setActiveTab('settings')}
          className={`py-1.5 px-2.5 text-sm font-medium rounded transition-colors cursor-pointer ${
            activeTab === 'settings' ? 'bg-gray-700 text-white' : 'text-gray-400 hover:text-gray-200'
          }`}
          aria-label={t('workhours:settings')}
        >
          <Settings size={16} />
        </button>
      </div>

      {activeTab === 'day' && (
        <DayView currentDate={currentDate} setCurrentDate={setCurrentDate} onNavigateToSettings={() => setActiveTab('settings')} />
      )}
      {activeTab === 'week' && (
        <WeekView currentDate={currentDate} setCurrentDate={setCurrentDate} onSelectDay={handleSelectDay} />
      )}
      {activeTab === 'month' && (
        <MonthView currentDate={currentDate} setCurrentDate={setCurrentDate} onSelectDay={handleSelectDay} />
      )}
      {activeTab === 'settings' && (
        <SettingsTab />
      )}
    </div>
  )
}

// ── Day view ───────────────────────────────────────────────────────────────

function DayView({
  currentDate,
  setCurrentDate,
  onNavigateToSettings,
}: {
  currentDate: string
  setCurrentDate: (d: string | ((prev: string) => string)) => void
  onNavigateToSettings: () => void
}) {
  const { t } = useTranslation(['workhours', 'common'])

  const currentDateRef = useRef(currentDate)
  const leaveDaysCacheRef = useRef<Map<string, LeaveDay[]>>(new Map())
  const datePickerRef = useRef<HTMLInputElement>(null)
  const [dayData, setDayData] = useState<{ day: WorkDay | null; summary: DaySummary | null } | null>(null)
  const [presets, setPresets] = useState<WorkDeductionPreset[]>([])
  const [flex, setFlex] = useState<{ flex: FlexPoolResult; reset_date: string; days_in_pool: number } | null>(null)
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [newStart, setNewStart] = useState('')
  const [newEnd, setNewEnd] = useState('')
  const [newIsInternal, setNewIsInternal] = useState(false)
  const [newDeductionName, setNewDeductionName] = useState('')
  const [newDeductionMinutes, setNewDeductionMinutes] = useState('')
  const [punchStart, setPunchStart] = useState<string | null>(null)
  const [leaveDay, setLeaveDay] = useState<LeaveDay | null>(null)
  const [leaveSaving, setLeaveSaving] = useState(false)
  const [selectedPresetId, setSelectedPresetId] = useState<number | null>(null)
  const [recentlyUsed, setRecentlyUsed] = useState<number[]>(() => {
    try {
      const stored = localStorage.getItem('workhours_recent_presets')
      if (!stored) return []
      const parsed: unknown = JSON.parse(stored)
      if (!Array.isArray(parsed)) return []
      return parsed.filter((v): v is number => typeof v === 'number' && isFinite(v))
    } catch {
      return []
    }
  })

  useEffect(() => {
    currentDateRef.current = currentDate
  }, [currentDate])

  const loadFlex = useCallback(() => {
    fetch('/api/workhours/flex', { credentials: 'include' })
      .then(r => (r.ok ? r.json() : null))
      .then((data: { flex: FlexPoolResult; reset_date: string; days_in_pool: number } | null) => setFlex(data))
      .catch(() => {})
  }, [])

  useEffect(() => {
    fetch('/api/workhours/presets', { credentials: 'include' })
      .then(r => (r.ok ? r.json() : { presets: [] }))
      .then((data: { presets: WorkDeductionPreset[] }) => setPresets(data.presets ?? []))
      .catch(() => {})
    loadFlex()
    // Restore any in-progress punch-in from the server.
    fetch('/api/workhours/punch-session', { credentials: 'include' })
      .then(r => (r.ok ? r.json() : null))
      .then((data: { session: { start_time: string; date?: string } | null } | null) => {
        if (data?.session) {
          const sessionDate = data.session.date
          if (sessionDate && sessionDate !== currentDateRef.current) {
            setCurrentDate(sessionDate)
          }
          setPunchStart(data.session.start_time)
          setNewStart(data.session.start_time)
        }
      })
      .catch(() => {})
  }, [loadFlex])

  const loadDay = useCallback(async (date: string, signal?: AbortSignal) => {
    setLoading(true)
    try {
      const r = await fetch(`/api/workhours/day?date=${encodeURIComponent(date)}`, { credentials: 'include', signal })
      if (signal?.aborted || currentDateRef.current !== date) return
      if (r.ok) {
        const data: { day: WorkDay | null; summary: DaySummary | null } = await r.json()
        if (currentDateRef.current === date) setDayData(data)
      } else {
        if (currentDateRef.current === date) setDayData({ day: null, summary: null })
      }
    } catch (err) {
      if (err instanceof Error && err.name === 'AbortError') return
      if (currentDateRef.current === date) setDayData({ day: null, summary: null })
    } finally {
      if (!signal?.aborted && currentDateRef.current === date) {
        setLoading(false)
      }
    }
  }, [])

  const loadLeaveDay = useCallback(async (date: string, signal?: AbortSignal) => {
    try {
      const year = date.slice(0, 4)
      const cached = leaveDaysCacheRef.current.get(year)
      if (cached) {
        if (currentDateRef.current === date) {
          setLeaveDay(cached.find(d => d.date === date) ?? null)
        }
        return
      }
      const r = await fetch(`/api/workhours/leave?year=${encodeURIComponent(year)}`, { credentials: 'include', signal })
      if (signal?.aborted || currentDateRef.current !== date) return
      if (r.ok) {
        const data: { leave_days: LeaveDay[]; balance: LeaveBalance } = await r.json()
        leaveDaysCacheRef.current.set(year, data.leave_days)
        if (currentDateRef.current === date) {
          setLeaveDay(data.leave_days.find(d => d.date === date) ?? null)
        }
      } else {
        if (currentDateRef.current === date) setLeaveDay(null)
      }
    } catch (err) {
      if (err instanceof Error && err.name === 'AbortError') return
      console.error('workhours: load leave day:', err)
      if (currentDateRef.current === date) setLeaveDay(null)
    }
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    // eslint-disable-next-line react-hooks/set-state-in-effect
    loadDay(currentDate, controller.signal)
    loadLeaveDay(currentDate, controller.signal)
    return () => controller.abort()
  }, [currentDate, loadDay, loadLeaveDay])

  const ensureDay = async (lunch?: boolean): Promise<WorkDay | null> => {
    const targetDate = currentDate
    const body = {
      date: targetDate,
      lunch: lunch !== undefined ? lunch : (dayData?.day?.lunch ?? false),
    }
    try {
      const r = await fetch('/api/workhours/day', {
        method: 'PUT',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      })
      if (!r.ok) return null
      const data: { day: WorkDay; summary: DaySummary } = await r.json()
      if (currentDateRef.current !== targetDate) return null
      setDayData(data)
      loadFlex()
      return data.day
    } catch {
      return null
    }
  }

  const handleLunchToggle = async () => {
    setSaving(true)
    try {
      await ensureDay(!(dayData?.day?.lunch ?? false))
    } finally {
      setSaving(false)
    }
  }

  const handleAddSession = async () => {
    if (!newStart || !newEnd) return
    setSaving(true)
    try {
      let day = dayData?.day ?? null
      if (!day) {
        day = await ensureDay()
        if (!day) return
      }
      const sortOrder = day.sessions?.length ?? 0
      const r = await fetch('/api/workhours/day/session', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          day_id: day.id,
          start_time: newStart,
          end_time: newEnd,
          sort_order: sortOrder,
          is_internal: newIsInternal,
        }),
      })
      if (r.ok) {
        setNewStart('')
        setNewEnd('')
        setNewIsInternal(false)
        await loadDay(currentDate)
        loadFlex()
      }
    } finally {
      setSaving(false)
    }
  }

  const handleDeleteSession = async (sessionID: number) => {
    setSaving(true)
    try {
      const r = await fetch(`/api/workhours/day/session/${sessionID}`, {
        method: 'DELETE',
        credentials: 'include',
      })
      if (r.ok) {
        await loadDay(currentDate)
        loadFlex()
      }
    } finally {
      setSaving(false)
    }
  }

  const handleToggleInternal = async (session: WorkSession) => {
    setSaving(true)
    try {
      await fetch(`/api/workhours/day/session/${session.id}`, {
        method: 'PUT',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          start_time: session.start_time,
          end_time: session.end_time,
          sort_order: session.sort_order,
          is_internal: !session.is_internal,
        }),
      })
      await loadDay(currentDate)
    } finally {
      setSaving(false)
    }
  }

  const handlePresetToggle = async (preset: WorkDeductionPreset) => {
    const existing = dayData?.day?.deductions.find(d => d.preset_id === preset.id)
    setSaving(true)
    try {
      if (existing) {
        const r = await fetch(`/api/workhours/day/deduction/${existing.id}`, {
          method: 'DELETE',
          credentials: 'include',
        })
        if (r.ok) {
          await loadDay(currentDate)
          loadFlex()
        }
      } else {
        let day = dayData?.day ?? null
        if (!day) {
          day = await ensureDay()
          if (!day) return
        }
        const r = await fetch('/api/workhours/day/deduction', {
          method: 'POST',
          credentials: 'include',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            day_id: day.id,
            name: preset.name,
            minutes: preset.default_minutes,
            preset_id: preset.id,
          }),
        })
        if (r.ok) {
          await loadDay(currentDate)
          loadFlex()
        }
      }
    } finally {
      setSaving(false)
    }
  }

  const handleAddDeduction = async () => {
    const name = newDeductionName.trim()
    const minutes = parseInt(newDeductionMinutes, 10)
    if (!name || !minutes || minutes <= 0) return
    setSaving(true)
    try {
      let day = dayData?.day ?? null
      if (!day) {
        day = await ensureDay()
        if (!day) return
      }
      const r = await fetch('/api/workhours/day/deduction', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ day_id: day.id, name, minutes }),
      })
      if (r.ok) {
        if (selectedPresetId !== null) {
          const updated = [selectedPresetId, ...recentlyUsed.filter(id => id !== selectedPresetId)].slice(0, 10)
          try {
            localStorage.setItem('workhours_recent_presets', JSON.stringify(updated))
          } catch {
            // storage unavailable or quota exceeded — non-fatal
          }
          setRecentlyUsed(updated)
          setSelectedPresetId(null)
        }
        setNewDeductionName('')
        setNewDeductionMinutes('')
        await loadDay(currentDate)
        loadFlex()
      }
    } finally {
      setSaving(false)
    }
  }

  const handleDeleteDeduction = async (deductionID: number) => {
    setSaving(true)
    try {
      const r = await fetch(`/api/workhours/day/deduction/${deductionID}`, {
        method: 'DELETE',
        credentials: 'include',
      })
      if (r.ok) {
        await loadDay(currentDate)
        loadFlex()
      }
    } finally {
      setSaving(false)
    }
  }

  const handlePresetDropdownSelect = (val: string) => {
    if (!val) {
      setSelectedPresetId(null)
      return
    }
    const presetId = parseInt(val, 10)
    const preset = presets.find(p => p.id === presetId)
    if (!preset) return
    setSelectedPresetId(presetId)
    setNewDeductionName(preset.name)
    setNewDeductionMinutes(String(preset.default_minutes))
  }

  const handlePunchIn = async () => {
    const startTime = currentTimeHHMM()
    setSaving(true)
    try {
      const r = await fetch('/api/workhours/punch-in', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ date: currentDate, start_time: startTime }),
      })
      if (r.ok) {
        setPunchStart(startTime)
        setNewStart(startTime)
        setNewEnd('')
      } else {
        alert(t('workhours:punchInError'))
      }
    } finally {
      setSaving(false)
    }
  }

  const handleCancelPunch = async () => {
    setSaving(true)
    try {
      const r = await fetch('/api/workhours/punch-session', {
        method: 'DELETE',
        credentials: 'include',
      })
      if (r.status === 204 || r.status === 404 || r.ok) {
        setPunchStart(null)
      } else {
        alert(t('workhours:punchCancelError'))
      }
    } catch {
      alert(t('workhours:punchCancelError'))
    } finally {
      setSaving(false)
    }
  }

  const handlePunchOut = async () => {
    if (!punchStart) return
    const endTime = currentTimeHHMM()
    if (endTime <= punchStart) {
      alert(t('workhours:punchMidnightError'))
      return
    }
    setSaving(true)
    try {
      const r = await fetch('/api/workhours/punch-out', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ end_time: endTime }),
      })
      if (r.ok) {
        const data: { day: WorkDay | null; summary: DaySummary | null; date: string } = await r.json()
        setPunchStart(null)
        setNewStart('')
        setNewEnd('')
        if (data.date === currentDate) {
          setDayData({ day: data.day, summary: data.summary })
        } else {
          setCurrentDate(data.date)
          await loadDay(data.date)
        }
        loadFlex()
      } else {
        alert(t('workhours:punchSaveError'))
      }
    } finally {
      setSaving(false)
    }
  }

  const handleCopyYesterday = async () => {
    if (sessions.length > 0) return
    const yesterday = prevWeekday(currentDate)
    setSaving(true)
    try {
      const r = await fetch(`/api/workhours/day?date=${encodeURIComponent(yesterday)}`, {
        credentials: 'include',
      })
      if (!r.ok) return
      const data: { day: WorkDay | null; summary: DaySummary | null } = await r.json()
      if (!data.day?.sessions?.length) return

      let d = dayData?.day ?? null
      if (!d) {
        d = await ensureDay()
        if (!d) return
      }

      for (const session of data.day.sessions) {
        const sr = await fetch('/api/workhours/day/session', {
          method: 'POST',
          credentials: 'include',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            day_id: d.id,
            start_time: session.start_time,
            end_time: session.end_time,
            sort_order: (d.sessions?.length ?? 0),
            is_internal: session.is_internal,
          }),
        })
        if (!sr.ok) {
          console.error('workhours: failed to copy session', session)
          break
        }
        // Keep track so subsequent sessions don't overwrite each other
        d = { ...d, sessions: [...(d.sessions ?? []), session] }
      }
      await loadDay(currentDate)
      loadFlex()
    } finally {
      setSaving(false)
    }
  }

  const handleSetLeave = async (leaveType: LeaveType | null) => {
    setLeaveSaving(true)
    const targetDate = currentDate
    try {
      if (leaveType === null) {
        const r = await fetch(`/api/workhours/leave?date=${encodeURIComponent(targetDate)}`, {
          method: 'DELETE',
          credentials: 'include',
        })
        if ((r.ok || r.status === 404) && currentDateRef.current === targetDate) {
          leaveDaysCacheRef.current.delete(targetDate.slice(0, 4))
          setLeaveDay(null)
        }
      } else {
        const r = await fetch('/api/workhours/leave', {
          method: 'PUT',
          credentials: 'include',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ date: targetDate, leave_type: leaveType, note: '' }),
        })
        if (r.ok && currentDateRef.current === targetDate) {
          const ld: LeaveDay = await r.json()
          leaveDaysCacheRef.current.delete(targetDate.slice(0, 4))
          setLeaveDay(ld)
        }
      }
    } catch (err) {
      console.error('workhours: set leave:', err)
    } finally {
      setLeaveSaving(false)
    }
  }

  const day = dayData?.day ?? null
  const summary = dayData?.summary ?? null
  const lunchChecked = day?.lunch ?? false
  const sessions = day?.sessions ?? []
  const deductions = day?.deductions ?? []

  const sortedPresets = [...presets].sort((a, b) => {
    const aIdx = recentlyUsed.indexOf(a.id)
    const bIdx = recentlyUsed.indexOf(b.id)
    if (aIdx !== -1 && bIdx !== -1) return aIdx - bIdx
    if (aIdx !== -1) return -1
    if (bIdx !== -1) return 1
    return a.sort_order - b.sort_order
  })

  const currentYear = parseInt(currentDate.split('-')[0])
  const holidays = getNorwegianHolidays(currentYear)
  const holidayName = holidays.get(currentDate)

  const dateLabel = formatDate(currentDate + 'T12:00:00', {
    weekday: 'long',
    year: 'numeric',
    month: 'long',
    day: 'numeric',
  })

  return (
    <div className="space-y-6">
      {/* Date navigation */}
      <div className="flex items-center justify-between bg-gray-800 rounded-lg p-3">
        <button
          type="button"
          onClick={() => setCurrentDate(prev => prevWeekday(prev))}
          className="p-1 text-gray-400 hover:text-white transition-colors cursor-pointer"
          aria-label={t('workhours:prevDay')}
        >
          <ChevronLeft size={20} />
        </button>
        <div className="relative flex items-center gap-1">
          <button
            type="button"
            onClick={() => {
              const input = datePickerRef.current
              if (!input) return
              // Prefer native showPicker when available, fall back to click/focus for broader browser support
              if (typeof (input as HTMLInputElement).showPicker === 'function') {
                ;(input as HTMLInputElement).showPicker()
              } else {
                input.click()
                input.focus()
              }
            }}
            className="flex items-center gap-1 text-sm font-medium text-white capitalize hover:text-blue-300 transition-colors cursor-pointer"
            title={t('workhours:selectDate')}
          >
            <Calendar size={14} className="text-gray-400" />
            {dateLabel}
          </button>
          <input
            ref={datePickerRef}
            type="date"
            value={currentDate}
            onChange={e => {
              if (!e.target.value) return
              const d = new Date(e.target.value + 'T12:00:00')
              while (d.getDay() === 0 || d.getDay() === 6) {
                d.setDate(d.getDate() - 1)
              }
              setCurrentDate(localDateStr(d))
            }}
            className="absolute left-0 top-0 opacity-0 pointer-events-none w-0 h-0"
            aria-hidden="true"
            tabIndex={-1}
          />
        </div>
        <button
          type="button"
          onClick={() => setCurrentDate(prev => nextWeekday(prev))}
          className="p-1 text-gray-400 hover:text-white transition-colors cursor-pointer"
          aria-label={t('workhours:nextDay')}
        >
          <ChevronRight size={20} />
        </button>
      </div>

      {loading ? (
        <div role="status" aria-live="polite" className="inline-flex items-center gap-2">
          <Skeleton className="h-5 w-24" />
          <span className="sr-only">{t('common:skeleton.loading')}</span>
        </div>
      ) : (
        <>
          {/* Holiday notice */}
          {holidayName && (
            <div className="flex items-center gap-2 bg-gray-800/60 rounded-lg px-3 py-2 text-sm text-gray-300">
              <span className="text-base">🎉</span>
              <span>{t('workhours:holidayLabel', { name: holidayName })}</span>
            </div>
          )}

          {/* Leave day marker */}
          <section className="space-y-2">
            <div className="flex items-center justify-between">
              <h2 className="text-xs font-semibold text-gray-400 uppercase tracking-wide">
                {t('workhours:leaveDay')}
              </h2>
              {leaveDay && (
                <button
                  type="button"
                  onClick={() => handleSetLeave(null)}
                  disabled={leaveSaving}
                  className="text-xs text-gray-500 hover:text-gray-300 transition-colors disabled:opacity-40 cursor-pointer"
                >
                  {t('workhours:removeLeave')}
                </button>
              )}
            </div>
            <div className="flex flex-wrap gap-2">
              {(['vacation', 'sick', 'personal', 'public_holiday'] as LeaveType[]).map(lt => (
                <button
                  key={lt}
                  type="button"
                  onClick={() => handleSetLeave(leaveDay?.leave_type === lt ? null : lt)}
                  disabled={leaveSaving}
                  className={`px-3 py-1.5 text-xs rounded transition-colors disabled:opacity-40 cursor-pointer border ${
                    leaveDay?.leave_type === lt
                      ? lt === 'vacation'
                        ? 'bg-purple-700 border-purple-500 text-white'
                        : lt === 'sick'
                          ? 'bg-orange-700 border-orange-500 text-white'
                          : lt === 'personal'
                            ? 'bg-teal-700 border-teal-500 text-white'
                            : 'bg-gray-700 border-gray-500 text-white'
                      : 'bg-gray-800 border-gray-700 text-gray-400 hover:text-gray-200 hover:border-gray-500'
                  }`}
                >
                  {t(`workhours:leaveType_${lt}`)}
                </button>
              ))}
            </div>
          </section>

          {/* Sessions */}
          <section className="space-y-3">
            <div className="flex items-center justify-between">
              <h2 className="text-xs font-semibold text-gray-400 uppercase tracking-wide">
                {t('workhours:sessions')}
              </h2>
              <div className="flex gap-2">
                {/* Copy yesterday button */}
                <button
                  type="button"
                  onClick={handleCopyYesterday}
                  disabled={saving || sessions.length > 0}
                  className="flex items-center gap-1 px-2 py-1 text-xs text-gray-400 hover:text-gray-200 bg-gray-800 hover:bg-gray-700 rounded transition-colors cursor-pointer disabled:opacity-40"
                  aria-label={t('workhours:copyYesterday')}
                  title={t('workhours:copyYesterday')}
                >
                  <Copy size={12} />
                  {t('workhours:copyYesterday')}
                </button>
                {/* Punch clock button */}
                {punchStart === null ? (
                  <button
                    type="button"
                    onClick={handlePunchIn}
                    disabled={saving}
                    className="flex items-center gap-1 px-2 py-1 text-xs text-green-400 hover:text-green-300 bg-gray-800 hover:bg-gray-700 rounded transition-colors cursor-pointer disabled:opacity-40"
                    aria-label={t('workhours:punchIn')}
                  >
                    <Clock size={12} />
                    {t('workhours:punchIn')}
                  </button>
                ) : (
                  <>
                    <button
                      type="button"
                      onClick={handlePunchOut}
                      disabled={saving}
                      className="flex items-center gap-1 px-2 py-1 text-xs text-red-400 hover:text-red-300 bg-gray-800 hover:bg-gray-700 rounded transition-colors cursor-pointer disabled:opacity-40 animate-pulse"
                      aria-label={t('workhours:punchOut')}
                    >
                      <Clock size={12} />
                      {t('workhours:punchOutAt', { time: punchStart })}
                    </button>
                    <button
                      type="button"
                      onClick={handleCancelPunch}
                      disabled={saving}
                      className="flex items-center gap-1 px-2 py-1 text-xs text-gray-400 hover:text-gray-200 bg-gray-800 hover:bg-gray-700 rounded transition-colors cursor-pointer disabled:opacity-40"
                      aria-label={t('workhours:cancelPunch')}
                    >
                      {t('workhours:cancelPunch')}
                    </button>
                  </>
                )}
              </div>
            </div>

            {sessions.length > 0 && (
              <div className="space-y-2">
                {sessions.map(s => {
                  const [sh, sm] = s.start_time.split(':').map(Number)
                  const [eh, em] = s.end_time.split(':').map(Number)
                  const mins = eh * 60 + em - (sh * 60 + sm)
                  return (
                    <div key={s.id} className={`flex items-center gap-3 rounded-lg px-3 py-2 ${s.is_internal ? 'bg-purple-900/40 border border-purple-700/40' : 'bg-gray-800'}`}>
                      <span className="text-white font-mono text-sm">{s.start_time}</span>
                      <span className="text-gray-500 text-xs">→</span>
                      <span className="text-white font-mono text-sm">{s.end_time}</span>
                      <span className="text-gray-400 text-xs ml-auto">{formatMins(mins)}</span>
                      <button
                        type="button"
                        onClick={() => handleToggleInternal(s)}
                        disabled={saving}
                        className={`transition-colors disabled:opacity-40 cursor-pointer ${s.is_internal ? 'text-purple-400 hover:text-purple-300' : 'text-gray-500 hover:text-purple-400'}`}
                        aria-label={s.is_internal ? t('workhours:markExternal') : t('workhours:markInternal')}
                        title={s.is_internal ? t('workhours:markExternal') : t('workhours:markInternal')}
                      >
                        <Building2 size={14} />
                      </button>
                      <button
                        type="button"
                        onClick={() => handleDeleteSession(s.id)}
                        disabled={saving}
                        className="text-gray-500 hover:text-red-400 transition-colors disabled:opacity-40 cursor-pointer"
                        aria-label={t('workhours:removeSession')}
                      >
                        <Trash2 size={14} />
                      </button>
                    </div>
                  )
                })}
              </div>
            )}

            {summary && summary.gross_minutes > 0 && (
              <p className="text-xs text-gray-400">
                {t('workhours:gross')}: <span className="font-mono">{formatMins(summary.gross_minutes)}</span>
              </p>
            )}

            {/* Add session row */}
            <div className="flex items-center gap-2 flex-wrap">
              <TimePicker
                value={newStart}
                onChange={setNewStart}
                aria-label={t('workhours:startTime')}
              />
              <span className="text-gray-500 text-xs">→</span>
              <TimePicker
                value={newEnd}
                onChange={setNewEnd}
                aria-label={t('workhours:endTime')}
              />
              <label className="flex items-center gap-1.5 cursor-pointer text-xs text-gray-400 select-none">
                <input
                  type="checkbox"
                  checked={newIsInternal}
                  onChange={e => setNewIsInternal(e.target.checked)}
                  className="accent-purple-500"
                  aria-label={t('workhours:markInternal')}
                />
                <Building2 size={12} className={newIsInternal ? 'text-purple-400' : 'text-gray-500'} />
                <span className={newIsInternal ? 'text-purple-400' : ''}>{t('workhours:internal')}</span>
              </label>
              <button
                type="button"
                onClick={handleAddSession}
                disabled={!newStart || !newEnd || saving}
                className="flex items-center gap-1 px-3 py-1.5 bg-blue-600 hover:bg-blue-500 disabled:opacity-40 disabled:cursor-not-allowed text-white text-sm rounded transition-colors cursor-pointer"
              >
                <Plus size={14} />
                {t('workhours:addSession')}
              </button>
            </div>
          </section>

          {/* Deductions */}
          <section className="space-y-3">
            <h2 className="text-xs font-semibold text-gray-400 uppercase tracking-wide">
              {t('workhours:deductions')}
            </h2>

            {/* Lunch checkbox */}
            <label className="flex items-center gap-3 cursor-pointer">
              <input
                type="checkbox"
                checked={lunchChecked}
                onChange={handleLunchToggle}
                disabled={saving}
                className="w-4 h-4 rounded accent-blue-500"
              />
              <span className="text-sm text-white">{t('workhours:lunch')}</span>
              {summary && summary.lunch_minutes > 0 && (
                <span className="text-xs text-gray-400 font-mono">({formatMins(summary.lunch_minutes)})</span>
              )}
            </label>

            {/* Preset chips */}
            {presets.length > 0 && (
              <div className="flex flex-wrap gap-2">
                {presets.map(p => {
                  const active = deductions.some(d => d.preset_id === p.id)
                  return (
                    <button
                      key={p.id}
                      type="button"
                      onClick={() => handlePresetToggle(p)}
                      disabled={saving}
                      className={`px-3 py-1 rounded-full text-xs font-medium transition-colors cursor-pointer disabled:opacity-40 ${
                        active
                          ? 'bg-blue-600 text-white'
                          : 'bg-gray-700 text-gray-300 hover:bg-gray-600'
                      }`}
                    >
                      {p.name} ({t('workhours:minutesValue', { count: p.default_minutes })})
                    </button>
                  )
                })}
              </div>
            )}

            {/* Custom deductions list */}
            {deductions.filter(d => !d.preset_id).length > 0 && (
              <div className="space-y-1">
                {deductions
                  .filter(d => !d.preset_id)
                  .map(d => (
                    <div key={d.id} className="flex items-center gap-2 text-sm bg-gray-800/50 rounded px-2 py-1">
                      <span className="flex-1 text-gray-300">{d.name}</span>
                      <span className="text-gray-400 text-xs font-mono">{formatMins(d.minutes)}</span>
                      <button
                        type="button"
                        onClick={() => handleDeleteDeduction(d.id)}
                        disabled={saving}
                        className="text-gray-500 hover:text-red-400 transition-colors disabled:opacity-40 cursor-pointer"
                        aria-label={t('workhours:removeDeduction')}
                      >
                        <Trash2 size={14} />
                      </button>
                    </div>
                  ))}
              </div>
            )}

            {/* Add custom deduction */}
            <div className="space-y-2">
              {/* Preset dropdown */}
              {sortedPresets.length > 0 && (
                <div className="flex items-center gap-2">
                  <Select
                    value={selectedPresetId !== null ? String(selectedPresetId) : ''}
                    onChange={handlePresetDropdownSelect}
                    placeholder={t('workhours:presetDropdownPlaceholder')}
                    aria-label={t('workhours:presetDropdownPlaceholder')}
                    className="flex-1"
                    options={sortedPresets.map(p => {
                      const icon = normalizePresetIcon(p.icon)
                      return {
                        value: String(p.id),
                        label: `${p.name} — ${t('workhours:minutesValue', { count: p.default_minutes })}`,
                        icon: icon || undefined,
                      } satisfies SelectOption
                    })}
                  />
                  <button
                    type="button"
                    onClick={onNavigateToSettings}
                    className="text-xs text-gray-400 hover:text-blue-400 transition-colors cursor-pointer whitespace-nowrap"
                  >
                    {t('workhours:managePresets')}
                  </button>
                </div>
              )}
              <div className="flex items-center gap-2 flex-wrap">
                <input
                  type="text"
                  value={newDeductionName}
                  onChange={e => setNewDeductionName(e.target.value)}
                  placeholder={t('workhours:deductionNamePlaceholder')}
                  aria-label={t('workhours:deductionNamePlaceholder')}
                  className="flex-1 min-w-32 bg-gray-800 text-white rounded px-2 py-1.5 text-sm border border-gray-700 focus:border-blue-500 focus:outline-none placeholder-gray-500"
                />
                <input
                  type="number"
                  value={newDeductionMinutes}
                  onChange={e => setNewDeductionMinutes(e.target.value)}
                  placeholder={t('workhours:minutesShort')}
                  min="1"
                  className="w-20 bg-gray-800 text-white rounded px-2 py-1.5 text-sm border border-gray-700 focus:border-blue-500 focus:outline-none placeholder-gray-500"
                  aria-label={t('workhours:minutes')}
                />
                <button
                  type="button"
                  onClick={handleAddDeduction}
                  disabled={!newDeductionName.trim() || !newDeductionMinutes || saving}
                  className="flex items-center gap-1 px-3 py-1.5 bg-gray-700 hover:bg-gray-600 disabled:opacity-40 disabled:cursor-not-allowed text-white text-sm rounded transition-colors cursor-pointer"
                  aria-label={t('workhours:addDeduction')}
                >
                  <Plus size={14} />
                </button>
              </div>
            </div>
          </section>

          {/* Summary card */}
          {summary && (
            <section className="bg-gray-800 rounded-lg p-4">
              <h2 className="text-xs font-semibold text-gray-400 uppercase tracking-wide mb-3">
                {t('workhours:summary')}
              </h2>
              <div className="grid grid-cols-2 gap-x-4 gap-y-2 text-sm">
                <span className="text-gray-400">{t('workhours:gross')}</span>
                <span className="text-white font-mono text-right">{formatMins(summary.gross_minutes)}</span>

                <span className="text-gray-400">{t('workhours:totalDeductions')}</span>
                <span className="text-white font-mono text-right">
                  -{formatMins(summary.lunch_minutes + summary.deduction_minutes)}
                </span>

                <span className="text-gray-400">{t('workhours:net')}</span>
                <span className="text-white font-mono text-right">{formatMins(summary.net_minutes)}</span>

                <span className="text-gray-400">{t('workhours:reported')}</span>
                <span className="text-white font-mono text-right">{formatMins(summary.reported_minutes)}</span>

                <span className="text-gray-400">{t('workhours:remainder')}</span>
                <span
                  className={`font-mono text-right ${summary.remainder_minutes < 0 ? 'text-red-400' : 'text-green-400'}`}
                >
                  {formatMins(summary.remainder_minutes)}
                </span>

                <span className="text-gray-400">{t('workhours:balance')}</span>
                <span
                  className={`font-mono text-right ${
                    summary.balance_minutes < 0
                      ? 'text-red-400'
                      : summary.balance_minutes > 0
                        ? 'text-green-400'
                        : 'text-white'
                  }`}
                >
                  {summary.balance_minutes > 0 ? '+' : ''}
                  {formatMins(summary.balance_minutes)}
                </span>
              </div>
            </section>
          )}

          {/* Flex pool running total */}
          {flex && (
            <section className="flex items-center justify-between bg-gray-800/50 rounded-lg px-4 py-3">
              <span className="text-sm text-gray-400">{t('workhours:flexPool')}</span>
              <span
                className={`font-mono text-sm font-semibold ${flex.flex.total_minutes < 0 ? 'text-red-400' : 'text-green-400'}`}
              >
                {flex.flex.total_minutes > 0 ? '+' : ''}
                {formatMins(flex.flex.total_minutes)}
              </span>
            </section>
          )}
        </>
      )}
    </div>
  )
}

// ── Week view ──────────────────────────────────────────────────────────────

function WeekView({
  currentDate,
  setCurrentDate,
  onSelectDay,
}: {
  currentDate: string
  setCurrentDate: (d: string | ((prev: string) => string)) => void
  onSelectDay: (date: string) => void
}) {
  const { t } = useTranslation(['workhours', 'common'])
  const [data, setData] = useState<WeekSummaryResponse | null>(null)
  const [loading, setLoading] = useState(false)

  const loadWeek = useCallback(async (date: string, signal: AbortSignal) => {
    setLoading(true)
    try {
      const r = await fetch(`/api/workhours/summary/week?date=${encodeURIComponent(date)}`, {
        credentials: 'include',
        signal,
      })
      if (signal.aborted) return
      setData(r.ok ? await r.json() : null)
    } catch (err) {
      if (err instanceof Error && err.name !== 'AbortError') setData(null)
    } finally {
      if (!signal.aborted) setLoading(false)
    }
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    // eslint-disable-next-line react-hooks/set-state-in-effect
    loadWeek(currentDate, controller.signal)
    return () => controller.abort()
  }, [currentDate, loadWeek])

  const summaryMap = new Map<string, DaySummary>()
  const dayMap = new Map<string, WorkDay>()
  const leaveDayMap = new Map<string, LeaveDay>()
  if (data) {
    data.summaries.forEach(s => summaryMap.set(s.date, s))
    data.days.forEach(d => dayMap.set(d.date, d))
    data.leave_days?.forEach(ld => leaveDayMap.set(ld.date, ld))
  }

  // The week_start from the API is the Monday of the week.
  // Fallback: normalize currentDate to its Monday so weekDays() renders the right range.
  const weekStart = data?.week_start ?? (() => {
    const d = new Date(currentDate + 'T12:00:00')
    const dow = d.getDay() // 0=Sun, 1=Mon, …
    const offsetToMon = dow === 0 ? -6 : 1 - dow
    d.setDate(d.getDate() + offsetToMon)
    return localDateStr(d)
  })()

  // Norwegian holidays for both years that might span the week
  const weekYear = parseInt(weekStart.split('-')[0])
  const weekHolidays = new Map([
    ...getNorwegianHolidays(weekYear),
    ...getNorwegianHolidays(weekYear + 1),
  ])

  // Build 5 weekday rows
  const rows = weekDays(weekStart)

  // Weekly totals — sum only the Mon–Fri rows shown in the table
  let totalNet = 0
  let totalReported = 0
  let totalBalance = 0
  rows.forEach(dateStr => {
    const s = summaryMap.get(dateStr)
    if (s) {
      totalNet += s.net_minutes
      totalReported += s.reported_minutes
      totalBalance += s.balance_minutes
    }
  })

  const weekLabel = (() => {
    const start = new Date(weekStart + 'T12:00:00')
    // Friday is 4 days after Monday
    const friday = new Date(weekStart + 'T12:00:00')
    friday.setDate(friday.getDate() + 4)
    const shortOpts: Intl.DateTimeFormatOptions = { day: 'numeric', month: 'short' }
    const startShort = formatDate(start, shortOpts)
    const fridayShort = formatDate(friday, shortOpts)
    const year = formatDate(start, { year: 'numeric' })
    return `${startShort} – ${fridayShort}, ${year}`
  })()

  return (
    <div className="space-y-4">
      {/* Week navigation */}
      <div className="flex items-center justify-between bg-gray-800 rounded-lg p-3">
        <button
          type="button"
          onClick={() => setCurrentDate(d => addWeeks(d, -1))}
          className="p-1 text-gray-400 hover:text-white transition-colors cursor-pointer"
          aria-label={t('workhours:prevWeek')}
        >
          <ChevronLeft size={20} />
        </button>
        <span className="text-sm font-medium text-white">{weekLabel}</span>
        <button
          type="button"
          onClick={() => setCurrentDate(d => addWeeks(d, 1))}
          className="p-1 text-gray-400 hover:text-white transition-colors cursor-pointer"
          aria-label={t('workhours:nextWeek')}
        >
          <ChevronRight size={20} />
        </button>
      </div>

      {loading ? (
        <div role="status" aria-live="polite" className="inline-flex items-center gap-2">
          <Skeleton className="h-5 w-24" />
          <span className="sr-only">{t('common:skeleton.loading')}</span>
        </div>
      ) : (
        <>
          {/* Week table */}
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="text-xs text-gray-400 uppercase tracking-wide border-b border-gray-700">
                  <th className="text-left py-2 pr-3 font-medium">{t('workhours:day')}</th>
                  <th className="text-right py-2 px-2 font-medium">{t('workhours:startTime')}</th>
                  <th className="text-right py-2 px-2 font-medium">{t('workhours:endTime')}</th>
                  <th className="text-right py-2 px-2 font-medium">{t('workhours:net')}</th>
                  <th className="text-right py-2 px-2 font-medium">{t('workhours:reported')}</th>
                  <th className="text-right py-2 pl-2 font-medium">+/-</th>
                </tr>
              </thead>
              <tbody>
                {rows.map(dateStr => {
                  const summary = summaryMap.get(dateStr)
                  const wd = dayMap.get(dateStr)
                  const range = sessionRange(wd)
                  const leaveEntry = leaveDayMap.get(dateStr)
                  const d = new Date(dateStr + 'T12:00:00')
                  const dayLabel = formatDate(d, {
                    weekday: 'short',
                    day: 'numeric',
                    month: 'short',
                  })
                  const balance = summary?.balance_minutes ?? null
                  const holidayLabel = weekHolidays.get(dateStr)
                  const isDimmed = !!holidayLabel || !!leaveEntry

                  return (
                    <tr
                      key={dateStr}
                      className={`border-b border-gray-800 transition-colors ${isDimmed ? 'opacity-60' : ''}`}
                    >
                      <td className="py-2.5 pr-3 capitalize">
                        <button
                          type="button"
                          onClick={() => onSelectDay(dateStr)}
                          className="w-full text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-blue-500/70 rounded-sm"
                        >
                          <span className={isDimmed ? 'text-gray-500' : 'text-gray-300'}>{dayLabel}</span>
                          {holidayLabel && (
                            <span className="block text-[0.65rem] text-gray-500 truncate max-w-24" title={holidayLabel}>
                              {holidayLabel}
                            </span>
                          )}
                          {leaveEntry && !holidayLabel && (
                            <span className="block text-[0.65rem] text-gray-500 truncate max-w-24">
                              {t(`workhours:leaveType_${leaveEntry.leave_type}`)}
                            </span>
                          )}
                        </button>
                      </td>
                      <td className="py-2.5 px-2 text-right font-mono text-gray-300">
                        {range ? range.start : <span className="text-gray-600">—</span>}
                      </td>
                      <td className="py-2.5 px-2 text-right font-mono text-gray-300">
                        {range ? range.end : <span className="text-gray-600">—</span>}
                      </td>
                      <td className="py-2.5 px-2 text-right font-mono text-gray-300">
                        {summary ? formatMins(summary.net_minutes) : <span className="text-gray-600">—</span>}
                      </td>
                      <td className="py-2.5 px-2 text-right font-mono text-gray-300">
                        {summary ? formatHours(summary.reported_minutes) : <span className="text-gray-600">—</span>}
                      </td>
                      <td
                        className={`py-2.5 pl-2 text-right font-mono font-medium ${
                          balance === null
                            ? 'text-gray-600'
                            : balance > 0
                              ? 'text-green-400'
                              : balance < 0
                                ? 'text-red-400'
                                : 'text-gray-400'
                        }`}
                      >
                        {balance === null ? (
                          '—'
                        ) : (
                          <>
                            {balance > 0 ? '+' : ''}
                            {formatMins(balance)}
                          </>
                        )}
                      </td>
                    </tr>
                  )
                })}
              </tbody>
              {summaryMap.size > 0 && (
                <tfoot>
                  <tr className="border-t border-gray-600 text-gray-200 font-semibold">
                    <td className="pt-2.5 pr-3 text-xs uppercase tracking-wide text-gray-400">
                      {t('workhours:weeklyTotal')}
                    </td>
                    <td />
                    <td />
                    <td className="pt-2.5 px-2 text-right font-mono">{formatMins(totalNet)}</td>
                    <td className="pt-2.5 px-2 text-right font-mono">{formatHours(totalReported)}</td>
                    <td
                      className={`pt-2.5 pl-2 text-right font-mono ${
                        totalBalance > 0 ? 'text-green-400' : totalBalance < 0 ? 'text-red-400' : 'text-gray-400'
                      }`}
                    >
                      {totalBalance > 0 ? '+' : ''}
                      {formatMins(totalBalance)}
                    </td>
                  </tr>
                </tfoot>
              )}
            </table>
          </div>

          {/* Week flex balance */}
          {data && data.flex && (
            <div className="flex items-center justify-between bg-gray-800/50 rounded-lg px-4 py-3">
              <span className="text-sm text-gray-400">{t('workhours:flexPool')}</span>
              <span
                className={`font-mono text-sm font-semibold ${
                  data.flex.total_minutes < 0 ? 'text-red-400' : 'text-green-400'
                }`}
              >
                {data.flex.total_minutes > 0 ? '+' : ''}
                {formatMins(data.flex.total_minutes)}
              </span>
            </div>
          )}
        </>
      )}
    </div>
  )
}

// ── Month view ─────────────────────────────────────────────────────────────

function MonthView({
  currentDate,
  setCurrentDate,
  onSelectDay,
}: {
  currentDate: string
  setCurrentDate: (d: string | ((prev: string) => string)) => void
  onSelectDay: (date: string) => void
}) {
  const { t } = useTranslation(['workhours', 'common'])
  const monthStr = dateToMonthStr(currentDate)
  const yearStr = monthStr.slice(0, 4)
  const [data, setData] = useState<MonthSummaryResponse | null>(null)
  const [leaveBalance, setLeaveBalance] = useState<LeaveBalance | null>(null)
  const [loading, setLoading] = useState(false)

  const loadMonth = useCallback(async (month: string, signal: AbortSignal) => {
    setLoading(true)
    try {
      const r = await fetch(`/api/workhours/summary/month?month=${encodeURIComponent(month)}`, {
        credentials: 'include',
        signal,
      })
      if (signal.aborted) return
      setData(r.ok ? await r.json() : null)
    } catch (err) {
      if (err instanceof Error && err.name !== 'AbortError') setData(null)
    } finally {
      if (!signal.aborted) setLoading(false)
    }
  }, [])

  const loadLeaveBalance = useCallback(async (year: string, signal: AbortSignal) => {
    try {
      const r = await fetch(`/api/workhours/leave/balance?year=${encodeURIComponent(year)}`, {
        credentials: 'include',
        signal,
      })
      if (signal.aborted) return
      if (r.ok) setLeaveBalance(await r.json())
      else setLeaveBalance(null)
    } catch (err) {
      if (err instanceof Error && err.name !== 'AbortError') setLeaveBalance(null)
    }
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    // eslint-disable-next-line react-hooks/set-state-in-effect
    loadMonth(monthStr, controller.signal)
    loadLeaveBalance(yearStr, controller.signal)
    return () => controller.abort()
  }, [monthStr, yearStr, loadMonth, loadLeaveBalance])

  const summaryMap = new Map<string, DaySummary>()
  const leaveDayMap = new Map<string, LeaveDay>()
  if (data) {
    data.summaries.forEach(s => summaryMap.set(s.date, s))
    data.leave_days?.forEach(ld => leaveDayMap.set(ld.date, ld))
  }

  const monthLabel = formatDate(new Date(monthStr + '-01T12:00:00'), {
    month: 'long',
    year: 'numeric',
  })

  // Day-of-week header labels (Mon–Sun)
  const dowHeaders = Array.from({ length: 7 }, (_, i) => {
    const d = new Date(2024, 0, 1 + i) // Jan 1 2024 is a Monday
    return formatDate(d, { weekday: 'short' })
  })

  const grid = buildMonthGrid(monthStr)

  // Norwegian holidays for this month's year (and adjacent year for Dec/Jan edge)
  const monthYear = parseInt(monthStr.split('-')[0])
  const monthHolidays = new Map([
    ...getNorwegianHolidays(monthYear),
    ...getNorwegianHolidays(monthYear + 1),
  ])
  const holidaySet = new Set(monthHolidays.keys())

  // Monthly totals
  const standard = data?.summaries[0]?.standard_minutes ?? 450
  const totalWorked = data ? data.summaries.reduce((sum, s) => sum + s.reported_minutes, 0) : 0
  const todayStr = localDateStr(new Date())
  const currentMonthStr = todayStr.length >= 7 ? todayStr.slice(0, 7) : monthStr
  const isCurrentMonth = monthStr === currentMonthStr
  const workdaysTarget = isCurrentMonth
    ? countWorkdaysUpToNow(monthStr, holidaySet)
    : countWorkdaysInMonth(monthStr, holidaySet)
  const totalTarget = workdaysTarget * standard
  const totalBalance = totalWorked - totalTarget

  const today = todayStr

  return (
    <div className="space-y-4">
      {/* Month navigation */}
      <div className="flex items-center justify-between bg-gray-800 rounded-lg p-3">
        <button
          type="button"
          onClick={() => setCurrentDate(addMonths(monthStr, -1) + '-01')}
          className="p-1 text-gray-400 hover:text-white transition-colors cursor-pointer"
          aria-label={t('workhours:prevMonth')}
        >
          <ChevronLeft size={20} />
        </button>
        <span className="text-sm font-medium text-white capitalize">{monthLabel}</span>
        <button
          type="button"
          onClick={() => setCurrentDate(addMonths(monthStr, 1) + '-01')}
          className="p-1 text-gray-400 hover:text-white transition-colors cursor-pointer"
          aria-label={t('workhours:nextMonth')}
        >
          <ChevronRight size={20} />
        </button>
      </div>

      {loading ? (
        <div role="status" aria-live="polite" className="inline-flex items-center gap-2">
          <Skeleton className="h-5 w-24" />
          <span className="sr-only">{t('common:skeleton.loading')}</span>
        </div>
      ) : (
        <>
          {/* Calendar grid */}
          <div>
            {/* Day-of-week headers */}
            <div className="grid grid-cols-7 gap-1 mb-1">
              {dowHeaders.map((h, i) => (
                <div
                  key={h}
                  className={`text-center text-xs font-medium py-1 ${
                    i >= 5 ? 'text-gray-600' : 'text-gray-400'
                  }`}
                >
                  {h}
                </div>
              ))}
            </div>
            {/* Weeks */}
            <div className="space-y-1">
              {grid.map((week, wi) => (
                <div key={week.find(d => d !== null) ?? wi} className="grid grid-cols-7 gap-1">
                  {week.map((dateStr, di) => {
                    if (!dateStr) {
                      return <div key={`pad-${di}`} className="aspect-square" />
                    }
                    const summary = summaryMap.get(dateStr)
                    const leaveEntry = leaveDayMap.get(dateStr)
                    const isWeekend = di >= 5
                    const isHoliday = !isWeekend && monthHolidays.has(dateStr)
                    const holidayLabel = monthHolidays.get(dateStr)
                    const isToday = dateStr === today
                    const dayNum = parseInt(dateStr.split('-')[2])
                    const cellClass = dayCellClass(summary, isWeekend, isHoliday, leaveEntry?.leave_type)
                    const isDisabled = isWeekend
                    const cellTitle = holidayLabel ?? (leaveEntry ? t(`workhours:leaveType_${leaveEntry.leave_type}`) : undefined)

                    return (
                      <button
                        key={dateStr}
                        type="button"
                        onClick={() => !isDisabled && onSelectDay(dateStr)}
                        disabled={isDisabled}
                        title={cellTitle}
                        className={`aspect-square rounded flex flex-col items-center justify-center text-xs transition-colors ${cellClass} ${
                          isDisabled ? 'cursor-default' : 'hover:ring-1 hover:ring-gray-500 cursor-pointer'
                        } ${isToday ? 'ring-1 ring-blue-500' : ''}`}
                        aria-label={cellTitle ? `${dateStr} – ${cellTitle}` : dateStr}
                      >
                        <span className="font-medium leading-none">{dayNum}</span>
                        {isHoliday ? (
                          <span className="text-[0.55rem] leading-tight mt-0.5 opacity-70 truncate max-w-full px-0.5 text-center">
                            {holidayLabel}
                          </span>
                        ) : leaveEntry ? (
                          <span className="text-[0.55rem] leading-tight mt-0.5 opacity-80 truncate max-w-full px-0.5 text-center">
                            {t(`workhours:leaveType_${leaveEntry.leave_type}`)}
                          </span>
                        ) : summary && summary.reported_minutes > 0 ? (
                          <span className="text-[0.6rem] leading-tight mt-0.5 opacity-80">
                            {formatHours(summary.reported_minutes)}
                          </span>
                        ) : null}
                      </button>
                    )
                  })}
                </div>
              ))}
            </div>
          </div>

          {/* Monthly totals */}
          <section className="bg-gray-800 rounded-lg p-4">
            <h2 className="text-xs font-semibold text-gray-400 uppercase tracking-wide mb-3">
              {t('workhours:monthlyTotal')}
            </h2>
            <div className="grid grid-cols-2 gap-x-4 gap-y-2 text-sm">
              <span className="text-gray-400">{t('workhours:worked')}</span>
              <span className="text-white font-mono text-right">{formatHours(totalWorked)}</span>

              <span className="text-gray-400">{t('workhours:target')}</span>
              <span className="text-white font-mono text-right">{formatHours(totalTarget)}</span>

              <span className="text-gray-400">{t('workhours:balance')}</span>
              <span
                className={`font-mono text-right ${
                  totalBalance > 0 ? 'text-green-400' : totalBalance < 0 ? 'text-red-400' : 'text-white'
                }`}
              >
                {totalBalance > 0 ? '+' : ''}
                {formatHours(totalBalance)}
              </span>
            </div>
          </section>

          {/* Leave balance */}
          {leaveBalance && leaveBalance.vacation_allowance > 0 && (
            <section className="bg-gray-800 rounded-lg p-4 space-y-3">
              <h2 className="text-xs font-semibold text-gray-400 uppercase tracking-wide">
                {t('workhours:leaveBalance')}
              </h2>
              <div className="space-y-2">
                {/* Vacation allowance bar */}
                <div>
                  <div className="flex justify-between text-sm mb-1">
                    <span className="text-gray-300">{t('workhours:leaveType_vacation')}</span>
                    <span className="text-gray-400 font-mono">
                      {t('workhours:leaveUsedOf', {
                        used: leaveBalance.vacation_used,
                        total: leaveBalance.vacation_allowance,
                      })}
                    </span>
                  </div>
                  <div className="h-1.5 bg-gray-700 rounded-full overflow-hidden">
                    <div
                      className="h-full bg-purple-500 rounded-full transition-all"
                      style={{
                        width: `${Math.min(100, (leaveBalance.vacation_used / leaveBalance.vacation_allowance) * 100)}%`,
                      }}
                    />
                  </div>
                </div>
                {/* Other leave types */}
                <div className="grid grid-cols-2 gap-x-4 gap-y-1 text-sm pt-1">
                  {leaveBalance.sick_used > 0 && (
                    <>
                      <span className="text-gray-400">{t('workhours:leaveType_sick')}</span>
                      <span className="text-white font-mono text-right">{leaveBalance.sick_used} {t('workhours:days')}</span>
                    </>
                  )}
                  {leaveBalance.personal_used > 0 && (
                    <>
                      <span className="text-gray-400">{t('workhours:leaveType_personal')}</span>
                      <span className="text-white font-mono text-right">{leaveBalance.personal_used} {t('workhours:days')}</span>
                    </>
                  )}
                  {leaveBalance.public_holiday_used > 0 && (
                    <>
                      <span className="text-gray-400">{t('workhours:leaveType_public_holiday')}</span>
                      <span className="text-white font-mono text-right">{leaveBalance.public_holiday_used} {t('workhours:days')}</span>
                    </>
                  )}
                </div>
              </div>
            </section>
          )}

          {/* Flex trend chart */}
          {data && data.summaries.length > 0 && (
            <FlexTrendChart summaries={data.summaries} />
          )}
        </>
      )}
    </div>
  )
}

// ── Flex trend chart ───────────────────────────────────────────────────────

function FlexTrendChart({ summaries }: { summaries: DaySummary[] }) {
  const { t } = useTranslation('workhours')

  // Use only work days (Mon-Fri), sorted chronologically
  const dataPoints = summaries
    .filter(s => {
      const dow = new Date(s.date + 'T12:00:00').getDay()
      return dow !== 0 && dow !== 6
    })
    .sort((a, b) => a.date.localeCompare(b.date))

  if (dataPoints.length === 0) return null

  // Cumulative remainder
  const points = dataPoints.reduce<number[]>((acc, s) => {
    acc.push((acc.length > 0 ? acc[acc.length - 1] : 0) + s.remainder_minutes)
    return acc
  }, [])

  const W = 400
  const H = 72
  const PX = 4
  const PY = 8
  const chartW = W - 2 * PX
  const chartH = H - 2 * PY

  const minVal = Math.min(0, ...points)
  const maxVal = Math.max(0, ...points)
  const range = maxVal - minVal || 1

  const n = points.length
  const toX = (i: number) => PX + (n > 1 ? (i / (n - 1)) * chartW : chartW / 2)
  const toY = (v: number) => PY + chartH - ((v - minVal) / range) * chartH

  const polyPts = points.map((v, i) => `${toX(i).toFixed(1)},${toY(v).toFixed(1)}`).join(' ')
  const zeroY = toY(0).toFixed(1)
  const lastVal = points[points.length - 1]
  const lineColor = lastVal >= 0 ? '#4ade80' : '#f87171'

  return (
    <section className="space-y-2">
      <h2 className="text-xs font-semibold text-gray-400 uppercase tracking-wide">
        {t('flexTrend')}
      </h2>
      <div className="bg-gray-800 rounded-lg px-3 py-3">
        <svg viewBox={`0 0 ${W} ${H}`} className="w-full" aria-hidden="true">
          {/* Zero reference line */}
          <line
            x1={PX}
            y1={zeroY}
            x2={W - PX}
            y2={zeroY}
            stroke="#374151"
            strokeWidth="1"
            strokeDasharray="4 3"
          />
          {/* Trend line */}
          {n > 1 && (
            <polyline points={polyPts} fill="none" stroke={lineColor} strokeWidth="2" strokeLinejoin="round" />
          )}
          {/* Data points */}
          {points.map((v, i) => (
            <circle key={dataPoints[i].date} cx={toX(i).toFixed(1)} cy={toY(v).toFixed(1)} r="3" fill={lineColor} />
          ))}
        </svg>
        <div className="flex justify-between text-xs text-gray-500 mt-1 px-1">
          <span>{dataPoints[0]?.date.slice(8)}</span>
          <span
            className={`font-mono font-medium ${lastVal >= 0 ? 'text-green-400' : 'text-red-400'}`}
          >
            {lastVal > 0 ? '+' : ''}
            {formatMins(lastVal)}
          </span>
          <span>{dataPoints[dataPoints.length - 1]?.date.slice(8)}</span>
        </div>
      </div>
    </section>
  )
}

// ── Deduction emoji picker ──────────────────────────────────────────────────

const DEDUCTION_EMOJIS = [
  { key: 'transport', emojis: ['🚗', '🚌', '🚲', '🚶', '🚕', '✈️'] },
  { key: 'childcare', emojis: ['👶', '🏫', '🎒', '🧒', '🍼'] },
  { key: 'medical', emojis: ['🏥', '💊', '🦷', '🩺', '💉'] },
  { key: 'errands', emojis: ['🛒', '📬', '🏦', '🏪', '📦'] },
  { key: 'meetings', emojis: ['☕', '📞', '💼', '🤝', '📅'] },
  { key: 'general', emojis: ['⏰', '🔧', '📋', '⏸️', '🔔'] },
]

// 'clock' was the legacy text value stored before the emoji picker was added.
// For persistence, treat it as "no icon" by normalizing it to an empty string.
function normalizePresetIconValue(icon: string): string {
  if (icon === 'clock') return ''
  return icon || ''
}

// For UI display, show the clock emoji as a placeholder when there is no icon
// or when the legacy 'clock' value is present.
function getPresetIconDisplay(icon: string): string {
  if (!icon || icon === 'clock') return '⏰'
  return icon
}

// Legacy helper kept for backwards compatibility: this now only performs
// storage normalization. Prefer using `normalizePresetIconValue` for
// persistence and `getPresetIconDisplay` for rendering.
function normalizePresetIcon(icon: string): string {
  return normalizePresetIconValue(icon)
}

interface EmojiPickerDropdownProps {
  value: string
  onChange: (emoji: string) => void
  customInputId: string
  buttonClassName?: string
}

function EmojiPickerDropdown({ value, onChange, customInputId, buttonClassName }: EmojiPickerDropdownProps) {
  const { t } = useTranslation(['workhours'])
  const [showPicker, setShowPicker] = useState(false)
  const pickerRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (showPicker) pickerRef.current?.focus()
  }, [showPicker])

  return (
    <div className="relative">
      <button
        type="button"
        onClick={() => setShowPicker(p => !p)}
        onKeyDown={e => { if (e.key === 'Escape') setShowPicker(false) }}
        className={`w-14 text-white rounded px-2 py-1.5 text-lg text-center border border-gray-600 focus:border-blue-500 focus:outline-none cursor-pointer ${buttonClassName ?? 'bg-gray-700'}`}
        aria-label={t('workhours:chooseIcon')}
        aria-haspopup="dialog"
        aria-expanded={showPicker}
      >
        {getPresetIconDisplay(value)}
      </button>
      {showPicker && (
        <>
          <div className="fixed inset-0 z-10" onClick={() => setShowPicker(false)} />
          <div
            role="dialog"
            aria-modal="true"
            aria-label={t('workhours:chooseIcon')}
            tabIndex={-1}
            ref={pickerRef}
            onKeyDown={e => { if (e.key === 'Escape') setShowPicker(false) }}
            className="absolute right-0 top-full mt-1 bg-gray-800 border border-gray-600 rounded-xl p-3 z-20 w-64 shadow-xl focus:outline-none"
          >
            {DEDUCTION_EMOJIS.map(({ key, emojis }) => (
              <div key={key} className="mb-3 last:mb-0">
                <p className="text-xs text-gray-400 mb-1">{t(`workhours:emojiCategories.${key}` as never, { defaultValue: key })}</p>
                <div className="flex flex-wrap gap-1">
                  {emojis.map(emoji => (
                    <button
                      key={emoji}
                      type="button"
                      onClick={() => { onChange(emoji); setShowPicker(false) }}
                      className={`text-xl p-1.5 rounded-lg transition-colors cursor-pointer ${value === emoji ? 'bg-blue-600' : 'hover:bg-gray-600'}`}
                    >
                      {emoji}
                    </button>
                  ))}
                </div>
              </div>
            ))}
            <div className="mt-2 border-t border-gray-600 pt-2">
              <label htmlFor={customInputId} className="block text-xs text-gray-400 mb-1">{t('workhours:customEmoji')}</label>
              <input
                id={customInputId}
                type="text"
                value={value ?? ''}
                onChange={e => onChange(e.target.value)}
                className="w-full bg-gray-700 text-white rounded-lg px-3 py-2 text-sm text-center focus:outline-none focus:ring-2 focus:ring-blue-500"
              />
            </div>
          </div>
        </>
      )}
    </div>
  )
}

// ── Settings tab ───────────────────────────────────────────────────────────

function SettingsTab() {
  const { t } = useTranslation(['workhours', 'common'])

  // Work settings
  const [standardHours, setStandardHours] = useState('7.5')
  const [rounding, setRounding] = useState('30')
  const [lunchMinutes, setLunchMinutes] = useState('30')
  const [vacationAllowance, setVacationAllowance] = useState('25')
  const [settingsLoaded, setSettingsLoaded] = useState(false)
  const [settingsSaving, setSettingsSaving] = useState(false)
  const [showResetFlexConfirm, setShowResetFlexConfirm] = useState(false)

  // Presets
  const [presets, setPresets] = useState<WorkDeductionPreset[]>([])
  const [presetsLoading, setPresetsLoading] = useState(false)
  const [presetSaving, setPresetSaving] = useState(false)
  const [newPresetName, setNewPresetName] = useState('')
  const [newPresetMinutes, setNewPresetMinutes] = useState('')
  const [newPresetIcon, setNewPresetIcon] = useState('')
  const [editingPreset, setEditingPreset] = useState<WorkDeductionPreset | null>(null)
  const [editName, setEditName] = useState('')
  const [editMinutes, setEditMinutes] = useState('')
  const [editIcon, setEditIcon] = useState('')

  const loadPresets = useCallback(() => {
    setPresetsLoading(true)
    fetch('/api/workhours/presets', { credentials: 'include' })
      .then(r => (r.ok ? r.json() : { presets: [] }))
      .then((data: { presets: WorkDeductionPreset[] }) => setPresets(data.presets ?? []))
      .catch(() => {})
      .finally(() => setPresetsLoading(false))
  }, [])

  useEffect(() => {
    fetch('/api/settings/preferences', { credentials: 'include' })
      .then(r => (r.ok ? r.json() : null))
      .then((data: { preferences?: Record<string, string> } | null) => {
        const prefs = data?.preferences
        if (prefs) {
          if (prefs.work_hours_standard_day) {
            setStandardHours((parseInt(prefs.work_hours_standard_day) / 60).toString())
          }
          if (prefs.work_hours_rounding) setRounding(prefs.work_hours_rounding)
          if (prefs.work_hours_lunch_minutes) setLunchMinutes(prefs.work_hours_lunch_minutes)
          if (prefs.work_hours_vacation_allowance) setVacationAllowance(prefs.work_hours_vacation_allowance)
        }
        setSettingsLoaded(true)
      })
      .catch(() => setSettingsLoaded(true))
    // eslint-disable-next-line react-hooks/set-state-in-effect
    loadPresets()
  }, [loadPresets])

  const handleSaveSettings = async () => {
    const hours = parseFloat(standardHours)
    if (isNaN(hours) || hours <= 0) return
    const lunch = Math.max(0, parseInt(lunchMinutes, 10) || 0)
    const parsedVacation = Number.parseInt(vacationAllowance, 10)
    const vacation = Number.isNaN(parsedVacation) ? 25 : Math.max(1, Math.min(100, parsedVacation))
    setSettingsSaving(true)
    try {
      const results = await Promise.all([
        fetch('/api/settings/preferences', {
          method: 'PUT',
          credentials: 'include',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ preferences: { work_hours_standard_day: String(Math.round(hours * 60)) } }),
        }),
        fetch('/api/settings/preferences', {
          method: 'PUT',
          credentials: 'include',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ preferences: { work_hours_rounding: rounding } }),
        }),
        fetch('/api/settings/preferences', {
          method: 'PUT',
          credentials: 'include',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ preferences: { work_hours_lunch_minutes: String(lunch) } }),
        }),
        fetch('/api/settings/preferences', {
          method: 'PUT',
          credentials: 'include',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ preferences: { work_hours_vacation_allowance: String(vacation) } }),
        }),
      ])
      if (results.some(r => !r.ok)) {
        console.error('workhours: one or more settings failed to save')
      }
    } finally {
      setSettingsSaving(false)
    }
  }

  const handleAddPreset = async () => {
    const name = newPresetName.trim()
    const minutes = parseInt(newPresetMinutes, 10)
    if (!name || !minutes || minutes <= 0) return
    setPresetSaving(true)
    try {
      const r = await fetch('/api/workhours/presets', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name, default_minutes: minutes, icon: newPresetIcon.trim() || 'clock' }),
      })
      if (r.ok) {
        setNewPresetName('')
        setNewPresetMinutes('')
        setNewPresetIcon('')
        loadPresets()
      }
    } finally {
      setPresetSaving(false)
    }
  }

  const handleEditPreset = (preset: WorkDeductionPreset) => {
    setEditingPreset(preset)
    setEditName(preset.name)
    setEditMinutes(String(preset.default_minutes))
    setEditIcon(preset.icon)
  }

  const handleSavePreset = async () => {
    if (!editingPreset) return
    const name = editName.trim()
    const minutes = parseInt(editMinutes, 10)
    if (!name || !minutes || minutes <= 0) return
    setPresetSaving(true)
    try {
      const r = await fetch(`/api/workhours/presets/${editingPreset.id}`, {
        method: 'PUT',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name, default_minutes: minutes, icon: editIcon.trim() || 'clock', active: editingPreset.active }),
      })
      if (r.ok) {
        const updated: WorkDeductionPreset = await r.json()
        setPresets(prev => prev.map(p => (p.id === updated.id ? updated : p)))
        setEditingPreset(null)
      }
    } finally {
      setPresetSaving(false)
    }
  }

  const handleDeletePreset = async (id: number) => {
    setPresetSaving(true)
    try {
      const r = await fetch(`/api/workhours/presets/${id}`, { method: 'DELETE', credentials: 'include' })
      if (r.ok) loadPresets()
    } finally {
      setPresetSaving(false)
    }
  }

  const handleFlexReset = async () => {
    setSettingsSaving(true)
    try {
      const r = await fetch('/api/workhours/flex/reset', { method: 'POST', credentials: 'include' })
      if (!r.ok) console.error('workhours: flex reset failed')
    } finally {
      setSettingsSaving(false)
    }
  }

  return (
    <div className="space-y-8">
      {/* Work settings */}
      <section className="space-y-4">
        <h2 className="text-xs font-semibold text-gray-400 uppercase tracking-wide">
          {t('workhours:workSettings')}
        </h2>
        {settingsLoaded && (
          <div className="space-y-3">
            <label className="flex items-center gap-3">
              <span className="text-sm text-gray-300 w-52">{t('workhours:standardDay')}</span>
              <input
                type="number"
                value={standardHours}
                onChange={e => setStandardHours(e.target.value)}
                min="1"
                max="16"
                step="0.5"
                className="w-20 bg-gray-800 text-white rounded px-2 py-1.5 text-sm border border-gray-700 focus:border-blue-500 focus:outline-none"
              />
            </label>
            <label htmlFor="rounding-select" className="flex items-center gap-3">
              <span className="text-sm text-gray-300 w-52">{t('workhours:rounding')}</span>
              <Select
                id="rounding-select"
                value={rounding}
                onChange={setRounding}
                aria-label={t('workhours:rounding')}
                options={[
                  { value: '15', label: '15' },
                  { value: '30', label: '30' },
                  { value: '60', label: '60' },
                ]}
              />
            </label>
            <label className="flex items-center gap-3">
              <span className="text-sm text-gray-300 w-52">{t('workhours:lunchDuration')}</span>
              <input
                type="number"
                value={lunchMinutes}
                onChange={e => setLunchMinutes(e.target.value)}
                min="0"
                max="120"
                className="w-20 bg-gray-800 text-white rounded px-2 py-1.5 text-sm border border-gray-700 focus:border-blue-500 focus:outline-none"
              />
            </label>
            <label className="flex items-center gap-3">
              <span className="text-sm text-gray-300 w-52">{t('workhours:vacationAllowance')}</span>
              <input
                type="number"
                value={vacationAllowance}
                onChange={e => setVacationAllowance(e.target.value)}
                min="1"
                max="100"
                className="w-20 bg-gray-800 text-white rounded px-2 py-1.5 text-sm border border-gray-700 focus:border-blue-500 focus:outline-none"
              />
            </label>
            <button
              type="button"
              onClick={handleSaveSettings}
              disabled={settingsSaving}
              className="px-4 py-2 bg-blue-600 hover:bg-blue-500 disabled:opacity-40 text-white text-sm rounded transition-colors cursor-pointer"
            >
              {t('common:actions.save')}
            </button>
          </div>
        )}
      </section>

      {/* Flex pool */}
      <section className="space-y-3">
        <h2 className="text-xs font-semibold text-gray-400 uppercase tracking-wide">
          {t('workhours:flexPool')}
        </h2>
        <button
          type="button"
          onClick={() => setShowResetFlexConfirm(true)}
          disabled={settingsSaving}
          className="px-4 py-2 bg-gray-700 hover:bg-gray-600 disabled:opacity-40 text-white text-sm rounded transition-colors cursor-pointer"
        >
          {t('workhours:resetFlexPool')}
        </button>
      </section>

      {/* Deduction presets */}
      <section className="space-y-3">
        <h2 className="text-xs font-semibold text-gray-400 uppercase tracking-wide">
          {t('workhours:presets')}
        </h2>

        {presetsLoading ? (
          <div role="status" aria-live="polite">
            <span className="sr-only">{t('common:skeleton.loading')}</span>
            <Skeleton className="h-5 w-24" />
          </div>
        ) : presets.length === 0 ? (
          <p className="text-sm text-gray-500">{t('workhours:noPresets')}</p>
        ) : (
          <div className="space-y-2">
            {presets.map(p => {
              const icon = normalizePresetIcon(p.icon)
              return editingPreset?.id === p.id ? (
                <div key={p.id} className="bg-gray-800 rounded-lg p-3 space-y-2">
                  <div className="flex gap-2 flex-wrap">
                    <input
                      type="text"
                      value={editName}
                      onChange={e => setEditName(e.target.value)}
                      placeholder={t('workhours:presetName')}
                      aria-label={t('workhours:presetName')}
                      className="flex-1 min-w-32 bg-gray-700 text-white rounded px-2 py-1.5 text-sm border border-gray-600 focus:border-blue-500 focus:outline-none"
                    />
                    <input
                      type="number"
                      value={editMinutes}
                      onChange={e => setEditMinutes(e.target.value)}
                      placeholder={t('workhours:minutesShort')}
                      min="1"
                      aria-label={t('workhours:defaultMinutes')}
                      className="w-20 bg-gray-700 text-white rounded px-2 py-1.5 text-sm border border-gray-600 focus:border-blue-500 focus:outline-none"
                    />
                    <EmojiPickerDropdown
                      value={editIcon}
                      onChange={setEditIcon}
                      customInputId="edit-icon-custom"
                    />
                  </div>
                  <div className="flex gap-2">
                    <button
                      type="button"
                      onClick={handleSavePreset}
                      disabled={presetSaving}
                      className="px-3 py-1.5 bg-blue-600 hover:bg-blue-500 disabled:opacity-40 text-white text-sm rounded cursor-pointer"
                    >
                      {t('common:actions.save')}
                    </button>
                    <button
                      type="button"
                      onClick={() => setEditingPreset(null)}
                      className="px-3 py-1.5 bg-gray-700 hover:bg-gray-600 text-white text-sm rounded cursor-pointer"
                    >
                      {t('common:actions.cancel')}
                    </button>
                  </div>
                </div>
              ) : (
                <div key={p.id} className="flex items-center gap-3 bg-gray-800/60 rounded-lg px-3 py-2">
                  {icon && <span className="text-base w-5 text-center">{icon}</span>}
                  <span className="flex-1 text-sm text-white">{p.name}</span>
                  <span className="text-xs text-gray-400 font-mono">
                    {t('workhours:minutesValue', { count: p.default_minutes })}
                  </span>
                  <button
                    type="button"
                    onClick={() => handleEditPreset(p)}
                    disabled={presetSaving}
                    className="text-xs text-gray-400 hover:text-white px-2 py-1 rounded transition-colors cursor-pointer disabled:opacity-40"
                  >
                    {t('common:actions.edit')}
                  </button>
                  <button
                    type="button"
                    onClick={() => handleDeletePreset(p.id)}
                    disabled={presetSaving}
                    className="text-gray-500 hover:text-red-400 transition-colors disabled:opacity-40 cursor-pointer"
                    aria-label={t('common:actions.delete')}
                  >
                    <Trash2 size={14} />
                  </button>
                </div>
              )
            })}
          </div>
        )}

        {/* Add new preset */}
        <div className="flex gap-2 flex-wrap">
          <input
            type="text"
            value={newPresetName}
            onChange={e => setNewPresetName(e.target.value)}
            placeholder={t('workhours:presetName')}
            aria-label={t('workhours:presetName')}
            className="flex-1 min-w-32 bg-gray-800 text-white rounded px-2 py-1.5 text-sm border border-gray-700 focus:border-blue-500 focus:outline-none placeholder-gray-500"
          />
          <input
            type="number"
            value={newPresetMinutes}
            onChange={e => setNewPresetMinutes(e.target.value)}
            placeholder={t('workhours:minutesShort')}
            min="1"
            aria-label={t('workhours:defaultMinutes')}
            className="w-20 bg-gray-800 text-white rounded px-2 py-1.5 text-sm border border-gray-700 focus:border-blue-500 focus:outline-none placeholder-gray-500"
          />
          <EmojiPickerDropdown
            value={newPresetIcon}
            onChange={setNewPresetIcon}
            customInputId="new-icon-custom"
            buttonClassName="bg-gray-800 border-gray-700"
          />
          <button
            type="button"
            onClick={handleAddPreset}
            disabled={!newPresetName.trim() || !newPresetMinutes || presetSaving}
            className="flex items-center gap-1 px-3 py-1.5 bg-blue-600 hover:bg-blue-500 disabled:opacity-40 disabled:cursor-not-allowed text-white text-sm rounded transition-colors cursor-pointer"
          >
            <Plus size={14} />
            {t('workhours:addPreset')}
          </button>
        </div>
      </section>
      <ConfirmDialog
        open={showResetFlexConfirm}
        onClose={() => setShowResetFlexConfirm(false)}
        onConfirm={handleFlexReset}
        title={t('workhours:resetFlexPool')}
        message={t('workhours:resetFlexConfirm')}
        variant="default"
      />
    </div>
  )
}
