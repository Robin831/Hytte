import { useState, useEffect, useMemo } from 'react'
import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { Circle, Users, DollarSign, Clock, AlertTriangle, RotateCcw, Tag, Settings } from 'lucide-react'
import { useForgeStatus } from '../../hooks/useForgeStatus'
import { computeNeedsAttentionItems } from '../../hooks/useNeedsAttention'
import { useAuth } from '../../auth'
import ConfirmDialog from '../ConfirmDialog'

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

interface StatusBarProps {
  showToast?: (message: string, type: 'success' | 'error') => void
  onNeedsAttentionClick?: () => void
  needsAttentionCount?: number
  onReleaseClick?: () => void
  onSettingsClick?: () => void
}

export default function StatusBar({ showToast, onNeedsAttentionClick, needsAttentionCount: needsAttentionCountProp, onReleaseClick, onSettingsClick }: StatusBarProps) {
  const { t } = useTranslation('forge')
  const { user } = useAuth()
  const { status, error, loading } = useForgeStatus()
  const ownNeedsAttentionCount = useMemo(() => computeNeedsAttentionItems(status).length, [status])
  const needsAttentionCount = needsAttentionCountProp ?? ownNeedsAttentionCount
  const [todayCost, setTodayCost] = useState<number>(0)
  const [lastPoll, setLastPoll] = useState<string | undefined>(undefined)
  const [confirmRestart, setConfirmRestart] = useState(false)
  const [restarting, setRestarting] = useState(false)

  async function handleRestart() {
    setConfirmRestart(false)
    setRestarting(true)
    try {
      const res = await fetch('/api/forge/restart', {
        method: 'POST',
        credentials: 'include',
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        showToast?.((data as { error?: string }).error ?? `HTTP ${res.status}`, 'error')
      } else {
        showToast?.(t('actions.restartSuccess'), 'success')
      }
    } catch (err) {
      showToast?.(err instanceof Error ? err.message : t('unknownError'), 'error')
    } finally {
      setRestarting(false)
    }
  }

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

      {needsAttentionCount > 0 &&
        (onNeedsAttentionClick ? (
          <button
            type="button"
            onClick={onNeedsAttentionClick}
            aria-label={t('mezzanine.statusBar.needsAttention', { count: needsAttentionCount })}
            className="flex items-center gap-1.5 rounded-lg border px-3 py-1.5 text-sm transition-colors
              bg-amber-600/20 text-amber-300 border-amber-600/30
              hover:bg-amber-600/30"
          >
            <AlertTriangle size={14} />
            <span className="font-medium">{needsAttentionCount}</span>
          </button>
        ) : (
          <div
            aria-label={t('mezzanine.statusBar.needsAttention', { count: needsAttentionCount })}
            className="flex items-center gap-1.5 rounded-lg border px-3 py-1.5 text-sm
              bg-amber-600/20 text-amber-300 border-amber-600/30"
          >
            <AlertTriangle size={14} />
            <span className="font-medium">{needsAttentionCount}</span>
          </div>
        ))}

      {user?.is_admin && onReleaseClick && (
        <button
          type="button"
          onClick={onReleaseClick}
          aria-label={t('release.title')}
          className="flex items-center gap-1.5 rounded-lg border px-3 py-1.5 text-sm transition-colors
            bg-emerald-600/20 text-emerald-300 border-emerald-600/30
            hover:bg-emerald-600/30"
        >
          <Tag size={14} />
          <span className="hidden sm:inline">{t('release.title')}</span>
        </button>
      )}

      {user?.is_admin && (
        <button
          type="button"
          onClick={() => setConfirmRestart(true)}
          disabled={restarting}
          aria-label={t('actions.restart')}
          className="flex items-center gap-1.5 rounded-lg border px-3 py-1.5 text-sm transition-colors
            bg-orange-600/20 text-orange-300 border-orange-600/30
            hover:bg-orange-600/30 disabled:opacity-50 disabled:cursor-not-allowed"
        >
          <RotateCcw size={14} className={restarting ? 'animate-spin' : ''} />
          <span className="hidden sm:inline">{t('actions.restart')}</span>
        </button>
      )}

      {user?.is_admin && onSettingsClick && (
        <button
          type="button"
          onClick={onSettingsClick}
          aria-label={t('actions.settings')}
          className="flex items-center gap-1.5 rounded-lg border px-3 py-1.5 text-sm transition-colors
            bg-gray-600/20 text-gray-300 border-gray-600/30
            hover:bg-gray-600/30"
        >
          <Settings size={14} />
          <span className="hidden sm:inline">{t('actions.settings')}</span>
        </button>
      )}

      <ConfirmDialog
        open={confirmRestart}
        title={t('actions.restartConfirmTitle')}
        message={t('actions.restartConfirmMessage')}
        confirmLabel={t('actions.restart')}
        destructive
        onConfirm={() => void handleRestart()}
        onCancel={() => setConfirmRestart(false)}
      />
    </div>
  )
}
