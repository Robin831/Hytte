import { useState, useMemo, useRef, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { Hammer, Circle, Users, GitPullRequest, List, AlertTriangle, RefreshCw, RotateCcw, Settings } from 'lucide-react'
import { NavLink } from 'react-router-dom'
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
import { usePanelCollapse } from '../hooks/usePanelCollapse'

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

  // Resizable panels state — pixel heights for each panel
  const PANEL_STORAGE_KEY = 'forge-dashboard-panel-heights'
  const defaultPanelHeights = { workers: 200, live: 400 }
  const [panelHeights, setPanelHeights] = useState<typeof defaultPanelHeights>(() => {
    try {
      const stored = localStorage.getItem(PANEL_STORAGE_KEY)
      if (stored) {
        const parsed = JSON.parse(stored)
        if (parsed && typeof parsed === 'object') {
          const parsedRecord = parsed as Record<string, unknown>
          const workers = parsedRecord.workers as number
          const live = parsedRecord.live as number
          if (
            typeof workers === 'number' && Number.isFinite(workers) && workers >= 100 && workers <= 1200 &&
            typeof live === 'number' && Number.isFinite(live) && live >= 100 && live <= 1200
          ) {
            return { workers, live }
          }
        }
      }
    } catch {
      // ignore parse errors, fall back to defaults
    }
    return defaultPanelHeights
  })
  // Tracks cleanup for any in-progress drag so unmount and window blur can cancel it
  const activeDragCleanupRef = useRef<(() => void) | null>(null)

  useEffect(() => {
    return () => {
      activeDragCleanupRef.current?.()
      activeDragCleanupRef.current = null
    }
  }, [])

  // Collapse state shared with WorkersCard/LiveActivity (same localStorage keys)
  const [workersOpen] = usePanelCollapse('workers')
  const [liveOpen] = usePanelCollapse('live-activity')

  function makePanelDragHandler(panel: 'workers' | 'live') {
    return function handleDragStart(e: React.PointerEvent) {
      e.preventDefault()
      const startY = e.clientY
      const startHeight = panelHeights[panel]
      let lastHeights: typeof defaultPanelHeights | null = null

      const cleanup = () => {
        document.removeEventListener('pointermove', onMove)
        document.removeEventListener('pointerup', onUp)
        window.removeEventListener('blur', onBlur)
        // eslint-disable-next-line react-hooks/refs
        activeDragCleanupRef.current = null
      }

      const onMove = (ev: PointerEvent) => {
        const delta = ev.clientY - startY
        const newHeight = Math.max(100, Math.min(startHeight + delta, 1200))
        const next = { ...panelHeights, [panel]: newHeight }
        lastHeights = next
        setPanelHeights(next)
      }

      const onUp = () => {
        cleanup()
        if (lastHeights) {
          try {
            localStorage.setItem(PANEL_STORAGE_KEY, JSON.stringify(lastHeights))
          } catch {
            // ignore quota exceeded or storage disabled
          }
        }
      }

      const onBlur = () => { cleanup() }

      // Cancel any previous drag before starting a new one
      activeDragCleanupRef.current?.()
      activeDragCleanupRef.current = cleanup
      document.addEventListener('pointermove', onMove)
      document.addEventListener('pointerup', onUp)
      window.addEventListener('blur', onBlur, { once: true })
    }
  }

  function makeKeyboardResizeHandler(panel: 'workers' | 'live') {
    return function(delta: number) {
      const step = 20 // pixels per keypress
      setPanelHeights(prev => {
        const newHeight = Math.max(100, Math.min(prev[panel] + delta * step, 1200))
        const next = { ...prev, [panel]: newHeight }
        try {
          localStorage.setItem(PANEL_STORAGE_KEY, JSON.stringify(next))
        } catch {
          // ignore quota exceeded or storage disabled
        }
        return next
      })
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

        {/* Settings — admin only */}
        {user?.is_admin && (
          <NavLink
            to="/forge/settings"
            className="flex items-center gap-1.5 min-h-[36px] px-3 rounded-lg text-sm font-medium transition-colors
              bg-gray-700 text-gray-300 border border-gray-600
              hover:bg-gray-600"
            aria-label={t('actions.settings')}
          >
            <Settings size={14} />
            <span className="hidden sm:inline">{t('actions.settings')}</span>
          </NavLink>
        )}

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

          {/* Panel group: Workers | handle | Live Activity | handle | lower panels */}
          <div className="flex flex-col">
            <div
              id="workers"
              style={{
                height: workersOpen ? panelHeights.workers : undefined,
                overflow: workersOpen ? 'hidden' : undefined,
              }}
            >
              <WorkersCard
                workers={activeWorkers}
                showToast={showToast}
                selectedWorkerId={selectedWorkerId}
                onSelectWorker={setUserSelectedWorkerId}
              />
            </div>

            <ResizePanelHandle
              id="workers-live"
              aria-label={t('splitter.workersLive')}
              onPointerDown={makePanelDragHandler('workers')}
              onKeyboardResize={makeKeyboardResizeHandler('workers')}
              value={panelHeights.workers}
              min={100}
              max={1200}
            />

            <div
              id="live-activity"
              style={{
                height: liveOpen ? panelHeights.live : undefined,
                overflow: liveOpen ? 'hidden' : undefined,
              }}
            >
              <LiveActivity selectedWorker={selectedWorker} resizable />
            </div>

            <ResizePanelHandle
              id="live-lower"
              aria-label={t('splitter.liveLower')}
              onPointerDown={makePanelDragHandler('live')}
              onKeyboardResize={makeKeyboardResizeHandler('live')}
              value={panelHeights.live}
              min={100}
              max={1200}
            />

            <div id="lower-panels">
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
