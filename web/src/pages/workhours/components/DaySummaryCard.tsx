import { useTranslation } from 'react-i18next'
import { formatMins } from '../dateUtils'
import type { DaySummary } from '../types'

/** The saved day's totals: gross, deductions, net, reported, remainder and balance. */
export default function DaySummaryCard({ summary }: { summary: DaySummary }) {
  const { t } = useTranslation(['workhours', 'common'])

  return (
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
  )
}
