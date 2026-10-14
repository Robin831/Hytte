import { useTranslation } from 'react-i18next'
import { formatMins } from '../dateUtils'
import type { FlexState } from '../types'

/** The flex pool's running total, with a redeem button once a rounding interval has built up. */
export default function FlexPoolCard({
  flex,
  redeeming,
  onRedeem,
}: {
  flex: FlexState
  redeeming: boolean
  onRedeem: () => void
}) {
  const { t } = useTranslation(['workhours', 'common'])

  return (
    <section className="flex items-center justify-between bg-gray-800/50 rounded-lg px-4 py-3">
      <span className="text-sm text-gray-400">{t('workhours:flexPool')}</span>
      <div className="flex items-center gap-2">
        {flex.flex.total_minutes >= (flex.rounding_minutes ?? 30) && (
          <button
            onClick={onRedeem}
            disabled={redeeming}
            className="text-xs px-2 py-1 rounded bg-blue-600 hover:bg-blue-500 text-white disabled:opacity-50"
          >
            {t('workhours:redeemFlex', { minutes: flex.rounding_minutes ?? 30 })}
          </button>
        )}
        <span
          className={`font-mono text-sm font-semibold ${flex.flex.total_minutes < 0 ? 'text-red-400' : 'text-green-400'}`}
        >
          {flex.flex.total_minutes > 0 ? '+' : ''}
          {formatMins(flex.flex.total_minutes)}
        </span>
      </div>
    </section>
  )
}
