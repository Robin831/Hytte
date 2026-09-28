import { useTranslation } from 'react-i18next'
import { formatDuration, formatKm, formatPace, type RunStats } from './liveApi'

/** RunStatsBar shows distance, current and average pace, and moving time. */
export default function RunStatsBar({ stats }: { stats: RunStats }) {
  const { t, i18n } = useTranslation('livestream')
  const items = [
    { label: t('stats.distance'), value: formatKm(stats.distanceM, i18n.language), unit: 'km' },
    { label: t('stats.pace'), value: formatPace(stats.currentPaceS), unit: '/km' },
    { label: t('stats.avgPace'), value: formatPace(stats.avgPaceS), unit: '/km' },
    { label: t('stats.time'), value: formatDuration(stats.elapsedS), unit: '' },
  ]
  return (
    <dl className="grid grid-cols-2 gap-2 sm:grid-cols-4">
      {items.map(item => (
        <div key={item.label} className="rounded-xl border border-gray-800 bg-gray-900 px-3 py-2">
          <dt className="text-xs text-gray-400">{item.label}</dt>
          <dd className="text-lg font-semibold tabular-nums text-white">
            {item.value}
            {item.unit && <span className="ml-1 text-xs font-normal text-gray-400">{item.unit}</span>}
          </dd>
        </div>
      ))}
    </dl>
  )
}
