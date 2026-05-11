import { useEffect, useId, useMemo, useReducer } from 'react'
import { useTranslation } from 'react-i18next'
import { AlertTriangle, Loader2 } from 'lucide-react'
import { Dialog, DialogBody, DialogFooter, DialogHeader } from '../ui/dialog'
import { formatDate } from '../../utils/formatDate'
import type { DayPlan, StridePlan, StrideEvaluationRecord, WeekSummary } from '../../types/stride'
import { DayCard } from './DayCard'
import { complianceBadgeClass, complianceIcon, flagIsSevere } from './strideHelpers'

type FetchState = {
  plan: StridePlan | null
  evaluations: StrideEvaluationRecord[]
  loading: boolean
  error: boolean
  evalError: boolean
}

type FetchAction =
  | { type: 'start' }
  | { type: 'success'; plan: StridePlan | null; evaluations: StrideEvaluationRecord[]; evalError: boolean }
  | { type: 'error' }

function fetchReducer(_state: FetchState, action: FetchAction): FetchState {
  switch (action.type) {
    case 'start': return { plan: null, evaluations: [], loading: true, error: false, evalError: false }
    case 'success': return { plan: action.plan, evaluations: action.evaluations, loading: false, error: false, evalError: action.evalError }
    case 'error': return { plan: null, evaluations: [], loading: false, error: true, evalError: false }
  }
}

interface WeekDetailsModalProps {
  week: WeekSummary | null
  workoutIdToDate: Map<number, string>
  onClose: () => void
}

export function WeekDetailsModal({ week, workoutIdToDate, onClose }: WeekDetailsModalProps) {
  const { t } = useTranslation('stride')
  const titleId = useId()

  const [{ plan, evaluations, loading, error, evalError }, dispatch] = useReducer(fetchReducer, {
    plan: null,
    evaluations: [],
    loading: false,
    error: false,
    evalError: false,
  })

  const planId = week?.plan_id

  useEffect(() => {
    if (!planId) return
    const controller = new AbortController()
    dispatch({ type: 'start' })
    ;(async () => {
      try {
        const [planRes, evalRes] = await Promise.all([
          fetch(`/api/stride/plans/${planId}`, { credentials: 'include', signal: controller.signal }),
          fetch(`/api/stride/evaluations?plan_id=${planId}`, { credentials: 'include', signal: controller.signal }),
        ])
        if (controller.signal.aborted) return
        if (!planRes.ok) throw new Error(`plan HTTP ${planRes.status}`)
        const planJson = await planRes.json()
        const evalJson = evalRes.ok ? await evalRes.json() : null
        if (controller.signal.aborted) return
        dispatch({
          type: 'success',
          plan: (planJson.plan ?? null) as StridePlan | null,
          evaluations: ((evalJson?.evaluations ?? []) as StrideEvaluationRecord[]),
          evalError: !evalRes.ok,
        })
      } catch (err) {
        if (err instanceof DOMException && err.name === 'AbortError') return
        if (!controller.signal.aborted) dispatch({ type: 'error' })
      }
    })()
    return () => {
      controller.abort()
    }
  }, [planId])

  const sortedPlanDays = useMemo(() => {
    if (!plan) return []
    return [...plan.plan].sort((a, b) => a.date.localeCompare(b.date))
  }, [plan])

  const planDateSet = useMemo(() => new Set(sortedPlanDays.map(d => d.date)), [sortedPlanDays])

  // Match evaluations to plan days for the day-by-day list, falling back to
  // eval.date for rest-day/missed evaluations without a linked workout.
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

  // Evaluations whose effective date sits outside this plan's week — mirrors
  // the "previous week feedback" block on the live plan view so late-evaluated
  // workouts (e.g. Sunday evaluated Monday under a new plan_id) stay visible.
  const orphanEvals = useMemo(() => {
    const orphans: Array<{ date: string; eval: StrideEvaluationRecord }> = []
    for (const rec of evaluations) {
      let date: string | undefined
      if (rec.workout_id != null) {
        date = workoutIdToDate.get(rec.workout_id)
      } else if (rec.eval.date) {
        date = rec.eval.date
      }
      if (!date || planDateSet.has(date)) continue
      if (rec.eval.compliance === 'rest_day' && !rec.eval.notes && (!rec.eval.flags || rec.eval.flags.length === 0)) {
        continue
      }
      orphans.push({ date, eval: rec })
    }
    return orphans.sort((a, b) => a.date.localeCompare(b.date))
  }, [evaluations, workoutIdToDate, planDateSet])

  if (!week) return null

  const pct = Math.min(Math.max(Math.round(Number(week.completion_rate) || 0), 0), 100)
  const chipClass = pct >= 80
    ? 'bg-green-500/20 text-green-300 border border-green-500/30'
    : pct >= 50
      ? 'bg-yellow-500/20 text-yellow-300 border border-yellow-500/30'
      : 'bg-red-500/20 text-red-300 border border-red-500/30'

  const weekLabel = t('plan.weekOf', {
    start: formatDate(`${week.week_start}T00:00:00`, { month: 'short', day: 'numeric' }),
    end: formatDate(`${week.week_end}T00:00:00`, { month: 'short', day: 'numeric' }),
  })

  return (
    <Dialog
      open
      onClose={onClose}
      aria-labelledby={titleId}
      maxWidth="max-w-2xl"
      overlayClassName="p-0 sm:p-4"
      className="sm:max-h-[90vh] max-h-screen h-full sm:h-auto sm:rounded-lg rounded-none"
    >
      <DialogHeader id={titleId} title={t('history.weekModal.title')} onClose={onClose} closeLabel={t('history.weekModal.close')} />
      <DialogBody>
        <div className="space-y-5">
          {/* Week-of header + completion chip + phase */}
          <div>
            <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
              <h3 className="text-base font-semibold text-white">{weekLabel}</h3>
              <span className={`text-xs font-medium px-2 py-0.5 rounded-full ${chipClass}`}>{pct}%</span>
              {week.phase && (
                <span className="text-xs px-1.5 py-0.5 bg-yellow-500/10 text-yellow-500 rounded">{week.phase}</span>
              )}
            </div>
            <p className="mt-1 text-xs text-gray-500">
              {t('history.week.sessions', { completed: week.sessions_completed, planned: week.sessions_planned })}
            </p>
          </div>

          {!!planId && loading && (
            <div className="flex items-center gap-2 text-sm text-gray-400">
              <Loader2 size={16} className="animate-spin" />
              <span>{t('loading')}</span>
            </div>
          )}

          {!!planId && error && !loading && (
            <p className="text-sm text-red-400">{t('plan.loadError')}</p>
          )}

          {!!planId && evalError && !loading && !error && (
            <p className="text-sm text-yellow-400">{t('history.weekModal.evalLoadError')}</p>
          )}

          {/* Day-by-day plan + evaluations */}
          {!!planId && !loading && !error && sortedPlanDays.length > 0 && (
            <div>
              {plan?.created_at && (
                <p className="mb-2 text-xs text-gray-500">
                  {t('plan.generatedAt', {
                    date: formatDate(plan.created_at, { dateStyle: 'medium' }),
                  })}
                </p>
              )}
              <div className="space-y-2">
                {sortedPlanDays.map(day => (
                  <DayCard
                    key={day.date}
                    day={day}
                    completed={dayEvaluationMap.has(day.date)}
                    evaluation={dayEvaluationMap.get(day.date)}
                  />
                ))}
              </div>
            </div>
          )}

          {/* Previous-week-style feedback: evaluations whose date falls outside the plan's days. */}
          {!!planId && !loading && !error && orphanEvals.length > 0 && (
            <div>
              <h4 className="mb-2 flex items-center gap-2 text-sm font-semibold text-white">
                <AlertTriangle size={14} className="text-yellow-400" />
                {t('plan.previousWeekFeedback')}
              </h4>
              <div className="space-y-2">
                {orphanEvals.map(({ date, eval: rec }) => {
                  const dateStr = `${date}T00:00:00`
                  const flags = Array.isArray(rec.eval.flags) ? rec.eval.flags : []
                  const contextSummary = rec.workout_context_summary?.trim()
                  return (
                    <div key={rec.id} className="bg-gray-800 rounded-xl border border-gray-700 p-3 space-y-2">
                      <div className="flex items-center gap-3">
                        {complianceIcon(rec.eval.compliance)}
                        <div className="flex-shrink-0">
                          <p className="text-xs font-semibold text-gray-400 uppercase">{formatDate(dateStr, { weekday: 'short' })}</p>
                          <p className="text-sm text-gray-300">{formatDate(dateStr, { month: 'short', day: 'numeric' })}</p>
                        </div>
                        <div className="flex-1 min-w-0 flex items-center gap-2 flex-wrap">
                          <span className="text-sm text-gray-300">{rec.eval.planned_type.replace(/_/g, ' ')}</span>
                          <span className={`text-xs font-medium px-2 py-0.5 rounded-full border ${complianceBadgeClass(rec.eval.compliance)}`}>
                            {t(`evaluation.${rec.eval.compliance}`)}
                          </span>
                          {flags.length > 0 && (
                            <span className="flex items-center gap-1 text-xs text-yellow-400">
                              <AlertTriangle size={12} />
                              {flags.length}
                            </span>
                          )}
                        </div>
                      </div>
                      {contextSummary && (
                        <div className="rounded-lg border border-gray-700 bg-gray-900/40 p-2">
                          <p className="text-xs font-semibold text-gray-500 uppercase mb-1">{t('evaluation.contextPanelTitle')}</p>
                          <p className="text-sm text-gray-300 whitespace-pre-wrap">{contextSummary}</p>
                        </div>
                      )}
                      {rec.eval.notes && (
                        <div>
                          <p className="text-xs font-semibold text-gray-500 uppercase mb-1">{t('evaluation.coachNotes')}</p>
                          <p className="text-sm text-gray-200">{rec.eval.notes}</p>
                        </div>
                      )}
                      {flags.length > 0 && (
                        <div className="flex flex-wrap gap-1.5">
                          {flags.map(flag => (
                            <span
                              key={flag}
                              className={`inline-flex items-center gap-1 px-2 py-0.5 text-xs rounded-full border ${
                                flagIsSevere(flag)
                                  ? 'bg-red-500/15 border-red-500/30 text-red-400'
                                  : 'bg-yellow-500/15 border-yellow-500/30 text-yellow-400'
                              }`}
                            >
                              <AlertTriangle size={10} />
                              {t(`evaluation.flagLabels.${flag}`, { defaultValue: flag.replace(/_/g, ' ') })}
                            </span>
                          ))}
                        </div>
                      )}
                      {rec.eval.adjustments && (
                        <div>
                          <p className="text-xs font-semibold text-gray-500 uppercase mb-1">{t('evaluation.adjustments')}</p>
                          <p className="text-sm text-gray-400">{rec.eval.adjustments}</p>
                        </div>
                      )}
                    </div>
                  )
                })}
              </div>
            </div>
          )}

          {(!planId || (!loading && !error && sortedPlanDays.length === 0 && orphanEvals.length === 0)) && (
            <p className="text-sm text-gray-500">{t('plan.empty')}</p>
          )}
        </div>
      </DialogBody>
      <DialogFooter>
        <button
          type="button"
          onClick={onClose}
          className="px-4 py-2 text-sm bg-gray-700 hover:bg-gray-600 text-white rounded-lg transition-colors"
        >
          {t('history.weekModal.close')}
        </button>
      </DialogFooter>
    </Dialog>
  )
}
