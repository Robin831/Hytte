import { useTranslation } from 'react-i18next'
import type { TrainingStatus } from '../types/training'

interface TrainingStatusBadgeProps {
  status: TrainingStatus
}

const STATUS_COLORS: Record<TrainingStatus, string> = {
  insufficient_data: 'bg-gray-500/20 text-gray-400 border-gray-500/30',
  detraining: 'bg-yellow-500/20 text-yellow-400 border-yellow-500/30',
  freshening: 'bg-cyan-500/20 text-cyan-400 border-cyan-500/30',
  optimal: 'bg-green-500/20 text-green-400 border-green-500/30',
  increasing: 'bg-blue-500/20 text-blue-400 border-blue-500/30',
  high_load: 'bg-yellow-500/20 text-yellow-400 border-yellow-500/30',
  overreaching: 'bg-red-500/20 text-red-400 border-red-500/30',
}

export default function TrainingStatusBadge({ status }: TrainingStatusBadgeProps) {
  const { t } = useTranslation('training')
  const label = t(`trends.weeklyLoad.statusLabels.${status}`, { defaultValue: status })
  const description = t(`trends.weeklyLoad.statusDescriptions.${status}`, { defaultValue: '' })
  const colorClass = STATUS_COLORS[status] ?? STATUS_COLORS.insufficient_data

  return (
    <div className="flex flex-col items-end gap-0.5">
      <span className={`inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium border ${colorClass}`}>
        {label}
      </span>
      {description && (
        <span className="text-xs text-gray-500">{description}</span>
      )}
    </div>
  )
}
