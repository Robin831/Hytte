import { useTranslation } from 'react-i18next'
import { Hammer, Circle, Users, GitPullRequest, List, AlertTriangle } from 'lucide-react'
import { useForgeStatus } from '../hooks/useForgeStatus'
import WorkersCard from '../components/WorkersCard'
import NeedsAttentionCard from '../components/NeedsAttentionCard'
import ReadyToMergeCard from '../components/ReadyToMergeCard'
import TodayStatsCard from '../components/TodayStatsCard'
import RecentEventsCard from '../components/RecentEventsCard'
import QueueSummaryCard from '../components/QueueSummaryCard'

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
  const { t: tc } = useTranslation('common')
  const { status, error, loading } = useForgeStatus()

  const activeWorkers = status?.worker_list.filter(w => w.status === 'pending' || w.status === 'running') ?? []
  const completedWorkers = status?.worker_list.filter(w => w.status !== 'pending' && w.status !== 'running') ?? []

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
          <div
            className="animate-spin rounded-full h-8 w-8 border-2 border-gray-600 border-t-amber-400"
            role="status"
            aria-live="polite"
            aria-busy="true"
          >
            <span className="sr-only">{tc('status.loading')}</span>
          </div>
        </div>
      ) : (
        <div className="flex flex-col gap-6">
          {/* Summary stat cards */}
          <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
            <StatCard
              icon={<Users size={20} className="text-blue-400" />}
              label={t('activeWorkers')}
              value={activeWorkers.length}
              sub={t('completedWorkers', { count: completedWorkers.length })}
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

          {/* Detailed cards */}
          <WorkersCard workers={activeWorkers} />
          <NeedsAttentionCard stuck={status?.stuck ?? []} />
          <ReadyToMergeCard prs={status?.ready_to_merge ?? []} />
          {status?.today_stats && <TodayStatsCard stats={status.today_stats} />}
          <RecentEventsCard events={status?.recent_events ?? []} />
          {status?.queue && status.queue.length > 0 && (
            <QueueSummaryCard queue={status.queue} />
          )}
        </div>
      )}
    </div>
  )
}
