import { useState, useEffect } from 'react'
import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { Circle, Users, DollarSign, Clock, AlertTriangle } from 'lucide-react'
import { useForgeStatus } from '../../hooks/useForgeStatus'

interface ChipProps {
  icon: ReactNode
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

interface BackendEvent {
  id: number
  timestamp: string
  type: string
  message: string
  bead_id: string
  anvil: string
}

interface CostSummary {
  period: string
  estimated_cost: number
}

export default function StatusBar() {
  const { t } = useTranslation('forge')
  const { status, error, loading } = useForgeStatus()
  const [todayCost, setTodayCost] = useState<number>(0)
  const [lastPoll, setLastPoll] = useState<string | undefined>(undefined)

  useEffect(() => {
    let cancelled = false
    let timeoutId: ReturnType<typeof setTimeout> | undefined

    async function fetchExtras() {
      try {
        const [costsRes, eventsRes] = await Promise.all([
          fetch('/api/forge/costs?period=today', { credentials: 'include' }),
          fetch('/api/forge/events?limit=1', { credentials: 'include' }),
        ])
        if (cancelled) return
        if (costsRes.ok) {
          const data: CostSummary = await costsRes.json()
          if (!cancelled) setTodayCost(data.estimated_cost ?? 0)
        }
        if (eventsRes.ok) {
          const data: BackendEvent[] = await eventsRes.json()
          if (!cancelled && data.length > 0) setLastPoll(data[0].timestamp)
        }
      } catch {
        // best-effort — cost/event data is supplementary
      } finally {
        if (!cancelled) {
          timeoutId = setTimeout(() => void fetchExtras(), 30000)
        }
      }
    }

    void fetchExtras()
    return () => {
      cancelled = true
      if (timeoutId !== undefined) clearTimeout(timeoutId)
    }
  }, [])

  if (loading && !status) {
    return (
      <div className="flex items-center gap-3 rounded-lg bg-gray-800/50 border border-gray-700/50 px-4 py-2">
        <span className="text-sm text-gray-500">{t('mezzanine.statusBar.loading')}</span>
      </div>
    )
  }

  if (error && !status) {
    return (
      <div className="flex items-center gap-2 rounded-lg bg-red-900/30 border border-red-700/50 px-4 py-2">
        <AlertTriangle size={14} className="text-red-400 shrink-0" />
        <span className="text-sm text-red-300">{t('mezzanine.statusBar.unavailable')}</span>
      </div>
    )
  }

  const daemonHealthy = status?.daemon_healthy ?? false
  const workerCount = status?.workers.active ?? 0

  const formattedCost = new Intl.NumberFormat(undefined, {
    style: 'currency',
    currency: 'USD',
    minimumFractionDigits: 2,
  }).format(todayCost)

  const formattedTime = lastPoll
    ? new Intl.DateTimeFormat(undefined, {
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
