import { useTranslation } from 'react-i18next'
import { formatDate } from '../../../utils/formatDate'
import { formatMins } from '../dateUtils'
import type { LiveEstimate } from '../usePunchClock'

/** The live "if punched out now" totals, or why they can't be shown. */
export default function LiveEstimateCard({ liveEstimate }: { liveEstimate: LiveEstimate }) {
  const { t } = useTranslation(['workhours', 'common'])

  if (liveEstimate.kind !== 'estimate') {
    // A punch-in that has been open for longer than the allowed
    // wrap is forgotten, not running — say so instead of claiming
    // the start time is in the future.
    return (
      <div className="rounded-lg border border-yellow-700/50 bg-yellow-900/20 px-3 py-2 text-xs text-yellow-400">
        {liveEstimate.kind === 'stale'
          ? t('workhours:punchEstimate.stalePunch', {
              date: formatDate(liveEstimate.punchDate + 'T12:00:00', { year: 'numeric', month: 'long', day: 'numeric' }),
            })
          : t('workhours:punchEstimate.invalidStart')}
      </div>
    )
  }
  const estimate = liveEstimate.calc
  const overStandard = estimate.balance > 0
  const atStandard = estimate.balance >= 0
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
          <span className="text-white font-mono text-sm">{formatMins(estimate.net)}</span>
        </div>
        <div>
          <span className="text-gray-400 text-xs">{t('workhours:punchEstimate.reported')}</span>{' '}
          <span className="text-white font-mono text-sm">{formatMins(estimate.reported)}</span>
        </div>
      </div>
      <div className="flex flex-wrap gap-x-3 gap-y-0.5 mt-1 text-xs text-gray-500">
        <span>{t('workhours:punchEstimate.gross')}: <span className="font-mono">{formatMins(estimate.gross)}</span></span>
        {estimate.lunch > 0 && (
          <span>{t('workhours:punchEstimate.lunch')}: <span className="font-mono">{formatMins(estimate.lunch)}</span></span>
        )}
        {estimate.deductions > 0 && (
          <span>{t('workhours:punchEstimate.deductions')}: <span className="font-mono">{formatMins(estimate.deductions)}</span></span>
        )}
      </div>
      {overStandard && (
        <div className="mt-1 text-xs text-green-400">
          {t('workhours:punchEstimate.overStandard', { amount: formatMins(estimate.balance) })}
        </div>
      )}
      {atStandard && !overStandard && (
        <div className="mt-1 text-xs text-green-400">
          {t('workhours:punchEstimate.atStandard')}
        </div>
      )}
    </section>
  )
}
