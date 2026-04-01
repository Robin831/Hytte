import { useTranslation } from 'react-i18next'
import { BarChart2 } from 'lucide-react'
import type { TodayStats } from '../hooks/useForgeStatus'
import { CollapsiblePanelHeader } from './CollapsiblePanelHeader'
import { usePanelCollapse } from '../hooks/usePanelCollapse'

interface TodayStatsCardProps {
  stats: TodayStats
}

export default function TodayStatsCard({ stats }: TodayStatsCardProps) {
  const { t } = useTranslation('forge')
  const [isOpen, toggle] = usePanelCollapse('today-stats')

  const formattedCost = new Intl.NumberFormat(undefined, {
    style: 'currency',
    currency: 'USD',
    minimumFractionDigits: 2,
    maximumFractionDigits: 4,
  }).format(stats.cost)

  return (
    <div className="bg-gray-800 rounded-xl border border-gray-700/50 overflow-hidden">
      <CollapsiblePanelHeader
        isOpen={isOpen}
        toggle={toggle}
        panelId="today-stats-panel"
        icon={<BarChart2 size={18} className="text-cyan-400 shrink-0" />}
        title={t('todayStats.title')}
      />

      <div id="today-stats-panel" hidden={!isOpen}>
        <div className="grid grid-cols-1 sm:grid-cols-3 divide-y sm:divide-y-0 sm:divide-x divide-gray-700/40">
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
    </div>
  )
}
