import { useState, useEffect, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { Hammer, Circle, Users, GitPullRequest, List, AlertTriangle } from 'lucide-react'

export interface ForgeStatus {
  daemon_healthy: boolean
  daemon_error?: string
  workers: {
    active: number
    completed: number
  }
  prs_open: number
  queue_ready: number
  needs_human: number
}

export function useForgeStatus() {
  const { t } = useTranslation('forge')
  const [status, setStatus] = useState<ForgeStatus | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)

  const fetchStatus = useCallback(async (signal: AbortSignal) => {
    try {
      const res = await fetch('/api/forge/status', { credentials: 'include', signal })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        setError((data as { error?: string }).error ?? `HTTP ${res.status}`)
        return
      }
      const data: ForgeStatus = await res.json()
      setStatus(data)
      setError(null)
    } catch (err) {
      if (err instanceof Error && err.name === 'AbortError') return
      setError(err instanceof Error ? err.message : t('unknownError'))
    } finally {
      setLoading(false)
    }
  }, [t])

  useEffect(() => {
    const controller = new AbortController()
    fetchStatus(controller.signal)
    const id = setInterval(() => fetchStatus(controller.signal), 5000)
    return () => {
      controller.abort()
      clearInterval(id)
    }
  }, [fetchStatus])

  return { status, error, loading }
}

interface StatCardProps {
  icon: React.ReactNode
  label: string
  value: number
  sub?: string
  highlight?: boolean
}

function StatCard({ icon, label, value, sub, highlight }: StatCardProps) {
  return (
    <div
      className={`bg-gray-800 rounded-xl p-5 flex flex-col gap-3 border ${
        highlight ? 'border-amber-600/50' : 'border-gray-700/50'
      }`}
    >
      <div className="flex items-center gap-2 text-gray-400 text-sm">
        {icon}
        <span>{label}</span>
      </div>
      <p className={`text-3xl font-bold ${highlight ? 'text-amber-400' : 'text-white'}`}>
        {value}
      </p>
      {sub && <p className="text-xs text-gray-500">{sub}</p>}
    </div>
  )
}

export default function ForgeDashboardPage() {
  const { t } = useTranslation('forge')
  const { status, error, loading } = useForgeStatus()

  return (
    <div className="p-6 max-w-5xl mx-auto">
      <div className="flex items-center gap-3 mb-6">
        <Hammer size={24} className="text-amber-400" />
        <h1 className="text-2xl font-semibold text-white">{t('title')}</h1>
        {!loading && (
          <span
            className={`ml-auto flex items-center gap-1.5 text-sm ${
              status === null ? 'text-gray-500' : status.daemon_healthy ? 'text-green-400' : 'text-red-400'
            }`}
          >
            <Circle size={8} fill="currentColor" />
            {status === null ? t('daemonUnknown') : status.daemon_healthy ? t('daemonOnline') : t('daemonOffline')}
          </span>
        )}
      </div>

      {error && (
        <div className="bg-red-900/30 border border-red-700/50 rounded-lg p-4 mb-6 text-red-300 text-sm">
          {error}
        </div>
      )}

      {loading ? (
        <div className="flex items-center justify-center h-48">
          <div className="animate-spin rounded-full h-8 w-8 border-2 border-gray-600 border-t-amber-400" />
        </div>
      ) : (
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
          <StatCard
            icon={<Users size={20} className="text-blue-400" />}
            label={t('activeWorkers')}
            value={status?.workers.active ?? 0}
            sub={t('completedWorkers', { count: status?.workers.completed ?? 0 })}
          />
          <StatCard
            icon={<GitPullRequest size={20} className="text-purple-400" />}
            label={t('openPRs')}
            value={status?.prs_open ?? 0}
          />
          <StatCard
            icon={<List size={20} className="text-cyan-400" />}
            label={t('queueReady')}
            value={status?.queue_ready ?? 0}
          />
          <StatCard
            icon={
              <AlertTriangle
                size={20}
                className={status?.needs_human ? 'text-amber-400' : 'text-gray-500'}
              />
            }
            label={t('needsHuman')}
            value={status?.needs_human ?? 0}
            highlight={!!status?.needs_human}
          />
        </div>
      )}
    </div>
  )
}
