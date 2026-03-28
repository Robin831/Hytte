import { useState, useEffect, useCallback, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { ChevronLeft, ChevronRight, Plus, Trash2 } from 'lucide-react'
import { formatDate } from '../utils/formatDate'

// ── Interfaces ──────────────────────────────────────────────────────────────

interface WorkSession {
  id: number
  day_id: number
  start_time: string
  end_time: string
  sort_order: number
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

interface WeekSummaryResponse {
  week_start: string
  week_end: string
  days: WorkDay[]
  summaries: DaySummary[]
  flex: FlexPoolResult
}

interface MonthSummaryResponse {
  month: string
  days: WorkDay[]
  summaries: DaySummary[]
  flex: FlexPoolResult
}

type ViewMode = 'day' | 'week' | 'month'

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

// Count Mon-Fri days in month up to today (or end of month if past)
function countWorkdaysUpToNow(monthStr: string): number {
  const year = parseInt(monthStr.split('-')[0])
  const month = parseInt(monthStr.split('-')[1]) - 1
  const numDays = new Date(year, month + 1, 0).getDate()
  const today = new Date()
  let count = 0
  for (let d = 1; d <= numDays; d++) {
    const date = new Date(year, month, d)
    if (date > today) break
    const dow = date.getDay()
    if (dow !== 0 && dow !== 6) count++
  }
  return count
}

// Returns Tailwind classes for a calendar cell based on reported hours
function dayCellClass(summary: DaySummary | undefined, isWeekend: boolean): string {
  if (isWeekend) return 'bg-gray-900/30 text-gray-600'
  if (!summary || summary.reported_minutes === 0) return 'bg-gray-800/40 text-gray-400'
  if (summary.reported_minutes > summary.standard_minutes) return 'bg-blue-900/60 text-blue-200'
  if (summary.reported_minutes === summary.standard_minutes) return 'bg-green-900/60 text-green-200'
  return 'bg-amber-900/60 text-amber-200'
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
          onClick={() => setActiveTab('day')}
          className={`flex-1 py-1.5 text-sm font-medium rounded transition-colors cursor-pointer ${
            activeTab === 'day' ? 'bg-gray-700 text-white' : 'text-gray-400 hover:text-gray-200'
          }`}
        >
          {t('workhours:viewDay')}
        </button>
        <button
          onClick={() => setActiveTab('week')}
          className={`flex-1 py-1.5 text-sm font-medium rounded transition-colors cursor-pointer ${
            activeTab === 'week' ? 'bg-gray-700 text-white' : 'text-gray-400 hover:text-gray-200'
          }`}
        >
          {t('workhours:viewWeek')}
        </button>
        <button
          onClick={() => setActiveTab('month')}
          className={`flex-1 py-1.5 text-sm font-medium rounded transition-colors cursor-pointer ${
            activeTab === 'month' ? 'bg-gray-700 text-white' : 'text-gray-400 hover:text-gray-200'
          }`}
        >
          {t('workhours:viewMonth')}
        </button>
      </div>

      {activeTab === 'day' && (
        <DayView currentDate={currentDate} setCurrentDate={setCurrentDate} />
      )}
      {activeTab === 'week' && (
        <WeekView initialDate={currentDate} onSelectDay={handleSelectDay} />
      )}
      {activeTab === 'month' && (
        <MonthView initialMonth={dateToMonthStr(currentDate)} onSelectDay={handleSelectDay} />
      )}
    </div>
  )
}

// ── Day view ───────────────────────────────────────────────────────────────

function DayView({
  currentDate,
  setCurrentDate,
}: {
  currentDate: string
  setCurrentDate: (d: string) => void
}) {
  const { t } = useTranslation(['workhours', 'common'])

  const currentDateRef = useRef(currentDate)
  const [dayData, setDayData] = useState<{ day: WorkDay | null; summary: DaySummary | null } | null>(null)
  const [presets, setPresets] = useState<WorkDeductionPreset[]>([])
  const [flex, setFlex] = useState<{ flex: FlexPoolResult; reset_date: string; days_in_pool: number } | null>(null)
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [newStart, setNewStart] = useState('')
  const [newEnd, setNewEnd] = useState('')
  const [newDeductionName, setNewDeductionName] = useState('')
  const [newDeductionMinutes, setNewDeductionMinutes] = useState('')

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

  useEffect(() => {
    const controller = new AbortController()
    // eslint-disable-next-line react-hooks/set-state-in-effect
    loadDay(currentDate, controller.signal)
    return () => controller.abort()
  }, [currentDate, loadDay])

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
        }),
      })
      if (r.ok) {
        setNewStart('')
        setNewEnd('')
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
      await fetch(`/api/workhours/day/session/${sessionID}`, {
        method: 'DELETE',
        credentials: 'include',
      })
      await loadDay(currentDate)
      loadFlex()
    } finally {
      setSaving(false)
    }
  }

  const handlePresetToggle = async (preset: WorkDeductionPreset) => {
    const existing = dayData?.day?.deductions.find(d => d.preset_id === preset.id)
    setSaving(true)
    try {
      if (existing) {
        await fetch(`/api/workhours/day/deduction/${existing.id}`, {
          method: 'DELETE',
          credentials: 'include',
        })
        await loadDay(currentDate)
        loadFlex()
      } else {
        let day = dayData?.day ?? null
        if (!day) {
          day = await ensureDay()
          if (!day) return
        }
        await fetch('/api/workhours/day/deduction', {
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
        await loadDay(currentDate)
        loadFlex()
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
      await fetch(`/api/workhours/day/deduction/${deductionID}`, {
        method: 'DELETE',
        credentials: 'include',
      })
      await loadDay(currentDate)
      loadFlex()
    } finally {
      setSaving(false)
    }
  }

  const day = dayData?.day ?? null
  const summary = dayData?.summary ?? null
  const lunchChecked = day?.lunch ?? false
  const sessions = day?.sessions ?? []
  const deductions = day?.deductions ?? []

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
          onClick={() => setCurrentDate(prev => prevWeekday(prev))}
          className="p-1 text-gray-400 hover:text-white transition-colors cursor-pointer"
          aria-label={t('workhours:prevDay')}
        >
          <ChevronLeft size={20} />
        </button>
        <span className="text-sm font-medium text-white capitalize">{dateLabel}</span>
        <button
          onClick={() => setCurrentDate(prev => nextWeekday(prev))}
          className="p-1 text-gray-400 hover:text-white transition-colors cursor-pointer"
          aria-label={t('workhours:nextDay')}
        >
          <ChevronRight size={20} />
        </button>
      </div>

      {loading ? (
        <p className="text-sm text-gray-400">{t('common:status.loading')}…</p>
      ) : (
        <>
          {/* Sessions */}
          <section className="space-y-3">
            <h2 className="text-xs font-semibold text-gray-400 uppercase tracking-wide">
              {t('workhours:sessions')}
            </h2>

            {sessions.length > 0 && (
              <div className="space-y-2">
                {sessions.map(s => {
                  const [sh, sm] = s.start_time.split(':').map(Number)
                  const [eh, em] = s.end_time.split(':').map(Number)
                  const mins = eh * 60 + em - (sh * 60 + sm)
                  return (
                    <div key={s.id} className="flex items-center gap-3 bg-gray-800 rounded-lg px-3 py-2">
                      <span className="text-white font-mono text-sm">{s.start_time}</span>
                      <span className="text-gray-500 text-xs">→</span>
                      <span className="text-white font-mono text-sm">{s.end_time}</span>
                      <span className="text-gray-400 text-xs ml-auto">{formatMins(mins)}</span>
                      <button
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
              <input
                type="time"
                value={newStart}
                onChange={e => setNewStart(e.target.value)}
                className="bg-gray-800 text-white rounded px-2 py-1.5 text-sm font-mono w-28 border border-gray-700 focus:border-blue-500 focus:outline-none"
                aria-label={t('workhours:startTime')}
              />
              <span className="text-gray-500 text-xs">→</span>
              <input
                type="time"
                value={newEnd}
                onChange={e => setNewEnd(e.target.value)}
                className="bg-gray-800 text-white rounded px-2 py-1.5 text-sm font-mono w-28 border border-gray-700 focus:border-blue-500 focus:outline-none"
                aria-label={t('workhours:endTime')}
              />
              <button
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
                      onClick={() => handlePresetToggle(p)}
                      disabled={saving}
                      className={`px-3 py-1 rounded-full text-xs font-medium transition-colors cursor-pointer disabled:opacity-40 ${
                        active
                          ? 'bg-blue-600 text-white'
                          : 'bg-gray-700 text-gray-300 hover:bg-gray-600'
                      }`}
                    >
                      {p.name} ({p.default_minutes}{t('workhours:minutesShort')})
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
                onClick={handleAddDeduction}
                disabled={!newDeductionName.trim() || !newDeductionMinutes || saving}
                className="flex items-center gap-1 px-3 py-1.5 bg-gray-700 hover:bg-gray-600 disabled:opacity-40 disabled:cursor-not-allowed text-white text-sm rounded transition-colors cursor-pointer"
                aria-label={t('workhours:addDeduction')}
              >
                <Plus size={14} />
              </button>
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
  initialDate,
  onSelectDay,
}: {
  initialDate: string
  onSelectDay: (date: string) => void
}) {
  const { t } = useTranslation(['workhours', 'common'])
  const [weekDate, setWeekDate] = useState(initialDate)
  const [data, setData] = useState<WeekSummaryResponse | null>(null)
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    setLoading(true)
    fetch(`/api/workhours/summary/week?date=${encodeURIComponent(weekDate)}`, { credentials: 'include' })
      .then(r => (r.ok ? r.json() : null))
      .then((d: WeekSummaryResponse | null) => setData(d))
      .catch(() => setData(null))
      .finally(() => setLoading(false))
  }, [weekDate])

  const summaryMap = new Map<string, DaySummary>()
  const dayMap = new Map<string, WorkDay>()
  if (data) {
    data.summaries.forEach(s => summaryMap.set(s.date, s))
    data.days.forEach(d => dayMap.set(d.date, d))
  }

  // The week_start from the API is the Monday of the week
  const weekStart = data?.week_start ?? weekDate

  // Build 5 weekday rows
  const rows = weekDays(weekStart)

  // Weekly totals
  let totalNet = 0
  let totalReported = 0
  let totalBalance = 0
  summaryMap.forEach(s => {
    totalNet += s.net_minutes
    totalReported += s.reported_minutes
    totalBalance += s.balance_minutes
  })

  const weekLabel = data
    ? (() => {
        const start = new Date(data.week_start + 'T12:00:00')
        // Friday is 4 days after Monday
        const friday = new Date(data.week_start + 'T12:00:00')
        friday.setDate(friday.getDate() + 4)
        const fmtShort = new Intl.DateTimeFormat(undefined, { day: 'numeric', month: 'short' })
        const fmtYear = new Intl.DateTimeFormat(undefined, { year: 'numeric' })
        return `${fmtShort.format(start)} – ${fmtShort.format(friday)}, ${fmtYear.format(start)}`
      })()
    : formatDate(weekDate + 'T12:00:00', { day: 'numeric', month: 'short', year: 'numeric' })

  return (
    <div className="space-y-4">
      {/* Week navigation */}
      <div className="flex items-center justify-between bg-gray-800 rounded-lg p-3">
        <button
          onClick={() => setWeekDate(d => addWeeks(d, -1))}
          className="p-1 text-gray-400 hover:text-white transition-colors cursor-pointer"
          aria-label={t('workhours:prevWeek')}
        >
          <ChevronLeft size={20} />
        </button>
        <span className="text-sm font-medium text-white">{weekLabel}</span>
        <button
          onClick={() => setWeekDate(d => addWeeks(d, 1))}
          className="p-1 text-gray-400 hover:text-white transition-colors cursor-pointer"
          aria-label={t('workhours:nextWeek')}
        >
          <ChevronRight size={20} />
        </button>
      </div>

      {loading ? (
        <p className="text-sm text-gray-400">{t('common:status.loading')}…</p>
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
                  const d = new Date(dateStr + 'T12:00:00')
                  const dayLabel = new Intl.DateTimeFormat(undefined, {
                    weekday: 'short',
                    day: 'numeric',
                    month: 'short',
                  }).format(d)
                  const balance = summary?.balance_minutes ?? null

                  return (
                    <tr
                      key={dateStr}
                      onClick={() => onSelectDay(dateStr)}
                      className="border-b border-gray-800 hover:bg-gray-800/60 cursor-pointer transition-colors"
                    >
                      <td className="py-2.5 pr-3 text-gray-300 capitalize">{dayLabel}</td>
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
  initialMonth,
  onSelectDay,
}: {
  initialMonth: string
  onSelectDay: (date: string) => void
}) {
  const { t } = useTranslation(['workhours', 'common'])
  const [monthStr, setMonthStr] = useState(initialMonth)
  const [data, setData] = useState<MonthSummaryResponse | null>(null)
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    setLoading(true)
    fetch(`/api/workhours/summary/month?month=${encodeURIComponent(monthStr)}`, { credentials: 'include' })
      .then(r => (r.ok ? r.json() : null))
      .then((d: MonthSummaryResponse | null) => setData(d))
      .catch(() => setData(null))
      .finally(() => setLoading(false))
  }, [monthStr])

  const summaryMap = new Map<string, DaySummary>()
  if (data) {
    data.summaries.forEach(s => summaryMap.set(s.date, s))
  }

  const monthLabel = new Intl.DateTimeFormat(undefined, { month: 'long', year: 'numeric' }).format(
    new Date(monthStr + '-01T12:00:00')
  )

  // Day-of-week header labels (Mon–Sun)
  const dowHeaders = Array.from({ length: 7 }, (_, i) => {
    const d = new Date(2024, 0, 1 + i) // Jan 1 2024 is a Monday
    return new Intl.DateTimeFormat(undefined, { weekday: 'short' }).format(d)
  })

  const grid = buildMonthGrid(monthStr)

  // Monthly totals
  const standard = data?.summaries[0]?.standard_minutes ?? 450
  const totalWorked = data ? data.summaries.reduce((sum, s) => sum + s.reported_minutes, 0) : 0
  const workdaysTarget = countWorkdaysUpToNow(monthStr)
  const totalTarget = workdaysTarget * standard
  const totalBalance = totalWorked - totalTarget

  const today = localDateStr(new Date())

  return (
    <div className="space-y-4">
      {/* Month navigation */}
      <div className="flex items-center justify-between bg-gray-800 rounded-lg p-3">
        <button
          onClick={() => setMonthStr(m => addMonths(m, -1))}
          className="p-1 text-gray-400 hover:text-white transition-colors cursor-pointer"
          aria-label={t('workhours:prevMonth')}
        >
          <ChevronLeft size={20} />
        </button>
        <span className="text-sm font-medium text-white capitalize">{monthLabel}</span>
        <button
          onClick={() => setMonthStr(m => addMonths(m, 1))}
          className="p-1 text-gray-400 hover:text-white transition-colors cursor-pointer"
          aria-label={t('workhours:nextMonth')}
        >
          <ChevronRight size={20} />
        </button>
      </div>

      {loading ? (
        <p className="text-sm text-gray-400">{t('common:status.loading')}…</p>
      ) : (
        <>
          {/* Calendar grid */}
          <div>
            {/* Day-of-week headers */}
            <div className="grid grid-cols-7 gap-1 mb-1">
              {dowHeaders.map((h, i) => (
                <div
                  key={i}
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
                <div key={wi} className="grid grid-cols-7 gap-1">
                  {week.map((dateStr, di) => {
                    if (!dateStr) {
                      return <div key={di} className="aspect-square" />
                    }
                    const summary = summaryMap.get(dateStr)
                    const isWeekend = di >= 5
                    const isToday = dateStr === today
                    const dayNum = parseInt(dateStr.split('-')[2])
                    const cellClass = dayCellClass(summary, isWeekend)

                    return (
                      <button
                        key={di}
                        onClick={() => !isWeekend && onSelectDay(dateStr)}
                        disabled={isWeekend}
                        className={`aspect-square rounded flex flex-col items-center justify-center text-xs transition-colors ${cellClass} ${
                          isWeekend ? 'cursor-default' : 'hover:ring-1 hover:ring-gray-500 cursor-pointer'
                        } ${isToday ? 'ring-1 ring-blue-500' : ''}`}
                        aria-label={dateStr}
                      >
                        <span className="font-medium leading-none">{dayNum}</span>
                        {summary && summary.reported_minutes > 0 && (
                          <span className="text-[0.6rem] leading-tight mt-0.5 opacity-80">
                            {formatHours(summary.reported_minutes)}
                          </span>
                        )}
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
  let cum = 0
  const points = dataPoints.map(s => {
    cum += s.remainder_minutes
    return cum
  })

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
        {t('workhours:flexTrend')}
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
            <circle key={i} cx={toX(i).toFixed(1)} cy={toY(v).toFixed(1)} r="3" fill={lineColor} />
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
