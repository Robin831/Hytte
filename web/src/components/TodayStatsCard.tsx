import { useTranslation } from 'react-i18next'
import { BarChart2 } from 'lucide-react'
import type { TodayStats } from '../hooks/useForgeStatus'

interface TodayStatsCardProps {
  stats: TodayStats
}

export default function TodayStatsCard({ stats }: TodayStatsCardProps) {
  const { t, i18n } = useTranslation('forge')

  const formattedCost = new Intl.NumberFormat(i18n.language, {
    style: 'currency',
    currency: 'USD',
    minimumFractionDigits: 2,
    maximumFractionDigits: 4,
  }).format(stats.cost)

  return (
    <div className="bg-gray-800 rounded-xl border border-gray-700/50 overflow-hidden">
      <div className="flex items-center gap-2 px-5 py-4 border-b border-gray-700/50">
        <BarChart2 size={18} className="text-cyan-400 shrink-0" />
        <h2 className="text-sm font-medium text-gray-300">{t('todayStats.title')}</h2>
      </div>

      <div className="grid grid-cols-3 divide-x divide-gray-700/40">
        <div className="px-5 py-4 flex flex-col gap-1">
          <span className="text-xs text-gray-500">{t('todayStats.cost')}</span>
          <span className="text-lg font-semibold text-white">{formattedCost}</span>
        </div>
        <div className="px-5 py-4 flex flex-col gap-1">
          <span className="text-xs text-gray-500">{t('todayStats.beadsProcessed')}</span>
          <span className="text-lg font-semibold text-white">{stats.beads_processed}</span>
        </div>
        <div className="px-5 py-4 flex flex-col gap-1">
          <span className="text-xs text-gray-500">{t('todayStats.prsCreated')}</span>
          <span className="text-lg font-semibold text-white">{stats.prs_created}</span>
        </div>
      </div>
    </div>
  )
}
