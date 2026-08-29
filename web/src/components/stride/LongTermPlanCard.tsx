import { useId, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Target, ChevronDown, ChevronRight, History } from 'lucide-react'
import { formatDate } from '../../utils/formatDate'
import { formatTargetTime } from '../../pages/strideUtils'
import type { MacroMesocycle, MacroPlanView } from '../../types/stride'
import { addWeeks, groupWeekRuns, macroWeekForDate, sortMacroWeeks } from './macroPlan'

// Left-edge accent per macro phase, sharing the timeline's palette so a build
// block reads the same colour in both places.
const PHASE_ACCENT: Record<string, string> = {
  base: 'border-blue-500/60',
  build: 'border-green-500/60',
  peak: 'border-orange-500/60',
  taper: 'border-red-500/60',
  race: 'border-yellow-500/60',
  recovery: 'border-purple-500/60',
}

function accentFor(phase: string): string {
  return PHASE_ACCENT[phase] ?? 'border-gray-600'
}

interface LongTermPlanCardProps {
  view: MacroPlanView
}

// The goal the block is being trained towards and its periodisation: the goal
// card (statement, target half-marathon time, benchmark, rationale, revision
// history) and the mesocycle strip with the week in progress highlighted.
export function LongTermPlanCard({ view }: LongTermPlanCardProps) {
  const { t } = useTranslation('stride')
  const [revisionsOpen, setRevisionsOpen] = useState(false)
  const headingId = useId()

  const weeks = useMemo(() => sortMacroWeeks(view.weeks), [view.weeks])

  // Date-granular so the lookup is stable across renders within a day.
  const today = useMemo(() => {
    const now = new Date()
    return new Date(now.getFullYear(), now.getMonth(), now.getDate())
  }, [])
  const currentWeek = useMemo(() => macroWeekForDate(weeks, today), [weeks, today])

  // The goal in force is the newest revision; the block's own goal column is
  // the fallback for a block written without a goal history.
  const goal = view.current_goal_revision?.goal ?? view.plan.goal

  // Newest first — the server returns the history oldest first.
  const revisions = useMemo(() => [...view.revisions].reverse(), [view.revisions])

  // The coach's periodisation, or the weeks' own mesocycle names when a block
  // was stored without one.
  const mesocycles: MacroMesocycle[] = useMemo(() => {
    if (view.plan.periodisation.length > 0) return view.plan.periodisation
    return groupWeekRuns(weeks, w => w.mesocycle)
      .filter(run => run.value !== '')
      .map(run => ({
        name: run.value,
        phase: run.startWeek.phase,
        start_week: run.startWeek.week_start,
        weeks: run.weeks,
        focus: '',
        race_id: run.startWeek.race_id,
      }))
  }, [view.plan.periodisation, weeks])

  // A mesocycle is current when the week in progress falls inside its span,
  // which is unambiguous even if the block reuses a mesocycle name.
  function isCurrentMesocycle(meso: MacroMesocycle): boolean {
    if (!currentWeek || !meso.start_week || meso.weeks <= 0) return false
    return (
      currentWeek.week_start >= meso.start_week &&
      currentWeek.week_start < addWeeks(meso.start_week, meso.weeks)
    )
  }

  return (
    <section aria-labelledby={headingId}>
      <h2 id={headingId} className="text-lg font-semibold text-white mb-4 flex items-center gap-2">
        <Target size={18} className="text-yellow-400" />
        {t('longTermPlan.title')}
      </h2>

      {/* Goal card */}
      <div className="bg-gray-800 rounded-xl border border-gray-700 p-4 space-y-3">
        <div className="flex items-start justify-between gap-2 flex-wrap">
          <p className="text-sm font-semibold text-white">{goal.statement}</p>
          {goal.primary_focus && (
            <span className="text-xs font-medium px-2 py-0.5 rounded-full bg-yellow-500/10 text-yellow-400 border border-yellow-500/30">
              {goal.primary_focus}
            </span>
          )}
        </div>

        <dl className="grid grid-cols-1 sm:grid-cols-2 gap-3">
          {goal.target_hm_time_s > 0 && (
            <div>
              <dt className="text-xs uppercase tracking-wide text-gray-500">{t('longTermPlan.targetHmTime')}</dt>
              <dd className="text-sm text-gray-200 font-medium">{formatTargetTime(goal.target_hm_time_s)}</dd>
            </div>
          )}
          {goal.benchmark && (
            <div>
              <dt className="text-xs uppercase tracking-wide text-gray-500">{t('longTermPlan.benchmark')}</dt>
              <dd className="text-sm text-gray-200">{goal.benchmark}</dd>
            </div>
          )}
        </dl>

        {goal.rationale && (
          <div>
            <p className="text-xs uppercase tracking-wide text-gray-500 mb-1">{t('longTermPlan.rationale')}</p>
            <p className="text-sm text-gray-400 whitespace-pre-line">{goal.rationale}</p>
          </div>
        )}

        {/* Goal history — every revision the weekly job or the athlete applied */}
        {revisions.length > 0 && (
          <div className="pt-1 border-t border-gray-700">
            <button
              type="button"
              onClick={() => setRevisionsOpen(open => !open)}
              aria-expanded={revisionsOpen}
              className="flex items-center gap-1.5 text-xs text-gray-400 hover:text-gray-200 transition-colors pt-2"
            >
              {revisionsOpen ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
              <History size={14} />
              {t('longTermPlan.revisions', { count: revisions.length })}
            </button>

            {revisionsOpen && (
              <ol className="mt-2 space-y-2">
                {revisions.map(revision => (
                  <li key={revision.id} className="text-xs text-gray-400 border-l-2 border-gray-700 pl-3">
                    <div className="flex items-center gap-2 flex-wrap text-gray-300">
                      <span>{formatDate(`${revision.week_start}T00:00:00`, { month: 'short', day: 'numeric', year: 'numeric' })}</span>
                      <span className="px-1.5 py-0.5 rounded bg-gray-700/60 text-gray-300">
                        {t(`longTermPlan.revisionSource.${revision.source}`, { defaultValue: revision.source })}
                      </span>
                      {revision.goal.target_hm_time_s > 0 && (
                        <span className="text-gray-400">
                          {t('longTermPlan.revisionTarget', { time: formatTargetTime(revision.goal.target_hm_time_s) })}
                        </span>
                      )}
                    </div>
                    {revision.reason && <p className="mt-0.5">{revision.reason}</p>}
                  </li>
                ))}
              </ol>
            )}
          </div>
        )}
      </div>

      {/* Mesocycle strip */}
      {mesocycles.length > 0 && (
        <div className="mt-4">
          <div className="flex items-center justify-between gap-2 flex-wrap mb-2">
            <h3 className="text-sm font-semibold text-gray-300 uppercase tracking-wide">
              {t('longTermPlan.mesocycles')}
            </h3>
            {currentWeek && (
              <span className="text-xs text-gray-400">
                {t('longTermPlan.currentWeek', { seq: currentWeek.seq, total: weeks.length })}
              </span>
            )}
          </div>

          <ol className="grid grid-cols-1 sm:grid-cols-2 gap-2">
            {mesocycles.map(meso => {
              const current = isCurrentMesocycle(meso)
              return (
                <li
                  key={`${meso.name}-${meso.start_week}`}
                  className={`rounded-lg border-l-4 ${accentFor(meso.phase)} bg-gray-800 border border-gray-700 px-3 py-2 ${current ? 'ring-1 ring-yellow-400/60' : ''}`}
                >
                  <div className="flex items-center justify-between gap-2 flex-wrap">
                    <span className="text-sm font-medium text-gray-200">{meso.name}</span>
                    {current && (
                      <span className="text-xs font-medium px-1.5 py-0.5 rounded bg-yellow-500/10 text-yellow-400 border border-yellow-500/30">
                        {t('longTermPlan.current')}
                      </span>
                    )}
                  </div>
                  <div className="flex items-center gap-1.5 text-xs text-gray-500 mt-0.5 flex-wrap">
                    <span>{t(`timeline.phases.${meso.phase}`, { defaultValue: meso.phase })}</span>
                    <span className="text-gray-600">·</span>
                    <span>{t('longTermPlan.weekCount', { count: meso.weeks })}</span>
                    <span className="text-gray-600">·</span>
                    <span>{formatDate(`${meso.start_week}T00:00:00`, { month: 'short', day: 'numeric' })}</span>
                  </div>
                  {meso.focus && <p className="text-xs text-gray-400 mt-1">{meso.focus}</p>}
                </li>
              )
            })}
          </ol>
        </div>
      )}
    </section>
  )
}
