import { useState, useMemo, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { Hammer, Circle, Users, GitPullRequest, List, AlertTriangle, RefreshCw, RotateCcw } from 'lucide-react'
import { useAuth } from '../auth'
import { useForgeStatus, useForgeWorkers } from '../hooks/useForgeStatus'
import { useToast } from '../hooks/useToast'
import WorkersCard from '../components/WorkersCard'
import NeedsAttentionCard from '../components/NeedsAttentionCard'
import ReadyToMergeCard from '../components/ReadyToMergeCard'
import TodayStatsCard from '../components/TodayStatsCard'
import CostsDashboardCard from '../components/CostsDashboardCard'
import FullQueueCard from '../components/FullQueueCard'
import LiveActivity from '../components/LiveActivity'
import ConfirmDialog from '../components/ConfirmDialog'
import ToastList from '../components/ToastList'
import { ResizePanelHandle } from '../components/ResizePanelHandle'

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
  const { status, error, loading: statusLoading } = useForgeStatus()
  const { workers: allWorkers, loading: workersLoading } = useForgeWorkers()
  const loading = statusLoading || workersLoading
  const { toasts, showToast } = useToast()

  const [refreshing, setRefreshing] = useState(false)
  const [confirmRefresh, setConfirmRefresh] = useState(false)
  const [confirmRestart, setConfirmRestart] = useState(false)
  const [restarting, setRestarting] = useState(false)
  const [userSelectedWorkerId, setUserSelectedWorkerId] = useState<string | null>(null)

  // Fetch workers independently from /api/forge/workers, which reads state.db
  // directly and does not depend on the /api/forge/status IPC health check. This
  // keeps all phases — smith, temper, warden, burnish, rebase, bellows — visible
  // even when the status endpoint is slow or temporarily failing.
  const activeWorkers = allWorkers.filter(w => w.status === 'pending' || w.status === 'running')
  const completedWorkers = allWorkers.filter(w => w.status !== 'pending' && w.status !== 'running')

  // Derive the effective selected worker ID during render to avoid calling setState
  // inside a useEffect. Auto-selects the most recently started active worker; when
  // a worker completes, switches to the next active one (or keeps showing the
  // completed worker's output). A user click sets userSelectedWorkerId to override.
  const selectedWorkerId = useMemo(() => {
    const sortedActive = [...activeWorkers].sort(
      (a, b) => new Date(b.started_at).getTime() - new Date(a.started_at).getTime()
    )

    if (userSelectedWorkerId === null) {
      // Initial selection: pick most recently started active worker, or most recently
      // completed worker as fallback so the panel is never empty.
      if (sortedActive.length > 0) return sortedActive[0].id
      const lastCompleted = [...completedWorkers].sort((a, b) => {
        const bTime = Date.parse(b.completed_at ?? b.updated_at ?? '') || 0
        const aTime = Date.parse(a.completed_at ?? a.updated_at ?? '') || 0
        return bTime - aTime
      })[0]
      return lastCompleted?.id ?? null
    }

    // If the user-selected worker is no longer active, switch to the next active
    // worker (if any). If none are active, keep showing the completed worker's
    // output — its log file is still readable.
    const selectedIsActive = activeWorkers.some(w => w.id === userSelectedWorkerId)
    if (!selectedIsActive && sortedActive.length > 0) {
      return sortedActive[0].id
    }

    return userSelectedWorkerId
  }, [userSelectedWorkerId, activeWorkers, completedWorkers])

  const selectedWorker = allWorkers.find(w => w.id === selectedWorkerId) ?? null

  // Resizable panels state — sizes are percentages summing to 100
  const PANEL_STORAGE_KEY = 'forge-dashboard-splitter'
  const defaultPanelSizes = { workers: 20, live: 45, lower: 35 }
  const [panelSizes, setPanelSizes] = useState<typeof defaultPanelSizes>(() => {
    try {
      const stored = localStorage.getItem(PANEL_STORAGE_KEY)
      if (stored) {
        const parsed = JSON.parse(stored)
        const isValidNumber = (value: unknown) =>
          typeof value === 'number' && Number.isFinite(value) && value >= 5 && value <= 90

        if (
          parsed &&
          typeof parsed === 'object' &&
          isValidNumber((parsed as Record<string, unknown>).workers) &&
          isValidNumber((parsed as Record<string, unknown>).live) &&
          isValidNumber((parsed as Record<string, unknown>).lower)
        ) {
          const workers = (parsed as Record<string, unknown>).workers as number
          const live = (parsed as Record<string, unknown>).live as number
          const lower = (parsed as Record<string, unknown>).lower as number
          const total = workers + live + lower

          if (total > 95 && total < 105) {
            return { workers, live, lower }
          }
        }
      }
    } catch {
      // ignore parse errors, fall back to defaults
    }
    return defaultPanelSizes
  })
  const panelContainerRef = useRef<HTMLDivElement>(null)

  function makePanelDragHandler(which: 'upper' | 'lower') {
    return function handleDragStart(e: React.MouseEvent) {
      e.preventDefault()
      const container = panelContainerRef.current
      if (!container) return
      const containerH = container.getBoundingClientRect().height
      if (containerH <= 0) return
      const startY = e.clientY
      const startSizes = { ...panelSizes }
      let lastSizes: typeof defaultPanelSizes | null = null

      const onMove = (ev: MouseEvent) => {
        const delta = ((ev.clientY - startY) / containerH) * 100
        let next: typeof defaultPanelSizes
        if (which === 'upper') {
          const w = Math.max(10, Math.min(startSizes.workers + delta, 100 - startSizes.lower - 15))
          const l = 100 - w - startSizes.lower
          if (l < 15) return
          next = { workers: w, live: l, lower: startSizes.lower }
        } else {
          const lo = Math.max(10, Math.min(startSizes.lower - delta, 100 - startSizes.workers - 15))
          const l = 100 - startSizes.workers - lo
          if (l < 15) return
          next = { workers: startSizes.workers, live: l, lower: lo }
        }
        lastSizes = next
        setPanelSizes(next)
      }

      const onUp = () => {
        document.removeEventListener('mousemove', onMove)
        document.removeEventListener('mouseup', onUp)
        if (lastSizes) {
          try {
            localStorage.setItem(PANEL_STORAGE_KEY, JSON.stringify(lastSizes))
          } catch {
            // ignore quota exceeded or storage disabled
          }
        }
      }
      document.addEventListener('mousemove', onMove)
      document.addEventListener('mouseup', onUp)
    }
  }

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

          {/* Resizable panel group: Workers | handle | Live Activity | handle | lower panels */}
          <div
            ref={panelContainerRef}
            className="flex flex-col"
            style={{ minHeight: '80vh', height: '80vh' }}
          >
            <div
              id="workers"
              style={{ flex: `${panelSizes.workers} 1 0%`, minHeight: '10%', overflow: 'auto' }}
            >
              <WorkersCard
                workers={activeWorkers}
                showToast={showToast}
                selectedWorkerId={selectedWorkerId}
                onSelectWorker={setUserSelectedWorkerId}
              />
            </div>

            <ResizePanelHandle id="workers-live" aria-label={t('splitter.workersLive')} onMouseDown={makePanelDragHandler('upper')} />

            <div
              id="live-activity"
              style={{ flex: `${panelSizes.live} 1 0%`, minHeight: '15%', overflow: 'hidden' }}
            >
              <LiveActivity selectedWorker={selectedWorker} resizable />
            </div>

            <ResizePanelHandle id="live-lower" aria-label={t('splitter.liveLower')} onMouseDown={makePanelDragHandler('lower')} />

            <div
              id="lower-panels"
              style={{ flex: `${panelSizes.lower} 1 0%`, minHeight: '10%', overflow: 'auto' }}
            >
              <div className="flex flex-col gap-6">
                <NeedsAttentionCard stuck={status?.stuck ?? []} showToast={showToast} />
                <ReadyToMergeCard prs={status?.open_prs ?? []} showToast={showToast} />
                <FullQueueCard showToast={showToast} />
                {status?.today_stats && <TodayStatsCard stats={status.today_stats} />}
                <CostsDashboardCard />
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
