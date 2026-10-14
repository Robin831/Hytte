import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import {
  applyRedemption,
  calculateDay,
  isPunchStale,
  liveSegmentMinutes,
  type DaySummaryCalc,
} from '../calculateDay'
import type { WorkDeduction, WorkSession, WorkSettings } from '../types'
import { currentTimeHHMM } from '../dateUtils'
import { useWorkHoursApi } from '../useWorkHoursApi'

/** An in-progress punch-in. */
export interface OpenPunch {
  /** HH:MM the punch-in started at. */
  start: string
  /**
   * Calendar date the punch-in was recorded on, or null when the server did
   * not say. Needed so the live estimate can tell a session that has run past
   * midnight from a future start.
   */
  date: string | null
}

/** The viewed day's saved data the live estimate adds the open punch to. */
export interface LiveEstimateInput {
  sessions: WorkSession[]
  deductions: WorkDeduction[]
  settings: WorkSettings
  lunch: boolean
  /** Flex redeemed on the day, credited into reported and balance like the server does. */
  redeemedMinutes: number
}

export type LiveEstimate =
  | { kind: 'estimate'; calc: DaySummaryCalc }
  /** Open for longer than the allowed wrap: forgotten rather than running. */
  | { kind: 'stale'; punchDate: string }
  /** Malformed or future start time, or a malformed saved session. */
  | { kind: 'invalid' }

export interface UsePunchClockOptions {
  /** The day being viewed; punch-ins are recorded on it. */
  date: string
  /** Moves the view to another day (a restored or punched-out session's date). */
  setDate: (date: string) => void
  /** Called after a punch-out has saved a session, so the day and flex can reload. */
  onChanged?: () => void
}

/**
 * Page-level punch clock: the open punch-in, its actions, the saving flag and
 * the live "if punched out now" estimate. Called once by WorkHoursPage so the
 * keyboard shortcut and the day view share the same state.
 */
export function usePunchClock({ date, setDate, onChanged }: UsePunchClockOptions) {
  const { t } = useTranslation(['workhours', 'common'])
  const api = useWorkHoursApi()

  const [openPunch, setOpenPunch] = useState<OpenPunch | null>(null)
  const [saving, setSaving] = useState(false)
  const [now, setNow] = useState(() => new Date())

  // Set synchronously so a second toggle in the same tick can't slip past the
  // saving check before the state update has rendered.
  const savingRef = useRef(false)
  const editAbortRef = useRef<AbortController | null>(null)
  const dateRef = useRef(date)
  const setDateRef = useRef(setDate)
  const onChangedRef = useRef(onChanged)

  useEffect(() => {
    dateRef.current = date
    setDateRef.current = setDate
    onChangedRef.current = onChanged
  })

  const markSaving = useCallback((value: boolean) => {
    savingRef.current = value
    setSaving(value)
  }, [])

  // Restore any in-progress punch-in from the server.
  useEffect(() => {
    const controller = new AbortController()
    api.getPunchSession(controller.signal)
      .then(session => {
        if (!session) return
        const sessionDate = session.date ?? null
        if (sessionDate && sessionDate !== dateRef.current) {
          setDateRef.current(sessionDate)
        }
        setOpenPunch({ start: session.start_time, date: sessionDate })
      })
      .catch(() => {})
    return () => controller.abort()
  }, [api])

  // Keep the live estimate ticking while a punch is open.
  const punchOpen = openPunch !== null
  useEffect(() => {
    if (!punchOpen) return
    const id = setInterval(() => setNow(new Date()), 60_000)
    return () => clearInterval(id)
  }, [punchOpen])

  const punchIn = useCallback(async () => {
    const startTime = currentTimeHHMM()
    const targetDate = dateRef.current
    markSaving(true)
    try {
      const ok = await api.punchIn({ date: targetDate, start_time: startTime })
      if (ok) {
        setOpenPunch({ start: startTime, date: targetDate })
        setNow(new Date())
      } else {
        alert(t('workhours:punchInError'))
      }
    } catch {
      alert(t('workhours:punchInError'))
    } finally {
      markSaving(false)
    }
  }, [api, markSaving, t])

  const cancelPunch = useCallback(async () => {
    markSaving(true)
    try {
      const ok = await api.cancelPunch()
      if (ok) {
        editAbortRef.current?.abort()
        editAbortRef.current = null
        setOpenPunch(null)
      } else {
        alert(t('workhours:punchCancelError'))
      }
    } catch {
      alert(t('workhours:punchCancelError'))
    } finally {
      markSaving(false)
    }
  }, [api, markSaving, t])

  const punchOut = useCallback(async () => {
    if (!openPunch) return
    const punchStart = openPunch.start
    // Abort any in-flight start-time edit so its revert path can't resurrect the punch.
    editAbortRef.current?.abort()
    editAbortRef.current = null
    const endTime = currentTimeHHMM()
    // An end time at or before the start can only mean the session ran past
    // midnight. Ask instead of dead-ending, so the shift isn't lost.
    let crossesMidnight = false
    if (endTime <= punchStart) {
      if (endTime === punchStart) {
        alert(t('workhours:punchZeroLengthError'))
        return
      }
      if (!confirm(t('workhours:punchCrossesMidnightConfirm', { start: punchStart, end: endTime }))) {
        return
      }
      crossesMidnight = true
    }
    markSaving(true)
    try {
      const data = await api.punchOut({ end_time: endTime, crosses_midnight: crossesMidnight })
      if (data) {
        setOpenPunch(null)
        if (data.date !== dateRef.current) {
          setDateRef.current(data.date)
        }
        onChangedRef.current?.()
      } else {
        alert(t('workhours:punchSaveError'))
      }
    } catch {
      alert(t('workhours:punchSaveError'))
    } finally {
      markSaving(false)
    }
  }, [api, markSaving, openPunch, t])

  const editStart = useCallback(async (newTime: string) => {
    if (!newTime || !openPunch || newTime === openPunch.start) return
    const prev = openPunch
    // Abort any previous in-flight edit and start a new one.
    editAbortRef.current?.abort()
    const controller = new AbortController()
    editAbortRef.current = controller
    setOpenPunch({ ...prev, start: newTime })
    try {
      const ok = await api.editPunchStart(newTime, controller.signal)
      if (!ok) {
        console.error('workhours: edit punch start failed')
        setOpenPunch(prev)
      }
    } catch (err) {
      if (err instanceof Error && err.name === 'AbortError') return
      console.error('workhours: edit punch start:', err)
      setOpenPunch(prev)
    }
  }, [api, openPunch])

  const toggle = useCallback(() => {
    if (savingRef.current) return
    if (openPunch === null) punchIn()
    else punchOut()
  }, [openPunch, punchIn, punchOut])

  /**
   * The "if punched out now" totals for the viewed day, or null when there is
   * nothing to estimate: no open punch, or the punch belongs to another day
   * (the viewed day's sessions plus its elapsed time would describe neither).
   */
  const liveEstimate = useCallback((input: LiveEstimateInput): LiveEstimate | null => {
    if (!openPunch) return null
    if (openPunch.date !== null && openPunch.date !== date) return null
    // The live segment is null when it cannot be measured (stale punch-in,
    // malformed or future start); calculateDay would count it as 0, so check
    // it first to say why instead.
    if (liveSegmentMinutes(openPunch.start, now, openPunch.date) === null) {
      if (openPunch.date !== null && isPunchStale(openPunch.date, now)) {
        return { kind: 'stale', punchDate: openPunch.date }
      }
      return { kind: 'invalid' }
    }
    try {
      const calc = applyRedemption(
        calculateDay(input.sessions, input.deductions, input.settings, {
          lunch: input.lunch,
          openPunchStart: openPunch.start,
          openPunchDate: openPunch.date,
          now,
        }),
        input.redeemedMinutes,
      )
      return { kind: 'estimate', calc }
    } catch {
      return { kind: 'invalid' }
    }
  }, [openPunch, date, now])

  return { openPunch, saving, punchIn, punchOut, cancelPunch, editStart, toggle, liveEstimate }
}

export type PunchClock = ReturnType<typeof usePunchClock>
