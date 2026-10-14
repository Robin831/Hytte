import { useState, useEffect, useMemo, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { Zap, RefreshCw, History } from 'lucide-react'
import { formatDate } from '../utils/formatDate'
import type { WeekSummary } from '../types/stride'
import { TrainingBlockTimeline } from '../components/stride/TrainingBlockTimeline'
import { LongTermPlanCard } from '../components/stride/LongTermPlanCard'
import { macroWeekForWeekStart } from '../components/stride/macroPlan'
import WorkoutLibrary from '../components/stride/WorkoutLibrary'
import RacePredictionsCard from '../components/training/RacePredictionsCard'
import type { RacePredictions } from '../types/training'
import StrideChatDrawer from '../components/stride/StrideChatDrawer'
import { DayCard } from '../components/stride/DayCard'
import { WeekDetailsModal } from '../components/stride/WeekDetailsModal'
import { PlanHistory } from '../components/stride/PlanHistory'
import { RaceCalendarSection } from '../components/stride/RaceCalendarSection'
import { CoachNotesSection } from '../components/stride/CoachNotesSection'
import { useStrideRaces } from '../hooks/useStrideRaces'
import { useStrideNotes } from '../hooks/useStrideNotes'
import { useStridePlan } from '../hooks/useStridePlan'
import { useStrideMacroPlan } from '../hooks/useStrideMacroPlan'

export default function StridePage() {
  const { t } = useTranslation('stride')

  const races = useStrideRaces()
  const notes = useStrideNotes()
  const macro = useStrideMacroPlan()
  const { macroPlan } = macro
  const {
    currentPlan,
    previousPlanId,
    chatPlanId,
    changedDates,
    completedDates,
    workoutIdToDate,
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
  } = useStridePlan({
    // A re-run can consume coach notes, so the list is re-read with it.
    reloadNotes: notes.reload,
    // Adjusting a week can auto-apply goal drift, so the block (and its goal
    // history) is reloaded rather than left showing the pre-adjustment goal.
    onPlanGenerated: macro.reloadMacroPlan,
  })

  const [racePredictions, setRacePredictions] = useState<RacePredictions | null>(null)
  // Week details modal state — opened when the user clicks a Plan History row.
  const [selectedWeek, setSelectedWeek] = useState<WeekSummary | null>(null)

  // Current fitness estimate — the stored weekly race-prediction snapshot the
  // plan generator also reads. Non-fatal when unavailable; the card hides.
  useEffect(() => {
    const controller = new AbortController()
    ;(async () => {
      try {
        const res = await fetch('/api/training/predictions', { credentials: 'include', signal: controller.signal })
        if (!res.ok) return
        const data: RacePredictions = await res.json()
        if (!controller.signal.aborted) setRacePredictions(data)
      } catch {
        // Card simply does not render.
      }
    })()
    return () => controller.abort()
  }, [])

  // Live refresh: an evaluation lands from a background Claude call minutes
  // after a workout context is saved, so without a push the new row only
  // appeared on a manual page refresh. The training SSE stream announces every
  // stored evaluation; any stride_eval_ready for this user refetches the
  // week's evaluations (cheap, and the event volume is a handful per day).
  // The macro block outcome rides the same stream (see useStrideMacroPlan).
  // The refs keep the subscription stable across renders while always calling
  // the latest handlers.
  const refreshEvaluationsRef = useRef(refreshEvaluations)
  const handleMacroOutcomeRef = useRef(macro.handleOutcome)
  useEffect(() => {
    refreshEvaluationsRef.current = refreshEvaluations
    handleMacroOutcomeRef.current = macro.handleOutcome
  })
  useEffect(() => {
    const es = new EventSource('/api/training/events', { withCredentials: true })
    es.addEventListener('stride_eval_ready', () => {
      void refreshEvaluationsRef.current()
    })
    const parse = (e: MessageEvent): unknown => {
      try {
        return JSON.parse(String(e.data))
      } catch {
        return {}
      }
    }
    es.addEventListener('stride_macro_ready', (e: MessageEvent) => {
      handleMacroOutcomeRef.current('ready', parse(e))
    })
    es.addEventListener('stride_macro_failed', (e: MessageEvent) => {
      handleMacroOutcomeRef.current('failed', parse(e))
    })
    return () => es.close()
  }, [])

  // The macro week the plan on screen materialises. The plan carries its own
  // phase (denormalised when the week was adjusted), so the macro week is only
  // consulted when that column is empty — a legacy plan generated before the
  // block existed.
  const planMacroWeek = useMemo(() => {
    if (!macroPlan || !currentPlan) return null
    return macroWeekForWeekStart(macroPlan.weeks, currentPlan.week_start)
  }, [macroPlan, currentPlan])
  const weekPhase = currentPlan?.phase || planMacroWeek?.phase || ''

  return (
    <div className="max-w-2xl mx-auto px-4 py-6 space-y-8">
      {/* Header */}
      <div className="flex items-center gap-3">
        <Zap size={28} className="text-yellow-400" />
        <div>
          <h1 className="text-2xl font-bold text-white">{t('title')}</h1>
          <p className="text-sm text-gray-400">{t('subtitle')}</p>
        </div>
      </div>

      {/* Training Block Timeline */}
      <TrainingBlockTimeline races={races.races} loading={races.loading} macroPlan={macroPlan} />

      {/* Long-term plan — only when a macro block exists */}
      {macroPlan && (
        <LongTermPlanCard
          view={macroPlan}
          historyWeeks={macro.macroHistory}
          historyLoading={macro.macroHistoryLoading}
          historyError={macro.macroHistoryError}
          onReloadHistory={() => { void macro.reloadMacroHistory() }}
          onRegenerate={macro.regenerate}
          onExtend={macro.extend}
          busyAction={macro.macroAction}
          actionError={macro.macroActionError}
        />
      )}

      {/* Current fitness estimate — the same weekly snapshot the coach reads
          when generating the plan, so athlete and coach see one number. */}
      {racePredictions?.predictions && racePredictions.predictions.length > 0 && (
        <RacePredictionsCard data={racePredictions} />
      )}

      {/* Weekly Plan */}
      <section>
        <div className="flex items-center justify-between mb-4">
          <h2 className="text-lg font-semibold text-white">{t('plan.title')}</h2>
          <button
            type="button"
            onClick={generatePlan}
            disabled={generating || planLoading}
            className="flex items-center gap-1.5 px-3 py-1.5 text-sm bg-yellow-600 hover:bg-yellow-700 disabled:opacity-50 text-white rounded-lg transition-colors"
          >
            <RefreshCw size={14} className={generating ? 'animate-spin' : ''} />
            {/* With a macro block the button adjusts the block's week rather
                than planning one from scratch, and says so. */}
            {generating
              ? t('plan.generating')
              : macroPlan
                ? t(generateWeekMode === 'current' ? 'plan.adjustThisWeek' : 'plan.adjustNextWeek')
                : t(generateWeekMode === 'current' ? 'plan.generateThisWeek' : 'plan.generateNextWeek')}
          </button>
        </div>

        {generateError && (
          <p className="mb-3 text-sm text-red-400">{generateError}</p>
        )}

        {rerunError && (
          <p className="mb-3 text-sm text-red-400">{rerunError}</p>
        )}

        {planLoading ? (
          <p className="text-sm text-gray-400">{t('loading')}</p>
        ) : planError ? (
          <p className="text-sm text-red-400">{t('plan.loadError')}</p>
        ) : currentPlan === null ? (
          <div className="flex flex-col items-center justify-center py-10 text-center bg-gray-800/50 rounded-xl border border-gray-700 border-dashed">
            <Zap size={32} className="text-gray-600 mb-3" />
            <p className="text-sm font-medium text-gray-300 mb-1">{t('plan.empty')}</p>
            <p className="text-xs text-gray-500">{t('plan.emptyHint')}</p>
          </div>
        ) : (
          <div>
            {/* Week header */}
            <div className="mb-3 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-gray-500">
              <span>
                {t('plan.weekOf', {
                  start: formatDate(`${currentPlan.week_start}T00:00:00`, { month: 'short', day: 'numeric' }),
                  end: formatDate(`${currentPlan.week_end}T00:00:00`, { month: 'short', day: 'numeric' }),
                })}
              </span>
              {weekPhase && (
                <span className="px-1.5 py-0.5 bg-yellow-500/10 text-yellow-500 rounded">
                  {t('plan.phase', { phase: t(`timeline.phases.${weekPhase}`, { defaultValue: weekPhase }) })}
                </span>
              )}
              <span>
                {t('plan.generatedAt', {
                  date: formatDate(currentPlan.created_at, { dateStyle: 'medium' }),
                })}
              </span>
            </div>

            {/* How this week deviates from its macro week target, in the
                coach's own words. Absent on plans with nothing to report. */}
            {currentPlan.adjustment_summary && (
              <div className="mb-3 rounded-lg border border-gray-700 bg-gray-800/60 px-3 py-2">
                <p className="text-xs uppercase tracking-wide text-gray-500 mb-0.5">
                  {t('plan.adjustmentSummary')}
                </p>
                <p className="text-sm text-gray-300 whitespace-pre-line">{currentPlan.adjustment_summary}</p>
              </div>
            )}

            {/* Day cards */}
            <div className="space-y-2">
              {sortedPlanDays.map(day => (
                <DayCard
                  key={day.date}
                  day={day}
                  completed={completedDates.has(day.date)}
                  evaluation={dayEvaluationMap.get(day.date)}
                  changedDates={changedDates}
                  onRerun={rerunDay}
                  rerunning={rerunningDate === day.date}
                  onEvalRevised={refreshEvaluations}
                />
              ))}
            </div>

            {/* Chat drawer */}
            <StrideChatDrawer
              planId={chatPlanId ?? currentPlan.id}
              currentPlanId={currentPlan.id}
              onViewPreviousChat={previousPlanId ? toggleChatPlan : undefined}
              onPlanUpdated={applyChatPlanUpdate}
            />
          </div>
        )}
      </section>

      {/* Workout library — the sessions the weekly coach rotates through */}
      <WorkoutLibrary />

      {/* Race Calendar */}
      <RaceCalendarSection
        races={races.races}
        loading={races.loading}
        onSave={races.saveRace}
        onDelete={races.deleteRace}
        onReload={races.reload}
      />

      {/* Coach Notes */}
      <CoachNotesSection
        notes={notes.notes}
        consumedNotes={notes.consumedNotes}
        loading={notes.loading}
        onCreate={notes.createNote}
        onUpdate={notes.updateNote}
        onDelete={notes.deleteNote}
        onReload={notes.reload}
      />

      {/* Plan History */}
      <section>
        <h2 className="text-lg font-semibold text-white flex items-center gap-2 mb-4">
          <History size={18} className="text-gray-400" />
          {t('history.title')}
        </h2>
        <PlanHistory onOpenWeek={setSelectedWeek} />
      </section>

      {/* Week details modal */}
      <WeekDetailsModal
        week={selectedWeek}
        workoutIdToDate={workoutIdToDate}
        onClose={() => setSelectedWeek(null)}
      />
    </div>
  )
}
