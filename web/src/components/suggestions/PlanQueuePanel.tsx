import { useTranslation } from 'react-i18next'
import { Loader2, X, RotateCcw, CheckCircle2, XCircle, ListOrdered } from 'lucide-react'
import {
  clearPlanQueueDone,
  dismissFailedPlan,
  enqueuePlan,
  removeQueuedPlan,
  type PlanQueueState,
} from '../../pages/suggestionsPlanQueue'

export interface PlanQueuePanelProps {
  state: PlanQueueState
}

// Header panel showing the serial "Plan it" queue: the suggestion being
// planned right now, the ones waiting their turn (removable), failures
// (retryable), and a session success count. Hidden when there is nothing to
// show.
export function PlanQueuePanel({ state }: PlanQueuePanelProps) {
  const { t } = useTranslation('suggestions')
  const { active: planning, queued, failed, doneCount } = state

  const busy = planning !== null || queued.length > 0 || failed.length > 0
  if (!busy && doneCount === 0) return null

  return (
    <div
      data-testid="plan-queue-panel"
      className="space-y-2 rounded-lg border border-gray-800 bg-gray-900/60 px-4 py-3"
    >
      <div className="flex items-center justify-between gap-2">
        <span className="inline-flex items-center gap-2 text-xs font-medium uppercase tracking-wide text-gray-400">
          <ListOrdered size={14} aria-hidden="true" />
          {t('queue.title')}
        </span>
        {!busy && (
          <button
            type="button"
            onClick={clearPlanQueueDone}
            aria-label={t('queue.clear')}
            data-testid="plan-queue-clear"
            className="text-gray-500 hover:text-gray-300"
          >
            <X size={14} aria-hidden="true" />
          </button>
        )}
      </div>

      {planning && (
        <p
          data-testid="plan-queue-current"
          className="flex items-center gap-2 text-sm text-blue-200"
        >
          <Loader2 size={14} className="animate-spin shrink-0" aria-hidden="true" />
          <span className="truncate">
            {t('queue.planningNow', { title: planning.suggestion.title })}
          </span>
        </p>
      )}

      {queued.length > 0 && (
        <div className="space-y-1">
          <p className="text-xs text-gray-400">
            {t('queue.queuedCount', { count: queued.length })}
          </p>
          <ul data-testid="plan-queue-list" className="space-y-1">
            {queued.map(entry => (
              <li
                key={entry.suggestion.id}
                className="flex items-center justify-between gap-2 text-sm text-gray-300"
              >
                <span className="truncate">{entry.suggestion.title}</span>
                <button
                  type="button"
                  onClick={() => removeQueuedPlan(entry.suggestion.id)}
                  aria-label={t('queue.remove', { title: entry.suggestion.title })}
                  className="shrink-0 text-gray-500 hover:text-red-300"
                >
                  <X size={14} aria-hidden="true" />
                </button>
              </li>
            ))}
          </ul>
        </div>
      )}

      {failed.length > 0 && (
        <ul data-testid="plan-queue-failed" className="space-y-1.5">
          {failed.map(entry => (
            <li key={entry.suggestion.id} className="text-sm">
              <div className="flex items-start gap-2 text-red-300">
                <XCircle size={14} className="mt-0.5 shrink-0" aria-hidden="true" />
                <span className="min-w-0 flex-1 break-words">
                  {t('queue.failed', {
                    title: entry.suggestion.title,
                    error: entry.error,
                  })}
                </span>
                <span className="flex shrink-0 items-center gap-2">
                  <button
                    type="button"
                    onClick={() => enqueuePlan(entry.suggestion, entry.feedback)}
                    className="inline-flex items-center gap-1 rounded-md border border-gray-700 bg-gray-800 px-2 py-0.5 text-xs font-medium text-gray-200 hover:border-gray-600 hover:text-white"
                  >
                    <RotateCcw size={12} aria-hidden="true" />
                    {t('queue.retry')}
                  </button>
                  <button
                    type="button"
                    onClick={() => dismissFailedPlan(entry.suggestion.id)}
                    aria-label={t('queue.dismissFailed', { title: entry.suggestion.title })}
                    className="text-gray-500 hover:text-gray-300"
                  >
                    <X size={14} aria-hidden="true" />
                  </button>
                </span>
              </div>
            </li>
          ))}
        </ul>
      )}

      {doneCount > 0 && (
        <p
          data-testid="plan-queue-done"
          className="flex items-center gap-2 text-xs text-emerald-300"
        >
          <CheckCircle2 size={14} aria-hidden="true" />
          {t('queue.doneCount', { count: doneCount })}
        </p>
      )}
    </div>
  )
}
