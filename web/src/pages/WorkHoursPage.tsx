import { useState, useEffect, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { ChevronLeft, ChevronRight, Plus, Trash2 } from 'lucide-react'

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

function formatMins(mins: number): string {
  const abs = Math.abs(mins)
  const h = Math.floor(abs / 60)
  const m = abs % 60
  const prefix = mins < 0 ? '-' : ''
  return `${prefix}${h}:${m.toString().padStart(2, '0')}`
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

export default function WorkHoursPage() {
  const { t, i18n } = useTranslation(['workhours', 'common'])

  const [currentDate, setCurrentDate] = useState(getInitialDate)
  const [dayData, setDayData] = useState<{ day: WorkDay | null; summary: DaySummary | null } | null>(null)
  const [presets, setPresets] = useState<WorkDeductionPreset[]>([])
  const [flex, setFlex] = useState<{ flex: FlexPoolResult; reset_date: string; days_in_pool: number } | null>(null)
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [newStart, setNewStart] = useState('')
  const [newEnd, setNewEnd] = useState('')
  const [newDeductionName, setNewDeductionName] = useState('')
  const [newDeductionMinutes, setNewDeductionMinutes] = useState('')

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

  const loadDay = useCallback(async (date: string) => {
    await Promise.resolve()
    setLoading(true)
    try {
      const r = await fetch(`/api/workhours/day?date=${date}`, { credentials: 'include' })
      if (r.ok) {
        const data: { day: WorkDay | null; summary: DaySummary | null } = await r.json()
        setDayData(data)
      }
    } catch {
      // ignore network errors
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    loadDay(currentDate)
  }, [currentDate, loadDay])

  const ensureDay = async (lunch?: boolean): Promise<WorkDay | null> => {
    const body = {
      date: currentDate,
      lunch: lunch !== undefined ? lunch : (dayData?.day?.lunch ?? false),
      notes: dayData?.day?.notes ?? '',
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

  const dateLabel = new Intl.DateTimeFormat(i18n.language, {
    weekday: 'long',
    year: 'numeric',
    month: 'long',
    day: 'numeric',
  }).format(new Date(currentDate + 'T12:00:00'))

  return (
    <div className="max-w-2xl mx-auto p-4 space-y-6">
      <h1 className="text-xl font-semibold text-white">{t('workhours:title')}</h1>

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
                <span className="text-white font-mono text-right">{summary.reported_hours.toFixed(1)}h</span>

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
