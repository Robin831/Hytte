import { useState, useEffect, useCallback, useMemo, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { localDateString } from '../lib/dates'
import type { DayPlan, StrideEvaluationRecord, StridePlan } from '../types/stride'

// How long changed day cards stay highlighted after a re-run or chat edit.
const HIGHLIGHT_MS = 3000

// Returns evaluations for a plan as a list (doesn't set state). Any failure,
// including an abort, yields null so the caller can keep what it has.
async function loadEvaluationsForPlan(planId: number, signal?: AbortSignal): Promise<StrideEvaluationRecord[] | null> {
  try {
    const res = await fetch(`/api/stride/evaluations?plan_id=${planId}`, { credentials: 'include', signal })
    if (!res.ok) return null
    const data = await res.json()
    return (data.evaluations ?? []) as StrideEvaluationRecord[]
  } catch {
    return null
  }
}

// Loads evaluations for both the current and the previous plan so that
// feedback from the outgoing week (e.g. Sunday's workout evaluated Monday
// 01:00, still linked to the old plan_id) is visible in the current week view.
// Merged and de-duplicated by id; the current plan takes precedence. Yields
// null if either request failed, rather than a list missing one plan's half.
async function loadMergedEvaluations(
  planId: number,
  previousPlanId: number | null,
  signal?: AbortSignal,
): Promise<StrideEvaluationRecord[] | null> {
  const [curr, prev] = await Promise.all([
    loadEvaluationsForPlan(planId, signal),
    previousPlanId ? loadEvaluationsForPlan(previousPlanId, signal) : Promise.resolve([]),
  ])
  if (curr === null || prev === null) return null
  const byId = new Map<number, StrideEvaluationRecord>()
  for (const e of [...prev, ...curr]) byId.set(e.id, e)
  return Array.from(byId.values())
}

export interface UseStridePlanOptions {
  /** Re-reads the coach notes after a day re-run (a re-run can consume notes). */
  reloadNotes?: (signal?: AbortSignal) => Promise<void>
  /** Runs after a successful generation, before the Generate button is released. */
  onPlanGenerated?: () => Promise<void>
}

export interface UseStridePlanResult {
  /** This week's plan, or null when none exists yet (404). */
  currentPlan: StridePlan | null
  /** The plan before the current one, whose evaluations are merged in. */
  previousPlanId: number | null
  /** Which plan's conversation the chat drawer shows. */
  chatPlanId: number | null
  /** Days to highlight after a re-run or chat edit; cleared after HIGHLIGHT_MS. */
  changedDates: Set<string>
  /** Local dates that have a recorded workout. */
  completedDates: Set<string>
  workoutIdToDate: Map<number, string>
  /** Evaluations of the current and previous plan, merged by id. */
  evaluations: StrideEvaluationRecord[]
  /** Newest evaluation per plan day. */
  dayEvaluationMap: Map<string, StrideEvaluationRecord>
  /** The current plan's days, Monday to Sunday. */
  sortedPlanDays: DayPlan[]
  planLoading: boolean
  planError: boolean
  generating: boolean
  generateError: string
  /** The week the Generate button targets; the button label reads it too. */
  generateWeekMode: 'current' | 'next'
  rerunningDate: string | null
  rerunError: string
  /** Re-read the merged evaluations. A failed load keeps the current list. */
  refreshEvaluations: () => Promise<void>
  /** Re-run the coach evaluation for one day, then refresh evaluations and notes. */
  rerunDay: (date: string) => Promise<void>
  /** Generate the week generateWeekMode names. */
  generatePlan: () => Promise<void>
  /** Apply a plan the coach chat rewrote, highlighting the changed days. */
  applyChatPlanUpdate: (newPlan: DayPlan[]) => void
  /** Switch the chat drawer between the current and the previous plan. */
  toggleChatPlan: () => void
}

/**
 * Owns the Stride weekly plan: the current plan and the one before it, their
 * merged evaluations, which days have a workout, plan generation, day re-runs,
 * chat edits and the changed-day highlight. Every request is aborted on unmount.
 */
export function useStridePlan(options: UseStridePlanOptions = {}): UseStridePlanResult {
  const { t } = useTranslation('stride')
  const [currentPlan, setCurrentPlan] = useState<StridePlan | null>(null)
  const [previousPlanId, setPreviousPlanId] = useState<number | null>(null)
  const [chatPlanId, setChatPlanId] = useState<number | null>(null)
  const [changedDates, setChangedDates] = useState<Set<string>>(new Set())
  const [completedDates, setCompletedDates] = useState<Set<string>>(new Set())
  const [workoutIdToDate, setWorkoutIdToDate] = useState<Map<number, string>>(new Map())
  const [evaluations, setEvaluations] = useState<StrideEvaluationRecord[]>([])
  const [planLoading, setPlanLoading] = useState(true)
  const [planError, setPlanError] = useState(false)
  const [generating, setGenerating] = useState(false)
  const [generateError, setGenerateError] = useState('')
  const [rerunningDate, setRerunningDate] = useState<string | null>(null)
  const [rerunError, setRerunError] = useState('')
  const highlightTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const rerunAbortRef = useRef<AbortController | null>(null)
  // Lives for as long as the hook is mounted; requests made outside an effect
  // use it so a response that lands after unmount is dropped.
  const lifetimeRef = useRef<AbortController | null>(null)
  // The callbacks come from sibling hooks; the ref always holds the latest so
  // the handlers below need not be rebuilt when they change.
  const optionsRef = useRef(options)
  useEffect(() => {
    optionsRef.current = options
  })

  const loadCurrentPlan = useCallback(async (signal?: AbortSignal) => {
    setPlanError(false)
    try {
      const res = await fetch('/api/stride/plans/current', { credentials: 'include', signal })
      if (res.status === 404) {
        if (!signal?.aborted) {
          setCurrentPlan(null)
          setPreviousPlanId(null)
        }
        return
      }
      if (!res.ok) {
        throw new Error(`Failed to load plan: ${res.status} ${res.statusText}`)
      }
      const data = await res.json()
      if (!signal?.aborted) {
        const plan: StridePlan | null = data.plan ?? null
        setCurrentPlan(plan)
        // Fetch the two most recent plans so we can identify the previous one.
        // This lets us load its evaluations (e.g. Sunday workout feedback that
        // was evaluated after the new plan was generated).
        if (plan) {
          try {
            const listRes = await fetch('/api/stride/plans?limit=2&offset=0', { credentials: 'include', signal })
            if (listRes.ok) {
              const listData = await listRes.json()
              const plans: StridePlan[] = listData.plans ?? []
              const prev = plans.find((p: StridePlan) => p.id !== plan.id)
              setPreviousPlanId(prev?.id ?? null)
            }
          } catch {
            // Non-fatal — we just won't show previous-plan evaluations.
          }
        }
      }
    } catch (error) {
      if (error instanceof DOMException && error.name === 'AbortError') return
      console.error('Failed to load current plan', error)
      if (!signal?.aborted) setPlanError(true)
    } finally {
      if (!signal?.aborted) {
        setPlanLoading(false)
      }
    }
  }, [])

  const loadWorkouts = useCallback(async (signal?: AbortSignal) => {
    try {
      const res = await fetch('/api/training/workouts', { credentials: 'include', signal })
      if (!res.ok) return
      const data = await res.json()
      if (!signal?.aborted) {
        const workouts: Array<{ id: number; started_at: string }> = data.workouts ?? []
        const dates = new Set<string>(workouts.map(w => localDateString(new Date(w.started_at))))
        const idToDate = new Map<number, string>(
          workouts.map(w => [w.id, localDateString(new Date(w.started_at))])
        )
        setCompletedDates(dates)
        setWorkoutIdToDate(idToDate)
      }
    } catch (error) {
      if (error instanceof DOMException && error.name === 'AbortError') return
      console.error('Failed to load workouts for completion status', error)
    }
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    lifetimeRef.current = controller
    // eslint-disable-next-line react-hooks/set-state-in-effect -- async data fetch; AbortController prevents stale updates on unmount
    void loadCurrentPlan(controller.signal)
    void loadWorkouts(controller.signal)
    return () => {
      controller.abort()
      if (lifetimeRef.current === controller) lifetimeRef.current = null
    }
  }, [loadCurrentPlan, loadWorkouts])

  useEffect(() => {
    return () => {
      if (highlightTimerRef.current) clearTimeout(highlightTimerRef.current)
      if (rerunAbortRef.current) rerunAbortRef.current.abort()
    }
  }, [])

  const planId = currentPlan?.id

  // Sync chatPlanId to current plan — when a new plan is generated or loaded,
  // the chat switches to the fresh conversation automatically.
  useEffect(() => {
    if (planId) {
      // eslint-disable-next-line react-hooks/set-state-in-effect -- chatPlanId tracks which plan's chat is shown; it can also be toggled to previousPlanId, so it cannot be purely derived
      setChatPlanId(planId)
    }
  }, [planId])

  useEffect(() => {
    if (!planId) return
    // eslint-disable-next-line react-hooks/set-state-in-effect -- reset before fetch; AbortController prevents stale updates on unmount
    setEvaluations([])
    const controller = new AbortController()
    void (async () => {
      const merged = await loadMergedEvaluations(planId, previousPlanId, controller.signal)
      if (merged && !controller.signal.aborted) setEvaluations(merged)
    })()
    return () => { controller.abort() }
  }, [planId, previousPlanId])

  // Re-reads the merged evaluations, e.g. after a revised evaluation or a
  // stride_eval_ready push. A failed load just leaves the current list.
  const refreshEvaluations = useCallback(async () => {
    if (!planId) return
    const signal = lifetimeRef.current?.signal
    const merged = await loadMergedEvaluations(planId, previousPlanId, signal)
    if (merged && !signal?.aborted) setEvaluations(merged)
  }, [planId, previousPlanId])

  const flashChangedDates = useCallback((dates: Set<string>) => {
    setChangedDates(dates)
    if (highlightTimerRef.current) clearTimeout(highlightTimerRef.current)
    highlightTimerRef.current = setTimeout(() => setChangedDates(new Set()), HIGHLIGHT_MS)
  }, [])

  const rerunDay = useCallback(async (date: string) => {
    setRerunError('')
    setRerunningDate(date)
    if (rerunAbortRef.current) rerunAbortRef.current.abort()
    const controller = new AbortController()
    rerunAbortRef.current = controller
    try {
      const res = await fetch(`/api/stride/days/${date}/reevaluate`, {
        method: 'POST',
        credentials: 'include',
        signal: controller.signal,
      })
      if (controller.signal.aborted) return
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        setRerunError(data.error ?? t('plan.rerunError'))
        return
      }
      if (planId) {
        const merged = await loadMergedEvaluations(planId, previousPlanId, controller.signal)
        if (controller.signal.aborted) return
        if (merged) setEvaluations(merged)
      }
      await optionsRef.current.reloadNotes?.(controller.signal)
      if (controller.signal.aborted) return
      flashChangedDates(new Set([date]))
    } catch (err) {
      if ((err as { name?: string })?.name === 'AbortError') return
      setRerunError(t('plan.rerunError'))
    } finally {
      if (rerunAbortRef.current === controller) {
        rerunAbortRef.current = null
      }
      if (!controller.signal.aborted) {
        setRerunningDate(null)
      }
    }
  }, [planId, previousPlanId, flashChangedDates, t])

  // Which week the Generate button targets. Derived from what the page is
  // actually showing: no plan on screen means this week is missing (the Monday
  // 02:00 job failed, or the user is returning mid-week), so generate the
  // current week; a plan on screen means the next one is what's wanted.
  // The button label reads from the same value so the two can never disagree.
  const generateWeekMode: 'current' | 'next' = currentPlan === null ? 'current' : 'next'

  const generatePlan = useCallback(async () => {
    setGenerateError('')
    setGenerating(true)
    try {
      const res = await fetch(`/api/stride/plans/generate?week=${generateWeekMode}`, {
        method: 'POST',
        credentials: 'include',
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        setGenerateError(data.error ?? t('plan.generateError'))
        return
      }
      const data = await res.json()
      const newPlan: StridePlan | null = data.plan ?? null
      if (newPlan) {
        // The current plan becomes the previous one; update before replacing.
        setCurrentPlan(prev => {
          if (prev && prev.id !== newPlan.id) {
            setPreviousPlanId(prev.id)
          }
          return newPlan
        })
      } else {
        setCurrentPlan(null)
      }
      await optionsRef.current.onPlanGenerated?.()
    } catch {
      setGenerateError(t('plan.generateError'))
    } finally {
      setGenerating(false)
    }
  }, [generateWeekMode, t])

  // Applies a plan the coach chat rewrote, highlighting the days it changed.
  const applyChatPlanUpdate = useCallback((newPlan: DayPlan[]) => {
    if (currentPlan) {
      const oldMap = new Map(currentPlan.plan.map(d => [d.date, JSON.stringify(d)]))
      const changed = new Set<string>()
      for (const day of newPlan) {
        if (oldMap.get(day.date) !== JSON.stringify(day)) {
          changed.add(day.date)
        }
      }
      if (changed.size > 0) flashChangedDates(changed)
    }
    setCurrentPlan(prev => prev ? { ...prev, plan: newPlan } : prev)
  }, [currentPlan, flashChangedDates])

  // Switches the chat drawer between the current and the previous plan's conversation.
  const toggleChatPlan = useCallback(() => {
    if (!planId || !previousPlanId) return
    setChatPlanId(prev => prev === planId ? previousPlanId : planId)
  }, [planId, previousPlanId])

  // Sort plan days Monday–Sunday
  const sortedPlanDays = useMemo(
    () => currentPlan ? [...currentPlan.plan].sort((a, b) => a.date.localeCompare(b.date)) : [],
    [currentPlan],
  )

  // Map each plan day date to its newest stride evaluation (via workout date lookup,
  // or via eval.date for rest_day/missed evaluations without a workout).
  // evaluations is ordered created_at DESC so the first entry per date is the newest.
  const dayEvaluationMap = useMemo(() => {
    const map = new Map<string, StrideEvaluationRecord>()
    for (const rec of evaluations) {
      if (rec.workout_id != null) {
        const date = workoutIdToDate.get(rec.workout_id)
        if (date && !map.has(date)) map.set(date, rec)
      } else if (rec.eval.date && !map.has(rec.eval.date)) {
        map.set(rec.eval.date, rec)
      }
    }
    return map
  }, [evaluations, workoutIdToDate])

  return {
    currentPlan,
    previousPlanId,
    chatPlanId,
    changedDates,
    completedDates,
    workoutIdToDate,
    evaluations,
    dayEvaluationMap,
    sortedPlanDays,
    planLoading,
    planError,
    generating,
    generateError,
    generateWeekMode,
    rerunningDate,
    rerunError,
    refreshEvaluations,
    rerunDay,
    generatePlan,
    applyChatPlanUpdate,
    toggleChatPlan,
  }
}
