import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Hammer, Circle, Users, GitPullRequest, List, AlertTriangle, RefreshCw, RotateCcw } from 'lucide-react'
import { useAuth } from '../auth'
import { useForgeStatus } from '../hooks/useForgeStatus'
import { useToast } from '../hooks/useToast'
import WorkersCard from '../components/WorkersCard'
import NeedsAttentionCard from '../components/NeedsAttentionCard'
import ReadyToMergeCard from '../components/ReadyToMergeCard'
import TodayStatsCard from '../components/TodayStatsCard'
import RecentEventsCard from '../components/RecentEventsCard'
import QueueSummaryCard from '../components/QueueSummaryCard'
import CostsDashboardCard from '../components/CostsDashboardCard'
import LiveActivity from '../components/LiveActivity'
import ConfirmDialog from '../components/ConfirmDialog'
import ToastList from '../components/ToastList'

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
  const { user } = useAuth()
  const { status, error, loading } = useForgeStatus()
  const { toasts, showToast } = useToast()

  const [refreshing, setRefreshing] = useState(false)
  const [confirmRefresh, setConfirmRefresh] = useState(false)
  const [confirmRestart, setConfirmRestart] = useState(false)
  const [restarting, setRestarting] = useState(false)

  const activeWorkers = status?.worker_list.filter(w => w.status === 'pending' || w.status === 'running') ?? []
  const completedWorkers = status?.worker_list.filter(w => w.status !== 'pending' && w.status !== 'running') ?? []

  async function handleRefresh() {
    setConfirmRefresh(false)
    setRefreshing(true)
    try {
      const res = await fetch('/api/forge/action/refresh', {
        method: 'POST',
        credentials: 'include',
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        showToast((data as { error?: string }).error ?? `HTTP ${res.status}`, 'error')
      } else {
        showToast(t('actions.refreshSuccess'), 'success')
      }
    } catch (err) {
      showToast(err instanceof Error ? err.message : t('unknownError'), 'error')
    } finally {
      setRefreshing(false)
    }
  }

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
        showToast((data as { error?: string }).error ?? `HTTP ${res.status}`, 'error')
      } else {
        showToast(t('actions.restartSuccess'), 'success')
      }
    } catch (err) {
      showToast(err instanceof Error ? err.message : t('unknownError'), 'error')
    } finally {
      setRestarting(false)
    }
  }

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

        {/* Refresh button */}
        <button
          type="button"
          onClick={() => setConfirmRefresh(true)}
          disabled={refreshing}
          aria-label={t('actions.refresh')}
          className="ml-2 flex items-center gap-1.5 min-h-[36px] px-3 rounded-lg text-sm font-medium transition-colors
            bg-gray-700 text-gray-300 border border-gray-600
            hover:bg-gray-600 disabled:opacity-50 disabled:cursor-not-allowed"
        >
          <RefreshCw size={14} className={refreshing ? 'animate-spin' : ''} />
          <span className="hidden sm:inline">{t('actions.refresh')}</span>
        </button>

        {/* Rebuild & Restart — admin only */}
        {user?.is_admin && (
          <button
            type="button"
            onClick={() => setConfirmRestart(true)}
            disabled={restarting}
            aria-label={t('actions.restart')}
            className="flex items-center gap-1.5 min-h-[36px] px-3 rounded-lg text-sm font-medium transition-colors
              bg-orange-600/20 text-orange-300 border border-orange-600/30
              hover:bg-orange-600/30 disabled:opacity-50 disabled:cursor-not-allowed"
          >
            <RotateCcw size={14} className={restarting ? 'animate-spin' : ''} />
            <span className="hidden sm:inline">{t('actions.restart')}</span>
          </button>
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

          {/* Two-column layout: status cards on the left, live activity on the right */}
          <div className="grid grid-cols-1 xl:grid-cols-3 gap-6">
            {/* Left column: detailed status cards */}
            <div className="xl:col-span-2 flex flex-col gap-6">
              <WorkersCard workers={activeWorkers} showToast={showToast} />
              <NeedsAttentionCard stuck={status?.stuck ?? []} showToast={showToast} />
              <ReadyToMergeCard prs={status?.ready_to_merge ?? []} showToast={showToast} />
              {status?.today_stats && <TodayStatsCard stats={status.today_stats} />}
              <CostsDashboardCard />
              <RecentEventsCard events={status?.recent_events ?? []} />
              {status?.queue && status.queue.length > 0 && (
                <QueueSummaryCard queue={status.queue} />
              )}
            </div>

            {/* Right column: live activity panel */}
            <div className="xl:col-span-1">
              <div className="sticky top-6">
                <LiveActivity workers={activeWorkers} />
              </div>
            </div>
          </div>
        </div>
      )}

      {/* Confirmation dialogs */}
      <ConfirmDialog
        open={confirmRefresh}
        title={t('actions.refreshConfirmTitle')}
        message={t('actions.refreshConfirmMessage')}
        confirmLabel={t('actions.refresh')}
        onConfirm={() => void handleRefresh()}
        onCancel={() => setConfirmRefresh(false)}
      />

      <ConfirmDialog
        open={confirmRestart}
        title={t('actions.restartConfirmTitle')}
        message={t('actions.restartConfirmMessage')}
        confirmLabel={t('actions.restart')}
        destructive
        onConfirm={() => void handleRestart()}
        onCancel={() => setConfirmRestart(false)}
      />

      <ToastList toasts={toasts} />
    </div>
  )
}
