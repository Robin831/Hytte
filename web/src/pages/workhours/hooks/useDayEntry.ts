import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { findSessionOverlaps } from '../../workHoursUtils'
import {
  applyRedemption,
  calculateDay,
  DEFAULT_WORK_SETTINGS,
  parseWorkSettings,
  type DaySummaryCalc,
} from '../calculateDay'
import type { DaySummary, LeaveDay, LeaveType, WorkDay, WorkDeductionPreset, WorkSession, WorkSettings } from '../types'
import { buildNavHolidaySet, prevWeekday } from '../dateUtils'
import { SessionConflict, useWorkHoursApi, type PunchOutResult } from '../useWorkHoursApi'

export type ConflictCause = 'save' | 'toggle' | 'copy'

/**
 * Overlap conflict from the last rejected save, shown inline next to the form
 * it came from: 'add' for the add-session row, or the id of the edited session.
 * `cause` picks the message: a rejected save of typed times, an internal
 * toggle on a stored overlap, or a copy-yesterday that stopped part way.
 */
export interface ActiveSessionConflict {
  target: 'add' | number
  cause: ConflictCause
  conflict: WorkSession
}

/** The add-session row. */
export interface SessionDraft {
  start: string
  end: string
  isInternal: boolean
  crossesMidnight: boolean
}

/** The inline editor of an existing session. */
export interface SessionEdit {
  start: string
  end: string
  crossesMidnight: boolean
}

/** The add-custom-deduction row. */
export interface DeductionDraft {
  name: string
  minutes: string
}

const EMPTY_SESSION_DRAFT: SessionDraft = { start: '', end: '', isInternal: false, crossesMidnight: false }
const RECENT_PRESETS_KEY = 'workhours_recent_presets'

export interface UseDayEntryOptions {
  /** The day being viewed (YYYY-MM-DD). */
  date: string
  /** Start of the open punch-in, which the add-session row is prefilled from. */
  openPunchStart: string | null
  /** The latest punch-out response from the page's punch clock; applied when it changes. */
  lastPunchOut: PunchOutResult | null
  /** Called after anything that may change the flex pool, so it can be reloaded. */
  onFlexChanged: () => void
}

/**
 * One work day's entry: loading the day for `date`, session and deduction
 * CRUD, presets, the lunch toggle, leave and copy-yesterday, plus the form
 * drafts those edits are typed into. `summary` is the saved day computed with
 * the shared calculateDay mirror of the Go calculator.
 */
export function useDayEntry({ date, openPunchStart, lastPunchOut, onFlexChanged }: UseDayEntryOptions) {
  const { t } = useTranslation(['workhours', 'common'])
  const api = useWorkHoursApi()

  const navYear = parseInt(date.split('-')[0])
  const navHolidaySet = useMemo(() => buildNavHolidaySet(navYear), [navYear])

  const currentDateRef = useRef(date)
  const leaveDaysCacheRef = useRef<Map<string, LeaveDay[]>>(new Map())
  const onFlexChangedRef = useRef(onFlexChanged)
  const [dayData, setDayData] = useState<{ day: WorkDay | null; summary: DaySummary | null } | null>(null)
  const [presets, setPresets] = useState<WorkDeductionPreset[]>([])
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [draft, setDraft] = useState<SessionDraft>(EMPTY_SESSION_DRAFT)
  // Session ids are per day, so the open editor is tagged with the day it was
  // opened on and derived away on a day change instead of being reset in an effect.
  const [editSession, setEditSession] = useState<{ date: string; id: number } | null>(null)
  const [edit, setEdit] = useState<SessionEdit>({ start: '', end: '', crossesMidnight: false })
  // Tagged with its day like editSession so it disappears on a day change.
  const [sessionConflict, setSessionConflict] = useState<(ActiveSessionConflict & { date: string }) | null>(null)
  const [deductionDraft, setDeductionDraft] = useState<DeductionDraft>({ name: '', minutes: '' })
  const [leaveDay, setLeaveDay] = useState<LeaveDay | null>(null)
  const [leaveSaving, setLeaveSaving] = useState(false)
  const [selectedPresetId, setSelectedPresetId] = useState<number | null>(null)
  const [workSettings, setWorkSettings] = useState<WorkSettings>(DEFAULT_WORK_SETTINGS)
  // Until the user's own settings arrive the saved totals come straight from
  // the server, which already applied them; see summary below.
  const [workSettingsLoaded, setWorkSettingsLoaded] = useState(false)
  const [recentlyUsed, setRecentlyUsed] = useState<number[]>(() => {
    try {
      const stored = localStorage.getItem(RECENT_PRESETS_KEY)
      if (!stored) return []
      const parsed: unknown = JSON.parse(stored)
      if (!Array.isArray(parsed)) return []
      return parsed.filter((v): v is number => typeof v === 'number' && isFinite(v))
    } catch {
      return []
    }
  })

  // The add-session form starts from the open punch's start time and follows
  // edits to it; a new punch-in or a closed punch also clears the end time.
  const [prefilledPunchStart, setPrefilledPunchStart] = useState<string | null>(null)
  if (openPunchStart !== prefilledPunchStart) {
    setPrefilledPunchStart(openPunchStart)
    const clearEnd = prefilledPunchStart === null || openPunchStart === null
    setDraft(d => ({ ...d, start: openPunchStart ?? '', end: clearEnd ? '' : d.end }))
  }

  const editSessionId = editSession && editSession.date === date ? editSession.id : null
  const activeConflict: ActiveSessionConflict | null = sessionConflict && sessionConflict.date === date ? sessionConflict : null
  // Records a 409 overlap from a session save; anything else is rethrown.
  const reportConflict = (err: unknown, target: 'add' | number, cause: ConflictCause) => {
    if (!(err instanceof SessionConflict)) throw err
    setSessionConflict({ date, target, cause, conflict: err.conflict })
  }
  const setEditSessionId = useCallback((id: number | null) => {
    setEditSession(id === null ? null : { date, id })
  }, [date])

  useEffect(() => {
    currentDateRef.current = date
  }, [date])

  useEffect(() => {
    onFlexChangedRef.current = onFlexChanged
  })

  const loadFlex = useCallback(() => onFlexChangedRef.current(), [])

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

  /** Reloads whichever day is being viewed now. */
  const reload = useCallback(() => loadDay(currentDateRef.current), [loadDay])

  useEffect(() => {
    api.getPresets()
      .then(setPresets)
      .catch(() => {})
    api.getPreferences()
      .then(prefs => {
        if (prefs) {
          setWorkSettings(parseWorkSettings(prefs))
          setWorkSettingsLoaded(true)
        }
      })
      .catch(() => {})
  }, [api])

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
    loadDay(date, controller.signal)
    loadLeaveDay(date, controller.signal)
    return () => controller.abort()
  }, [date, loadDay, loadLeaveDay])

  // A punch-out saved a session; show the saved day and reload flex. When it
  // landed on another day the clock has already moved the view there, and
  // the ref effect above has caught up by the time this runs.
  const seenPunchOutRef = useRef(lastPunchOut)
  useEffect(() => {
    if (!lastPunchOut || seenPunchOutRef.current === lastPunchOut) return
    seenPunchOutRef.current = lastPunchOut
    if (lastPunchOut.date === currentDateRef.current) {
      setDayData({ day: lastPunchOut.day, summary: lastPunchOut.summary })
    }
    loadFlex()
  }, [lastPunchOut, loadFlex])

  const ensureDay = async (lunch?: boolean): Promise<WorkDay | null> => {
    const targetDate = date
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

  const toggleLunch = async () => {
    setSaving(true)
    try {
      await ensureDay(!(dayData?.day?.lunch ?? false))
    } finally {
      setSaving(false)
    }
  }

  // Resolves the "ends next day" flag for the entered times. The flag is never
  // set behind the user's back: an end before the start is only treated as
  // next-day after an explicit confirmation (or after ticking the checkbox).
  // Returns null when the combination is invalid and nothing should be saved.
  const resolveCrossesMidnight = (start: string, end: string, checked: boolean): boolean | null => {
    if (checked) {
      // Mirror the server-side rule so a bad combination gets a clear message
      // instead of a silent failure.
      if (end >= start) {
        alert(t('workhours:crossesMidnightRangeError'))
        return null
      }
      return true
    }
    if (end === start) {
      alert(t('workhours:sessionRangeError'))
      return null
    }
    if (end < start) {
      if (!confirm(t('workhours:sessionCrossesMidnightConfirm', { start, end }))) return null
      return true
    }
    return false
  }

  // Draft edits clear the conflict message only when a value actually changed.
  const changeDraft = (patch: Partial<SessionDraft>) => {
    if ((Object.keys(patch) as (keyof SessionDraft)[]).some(k => patch[k] !== draft[k])) setSessionConflict(null)
    setDraft(d => ({ ...d, ...patch }))
  }

  const changeEdit = (patch: Partial<SessionEdit>) => {
    if ((Object.keys(patch) as (keyof SessionEdit)[]).some(k => patch[k] !== edit[k])) setSessionConflict(null)
    setEdit(e => ({ ...e, ...patch }))
  }

  const addSession = async () => {
    if (!draft.start || !draft.end) return
    const crossesMidnight = resolveCrossesMidnight(draft.start, draft.end, draft.crossesMidnight)
    if (crossesMidnight === null) return
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
        start_time: draft.start,
        end_time: draft.end,
        sort_order: sortOrder,
        is_internal: draft.isInternal,
        crosses_midnight: crossesMidnight,
      })
      if (ok) {
        setSessionConflict(null)
        setDraft(EMPTY_SESSION_DRAFT)
        await loadDay(date)
        loadFlex()
      }
    } catch (err) {
      reportConflict(err, 'add', 'save')
    } finally {
      setSaving(false)
    }
  }

  const deleteSession = async (sessionID: number) => {
    setSaving(true)
    try {
      const ok = await api.deleteSession(sessionID)
      if (ok) {
        // The message may name the deleted session.
        setSessionConflict(null)
        await loadDay(date)
        loadFlex()
      }
    } finally {
      setSaving(false)
    }
  }

  const startEditSession = (session: WorkSession) => {
    setSessionConflict(null)
    setEditSessionId(session.id)
    setEdit({ start: session.start_time, end: session.end_time, crossesMidnight: session.crosses_midnight })
  }

  const cancelEditSession = () => {
    setSessionConflict(null)
    setEditSessionId(null)
  }

  // Saves an edited session. Times and the "ends next day" flag are edited
  // together, so a wrongly flagged session can be corrected without deleting it.
  const saveEditSession = async (session: WorkSession) => {
    if (!edit.start || !edit.end) return
    const crossesMidnight = resolveCrossesMidnight(edit.start, edit.end, edit.crossesMidnight)
    if (crossesMidnight === null) return
    setSaving(true)
    try {
      const ok = await api.updateSession(session.id, {
        start_time: edit.start,
        end_time: edit.end,
        sort_order: session.sort_order,
        is_internal: session.is_internal,
        crosses_midnight: crossesMidnight,
      })
      if (!ok) {
        alert(t('workhours:punchSaveError'))
        return
      }
      setSessionConflict(null)
      setEditSessionId(null)
      await loadDay(date)
      loadFlex()
    } catch (err) {
      reportConflict(err, session.id, 'save')
    } finally {
      setSaving(false)
    }
  }

  const toggleInternal = async (session: WorkSession) => {
    setSaving(true)
    try {
      const ok = await api.updateSession(session.id, {
        start_time: session.start_time,
        end_time: session.end_time,
        sort_order: session.sort_order,
        is_internal: !session.is_internal,
        crosses_midnight: session.crosses_midnight,
      })
      if (!ok) {
        console.error('Failed to toggle internal flag')
        return
      }
      setSessionConflict(null)
      await loadDay(date)
    } catch (err) {
      // The times are unchanged, but a session already stored overlapping
      // another is still rejected by the server's overlap check.
      reportConflict(err, session.id, 'toggle')
    } finally {
      setSaving(false)
    }
  }

  const togglePreset = async (preset: WorkDeductionPreset) => {
    const existing = dayData?.day?.deductions.find(d => d.preset_id === preset.id)
    setSaving(true)
    try {
      if (existing) {
        const ok = await api.deleteDeduction(existing.id)
        if (ok) {
          await loadDay(date)
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
          await loadDay(date)
          loadFlex()
        }
      }
    } finally {
      setSaving(false)
    }
  }

  const addDeduction = async () => {
    const name = deductionDraft.name.trim()
    const minutes = parseInt(deductionDraft.minutes, 10)
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
            localStorage.setItem(RECENT_PRESETS_KEY, JSON.stringify(updated))
          } catch {
            // storage unavailable or quota exceeded — non-fatal
          }
          setRecentlyUsed(updated)
          setSelectedPresetId(null)
        }
        setDeductionDraft({ name: '', minutes: '' })
        await loadDay(date)
        loadFlex()
      }
    } finally {
      setSaving(false)
    }
  }

  const deleteDeduction = async (deductionID: number) => {
    setSaving(true)
    try {
      const ok = await api.deleteDeduction(deductionID)
      if (ok) {
        await loadDay(date)
        loadFlex()
      }
    } finally {
      setSaving(false)
    }
  }

  const changeDeductionDraft = (patch: Partial<DeductionDraft>) => {
    setDeductionDraft(d => ({ ...d, ...patch }))
  }

  // Picking a preset in the dropdown fills the custom deduction row with it.
  const selectPreset = (val: string) => {
    if (!val) {
      setSelectedPresetId(null)
      return
    }
    const presetId = parseInt(val, 10)
    const preset = presets.find(p => p.id === presetId)
    if (!preset) return
    setSelectedPresetId(presetId)
    setDeductionDraft({ name: preset.name, minutes: String(preset.default_minutes) })
  }

  const day = dayData?.day ?? null
  const serverSummary = dayData?.summary ?? null
  const lunch = day?.lunch ?? false
  const sessions = day?.sessions ?? []
  const deductions = day?.deductions ?? []
  const overlaps = useMemo(() => findSessionOverlaps(day?.sessions ?? []), [day])

  const copyYesterday = async () => {
    if (sessions.length > 0) return
    const yesterday = prevWeekday(date, navHolidaySet)
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
        let ok: boolean
        try {
          ok = await api.addSession({
            day_id: d.id,
            start_time: session.start_time,
            end_time: session.end_time,
            sort_order: (d.sessions?.length ?? 0),
            is_internal: session.is_internal,
            crosses_midnight: session.crosses_midnight,
          })
        } catch (err) {
          // Yesterday's sessions overlapped each other; the ones copied before
          // the conflict are kept.
          reportConflict(err, 'add', 'copy')
          break
        }
        if (!ok) {
          console.error('workhours: failed to copy session', session)
          break
        }
        // Keep track so subsequent sessions don't overwrite each other
        d = { ...d, sessions: [...(d.sessions ?? []), session] }
      }
      await loadDay(date)
      loadFlex()
    } finally {
      setSaving(false)
    }
  }

  const setLeave = async (leaveType: LeaveType | null) => {
    setLeaveSaving(true)
    const targetDate = date
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

  // Flex redeemed for this date, which the server credits into reported and
  // balance after CalculateDay; both the saved summary and the live estimate
  // must credit it the same way.
  const redeemedMinutes = serverSummary?.redeemed_minutes ?? 0

  // Saved totals go through the shared calculateDay mirror of the Go
  // calculator, so they always agree with the live estimate. The server's
  // summary is the fallback while settings load or if a stored time is
  // malformed (where the Go calculator would have failed too).
  const summary = useMemo<DaySummary | null>(() => {
    if (!serverSummary || !day || !workSettingsLoaded) return serverSummary
    let calc: DaySummaryCalc
    try {
      calc = applyRedemption(
        calculateDay(day.sessions ?? [], day.deductions ?? [], workSettings, { lunch: day.lunch }),
        redeemedMinutes,
      )
    } catch {
      return serverSummary
    }
    return {
      ...serverSummary,
      gross_minutes: calc.gross,
      lunch_minutes: calc.lunch,
      deduction_minutes: calc.deductions,
      net_minutes: calc.net,
      reported_minutes: calc.reported,
      reported_hours: calc.reported / 60,
      remainder_minutes: calc.remainder,
      standard_minutes: workSettings.standard_day_minutes,
      balance_minutes: calc.balance,
    }
  }, [serverSummary, day, workSettings, workSettingsLoaded, redeemedMinutes])

  // Recently used presets first, then the rest in their configured order.
  const sortedPresets = [...presets].sort((a, b) => {
    const aIdx = recentlyUsed.indexOf(a.id)
    const bIdx = recentlyUsed.indexOf(b.id)
    if (aIdx !== -1 && bIdx !== -1) return aIdx - bIdx
    if (aIdx !== -1) return -1
    if (bIdx !== -1) return 1
    return a.sort_order - b.sort_order
  })

  return {
    day,
    loading,
    saving,
    sessions,
    deductions,
    lunch,
    overlaps,
    presets,
    sortedPresets,
    selectedPresetId,
    leaveDay,
    leaveSaving,
    workSettings,
    redeemedMinutes,
    summary,
    draft,
    edit,
    editSessionId,
    conflict: activeConflict,
    deductionDraft,
    navHolidaySet,
    reload,
    changeDraft,
    changeEdit,
    addSession,
    deleteSession,
    startEditSession,
    cancelEditSession,
    saveEditSession,
    toggleInternal,
    toggleLunch,
    togglePreset,
    selectPreset,
    changeDeductionDraft,
    addDeduction,
    deleteDeduction,
    copyYesterday,
    setLeave,
  }
}

export type DayEntry = ReturnType<typeof useDayEntry>
