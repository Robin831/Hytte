import { useState, useId } from 'react'
import { useTranslation } from 'react-i18next'
import { AlertTriangle, CheckCircle2, ChevronDown, ChevronUp, Circle, RefreshCw } from 'lucide-react'
import { formatDate } from '../../utils/formatDate'
import type { DayPlan, StrideEvaluationRecord } from '../../types/stride'
import { complianceBadgeClass, complianceIcon, flagIsSevere } from './strideHelpers'

interface DayCardProps {
  day: DayPlan
  completed: boolean
  evaluation?: StrideEvaluationRecord
  changedDates?: Set<string>
  onRerun?: (date: string) => void
  rerunning?: boolean
}

export function DayCard({ day, completed, evaluation, changedDates, onRerun, rerunning }: DayCardProps) {
  const { t } = useTranslation('stride')
  const [expanded, setExpanded] = useState(false)

  const date = `${day.date}T00:00:00`
  const dayName = formatDate(date, { weekday: 'short' })
  const dateLabel = formatDate(date, { month: 'short', day: 'numeric' })

  const complianceLabel = evaluation ? t(`evaluation.${evaluation.eval.compliance}`) : null
  const hasExpandableContent = (!day.rest_day && !!day.session) || (!!evaluation && (day.rest_day || !day.session))
  const isHighlighted = changedDates?.has(day.date) ?? false
  const detailsId = useId()

  return (
    <div className={`bg-gray-800 rounded-xl border border-gray-700 overflow-hidden transition-all duration-1000 ${isHighlighted ? 'ring-2 ring-yellow-400/50' : ''}`}>
      <div className="relative flex items-stretch">
        <button
          type="button"
          onClick={() => hasExpandableContent && setExpanded(v => !v)}
          className={`flex-1 min-w-0 flex items-center gap-3 p-3 text-left ${hasExpandableContent ? 'hover:bg-gray-700 active:bg-gray-600 cursor-pointer' : 'cursor-default'}`}
          aria-expanded={expanded && hasExpandableContent}
          aria-controls={detailsId}
          disabled={!hasExpandableContent}
        >
          <div className="flex-shrink-0">
            {evaluation ? (
              complianceIcon(evaluation.eval.compliance)
            ) : completed ? (
              <CheckCircle2 size={18} className="text-green-400" />
            ) : (
              <Circle size={18} className="text-gray-600" />
            )}
          </div>

          <div className="flex-shrink-0 w-16">
            <p className="text-xs font-semibold text-gray-400 uppercase">{dayName}</p>
            <p className="text-sm text-gray-300">{dateLabel}</p>
          </div>

          <div className="flex-1 min-w-0 flex items-center gap-2 flex-wrap">
            {day.rest_day ? (
              <span className="text-xs font-medium px-2 py-0.5 rounded-full bg-gray-700 text-gray-400">{t('plan.restDay')}</span>
            ) : day.session ? (
              <p className="text-sm text-white truncate">{day.session.description}</p>
            ) : null}
            {evaluation && complianceLabel && (
              <span className={`text-xs font-medium px-2 py-0.5 rounded-full border ${complianceBadgeClass(evaluation.eval.compliance)}`}>
                {complianceLabel}
              </span>
            )}
            {evaluation && Array.isArray(evaluation.eval.flags) && evaluation.eval.flags.length > 0 && (
              <span className="flex items-center gap-1 text-xs text-yellow-400" aria-label={t('evaluation.warnings')}>
                <AlertTriangle size={12} />
                {evaluation.eval.flags.length}
              </span>
            )}
          </div>

          {hasExpandableContent && (
            <div className="flex-shrink-0">
              {expanded ? (
                <ChevronUp size={16} className="text-gray-500" />
              ) : (
                <ChevronDown size={16} className="text-gray-500" />
              )}
            </div>
          )}
        </button>

        {onRerun && (
          <button
            type="button"
            onClick={() => onRerun(day.date)}
            disabled={rerunning}
            className="flex-shrink-0 px-3 flex items-center text-gray-500 hover:text-yellow-400 disabled:text-gray-600 disabled:cursor-not-allowed transition-colors border-l border-gray-700"
            aria-label={t('plan.rerunCoach')}
            title={t('plan.rerunCoach')}
          >
            <RefreshCw size={14} className={rerunning ? 'animate-spin' : ''} />
          </button>
        )}
      </div>

      <div
        id={detailsId}
        className={`grid transition-[grid-template-rows] duration-200 ease-in-out ${
          expanded && hasExpandableContent ? 'grid-rows-[1fr]' : 'grid-rows-[0fr]'
        }`}
        aria-hidden={!(expanded && hasExpandableContent)}
        // @ts-expect-error — `inert` is a valid HTML attribute not yet in React's typings
        inert={!(expanded && hasExpandableContent) ? '' : undefined}
      >
        <div className="overflow-hidden">
          <div className="px-4 pb-4 space-y-3 border-t border-gray-700 pt-3">
            {!day.rest_day && day.session && (
              <>
                {day.session.description && (
                  <p className="text-sm text-gray-200">{day.session.description}</p>
                )}
                {day.session.warmup && (
                  <div>
                    <p className="text-xs font-semibold text-gray-500 uppercase mb-1">{t('plan.warmup')}</p>
                    <p className="text-sm text-gray-200">{day.session.warmup}</p>
                  </div>
                )}
                {day.session.main_set && (
                  <div>
                    <p className="text-xs font-semibold text-gray-500 uppercase mb-1">{t('plan.mainSet')}</p>
                    <p className="text-sm text-gray-200">{day.session.main_set}</p>
                  </div>
                )}
                {day.session.cooldown && (
                  <div>
                    <p className="text-xs font-semibold text-gray-500 uppercase mb-1">{t('plan.cooldown')}</p>
                    <p className="text-sm text-gray-200">{day.session.cooldown}</p>
                  </div>
                )}
                {day.session.strides && (
                  <div>
                    <p className="text-xs font-semibold text-gray-500 uppercase mb-1">{t('plan.strides')}</p>
                    <p className="text-sm text-gray-200">{day.session.strides}</p>
                  </div>
                )}
                {day.session.target_hr_cap > 0 && (
                  <div>
                    <p className="text-xs font-semibold text-gray-500 uppercase mb-1">{t('plan.targetHR')}</p>
                    <p className="text-sm text-gray-200">{t('plan.bpm', { value: day.session.target_hr_cap })}</p>
                  </div>
                )}
              </>
            )}

            {evaluation && (() => {
              const flags = Array.isArray(evaluation.eval.flags) ? evaluation.eval.flags : []
              const contextSummary = evaluation.workout_context_summary?.trim()
              return (
                <div className={`space-y-2 ${!day.rest_day && day.session ? 'mt-3 pt-3 border-t border-gray-700' : ''}`}>
                  {contextSummary && (
                    <div className="rounded-lg border border-gray-700 bg-gray-900/40 p-2">
                      <p className="text-xs font-semibold text-gray-500 uppercase mb-1">{t('evaluation.contextPanelTitle')}</p>
                      <p className="text-sm text-gray-300 whitespace-pre-wrap">{contextSummary}</p>
                    </div>
                  )}
                  {evaluation.eval.notes && (
                    <div>
                      <p className="text-xs font-semibold text-gray-500 uppercase mb-1">{t('evaluation.coachNotes')}</p>
                      <p className="text-sm text-gray-200">{evaluation.eval.notes}</p>
                    </div>
                  )}
                  {flags.length > 0 && (
                    <div>
                      <p className="text-xs font-semibold text-gray-500 uppercase mb-1">{t('evaluation.warnings')}</p>
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
                    </div>
                  )}
                  {evaluation.eval.adjustments && (
                    <div>
                      <p className="text-xs font-semibold text-gray-500 uppercase mb-1">{t('evaluation.adjustments')}</p>
                      <p className="text-sm text-gray-400">{evaluation.eval.adjustments}</p>
                    </div>
                  )}
                </div>
              )
            })()}
          </div>
        </div>
      </div>
    </div>
  )
}
