import { useState, useEffect, useCallback, useMemo, useRef, type RefObject } from 'react'
import { useTranslation } from 'react-i18next'
import { Building2, Calendar, ChevronLeft, ChevronRight, Clock, Copy, Plus, Trash2 } from 'lucide-react'
import { formatDate } from '../../../utils/formatDate'
import { Skeleton } from '../../../components/ui/skeleton'
import { Select, type SelectOption } from '../../../components/ui/select'
import { TimePicker } from '../../../components/ui/time-picker'
import { calculateDayWithLivePunch, type WorkSession, type WorkSettings } from '../../workHoursUtils'
import type { DaySummary, FlexState, LeaveDay, LeaveType, WorkDay, WorkDeductionPreset } from '../types'
import {
  buildNavHolidaySet,
  currentTimeHHMM,
  formatMins,
  getNorwegianHolidays,
  localDateStr,
  nextWeekday,
  prevWeekday,
} from '../dateUtils'
import { normalizePresetIcon } from '../presetIcons'
import { useWorkHoursApi } from '../useWorkHoursApi'

export default function DayView({
  currentDate,
  setCurrentDate,
  onNavigateToSettings,
  punchToggleRef,
}: {
  currentDate: string
  setCurrentDate: (d: string | ((prev: string) => string)) => void
  onNavigateToSettings: () => void
  punchToggleRef?: RefObject<(() => void) | null>
}) {
  const { t } = useTranslation(['workhours', 'common'])
  const api = useWorkHoursApi()

  const navYear = parseInt(currentDate.split('-')[0])
  const navHolidaySet = useMemo(() => buildNavHolidaySet(navYear), [navYear])

  const currentDateRef = useRef(currentDate)
  const leaveDaysCacheRef = useRef<Map<string, LeaveDay[]>>(new Map())
  const datePickerRef = useRef<HTMLInputElement>(null)
  const punchEditAbortRef = useRef<AbortController | null>(null)
  const [dayData, setDayData] = useState<{ day: WorkDay | null; summary: DaySummary | null } | null>(null)
  const [presets, setPresets] = useState<WorkDeductionPreset[]>([])
  const [flex, setFlex] = useState<FlexState | null>(null)
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
  const [redeemingFlex, setRedeemingFlex] = useState(false)
  const [selectedPresetId, setSelectedPresetId] = useState<number | null>(null)
  const [workSettings, setWorkSettings] = useState<WorkSettings>({ standard_day_minutes: 450, lunch_minutes: 30, rounding_minutes: 30 })
  const [now, setNow] = useState(() => new Date())
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
    api.getFlex()
      .then(data => setFlex(data))
      .catch(() => {})
  }, [api])

  const loadDay = useCallback(async (date: string, signal?: AbortSignal) => {
    setLoading(true)
    try {
      const data = await api.getDay(date, signal)
      if (signal?.aborted || currentDateRef.current !== date) return
      if (data) {
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
  }, [api])

  const handleFlexRedeem = useCallback(async () => {
    setRedeemingFlex(true)
    try {
      const res = await api.redeemFlex(currentDateRef.current)
      if (res.ok) {
        loadFlex()
        loadDay(currentDateRef.current)
      } else {
        alert(res.error ?? t('workhours:redeemFlexError'))
      }
    } catch {
      alert(t('workhours:redeemFlexError'))
    } finally {
      setRedeemingFlex(false)
    }
  }, [api, loadFlex, loadDay, t])

  useEffect(() => {
    api.getPresets()
      .then(setPresets)
      .catch(() => {})
    loadFlex()
    // Restore any in-progress punch-in from the server.
    api.getPunchSession()
      .then(session => {
        if (session) {
          const sessionDate = session.date
          if (sessionDate && sessionDate !== currentDateRef.current) {
            setCurrentDate(sessionDate)
          }
          setPunchStart(session.start_time)
          setNewStart(session.start_time)
        }
      })
      .catch(() => {})
    api.getPreferences()
      .then(prefs => {
        if (prefs) {
          const parsePref = (val: string | undefined, fallback: number, requirePositive = false): number => {
            const n = parseInt(val ?? '', 10)
            if (!Number.isFinite(n)) return fallback
            if (requirePositive && n <= 0) return fallback
            return n
          }
          setWorkSettings({
            standard_day_minutes: parsePref(prefs.work_hours_standard_day, 450, true),
            lunch_minutes: parsePref(prefs.work_hours_lunch_minutes, 30),
            rounding_minutes: parsePref(prefs.work_hours_rounding, 30, true),
          })
        }
      })
      .catch(() => {})
  }, [api, loadFlex, setCurrentDate])

  useEffect(() => {
    if (punchStart === null) return
    const id = setInterval(() => setNow(new Date()), 60_000)
    return () => clearInterval(id)
  }, [punchStart])

  useEffect(() => {
    if (!punchToggleRef) return
    punchToggleRef.current = () => {
      if (punchStart === null) {
        handlePunchIn()
      } else {
        handlePunchOut()
      }
    }
    return () => { punchToggleRef.current = null }
  })

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
      const data = await api.getLeaveForYear(year, signal)
      if (signal?.aborted || currentDateRef.current !== date) return
      if (data) {
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
  }, [api])

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
      const data = await api.saveDay(body)
      if (!data) return null
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
      const ok = await api.addSession({
        day_id: day.id,
        start_time: newStart,
        end_time: newEnd,
        sort_order: sortOrder,
        is_internal: newIsInternal,
      })
      if (ok) {
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
      const ok = await api.deleteSession(sessionID)
      if (ok) {
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
      const ok = await api.updateSession(session.id, {
        start_time: session.start_time,
        end_time: session.end_time,
        sort_order: session.sort_order,
        is_internal: !session.is_internal,
      })
      if (!ok) {
        console.error('Failed to toggle internal flag')
        return
      }
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
        const ok = await api.deleteDeduction(existing.id)
        if (ok) {
          await loadDay(currentDate)
          loadFlex()
        }
      } else {
        let day = dayData?.day ?? null
        if (!day) {
          day = await ensureDay()
          if (!day) return
        }
        const ok = await api.addDeduction({
          day_id: day.id,
          name: preset.name,
          minutes: preset.default_minutes,
          preset_id: preset.id,
        })
        if (ok) {
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
      const ok = await api.addDeduction({ day_id: day.id, name, minutes })
      if (ok) {
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
      const ok = await api.deleteDeduction(deductionID)
      if (ok) {
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
      const ok = await api.punchIn({ date: currentDate, start_time: startTime })
      if (ok) {
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
      const ok = await api.cancelPunch()
      if (ok) {
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
    // Abort any in-flight start-time edit so its revert path can't resurrect punchStart.
    punchEditAbortRef.current?.abort()
    punchEditAbortRef.current = null
    const endTime = currentTimeHHMM()
    if (endTime <= punchStart) {
      alert(t('workhours:punchMidnightError'))
      return
    }
    setSaving(true)
    try {
      const data = await api.punchOut({ end_time: endTime })
      if (data) {
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

  const handleEditPunchStart = async (newTime: string) => {
    if (!newTime || !punchStart || newTime === punchStart) return
    const prev: string = punchStart
    // Abort any previous in-flight edit and start a new one.
    punchEditAbortRef.current?.abort()
    const controller = new AbortController()
    punchEditAbortRef.current = controller
    setPunchStart(newTime)
    setNewStart(newTime)
    try {
      const ok = await api.editPunchStart(newTime, controller.signal)
      if (!ok) {
        console.error('workhours: edit punch start failed')
        setPunchStart(prev)
        setNewStart(prev)
      }
    } catch (err) {
      if (err instanceof Error && err.name === 'AbortError') return
      console.error('workhours: edit punch start:', err)
      setPunchStart(prev)
      setNewStart(prev)
    }
  }

  const handleCopyYesterday = async () => {
    if (sessions.length > 0) return
    const yesterday = prevWeekday(currentDate, navHolidaySet)
    setSaving(true)
    try {
      const data = await api.getDay(yesterday)
      if (!data?.day?.sessions?.length) return

      let d = dayData?.day ?? null
      if (!d) {
        d = await ensureDay()
        if (!d) return
      }

      for (const session of data.day.sessions) {
        const ok = await api.addSession({
          day_id: d.id,
          start_time: session.start_time,
          end_time: session.end_time,
          sort_order: (d.sessions?.length ?? 0),
          is_internal: session.is_internal,
        })
        if (!ok) {
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
        const ok = await api.removeLeave(targetDate)
        if (ok && currentDateRef.current === targetDate) {
          leaveDaysCacheRef.current.delete(targetDate.slice(0, 4))
          setLeaveDay(null)
        }
      } else {
        const ld = await api.setLeave(targetDate, leaveType, '')
        if (ld && currentDateRef.current === targetDate) {
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
          onClick={() => setCurrentDate(prev => prevWeekday(prev, navHolidaySet))}
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
              const pickedYear = d.getFullYear()
              const pickedHolidaySet = buildNavHolidaySet(pickedYear)
              while (d.getDay() === 0 || d.getDay() === 6 || pickedHolidaySet.has(localDateStr(d))) {
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
          onClick={() => setCurrentDate(prev => nextWeekday(prev, navHolidaySet))}
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
                    <div className="flex items-center gap-1 text-xs">
                      <label htmlFor="punch-start-time" className="text-gray-400">{t('workhours:punchStartLabel')}</label>
                      <input
                        id="punch-start-time"
                        key={punchStart}
                        type="time"
                        defaultValue={punchStart}
                        onBlur={e => handleEditPunchStart(e.target.value)}
                        onKeyDown={e => {
                          if (e.key === 'Enter') {
                            e.currentTarget.blur()
                          }
                        }}
                        className="bg-gray-800 border border-gray-600 rounded px-1 py-0.5 text-xs text-white font-mono w-[5rem] focus:border-blue-500 focus:outline-none"
                      />
                    </div>
                    <button
                      type="button"
                      onClick={handlePunchOut}
                      disabled={saving}
                      className="flex items-center gap-1 px-2 py-1 text-xs text-red-400 hover:text-red-300 bg-gray-800 hover:bg-gray-700 rounded transition-colors cursor-pointer disabled:opacity-40 animate-pulse"
                      aria-label={t('workhours:punchOut')}
                    >
                      <Clock size={12} />
                      {t('workhours:punchOut')}
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

            {punchStart !== null && (() => {
              const estimate = calculateDayWithLivePunch(now, punchStart, sessions, lunchChecked, deductions, workSettings)
              if (!estimate) {
                return (
                  <div className="rounded-lg border border-yellow-700/50 bg-yellow-900/20 px-3 py-2 text-xs text-yellow-400">
                    {t('workhours:punchEstimate.invalidStart')}
                  </div>
                )
              }
              const overStandard = estimate.reportedMinutes > estimate.standardMinutes
              const atStandard = estimate.reportedMinutes >= estimate.standardMinutes
              return (
                <section
                  className={`rounded-lg border px-3 py-2.5 text-sm ${atStandard ? 'border-green-700/50 bg-green-900/20' : 'border-gray-700 bg-gray-800/50'}`}
                >
                  <h3 className="text-xs font-semibold text-gray-400 uppercase tracking-wide mb-2">
                    {t('workhours:punchEstimate.title')}
                  </h3>
                  <div className="flex flex-wrap gap-x-4 gap-y-1">
                    <div>
                      <span className="text-gray-400 text-xs">{t('workhours:punchEstimate.net')}</span>{' '}
                      <span className="text-white font-mono text-sm">{formatMins(estimate.netMinutes)}</span>
                    </div>
                    <div>
                      <span className="text-gray-400 text-xs">{t('workhours:punchEstimate.reported')}</span>{' '}
                      <span className="text-white font-mono text-sm">{formatMins(estimate.reportedMinutes)}</span>
                    </div>
                  </div>
                  <div className="flex flex-wrap gap-x-3 gap-y-0.5 mt-1 text-xs text-gray-500">
                    <span>{t('workhours:punchEstimate.gross')}: <span className="font-mono">{formatMins(estimate.grossMinutes)}</span></span>
                    {estimate.lunchMinutes > 0 && (
                      <span>{t('workhours:punchEstimate.lunch')}: <span className="font-mono">{formatMins(estimate.lunchMinutes)}</span></span>
                    )}
                    {estimate.deductionMinutes > 0 && (
                      <span>{t('workhours:punchEstimate.deductions')}: <span className="font-mono">{formatMins(estimate.deductionMinutes)}</span></span>
                    )}
                  </div>
                  {overStandard && (
                    <div className="mt-1 text-xs text-green-400">
                      {t('workhours:punchEstimate.overStandard', { amount: formatMins(estimate.reportedMinutes - estimate.standardMinutes) })}
                    </div>
                  )}
                  {atStandard && !overStandard && (
                    <div className="mt-1 text-xs text-green-400">
                      {t('workhours:punchEstimate.atStandard')}
                    </div>
                  )}
                </section>
              )
            })()}

            {sessions.length > 0 && (
              <div className="space-y-2">
                {sessions.map(s => {
                  const [sh, sm] = s.start_time.split(':').map(Number)
                  const [eh, em] = s.end_time.split(':').map(Number)
                  const mins = eh * 60 + em - (sh * 60 + sm)
                  return (
                    <div key={s.id} className={`flex items-center gap-3 rounded-lg border px-3 py-2 ${s.is_internal ? 'bg-purple-900/40 border-purple-700/40' : 'bg-gray-800 border-transparent'}`}>
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
              <div className="flex items-center gap-2">
                {flex.flex.total_minutes >= (flex.rounding_minutes ?? 30) && (
                  <button
                    onClick={handleFlexRedeem}
                    disabled={redeemingFlex}
                    className="text-xs px-2 py-1 rounded bg-blue-600 hover:bg-blue-500 text-white disabled:opacity-50"
                  >
                    {t('workhours:redeemFlex', { minutes: flex.rounding_minutes ?? 30 })}
                  </button>
                )}
                <span
                  className={`font-mono text-sm font-semibold ${flex.flex.total_minutes < 0 ? 'text-red-400' : 'text-green-400'}`}
                >
                  {flex.flex.total_minutes > 0 ? '+' : ''}
                  {formatMins(flex.flex.total_minutes)}
                </span>
              </div>
            </section>
          )}
        </>
      )}
    </div>
  )
}
