import { useTranslation } from 'react-i18next'
import { Circle, Users, DollarSign, Clock } from 'lucide-react'
import { useForgeStatus } from '../../hooks/useForgeStatus'

interface ChipProps {
  icon: React.ReactNode
  label: string
  value: string
  variant?: 'default' | 'success' | 'danger'
}

function Chip({ icon, label, value, variant = 'default' }: ChipProps) {
  const bg =
    variant === 'success'
      ? 'bg-green-900/40 border-green-700/50'
      : variant === 'danger'
        ? 'bg-red-900/40 border-red-700/50'
        : 'bg-gray-800 border-gray-700/50'

  return (
    <div className={`flex items-center gap-2 rounded-lg border px-3 py-1.5 text-sm ${bg}`}>
      {icon}
      <span className="text-gray-400 hidden sm:inline">{label}</span>
      <span className="font-medium text-white">{value}</span>
    </div>
  )
}

export default function StatusBar() {
  const { t, i18n } = useTranslation('forge')
  const { status, loading } = useForgeStatus()

  if (loading && !status) {
    return (
      <div className="flex items-center gap-3 rounded-lg bg-gray-800/50 border border-gray-700/50 px-4 py-2">
        <span className="text-sm text-gray-500">{t('mezzanine.statusBar.loading')}</span>
      </div>
    )
  }

  const daemonHealthy = status?.daemon_healthy ?? false
  const workerCount = status?.workers.active ?? 0
  const todayCost = status?.today_stats?.cost ?? 0
  const lastPoll = status?.recent_events?.[0]?.timestamp

  const formattedCost = new Intl.NumberFormat(i18n.language, {
    style: 'currency',
    currency: 'USD',
    minimumFractionDigits: 2,
  }).format(todayCost)

  const formattedTime = lastPoll
    ? new Intl.DateTimeFormat(i18n.language, {
        hour: '2-digit',
        minute: '2-digit',
        second: '2-digit',
      }).format(new Date(lastPoll))
    : '--:--'

  return (
    <div className="flex flex-wrap items-center gap-2">
      <Chip
        icon={
          <Circle
            size={10}
            className={daemonHealthy ? 'fill-green-400 text-green-400' : 'fill-red-400 text-red-400'}
          />
        }
        label={t('mezzanine.statusBar.daemon')}
        value={daemonHealthy ? t('daemonOnline') : t('daemonOffline')}
        variant={daemonHealthy ? 'success' : 'danger'}
      />
      <Chip
        icon={<Users size={14} className="text-gray-400" />}
        label={t('mezzanine.statusBar.workers')}
        value={String(workerCount)}
      />
      <Chip
        icon={<DollarSign size={14} className="text-gray-400" />}
        label={t('mezzanine.statusBar.dailyCost')}
        value={formattedCost}
      />
      <Chip
        icon={<Clock size={14} className="text-gray-400" />}
        label={t('mezzanine.statusBar.lastPoll')}
        value={formattedTime}
      />
    </div>
  )
}
